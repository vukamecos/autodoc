package usecase

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
	"github.com/vukamecos/autodoc/internal/markdown"
	"github.com/vukamecos/autodoc/internal/observability"
)

// RunDocUpdateUseCase orchestrates the full documentation update flow.
type RunDocUpdateUseCase struct {
	repo            domain.RepositoryPort
	mrCreator       domain.MRCreatorPort
	state           domain.StateStorePort
	docStore        domain.DocumentStorePort
	docWriter       domain.DocumentWriterPort
	acp             domain.ACPClientPort
	analyzer        domain.ChangeAnalyzerPort
	mapper          domain.DocumentMapperPort
	validator       domain.ValidationPort
	gitCfg          config.GitConfig
	maxContextBytes int
	dryRun          bool
	log             *slog.Logger
	metrics         *observability.Metrics
}

// New constructs a RunDocUpdateUseCase with all required dependencies.
func New(
	repo domain.RepositoryPort,
	mrCreator domain.MRCreatorPort,
	state domain.StateStorePort,
	docStore domain.DocumentStorePort,
	docWriter domain.DocumentWriterPort,
	acp domain.ACPClientPort,
	analyzer domain.ChangeAnalyzerPort,
	mapper domain.DocumentMapperPort,
	validator domain.ValidationPort,
	gitCfg config.GitConfig,
	maxContextBytes int,
	dryRun bool,
	log *slog.Logger,
	metrics *observability.Metrics,
) *RunDocUpdateUseCase {
	return &RunDocUpdateUseCase{
		repo:            repo,
		mrCreator:       mrCreator,
		state:           state,
		docStore:        docStore,
		docWriter:       docWriter,
		acp:             acp,
		analyzer:        analyzer,
		mapper:          mapper,
		validator:       validator,
		gitCfg:          gitCfg,
		maxContextBytes: maxContextBytes,
		dryRun:          dryRun,
		log:             log,
		metrics:         metrics,
	}
}

