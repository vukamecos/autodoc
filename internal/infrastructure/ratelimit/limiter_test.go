package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcquireRelease(t *testing.T) {
	l := New(Config{MaxConcurrent: 2, MaxPerInterval: 10, RefillInterval: time.Minute})
	l.Start()
	defer l.Stop()

	ctx := context.Background()

	if err := l.Acquire(ctx); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := l.Acquire(ctx); err != nil {
		t.Fatalf("second acquire: %v", err)
	}

	// Release one slot.
	l.Release()
	if err := l.Acquire(ctx); err != nil {
		t.Fatalf("third acquire after release: %v", err)
	}

	l.Release()
	l.Release()
}

func TestConcurrencyLimit(t *testing.T) {
	l := New(Config{MaxConcurrent: 2, MaxPerInterval: 100, RefillInterval: time.Minute})
	l.Start()
	defer l.Stop()

	var active atomic.Int32
	var maxActive atomic.Int32
	var wg sync.WaitGroup

	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			if err := l.Acquire(ctx); err != nil {
				return
			}
			cur := active.Add(1)
			// Track max concurrency.
			for {
				old := maxActive.Load()
				if cur <= old || maxActive.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			active.Add(-1)
			l.Release()
		}()
	}

	wg.Wait()
	if maxActive.Load() > 2 {
		t.Errorf("expected max concurrency <= 2, got %d", maxActive.Load())
	}
}

func TestRateLimit(t *testing.T) {
	l := New(Config{MaxConcurrent: 10, MaxPerInterval: 3, RefillInterval: time.Minute})
	l.Start()
	defer l.Stop()

	ctx := context.Background()

	// Should be able to acquire 3 tokens immediately.
	for range 3 {
		if err := l.Acquire(ctx); err != nil {
			t.Fatalf("acquire: %v", err)
		}
		l.Release()
	}

	if l.Available() != 0 {
		t.Errorf("expected 0 available tokens, got %d", l.Available())
	}
}

func TestAcquireCancelled(t *testing.T) {
	l := New(Config{MaxConcurrent: 1, MaxPerInterval: 0, RefillInterval: time.Minute})
	// Override tokens to 0 so acquire blocks on rate limit.
	l.mu.Lock()
	l.tokens = 0
	l.mu.Unlock()

	l.Start()
	defer l.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := l.Acquire(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
		l.Release()
	}
}

func TestAcquireSemaphoreCancelled(t *testing.T) {
	l := New(Config{MaxConcurrent: 1, MaxPerInterval: 10, RefillInterval: time.Minute})
	l.Start()
	defer l.Stop()

	ctx := context.Background()
	// Acquire the single slot.
	if err := l.Acquire(ctx); err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// Second acquire should block; cancel it.
	ctx2, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := l.Acquire(ctx2)
	if err == nil {
		t.Fatal("expected error when semaphore is full")
		l.Release()
	}

	l.Release() // release first
}

func TestStartStop(t *testing.T) {
	l := New(Config{MaxConcurrent: 5, MaxPerInterval: 10, RefillInterval: 10 * time.Millisecond})
	l.Start()
	time.Sleep(25 * time.Millisecond)
	l.Stop() // should not hang or panic
}

func TestDefaultConfig(t *testing.T) {
	l := New(Config{})
	if cap(l.sem) != 10 {
		t.Errorf("expected default MaxConcurrent=10, got %d", cap(l.sem))
	}
	if l.maxRate != 60 {
		t.Errorf("expected default MaxPerInterval=60, got %d", l.maxRate)
	}
	if l.refill != time.Minute {
		t.Errorf("expected default RefillInterval=1m, got %v", l.refill)
	}
}
