package audit_test

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/testsupport"
)

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestRecord_RollbackLeavesNoEvent: if the surrounding transaction rolls back,
// the event goes with it. (Atomicity, direction 1.)
func TestRecord_RollbackLeavesNoEvent(t *testing.T) {
	db := testsupport.NewDB(t)
	sink := audit.NewSink()
	ctx := testsupport.CtxUser("u1", "editor")
	boom := errors.New("boom")

	err := appdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		if _, err := sink.Record(ctx, tx, audit.Event{
			Module: audit.ModuleTodo, Action: "card.create",
			EntityType: "card", EntityID: "c1", Summary: "vytvořena karta",
		}); err != nil {
			return err
		}
		return boom // force rollback AFTER a successful record
	})
	if !errors.Is(err, boom) {
		t.Fatalf("WithTx err = %v, want boom", err)
	}
	if n := countRows(t, db, "audit_events"); n != 0 {
		t.Fatalf("audit_events = %d, want 0 after rollback", n)
	}
}

// TestRecord_EventFailureRollsBackMutation: if the event insert fails (here a
// bad level rejected by the CHECK), the whole transaction — including the domain
// mutation — rolls back. (Atomicity, direction 2: no action succeeds unlogged.)
func TestRecord_EventFailureRollsBackMutation(t *testing.T) {
	db := testsupport.NewDB(t)
	sink := audit.NewSink()
	ctx := testsupport.CtxUser("u1", "editor")

	err := appdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		// A real domain mutation first.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO boards (id, name, position, created_at, archived)
			 VALUES ('b1','Domácnost','V','2026-01-01T00:00:00Z',0)`); err != nil {
			return err
		}
		// Then an audit write that the DB rejects.
		_, err := sink.Record(ctx, tx, audit.Event{
			Module: audit.ModuleTodo, Action: "board.create",
			EntityType: "board", EntityID: "b1", Summary: "vytvořena nástěnka",
			Level: "explode", // not in the CHECK set → insert fails
		})
		return err
	})
	if err == nil {
		t.Fatal("expected the bad-level audit insert to fail the transaction")
	}
	if n := countRows(t, db, "boards"); n != 0 {
		t.Fatalf("boards = %d, want 0 (mutation must roll back with the failed audit write)", n)
	}
	if n := countRows(t, db, "audit_events"); n != 0 {
		t.Fatalf("audit_events = %d, want 0", n)
	}
}

// TestRecord_ActorAndRequestFromContext: the actor and request id are sourced
// from context, so a handler cannot forge them (Event has no such fields).
func TestRecord_ActorAndRequestFromContext(t *testing.T) {
	db := testsupport.NewDB(t)
	sink := audit.NewSink()
	ctx := testsupport.CtxUser("karel-42", "admin")

	var id string
	if err := appdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		id, err = sink.Record(ctx, tx, audit.Event{
			Module: audit.ModuleEvents, Action: "event.update",
			EntityType: "event", EntityID: "e1", Summary: "upravena událost",
		})
		return err
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	var (
		actorID, actorType, reqID, site string
	)
	if err := db.QueryRow(
		`SELECT actor_user_id, actor_type, request_id, site FROM audit_events WHERE id = ?`, id).
		Scan(&actorID, &actorType, &reqID, &site); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if actorID != "karel-42" || actorType != "user" || reqID != "test-req" || site != "home" {
		t.Errorf("row actor/request = %q/%q/%q/%q, want karel-42/user/test-req/home",
			actorID, actorType, reqID, site)
	}
}

// TestRecord_WritesFullChangeValues: one audit_changes row per changed field,
// with untruncated values (a paragraph-length note survives intact).
func TestRecord_WritesFullChangeValues(t *testing.T) {
	db := testsupport.NewDB(t)
	sink := audit.NewSink()
	ctx := testsupport.CtxUser("u1", "editor")
	longNote := strings.Repeat("Vyměnit baterii v kotli a objednat servis. ", 60)

	var id string
	if err := appdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		id, err = sink.Record(ctx, tx, audit.Event{
			Module: audit.ModuleTodo, Action: "card.update",
			EntityType: "card", EntityID: "c1", Summary: "upravena karta",
			Changes: []audit.Change{
				{Field: "title", Old: audit.Ptr("Koupit"), New: audit.Ptr("Koupit mléko")},
				{Field: "notes", Old: nil, New: audit.Ptr(longNote)},
			},
		})
		return err
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	rows, err := db.Query(
		`SELECT field, old_value, new_value FROM audit_changes WHERE event_id = ? ORDER BY field`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string][2]sql.NullString{}
	for rows.Next() {
		var f string
		var o, n sql.NullString
		if err := rows.Scan(&f, &o, &n); err != nil {
			t.Fatal(err)
		}
		got[f] = [2]sql.NullString{o, n}
	}
	if len(got) != 2 {
		t.Fatalf("changes = %d, want 2", len(got))
	}
	if got["notes"][0].Valid {
		t.Error("notes old_value should be NULL")
	}
	if got["notes"][1].String != longNote {
		t.Errorf("notes new_value truncated: got %d chars, want %d", len(got["notes"][1].String), len(longNote))
	}
}

// TestRecord_MetaAndFTS: meta is stored as JSON and FTS5 finds a Czech term with
// diacritics; the delete trigger keeps the index in sync.
func TestRecord_MetaAndFTS(t *testing.T) {
	db := testsupport.NewDB(t)
	sink := audit.NewSink()
	ctx := testsupport.CtxUser("u1", "editor")

	var id string
	if err := appdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		id, err = sink.Record(ctx, tx, audit.Event{
			Module: audit.ModuleTodo, Action: "card.move",
			EntityType: "card", EntityID: "c1",
			Summary: "Přesunuta karta „Vyměnit kotlík“ do Hotovo",
			Meta:    map[string]any{"via": "dashboard"},
		})
		return err
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	var meta string
	if err := db.QueryRow(`SELECT meta FROM audit_events WHERE id = ?`, id).Scan(&meta); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(meta, "dashboard") {
		t.Errorf("meta = %q, want to contain dashboard", meta)
	}

	ftsCount := func(term string) int {
		var n int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM audit_events_fts WHERE audit_events_fts MATCH ?`, term).Scan(&n); err != nil {
			t.Fatalf("fts match %q: %v", term, err)
		}
		return n
	}
	if got := ftsCount("kotlík"); got != 1 {
		t.Errorf("FTS match for kotlík = %d, want 1", got)
	}
	if got := ftsCount("dashboard"); got != 1 {
		t.Errorf("FTS match for dashboard (meta) = %d, want 1", got)
	}

	// Delete trigger keeps the index synced.
	if _, err := db.Exec(`DELETE FROM audit_events WHERE id = ?`, id); err != nil {
		t.Fatalf("delete event: %v", err)
	}
	if got := ftsCount("kotlík"); got != 0 {
		t.Errorf("after delete FTS match for kotlík = %d, want 0", got)
	}
}
