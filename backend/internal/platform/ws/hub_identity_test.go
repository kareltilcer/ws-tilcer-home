package ws_test

// v10 — the hub learns who is connected (PRD D232, FR-V10-18).
//
// Until v10 every fan-out was a broadcast, which was correct while every module
// published data the whole household could read. `chat` publishes message BODIES
// to a member set, so the connection carries the user id the upgrade handler
// already resolved and PublishTo targets it.
//
// These tests are in their own file because they are one change's worth of
// evidence: the targeting works, the existing broadcast is untouched, and the
// second map does not leak.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cws "github.com/coder/websocket"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/ws"
)

// newIdentityServer builds a hub whose /ws resolves the connecting user from an
// X-Test-User header, so one server can hold connections for several members —
// which is what every test below is actually about.
func newIdentityServer(t *testing.T) (*ws.Hub, string) {
	t.Helper()
	hub := ws.NewHub()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.Handler(ws.Config{
		Authenticate: func(r *http.Request) (reqctx.Actor, bool) {
			id := r.Header.Get("X-Test-User")
			if id == "" {
				return reqctx.Actor{}, false
			}
			return reqctx.Actor{UserID: id, Type: "user", Roles: []string{"reader"}}, true
		},
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return hub, "ws" + strings.TrimPrefix(srv.URL, "http")
}

// dialAs opens a connection authenticated as userID.
func dialAs(ctx context.Context, t *testing.T, wsURL, userID string) *cws.Conn {
	t.Helper()
	c, _, err := cws.Dial(ctx, wsURL+"/ws", &cws.DialOptions{
		HTTPHeader: http.Header{"X-Test-User": []string{userID}},
	})
	if err != nil {
		t.Fatalf("dial as %s: %v", userID, err)
	}
	t.Cleanup(func() { _ = c.Close(cws.StatusNormalClosure, "") })
	return c
}

// readType reads one frame and returns its Type, or "" on any read failure.
//
// ⚠ NEGATIVE ASSERTIONS DO NOT USE A TIMEOUT HERE, and the reason is a property of
// the library rather than a style preference: a coder/websocket Read whose context
// expires CLOSES the connection. So "wait 300 ms and see nothing" works exactly
// once per connection and silently poisons every read after it — the next
// assertion reads "" whether the frame arrived or not, which is a test that passes
// for the wrong reason.
//
// Instead, "this client must not receive X" is asserted with the sentinelType
// broadcast below: publish X, then broadcast a sentinel every client is entitled
// to, and assert the client's NEXT frame is the sentinel. It is stronger (it
// proves the client was reachable and still did not get X) and it costs no wall
// clock.
func readType(t *testing.T, conn *cws.Conn, within time.Duration) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		return ""
	}
	var m ws.Message
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m.Type
}

// sentinelType is broadcast with Publish (which reaches everyone) immediately
// after a targeted publish, so that "did not receive" can be asserted by reading
// the next frame rather than by waiting for silence. See readType.
const sentinelType = "sentinel.broadcast"

// readTimeout is generous because every read below now EXPECTS a frame.
const readTimeout = 3 * time.Second

func waitTracked(t *testing.T, hub *ws.Hub, userID string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.TrackedClientsForTest(userID) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hub tracks %d clients for %q, want %d",
		hub.TrackedClientsForTest(userID), userID, want)
}

// TestPublishTo_ReachesOnlyTheNamedUsers is leak-table row 7 at the platform
// level — the whole reason platform/ws changes in v10. Three connected members,
// two of them the audience: the third must receive NOTHING, not a redacted frame
// and not an empty one.
func TestPublishTo_ReachesOnlyTheNamedUsers(t *testing.T) {
	hub, wsURL := newIdentityServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	karel := dialAs(ctx, t, wsURL, "u-karel")
	marie := dialAs(ctx, t, wsURL, "u-marie")
	outsider := dialAs(ctx, t, wsURL, "u-outsider")
	waitCount(t, hub, 3)

	hub.PublishTo([]string{"u-karel", "u-marie"},
		ws.Message{Type: "chat.message.created", Payload: map[string]any{"body": "tajné"}})
	hub.Publish(ws.Message{Type: sentinelType}) // reaches all three — see readType

	if got := readType(t, karel, readTimeout); got != "chat.message.created" {
		t.Errorf("member karel got %q, want chat.message.created", got)
	}
	if got := readType(t, marie, readTimeout); got != "chat.message.created" {
		t.Errorf("member marie got %q, want chat.message.created", got)
	}
	// The assertion the module is built around. The outsider IS connected and IS
	// reachable — the sentinel proves it — and the frame before it must not exist.
	if got := readType(t, outsider, readTimeout); got != sentinelType {
		t.Errorf("a NON-MEMBER's first frame was %q, want the sentinel — PublishTo must "+
			"never reach a user outside the audience (D232, leak row 7)", got)
	}
}

