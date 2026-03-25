package circuitbreaker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_Execute_Closed_AllowsRequests(t *testing.T) {
	cb := New(Config{FailureThreshold: 3})

	callCount := 0
	fn := func() error {
		callCount++
		return nil
	}

	for i := range 5 {
		err := cb.Execute(context.TODO(), fn)
		if err != nil {
			t.Fatalf("unexpected error on call %d: %v", i, err)
		}
	}

	if callCount != 5 {
		t.Errorf("expected 5 calls, got %d", callCount)
	}

	if cb.State() != StateClosed {
		t.Errorf("expected state closed, got %s", cb.State())
	}
}

func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	cb := New(Config{
		FailureThreshold: 3,
		Timeout:          5 * time.Second,
	})

	testErr := errors.New("test error")
	fn := func() error {
		return testErr
	}

	for range 3 {
		err := cb.Execute(context.TODO(), fn)
		if err != testErr {
			t.Fatalf("expected test error, got %v", err)
		}
	}

	if cb.State() != StateOpen {
		t.Errorf("expected state open, got %s", cb.State())
	}

	executed := false
	err := cb.Execute(context.TODO(), func() error {
		executed = true
		return nil
	})
	if err != ErrOpenCircuit {
		t.Errorf("expected ErrOpenCircuit, got %v", err)
	}
	if executed {
		t.Error("function should not have been executed when circuit is open")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := New(Config{
		FailureThreshold: 2,
		Timeout:          5 * time.Second,
	})

	for range 2 {
		_ = cb.Execute(context.TODO(), func() error { return errors.New("fail") })
	}

	if cb.State() != StateOpen {
		t.Fatal("circuit should be open")
	}

	cb.Reset()

	if cb.State() != StateClosed {
		t.Errorf("expected state closed after reset, got %s", cb.State())
	}

	executed := false
	err := cb.Execute(context.TODO(), func() error {
		executed = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !executed {
		t.Error("function should have been executed after reset")
	}
}

func TestCircuitBreaker_HalfOpen_AllowsTestRequest(t *testing.T) {
	cb := New(Config{
		FailureThreshold: 3,
		Timeout:          50 * time.Millisecond,
	})

	for range 3 {
		_ = cb.Execute(context.TODO(), func() error { return errors.New("fail") })
	}

	if cb.State() != StateOpen {
		t.Fatal("circuit should be open")
	}

	time.Sleep(100 * time.Millisecond)

	executed := false
	err := cb.Execute(context.TODO(), func() error {
		executed = true
		return nil
	})

	if !executed {
		t.Error("function should have been executed in half-open state")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCircuitBreaker_HalfOpen_SuccessCloses(t *testing.T) {
	cb := New(Config{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
	})

	for range 3 {
		_ = cb.Execute(context.TODO(), func() error { return errors.New("fail") })
	}

	time.Sleep(60 * time.Millisecond)

	_ = cb.Execute(context.TODO(), func() error { return nil })
	if cb.State() != StateHalfOpen {
		t.Errorf("expected half-open after first success, got %s", cb.State())
	}

	_ = cb.Execute(context.TODO(), func() error { return nil })
	if cb.State() != StateClosed {
		t.Errorf("expected closed after second success, got %s", cb.State())
	}
}

func TestCircuitBreaker_HalfOpen_FailureReopens(t *testing.T) {
	cb := New(Config{
		FailureThreshold: 3,
		Timeout:          50 * time.Millisecond,
	})

	for range 3 {
		_ = cb.Execute(context.TODO(), func() error { return errors.New("fail") })
	}

	time.Sleep(60 * time.Millisecond)

	err := cb.Execute(context.TODO(), func() error { return errors.New("fail again") })
	if err == nil {
		t.Error("expected error")
	}

	if cb.State() != StateOpen {
		t.Errorf("expected open after failure in half-open, got %s", cb.State())
	}
}

func TestCircuitBreaker_ResetsFailuresOnSuccess(t *testing.T) {
	cb := New(Config{FailureThreshold: 3})

	callCount := 0
	fn := func() error {
		callCount++
		if callCount < 2 {
			return errors.New("fail")
		}
		return nil
	}

	_ = cb.Execute(context.TODO(), fn)
	_ = cb.Execute(context.TODO(), fn)
	_ = cb.Execute(context.TODO(), fn)

	_ = cb.Execute(context.TODO(), func() error { return errors.New("fail") })
	_ = cb.Execute(context.TODO(), func() error { return errors.New("fail") })

	if cb.State() != StateClosed {
		t.Errorf("expected closed, got %s", cb.State())
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := New(Config{FailureThreshold: 100})

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(fail bool) {
			defer wg.Done()
			if fail {
				_ = cb.Execute(context.TODO(), func() error { return errors.New("fail") })
			} else {
				_ = cb.Execute(context.TODO(), func() error { return nil })
			}
		}(i%2 == 0)
	}
	wg.Wait()

	state := cb.State()
	if state != StateClosed && state != StateOpen {
		t.Errorf("unexpected state: %s", state)
	}
}

func TestCircuitBreaker_StateCallback(t *testing.T) {
	var transitions []string
	callback := func(from, to State) {
		transitions = append(transitions, from.String()+"->"+to.String())
	}

	cb := NewWithCallback(Config{FailureThreshold: 2}, callback)

	_ = cb.Execute(context.TODO(), func() error { return errors.New("fail") })
	_ = cb.Execute(context.TODO(), func() error { return errors.New("fail") })

	if len(transitions) != 1 || transitions[0] != "closed->open" {
		t.Errorf("expected [closed->open], got %v", transitions)
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	cb := New(Config{FailureThreshold: 5})

	_ = cb.Execute(context.TODO(), func() error { return errors.New("fail") })
	_ = cb.Execute(context.TODO(), func() error { return errors.New("fail") })

	state, failures, successes, _ := cb.Stats()

	if state != StateClosed {
		t.Errorf("expected closed, got %s", state)
	}
	if failures != 2 {
		t.Errorf("expected 2 failures, got %d", failures)
	}
	if successes != 0 {
		t.Errorf("expected 0 successes, got %d", successes)
	}
}

func TestState_String(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{State(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.FailureThreshold != 5 {
		t.Errorf("expected FailureThreshold=5, got %d", cfg.FailureThreshold)
	}
	if cfg.SuccessThreshold != 2 {
		t.Errorf("expected SuccessThreshold=2, got %d", cfg.SuccessThreshold)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("expected Timeout=30s, got %v", cfg.Timeout)
	}
}

func TestCircuitBreaker_ContextCancellation(t *testing.T) {
	cb := New(Config{FailureThreshold: 3})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	executed := false
	err := cb.Execute(ctx, func() error {
		executed = true
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !executed {
		t.Error("function should have been executed")
	}
}
