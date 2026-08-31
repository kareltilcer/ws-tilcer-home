package chat_test

// Realtime: the audience (D232/D233) and the gap check (D259).
//
// ⚠ THE /ws PAYLOAD IS THE FIRST ONE IN HOME THAT CARRIES CONTENT. Every other
// module publishes "something changed" to every connected client and lets the
// browser refetch through its own session. Chat publishes the message itself to a
// named set, so the audience is not an optimisation — it is the access rule, and it
// is asserted here as directly as the HTTP refusals are.

import (
	"net/http"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/chat"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/optional"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
)

// TestMessagePublishReachesExactlyTheMembers is D233.
func TestMessagePublishReachesExactlyTheMembers(t *testing.T) {
	hh := newHousehold(t, kaja, andy, boss, quiet)
	c := hh.group(kaja, "Trojka", andy)

	hh.notify.reset()
	hh.send(kaja, c.ID, "jen pro nás dva")

	if len(hh.notify.audiences) != 1 {
		t.Fatalf("one message produced %d publishes, want 1", len(hh.notify.audiences))
	}
	if hh.notify.types[0] != "chat_message.created" {
		t.Errorf("published type %q, want chat_message.created", hh.notify.types[0])
	}
	got := map[string]bool{}
	for _, id := range hh.notify.audiences[0] {
		got[id] = true
	}
	if !got[kaja.id] || !got[andy.id] {
		t.Errorf("the audience %v is missing a member of the conversation", hh.notify.audiences[0])
	}
	for _, outsider := range []member{boss, quiet} {
		if got[outsider.id] {
			t.Errorf("%s is not in this conversation and received its message over /ws — "+
				"the payload IS the content (D233)", outsider.id)
		}
	}
	if len(got) != 2 {
		t.Errorf("audience has %d members, want exactly the 2 in the room: %v",
			len(got), hh.notify.audiences[0])
	}
}

// TestRemovedMemberIsToldSpecifically is D233's other half and leak row 22.
//
// The removed member's client needs to leave a view that has quietly become
// forbidden. No socket is force-closed and their already-fetched page is not
// scrubbed — the next request 404s, which is the accepted bound.
func TestRemovedMemberIsToldSpecifically(t *testing.T) {
	hh := newHousehold(t, kaja, andy, boss)
	c := hh.group(kaja, "Dočasné členství", andy)

	hh.notify.reset()
	if err := hh.svc.RemoveMember(hh.ctx(kaja), c.ID, andy.id); err != nil {
		t.Fatalf("remove member: %v", err)
	}

	// ⚠ TWO FRAMES, AND THE SPLIT IS THE WHOLE POINT (v10 review). The membership
	// frame — the one that names a person — goes to the removed member ALONE.
	// Telling the room WHO left would be a system message, and there are none
	// (D221). What the room gets instead is the room's own frame, which names
	// nobody: "this conversation changed, refetch through the membership join".
	// Without it the remaining members hold a panel that still lists the person who
	// has gone, and go on being offered a remove button whose click answers 404.
	if len(hh.notify.audiences) != 2 {
		t.Fatalf("removing a member produced %d publishes, want 2", len(hh.notify.audiences))
	}
	membership, room := -1, -1
	for i, typ := range hh.notify.types {
		switch typ {
		case "chat_membership.changed":
			membership = i
		case "chat_conversation.changed":
			room = i
		}
	}
	if membership < 0 || room < 0 {
		t.Fatalf("published %v, want one chat_membership.changed and one chat_conversation.changed",
			hh.notify.types)
	}
	if got := hh.notify.audiences[membership]; len(got) != 1 || got[0] != andy.id {
		t.Errorf("the membership change went to %v, want only the removed member — "+
			"telling the room who left is a system message, and there are none (D221)", got)
	}
	// The room's frame reaches whoever is still in it, and never the person who left.
	for _, id := range hh.notify.audiences[room] {
		if id == andy.id {
			t.Errorf("the removed member is still in the room's own audience %v",
				hh.notify.audiences[room])
		}
	}
	if len(hh.notify.audiences[room]) != 1 || hh.notify.audiences[room][0] != kaja.id {
		t.Errorf("the room's frame went to %v, want the remaining member %s",
			hh.notify.audiences[room], kaja.id)
	}
	// ⚠ AND IT NAMES NOBODY. A frame carrying the departed member's id would be the
	// system message D221 refuses, in a payload instead of in the thread.
	ev, ok := hh.notify.payloads[room].(chat.ConversationEvent)
	if !ok || ev.ConversationID != c.ID || ev.Gone {
		t.Errorf("room payload = %+v, want a plain changed frame for %s",
			hh.notify.payloads[room], c.ID)
	}

	// And the bound: the next request 404s.
	if rr := hh.as(andy, "GET", "/api/chat/conversations/"+c.ID+"/messages", ""); rr.Code != http.StatusNotFound {
		t.Errorf("a removed member's next request returned %d, want 404", rr.Code)
	}
}

