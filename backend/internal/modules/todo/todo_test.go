package todo_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/todo"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

type fixture struct {
	db  *sql.DB
	svc *todo.Service
	ctx context.Context
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := testsupport.NewDB(t)
	return fixture{
		db:  db,
		svc: todo.NewService(db, audit.NewSink(), nil),
		ctx: testsupport.CtxUser("editor-1", "editor"),
	}
}

func (f fixture) board(t *testing.T) *todo.Board {
	t.Helper()
	b, err := f.svc.CreateBoard(f.ctx, todo.BoardCreate{Name: "Domácnost"})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	return b
}

func (f fixture) column(t *testing.T, boardID, name, kind string) *todo.Column {
	t.Helper()
	c, err := f.svc.CreateColumn(f.ctx, boardID, todo.ColumnCreate{Name: name, Kind: kind})
	if err != nil {
		t.Fatalf("create column %q: %v", name, err)
	}
	return c
}

func (f fixture) card(t *testing.T, columnID, title string) *todo.Card {
	t.Helper()
	c, err := f.svc.CreateCard(f.ctx, columnID, todo.CardCreate{Title: title})
	if err != nil {
		t.Fatalf("create card %q: %v", title, err)
	}
	return c
}

func TestMoveCard_OneRowAndDoneAt(t *testing.T) {
	f := newFixture(t)
	b := f.board(t)
	backlog := f.column(t, b.ID, "Zásobník", todo.KindNormal)
	done := f.column(t, b.ID, "Hotovo", todo.KindDone)

	a := f.card(t, backlog.ID, "A")
	bCard := f.card(t, backlog.ID, "B")
	c := f.card(t, backlog.ID, "C")

	// Move B into the done column (append).
	moved, err := f.svc.MoveCard(f.ctx, bCard.ID, todo.CardMoveRequest{ColumnID: done.ID}, "")
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.ColumnID != done.ID {
		t.Errorf("card column = %s, want done column", moved.ColumnID)
	}
	if moved.DoneAt == nil {
		t.Error("entering a done column should stamp done_at")
	}

	// Siblings A and C keep their positions (a move rewrites one row).
	assertPosition(t, f.db, a.ID, a.Position)
	assertPosition(t, f.db, c.ID, c.Position)

	// Move B back to a non-done column clears done_at.
	moved, err = f.svc.MoveCard(f.ctx, bCard.ID, todo.CardMoveRequest{ColumnID: backlog.ID}, "")
	if err != nil {
		t.Fatal(err)
	}
	if moved.DoneAt != nil {
		t.Error("leaving a done column should clear done_at")
	}
}

func TestMultipleNowAndDoneColumns(t *testing.T) {
	f := newFixture(t)
	b := f.board(t)
	// Two now and two done columns on one board — nothing may assume exactly one.
	f.column(t, b.ID, "Právě dělám", todo.KindNow)
	f.column(t, b.ID, "Dnes", todo.KindNow)
	done1 := f.column(t, b.ID, "Hotovo", todo.KindDone)
	f.column(t, b.ID, "Archiv", todo.KindDone)

	var nowCount, doneCount int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM columns WHERE board_id=? AND kind='now'`, b.ID).Scan(&nowCount); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM columns WHERE board_id=? AND kind='done'`, b.ID).Scan(&doneCount); err != nil {
		t.Fatal(err)
	}
	if nowCount != 2 || doneCount != 2 {
		t.Fatalf("kinds = now:%d done:%d, want 2/2", nowCount, doneCount)
	}

	// A card moved to either done column stamps done_at.
	backlog := f.column(t, b.ID, "Zásobník", todo.KindNormal)
	card := f.card(t, backlog.ID, "Úkol")
	moved, err := f.svc.MoveCard(f.ctx, card.ID, todo.CardMoveRequest{ColumnID: done1.ID}, "")
	if err != nil || moved.DoneAt == nil {
		t.Fatalf("move to done1: done_at=%v err=%v", moved.DoneAt, err)
	}
}

