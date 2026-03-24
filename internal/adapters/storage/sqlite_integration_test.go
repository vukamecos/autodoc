package storage

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
)

var ctx = context.Background()

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestStore(t *testing.T) *Store {
	t.Helper()
	f, err := os.CreateTemp("", "autodoc-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	_ = f.Close()
	t.Cleanup(func() { _ = os.Remove(name) })

	store, err := New(config.StorageConfig{DSN: name}, slog.Default())
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// ---------------------------------------------------------------------------
// LoadState — no prior row
// ---------------------------------------------------------------------------

func TestLoadState_NotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.LoadState(ctx)
	if !errors.Is(err, domain.ErrStateNotFound) {
		t.Fatalf("expected ErrStateNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// SaveState + LoadState roundtrip
// ---------------------------------------------------------------------------

func TestSaveAndLoadState_Roundtrip(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	original := &domain.RunState{
		LastProcessedSHA: "deadbeefcafe",
		LastRunAt:        now,
		Status:           domain.RunStatusSuccess,
		OpenMRIDs:        []string{"101", "202"},
		ContextHash:      "abcdef1234567890",
	}

	if err := s.SaveState(ctx, original); err != nil {
		t.Fatalf("SaveState() error: %v", err)
	}

	loaded, err := s.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}

	if loaded.LastProcessedSHA != original.LastProcessedSHA {
		t.Errorf("LastProcessedSHA: got %q, want %q", loaded.LastProcessedSHA, original.LastProcessedSHA)
	}
	if !loaded.LastRunAt.Equal(original.LastRunAt) {
		t.Errorf("LastRunAt: got %v, want %v", loaded.LastRunAt, original.LastRunAt)
	}
	if loaded.Status != original.Status {
		t.Errorf("Status: got %q, want %q", loaded.Status, original.Status)
	}
	if loaded.ContextHash != original.ContextHash {
		t.Errorf("ContextHash: got %q, want %q", loaded.ContextHash, original.ContextHash)
	}
	if len(loaded.OpenMRIDs) != 2 || loaded.OpenMRIDs[0] != "101" || loaded.OpenMRIDs[1] != "202" {
		t.Errorf("OpenMRIDs: got %v, want [101 202]", loaded.OpenMRIDs)
	}
}

// ---------------------------------------------------------------------------
// SaveState twice — idempotent (INSERT OR REPLACE)
// ---------------------------------------------------------------------------

func TestSaveState_Idempotent(t *testing.T) {
	s := newTestStore(t)

	first := &domain.RunState{
		LastProcessedSHA: "sha-first",
		LastRunAt:        time.Now().UTC(),
		Status:           domain.RunStatusSkipped,
		OpenMRIDs:        []string{},
		ContextHash:      "hash1",
	}
	if err := s.SaveState(ctx, first); err != nil {
		t.Fatalf("first SaveState() error: %v", err)
	}

	second := &domain.RunState{
		LastProcessedSHA: "sha-second",
		LastRunAt:        time.Now().UTC(),
		Status:           domain.RunStatusSuccess,
		OpenMRIDs:        []string{"55"},
		ContextHash:      "hash2",
	}
	if err := s.SaveState(ctx, second); err != nil {
		t.Fatalf("second SaveState() error: %v", err)
	}

	loaded, err := s.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}

	if loaded.LastProcessedSHA != "sha-second" {
		t.Errorf("expected second SHA to overwrite first, got %q", loaded.LastProcessedSHA)
	}
	if loaded.Status != domain.RunStatusSuccess {
		t.Errorf("expected Success status, got %q", loaded.Status)
	}
}

// ---------------------------------------------------------------------------
// OpenMRIDs serialization edge cases
// ---------------------------------------------------------------------------

func TestSaveState_EmptyOpenMRIDs(t *testing.T) {
	s := newTestStore(t)

	st := &domain.RunState{
		LastProcessedSHA: "sha",
		LastRunAt:        time.Now().UTC(),
		Status:           domain.RunStatusSkipped,
		OpenMRIDs:        []string{},
		ContextHash:      "",
	}
	if err := s.SaveState(ctx, st); err != nil {
		t.Fatalf("SaveState() error: %v", err)
	}

	loaded, err := s.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}
	if loaded.OpenMRIDs == nil || len(loaded.OpenMRIDs) != 0 {
		t.Errorf("expected empty OpenMRIDs slice, got %v", loaded.OpenMRIDs)
	}
}

func TestSaveState_ManyOpenMRIDs(t *testing.T) {
	s := newTestStore(t)

	ids := []string{"1", "2", "3", "4", "5"}
	st := &domain.RunState{
		LastProcessedSHA: "sha",
		LastRunAt:        time.Now().UTC(),
		Status:           domain.RunStatusSuccess,
		OpenMRIDs:        ids,
	}
	if err := s.SaveState(ctx, st); err != nil {
		t.Fatalf("SaveState() error: %v", err)
	}

	loaded, _ := s.LoadState(ctx)
	if len(loaded.OpenMRIDs) != len(ids) {
		t.Fatalf("expected %d MR IDs, got %d", len(ids), len(loaded.OpenMRIDs))
	}
	for i, id := range ids {
		if loaded.OpenMRIDs[i] != id {
			t.Errorf("OpenMRIDs[%d]: got %q, want %q", i, loaded.OpenMRIDs[i], id)
		}
	}
}

// ---------------------------------------------------------------------------
// ContextHash preserved across runs
// ---------------------------------------------------------------------------

func TestContextHash_Roundtrip(t *testing.T) {
	s := newTestStore(t)

	const hash = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	st := &domain.RunState{
		LastProcessedSHA: "sha",
		LastRunAt:        time.Now().UTC(),
		Status:           domain.RunStatusSuccess,
		OpenMRIDs:        []string{},
		ContextHash:      hash,
	}
	_ = s.SaveState(ctx, st)

	loaded, err := s.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}
	if loaded.ContextHash != hash {
		t.Errorf("ContextHash: got %q, want %q", loaded.ContextHash, hash)
	}
}

