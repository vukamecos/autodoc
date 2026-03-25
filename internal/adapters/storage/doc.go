// Package storage implements [domain.StateStorePort] using SQLite.
//
// Run state is stored in a single-row table (run_state) with an UPSERT
// pattern so the store works correctly from first run through millions of
// pipeline executions without any schema migrations.
//
// The database path is configured via storage.dsn (default: autodoc.db).
package storage
