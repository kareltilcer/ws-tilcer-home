package events_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/testsupport"
)

type fixture struct {
	db  *sql.DB
	svc *events.Service
	ctx context.Context
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := testsupport.NewDB(t)
	return fixture{
		db:  db,
		svc: events.NewService(db, audit.NewSink(), nil, 500, 24),
		ctx: testsupport.CtxUser("editor-1", "editor"),
	}
}

func TestCreateAndGetEvent(t *testing.T) {
	f := newFixture(t)
	ev, err := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "Zaplatit plyn", StartsOn: "2026-07-31",
		RRule: "FREQ=MONTHLY;INTERVAL=1", ReminderEnabled: true, ReminderLead: "1w",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ev.RRule == nil || *ev.RRule != "FREQ=MONTHLY;INTERVAL=1" {
		t.Errorf("rrule = %v, want canonical monthly", ev.RRule)
	}
	got, err := f.svc.GetEvent(f.ctx, ev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Zaplatit plyn" {
		t.Errorf("title = %q", got.Title)
	}
}

func TestReminderWithoutLeadRejected(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "X", StartsOn: "2026-07-01", ReminderEnabled: true, // no lead
	})
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 422 {
		t.Fatalf("err = %v, want 422", err)
	}
}

func TestBadRRuleRejected(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.CreateEvent(f.ctx, events.EventCreate{Title: "X", StartsOn: "2026-07-01", RRule: "FREQ=DAILY"})
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 422 {
		t.Fatalf("err = %v, want 422", err)
	}
}

func TestOccurrences_GroupedByMonth(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "Nájem", StartsOn: "2026-07-31", RRule: "FREQ=MONTHLY;INTERVAL=1",
	}); err != nil {
		t.Fatal(err)
	}
	months, err := f.svc.Occurrences(f.ctx, "2026-07-01", "2026-09-30", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(months.Months) != 3 {
		t.Fatalf("months = %d, want 3", len(months.Months))
	}
	want := map[string]string{"2026-07": "2026-07-31", "2026-08": "2026-08-31", "2026-09": "2026-09-30"}
	for _, m := range months.Months {
		if len(m.Occurrences) != 1 {
			t.Fatalf("month %s has %d occurrences", m.Month, len(m.Occurrences))
		}
		if m.Occurrences[0].OccurrenceOn != want[m.Month] {
			t.Errorf("month %s occurrence = %s, want %s", m.Month, m.Occurrences[0].OccurrenceOn, want[m.Month])
		}
		if !m.Occurrences[0].Recurring {
			t.Error("occurrence should be flagged recurring")
		}
	}

	// Nothing is persisted per occurrence.
	var completions int
	f.db.QueryRow(`SELECT COUNT(*) FROM event_reminder_completions`).Scan(&completions)
	if completions != 0 {
		t.Errorf("expansion persisted %d completion rows, want 0", completions)
	}
	assertNoOccurrencesTable(t, f.db)
}

func TestOccurrences_WindowTooWide(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Occurrences(f.ctx, "2026-01-01", "2030-01-01", false) // ~48 months > 24
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 422 {
		t.Fatalf("err = %v, want 422", err)
	}
}

