package ollama

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
)

// ollamaBaseURL is the default Ollama endpoint for integration tests.
const ollamaBaseURL = "http://localhost:11434"

// defaultTestModel is used unless OLLAMA_TEST_MODEL is set.
const defaultTestModel = "qwen3:8b"

func skipIfOllamaUnavailable(t *testing.T) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(ollamaBaseURL + "/api/tags")
	if err != nil {
		t.Skipf("Ollama not reachable at %s: %v", ollamaBaseURL, err)
	}
	_ = resp.Body.Close()
}

func testModel() string {
	if m := os.Getenv("OLLAMA_TEST_MODEL"); m != "" {
		return m
	}
	return defaultTestModel
}

// TestIntegration_Generate_RealOllama sends a real request to a local Ollama
// instance and verifies that the response is a valid ACPResponse.
//
// Prerequisites:
//   - Ollama must be running at localhost:11434
//   - The test model must be pulled (default: qwen3:8b)
//
// Override the model: OLLAMA_TEST_MODEL=codestral:22b go test ...
func TestIntegration_Generate_RealOllama(t *testing.T) {
	skipIfOllamaUnavailable(t)

	model := testModel()
	t.Logf("using model: %s", model)

	cfg := config.ACPConfig{
		BaseURL:    ollamaBaseURL,
		Model:      model,
		Timeout:    5 * time.Minute, // local inference can be slow
		MaxRetries: 1,
		RetryDelay: 2 * time.Second,
	}
	client := New(cfg, slog.Default())

	req := domain.ACPRequest{
		CorrelationID: "integration-test-1",
		Instructions:  "Update the documentation based on the provided code diff. Preserve existing style and structure. Only output the updated document.",
		ChangeSummary: "1 file changed: added JWT_TTL environment variable to auth config",
		Diff: `diff --git a/internal/auth/config.go b/internal/auth/config.go
index abc1234..def5678 100644
--- a/internal/auth/config.go
+++ b/internal/auth/config.go
@@ -10,6 +10,8 @@ type AuthConfig struct {
     JWTSecret    string
     TokenExpiry  time.Duration
+    // JWT_TTL overrides the default token time-to-live.
+    JWTTTL       time.Duration
 }

 func LoadAuthConfig() *AuthConfig {
@@ -18,5 +20,7 @@ func LoadAuthConfig() *AuthConfig {
         JWTSecret:   os.Getenv("AUTH_JWT_SECRET"),
         TokenExpiry: 24 * time.Hour,
+        JWTTTL:      parseDuration(os.Getenv("AUTH_JWT_TTL"), 1*time.Hour),
     }
 }
`,
		Documents: []domain.Document{
			{
				Path: "docs/configuration.md",
				Content: `# Configuration

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| AUTH_JWT_SECRET | Secret key for signing JWT tokens | (required) |
| DATABASE_URL | PostgreSQL connection string | localhost:5432 |
`,
			},
		},
	}

	resp, err := client.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	// Validate response structure.
	t.Logf("summary: %s", resp.Summary)
	t.Logf("files: %d", len(resp.Files))
	t.Logf("notes: %v", resp.Notes)

	if resp.Summary == "" {
		t.Error("expected non-empty summary")
	}

	if len(resp.Files) == 0 {
		t.Fatal("expected at least one file in response")
	}

	foundTarget := false
	for _, f := range resp.Files {
		t.Logf("  file: path=%q action=%q content_len=%d", f.Path, f.Action, len(f.Content))

		if f.Path == "" {
			t.Error("file has empty path")
		}
		if f.Action != "update" && f.Action != "create" {
			t.Errorf("unexpected action %q for %s", f.Action, f.Path)
		}
		if f.Content == "" {
			t.Errorf("file %s has empty content", f.Path)
		}
		if f.Path == "docs/configuration.md" {
			foundTarget = true
			// The updated doc should mention AUTH_JWT_TTL.
			if !containsStr(f.Content, "JWT_TTL") && !containsStr(f.Content, "jwt_ttl") {
				t.Error("expected updated configuration.md to mention JWT_TTL")
			}
		}
	}

	if !foundTarget {
		t.Error("expected docs/configuration.md in response files")
	}
}

// TestIntegration_Generate_MinimalDiff tests with a trivial diff to verify
// the Ollama adapter handles simple cases without errors.
func TestIntegration_Generate_MinimalDiff(t *testing.T) {
	skipIfOllamaUnavailable(t)

	model := testModel()
	cfg := config.ACPConfig{
		BaseURL:    ollamaBaseURL,
		Model:      model,
		Timeout:    5 * time.Minute,
		MaxRetries: 1,
		RetryDelay: 2 * time.Second,
	}
	client := New(cfg, slog.Default())

	req := domain.ACPRequest{
		CorrelationID: "integration-test-2",
		Instructions:  "Update the README based on the code change. Keep it concise.",
		ChangeSummary: "1 file changed: renamed function Start to Run",
		Diff: `@@ -5,7 +5,7 @@
-func Start() error {
+func Run() error {
`,
		Documents: []domain.Document{
			{
				Path:    "README.md",
				Content: "# MyApp\n\n## Usage\n\nCall `Start()` to begin.\n",
			},
		},
	}

	resp, err := client.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	if resp.Summary == "" {
		t.Error("expected non-empty summary")
	}
	if len(resp.Files) == 0 {
		t.Error("expected at least one file")
	}

	for _, f := range resp.Files {
		t.Logf("  file: path=%q action=%q content_len=%d", f.Path, f.Action, len(f.Content))
	}
}
