package todo_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/todo"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

func pvExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func pvBoard(t *testing.T, db *sql.DB, id, name, pos string) {
	pvExec(t, db, `INSERT INTO boards (id,name,description,position,created_by,created_at,archived)
		VALUES (?,?,NULL,?,NULL,'2026-01-01T00:00:00Z',0)`, id, name, pos)
}
func pvColumn(t *testing.T, db *sql.DB, id, boardID, name, kind, pos string) {
	pvExec(t, db, `INSERT INTO columns (id,board_id,name,priority,position,kind,created_at)
		VALUES (?,?,?,0,?,?, '2026-01-01T00:00:00Z')`, id, boardID, name, pos, kind)
}
func pvCard(t *testing.T, db *sql.DB, id, colID, title, pos string) {
	pvExec(t, db, `INSERT INTO cards (id,column_id,title,notes,position,created_by,created_at,updated_at,done_at,archived)
		VALUES (?,?,?,NULL,?,NULL,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',NULL,0)`, id, colID, title, pos)
}

// The pravedelam provider aggregates now-cards across all boards, supports
// multiple now columns per board, and resolves each board's first done column
// (or nil → archive path). This is the v1 cross-board aggregation, now a provider.
func TestPravedelamProvider_CrossBoardMultipleNowAndDoneColumn(t *testing.T) {
	db := testsupport.NewDB(t) // unseeded — we build our own boards

	// Board 1 (position "a"): two now columns + a done column.
	pvBoard(t, db, "b1", "Domácnost", "a")
	pvColumn(t, db, "n1", "b1", "Právě dělám", "now", "a")
	pvColumn(t, db, "n1b", "b1", "Dnes", "now", "b")
	pvColumn(t, db, "d1", "b1", "Hotovo", "done", "c")
	pvCard(t, db, "c1", "n1", "Zaplatit plyn", "a")
	pvCard(t, db, "c3", "n1b", "Vynést koš", "a")

	// Board 2 (position "b"): one now column, NO done column.
	pvBoard(t, db, "b2", "Chata", "b")
	pvColumn(t, db, "n2", "b2", "Právě dělám", "now", "a")
	pvCard(t, db, "c2", "n2", "Naštípat dříví", "a")

	p := todo.NewPravedelamProviderForTest(todo.NewStore(db))
	data, err := p.Data(context.Background(), registry.User{})
	if err != nil {
		t.Fatal(err)
	}
	w := data.(todo.PravedelamWidget)
	if len(w.Tasks) != 3 {
		t.Fatalf("tasks = %d, want 3 (2 boards, multiple now columns)", len(w.Tasks))
	}
	byCard := map[string]todo.DashboardTask{}
	for _, task := range w.Tasks {
		byCard[task.CardID] = task
	}
	if byCard["c1"].DoneColumnID == nil || *byCard["c1"].DoneColumnID != "d1" {
		t.Errorf("c1 done column = %v, want d1", byCard["c1"].DoneColumnID)
	}
	if byCard["c3"].DoneColumnID == nil || *byCard["c3"].DoneColumnID != "d1" {
		t.Errorf("c3 done column = %v, want d1 (multiple now columns share the board's done column)", byCard["c3"].DoneColumnID)
	}
	if byCard["c2"].DoneColumnID != nil {
		t.Errorf("c2 done column = %v, want nil (board 2 has no done column → archive path)", byCard["c2"].DoneColumnID)
	}
	if w.Tasks[0].BoardID != "b1" || w.Tasks[len(w.Tasks)-1].BoardID != "b2" {
		t.Errorf("cards not ordered by board position: %+v", w.Tasks)
	}
}
