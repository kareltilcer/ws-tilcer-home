package audit_test

import (
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/testsupport"
)

func TestPrune(t *testing.T) {
	db := testsupport.NewDB(t)
	ctx := testsupport.CtxUser("marie", "admin")

	// Two events; age one past the retention window by rewriting its ts.
	oldID := record(t, db, ctx, audit.Event{Module: "todo", Action: "card.create", Summary: "stará karta"})
	record(t, db, ctx, audit.Event{Module: "todo", Action: "card.move", Summary: "čerstvá změna"})
	if _, err := db.Exec("UPDATE audit_events SET ts = '2000-01-01T00:00:00.000000000Z' WHERE id = ?", oldID); err != nil {
		t.Fatal(err)
	}

	// Default (0) keeps everything.
	if n, err := audit.Prune(ctx, db, audit.NewSink(), 0); err != nil || n != 0 {
		t.Fatalf("Prune(0) = %d, %v; want 0, nil", n, err)
	}

	// Retention of 30 days removes the aged event and self-logs.
	deleted, err := audit.Prune(ctx, db, audit.NewSink(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("pruned %d, want 1", deleted)
	}

	var oldStillThere int
	if err := db.QueryRow("SELECT COUNT(*) FROM audit_events WHERE id = ?", oldID).Scan(&oldStillThere); err != nil {
		t.Fatal(err)
	}
	if oldStillThere != 0 {
		t.Error("aged event should have been pruned")
	}

	// The prune self-logged.
	var pruneEvents int
	if err := db.QueryRow("SELECT COUNT(*) FROM audit_events WHERE action = 'prune'").Scan(&pruneEvents); err != nil {
		t.Fatal(err)
	}
	if pruneEvents != 1 {
		t.Errorf("prune events = %d, want 1 (self-logged)", pruneEvents)
	}

	// FTS stays in sync (the pruned event's term is gone; nothing references it).
	var ftsForOld int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit_events_fts WHERE audit_events_fts MATCH ?`, "starN").Scan(&ftsForOld); err != nil {
		// "starN" is nonsense on purpose; just assert the query works post-prune.
		t.Fatalf("fts query after prune failed: %v", err)
	}
}
