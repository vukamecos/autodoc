package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/vukamecos/autodoc/internal/circuitbreaker"
	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
	"github.com/vukamecos/autodoc/internal/observability"
	"github.com/vukamecos/autodoc/internal/retry"
)

// Client implements domain.ACPClientPort via HTTP.
type Client struct {
	httpClient     *http.Client
	baseURL        string
	token          string
	retryOpts      retry.Options
	log            *slog.Logger
	metrics        *observability.Metrics
	circuitBreaker *circuitbreaker.CircuitBreaker
}

// New constructs an ACP Client from config.
func New(cfg config.ACPConfig, log *slog.Logger, metrics *observability.Metrics) *Client {
	var cb *circuitbreaker.CircuitBreaker
	if cfg.CircuitBreakerEnabled {
		cbConfig := circuitbreaker.Config{
			FailureThreshold: cfg.CircuitBreakerThreshold,
			SuccessThreshold: 2,
			Timeout:          cfg.CircuitBreakerTimeout,
		}
		cb = circuitbreaker.NewWithCallback(cbConfig, func(from, to circuitbreaker.State) {
			log.Warn("circuit breaker state changed",
				slog.String("from", from.String()),
				slog.String("to", to.String()),
				slog.String("component", "acp_client"),
			)
			if metrics != nil {
				metrics.CircuitBreakerState.WithLabelValues("acp").Set(stateToFloat(to))
			}
		})
		// Set initial state
		if metrics != nil {
			metrics.CircuitBreakerState.WithLabelValues("acp").Set(0)
		}
	}

	return &Client{
		httpClient:     &http.Client{Timeout: cfg.Timeout},
		baseURL:        cfg.BaseURL,
		token:          cfg.Token,
		retryOpts:      retry.Options{MaxRetries: cfg.MaxRetries, RetryDelay: cfg.RetryDelay},
		log:            log,
		metrics:        metrics,
		circuitBreaker: cb,
	}
}

// stateToFloat converts circuit breaker state to float for metrics (0=closed, 1=half-open, 2=open).
func stateToFloat(s circuitbreaker.State) float64 {
	switch s {
	case circuitbreaker.StateClosed:
		return 0
	case circuitbreaker.StateHalfOpen:
		return 1
	case circuitbreaker.StateOpen:
		return 2
	default:
		return 0
	}
}

// Generate sends an ACPRequest to the ACP service and returns an ACPResponse.
// Transport-level errors and 5xx/429 responses are retried with exponential
// backoff and jitter up to MaxRetries times. If circuit breaker is enabled,
// requests will fail fast when the circuit is open.
func (c *Client) Generate(ctx context.Context, req domain.ACPRequest) (*domain.ACPResponse, error) {
	// Circuit breaker check
	if c.circuitBreaker != nil {
		state, failures, _, _ := c.circuitBreaker.Stats()
		if state == circuitbreaker.StateOpen {
			c.log.WarnContext(ctx, "acp: circuit breaker is open, rejecting request",
				slog.String("correlation_id", req.CorrelationID),
				slog.Uint64("consecutive_failures", uint64(failures)),
			)
			if c.metrics != nil {
				c.metrics.ACPRequestsTotal.WithLabelValues("circuit_open").Inc()
			}
			return nil, fmt.Errorf("acp: %w", circuitbreaker.ErrOpenCircuit)
		}
	}

	start := time.Now()
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("acp: marshal request: %w", err)
	}

	c.log.InfoContext(ctx, "acp: sending request",
		slog.String("correlation_id", req.CorrelationID),
	)

	var result *domain.ACPResponse
	var execErr error

	executeRequest := func() error {
		makeReq := func() (*http.Request, error) {
			r, err := http.NewRequestWithContext(ctx, http.MethodPost,
				c.baseURL+"/generate", bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer "+c.token)
			return r, nil
		}

		resp, err := retry.Do(ctx, c.httpClient, c.retryOpts, makeReq)
		if err != nil {
			return fmt.Errorf("acp: request failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("acp: status %d: %w", resp.StatusCode, domain.ErrInvalidACPResponse)
		}

		var acpResp domain.ACPResponse
		if err := json.NewDecoder(resp.Body).Decode(&acpResp); err != nil {
			return fmt.Errorf("acp: decode response: %w", domain.ErrInvalidACPResponse)
		}
		if acpResp.Summary == "" && len(acpResp.Files) == 0 {
			return fmt.Errorf("acp: response missing required fields: %w", domain.ErrInvalidACPResponse)
		}

		result = &acpResp
		return nil
	}

	// Execute with circuit breaker if enabled
	if c.circuitBreaker != nil {
		execErr = c.circuitBreaker.Execute(ctx, executeRequest)
	} else {
		execErr = executeRequest()
	}

	// Record metrics and handle result
	if execErr != nil {
		if c.metrics != nil {
			c.metrics.ACPRequestDuration.Observe(time.Since(start).Seconds())
			if errors.Is(execErr, circuitbreaker.ErrOpenCircuit) {
				// Already counted above
			} else {
				c.metrics.ACPRequestsTotal.WithLabelValues("failed").Inc()
			}
		}
		return nil, execErr
	}

	if c.metrics != nil {
		c.metrics.ACPRequestDuration.Observe(time.Since(start).Seconds())
		c.metrics.ACPRequestsTotal.WithLabelValues("success").Inc()
	}

	c.log.InfoContext(ctx, "acp: received response",
		slog.String("correlation_id", req.CorrelationID),
		slog.Int("files", len(result.Files)),
	)
	return result, nil
}

// ResetCircuit forces the circuit breaker back to closed state.
// Used by the admin /admin/reset-circuit endpoint.
func (c *Client) ResetCircuit() {
	if c.circuitBreaker != nil {
		c.circuitBreaker.Reset()
	}
}
