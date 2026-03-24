package usecase

import (
	"context"
	"testing"

	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
)

var ctx = context.Background()

// ---------------------------------------------------------------------------
// ChangeAnalyzer
// ---------------------------------------------------------------------------

func TestChangeAnalyzer_ClassifiesCode(t *testing.T) {
	a := NewChangeAnalyzer()
	diffs := []domain.FileDiff{{Path: "internal/service/payment.go"}}
	changes, err := a.Analyze(ctx, diffs)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Category != domain.FileCategoryCode {
		t.Errorf("expected Code, got %v", changes[0].Category)
	}
}

func TestChangeAnalyzer_ClassifiesConfig(t *testing.T) {
	a := NewChangeAnalyzer()
	for _, path := range []string{"configs/app.yaml", ".env.example", "config.toml"} {
		diffs := []domain.FileDiff{{Path: path}}
		changes, _ := a.Analyze(ctx, diffs)
		if len(changes) != 1 || changes[0].Category != domain.FileCategoryConfig {
			t.Errorf("expected Config for %q, got %v", path, changes[0].Category)
		}
	}
}

func TestChangeAnalyzer_ClassifiesInfrastructure(t *testing.T) {
	a := NewChangeAnalyzer()
	for _, path := range []string{"Dockerfile", "k8s/deploy.yaml", "infra/main.tf"} {
		diffs := []domain.FileDiff{{Path: path}}
		changes, _ := a.Analyze(ctx, diffs)
		if len(changes) != 1 || changes[0].Category != domain.FileCategoryInfrastructure {
			t.Errorf("expected Infrastructure for %q, got %v", path, changes[0].Category)
		}
	}
}

func TestChangeAnalyzer_ExcludesDocumentation(t *testing.T) {
	a := NewChangeAnalyzer()
	for _, path := range []string{"docs/arch.md", "README.md", "docs/modules/auth.md"} {
		diffs := []domain.FileDiff{{Path: path}}
		changes, _ := a.Analyze(ctx, diffs)
		if len(changes) != 0 {
			t.Errorf("expected documentation file %q to be excluded, got %d changes", path, len(changes))
		}
	}
}

func TestChangeAnalyzer_ClassifiesTest(t *testing.T) {
	a := NewChangeAnalyzer()
	diffs := []domain.FileDiff{{Path: "internal/service/payment_test.go"}}
	changes, _ := a.Analyze(ctx, diffs)
	if len(changes) != 1 || changes[0].Category != domain.FileCategoryTest {
		t.Errorf("expected Test category, got %v", changes[0].Category)
	}
}

func TestChangeAnalyzer_APIImpactZone(t *testing.T) {
	a := NewChangeAnalyzer()
	diffs := []domain.FileDiff{{Path: "api/auth/handler.go"}}
	changes, _ := a.Analyze(ctx, diffs)
	if len(changes) == 0 {
		t.Fatal("expected 1 change")
	}
	hasAPI := false
	for _, z := range changes[0].ImpactZones {
		if z == domain.ImpactZoneAPI {
			hasAPI = true
		}
	}
	if !hasAPI {
		t.Errorf("expected API impact zone for api/ path, got %v", changes[0].ImpactZones)
	}
}

func TestChangeAnalyzer_EmptyDiff(t *testing.T) {
	a := NewChangeAnalyzer()
	changes, err := a.Analyze(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("expected 0 changes for empty diff, got %d", len(changes))
	}
}

// ---------------------------------------------------------------------------
// DocumentMapper
// ---------------------------------------------------------------------------

func TestDocumentMapper_RuleMatch(t *testing.T) {
	cfg := config.MappingConfig{
		Rules: []config.MappingRule{
			{
				Match:  config.MappingMatch{Paths: []string{"internal/auth/**"}},
				Update: []string{"README.md", "docs/modules/auth.md"},
			},
		},
	}
	m := NewDocumentMapper(cfg)

	changes := []domain.AnalyzedChange{
		{Diff: domain.FileDiff{Path: "internal/auth/handler.go"}},
	}
	paths, err := m.MapToDocs(ctx, changes)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 doc paths, got %d: %v", len(paths), paths)
	}
}

func TestDocumentMapper_FallbackToReadme(t *testing.T) {
	m := NewDocumentMapper(config.MappingConfig{}) // no rules

	changes := []domain.AnalyzedChange{
		{Diff: domain.FileDiff{Path: "internal/service/foo.go"}},
	}
	paths, err := m.MapToDocs(ctx, changes)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "README.md" {
		t.Errorf("expected [README.md] as fallback, got %v", paths)
	}
}

func TestDocumentMapper_Deduplication(t *testing.T) {
	cfg := config.MappingConfig{
		Rules: []config.MappingRule{
			{
				Match:  config.MappingMatch{Paths: []string{"internal/**"}},
				Update: []string{"README.md", "docs/arch.md"},
			},
		},
	}
	m := NewDocumentMapper(cfg)

	// Two changes both matching the same rule — each target should appear once.
	changes := []domain.AnalyzedChange{
		{Diff: domain.FileDiff{Path: "internal/foo.go"}},
		{Diff: domain.FileDiff{Path: "internal/bar.go"}},
	}
	paths, err := m.MapToDocs(ctx, changes)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Errorf("expected 2 unique doc paths, got %d: %v", len(paths), paths)
	}
}

func TestDocumentMapper_ExactPathMatch(t *testing.T) {
	cfg := config.MappingConfig{
		Rules: []config.MappingRule{
			{
				Match:  config.MappingMatch{Paths: []string{".env.example"}},
				Update: []string{"docs/configuration.md"},
			},
		},
	}
	m := NewDocumentMapper(cfg)

	changes := []domain.AnalyzedChange{
		{Diff: domain.FileDiff{Path: ".env.example"}},
	}
	paths, _ := m.MapToDocs(ctx, changes)
	if len(paths) != 1 || paths[0] != "docs/configuration.md" {
		t.Errorf("expected [docs/configuration.md], got %v", paths)
	}
}
