package dashboard

import (
	"database/sql"
	"embed"
	"io/fs"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Module is the Nástěnka widget host. It owns only user_dashboard_layout and
// renders widgets contributed by other modules through the catalog — it imports
// no feature module (enforced by the architecture test).
type Module struct {
	handler *Handler
}

// NewModule builds the host over the assembled widget catalog and db (for the
// layout table). The catalog is built by the core from every module's providers.
func NewModule(catalog *registry.Catalog, db *sql.DB) *Module {
	svc := NewService(catalog, NewLayoutStore(db))
	return &Module{handler: NewHandler(svc)}
}

func (m *Module) Name() string { return "dashboard" }

func (m *Module) RegisterRoutes(r chi.Router) { m.handler.Mount(r) }

func (m *Module) Migrations() fs.FS { return MigrationsFS }

// AuditActions is empty: the host owns no feature data, and layout is a personal
// view preference (not audited).
func (m *Module) AuditActions() []string { return nil }

func (m *Module) Widgets() []registry.WidgetProvider { return nil }
