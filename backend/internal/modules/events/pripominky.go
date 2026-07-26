package events

import (
	"context"
	"sort"
	"time"

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
	today := p.today()
	evs, err := p.store.ListForWindow(ctx, false)
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
	completions, err := p.store.CompletionsFor(ctx, ids)
	if err != nil {
		return nil, err
	}

	from := today.AddDays(-p.lookbackDays)
	to := today.AddDays(maxLeadDays)

	out := []DashboardReminder{}
	for _, e := range withReminder {
		rule, anchor, err := parseSeries(&e)
		if err != nil {
			continue // unparseable stored rule — skip rather than fail the widget
		}
		occs := recur.Expand(anchor, rule, from, to, p.maxOcc)
		var chosen dates.Date
		found := false
		for _, o := range occs {
			if !completions[e.ID][o.String()] {
				chosen = o
				found = true
				break
			}
		}
		if !found {
			continue
		}
		lead := *e.ReminderLead
		if today.Before(subtractLead(chosen, lead)) {
			continue // not active yet (today < occurrence − lead)
		}
		out = append(out, DashboardReminder{
			EventID:      e.ID,
			OccurrenceOn: chosen.String(),
			Title:        e.Title,
			Recurring:    rule != nil,
			ReminderLead: lead,
			Overdue:      chosen.Before(today),
			DaysUntil:    today.DaysUntil(chosen),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Overdue != out[j].Overdue {
			return out[i].Overdue // overdue first
		}
		return out[i].OccurrenceOn < out[j].OccurrenceOn
	})
	return PripominkyWidget{Reminders: out}, nil
}

// subtractLead returns occurrence − lead in calendar space (month leads clamp to
// the target month's last day, mirroring D19).
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
