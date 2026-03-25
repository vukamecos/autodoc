package kimi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/vukamecos/autodoc/internal/infrastructure/config"
	"github.com/vukamecos/autodoc/internal/domain"
)

var ctx = context.Background()

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.ACPConfig{
		BaseURL:    srv.URL,
		Model:      "moonshot-v1-8k",
		Token:      "test-token",
		Timeout:    10 * time.Second,
		MaxRetries: 0,
		RetryDelay: time.Millisecond,
	}
	return New(cfg, slog.Default(), nil)
}

func kimiResponse(t *testing.T, w http.ResponseWriter, acpResp domain.ACPResponse) {
	t.Helper()
	raw, _ := json.Marshal(acpResp)
	resp := openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{
			{
				Message:      openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: string(raw)},
				FinishReason: openai.FinishReasonStop,
			},
		},
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
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Verify auth header.
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %q", auth)
		}

		// Verify request structure.
		var req openai.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != "moonshot-v1-8k" {
			t.Errorf("expected model 'moonshot-v1-8k', got %q", req.Model)
		}
		if req.ResponseFormat == nil || req.ResponseFormat.Type != openai.ChatCompletionResponseFormatTypeJSONObject {
			t.Error("expected response_format.type='json_object'")
		}
		if len(req.Messages) < 2 {
			t.Fatalf("expected at least 2 messages, got %d", len(req.Messages))
		}
		if req.Messages[0].Role != openai.ChatMessageRoleSystem {
			t.Errorf("expected first message role='system', got %q", req.Messages[0].Role)
		}
		if req.Messages[1].Role != openai.ChatMessageRoleUser {
			t.Errorf("expected second message role='user', got %q", req.Messages[1].Role)
		}

		kimiResponse(t, w, acpResp)
	})

	client := newTestClient(t, handler)
	resp, err := client.Generate(ctx, domain.ACPRequest{
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
		var req openai.ChatCompletionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		capturedModel = req.Model
		kimiResponse(t, w, domain.ACPResponse{
			Summary: "ok",
			Files:   []domain.ACPFile{{Path: "README.md", Action: "update", Content: "x"}},
		})
	})

	client := newTestClient(t, handler)
	_, err := client.Generate(ctx, domain.ACPRequest{
		CorrelationID: "test-override",
		Model:         "moonshot-v1-128k", // override
	})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if capturedModel != "moonshot-v1-128k" {
		t.Errorf("expected model override 'moonshot-v1-128k', got %q", capturedModel)
	}
}

// ---------------------------------------------------------------------------
// Error paths
// ---------------------------------------------------------------------------

func TestGenerate_ServerError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not loaded", http.StatusServiceUnavailable)
	})

	client := newTestClient(t, handler)
	_, err := client.Generate(ctx, domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error for 503 response")
	}
}

func TestGenerate_InvalidJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{
					Message:      openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "not json"},
					FinishReason: openai.FinishReasonStop,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := newTestClient(t, handler)
	_, err := client.Generate(ctx, domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error for invalid JSON content")
	}
}

func TestGenerate_EmptyChoices(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openai.ChatCompletionResponse{Choices: nil})
	})

	client := newTestClient(t, handler)
	_, err := client.Generate(ctx, domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestGenerate_EmptyFilesAndSummary(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kimiResponse(t, w, domain.ACPResponse{Summary: "", Files: nil})
	})

	client := newTestClient(t, handler)
	_, err := client.Generate(ctx, domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error for empty summary and files")
	}
}

// ---------------------------------------------------------------------------
// ResetCircuit
// ---------------------------------------------------------------------------

func TestResetCircuit_NilCircuitBreaker(t *testing.T) {
	c := &Client{} // no circuit breaker
	c.ResetCircuit() // must not panic
}

// ---------------------------------------------------------------------------
// buildUserMessage
// ---------------------------------------------------------------------------

func TestBuildUserMessage_ContainsAllSections(t *testing.T) {
	req := domain.ACPRequest{
		Instructions:  "Update auth docs",
		ChangeSummary: "2 files",
		Diff:          "@@ diff @@",
		Documents:     []domain.Document{{Path: "README.md", Content: "# Title\n"}},
	}
	msg := buildUserMessage(req)
	for _, want := range []string{
		"## Instructions", "Update auth docs",
		"## Change Summary", "2 files",
		"## Diff", "@@ diff @@",
		"## Current Documents", "### README.md",
	} {
		if !containsStr(msg, want) {
			t.Errorf("expected message to contain %q", want)
		}
	}
}

func containsStr(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) &&
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}()
}
