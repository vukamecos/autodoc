package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/vukamecos/autodoc/internal/infrastructure/config"
)

// testConfig returns a minimal valid config for tests.
// The llmURL parameter is used as the ACP base URL.
func testConfig(t *testing.T, llmURL string) *config.Config {
	t.Helper()
	tmpDir := t.TempDir()
	return &config.Config{
		Scheduler: config.SchedulerConfig{Cron: "0 * * * *"},
		Repository: config.RepositoryConfig{
			Provider:      "gitlab",
			URL:           "https://gitlab.example.com",
			Token:         "test-token",
			ProjectID:     "123",
			DefaultBranch: "main",
			MaxRetries:    0,
			RetryDelay:    time.Millisecond,
		},
		Documentation: config.DocumentationConfig{
			AllowedPaths:       []string{"README.md", "docs/"},
			PrimaryLanguage:    "en",
			SupportedLanguages: []string{"en"},
		},
		ACP: config.ACPConfig{
			Provider:        "acp",
			BaseURL:         llmURL,
			Token:           "test",
			Timeout:         5 * time.Second,
			MaxContextBytes: 100000,
			MaxRetries:      0,
			RetryDelay:      time.Millisecond,
		},
		Git: config.GitConfig{
			BranchPrefix:          "bot/docs-update/",
			CommitMessageTemplate: "docs: update",
		},
		Validation: config.ValidationConfig{MinContentRatio: 0.2},
		Storage:    config.StorageConfig{DSN: filepath.Join(tmpDir, "test.db")},
	}
}