// TestPublishTo_ReachesEveryTabOfOneUser: one member, three devices. byUser is a
// SET per id precisely because a single-client map would deliver to whichever tab
// connected last and silently drop the rest.
func TestPublishTo_ReachesEveryTabOfOneUser(t *testing.T) {
	hub, wsURL := newIdentityServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conns := map[string]*cws.Conn{
		"phone":  dialAs(ctx, t, wsURL, "u-karel"),
		"laptop": dialAs(ctx, t, wsURL, "u-karel"),
		"tab":    dialAs(ctx, t, wsURL, "u-karel"),
	}
	waitTracked(t, hub, "u-karel", 3)

	hub.PublishTo([]string{"u-karel"}, ws.Message{Type: "chat.message.created"})

	for name, conn := range conns {
		if got := readType(t, conn, readTimeout); got != "chat.message.created" {
			t.Errorf("%s got %q, want chat.message.created", name, got)
		}
	}
}

// TestPublishTo_DeliversOnceForARepeatedID guards the union: an audience list
// naming the same member twice — a caller de-duplicating badly, or a member
// appearing as both author and recipient — must not produce two bubbles.
func TestPublishTo_DeliversOnceForARepeatedID(t *testing.T) {
	hub, wsURL := newIdentityServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialAs(ctx, t, wsURL, "u-karel")
	waitTracked(t, hub, "u-karel", 1)

	hub.PublishTo([]string{"u-karel", "u-karel"}, ws.Message{Type: "chat.message.created"})
	hub.Publish(ws.Message{Type: sentinelType})

	if got := readType(t, conn, readTimeout); got != "chat.message.created" {
		t.Fatalf("first read = %q, want chat.message.created", got)
	}
	// A duplicate would sit here, ahead of the sentinel.
	if got := readType(t, conn, readTimeout); got != sentinelType {
		t.Errorf("second frame was %q, want the sentinel — a repeated user id delivered "+
			"twice, and a duplicate chat frame is a duplicate bubble", got)
	}
}

// TestPublishTo_UnknownUserIsANoOp. A phone that is asleep is the normal case,
// not a failure: nothing panics, nothing blocks, and the connected members of the
// same publish still receive.
func TestPublishTo_UnknownUserIsANoOp(t *testing.T) {
	hub, wsURL := newIdentityServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialAs(ctx, t, wsURL, "u-karel")
	waitTracked(t, hub, "u-karel", 1)

	hub.PublishTo([]string{"u-nobody"}, ws.Message{Type: "not.for.karel"})
	hub.PublishTo(nil, ws.Message{Type: "not.for.karel"})
	hub.PublishTo([]string{"u-nobody", "u-karel"}, ws.Message{Type: "chat.message.created"})

	// The first two publishes named nobody who is connected; the third named karel
	// alongside an absent member. His FIRST frame therefore proves both halves: the
	// no-op publishes delivered nothing, and the absent recipient did not stop the
	// present one.
	if got := readType(t, conn, readTimeout); got != "chat.message.created" {
		t.Errorf("connected member's first frame was %q, want chat.message.created — "+
			"an absent recipient must be a no-op, not a failure and not a misdelivery", got)
	}
}

// TestPublishStillReachesEveryClient is the regression guard for the other ten
// modules. Publish is unchanged by v10 and its fan-out must stay a broadcast: a
// connection is reachable by it whether or not the hub knows who owns it.
func TestPublishStillReachesEveryClient(t *testing.T) {
	hub, wsURL := newIdentityServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conns := map[string]*cws.Conn{
		"karel": dialAs(ctx, t, wsURL, "u-karel"),
		"marie": dialAs(ctx, t, wsURL, "u-marie"),
	}
	waitCount(t, hub, 2)

	hub.Publish(ws.Message{Type: "card.moved"})

	for name, conn := range conns {
		if got := readType(t, conn, readTimeout); got != "card.moved" {
			t.Errorf("%s got %q from Publish, want card.moved — every existing module's "+
				"broadcast must stay byte-identical to v9's", name, got)
		}
	}
}

