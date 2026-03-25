package config

import (
	"fmt"
	"strings"
)

// Validate checks the configuration for errors and returns a consolidated error
// if any required fields are missing or invalid.
func (cfg *Config) Validate() error {
	var errors []string

	// Repository validation
	if cfg.Repository.ProjectID == "" {
		errors = append(errors, "repository.project_id is required")
	}
	if cfg.Repository.Provider != "" && cfg.Repository.Provider != "gitlab" && cfg.Repository.Provider != "github" {
		errors = append(errors, fmt.Sprintf("repository.provider must be 'gitlab' or 'github', got %q", cfg.Repository.Provider))
	}
	if cfg.Repository.Provider == "gitlab" && cfg.Repository.URL == "" {
		errors = append(errors, "repository.url is required for GitLab provider")
	}

	// ACP validation
	if cfg.ACP.Provider != "" && cfg.ACP.Provider != "acp" && cfg.ACP.Provider != "ollama" {
		errors = append(errors, fmt.Sprintf("acp.provider must be 'acp' or 'ollama', got %q", cfg.ACP.Provider))
	}
	// acp.model is optional for ollama — when empty, the model is auto-selected
	// per request based on diff size (see usecase.ModelSelector).
	if cfg.ACP.MaxContextBytes <= 0 {
		errors = append(errors, fmt.Sprintf("acp.max_context_bytes must be positive, got %d", cfg.ACP.MaxContextBytes))
	}
	if cfg.ACP.Timeout <= 0 {
		errors = append(errors, fmt.Sprintf("acp.timeout must be positive, got %v", cfg.ACP.Timeout))
	}

	// Documentation validation
	if len(cfg.Documentation.AllowedPaths) == 0 {
		errors = append(errors, "documentation.allowed_paths must not be empty")
	}
	for _, path := range cfg.Documentation.AllowedPaths {
		if path == "" {
			errors = append(errors, "documentation.allowed_paths contains empty string")
			break
		}
	}

	// Language validation
	if cfg.Documentation.PrimaryLanguage == "" {
		errors = append(errors, "documentation.primary_language is required")
	}
	if len(cfg.Documentation.SupportedLanguages) == 0 {
		errors = append(errors, "documentation.supported_languages must not be empty")
	}
	primaryInSupported := false
	for _, lang := range cfg.Documentation.SupportedLanguages {
		if lang == "" {
			errors = append(errors, "documentation.supported_languages contains empty string")
			break
		}
		if lang == cfg.Documentation.PrimaryLanguage {
			primaryInSupported = true
		}
	}
	if cfg.Documentation.PrimaryLanguage != "" && len(cfg.Documentation.SupportedLanguages) > 0 && !primaryInSupported {
		errors = append(errors, fmt.Sprintf("primary_language '%s' must be in supported_languages", cfg.Documentation.PrimaryLanguage))
	}

	// Scheduler validation
	if cfg.Scheduler.Cron == "" {
		errors = append(errors, "scheduler.cron is required")
	}

	// Validation config
	if cfg.Validation.MinContentRatio < 0 || cfg.Validation.MinContentRatio > 1 {
		errors = append(errors, fmt.Sprintf("validation.min_content_ratio must be between 0 and 1, got %.2f", cfg.Validation.MinContentRatio))
	}

	// Git config
	if cfg.Git.BranchPrefix == "" {
		errors = append(errors, "git.branch_prefix is required")
	}

	if len(errors) > 0 {
		return fmt.Errorf("config validation failed:\n  - %s", strings.Join(errors, "\n  - "))
	}

	return nil
}

// ValidateAndSetDefaults validates the config and applies auto-corrections where possible.
// Returns true if any corrections were made.
func (cfg *Config) ValidateAndSetDefaults() (bool, []string) {
	var corrections []string

	// Auto-set provider defaults
	if cfg.Repository.Provider == "" {
		cfg.Repository.Provider = "gitlab"
		corrections = append(corrections, "repository.provider defaulted to 'gitlab'")
	}
	if cfg.ACP.Provider == "" {
		cfg.ACP.Provider = "acp"
		corrections = append(corrections, "acp.provider defaulted to 'acp'")
	}

	// Auto-detect language from allowed_paths if not set
	if cfg.Documentation.PrimaryLanguage == "" {
		cfg.Documentation.PrimaryLanguage = "en"
		corrections = append(corrections, "documentation.primary_language defaulted to 'en'")
	}

	// If supported_languages not set, use primary language only
	if len(cfg.Documentation.SupportedLanguages) == 0 {
		cfg.Documentation.SupportedLanguages = []string{cfg.Documentation.PrimaryLanguage}
		corrections = append(corrections, fmt.Sprintf("documentation.supported_languages defaulted to [%s]", cfg.Documentation.PrimaryLanguage))
	}

	// Validate that primary_language is in supported_languages
	primaryInSupported := false
	for _, lang := range cfg.Documentation.SupportedLanguages {
		if lang == cfg.Documentation.PrimaryLanguage {
			primaryInSupported = true
			break
		}
	}
	if !primaryInSupported {
		cfg.Documentation.SupportedLanguages = append(cfg.Documentation.SupportedLanguages, cfg.Documentation.PrimaryLanguage)
		corrections = append(corrections, fmt.Sprintf("added primary_language '%s' to supported_languages", cfg.Documentation.PrimaryLanguage))
	}

	// Validate after corrections
	if err := cfg.Validate(); err != nil {
		return false, []string{err.Error()}
	}

	return len(corrections) > 0, corrections
}
