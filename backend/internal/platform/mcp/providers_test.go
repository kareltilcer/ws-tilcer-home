package mcp_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/notes"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/todo"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// ⚠ LEAK ROW 8, THE TRAP ITSELF (D302). Every provider given a ctx with NO ACTOR
// must return an ERROR — never an empty slice, never an unscoped load.
//
// This is v9's exact trap, and v9's build is what proves it is not theoretical:
// its preview worker and its image GC had no actor, a viewer-scoped read returned
// nothing, and the next person reached for the unscoped load. An MCP tool call has
// an actor only AFTER the token resolves, so every provider is one refactor away
// from being called without one.
//
// ⚠ AN EMPTY SLICE IS WHAT THE BUG LOOKS LIKE on the day somebody "fixes" the nil,
// which is exactly why the assertion is `err != nil` and not `len(hits) == 0`.
func TestEveryProviderErrorsWithoutAnActor(t *testing.T) {
	db := testsupport.NewDB(t)
	sink := audit.NewSink()
	notify := func(context.Context, string, any) {}
	loc, err := time.LoadLocation("Europe/Prague")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}

	providers := map[string]mcp.Provider{
		"todo":   todo.NewModule(todo.NewService(db, sink, notify)).MCPProvider(),
		"events": events.NewModule(events.NewService(db, sink, notify, 500, 24), loc, 30).MCPProvider(),
		"notes":  notes.NewModule(notes.NewService(db, sink, notify, nil, notes.ImageOptions{}, nil)).MCPProvider(),
	}

	// ⚠ A BARE context.Background() IS THE POINT — it is what a background job, a
	// worker, or a refactored call site hands over.
	bare := context.Background()

	for name, p := range providers {
		t.Run(name+": Search", func(t *testing.T) {
			hits, err := p.Search(bare, mcp.Query{Text: "cokoliv", Limit: 10})
			if err == nil {
				t.Fatalf("%s.Search returned (%d hits, nil error) on an actor-less context."+
					" An empty slice is what an unscoped-load bug looks like the day somebody"+
					" 'fixes' the nil — it has to be an ERROR (D302).", name, len(hits))
			}
		})
	}

	// ⚠ AND THE RESOURCE SURFACES TOO, because they are the same read wearing a
	// different name: a listing is an answer, and a read is the answer itself.
	notesProvider := providers["notes"]
	t.Run("notes: ListResources", func(t *testing.T) {
		if _, err := notesProvider.ListResources(bare, 10); err == nil {
			t.Fatal("notes.ListResources returned no error on an actor-less context")
		}
	})
	t.Run("notes: Read", func(t *testing.T) {
		if _, err := notesProvider.Read(bare, "home://notes/soukrome/denik"); err == nil {
			t.Fatal("notes.Read returned no error on an actor-less context")
		}
	})
}

