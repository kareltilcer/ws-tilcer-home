// Package dashboard is the Nástěnka read model: it owns no tables and has one
// endpoint. It aggregates active event reminders and every to-do in a "now"
// column across all boards (FR-N1). Mark-done is not implemented here — the
// frontend reuses the owning modules' endpoints (card move / reminder complete),
// which log under those modules with meta.via="dashboard".
package dashboard

import (
	"context"
	"sort"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/recur"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/todo"
)

// ---- Wire types (match openapi.yaml Dashboard schema) ----

type Dashboard struct {
	Reminders []DashboardReminder `json:"reminders"`
	Tasks     []DashboardTask     `json:"tasks"`
}

type DashboardReminder struct {
	EventID      string `json:"event_id"`
	OccurrenceOn string `json:"occurrence_on"`
	Title        string `json:"title"`
	Recurring    bool   `json:"recurring"`
	ReminderLead string `json:"reminder_lead"`
	Overdue      bool   `json:"overdue"`
	DaysUntil    int    `json:"days_until"`
}

type DashboardTask struct {
	CardID            string                 `json:"card_id"`
	Title             string                 `json:"title"`
	BoardID           string                 `json:"board_id"`
	BoardName         string                 `json:"board_name"`
	ColumnID          string                 `json:"column_id"`
	ColumnName        string                 `json:"column_name"`
	LabelIDs          []string               `json:"label_ids"`
	ChecklistProgress todo.ChecklistProgress `json:"checklist_progress"`
	// DoneColumnID is the board's first kind=done column (additive to openapi) so
	// the client can mark-done via the card-move endpoint without a second
	// request; null ⇒ the board has no done column, so mark-done archives (D15).
	DoneColumnID *string `json:"done_column_id"`
}

// Service computes the dashboard from the todo and events read stores. "Today"
// is evaluated via todayFn (in HOME_TIMEZONE in production, injected in tests).
type Service struct {
	todoStore    *todo.Store
	evStore      *events.Store
	lookbackDays int
	maxOcc       int
	todayFn      func() dates.Date
}

// NewService builds the dashboard service; "today" is computed in loc.
func NewService(todoStore *todo.Store, evStore *events.Store, loc *time.Location, lookbackDays, maxOcc int) *Service {
	return &Service{
		todoStore:    todoStore,
		evStore:      evStore,
		lookbackDays: lookbackDays,
		maxOcc:       maxOcc,
		todayFn:      func() dates.Date { return dates.Today(loc) },
	}
}

// NewServiceWithToday builds a service with a fixed "today" — for deterministic
// tests of the activation boundary and lookback bound.
func NewServiceWithToday(todoStore *todo.Store, evStore *events.Store, lookbackDays, maxOcc int, today dates.Date) *Service {
	return &Service{
		todoStore:    todoStore,
		evStore:      evStore,
		lookbackDays: lookbackDays,
		maxOcc:       maxOcc,
		todayFn:      func() dates.Date { return today },
	}
}

// Dashboard returns both computed lists, active items only (D16).
func (s *Service) Dashboard(ctx context.Context) (Dashboard, error) {
	today := s.todayFn()
	reminders, err := s.reminders(ctx, today)
	if err != nil {
		return Dashboard{}, err
	}
	tasks, err := s.tasks(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	return Dashboard{Reminders: reminders, Tasks: tasks}, nil
}

// reminders computes at most one active reminder per event: the earliest
// uncompleted occurrence within the lookback window, shown only once
// today >= occurrence - lead. Sorted overdue-first, then by date ascending.
func (s *Service) reminders(ctx context.Context, today dates.Date) ([]DashboardReminder, error) {
	evs, err := s.evStore.ListForWindow(ctx, false)
	if err != nil {
		return nil, err
	}
	var withReminder []events.Event
	var ids []string
	for _, e := range evs {
		if e.ReminderEnabled && e.ReminderLead != nil {
			withReminder = append(withReminder, e)
			ids = append(ids, e.ID)
		}
	}
	completions, err := s.evStore.CompletionsFor(ctx, ids)
	if err != nil {
		return nil, err
	}

	// Window: from lookback in the past to just past the maximum lead ahead, so
	// both overdue and soon-to-be-active occurrences are captured, bounded.
	from := today.AddDays(-s.lookbackDays)
	to := today.AddDays(maxLeadDays)

	out := []DashboardReminder{}
	for _, e := range withReminder {
		rule, err := recur.Parse(strOr(e.RRule))
		if err != nil {
			continue // unparseable stored rule — skip rather than fail the page
		}
		anchor, err := dates.Parse(e.StartsOn)
		if err != nil {
			continue
		}
		occs := recur.Expand(anchor, rule, from, to, s.maxOcc)

		// Earliest uncompleted occurrence in the window.
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
			continue // not active yet (today < occurrence - lead)
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
	return out, nil
}

func (s *Service) tasks(ctx context.Context) ([]DashboardTask, error) {
	cards, err := s.todoStore.NowCards(ctx)
	if err != nil {
		return nil, err
	}
	doneCols, err := s.todoStore.FirstDoneColumns(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]DashboardTask, 0, len(cards))
	for _, c := range cards {
		var doneCol *string
		if id, ok := doneCols[c.BoardID]; ok {
			d := id
			doneCol = &d
		}
		out = append(out, DashboardTask{
			CardID:            c.CardID,
			Title:             c.Title,
			BoardID:           c.BoardID,
			BoardName:         c.BoardName,
			ColumnID:          c.ColumnID,
			ColumnName:        c.ColumnName,
			LabelIDs:          c.LabelIDs,
			ChecklistProgress: c.ChecklistProgress,
			DoneColumnID:      doneCol,
		})
	}
	return out, nil
}

// maxLeadDays bounds how far ahead a reminder can be active (max lead ≈ 1 month),
// with a margin — keeps the reminder expansion window bounded.
const maxLeadDays = 40

// subtractLead returns occurrence − lead, evaluated in calendar space.
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

func strOr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
