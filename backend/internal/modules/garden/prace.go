package garden

import (
	"context"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

// THE ONE WIDGET (PRD FR-G16, D123).
//
// There is deliberately no second widget. Harvest surfaces here as a `harvest`
// task rather than as a "Ke sklizni" card that would be dead weight from
// November to May — and the widget's own quiet state is designed as a correct
// answer, not as an empty one, because from November to February that IS its
// normal state.

// Widget states.
const (
	stateWork  = "work"
	stateQuiet = "quiet"
)

// PraceWidget is the garden.prace payload.
type PraceWidget struct {
	State string `json:"state"`
	// Overdue comes first and separately: work whose window has passed is a
	// different question from work coming up, and merging them into one sorted
	// list buries it.
	Overdue []PraceItem  `json:"overdue,omitempty"`
	Weeks   []PraceWeek  `json:"weeks,omitempty"`
	Total   int          `json:"total"`
}

// PraceWeek groups upcoming work by ISO week, which is how gardening is planned
// and how the print sheet is laid out.
type PraceWeek struct {
	Week    string      `json:"week"`     // "12/2027"
	FromISO string      `json:"from"`     // Monday, for the heading
	Items   []PraceItem `json:"items"`
}

// PraceItem is one line. It carries the crop and the bed because "Výsadba" on
// its own tells you nothing when six things need planting.
type PraceItem struct {
	ID         string  `json:"id"`
	TitleCS    string  `json:"title_cs"`
	Kind       string  `json:"kind"`
	BedCode    *string `json:"bed_code"`
	PlantName  *string `json:"plant_name"`
	WindowFrom string  `json:"window_from"`
	WindowTo   string  `json:"window_to"`
	WindowCS   string  `json:"window_cs"` // "15.–25. 5." — a window, never a single date
	Overdue    bool    `json:"overdue"`
}

// praceHorizonDays is how far ahead the widget looks.
const praceHorizonDays = 30

type praceProvider struct{ svc *Service }

func (p *praceProvider) Key() string    { return "garden.prace" }
func (p *praceProvider) Title() string  { return "Práce na zahradě" }
func (p *praceProvider) Module() string { return "garden" }
func (p *praceProvider) Description() string {
	return "Co je potřeba udělat na zahradě"
}
func (p *praceProvider) DefaultSize() string { return "wide" }
func (p *praceProvider) AdminOnly() bool     { return false }

// Data returns the next thirty days of work.
//
// The window filter is an OVERLAP, not a containment: a job that started last
// week and runs until Friday is exactly the job you most need to see, and a
// "starts within the horizon" filter would drop it on the day it became urgent.
//
// Reads are open to every role including `reader`, who sees the widget and can
// change nothing — ticking a task off is an ordinary write (D124).
func (p *praceProvider) Data(ctx context.Context, _ registry.User) (any, error) {
	s := p.svc
	today := s.today()
	// Exhaustive: a task-heavy May can overrun a single page, and a widget that
	// silently stops mid-week reads as "nothing else to do" — the exact lie the
	// quiet state exists to avoid telling.
	tasks, err := s.store.allTasks(ctx, nil, TaskFilter{
		From: today.String(), To: today.AddDays(praceHorizonDays).String(), Status: TaskOpen,
	})
	if err != nil {
		return nil, err
	}
	// Overdue work is not inside the horizon by definition, so it is fetched
	// separately rather than by widening the window and hoping. ToEnd, not To:
	// a job still mid-window belongs to a week group above, not here.
	overdue, err := s.store.allTasks(ctx, nil, TaskFilter{
		ToEnd: today.AddDays(-1).String(), Status: TaskOpen,
	})
	if err != nil {
		return nil, err
	}

	out := PraceWidget{State: stateWork}
	for _, t := range overdue {
		out.Overdue = append(out.Overdue, praceItem(t, true))
	}

	byWeek := map[string][]PraceItem{}
	var order []string
	for _, t := range tasks {
		from, err := dates.Parse(t.WindowFrom)
		if err != nil {
			continue
		}
		// A task whose window opened before today belongs to THIS week, not to
		// the week it started in — otherwise the dashboard shows a heading for a
		// week that is already over.
		if from.Before(today) {
			from = today
		}
		y, w := isoWeekOf(from)
		key := isoKey(y, w)
		if _, seen := byWeek[key]; !seen {
			order = append(order, key)
		}
		byWeek[key] = append(byWeek[key], praceItem(t, false))
	}
	for _, key := range order {
		y, w := parseISOKey(key)
		out.Weeks = append(out.Weeks, PraceWeek{
			Week:    weekLabelCS(key),
			FromISO: isoWeekMonday(y, w).String(),
			Items:   byWeek[key],
		})
	}
	out.Total = len(out.Overdue)
	for _, wk := range out.Weeks {
		out.Total += len(wk.Items)
	}
	// "Na zahradě je teď klid" is a correct answer, not a failure — say so with
	// an explicit state rather than an empty array the frontend has to interpret.
	if out.Total == 0 {
		return PraceWidget{State: stateQuiet}, nil
	}
	return out, nil
}

func praceItem(t Task, overdue bool) PraceItem {
	item := PraceItem{
		ID: t.ID, TitleCS: t.TitleCS, Kind: t.Kind, BedCode: t.BedCode, PlantName: t.PlantName,
		WindowFrom: t.WindowFrom, WindowTo: t.WindowTo, Overdue: overdue,
	}
	from, errFrom := dates.Parse(t.WindowFrom)
	to, errTo := dates.Parse(t.WindowTo)
	if errFrom == nil && errTo == nil {
		item.WindowCS = czRange(from, to)
	}
	return item
}

func isoKey(year, week int) string {
	w := "0" + itoaSmall(week)
	return itoaYear(year) + "-W" + w[len(w)-2:]
}

func parseISOKey(key string) (year, week int) {
	if len(key) != 8 {
		return 0, 0
	}
	for _, c := range key[:4] {
		year = year*10 + int(c-'0')
	}
	for _, c := range key[6:] {
		week = week*10 + int(c-'0')
	}
	return year, week
}

func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// itoaYear renders a four-digit year (also used by the check tests' fixtures).
func itoaYear(y int) string {
	out := []byte("0000")
	for i := 3; i >= 0; i-- {
		out[i] = byte('0' + y%10)
		y /= 10
	}
	return string(out)
}
