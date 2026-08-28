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

// ---- the second review pass ----

// ⚠ THE DIRECTORY AND THE MEMBERSHIP GATE ARE ONE SET, OR THEY ARE A DEAD BUTTON.
// The first review narrowed labels() to keep push.Store.Members' email substitution
// out of chat — correctly — and left that same map serving as "who is in the
// directory". A member whose sessions.display_name is NULL then appeared in the
// picker under their id and answered every click with 422.
func TestAMemberTheDirectoryListsCanBeAdded(t *testing.T) {
	nameless := member{"u-nameless", "u-nameless@example.test", []string{"editor"}}
	hh := newHousehold(t, kaja, nameless)
	hh.join(kaja)

	dir := decode[chat.Directory](t, hh.as(kaja, "GET", "/api/chat/directory", ""))
	listed := false
	for _, e := range dir.Items {
		if e.UserID == nameless.id {
			listed = true
		}
	}
	if !listed {
		t.Fatal("the fixture is wrong: the nameless member is not in the directory at all")
	}

	room := hh.group(kaja, "Skupina")
	if rr := hh.as(kaja, "POST", "/api/chat/conversations/"+room.ID+"/members",
		`{"user_id":"u-nameless"}`); rr.Code != http.StatusOK {
		t.Errorf("adding a member the directory lists returned %d %s — the picker renders "+
			"exactly these rows, so this is a button that cannot work", rr.Code, rr.Body.String())
	}
	// The same set, through the other door.
	if rr := hh.as(kaja, "POST", "/api/chat/conversations",
		`{"name":"Druhá","member_ids":["u-nameless"]}`); rr.Code != http.StatusCreated {
		t.Errorf("creating a conversation with them returned %d %s", rr.Code, rr.Body.String())
	}
	// And still no address anywhere: the id is the fallback, not the email.
	if body := hh.as(kaja, "GET", "/api/chat/directory", "").Body.String(); indexOf(body, "@example.test") >= 0 {
		t.Errorf("widening the gate reintroduced the email leak:\n%s", body)
	}
}

