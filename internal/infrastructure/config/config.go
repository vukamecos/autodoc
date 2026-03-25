package config

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Scheduler     SchedulerConfig     `mapstructure:"scheduler" yaml:"scheduler"`
	Repository    RepositoryConfig    `mapstructure:"repository" yaml:"repository"`
	Documentation DocumentationConfig `mapstructure:"documentation" yaml:"documentation"`
	Mapping       MappingConfig       `mapstructure:"mapping" yaml:"mapping"`
	ACP           ACPConfig           `mapstructure:"acp" yaml:"acp"`
	Git           GitConfig           `mapstructure:"git" yaml:"git"`
	Validation    ValidationConfig    `mapstructure:"validation" yaml:"validation"`
	Storage       StorageConfig       `mapstructure:"storage" yaml:"storage"`
	Observability ObservabilityConfig `mapstructure:"observability" yaml:"observability"`
}

type SchedulerConfig struct {
	Cron string `mapstructure:"cron" yaml:"cron"`
}

type RepositoryConfig struct {
	Provider          string        `mapstructure:"provider" yaml:"provider"`
	URL               string        `mapstructure:"url" yaml:"url"`
	Token             string        `mapstructure:"token" yaml:"token"`
	DefaultBranch     string        `mapstructure:"default_branch" yaml:"default_branch"`
	ProtectedBranches []string      `mapstructure:"protected_branches" yaml:"protected_branches"`
	ProjectID         string        `mapstructure:"project_id" yaml:"project_id"`
	MaxRetries        int           `mapstructure:"max_retries" yaml:"max_retries"`
	RetryDelay        time.Duration `mapstructure:"retry_delay" yaml:"retry_delay"`
}

type DocumentationConfig struct {
	AllowedPaths       []string            `mapstructure:"allowed_paths" yaml:"allowed_paths"`
	PrimaryLanguage    string              `mapstructure:"primary_language" yaml:"primary_language"`
	SupportedLanguages []string            `mapstructure:"supported_languages" yaml:"supported_languages"`
	RequiredSections   map[string][]string `mapstructure:"required_sections" yaml:"required_sections"`
}

type MappingRule struct {
	Match  MappingMatch `mapstructure:"match" yaml:"match"`
	Update []string     `mapstructure:"update" yaml:"update"`
}

type MappingMatch struct {
	Paths []string `mapstructure:"paths" yaml:"paths"`
}

type MappingConfig struct {
	Rules []MappingRule `mapstructure:"rules" yaml:"rules"`
}

type ACPConfig struct {
	Provider                string        `mapstructure:"provider" yaml:"provider"`
	Model                   string        `mapstructure:"model" yaml:"model"`
	BaseURL                 string        `mapstructure:"base_url" yaml:"base_url"`
	Token                   string        `mapstructure:"token" yaml:"token"`
	Timeout                 time.Duration `mapstructure:"timeout" yaml:"timeout"`
	MaxContextBytes         int           `mapstructure:"max_context_bytes" yaml:"max_context_bytes"`
	Mode                    string        `mapstructure:"mode" yaml:"mode"`
	MaxRetries              int           `mapstructure:"max_retries" yaml:"max_retries"`
	RetryDelay              time.Duration `mapstructure:"retry_delay" yaml:"retry_delay"`
	CircuitBreakerEnabled   bool          `mapstructure:"circuit_breaker_enabled" yaml:"circuit_breaker_enabled"`
	CircuitBreakerThreshold uint32        `mapstructure:"circuit_breaker_threshold" yaml:"circuit_breaker_threshold"`
	CircuitBreakerTimeout   time.Duration `mapstructure:"circuit_breaker_timeout" yaml:"circuit_breaker_timeout"`
}

type GitConfig struct {
	BranchPrefix          string `mapstructure:"branch_prefix" yaml:"branch_prefix"`
	CommitMessageTemplate string `mapstructure:"commit_message_template" yaml:"commit_message_template"`
}

type ValidationConfig struct {
	MarkdownLint        bool    `mapstructure:"markdown_lint" yaml:"markdown_lint"`
	ForbidNonDocChanges bool    `mapstructure:"forbid_non_doc_changes" yaml:"forbid_non_doc_changes"`
	MaxChangedFiles     int     `mapstructure:"max_changed_files" yaml:"max_changed_files"`
	MinContentRatio     float64 `mapstructure:"min_content_ratio" yaml:"min_content_ratio"`
}

type StorageConfig struct {
	DSN string `mapstructure:"dsn" yaml:"dsn"`
}

type ObservabilityConfig struct {
	PprofEnabled    bool   `mapstructure:"pprof_enabled" yaml:"pprof_enabled"`
	PprofAddr       string `mapstructure:"pprof_addr" yaml:"pprof_addr"`
	TracingEnabled  bool   `mapstructure:"tracing_enabled" yaml:"tracing_enabled"`
	TracingEndpoint string `mapstructure:"tracing_endpoint" yaml:"tracing_endpoint"`
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

// LoadFromViper loads configuration from viper (which has already been initialized).
// This is the preferred method when using Cobra CLI.
func LoadFromViper() (*Config, error) {
	cfg := &Config{}
	applyDefaults(cfg)

	// Unmarshal viper config into our struct
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Handle environment variable overrides for tokens
	// These take precedence over config file values
	if tok := os.Getenv("AUTODOC_GITLAB_TOKEN"); tok != "" {
		cfg.Repository.Token = tok
	}
	if tok := os.Getenv("AUTODOC_GITHUB_TOKEN"); tok != "" {
		cfg.Repository.Token = tok
	}
	if tok := os.Getenv("AUTODOC_ACP_TOKEN"); tok != "" {
		cfg.ACP.Token = tok
	}

	// Also check viper environment variables
	if tok := viper.GetString("gitlab_token"); tok != "" {
		cfg.Repository.Token = tok
	}
	if tok := viper.GetString("github_token"); tok != "" {
		cfg.Repository.Token = tok
	}
	if tok := viper.GetString("acp_token"); tok != "" {
		cfg.ACP.Token = tok
	}

	// Apply auto-corrections and defaults where needed (corrections are silently applied).
	cfg.ValidateAndSetDefaults()

	// Final validation
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