// TestPrevMessageIDIsOneValueForTheWholeAudience is D259's premise.
//
// ⚠ IT IS COMPUTED ONCE, NOT PER RECIPIENT. PublishTo marshals a single frame, so a
// per-member value would mean one marshal per member and defeat the whole point of
// the targeted fan-out. Everything about the gap check follows from that.
func TestPrevMessageIDIsOneValueForTheWholeAudience(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Řada", andy)

	hh.notify.reset()
	first := hh.send(kaja, c.ID, "jedna")
	second := hh.send(andy, c.ID, "dvě")
	third := hh.send(kaja, c.ID, "tři")

	if len(hh.notify.payloads) != 3 {
		t.Fatalf("three sends produced %d payloads", len(hh.notify.payloads))
	}
	events := make([]chat.MessageEvent, 0, 3)
	for _, p := range hh.notify.payloads {
		ev, ok := p.(chat.MessageEvent)
		if !ok {
			t.Fatalf("payload %T is not a MessageEvent — the frontend's gap check reads "+
				"prev_message_id off this shape", p)
		}
		events = append(events, ev)
	}

	if events[0].PrevMessageID != nil {
		t.Errorf("the first message in a conversation carries prev_message_id %q, want null",
			*events[0].PrevMessageID)
	}
	if events[1].PrevMessageID == nil || *events[1].PrevMessageID != first.ID {
		t.Errorf("second message's prev is %v, want %s", events[1].PrevMessageID, first.ID)
	}
	if events[2].PrevMessageID == nil || *events[2].PrevMessageID != second.ID {
		t.Errorf("third message's prev is %v, want %s", events[2].PrevMessageID, second.ID)
	}
	_ = third
}

