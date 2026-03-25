// Package anthropic implements [domain.ACPClientPort] using the Anthropic
// Messages API (/v1/messages). Unlike OpenAI-compatible providers, Anthropic
// uses a distinct request/response format: the system prompt is a top-level
// field, responses arrive as typed content blocks, and authentication is via
// the x-api-key header with a required anthropic-version header.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/vukamecos/autodoc/internal/circuitbreaker"
	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
	"github.com/vukamecos/autodoc/internal/observability"
)

const (
	defaultBaseURL   = "https://api.anthropic.com"
	defaultModel     = "claude-3-5-haiku-latest"
	defaultMaxTokens = 8192
)

const systemPrompt = `You are a documentation maintenance assistant. You receive code diffs and existing documentation, and produce updated documentation.

You MUST respond with a JSON object in this exact format — no markdown fences, no extra text, ONLY valid JSON:

{
  "summary": "One-sentence description of what was updated and why.",
  "files": [
    {
      "path": "the/file/path.md",
      "action": "update",
      "content": "The full updated markdown content of the file."
    }
  ],
  "notes": ["Optional observations for the human reviewer."]
}

Rules:
- "action" is "update" for existing documents or "create" for new ones.
- "content" must contain the COMPLETE file content, not a partial diff.
- Preserve the existing style, structure, and language of the documentation.
- Do not invent facts not present in the code or diff.
- If information is insufficient, add a TODO comment in "notes" rather than guessing.
- Keep changes minimal and accurate.`

// Client implements domain.ACPClientPort via the Anthropic Messages API.
type Client struct {
	client         anthropic.Client
	model          string // default; overridden per-request by ACPRequest.Model
	maxTokens      int
	maxRetries     int
	retryDelay     time.Duration
	log            *slog.Logger
	metrics        *observability.Metrics
	circuitBreaker *circuitbreaker.CircuitBreaker
}

// New constructs an Anthropic Client from config.
// The API key must be set via AUTODOC_ACP_TOKEN or acp.token.
func New(cfg config.ACPConfig, log *slog.Logger, metrics *observability.Metrics) *Client {
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	model := cfg.Model
	if model == "" {
		model = defaultModel
	}

	opts := []option.RequestOption{
		option.WithAPIKey(cfg.Token),
	}
	if cfg.Timeout > 0 {
		opts = append(opts, option.WithHTTPClient(&http.Client{Timeout: cfg.Timeout}))
	}
	if strings.TrimRight(base, "/") != strings.TrimRight(defaultBaseURL, "/") {
		opts = append(opts, option.WithBaseURL(base))
	}
	anthClient := anthropic.NewClient(opts...)

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
				slog.String("component", "anthropic_client"),
			)
			if metrics != nil {
				metrics.CircuitBreakerState.WithLabelValues("anthropic").Set(stateToFloat(to))
			}
		})
		if metrics != nil {
			metrics.CircuitBreakerState.WithLabelValues("anthropic").Set(0)
		}
	}

	return &Client{
		client:         anthClient,
		model:          model,
		maxTokens:      defaultMaxTokens,
		maxRetries:     cfg.MaxRetries,
		retryDelay:     cfg.RetryDelay,
		log:            log,
		metrics:        metrics,
		circuitBreaker: cb,
	}
}

