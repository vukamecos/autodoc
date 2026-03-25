// Package openaicompat provides a generic OpenAI-compatible client that
// implements [domain.ACPClientPort]. It is used by the openai, mistral, groq,
// and deepseek provider adapters, which only differ by base URL, default model,
// and authentication header values.
package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"

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

type retryOptions struct {
	MaxRetries int
	RetryDelay time.Duration
}

// Client is a generic OpenAI-compatible chat completions client.
type Client struct {
	oai            *openai.Client
	model          string // default; overridden per-request by ACPRequest.Model
	component      string // e.g. "openai", "mistral" — used in log/metric labels
	retryOpts      retryOptions
	log            *slog.Logger
	metrics        *observability.Metrics
	circuitBreaker *circuitbreaker.CircuitBreaker
}

// New constructs a Client.
//
//   - component    — label used in log lines and circuit-breaker metrics (e.g. "openai").
//   - defaultBaseURL — API base URL used when cfg.BaseURL is empty.
//   - defaultModel   — model used when neither cfg.Model nor req.Model is set.
func New(
	cfg config.ACPConfig,
	component, defaultBaseURL, defaultModel string,
	log *slog.Logger,
	metrics *observability.Metrics,
) *Client {
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	model := cfg.Model
	if model == "" {
		model = defaultModel
	}

	oaiCfg := openai.DefaultConfig(cfg.Token)
	oaiCfg.BaseURL = strings.TrimRight(base, "/")
	if cfg.Timeout > 0 {
		oaiCfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	}
	oaiClient := openai.NewClientWithConfig(oaiCfg)

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
				slog.String("component", component+"_client"),
			)
			if metrics != nil {
				metrics.CircuitBreakerState.WithLabelValues(component).Set(stateToFloat(to))
			}
		})
		if metrics != nil {
			metrics.CircuitBreakerState.WithLabelValues(component).Set(0)
		}
	}

	return &Client{
		oai:            oaiClient,
		model:          model,
		component:      component,
		retryOpts:      retryOptions{MaxRetries: cfg.MaxRetries, RetryDelay: cfg.RetryDelay},
		log:            log,
		metrics:        metrics,
		circuitBreaker: cb,
	}
}

// Generate sends the ACPRequest to the provider and returns the parsed ACPResponse.
func (c *Client) Generate(ctx context.Context, req domain.ACPRequest) (*domain.ACPResponse, error) {
	prefix := c.component + ":"

	// Circuit breaker check.
	if c.circuitBreaker != nil {
		state, failures, _, _ := c.circuitBreaker.Stats()
		if state == circuitbreaker.StateOpen {
			c.log.WarnContext(ctx, prefix+" circuit breaker is open, rejecting request",
				slog.String("correlation_id", req.CorrelationID),
				slog.Uint64("consecutive_failures", uint64(failures)),
			)
			if c.metrics != nil {
				c.metrics.ACPRequestsTotal.WithLabelValues("circuit_open").Inc()
			}
			return nil, fmt.Errorf("%s %w", prefix, circuitbreaker.ErrOpenCircuit)
		}
	}

	start := time.Now()

	model := c.model
	if req.Model != "" {
		model = req.Model
	}

	userMsg := BuildUserMessage(req)

	c.log.InfoContext(ctx, prefix+" sending request",
		slog.String("model", model),
		slog.String("correlation_id", req.CorrelationID),
		slog.Int("prompt_bytes", len(userMsg)),
	)

	var result *domain.ACPResponse
	var execErr error

	executeRequest := func() error {
		return retryCall(ctx, c.retryOpts.MaxRetries, c.retryOpts.RetryDelay, func() error {
			resp, err := c.oai.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
				Model: model,
				Messages: []openai.ChatCompletionMessage{
					{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
					{Role: openai.ChatMessageRoleUser, Content: userMsg},
				},
				ResponseFormat: &openai.ChatCompletionResponseFormat{
					Type: openai.ChatCompletionResponseFormatTypeJSONObject,
				},
			})
			if err != nil {
				return fmt.Errorf("%s request failed: %w", prefix, err)
			}

			if len(resp.Choices) == 0 {
				return fmt.Errorf("%s empty choices in response: %w", prefix, domain.ErrInvalidACPResponse)
			}

			content := strings.TrimSpace(resp.Choices[0].Message.Content)
			var acpResp domain.ACPResponse
			if err := json.Unmarshal([]byte(content), &acpResp); err != nil {
				c.log.WarnContext(ctx, prefix+" model output is not valid ACPResponse JSON",
					slog.String("raw_content", truncate(content, 500)),
					slog.String("error", err.Error()),
				)
				return fmt.Errorf("%s parse model output as ACPResponse: %w", prefix, domain.ErrInvalidACPResponse)
			}

			if acpResp.Summary == "" && len(acpResp.Files) == 0 {
				return fmt.Errorf("%s response missing required fields: %w", prefix, domain.ErrInvalidACPResponse)
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

	c.log.InfoContext(ctx, prefix+" received response",
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

// BuildUserMessage assembles the user prompt from ACPRequest fields.
// Exported so that provider-specific wrappers and tests can use it directly.
func BuildUserMessage(req domain.ACPRequest) string {
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
