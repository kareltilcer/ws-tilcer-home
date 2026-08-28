package chat_test

// Regressions for the v10 PR 2 review. Each test here pins a defect that shipped
// green — the suite passed with every one of them present — which is the argument
// for the tests rather than for the fixes.

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/chat"
)

// ⚠ THE BUG WAS A 500 ON ORDINARY CZECH, NOT ON AN ATTACK. The raw query went
// straight into `chat_messages_fts MATCH ?`, so an apostrophe, a question mark, a
// time or a hyphen was FTS5 SYNTAX rather than text: `mama's` and `co?` are "fts5:
// syntax error", `9:30` and `a-b` are "no such column".
//
// The assertion is the STATUS, deliberately, not the hits. What each of these
// should match is a question about the tokenizer; what none of them may do is fail
// the request.
func TestSearchSurvivesOrdinaryPunctuation(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	hh.join(kaja)
	hh.join(andy)
	room := hh.group(kaja, "Dovolená", andy)
	hh.send(kaja, room.ID, "mama volala v 9:30, prijdu pozdeji")

	for _, q := range []string{
		"mama's", "co?", "9:30", "a-b", `"`, ":-)", "-", "*", "AND", "NEAR(", "mama",
	} {
		rr := hh.as(kaja, "GET", "/api/chat/search?q="+url.QueryEscape(q), "")
		if rr.Code != http.StatusOK {
			t.Errorf("search %q: %d %s — punctuation must not be FTS5 syntax", q, rr.Code, rr.Body.String())
		}
	}

	// And it still searches: the sanitiser quotes tokens, it does not discard them.
	rr := hh.as(kaja, "GET", "/api/chat/search?q="+url.QueryEscape("mama"), "")
	page := decode[chat.SearchPage](t, rr)
	if len(page.Items) != 1 {
		t.Fatalf("search for a word that IS in the room returned %d hits, want 1", len(page.Items))
	}

	// A query with nothing searchable in it is an empty page, never a MATCH on the
	// empty string — whose behaviour FTS5 leaves unspecified.
	empty := decode[chat.SearchPage](t, hh.as(kaja, "GET", "/api/chat/search?q="+url.QueryEscape(":-)"), ""))
	if len(empty.Items) != 0 {
		t.Errorf("punctuation-only query returned %d hits, want an empty page", len(empty.Items))
	}
}

// ⚠ THE SECOND DELETE USED TO SUCCEED AND COST SOMETHING. TrashConversation's own
// `deleted_at IS NULL` made the UPDATE a no-op, so nothing looked wrong — but the
// service still wrote a second "přesunuta do koše" to the Log and re-queued every
// object key with a purge_after of now + TrashDays, while deleted_at (and the
// countdown the koš row renders from it) stayed where it was.
func TestSoftDeletingATrashedConversationIsRefused(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	hh.join(kaja)
	room := hh.group(kaja, "Omyl")

	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+room.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("first delete: %d %s", rr.Code, rr.Body.String())
	}
	before := auditCount(t, hh.db)

	rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+room.ID, "")
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("second delete: %d %s, want 422", rr.Code, rr.Body.String())
	}
	if after := auditCount(t, hh.db); after != before {
		t.Errorf("a refused delete wrote %d audit events; the Log must not gain a second „do koše\"", after-before)
	}

	// ⚠ AND `hard` IS NOT REFUSED. Smazat natrvalo is reached FROM the koš — it is
	// the one verb whose whole job is to act on an already-trashed room, and a guard
	// that caught it would trap somebody deleting a heavy conversation to free space.
	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+room.ID+"?hard=true", ""); rr.Code != http.StatusNoContent {
		t.Fatalf("purge from the koš: %d %s", rr.Code, rr.Body.String())
	}
}

// The mirror: restoring a room that is not in the koš changes nothing and says
// nothing, rather than logging a restore that restored nothing.
func TestRestoringALiveConversationWritesNoEvent(t *testing.T) {
	hh := newHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Živá")
	before := auditCount(t, hh.db)

	rr := hh.as(kaja, "POST", "/api/chat/conversations/"+room.ID+"/restore", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("restore of a live room: %d %s", rr.Code, rr.Body.String())
	}
	if after := auditCount(t, hh.db); after != before {
		t.Errorf("restoring a live conversation wrote %d audit events, want 0", after-before)
	}
}

