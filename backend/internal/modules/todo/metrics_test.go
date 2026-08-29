package todo_test

import (
	"context"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/todo"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/optional"
)

// prague is the zone every metric's "today" is evaluated in (HOME_TIMEZONE).
func prague(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Prague")
	if err != nil {
		t.Fatalf("load Europe/Prague: %v", err)
	}
	return loc
}

func todoMetrics(f fixture) metrics.Provider {
	return todo.NewModule(f.svc).MetricProvider()
}

func TestTodoMetricsDescriptors(t *testing.T) {
	f := newFixture(t)
	got := todoMetrics(f).Descriptors()

	want := map[string]bool{
		todo.MetricPravedelamCount: true,
		todo.MetricDoneToday:       true,
		todo.MetricOpenTotal:       true,
	}
	if len(got) != len(want) {
		t.Fatalf("published %d descriptors, want %d", len(got), len(want))
	}
	for _, d := range got {
		if !want[d.Key] {
			t.Errorf("unexpected metric key %q", d.Key)
		}
		// A board is shared, so every recipient must see the same number.
		if d.Scope != metrics.ScopeHousehold {
			t.Errorf("%s scope = %q, want household", d.Key, d.Scope)
		}
		if d.Label == "" || d.Unit == "" {
			t.Errorf("%s needs a Czech label and unit for the composer, got %+v", d.Key, d)
		}
	}
}

func TestTodoMetricsCountAcrossBoards(t *testing.T) {
	f := newFixture(t)
	p := todoMetrics(f)
	ctx := context.Background()
	now := time.Now().In(prague(t))

	b1 := f.board(t)
	zasobnik := f.column(t, b1.ID, "Zásobník", "normal")
	nowCol := f.column(t, b1.ID, "Právě dělám", "now")
	doneCol := f.column(t, b1.ID, "Hotovo", "done")

	f.card(t, nowCol.ID, "Vyluxovat")
	f.card(t, nowCol.ID, "Umýt nádobí")
	f.card(t, zasobnik.ID, "Zavolat instalatérovi")
	finished := f.card(t, zasobnik.ID, "Vynést koš")

	// A second board contributes too — the metric spans all non-archived boards.
	b2, err := f.svc.CreateBoard(f.ctx, todo.BoardCreate{Name: "Chata"})
	if err != nil {
		t.Fatalf("second board: %v", err)
	}
	nowCol2 := f.column(t, b2.ID, "Právě dělám", "now")
	f.card(t, nowCol2.ID, "Posekat trávu")

	mustValue := func(key string) int {
		t.Helper()
		v, err := p.Value(ctx, "u1", key, now)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		return v
	}

	if got := mustValue(todo.MetricPravedelamCount); got != 3 {
		t.Errorf("pravedelam_count = %d, want 3 (2 on Domácnost + 1 on Chata)", got)
	}
	if got := mustValue(todo.MetricOpenTotal); got != 5 {
		t.Errorf("open_total = %d, want 5 (everything not in a done column)", got)
	}
	if got := mustValue(todo.MetricDoneToday); got != 0 {
		t.Errorf("done_today = %d before anything was finished, want 0", got)
	}

	// Finishing a card stamps done_at, which is what done_today counts.
	if _, err := f.svc.MoveCard(f.ctx, finished.ID, todo.CardMoveRequest{ColumnID: doneCol.ID}, ""); err != nil {
		t.Fatalf("move to done: %v", err)
	}

	if got := mustValue(todo.MetricDoneToday); got != 1 {
		t.Errorf("done_today = %d after finishing one card, want 1", got)
	}
	if got := mustValue(todo.MetricOpenTotal); got != 4 {
		t.Errorf("open_total = %d after finishing one, want 4", got)
	}
}

// An archived board's cards must not inflate the household's counts.
func TestTodoMetricsIgnoreArchived(t *testing.T) {
	f := newFixture(t)
	p := todoMetrics(f)
	ctx := context.Background()
	now := time.Now().In(prague(t))

	b := f.board(t)
	nowCol := f.column(t, b.ID, "Právě dělám", "now")
	card := f.card(t, nowCol.ID, "Skrytý úkol")

	if _, err := f.svc.UpdateCard(f.ctx, card.ID, todo.CardUpdate{Archived: optional.Of(true)}); err != nil {
		t.Fatalf("archive card: %v", err)
	}
	if got, _ := p.Value(ctx, "u1", todo.MetricPravedelamCount, now); got != 0 {
		t.Errorf("pravedelam_count = %d with the only card archived, want 0", got)
	}
}

// Household scope means literally the same number for two different members.
func TestTodoMetricsAreIdenticalForEveryRecipient(t *testing.T) {
	f := newFixture(t)
	p := todoMetrics(f)
	ctx := context.Background()
	now := time.Now().In(prague(t))

	b := f.board(t)
	nowCol := f.column(t, b.ID, "Právě dělám", "now")
	f.card(t, nowCol.ID, "Společný úkol")

	karel, _ := p.Value(ctx, "karel", todo.MetricPravedelamCount, now)
	eva, _ := p.Value(ctx, "eva", todo.MetricPravedelamCount, now)
	if karel != eva || karel != 1 {
		t.Errorf("household metric differed per user: karel=%d eva=%d, want 1 for both", karel, eva)
	}
}

func TestTodoMetricsUnknownKey(t *testing.T) {
	f := newFixture(t)
	if _, err := todoMetrics(f).Value(context.Background(), "u1", "todo.nonsense", time.Now()); err == nil {
		t.Error("expected an error for an unknown metric key")
	}
}
