// Package testsupport provides shared helpers for backend tests: a migrated
// temp-file database, and (later) a fake auth introspector and context helpers.
package testsupport

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/bootstrap"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
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
	migFS, err := bootstrap.MigrationFS()
	if err != nil {
		t.Fatalf("assemble migrations: %v", err)
	}
	if err := appdb.Migrate(sqldb, migFS); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return sqldb
}
