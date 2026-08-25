package notes

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

// Module is the notes (Poznámky) feature module: Markdown notes in a folder tree,
// slug-path URLs, two-scope pinning, and the notes.pripnute dashboard widget
// (FR-P1–P8). Self-contained per HANDOFF §3 — it imports only platform/ and
// registry, and is discovered by the dashboard host purely through its widget
// provider.
type Module struct {
	handler *Handler
	widgets []registry.WidgetProvider
	metrics *metricProvider
	lists   *listProvider
	// svc backs the v9 storage catalog. Only the SERVICE can attribute R2 objects
	// to members or list private items — it holds the store and the blob client —
	// but the catalog collects MODULES, so the module forwards. See StorageBlobs.
	svc *Service
}

// NewModule builds the notes module over svc.
func NewModule(svc *Service) *Module {
	return &Module{
		svc:     svc,
		handler: NewHandler(svc),
		widgets: []registry.WidgetProvider{newPripnuteProvider(svc.store)},
		metrics: &metricProvider{store: svc.store},
		lists:   &listProvider{store: svc.store},
	}
}

// StorageBlobs and PrivateItems forward to the service for the v9 storage catalog
// (D191/D194/D198).
//
// ⚠ THESE FORWARDERS ARE LOad-BEARING and their absence is SILENT. The catalog is
// assembled from the MODULE set, and storage.Collect discovers BlobSource and
// PrivateInventory by TYPE ASSERTION — a module that does not implement them is
// not an error, it is simply "a module that holds no bytes", which is the normal
// case for eight of the ten. So when these methods lived only on the Service the
// build was green, every test passed, and the Úložiště page quietly reported
// `0 B` and an empty purge listing.
//
// That was found by opening the page, not by a test. TestRealModulesImplementTheStorageCatalog
// in internal/arch now asserts it.
func (m *Module) StorageBlobs(ctx context.Context) ([]storage.BlobUsage, error) {
	return m.svc.StorageBlobs(ctx)
}

func (m *Module) PrivateItems(ctx context.Context, ownerUserID string) ([]storage.Item, int64, error) {
	return m.svc.PrivateItems(ctx, ownerUserID)
}

func (m *Module) Name() string { return "notes" }

func (m *Module) RegisterRoutes(r chi.Router) { m.handler.Mount(r) }

func (m *Module) Migrations() fs.FS { return MigrationsFS }

func (m *Module) AuditActions() []string {
	return []string{
		"note.create", "note.update", "note.move", "note.delete", "note.pin", "note.unpin",
		"folder.create", "folder.update", "folder.move", "folder.delete",
		// v9: publishing a private item into the shared tree. It is its own verb
		// rather than a flavour of `move` because "this became visible to the
		// household" is the most consequential thing that can happen to a private
		// item, and it is the one change in the module that cannot be undone (D182).
		//
		// ⚠ A new action must ALSO be added to admin/labels.go's actionLabels, which
		// is a hand-maintained map that falls back to the raw key — without an entry
		// these show up as `notes.note.publish` in the rule composer (D213).
		"note.publish", "folder.publish",
	}
}

func (m *Module) Widgets() []registry.WidgetProvider { return m.widgets }

// StorageTables declares this module's tables for the v9 storage catalog (D191).
//
// ⚠ notes_fts is EXTERNAL-CONTENT FTS5, so five `type='table'` rows exist where
// the migration writes one (D211). notes_fts_data routinely outweighs `notes`
// itself, so omitting the shadows would not merely under-report — it would make
// the storage page's per-module totals stop adding up to the database total, and
// that arithmetic is the page's whole premise.
func (m *Module) StorageTables() []string {
	return append([]string{"folders", "notes", "note_pins", "note_images"},
		storage.FTSShadows("notes_fts")...)
}
