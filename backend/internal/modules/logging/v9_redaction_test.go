package logging_test

// v9 — redaction of private-item events (PRD §V9-4a rows 12–14, D187/D188/D209).
//
// The log browser is admin-only, so every test here is an ADMIN trying to learn
// something about another member's private note through the one screen that shows
// everything. That is the uncomfortable case, and it is the one the rules exist for.
//
// ⚠ THERE ARE TWO RULES AND FOUR DOORS. Browsing REDACTS — a row appears saying
// only that something private happened. Matching (?q=, ?entity_id=, /stats)
// EXCLUDES, because whether a row comes back at all is itself the answer. The
// first draft of the leak table had the first rule and two of the doors.

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/logging"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

const (
	owner    = "u-kaja"
	intruder = "u-andy"
	// privateTitle occurs ONLY inside the private note's summary and diffs. Any
	// path that surfaces it, or even confirms it matched, has leaked.
	privateTitle = "zzsoukromytajnynazev"
)

// seedPrivateEvent writes one private-item event the way the notes module does:
// full summary, full diffs, plus the meta marker.
func seedPrivateEvent(t *testing.T, db *sql.DB, entityID string) string {
	t.Helper()
	sink := audit.NewSink()
	ctx := testsupport.CtxUser(owner, "editor")
	var id string
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	id, err = sink.Record(ctx, tx, audit.Event{
		Module:     audit.ModuleNotes,
		Action:     "note.update",
		EntityType: "note",
		EntityID:   entityID,
		Summary:    "Upravena poznámka „" + privateTitle + "“",
		Meta: map[string]any{
			audit.MetaVisibility: audit.VisibilityPrivate,
			audit.MetaOwnerID:    owner,
		},
		Changes: []audit.Change{
			{Field: "title", Old: audit.Ptr("staré"), New: audit.Ptr(privateTitle)},
		},
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return id
}

func seedSharedEvent(t *testing.T, db *sql.DB) string {
	t.Helper()
	sink := audit.NewSink()
	tx, _ := db.Begin()
	id, err := sink.Record(testsupport.CtxUser(owner, "editor"), tx, audit.Event{
		Module: audit.ModuleNotes, Action: "note.create", EntityType: "note", EntityID: "n-shared",
		Summary: "Vytvořena poznámka „Sdílená“",
		Meta:    map[string]any{audit.MetaVisibility: audit.VisibilityShared},
	})
	if err != nil {
		t.Fatalf("record shared: %v", err)
	}
	_ = tx.Commit()
	return id
}

// ---- Row 12: browsing redacts ----

func TestBrowseRedactsAPrivateEventForANonOwner(t *testing.T) {
	db := testsupport.NewDB(t)
	store := logging.NewStore(db)
	seedPrivateEvent(t, db, "n-private")
	seedSharedEvent(t, db)
	ctx := context.Background()

	page, err := store.Browse(ctx, logging.Filter{}, intruder)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	var private *logging.AuditEvent
	for i := range page.Items {
		if page.Items[i].Action == "note.update" {
			private = &page.Items[i]
		}
	}
	if private == nil {
		t.Fatal("the private event vanished from an unfiltered browse — it should be REDACTED, " +
			"not hidden: the household may learn that something private happened (D187)")
	}
	if !private.Redacted {
		t.Error("redacted = false on a foreign private event — a client cannot tell the fixed " +
			"phrase from a summary somebody wrote")
	}
	if private.Summary == "" || private.Summary != audit.RedactedNote {
		t.Errorf("summary = %q, want %q", private.Summary, audit.RedactedNote)
	}
	if private.EntityID != nil {
		t.Errorf("entity_id = %v, want nil — an id in a log row is an id that can be fed back "+
			"into the entity timeline, and the purge screen hands admins ids by design (D209)",
			*private.EntityID)
	}
	// The owner sees the real thing; the spine is their history too (D187).
	page, err = store.Browse(ctx, logging.Filter{}, owner)
	if err != nil {
		t.Fatalf("browse as owner: %v", err)
	}
	for _, e := range page.Items {
		if e.Action == "note.update" {
			if e.Redacted {
				t.Error("the OWNER's own event came back redacted — redaction is per-viewer, and " +
					"redacting at write time is exactly what D187 rejects")
			}
			if e.Summary == audit.RedactedNote {
				t.Errorf("the owner sees the fixed phrase instead of their own summary")
			}
		}
	}
}

func TestGetRedactsAndDropsTheChanges(t *testing.T) {
	db := testsupport.NewDB(t)
	store := logging.NewStore(db)
	id := seedPrivateEvent(t, db, "n-private")
	ctx := context.Background()

	got, err := store.Get(ctx, id, intruder)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("the event 404d for a non-owner — browsing REDACTS rather than hides (D187)")
	}
	if !got.Redacted || got.Summary != audit.RedactedNote {
		t.Errorf("summary = %q redacted = %v, want the fixed phrase", got.Summary, got.Redacted)
	}
	if len(got.Changes) != 0 {
		t.Errorf("changes = %v, want empty — the field diffs carry the private title verbatim, "+
			"which is the whole thing being protected", got.Changes)
	}
	if got.Changes == nil {
		t.Error("changes is null, want [] — D174 still holds: a client that indexes into it " +
			"must not have to special-case a redacted row")
	}

	owned, err := store.Get(ctx, id, owner)
	if err != nil {
		t.Fatalf("get as owner: %v", err)
	}
	if owned.Redacted || len(owned.Changes) != 1 {
		t.Errorf("the owner got redacted=%v with %d changes, want false with 1 — their own "+
			"history has to stay intact", owned.Redacted, len(owned.Changes))
	}
}

// ---- Row 13: ?q= excludes ----

// TestSearchExcludesPrivateEventsForANonOwner is the SECOND rule (D188), and the
// distinction is subtle enough to be worth restating: a redacted HIT would still
// tell the searcher that their term occurs in a private title. The hit itself is
// the disclosure, so the row must not match at all.
func TestSearchExcludesPrivateEventsForANonOwner(t *testing.T) {
	db := testsupport.NewDB(t)
	store := logging.NewStore(db)
	seedPrivateEvent(t, db, "n-private")
	ctx := context.Background()

	page, err := store.Browse(ctx, logging.Filter{Q: privateTitle}, intruder)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("a non-owner searching for a term that occurs ONLY in a private title got %d "+
			"hit(s), want 0. Redacting the hit is not enough — the hit is the answer (D188)",
			len(page.Items))
	}
	page, err = store.Browse(ctx, logging.Filter{Q: privateTitle}, owner)
	if err != nil {
		t.Fatalf("search as owner: %v", err)
	}
	if len(page.Items) != 1 {
		t.Errorf("the owner searching their own private title got %d hits, want 1", len(page.Items))
	}
}