func TestComplete_IdempotentUndoAndBogus(t *testing.T) {
	f := newFixture(t)
	ev, _ := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "Odečet vody", StartsOn: "2026-07-15", RRule: "FREQ=MONTHLY;INTERVAL=1",
		ReminderEnabled: true, ReminderLead: "2d",
	})

	// Bogus occurrence (15th is the anchor; 16th is not an occurrence).
	if _, err := f.svc.Complete(f.ctx, ev.ID, "2026-08-16", ""); err == nil {
		t.Error("completing a non-occurrence should fail 422")
	}

	// Complete a real occurrence, twice → one row (idempotent).
	if _, err := f.svc.Complete(f.ctx, ev.ID, "2026-08-15", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Complete(f.ctx, ev.ID, "2026-08-15", ""); err != nil {
		t.Fatal(err)
	}
	if n := completionCount(t, f.db, ev.ID); n != 1 {
		t.Fatalf("completions = %d, want 1 (idempotent)", n)
	}
	// The completion self-logged exactly once (only the first insert logs).
	if n := auditCount(t, f.db, "reminder.complete"); n != 1 {
		t.Errorf("reminder.complete events = %d, want 1", n)
	}

	// Undo removes it.
	if err := f.svc.Uncomplete(f.ctx, ev.ID, "2026-08-15"); err != nil {
		t.Fatal(err)
	}
	if n := completionCount(t, f.db, ev.ID); n != 0 {
		t.Errorf("completions after undo = %d, want 0", n)
	}
}

func TestOccurrences_ReflectsCompletion(t *testing.T) {
	f := newFixture(t)
	ev, _ := f.svc.CreateEvent(f.ctx, events.EventCreate{
		Title: "Servis kotle", StartsOn: "2026-07-10", RRule: "FREQ=MONTHLY;INTERVAL=1",
		ReminderEnabled: true, ReminderLead: "1w",
	})
	if _, err := f.svc.Complete(f.ctx, ev.ID, "2026-07-10", ""); err != nil {
		t.Fatal(err)
	}
	months, _ := f.svc.Occurrences(f.ctx, "2026-07-01", "2026-08-31", false)
	for _, m := range months.Months {
		for _, o := range m.Occurrences {
			wantCompleted := o.OccurrenceOn == "2026-07-10"
			if o.ReminderCompleted != wantCompleted {
				t.Errorf("occurrence %s completed=%v, want %v", o.OccurrenceOn, o.ReminderCompleted, wantCompleted)
			}
		}
	}
}

func TestDeleteHardCascadesLinksAndCompletions(t *testing.T) {
	f := newFixture(t)
	ev, _ := f.svc.CreateEvent(f.ctx, events.EventCreate{Title: "X", StartsOn: "2026-07-10"})
	if _, err := f.svc.CreateLink(f.ctx, ev.ID, events.EventLinkCreate{URL: "https://example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Complete(f.ctx, ev.ID, "2026-07-10", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteEvent(f.ctx, ev.ID, true); err != nil {
		t.Fatal(err)
	}
	var links, comps int
	f.db.QueryRow(`SELECT COUNT(*) FROM event_links WHERE event_id=?`, ev.ID).Scan(&links)
	f.db.QueryRow(`SELECT COUNT(*) FROM event_reminder_completions WHERE event_id=?`, ev.ID).Scan(&comps)
	if links != 0 || comps != 0 {
		t.Errorf("after hard delete links=%d comps=%d, want 0/0", links, comps)
	}
}

func TestAudit_EventCreateHasDiff(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.CreateEvent(f.ctx, events.EventCreate{Title: "Narozeniny", StartsOn: "2026-07-10"}); err != nil {
		t.Fatal(err)
	}
	var newVal string
	err := f.db.QueryRow(`
		SELECT ac.new_value FROM audit_changes ac JOIN audit_events e ON e.id = ac.event_id
		WHERE e.action='event.create' AND ac.field='title'`).Scan(&newVal)
	if err != nil || newVal != "Narozeniny" {
		t.Fatalf("event.create title diff = %q, err=%v", newVal, err)
	}
}

// ---- helpers ----

func completionCount(t *testing.T, db *sql.DB, eventID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_reminder_completions WHERE event_id=?`, eventID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func auditCount(t *testing.T, db *sql.DB, action string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action=?`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func assertNoOccurrencesTable(t *testing.T, db *sql.DB) {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='occurrences'`).Scan(&name)
	if err != sql.ErrNoRows {
		t.Errorf("an 'occurrences' table exists (%v) — occurrences must never be persisted", err)
	}
}
