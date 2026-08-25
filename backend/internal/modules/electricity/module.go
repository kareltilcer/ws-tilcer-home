package electricity

import (
	"embed"
	"io/fs"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Module is the electricity (Elektřina) feature module: VT/NT meter readings at
// arbitrary dates, a ceník versioned by effective date, zálohy with a due day,
// user-set settlement periods, and the one question they answer — vyjdou zálohy,
// nebo doplatím?
//
// THIS IS THE FIRST HOME MODULE THAT CONTRIBUTES NOTHING (PRD D147, D156).
// No widget, no metric, no list, no push, no scheduler job, no blob store, no
// seed migration and no derived storage. Karel's answers were "no widget" and
// "no chase", and taking them literally produced the leanest module in the app.
//
// Two consequences worth stating, because both are load-bearing:
//
//  1. The module has to be WORTH OPENING ON PURPOSE, since nothing will ever
//     remind anyone it exists. That is why Přehled carries the module's entire
//     value, and why the one concession is a plain in-app line — "poslední odečet
//     před 47 dny" with a Zadat odečet button. Text on a page you already opened
//     is not a notification.
//  2. Of the four non-registry host maps that v5, v6 and v7 each tripped over,
//     only THREE are edited here — and the fourth inverts into a trap in the
//     opposite direction. platform/widgets/registry.tsx must NOT be touched: an
//     entry there for a module with no widget provider produces a dashboard tile
//     that resolves to nothing. No compile error, no runtime error, an empty card.
//
// FIVE IMPORTS IT MUST NEVER GAIN, asserted by internal/arch: platform/metrics,
// platform/lists, platform/push, platform/scheduler and platform/blobstore. All
// five are what a later "small improvement" reaches for, and this module's whole
// shape is the claim that it needs none of them. A comment saying so is not
// enforcement; a failing build is.
//
// Everything is computed ON READ by compute.go (D152): no derived column, no
// cache table, no periodic job. There is nothing here that can fall behind.
type Module struct {
	handler *Handler
}

// NewModule builds the electricity module over svc.
func NewModule(svc *Service) *Module {
	return &Module{handler: NewHandler(svc)}
}

func (m *Module) Name() string { return "electricity" }

func (m *Module) RegisterRoutes(r chi.Router) { m.handler.Mount(r) }

func (m *Module) Migrations() fs.FS { return MigrationsFS }

// AuditActions are the fifteen verbs this module emits, for the log browser's
// filter and the admin trigger composer's picker.
//
// All fifteen are editor/admin actions with field-level diffs, and NONE has a
// `system` actor: there is no background job anywhere in this module, so nothing
// here can be written by anything but a person. That is a stronger statement
// than it looks — it means every row in the Log for this module answers "who",
// and "who changed the VT price and to what" is exactly the question these
// entries exist to answer a year later, when the vyúčtování disagrees.
func (m *Module) AuditActions() []string {
	return []string{
		"reading.create", "reading.update", "reading.delete",
		"tariff.create", "tariff.update", "tariff.delete",
		"advance.create", "advance.update", "advance.delete",
		"payment.create", "payment.update", "payment.delete",
		"period.create", "period.update", "period.delete",
	}
}

// Widgets returns NIL, not an empty non-nil slice (D147).
//
// The distinction is not pedantry: an empty non-nil slice would put an empty
// section into the admin composer, which is a worse outcome than the module
// simply not appearing there. Elektřina publishes nothing to Nástěnka, and this
// is the method that says so.
//
// registry.Module requires this method, which is why it exists at all. There is
// deliberately no MetricProvider() and no ListProvider(): metrics.Collect and
// lists.Collect are duck-typed on their Source interfaces and skip a module that
// does not implement them, so NOT WRITING THEM is the mechanism — this module is
// absent from those catalogs by construction rather than by a registration it
// forgot to make.
func (m *Module) Widgets() []registry.WidgetProvider { return nil }

// StorageTables declares this module's tables for the v9 storage catalog (D191).
//
// This is the one catalog `electricity` DOES join, and the exception proves the
// rule: it publishes no widget, no metric, no list and no push (D147/D156), but it
// still occupies disk, and a storage page that silently omitted a module would be
// wrong rather than minimal.
func (m *Module) StorageTables() []string {
	return []string{
		"electricity_readings", "electricity_tariffs", "electricity_advances",
		"electricity_payments", "electricity_periods",
	}
}