// ---- Row 14: the three doors the first draft missed (D209) ----

// TestEntityIdFilterExcludesRatherThanRedacts. An exact match is a stronger oracle
// than a lexical one: N redacted rows still confirm the id exists — and the purge
// screen hands admins those ids on purpose.
func TestEntityIdFilterExcludesRatherThanRedacts(t *testing.T) {
	db := testsupport.NewDB(t)
	store := logging.NewStore(db)
	seedPrivateEvent(t, db, "n-private")
	ctx := context.Background()

	page, err := store.Browse(ctx, logging.Filter{EntityID: "n-private"}, intruder)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("?entity_id= on a private id returned %d row(s) for a non-owner, want 0. "+
			"Redacted rows would still answer 'yes, that id exists and something happened to "+
			"it' — and an admin gets those ids from Soukromé položky (D209)", len(page.Items))
	}
	// An unknown id gives the same empty page, so the two are indistinguishable.
	unknown, err := store.Browse(ctx, logging.Filter{EntityID: "n-does-not-exist"}, intruder)
	if err != nil {
		t.Fatalf("filter unknown: %v", err)
	}
	if len(unknown.Items) != len(page.Items) {
		t.Error("a private entity_id and an unknown one gave different-sized pages")
	}
	// The owner still gets their own history by id.
	own, err := store.Browse(ctx, logging.Filter{EntityID: "n-private"}, owner)
	if err != nil {
		t.Fatalf("filter as owner: %v", err)
	}
	if len(own.Items) != 1 {
		t.Errorf("the owner filtering on their own private id got %d rows, want 1", len(own.Items))
	}
}

