package documents

import (
	"embed"
	"io/fs"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Module is the documents (Dokumenty) feature module: files in their own folder
// tree with the bytes in object storage, permanent id-based household-only content
// URLs, async previews, two-scope pinning, and the documents.pripnute dashboard
// widget (FR-DOC1–DOC11).
//
// Self-contained per HANDOFF §3: it imports only platform/ (including the new
// platform/blobstore) and registry, and the dashboard host discovers it purely
// through its widget provider — adding this module to the app is adding a package
// that registers itself, with no host edit.
type Module struct {
	handler *Handler
	widgets []registry.WidgetProvider
	metrics *metricProvider
}

// NewModule builds the documents module over svc.
func NewModule(svc *Service) *Module {
	return &Module{
		handler: NewHandler(svc),
		widgets: []registry.WidgetProvider{newPripnuteProvider(svc.store)},
		metrics: &metricProvider{store: svc.store},
	}
}

func (m *Module) Name() string { return "documents" }

func (m *Module) RegisterRoutes(r chi.Router) { m.handler.Mount(r) }

func (m *Module) Migrations() fs.FS { return MigrationsFS }

// AuditActions lists the verbs this module emits, for the log browser's filter
// catalog. Personal pins are deliberately absent: they are a per-user view
// preference and emit nothing (D47).
func (m *Module) AuditActions() []string {
	return []string{
		"document.create", "document.update", "document.move", "document.delete",
		"document.pin", "document.unpin",
		"document_folder.create", "document_folder.update", "document_folder.move", "document_folder.delete",
	}
}

func (m *Module) Widgets() []registry.WidgetProvider { return m.widgets }
