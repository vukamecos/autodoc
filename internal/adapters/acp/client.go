package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
	"github.com/vukamecos/autodoc/internal/retry"
)

// Client implements domain.ACPClientPort via HTTP.
type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
	retryOpts  retry.Options
	log        *slog.Logger
}

// New constructs an ACP Client from config.
func New(cfg config.ACPConfig, log *slog.Logger) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: cfg.Timeout},
		baseURL:    cfg.BaseURL,
		token:      cfg.Token,
		retryOpts:  retry.Options{MaxRetries: cfg.MaxRetries, RetryDelay: cfg.RetryDelay},
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
		return nil, fmt.Errorf("acp: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
