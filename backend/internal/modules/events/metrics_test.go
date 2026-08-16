package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
)

func pragueLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Prague")
	if err != nil {
		t.Fatalf("load Europe/Prague: %v", err)
	}
	return loc
}

func eventMetrics(t *testing.T, f fixture) metrics.Provider {
	t.Helper()
	return events.NewModule(f.svc, pragueLoc(t), 30).MetricProvider()
}

func TestEventMetricsDescriptors(t *testing.T) {
	f := newFixture(t)
	got := eventMetrics(t, f).Descriptors()

	want := map[string]bool{
		events.MetricPripominkyToday:     true,
		events.MetricPripominkyTodayOpen: true,
		events.MetricOverdueOpen:         true,
		events.MetricDueWithin7d:         true,
	}
	if len(got) != len(want) {
		t.Fatalf("published %d descriptors, want %d", len(got), len(want))
	}
	for _, d := range got {
		if !want[d.Key] {
			t.Errorf("unexpected metric key %q", d.Key)
		}
		// Completion in this module is SHARED (D68 keeps it that way in v5), so
		// every one of these is the same number for the whole household.
		if d.Scope != metrics.ScopeHousehold {
			t.Errorf("%s scope = %q, want household — events completion is shared", d.Key, d.Scope)
		}
	}
}

func TestEventMetricsReminderCounts(t *testing.T) {
	f := newFixture(t)
	p := eventMetrics(t, f)
	ctx := context.Background()
	loc := pragueLoc(t)
	now := time.Now().In(loc)
	today := now.Format("2006-01-02")

	dueToday, err := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "Zaplatit nájem", StartsOn: today, ReminderEnabled: true, ReminderLead: "1d",
	})
	if err != nil {
		t.Fatalf("create today's event: %v", err)
	}
	// A reminder-less event today must not be counted as a reminder.
	if _, err := f.svc.CreateEvent(f.ctx, events.EventCreate{Title: "Bez připomínky", StartsOn: today}); err != nil {
		t.Fatalf("create reminder-less event: %v", err)
	}
	// Something inside the 7-day look-ahead, but not today.
	if _, err := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "Kontrola kotle", StartsOn: now.AddDate(0, 0, 3).Format("2006-01-02"),
	}); err != nil {
		t.Fatalf("create upcoming event: %v", err)
	}

	value := func(key string) int {
		t.Helper()
		v, err := p.Value(ctx, "u1", key, now)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		return v
	}

	if got := value(events.MetricPripominkyToday); got != 1 {
		t.Errorf("pripominky_today = %d, want 1 (only the reminder-enabled event)", got)
	}
	if got := value(events.MetricPripominkyTodayOpen); got != 1 {
		t.Errorf("pripominky_today_open = %d before completion, want 1", got)
	}
	if got := value(events.MetricDueWithin7d); got != 3 {
		t.Errorf("due_within_7d = %d, want 3 (both of today's plus the one in 3 days)", got)
	}

	// Completion is shared: one member marking it done clears it for everyone.
	if _, err := f.svc.Complete(f.ctx, dueToday.ID, today, ""); err != nil {
		t.Fatalf("complete occurrence: %v", err)
	}

	if got := value(events.MetricPripominkyTodayOpen); got != 0 {
		t.Errorf("pripominky_today_open = %d after completion, want 0", got)
	}
	if got := value(events.MetricPripominkyToday); got != 1 {
		t.Errorf("pripominky_today = %d after completion, want 1 (it is still due today)", got)
	}
}

func TestEventMetricsOverdueOpen(t *testing.T) {
	f := newFixture(t)
	p := eventMetrics(t, f)
	ctx := context.Background()
	loc := pragueLoc(t)
	now := time.Now().In(loc)

	if _, err := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "Propásnutá revize", StartsOn: now.AddDate(0, 0, -5).Format("2006-01-02"),
		ReminderEnabled: true, ReminderLead: "1d",
	}); err != nil {
		t.Fatalf("create past event: %v", err)
	}

	got, err := p.Value(ctx, "u1", events.MetricOverdueOpen, now)
	if err != nil {
		t.Fatalf("overdue_open: %v", err)
	}
	if got != 1 {
		t.Errorf("overdue_open = %d, want 1", got)
	}
}

// Shared completion means the count cannot differ between two members.
func TestEventMetricsAreIdenticalForEveryRecipient(t *testing.T) {
	f := newFixture(t)
	p := eventMetrics(t, f)
	ctx := context.Background()
	now := time.Now().In(pragueLoc(t))

	if _, err := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "Společná připomínka", StartsOn: now.Format("2006-01-02"),
		ReminderEnabled: true, ReminderLead: "1d",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	karel, _ := p.Value(ctx, "karel", events.MetricPripominkyTodayOpen, now)
	eva, _ := p.Value(ctx, "eva", events.MetricPripominkyTodayOpen, now)
	if karel != eva || karel != 1 {
		t.Errorf("household metric differed per user: karel=%d eva=%d, want 1 for both", karel, eva)
	}
}

func TestEventMetricsUnknownKey(t *testing.T) {
	f := newFixture(t)
	if _, err := eventMetrics(t, f).Value(context.Background(), "u1", "events.nonsense", time.Now()); err == nil {
		t.Error("expected an error for an unknown metric key")
	}
}

// A RECURRING overdue reminder counts once, not once per missed occurrence.
//
// The Připomínky widget picks each event's earliest uncompleted occurrence and
// renders one row for it, so counting occurrences here would make an 08:00
// summary say "20 připomínek po termínu" for a single daily reminder left alone
// for twenty days, while the dashboard the household then opens lists exactly
// one. The two must not disagree about the same word.
func TestEventMetricsOverdueCountsARecurringReminderOnce(t *testing.T) {
	f := newFixture(t)
	p := eventMetrics(t, f)
	ctx := context.Background()
	loc := pragueLoc(t)
	now := time.Now().In(loc)

	// Weekly since four weeks ago: four uncompleted past occurrences inside the
	// 30-day lookback, all of them the same reminder.
	if _, err := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "Zalít kytky", StartsOn: now.AddDate(0, 0, -28).Format("2006-01-02"),
		RRule: "FREQ=WEEKLY;INTERVAL=1", ReminderEnabled: true, ReminderLead: "1d",
	}); err != nil {
		t.Fatalf("create recurring event: %v", err)
	}

	got, err := p.Value(ctx, "u1", events.MetricOverdueOpen, now)
	if err != nil {
		t.Fatalf("overdue_open: %v", err)
	}
	if got != 1 {
		t.Errorf("overdue_open = %d, want 1 — the widget shows this reminder as a single row", got)
	}

	// A second, unrelated overdue reminder is a second row, and a second count.
	if _, err := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "Propásnutá revize", StartsOn: now.AddDate(0, 0, -3).Format("2006-01-02"),
		ReminderEnabled: true, ReminderLead: "1d",
	}); err != nil {
		t.Fatalf("create second past event: %v", err)
	}
	got, err = p.Value(ctx, "u1", events.MetricOverdueOpen, now)
	if err != nil {
		t.Fatalf("overdue_open: %v", err)
	}
	if got != 2 {
		t.Errorf("overdue_open = %d, want 2 — one per overdue reminder", got)
	}
}
