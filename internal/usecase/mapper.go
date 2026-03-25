package usecase

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
)

// DocumentMapper implements domain.DocumentMapperPort using MappingConfig rules.
type DocumentMapper struct {
	cfg config.MappingConfig
}

// NewDocumentMapper creates a DocumentMapper.
func NewDocumentMapper(cfg config.MappingConfig) *DocumentMapper {
	return &DocumentMapper{cfg: cfg}
}

// MapToDocs returns the unique set of document paths that should be updated
// given the provided AnalyzedChanges.
func (m *DocumentMapper) MapToDocs(_ context.Context, changes []domain.AnalyzedChange) ([]string, error) {
	seen := map[string]struct{}{}
	var result []string

	for _, change := range changes {
		matched := false
		for _, rule := range m.cfg.Rules {
			if ruleMatches(rule, change.Diff.Path) {
				for _, target := range rule.Update {
					if _, ok := seen[target]; !ok {
						seen[target] = struct{}{}
						result = append(result, target)
					}
				}
				matched = true
			}
		}
		if !matched {
			if _, ok := seen["README.md"]; !ok {
				seen["README.md"] = struct{}{}
				result = append(result, "README.md")
			}
		}
	}

	return result, nil
}

// ruleMatches returns true if path matches any of the rule's match patterns.
func ruleMatches(rule config.MappingRule, path string) bool {
	for _, pattern := range rule.Match.Paths {
		// Support trailing /** glob
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
