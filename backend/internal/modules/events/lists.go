package events

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/recur"
)

// List keys published by this module. The first four are the NAMED counterpart
// of the metric with the same key: "3 připomínky na dnešek" and "which three"
// are the same question asked two ways, so each pair shares one selection — the
// two "na dnešek" keys resolve through the Připomínky widget's own scan
// (currentReminders, D99), the other two through scanOccurrences. A summary
// that used both surfaces must never be able to say three and then list four.
//
// ListPripominkyActive has no metric twin: it is the whole Připomínky widget in
// words, and "how many rows are on the dashboard" is a number nobody asked for.
//
// All of them are HOUSEHOLD-scoped for the same reason the metrics are:
// completion in this module is shared (D68).
const (
	ListPripominkyToday     = "events.pripominky_today"
	ListPripominkyTodayOpen = "events.pripominky_today_open"
	ListPripominkyActive    = "events.pripominky_active"
	ListOverdueOpen         = "events.overdue_open"
	ListDueWithin7d         = "events.due_within_7d"
)

// listProvider resolves the events lists a scheduled summary can name.
type listProvider struct {
	store        *Store
	lookbackDays int
	maxOcc       int
}

// ListProvider exposes this module's lists to the platform registry. Like
// MetricProvider it is discovered by type assertion, so publishing lists changed
// no module contract.
func (m *Module) ListProvider() lists.Provider { return m.lists }

func (p *listProvider) Descriptors() []lists.Descriptor {
	return []lists.Descriptor{
		{Key: ListPripominkyToday, Label: "Připomínky na dnešek", Empty: "žádné připomínky", Scope: lists.ScopeHousehold},
		{Key: ListPripominkyTodayOpen, Label: "Nesplněné připomínky na dnešek", Empty: "nic nezbývá", Scope: lists.ScopeHousehold},
		{Key: ListPripominkyActive, Label: "Aktivní připomínky (i po termínu)", Empty: "žádné aktivní připomínky", Scope: lists.ScopeHousehold},
		{Key: ListOverdueOpen, Label: "Připomínky po termínu", Empty: "nic po termínu", Scope: lists.ScopeHousehold},
		{Key: ListDueWithin7d, Label: "Události v příštích 7 dnech", Empty: "žádné události", Scope: lists.ScopeHousehold},
	}
}

func (p *listProvider) Items(ctx context.Context, _ string, key string, asOf time.Time) ([]string, error) {
	today := dates.New(asOf.Year(), asOf.Month(), asOf.Day())

	switch key {
	case ListPripominkyToday:
		// A "připomínka na dnešek" is a reminder that is CURRENT: its lead has
		// opened (today >= occurrence − lead) and its day has not passed — the
		// widget's non-overdue rows, completed or not, since ticking one off does
		// not unsay that it was today's reminder. Selecting by the event's own
		// date would miss the rent due next week whose 1w lead opened this
		// morning; selecting by "lead opens exactly today" would miss an event
		// created after its lead had already opened, and would drop an open
		// reminder from every summary between its first morning and the day it
		// turns overdue (D99).
		return p.currentLines(ctx, today, true)
	case ListPripominkyTodayOpen:
		return p.currentLines(ctx, today, false)
	case ListPripominkyActive:
		// Everything the Nástěnka Připomínky widget shows right now, overdue
		// included — one line per event, in the widget's own order, because it
		// resolves through the widget's own selection (D100).
		return p.activeLines(ctx, today)
	case ListOverdueOpen:
		// Once per event, matching the metric and the Připomínky widget: a daily
		// reminder left alone for a fortnight is ONE thing to deal with, and it is
		// dated so "Vynést koš (2. 9.)" says how long it has been waiting.
		return p.lines(ctx, occurrenceQuery{
			from: today.AddDays(-p.lookbackDays), to: today.AddDays(-1), maxOcc: p.maxOcc,
			reminderOnly: true, openOnly: true, perEvent: true,
		})
	case ListDueWithin7d:
		// A multi-day look-ahead — a bare list of titles spanning a week tells
		// the household nothing about when.
		return p.lines(ctx, occurrenceQuery{
			from: today, to: today.AddDays(7), maxOcc: p.maxOcc,
		})
	default:
		return nil, fmt.Errorf("events: unknown list %q", key)
	}
}

