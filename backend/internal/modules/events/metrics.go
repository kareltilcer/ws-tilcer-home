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
// The "připomínky na dnešek" metrics resolve through the Připomínky widget's
// own selection, exactly like the same-keyed lists: a "připomínka na dnešek"
// is a CURRENT widget row — its lead has opened and its day has not passed —
// so the number, the lines under it and the dashboard can never disagree
// (D99). Only MetricDueWithin7d asks about event dates, because a look-ahead
// is a question about the calendar rather than about reminders.
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
		return p.count(ctx, occurrenceQuery{from: today, to: today.AddDays(7), maxOcc: p.maxOcc})
	case MetricPripominkyToday:
		// Current reminders — the same selection the same-keyed list names, so
		// "3 připomínky na dnešek" and the three lines under it can never disagree.
		return p.countCurrent(ctx, today, true)
	case MetricPripominkyTodayOpen:
		return p.countCurrent(ctx, today, false)
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
		return p.count(ctx, occurrenceQuery{
			from: today.AddDays(-p.lookbackDays), to: today.AddDays(-1), maxOcc: p.maxOcc,
			reminderOnly: true, openOnly: true, perEvent: true,
		})
	default:
		return 0, fmt.Errorf("events: unknown metric %q", key)
	}
}

// count counts what scanOccurrences selects. The two summary surfaces — a
// metric's number and a list's items — MUST answer the same question the same
// way, so the selection lives in one place and this is only the counting half
// of it (see scanOccurrences in lists.go).
func (p *metricProvider) count(ctx context.Context, q occurrenceQuery) (int, error) {
	occs, err := scanOccurrences(ctx, p.store, q)
	if err != nil {
		return 0, err
	}
	return len(occs), nil
}

// countCurrent is the counting half of currentLines — the widget's non-overdue
// rows (see currentReminders in pripominky.go).
func (p *metricProvider) countCurrent(ctx context.Context, today dates.Date, completedToo bool) (int, error) {
	rem, err := currentReminders(ctx, p.store, today, p.lookbackDays, p.maxOcc, completedToo)
	if err != nil {
		return 0, err
	}
	return len(rem), nil
}
