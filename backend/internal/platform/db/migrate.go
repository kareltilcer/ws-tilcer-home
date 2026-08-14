package db

import (
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// Migrate applies all pending migrations from fsys (the merged per-module
// sequence assembled by the registry, PRD §5 D25). It is safe to run on every
// boot: Goose records applied versions, and a database restored from Litestream
// already carries that version table, so nothing re-runs.
//
// WithAllowMissing is required by the version-block-per-module numbering. Every
// module contributes to ONE Goose sequence but owns a numeric block (logging 01,
// platform 02, todo 03, events 04, dashboard 05, notes 06, documents 07). A new
// migration added to an earlier block (e.g. notes 06002) can therefore carry a
// version BELOW one from a later block (documents 07xxx) that production already
// applied. Goose's default ordered mode rejects that unapplied-but-lower version
// as a "missing migration" and refuses to boot; allowing it applies the missing
// one before the new higher one. Cross-module migrations touch disjoint tables,
// so their relative order does not matter (see TestMigrate_AppliesOutOfOrderMigration).
func Migrate(sqldb *sql.DB, fsys fs.FS) error {
	goose.SetBaseFS(fsys)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	// Quieten Goose's default stdout logging; the caller logs boot progress.
	goose.SetLogger(goose.NopLogger())
	if err := goose.Up(sqldb, ".", goose.WithAllowMissing()); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
