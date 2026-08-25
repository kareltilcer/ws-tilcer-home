package documents

import (
	"context"
	"embed"
	"io/fs"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
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
	// svc backs the v9 storage catalog — see the notes module for why the module
	// forwards rather than the service being registered directly.
	svc *Service
}

// NewModule builds the documents module over svc.
func NewModule(svc *Service) *Module {
	return &Module{
		svc:     svc,
		handler: NewHandler(svc),
		widgets: []registry.WidgetProvider{newPripnuteProvider(svc.store)},
		metrics: &metricProvider{store: svc.store},
	}
}

// StorageBlobs and PrivateItems forward to the service for the v9 storage catalog.
// ⚠ Their absence is SILENT — see the notes module's copy of this comment.
func (m *Module) StorageBlobs(ctx context.Context) ([]storage.BlobUsage, error) {
	return m.svc.StorageBlobs(ctx)
}

func (m *Module) PrivateItems(ctx context.Context, ownerUserID string) ([]storage.Item, int64, error) {
	return m.svc.PrivateItems(ctx, ownerUserID)
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
		// v9: publishing a private item into the shared tree. Its own verb rather
		// than a flavour of `move` because "this became visible to the household" is
		// the most consequential thing that can happen to a private item, and it is
		// the one change in the module that cannot be undone (D182).
		//
		// ⚠ A new action must ALSO be added to admin/labels.go's actionLabels, a
		// hand-maintained map that falls back to the raw key — without an entry these
		// show up as `documents.document.publish` in the rule composer (D213).
		"document.publish", "document_folder.publish",
	}
}

func (m *Module) Widgets() []registry.WidgetProvider { return m.widgets }

// StorageTables declares this module's tables for the v9 storage catalog (D191).
// documents_fts is external-content FTS5 — see the notes module for why the four
// shadow rows matter (D211).
func (m *Module) StorageTables() []string {
	return append([]string{"document_folders", "documents", "document_pins"},
		storage.FTSShadows("documents_fts")...)
}
