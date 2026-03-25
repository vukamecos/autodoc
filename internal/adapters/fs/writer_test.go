package fs

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vukamecos/autodoc/internal/domain"
)

var ctx = context.Background()

// newTestWriter creates a Writer with a temporary root directory.
func newTestWriter(t *testing.T, allowedPaths []string) (*Writer, string) {
	t.Helper()
	tmpDir := t.TempDir()
	return New(tmpDir, allowedPaths, slog.Default()), tmpDir
}

// ---------------------------------------------------------------------------
// WriteDocument
// ---------------------------------------------------------------------------

func TestWriteDocument_Success(t *testing.T) {
	w, root := newTestWriter(t, []string{"README.md", "docs/**"})

	doc := domain.Document{
		Path:    "README.md",
		Content: "# Hello World\n",
	}

	if err := w.WriteDocument(ctx, doc); err != nil {
		t.Fatalf("WriteDocument() error: %v", err)
	}

	// Verify file was written
	content, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(content) != doc.Content {
		t.Errorf("expected content %q, got %q", doc.Content, string(content))
	}
}

func TestWriteDocument_CreatesDirectories(t *testing.T) {
	w, root := newTestWriter(t, []string{"docs/**"})

	doc := domain.Document{
		Path:    "docs/guide/installation.md",
		Content: "# Installation\n",
	}

	if err := w.WriteDocument(ctx, doc); err != nil {
		t.Fatalf("WriteDocument() error: %v", err)
	}

	// Verify nested directories were created
	fullPath := filepath.Join(root, "docs", "guide", "installation.md")
	if _, err := os.Stat(fullPath); err != nil {
		t.Errorf("expected file to exist at %s: %v", fullPath, err)
	}
}

func TestWriteDocument_ForbiddenPath(t *testing.T) {
	w, _ := newTestWriter(t, []string{"README.md", "docs/**"})

	tests := []struct {
		name string
		path string
	}{
		{"source code", "main.go"},
		{"config file", ".env"},
		{"outside docs", "src/utils.go"},
		{"parent directory", "../secrets.txt"},
		{"absolute path", "/etc/passwd"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := domain.Document{Path: tc.path, Content: "test"}
			err := w.WriteDocument(ctx, doc)
			if err == nil {
				t.Errorf("expected error for forbidden path %q", tc.path)
			}
			if !errors.Is(err, domain.ErrForbiddenPath) {
				t.Errorf("expected ErrForbiddenPath, got %v", err)
			}
		})
	}
}

func TestWriteDocument_GlobPattern(t *testing.T) {
	w, root := newTestWriter(t, []string{"*.md"})

	doc := domain.Document{
		Path:    "CONTRIBUTING.md",
		Content: "# Contributing\n",
	}

	if err := w.WriteDocument(ctx, doc); err != nil {
		t.Fatalf("WriteDocument() error: %v", err)
	}

	// Verify file was written
	if _, err := os.ReadFile(filepath.Join(root, "CONTRIBUTING.md")); err != nil {
		t.Errorf("expected file to exist: %v", err)
	}
}

func TestWriteDocument_PrefixPattern(t *testing.T) {
	w, root := newTestWriter(t, []string{"docs/**"})

	tests := []struct {
		path string
		desc string
	}{
		{"docs/README.md", "file directly in docs"},
		{"docs/api/reference.md", "nested file in docs"},
		{"docs/deep/nested/path/file.md", "deeply nested file"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			doc := domain.Document{Path: tc.path, Content: "# Test\n"}
			if err := w.WriteDocument(ctx, doc); err != nil {
				t.Errorf("WriteDocument() error for %q: %v", tc.path, err)
			}

			fullPath := filepath.Join(root, tc.path)
			if _, err := os.Stat(fullPath); err != nil {
				t.Errorf("expected file to exist at %s: %v", tc.path, err)
			}
		})
	}
}

func TestWriteDocument_OverwritesExisting(t *testing.T) {
	w, root := newTestWriter(t, []string{"README.md"})

	// Write initial content
	doc1 := domain.Document{Path: "README.md", Content: "# Old\n"}
	if err := w.WriteDocument(ctx, doc1); err != nil {
		t.Fatalf("first WriteDocument() error: %v", err)
	}

	// Write new content
	doc2 := domain.Document{Path: "README.md", Content: "# New\n"}
	if err := w.WriteDocument(ctx, doc2); err != nil {
		t.Fatalf("second WriteDocument() error: %v", err)
	}

	// Verify content was overwritten
	content, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != "# New\n" {
		t.Errorf("expected new content, got %q", string(content))
	}
}

func TestWriteDocument_Atomic(t *testing.T) {
	w, root := newTestWriter(t, []string{"README.md"})

	// Write content
	doc := domain.Document{Path: "README.md", Content: "# Test\n"}
	if err := w.WriteDocument(ctx, doc); err != nil {
		t.Fatalf("WriteDocument() error: %v", err)
	}

	// Verify no temp files remain
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("failed to read directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".autodoc-tmp-") {
			t.Errorf("temp file not cleaned up: %s", entry.Name())
		}
	}
}

