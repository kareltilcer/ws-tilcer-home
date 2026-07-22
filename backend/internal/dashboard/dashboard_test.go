package dashboard_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/dashboard"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/testsupport"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/todo"
)

type env struct {
	db        *sql.DB
	todoSvc   *todo.Service
	eventsSvc *events.Service
	ctx       context.Context
}

func newEnv(t *testing.T) env {
	t.Helper()
	db := testsupport.NewDB(t)
	return env{
		db:        db,
		todoSvc:   todo.NewService(db, audit.NewSink(), nil),
		eventsSvc: events.NewService(db, audit.NewSink(), nil, 500, 24),
		ctx:       testsupport.CtxUser("u", "editor"),
	}
}

func (e env) dash(today dates.Date) *dashboard.Service {
	return dashboard.NewServiceWithToday(e.todoSvc.Store(), e.eventsSvc.Store(), 30, 500, today)
}

func (e env) board(t *testing.T, name string) *todo.Board {
	t.Helper()
	b, err := e.todoSvc.CreateBoard(e.ctx, todo.BoardCreate{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (e env) column(t *testing.T, boardID, name, kind string) *todo.Column {
	t.Helper()
	c, err := e.todoSvc.CreateColumn(e.ctx, boardID, todo.ColumnCreate{Name: name, Kind: kind})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (e env) card(t *testing.T, colID, title string) {
	t.Helper()
	if _, err := e.todoSvc.CreateCard(e.ctx, colID, todo.CardCreate{Title: title}); err != nil {
		t.Fatal(err)
	}
}

func TestTasks_CrossBoardWithDoneColumnResolution(t *testing.T) {
	e := newEnv(t)

	// Board A: a now column (+ a done column) with one card, plus a SECOND now
	// column with another card (multiple now columns must all contribute).
	a := e.board(t, "Domácnost")
	aNow := e.column(t, a.ID, "Právě dělám", todo.KindNow)
	aDone := e.column(t, a.ID, "Hotovo", todo.KindDone)
	aNow2 := e.column(t, a.ID, "Dnes", todo.KindNow)
	e.card(t, aNow.ID, "A-1")
	e.card(t, aNow2.ID, "A-2")

	// Board B: a now column but NO done column.
	b := e.board(t, "Chata")
	bNow := e.column(t, b.ID, "Právě dělám", todo.KindNow)
	e.card(t, bNow.ID, "B-1")

	// A normal-column card must not appear.
	aBacklog := e.column(t, a.ID, "Zásobník", todo.KindNormal)
	e.card(t, aBacklog.ID, "A-backlog")

	d, err := e.dash(dates.New(2026, 7, 15)).Dashboard(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tasks) != 3 {
		t.Fatalf("tasks = %d, want 3 (2 boards, 3 now-cards)", len(d.Tasks))
	}
	titles := map[string]dashboard.DashboardTask{}
	for _, tk := range d.Tasks {
		titles[tk.Title] = tk
	}
	// Board A cards resolve to A's done column; board B card has none (archive path).
	if titles["A-1"].DoneColumnID == nil || *titles["A-1"].DoneColumnID != aDone.ID {
		t.Errorf("A-1 done_column_id = %v, want %s", titles["A-1"].DoneColumnID, aDone.ID)
	}
	if titles["A-2"].DoneColumnID == nil || *titles["A-2"].DoneColumnID != aDone.ID {
		t.Errorf("A-2 done_column_id = %v, want %s", titles["A-2"].DoneColumnID, aDone.ID)
	}
	if titles["B-1"].DoneColumnID != nil {
		t.Errorf("B-1 done_column_id = %v, want nil (no done column)", titles["B-1"].DoneColumnID)
	}
	if titles["A-1"].BoardName != "Domácnost" || titles["B-1"].BoardName != "Chata" {
		t.Error("tasks should carry their board name for grouping")
	}
}

func TestOneReminderPerEvent_AndAdvanceOnCompletion(t *testing.T) {
	e := newEnv(t)
	today := dates.New(2026, 7, 15)
	// Monthly on the 15th, anchored two months back; lead 1 week. Occurrences
	// May15, Jun15, Jul15 — Jun15 and Jul15 are within the 30-day lookback.
	ev, err := e.eventsSvc.CreateEvent(e.ctx, events.EventCreate{
		Title: "Nájem", StartsOn: "2026-05-15", RRule: "FREQ=MONTHLY;INTERVAL=1",
		ReminderEnabled: true, ReminderLead: "1w",
	})
	if err != nil {
		t.Fatal(err)
	}

	d, _ := e.dash(today).Dashboard(e.ctx)
	if len(d.Reminders) != 1 {
		t.Fatalf("reminders = %d, want exactly 1 (earliest uncompleted)", len(d.Reminders))
	}
	if d.Reminders[0].OccurrenceOn != "2026-06-15" || !d.Reminders[0].Overdue {
		t.Errorf("reminder = %+v, want 2026-06-15 overdue", d.Reminders[0])
	}

	// Complete the current (Jun 15) occurrence → the next (Jul 15) becomes live.
	if _, err := e.eventsSvc.Complete(e.ctx, ev.ID, "2026-06-15", ""); err != nil {
		t.Fatal(err)
	}
	d, _ = e.dash(today).Dashboard(e.ctx)
	if len(d.Reminders) != 1 || d.Reminders[0].OccurrenceOn != "2026-07-15" {
		t.Fatalf("after completion reminder = %+v, want 2026-07-15", d.Reminders)
	}
	if d.Reminders[0].Overdue {
		t.Error("2026-07-15 on 2026-07-15 is not overdue")
	}
}

func TestActivationBoundary(t *testing.T) {
	e := newEnv(t)
	// One-off event on 2026-07-22 with a 1-week lead → active from 2026-07-15.
	if _, err := e.eventsSvc.CreateEvent(e.ctx, events.EventCreate{
		Title: "Očkování psa", StartsOn: "2026-07-22",
		ReminderEnabled: true, ReminderLead: "1w",
	}); err != nil {
		t.Fatal(err)
	}

	// Day −8 (2026-07-14): not yet active.
	d, _ := e.dash(dates.New(2026, 7, 14)).Dashboard(e.ctx)
	if len(d.Reminders) != 0 {
		t.Errorf("on day −8 reminders = %d, want 0", len(d.Reminders))
	}
	// Day −7 (2026-07-15): active.
	d, _ = e.dash(dates.New(2026, 7, 15)).Dashboard(e.ctx)
	if len(d.Reminders) != 1 {
		t.Errorf("on day −7 reminders = %d, want 1", len(d.Reminders))
	}
}

func TestLookbackBound(t *testing.T) {
	e := newEnv(t)
	// One-off 44 days before "today" (2026-07-15), uncompleted. Older than the
	// 30-day lookback → drops off rather than accumulating.
	if _, err := e.eventsSvc.CreateEvent(e.ctx, events.EventCreate{
		Title: "Stará připomínka", StartsOn: "2026-06-01",
		ReminderEnabled: true, ReminderLead: "1w",
	}); err != nil {
		t.Fatal(err)
	}
	d, _ := e.dash(dates.New(2026, 7, 15)).Dashboard(e.ctx)
	if len(d.Reminders) != 0 {
		t.Errorf("reminders = %d, want 0 (older than lookback)", len(d.Reminders))
	}
}
