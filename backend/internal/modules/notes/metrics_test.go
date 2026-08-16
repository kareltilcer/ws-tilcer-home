package notes_test

import (
	"context"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/notes"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

func TestNotesMetricDescriptor(t *testing.T) {
	x := newH(t)
	got := notes.NewModule(x.svc).MetricProvider().Descriptors()

	if len(got) != 1 || got[0].Key != notes.MetricPinnedCount {
		t.Fatalf("descriptors = %+v, want just %s", got, notes.MetricPinnedCount)
	}
	// PERSONAL is the whole point: personal pins differ per member, which is what
	// forces summaries to render once per recipient (D60).
	if got[0].Scope != metrics.ScopePersonal {
		t.Errorf("scope = %q, want personal", got[0].Scope)
	}
}

// The count is household ∪ mine, de-duplicated — exactly the widget's population.
func TestNotesPinnedCountIsPerRecipientAndDeduped(t *testing.T) {
	x := newH(t)
	p := notes.NewModule(x.svc).MetricProvider()
	ctx := context.Background()

	karel := testsupport.CtxUser("karel", "editor")
	eva := testsupport.CtxUser("eva", "editor")

	shared := x.note(x.svc.CreateNote(karel, notes.NoteCreate{Title: "Wi-Fi heslo"}))
	karelsOwn := x.note(x.svc.CreateNote(karel, notes.NoteCreate{Title: "Můj seznam"}))
	evasOwn := x.note(x.svc.CreateNote(eva, notes.NoteCreate{Title: "Evin seznam"}))
	x.note(x.svc.CreateNote(karel, notes.NoteCreate{Title: "Nepřipnutá"}))

	// One household pin everyone sees, plus a personal pin each.
	if _, err := x.svc.Pin(karel, shared.ID, "household", ""); err != nil {
		t.Fatalf("household pin: %v", err)
	}
	if _, err := x.svc.Pin(karel, karelsOwn.ID, "personal", ""); err != nil {
		t.Fatalf("karel personal pin: %v", err)
	}
	if _, err := x.svc.Pin(eva, evasOwn.ID, "personal", ""); err != nil {
		t.Fatalf("eva personal pin: %v", err)
	}
	// Karel ALSO pins the shared note personally — it must still count once.
	if _, err := x.svc.Pin(karel, shared.ID, "personal", ""); err != nil {
		t.Fatalf("karel double pin: %v", err)
	}

	karelCount, err := p.Value(ctx, "karel", notes.MetricPinnedCount, time.Now())
	if err != nil {
		t.Fatalf("karel: %v", err)
	}
	evaCount, err := p.Value(ctx, "eva", notes.MetricPinnedCount, time.Now())
	if err != nil {
		t.Fatalf("eva: %v", err)
	}

	if karelCount != 2 {
		t.Errorf("karel = %d, want 2 (household + his own; the doubly-pinned note counts once)", karelCount)
	}
	if evaCount != 2 {
		t.Errorf("eva = %d, want 2 (household + her own)", evaCount)
	}

	// A member with no personal pins still sees the household one.
	stranger, err := p.Value(ctx, "nikdo", notes.MetricPinnedCount, time.Now())
	if err != nil {
		t.Fatalf("stranger: %v", err)
	}
	if stranger != 1 {
		t.Errorf("member with no personal pins = %d, want 1 (the household pin)", stranger)
	}
}

func TestNotesMetricUnknownKey(t *testing.T) {
	x := newH(t)
	p := notes.NewModule(x.svc).MetricProvider()
	if _, err := p.Value(context.Background(), "u1", "notes.nonsense", time.Now()); err == nil {
		t.Error("expected an error for an unknown metric key")
	}
}