// ---------------------------------------------------------------------------
// ReadDocument
// ---------------------------------------------------------------------------

func TestReadDocument_Exists(t *testing.T) {
	w, root := newTestWriter(t, []string{"docs/**"})

	// Create a file manually
	content := "# Existing Doc\nSome content here.\n"
	testPath := filepath.Join(root, "docs", "existing.md")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	if err := os.WriteFile(testPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	doc, err := w.ReadDocument(ctx, "docs/existing.md")
	if err != nil {
		t.Fatalf("ReadDocument() error: %v", err)
	}
	if doc.Path != "docs/existing.md" {
		t.Errorf("expected path 'docs/existing.md', got %q", doc.Path)
	}
	if doc.Content != content {
		t.Errorf("expected content %q, got %q", content, doc.Content)
	}
}

func TestReadDocument_NotFound(t *testing.T) {
	w, _ := newTestWriter(t, []string{"docs/**"})

	// Reading non-existent file should return empty document, not error
	doc, err := w.ReadDocument(ctx, "docs/nonexistent.md")
	if err != nil {
		t.Fatalf("ReadDocument() should not error for missing file: %v", err)
	}
	if doc.Path != "docs/nonexistent.md" {
		t.Errorf("expected path 'docs/nonexistent.md', got %q", doc.Path)
	}
	if doc.Content != "" {
		t.Errorf("expected empty content for missing file, got %q", doc.Content)
	}
}

func TestReadDocument_NestedDirectory(t *testing.T) {
	w, root := newTestWriter(t, []string{"docs/**"})

	// Create nested file
	testPath := filepath.Join(root, "docs", "api", "v1", "endpoints.md")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	if err := os.WriteFile(testPath, []byte("# API\n"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	doc, err := w.ReadDocument(ctx, "docs/api/v1/endpoints.md")
	if err != nil {
		t.Fatalf("ReadDocument() error: %v", err)
	}
	if doc.Content != "# API\n" {
		t.Errorf("unexpected content: %q", doc.Content)
	}
}

// ---------------------------------------------------------------------------
// isAllowed helper
// ---------------------------------------------------------------------------

func TestIsAllowed(t *testing.T) {
	w, _ := newTestWriter(t, []string{
		"README.md",
		"CONTRIBUTING.md",
		"docs/**",
		"*.txt",
	})

	tests := []struct {
		path    string
		allowed bool
	}{
		// Exact matches
		{"README.md", true},
		{"CONTRIBUTING.md", true},

		// Glob patterns
		{"file.txt", true},
		{"notes.txt", true},

		// Prefix patterns (/**)
		{"docs/README.md", true},
		{"docs/api/spec.md", true},
		{"docs/a/b/c/d/e.md", true},

		// Not allowed
		{"main.go", false},
		{"src/main.go", false},
		{".env", false},
		{"docs", false}, // docs itself is not allowed, only docs/**

		// Actually matches *.txt pattern
		{"README.txt", true},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := w.isAllowed(tc.path)
			if got != tc.allowed {
				t.Errorf("isAllowed(%q) = %v, want %v", tc.path, got, tc.allowed)
			}
		})
	}
}

func TestIsAllowed_EmptyAllowedPaths(t *testing.T) {
	w, _ := newTestWriter(t, []string{})

	if w.isAllowed("README.md") {
		t.Error("isAllowed should return false when no paths are allowed")
	}
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()
	allowedPaths := []string{"README.md", "docs/**"}
	log := slog.Default()

	w := New(tmpDir, allowedPaths, log)

	if w.rootDir != tmpDir {
		t.Errorf("expected rootDir %q, got %q", tmpDir, w.rootDir)
	}
	if len(w.allowedPaths) != len(allowedPaths) {
		t.Errorf("expected %d allowed paths, got %d", len(allowedPaths), len(w.allowedPaths))
	}
	if w.log != log {
		t.Error("expected logger to be set")
	}
}

// ---------------------------------------------------------------------------
// Round-trip: Write then Read
// ---------------------------------------------------------------------------

func TestWriteReadRoundTrip(t *testing.T) {
	w, _ := newTestWriter(t, []string{"README.md", "docs/**"})

	original := domain.Document{
		Path:    "docs/guide.md",
		Content: "# Guide\n\nThis is a guide.\n",
	}

	// Write
	if err := w.WriteDocument(ctx, original); err != nil {
		t.Fatalf("WriteDocument() error: %v", err)
	}

	// Read back
	read, err := w.ReadDocument(ctx, original.Path)
	if err != nil {
		t.Fatalf("ReadDocument() error: %v", err)
	}

	if read.Content != original.Content {
		t.Errorf("content mismatch: wrote %q, read %q", original.Content, read.Content)
	}
	if read.Path != original.Path {
		t.Errorf("path mismatch: wrote %q, read %q", original.Path, read.Path)
	}
}
