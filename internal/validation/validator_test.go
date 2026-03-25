package validation

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/vukamecos/autodoc/internal/infrastructure/config"
	"github.com/vukamecos/autodoc/internal/domain"
)

var ctx = context.Background()

func newValidator(valCfg config.ValidationConfig, docCfg config.DocumentationConfig) *Validator {
	return New(valCfg, docCfg, slog.Default(), nil)
}

// ---------------------------------------------------------------------------
// checkAllowedPath
// ---------------------------------------------------------------------------

func TestCheckAllowedPath(t *testing.T) {
	docCfg := config.DocumentationConfig{
		AllowedPaths: []string{"README.md", "docs/**"},
	}
	v := newValidator(config.ValidationConfig{}, docCfg)

	allowed := []string{"README.md", "docs/arch.md", "docs/modules/auth.md"}
	for _, p := range allowed {
		if err := v.checkAllowedPath(p); err != nil {
			t.Errorf("expected %q to be allowed, got error: %v", p, err)
		}
	}

	forbidden := []string{"internal/foo.go", "cmd/main.go", "docs.md"}
	for _, p := range forbidden {
		if err := v.checkAllowedPath(p); err == nil {
			t.Errorf("expected %q to be forbidden, but got no error", p)
		}
	}
}

// ---------------------------------------------------------------------------
// checkNotEmpty
// ---------------------------------------------------------------------------

func TestCheckNotEmpty(t *testing.T) {
	v := newValidator(config.ValidationConfig{}, config.DocumentationConfig{})

	if err := v.checkNotEmpty(domain.Document{Path: "README.md", Content: "# Hello\n"}); err != nil {
		t.Errorf("unexpected error for non-empty doc: %v", err)
	}
	if err := v.checkNotEmpty(domain.Document{Path: "README.md", Content: "   "}); err == nil {
		t.Error("expected error for whitespace-only doc")
	}
	if err := v.checkNotEmpty(domain.Document{Path: "README.md", Content: ""}); err == nil {
		t.Error("expected error for empty doc")
	}
}

// ---------------------------------------------------------------------------
// checkForbidNonDoc
// ---------------------------------------------------------------------------

func TestCheckForbidNonDoc(t *testing.T) {
	v := newValidator(config.ValidationConfig{ForbidNonDocChanges: true}, config.DocumentationConfig{})

	ok := []string{"docs/arch.md", "README.md", "docs/modules/auth.md"}
	for _, p := range ok {
		if err := v.checkForbidNonDoc(p); err != nil {
			t.Errorf("expected %q to pass, got: %v", p, err)
		}
	}

	bad := []string{"internal/foo.go", "Makefile", "go.mod"}
	for _, p := range bad {
		if err := v.checkForbidNonDoc(p); err == nil {
			t.Errorf("expected %q to be rejected, but got no error", p)
		}
	}
}

