package events

import (
	"context"
	"sort"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/recur"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

// TentoMesicWidget is the events.tento-mesic payload (openapi TentoMesicWidget):
// a read-only look-ahead list of occurrences.
type TentoMesicWidget struct {
	Occurrences []Occurrence `json:"occurrences"`
}

// tentoMesicProvider implements FR-E8: upcoming occurrences from today through
// the end of the current month, sorted by date. Read-only — no completion
// control. This is the look-ahead that relaxes v1 D16 (widgets set their own
// scope).
type tentoMesicProvider struct {
	store  *Store
	maxOcc int
	today  func() dates.Date
}

func newTentoMesicProvider(store *Store, loc *time.Location, maxOcc int) *tentoMesicProvider {
	return &tentoMesicProvider{
		store:  store,
		maxOcc: maxOcc,
		today:  func() dates.Date { return dates.Today(loc) },
	}
}

func (p *tentoMesicProvider) Key() string    { return "events.tento-mesic" }
func (p *tentoMesicProvider) Title() string  { return "Tento měsíc" }
func (p *tentoMesicProvider) Module() string { return "events" }
func (p *tentoMesicProvider) Description() string {
	return "Nadcházející události do konce měsíce."
}
func (p *tentoMesicProvider) DefaultSize() string { return "narrow" }
func (p *tentoMesicProvider) AdminOnly() bool     { return false }

func (p *tentoMesicProvider) Data(ctx context.Context, _ registry.User) (any, error) {
	today := p.today()
	end := dates.New(today.Y, today.M, dates.DaysInMonth(today.Y, today.M))

	evs, err := p.store.ListForWindow(ctx, false)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(evs))
	for i, e := range evs {
		ids[i] = e.ID
	}
	completions, err := p.store.CompletionsFor(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := []Occurrence{}
	for i := range evs {
		e := evs[i]
		rule, anchor, err := parseSeries(&e)
		if err != nil {
			continue
		}
		for _, o := range recur.Expand(anchor, rule, today, end, p.maxOcc) {
			on := o.String()
			out = append(out, Occurrence{
				EventID:           e.ID,
				OccurrenceOn:      on,
				Title:             e.Title,
				Description:       e.Description,
				Recurring:         rule != nil,
				ReminderEnabled:   e.ReminderEnabled,
				ReminderLead:      e.ReminderLead,
				ReminderCompleted: completions[e.ID][on],
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OccurrenceOn != out[j].OccurrenceOn {
			return out[i].OccurrenceOn < out[j].OccurrenceOn
		}
		return out[i].Title < out[j].Title
	})
	return TentoMesicWidget{Occurrences: out}, nil
}
