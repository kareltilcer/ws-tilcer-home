package events

import (
	"embed"
	"io/fs"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Module is the events (Okno do budoucnosti) feature module: recurring all-day
// events with reminders, plus the events.pripominky and events.tento-mesic
// dashboard widgets it contributes (FR-E7, FR-E8).
type Module struct {
	svc     *Service
	handler *Handler
	widgets []registry.WidgetProvider
	metrics *metricProvider
}

// NewModule builds the events module over svc. loc + lookbackDays configure the
// widget providers' date math (evaluated in HOME_TIMEZONE).
func NewModule(svc *Service, loc *time.Location, lookbackDays int) *Module {
	return &Module{
		svc:     svc,
		handler: NewHandler(svc),
		widgets: []registry.WidgetProvider{
			newPripominkyProvider(svc.store, loc, lookbackDays, svc.maxOccurrences),
			newTentoMesicProvider(svc.store, loc, svc.maxOccurrences),
		},
		metrics: &metricProvider{
			store:        svc.store,
			lookbackDays: lookbackDays,
			maxOcc:       svc.maxOccurrences,
		},
	}
}

func (m *Module) Name() string { return "events" }

func (m *Module) RegisterRoutes(r chi.Router) { m.handler.Mount(r) }

func (m *Module) Migrations() fs.FS { return MigrationsFS }

func (m *Module) AuditActions() []string {
	return []string{
		"event.create", "event.update", "event.delete",
		"reminder.complete", "reminder.uncomplete",
	}
}

func (m *Module) Widgets() []registry.WidgetProvider { return m.widgets }
