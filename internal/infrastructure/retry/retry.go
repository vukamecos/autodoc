// Package retry provides shared HTTP retry logic with exponential backoff and jitter.
package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"time"
)

// retryableStatuses are HTTP status codes that indicate a transient server-side
// condition worth retrying. 4xx codes (except 429) are not retried — they are
// caller errors.
var retryableStatuses = map[int]bool{
	http.StatusTooManyRequests:     true, // 429
	http.StatusInternalServerError: true, // 500
	http.StatusBadGateway:         true, // 502
	http.StatusServiceUnavailable: true, // 503
	http.StatusGatewayTimeout:     true, // 504
}

// Options controls retry behaviour.
type Options struct {
	MaxRetries int
	RetryDelay time.Duration // base delay; exponential backoff is applied per attempt
}

// Do executes makeReq then the HTTP call with retry on transport errors and
// retryable HTTP status codes. makeReq is invoked on every attempt so that
// the request body reader is always fresh. The returned response has an open
// body that the caller must close; on retryable responses the body is drained
// and closed internally before the next attempt.
func Do(ctx context.Context, client *http.Client, opts Options, makeReq func() (*http.Request, error)) (*http.Response, error) {
	var lastErr error
	for attempt := range opts.MaxRetries + 1 {
		if attempt > 0 {
			if err := sleep(ctx, attempt, opts.RetryDelay); err != nil {
				return nil, err // context cancelled during backoff
			}
		}

		req, err := makeReq()
		if err != nil {
			return nil, err // request construction failure is not retryable
		}

		resp, err := client.Do(req)
		if err != nil {
			if IsTransportError(err) {
				lastErr = err
				continue
			}
			return nil, err // non-retryable (e.g. context cancelled)
		}

		if retryableStatuses[resp.StatusCode] {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("http status %d", resp.StatusCode)
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("all %d attempts failed: %w", opts.MaxRetries+1, lastErr)
}

// IsTransportError reports whether err is a network/transport-level error
// that is safe to retry (connection refused, timeout, DNS failure, etc.).
func IsTransportError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

// sleep waits for the backoff duration for the given attempt (1-based).
// Returns ctx.Err() if the context is cancelled while waiting.
func sleep(ctx context.Context, attempt int, base time.Duration) error {
	// Exponential backoff: base * 2^(attempt-1), capped at 30 s.
	exp := time.Duration(1<<(attempt-1)) * base
	if exp > 30*time.Second {
		exp = 30 * time.Second
	}
	// Uniform jitter in [0, base) to spread concurrent retries.
	jitter := time.Duration(rand.Int64N(int64(base)))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(exp + jitter):
		return nil
	}
}