// TestAnonymousClientIsBroadcastOnly covers the dev bypass with no configured
// actor. It joins `clients` and never `byUser`, so Publish reaches it and
// PublishTo cannot — the safe direction for a connection nobody identified.
func TestAnonymousClientIsBroadcastOnly(t *testing.T) {
	hub := ws.NewHub()
	mux := http.NewServeMux()
	// BypassActor with an EMPTY UserID: the shape HOME_DEV_AUTH_BYPASS produces
	// when no dev actor id is configured.
	mux.HandleFunc("/ws", hub.Handler(ws.Config{BypassActor: &reqctx.Actor{}}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(cws.StatusNormalClosure, "") })
	waitCount(t, hub, 1)

	if n := hub.TrackedUsersForTest(); n != 0 {
		t.Errorf("an anonymous client was indexed under %d user ids, want 0", n)
	}

	// An empty id is the one a naive index would file this client under, so it is
	// the id worth targeting. The sentinel that follows is what the client is
	// entitled to.
	hub.PublishTo([]string{""}, ws.Message{Type: "chat.message.created"})
	hub.Publish(ws.Message{Type: sentinelType})

	got := readType(t, conn, readTimeout)
	if got == "chat.message.created" {
		t.Error("PublishTo reached an ANONYMOUS client — an unidentified connection must " +
			"be broadcast-only, or the dev bypass becomes a way into a member-restricted feed")
	}
	if got != sentinelType {
		t.Errorf("Publish did not reach the anonymous client (first frame %q, want the "+
			"sentinel) — an anonymous connection must still receive every broadcast", got)
	}
}

// TestBypassActorRegistersUnderItsID: with a dev actor id configured, targeted
// pushes DO arrive, so a developer running under the bypass sees chat work.
func TestBypassActorRegistersUnderItsID(t *testing.T) {
	hub := ws.NewHub()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.Handler(ws.Config{BypassActor: &reqctx.Actor{UserID: "dev-1"}}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(cws.StatusNormalClosure, "") })
	waitTracked(t, hub, "dev-1", 1)

	hub.PublishTo([]string{"dev-1"}, ws.Message{Type: "chat.message.created"})
	if got := readType(t, conn, readTimeout); got != "chat.message.created" {
		t.Errorf("bypass client got %q, want chat.message.created", got)
	}
}

// TestByUserEmptiesOnDisconnect is the leak test, and it is why the test seam in
// export_test.go exists.
//
// ⚠ Nothing OBSERVABLE breaks when `remove` forgets the second map: Publish stays
// correct, PublishTo stays correct (a dead client's send channel simply fills and
// drops), Count stays correct. What grows is a set of dead *client pointers per
// user id, each holding a 32-slot channel and a cancel func, for the process's
// lifetime — memory, months later, on an app nobody profiles. So it is asserted
// against the map rather than against behaviour.
func TestByUserEmptiesOnDisconnect(t *testing.T) {
	hub, wsURL := newIdentityServer(t)

	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		conn, _, err := cws.Dial(ctx, wsURL+"/ws", &cws.DialOptions{
			HTTPHeader: http.Header{"X-Test-User": []string{"u-karel"}},
		})
		if err != nil {
			cancel()
			t.Fatalf("dial %d: %v", i, err)
		}
		waitTracked(t, hub, "u-karel", 1)
		_ = conn.Close(cws.StatusNormalClosure, "")
		cancel()
		waitCount(t, hub, 0)
	}

	if n := hub.TrackedClientsForTest("u-karel"); n != 0 {
		t.Errorf("after 50 connect/disconnect cycles the hub still tracks %d clients for "+
			"u-karel — remove() must delete from BOTH maps, or every disconnect leaks a "+
			"dead client that never errors and never logs (D232)", n)
	}
	// And the id itself is gone rather than left as an empty set: otherwise the
	// index grows by one entry per member the hub has ever seen and never shrinks.
	if n := hub.TrackedUsersForTest(); n != 0 {
		t.Errorf("the per-user index still holds %d ids with no connections, want 0", n)
	}
}
