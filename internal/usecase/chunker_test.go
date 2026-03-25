package usecase

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vukamecos/autodoc/internal/config"
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

// ---------------------------------------------------------------------------
// diffBudget tests
// ---------------------------------------------------------------------------

func TestDiffBudget_NormalCase(t *testing.T) {
	uc := &RunDocUpdateUseCase{acpCfg: config.ACPConfig{MaxContextBytes: 500000}}
	
	// Document size 10000, overhead 2048
	// budget = 500000 - 10000 - 2048 = 487952
	budget := uc.diffBudget(10000)
	expected := 500000 - 10000 - 2048
	
	if budget != expected {
		t.Errorf("expected budget %d, got %d", expected, budget)
	}
}

func TestDiffBudget_LargeDocument(t *testing.T) {
	uc := &RunDocUpdateUseCase{acpCfg: config.ACPConfig{MaxContextBytes: 500000}}
	
	// Document takes most of the budget: 500000 - 480000 - 2048 = 17952
	// Since 17952 >= 2048 (overhead), we get the calculated budget
	budget := uc.diffBudget(480000)
	expected := 500000 - 480000 - 2048 // 17952
	
	if budget != expected {
		t.Errorf("expected budget %d for large doc, got %d", expected, budget)
	}
}

func TestDiffBudget_VeryLargeDocument(t *testing.T) {
	uc := &RunDocUpdateUseCase{acpCfg: config.ACPConfig{MaxContextBytes: 500000}}
	
	// Document leaves less than overhead: 500000 - 498000 - 2048 = -48
	// Since -48 < 2048, we get maxContextBytes/4
	budget := uc.diffBudget(498000)
	expected := 500000 / 4 // 125000
	
	if budget != expected {
		t.Errorf("expected minimum budget %d for very large doc, got %d", expected, budget)
	}
}

func TestDiffBudget_EmptyDocument(t *testing.T) {
	uc := &RunDocUpdateUseCase{acpCfg: config.ACPConfig{MaxContextBytes: 500000}}
	
	budget := uc.diffBudget(0)
	expected := 500000 - 0 - 2048
	
	if budget != expected {
		t.Errorf("expected budget %d for empty doc, got %d", expected, budget)
	}
}

// ---------------------------------------------------------------------------
// buildACPRequest tests
// ---------------------------------------------------------------------------

func TestBuildACPRequest_Content(t *testing.T) {
	changes := []domain.AnalyzedChange{
		{Diff: domain.FileDiff{Path: "a.go", Patch: "patch1"}},
		{Diff: domain.FileDiff{Path: "b.go", Patch: "patch2"}},
	}
	doc := domain.Document{Path: "README.md", Content: "# Title\n"}
	
	req := buildACPRequest(changes, doc)
	
	if req.CorrelationID == "" {
		t.Error("expected non-empty correlation ID")
	}
	
	if !strings.Contains(req.Diff, "patch1") {
		t.Error("expected diff to contain patch1")
	}
	if !strings.Contains(req.Diff, "patch2") {
		t.Error("expected diff to contain patch2")
	}
	
	if req.ChangeSummary != "2 files changed" {
		t.Errorf("expected '2 files changed', got %q", req.ChangeSummary)
	}
	
	if len(req.Documents) != 1 || req.Documents[0].Path != "README.md" {
		t.Errorf("expected document README.md, got %v", req.Documents)
	}
}

// ---------------------------------------------------------------------------
// Additional computeContextHash tests
// ---------------------------------------------------------------------------

func TestComputeContextHash_EmptyInputs(t *testing.T) {
	// Empty changes and docs should produce consistent hash
	h1 := computeContextHash(nil, nil)
	h2 := computeContextHash(nil, nil)
	
	if h1 != h2 {
		t.Error("empty inputs should produce deterministic hash")
	}
	
	if h1 == "" {
		t.Error("empty inputs should produce non-empty hash")
	}
}

func TestComputeContextHash_OnlyChanges(t *testing.T) {
	changes := []domain.AnalyzedChange{
		{Diff: domain.FileDiff{Path: "a.go", Patch: "p1"}},
	}
	h := computeContextHash(changes, nil)
	
	if h == "" {
		t.Error("should produce non-empty hash with only changes")
	}
}

func TestComputeContextHash_OnlyDocs(t *testing.T) {
	docs := []string{"README.md"}
	h := computeContextHash(nil, docs)
	
	if h == "" {
		t.Error("should produce non-empty hash with only docs")
	}
}

