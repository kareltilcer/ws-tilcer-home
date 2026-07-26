package events_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// pvInsertEvent seeds an event directly. rrule "" = one-off; lead "" = no reminder.
func pvInsertEvent(t *testing.T, db *sql.DB, id, startsOn, rrule, lead string) {
	t.Helper()
	var rr, ld any
	remEnabled := 0
	if rrule != "" {
		rr = rrule
	}
	if lead != "" {
		remEnabled = 1
		ld = lead
	}
	if _, err := db.Exec(`INSERT INTO events
		(id,title,description,starts_on,rrule,timezone,reminder_enabled,reminder_lead,created_by,created_at,updated_at,archived)
		VALUES (?,?,NULL,?,?, 'Europe/Prague', ?, ?, NULL,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',0)`,
		id, id, startsOn, rr, remEnabled, ld); err != nil {
		t.Fatalf("insert event %s: %v", id, err)
	}
}

func pvInsertCompletion(t *testing.T, db *sql.DB, eventID, occ string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO event_reminder_completions (id,event_id,occurrence_on,completed_by,completed_at)
		VALUES (?,?,?,NULL,'2026-01-01T00:00:00Z')`, eventID+"-"+occ, eventID, occ); err != nil {
		t.Fatalf("insert completion: %v", err)
	}
}

// pripominky: activation boundary, at most one per event (advancing past
// completions), overdue flagged and sorted first, lookback bound.
func TestPripominkyProvider_ActivationOverdueOnePerEvent(t *testing.T) {
	db := testsupport.NewDB(t)
	today := dates.New(2026, 7, 15)

	pvInsertEvent(t, db, "A", "2026-07-20", "", "1w")                       // active (lead window open)
	pvInsertEvent(t, db, "B", "2026-07-30", "", "1d")                       // NOT active yet (1d lead)
	pvInsertEvent(t, db, "C", "2026-07-10", "", "1d")                       // overdue (past, uncompleted)
	pvInsertEvent(t, db, "D", "2026-07-01", "FREQ=WEEKLY;INTERVAL=1", "1d") // recurring
	pvInsertCompletion(t, db, "D", "2026-07-01")
	pvInsertCompletion(t, db, "D", "2026-07-08") // earliest uncompleted advances to 07-15

	p := events.NewPripominkyProviderForTest(events.NewStore(db), 30, 500, func() dates.Date { return today })
	data, err := p.Data(context.Background(), registry.User{})
	if err != nil {
		t.Fatal(err)
	}
	rem := data.(events.PripominkyWidget).Reminders

	if len(rem) != 3 {
		t.Fatalf("reminders = %d, want 3 (A, C, D — B not active); got %+v", len(rem), rem)
	}
	if rem[0].EventID != "C" || !rem[0].Overdue {
		t.Errorf("first reminder = %+v, want overdue C first", rem[0])
	}
	seen := map[string]events.DashboardReminder{}
	for _, r := range rem {
		if _, dup := seen[r.EventID]; dup {
			t.Fatalf("event %s appears more than once (must be at most one per event)", r.EventID)
		}
		seen[r.EventID] = r
	}
	if _, ok := seen["B"]; ok {
		t.Error("B must not be active yet (today < occurrence − lead)")
	}
	if d := seen["D"]; d.OccurrenceOn != "2026-07-15" || d.DaysUntil != 0 {
		t.Errorf("D reminder = %+v, want the advanced 2026-07-15 occurrence, days_until 0", d)
	}
}

// tento-mesic: only occurrences from today through the end of the current month.
func TestTentoMesicProvider_WindowIsRestOfMonth(t *testing.T) {
	db := testsupport.NewDB(t)
	today := dates.New(2026, 7, 15)

	pvInsertEvent(t, db, "E", "2026-07-20", "", "")                       // this month → included
	pvInsertEvent(t, db, "F", "2026-08-05", "", "")                       // next month → excluded
	pvInsertEvent(t, db, "G", "2026-07-01", "FREQ=WEEKLY;INTERVAL=1", "") // weekly → 07-15,07-22,07-29

	p := events.NewTentoMesicProviderForTest(events.NewStore(db), 500, func() dates.Date { return today })
	data, err := p.Data(context.Background(), registry.User{})
	if err != nil {
		t.Fatal(err)
	}
	occ := data.(events.TentoMesicWidget).Occurrences

	if len(occ) != 4 {
		t.Fatalf("occurrences = %d, want 4 (E + three G occurrences); got %+v", len(occ), occ)
	}
	for _, o := range occ {
		if o.EventID == "F" {
			t.Error("F is next month and must be excluded")
		}
		if o.OccurrenceOn < "2026-07-15" || o.OccurrenceOn > "2026-07-31" {
			t.Errorf("occurrence %s outside [today, end-of-month]", o.OccurrenceOn)
		}
	}
	for i := 1; i < len(occ); i++ {
		if occ[i-1].OccurrenceOn > occ[i].OccurrenceOn {
			t.Errorf("occurrences not sorted: %s before %s", occ[i-1].OccurrenceOn, occ[i].OccurrenceOn)
		}
	}
}