// TestEntityTimelineExcludesForeignPrivateEvents is the worst of the three: it is
// addressed BY ID and returns FULL field diffs, and the purge screen supplies the
// ids. Left alone, an admin could read every private title and every before/after
// value by pasting an id from one Administrace screen into another.
func TestEntityTimelineExcludesForeignPrivateEvents(t *testing.T) {
	db := testsupport.NewDB(t)
	store := logging.NewStore(db)
	seedPrivateEvent(t, db, "n-private")
	ctx := context.Background()

	page, err := store.Timeline(ctx, "note", "n-private", "", "", 0, "", intruder)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("the entity timeline returned %d event(s) for a non-owner, want 0 (D209). "+
			"This route returns AuditEventDetail with full `changes`, and Soukromé položky "+
			"hands admins the ids by design", len(page.Items))
	}
	own, err := store.Timeline(ctx, "note", "n-private", "", "", 0, "", owner)
	if err != nil {
		t.Fatalf("timeline as owner: %v", err)
	}
	if len(own.Items) != 1 || len(own.Items[0].Changes) != 1 {
		t.Errorf("the owner's timeline of their own item: %d events / %d changes, want 1 / 1",
			len(own.Items), len(own.Items[0].Changes))
	}
}

// TestStatsExcludePrivateEventsForANonOwner. Otherwise a bucket that ticks up
// while nothing visible changed says "somebody did something private just now",
// on a chart, every day.
func TestStatsExcludePrivateEventsForANonOwner(t *testing.T) {
	db := testsupport.NewDB(t)
	store := logging.NewStore(db)
	seedPrivateEvent(t, db, "n-private")
	seedSharedEvent(t, db)
	ctx := context.Background()

	res, err := store.Stats(ctx, "action", "day", "", "", intruder)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	for _, tot := range res.Totals {
		if tot.Key == "note.update" {
			t.Errorf("a non-owner's /stats counts the private note.update event (%d) — the "+
				"totals must exclude it (D209)", tot.Count)
		}
	}
	res, err = store.Stats(ctx, "action", "day", "", "", owner)
	if err != nil {
		t.Fatalf("stats as owner: %v", err)
	}
	found := false
	for _, tot := range res.Totals {
		if tot.Key == "note.update" {
			found = true
		}
	}
	if !found {
		t.Error("the owner's own /stats dropped their private event too — the exclusion is " +
			"per-viewer, not global")
	}
}

// TestRedactionLeavesOrdinaryEventsAlone is the sanity guard on visibleEventsCond.
//
// The predicate uses SQLite's NULL-safe `IS NOT` for a reason: almost every event
// in the log has no meta at all, so a plain `<>` would evaluate to NULL and filter
// out the entire log. That failure would be spectacular rather than subtle, which
// is exactly why it deserves a test that would catch it on the first run.
func TestRedactionLeavesOrdinaryEventsAlone(t *testing.T) {
	db := testsupport.NewDB(t)
	store := logging.NewStore(db)
	sink := audit.NewSink()
	tx, _ := db.Begin()
	if _, err := sink.Record(testsupport.CtxUser(owner, "editor"), tx, audit.Event{
		Module: audit.ModuleTodo, Action: "card.move", EntityType: "card", EntityID: "c1",
		Summary: "Přesunut úkol", // no meta at all, like most of the log
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	_ = tx.Commit()

	for _, viewer := range []string{"", owner, intruder} {
		page, err := store.Browse(context.Background(), logging.Filter{Q: "úkol"}, viewer)
		if err != nil {
			t.Fatalf("browse (viewer %q): %v", viewer, err)
		}
		if len(page.Items) != 1 {
			t.Errorf("viewer %q sees %d ordinary events, want 1 — a meta-less event has a NULL "+
				"visibility, and a non-NULL-safe comparison would filter out the whole log",
				viewer, len(page.Items))
		}
	}
}

// TestRedactedMetaDoesNotCarryTheOwner: `meta` is rendered verbatim by the SPA's
// detail drawer, and the raw meta names the member the item belongs to.
func TestRedactedMetaDoesNotCarryTheOwner(t *testing.T) {
	db := testsupport.NewDB(t)
	store := logging.NewStore(db)
	id := seedPrivateEvent(t, db, "n-private")

	got, err := store.Get(context.Background(), id, intruder)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Meta) > 0 {
		var m map[string]any
		_ = json.Unmarshal(got.Meta, &m)
		if _, ok := m[audit.MetaOwnerID]; ok {
			t.Errorf("a redacted row still carries meta.owner_id (%v) — the SPA renders meta "+
				"verbatim", m[audit.MetaOwnerID])
		}
	}
}
