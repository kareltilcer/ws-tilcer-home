package finance

import (
	"embed"
	"io/fs"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Module is the finance (Finance) feature module: one row per calendar month,
// the split derived on read, one dashboard widget, four metrics and one list.
//
// It is registry-driven throughout — the widget joins the catalog, the metric
// and list keys reach the admin composer, and finance.month.* joins the action
// catalog the trigger composer reads — so the host needs no edit beyond building
// it. Self-contained per HANDOFF §3: it imports only platform/ and registry.
//
// NOTE: the SEED of `fin`'s historic months is deliberately NOT in MigrationsFS.
// It lives in the sibling finance/seed package as its own migration source, which
// only the server entrypoint includes (D91) — see seed/embed.go.
type Module struct {
	handler *Handler
	widgets []registry.WidgetProvider
	metrics *metricProvider
	lists   *listProvider
}

// NewModule builds the finance module over svc. loc is HOME_TIMEZONE, which
// decides where the month boundary falls for the widget's "current month".
func NewModule(svc *Service, loc *time.Location) *Module {
	return &Module{
		handler: NewHandler(svc),
		widgets: []registry.WidgetProvider{newRozpocetProvider(svc.store, loc)},
		metrics: &metricProvider{store: svc.store, loc: loc},
		lists:   &listProvider{store: svc.store, loc: loc},
	}
}

func (m *Module) Name() string { return "finance" }

func (m *Module) RegisterRoutes(r chi.Router) { m.handler.Mount(r) }

func (m *Module) Migrations() fs.FS { return MigrationsFS }

func (m *Module) AuditActions() []string {
	return []string{"month.create", "month.update", "month.delete"}
}

func (m *Module) Widgets() []registry.WidgetProvider { return m.widgets }

// StorageTables declares this module's tables for the v9 storage catalog (D191).
func (m *Module) StorageTables() []string {
	return []string{"finance_months"}
}
