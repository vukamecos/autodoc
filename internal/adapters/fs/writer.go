package fs

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/vukamecos/autodoc/internal/domain"
)

// Writer implements domain.DocumentStorePort and domain.DocumentWriterPort
// using the local filesystem.
type Writer struct {
	rootDir      string
	allowedPaths []string
	log          *slog.Logger
}

// New creates a new Writer rooted at rootDir, restricting writes to allowedPaths.
func New(rootDir string, allowedPaths []string, log *slog.Logger) *Writer {
	return &Writer{
		rootDir:      rootDir,
		allowedPaths: allowedPaths,
		log:          log,
	}
}

// isAllowed reports whether path matches any of the allowed path patterns.
func (w *Writer) isAllowed(path string) bool {
	for _, pattern := range w.allowedPaths {
		matched, err := filepath.Match(pattern, path)
		if err == nil && matched {
			return true
		}
		// Also match prefix for glob-like patterns ending in /**
		trimmed := strings.TrimSuffix(pattern, "/**")
		if trimmed != pattern && strings.HasPrefix(path, trimmed+"/") {
			return true
		}
	}
	return false
}

// WriteDocument writes doc.Content to a file at rootDir/doc.Path atomically.
// Returns domain.ErrForbiddenPath if the path is not within allowed paths.
func (w *Writer) WriteDocument(ctx context.Context, doc domain.Document) error {
	if !w.isAllowed(doc.Path) {
		return fmt.Errorf("fs: write %q: %w", doc.Path, domain.ErrForbiddenPath)
	}

	absPath := filepath.Join(w.rootDir, doc.Path)

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("fs: mkdir for %q: %w", doc.Path, err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(absPath), ".autodoc-tmp-*")
	if err != nil {
		return fmt.Errorf("fs: create temp file for %q: %w", doc.Path, err)
	}
	tmpName := tmpFile.Name()

	if _, err := tmpFile.WriteString(doc.Content); err != nil {
		tmpFile.Close()
		os.Remove(tmpName)
		return fmt.Errorf("fs: write temp file for %q: %w", doc.Path, err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("fs: close temp file for %q: %w", doc.Path, err)
	}

	if err := os.Rename(tmpName, absPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("fs: rename temp file to %q: %w", doc.Path, err)
	}

	w.log.InfoContext(ctx, "fs: wrote document", "path", doc.Path)
	return nil
}

// ReadDocument reads the document at rootDir/path.
// If the file does not exist, an empty Document is returned without error.
func (w *Writer) ReadDocument(ctx context.Context, path string) (*domain.Document, error) {
	absPath := filepath.Join(w.rootDir, path)
	data, err := os.ReadFile(absPath)
	if os.IsNotExist(err) {
		return &domain.Document{Path: path, Content: ""}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fs: read %q: %w", path, err)
	}
	return &domain.Document{Path: path, Content: string(data)}, nil
}
