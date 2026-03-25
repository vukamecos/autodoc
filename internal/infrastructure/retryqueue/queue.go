// Package retryqueue provides an in-memory retry queue for failed pipeline runs.
// When an LLM provider is temporarily unavailable (e.g. circuit breaker open),
// the run parameters are queued and retried automatically after a configurable delay.
package retryqueue

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Runnable is a function that can be retried. Typically wraps usecase.Run.
type Runnable func(ctx context.Context) error

// Item represents a queued retry attempt.
type Item struct {
	ID        string
	Run       Runnable
	Attempts  int
	QueuedAt  time.Time
	LastError string
}

// Config controls retry queue behavior.
type Config struct {
	MaxSize       int           // Maximum number of queued items (0 = unlimited).
	MaxRetries    int           // Maximum retry attempts per item before discarding.
	RetryInterval time.Duration // Delay between retry attempts.
}

// Queue is a bounded, in-memory retry queue with background processing.
type Queue struct {
	cfg    Config
	log    *slog.Logger
	mu     sync.Mutex
	items  []Item
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// New creates a retry queue. Call Start() to begin processing.
func New(cfg Config, log *slog.Logger) *Queue {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = 30 * time.Second
	}
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 100
	}
	return &Queue{
		cfg:    cfg,
		log:    log,
		stopCh: make(chan struct{}),
	}
}

// Enqueue adds a failed run to the retry queue. Returns false if the queue is full.
func (q *Queue) Enqueue(id string, run Runnable, lastErr error) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) >= q.cfg.MaxSize {
		q.log.Warn("retryqueue: queue full, dropping item", "id", id, "max_size", q.cfg.MaxSize)
		return false
	}

	errMsg := ""
	if lastErr != nil {
		errMsg = lastErr.Error()
	}

	q.items = append(q.items, Item{
		ID:        id,
		Run:       run,
		Attempts:  0,
		QueuedAt:  time.Now(),
		LastError: errMsg,
	})
	q.log.Info("retryqueue: item queued", "id", id, "queue_size", len(q.items))
	return true
}

// Len returns the current number of queued items.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Start begins the background retry loop.
func (q *Queue) Start() {
	q.wg.Add(1)
	go q.processLoop()
}

// Stop signals the retry loop to stop and waits for it to finish.
func (q *Queue) Stop() {
	close(q.stopCh)
	q.wg.Wait()
}

func (q *Queue) processLoop() {
	defer q.wg.Done()

	ticker := time.NewTicker(q.cfg.RetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-q.stopCh:
			return
		case <-ticker.C:
			q.processOne()
		}
	}
}

func (q *Queue) processOne() {
	q.mu.Lock()
	if len(q.items) == 0 {
		q.mu.Unlock()
		return
	}

	// Take the oldest item.
	item := q.items[0]
	q.items = q.items[1:]
	q.mu.Unlock()

	item.Attempts++
	q.log.Info("retryqueue: retrying item",
		"id", item.ID,
		"attempt", item.Attempts,
		"max_retries", q.cfg.MaxRetries,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := item.Run(ctx); err != nil {
		if item.Attempts >= q.cfg.MaxRetries {
			q.log.Warn("retryqueue: item exhausted retries, discarding",
				"id", item.ID,
				"attempts", item.Attempts,
				"error", err.Error(),
			)
			return
		}

		// Re-queue for another attempt.
		item.LastError = err.Error()
		q.mu.Lock()
		if len(q.items) < q.cfg.MaxSize {
			q.items = append(q.items, item)
			q.log.Info("retryqueue: item re-queued after failure", "id", item.ID, "queue_size", len(q.items))
		} else {
			q.log.Warn("retryqueue: queue full, dropping re-queue", "id", item.ID)
		}
		q.mu.Unlock()
		return
	}

	q.log.Info("retryqueue: item succeeded on retry", "id", item.ID, "attempt", item.Attempts)
}