func TestColumnDelete_BlockedThenCascade(t *testing.T) {
	f := newFixture(t)
	b := f.board(t)
	col := f.column(t, b.ID, "Zásobník", todo.KindNormal)
	f.card(t, col.ID, "Úkol 1")
	f.card(t, col.ID, "Úkol 2")

	// Non-empty delete without cascade → 409 with the card count.
	err := f.svc.DeleteColumn(f.ctx, col.ID, false)
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 409 {
		t.Fatalf("delete without cascade err = %v, want 409", err)
	}

	// Cascade deletes the column and its cards, logging each card deletion.
	if err := f.svc.DeleteColumn(f.ctx, col.ID, true); err != nil {
		t.Fatalf("cascade delete: %v", err)
	}
	var cards int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM cards WHERE column_id=?`, col.ID).Scan(&cards); err != nil {
		t.Fatal(err)
	}
	if cards != 0 {
		t.Errorf("cards after cascade = %d, want 0", cards)
	}
	if n := auditCount(t, f.db, "card.delete"); n != 2 {
		t.Errorf("card.delete events = %d, want 2 (each cascaded card logged)", n)
	}
	if n := auditCount(t, f.db, "column.delete"); n != 1 {
		t.Errorf("column.delete events = %d, want 1", n)
	}
}

func TestCardDelete_SoftThenHard(t *testing.T) {
	f := newFixture(t)
	b := f.board(t)
	col := f.column(t, b.ID, "Zásobník", todo.KindNormal)
	card := f.card(t, col.ID, "Úkol")

	if err := f.svc.DeleteCard(f.ctx, card.ID, false); err != nil {
		t.Fatal(err)
	}
	var archived int
	if err := f.db.QueryRow(`SELECT archived FROM cards WHERE id=?`, card.ID).Scan(&archived); err != nil {
		t.Fatalf("soft-deleted card should still exist: %v", err)
	}
	if archived != 1 {
		t.Error("soft delete should set archived=1")
	}

	if err := f.svc.DeleteCard(f.ctx, card.ID, true); err != nil {
		t.Fatal(err)
	}
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM cards WHERE id=?`, card.ID).Scan(&n)
	if n != 0 {
		t.Error("hard delete should remove the card")
	}
}