// Run executes the full documentation update pipeline.
func (uc *RunDocUpdateUseCase) Run(ctx context.Context) error {
	// 1. Load state; if not found, initialize with current HEAD and return.
	runState, err := uc.state.LoadState(ctx)
	if errors.Is(err, domain.ErrStateNotFound) {
		uc.log.InfoContext(ctx, "run: no prior state found, initializing")
		headSHA, headErr := uc.repo.HeadSHA(ctx)
		if headErr != nil {
			return fmt.Errorf("run: get head sha for init: %w", headErr)
		}
		initState := &domain.RunState{
			LastProcessedSHA: headSHA,
			LastRunAt:        time.Now(),
			Status:           domain.RunStatusSkipped,
			OpenMRIDs:        []string{},
		}
		if saveErr := uc.state.SaveState(ctx, initState); saveErr != nil {
			return fmt.Errorf("run: save init state: %w", saveErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("run: load state: %w", err)
	}

	// 2. Fetch repo.
	uc.log.InfoContext(ctx, "run: fetching repository")
	if err := uc.repo.Fetch(ctx); err != nil {
		return fmt.Errorf("run: fetch repo: %w", err)
	}

	// 3. Get HEAD SHA; skip if nothing new.
	headSHA, err := uc.repo.HeadSHA(ctx)
	if err != nil {
		return fmt.Errorf("run: get head sha: %w", err)
	}
	if headSHA == runState.LastProcessedSHA {
		uc.log.InfoContext(ctx, "run: no new commits since last run")
		return nil
	}

	// 4. Check for open bot MRs.
	uc.log.InfoContext(ctx, "run: checking for open bot MRs")
	openMRs, err := uc.mrCreator.OpenBotMRs(ctx)
	if err != nil {
		return fmt.Errorf("run: check open bot mrs: %w", err)
	}
	if len(openMRs) > 0 {
		uc.log.WarnContext(ctx, "run: open bot MR already exists, skipping", "count", len(openMRs))
		return domain.ErrOpenMRExists
	}

	// 5. Diff from last processed SHA to HEAD.
	uc.log.InfoContext(ctx, "run: computing diff", "from", runState.LastProcessedSHA, "to", headSHA)
	start := time.Now()
	diffs, err := uc.repo.Diff(ctx, runState.LastProcessedSHA, headSHA)
	if err != nil {
		return fmt.Errorf("run: diff: %w", err)
	}
	uc.log.InfoContext(ctx, "run: diff computed", "duration_ms", time.Since(start).Milliseconds(), "files", len(diffs))

	// 6. Analyze changes.
	uc.log.InfoContext(ctx, "run: analyzing changes", "diff_count", len(diffs))
	start = time.Now()
	changes, err := uc.analyzer.Analyze(ctx, diffs)
	if err != nil {
		return fmt.Errorf("run: analyze changes: %w", err)
	}
	uc.log.InfoContext(ctx, "run: changes analyzed", "duration_ms", time.Since(start).Milliseconds(), "changes", len(changes))

	// 7. If no relevant changes, update state and return.
	if len(changes) == 0 {
		uc.log.InfoContext(ctx, "run: no relevant changes, updating state")
		runState.LastProcessedSHA = headSHA
		runState.LastRunAt = time.Now()
		runState.Status = domain.RunStatusSkipped
		if err := uc.state.SaveState(ctx, runState); err != nil {
			return fmt.Errorf("run: save state (no changes): %w", err)
		}
		return nil
	}

	// 8. Map to doc paths.
	uc.log.InfoContext(ctx, "run: mapping changes to documents")
	docPaths, err := uc.mapper.MapToDocs(ctx, changes)
	if err != nil {
		return fmt.Errorf("run: map to docs: %w", err)
	}

	// 8b. Deduplicate by context hash — skip if we already processed this exact
	// set of changes in a previous run (prevents redundant ACP calls and
	// duplicate MR creation when the same commits are re-evaluated).
	ctxHash := computeContextHash(changes, docPaths)
	if ctxHash == runState.ContextHash {
		uc.log.InfoContext(ctx, "run: context hash unchanged, nothing new to process")
		return nil
	}

	// 9. For each doc path: read, call ACP, validate, write.
	var updatedDocs []domain.Document
	for _, docPath := range docPaths {
		current, err := uc.docStore.ReadDocument(ctx, docPath)
		if err != nil {
			return fmt.Errorf("run: read document %q: %w", docPath, err)
		}

		uc.log.InfoContext(ctx, "run: calling ACP", "doc", docPath)
		start = time.Now()
		acpResp, err := uc.generateWithChunking(ctx, changes, *current)
		if err != nil {
			return fmt.Errorf("run: acp generate for %q: %w", docPath, err)
		}
		uc.log.InfoContext(ctx, "run: ACP call completed", "duration_ms", time.Since(start).Milliseconds(), "files", len(acpResp.Files))

		for _, acpFile := range acpResp.Files {
			if acpFile.Path != docPath {
				continue
			}

			// Section-aware patch: only replace changed sections instead of the
			// whole file. For new files (action == "create") use full content.
			content := acpFile.Content
			if acpFile.Action != "create" && current.Content != "" {
				content = markdown.PatchDocument(current.Content, acpFile.Content)
				uc.log.InfoContext(ctx, "run: applied section-aware patch", slog.String("doc", docPath))
			}

			updated := domain.Document{Path: acpFile.Path, Content: content}

			start = time.Now()
			if err := uc.validator.Validate(ctx, *current, updated); err != nil {
				uc.log.WarnContext(ctx, "run: validation failed, skipping doc", "doc", docPath, "error", err)
				continue
			}
			uc.log.InfoContext(ctx, "run: validation passed", "duration_ms", time.Since(start).Milliseconds(), "doc", docPath)

			if !uc.dryRun {
				if err := uc.docWriter.WriteDocument(ctx, updated); err != nil {
					return fmt.Errorf("run: write document %q: %w", docPath, err)
				}
			}
			updatedDocs = append(updatedDocs, updated)
		}
	}

	// 10. If no docs were written, nothing meaningful changed.
	if len(updatedDocs) == 0 {
		uc.log.InfoContext(ctx, "run: no meaningful document changes")
		runState.LastProcessedSHA = headSHA
		runState.LastRunAt = time.Now()
		runState.Status = domain.RunStatusSkipped
		_ = uc.state.SaveState(ctx, runState)
		return nil
	}

	if uc.metrics != nil {
		for range updatedDocs {
			uc.metrics.DocsUpdatedTotal.Inc()
		}
	}

	// 11. If not dry-run: create branch, commit, create MR.
	var mrID string
	if !uc.dryRun {
		branchName := fmt.Sprintf("%s%d", uc.gitCfg.BranchPrefix, time.Now().Unix())
		uc.log.InfoContext(ctx, "run: creating branch", "branch", branchName)

		if err := uc.mrCreator.CreateBranch(ctx, branchName); err != nil {
			return fmt.Errorf("run: create branch: %w", err)
		}

		commitMsg := uc.gitCfg.CommitMessageTemplate
		if err := uc.mrCreator.CommitFiles(ctx, branchName, updatedDocs, commitMsg); err != nil {
			return fmt.Errorf("run: commit files: %w", err)
		}

		mr := domain.MergeRequest{
			Title:       "Docs: synchronize documentation with repository changes",
			Description: buildMRDescription(updatedDocs),
			Branch:      branchName,
		}
		mrID, err = uc.mrCreator.CreateMR(ctx, mr)
		if err != nil {
			return fmt.Errorf("run: create mr: %w", err)
		}
		uc.log.InfoContext(ctx, "run: merge request created", "mr_id", mrID)

		if uc.metrics != nil {
			uc.metrics.MRCreatedTotal.Inc()
		}
	}

	// 12. Update state.
	runState.LastProcessedSHA = headSHA
	runState.LastRunAt = time.Now()
	runState.Status = domain.RunStatusSuccess
	runState.ContextHash = ctxHash
	if mrID != "" {
		runState.OpenMRIDs = append(runState.OpenMRIDs, mrID)
	}
	if err := uc.state.SaveState(ctx, runState); err != nil {
		return fmt.Errorf("run: save state: %w", err)
	}

	if uc.metrics != nil {
		uc.metrics.RunsTotal.WithLabelValues("success").Inc()
	}

	return nil
}

// computeContextHash returns a stable SHA-256 hex digest of the input context
// (the set of analyzed changes and target document paths). It is used to detect
// duplicate runs that would produce identical documentation updates.
func computeContextHash(changes []domain.AnalyzedChange, docPaths []string) string {
	h := sha256.New()

	// Sort changes by file path for a deterministic ordering.
	sorted := make([]domain.AnalyzedChange, len(changes))
	copy(sorted, changes)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Diff.Path < sorted[j].Diff.Path
	})
	for _, c := range sorted {
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00", c.Diff.Path, string(c.Diff.Status), c.Diff.Patch)
	}

	// Sort doc paths for a deterministic ordering.
	sortedDocs := make([]string, len(docPaths))
	copy(sortedDocs, docPaths)
	sort.Strings(sortedDocs)
	for _, d := range sortedDocs {
		_, _ = fmt.Fprintf(h, "%s\x00", d)
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

// buildMRDescription creates a human-readable MR description.
func buildMRDescription(docs []domain.Document) string {
	var sb strings.Builder
	sb.WriteString("This MR was automatically created by autodoc.\n\n")
	sb.WriteString("**Updated documents:**\n")
	for _, d := range docs {
		fmt.Fprintf(&sb, "- `%s`\n", d.Path)
	}
	sb.WriteString("\nPlease review the changes before merging.")
	return sb.String()
}

// ---------------------------------------------------------------------------
// ChangeAnalyzer — path-based file classifier
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// DocumentMapper — config-rule-based document mapper
// ---------------------------------------------------------------------------

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