// Generate sends the ACPRequest to Anthropic and returns the parsed ACPResponse.
func (c *Client) Generate(ctx context.Context, req domain.ACPRequest) (*domain.ACPResponse, error) {
	// Circuit breaker check.
	if c.circuitBreaker != nil {
		state, failures, _, _ := c.circuitBreaker.Stats()
		if state == circuitbreaker.StateOpen {
			c.log.WarnContext(ctx, "anthropic: circuit breaker is open, rejecting request",
				slog.String("correlation_id", req.CorrelationID),
				slog.Uint64("consecutive_failures", uint64(failures)),
			)
			if c.metrics != nil {
				c.metrics.ACPRequestsTotal.WithLabelValues("circuit_open").Inc()
			}
			return nil, fmt.Errorf("anthropic: %w", circuitbreaker.ErrOpenCircuit)
		}
	}

	start := time.Now()

	model := c.model
	if req.Model != "" {
		model = req.Model
	}

	userMsg := buildUserMessage(req)

	c.log.InfoContext(ctx, "anthropic: sending request",
		slog.String("model", model),
		slog.String("correlation_id", req.CorrelationID),
		slog.Int("prompt_bytes", len(userMsg)),
	)

	var result *domain.ACPResponse
	var execErr error

	executeRequest := func() error {
		return retryCall(ctx, c.maxRetries, c.retryDelay, func() error {
			resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
				Model:     anthropic.Model(model),
				MaxTokens: int64(c.maxTokens),
				System: []anthropic.TextBlockParam{
					{Text: systemPrompt},
				},
				Messages: []anthropic.MessageParam{
					anthropic.NewUserMessage(anthropic.NewTextBlock(userMsg)),
				},
			})
			if err != nil {
				return fmt.Errorf("anthropic: request failed: %w", err)
			}

			// Find the first text content block.
			var content string
			for _, block := range resp.Content {
				if block.Type == "text" {
					content = strings.TrimSpace(block.Text)
					break
				}
			}
			if content == "" {
				return fmt.Errorf("anthropic: no text content in response: %w", domain.ErrInvalidACPResponse)
			}

			var acpResp domain.ACPResponse
			if err := json.Unmarshal([]byte(content), &acpResp); err != nil {
				c.log.WarnContext(ctx, "anthropic: model output is not valid ACPResponse JSON",
					slog.String("raw_content", truncate(content, 500)),
					slog.String("error", err.Error()),
				)
				return fmt.Errorf("anthropic: parse model output as ACPResponse: %w", domain.ErrInvalidACPResponse)
			}

			if acpResp.Summary == "" && len(acpResp.Files) == 0 {
				return fmt.Errorf("anthropic: response missing required fields: %w", domain.ErrInvalidACPResponse)
			}

			result = &acpResp
			return nil
		})
	}

	if c.circuitBreaker != nil {
		execErr = c.circuitBreaker.Execute(ctx, executeRequest)
	} else {
		execErr = executeRequest()
	}

	if execErr != nil {
		if c.metrics != nil {
			c.metrics.ACPRequestDuration.Observe(time.Since(start).Seconds())
			if !errors.Is(execErr, circuitbreaker.ErrOpenCircuit) {
				c.metrics.ACPRequestsTotal.WithLabelValues("failed").Inc()
			}
		}
		return nil, execErr
	}

	if c.metrics != nil {
		c.metrics.ACPRequestDuration.Observe(time.Since(start).Seconds())
		c.metrics.ACPRequestsTotal.WithLabelValues("success").Inc()
	}

	c.log.InfoContext(ctx, "anthropic: received response",
		slog.String("correlation_id", req.CorrelationID),
		slog.Int("files", len(result.Files)),
		slog.String("summary", truncate(result.Summary, 120)),
	)
	return result, nil
}

// ResetCircuit forces the circuit breaker back to closed state.
func (c *Client) ResetCircuit() {
	if c.circuitBreaker != nil {
		c.circuitBreaker.Reset()
	}
}

// buildUserMessage assembles the user prompt from the ACPRequest fields.
func buildUserMessage(req domain.ACPRequest) string {
	var sb strings.Builder

	if req.Instructions != "" {
		sb.WriteString("## Instructions\n\n")
		sb.WriteString(req.Instructions)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Change Summary\n\n")
	sb.WriteString(req.ChangeSummary)
	sb.WriteString("\n\n")

	sb.WriteString("## Diff\n\n```diff\n")
	sb.WriteString(req.Diff)
	sb.WriteString("```\n\n")

	if len(req.Documents) > 0 {
		sb.WriteString("## Current Documents\n\n")
		for _, doc := range req.Documents {
			fmt.Fprintf(&sb, "### %s\n\n```markdown\n%s```\n\n", doc.Path, doc.Content)
		}
	}

	return sb.String()
}

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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// retryCall retries fn up to maxRetries times with retryDelay between attempts.
func retryCall(ctx context.Context, maxRetries int, retryDelay time.Duration, fn func() error) error {
	var err error
	for i := 0; i <= maxRetries; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryDelay):
			}
		}
		err = fn()
		if err == nil {
			return nil
		}
	}
	return err
}