func TestCheckForbidNonDoc_Disabled(t *testing.T) {
	v := newValidator(config.ValidationConfig{ForbidNonDocChanges: false}, config.DocumentationConfig{})
	if err := v.checkForbidNonDoc("internal/foo.go"); err != nil {
		t.Errorf("expected no error when check is disabled, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// checkRequiredSections
// ---------------------------------------------------------------------------

func TestCheckRequiredSections_Present(t *testing.T) {
	docCfg := config.DocumentationConfig{
		RequiredSections: map[string][]string{
			"readme": {"## Description", "## Running"},
		},
	}
	v := newValidator(config.ValidationConfig{}, docCfg)

	doc := domain.Document{
		Path:    "README.md",
		Content: "## Description\nfoo\n## Running\nbar\n",
	}
	if err := v.checkRequiredSections(doc); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckRequiredSections_Missing(t *testing.T) {
	docCfg := config.DocumentationConfig{
		RequiredSections: map[string][]string{
			"readme": {"## Running"},
		},
	}
	v := newValidator(config.ValidationConfig{}, docCfg)

	doc := domain.Document{
		Path:    "README.md",
		Content: "## Description\nno running section\n",
	}
	err := v.checkRequiredSections(doc)
	if err == nil {
		t.Fatal("expected error for missing required section")
	}
	if !strings.Contains(err.Error(), "section \"## Running\" missing") {
		t.Errorf("expected error mentioning missing section, got: %v", err)
	}
}

func TestCheckRequiredSections_OnlyMatchesCorrectFile(t *testing.T) {
	docCfg := config.DocumentationConfig{
		RequiredSections: map[string][]string{
			"readme": {"## Special"},
		},
	}
	v := newValidator(config.ValidationConfig{}, docCfg)

	// A different file — the readme rules should not apply.
	doc := domain.Document{
		Path:    "docs/arch.md",
		Content: "no special section here",
	}
	if err := v.checkRequiredSections(doc); err != nil {
		t.Errorf("required-section rule should not apply to %q, got: %v", doc.Path, err)
	}
}

// ---------------------------------------------------------------------------
// checkContentShrink
// ---------------------------------------------------------------------------

func TestCheckContentShrink_WithinRatio(t *testing.T) {
	v := newValidator(config.ValidationConfig{MinContentRatio: 0.5}, config.DocumentationConfig{})
	orig := domain.Document{Content: strings.Repeat("x", 100)}
	upd := domain.Document{Content: strings.Repeat("x", 60)}
	if err := v.checkContentShrink(orig, upd); err != nil {
		t.Errorf("unexpected shrink error: %v", err)
	}
}

func TestCheckContentShrink_BelowRatio(t *testing.T) {
	v := newValidator(config.ValidationConfig{MinContentRatio: 0.5}, config.DocumentationConfig{})
	orig := domain.Document{Content: strings.Repeat("x", 100)}
	upd := domain.Document{Content: strings.Repeat("x", 40)}
	if err := v.checkContentShrink(orig, upd); err == nil {
		t.Error("expected shrink error when content drops below ratio")
	}
}

func TestCheckContentShrink_OriginalEmpty(t *testing.T) {
	v := newValidator(config.ValidationConfig{MinContentRatio: 0.5}, config.DocumentationConfig{})
	if err := v.checkContentShrink(domain.Document{}, domain.Document{Content: "new"}); err != nil {
		t.Errorf("expected no error when original is empty, got: %v", err)
	}
}

func TestCheckContentShrink_ZeroRatioDisabled(t *testing.T) {
	v := newValidator(config.ValidationConfig{MinContentRatio: 0}, config.DocumentationConfig{})
	orig := domain.Document{Content: strings.Repeat("x", 100)}
	upd := domain.Document{Content: "x"}
	if err := v.checkContentShrink(orig, upd); err != nil {
		t.Errorf("expected no error when ratio is 0 (disabled), got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// checkMarkdownLint
// ---------------------------------------------------------------------------

func TestCheckMarkdownLint_Valid(t *testing.T) {
	v := newValidator(config.ValidationConfig{MarkdownLint: true}, config.DocumentationConfig{})

	cases := []string{
		"# Title\nbody text\n",
		"## Section\n```go\nfunc foo() {}\n```\n",
		"text\n~~~\ncode\n~~~\n",
	}
	for _, c := range cases {
		doc := domain.Document{Path: "README.md", Content: c}
		if err := v.checkMarkdownLint(doc); err != nil {
			t.Errorf("unexpected lint error for %q: %v", c, err)
		}
	}
}

func TestCheckMarkdownLint_UnclosedFence(t *testing.T) {
	v := newValidator(config.ValidationConfig{MarkdownLint: true}, config.DocumentationConfig{})
	doc := domain.Document{
		Path:    "README.md",
		Content: "# Title\n```go\nfunc foo() {}\n", // fence never closed
	}
	if err := v.checkMarkdownLint(doc); err == nil {
		t.Error("expected lint error for unclosed fence")
	}
}

func TestCheckMarkdownLint_EmptyHeading(t *testing.T) {
	v := newValidator(config.ValidationConfig{MarkdownLint: true}, config.DocumentationConfig{})
	doc := domain.Document{
		Path:    "README.md",
		Content: "## \nsome body\n",
	}
	if err := v.checkMarkdownLint(doc); err == nil {
		t.Error("expected lint error for empty heading")
	}
}

func TestCheckMarkdownLint_HeadingInsideFenceNotFlagged(t *testing.T) {
	v := newValidator(config.ValidationConfig{MarkdownLint: true}, config.DocumentationConfig{})
	doc := domain.Document{
		Path:    "README.md",
		Content: "```\n## \n```\n", // empty heading is inside a code block
	}
	if err := v.checkMarkdownLint(doc); err != nil {
		t.Errorf("empty heading inside fence should not be flagged: %v", err)
	}
}

func TestCheckMarkdownLint_Disabled(t *testing.T) {
	v := newValidator(config.ValidationConfig{MarkdownLint: false}, config.DocumentationConfig{})
	doc := domain.Document{
		Path:    "README.md",
		Content: "## \n```unclosed\n",
	}
	if err := v.checkMarkdownLint(doc); err != nil {
		t.Errorf("expected no error when lint is disabled, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Validate (integration — first failing check short-circuits)
// ---------------------------------------------------------------------------

func TestValidate_ShortCircuitsOnFirstFailure(t *testing.T) {
	docCfg := config.DocumentationConfig{
		AllowedPaths: []string{"docs/**"},
	}
	valCfg := config.ValidationConfig{MarkdownLint: true}
	v := newValidator(valCfg, docCfg)

	// Path is forbidden — should fail on checkAllowedPath, not reach lint.
	orig := domain.Document{Path: "docs/ok.md", Content: "# Title\n"}
	upd := domain.Document{Path: "internal/bad.go", Content: "# Title\n"}

	err := v.Validate(ctx, orig, upd)
	if err == nil {
		t.Fatal("expected validation error")
	}
	// First check is allowed_path, should fail with path not allowed
	if !strings.Contains(err.Error(), "not in allowed paths") {
		t.Errorf("expected 'not in allowed paths' error as first failure, got: %v", err)
	}
}
