package todo_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/todo"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/optional"
)

func todoLists(f fixture) lists.Provider {
	return todo.NewModule(f.svc).ListProvider()
}

func TestTodoListsDescriptors(t *testing.T) {
	f := newFixture(t)
	got := todoLists(f).Descriptors()

	want := map[string]bool{
		todo.ListPravedelamCount: true,
		todo.ListDoneToday:       true,
		todo.ListOpenTotal:       true,
	}
	if len(got) != len(want) {
		t.Fatalf("published %d descriptors, want %d", len(got), len(want))
	}
	for _, d := range got {
		if !want[d.Key] {
			t.Errorf("unexpected list key %q", d.Key)
		}
		if d.Scope != lists.ScopeHousehold {
			t.Errorf("%s scope = %q, want household — a board is shared", d.Key, d.Scope)
		}
		if d.Label == "" || strings.TrimSpace(d.Empty) == "" {
			t.Errorf("%s needs a Czech label and an empty text for the composer, got %+v", d.Key, d)
		}
	}
}

// The list names the cards the metric counts, in the order the board reads.
func TestTodoListsNameTheCardsTheMetricsCount(t *testing.T) {
	f := newFixture(t)
	mod := todo.NewModule(f.svc)
	ctx := context.Background()
	now := time.Now().In(prague(t))

	b := f.board(t)
	zasobnik := f.column(t, b.ID, "Zásobník", "normal")
	nowCol := f.column(t, b.ID, "Právě dělám", "now")
	doneCol := f.column(t, b.ID, "Hotovo", "done")

	f.card(t, nowCol.ID, "Vyluxovat")
	f.card(t, nowCol.ID, "Umýt nádobí")
	finished := f.card(t, zasobnik.ID, "Vynést koš")

	items, err := mod.ListProvider().Items(ctx, "u1", todo.ListPravedelamCount, now)
	if err != nil {
		t.Fatalf("pravedelam list: %v", err)
	}
	if got := strings.Join(items, "|"); got != "Vyluxovat|Umýt nádobí" {
		t.Errorf("items = %q, want the two cards in board order", got)
	}
	count, err := mod.MetricProvider().Value(ctx, "u1", todo.MetricPravedelamCount, now)
	if err != nil {
		t.Fatalf("pravedelam metric: %v", err)
	}
	if count != len(items) {
		t.Errorf("metric says %d, the list names %d — the two must agree", count, len(items))
	}

	if _, err := f.svc.MoveCard(f.ctx, finished.ID, todo.CardMoveRequest{ColumnID: doneCol.ID}, ""); err != nil {
		t.Fatalf("move to done: %v", err)
	}
	done, err := mod.ListProvider().Items(ctx, "u1", todo.ListDoneToday, now)
	if err != nil {
		t.Fatalf("done_today list: %v", err)
	}
	if len(done) != 1 || done[0] != "Vynést koš" {
		t.Errorf("done today = %v, want the card just finished", done)
	}
}

// An archived card is off the board, so it must be off the list too.
func TestTodoListsIgnoreArchived(t *testing.T) {
	f := newFixture(t)
	p := todoLists(f)
	now := time.Now().In(prague(t))

	b := f.board(t)
	nowCol := f.column(t, b.ID, "Právě dělám", "now")
	card := f.card(t, nowCol.ID, "Skrytý úkol")

	if _, err := f.svc.UpdateCard(f.ctx, card.ID, todo.CardUpdate{Archived: optional.Of(true)}); err != nil {
		t.Fatalf("archive card: %v", err)
	}
	items, err := p.Items(context.Background(), "u1", todo.ListPravedelamCount, now)
	if err != nil {
		t.Fatalf("pravedelam list: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("items = %v with the only card archived, want none", items)
	}
}

func TestTodoListsUnknownKey(t *testing.T) {
	f := newFixture(t)
	if _, err := todoLists(f).Items(context.Background(), "u1", "todo.nonsense", time.Now()); err == nil {
		t.Error("expected an error for an unknown list key")
	}
}
