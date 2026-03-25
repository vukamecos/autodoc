package usecase

import (
	"context"
	"testing"

	"github.com/vukamecos/autodoc/internal/infrastructure/config"
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

// ---------------------------------------------------------------------------
// Additional tests for classifyFile and priorityFor
// ---------------------------------------------------------------------------

func TestClassifyFile_EdgeCases(t *testing.T) {
	tests := []struct {
		path     string
		category domain.FileCategory
		zones    []domain.ImpactZone
	}{
		// Documentation variants
		{"docs/README.md", domain.FileCategoryDocumentation, nil},
		{"readme.markdown", domain.FileCategoryDocumentation, nil},
		{"README.rst", domain.FileCategoryDocumentation, nil},
		{"docs/subdir/file.md", domain.FileCategoryDocumentation, nil},
		
		// Test files variants
		{"foo_test.go", domain.FileCategoryTest, []domain.ImpactZone{domain.ImpactZoneModule}},
		{"test/integration/test.py", domain.FileCategoryTest, []domain.ImpactZone{domain.ImpactZoneModule}},
		{"test/component_test.js", domain.FileCategoryTest, []domain.ImpactZone{domain.ImpactZoneModule}},
		
		// Infrastructure variants
		{"docker-compose.yml", domain.FileCategoryInfrastructure, []domain.ImpactZone{domain.ImpactZoneArchitecture}},
		{"Dockerfile.prod", domain.FileCategoryInfrastructure, []domain.ImpactZone{domain.ImpactZoneArchitecture}},
		{"k8s/production/deployment.yaml", domain.FileCategoryInfrastructure, []domain.ImpactZone{domain.ImpactZoneArchitecture}},
		{"terraform/main.tf", domain.FileCategoryInfrastructure, []domain.ImpactZone{domain.ImpactZoneArchitecture}},
		{"infrastructure/aws/ecs.tf", domain.FileCategoryInfrastructure, []domain.ImpactZone{domain.ImpactZoneArchitecture}},
		
		// Config variants
		{"config.yaml", domain.FileCategoryConfig, []domain.ImpactZone{domain.ImpactZoneConfig}},
		{"settings.yml", domain.FileCategoryConfig, []domain.ImpactZone{domain.ImpactZoneConfig}},
		{"app.toml", domain.FileCategoryConfig, []domain.ImpactZone{domain.ImpactZoneConfig}},
		{".env.local", domain.FileCategoryConfig, []domain.ImpactZone{domain.ImpactZoneConfig}},
		{".env.production", domain.FileCategoryConfig, []domain.ImpactZone{domain.ImpactZoneConfig}},
		
		// Code with API path
		{"internal/api/users/handler.go", domain.FileCategoryCode, []domain.ImpactZone{domain.ImpactZoneModule, domain.ImpactZoneAPI}},
		{"pkg/api/v2/client.ts", domain.FileCategoryCode, []domain.ImpactZone{domain.ImpactZoneModule, domain.ImpactZoneAPI}},
		{"src/api/routes.js", domain.FileCategoryCode, []domain.ImpactZone{domain.ImpactZoneModule, domain.ImpactZoneAPI}},
		
		// Code without API path
		{"internal/service/user.go", domain.FileCategoryCode, []domain.ImpactZone{domain.ImpactZoneModule}},
		{"pkg/utils/helpers.go", domain.FileCategoryCode, []domain.ImpactZone{domain.ImpactZoneModule}},
		{"main.go", domain.FileCategoryCode, []domain.ImpactZone{domain.ImpactZoneModule}},
		{"src/components/Button.tsx", domain.FileCategoryCode, []domain.ImpactZone{domain.ImpactZoneModule}},
	}
	
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			a := NewChangeAnalyzer()
			diffs := []domain.FileDiff{{Path: tc.path}}
			changes, _ := a.Analyze(ctx, diffs)
			
			if len(changes) == 0 {
				if tc.category != domain.FileCategoryDocumentation {
					t.Fatalf("expected 1 change for %q, got 0", tc.path)
				}
				return // documentation is filtered out
			}
			
			if changes[0].Category != tc.category {
				t.Errorf("expected category %v for %q, got %v", tc.category, tc.path, changes[0].Category)
			}
			
			if tc.zones != nil {
				if len(changes[0].ImpactZones) != len(tc.zones) {
					t.Errorf("expected %d impact zones for %q, got %d", len(tc.zones), tc.path, len(changes[0].ImpactZones))
				}
			}
		})
	}
}

