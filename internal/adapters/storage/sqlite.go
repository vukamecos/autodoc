package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/vukamecos/autodoc/internal/infrastructure/config"
	"github.com/vukamecos/autodoc/internal/domain"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS run_state (
    id INTEGER PRIMARY KEY,
    last_processed_sha TEXT NOT NULL DEFAULT '',
    last_run_at DATETIME NOT NULL,
    status TEXT NOT NULL DEFAULT 'success',
    open_mr_ids TEXT NOT NULL DEFAULT '[]',
    context_hash TEXT NOT NULL DEFAULT ''
);`

// Store implements domain.StateStorePort using SQLite.
type Store struct {
	db  *sql.DB
	log *slog.Logger
}

// New opens the SQLite database and ensures the schema exists.
func New(cfg config.StorageConfig, log *slog.Logger) (*Store, error) {
	db, err := sql.Open("sqlite", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("storage: open db: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("storage: create schema: %w", err)
	}

	return &Store{db: db, log: log}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// LoadState loads the single persisted RunState row (id=1).
// Returns domain.ErrStateNotFound if no row exists yet.
func (s *Store) LoadState(ctx context.Context) (*domain.RunState, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT last_processed_sha, last_run_at, status, open_mr_ids, context_hash
		 FROM run_state WHERE id = 1`)

	var (
		sha           string
		lastRunAt     time.Time
		status        string
		openMRIDsJSON string
		contextHash   string
	)

	err := row.Scan(&sha, &lastRunAt, &status, &openMRIDsJSON, &contextHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: load state: %w", err)
	}

	var openMRIDs []string
	if err := json.Unmarshal([]byte(openMRIDsJSON), &openMRIDs); err != nil {
		return nil, fmt.Errorf("storage: unmarshal open_mr_ids: %w", err)
	}

	return &domain.RunState{
		LastProcessedSHA: sha,
		LastRunAt:        lastRunAt,
		Status:           domain.RunStatus(status),
		OpenMRIDs:        openMRIDs,
		ContextHash:      contextHash,
	}, nil
}

// SaveState persists the RunState as the single row (id=1).
func (s *Store) SaveState(ctx context.Context, state *domain.RunState) error {
	openMRIDsJSON, err := json.Marshal(state.OpenMRIDs)
	if err != nil {
		return fmt.Errorf("storage: marshal open_mr_ids: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO run_state
		 (id, last_processed_sha, last_run_at, status, open_mr_ids, context_hash)
		 VALUES (1, ?, ?, ?, ?, ?)`,
		state.LastProcessedSHA,
		state.LastRunAt,
		string(state.Status),
		string(openMRIDsJSON),
		state.ContextHash,
	)
	if err != nil {
		return fmt.Errorf("storage: save state: %w", err)
	}
	return nil
}
