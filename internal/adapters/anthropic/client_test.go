package anthropic

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vukamecos/autodoc/internal/infrastructure/config"
	"github.com/vukamecos/autodoc/internal/domain"
)

// sdkMessagesRequest matches the JSON format the anthropic SDK sends to /v1/messages.
type sdkMessagesRequest struct {
	Model     string `json:"model"`
	MaxTokens int64  `json:"max_tokens"`
	System    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"system"`
	Messages []struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"messages"`
}

// sdkMessagesResponse matches the JSON format the anthropic SDK expects back.
type sdkMessagesResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.ACPConfig{
		BaseURL:    srv.URL,
		Model:      "claude-3-5-haiku-latest",
		Token:      "test-api-key",
		Timeout:    10 * time.Second,
		MaxRetries: 0,
		RetryDelay: time.Millisecond,
	}
	return New(cfg, slog.Default(), nil)
}

func anthropicResponse(t *testing.T, w http.ResponseWriter, acpResp domain.ACPResponse) {
	t.Helper()
	raw, _ := json.Marshal(acpResp)
	resp := sdkMessagesResponse{
		ID:         "msg_test",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-3-5-haiku-latest",
		StopReason: "end_turn",
		Content: []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{
			{Type: "text", Text: string(raw)},
		},
		Usage: struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		}{InputTokens: 10, OutputTokens: 20},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ---------------------------------------------------------------------------
// Success path
// ---------------------------------------------------------------------------

func TestGenerate_Success(t *testing.T) {
	acpResp := domain.ACPResponse{
		Summary: "Updated README",
		Files:   []domain.ACPFile{{Path: "README.md", Action: "update", Content: "# New\n"}},
		Notes:   []string{"minor wording change"},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Verify Anthropic-specific headers set by the SDK.
		if key := r.Header.Get("X-Api-Key"); key != "test-api-key" {
			t.Errorf("expected X-Api-Key 'test-api-key', got %q", key)
		}
		if ver := r.Header.Get("Anthropic-Version"); ver == "" {
			t.Error("expected non-empty anthropic-version header")
		}
		// Verify request structure.
		var req sdkMessagesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != "claude-3-5-haiku-latest" {
			t.Errorf("expected model 'claude-3-5-haiku-latest', got %q", req.Model)
		}
		if req.MaxTokens <= 0 {
			t.Errorf("expected positive max_tokens, got %d", req.MaxTokens)
		}
		if len(req.System) == 0 || req.System[0].Text == "" {
			t.Error("expected non-empty system prompt")
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
			t.Errorf("expected single user message, got %v", req.Messages)
		}
		anthropicResponse(t, w, acpResp)
	})

	client := newTestClient(t, handler)
	resp, err := client.Generate(context.Background(), domain.ACPRequest{
		CorrelationID: "test-1",
		Instructions:  "Update the README",
		ChangeSummary: "1 file changed",
		Diff:          "@@ -1 +1 @@\n-old\n+new\n",
		Documents:     []domain.Document{{Path: "README.md", Content: "# Old\n"}},
	})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if resp.Summary != "Updated README" {
		t.Errorf("unexpected summary: %q", resp.Summary)
	}
	if len(resp.Files) != 1 || resp.Files[0].Path != "README.md" {
		t.Errorf("unexpected files: %v", resp.Files)
	}
}

// ---------------------------------------------------------------------------
// Per-request model override
// ---------------------------------------------------------------------------

func TestGenerate_ModelOverride(t *testing.T) {
	var capturedModel string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req sdkMessagesRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		capturedModel = req.Model
		anthropicResponse(t, w, domain.ACPResponse{
			Summary: "ok",
			Files:   []domain.ACPFile{{Path: "README.md", Action: "update", Content: "x"}},
		})
	})

	client := newTestClient(t, handler)
	_, err := client.Generate(context.Background(), domain.ACPRequest{
		CorrelationID: "override",
		Model:         "claude-opus-4-6", // override
	})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if capturedModel != "claude-opus-4-6" {
		t.Errorf("expected model override 'claude-opus-4-6', got %q", capturedModel)
	}
}

// ---------------------------------------------------------------------------
// Error paths
// ---------------------------------------------------------------------------

func TestGenerate_ServerError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "overloaded", http.StatusServiceUnavailable)
	})
	client := newTestClient(t, handler)
	_, err := client.Generate(context.Background(), domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error for 503 response")
	}
}

func TestGenerate_InvalidJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := sdkMessagesResponse{
			ID:         "msg_test",
			Type:       "message",
			Role:       "assistant",
			Model:      "claude-3-5-haiku-latest",
			StopReason: "end_turn",
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{
				{Type: "text", Text: "not json"},
			},
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 10, OutputTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	client := newTestClient(t, handler)
	_, err := client.Generate(context.Background(), domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error for invalid JSON content")
	}
}

func TestGenerate_NoTextContent(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a response with no text blocks (empty content).
		resp := sdkMessagesResponse{
			ID:         "msg_test",
			Type:       "message",
			Role:       "assistant",
			Model:      "claude-3-5-haiku-latest",
			StopReason: "tool_use",
			Content:    nil,
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 10, OutputTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	client := newTestClient(t, handler)
	_, err := client.Generate(context.Background(), domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error for missing text content block")
	}
}

func TestGenerate_EmptyFilesAndSummary(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anthropicResponse(t, w, domain.ACPResponse{Summary: "", Files: nil})
	})
	client := newTestClient(t, handler)
	_, err := client.Generate(context.Background(), domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error for empty summary and files")
	}
}

// ---------------------------------------------------------------------------
// ResetCircuit
// ---------------------------------------------------------------------------

func TestResetCircuit_NilCircuitBreaker(t *testing.T) {
	c := &Client{}
	c.ResetCircuit() // must not panic
}
