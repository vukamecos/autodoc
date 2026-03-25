package validation

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/vukamecos/autodoc/internal/infrastructure/config"
	"github.com/vukamecos/autodoc/internal/domain"
	"github.com/vukamecos/autodoc/internal/infrastructure/observability"
)

// Validator implements domain.ValidationPort.
type Validator struct {
	cfg     config.ValidationConfig
	doc     config.DocumentationConfig
	log     *slog.Logger
	metrics *observability.Metrics
}

// New creates a new Validator.
func New(cfg config.ValidationConfig, doc config.DocumentationConfig, log *slog.Logger, metrics *observability.Metrics) *Validator {
	return &Validator{cfg: cfg, doc: doc, log: log, metrics: metrics}
}

// Validate runs all configured checks against an updated Document.
// Checks are applied in order; the first failure is returned.
func (v *Validator) Validate(ctx context.Context, original, updated domain.Document) error {
	checks := []struct {
		name string
		fn   func() error
	}{
		{"allowed_path", func() error { return v.checkAllowedPath(updated.Path) }},
		{"not_empty", func() error { return v.checkNotEmpty(updated) }},
		{"forbid_non_doc", func() error { return v.checkForbidNonDoc(updated.Path) }},
		{"required_sections", func() error { return v.checkRequiredSections(updated) }},
		{"content_shrink", func() error { return v.checkContentShrink(original, updated) }},
		{"markdown_lint", func() error { return v.checkMarkdownLint(updated) }},
	}

	for _, check := range checks {
		if err := check.fn(); err != nil {
			v.log.WarnContext(ctx, "validation failed",
				slog.String("doc", updated.Path),
				slog.String("check", check.name),
				slog.String("error", err.Error()),
			)
			if v.metrics != nil {
				v.metrics.ValidationFailuresTotal.WithLabelValues(check.name).Inc()
			}
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Individual checks
// ---------------------------------------------------------------------------

// checkAllowedPath verifies the path matches an allowed-paths glob.
func (v *Validator) checkAllowedPath(path string) error {
	if !v.isAllowed(path) {
		return fmt.Errorf("path %q not in allowed paths %v", path, v.doc.AllowedPaths)
	}
	return nil
}

// checkNotEmpty ensures the updated document is not blank.
func (v *Validator) checkNotEmpty(doc domain.Document) error {
	if strings.TrimSpace(doc.Content) == "" {
		return fmt.Errorf("document %s has empty content", doc.Path)
	}
	return nil
}

// checkForbidNonDoc rejects paths outside docs/ and README.md when
// ForbidNonDocChanges is enabled.
func (v *Validator) checkForbidNonDoc(path string) error {
	if !v.cfg.ForbidNonDocChanges {
		return nil
	}
	if !strings.HasPrefix(path, "docs/") && !strings.EqualFold(filepath.Base(path), "readme.md") {
		return fmt.Errorf("path %q is not a documentation path (ForbidNonDocChanges enabled)", path)
	}
	return nil
}

// checkRequiredSections verifies that required sections are present.
// The config key (e.g. "readme") is matched against the document's base name
// without extension (case-insensitive).
func (v *Validator) checkRequiredSections(doc domain.Document) error {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(doc.Path), filepath.Ext(doc.Path)))
	for key, sections := range v.doc.RequiredSections {
		if !strings.EqualFold(key, base) {
			continue
		}
		for _, section := range sections {
			if !strings.Contains(doc.Content, section) {
				return fmt.Errorf("required section %q missing in %s (config key: %s)", section, doc.Path, key)
			}
		}
	}
	return nil
}

// checkContentShrink rejects an update that reduces the document to less than
// MinContentRatio of the original length, guarding against accidental deletions.
func (v *Validator) checkContentShrink(original, updated domain.Document) error {
	ratio := v.cfg.MinContentRatio
	if ratio <= 0 || len(original.Content) == 0 {
		return nil
	}
	threshold := int(float64(len(original.Content)) * ratio)
	if len(updated.Content) < threshold {
		actualRatio := float64(len(updated.Content)) / float64(len(original.Content))
		return fmt.Errorf(
			"content shrank from %d to %d bytes (ratio %.2f, min %.2f) in %s",
			len(original.Content), len(updated.Content), actualRatio, ratio, updated.Path,
		)
	}
	return nil
}

// checkMarkdownLint runs basic structural checks on markdown content.
// Only active when cfg.MarkdownLint is true.
func (v *Validator) checkMarkdownLint(doc domain.Document) error {
	if !v.cfg.MarkdownLint {
		return nil
	}
	if err := lintMarkdown(doc.Path, doc.Content); err != nil {
		return fmt.Errorf("markdown lint: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Markdown linter
// ---------------------------------------------------------------------------

// lintMarkdown performs structural checks on a markdown document.
//
// Checks:
//  1. Code fences (```) must be balanced (even count).
//  2. Headings must not be empty (e.g. "## " with no title text).
//  3. No tab characters at the start of a line inside a fenced code block
//     that would be misinterpreted as an indented code block (informational only,
//     skipped here to avoid false positives).
func lintMarkdown(path, content string) error {
	var (
		fenceCount  int
		inFence     bool
		fenceMarker string
	)

	for lineno, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		// Track fenced code blocks (``` or ~~~).
		if marker, ok := fenceStart(trimmed); ok {
			if !inFence {
				inFence = true
				fenceMarker = marker
				fenceCount++
			} else if strings.HasPrefix(trimmed, fenceMarker) && strings.TrimSpace(strings.TrimPrefix(trimmed, fenceMarker)) == "" {
				// Closing fence
				inFence = false
				fenceMarker = ""
				fenceCount++
			}
			continue
		}

		if inFence {
			continue // content inside a code block is not linted
		}

		// Check for empty headings outside code blocks.
		if strings.HasPrefix(trimmed, "#") {
			i := 0
			for i < len(trimmed) && trimmed[i] == '#' {
				i++
			}
			if i <= 6 {
				rest := strings.TrimSpace(trimmed[i:])
				if rest == "" {
					return fmt.Errorf("line %d: empty heading %q", lineno+1, trimmed)
				}
			}
		}
	}

	// Fences must be balanced (even count means all opened fences are closed).
	if fenceCount%2 != 0 {
		return fmt.Errorf("unclosed code fence in %s (odd fence count: %d)", path, fenceCount)
	}

	return nil
}

// fenceStart returns the fence marker (``` or ~~~) if line opens a code fence.
func fenceStart(line string) (marker string, ok bool) {
	for _, m := range []string{"```", "~~~"} {
		if strings.HasPrefix(line, m) {
			return m, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (v *Validator) isAllowed(path string) bool {
	for _, pattern := range v.doc.AllowedPaths {
		trimmed := strings.TrimSuffix(pattern, "/**")
		if trimmed != pattern {
			if strings.HasPrefix(path, trimmed+"/") || path == trimmed {
				return true
			}
			continue
		}
		matched, err := filepath.Match(pattern, path)
		if err == nil && matched {
			return true
		}
	}
	return false
}