// TestGapCheckTerminatesForAMemberAddedToABusyConversation is the test HANDOFF-12
// §8.1 asks for by name, and it models the CLIENT to get it.
//
// ⚠ THE CHECK IS ONE-SHOT PER RECEIVED MESSAGE, AND THAT IS WHAT MAKES IT
// TERMINATE. prev_message_id describes the CONVERSATION's sequence, not any one
// member's view of it — so a member whose floor sits above it can never hold it, and
// their first message after joining always looks like a gap. One refetch later they
// hold message N, the next payload carries prev = N, and it matches from then on.
//
// A client that re-checked AFTER its own refetch would loop on every message
// forever, which is why the model below refetches and then accepts.
func TestGapCheckTerminatesForAMemberAddedToABusyConversation(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Rušná")

	// A busy conversation Andy is not in yet.
	for i := range 20 {
		hh.send(kaja, c.ID, string(rune('a'+i%26))+" před připojením")
	}
	if _, err := hh.svc.AddMember(hh.ctx(kaja), c.ID, chat.ConversationMemberAdd{UserID: andy.id}); err != nil {
		t.Fatalf("add member: %v", err)
	}

	// The client model: what the browser holds, and what it does with each frame.
	var (
		held     string // the id of the newest message this client has
		refetches int
	)
	receive := func(ev chat.MessageEvent) {
		prev := ""
		if ev.PrevMessageID != nil {
			prev = *ev.PrevMessageID
		}
		if prev != held {
			// A gap: refetch the tail. ⚠ ONE-SHOT — after the refetch this client
			// holds the newest message and does NOT re-check.
			refetches++
			page, err := hh.svc.Thread(hh.ctx(andy), c.ID, "", "", 50)
			if err != nil {
				t.Fatalf("tail refetch: %v", err)
			}
			if len(page.Items) > 0 {
				held = page.Items[0].ID // backward = newest first
			}
			return
		}
		held = ev.Message.ID
	}

	// ⚠ SEND AND RECEIVE ARE INTERLEAVED, because that is what a live client does
	// and the distinction is not cosmetic. Replaying five frames after all five
	// sends makes the first refetch jump straight to the newest message, so every
	// remaining frame's prev looks like a gap and the test reports five refetches —
	// an artefact of the harness, not of the protocol.
	for i := range 5 {
		hh.notify.reset()
		hh.send(kaja, c.ID, string(rune('A'+i))+" po připojení")
		for _, p := range hh.notify.payloads {
			receive(p.(chat.MessageEvent))
		}
	}

	if refetches != 1 {
		t.Fatalf("a member added to a busy conversation refetched %d times over five "+
			"messages, want exactly 1.\n\nMore than one means the client re-checks after "+
			"its own refetch and will loop on every message; zero means the gap went "+
			"undetected, which is the dropped frame D259 exists to catch.", refetches)
	}

	// And the floor still holds through all of it: Andy sees the five messages sent
	// after they were added, and none of the twenty before.
	page, err := hh.svc.Thread(hh.ctx(andy), c.ID, "", "", 50)
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if len(page.Items) != 5 {
		t.Fatalf("Andy sees %d messages, want the 5 sent after they were added", len(page.Items))
	}
}

// TestPushGoesToEveryMemberButTheAuthorAndHonoursTheMute is D248.
//
// Three filters in three places, and this asserts the two that live in chat: the
// author is excluded, and a member who muted THIS conversation is excluded. The
// third — the app-wide cat_chat bucket — belongs to push.EligibleSubscriptions and
// is tested there.
func TestPushGoesToEveryMemberButTheAuthorAndHonoursTheMute(t *testing.T) {
	hh := newHousehold(t, kaja, andy, quiet)
	c := hh.group(kaja, "Hlučná", andy, quiet)

	// Andy silences this room.
	if _, err := hh.svc.UpdateSelf(hh.ctx(andy), c.ID,
		chat.ConversationMemberSelfUpdate{Muted: optional.Of(true)}); err != nil {
		t.Fatalf("mute: %v", err)
	}

	hh.send(kaja, c.ID, "zpráva do ztlumené místnosti")
	recipients := hh.awaitPush(t)

	for _, id := range recipients {
		if id == kaja.id {
			t.Errorf("the author was pushed their own message")
		}
		if id == andy.id {
			t.Errorf("a member who muted this conversation was pushed (D248)")
		}
	}
	if len(recipients) != 1 || recipients[0] != quiet.id {
		t.Fatalf("push went to %v, want only %s", recipients, quiet.id)
	}
}