// lines renders the selected occurrences as one short dated Czech line each,
// in the one shared reading order (see lessByDayThenCzechTitle).
func (p *listProvider) lines(ctx context.Context, q occurrenceQuery) ([]string, error) {
	occs, err := scanOccurrences(ctx, p.store, q)
	if err != nil {
		return nil, err
	}
	czech := czechCollator()
	sort.SliceStable(occs, func(i, j int) bool {
		return lessByDayThenCzechTitle(czech, occs[i].on, occs[j].on, occs[i].title, occs[j].title)
	})

	out := make([]string, 0, len(occs))
	for _, o := range occs {
		out = append(out, dateLine(o.title, o.on))
	}
	return out, nil
}

// currentLines names the current reminders — the widget's non-overdue rows —
// shared with the metric of the same key, which counts exactly these lines.
func (p *listProvider) currentLines(ctx context.Context, today dates.Date, completedToo bool) ([]string, error) {
	rem, err := currentReminders(ctx, p.store, today, p.lookbackDays, p.maxOcc, completedToo)
	if err != nil {
		return nil, err
	}
	return reminderLines(rem), nil
}

// activeLines names what the Připomínky widget shows, in the widget's order
// (overdue first, then by date) — it IS the widget's selection rather than a
// second one shaped to agree with it.
func (p *listProvider) activeLines(ctx context.Context, today dates.Date) ([]string, error) {
	rem, err := activeReminders(ctx, p.store, today, p.lookbackDays, p.maxOcc)
	if err != nil {
		return nil, err
	}
	return reminderLines(rem), nil
}

// reminderLines renders widget rows as dated list lines, keeping the rows'
// own order. Dated like every multi-day list: a reminder may be for last
// Tuesday or for a fortnight out, and "Vynést koš" alone says neither.
func reminderLines(rem []DashboardReminder) []string {
	out := make([]string, 0, len(rem))
	for _, r := range rem {
		out = append(out, dateLine(r.Title, r.On))
	}
	return out
}

// dateLine is one list line carrying its date — "Vynést koš (2. 9.)", the
// app-wide short Czech day format.
func dateLine(title string, on dates.Date) string {
	return fmt.Sprintf("%s (%d. %d.)", title, on.D, int(on.M))
}

// ---- the shared selection ----

// occurrence is one expanded (event, date) pair — the unit both the metric
// counts and the list names.
type occurrence struct {
	title string
	on    dates.Date
}

// occurrenceQuery is what both summary surfaces select by.
type occurrenceQuery struct {
	from, to dates.Date
	maxOcc   int
	// reminderOnly keeps only reminder-enabled events (a plain calendar entry is
	// not a "připomínka").
	reminderOnly bool
	// openOnly drops occurrences somebody has already ticked off.
	openOnly bool
	// perEvent takes each EVENT at most once instead of every occurrence — the
	// unit the Připomínky widget presents. It matters only for multi-day windows.
	perEvent bool
}

// scanOccurrences expands every selected event across [from, to]. Completions
// are loaded in one batched query, so the cost is one event scan plus one
// completion scan regardless of how many events there are.
//
// This is the ONE definition of "which occurrences does a summary mean", shared
// by the metric counts and the lists — see the list-key comment above.
func scanOccurrences(ctx context.Context, store *Store, q occurrenceQuery) ([]occurrence, error) {
	evs, err := store.ListForWindow(ctx, false)
	if err != nil {
		return nil, err
	}

	var (
		selected []Event
		ids      []string
	)
	for _, e := range evs {
		if q.reminderOnly && (!e.ReminderEnabled || e.ReminderLead == nil) {
			continue
		}
		selected = append(selected, e)
		ids = append(ids, e.ID)
	}
	if len(selected) == 0 {
		return nil, nil
	}

	completions := map[string]map[string]bool{}
	if q.openOnly {
		if completions, err = store.CompletionsFor(ctx, ids); err != nil {
			return nil, err
		}
	}

	var out []occurrence
	for _, e := range selected {
		rule, anchor, err := parseSeries(&e)
		if err != nil {
			continue // an unparseable stored rule skips the event, never fails the summary
		}
		for _, occ := range recur.Expand(anchor, rule, q.from, q.to, q.maxOcc) {
			if q.openOnly && completions[e.ID][occ.String()] {
				continue
			}
			out = append(out, occurrence{title: e.Title, on: occ})
			if q.perEvent {
				break // this event is taken; its other occurrences are the same reminder
			}
		}
	}
	return out, nil
}
