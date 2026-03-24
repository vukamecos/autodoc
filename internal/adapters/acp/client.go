package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"time"

	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
)

// retryableStatuses are HTTP status codes that indicate a transient server-side
// error worth retrying. 4xx codes are never retried — they are caller errors.
var retryableStatuses = map[int]bool{
	http.StatusTooManyRequests:     true, // 429
	http.StatusInternalServerError: true, // 500
	http.StatusBadGateway:         true, // 502
	http.StatusServiceUnavailable: true, // 503
	http.StatusGatewayTimeout:     true, // 504
}

// Client implements domain.ACPClientPort via HTTP.
type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
	maxRetries int
	retryDelay time.Duration
	log        *slog.Logger
}

// New constructs an ACP Client from config.
func New(cfg config.ACPConfig, log *slog.Logger) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: cfg.Timeout},
		baseURL:    cfg.BaseURL,
		token:      cfg.Token,
		maxRetries: cfg.MaxRetries,
		retryDelay: cfg.RetryDelay,
		log:        log,
	}
}

// Generate sends an ACPRequest to the ACP service and returns an ACPResponse.
// Transport-level errors and 5xx/429 responses are retried with exponential
// backoff and jitter up to MaxRetries times.
func (c *Client) Generate(ctx context.Context, req domain.ACPRequest) (*domain.ACPResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("acp: marshal request: %w", err)
	}

	c.log.InfoContext(ctx, "acp: sending request",
		slog.String("correlation_id", req.CorrelationID),
	)

	var lastErr error
	for attempt := range c.maxRetries + 1 {
		if attempt > 0 {
			if err := c.sleep(ctx, attempt); err != nil {
				return nil, err // context cancelled during backoff
			}
			c.log.WarnContext(ctx, "acp: retrying",
				slog.Int("attempt", attempt),
				slog.String("correlation_id", req.CorrelationID),
				slog.String("error", lastErr.Error()),
			)
		}

		resp, err := c.do(ctx, body)
		if err != nil {
			if isTransportError(err) {
				lastErr = err
				continue
			}
			return nil, err // non-retryable error (e.g. context cancelled)
		}

		if retryableStatuses[resp.StatusCode] {
			lastErr = fmt.Errorf("acp: status %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("acp: status %d: %w", resp.StatusCode, domain.ErrInvalidACPResponse)
		}

		var acpResp domain.ACPResponse
		if err := json.NewDecoder(resp.Body).Decode(&acpResp); err != nil {
			return nil, fmt.Errorf("acp: decode response: %w", domain.ErrInvalidACPResponse)
		}
		if acpResp.Summary == "" && len(acpResp.Files) == 0 {
			return nil, fmt.Errorf("acp: response missing required fields: %w", domain.ErrInvalidACPResponse)
		}

		c.log.InfoContext(ctx, "acp: received response",
			slog.String("correlation_id", req.CorrelationID),
			slog.Int("files", len(acpResp.Files)),
		)
		return &acpResp, nil
	}

	return nil, fmt.Errorf("acp: all %d attempts failed, last error: %w", c.maxRetries+1, lastErr)
}

// do executes a single POST /generate request and returns the raw response.
// The caller is responsible for closing resp.Body.
func (c *Client) do(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/generate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	// Drain and discard body for retryable status codes so the connection
	// can be reused, then close it before the caller sees the response.
	if retryableStatuses[resp.StatusCode] {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}

	return resp, nil
}

// sleep waits for the backoff duration for the given attempt number.
// Returns ctx.Err() if the context is cancelled while waiting.
func (c *Client) sleep(ctx context.Context, attempt int) error {
	// Exponential backoff: base * 2^(attempt-1), capped at 30 s.
	exp := time.Duration(1<<(attempt-1)) * c.retryDelay
	if exp > 30*time.Second {
		exp = 30 * time.Second
	}
	// Add uniform jitter in [0, retryDelay) to spread retries.
	jitter := time.Duration(rand.Int64N(int64(c.retryDelay)))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(exp + jitter):
		return nil
	}
}

// isTransportError reports whether err is a network/transport-level error
// that is safe to retry (connection refused, timeout, DNS failure, etc.).
// Application-level errors (4xx, invalid JSON) are not transport errors.
func isTransportError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// *url.Error wraps net.Error for most http.Client failures, but check
	// the underlying cause as well in case wrapping chains vary.
	var opErr *net.OpError
	return errors.As(err, &opErr)
}