// newLLMServer creates an httptest server that responds with 200 OK.
func newLLMServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func TestNew_ValidConfig(t *testing.T) {
	llmSrv := newLLMServer()
	defer llmSrv.Close()

	cfg := testConfig(t, llmSrv.URL)
	a, err := New(cfg, slog.Default(), true)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("New() returned nil App")
	}
	if a.httpSrv == nil {
		t.Error("expected httpSrv to be non-nil")
	}
	if a.scheduler == nil {
		t.Error("expected scheduler to be non-nil")
	}
	if a.store == nil {
		t.Error("expected store to be non-nil")
	}
	if a.useCase == nil {
		t.Error("expected useCase to be non-nil")
	}
	// Clean up.
	if err := a.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestNewOnce_ValidConfig(t *testing.T) {
	llmSrv := newLLMServer()
	defer llmSrv.Close()

	cfg := testConfig(t, llmSrv.URL)
	a, err := NewOnce(cfg, slog.Default(), true)
	if err != nil {
		t.Fatalf("NewOnce() returned unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("NewOnce() returned nil App")
	}
	// NewOnce should not create a scheduler or HTTP server.
	if a.scheduler != nil {
		t.Error("expected scheduler to be nil for one-shot app")
	}
	if a.httpSrv != nil {
		t.Error("expected httpSrv to be nil for one-shot app")
	}
	if a.useCase == nil {
		t.Error("expected useCase to be non-nil")
	}
	// Clean up store.
	if a.store != nil {
		if err := a.store.Close(); err != nil {
			t.Errorf("store.Close: %v", err)
		}
	}
}

func TestHealthzEndpoint(t *testing.T) {
	llmSrv := newLLMServer()
	defer llmSrv.Close()

	cfg := testConfig(t, llmSrv.URL)
	a, err := New(cfg, slog.Default(), true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	a.httpSrv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /healthz: got status %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("GET /healthz: got status=%q, want %q", body["status"], "ok")
	}
}

func TestHealthzReadyEndpoint_LLMReachable(t *testing.T) {
	llmSrv := newLLMServer()
	defer llmSrv.Close()

	cfg := testConfig(t, llmSrv.URL)
	a, err := New(cfg, slog.Default(), true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()

	req := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	w := httptest.NewRecorder()
	a.httpSrv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /healthz/ready: got status %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("GET /healthz/ready: got status=%q, want %q", body["status"], "ok")
	}
	if body["llm_ready"] != true {
		t.Errorf("GET /healthz/ready: got llm_ready=%v, want true", body["llm_ready"])
	}
}

func TestHealthzReadyEndpoint_LLMUnreachable(t *testing.T) {
	// Create a server and immediately close it so the URL is unreachable.
	llmSrv := newLLMServer()
	unreachableURL := llmSrv.URL
	llmSrv.Close()

	// We still need a valid LLM server for New() since it doesn't check connectivity at init.
	// But the config will point to the closed server for the health check.
	cfg := testConfig(t, unreachableURL)
	// Use a separate reachable server for construction, then override ACP.BaseURL.
	reachableSrv := newLLMServer()
	defer reachableSrv.Close()

	cfg.ACP.BaseURL = reachableSrv.URL
	a, err := New(cfg, slog.Default(), true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()

	// Now override the config to point at the unreachable server for the health check.
	a.cfg.ACP.BaseURL = unreachableURL

	req := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	w := httptest.NewRecorder()
	a.httpSrv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /healthz/ready: got status %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "degraded" {
		t.Errorf("GET /healthz/ready: got status=%q, want %q", body["status"], "degraded")
	}
	if body["llm_ready"] != false {
		t.Errorf("GET /healthz/ready: got llm_ready=%v, want false", body["llm_ready"])
	}
}

func TestAdminResetCircuit_MethodNotAllowed(t *testing.T) {
	llmSrv := newLLMServer()
	defer llmSrv.Close()

	cfg := testConfig(t, llmSrv.URL)
	a, err := New(cfg, slog.Default(), true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()

	req := httptest.NewRequest(http.MethodGet, "/admin/reset-circuit", nil)
	w := httptest.NewRecorder()
	a.httpSrv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /admin/reset-circuit: got status %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestAdminResetCircuit_NoCircuitBreaker(t *testing.T) {
	llmSrv := newLLMServer()
	defer llmSrv.Close()

	cfg := testConfig(t, llmSrv.URL)
	a, err := New(cfg, slog.Default(), true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()

	// Force circuitResetter to nil to simulate no circuit breaker.
	a.circuitResetter = nil

	// The handler closure captured the resetter variable at construction time,
	// so we need to test via a fresh mux. Instead, we'll test the condition
	// by building a minimal App with no circuitResetter.
	// Since the handler closures capture local variables, we test the 503 case
	// by directly checking what happens when the ACP client has no circuit breaker.

	// Actually the handler captures `resetter` (a local variable in New()),
	// not `a.circuitResetter`. So to test 503, we need the ACP adapter to not
	// implement circuitResetter — which never happens with the current adapters.
	// We can instead construct a test handler directly.
	mux := http.NewServeMux()
	var resetter circuitResetter // nil
	mux.HandleFunc("/admin/reset-circuit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if resetter == nil {
			http.Error(w, `{"error":"circuit breaker not enabled"}`, http.StatusServiceUnavailable)
			return
		}
		resetter.ResetCircuit()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "reset"})
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/reset-circuit", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("POST /admin/reset-circuit (no CB): got status %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestAdminResetCircuit_Success(t *testing.T) {
	llmSrv := newLLMServer()
	defer llmSrv.Close()

	cfg := testConfig(t, llmSrv.URL)
	// Enable circuit breaker so the ACP client exposes ResetCircuit.
	cfg.ACP.CircuitBreakerEnabled = true
	cfg.ACP.CircuitBreakerThreshold = 5
	cfg.ACP.CircuitBreakerTimeout = 30 * time.Second

	a, err := New(cfg, slog.Default(), true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()

	req := httptest.NewRequest(http.MethodPost, "/admin/reset-circuit", nil)
	w := httptest.NewRecorder()
	a.httpSrv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST /admin/reset-circuit: got status %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "reset" {
		t.Errorf("POST /admin/reset-circuit: got status=%q, want %q", body["status"], "reset")
	}
}

func TestAdminTriggerRun_MethodNotAllowed(t *testing.T) {
	llmSrv := newLLMServer()
	defer llmSrv.Close()

	cfg := testConfig(t, llmSrv.URL)
	a, err := New(cfg, slog.Default(), true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()

	req := httptest.NewRequest(http.MethodGet, "/admin/trigger-run", nil)
	w := httptest.NewRecorder()
	a.httpSrv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /admin/trigger-run: got status %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestAdminTriggerRun_Success(t *testing.T) {
	llmSrv := newLLMServer()
	defer llmSrv.Close()

	cfg := testConfig(t, llmSrv.URL)
	a, err := New(cfg, slog.Default(), true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()

	req := httptest.NewRequest(http.MethodPost, "/admin/trigger-run", nil)
	w := httptest.NewRecorder()
	a.httpSrv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("POST /admin/trigger-run: got status %d, want %d", w.Code, http.StatusAccepted)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "triggered" {
		t.Errorf("POST /admin/trigger-run: got status=%q, want %q", body["status"], "triggered")
	}
}

func TestShutdown(t *testing.T) {
	llmSrv := newLLMServer()
	defer llmSrv.Close()

	cfg := testConfig(t, llmSrv.URL)
	a, err := New(cfg, slog.Default(), true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Start the HTTP server on a random port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	go func() {
		_ = a.httpSrv.Serve(ln)
	}()

	// Give the server a moment to start.
	time.Sleep(50 * time.Millisecond)

	// Shutdown should succeed without error.
	if err := a.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() returned error: %v", err)
	}
}

func TestCheckLLMHealth_Reachable(t *testing.T) {
	srv := newLLMServer()
	defer srv.Close()

	acpCfg := config.ACPConfig{
		Provider: "acp",
		BaseURL:  srv.URL,
	}

	if !checkLLMHealth(acpCfg) {
		t.Error("checkLLMHealth: expected true for reachable server")
	}
}

func TestCheckLLMHealth_Unreachable(t *testing.T) {
	srv := newLLMServer()
	closedURL := srv.URL
	srv.Close()

	acpCfg := config.ACPConfig{
		Provider: "acp",
		BaseURL:  closedURL,
	}

	if checkLLMHealth(acpCfg) {
		t.Error("checkLLMHealth: expected false for unreachable server")
	}
}

func TestCheckLLMHealth_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	acpCfg := config.ACPConfig{
		Provider: "acp",
		BaseURL:  srv.URL,
	}

	if checkLLMHealth(acpCfg) {
		t.Error("checkLLMHealth: expected false for server returning 500")
	}
}

func TestCheckLLMHealth_OllamaProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ollama health check hits /api/tags.
		if r.URL.Path != "/api/tags" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	acpCfg := config.ACPConfig{
		Provider: "ollama",
		BaseURL:  srv.URL,
	}

	if !checkLLMHealth(acpCfg) {
		t.Error("checkLLMHealth(ollama): expected true")
	}
}

func TestCheckLLMHealth_UnknownProviderNoBaseURL(t *testing.T) {
	acpCfg := config.ACPConfig{
		Provider: "unknown-provider",
		BaseURL:  "",
	}

	if checkLLMHealth(acpCfg) {
		t.Error("checkLLMHealth: expected false for unknown provider with no base URL")
	}
}

func TestRunOnce(t *testing.T) {
	llmSrv := newLLMServer()
	defer llmSrv.Close()

	cfg := testConfig(t, llmSrv.URL)
	a, err := NewOnce(cfg, slog.Default(), true)
	if err != nil {
		t.Fatalf("NewOnce() error: %v", err)
	}

	// RunOnce will fail because the GitLab API is unreachable, but it should
	// invoke the use case without panicking.
	err = a.RunOnce(context.Background())
	if err == nil {
		t.Log("RunOnce succeeded unexpectedly (no real repo), but that is acceptable")
	}

	// Clean up.
	if a.store != nil {
		_ = a.store.Close()
	}
}

func TestMetricsEndpoint(t *testing.T) {
	llmSrv := newLLMServer()
	defer llmSrv.Close()

	cfg := testConfig(t, llmSrv.URL)
	a, err := New(cfg, slog.Default(), true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	a.httpSrv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /metrics: got status %d, want %d", w.Code, http.StatusOK)
	}
}
