package todo

import (
	"context"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
)

// Metric keys published by this module (PRD §V5-4, D69). All three are
// HOUSEHOLD-scoped: a board is shared, so every recipient sees the same number.
const (
	MetricPravedelamCount = "todo.pravedelam_count"
	MetricDoneToday       = "todo.done_today"
	MetricOpenTotal       = "todo.open_total"
)

// metricProvider resolves the todo metrics a scheduled summary can reference.
// It reads the same bounded shape the Právě dělám widget does — one COUNT per
// metric, no per-recipient work — and reaches nothing outside this module.
type metricProvider struct{ store *Store }

// MetricProvider exposes this module's metrics to the platform registry. It is
// deliberately not part of registry.Module: the host discovers it by asserting
// metrics.Source, so adding metrics changed no module contract.
func (m *Module) MetricProvider() metrics.Provider { return m.metrics }

func (p *metricProvider) Descriptors() []metrics.Descriptor {
	return []metrics.Descriptor{
		{Key: MetricPravedelamCount, Label: "Úkoly v Právě dělám", Unit: "úkolů", Scope: metrics.ScopeHousehold},
		{Key: MetricDoneToday, Label: "Hotovo dnes", Unit: "úkolů", Scope: metrics.ScopeHousehold},
		{Key: MetricOpenTotal, Label: "Nedokončené úkoly celkem", Unit: "úkolů", Scope: metrics.ScopeHousehold},
	}
}

func (p *metricProvider) Value(ctx context.Context, _ string, key string, asOf time.Time) (int, error) {
	switch key {
	case MetricPravedelamCount:
		return p.store.CountCardsInKind(ctx, []string{"now"})
	case MetricOpenTotal:
		// "Open" is everything not in a done column — the normal and now columns
		// together, which is what a household reads as "still to do".
		return p.store.CountCardsInKind(ctx, []string{"normal", "now"})
	case MetricDoneToday:
		start, end := localDayBounds(asOf)
		return p.store.CountDoneBetween(ctx, start, end)
	default:
		return 0, fmt.Errorf("todo: unknown metric %q", key)
	}
}

// localDayBounds returns the UTC instants bracketing asOf's LOCAL day. Cards
// stamp done_at in UTC, but "hotovo dnes" means the household's day in
// Europe/Prague — so the window is computed in the local zone and converted,
// never by truncating the UTC timestamp (which would be wrong by an hour or two
// for everything done late in the evening).
func localDayBounds(asOf time.Time) (time.Time, time.Time) {
	y, m, d := asOf.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, asOf.Location())
	return start.UTC(), start.AddDate(0, 0, 1).UTC()
}
