package chat_test

// Reactions (v10.1, D265) — written from the attacker's side, like everything else
// in this package.
//
// A reaction is the smallest thing chat stores and it inherits every rule the
// largest one has: the membership refusal is a 404, the audience is bounded by the
// floor, and nothing about it reaches the Log. What is NEW here is that a reaction
// is the first datum in the module a member writes onto SOMEBODY ELSE'S row — so
// "whose message is it" stops being the write gate, and the tests say so explicitly
// rather than leaving the absence of an authorship check looking like an oversight.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/chat"
)

// heart is the palette's ❤️ — TWO code points (U+2764 U+FE0F), spelled once here so
// no test in this file can accidentally assert against the bare U+2764.
const heart = "❤️"

// TestReactionRoundTrips is the ordinary path: add, read back, remove.
func TestReactionRoundTrips(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče", andy)
	msg := hh.send(kaja, c.ID, "Klíče jsou pod květináčem.")

	rr := hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions", `{"emoji":"`+heart+`","reacted":true}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("react: %d %s", rr.Code, rr.Body.String())
	}
	out := decode[chat.Message](t, rr)
	if len(out.Reactions) != 1 || out.Reactions[0].Emoji != heart {
		t.Fatalf("after reacting the message carries %+v, want one %s chip", out.Reactions, heart)
	}
	if len(out.Reactions[0].By) != 1 || out.Reactions[0].By[0].UserID != andy.id {
		t.Fatalf("the chip's reactors are %+v, want just %s", out.Reactions[0].By, andy.id)
	}
	// ⚠ THE LABEL IS THE POINT OF `By` BEING A STRUCT. The design puts who reacted
	// under the cursor, and a client that had to resolve six user ids against the
	// members list to draw a tooltip would resolve them wrongly for anybody who has
	// since left the room.
	if out.Reactions[0].By[0].Label != andy.name {
		t.Errorf("the reactor's label is %q, want %q", out.Reactions[0].By[0].Label, andy.name)
	}

	// The thread agrees with the response — the same chip, loaded the other way.
	page := decode[chat.MessagePage](t, hh.as(kaja, "GET", "/api/chat/conversations/"+c.ID+"/messages", ""))
	if len(page.Items) != 1 || len(page.Items[0].Reactions) != 1 {
		t.Fatalf("the thread renders %+v, want the message carrying one chip", page.Items)
	}

	off := hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions", `{"emoji":"`+heart+`","reacted":false}`)
	if off.Code != http.StatusOK {
		t.Fatalf("un-react: %d %s", off.Code, off.Body.String())
	}
	if got := decode[chat.Message](t, off); len(got.Reactions) != 0 {
		t.Fatalf("after removing, the message still carries %+v", got.Reactions)
	}
}

// TestReactionIsIdempotentInBothDirections is what makes the double-tap gesture
// safe.
//
// ⚠ A GESTURE FIRES TWICE FAR MORE EASILY THAN A BUTTON DOES — a bounced finger, a
// retried request, a slow uplink — and a TOGGLE applied twice lands on the opposite
// of what the member meant, with a chip that silently disappeared as the only sign.
// The route takes the desired state instead, so a replay is a no-op. This test is
// the reason the route is a PUT.
func TestReactionIsIdempotentInBothDirections(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče", andy)
	msg := hh.send(kaja, c.ID, "Ahoj")

	for range 3 {
		if rr := hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions",
			`{"emoji":"👍","reacted":true}`); rr.Code != http.StatusOK {
			t.Fatalf("repeated add: %d %s", rr.Code, rr.Body.String())
		}
	}
	out := decode[chat.Message](t, hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions",
		`{"emoji":"👍","reacted":true}`))
	if len(out.Reactions) != 1 || len(out.Reactions[0].By) != 1 {
		t.Fatalf("four identical adds produced %+v, want one reactor on one chip", out.Reactions)
	}

	for range 3 {
		if rr := hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions",
			`{"emoji":"👍","reacted":false}`); rr.Code != http.StatusOK {
			t.Fatalf("repeated remove: %d %s", rr.Code, rr.Body.String())
		}
	}
}

