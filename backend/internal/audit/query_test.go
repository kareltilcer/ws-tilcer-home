package audit_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/testsupport"
)

// record writes one event through the sink in its own committed transaction.
func record(t *testing.T, db *sql.DB, ctx context.Context, e audit.Event) string {
	t.Helper()
	sink := audit.NewSink()
	var id string
	if err := appdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		id, err = sink.Record(ctx, tx, e)
		return err
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	return id
}

func TestStore_BrowseFiltersAndPagination(t *testing.T) {
	db := testsupport.NewDB(t)
	store := audit.NewStore(db)
	ctx := testsupport.CtxUser("marie", "admin")

	// Five events across two modules; one carries a searchable Czech term.
	record(t, db, ctx, audit.Event{Module: "todo", Action: "card.create", EntityType: "card", EntityID: "c1", Summary: "vytvořena karta"})
	record(t, db, ctx, audit.Event{Module: "todo", Action: "card.move", EntityType: "card", EntityID: "c1", Summary: "Přesunuta karta Vyměnit kotlík", Meta: map[string]any{"via": "dashboard"}})
	record(t, db, ctx, audit.Event{Module: "events", Action: "event.create", EntityType: "event", EntityID: "e1", Summary: "vytvořena událost", Level: "warn"})
	record(t, db, ctx, audit.Event{Module: "events", Action: "event.update", EntityType: "event", EntityID: "e1", Summary: "upravena událost"})
	record(t, db, ctx, audit.Event{Module: "todo", Action: "board.create", EntityType: "board", EntityID: "b1", Summary: "vytvořena nástěnka"})

	// Filter by module.
	page, err := store.Browse(ctx, audit.Filter{Module: "todo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("module=todo returned %d, want 3", len(page.Items))
	}
	// Newest-first: the board.create event (last inserted) is first.
	if page.Items[0].Action != "board.create" {
		t.Errorf("first item action = %q, want board.create (newest-first)", page.Items[0].Action)
	}

	// Filter by level.
	page, _ = store.Browse(ctx, audit.Filter{Level: "warn"})
	if len(page.Items) != 1 || page.Items[0].Action != "event.create" {
		t.Errorf("level=warn returned %d items, want 1 event.create", len(page.Items))
	}

	// FTS free-text with diacritics.
	page, _ = store.Browse(ctx, audit.Filter{Q: "kotlík"})
	if len(page.Items) != 1 || page.Items[0].Action != "card.move" {
		t.Errorf("q=kotlík returned %d, want 1 card.move", len(page.Items))
	}

	// Pagination: page size 2 across all 5, each seen exactly once.
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		p, err := store.Browse(ctx, audit.Filter{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range p.Items {
			if seen[it.ID] {
				t.Fatalf("event %s seen twice across pages", it.ID)
			}
			seen[it.ID] = true
		}
		pages++
		if p.NextCursor == nil {
			break
		}
		cursor = *p.NextCursor
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != 5 {
		t.Fatalf("paginated over %d events, want 5", len(seen))
	}
}

func TestStore_GetWithChanges(t *testing.T) {
	db := testsupport.NewDB(t)
	store := audit.NewStore(db)
	ctx := testsupport.CtxUser("marie", "admin")

	id := record(t, db, ctx, audit.Event{
		Module: "todo", Action: "card.update", EntityType: "card", EntityID: "c1", Summary: "upravena karta",
		Changes: []audit.Change{{Field: "title", Old: audit.Ptr("A"), New: audit.Ptr("B")}},
	})

	detail, err := store.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if detail == nil {
		t.Fatal("expected event, got nil")
	}
	if len(detail.Changes) != 1 || detail.Changes[0].Field != "title" {
		t.Errorf("changes = %+v, want one title change", detail.Changes)
	}
	if detail.ChangeCount != 1 {
		t.Errorf("change_count = %d, want 1", detail.ChangeCount)
	}

	missing, err := store.Get(ctx, "does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Error("expected nil for unknown event")
	}
}

func TestStore_TimelineOldestFirstCrossModule(t *testing.T) {
	db := testsupport.NewDB(t)
	store := audit.NewStore(db)
	ctx := testsupport.CtxUser("marie", "admin")

	// A card's history including a move triggered from the dashboard.
	record(t, db, ctx, audit.Event{Module: "todo", Action: "card.create", EntityType: "card", EntityID: "c1", Summary: "vytvořena"})
	record(t, db, ctx, audit.Event{Module: "todo", Action: "card.move", EntityType: "card", EntityID: "c1", Summary: "přesunuta z Nástěnky", Meta: map[string]any{"via": "dashboard"}})
	// An unrelated entity that must not appear.
	record(t, db, ctx, audit.Event{Module: "events", Action: "event.create", EntityType: "event", EntityID: "e1", Summary: "jiná entita"})

	page, err := store.Timeline(ctx, "card", "c1", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("timeline len = %d, want 2", len(page.Items))
	}
	if page.Items[0].Action != "card.create" || page.Items[1].Action != "card.move" {
		t.Errorf("timeline not oldest-first: %q then %q", page.Items[0].Action, page.Items[1].Action)
	}
	// The dashboard-triggered move carries meta.via and stays under module=todo.
	if page.Items[1].Module != "todo" {
		t.Errorf("cross-module move logged under %q, want todo", page.Items[1].Module)
	}
}

func TestStore_Stats(t *testing.T) {
	db := testsupport.NewDB(t)
	store := audit.NewStore(db)
	ctx := testsupport.CtxUser("marie", "admin")

	record(t, db, ctx, audit.Event{Module: "todo", Action: "card.create", Summary: "a"})
	record(t, db, ctx, audit.Event{Module: "todo", Action: "card.move", Summary: "b"})
	record(t, db, ctx, audit.Event{Module: "events", Action: "event.create", Summary: "c"})

	res, err := store.Stats(ctx, "module", "day", "", "")
	if err != nil {
		t.Fatal(err)
	}
	totals := map[string]int{}
	for _, tot := range res.Totals {
		totals[tot.Key] = tot.Count
	}
	if totals["todo"] != 2 || totals["events"] != 1 {
		t.Errorf("module totals = %v, want todo:2 events:1", totals)
	}
	if len(res.Buckets) == 0 {
		t.Error("expected at least one time bucket")
	}

	if _, err := store.Stats(ctx, "bogus", "day", "", ""); err == nil {
		t.Error("expected error for invalid dimension")
	}
}
