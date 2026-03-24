package validation

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

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

// Validate runs all configured checks against an updated Document.
// Checks are applied in order; the first failure is returned.
func (v *Validator) Validate(ctx context.Context, original, updated domain.Document) error {
	checks := []func() error{
		func() error { return v.checkAllowedPath(updated.Path) },
		func() error { return v.checkNotEmpty(updated) },
		func() error { return v.checkForbidNonDoc(updated.Path) },
		func() error { return v.checkRequiredSections(updated) },
		func() error { return v.checkContentShrink(original, updated) },
		func() error { return v.checkMarkdownLint(updated) },
	}

	for _, check := range checks {
		if err := check(); err != nil {
			v.log.WarnContext(ctx, "validation failed",
				slog.String("doc", updated.Path),
				slog.String("error", err.Error()),
			)
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
		return fmt.Errorf("validation: %w: %s", domain.ErrForbiddenPath, path)
	}
	return nil
}

// checkNotEmpty ensures the updated document is not blank.
func (v *Validator) checkNotEmpty(doc domain.Document) error {
	if strings.TrimSpace(doc.Content) == "" {
		return fmt.Errorf("validation: %w: %s", domain.ErrEmptyDocument, doc.Path)
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
		return fmt.Errorf("validation: %w: %s is not a documentation path", domain.ErrForbiddenPath, path)
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
				return fmt.Errorf("validation: %w: section %q missing in %s",
					domain.ErrMissingSections, section, doc.Path)
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
		return fmt.Errorf(
			"validation: content shrank too much in %s: updated=%d bytes, original=%d bytes, min_ratio=%.2f",
			updated.Path, len(updated.Content), len(original.Content), ratio,
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
		return fmt.Errorf("validation: markdown lint %s: %w", doc.Path, err)
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