func TestPriorityFor_Ordering(t *testing.T) {
	a := NewChangeAnalyzer()
	
	// Get priorities for different categories
	codeDiffs := []domain.FileDiff{{Path: "main.go"}}
	configDiffs := []domain.FileDiff{{Path: "config.yaml"}}
	infraDiffs := []domain.FileDiff{{Path: "Dockerfile"}}
	testDiffs := []domain.FileDiff{{Path: "main_test.go"}}
	
	codeChanges, _ := a.Analyze(ctx, codeDiffs)
	configChanges, _ := a.Analyze(ctx, configDiffs)
	infraChanges, _ := a.Analyze(ctx, infraDiffs)
	testChanges, _ := a.Analyze(ctx, testDiffs)
	
	// Code should have highest priority
	if codeChanges[0].Priority <= configChanges[0].Priority {
		t.Error("Code should have higher priority than Config")
	}
	if configChanges[0].Priority <= infraChanges[0].Priority {
		t.Error("Config should have higher priority than Infrastructure")
	}
	if infraChanges[0].Priority <= testChanges[0].Priority {
		t.Error("Infrastructure should have higher priority than Test")
	}
}

func TestChangeAnalyzer_MultipleMixedChanges(t *testing.T) {
	a := NewChangeAnalyzer()
	diffs := []domain.FileDiff{
		{Path: "internal/service/user.go"},
		{Path: "config.yaml"},
		{Path: "Dockerfile"},
		{Path: "main_test.go"},
		{Path: "docs/README.md"}, // should be excluded
	}
	
	changes, err := a.Analyze(ctx, diffs)
	if err != nil {
		t.Fatal(err)
	}
	
	// Should have 4 changes (docs excluded)
	if len(changes) != 4 {
		t.Errorf("expected 4 changes (docs excluded), got %d", len(changes))
	}
	
	// Verify categories
	categories := make(map[domain.FileCategory]int)
	for _, c := range changes {
		categories[c.Category]++
	}
	
	if categories[domain.FileCategoryCode] != 1 {
		t.Errorf("expected 1 Code change, got %d", categories[domain.FileCategoryCode])
	}
	if categories[domain.FileCategoryConfig] != 1 {
		t.Errorf("expected 1 Config change, got %d", categories[domain.FileCategoryConfig])
	}
	if categories[domain.FileCategoryInfrastructure] != 1 {
		t.Errorf("expected 1 Infrastructure change, got %d", categories[domain.FileCategoryInfrastructure])
	}
	if categories[domain.FileCategoryTest] != 1 {
		t.Errorf("expected 1 Test change, got %d", categories[domain.FileCategoryTest])
	}
}

// ---------------------------------------------------------------------------
// Additional tests for DocumentMapper
// ---------------------------------------------------------------------------

