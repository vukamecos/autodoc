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
}

type SchedulerConfig struct {
	Cron string `yaml:"cron"`
}

type RepositoryConfig struct {
	Provider          string   `yaml:"provider"`
	URL               string   `yaml:"url"`
	Token             string   `yaml:"token"`
	DefaultBranch     string   `yaml:"default_branch"`
	ProtectedBranches []string `yaml:"protected_branches"`
	ProjectID         string   `yaml:"project_id"`
}

type DocumentationConfig struct {
	AllowedPaths     []string            `yaml:"allowed_paths"`
	PrimaryLanguage  string              `yaml:"primary_language"`
	RequiredSections map[string][]string `yaml:"required_sections"`
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
	BaseURL         string        `yaml:"base_url"`
	Token           string        `yaml:"token"`
	Timeout         time.Duration `yaml:"timeout"`
	MaxContextBytes int           `yaml:"max_context_bytes"`
	Mode            string        `yaml:"mode"`
}

type GitConfig struct {
	BranchPrefix          string `yaml:"branch_prefix"`
	CommitMessageTemplate string `yaml:"commit_message_template"`
}

type ValidationConfig struct {
	MarkdownLint        bool `yaml:"markdown_lint"`
	ForbidNonDocChanges bool `yaml:"forbid_non_doc_changes"`
	MaxChangedFiles     int  `yaml:"max_changed_files"`
}

type StorageConfig struct {
	DSN string `yaml:"dsn"`
}

// Load reads a YAML config file at path and applies defaults and environment overrides.
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

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	cfg.Scheduler.Cron = "0 * * * *"
	cfg.Git.BranchPrefix = "bot/docs-update/"
	cfg.Git.CommitMessageTemplate = "docs: update documentation for recent repository changes"
	cfg.Storage.DSN = "autodoc.db"
	cfg.ACP.Timeout = 120 * time.Second
	cfg.ACP.MaxContextBytes = 500000
	cfg.Repository.DefaultBranch = "main"
}
