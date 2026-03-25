package usecase

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/vukamecos/autodoc/internal/infrastructure/config"
	"github.com/vukamecos/autodoc/internal/domain"
	"github.com/vukamecos/autodoc/internal/infrastructure/markdown"
	"github.com/vukamecos/autodoc/internal/infrastructure/observability"
	"github.com/vukamecos/autodoc/internal/infrastructure/ratelimit"
)

// RunDocUpdateUseCase orchestrates the full documentation update flow.
type RunDocUpdateUseCase struct {
	repo      domain.RepositoryPort
	mrCreator domain.MRCreatorPort
	state     domain.StateStorePort
	docStore  domain.DocumentStorePort
	docWriter domain.DocumentWriterPort
	acp       domain.ACPClientPort
	analyzer  domain.ChangeAnalyzerPort
	mapper    domain.DocumentMapperPort
	validator domain.ValidationPort
	gitCfg    config.GitConfig
	acpCfg    config.ACPConfig
	dryRun    bool
	log       *slog.Logger
	metrics   *observability.Metrics
	limiter   *ratelimit.Limiter
	tracer    trace.Tracer
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
	acpCfg config.ACPConfig,
	dryRun bool,
	log *slog.Logger,
	metrics *observability.Metrics,
	limiter *ratelimit.Limiter,
) *RunDocUpdateUseCase {
	return &RunDocUpdateUseCase{
		repo:      repo,
		mrCreator: mrCreator,
		state:     state,
		docStore:  docStore,
		docWriter: docWriter,
		acp:       acp,
		analyzer:  analyzer,
		mapper:    mapper,
		validator: validator,
		gitCfg:    gitCfg,
		acpCfg:    acpCfg,
		dryRun:    dryRun,
		log:       log,
		metrics:   metrics,
		limiter:   limiter,
		tracer:    otelNoopTracer(),
	}
}

func otelNoopTracer() trace.Tracer {
	return noop.NewTracerProvider().Tracer("autodoc")
}

// SetTracer configures a real OTel tracer for the use case.
func (uc *RunDocUpdateUseCase) SetTracer(t trace.Tracer) {
	uc.tracer = t
}

