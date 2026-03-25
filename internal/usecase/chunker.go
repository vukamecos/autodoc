package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vukamecos/autodoc/internal/domain"
)

// generateWithChunking calls ACP for a single document, splitting the diff
// into multiple requests if the total size exceeds maxContextBytes.
//
// Each chunk is processed sequentially: the document returned by chunk N is
// passed as the current document to chunk N+1, so every chunk sees an
// up-to-date version of the file.
func (uc *RunDocUpdateUseCase) generateWithChunking(
	ctx context.Context,
	changes []domain.AnalyzedChange,
	current domain.Document,
) (*domain.ACPResponse, error) {
	budget := uc.diffBudget(len(current.Content))
	chunks := chunkChanges(changes, budget)
	totalChanges := len(changes)
	totalBytes := 0
	for _, c := range changes {
		totalBytes += len(c.Diff.Patch)
	}

	// Auto-select model based on diff size for all local/hosted LLM providers.
	// SelectModel returns the configured model if one is set, or picks the
	// best model from the provider-specific size table when acp.model is empty.
	// The remote "acp" provider manages its own model selection internally.
	selectedModel := ""
	if uc.acpCfg.Provider != "acp" && uc.acpCfg.Provider != "" {
		selector := NewModelSelector(uc.acpCfg)
		rec := selector.SelectModel(totalBytes)
		selectedModel = rec.Model
		uc.log.InfoContext(ctx, "chunker: model selected",
			"model", rec.Model,
			"reason", rec.Reason,
			"confidence", rec.Confidence,
		)
	}

	if len(chunks) == 1 {
		uc.log.InfoContext(ctx, "chunker: single chunk", "doc", current.Path, "budget", budget, "changes", totalChanges, "bytes", totalBytes)
		req := buildACPRequest(chunks[0], current)
		req.Model = selectedModel
		return uc.acp.Generate(ctx, req)
	}

	uc.log.InfoContext(ctx, "chunker: diff exceeds context limit, splitting",
		"chunks", len(chunks),
		"doc", current.Path,
		"budget", budget,
		"total_changes", totalChanges,
		"total_bytes", totalBytes,
	)

	if uc.metrics != nil {
		uc.metrics.ChunkedRequestsTotal.Inc()
	}

	// Accumulate the final merged response across all chunks.
	merged := &domain.ACPResponse{}

	for i, chunk := range chunks {
		req := buildACPRequest(chunk, current)
		req.Model = selectedModel
		req.Instructions = fmt.Sprintf(
			"Update the documentation based on the provided code diff (part %d of %d). "+
				"Preserve the existing style and structure. Only output the updated document.",
			i+1, len(chunks),
		)
		req.CorrelationID = fmt.Sprintf("autodoc-%d-chunk%d", time.Now().UnixNano(), i+1)

		chunkBytes := 0
		for _, c := range chunk {
			chunkBytes += len(c.Diff.Patch)
		}
		uc.log.InfoContext(ctx, "chunker: processing chunk", "chunk", i+1, "total_chunks", len(chunks), "changes", len(chunk), "bytes", chunkBytes)

		resp, err := uc.acp.Generate(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("chunker: chunk %d/%d: %w", i+1, len(chunks), err)
		}

		// Feed the updated document content forward to the next chunk.
		for _, f := range resp.Files {
			if f.Path == current.Path {
				current = domain.Document{Path: f.Path, Content: f.Content}
				break
			}
		}

		mergeResponse(merged, resp)
	}

	return merged, nil
}

// diffBudget returns how many bytes of diff content can fit in a single
// ACP request after reserving space for the current document and a fixed
// overhead for instructions/metadata.
func (uc *RunDocUpdateUseCase) diffBudget(docSize int) int {
	const overhead = 2048
	maxContext := uc.acpCfg.MaxContextBytes
	budget := maxContext - docSize - overhead
	if budget < overhead {
		// Document alone is close to the limit; give at least some budget.
		budget = maxContext / 4
	}
	return budget
}

// chunkChanges splits changes into groups where the total patch byte size of
// each group does not exceed diffBudget. A single change that exceeds the
// budget by itself is placed in its own chunk (it cannot be split further).
func chunkChanges(changes []domain.AnalyzedChange, diffBudget int) [][]domain.AnalyzedChange {
	if diffBudget <= 0 || totalPatchSize(changes) <= diffBudget {
		return [][]domain.AnalyzedChange{changes}
	}

	var chunks [][]domain.AnalyzedChange
	var current []domain.AnalyzedChange
	currentSize := 0

	for _, c := range changes {
		sz := len(c.Diff.Patch)
		if len(current) > 0 && currentSize+sz > diffBudget {
			chunks = append(chunks, current)
			current = nil
			currentSize = 0
		}
		current = append(current, c)
		currentSize += sz
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

// mergeResponse folds src into dst: new file entries are appended; existing
// paths are overwritten with the latest content (last chunk wins).
func mergeResponse(dst, src *domain.ACPResponse) {
	if src.Summary != "" {
		if dst.Summary == "" {
			dst.Summary = src.Summary
		} else {
			dst.Summary = dst.Summary + " " + src.Summary
		}
	}
	dst.Notes = append(dst.Notes, src.Notes...)

	index := make(map[string]int, len(dst.Files))
	for i, f := range dst.Files {
		index[f.Path] = i
	}
	for _, f := range src.Files {
		if i, ok := index[f.Path]; ok {
			dst.Files[i] = f // overwrite with latest version
		} else {
			index[f.Path] = len(dst.Files)
			dst.Files = append(dst.Files, f)
		}
	}
}

func totalPatchSize(changes []domain.AnalyzedChange) int {
	var n int
	for _, c := range changes {
		n += len(c.Diff.Patch)
	}
	return n
}

// buildACPRequest constructs an ACPRequest for a given document and change set.
func buildACPRequest(changes []domain.AnalyzedChange, current domain.Document) domain.ACPRequest {
	var sb strings.Builder
	for _, c := range changes {
		sb.WriteString(c.Diff.Patch)
		sb.WriteByte('\n')
	}
	return domain.ACPRequest{
		CorrelationID: fmt.Sprintf("autodoc-%d", time.Now().UnixNano()),
		Instructions:  "Update the documentation based on the provided code diff. Preserve existing style and structure.",
		ChangeSummary: fmt.Sprintf("%d files changed", len(changes)),
		Diff:          sb.String(),
		Documents:     []domain.Document{current},
	}
}
