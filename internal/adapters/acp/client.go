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
)

// Client implements domain.ACPClientPort via HTTP.
type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
	log        *slog.Logger
}

// New constructs an ACP Client from config.
func New(cfg config.ACPConfig, log *slog.Logger) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: cfg.Timeout},
		baseURL:    cfg.BaseURL,
		token:      cfg.Token,
		log:        log,
	}
}

// Generate sends an ACPRequest to the ACP service and returns an ACPResponse.
func (c *Client) Generate(ctx context.Context, req domain.ACPRequest) (*domain.ACPResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("acp: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/generate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("acp: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	c.log.InfoContext(ctx, "acp: sending request", "correlation_id", req.CorrelationID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("acp: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("acp: unexpected status %d: %w", resp.StatusCode, domain.ErrInvalidACPResponse)
	}

	var acpResp domain.ACPResponse
	if err := json.NewDecoder(resp.Body).Decode(&acpResp); err != nil {
		return nil, fmt.Errorf("acp: decode response: %w", domain.ErrInvalidACPResponse)
	}

	if acpResp.Summary == "" && len(acpResp.Files) == 0 {
		return nil, fmt.Errorf("acp: response missing required fields: %w", domain.ErrInvalidACPResponse)
	}

	c.log.InfoContext(ctx, "acp: received response",
		"correlation_id", req.CorrelationID,
		"files_count", len(acpResp.Files),
	)

	return &acpResp, nil
}