// Run executes the full documentation update pipeline.
func (uc *RunDocUpdateUseCase) Run(ctx context.Context) error {
	ctx, span := uc.tracer.Start(ctx, "RunDocUpdate")
	defer span.End()

	// Attach a unique run ID to every log line emitted during this execution
	// so all pipeline steps can be correlated in structured log queries.
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	log := uc.log.With(slog.String("run_id", runID))
	span.SetAttributes(attribute.String("run_id", runID))

	// 1. Load state; if not found, initialize with current HEAD and return.
	runState, err := uc.state.LoadState(ctx)
	if errors.Is(err, domain.ErrStateNotFound) {
		log.InfoContext(ctx, "run: no prior state found, initializing")
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
	fetchCtx, fetchSpan := uc.tracer.Start(ctx, "FetchRepo")
	log.InfoContext(ctx, "run: fetching repository")
	if err := uc.repo.Fetch(fetchCtx); err != nil {
		fetchSpan.End()
		return fmt.Errorf("run: fetch repo: %w", err)
	}
	fetchSpan.End()

	// 3. Get HEAD SHA; skip if nothing new.
	headSHA, err := uc.repo.HeadSHA(ctx)
	if err != nil {
		return fmt.Errorf("run: get head sha: %w", err)
	}
	if headSHA == runState.LastProcessedSHA {
		log.InfoContext(ctx, "run: no new commits since last run")
		return nil
	}

	// 4. Check for open bot MRs. Keep the first one so we can update it later
	// instead of opening a duplicate. existingMR is empty when none are open.
	log.InfoContext(ctx, "run: checking for open bot MRs")
	openMRs, err := uc.mrCreator.OpenBotMRs(ctx)
	if err != nil {
		return fmt.Errorf("run: check open bot mrs: %w", err)
	}
	var existingMR domain.MergeRequest
	if len(openMRs) > 0 {
		existingMR = openMRs[0]
		log.InfoContext(ctx, "run: found open bot MR, will update it instead of opening a new one",
			"mr_id", existingMR.ID,
			"mr_branch", existingMR.Branch,
			"mr_url", existingMR.URL,
		)
	}

	// 5. Diff from last processed SHA to HEAD.
	diffCtx, diffSpan := uc.tracer.Start(ctx, "ComputeDiff")
	log.InfoContext(ctx, "run: computing diff", "from", runState.LastProcessedSHA, "to", headSHA)
	start := time.Now()
	diffs, err := uc.repo.Diff(diffCtx, runState.LastProcessedSHA, headSHA)
	if err != nil {
		diffSpan.End()
		return fmt.Errorf("run: diff: %w", err)
	}
	diffSpan.End()
	totalDiffBytes := 0
	for _, d := range diffs {
		totalDiffBytes += len(d.Patch)
	}
	log.InfoContext(ctx, "run: diff computed", "duration_ms", time.Since(start).Milliseconds(), "files", len(diffs), "total_bytes", totalDiffBytes)

	// 6. Analyze changes.
	analyzeCtx, analyzeSpan := uc.tracer.Start(ctx, "AnalyzeChanges")
	log.InfoContext(ctx, "run: analyzing changes", "diff_count", len(diffs))
	start = time.Now()
	changes, err := uc.analyzer.Analyze(analyzeCtx, diffs)
	if err != nil {
		analyzeSpan.End()
		return fmt.Errorf("run: analyze changes: %w", err)
	}
	analyzeSpan.SetAttributes(attribute.Int("change_count", len(changes)))
	analyzeSpan.End()
	log.InfoContext(ctx, "run: changes analyzed", "duration_ms", time.Since(start).Milliseconds(), "changes", len(changes))

	// 7. If no relevant changes, update state and return.
	if len(changes) == 0 {
		log.InfoContext(ctx, "run: no relevant changes, updating state")
		runState.LastProcessedSHA = headSHA
		runState.LastRunAt = time.Now()
		runState.Status = domain.RunStatusSkipped
		if err := uc.state.SaveState(ctx, runState); err != nil {
			return fmt.Errorf("run: save state (no changes): %w", err)
		}
		return nil
	}

	// 8. Map to doc paths.
	log.InfoContext(ctx, "run: mapping changes to documents")
	docPaths, err := uc.mapper.MapToDocs(ctx, changes)
	if err != nil {
		return fmt.Errorf("run: map to docs: %w", err)
	}

	// 8b. Deduplicate by context hash — skip if we already processed this exact
	// set of changes in a previous run (prevents redundant ACP calls and
	// duplicate MR creation when the same commits are re-evaluated).
	ctxHash := computeContextHash(changes, docPaths)
	if ctxHash == runState.ContextHash {
		log.InfoContext(ctx, "run: context hash unchanged, nothing new to process")
		return nil
	}

	// 9. For each doc path: read, call ACP, validate, write.
	var updatedDocs []domain.Document
	for _, docPath := range docPaths {
		current, err := uc.docStore.ReadDocument(ctx, docPath)
		if err != nil {
			return fmt.Errorf("run: read document %q: %w", docPath, err)
		}

		acpCtx, acpSpan := uc.tracer.Start(ctx, "ACPGenerate",
			trace.WithAttributes(attribute.String("doc_path", docPath)))
		log.InfoContext(ctx, "run: calling ACP", "doc", docPath)
		start = time.Now()
		acpResp, err := uc.generateWithChunking(acpCtx, changes, *current)
		if err != nil {
			acpSpan.End()
			return fmt.Errorf("run: acp generate for %q: %w", docPath, err)
		}
		acpSpan.SetAttributes(attribute.Int("response_files", len(acpResp.Files)))
		acpSpan.End()
		log.InfoContext(ctx, "run: ACP call completed", "duration_ms", time.Since(start).Milliseconds(), "files", len(acpResp.Files))

		for _, acpFile := range acpResp.Files {
			if acpFile.Path != docPath {
				continue
			}

			// Section-aware patch: only replace changed sections instead of the
			// whole file. For new files (action == "create") use full content.
			content := acpFile.Content
			if acpFile.Action != "create" && current.Content != "" {
				content = markdown.PatchDocument(current.Content, acpFile.Content)
				log.InfoContext(ctx, "run: applied section-aware patch", slog.String("doc", docPath))
			}

			updated := domain.Document{Path: acpFile.Path, Content: content}

			start = time.Now()
			if err := uc.validator.Validate(ctx, *current, updated); err != nil {
				log.WarnContext(ctx, "run: validation failed, skipping doc", "doc", docPath, "error", err)
				continue
			}
			log.InfoContext(ctx, "run: validation passed", "duration_ms", time.Since(start).Milliseconds(), "doc", docPath)

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
		log.InfoContext(ctx, "run: no meaningful document changes")
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

	// 11. If not dry-run: commit docs and create or update the bot MR.
	_, mrSpan := uc.tracer.Start(ctx, "CommitAndMR")
	var mrID string
	if !uc.dryRun {
		commitMsg := uc.gitCfg.CommitMessageTemplate
		mrTitle := "Docs: synchronize documentation with repository changes"
		mrDesc := buildMRDescription(updatedDocs)

		if existingMR.ID != "" {
			// Re-use the existing MR's branch: commit updated docs on top of it.
			log.InfoContext(ctx, "run: committing to existing bot MR branch", "branch", existingMR.Branch)
			if err := uc.mrCreator.CommitFiles(ctx, existingMR.Branch, updatedDocs, commitMsg); err != nil {
				return fmt.Errorf("run: commit files to existing branch: %w", err)
			}
			// Refresh the MR description to reflect the latest set of changed docs.
			updatedMR := domain.MergeRequest{Title: mrTitle, Description: mrDesc}
			if err := uc.mrCreator.UpdateMR(ctx, existingMR.ID, updatedMR); err != nil {
				// Non-fatal: the commits are already pushed, so log and continue.
				log.WarnContext(ctx, "run: failed to update MR description", "mr_id", existingMR.ID, "error", err)
			}
			mrID = existingMR.ID
			log.InfoContext(ctx, "run: existing bot MR updated", "mr_id", mrID, "mr_url", existingMR.URL)
		} else {
			// No existing MR — create a new branch and open a fresh MR.
			branchName := fmt.Sprintf("%s%d", uc.gitCfg.BranchPrefix, time.Now().Unix())
			log.InfoContext(ctx, "run: creating branch", "branch", branchName)
			if err := uc.mrCreator.CreateBranch(ctx, branchName); err != nil {
				return fmt.Errorf("run: create branch: %w", err)
			}
			if err := uc.mrCreator.CommitFiles(ctx, branchName, updatedDocs, commitMsg); err != nil {
				return fmt.Errorf("run: commit files: %w", err)
			}
			mr := domain.MergeRequest{Title: mrTitle, Description: mrDesc, Branch: branchName}
			createdMR, err := uc.mrCreator.CreateMR(ctx, mr)
			if err != nil {
				return fmt.Errorf("run: create mr: %w", err)
			}
			mrID = createdMR.ID
			log.InfoContext(ctx, "run: merge request created", "mr_id", mrID, "mr_url", createdMR.URL)
			if uc.metrics != nil {
				uc.metrics.MRCreatedTotal.Inc()
			}
		}
	}

	mrSpan.End()

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

