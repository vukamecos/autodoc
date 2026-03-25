// Package ollama implements domain.ACPClientPort using the Ollama local LLM API.
// It translates ACPRequest into an Ollama /api/chat call and parses the model's
// JSON output back into an ACPResponse.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
	"github.com/vukamecos/autodoc/internal/retry"
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
	httpClient *http.Client
	baseURL    string // e.g. http://localhost:11434
	model      string
	retryOpts  retry.Options
	log        *slog.Logger
}

// New constructs an Ollama Client.
func New(cfg config.ACPConfig, log *slog.Logger) *Client {
	base := cfg.BaseURL
	if base == "" {
		base = "http://localhost:11434"
	}
	return &Client{
		httpClient: &http.Client{Timeout: cfg.Timeout},
		baseURL:    strings.TrimRight(base, "/"),
		model:      cfg.Model,
		retryOpts:  retry.Options{MaxRetries: cfg.MaxRetries, RetryDelay: cfg.RetryDelay},
		log:        log,
	}
}

// chatRequest is the Ollama /api/chat request body.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Format   string        `json:"format"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse is the Ollama /api/chat response body (non-streaming).
type chatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// Generate sends the ACPRequest to Ollama and returns the parsed ACPResponse.
func (c *Client) Generate(ctx context.Context, req domain.ACPRequest) (*domain.ACPResponse, error) {
	userMsg := buildUserMessage(req)

	ollamaReq := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		},
		Stream: false,
		Format: "json",
	}

	body, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: marshal request: %w", err)
	}

	c.log.InfoContext(ctx, "ollama: sending request",
		slog.String("model", c.model),
		slog.String("correlation_id", req.CorrelationID),
		slog.Int("prompt_bytes", len(userMsg)),
	)

	makeReq := func() (*http.Request, error) {
		r, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.baseURL+"/api/chat", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		r.Header.Set("Content-Type", "application/json")
		return r, nil
	}

	resp, err := retry.Do(ctx, c.httpClient, c.retryOpts, makeReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama: status %d: %w", resp.StatusCode, domain.ErrInvalidACPResponse)
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("ollama: decode chat response: %w", err)
	}

	// Parse the model's JSON content into ACPResponse.
	content := strings.TrimSpace(chatResp.Message.Content)
	var acpResp domain.ACPResponse
	if err := json.Unmarshal([]byte(content), &acpResp); err != nil {
		c.log.WarnContext(ctx, "ollama: model output is not valid ACPResponse JSON",
			slog.String("raw_content", truncate(content, 500)),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("ollama: parse model output as ACPResponse: %w", domain.ErrInvalidACPResponse)
	}

	if acpResp.Summary == "" && len(acpResp.Files) == 0 {
		return nil, fmt.Errorf("ollama: response missing required fields: %w", domain.ErrInvalidACPResponse)
	}

	c.log.InfoContext(ctx, "ollama: received response",
		slog.String("correlation_id", req.CorrelationID),
		slog.Int("files", len(acpResp.Files)),
		slog.String("summary", truncate(acpResp.Summary, 120)),
	)
	return &acpResp, nil
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
