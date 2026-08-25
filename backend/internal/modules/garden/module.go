package garden

import (
	"embed"
	"io/fs"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
)

//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Module is the garden (Zahrada) feature module: the household's crop knowledge,
// its bed plan, the checks that plan implies, and the work both imply.
//
// It is registry-driven throughout — the widget joins the catalog, the six
// metrics and six lists reach the admin composer, and garden.* joins the action
// catalog the trigger composer reads — so the host needs no edit beyond building
// it. Self-contained per HANDOFF §3: it imports only platform/ and registry.
//
// TWO IMPORTS IT MUST NEVER GAIN, asserted by internal/arch:
//
//   - platform/push — the module sends NOTHING (D113). It publishes a metric, a
//     list and one audit event whose Czech summary already reads as a finished
//     notification; Administrace decides at runtime whether that becomes a
//     scheduled summary or a trigger rule. Both work on day one, and a third
//     path inside here would be the one nobody can configure.
//   - platform/blobstore — the module holds NO BYTES (D122). Photos are out of
//     scope, so there is no bucket, no mirror job and no reconciliation.
//
// Both are exactly the kind of thing a later "small improvement" reaches for,
// which is why they are a test rather than a comment.
//
// NOTE: the BUILT-IN RULES are deliberately NOT in MigrationsFS. They live in
// the sibling garden/seed package as their own migration source, which only the
// server entrypoint includes (D115) — see seed/embed.go.
type Module struct {
	handler *Handler
	widgets []registry.WidgetProvider
	metrics *metricProvider
	lists   *listProvider
}

// NewModule builds the garden module over svc.
func NewModule(svc *Service) *Module {
	return &Module{
		handler: NewHandler(svc),
		widgets: []registry.WidgetProvider{&praceProvider{svc: svc}},
		metrics: &metricProvider{svc: svc},
		lists:   &listProvider{svc: svc},
	}
}

func (m *Module) Name() string { return "garden" }

func (m *Module) RegisterRoutes(r chi.Router) { m.handler.Mount(r) }

func (m *Module) Migrations() fs.FS { return MigrationsFS }

// AuditActions are the verbs this module emits, for the log browser's filter and
// the trigger composer's picker. `frost_warning` is the only system-written one.
func (m *Module) AuditActions() []string {
	return []string{
		"plant.create", "plant.update", "plant.delete",
		"variety.create", "variety.update", "variety.delete",
		"bed.create", "bed.update", "bed.delete", "bed.move",
		"season.create", "season.update", "season.close", "season.reopen",
		"planting.create", "planting.update", "planting.delete",
		"task.create", "task.update", "task.delete",
		"harvest.create", "harvest.update", "harvest.delete",
		"storage.create", "storage.update", "storage.delete",
		"rule.create", "rule.update", "rule.delete",
		"settings.update", "frost_warning",
	}
}

func (m *Module) Widgets() []registry.WidgetProvider { return m.widgets }

// StorageTables declares this module's tables for the v9 storage catalog (D191).
//
// The largest declaration in Home — thirteen tables plus an FTS5 index — which is
// the point of measuring per module rather than guessing.
//
// ⚠ garden_plants_fts is EXTERNAL-CONTENT FTS5, so it materialises five
// `type='table'` rows and FTSShadows expands them. Leaving the four shadows out
// would under-report this module by more than the plants table itself weighs, and
// the per-module totals would stop summing to the database total (D211).
func (m *Module) StorageTables() []string {
	tables := []string{
		"garden_beds", "garden_plants", "garden_varieties", "garden_seasons",
		"garden_plantings", "garden_tasks", "garden_harvests", "garden_storage_items",
		"garden_rules", "garden_settings", "garden_weather_days", "garden_warning_dismissals",
	}
	return append(tables, storage.FTSShadows("garden_plants_fts")...)
}
