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
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/electricity"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/finance"
	financeseed "github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/finance/seed"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/garden"
	gardenseed "github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/garden/seed"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/logging"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/notes"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/todo"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

// MigrationSources returns every SCHEMA migration contributor. Goose applies
// migrations globally by their numeric filename prefix, so the effective order is
// logging(01) → platform(02) → todo(03) → events(04) → dashboard(05) → notes(06)
// → documents(07) → admin(08) → finance(09) → garden(10) → electricity(11), regardless of the slice
// order below (PRD §5 D25). The logging (audit) tables must exist before any
// feature table because every module writes through the audit spine; its 01xxx
// prefix guarantees that. The slice is listed in prefix order purely so it reads
// the way the migrations actually run.
//
// v5 adds two contributors' worth of tables: the per-user push tables and the
// outbox cursor go in the PLATFORM block (any module may send, so a member's
// consent cannot depend on the admin module), while the rule/schedule/delivery
// tables are the admin module's own and run last.
//
// THIS IS THE SCHEMA, and it is what tests migrate with (testsupport.NewDB). The
// v6 finance and v7 garden modules each ship a second, PRODUCTION-ONLY source
// — see MigrationSourcesWithSeed.
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
		{Name: "garden", FS: garden.MigrationsFS},
		// v8: electricity is block 11 and has NO seed counterpart below —
		// deliberately, unlike finance and garden. There is no historic data to
		// carry and no built-in knowledge to preload, so there is nothing for
		// testsupport to exclude. Do not add one for symmetry.
		{Name: "electricity", FS: electricity.MigrationsFS},
	}
}

// MigrationSourcesWithSeed adds the PRODUCTION-ONLY seed sources. ONLY the
// server entrypoint uses this: a test must migrate a SCHEMA, not a household's
// finances and not a pre-loaded opinion about companion planting.
//
// Two sources ride here, for the same reason in two shapes:
//
//   - finance-seed (block 09900, D91) carries the historic months from the
//     retiring `fin` service. Without the split, every module test — and any
//     test that counts rows — would run against fifteen months of real data.
//   - garden-seed (block 10900, D115) carries the built-in compatibility rules.
//     Without the split, a garden check fixture would pass because a SEEDED rule
//     matched rather than the one the test wrote: a false green that is very
//     hard to see.
//
// Default = no seed is deliberate: a future caller who forgets these exist gets
// the safe behaviour.
func MigrationSourcesWithSeed() []registry.MigrationSource {
	return append(MigrationSources(),
		registry.MigrationSource{Name: "finance-seed", FS: financeseed.MigrationsFS},
		registry.MigrationSource{Name: "garden-seed", FS: gardenseed.MigrationsFS})
}

// MigrationFS assembles the merged, SCHEMA-ONLY migration FS. This is what
// testsupport migrates with.
func MigrationFS() (fs.FS, error) {
	return registry.MergeMigrations(MigrationSources())
}

// MigrationFSWithSeed assembles the schema plus the production-only seeds.
// cmd/home calls this one; nothing else should.
func MigrationFSWithSeed() (fs.FS, error) {
	return registry.MergeMigrations(MigrationSourcesWithSeed())
}
