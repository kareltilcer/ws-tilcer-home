package events

import (
	"context"
	"sort"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/recur"
)

// Occurrences expands every non-archived event's recurrence within [from, to]
// and returns them grouped by month, ascending (FR-E5). Window-bounded and
// capped per event; occurrences are computed, never stored.
func (s *Service) Occurrences(ctx context.Context, fromStr, toStr string, includeArchived bool) (OccurrenceMonths, error) {
	from, err := dates.Parse(fromStr)
	if err != nil {
		return OccurrenceMonths{}, httpx.ErrUnprocessable("from must be a YYYY-MM-DD date")
	}
	to, err := dates.Parse(toStr)
	if err != nil {
		return OccurrenceMonths{}, httpx.ErrUnprocessable("to must be a YYYY-MM-DD date")
	}
	if to.Before(from) {
		return OccurrenceMonths{}, httpx.ErrUnprocessable("to must not be before from")
	}
	// Bound the window span (inclusive month count) to guard open-ended series.
	spanMonths := (to.Y-from.Y)*12 + (int(to.M) - int(from.M)) + 1
	if spanMonths > s.maxWindowMonths {
		return OccurrenceMonths{}, httpx.ErrUnprocessable("window is wider than the permitted span")
	}

	evs, err := s.store.ListForWindow(ctx, includeArchived)
	if err != nil {
		return OccurrenceMonths{}, err
	}
	ids := make([]string, len(evs))
	for i, e := range evs {
		ids[i] = e.ID
	}
	completions, err := s.store.CompletionsFor(ctx, ids)
	if err != nil {
		return OccurrenceMonths{}, err
	}

	byMonth := map[string][]Occurrence{}
	for i := range evs {
		e := evs[i]
		rule, anchor, err := parseSeries(&e)
		if err != nil {
			// A stored rule that no longer parses is a data problem, not a client
			// error; skip the event rather than failing the whole window.
			continue
		}
		for _, occ := range recur.Expand(anchor, rule, from, to, s.maxOccurrences) {
			on := occ.String()
			month := on[:7] // YYYY-MM
			byMonth[month] = append(byMonth[month], Occurrence{
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

	months := make([]string, 0, len(byMonth))
	for m := range byMonth {
		months = append(months, m)
	}
	sort.Strings(months)

	out := OccurrenceMonths{Months: make([]OccurrenceMonth, 0, len(months))}
	for _, m := range months {
		occs := byMonth[m]
		sort.Slice(occs, func(i, j int) bool {
			if occs[i].OccurrenceOn != occs[j].OccurrenceOn {
				return occs[i].OccurrenceOn < occs[j].OccurrenceOn
			}
			return occs[i].Title < occs[j].Title
		})
		out.Months = append(out.Months, OccurrenceMonth{Month: m, Occurrences: occs})
	}
	return out, nil
}
