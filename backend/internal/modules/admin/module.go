package admin

import (
	"embed"
	"io/fs"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Module is the admin (Administrace) feature module — module SEVEN, and the
// first that is admin-only end to end (D62: the same gate as the log browser,
// with no reader view; for a non-admin the module simply does not exist).
//
// It is the configuration surface for notifications: broadcasts, trigger rules
// bound to audit action keys, and scheduled summaries composed from the metrics
// catalog. The channel it sends through is platform infrastructure, not this
// module's, which is what lets any module notify and lets every member keep
// control of their own devices (D52/D53).
//
// It contributes NO widgets: the dashboard is for household data, and admin
// configuration is not that.
//
// Boundaries it deliberately respects (D28): it imports only platform/* — the
// push channel, the scheduler, the metrics registry, and the audit package whose
// outbox it listens to. It never imports `logging` (its listener is registered
// through a platform hook), and it never imports todo/events/notes/documents
// (every count it prints comes through the metrics contract).
type Module struct {
	svc     *Service
	handler *Handler
}

// NewModule builds the admin module over svc.
func NewModule(svc *Service) *Module {
	return &Module{svc: svc, handler: NewHandler(svc)}
}

func (m *Module) Name() string { return "admin" }

func (m *Module) RegisterRoutes(r chi.Router) { m.handler.Mount(r) }

func (m *Module) Migrations() fs.FS { return MigrationsFS }

// AuditActions lists the verbs this module emits. The notifier logs its own
// configuration changes — recursive but bounded, like logging.prune — so the Log
// browser shows who changed a rule and when.
//
// Delivery attempts are deliberately absent: they are operational records in
// notification_deliveries, not audit events (D64).
func (m *Module) AuditActions() []string {
	return []string{
		"broadcast.send",
		"rule.create", "rule.update", "rule.delete",
		"schedule.create", "schedule.update", "schedule.delete",
		"notification.test",
	}
}

// Widgets returns none — see the type comment.
func (m *Module) Widgets() []registry.WidgetProvider { return nil }