// ---------------------------------------------------------------------------
// All RunStatus values persist correctly
// ---------------------------------------------------------------------------

func TestRunStatus_AllValues(t *testing.T) {
	s := newTestStore(t)

	statuses := []domain.RunStatus{
		domain.RunStatusSuccess,
		domain.RunStatusFailed,
		domain.RunStatusRetryable,
		domain.RunStatusSkipped,
	}

	for _, status := range statuses {
		st := &domain.RunState{
			LastProcessedSHA: "sha",
			LastRunAt:        time.Now().UTC(),
			Status:           status,
			OpenMRIDs:        []string{},
		}
		if err := s.SaveState(ctx, st); err != nil {
			t.Fatalf("SaveState(%q) error: %v", status, err)
		}
		loaded, err := s.LoadState(ctx)
		if err != nil {
			t.Fatalf("LoadState() error: %v", err)
		}
		if loaded.Status != status {
			t.Errorf("status roundtrip: got %q, want %q", loaded.Status, status)
		}
	}
}

// ---------------------------------------------------------------------------
// Schema idempotency — New() on an existing database
// ---------------------------------------------------------------------------

func TestNew_IdempotentSchema(t *testing.T) {
	f, err := os.CreateTemp("", "autodoc-schema-*.db")
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	_ = f.Close()
	t.Cleanup(func() { _ = os.Remove(name) })

	cfg := config.StorageConfig{DSN: name}

	// Open and close twice — schema CREATE IF NOT EXISTS must not error.
	s1, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatalf("first New() error: %v", err)
	}
	_ = s1.Close()

	s2, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatalf("second New() on existing DB error: %v", err)
	}
	_ = s2.Close()
}
