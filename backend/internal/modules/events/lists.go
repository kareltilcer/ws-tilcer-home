package events

import (
	"context"
	"fmt"
	"sort"
	"time"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/recur"
)

// List keys published by this module. Each one is the NAMED counterpart of the
// metric with the same key: "3 připomínky na dnešek" and "which three" are the
// same question asked two ways, so they share a key and — via scanOccurrences —
// the selection behind it. A summary that used both must never be able to say
// three and then list four.
//
// All four are HOUSEHOLD-scoped for the same reason the metrics are: completion
// in this module is shared (D68).
const (
	ListPripominkyToday     = "events.pripominky_today"
	ListPripominkyTodayOpen = "events.pripominky_today_open"
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
		{Key: ListOverdueOpen, Label: "Připomínky po termínu", Empty: "nic po termínu", Scope: lists.ScopeHousehold},
		{Key: ListDueWithin7d, Label: "Události v příštích 7 dnech", Empty: "žádné události", Scope: lists.ScopeHousehold},
	}
}

func (p *listProvider) Items(ctx context.Context, _ string, key string, asOf time.Time) ([]string, error) {
	today := dates.New(asOf.Year(), asOf.Month(), asOf.Day())

	switch key {
	case ListPripominkyToday:
		return p.titles(ctx, today, today, true, false, false, false)
	case ListPripominkyTodayOpen:
		return p.titles(ctx, today, today, true, true, false, false)
	case ListOverdueOpen:
		// Once per event, matching the metric and the Připomínky widget: a daily
		// reminder left alone for a fortnight is ONE thing to deal with, and it is
		// dated so "Vynést koš (2. 9.)" says how long it has been waiting.
		return p.titles(ctx, today.AddDays(-p.lookbackDays), today.AddDays(-1), true, true, true, true)
	case ListDueWithin7d:
		// A multi-day look-ahead, so each line carries its date — a bare list of
		// titles spanning a week tells the household nothing about when.
		return p.titles(ctx, today, today.AddDays(7), false, false, false, true)
	default:
		return nil, fmt.Errorf("events: unknown list %q", key)
	}
}

// titles renders the selected occurrences as one short Czech line each, in the
// order a household reads them: soonest first, then alphabetically so a day's
// worth of reminders has a stable order between two sends of the same summary.
func (p *listProvider) titles(ctx context.Context, from, to dates.Date, reminderOnly, openOnly, perEvent, dated bool) ([]string, error) {
	occs, err := scanOccurrences(ctx, p.store, occurrenceQuery{
		from: from, to: to, maxOcc: p.maxOcc,
		reminderOnly: reminderOnly, openOnly: openOnly, perEvent: perEvent,
	})
	if err != nil {
		return nil, err
	}
	// Czech collation, not byte order: a byte compare files every Č/Ř/Š/Ú/Ž
	// title after the whole ASCII alphabet, which no household would call
	// "alphabetical". A collator is not safe for concurrent use, so each call
	// builds its own — the lists here are a handful of lines.
	czech := collate.New(language.Czech)
	sort.SliceStable(occs, func(i, j int) bool {
		if !occs[i].on.Equal(occs[j].on) {
			return occs[i].on.Before(occs[j].on)
		}
		return czech.CompareString(occs[i].title, occs[j].title) < 0
	})

	out := make([]string, 0, len(occs))
	for _, o := range occs {
		if dated {
			out = append(out, fmt.Sprintf("%s (%d. %d.)", o.title, o.on.D, int(o.on.M)))
			continue
		}
		out = append(out, o.title)
	}
	return out, nil
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
