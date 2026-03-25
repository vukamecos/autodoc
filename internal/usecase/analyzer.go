package usecase

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/vukamecos/autodoc/internal/domain"
)

// ChangeAnalyzer implements domain.ChangeAnalyzerPort using simple path rules.
type ChangeAnalyzer struct{}

// NewChangeAnalyzer creates a ChangeAnalyzer.
func NewChangeAnalyzer() *ChangeAnalyzer { return &ChangeAnalyzer{} }

// Analyze classifies diffs and returns AnalyzedChanges. Documentation-only
// changes are excluded from the result (they don't trigger doc updates).
func (a *ChangeAnalyzer) Analyze(_ context.Context, diffs []domain.FileDiff) ([]domain.AnalyzedChange, error) {
	var results []domain.AnalyzedChange
	for _, d := range diffs {
		category, zones := classifyFile(d.Path)
		if category == domain.FileCategoryDocumentation {
			continue
		}
		results = append(results, domain.AnalyzedChange{
			Diff:        d,
			Category:    category,
			ImpactZones: zones,
			Priority:    priorityFor(category),
		})
	}
	return results, nil
}

func classifyFile(path string) (domain.FileCategory, []domain.ImpactZone) {
	lower := strings.ToLower(path)
	base := strings.ToLower(filepath.Base(path))

	switch {
	case strings.HasPrefix(lower, "docs/") || strings.HasPrefix(base, "readme"):
		return domain.FileCategoryDocumentation, nil

	case strings.HasSuffix(lower, "_test.go") || strings.HasPrefix(lower, "test/"):
		return domain.FileCategoryTest, []domain.ImpactZone{domain.ImpactZoneModule}

	case strings.HasPrefix(base, "docker") ||
		strings.HasPrefix(lower, "k8s/") ||
		strings.HasSuffix(lower, ".tf"):
		return domain.FileCategoryInfrastructure, []domain.ImpactZone{domain.ImpactZoneArchitecture}

	case strings.HasSuffix(lower, ".yaml") ||
		strings.HasSuffix(lower, ".yml") ||
		strings.HasSuffix(lower, ".toml") ||
		strings.HasPrefix(base, ".env"):
		zones := []domain.ImpactZone{domain.ImpactZoneConfig}
		return domain.FileCategoryConfig, zones

	default:
		// Code: .go, .proto, .py, etc.
		zones := []domain.ImpactZone{domain.ImpactZoneModule}
		if strings.Contains(lower, "api/") {
			zones = append(zones, domain.ImpactZoneAPI)
		}
		return domain.FileCategoryCode, zones
	}
}

func priorityFor(cat domain.FileCategory) int {
	switch cat {
	case domain.FileCategoryCode:
		return 3
	case domain.FileCategoryConfig:
		return 2
	case domain.FileCategoryInfrastructure:
		return 1
	default:
		return 0
	}
}
