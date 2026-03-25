// Package ollama implements domain.ACPClientPort using the Ollama local LLM API.
// It translates ACPRequest into an Ollama /api/chat call and parses the model's
// JSON output back into an ACPResponse.
package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	ollamaapi "github.com/ollama/ollama/api"

	"github.com/vukamecos/autodoc/internal/infrastructure/circuitbreaker"
	"github.com/vukamecos/autodoc/internal/infrastructure/config"
	"github.com/vukamecos/autodoc/internal/domain"
	"github.com/vukamecos/autodoc/internal/infrastructure/observability"
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

// Client implements domain.ACPClientPort via the Ollama /api/chat endpoint.
type Client struct {
	ollama         *ollamaapi.Client
	model          string
	maxRetries     int
	retryDelay     time.Duration
	log            *slog.Logger
	metrics        *observability.Metrics
	circuitBreaker *circuitbreaker.CircuitBreaker
}

// New constructs an Ollama Client.
func New(cfg config.ACPConfig, log *slog.Logger, metrics *observability.Metrics) *Client {
	base := cfg.BaseURL
	if base == "" {
		base = "http://localhost:11434"
	}

	baseURL, _ := url.Parse(strings.TrimRight(base, "/"))
	httpClient := &http.Client{Timeout: cfg.Timeout}
	ollamaClient := ollamaapi.NewClient(baseURL, httpClient)

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
				slog.String("component", "ollama_client"),
			)
			if metrics != nil {
				metrics.CircuitBreakerState.WithLabelValues("ollama").Set(stateToFloat(to))
			}
		})
		// Set initial state
		if metrics != nil {
			metrics.CircuitBreakerState.WithLabelValues("ollama").Set(0)
		}
	}

	return &Client{
		ollama:         ollamaClient,
		model:          cfg.Model,
		maxRetries:     cfg.MaxRetries,
		retryDelay:     cfg.RetryDelay,
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

// Generate sends the ACPRequest to Ollama and returns the parsed ACPResponse.
func (c *Client) Generate(ctx context.Context, req domain.ACPRequest) (*domain.ACPResponse, error) {
	// Circuit breaker check
	if c.circuitBreaker != nil {
		state, failures, _, _ := c.circuitBreaker.Stats()
		if state == circuitbreaker.StateOpen {
			c.log.WarnContext(ctx, "ollama: circuit breaker is open, rejecting request",
				slog.String("correlation_id", req.CorrelationID),
				slog.Uint64("consecutive_failures", uint64(failures)),
			)
			if c.metrics != nil {
				c.metrics.ACPRequestsTotal.WithLabelValues("circuit_open").Inc()
			}
			return nil, fmt.Errorf("ollama: %w", circuitbreaker.ErrOpenCircuit)
		}
	}

	start := time.Now()
	userMsg := buildUserMessage(req)

	model := c.model
	if req.Model != "" {
		model = req.Model
	}

	c.log.InfoContext(ctx, "ollama: sending request",
		slog.String("model", model),
		slog.String("correlation_id", req.CorrelationID),
		slog.Int("prompt_bytes", len(userMsg)),
	)

	var result *domain.ACPResponse
	var execErr error

	executeRequest := func() error {
		return retryCall(ctx, c.maxRetries, c.retryDelay, func() error {
			stream := false
			chatReq := &ollamaapi.ChatRequest{
				Model: model,
				Messages: []ollamaapi.Message{
					{Role: "system", Content: systemPrompt},
					{Role: "user", Content: userMsg},
				},
				Stream: &stream,
				Format: json.RawMessage(`"json"`),
			}

			var finalContent string
			err := c.ollama.Chat(ctx, chatReq, func(resp ollamaapi.ChatResponse) error {
				finalContent = resp.Message.Content
				return nil
			})
			if err != nil {
				return fmt.Errorf("ollama: request failed: %w", err)
			}

			// Parse the model's JSON content into ACPResponse.
			content := strings.TrimSpace(finalContent)
			var acpResp domain.ACPResponse
			if err := json.Unmarshal([]byte(content), &acpResp); err != nil {
				c.log.WarnContext(ctx, "ollama: model output is not valid ACPResponse JSON",
					slog.String("raw_content", truncate(content, 500)),
					slog.String("error", err.Error()),
				)
				return fmt.Errorf("ollama: parse model output as ACPResponse: %w", domain.ErrInvalidACPResponse)
			}

			if acpResp.Summary == "" && len(acpResp.Files) == 0 {
				return fmt.Errorf("ollama: response missing required fields: %w", domain.ErrInvalidACPResponse)
			}

			result = &acpResp
			return nil
		})
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

	c.log.InfoContext(ctx, "ollama: received response",
		slog.String("correlation_id", req.CorrelationID),
		slog.Int("files", len(result.Files)),
		slog.String("summary", truncate(result.Summary, 120)),
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

// truncate returns the first n bytes of s, appending "…" if truncated.
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