// ⚠ D297's notes half: reading a private note THROUGH A TOKEN is audited, and the
// event records the CONTAINER and never the content.
//
// Without it, "what has the assistant seen?" has no answer at all — unacceptable
// for a surface deliberately opened onto private notes. With more than it, the Log
// becomes a second copy of the note, which is exactly what D231 prevents for chat.
func TestPrivateNoteReadIsAuditedWithoutContent(t *testing.T) {
	h := newHarness(t)
	noteID := h.createPrivateNoteAs(memberA, "Deník", "velmi tajný obsah který se nesmí objevit v logu")
	secret, tok := h.mintToken(memberA, "Claude", nil, time.Time{})

	if n := testsupport.CountAudit(t, h.db, "notes", "private.read"); n != 0 {
		t.Fatalf("something wrote a read event before the read: %d", n)
	}

	_, result := h.call(secret, "home_notes_tree", map[string]any{"note": noteID})
	if isError(result) {
		t.Fatalf("the owner could not read their own private note: %s", resultText(t, result))
	}
	if n := testsupport.CountAudit(t, h.db, "notes", "private.read"); n != 1 {
		t.Fatalf("expected exactly one notes.private.read event, got %d", n)
	}

	var summary, meta, entityID string
	if err := h.db.QueryRow(`
		SELECT summary, COALESCE(meta, ''), COALESCE(entity_id, '')
		  FROM audit_events WHERE module = 'notes' AND action = 'private.read'`).
		Scan(&summary, &meta, &entityID); err != nil {
		t.Fatalf("read the event: %v", err)
	}
	if entityID != noteID {
		t.Fatalf("entity_id is %q, want the note id", entityID)
	}
	if !strings.Contains(meta, tok.ID) {
		t.Fatalf("the event does not name the token: %s", meta)
	}
	// ⚠ THE BODY MUST NOT BE ANYWHERE IN IT.
	for _, field := range []string{summary, meta} {
		if strings.Contains(field, "velmi tajný obsah") {
			t.Fatalf("the read event carries the note's BODY: %s", field)
		}
	}
	// And no field diffs at all — a read changes nothing, so audit_changes must
	// stay empty for it.
	var changes int
	if err := h.db.QueryRow(`
		SELECT COUNT(*) FROM audit_changes c
		  JOIN audit_events e ON e.id = c.event_id
		 WHERE e.action = 'private.read'`).Scan(&changes); err != nil {
		t.Fatalf("count changes: %v", err)
	}
	if changes != 0 {
		t.Fatalf("a read event has %d field diffs", changes)
	}

	// ⚠ A SHARED NOTE WRITES NOTHING. The Log is not a record of everything anybody
	// has ever looked at; only the private root is.
	sharedID := h.createSharedNoteAs(memberA, "Recepty", "guláš")
	h.call(secret, "home_notes_tree", map[string]any{"note": sharedID})
	if n := testsupport.CountAudit(t, h.db, "notes", "private.read"); n != 1 {
		t.Fatalf("reading a SHARED note wrote a private-read event: now %d", n)
	}

	// ⚠ AND A BROWSER READ WRITES NOTHING, as it always has. The event fires only
	// when the reader is a token.
	if rr := h.api(http.MethodGet, "/api/notes/"+noteID, ""); rr.Code != http.StatusOK {
		t.Fatalf("the browser read got %d: %s", rr.Code, rr.Body.String())
	}
	if n := testsupport.CountAudit(t, h.db, "notes", "private.read"); n != 1 {
		t.Fatalf("a BROWSER read wrote a private-read event: now %d", n)
	}
}

// The read event is itself scoped private, so the household sees the redaction
// phrase and the owner sees which note.
//
// ⚠ A READ EVENT THAT LEAKED THE TITLE WOULD DISCLOSE MORE THAN THE READ IT
// RECORDS, which would be an absurd shape for an audit trail to take.
func TestPrivateReadEventIsItselfRedacted(t *testing.T) {
	h := newHarness(t)
	h.createPrivateNoteAs(memberB, "Deník", "obsah")

	bSecret, _ := h.mintToken(memberB, "Claude B", nil, time.Time{})
	noteID := h.noteIDByTitle(memberB, "Deník")
	if _, result := h.call(bSecret, "home_notes_tree", map[string]any{"note": noteID}); isError(result) {
		t.Fatalf("member B could not read their own note: %s", resultText(t, result))
	}

	aSecret, _ := h.mintToken(memberA, "Claude A", nil, time.Time{})
	_, digest := h.call(aSecret, "home_activity", map[string]any{
		"since": time.Now().Add(-time.Hour).Format(time.RFC3339), "limit": 100,
	})
	text := resultText(t, digest)
	if strings.Contains(text, "Deník") {
		t.Fatalf("member A's digest names the private note member B's assistant read:\n%s", text)
	}
	if !strings.Contains(text, "podrobnosti skryty") {
		t.Fatalf("the read event was not redacted for another member:\n%s", text)
	}
}
