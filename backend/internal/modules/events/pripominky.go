package events

import (
	"context"
	"sort"
	"time"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/recur"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

// maxLeadDays bounds how far ahead a reminder can be active (max lead ≈ 1 month,
// with a margin) — keeps the expansion window bounded.
const maxLeadDays = 40

// DashboardReminder is one active reminder for the events.pripominky widget
// (matches openapi DashboardReminder).
type DashboardReminder struct {
	EventID      string `json:"event_id"`
	OccurrenceOn string `json:"occurrence_on"`
	Title        string `json:"title"`
	Recurring    bool   `json:"recurring"`
	ReminderLead string `json:"reminder_lead"`
	Overdue      bool   `json:"overdue"`
	DaysUntil    int    `json:"days_until"`
	// On is the date OccurrenceOn was formatted from, kept typed so the summary
	// lists can re-render it without parsing their own output. Never serialized.
	On dates.Date `json:"-"`
}

// PripominkyWidget is the events.pripominky payload (openapi PripominkyWidget).
type PripominkyWidget struct {
	Reminders []DashboardReminder `json:"reminders"`
}

// pripominkyProvider implements FR-E7: the active reminders — for each
// non-archived reminder-enabled event, the earliest uncompleted occurrence within
// the lookback, shown once today >= occurrence − lead; at most one per event;
// overdue first, then by date. This is v1's dashboard reminder list, now a
// module-owned provider.
type pripominkyProvider struct {
	store        *Store
	lookbackDays int
	maxOcc       int
	today        func() dates.Date
}

func newPripominkyProvider(store *Store, loc *time.Location, lookbackDays, maxOcc int) *pripominkyProvider {
	return &pripominkyProvider{
		store:        store,
		lookbackDays: lookbackDays,
		maxOcc:       maxOcc,
		today:        func() dates.Date { return dates.Today(loc) },
	}
}

func (p *pripominkyProvider) Key() string    { return "events.pripominky" }
func (p *pripominkyProvider) Title() string  { return "Připomínky" }
func (p *pripominkyProvider) Module() string { return "events" }
func (p *pripominkyProvider) Description() string {
	return "Aktivní připomínky nadcházejících událostí."
}
func (p *pripominkyProvider) DefaultSize() string { return "narrow" }
func (p *pripominkyProvider) AdminOnly() bool     { return false }

func (p *pripominkyProvider) Data(ctx context.Context, _ registry.User) (any, error) {
	rem, err := activeReminders(ctx, p.store, p.today(), p.lookbackDays, p.maxOcc)
	if err != nil {
		return nil, err
	}
	return PripominkyWidget{Reminders: rem}, nil
}

// activeReminders is the ONE definition of "aktivní připomínka": for each
// non-archived reminder-enabled event, its EARLIEST UNCOMPLETED occurrence
// within the lookback, taken once its lead has opened (today >= occurrence −
// lead) — at most one row per event, overdue first and then by date.
//
// It is shared rather than duplicated because the events.pripominky_active
// list promises to name exactly what this widget shows (D100). Two
// implementations agreeing today is not the same as one that cannot drift:
// "aktivní" is a four-part rule (reminder-enabled, uncompleted, lead open,
// once per event) and every part of it is a place the two could part ways.
func activeReminders(ctx context.Context, store *Store, today dates.Date, lookbackDays, maxOcc int) ([]DashboardReminder, error) {
	return scanReminders(ctx, store, today, lookbackDays, maxOcc, false)
}

// currentReminders is the "na dnešek" selection: the widget's rows that are
// NOT overdue — lead open, day not yet passed (D99). It resolves through the
// widget's own scan for the same reason the active list does: the summary and
// the dashboard must not disagree about which reminders today holds.
//
// completedToo falls back to the occurrence the household already ticked off,
// for an event that therefore has no open row of its own — the completion-blind
// pripominky_today count ("completing it does not unsay it was today's
// reminder"); the _open variant passes false and gets the widget's rows verbatim.
func currentReminders(ctx context.Context, store *Store, today dates.Date, lookbackDays, maxOcc int, completedToo bool) ([]DashboardReminder, error) {
	rem, err := scanReminders(ctx, store, today, lookbackDays, maxOcc, completedToo)
	if err != nil {
		return nil, err
	}
	cur := make([]DashboardReminder, 0, len(rem))
	for _, r := range rem {
		if !r.Overdue {
			cur = append(cur, r)
		}
	}
	return cur, nil
}

// scanReminders is the shared scan behind both selections above. completedToo
// only adds a FALLBACK: an event with nothing open left is represented by the
// occurrence that was ticked off, as long as that day has not passed. It never
// overrides the widget's row, so the two surfaces cannot name one event under
// two different dates.
func scanReminders(ctx context.Context, store *Store, today dates.Date, lookbackDays, maxOcc int, completedToo bool) ([]DashboardReminder, error) {
	evs, err := store.ListForWindow(ctx, false)
	if err != nil {
		return nil, err
	}
	var withReminder []Event
	var ids []string
	for _, e := range evs {
		if e.ReminderEnabled && e.ReminderLead != nil {
			withReminder = append(withReminder, e)
			ids = append(ids, e.ID)
		}
	}
	completions, err := store.CompletionsFor(ctx, ids)
	if err != nil {
		return nil, err
	}

	from := today.AddDays(-lookbackDays)
	to := today.AddDays(maxLeadDays)

	out := []DashboardReminder{}
	for _, e := range withReminder {
		rule, anchor, err := parseSeries(&e)
		if err != nil {
			continue // unparseable stored rule — skip rather than fail the widget
		}
		occs := recur.Expand(anchor, rule, from, to, maxOcc)
		lead := *e.ReminderLead
		done := completions[e.ID]
		// The widget's row for this event: its earliest UNCOMPLETED occurrence.
		chosen, found := firstActive(occs, today, lead, func(o dates.Date) bool { return !done[o.String()] })
		if !found && completedToo {
			// Nothing of this event is open, but a reminder ticked off while it is
			// still current is still one of today's. Only as a FALLBACK, never
			// alongside: while the widget HAS a row for this event, the summary must
			// name that row's date and no other, or one reminder is named under two
			// dates in a single send (D99).
			chosen, found = firstActive(occs, today, lead, func(o dates.Date) bool { return !o.Before(today) })
		}
		if !found {
			continue
		}
		out = append(out, DashboardReminder{
			EventID:      e.ID,
			OccurrenceOn: chosen.String(),
			Title:        e.Title,
			Recurring:    rule != nil,
			ReminderLead: lead,
			Overdue:      chosen.Before(today),
			DaysUntil:    today.DaysUntil(chosen),
			On:           chosen,
		})
	}

	// Overdue first, then the one shared reading order — so the widget and the
	// lists read the same twice in a row rather than in whatever order the
	// event scan happened to return.
	czech := czechCollator()
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Overdue != out[j].Overdue {
			return out[i].Overdue // overdue first
		}
		return lessByDayThenCzechTitle(czech, out[i].On, out[j].On, out[i].Title, out[j].Title)
	})
	return out, nil
}