// ⚠ next_cursor AND THE PAGE COME FROM THE SAME QUERY. The list used to declare
// `limit` and `cursor` in the spec and honour neither, which is page one dressed as
// the whole result — the failure Search refuses one file away by answering an
// unhonourable cursor with 422.
func TestConversationListPagesOnLimitAndCursor(t *testing.T) {
	hh := newHousehold(t, kaja)
	hh.join(kaja)
	for _, name := range []string{"Jedna", "Dvě", "Tři"} {
		hh.group(kaja, name)
	}

	first := decode[chat.ConversationPage](t, hh.as(kaja, "GET", "/api/chat/conversations?limit=2", ""))
	if len(first.Items) != 2 {
		t.Fatalf("limit=2 returned %d rooms, want 2", len(first.Items))
	}
	if first.NextCursor == nil {
		t.Fatal("a truncated page must carry next_cursor, or the rest is unreachable")
	}

	rest := decode[chat.ConversationPage](t, hh.as(kaja,
		"GET", "/api/chat/conversations?limit=2&cursor="+url.QueryEscape(*first.NextCursor), ""))
	if len(rest.Items) == 0 {
		t.Fatal("the second page is empty; the cursor did not advance")
	}

	// No row appears on both pages, and no row is skipped: four rooms exist (three
	// groups plus Všichni) and the two pages must be exactly those four.
	seen := map[string]bool{}
	for _, c := range append(append([]chat.Conversation{}, first.Items...), rest.Items...) {
		if seen[c.ID] {
			t.Errorf("conversation %s appears on both pages", c.ID)
		}
		seen[c.ID] = true
	}
	if len(seen) != 4 {
		t.Errorf("paged over %d distinct rooms, want 4 (three groups + Všichni)", len(seen))
	}

	// A cursor this endpoint did not mint is REFUSED, not ignored.
	if rr := hh.as(kaja, "GET", "/api/chat/conversations?cursor=nonsense", ""); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("malformed cursor: %d %s, want 422", rr.Code, rr.Body.String())
	}
}

// ⚠ THE EMAIL ARRIVED INSIDE THE DISPLAY NAME, WHICH IS WHY THE SHAPE ARGUMENT WAS
// NOT ENOUGH. types.go says chat's wire types carry no email because there is no
// field to fill in — true, and beside the point: push.Store.Members substitutes the
// EMAIL into DisplayName when sessions.display_name is NULL, so it came through the
// one field chat does copy. D230 makes this the first surface in Home that shows the
// directory to a non-admin.
//
// TestDirectoryCarriesNoEmailAndNoRoles covers the ordinary row; this covers the row
// that has no name of its own.
func TestDirectoryFallsBackToTheIdRatherThanTheEmail(t *testing.T) {
	// ⚠ THE DISPLAY NAME **IS** THE EMAIL HERE, AND THAT IS THE WHOLE FIXTURE. It
	// is not a contrived value: it is exactly what push.Store.Members hands chat
	// when sessions.display_name is NULL, because that projection substitutes one
	// for the other. A fixture with an EMPTY display name would pass against the
	// old code too and prove nothing.
	nameless := member{"u-nameless", "u-nameless@example.test", []string{"editor"}}
	hh := newHousehold(t, kaja, nameless)
	hh.join(kaja)

	rr := hh.as(kaja, "GET", "/api/chat/directory", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("directory: %d %s", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); indexOf(body, "@example.test") >= 0 {
		t.Errorf("the directory carries an email address:\n%s", body)
	}

	dir := decode[chat.Directory](t, rr)
	for _, e := range dir.Items {
		if e.UserID == nameless.id && e.DisplayName != nameless.id {
			t.Errorf("a member with no display name renders as %q, want their id", e.DisplayName)
		}
	}

	// And the same substitution must not reach a message's author label either —
	// labels() and Directory() are two call sites of one rule.
	hh.join(nameless)
	room := hh.group(nameless, "Bez jména")
	msg := hh.send(nameless, room.ID, "ahoj")
	if indexOf(msg.AuthorLabel, "@") >= 0 {
		t.Errorf("author label is %q, want no address", msg.AuthorLabel)
	}
}
