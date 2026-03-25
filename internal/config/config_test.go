package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Load
// ---------------------------------------------------------------------------

func TestLoad_ValidFile(t *testing.T) {
	yaml := `
scheduler:
  cron: "0 * * * *"
repository:
  provider: gitlab
  url: https://gitlab.example.com
  project_id: "group/repo"
documentation:
  allowed_paths: ["README.md", "docs/**"]
  primary_language: en
  supported_languages: [en]
acp:
  provider: acp
  base_url: http://acp.example.com
  max_context_bytes: 500000
  timeout: 120s
git:
  branch_prefix: "bot/docs/"
`
	path := writeTempFile(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.Repository.Provider != "gitlab" {
		t.Errorf("expected provider 'gitlab', got %q", cfg.Repository.Provider)
	}
	if cfg.Scheduler.Cron != "0 * * * *" {
		t.Errorf("unexpected cron %q", cfg.Scheduler.Cron)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/no/such/file.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeTempFile(t, ":::invalid yaml:::")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoad_InvalidConfig_MissingProjectID(t *testing.T) {
	yaml := `
repository:
  provider: gitlab
  url: https://gitlab.example.com
documentation:
  allowed_paths: ["README.md"]
  primary_language: en
  supported_languages: [en]
acp:
  max_context_bytes: 500000
  timeout: 120s
git:
  branch_prefix: "bot/"
`
	path := writeTempFile(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for missing project_id, got nil")
	}
}

func TestLoad_EnvOverride_GitLabToken(t *testing.T) {
	t.Setenv("AUTODOC_GITLAB_TOKEN", "secret-token")

	yaml := `
repository:
  provider: gitlab
  url: https://gitlab.example.com
  project_id: "group/repo"
documentation:
  allowed_paths: ["README.md"]
  primary_language: en
  supported_languages: [en]
acp:
  max_context_bytes: 500000
  timeout: 120s
git:
  branch_prefix: "bot/"
`
	path := writeTempFile(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.Repository.Token != "secret-token" {
		t.Errorf("expected token 'secret-token', got %q", cfg.Repository.Token)
	}
}

func TestLoad_EnvOverride_ACPToken(t *testing.T) {
	t.Setenv("AUTODOC_ACP_TOKEN", "acp-tok")

	yaml := `
repository:
  provider: gitlab
  url: https://gitlab.example.com
  project_id: "group/repo"
documentation:
  allowed_paths: ["README.md"]
  primary_language: en
  supported_languages: [en]
acp:
  max_context_bytes: 500000
  timeout: 120s
git:
  branch_prefix: "bot/"
`
	path := writeTempFile(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.ACP.Token != "acp-tok" {
		t.Errorf("expected ACP token 'acp-tok', got %q", cfg.ACP.Token)
	}
}

func TestLoad_AppliesDefaults(t *testing.T) {
	yaml := `
repository:
  provider: gitlab
  url: https://gitlab.example.com
  project_id: "group/repo"
documentation:
  allowed_paths: ["README.md"]
  primary_language: en
  supported_languages: [en]
git:
  branch_prefix: "bot/"
`
	path := writeTempFile(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.Storage.DSN != "autodoc.db" {
		t.Errorf("expected default DSN 'autodoc.db', got %q", cfg.Storage.DSN)
	}
	if cfg.ACP.MaxContextBytes != 500000 {
		t.Errorf("expected default max_context_bytes 500000, got %d", cfg.ACP.MaxContextBytes)
	}
}

// ---------------------------------------------------------------------------
// Validate
// ---------------------------------------------------------------------------

func TestValidate_MissingProjectID(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Repository.ProjectID = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing project_id")
	}
}

func TestValidate_UnknownProvider(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Repository.Provider = "bitbucket"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestValidate_UnknownACPProvider(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ACP.Provider = "openai"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for unknown acp provider")
	}
}

func TestValidate_EmptyAllowedPaths(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Documentation.AllowedPaths = nil
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty allowed_paths")
	}
}

func TestValidate_MinContentRatioOutOfRange(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Validation.MinContentRatio = 1.5
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for min_content_ratio > 1")
	}
}

func TestValidate_PrimaryLanguageNotInSupported(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Documentation.PrimaryLanguage = "de"
	cfg.Documentation.SupportedLanguages = []string{"en"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error when primary_language not in supported_languages")
	}
}

func TestValidate_OllamaWithoutModel_Allowed(t *testing.T) {
	// acp.model is optional for ollama — auto-selection handles it.
	cfg := minimalValidConfig()
	cfg.ACP.Provider = "ollama"
	cfg.ACP.Model = ""
	err := cfg.Validate()
	if err != nil {
		t.Errorf("expected no error for ollama without model (auto-select), got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ValidateAndSetDefaults
// ---------------------------------------------------------------------------

func TestValidateAndSetDefaults_DefaultsProvider(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Repository.Provider = ""
	cfg.ACP.Provider = ""
	changed, _ := cfg.ValidateAndSetDefaults()
	if !changed {
		t.Error("expected corrections to be reported")
	}
	if cfg.Repository.Provider != "gitlab" {
		t.Errorf("expected provider defaulted to 'gitlab', got %q", cfg.Repository.Provider)
	}
	if cfg.ACP.Provider != "acp" {
		t.Errorf("expected acp provider defaulted to 'acp', got %q", cfg.ACP.Provider)
	}
}

func TestValidateAndSetDefaults_DefaultsLanguage(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Documentation.PrimaryLanguage = ""
	cfg.Documentation.SupportedLanguages = nil
	_, _ = cfg.ValidateAndSetDefaults()
	if cfg.Documentation.PrimaryLanguage != "en" {
		t.Errorf("expected primary_language defaulted to 'en', got %q", cfg.Documentation.PrimaryLanguage)
	}
}

func TestValidateAndSetDefaults_AddsPrimaryToSupported(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Documentation.SupportedLanguages = []string{"fr"}
	// primary is "en", not in supported → should be appended
	_, corrections := cfg.ValidateAndSetDefaults()
	found := false
	for _, c := range corrections {
		if c != "" {
			found = true
		}
	}
	_ = found
	hasPrimary := false
	for _, lang := range cfg.Documentation.SupportedLanguages {
		if lang == cfg.Documentation.PrimaryLanguage {
			hasPrimary = true
		}
	}
	if !hasPrimary {
		t.Error("primary_language should have been added to supported_languages")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func minimalValidConfig() *Config {
	cfg := &Config{}
	applyDefaults(cfg)
	cfg.Repository.Provider = "gitlab"
	cfg.Repository.URL = "https://gitlab.example.com"
	cfg.Repository.ProjectID = "group/repo"
	cfg.Documentation.AllowedPaths = []string{"README.md", "docs/**"}
	cfg.Documentation.PrimaryLanguage = "en"
	cfg.Documentation.SupportedLanguages = []string{"en"}
	cfg.Git.BranchPrefix = "bot/"
	cfg.ACP.Provider = "acp"
	return cfg
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "autodoc.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}