// firstActive is one event's row: the earliest occurrence keep() accepts, taken
// only once its lead has opened. A later occurrence cannot rescue an event whose
// earliest accepted one is not active yet — subtractLead is monotonic in the
// occurrence, so if that lead has not opened, no later one has either.
func firstActive(occs []dates.Date, today dates.Date, lead string, keep func(dates.Date) bool) (dates.Date, bool) {
	for _, o := range occs {
		if !keep(o) {
			continue
		}
		if today.Before(subtractLead(o, lead)) {
			break // not active yet (today < occurrence − lead)
		}
		return o, true
	}
	return dates.Date{}, false
}

// czechCollator builds the collator every summary sort uses. Czech collation,
// not byte order: a byte compare files every Č/Ř/Š/Ú/Ž title after the whole
// ASCII alphabet, which no household would call "alphabetical". A collator is
// not safe for concurrent use, so each call builds its own — the lists here
// are a handful of lines.
func czechCollator() *collate.Collator { return collate.New(language.Czech) }

// lessByDayThenCzechTitle is the ONE order a household reads reminders in —
// soonest first, then alphabetically, stable between two sends of the same
// summary. Shared by the widget sort and the list sort so the two cannot
// drift apart.
func lessByDayThenCzechTitle(czech *collate.Collator, onI, onJ dates.Date, titleI, titleJ string) bool {
	if !onI.Equal(onJ) {
		return onI.Before(onJ)
	}
	return czech.CompareString(titleI, titleJ) < 0
}

// subtractLead returns occurrence − lead in calendar space (month leads clamp to
// the target month's last day, mirroring D19). This is the date a reminder
// ENTERS the Připomínky widget — and from then until its day passes it is one
// of the rows events.pripominky_today names (D99).
func subtractLead(occ dates.Date, lead string) dates.Date {
	switch lead {
	case "1d":
		return occ.AddDays(-1)
	case "2d":
		return occ.AddDays(-2)
	case "1w":
		return occ.AddDays(-7)
	case "2w":
		return occ.AddDays(-14)
	case "1m":
		y, m := occ.Y, occ.M-1
		if m < 1 {
			m = 12
			y--
		}
		day := occ.D
		if last := dates.DaysInMonth(y, m); day > last {
			day = last
		}
		return dates.New(y, m, day)
	}
	return occ
}
