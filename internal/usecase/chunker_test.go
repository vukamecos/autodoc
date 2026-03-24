package usecase

import (
	"testing"

	"github.com/vukamecos/autodoc/internal/domain"
)

// ---------------------------------------------------------------------------
// chunkChanges
// ---------------------------------------------------------------------------

func TestChunkChanges_NoSplitNeeded(t *testing.T) {
	changes := makeChanges("aaa", "bbb") // total = 6 bytes
	chunks := chunkChanges(changes, 100)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if len(chunks[0]) != 2 {
		t.Errorf("expected all changes in one chunk, got %d", len(chunks[0]))
	}
}

func TestChunkChanges_SplitAtBudget(t *testing.T) {
	// Each change has 5 bytes; budget is 8 → first chunk fits 1, second fits 1.
	changes := makeChanges("aaaaa", "bbbbb", "ccccc")
	chunks := chunkChanges(changes, 8)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (one per change), got %d", len(chunks))
	}
}

func TestChunkChanges_OversizedSingleChange(t *testing.T) {
	// A single change that exceeds the budget goes into its own chunk.
	changes := makeChanges("verylongpatch")
	chunks := chunkChanges(changes, 4)
	if len(chunks) != 1 {
		t.Fatalf("expected oversized change in its own chunk, got %d chunks", len(chunks))
	}
}

func TestChunkChanges_ZeroBudget(t *testing.T) {
	// Zero/negative budget → single chunk with all changes.
	changes := makeChanges("a", "b")
	chunks := chunkChanges(changes, 0)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for zero budget, got %d", len(chunks))
	}
}

func TestChunkChanges_Empty(t *testing.T) {
	chunks := chunkChanges(nil, 100)
	if len(chunks) != 1 || len(chunks[0]) != 0 {
		t.Errorf("expected single empty chunk for nil input, got %v", chunks)
	}
}

// ---------------------------------------------------------------------------
// mergeResponse
// ---------------------------------------------------------------------------

func TestMergeResponse_NewFilesAppended(t *testing.T) {
	dst := &domain.ACPResponse{
		Files: []domain.ACPFile{{Path: "README.md", Content: "v1"}},
	}
	src := &domain.ACPResponse{
		Files: []domain.ACPFile{{Path: "docs/arch.md", Content: "new"}},
	}
	mergeResponse(dst, src)

	if len(dst.Files) != 2 {
		t.Fatalf("expected 2 files after merge, got %d", len(dst.Files))
	}
}

func TestMergeResponse_ExistingPathOverwritten(t *testing.T) {
	dst := &domain.ACPResponse{
		Files: []domain.ACPFile{{Path: "README.md", Content: "old"}},
	}
	src := &domain.ACPResponse{
		Files: []domain.ACPFile{{Path: "README.md", Content: "new"}},
	}
	mergeResponse(dst, src)

	if len(dst.Files) != 1 {
		t.Fatalf("expected 1 file (no duplicate), got %d", len(dst.Files))
	}
	if dst.Files[0].Content != "new" {
		t.Errorf("expected 'new' content, got %q", dst.Files[0].Content)
	}
}

func TestMergeResponse_SummaryConcat(t *testing.T) {
	dst := &domain.ACPResponse{Summary: "first"}
	src := &domain.ACPResponse{Summary: "second"}
	mergeResponse(dst, src)
	if dst.Summary != "first second" {
		t.Errorf("expected 'first second', got %q", dst.Summary)
	}
}

func TestMergeResponse_SummaryFirstWhenDstEmpty(t *testing.T) {
	dst := &domain.ACPResponse{}
	src := &domain.ACPResponse{Summary: "only"}
	mergeResponse(dst, src)
	if dst.Summary != "only" {
		t.Errorf("expected 'only', got %q", dst.Summary)
	}
}

func TestMergeResponse_NotesAccumulated(t *testing.T) {
	dst := &domain.ACPResponse{Notes: []string{"note1"}}
	src := &domain.ACPResponse{Notes: []string{"note2", "note3"}}
	mergeResponse(dst, src)
	if len(dst.Notes) != 3 {
		t.Errorf("expected 3 notes, got %d: %v", len(dst.Notes), dst.Notes)
	}
}

// ---------------------------------------------------------------------------
// computeContextHash
// ---------------------------------------------------------------------------

func TestComputeContextHash_Deterministic(t *testing.T) {
	changes := []domain.AnalyzedChange{
		{Diff: domain.FileDiff{Path: "foo.go", Status: domain.ChangeStatusModified, Patch: "@@ -1 +1 @@"}},
	}
	docs := []string{"README.md", "docs/arch.md"}

	h1 := computeContextHash(changes, docs)
	h2 := computeContextHash(changes, docs)
	if h1 != h2 {
		t.Error("hash is not deterministic")
	}
}

func TestComputeContextHash_OrderIndependent(t *testing.T) {
	c1 := domain.AnalyzedChange{Diff: domain.FileDiff{Path: "a.go", Patch: "pa"}}
	c2 := domain.AnalyzedChange{Diff: domain.FileDiff{Path: "b.go", Patch: "pb"}}

	h1 := computeContextHash([]domain.AnalyzedChange{c1, c2}, []string{"README.md"})
	h2 := computeContextHash([]domain.AnalyzedChange{c2, c1}, []string{"README.md"})
	if h1 != h2 {
		t.Error("hash should be order-independent (sorted by path)")
	}
}

func TestComputeContextHash_DifferentInputsDifferentHash(t *testing.T) {
	base := []domain.AnalyzedChange{
		{Diff: domain.FileDiff{Path: "foo.go", Patch: "patch1"}},
	}
	other := []domain.AnalyzedChange{
		{Diff: domain.FileDiff{Path: "foo.go", Patch: "patch2"}},
	}
	docs := []string{"README.md"}

	if computeContextHash(base, docs) == computeContextHash(other, docs) {
		t.Error("different patches should produce different hashes")
	}
}

func TestComputeContextHash_DocPathsAffectHash(t *testing.T) {
	changes := []domain.AnalyzedChange{
		{Diff: domain.FileDiff{Path: "foo.go", Patch: "p"}},
	}
	h1 := computeContextHash(changes, []string{"README.md"})
	h2 := computeContextHash(changes, []string{"docs/arch.md"})
	if h1 == h2 {
		t.Error("different doc paths should produce different hashes")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func makeChanges(patches ...string) []domain.AnalyzedChange {
	out := make([]domain.AnalyzedChange, len(patches))
	for i, p := range patches {
		out[i] = domain.AnalyzedChange{
			Diff: domain.FileDiff{Path: "file.go", Patch: p},
		}
	}
	return out
}