func TestDocumentMapper_MultipleRules(t *testing.T) {
	cfg := config.MappingConfig{
		Rules: []config.MappingRule{
			{
				Match:  config.MappingMatch{Paths: []string{"internal/auth/**", "api/auth/**"}},
				Update: []string{"docs/modules/auth.md"},
			},
			{
				Match:  config.MappingMatch{Paths: []string{"internal/payment/**"}},
				Update: []string{"docs/modules/payment.md"},
			},
			{
				Match:  config.MappingMatch{Paths: []string{"configs/**"}},
				Update: []string{"docs/configuration.md"},
			},
		},
	}
	m := NewDocumentMapper(cfg)
	
	// Changes matching different rules
	changes := []domain.AnalyzedChange{
		{Diff: domain.FileDiff{Path: "internal/auth/handler.go"}},
		{Diff: domain.FileDiff{Path: "internal/payment/service.go"}},
		{Diff: domain.FileDiff{Path: "configs/app.yaml"}},
	}
	
	paths, err := m.MapToDocs(ctx, changes)
	if err != nil {
		t.Fatal(err)
	}
	
	// Should have 3 unique doc paths
	if len(paths) != 3 {
		t.Errorf("expected 3 doc paths, got %d: %v", len(paths), paths)
	}
	
	expectedPaths := map[string]bool{
		"docs/modules/auth.md":    false,
		"docs/modules/payment.md": false,
		"docs/configuration.md":   false,
	}
	
	for _, p := range paths {
		expectedPaths[p] = true
	}
	
	for p, found := range expectedPaths {
		if !found {
			t.Errorf("expected %q in results", p)
		}
	}
}

func TestDocumentMapper_OverlappingRules(t *testing.T) {
	cfg := config.MappingConfig{
		Rules: []config.MappingRule{
			{
				Match:  config.MappingMatch{Paths: []string{"internal/**"}},
				Update: []string{"README.md", "docs/architecture.md"},
			},
			{
				Match:  config.MappingMatch{Paths: []string{"internal/auth/**"}},
				Update: []string{"docs/modules/auth.md"},
			},
		},
	}
	m := NewDocumentMapper(cfg)
	
	// File matching both rules (specific and general)
	changes := []domain.AnalyzedChange{
		{Diff: domain.FileDiff{Path: "internal/auth/handler.go"}},
	}
	
	paths, err := m.MapToDocs(ctx, changes)
	if err != nil {
		t.Fatal(err)
	}
	
	// Should have all 3 docs from both rules
	if len(paths) != 3 {
		t.Errorf("expected 3 doc paths (from both rules), got %d: %v", len(paths), paths)
	}
}

func TestDocumentMapper_NoMatchingRules(t *testing.T) {
	cfg := config.MappingConfig{
		Rules: []config.MappingRule{
			{
				Match:  config.MappingMatch{Paths: []string{"internal/**"}},
				Update: []string{"README.md"},
			},
		},
	}
	m := NewDocumentMapper(cfg)
	
	// Changes not matching any rule should fallback to README.md
	changes := []domain.AnalyzedChange{
		{Diff: domain.FileDiff{Path: "cmd/main.go"}},
	}
	
	paths, err := m.MapToDocs(ctx, changes)
	if err != nil {
		t.Fatal(err)
	}
	
	// Fallback to README.md
	if len(paths) != 1 || paths[0] != "README.md" {
		t.Errorf("expected [README.md] as fallback, got %v", paths)
	}
}

func TestDocumentMapper_GlobPatterns(t *testing.T) {
	cfg := config.MappingConfig{
		Rules: []config.MappingRule{
			{
				Match:  config.MappingMatch{Paths: []string{"*.go"}},
				Update: []string{"docs/go-code.md"},
			},
			{
				Match:  config.MappingMatch{Paths: []string{"*.proto"}},
				Update: []string{"docs/api-spec.md"},
			},
		},
	}
	m := NewDocumentMapper(cfg)
	
	tests := []struct {
		path     string
		expected string
	}{
		{"main.go", "docs/go-code.md"},
		{"service.go", "docs/go-code.md"},
		{"api.proto", "docs/api-spec.md"},
		{"types.proto", "docs/api-spec.md"},
	}
	
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			changes := []domain.AnalyzedChange{{Diff: domain.FileDiff{Path: tc.path}}}
			paths, _ := m.MapToDocs(ctx, changes)
			if len(paths) != 1 || paths[0] != tc.expected {
				t.Errorf("expected [%s], got %v", tc.expected, paths)
			}
		})
	}
}

func TestDocumentMapper_EmptyChanges(t *testing.T) {
	m := NewDocumentMapper(config.MappingConfig{})
	
	paths, err := m.MapToDocs(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	
	if len(paths) != 0 {
		t.Errorf("expected 0 paths for empty changes, got %d", len(paths))
	}
}
