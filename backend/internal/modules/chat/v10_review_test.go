package chat_test

// Regressions for the v10 PR 2 review. Each test here pins a defect that shipped
// green — the suite passed with every one of them present — which is the argument
// for the tests rather than for the fixes.

import (
	"net/http"
	"net/url"
	"strings"
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

// ---- the fourth review ----

// ⚠ `?hard=` IS NOT `?hard`. url.Values decodes both to [""], so reading the bare
// flag as "present with an empty value" made the two spellings one — and one of
// them is an irreversible purge. `?hard=${flag}` is what any client emits when its
// variable is empty, so the reversible verb and the destructive one were a template
// substitution apart.
func TestAnEmptyHardValueTrashesRatherThanPurges(t *testing.T) {
	hh := newHousehold(t, kaja, andy)

	empty := hh.group(kaja, "Prázdná hodnota", andy)
	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+empty.ID+"?hard=", ""); rr.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rr.Code, rr.Body.String())
	}
	var trashed int
	if err := hh.db.QueryRow(
		`SELECT COUNT(*) FROM chat_conversations WHERE id = ? AND deleted_at IS NOT NULL`,
		empty.ID).Scan(&trashed); err != nil {
		t.Fatalf("read the koš: %v", err)
	}
	if trashed != 1 {
		t.Errorf("?hard= left %d trashed rows, want 1 — an empty value purged the room", trashed)
	}

	// And the spellings that DO mean it still purge: the fix must not make
	// Smazat natrvalo unreachable.
	for _, spelling := range []string{"?hard=true", "?hard=1", "?hard"} {
		room := hh.group(kaja, "Natrvalo "+spelling, andy)
		if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+room.ID+spelling, ""); rr.Code != http.StatusNoContent {
			t.Fatalf("%s: %d %s", spelling, rr.Code, rr.Body.String())
		}
		var left int
		if err := hh.db.QueryRow(
			`SELECT COUNT(*) FROM chat_conversations WHERE id = ?`, room.ID).Scan(&left); err != nil {
			t.Fatalf("count rows: %v", err)
		}
		if left != 0 {
			t.Errorf("%s left %d rows, want a purge", spelling, left)
		}
	}
}

// ⚠ AN EMPTIED GROUP IS NOT A DELETED ONE. It is a live row that has left every
// read there is: not trashed, so absent from the koš, and every listing is a
// membership join, so neither its former member nor an admin can reach it again.
// Its bytes would go on counting against chat.total with nothing able to free them.
//
// ⚠ AND THE REFUSAL MUST NOT BECOME A MEMBERSHIP ORACLE. Removing somebody who is
// not in a one-member room still has to answer the same 404 an unknown id gets —
// which is why the count is consulted only when the target is the caller.
func TestTheLastMemberOfAGroupCannotLeave(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	room := hh.group(kaja, "Sám")

	rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+room.ID+"/members/"+kaja.id, "")
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("the last member leaving = %d, want 422 (%s)", rr.Code, rr.Body.String())
	}
	var n int
	if err := hh.db.QueryRow(
		`SELECT COUNT(*) FROM chat_members WHERE conversation_id = ?`, room.ID).Scan(&n); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if n != 1 {
		t.Errorf("the room has %d members, want the one who could not leave", n)
	}

	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+room.ID+"/members/"+andy.id, ""); rr.Code != http.StatusNotFound {
		t.Errorf("removing a non-member from a one-member room = %d, want 404 — a 422 here "+
			"would say how many people are in a room the caller is asking about", rr.Code)
	}

	// With somebody else in it, leaving is ordinary again.
	if _, err := hh.svc.AddMember(hh.ctx(kaja), room.ID,
		chat.ConversationMemberAdd{UserID: andy.id}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := hh.svc.RemoveMember(hh.ctx(kaja), room.ID, kaja.id); err != nil {
		t.Errorf("leaving a two-member room: %v", err)
	}
}

