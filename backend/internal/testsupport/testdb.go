// Package testsupport provides shared helpers for backend tests: a migrated
// temp-file database, and (later) a fake auth introspector and context helpers.
package testsupport

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/db"
)

// NewDB returns a freshly-migrated database backed by a temp file (not
// :memory:, whose per-connection semantics break FTS triggers and multi-
// statement transactions under database/sql pooling). The file is removed with
// the test's temp dir.
func NewDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "home_test.db")
	sqldb, err := appdb.Open(path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	if err := appdb.Migrate(sqldb); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return sqldb
}

// NewSeededDB returns a migrated temp-file database with the default board seeded.
func NewSeededDB(t *testing.T) *sql.DB {
	t.Helper()
	sqldb := NewDB(t)
	if _, err := appdb.SeedIfEmpty(context.Background(), sqldb); err != nil {
		t.Fatalf("seed test db: %v", err)
	}
	return sqldb
}
