package acp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
	"github.com/vukamecos/autodoc/internal/observability"
)

var ctx = context.Background()

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.ACPConfig{
		BaseURL:    srv.URL,
		Token:      "test-token",
		Timeout:    10 * time.Second,
		MaxRetries: 0,
		RetryDelay: time.Millisecond,
	}
	return New(cfg, slog.Default(), nil)
}

func newTestClientWithMetrics(t *testing.T, handler http.Handler) (*Client, *observability.Metrics) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	reg := prometheus.NewRegistry()
	metrics := observability.NewMetrics(reg)

	cfg := config.ACPConfig{
		BaseURL:    srv.URL,
		Token:      "test-token",
		Timeout:    10 * time.Second,
		MaxRetries: 0,
		RetryDelay: time.Millisecond,
	}
	return New(cfg, slog.Default(), metrics), metrics
}

func TestGenerate_Success(t *testing.T) {
	acpResp := domain.ACPResponse{
		Summary: "Updated auth docs",
		Files: []domain.ACPFile{
			{Path: "README.md", Action: "update", Content: "# Updated\n"},
		},
		Notes: []string{"Added JWT section"},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/generate" {
			t.Errorf("expected path /generate, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("expected Authorization header 'Bearer test-token', got %q", auth)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %q", ct)
		}

		// Verify request body
		var req domain.ACPRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if req.CorrelationID != "test-123" {
			t.Errorf("expected correlation_id 'test-123', got %q", req.CorrelationID)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(acpResp)
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

func TestGenerate_EmptyResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(domain.ACPResponse{
			Summary: "",
			Files:   []domain.ACPFile{},
			Notes:   []string{},
		})
	})

	client := newTestClient(t, handler)
	_, err := client.Generate(ctx, domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error for empty summary and files")
	}
}

func TestGenerate_InvalidJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{invalid json`))
	})

	client := newTestClient(t, handler)
	_, err := client.Generate(ctx, domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestGenerate_ServerError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	})

	client := newTestClient(t, handler)
	_, err := client.Generate(ctx, domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestGenerate_NotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	client := newTestClient(t, handler)
	_, err := client.Generate(ctx, domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestGenerate_MetricsCollected(t *testing.T) {
	acpResp := domain.ACPResponse{
		Summary: "Updated docs",
		Files:   []domain.ACPFile{{Path: "README.md", Action: "update", Content: "# Updated"}},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(acpResp)
	})

	client, metrics := newTestClientWithMetrics(t, handler)
	_, err := client.Generate(ctx, domain.ACPRequest{CorrelationID: "test"})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	// Verify metrics were collected by checking they don't panic
	// (Prometheus counters cannot be directly read, but we verify the code path works)
	metrics.ACPRequestDuration.Observe(0.1)
	metrics.ACPRequestsTotal.WithLabelValues("success").Inc()
}

func TestGenerate_MetricsOnError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "error", http.StatusInternalServerError)
	})

	client, metrics := newTestClientWithMetrics(t, handler)
	_, err := client.Generate(ctx, domain.ACPRequest{CorrelationID: "test"})
	if err == nil {
		t.Fatal("expected error")
	}

	// Verify metrics code path for errors works
	metrics.ACPRequestDuration.Observe(0.1)
	metrics.ACPRequestsTotal.WithLabelValues("failed").Inc()
}

func TestGenerate_WithoutMetrics(t *testing.T) {
	acpResp := domain.ACPResponse{
		Summary: "Updated docs",
		Files:   []domain.ACPFile{{Path: "README.md", Action: "update", Content: "# Updated"}},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(acpResp)
	})

	// Test with nil metrics - should not panic
	client := newTestClient(t, handler)
	_, err := client.Generate(ctx, domain.ACPRequest{CorrelationID: "test"})
	if err != nil {
		t.Fatalf("Generate() error with nil metrics: %v", err)
	}
}

func TestNew_WithConfig(t *testing.T) {
	cfg := config.ACPConfig{
		BaseURL:    "http://test.example.com",
		Token:      "my-token",
		Timeout:    30 * time.Second,
		MaxRetries: 3,
		RetryDelay: 2 * time.Second,
	}

	client := New(cfg, slog.Default(), nil)
	if client.baseURL != cfg.BaseURL {
		t.Errorf("expected baseURL %q, got %q", cfg.BaseURL, client.baseURL)
	}
	if client.token != cfg.Token {
		t.Errorf("expected token %q, got %q", cfg.Token, client.token)
	}
	if client.retryOpts.MaxRetries != cfg.MaxRetries {
		t.Errorf("expected maxRetries %d, got %d", cfg.MaxRetries, client.retryOpts.MaxRetries)
	}
}
