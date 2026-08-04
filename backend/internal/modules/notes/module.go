package notes

import (
	"embed"
	"io/fs"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Module is the notes (Poznámky) feature module: Markdown notes in a folder tree,
// slug-path URLs, two-scope pinning, and the notes.pripnute dashboard widget
// (FR-P1–P8). Self-contained per HANDOFF §3 — it imports only platform/ and
// registry, and is discovered by the dashboard host purely through its widget
// provider.
type Module struct {
	handler *Handler
	widgets []registry.WidgetProvider
}

// NewModule builds the notes module over svc.
func NewModule(svc *Service) *Module {
	return &Module{
		handler: NewHandler(svc),
		widgets: []registry.WidgetProvider{newPripnuteProvider(svc.store)},
	}
}

func (m *Module) Name() string { return "notes" }

func (m *Module) RegisterRoutes(r chi.Router) { m.handler.Mount(r) }

func (m *Module) Migrations() fs.FS { return MigrationsFS }

func (m *Module) AuditActions() []string {
	return []string{
		"note.create", "note.update", "note.move", "note.delete", "note.pin", "note.unpin",
		"folder.create", "folder.update", "folder.move", "folder.delete",
	}
}

func (m *Module) Widgets() []registry.WidgetProvider { return m.widgets }
