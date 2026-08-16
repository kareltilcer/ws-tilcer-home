package events_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
)

func eventLists(t *testing.T, f fixture) lists.Provider {
	t.Helper()
	return events.NewModule(f.svc, pragueLoc(t), 30).ListProvider()
}

func TestEventListsDescriptors(t *testing.T) {
	f := newFixture(t)
	got := eventLists(t, f).Descriptors()

	want := map[string]bool{
		events.ListPripominkyToday:     true,
		events.ListPripominkyTodayOpen: true,
		events.ListOverdueOpen:         true,
		events.ListDueWithin7d:         true,
	}
	if len(got) != len(want) {
		t.Fatalf("published %d descriptors, want %d", len(got), len(want))
	}
	for _, d := range got {
		if !want[d.Key] {
			t.Errorf("unexpected list key %q", d.Key)
		}
		if d.Scope != lists.ScopeHousehold {
			t.Errorf("%s scope = %q, want household — events completion is shared", d.Key, d.Scope)
		}
		// An empty list must have something to say; the sender's placeholder means
		// "we could not tell you", which is a different message.
		if strings.TrimSpace(d.Empty) == "" {
			t.Errorf("%s published no empty text", d.Key)
		}
	}
}

// The list names exactly what the metric counts — that is the whole point of
// sharing a key between the two catalogs.
func TestEventListsNameWhatTheMetricsCount(t *testing.T) {
	f := newFixture(t)
	mod := events.NewModule(f.svc, pragueLoc(t), 30)
	ctx := context.Background()
	now := time.Now().In(pragueLoc(t))
	today := now.Format("2006-01-02")

	dueToday, err := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "Zaplatit nájem", StartsOn: today, ReminderEnabled: true, ReminderLead: "1d",
	})
	if err != nil {
		t.Fatalf("create today's event: %v", err)
	}
	if _, err := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "Vynést koš", StartsOn: today, ReminderEnabled: true, ReminderLead: "1d",
	}); err != nil {
		t.Fatalf("create second event: %v", err)
	}
	// A reminder-less event today is not a "připomínka" for either catalog.
	if _, err := f.svc.CreateEvent(f.ctx, events.EventCreate{Title: "Bez připomínky", StartsOn: today}); err != nil {
		t.Fatalf("create reminder-less event: %v", err)
	}

	items, err := mod.ListProvider().Items(ctx, "u1", events.ListPripominkyToday, now)
	if err != nil {
		t.Fatalf("pripominky_today: %v", err)
	}
	if got := strings.Join(items, "|"); got != "Vynést koš|Zaplatit nájem" {
		t.Errorf("items = %q, want both reminders sorted by title within the day", got)
	}
	count, err := mod.MetricProvider().Value(ctx, "u1", events.MetricPripominkyToday, now)
	if err != nil {
		t.Fatalf("pripominky_today metric: %v", err)
	}
	if count != len(items) {
		t.Errorf("metric says %d, the list names %d — the two must agree", count, len(items))
	}

	// Completion is shared, so ticking one off shortens the open list for everyone.
	if _, err := f.svc.Complete(f.ctx, dueToday.ID, today, ""); err != nil {
		t.Fatalf("complete occurrence: %v", err)
	}
	open, err := mod.ListProvider().Items(ctx, "u1", events.ListPripominkyTodayOpen, now)
	if err != nil {
		t.Fatalf("pripominky_today_open: %v", err)
	}
	if strings.Join(open, "|") != "Vynést koš" {
		t.Errorf("open items = %v, want only the uncompleted one", open)
	}
}

// A multi-day window dates every line — "Kontrola kotle" alone says nothing
// about when, and an overdue one has to say how long it has been waiting.
func TestEventListsDateTheirItemsOverAMultiDayWindow(t *testing.T) {
	f := newFixture(t)
	p := eventLists(t, f)
	ctx := context.Background()
	now := time.Now().In(pragueLoc(t))
	in3 := now.AddDate(0, 0, 3)
	ago5 := now.AddDate(0, 0, -5)

	if _, err := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "Kontrola kotle", StartsOn: in3.Format("2006-01-02"),
	}); err != nil {
		t.Fatalf("create upcoming: %v", err)
	}
	if _, err := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "Propásnutá revize", StartsOn: ago5.Format("2006-01-02"),
		ReminderEnabled: true, ReminderLead: "1d",
	}); err != nil {
		t.Fatalf("create past: %v", err)
	}

	upcoming, err := p.Items(ctx, "u1", events.ListDueWithin7d, now)
	if err != nil {
		t.Fatalf("due_within_7d: %v", err)
	}
	wantUpcoming := "Kontrola kotle (" + czDay(in3) + ")"
	if len(upcoming) != 1 || upcoming[0] != wantUpcoming {
		t.Errorf("upcoming = %v, want [%q]", upcoming, wantUpcoming)
	}

	overdue, err := p.Items(ctx, "u1", events.ListOverdueOpen, now)
	if err != nil {
		t.Fatalf("overdue_open: %v", err)
	}
	wantOverdue := "Propásnutá revize (" + czDay(ago5) + ")"
	if len(overdue) != 1 || overdue[0] != wantOverdue {
		t.Errorf("overdue = %v, want [%q]", overdue, wantOverdue)
	}
}

// A recurring overdue reminder is ONE line, as it is one row on the dashboard
// and one in the count.
func TestEventListsNameARecurringOverdueReminderOnce(t *testing.T) {
	f := newFixture(t)
	p := eventLists(t, f)
	now := time.Now().In(pragueLoc(t))

	if _, err := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "Zalít kytky", StartsOn: now.AddDate(0, 0, -28).Format("2006-01-02"),
		RRule: "FREQ=WEEKLY;INTERVAL=1", ReminderEnabled: true, ReminderLead: "1d",
	}); err != nil {
		t.Fatalf("create recurring: %v", err)
	}

	items, err := p.Items(context.Background(), "u1", events.ListOverdueOpen, now)
	if err != nil {
		t.Fatalf("overdue_open: %v", err)
	}
	if len(items) != 1 || !strings.HasPrefix(items[0], "Zalít kytky") {
		t.Errorf("items = %v, want the reminder named once", items)
	}
}

func TestEventListsUnknownKey(t *testing.T) {
	f := newFixture(t)
	if _, err := eventLists(t, f).Items(context.Background(), "u1", "events.nonsense", time.Now()); err == nil {
		t.Error("expected an error for an unknown list key")
	}
}

// czDay renders the "d. M." suffix the list provider appends.
func czDay(t time.Time) string { return fmt.Sprintf("%d. %d.", t.Day(), int(t.Month())) }
