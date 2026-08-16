package events

import (
	"context"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
)

// Metric keys published by this module (PRD §V5-4, D69).
//
// All four are HOUSEHOLD-scoped because completion in this module is shared: one
// member marking a reminder done marks it done for everyone. v5 deliberately did
// NOT change that (D68 reverts OQ-7), so there is no per-user variant here.
const (
	MetricPripominkyToday     = "events.pripominky_today"
	MetricPripominkyTodayOpen = "events.pripominky_today_open"
	MetricOverdueOpen         = "events.overdue_open"
	MetricDueWithin7d         = "events.due_within_7d"
)

// metricProvider resolves the events metrics. It reuses the bounded
// expand-on-read the Připomínky widget uses — occurrences are never stored, so a
// count is an expansion over a capped window, not a table scan.
//
// One deliberate difference from the widget, worth stating because the two are
// read side by side: these metrics are anchored to the OCCURRENCE DATE, not to
// reminder_lead. The widget answers "what should I be looking at now?" and shows
// a reminder from `occurrence − lead` onward; a summary answers "what is due
// today / what is past its date?", and a reminder with a two-week lead is not
// something to report as due today merely because its lead has opened. Where the
// two DO answer the same question — overdue — the counting unit is aligned; see
// MetricOverdueOpen.
type metricProvider struct {
	store        *Store
	lookbackDays int
	maxOcc       int
}

// MetricProvider exposes this module's metrics to the platform registry.
func (m *Module) MetricProvider() metrics.Provider { return m.metrics }

func (p *metricProvider) Descriptors() []metrics.Descriptor {
	return []metrics.Descriptor{
		{Key: MetricPripominkyToday, Label: "Připomínky na dnešek", Unit: "připomínek", Scope: metrics.ScopeHousehold},
		{Key: MetricPripominkyTodayOpen, Label: "Nesplněné připomínky na dnešek", Unit: "připomínek", Scope: metrics.ScopeHousehold},
		{Key: MetricOverdueOpen, Label: "Připomínky po termínu", Unit: "připomínek", Scope: metrics.ScopeHousehold},
		{Key: MetricDueWithin7d, Label: "Události v příštích 7 dnech", Unit: "událostí", Scope: metrics.ScopeHousehold},
	}
}

func (p *metricProvider) Value(ctx context.Context, _ string, key string, asOf time.Time) (int, error) {
	today := dates.New(asOf.Year(), asOf.Month(), asOf.Day())

	switch key {
	case MetricDueWithin7d:
		// Every event's occurrences in [today, today+7] — the look-ahead the
		// Tento měsíc widget shows, counted rather than listed.
		return p.countOccurrences(ctx, today, today.AddDays(7), false, false, false)
	case MetricPripominkyToday:
		// A single day, so per-occurrence and per-event counting coincide: an
		// event cannot recur twice within one date.
		return p.countOccurrences(ctx, today, today, true, false, false)
	case MetricPripominkyTodayOpen:
		return p.countOccurrences(ctx, today, today, true, true, false)
	case MetricOverdueOpen:
		// Reminders with a past uncompleted occurrence, bounded by the same
		// lookback the dashboard uses — "overdue" has to stop somewhere.
		//
		// Counted ONCE PER EVENT (perEvent), because that is what the Připomínky
		// widget shows: it picks each event's earliest uncompleted occurrence and
		// renders one row for it. Counting occurrences instead would make an 08:00
		// summary say "20 připomínek po termínu" for a single daily reminder left
		// alone for twenty days, while the dashboard the household then opens
		// lists exactly one — the two must not disagree about the same word.
		return p.countOccurrences(ctx, today.AddDays(-p.lookbackDays), today.AddDays(-1), true, true, true)
	default:
		return 0, fmt.Errorf("events: unknown metric %q", key)
	}
}

// countOccurrences counts what scanOccurrences selects. The two summary
// surfaces — a metric's number and a list's items — MUST answer the same
// question the same way, so the selection lives in one place and this is only
// the counting half of it (see scanOccurrences in lists.go).
func (p *metricProvider) countOccurrences(ctx context.Context, from, to dates.Date, reminderOnly, openOnly, perEvent bool) (int, error) {
	occs, err := scanOccurrences(ctx, p.store, occurrenceQuery{
		from: from, to: to, maxOcc: p.maxOcc,
		reminderOnly: reminderOnly, openOnly: openOnly, perEvent: perEvent,
	})
	if err != nil {
		return 0, err
	}
	return len(occs), nil
}
