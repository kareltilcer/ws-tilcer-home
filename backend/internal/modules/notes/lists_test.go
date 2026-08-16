package notes_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/notes"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

func TestNotesListDescriptor(t *testing.T) {
	x := newH(t)
	got := notes.NewModule(x.svc).ListProvider().Descriptors()

	if len(got) != 1 || got[0].Key != notes.ListPinned {
		t.Fatalf("descriptors = %+v, want just %s", got, notes.ListPinned)
	}
	if got[0].Scope != lists.ScopePersonal {
		t.Errorf("scope = %q, want personal — a summary naming these must render per recipient", got[0].Scope)
	}
	if strings.TrimSpace(got[0].Empty) == "" {
		t.Error("the list published no empty text")
	}
}

// The list names the notes the metric counts: household ∪ mine, de-duplicated,
// household block first — the widget's population in the widget's order.
func TestNotesPinnedListIsPerRecipientAndDeduped(t *testing.T) {
	x := newH(t)
	mod := notes.NewModule(x.svc)
	ctx := context.Background()

	karel := testsupport.CtxUser("karel", "editor")
	eva := testsupport.CtxUser("eva", "editor")

	shared := x.note(x.svc.CreateNote(karel, notes.NoteCreate{Title: "Wi-Fi heslo"}))
	karelsOwn := x.note(x.svc.CreateNote(karel, notes.NoteCreate{Title: "Můj seznam"}))
	evasOwn := x.note(x.svc.CreateNote(eva, notes.NoteCreate{Title: "Evin seznam"}))
	x.note(x.svc.CreateNote(karel, notes.NoteCreate{Title: "Nepřipnutá"}))

	if _, err := x.svc.Pin(karel, shared.ID, "household", ""); err != nil {
		t.Fatalf("household pin: %v", err)
	}
	if _, err := x.svc.Pin(karel, karelsOwn.ID, "personal", ""); err != nil {
		t.Fatalf("karel personal pin: %v", err)
	}
	if _, err := x.svc.Pin(eva, evasOwn.ID, "personal", ""); err != nil {
		t.Fatalf("eva personal pin: %v", err)
	}
	// Karel ALSO pins the shared note personally — it must still be named once.
	if _, err := x.svc.Pin(karel, shared.ID, "personal", ""); err != nil {
		t.Fatalf("karel double pin: %v", err)
	}

	items := func(userID string) []string {
		t.Helper()
		got, err := mod.ListProvider().Items(ctx, userID, notes.ListPinned, time.Now())
		if err != nil {
			t.Fatalf("%s: %v", userID, err)
		}
		return got
	}

	if got := strings.Join(items("karel"), "|"); got != "Wi-Fi heslo|Můj seznam" {
		t.Errorf("karel = %q, want the household pin then his own (the doubly-pinned note once)", got)
	}
	if got := strings.Join(items("eva"), "|"); got != "Wi-Fi heslo|Evin seznam" {
		t.Errorf("eva = %q, want the household pin then hers", got)
	}
	if got := strings.Join(items("nikdo"), "|"); got != "Wi-Fi heslo" {
		t.Errorf("member with no personal pins = %q, want just the household pin", got)
	}

	// The two catalogs must agree about the same person's pins.
	count, err := mod.MetricProvider().Value(ctx, "karel", notes.MetricPinnedCount, time.Now())
	if err != nil {
		t.Fatalf("karel count: %v", err)
	}
	if count != len(items("karel")) {
		t.Errorf("metric says %d, the list names %d", count, len(items("karel")))
	}
}

func TestNotesListUnknownKey(t *testing.T) {
	x := newH(t)
	p := notes.NewModule(x.svc).ListProvider()
	if _, err := p.Items(context.Background(), "u1", "notes.nonsense", time.Now()); err == nil {
		t.Error("expected an error for an unknown list key")
	}
}
