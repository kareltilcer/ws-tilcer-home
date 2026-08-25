package todo

import (
	"embed"
	"io/fs"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Module is the todo (Úkoly) feature module: the board, plus (from Phase D) the
// todo.pravedelam dashboard widget it contributes (FR-T8).
type Module struct {
	svc     *Service
	handler *Handler
	widget  registry.WidgetProvider
	metrics *metricProvider
	lists   *listProvider
}

// NewModule builds the todo module over svc.
func NewModule(svc *Service) *Module {
	return &Module{
		svc:     svc,
		handler: NewHandler(svc),
		widget:  newPravedelamProvider(svc.Store()),
		metrics: &metricProvider{store: svc.Store()},
		lists:   &listProvider{store: svc.Store()},
	}
}

func (m *Module) Name() string { return "todo" }

func (m *Module) RegisterRoutes(r chi.Router) { m.handler.Mount(r) }

func (m *Module) Migrations() fs.FS { return MigrationsFS }

func (m *Module) AuditActions() []string {
	return []string{
		"board.create", "board.update", "board.delete",
		"column.create", "column.update", "column.move", "column.delete",
		"card.create", "card.update", "card.move", "card.delete",
		"label.create", "label.update", "label.delete",
	}
}

func (m *Module) Widgets() []registry.WidgetProvider {
	return []registry.WidgetProvider{m.widget}
}

// StorageTables declares the SQLite tables this module owns, for the v9 storage
// catalog (platform/storage, D191). The platform sizes them; it needs no idea what
// they mean, which is why this is a plain []string and not a richer descriptor.
//
// ⚠ A table added to this module's migrations and NOT added here fails
// TestEveryTableIsDeclared. That is deliberate: v9 opened a fifth registration
// surface, and unlike the four host maps before it, this one is closable by
// machine (D192).
func (m *Module) StorageTables() []string {
	return []string{"boards", "columns", "cards", "labels", "card_labels", "card_links", "checklist_items"}
}
