package validation

import (
	"context"
	"fmt"
	"strings"

	"log/slog"

	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
)

// Validator implements domain.ValidationPort.
type Validator struct {
	cfg config.ValidationConfig
	doc config.DocumentationConfig
	log *slog.Logger
}

// New creates a new Validator.
func New(cfg config.ValidationConfig, doc config.DocumentationConfig, log *slog.Logger) *Validator {
	return &Validator{cfg: cfg, doc: doc, log: log}
}

// Validate checks an updated Document against the validation rules.
func (v *Validator) Validate(ctx context.Context, original, updated domain.Document) error {
	// 1. Check path is within allowed paths.
	if !v.isAllowed(updated.Path) {
		return fmt.Errorf("validation: %w: %s", domain.ErrForbiddenPath, updated.Path)
	}

	// 2. Document must not be empty.
	if strings.TrimSpace(updated.Content) == "" {
		return fmt.Errorf("validation: %w: %s", domain.ErrEmptyDocument, updated.Path)
	}

	// 3. Check required sections.
	for docKey, sections := range v.doc.RequiredSections {
		_ = docKey
		for _, section := range sections {
			if !strings.Contains(updated.Content, section) {
				return fmt.Errorf("validation: %w: missing section %q in %s", domain.ErrMissingSections, section, updated.Path)
			}
		}
	}

	// 4. If ForbidNonDocChanges, path must start with "docs/" or equal "README.md".
	if v.cfg.ForbidNonDocChanges {
		if !strings.HasPrefix(updated.Path, "docs/") && updated.Path != "README.md" {
			return fmt.Errorf("validation: %w: %s is not a documentation path", domain.ErrForbiddenPath, updated.Path)
		}
	}

	return nil
}

// isAllowed checks whether path matches any of the configured allowed paths.
func (v *Validator) isAllowed(path string) bool {
	for _, pattern := range v.doc.AllowedPaths {
		trimmed := strings.TrimSuffix(pattern, "/**")
		if trimmed != pattern {
			if strings.HasPrefix(path, trimmed+"/") || path == trimmed {
				return true
			}
			continue
		}
		if pattern == path {
			return true
		}
	}
	return false
}