// TestReactionsAreNotAnAuthorshipDecision states the asymmetry with edit and delete.
//
// Edit and delete are the author's alone and answer 404 to anybody else, which is
// how a stranger's message stays a stranger's. A reaction is the opposite by
// design: it is the whole point that other people leave it.
func TestReactionsAreNotAnAuthorshipDecision(t *testing.T) {
	hh := newHousehold(t, kaja, andy, quiet)
	c := hh.group(kaja, "Rodiče", andy, quiet)
	msg := hh.send(kaja, c.ID, "Ahoj")

	// ⚠ INCLUDING THE READER (D222). Chat is the first module in Home where a
	// reader writes, and a reaction is the smallest write there is — a role gate
	// here would be the only one in the module besides the clean-up page.
	for _, m := range []member{andy, quiet, kaja} {
		rr := hh.as(m, "PUT", "/api/chat/messages/"+msg.ID+"/reactions", `{"emoji":"🙏","reacted":true}`)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s reacting to %s's message: %d %s", m.id, kaja.id, rr.Code, rr.Body.String())
		}
	}
	out := decode[chat.Message](t, hh.as(kaja, "PUT", "/api/chat/messages/"+msg.ID+"/reactions",
		`{"emoji":"🙏","reacted":true}`))
	if len(out.Reactions) != 1 || len(out.Reactions[0].By) != 3 {
		t.Fatalf("three members reacted, the chip holds %+v", out.Reactions)
	}
}

// TestReactionRefusesAnEmojiOutsideThePalette is the closed set, asserted.
//
// ⚠ WITHOUT IT `emoji` IS A FREE-TEXT COLUMN, and a free-text column on a row that
// writes no audit event and raises no push is a message-sending channel with none of
// the properties D231 weighed when it accepted that for messages.
func TestReactionRefusesAnEmojiOutsideThePalette(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče", andy)
	msg := hh.send(kaja, c.ID, "Ahoj")

	for _, bad := range []string{"🍺", "", "x", strings.Repeat("👍", 40), "❤"} {
		body, err := json.Marshal(map[string]any{"emoji": bad, "reacted": true})
		if err != nil {
			t.Fatal(err)
		}
		rr := hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions", string(body))
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("emoji %q was answered %d, want 422 — the palette is seven values and "+
				"the server is where that is true", bad, rr.Code)
		}
	}
	// ⚠ "❤" (U+2764 alone) is in that list on purpose. The palette's heart is TWO
	// code points and the variation selector is part of it; accepting the bare one
	// would store a value the frontend never sends and can never match, so a chip
	// would exist that nobody could ever remove.
	if got := decode[chat.Message](t, hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions",
		`{"emoji":"`+heart+`","reacted":true}`)); len(got.Reactions) != 1 {
		t.Fatalf("the palette's own heart was refused: %+v", got.Reactions)
	}
}

// TestReactionRefusesANonMemberWithTheSame404AnUnknownIDGets is leak row 1, for the
// route that did not exist when that table was written.
func TestReactionRefusesANonMemberWithTheSame404AnUnknownIDGets(t *testing.T) {
	hh := newHousehold(t, kaja, andy, boss)
	c := hh.group(kaja, "Rodiče")
	msg := hh.send(kaja, c.ID, "Tajné")

	body := `{"emoji":"` + heart + `","reacted":true}`
	// ⚠ THE ADMIN IS IN THIS LOOP. D255 gives an admin restore and purge over a room
	// they are not in and nothing else — a reaction is a read of the message plus a
	// write, and they may do neither.
	for _, m := range []member{andy, boss} {
		mine := hh.as(m, "PUT", "/api/chat/messages/"+msg.ID+"/reactions", body)
		none := hh.as(m, "PUT", "/api/chat/messages/01900000-0000-7000-8000-000000000000/reactions", body)
		if mine.Code != http.StatusNotFound {
			t.Errorf("%s reacting to a message in a room they are not in: %d, want 404", m.id, mine.Code)
		}
		if mine.Body.String() != none.Body.String() {
			t.Errorf("%s: the non-member refusal (%s) differs from the unknown-id one (%s) — "+
				"two bodies is a membership oracle spelled quietly", m.id, mine.Body.String(), none.Body.String())
		}
	}
}

