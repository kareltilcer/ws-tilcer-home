package chat_test

// The conversation row grew two things in v10.1 (D266, D267): a preview of the last
// message, and a count of what is in the caller's koš.
//
// Both are SCALARS DERIVED FROM ROWS THE CALLER MAY NOT NECESSARILY READ, which is
// the shape §V10-4a's table is entirely about. A preview taken as MAX(id) over the
// conversation is a body from above the caller's floor, printed on the row they see
// before they open anything — worse than the thread's leak would be, because nobody
// has to click. A koš count taken over the table is how many rooms EXIST.

import (
	"net/http"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/chat"
)

// listAs fetches one member's conversation listing.
func (hh *household) listAs(m member, state string) chat.ConversationPage {
	hh.t.Helper()
	path := "/api/chat/conversations"
	if state != "" {
		path += "?state=" + state
	}
	rr := hh.as(m, "GET", path, "")
	if rr.Code != http.StatusOK {
		hh.t.Fatalf("list as %s: %d %s", m.id, rr.Code, rr.Body.String())
	}
	return decode[chat.ConversationPage](hh.t, rr)
}

// row finds one conversation in a listing.
func row(t *testing.T, page chat.ConversationPage, id string) chat.Conversation {
	t.Helper()
	for _, c := range page.Items {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("conversation %s is not in the listing", id)
	return chat.Conversation{}
}

// TestConversationRowPreviewsTheLastMessage is the ordinary path.
func TestConversationRowPreviewsTheLastMessage(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče", andy)
	hh.send(kaja, c.ID, "První")
	last := hh.send(andy, c.ID, "Klíče jsou pod květináčem.")

	got := row(t, hh.listAs(kaja, ""), c.ID)
	if got.LastMessage == nil {
		t.Fatalf("the row carries no preview at all")
	}
	if got.LastMessage.ID != last.ID {
		t.Errorf("the preview is message %s, want the newest one %s", got.LastMessage.ID, last.ID)
	}
	if got.LastMessage.Excerpt != "Klíče jsou pod květináčem." {
		t.Errorf("excerpt = %q", got.LastMessage.Excerpt)
	}
	// ⚠ THE AUTHOR IS ON IT. The design's row reads "Marie: Klíče jsou pod
	// květináčem." — in a room with five people, a preview with no name is a
	// sentence with no idea who said it.
	if got.LastMessage.AuthorLabel != andy.name {
		t.Errorf("author label = %q, want %q", got.LastMessage.AuthorLabel, andy.name)
	}

	// ⚠ AND THE SINGLE-ROOM READ AGREES WITH THE LIST. The client holds this room
	// under its own key and the list rows under another; a `Conversation` whose
	// preview exists on one and not the other loses its line the moment anything
	// refetches the single room, which every send and every read-marker advance does.
	single := decode[chat.Conversation](t, hh.as(kaja, "GET", "/api/chat/conversations/"+c.ID, ""))
	if single.LastMessage == nil || single.LastMessage.ID != last.ID {
		t.Errorf("GET one conversation returned preview %+v, want the same message the list gives",
			single.LastMessage)
	}
}

// TestConversationPreviewNeverCrossesTheFloor is the leak this feature would
// introduce if it were written the obvious way.
func TestConversationPreviewNeverCrossesTheFloor(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče")
	hh.send(kaja, c.ID, "TAJEMSTVÍ PŘED PŘIDÁNÍM")

	if _, err := hh.svc.AddMember(hh.ctx(kaja), c.ID, chat.ConversationMemberAdd{UserID: andy.id}); err != nil {
		t.Fatalf("add member: %v", err)
	}

	// Nothing has been written since the add, so there is no message this member may
	// read — and the row must say so with an absence, never with the room's newest.
	got := row(t, hh.listAs(andy, ""), c.ID)
	if got.LastMessage != nil {
		t.Fatalf("a member added after the last message sees a preview of it: %+v\n"+
			"MAX(id) over a conversation is the newest message the ROOM has, not the newest "+
			"message the CALLER may read.", got.LastMessage)
	}
	// The person who was there all along still sees it.
	if mine := row(t, hh.listAs(kaja, ""), c.ID); mine.LastMessage == nil {
		t.Fatalf("the floor bound the wrong member: %s sees no preview of their own message", kaja.id)
	}

	// And once something IS written above the floor, that is what shows.
	fresh := hh.send(kaja, c.ID, "Po přidání")
	after := row(t, hh.listAs(andy, ""), c.ID)
	if after.LastMessage == nil || after.LastMessage.ID != fresh.ID {
		t.Fatalf("after a message above the floor the preview is %+v, want %s", after.LastMessage, fresh.ID)
	}
}

// TestConversationPreviewOfATombstoneIsStillTheLastMessage.
//
// ⚠ IT IS NOT SKIPPED BACK TO THE NEWEST SURVIVING MESSAGE. A delete blanks the
// body in place and leaves the row (D223); the thread shows *Zpráva byla smazána*
// there, and a list that quietly reached past it would print a line the room no
// longer ends with. The flags travel and the Czech stays in cs.ts.
func TestConversationPreviewOfATombstoneIsStillTheLastMessage(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče", andy)
	hh.send(kaja, c.ID, "Zůstane")
	doomed := hh.send(kaja, c.ID, "Zmizí")

	if err := hh.svc.DeleteMessage(hh.ctx(kaja), doomed.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got := row(t, hh.listAs(andy, ""), c.ID)
	if got.LastMessage == nil || got.LastMessage.ID != doomed.ID {
		t.Fatalf("preview = %+v, want the tombstone %s", got.LastMessage, doomed.ID)
	}
	if !got.LastMessage.Deleted {
		t.Errorf("the tombstone's preview does not say it is deleted")
	}
	if got.LastMessage.Excerpt != "" {
		t.Errorf("the tombstone's preview still carries text: %q — the delete blanks the body "+
			"in place and this reads the same column the thread does", got.LastMessage.Excerpt)
	}
}

// TestConversationPreviewIsNullForARoomWithNothingInIt.
//
// Null covers two situations that read the same on the row — nobody has written
// here, and everything written is below my floor — and neither of them is an empty
// string. The client renders the absence; it does not print a blank line.
func TestConversationPreviewIsNullForARoomWithNothingInIt(t *testing.T) {
	hh := newHousehold(t, kaja)
	c := hh.group(kaja, "Prázdná")

	if got := row(t, hh.listAs(kaja, ""), c.ID); got.LastMessage != nil {
		t.Errorf("an empty room previews %+v, want null", got.LastMessage)
	}
}

// TestConversationPreviewSkipsALeadingBlankLine.
//
// ⚠ `excerpt` CUTS AT THE FIRST NEWLINE, and validateBody trims only the right-hand
// end — so a body somebody began with a Shift+Enter excerpted to "". The row reads an
// empty excerpt as "this message is files only" and printed *0 souborů* under a
// message carrying none. After the trim an empty excerpt means an empty BODY, which a
// message can only have when it has files, so the client's fallback is true again.
func TestConversationPreviewSkipsALeadingBlankLine(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče", andy)
	hh.send(kaja, c.ID, "\n\nKlíče jsou pod květináčem.")

	got := row(t, hh.listAs(andy, ""), c.ID)
	if got.LastMessage == nil {
		t.Fatalf("the row carries no preview at all")
	}
	if got.LastMessage.Excerpt != "Klíče jsou pod květináčem." {
		t.Errorf("excerpt = %q, want the first line that actually says something — an empty one "+
			"is the row's signal for a files-only message, and this message has no files",
			got.LastMessage.Excerpt)
	}
}

// TestATrashedRoomHasNoPreview is memberScope's own koš term, on the one read that
// did not carry it.
//
// ⚠ A CONVERSATION IN THE KOŠ HAS LEFT EVERY READ OF ITS MESSAGES (D253) — the thread,
// search, unread, the reply quote and the attachment listing all refuse it, because
// `c.deleted_at IS NULL` is stated once in memberScope and nothing else has to
// remember. The preview query joined chat_members and not chat_conversations, so the
// koš listing was the one surface handing back a body from a trashed room. Nothing
// rendered it, which is exactly why it needs a test rather than an eye.
func TestATrashedRoomHasNoPreview(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče", andy)
	hh.send(kaja, c.ID, "TAJEMSTVÍ V KOŠI")

	if err := hh.svc.DeleteConversation(hh.ctx(kaja), c.ID, false); err != nil {
		t.Fatalf("trash: %v", err)
	}
	got := row(t, hh.listAs(kaja, "trash"), c.ID)
	if got.LastMessage != nil {
		t.Fatalf("a trashed room previews %+v — the koš listing is the one read of a message "+
			"that did not carry memberScope's `deleted_at IS NULL`", got.LastMessage)
	}
	// And it comes back when the room does.
	if _, err := hh.svc.RestoreConversation(hh.ctx(kaja), c.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if after := row(t, hh.listAs(kaja, ""), c.ID); after.LastMessage == nil {
		t.Errorf("a restored room has no preview — the koš term bound more than the koš")
	}
}

// TestTrashedCountIsTheCallersKošAndNobodyElses is D267's whole risk in one test.
func TestTrashedCountIsTheCallersKošAndNobodyElses(t *testing.T) {
	hh := newHousehold(t, kaja, andy, boss)
	hh.join(andy)
	hh.join(boss)

	mine := hh.group(kaja, "Moje")
	theirs := hh.group(andy, "Jejich")

	if got := hh.listAs(kaja, "").TrashedCount; got != 0 {
		t.Fatalf("an empty koš counts %d", got)
	}
	if err := hh.svc.DeleteConversation(hh.ctx(kaja), mine.ID, false); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if err := hh.svc.DeleteConversation(hh.ctx(andy), theirs.ID, false); err != nil {
		t.Fatalf("trash: %v", err)
	}

	if got := hh.listAs(kaja, "").TrashedCount; got != 1 {
		t.Errorf("%s's koš counts %d, want 1 — a count that included %s's room would be a "+
			"scalar answering how many conversations exist", kaja.id, got, andy.id)
	}
	// ⚠ AN ADMIN GETS NO WIDER ANSWER. D255's two verbs over a foreign room are
	// restore and purge; this is a read, and it stays member-scoped like every other
	// one in the module.
	if got := hh.listAs(boss, "").TrashedCount; got != 0 {
		t.Errorf("an admin who is in neither room counts %d trashed conversations, want 0", got)
	}
	// It rides both listings, because the sidebar hides an empty koš and only the
	// ACTIVE request is one it always makes.
	if got := hh.listAs(kaja, "trash").TrashedCount; got != 1 {
		t.Errorf("?state=trash reports %d, want the same 1 the active listing reports", got)
	}
}
