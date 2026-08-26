package ws

// The one property of DisconnectSession that cannot be tested through a socket:
// what the INDEXES hold between the revocation and the handler unwinding. Through
// a real connection the handler's own remove() lands within microseconds, so both
// the fixed and the broken version reach an empty byUser and the assertion proves
// nothing about which one ran. Building the clients here removes the handler
// entirely — nothing else will ever call remove() — so the state left behind is
// DisconnectSession's alone. Same reason the backpressure test is in package ws.

import (
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
	newClient := func(userID, sessionID string) *client {
		return &client{send: make(chan []byte, sendBuffer), cancel: func() {}, userID: userID, sessionID: sessionID}
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