// TestReactionRefusesAMessageBelowTheFloor is the floor, on the newest route.
//
// A member added to a room yesterday may not read last week — and may not react to
// it either, because reacting is a read of the message plus a write onto it, and the
// response carries the body back.
func TestReactionRefusesAMessageBelowTheFloor(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče")
	old := hh.send(kaja, c.ID, "Před přidáním")

	if _, err := hh.svc.AddMember(hh.ctx(kaja), c.ID, chat.ConversationMemberAdd{UserID: andy.id}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	fresh := hh.send(kaja, c.ID, "Po přidání")

	if rr := hh.as(andy, "PUT", "/api/chat/messages/"+old.ID+"/reactions",
		`{"emoji":"`+heart+`","reacted":true}`); rr.Code != http.StatusNotFound {
		t.Errorf("a member below their floor reacted to %s: %d, want 404", old.ID, rr.Code)
	}
	if rr := hh.as(andy, "PUT", "/api/chat/messages/"+fresh.ID+"/reactions",
		`{"emoji":"`+heart+`","reacted":true}`); rr.Code != http.StatusOK {
		t.Errorf("a member above their floor was refused: %d %s", rr.Code, rr.Body.String())
	}
}

// TestReactionPublishesToTheFlooredAudienceOnly is the MemberIDsAbove rule, which
// this route inherits and could easily have missed.
//
// ⚠ THE FRAME CARRIES THE WHOLE MESSAGE, BODY INCLUDED. Publishing a reaction to
// `MemberIDs` would push an old message's full text to exactly the people the floor
// exists to keep it from — the correction EditMessage took in PR 2, in a verb that
// did not exist then.
func TestReactionPublishesToTheFlooredAudienceOnly(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče")
	old := hh.send(kaja, c.ID, "Před přidáním")
	if _, err := hh.svc.AddMember(hh.ctx(kaja), c.ID, chat.ConversationMemberAdd{UserID: andy.id}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	hh.notify.reset()

	if rr := hh.as(kaja, "PUT", "/api/chat/messages/"+old.ID+"/reactions",
		`{"emoji":"`+heart+`","reacted":true}`); rr.Code != http.StatusOK {
		t.Fatalf("react: %d %s", rr.Code, rr.Body.String())
	}

	var found bool
	for i, typ := range hh.notify.types {
		if typ != "chat_message.updated" {
			continue
		}
		found = true
		for _, id := range hh.notify.audiences[i] {
			if id == andy.id {
				t.Fatalf("the reaction frame for a message written before %s joined reached them; "+
					"the audience must be MemberIDsAbove, not MemberIDs", andy.id)
			}
		}
	}
	if !found {
		t.Fatalf("a reaction published no chat_message.updated frame; the chips would appear "+
			"only for the person who tapped. Types seen: %v", hh.notify.types)
	}
}

// TestReactionCarriesNoPerRecipientField is why `mine` is absent from the wire.
//
// ⚠ ws.PublishTo MARSHALS ONCE FOR THE WHOLE AUDIENCE (D233), so any field whose
// value depends on WHO is receiving it is a field that is correct for at most one
// recipient. `mine` is exactly such a field, and the fix is that it never exists:
// the reactors ride as ids and every client answers the question locally.
func TestReactionCarriesNoPerRecipientField(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče", andy)
	msg := hh.send(kaja, c.ID, "Ahoj")

	rr := hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions", `{"emoji":"`+heart+`","reacted":true}`)
	raw := rr.Body.String()
	for _, forbidden := range []string{`"mine"`, `"reacted_by_me"`, `"count"`} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("the message payload carries %s: %s\n"+
				"A per-recipient field cannot ride a frame that is marshalled once for everybody.",
				forbidden, raw)
		}
	}
	if !strings.Contains(raw, `"user_id"`) {
		t.Errorf("the payload carries no user_id, so no client can work out whether a chip is theirs: %s", raw)
	}
}

// TestReactionsAreNotAudited extends D231's breach by exactly one datum, and says so.
//
// If a message writes no audit event, a reaction to one certainly must not: it is
// strictly less than a message, and auditing it would rebuild the traffic-analysis
// record D231 declined to keep — who responded to whom, how often, at what hour.
func TestReactionsAreNotAudited(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče", andy)
	msg := hh.send(kaja, c.ID, "Ahoj")

	before := auditCount(t, hh.db)
	if rr := hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions",
		`{"emoji":"`+heart+`","reacted":true}`); rr.Code != http.StatusOK {
		t.Fatalf("react: %d %s", rr.Code, rr.Body.String())
	}
	if rr := hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions",
		`{"emoji":"`+heart+`","reacted":false}`); rr.Code != http.StatusOK {
		t.Fatalf("un-react: %d %s", rr.Code, rr.Body.String())
	}
	if after := auditCount(t, hh.db); after != before {
		t.Errorf("reacting wrote %d audit events; D231's breach covers messages and a reaction "+
			"is less than one", after-before)
	}
}

