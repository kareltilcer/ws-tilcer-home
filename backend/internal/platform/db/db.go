// Package db opens the embedded SQLite database, runs migrations, seeds the
// default board, and provides the transaction backbone every mutation flows
// through (WithTx). One DB file, WAL journaling, single writer.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (registers "sqlite"), FTS5 built in
)

// Open opens the SQLite database at path with the service's standard pragmas and
// verifies connectivity. Pragmas are set via the DSN so they apply to every
// pooled connection; the pool is capped at a single connection to keep SQLite's
// single-writer model simple and free of lock contention at household scale.
func Open(path string) (*sql.DB, error) {
	dsn := "file:" + url.PathEscape(path) +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)"

	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	sqldb.SetMaxOpenConns(1)
	if err := sqldb.PingContext(context.Background()); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("ping sqlite %q: %w", path, err)
	}
	return sqldb, nil
}

// Ping checks database connectivity (used by the readiness probe).
func Ping(ctx context.Context, sqldb *sql.DB) error { return sqldb.PingContext(ctx) }

// ProbeFTS5 verifies the driver was built with FTS5 by creating and dropping a
// throwaway virtual table. A failure here should abort startup — the log
// browser's free-text search depends on FTS5, and a missing build surfaces at
// boot rather than at first search.
func ProbeFTS5(ctx context.Context, sqldb *sql.DB) error {
	const probe = "fts5_probe_tmp"
	if _, err := sqldb.ExecContext(ctx,
		fmt.Sprintf("CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(x)", probe)); err != nil {
		return fmt.Errorf("FTS5 not available in this SQLite build: %w", err)
	}
	if _, err := sqldb.ExecContext(ctx, "DROP TABLE "+probe); err != nil {
		return fmt.Errorf("FTS5 probe cleanup: %w", err)
	}
	return nil
}
