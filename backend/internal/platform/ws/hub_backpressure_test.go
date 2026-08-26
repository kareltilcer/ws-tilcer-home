package ws

// The one property of PublishTo that cannot be tested through a socket: what a
// FULL send buffer does. A real client's write pump drains into the kernel's
// socket buffer, which absorbs far more than sendBuffer frames, so saturation is
// only reachable by building the clients here — which is also why this test is in
// package ws rather than beside the others in ws_test.

import (
	"testing"
	"time"
)

// TestPublishToDropsOnlyForTheSaturatedClient pins the load-bearing `default:` in
// PublishTo's send.
//
// ⚠ PublishTo does its sends while HOLDING h.mu. A maintainer who decides a
// dropped chat frame is unacceptable and makes that send blocking (or gives it a
// timeout) would not slow one phone down — they would freeze the mutex that
// Publish, add, remove and Count all need, so one wedged device stops the whole
// household's live updates. Nothing else in the suite exercises backpressure at
// all: every other test reads its frames eagerly.
func TestPublishToDropsOnlyForTheSaturatedClient(t *testing.T) {
	h := NewHub(nil)
	newClient := func(userID string) *client {
		return &client{send: make(chan []byte, sendBuffer), cancel: func() {}, userID: userID}
	}
	slow, fast := newClient("u-slow"), newClient("u-karel")
	h.add(slow)
	h.add(fast)
	for i := 0; i < sendBuffer; i++ {
		slow.send <- []byte("backlog")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.PublishTo([]string{"u-slow", "u-karel"}, Message{Type: "chat.message.created"})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		// Fatal rather than Error: the hub mutex is still held, so every later
		// assertion here would block too.
		t.Fatal("PublishTo blocked on a client whose send buffer is full — it must drop " +
			"for that client and move on, or one wedged device stalls the hub for everyone")
	}

	if n := len(fast.send); n != 1 {
		t.Errorf("the healthy recipient queued %d frames, want 1 — a saturated member of "+
			"the audience must not cost the others their message", n)
	}
	if n := len(slow.send); n != sendBuffer {
		t.Errorf("the saturated client's buffer holds %d frames, want %d (unchanged)", n, sendBuffer)
	}
	// And the mutex was released, so the hub is still usable afterwards.
	if n := h.Count(); n != 2 {
		t.Errorf("hub client count = %d after a saturated publish, want 2", n)
	}
}
