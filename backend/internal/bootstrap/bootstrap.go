// Package bootstrap composes the compile-time modular monolith. It is the single
// place that knows the full set of feature modules and assembles the one Goose
// migration sequence from their per-module files (PRD §5 D25, HANDOFF §3). Both
// the server entrypoint (cmd/home) and the test harness (platform/testsupport)
// go through here, so they always agree on the module set and the schema.
//
// bootstrap sits above the modules (it imports them); modules never import
// bootstrap, so there is no cycle. It stays out of platform/ precisely because
// platform must not depend on modules.
package bootstrap

import (
	"io/fs"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/admin"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/dashboard"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/documents"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/finance"
	financeseed "github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/finance/seed"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/logging"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/notes"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/todo"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

// MigrationSources returns every SCHEMA migration contributor. Goose applies
// migrations globally by their numeric filename prefix, so the effective order is
// logging(01) → platform(02) → todo(03) → events(04) → dashboard(05) → notes(06)
// → documents(07) → admin(08) → finance(09), regardless of the slice order below
// (PRD §5 D25). The logging (audit) tables must exist before any feature table
// because every module writes through the audit spine; its 01xxx prefix
// guarantees that. The slice is listed in prefix order purely so it reads the way
// the migrations actually run.
//
// v5 adds two contributors' worth of tables: the per-user push tables and the
// outbox cursor go in the PLATFORM block (any module may send, so a member's
// consent cannot depend on the admin module), while the rule/schedule/delivery
// tables are the admin module's own and run last.
//
// THIS IS THE SCHEMA, and it is what tests migrate with (testsupport.NewDB). The
// v6 finance module ships a second, production-only source carrying `fin`'s
// historic months — see MigrationSourcesWithSeed.
func MigrationSources() []registry.MigrationSource {
	return []registry.MigrationSource{
		{Name: "logging", FS: logging.MigrationsFS},
		{Name: "platform", FS: appdb.MigrationsFS},
		{Name: "todo", FS: todo.MigrationsFS},
		{Name: "events", FS: events.MigrationsFS},
		{Name: "dashboard", FS: dashboard.MigrationsFS},
		{Name: "notes", FS: notes.MigrationsFS},
		{Name: "documents", FS: documents.MigrationsFS},
		{Name: "admin", FS: admin.MigrationsFS},
		{Name: "finance", FS: finance.MigrationsFS},
	}
}

// MigrationSourcesWithSeed adds the one-off historic-data seed carried over from
// the retiring `fin` service (PRD D91, block 09900). ONLY the server entrypoint
// uses this: a test must migrate a SCHEMA, not a household's finances. Without
// the split, every module test — and any test that counts rows — would run
// against fifteen months of real data.
//
// Default = no seed is deliberate: a future caller who forgets this exists gets
// the safe behaviour.
func MigrationSourcesWithSeed() []registry.MigrationSource {
	return append(MigrationSources(),
		registry.MigrationSource{Name: "finance-seed", FS: financeseed.MigrationsFS})
}

// MigrationFS assembles the merged, SCHEMA-ONLY migration FS. This is what
// testsupport migrates with.
func MigrationFS() (fs.FS, error) {
	return registry.MergeMigrations(MigrationSources())
}

// MigrationFSWithSeed assembles the schema plus the production-only finance seed.
// cmd/home calls this one; nothing else should.
func MigrationFSWithSeed() (fs.FS, error) {
	return registry.MergeMigrations(MigrationSourcesWithSeed())
}
