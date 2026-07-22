package db

import (
	"database/sql"
	"fmt"

	"github.com/kareltilcer/ws-tilcer-home/backend/migrations"
	"github.com/pressly/goose/v3"
)

// Migrate applies all pending migrations from the embedded FS. It is safe to run
// on every boot: Goose records applied versions, and a database restored from
// Litestream already carries that version table, so nothing re-runs.
func Migrate(sqldb *sql.DB) error {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	// Quieten Goose's default stdout logging; the caller logs boot progress.
	goose.SetLogger(goose.NopLogger())
	if err := goose.Up(sqldb, "."); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
