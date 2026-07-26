package logging

import (
	"database/sql"
	"embed"
	"io/fs"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

// migrationsFS holds this module's audit-spine tables (created first in the
// global sequence).
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Module is the logging feature module: the admin log browser over the audit
// spine. The spine WRITER lives in platform/audit (so every module can call it);
// this module owns the audit TABLES (its migrations) and the read-side browser.
type Module struct {
	handler *HTTPHandler
}

// New builds the logging module over db.
func New(db *sql.DB) *Module {
	return &Module{handler: NewHTTPHandler(NewStore(db))}
}

func (m *Module) Name() string { return "logging" }

// RegisterRoutes mounts /api/logs/** behind the admin gate (D5): a reader or
// editor gets 403 at the route, not merely a hidden nav entry.
func (m *Module) RegisterRoutes(r chi.Router) {
	r.Route("/logs", func(r chi.Router) {
		r.Use(httpx.RequireAdmin)
		m.handler.Mount(r)
	})
}

func (m *Module) Migrations() fs.FS { return MigrationsFS }

// AuditActions are the verbs this module emits itself (the retention prune).
func (m *Module) AuditActions() []string { return []string{"prune"} }

func (m *Module) Widgets() []registry.WidgetProvider { return nil }
