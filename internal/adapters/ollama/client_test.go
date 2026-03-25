package ollama

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ollamaapi "github.com/ollama/ollama/api"

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
		Model:      "test-model",
		Timeout:    10 * time.Second,
		MaxRetries: 0,
		RetryDelay: time.Millisecond,
	}
	return New(cfg, slog.Default(), nil)
}

func TestGenerate_Success(t *testing.T) {
	acpResp := domain.ACPResponse{
		Summary: "Updated auth docs",
		Files: []domain.ACPFile{
			{Path: "README.md", Action: "update", Content: "# Updated\n"},
		},
		Notes: []string{"Added JWT section"},
	}
	rawJSON, _ := json.Marshal(acpResp)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Verify request body structure using SDK types.
		var req ollamaapi.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if req.Model != "test-model" {
			t.Errorf("expected model 'test-model', got %q", req.Model)
		}
		if req.Stream == nil || *req.Stream != false {
			t.Error("expected stream=false")
		}
		// Format should be "json"
		wantFormat := json.RawMessage(`"json"`)
		if string(req.Format) != string(wantFormat) {
			t.Errorf("expected format=%s, got %s", wantFormat, req.Format)
		}
		if len(req.Messages) < 2 {
			t.Errorf("expected at least 2 messages (system+user), got %d", len(req.Messages))
		}
		if req.Messages[0].Role != "system" {
			t.Errorf("expected first message role='system', got %q", req.Messages[0].Role)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ollamaapi.ChatResponse{
			Message: ollamaapi.Message{Role: "assistant", Content: string(rawJSON)},
			Done:    true,
		})
	})

	client := newTestClient(t, handler)
	resp, err := client.Generate(ctx, domain.ACPRequest{
		CorrelationID: "test-123",
		Instructions:  "Update the docs",
		ChangeSummary: "1 file changed",
		Diff:          "@@ -1 +1 @@\n-old\n+new\n",
		Documents:     []domain.Document{{Path: "README.md", Content: "# Old\n"}},
	})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if resp.Summary != "Updated auth docs" {
		t.Errorf("unexpected summary: %q", resp.Summary)
	}
	if len(resp.Files) != 1 || resp.Files[0].Path != "README.md" {
		t.Errorf("unexpected files: %v", resp.Files)
	}
	if len(resp.Notes) != 1 || resp.Notes[0] != "Added JWT section" {
		t.Errorf("unexpected notes: %v", resp.Notes)
	}
}

func TestGenerate_InvalidJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ollamaapi.ChatResponse{
			Message: ollamaapi.Message{Role: "assistant", Content: "this is not json at all"},
			Done:    true,
		})
	})

	client := newTestClient(t, handler)
	_, err := client.Generate(ctx, domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error for invalid JSON output")
	}
}

func TestGenerate_EmptyResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ollamaapi.ChatResponse{
			Message: ollamaapi.Message{Role: "assistant", Content: `{"summary":"","files":[],"notes":[]}`},
			Done:    true,
		})
	})

	client := newTestClient(t, handler)
	_, err := client.Generate(ctx, domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error for empty summary and files")
	}
}

func TestGenerate_ServerError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	})

	client := newTestClient(t, handler)
	_, err := client.Generate(ctx, domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestBuildUserMessage(t *testing.T) {
	req := domain.ACPRequest{
		Instructions:  "Update the auth docs",
		ChangeSummary: "2 files changed",
		Diff:          "@@ diff @@",
		Documents:     []domain.Document{{Path: "README.md", Content: "# Title\n"}},
	}
	msg := buildUserMessage(req)

	for _, want := range []string{
		"## Instructions",
		"Update the auth docs",
		"## Change Summary",
		"2 files changed",
		"## Diff",
		"@@ diff @@",
		"## Current Documents",
		"### README.md",
		"# Title",
	} {
		if !containsStr(msg, want) {
			t.Errorf("expected user message to contain %q", want)
		}
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
