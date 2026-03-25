package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Scheduler     SchedulerConfig     `yaml:"scheduler"`
	Repository    RepositoryConfig    `yaml:"repository"`
	Documentation DocumentationConfig `yaml:"documentation"`
	Mapping       MappingConfig       `yaml:"mapping"`
	ACP           ACPConfig           `yaml:"acp"`
	Git           GitConfig           `yaml:"git"`
	Validation    ValidationConfig    `yaml:"validation"`
	Storage       StorageConfig       `yaml:"storage"`
	Observability ObservabilityConfig `yaml:"observability"`
}

type SchedulerConfig struct {
	Cron string `yaml:"cron"`
}

type RepositoryConfig struct {
	Provider          string        `yaml:"provider"`
	URL               string        `yaml:"url"`
	Token             string        `yaml:"token"`
	DefaultBranch     string        `yaml:"default_branch"`
	ProtectedBranches []string      `yaml:"protected_branches"`
	ProjectID         string        `yaml:"project_id"`
	MaxRetries        int           `yaml:"max_retries"`
	RetryDelay        time.Duration `yaml:"retry_delay"`
}

type DocumentationConfig struct {
	AllowedPaths      []string            `yaml:"allowed_paths"`
	PrimaryLanguage   string              `yaml:"primary_language"`
	SupportedLanguages []string           `yaml:"supported_languages"` // e.g., ["en", "ru", "de"]
	RequiredSections  map[string][]string `yaml:"required_sections"`
}

type MappingRule struct {
	Match  MappingMatch `yaml:"match"`
	Update []string     `yaml:"update"`
}

type MappingMatch struct {
	Paths []string `yaml:"paths"`
}

type MappingConfig struct {
	Rules []MappingRule `yaml:"rules"`
}

type ACPConfig struct {
	Provider               string        `yaml:"provider"`                 // "acp" (default) or "ollama"
	Model                  string        `yaml:"model"`                    // Ollama model name (e.g. "llama3.1")
	BaseURL                string        `yaml:"base_url"`
	Token                  string        `yaml:"token"`
	Timeout                time.Duration `yaml:"timeout"`
	MaxContextBytes        int           `yaml:"max_context_bytes"`
	Mode                   string        `yaml:"mode"`
	MaxRetries             int           `yaml:"max_retries"`
	RetryDelay             time.Duration `yaml:"retry_delay"`
	CircuitBreakerEnabled  bool          `yaml:"circuit_breaker_enabled"`  // default: true
	CircuitBreakerThreshold uint32       `yaml:"circuit_breaker_threshold"` // consecutive failures to open, default: 5
	CircuitBreakerTimeout  time.Duration `yaml:"circuit_breaker_timeout"`   // time before half-open, default: 30s
}

type GitConfig struct {
	BranchPrefix          string `yaml:"branch_prefix"`
	CommitMessageTemplate string `yaml:"commit_message_template"`
}

type ValidationConfig struct {
	MarkdownLint        bool    `yaml:"markdown_lint"`
	ForbidNonDocChanges bool    `yaml:"forbid_non_doc_changes"`
	MaxChangedFiles     int     `yaml:"max_changed_files"`
	// MinContentRatio is the minimum allowed ratio of updated/original content
	// length. Prevents ACP from accidentally deleting most of a document.
	// 0 disables the check. Default: 0.2 (document must keep ≥20% of original size).
	MinContentRatio     float64 `yaml:"min_content_ratio"`
}

type StorageConfig struct {
	DSN string `yaml:"dsn"`
}

type ObservabilityConfig struct {
	PprofEnabled bool   `yaml:"pprof_enabled"`
	PprofAddr    string `yaml:"pprof_addr"` // defaults to ":6060" when pprof is enabled
}

// Load reads a YAML config file at path, applies defaults and environment overrides,
// and validates the configuration. Returns an error if the config is invalid.
func Load(path string) (*Config, error) {
	cfg := &Config{}
	applyDefaults(cfg)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Tokens from environment — provider-specific vars take precedence over the file value.
	if tok := os.Getenv("AUTODOC_GITLAB_TOKEN"); tok != "" {
		cfg.Repository.Token = tok
	}
	if tok := os.Getenv("AUTODOC_GITHUB_TOKEN"); tok != "" {
		cfg.Repository.Token = tok
	}
	if tok := os.Getenv("AUTODOC_ACP_TOKEN"); tok != "" {
		cfg.ACP.Token = tok
	}

	// Validate configuration (fail fast on invalid config)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	cfg.Scheduler.Cron = "0 * * * *"
	cfg.Git.BranchPrefix = "bot/docs-update/"
	cfg.Git.CommitMessageTemplate = "docs: update documentation for recent repository changes"
	cfg.Storage.DSN = "autodoc.db"
	cfg.Validation.MinContentRatio = 0.2
	cfg.ACP.Timeout = 120 * time.Second
	cfg.ACP.MaxContextBytes = 500000
	cfg.ACP.MaxRetries = 3
	cfg.ACP.RetryDelay = time.Second
	cfg.ACP.CircuitBreakerEnabled = true
	cfg.ACP.CircuitBreakerThreshold = 5
	cfg.ACP.CircuitBreakerTimeout = 30 * time.Second
	cfg.Repository.DefaultBranch = "main"
	cfg.Repository.MaxRetries = 3
	cfg.Repository.RetryDelay = time.Second
}
