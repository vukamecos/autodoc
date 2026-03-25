// Package ratelimit provides concurrency and rate limiting for LLM API calls.
// It prevents overloading external providers when many changes arrive at once.
package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Limiter controls concurrency and request rate for outgoing API calls.
// It combines a semaphore (max concurrent requests) with a token bucket
// (max requests per interval) to provide backpressure under load.
type Limiter struct {
	sem     chan struct{}
	mu      sync.Mutex
	tokens  int
	maxRate int
	refill  time.Duration
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// Config controls the limiter's behavior.
type Config struct {
	// MaxConcurrent is the maximum number of simultaneous API calls.
	// Zero means no concurrency limit.
	MaxConcurrent int

	// MaxPerInterval is the maximum number of requests per RefillInterval.
	// Zero means no rate limit.
	MaxPerInterval int

	// RefillInterval is how often the rate limit token bucket refills.
	// Defaults to 1 minute if zero.
	RefillInterval time.Duration
}

// New creates a Limiter. Call Start() to begin the refill loop.
func New(cfg Config) *Limiter {
	if cfg.RefillInterval <= 0 {
		cfg.RefillInterval = time.Minute
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 10
	}
	if cfg.MaxPerInterval <= 0 {
		cfg.MaxPerInterval = 60
	}

	return &Limiter{
		sem:     make(chan struct{}, cfg.MaxConcurrent),
		tokens:  cfg.MaxPerInterval,
		maxRate: cfg.MaxPerInterval,
		refill:  cfg.RefillInterval,
		stopCh:  make(chan struct{}),
	}
}

// Start begins the token refill loop.
func (l *Limiter) Start() {
	l.wg.Add(1)
	go l.refillLoop()
}

// Stop stops the refill loop.
func (l *Limiter) Stop() {
	close(l.stopCh)
	l.wg.Wait()
}

// Acquire blocks until the caller can proceed (both a semaphore slot and a
// rate-limit token are available) or the context is cancelled.
func (l *Limiter) Acquire(ctx context.Context) error {
	// Wait for a semaphore slot (concurrency limit).
	select {
	case l.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Wait for a rate-limit token.
	for {
		l.mu.Lock()
		if l.tokens > 0 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()

		// No tokens available — wait briefly and check again.
		select {
		case <-ctx.Done():
			// Release the semaphore slot since we didn't use it.
			<-l.sem
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Release returns a semaphore slot. Must be called after Acquire succeeds.
func (l *Limiter) Release() {
	<-l.sem
}

// Available returns the current number of available rate-limit tokens.
func (l *Limiter) Available() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.tokens
}

func (l *Limiter) refillLoop() {
	defer l.wg.Done()

	ticker := time.NewTicker(l.refill)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.mu.Lock()
			l.tokens = l.maxRate
			l.mu.Unlock()
		}
	}
}