// ---------------------------------------------------------------------------
// Additional mergeResponse tests
// ---------------------------------------------------------------------------

func TestMergeResponse_EmptyDst(t *testing.T) {
	dst := &domain.ACPResponse{}
	src := &domain.ACPResponse{
		Summary: "source summary",
		Files:   []domain.ACPFile{{Path: "a.md", Content: "a"}},
		Notes:   []string{"note1"},
	}
	
	mergeResponse(dst, src)
	
	if dst.Summary != "source summary" {
		t.Errorf("expected summary 'source summary', got %q", dst.Summary)
	}
	if len(dst.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(dst.Files))
	}
	if len(dst.Notes) != 1 {
		t.Errorf("expected 1 note, got %d", len(dst.Notes))
	}
}

func TestMergeResponse_EmptySrc(t *testing.T) {
	dst := &domain.ACPResponse{
		Summary: "existing",
		Files:   []domain.ACPFile{{Path: "a.md", Content: "a"}},
		Notes:   []string{"note1"},
	}
	src := &domain.ACPResponse{}
	
	mergeResponse(dst, src)
	
	if dst.Summary != "existing" {
		t.Errorf("expected summary unchanged, got %q", dst.Summary)
	}
	if len(dst.Files) != 1 {
		t.Errorf("expected 1 file unchanged, got %d", len(dst.Files))
	}
	if len(dst.Notes) != 1 {
		t.Errorf("expected 1 note unchanged, got %d", len(dst.Notes))
	}
}

func TestMergeResponse_MultipleMerges(t *testing.T) {
	dst := &domain.ACPResponse{}
	
	mergeResponse(dst, &domain.ACPResponse{
		Summary: "first",
		Files:   []domain.ACPFile{{Path: "a.md", Content: "a1"}},
		Notes:   []string{"note1"},
	})
	
	mergeResponse(dst, &domain.ACPResponse{
		Summary: "second",
		Files:   []domain.ACPFile{{Path: "b.md", Content: "b1"}},
		Notes:   []string{"note2"},
	})
	
	mergeResponse(dst, &domain.ACPResponse{
		Summary: "third",
		Files:   []domain.ACPFile{{Path: "a.md", Content: "a2"}}, // overwrites
		Notes:   []string{"note3"},
	})
	
	// Summary should be concatenated
	if dst.Summary != "first second third" {
		t.Errorf("expected 'first second third', got %q", dst.Summary)
	}
	
	// Should have 2 files
	if len(dst.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(dst.Files))
	}
	
	// a.md should have latest content
	for _, f := range dst.Files {
		if f.Path == "a.md" && f.Content != "a2" {
			t.Errorf("expected a.md content 'a2', got %q", f.Content)
		}
	}
	
	// Should have 3 notes
	if len(dst.Notes) != 3 {
		t.Errorf("expected 3 notes, got %d", len(dst.Notes))
	}
}

// ---------------------------------------------------------------------------
// Integration-style tests for the chunker flow
// ---------------------------------------------------------------------------

func TestChunkChanges_VariousSizes(t *testing.T) {
	tests := []struct {
		name          string
		patchSizes    []int
		budget        int
		expectedChunks int
	}{
		{
			name:          "all fit in one chunk",
			patchSizes:    []int{100, 200, 300},
			budget:        1000,
			expectedChunks: 1,
		},
		{
			name:          "splits into two",
			patchSizes:    []int{600, 600},
			budget:        1000,
			expectedChunks: 2,
		},
		{
			name:          "each in own chunk",
			patchSizes:    []int{500, 500, 500},
			budget:        600,
			expectedChunks: 3,
		},
		{
			name:          "empty patches",
			patchSizes:    []int{0, 0, 0},
			budget:        100,
			expectedChunks: 1,
		},
	}
	
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changes := make([]domain.AnalyzedChange, len(tc.patchSizes))
			for i, size := range tc.patchSizes {
				changes[i] = domain.AnalyzedChange{
					Diff: domain.FileDiff{Path: fmt.Sprintf("file%d.go", i), Patch: strings.Repeat("x", size)},
				}
			}
			
			chunks := chunkChanges(changes, tc.budget)
			if len(chunks) != tc.expectedChunks {
				t.Errorf("expected %d chunks, got %d", tc.expectedChunks, len(chunks))
			}
		})
	}
}
