package retry

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const fastDelay = 5 * time.Millisecond

// ---------------------------------------------------------------------------
// IsTransportError
// ---------------------------------------------------------------------------

func TestIsTransportError_NetError(t *testing.T) {
	// *url.Error (wrapping net.Error) is what http.Client returns for network issues.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // close immediately so requests fail with a transport error

	client := srv.Client()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, err := client.Do(req)
	if err == nil {
		t.Skip("expected transport error, got nil — possibly a flaky environment")
	}
	if !IsTransportError(err) {
		t.Errorf("expected IsTransportError=true for closed-server error, got false; err=%v", err)
	}
}

func TestIsTransportError_NonNetError(t *testing.T) {
	err := errors.New("some application error")
	if IsTransportError(err) {
		t.Error("expected IsTransportError=false for plain error")
	}
}

func TestIsTransportError_OpError(t *testing.T) {
	opErr := &net.OpError{Op: "dial", Err: errors.New("refused")}
	if !IsTransportError(opErr) {
		t.Error("expected IsTransportError=true for *net.OpError")
	}
}

// ---------------------------------------------------------------------------
// Do — success on first attempt
// ---------------------------------------------------------------------------

func TestDo_SuccessFirstAttempt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	opts := Options{MaxRetries: 3, RetryDelay: fastDelay}
	resp, err := Do(context.Background(), srv.Client(), opts, getReq(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Do — retries on 5xx then succeeds
// ---------------------------------------------------------------------------

func TestDo_RetryOn5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable) // 503
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	opts := Options{MaxRetries: 3, RetryDelay: fastDelay}
	resp, err := Do(context.Background(), srv.Client(), opts, getReq(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after retries, got %d", resp.StatusCode)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 calls (2 failures + 1 success), got %d", calls.Load())
	}
}

// ---------------------------------------------------------------------------
// Do — all retries exhausted
// ---------------------------------------------------------------------------

func TestDo_AllRetriesExhausted(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	opts := Options{MaxRetries: 2, RetryDelay: fastDelay}
	_, err := Do(context.Background(), srv.Client(), opts, getReq(srv.URL))
	if err == nil {
		t.Fatal("expected error when all retries exhausted")
	}
	if calls.Load() != 3 { // 1 initial + 2 retries
		t.Errorf("expected 3 total calls, got %d", calls.Load())
	}
}

// ---------------------------------------------------------------------------
// Do — non-retryable 4xx returns immediately
// ---------------------------------------------------------------------------

func TestDo_NonRetryable4xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound) // 404 — not retryable
	}))
	defer srv.Close()

	opts := Options{MaxRetries: 3, RetryDelay: fastDelay}
	resp, err := Do(context.Background(), srv.Client(), opts, getReq(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	if calls.Load() != 1 {
		t.Errorf("expected exactly 1 call for non-retryable 4xx, got %d", calls.Load())
	}
}

// ---------------------------------------------------------------------------
// Do — 429 is retried
// ---------------------------------------------------------------------------

func TestDo_RetryOn429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	opts := Options{MaxRetries: 2, RetryDelay: fastDelay}
	resp, err := Do(context.Background(), srv.Client(), opts, getReq(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after 429 retry, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Do — context cancellation during backoff
// ---------------------------------------------------------------------------

func TestDo_ContextCancelledDuringBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after first failed attempt, before backoff sleep completes.
	opts := Options{MaxRetries: 5, RetryDelay: 200 * time.Millisecond}

	done := make(chan error, 1)
	go func() {
		_, err := Do(ctx, srv.Client(), opts, getReq(srv.URL))
		done <- err
	}()

	// Give the first attempt time to fail, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	err := <-done
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func getReq(url string) func() (*http.Request, error) {
	return func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, url, nil)
	}
}
