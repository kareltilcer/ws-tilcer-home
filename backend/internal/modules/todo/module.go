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
}

// NewModule builds the todo module over svc.
func NewModule(svc *Service) *Module {
	return &Module{
		svc:     svc,
		handler: NewHandler(svc),
		widget:  newPravedelamProvider(svc.Store()),
		metrics: &metricProvider{store: svc.Store()},
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