// ⚠ THE READ MARKER IS AN ID FOR THE REASON THE FLOOR IS. Against `created_at`, a
// message committing in the same millisecond as the one somebody marked read is
// excluded by `created_at > last_read_at` — and because the marker only ever moves
// forward, no later read can bring it back. It is unread that can never be counted.
func TestUnreadSurvivesTwoMessagesInOneMillisecond(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	hh.join(kaja)
	hh.join(andy)
	room := hh.group(kaja, "Skupina", andy)

	first := hh.send(andy, room.ID, "první")
	second := hh.send(andy, room.ID, "druhá")
	// Force the tie a burst of sends produces on its own: nowUTC has millisecond
	// resolution and the pool is capped at one connection.
	if _, err := hh.db.Exec(`UPDATE chat_messages SET created_at = ? WHERE id IN (?, ?)`,
		first.CreatedAt, first.ID, second.ID); err != nil {
		t.Fatalf("tie the timestamps: %v", err)
	}

	state, err := hh.svc.AdvanceRead(hh.ctx(kaja), room.ID, chat.ReadUpdate{UntilMessageID: first.ID})
	if err != nil {
		t.Fatalf("advance read: %v", err)
	}
	if state.UnreadCount != 1 {
		t.Errorf("unread_count = %d, want 1 — %s shares a millisecond with the marker and "+
			"a timestamp comparison loses it permanently", state.UnreadCount, second.ID)
	}
	// The list agrees with the badge: both bounds are the same two columns.
	page, err := hh.svc.ListConversations(hh.ctx(kaja), "", "", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, c := range page.Items {
		if c.ID == room.ID && c.UnreadCount != 1 {
			t.Errorf("the list says %d unread where the badge says 1", c.UnreadCount)
		}
	}
}

// ⚠ ADDING SOMEBODY ALREADY IN THE ROOM IS A NO-OP, AND IT MUST NARRATE ITSELF AS
// ONE. InsertMember's ON CONFLICT discards the write — correctly, since re-running
// it would push an existing member's floor forward and cut them off from history
// they can already read — but the audit event and the /ws frame went out anyway, so
// the Log recorded an addition that never happened.
func TestReAddingAnExistingMemberRecordsNothing(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	hh.join(kaja)
	hh.join(andy)
	room := hh.group(kaja, "Skupina", andy)
	hh.send(kaja, room.ID, "zpráva, kterou Andy vidí")

	var floorBefore string
	if err := hh.db.QueryRow(
		`SELECT effective_from_id FROM chat_members WHERE conversation_id = ? AND user_id = ?`,
		room.ID, andy.id).Scan(&floorBefore); err != nil {
		t.Fatalf("read floor: %v", err)
	}
	events, frames := auditCount(t, hh.db), len(hh.notify.types)

	if rr := hh.as(kaja, "POST", "/api/chat/conversations/"+room.ID+"/members",
		`{"user_id":"u-andy"}`); rr.Code != http.StatusOK {
		t.Fatalf("re-adding an existing member returned %d, want 200 (idempotent)", rr.Code)
	}

	if n := auditCount(t, hh.db); n != events {
		t.Errorf("audit rows %d → %d: the Log recorded an addition that did not happen", events, n)
	}
	if n := len(hh.notify.types); n != frames {
		t.Errorf("%d /ws frames for a membership that did not change", n-frames)
	}
	var floorAfter string
	_ = hh.db.QueryRow(
		`SELECT effective_from_id FROM chat_members WHERE conversation_id = ? AND user_id = ?`,
		room.ID, andy.id).Scan(&floorAfter)
	if floorAfter != floorBefore {
		t.Errorf("the existing member's floor moved from %q to %q — re-adding must never "+
			"cut somebody off from history they can already read", floorBefore, floorAfter)
	}
}

// ⚠ THE ADDED MEMBER IS TOLD, exactly as the removed one is. MembershipEvent
// declared a `Removed` bool and only ever published the true case, so the frame
// applyChatFrame already handles never arrived for an add — the new member's tab
// held a conversation list without the room and had no reason to refetch.
func TestAddedMemberIsToldOverTheSocket(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	hh.join(kaja)
	hh.join(andy)
	room := hh.group(kaja, "Skupina")

	before := len(hh.notify.types)
	if _, err := hh.svc.AddMember(hh.ctx(kaja), room.ID,
		chat.ConversationMemberAdd{UserID: andy.id}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	found := false
	for i := before; i < len(hh.notify.types); i++ {
		if hh.notify.types[i] != "chat_membership.changed" {
			continue
		}
		found = true
		// ⚠ TO THEM SPECIFICALLY. Nobody else's list changes shape, and the audience
		// for a membership change is the person it happened to.
		if got := hh.notify.audiences[i]; len(got) != 1 || got[0] != andy.id {
			t.Errorf("membership frame went to %v, want only %s", got, andy.id)
		}
		ev, ok := hh.notify.payloads[i].(chat.MembershipEvent)
		if !ok || ev.Removed || ev.ConversationID != room.ID {
			t.Errorf("payload = %+v, want the add case for %s", hh.notify.payloads[i], room.ID)
		}
	}
	if !found {
		t.Error("adding a member published no chat_membership.changed frame")
	}
}

// ⚠ A PAGE IS NEWEST-FIRST WHICHEVER WAY IT WAS WALKED. `forward` has to READ
// ascending, but returning it that way gives one endpoint two contracts — and every
// client invariant is built on one: items[0] is the newest message, which is what
// the gap check compares and what a live message is unshifted in front of.
func TestBothDirectionsReturnNewestFirst(t *testing.T) {
	hh := newHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Skupina")
	var ids []string
	for _, body := range []string{"a", "b", "c", "d"} {
		ids = append(ids, hh.send(kaja, room.ID, body).ID)
	}
	newest := ids[len(ids)-1]

	for _, direction := range []string{"backward", "forward", ""} {
		page, err := hh.svc.Thread(hh.ctx(kaja), room.ID, direction, "", 10)
		if err != nil {
			t.Fatalf("thread %q: %v", direction, err)
		}
		if len(page.Items) == 0 || page.Items[0].ID != newest {
			t.Errorf("direction=%q put %q at items[0], want the newest message %q",
				direction, page.Items[0].ID, newest)
		}
	}

	// And the cursor still points the way it is travelling: forward continues from
	// the newest row it returned, backward from the oldest.
	fwd, err := hh.svc.Thread(hh.ctx(kaja), room.ID, "forward", "", 2)
	if err != nil {
		t.Fatalf("forward page: %v", err)
	}
	if !fwd.HasMore || fwd.NextCursor == nil || *fwd.NextCursor != fwd.Items[0].ID {
		t.Errorf("forward next_cursor = %v, want the newest of the page (%q) — reading the "+
			"far end of the slice walks back over ground already covered",
			fwd.NextCursor, fwd.Items[0].ID)
	}
	back, err := hh.svc.Thread(hh.ctx(kaja), room.ID, "backward", "", 2)
	if err != nil {
		t.Fatalf("backward page: %v", err)
	}
	if !back.HasMore || back.NextCursor == nil || *back.NextCursor != back.Items[len(back.Items)-1].ID {
		t.Errorf("backward next_cursor = %v, want the oldest of the page", back.NextCursor)
	}
}