// ⚠ THE FRAME REPLACES THE WHOLE MESSAGE, so a field the edit omits is a field that
// DISAPPEARS from every client's bubble. replaceMessage swaps the cached object
// outright; it does not merge. An edit that silently unquoted its own reply is what
// found it.
func TestAnEditStillCarriesItsQuote(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	room := hh.group(kaja, "Citace", andy)
	parent := hh.send(kaja, room.ID, "otázka")

	reply, err := hh.svc.SendMessage(hh.ctx(andy), room.ID,
		chat.MessageCreate{Body: "odpověď", ReplyToID: &parent.ID})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	edited, err := hh.svc.EditMessage(hh.ctx(andy), reply.ID, chat.MessageUpdate{Body: "opravená odpověď"})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if edited.ReplyTo == nil {
		t.Fatalf("the edit dropped reply_to entirely — the quote vanishes until a refetch")
	}
	if !edited.ReplyTo.Available || edited.ReplyTo.ID != parent.ID {
		t.Errorf("the edit's quote = %+v, want the parent %q", edited.ReplyTo, parent.ID)
	}
}

// ⚠ A MEMBERSHIP CHANGE IS A STRUCTURAL VERB, and the room hears about it — the
// same defect publishConversation was added for on rename and trash. Without it the
// other members hold a panel and a member_count one person out of date.
//
// The SPLIT is what keeps D221: the frame that names a person goes to that person
// alone, and the room gets the frame that names nobody.
func TestAddingAMemberTellsTheRestOfTheRoom(t *testing.T) {
	hh := newHousehold(t, kaja, andy, quiet)
	room := hh.group(kaja, "Skupina", quiet)

	hh.notify.reset()
	if _, err := hh.svc.AddMember(hh.ctx(kaja), room.ID,
		chat.ConversationMemberAdd{UserID: andy.id}); err != nil {
		t.Fatalf("add member: %v", err)
	}

	var toAdded, toRoom []string
	for i, typ := range hh.notify.types {
		switch typ {
		case "chat_membership.changed":
			toAdded = hh.notify.audiences[i]
		case "chat_conversation.changed":
			toRoom = hh.notify.audiences[i]
		}
	}
	if len(toAdded) != 1 || toAdded[0] != andy.id {
		t.Errorf("the membership frame went to %v, want only the added member", toAdded)
	}
	if len(toRoom) != 2 {
		t.Errorf("the room's frame went to %v, want the two who were already in it", toRoom)
	}
	for _, id := range toRoom {
		if id == andy.id {
			t.Errorf("the added member is in the room's audience %v as well — the two frames "+
				"refetch the same keys, so they would be told twice", toRoom)
		}
	}
}