// TestEveryMessageInARoomRe-alertsUnderTheOneCollapseTag is the other half of the
// per-conversation Tag.
//
// ⚠ THE COLLAPSING AND THE SILENCE CAME FROM THE SAME FIELD, AND ONLY ONE OF THEM
// WAS WANTED. A notification that replaces a same-tag predecessor does not alert
// again unless Renotify says so, so shipping the tag alone bought one tidy entry
// per room at the price of announcing only the FIRST message in it. Every reply
// after that updated a notification in the shade with no sound, no vibration and
// no banner — worst precisely during an active conversation, which is the moment
// somebody most wants to be told.
//
// So both fields are asserted together on the SECOND message, not the first: the
// first message alerted correctly even with the bug, and a test that sends one
// message passes against the code this replaces.
func TestEveryMessageInARoomRealertsUnderTheOneCollapseTag(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Domácnost", andy)

	hh.send(kaja, c.ID, "první")
	first := hh.awaitPushEnvelope(t)
	hh.send(kaja, c.ID, "druhá")
	second := hh.awaitPushEnvelope(t)

	wantTag := "chat:" + c.ID
	for _, e := range []struct {
		which string
		env   push.Envelope
	}{{"first", first}, {"second", second}} {
		if e.env.Tag != wantTag {
			t.Errorf("%s message tagged %q, want %q — two rooms must stay two entries",
				e.which, e.env.Tag, wantTag)
		}
		if !e.env.Renotify {
			t.Errorf("%s message did not ask to re-alert: a same-tag replacement is "+
				"delivered silently, so this message reached the phone unannounced", e.which)
		}
	}
}

// TestEditAndDeleteReachOnlyTheMembersAboveWhoseFloorTheMessageSits is the floor
// applied to the AUDIENCE rather than only to the read (D218/D226).
//
// ⚠ IT IS THE ONE AUDIENCE A CORRECT-LOOKING IMPLEMENTATION GETS WRONG. A SEND may
// use MemberIDs, because a message minted now sorts above every floor in the room —
// so "every member" and "every member who may read it" are the same set, and the
// habit reads safe. They are NOT the same set for an EXISTING message: somebody
// added afterwards is bounded off it by Thread, MessageByID, quoteMap and Search,
// and publishing an edit to them hands their socket the full new body the floor
// exists to withhold. Nothing renders it — replaceMessage finds no row to replace —
// but it has already reached the browser.
//
// Both verbs are asserted, because they leak different things: the edit leaks the
// body, the delete leaks the id, the author and the time of a message somebody may
// not read. Swapping MemberIDsAbove back to MemberIDs must fail here.
func TestEditAndDeleteReachOnlyTheMembersAboveWhoseFloorTheMessageSits(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Historie")

	// Written while Kája is alone in the room.
	old := hh.send(kaja, c.ID, "tohle Andy nikdy neuvidí")

	// Andy joins afterwards, so his floor sits ABOVE that message.
	if _, err := hh.svc.AddMember(hh.ctx(kaja), c.ID,
		chat.ConversationMemberAdd{UserID: andy.id}); err != nil {
		t.Fatalf("add andy: %v", err)
	}

	for _, tc := range []struct {
		verb string
		typ  string
		run  func() error
	}{
		{"edit", "chat_message.updated", func() error {
			_, err := hh.svc.EditMessage(hh.ctx(kaja), old.ID,
				chat.MessageUpdate{Body: "upravené tajemství"})
			return err
		}},
		{"delete", "chat_message.deleted", func() error {
			return hh.svc.DeleteMessage(hh.ctx(kaja), old.ID)
		}},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			hh.notify.reset()
			if err := tc.run(); err != nil {
				t.Fatalf("%s: %v", tc.verb, err)
			}
			if len(hh.notify.audiences) != 1 {
				t.Fatalf("%s produced %d publishes, want 1", tc.verb, len(hh.notify.audiences))
			}
			if hh.notify.types[0] != tc.typ {
				t.Errorf("published type %q, want %q", hh.notify.types[0], tc.typ)
			}
			for _, id := range hh.notify.audiences[0] {
				if id == andy.id {
					t.Errorf("the %s of a message BELOW Andy's floor was published to him: %v.\n\n"+
						"Every read path refuses him this message (D218); the one place the\n"+
						"payload leaves must use the same predicate — Store.MemberIDsAbove,\n"+
						"never MemberIDs.", tc.verb, hh.notify.audiences[0])
				}
			}
			if len(hh.notify.audiences[0]) != 1 || hh.notify.audiences[0][0] != kaja.id {
				t.Errorf("%s audience = %v, want only %s", tc.verb, hh.notify.audiences[0], kaja.id)
			}
		})
	}
}
