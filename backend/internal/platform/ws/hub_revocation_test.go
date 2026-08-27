package ws

// The one property of DisconnectSession that cannot be tested through a socket:
// what the INDEXES hold between the revocation and the handler unwinding. Through
// a real connection the handler's own remove() lands within microseconds, so both
// the fixed and the broken version reach an empty byUser and the assertion proves
// nothing about which one ran. Building the clients here removes the handler
// entirely — nothing else will ever call remove() — so the state left behind is
// DisconnectSession's alone. Same reason the backpressure test is in package ws.

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

// TestDisconnectSessionUnindexesBeforeItCancels pins the half of a revocation
// that is not the close.
//
// ⚠ Cancelling alone left the revoked client in byUser until its own handler
// goroutine unwound, and in that window PublishTo still found it: a concurrent
// chat publish queued a message BODY on its send channel, and the write pump —
// sitting in a select with ctx.Done() AND c.send both ready, which Go picks
// between at random — could put that private payload on the wire of a session
// that had already been revoked. The whole premise of this PR is that a socket
// must not outlive its session, and delivery is the half that matters.
//
// Nothing else in the suite can catch it. TestDisconnectSessionClosesEveryTabOfThatSession
// waits for the count to settle, which it does either way.
func TestDisconnectSessionUnindexesBeforeItCancels(t *testing.T) {
	h := NewHub(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	// ⚠ A REAL cancel, not a no-op stub. revoke() is only half a revocation if the
	// cancel behind it does nothing, and a stub cannot tell a client that was
	// closed from one that was merely unindexed — the two halves this test is
	// about. Each client's context stands in for the write pump's.
	done := map[*client]context.Context{}
	newClient := func(userID, sessionID string) *client {
		ctx, cancel := context.WithCancel(context.Background())
		c := &client{send: make(chan []byte, sendBuffer), cancel: cancel, userID: userID, sessionID: sessionID}
		t.Cleanup(cancel)
		done[c] = ctx
		return c
	}
	// Two tabs of the doomed session, plus the same member's OTHER device, whose
	// session nobody touched.
	phoneTab1 := newClient("u-karel", "s-phone")
	phoneTab2 := newClient("u-karel", "s-phone")
	laptop := newClient("u-karel", "s-laptop")
	for _, c := range []*client{phoneTab1, phoneTab2, laptop} {
		h.add(c)
	}

	h.DisconnectSession("s-phone")

	for name, c := range map[string]*client{"tab 1": phoneTab1, "tab 2": phoneTab2} {
		select {
		case <-done[c].Done():
		default:
			t.Errorf("revoked %s was never cancelled — unindexing without closing leaves the "+
				"socket live on a session that is 401 on every other request", name)
		}
		if !c.revoked.Load() {
			t.Errorf("revoked %s is not marked revoked, so it would close as a NORMAL closure "+
				"and the browser would reconnect into an upgrade that 401s forever", name)
		}
	}
	select {
	case <-done[laptop].Done():
		t.Error("the member's untouched device was cancelled — a revocation is per SESSION")
	default:
	}

	// ⚠ The assertion is about the INDEX, not the socket. revoke() has run; no
	// handler ever will, so anything still filed here is what a live PublishTo
	// would find.
	if n := h.TrackedClientsForTest("u-karel"); n != 1 {
		t.Errorf("%d sockets still indexed for the member after revoking one of their two "+
			"sessions, want 1 (the laptop) — a revoked socket that is still in byUser goes on "+
			"receiving message bodies until its handler happens to unwind", n)
	}
	if n := h.TrackedSessionsForTest(); n != 1 {
		t.Errorf("%d sessions still indexed, want 1 — the revoked session must leave bySession "+
			"with the same lock that decided it was revoked", n)
	}
	if n := h.Count(); n != 1 {
		t.Errorf("hub client count = %d, want 1", n)
	}
	// And it really is targetable-by-nobody: a publish to that member reaches the
	// untouched device and only that one.
	h.PublishTo([]string{"u-karel"}, Message{Type: "chat.message.created"})
	if n := len(laptop.send); n != 1 {
		t.Errorf("the member's untouched device queued %d frames, want 1 — revoking a phone "+
			"must not cost the laptop its feed", n)
	}
	for name, c := range map[string]*client{"tab 1": phoneTab1, "tab 2": phoneTab2} {
		if n := len(c.send); n != 0 {
			t.Errorf("revoked %s queued %d frames, want 0 — this is the leak the revocation "+
				"exists to close", name, n)
		}
	}
}

// TestPublishToSkipsASocketRevokedAfterTheSnapshot pins the half of the leak that
// unindexing under h.mu cannot reach.
//
// ⚠ DisconnectSession's removeLocked stops the NEXT fan-out from finding the
// socket. It does nothing about one already in flight: PublishTo snapshots its
// recipients under h.mu and then RELEASES it to marshal (encoding a chat payload
// under the index would stall Publish, add, remove and Count), so a revocation
// landing in that gap still finds an open send channel. Without fanOut's revoked
// check the message BODY is queued there, and the write pump — in a select with
// ctx.Done() AND c.send both ready, which Go picks between at random — can put it
// on the wire of a session that was revoked before the marshal even finished.
//
// The state below is exactly that mid-flight one: still indexed (the snapshot has
// been taken), already revoked. TestDisconnectSessionUnindexesBeforeItCancels
// cannot reach it — it publishes only after DisconnectSession has returned, by
// which point the client is out of byUser and the send would be skipped either
// way.
func TestPublishToSkipsASocketRevokedAfterTheSnapshot(t *testing.T) {
	h := NewHub(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	revoked := &client{send: make(chan []byte, sendBuffer), cancel: func() {}, userID: "u-karel", sessionID: "s-phone"}
	live := &client{send: make(chan []byte, sendBuffer), cancel: func() {}, userID: "u-karel", sessionID: "s-laptop"}
	h.add(revoked)
	h.add(live)
	// Revoked WITHOUT unindexing: the socket a fan-out that snapshotted a moment
	// earlier is still holding.
	revoked.revoke()

	h.PublishTo([]string{"u-karel"}, Message{Type: "chat.message.created"})

	if n := len(revoked.send); n != 0 {
		t.Errorf("a revoked socket queued %d frames, want 0 — a message body handed to a session "+
			"that is already gone is the leak the whole revocation mechanism exists to close", n)
	}
	if n := len(live.send); n != 1 {
		t.Errorf("the member's live device queued %d frames, want 1 — skipping a revoked socket "+
			"must not cost its neighbours the message", n)
	}
}

// TestPublishSkipsARevokedSocket is the same guarantee on the broadcast path,
// which shares the fan-out. A broadcast carries no private content, so this is
// belt-and-braces rather than a leak — but the two paths must not diverge, or a
// future change that gives Publish an audience inherits a hole nothing tests.
func TestPublishSkipsARevokedSocket(t *testing.T) {
	h := NewHub(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	revoked := &client{send: make(chan []byte, sendBuffer), cancel: func() {}, userID: "u-karel"}
	live := &client{send: make(chan []byte, sendBuffer), cancel: func() {}, userID: "u-eva"}
	h.add(revoked)
	h.add(live)
	revoked.revoke()

	h.Publish(Message{Type: "todo.changed"})

	if n := len(revoked.send); n != 0 {
		t.Errorf("a revoked socket queued %d broadcast frames, want 0", n)
	}
	if n := len(live.send); n != 1 {
		t.Errorf("the live socket queued %d broadcast frames, want 1 — Publish must still reach "+
			"every client that has not been revoked", n)
	}
}