// ⚠ THE FLOOR IS AN ID BOUND, AND THE UI MAY NOT RE-DERIVE IT FROM A CLOCK. The
// floor line and the members panel used to compare effective_from against the room's
// created_at — a second spelling of one access rule, which floor.go forbids, and one
// that DISAGREES: adding somebody to a room with no messages yet writes
// effective_from = now over an EMPTY bound, so the timestamps claimed history was
// withheld where the server says they read all of it.
func TestReadsFromBeginningIsAnsweredRatherThanInferred(t *testing.T) {
	hh := newHousehold(t, kaja, andy)

	// A room with nothing in it: the floor id is '' however late the add lands.
	empty := hh.group(kaja, "Prázdná")
	if _, err := hh.svc.AddMember(hh.ctx(kaja), empty.ID,
		chat.ConversationMemberAdd{UserID: andy.id}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	got, err := hh.svc.GetConversation(hh.ctx(andy), empty.ID)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if !got.ReadsFromBeginning {
		t.Errorf("reads_from_beginning = false for a member added to an EMPTY room, who can "+
			"read all of it (effective_from %s, created_at %s)", got.EffectiveFrom, got.CreatedAt)
	}

	// A room with history: the floor is real and has to say so.
	busy := hh.group(kaja, "Plná")
	hh.send(kaja, busy.ID, "tajemství")
	if _, err := hh.svc.AddMember(hh.ctx(kaja), busy.ID,
		chat.ConversationMemberAdd{UserID: andy.id}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if got, err = hh.svc.GetConversation(hh.ctx(andy), busy.ID); err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if got.ReadsFromBeginning {
		t.Error("reads_from_beginning = true for a member added ABOVE real history")
	}

	// And per member in the panel: the founder reads all of it, the late arrival does not.
	members, err := hh.svc.ListMembers(hh.ctx(andy), busy.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	for _, m := range members.Items {
		want := m.UserID == kaja.id
		if m.ReadsFromBeginning != want {
			t.Errorf("%s reads_from_beginning = %v, want %v", m.UserID, m.ReadsFromBeginning, want)
		}
	}
}

// ⚠ A VERDICT CANNOT BE MORE CERTAIN THAN THE FIGURE IT JUDGES (D161). `bytes` is
// null until PR 3 measures it, so `over_conversation_limit` shipping a hard false
// was the same lie one field up, in boolean form.
func TestOverConversationLimitIsNullWhileBytesIs(t *testing.T) {
	hh := newHousehold(t, kaja)
	room := hh.group(kaja, "Neměřeno")
	body := hh.as(kaja, "GET", "/api/chat/conversations/"+room.ID, "").Body.String()
	if !strings.Contains(body, `"bytes":null`) {
		t.Fatalf("bytes is no longer null, so this test no longer asks anything: %s", body)
	}
	if !strings.Contains(body, `"over_conversation_limit":null`) {
		t.Errorf("over_conversation_limit is a verdict about an unmeasured figure: %s", body)
	}
}

// ⚠ CREATING A ROOM AROUND SOMEBODY IS AN ADDITION, AND IT IS ANNOUNCED LIKE ONE
// (the fifth review). AddMember publishes to the person it happened to precisely
// because their client is holding a conversation list that does not contain the
// room and has no reason to refetch it — and CreateConversation with `member_ids`
// puts them in the identical position while publishing nothing at all, so the room
// and its unread badge stayed invisible to them until a refetch-on-focus or the
// first message. It was invisible only because the create dialog sent no
// member_ids; it picks the founding members now, so this frame is on the live path
// rather than ahead of it.
//
// The creator is deliberately NOT told: they are holding the response.
func TestCreatingAConversationTellsTheMembersItAdds(t *testing.T) {
	hh := newHousehold(t, kaja, andy, quiet)

	hh.notify.reset()
	room, err := hh.svc.CreateConversation(hh.ctx(kaja), chat.ConversationCreate{
		Name: "Založená kolem nich", MemberIDs: []string{andy.id, quiet.id},
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	told := map[string]int{}
	for i, typ := range hh.notify.types {
		if typ != "chat_membership.changed" {
			t.Errorf("create published %q, want only chat_membership.changed", typ)
			continue
		}
		ev, ok := hh.notify.payloads[i].(chat.MembershipEvent)
		if !ok || ev.ConversationID != room.ID || ev.Removed {
			t.Errorf("payload = %+v, want a join for %s", hh.notify.payloads[i], room.ID)
		}
		// ⚠ ADDRESSED TO THEM. A membership frame names a person, so it goes to
		// that person alone — the D221 split AddMember already keeps.
		if got := hh.notify.audiences[i]; len(got) != 1 || got[0] != ev.UserID {
			t.Errorf("the membership frame went to %v, want only %s", got, ev.UserID)
		}
		told[ev.UserID]++
	}
	for _, m := range []member{andy, quiet} {
		if told[m.id] != 1 {
			t.Errorf("%s was told %d times, want exactly once", m.id, told[m.id])
		}
	}
	if told[kaja.id] != 0 {
		t.Errorf("the creator was told %d times; they are holding the response", told[kaja.id])
	}
}

// ---- reported from the household ----

// ⚠ A BUBBLE WITH NOTHING WRITTEN IN IT, AND IT LED THE PICKER. `push.Store.Members`
// substituted the email only when the display name was exactly empty, so a name of
// spaces survived as itself — past chat's directoryName, which detects that one
// substitution and nothing else — and the add-member picker drew a member nobody
// could read or recognise. It drew it first, because a space sorts before every
// letter. The projection now trims before its fallback and this asserts the result
// at chat's own boundary, where the interface (not push.Store) is what chat has.
func TestDirectoryDoesNotListAMemberWithABlankName(t *testing.T) {
	// Blank in the two shapes a profile field produces: spaces, and a non-breaking
	// space that looks identical and is not one.
	spaces := member{"u-spaces", "   ", []string{"editor"}}
	nbsp := member{"u-nbsp", " ", []string{"editor"}}
	hh := newHousehold(t, kaja, spaces, nbsp)
	hh.join(kaja)

	dir := decode[chat.Directory](t, hh.as(kaja, "GET", "/api/chat/directory", ""))
	for _, e := range dir.Items {
		if strings.TrimSpace(e.DisplayName) == "" {
			t.Errorf("the directory lists %s as %q — a bubble with nothing in it", e.UserID, e.DisplayName)
		}
	}
	byID := map[string]string{}
	for _, e := range dir.Items {
		byID[e.UserID] = e.DisplayName
	}
	for _, m := range []member{spaces, nbsp} {
		if byID[m.id] != m.id {
			t.Errorf("%s renders as %q, want their id", m.id, byID[m.id])
		}
	}

	// ⚠ And not on the member row either, which is the same projection reaching a
	// second surface: a blank name there takes the avatar's initial with it.
	room := hh.group(kaja, "Skupina", spaces)
	members := decode[chat.ConversationMemberList](t,
		hh.as(kaja, "GET", "/api/chat/conversations/"+room.ID+"/members", ""))
	for _, m := range members.Items {
		if strings.TrimSpace(m.DisplayName) == "" {
			t.Errorf("member %s renders as %q on the panel row", m.UserID, m.DisplayName)
		}
	}
}

// ⚠ THE PICKER AND THE MEMBERSHIP GATE ARE ONE SET (the same rule
// TestAMemberTheDirectoryListsCanBeAdded states, from the other end). AddMember
// answers an empty user id with 422, so a directory row carrying one is a button
// that can only fail — and CreateConversation, which checks that same projection
// for "is this person in the directory", would have accepted it and written a
// membership row for nobody.
//
// ⚠ AddMember's TEST IS `strings.TrimSpace(in.UserID) == ""`, SO THIS ONE IS TOO.
// The first filter here read `!= ""`, which is a DIFFERENT question: an id of spaces
// passed it, led the picker as a live button, met the 422 anyway — and went through
// CreateConversation, which has no trim of its own, to leave a real chat_members row
// for nobody. Two spellings of the same gate is how the gate reopens.
func TestDirectoryDoesNotListARowWithNoUserID(t *testing.T) {
	nobody := member{"", "Bez ID", []string{"editor"}}
	ghost := member{"  ", "Duch", []string{"editor"}}
	hh := newHousehold(t, kaja, nobody, ghost)
	hh.join(kaja)

	dir := decode[chat.Directory](t, hh.as(kaja, "GET", "/api/chat/directory", ""))
	for _, e := range dir.Items {
		if strings.TrimSpace(e.UserID) == "" {
			t.Errorf("the directory lists a row with no user id: %+v", e)
		}
	}

	for _, id := range []string{"", "  "} {
		if _, err := hh.svc.CreateConversation(hh.ctx(kaja), chat.ConversationCreate{
			Name: "Kolem nikoho", MemberIDs: []string{id},
		}); err == nil {
			t.Errorf("CreateConversation accepted member id %q; AddMember answers one with 422", id)
		}
	}
}