// TestDeletingAMessageTakesItsReactionsWithIt.
//
// D223 keeps the row so replies do not point at nothing, and blanks the body. It
// does not keep six people's hearts on a sentence nobody can read any more — chips
// under *Zpráva byla smazána* would be the one part of a deleted message that
// survived the delete.
func TestDeletingAMessageTakesItsReactionsWithIt(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče", andy)
	msg := hh.send(kaja, c.ID, "Smažu to")

	if rr := hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions",
		`{"emoji":"`+heart+`","reacted":true}`); rr.Code != http.StatusOK {
		t.Fatalf("react: %d %s", rr.Code, rr.Body.String())
	}
	if err := hh.svc.DeleteMessage(hh.ctx(kaja), msg.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var rows int
	if err := hh.db.QueryRow(`SELECT COUNT(*) FROM chat_reactions WHERE message_id = ?`, msg.ID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("a tombstone still holds %d reaction rows", rows)
	}
	page := decode[chat.MessagePage](t, hh.as(kaja, "GET", "/api/chat/conversations/"+c.ID+"/messages", ""))
	if len(page.Items) != 1 || len(page.Items[0].Reactions) != 0 {
		t.Errorf("the tombstone renders %+v, want no chips", page.Items)
	}
	// And a new reaction on it is refused rather than silently written to a row
	// nobody can read.
	if rr := hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions",
		`{"emoji":"`+heart+`","reacted":true}`); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("reacting to a tombstone: %d, want 422", rr.Code)
	}
}

// TestEditingAMessageKeepsItsReactions.
//
// ⚠ THE /ws FRAME REPLACES THE WHOLE MESSAGE, so a field the edit path omits is a
// field that DISAPPEARS from every other member's bubble until something refetches.
// PR 2 learned that with the reply quote and PR 3 with the attachments; this is the
// third field to which it applies, and the first that belongs to other people.
func TestEditingAMessageKeepsItsReactions(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče", andy)
	msg := hh.send(kaja, c.ID, "Klíč je pod květináčem")

	if rr := hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions",
		`{"emoji":"`+heart+`","reacted":true}`); rr.Code != http.StatusOK {
		t.Fatalf("react: %d %s", rr.Code, rr.Body.String())
	}
	edited, err := hh.svc.EditMessage(hh.ctx(kaja), msg.ID, chat.MessageUpdate{Body: "Klíč je pod rohožkou"})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if len(edited.Reactions) != 1 || len(edited.Reactions[0].By) != 1 {
		t.Fatalf("the edit returned %+v; a typo fix must not wipe other people's reactions "+
			"off every screen in the household", edited.Reactions)
	}
}

// TestReactionChipsAreInPaletteOrder.
//
// Two members looking at the same message must see the same order, and a chip must
// not jump under a reader's finger because somebody else reacted. Insertion order
// gives neither.
func TestReactionChipsAreInPaletteOrder(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče", andy)
	msg := hh.send(kaja, c.ID, "Ahoj")

	// Added back-to-front on purpose.
	for _, e := range []string{"✅", "😢", "👍", heart} {
		if rr := hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions",
			`{"emoji":"`+e+`","reacted":true}`); rr.Code != http.StatusOK {
			t.Fatalf("react %s: %d %s", e, rr.Code, rr.Body.String())
		}
	}
	page := decode[chat.MessagePage](t, hh.as(kaja, "GET", "/api/chat/conversations/"+c.ID+"/messages", ""))
	got := make([]string, 0, 4)
	for _, r := range page.Items[0].Reactions {
		got = append(got, r.Emoji)
	}
	want := []string{heart, "👍", "😢", "✅"}
	if strings.Join(got, "") != strings.Join(want, "") {
		t.Errorf("chips came back as %v, want palette order %v", got, want)
	}
}

// TestReactionRefusesABodyWithNoReacted is the field's absence, which used to be a
// removal (review round 2).
//
// ⚠ `reacted` IS THE WHOLE OF WHAT THIS ROUTE DECIDES, so decoding a missing one as
// Go's zero `false` made a body that named only the emoji delete the caller's chip
// and answer 200 saying so — the opposite of what a client sending it plainly meant,
// with no error to notice. The spec has said `required: [emoji, reacted]` since
// 0.13.0; the code now says it too.
func TestReactionRefusesABodyWithNoReacted(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rodiče", andy)
	msg := hh.send(kaja, c.ID, "Ahoj")

	if rr := hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions",
		`{"emoji":"`+heart+`","reacted":true}`); rr.Code != http.StatusOK {
		t.Fatalf("react: %d %s", rr.Code, rr.Body.String())
	}
	rr := hh.as(andy, "PUT", "/api/chat/messages/"+msg.ID+"/reactions", `{"emoji":"`+heart+`"}`)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("a body with no `reacted` was answered %d, want 422 — read as false it "+
			"deletes the chip the caller was trying to add", rr.Code)
	}
	// And the chip it did not mention is still there.
	page := decode[chat.MessagePage](t, hh.as(kaja, "GET", "/api/chat/conversations/"+c.ID+"/messages", ""))
	if len(page.Items) != 1 || len(page.Items[0].Reactions) != 1 {
		t.Errorf("the refused request removed the reaction anyway: %+v", page.Items)
	}
}
