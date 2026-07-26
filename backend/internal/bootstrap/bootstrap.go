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

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/dashboard"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/logging"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/todo"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

// MigrationSources returns every migration contributor in the fixed order
// logging → platform → todo → events → dashboard (PRD §5 D25). The logging
// (audit) tables must exist before any feature table because every module writes
// through the audit spine.
func MigrationSources() []registry.MigrationSource {
	return []registry.MigrationSource{
		{Name: "logging", FS: logging.MigrationsFS},
		{Name: "platform", FS: appdb.MigrationsFS},
		{Name: "todo", FS: todo.MigrationsFS},
		{Name: "events", FS: events.MigrationsFS},
		{Name: "dashboard", FS: dashboard.MigrationsFS},
	}
}

// MigrationFS assembles the merged, boot-time migration FS for the whole service.
func MigrationFS() (fs.FS, error) {
	return registry.MergeMigrations(MigrationSources())
}