func TestTree_ShapeAndFilters(t *testing.T) {
	f := newFixture(t)
	b := f.board(t)
	c1 := f.column(t, b.ID, "Zásobník", todo.KindNormal)
	f.column(t, b.ID, "Prázdný", todo.KindNormal) // empty column must still render

	label, err := f.svc.CreateLabel(f.ctx, b.ID, todo.LabelCreate{Name: "Byt", Color: "#fff"})
	if err != nil {
		t.Fatal(err)
	}
	card := f.card(t, c1.ID, "Vyměnit baterii v kotli")
	f.card(t, c1.ID, "Zaplatit plyn")
	if err := f.svc.AttachLabel(f.ctx, card.ID, label.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateChecklistItem(f.ctx, card.ID, todo.ChecklistItemCreate{Text: "Krok 1"}); err != nil {
		t.Fatal(err)
	}

	tree, err := f.svc.Tree(f.ctx, b.ID, nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Columns) != 2 {
		t.Fatalf("tree columns = %d, want 2 (incl. empty)", len(tree.Columns))
	}
	// Card carries label ids + checklist progress, but not links/checklist arrays.
	var target *todo.Card
	for i := range tree.Columns[0].Cards {
		if tree.Columns[0].Cards[i].ID == card.ID {
			target = &tree.Columns[0].Cards[i]
		}
	}
	if target == nil {
		t.Fatal("card not found in tree")
	}
	if len(target.LabelIDs) != 1 || target.ChecklistProgress.Total != 1 {
		t.Errorf("card labels=%v progress=%+v, want 1 label / total 1", target.LabelIDs, target.ChecklistProgress)
	}

	// Text filter narrows.
	tree, _ = f.svc.Tree(f.ctx, b.ID, nil, "kotli", false)
	total := 0
	for _, col := range tree.Columns {
		total += len(col.Cards)
	}
	if total != 1 {
		t.Errorf("q=kotli matched %d cards, want 1", total)
	}

	// Label filter narrows.
	tree, _ = f.svc.Tree(f.ctx, b.ID, []string{label.ID}, "", false)
	total = 0
	for _, col := range tree.Columns {
		total += len(col.Cards)
	}
	if total != 1 {
		t.Errorf("label filter matched %d cards, want 1", total)
	}
}

func TestTree_LinkCount(t *testing.T) {
	f := newFixture(t)
	b := f.board(t)
	col := f.column(t, b.ID, "Zásobník", todo.KindNormal)
	withLinks := f.card(t, col.ID, "S odkazy")
	noLinks := f.card(t, col.ID, "Bez odkazů")

	for _, u := range []string{"https://a.example", "https://b.example"} {
		if _, err := f.svc.CreateCardLink(f.ctx, withLinks.ID, todo.CardLinkCreate{URL: u}); err != nil {
			t.Fatalf("create link: %v", err)
		}
	}

	tree, err := f.svc.Tree(f.ctx, b.ID, nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, c := range tree.Columns[0].Cards {
		got[c.ID] = c.LinkCount
	}
	if got[withLinks.ID] != 2 {
		t.Errorf("card with 2 links: link_count = %d, want 2", got[withLinks.ID])
	}
	if got[noLinks.ID] != 0 {
		t.Errorf("card with no links: link_count = %d, want 0", got[noLinks.ID])
	}
}

func TestListLabels_CardCount(t *testing.T) {
	f := newFixture(t)
	b := f.board(t)
	col := f.column(t, b.ID, "Zásobník", todo.KindNormal)

	label, err := f.svc.CreateLabel(f.ctx, b.ID, todo.LabelCreate{Name: "Byt", Color: "#fff"})
	if err != nil {
		t.Fatal(err)
	}
	unused, err := f.svc.CreateLabel(f.ctx, b.ID, todo.LabelCreate{Name: "Nepoužitý", Color: "#000"})
	if err != nil {
		t.Fatal(err)
	}

	c1 := f.card(t, col.ID, "Karta 1")
	c2 := f.card(t, col.ID, "Karta 2")
	archived := f.card(t, col.ID, "Archivovaná")
	for _, id := range []string{c1.ID, c2.ID, archived.ID} {
		if err := f.svc.AttachLabel(f.ctx, id, label.ID); err != nil {
			t.Fatal(err)
		}
	}
	// Soft-delete (archive) the third card — archived cards must not count.
	if err := f.svc.DeleteCard(f.ctx, archived.ID, false); err != nil {
		t.Fatal(err)
	}

	labels, err := f.svc.ListLabels(f.ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]*int{}
	for i := range labels {
		counts[labels[i].ID] = labels[i].CardCount
	}
	assertCount := func(name, id string, want int) {
		t.Helper()
		got := counts[id]
		if got == nil {
			t.Errorf("%s: card_count is nil, want %d", name, want)
			return
		}
		if *got != want {
			t.Errorf("%s: card_count = %d, want %d", name, *got, want)
		}
	}
	assertCount("used label (archived excluded)", label.ID, 2)
	assertCount("unused label", unused.ID, 0)
}

func TestTree_CardCountIgnoresFilters(t *testing.T) {
	f := newFixture(t)
	b := f.board(t)
	col := f.column(t, b.ID, "Zásobník", todo.KindNormal)
	empty := f.column(t, b.ID, "Prázdný", todo.KindNormal)
	f.card(t, col.ID, "Vyměnit baterii")
	archived := f.card(t, col.ID, "Hotová")
	if err := f.svc.DeleteCard(f.ctx, archived.ID, false); err != nil { // soft-delete (archive)
		t.Fatal(err)
	}

	// A query that matches nothing hides every card, but card_count still reports
	// the true total (including the archived card) so column delete stays honest.
	tree, err := f.svc.Tree(f.ctx, b.ID, nil, "nenajdenic", false)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	visible := 0
	for _, c := range tree.Columns {
		counts[c.Column.ID] = c.CardCount
		visible += len(c.Cards)
	}
	if visible != 0 {
		t.Fatalf("filtered cards visible = %d, want 0 (filter hides all)", visible)
	}
	if counts[col.ID] != 2 {
		t.Errorf("card_count = %d, want 2 (incl. archived, ignoring filter)", counts[col.ID])
	}
	if counts[empty.ID] != 0 {
		t.Errorf("empty column card_count = %d, want 0", counts[empty.ID])
	}
}

func TestReorderColumns_AtomicAndValidated(t *testing.T) {
	f := newFixture(t)
	b := f.board(t)
	c1 := f.column(t, b.ID, "A", todo.KindNormal)
	c2 := f.column(t, b.ID, "B", todo.KindNormal)
	c3 := f.column(t, b.ID, "C", todo.KindNormal)

	// Reorder to C, A, B in one batch.
	cols, err := f.svc.ReorderColumns(f.ctx, b.ID, []todo.ColumnPositionInput{
		{ID: c3.ID, Position: "1"},
		{ID: c1.ID, Position: "2"},
		{ID: c2.ID, Position: "3"},
	})
	if err != nil {
		t.Fatalf("reorder: %v", err)
	}
	got := []string{cols[0].ID, cols[1].ID, cols[2].ID}
	want := []string{c3.ID, c1.ID, c2.ID}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}

	// A batch that includes a column from another board is rejected whole — the
	// valid write in the same batch must NOT persist (single transaction).
	other := f.board(t)
	oc := f.column(t, other.ID, "X", todo.KindNormal)
	if _, err := f.svc.ReorderColumns(f.ctx, b.ID, []todo.ColumnPositionInput{
		{ID: c1.ID, Position: "9"},
		{ID: oc.ID, Position: "9"},
	}); err == nil {
		t.Fatal("expected error for a column not on this board")
	}
	var pos string
	if err := f.db.QueryRow(`SELECT position FROM columns WHERE id=?`, c1.ID).Scan(&pos); err != nil {
		t.Fatal(err)
	}
	if pos != "2" {
		t.Errorf("c1 position = %q after failed batch, want unchanged %q (rollback)", pos, "2")
	}

	// A batch with two columns sharing one position key is rejected up front — two
	// columns on the same order key would give an ambiguous, unstable sort order.
	if _, err := f.svc.ReorderColumns(f.ctx, b.ID, []todo.ColumnPositionInput{
		{ID: c1.ID, Position: "5"},
		{ID: c2.ID, Position: "5"},
	}); err == nil {
		t.Fatal("expected error for duplicate position in batch")
	}

	// Current keys: c3="1", c1="2", c2="3". A partial batch that drops c1 onto "3"
	// collides with c2 — which is untouched, so the within-batch dedupe can't see it.
	// It must still be rejected (global uniqueness), and c1 must stay put (rollback).
	if _, err := f.svc.ReorderColumns(f.ctx, b.ID, []todo.ColumnPositionInput{
		{ID: c1.ID, Position: "3"},
	}); err == nil {
		t.Fatal("expected error for a position colliding with a column outside the batch")
	}
	if err := f.db.QueryRow(`SELECT position FROM columns WHERE id=?`, c1.ID).Scan(&pos); err != nil {
		t.Fatal(err)
	}
	if pos != "2" {
		t.Errorf("c1 position = %q after rejected collision, want unchanged %q", pos, "2")
	}

	// A partial batch onto a free key still succeeds — the guard rejects only real
	// collisions, not every partial reorder.
	if _, err := f.svc.ReorderColumns(f.ctx, b.ID, []todo.ColumnPositionInput{
		{ID: c1.ID, Position: "0"},
	}); err != nil {
		t.Fatalf("partial batch onto a free key: %v", err)
	}
}

func TestMoveCard_BeforeCardIDAnchor(t *testing.T) {
	f := newFixture(t)
	b := f.board(t)
	col := f.column(t, b.ID, "Zásobník", todo.KindNormal)

	a := f.card(t, col.ID, "A")
	bCard := f.card(t, col.ID, "B")
	c := f.card(t, col.ID, "C") // order: A < B < C

	// Move C to sit immediately before B. The server computes the slot from the true
	// stored order, so C lands strictly between A and B regardless of client keys.
	moved, err := f.svc.MoveCard(f.ctx, c.ID, todo.CardMoveRequest{ColumnID: col.ID, BeforeCardID: bCard.ID}, "")
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if !(moved.Position > a.Position && moved.Position < bCard.Position) {
		t.Errorf("moved position %q, want strictly between %q and %q", moved.Position, a.Position, bCard.Position)
	}
	// Anchoring rewrites one row: A and B keep their positions.
	assertPosition(t, f.db, a.ID, a.Position)
	assertPosition(t, f.db, bCard.ID, bCard.Position)

	// An anchor that does not exist (concurrent delete) falls back to appending at
	// the tail rather than erroring the drop.
	moved, err = f.svc.MoveCard(f.ctx, a.ID, todo.CardMoveRequest{ColumnID: col.ID, BeforeCardID: "does-not-exist"}, "")
	if err != nil {
		t.Fatalf("move with missing anchor: %v", err)
	}
	if moved.Position < bCard.Position {
		t.Errorf("missing-anchor fallback position %q, want at the tail (> %q)", moved.Position, bCard.Position)
	}
}

func TestCardMutations_CarryLinkCount(t *testing.T) {
	f := newFixture(t)
	b := f.board(t)
	c1 := f.column(t, b.ID, "Zásobník", todo.KindNormal)
	c2 := f.column(t, b.ID, "Hotovo", todo.KindDone)
	card := f.card(t, c1.ID, "S odkazy")
	for _, u := range []string{"https://a.example", "https://b.example"} {
		if _, err := f.svc.CreateCardLink(f.ctx, card.ID, todo.CardLinkCreate{URL: u}); err != nil {
			t.Fatalf("create link: %v", err)
		}
	}

	// The mutation responses (not just Tree/GetCardDetail) must report the true
	// link_count. A consumer that trusts the response directly, rather than
	// refetching the tree, would otherwise render a linked card as having none.
	moved, err := f.svc.MoveCard(f.ctx, card.ID, todo.CardMoveRequest{ColumnID: c2.ID}, "")
	if err != nil {
		t.Fatal(err)
	}
	if moved.LinkCount != 2 {
		t.Errorf("MoveCard link_count = %d, want 2", moved.LinkCount)
	}
	updated, err := f.svc.UpdateCard(f.ctx, card.ID, todo.CardUpdate{Title: strPtr("Nový")})
	if err != nil {
		t.Fatal(err)
	}
	if updated.LinkCount != 2 {
		t.Errorf("UpdateCard link_count = %d, want 2", updated.LinkCount)
	}
}

func TestAudit_CardUpdateProducesDiff(t *testing.T) {
	f := newFixture(t)
	b := f.board(t)
	col := f.column(t, b.ID, "Zásobník", todo.KindNormal)
	card := f.card(t, col.ID, "Původní")

	if _, err := f.svc.UpdateCard(f.ctx, card.ID, todo.CardUpdate{Title: strPtr("Nový název")}); err != nil {
		t.Fatal(err)
	}
	// The card.update event has a title diff old→new.
	var old, newVal string
	err := f.db.QueryRow(`
		SELECT ac.old_value, ac.new_value
		FROM audit_changes ac JOIN audit_events e ON e.id = ac.event_id
		WHERE e.action='card.update' AND ac.field='title'`).Scan(&old, &newVal)
	if err != nil {
		t.Fatalf("expected a title diff: %v", err)
	}
	if old != "Původní" || newVal != "Nový název" {
		t.Errorf("diff = %q→%q, want Původní→Nový název", old, newVal)
	}
}

// ---- helpers ----

func assertPosition(t *testing.T, db *sql.DB, cardID, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT position FROM cards WHERE id=?`, cardID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("card %s position = %q, want unchanged %q (sibling rewritten)", cardID, got, want)
	}
}

func auditCount(t *testing.T, db *sql.DB, action string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action=?`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func strPtr(s string) *string { return &s }
