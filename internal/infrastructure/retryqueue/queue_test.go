package retryqueue

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func newTestQueue(t *testing.T, cfg Config) *Queue {
	t.Helper()
	q := New(cfg, slog.Default())
	return q
}

func TestEnqueue_AddsItem(t *testing.T) {
	q := newTestQueue(t, Config{MaxSize: 10, MaxRetries: 3, RetryInterval: time.Hour})

	ok := q.Enqueue("test-1", func(ctx context.Context) error { return nil }, errors.New("fail"))
	if !ok {
		t.Fatal("expected enqueue to succeed")
	}
	if q.Len() != 1 {
		t.Errorf("expected queue length 1, got %d", q.Len())
	}
}

func TestEnqueue_RejectsWhenFull(t *testing.T) {
	q := newTestQueue(t, Config{MaxSize: 2, MaxRetries: 3, RetryInterval: time.Hour})

	q.Enqueue("1", func(ctx context.Context) error { return nil }, nil)
	q.Enqueue("2", func(ctx context.Context) error { return nil }, nil)
	ok := q.Enqueue("3", func(ctx context.Context) error { return nil }, nil)
	if ok {
		t.Fatal("expected enqueue to be rejected when queue is full")
	}
	if q.Len() != 2 {
		t.Errorf("expected queue length 2, got %d", q.Len())
	}
}

func TestProcessOne_SuccessfulRetry(t *testing.T) {
	q := newTestQueue(t, Config{MaxSize: 10, MaxRetries: 3, RetryInterval: time.Hour})

	var called atomic.Int32
	q.Enqueue("test-1", func(ctx context.Context) error {
		called.Add(1)
		return nil
	}, errors.New("initial fail"))

	q.processOne()

	if called.Load() != 1 {
		t.Errorf("expected run to be called once, called %d times", called.Load())
	}
	if q.Len() != 0 {
		t.Errorf("expected queue to be empty after success, got %d", q.Len())
	}
}

func TestProcessOne_FailureRequeues(t *testing.T) {
	q := newTestQueue(t, Config{MaxSize: 10, MaxRetries: 3, RetryInterval: time.Hour})

	q.Enqueue("test-1", func(ctx context.Context) error {
		return errors.New("still failing")
	}, errors.New("initial fail"))

	q.processOne()

	if q.Len() != 1 {
		t.Errorf("expected item to be re-queued, got queue length %d", q.Len())
	}
}

func TestProcessOne_ExhaustsRetries(t *testing.T) {
	q := newTestQueue(t, Config{MaxSize: 10, MaxRetries: 1, RetryInterval: time.Hour})

	q.Enqueue("test-1", func(ctx context.Context) error {
		return errors.New("permanent fail")
	}, errors.New("initial fail"))

	q.processOne()

	// MaxRetries=1, so after 1 attempt it should be discarded.
	if q.Len() != 0 {
		t.Errorf("expected item to be discarded after max retries, got queue length %d", q.Len())
	}
}

func TestProcessOne_EmptyQueue(t *testing.T) {
	q := newTestQueue(t, Config{MaxSize: 10, MaxRetries: 3, RetryInterval: time.Hour})
	q.processOne() // should not panic
}

func TestStartStop(t *testing.T) {
	q := newTestQueue(t, Config{MaxSize: 10, MaxRetries: 3, RetryInterval: 10 * time.Millisecond})
	q.Start()

	var called atomic.Int32
	q.Enqueue("test-1", func(ctx context.Context) error {
		called.Add(1)
		return nil
	}, errors.New("fail"))

	// Wait for the retry loop to process it.
	time.Sleep(50 * time.Millisecond)
	q.Stop()

	if called.Load() < 1 {
		t.Error("expected run to be called at least once")
	}
}

func TestDefaultConfig(t *testing.T) {
	q := New(Config{}, slog.Default())
	if q.cfg.MaxRetries != 3 {
		t.Errorf("expected default MaxRetries=3, got %d", q.cfg.MaxRetries)
	}
	if q.cfg.RetryInterval != 30*time.Second {
		t.Errorf("expected default RetryInterval=30s, got %v", q.cfg.RetryInterval)
	}
	if q.cfg.MaxSize != 100 {
		t.Errorf("expected default MaxSize=100, got %d", q.cfg.MaxSize)
	}
}
