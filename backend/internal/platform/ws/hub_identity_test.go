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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cws "github.com/coder/websocket"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/ws"
)

// newIdentityServer builds a hub whose /ws resolves the connecting user from an
// X-Test-User header, so one server can hold connections for several members —
// which is what every test below is actually about.
//
// The session id and token are derived from the header too, so a member dialling
// twice with the same X-Test-User shares ONE session (two tabs of one browser);
// dialAsSession opens a distinct one, which is what the revocation tests need.
func newIdentityServer(t *testing.T) (*ws.Hub, string) {
	t.Helper()
	hub := ws.NewHub(testsupport.DiscardLogger())
	wsURL := newWSServer(t, hub, ws.Config{
		Authenticate: func(r *http.Request) (ws.Upgrade, bool) {
			id := r.Header.Get("X-Test-User")
			if id == "" {
				return ws.Upgrade{}, false
			}
			sessionID := r.Header.Get("X-Test-Session")
			if sessionID == "" {
				sessionID = "sess-" + id
			}
			return ws.Upgrade{
				Actor:     reqctx.Actor{UserID: id, Type: "user", Roles: []string{"reader"}},
				SessionID: sessionID,
				Token:     "tok-" + sessionID,
			}, true
		},
	})
	return hub, wsURL
}

// dialAs opens a connection authenticated as userID, on that member's default
// session.
func dialAs(ctx context.Context, t *testing.T, wsURL, userID string) *cws.Conn {
	t.Helper()
	return dialAsSession(ctx, t, wsURL, userID, "")
}

// dialAsSession opens a connection authenticated as userID on a named session —
// one member's phone and laptop are two sessions, two tabs of one browser are one.
func dialAsSession(ctx context.Context, t *testing.T, wsURL, userID, sessionID string) *cws.Conn {
	t.Helper()
	header := http.Header{"X-Test-User": []string{userID}}
	if sessionID != "" {
		header.Set("X-Test-Session", sessionID)
	}
	c, _, err := cws.Dial(ctx, wsURL+"/ws", &cws.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("dial as %s: %v", userID, err)
	}
	t.Cleanup(func() { _ = c.Close(cws.StatusNormalClosure, "") })
	return c
}

// readMessage reads one frame and decodes it, failing the test on a read error.
// readType and readOrigin are the two projections of it the assertions want; the
// policy below is stated once so neither copy can drift from it.
//
// ⚠ It does NOT swallow the error into "". Every call site now expects a frame,
// and a Read that times out also CLOSES the connection (below) — so a single slow
// read on a loaded machine would return "" and then make every later read on that
// socket return "" too, reporting a chain of failures against PublishTo when the
// cause was one slow read. The error names the cause; report it and stop.
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
func readMessage(t *testing.T, conn *cws.Conn, within time.Duration) ws.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read within %s: %v", within, err)
	}
	var m ws.Message
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// readType reads one frame and returns its Type. See readMessage.
func readType(t *testing.T, conn *cws.Conn, within time.Duration) string {
	t.Helper()
	return readMessage(t, conn, within).Type
}

// sentinelType is broadcast with Publish (which reaches everyone) immediately
// after a targeted publish, so that "did not receive" can be asserted by reading
// the next frame rather than by waiting for silence. See readType.
const sentinelType = "sentinel.broadcast"

// readTimeout is generous because every read below now EXPECTS a frame.
const readTimeout = 3 * time.Second

func waitTracked(t *testing.T, hub *ws.Hub, userID string, want int) {
	t.Helper()
	waitFor(t, func() int { return hub.TrackedClientsForTest(userID) }, want,
		"clients tracked for "+userID)
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

// TestAnonymousClientIsBroadcastOnly covers an actor that carries no user id at
// all. It joins `clients` and never `byUser`, so Publish reaches it and PublishTo
// cannot — the safe direction for a connection nobody identified.
//
// ⚠ This is NOT what HOME_DEV_AUTH_BYPASS produces. HOME_DEV_ACTOR_ID defaults to
// "dev-user" and a blank value falls back to that default, so the composition root
// always builds a BypassActor with an id — see
// TestBypassRegistersEveryClientUnderOneID for what the bypass really does.
//
// ⚠ This covers the BYPASS arm only. An id-less actor arriving through
// Authenticate — the branch production takes, and the one that warns — is a
// different code path with its own test: TestAuthenticatedActorWithNoUserIDIsBroadcastOnly.
func TestAnonymousClientIsBroadcastOnly(t *testing.T) {
	hub := ws.NewHub(testsupport.DiscardLogger())
	wsURL := newWSServer(t, hub, ws.Config{BypassActor: &reqctx.Actor{}})

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

// TestAuthenticatedActorWithNoUserIDIsBroadcastOnly covers the SAME shape through
// the branch production actually uses.
//
// ⚠ The test above reaches an id-less client through BypassActor, which is the
// handler's `else` arm — it never enters the `if userID == ""` block on the
// Authenticate path, so that block (and its warning) could be deleted whole with
// the suite still green. reqctx.Actor documents UserID as "" for system/service
// principals, so an authenticated connection carrying no id is reachable the day
// a non-user principal opens a socket, and it must be broadcast-only there too.
func TestAuthenticatedActorWithNoUserIDIsBroadcastOnly(t *testing.T) {
	logger, logs := testsupport.CaptureLogger()
	hub := ws.NewHub(logger)
	wsURL := newWSServer(t, hub, ws.Config{
		Authenticate: func(*http.Request) (ws.Upgrade, bool) {
			// Authenticated, and deliberately carrying no user id — a service
			// principal, not the dev bypass. The session is real.
			return ws.Upgrade{
				Actor:     reqctx.Actor{Type: "service", Label: "importer"},
				SessionID: "s-service",
				Token:     "tok-service",
			}, true
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(cws.StatusNormalClosure, "") })
	waitCount(t, hub, 1)

	if n := hub.TrackedUsersForTest(); n != 0 {
		t.Errorf("an authenticated actor with no user id was indexed under %d user ids, want 0", n)
	}
	// ⚠ And it was LOGGED. Being broadcast-only is what indexAdd's empty-key skip
	// already guarantees, so without this the whole `if userID == ""` block could
	// be deleted with the suite green — and "authenticated but unidentified" would
	// go back to being indistinguishable from a deliberately anonymous connection.
	if !strings.Contains(logs.String(), "no user id") {
		t.Errorf("nothing warned about an authenticated actor carrying no user id; the log "+
			"is the only trace this bug state leaves. Got:\n%s", logs.String())
	}

	hub.PublishTo([]string{""}, ws.Message{Type: "chat.message.created"})
	hub.Publish(ws.Message{Type: sentinelType})

	got := readType(t, conn, readTimeout)
	if got == "chat.message.created" {
		t.Error("PublishTo reached an authenticated connection carrying NO user id — an " +
			"unidentified principal must be broadcast-only, whatever authenticated it")
	}
	if got != sentinelType {
		t.Errorf("Publish did not reach the unidentified client (first frame %q, want the "+
			"sentinel) — it must still receive every broadcast", got)
	}
}

// TestAuthenticatedConnectionWithoutASessionOrTokenIsLogged covers the bug state
// that is worse than an unidentified actor, because it is the one that cannot be
// UNDONE.
//
// ⚠ A connection whose Upgrade carries no session id is invisible to
// DisconnectSession (indexAdd skips the empty key), and this shape hands over no
// token either, so no revalidation pump starts: BOTH revocation mechanisms are
// disabled for the life of the socket, and every other signal — Count, the
// boards — looks perfectly healthy. (A session id WITHOUT a token is the milder
// shape — bySession still reaches it — and keeps its targeting; only the
// no-session-id shape is degraded.)
//
// ⚠ So it is degraded to BROADCAST-ONLY, exactly as an actor with no user id is.
// Warning and otherwise carrying on left the worse of the two bug states as the
// only one that can leak: the socket stayed in byUser, so PublishTo kept handing
// it member-restricted payloads after a logout, after the TTL expired, after
// auth closed the account, with nothing able to close it. The upgrade is still
// not refused — that would take out live boards over a revocation-plumbing
// problem — but the half that can leak is dropped.
func TestAuthenticatedConnectionWithoutASessionOrTokenIsLogged(t *testing.T) {
	logger, logs := testsupport.CaptureLogger()
	hub := ws.NewHub(logger)
	wsURL := newWSServer(t, hub, ws.Config{
		Authenticate: func(*http.Request) (ws.Upgrade, bool) {
			// Identified, authenticated — and unrevocable: no session id, no token.
			return ws.Upgrade{Actor: reqctx.Actor{UserID: "u-karel", Type: "user"}}, true
		},
		Revalidate: func(context.Context, string) (string, ws.Revalidation) {
			t.Error("the revalidation pump ran for a connection that handed over no token")
			return "u-karel", ws.RevalidationValid
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(cws.StatusNormalClosure, "") })
	waitCount(t, hub, 1)

	if !strings.Contains(logs.String(), "cannot be revoked") {
		t.Errorf("nothing warned about a connection neither DisconnectSession nor the "+
			"revalidation pump can reach. Got:\n%s", logs.String())
	}
	// And the state the warning describes is real: no session index entry, so a
	// revocation for it sweeps nothing.
	if n := hub.TrackedSessionsForTest(); n != 0 {
		t.Errorf("the session index holds %d ids for a connection that gave none, want 0", n)
	}
	// Which is exactly why it must not be targetable: an unrevocable socket in
	// byUser receives member-restricted payloads that nothing can ever stop.
	if n := hub.TrackedClientsForTest("u-karel"); n != 0 {
		t.Errorf("an UNREVOCABLE connection is indexed under its member (%d client(s)), want 0 — "+
			"PublishTo would deliver to it after a logout, after the TTL expires and after "+
			"auth closes the account, with both revocation mechanisms disabled", n)
	}

	hub.PublishTo([]string{"u-karel"}, ws.Message{Type: "chat.message.created"})
	hub.Publish(ws.Message{Type: sentinelType})

	got := readType(t, conn, readTimeout)
	if got == "chat.message.created" {
		t.Error("PublishTo reached a connection neither DisconnectSession nor the revalidation " +
			"pump can close — an unrevocable socket must be broadcast-only")
	}
	// And the broadcast half survives, which is why the upgrade is not refused.
	if got != sentinelType {
		t.Errorf("Publish did not reach the unrevocable client (first frame %q, want the "+
			"sentinel) — it must still receive every broadcast", got)
	}
}

// TestBypassActorRegistersUnderItsID: with a dev actor id configured, targeted
// pushes DO arrive, so a developer running under the bypass sees chat work.
func TestBypassActorRegistersUnderItsID(t *testing.T) {
	hub := ws.NewHub(testsupport.DiscardLogger())
	wsURL := newWSServer(t, hub, ws.Config{BypassActor: &reqctx.Actor{UserID: "dev-1"}})

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

// TestBypassRegistersEveryClientUnderOneID pins what HOME_DEV_AUTH_BYPASS
// ACTUALLY does, as opposed to the id-less shape above.
//
// ⚠ Authenticate is never called under the bypass, and the configured id is never
// empty (HOME_DEV_ACTOR_ID defaults to "dev-user", and a blank value falls back to
// that default), so every connection — a second browser, a phone on the same LAN,
// anything that can reach the port — lands in the SAME byUser set and receives the
// whole of that member's targeted feed, chat bodies included. That is the price of
// fake authentication and the reason the bypass is refused in production. It is
// pinned here so nobody reads the anonymous test above and concludes the bypass is
// broadcast-only.
func TestBypassRegistersEveryClientUnderOneID(t *testing.T) {
	hub := ws.NewHub(testsupport.DiscardLogger())
	wsURL := newWSServer(t, hub, ws.Config{BypassActor: &reqctx.Actor{UserID: "dev-user"}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dial := func(who string) *cws.Conn {
		c, _, err := cws.Dial(ctx, wsURL+"/ws", nil) // no credentials of any kind
		if err != nil {
			t.Fatalf("dial %s: %v", who, err)
		}
		t.Cleanup(func() { _ = c.Close(cws.StatusNormalClosure, "") })
		return c
	}
	first, second := dial("first browser"), dial("second browser")
	waitTracked(t, hub, "dev-user", 2)

	hub.PublishTo([]string{"dev-user"}, ws.Message{Type: "chat.message.created"})
	for name, conn := range map[string]*cws.Conn{"first": first, "second": second} {
		if got := readType(t, conn, readTimeout); got != "chat.message.created" {
			t.Errorf("%s bypass client got %q, want chat.message.created — under the bypass "+
				"every connection shares the configured member's targeted feed", name, got)
		}
	}
}

// TestRevalidationClosesASocketWhoseSessionIsGone is the guard for the leak that
// has nothing to do with the audience: a socket is authenticated ONCE, at upgrade,
// and then lives as long as the browser keeps it.
//
// ⚠ Before v10 that cost nothing — every fan-out was household-wide. Now the
// payload is the private content, so a member whose account is disabled (the
// middleware fails closed and revokes) or whose session simply expires would go on
// receiving message bodies over a connection that is 401 on every other request.
func TestRevalidationClosesASocketWhoseSessionIsGone(t *testing.T) {
	var verdict atomic.Int32
	verdict.Store(int32(ws.RevalidationValid))
	hub, wsURL := newRevalidatingServer(t, func(context.Context, string) (string, ws.Revalidation) {
		return "u-karel", ws.Revalidation(verdict.Load())
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(cws.StatusNormalClosure, "") })
	waitTracked(t, hub, "u-karel", 1)

	verdict.Store(int32(ws.RevalidationGone)) // revoked while the socket stays open
	waitCount(t, hub, 0)

	if n := hub.TrackedClientsForTest("u-karel"); n != 0 {
		t.Errorf("a revoked session still holds %d socket(s) — PublishTo would keep "+
			"delivering message bodies to an account that is 401 everywhere else", n)
	}
}

// TestRevalidationClosesASocketWhoseSessionChangedHands is the OTHER half of the
// pump's decision, and it had no test.
//
// ⚠ A socket is indexed under the user id it opened with. If the session it was
// authorised by now resolves to somebody else — a different member signing in on
// the same browser — a verdict of "still valid" is not enough: the connection
// would go on receiving the FIRST member's audience under the second member's
// session.
func TestRevalidationClosesASocketWhoseSessionChangedHands(t *testing.T) {
	var owner atomic.Value
	owner.Store("u-karel")
	hub, wsURL := newRevalidatingServer(t, func(context.Context, string) (string, ws.Revalidation) {
		// Always VALID — only the member behind the session changes.
		return owner.Load().(string), ws.RevalidationValid
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(cws.StatusNormalClosure, "") })
	waitTracked(t, hub, "u-karel", 1)

	owner.Store("u-marie")
	waitCount(t, hub, 0)

	if n := hub.TrackedClientsForTest("u-karel"); n != 0 {
		t.Errorf("the socket still holds %d client(s) indexed under the member who OPENED "+
			"it, while its session now belongs to another — a changed id must close the "+
			"connection just as a rejected one does", n)
	}
}

// TestRevalidationKeepsTheSocketWhenTheVerdictIsUnknown pins the difference
// between "revoked" and "could not tell".
//
// ⚠ The session store is one SQLite connection behind a 5s busy timeout, so a
// long write can make a queued lookup error. Treating that as a revocation would
// close every socket in the household at once, over sessions nobody revoked, and
// log it as if they had been. The socket must survive an undecidable verdict.
func TestRevalidationKeepsTheSocketWhenTheVerdictIsUnknown(t *testing.T) {
	var calls atomic.Int32
	hub, wsURL := newRevalidatingServer(t, func(context.Context, string) (string, ws.Revalidation) {
		calls.Add(1)
		return "", ws.RevalidationUnknown // the store could not answer
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(cws.StatusNormalClosure, "") })
	waitTracked(t, hub, "u-karel", 1)

	// Let several ticks fail to decide, then confirm the connection is still there
	// and still reachable.
	waitAtLeast(t, func() int { return int(calls.Load()) }, 4, "revalidation attempts")
	if n := hub.Count(); n != 1 {
		t.Fatalf("hub client count = %d after %d undecidable revalidations, want 1 — a "+
			"database hiccup is not a revocation", n, calls.Load())
	}
	hub.PublishTo([]string{"u-karel"}, ws.Message{Type: "chat.message.created"})
	if got := readType(t, conn, readTimeout); got != "chat.message.created" {
		t.Errorf("the kept socket got %q, want chat.message.created", got)
	}
}

// TestRevalidationRunsImmediatelyOnConnect: the pump's FIRST check does not wait
// out an interval.
//
// ⚠ The upgrade decision and the hub registration are not one atomic step, so a
// revocation that sweeps bySession in between misses the connection entirely — it
// was not indexed yet. Without an immediate first pass that socket holds an
// already-revoked session until the first tick, which in production is minutes.
// (newTwoSeamServer simulates exactly that in-window revocation, which is what
// arms the connect-time check — an unchanged epoch skips it.)
func TestRevalidationRunsImmediatelyOnConnect(t *testing.T) {
	var calls atomic.Int32
	hub, wsURL := newRevalidatingServerEvery(t, time.Hour, // a tick will never come
		func(context.Context, string) (string, ws.Revalidation) {
			calls.Add(1)
			return "u-karel", ws.RevalidationGone
		})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(cws.StatusNormalClosure, "") })

	// ⚠ THE POSITIVE SIGNAL COMES FIRST, and it is not optional. Dial returns on
	// the 101, which websocket.Accept writes BEFORE the handler reaches h.add, so
	// a bare waitCount(0) has a legal interleaving in which it observes an
	// unregistered client and passes having proven nothing — including with the
	// immediate check deleted, which is the one mutation this test exists to
	// catch. Every sibling waits for tracked==1 first; this socket is meant to die
	// at once, so it waits for the CHECK instead.
	waitAtLeast(t, func() int { return int(calls.Load()) }, 1, "revalidation attempts")
	// The interval is an hour, so anything that closed this socket did so on that
	// immediate first pass.
	waitCount(t, hub, 0)
}

// TestRevalidationKeepsAnIdentifiedlessConnection is the other side of
// TestAuthenticatedActorWithNoUserIDIsBroadcastOnly, and the branch that test
// cannot reach because it configures no Revalidate.
//
// ⚠ The handler deliberately KEEPS a connection whose actor carries no user id
// (reqctx.Actor documents "" for system/service principals) rather than refusing
// the upgrade over a targeting problem. A changed-id check that compares a real
// id against that empty one contradicts it: every such socket dies on its own
// first check, the client's backoff has already been reset by `open`, and the
// upgrade path never looks at the id — so it is re-accepted and re-closed
// forever, at a Lookup and possibly a Mint per cycle.
func TestRevalidationKeepsAnIdentifiedlessConnection(t *testing.T) {
	var calls atomic.Int32
	hub := ws.NewHub(testsupport.DiscardLogger())
	wsURL := newWSServer(t, hub, ws.Config{
		Authenticate: func(*http.Request) (ws.Upgrade, bool) {
			// Authenticated, no user id — and a real session, so the pump runs.
			return ws.Upgrade{
				Actor:     reqctx.Actor{Type: "service", Label: "importer"},
				SessionID: "s-service",
				Token:     "tok-service",
			}, true
		},
		Revalidate: func(context.Context, string) (string, ws.Revalidation) {
			// What the composition root's Revalidate really answers: the session
			// row's own user id, which is never the empty string.
			calls.Add(1)
			return "u-karel", ws.RevalidationValid
		},
		RevalidateEvery: 20 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(cws.StatusNormalClosure, "") })
	waitCount(t, hub, 1)

	// Several checks have now disagreed with c.userID, and the socket survived
	// all of them.
	waitAtLeast(t, func() int { return int(calls.Load()) }, 3, "revalidation attempts")
	if n := hub.Count(); n != 1 {
		t.Fatalf("hub client count = %d after %d revalidations of an id-less connection, want 1 "+
			"— an empty user id is not a CHANGED one, and closing on it is an unbounded "+
			"reconnect loop over a connection PublishTo cannot reach anyway", n, calls.Load())
	}
	// And it is still a live broadcast recipient, which is the whole point of
	// having kept it.
	hub.Publish(ws.Message{Type: sentinelType})
	if got := readType(t, conn, readTimeout); got != sentinelType {
		t.Errorf("the kept id-less client got %q, want the sentinel", got)
	}
}

// TestRevalidationKeepsASocketWhenTheVerdictResolvesNoID is the MIRROR of
// TestRevalidationKeepsAnIdentifiedlessConnection: there the socket carried no
// id, here the verdict does not resolve one.
//
// ⚠ An empty verdict id is "I did not resolve a member", not "a different
// member". A Revalidate that only proves the session live — a liveness-only
// re-check, a stub, an auth mode that answers yes/no — returns ("",
// RevalidationValid), which differs from every real id the sockets opened with.
// Compared naively, the FIRST healthy tick closes every identified socket in the
// household with a policy code, and ws.ts turns each of those into a login
// screen: the whole family signed out by a check that said the session was fine.
func TestRevalidationKeepsASocketWhenTheVerdictResolvesNoID(t *testing.T) {
	var calls atomic.Int32
	hub, wsURL := newRevalidatingServer(t, func(context.Context, string) (string, ws.Revalidation) {
		calls.Add(1)
		return "", ws.RevalidationValid // live, but resolves no member
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(cws.StatusNormalClosure, "") })
	waitCount(t, hub, 1)

	// Several checks have now returned an id that differs from u-karel, and the
	// socket survived all of them.
	waitAtLeast(t, func() int { return int(calls.Load()) }, 3, "revalidation attempts")
	if n := hub.Count(); n != 1 {
		t.Fatalf("hub client count = %d after %d Valid verdicts carrying no user id, want 1 — "+
			"an unresolved id is not a CHANGED one, and closing on it signs out every "+
			"identified member in the household on the first healthy tick", n, calls.Load())
	}
	// And it is still targetable, which is what makes the difference from the
	// id-less connection this mirrors.
	hub.PublishTo([]string{"u-karel"}, ws.Message{Type: sentinelType})
	if got := readType(t, conn, readTimeout); got != sentinelType {
		t.Errorf("the kept client got %q, want the sentinel", got)
	}
}

// TestOnePumpPerSessionNotPerSocket. Every tab of one browser carries the same
// cookie, so they cannot disagree about the verdict.
//
// ⚠ A ticker per SOCKET means four Lookups per interval for four tabs against a
// pool of exactly one connection (db.SetMaxOpenConns(1)) — and four Mints rather
// than one whenever a re-mint fails transiently, because that path stamps no
// roles_refreshed_at for the next tab to read. The saving is only real if the
// pump is keyed by session, so it is asserted rather than assumed.
func TestOnePumpPerSessionNotPerSocket(t *testing.T) {
	var calls atomic.Int32
	hub := ws.NewHub(testsupport.DiscardLogger())
	wsURL := newWSServer(t, hub, ws.Config{
		Authenticate: func(r *http.Request) (ws.Upgrade, bool) {
			return ws.Upgrade{
				Actor:     reqctx.Actor{UserID: "u-karel", Type: "user"},
				SessionID: "s-karel",
				Token:     "tok-karel",
			}, true
		},
		Revalidate: func(context.Context, string) (string, ws.Revalidation) {
			calls.Add(1)
			return "u-karel", ws.RevalidationValid
		},
		RevalidateEvery: 20 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 4; i++ {
		c, _, err := cws.Dial(ctx, wsURL+"/ws", nil) // four tabs, one cookie jar
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		t.Cleanup(func() { _ = c.Close(cws.StatusNormalClosure, "") })
	}
	waitCount(t, hub, 4)

	// ONE ticker for the session (no connect-time checks here: nothing moved the
	// revocation epoch, so the handler skips them). Let it run ~25 intervals:
	// four tickers would be far past this bound, one is comfortably under it.
	base := calls.Load()
	time.Sleep(500 * time.Millisecond)
	ticks := calls.Load() - base
	if ticks > 45 {
		t.Errorf("%d revalidations in ~25 intervals across 4 tabs of ONE session — that is a "+
			"ticker per socket; every tab of a cookie jar shares one verdict and must share "+
			"one query", ticks)
	}
	if ticks == 0 {
		t.Error("the session's ticker never ran; the recurring check is what bounds a " +
			"revocation nothing announces")
	}
	if n := hub.Count(); n != 4 {
		t.Errorf("hub client count = %d, want 4", n)
	}
}

// TestSocketsThatDisagreeGetSeparatePumps is the OTHER half of
// TestOnePumpPerSessionNotPerSocket, and without it pumpKey's whole reason for
// being three fields is unpinned: every other pump assertion in this file uses
// sockets that agree on all three, so collapsing the key back to a bare
// sessionID — the regression pumpKey's own doc calls a silent hazard — leaves the
// entire suite green.
//
// ⚠ Two bugs ship behind that. A later socket handing over a DIFFERENT token has
// it discarded: the ticker goes on re-checking the first socket's token and,
// when that stops resolving, closes every socket of a session that is perfectly
// live. And a session whose first socket is an id-less service principal pins
// openedAs to "" for the pump's whole life, disabling the changed-id guard for
// every later socket on it.
func TestSocketsThatDisagreeGetSeparatePumps(t *testing.T) {
	for _, tc := range []struct {
		name            string
		secondUser      string
		secondToken     string
		wantPumps       int
		whenItRegresses string
	}{{
		name: "same session, same user, same token — one ticker",
		// The ORDINARY case, and the control: keying on all three must not split
		// the tabs of one cookie jar, which is what one-pump-per-session buys.
		secondUser: "u-karel", secondToken: "tok-karel", wantPumps: 1,
		whenItRegresses: "every tab of one cookie jar agrees on all three and must share ONE ticker",
	}, {
		name:       "same session, different token — two tickers",
		secondUser: "u-karel", secondToken: "tok-karel-reissued", wantPumps: 2,
		whenItRegresses: "the second socket's token was discarded and the ticker is re-checking a " +
			"token nobody holds any more — when that stops resolving it closes a live session",
	}, {
		name: "same session, no user id — two tickers",
		// An id-less service principal. Its pump must not be the one a real member's
		// socket joins, or that member's changed-id guard is disabled for good.
		secondUser: "", secondToken: "tok-karel", wantPumps: 2,
		whenItRegresses: "an id-less principal pinned openedAs to \"\" for the whole pump, disabling " +
			"the changed-id guard for every later socket on that session",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			var nth atomic.Int32
			hub := ws.NewHub(testsupport.DiscardLogger())
			// One SESSION throughout; only the user id and the token vary, and only
			// on the second dial.
			wsURL := newWSServer(t, hub, ws.Config{
				Authenticate: func(*http.Request) (ws.Upgrade, bool) {
					user, token := "u-karel", "tok-karel"
					if nth.Add(1) == 2 {
						user, token = tc.secondUser, tc.secondToken
					}
					return ws.Upgrade{
						Actor:     reqctx.Actor{UserID: user, Type: "user"},
						SessionID: "s-karel",
						Token:     token,
					}, true
				},
				Revalidate: func(context.Context, string) (string, ws.Revalidation) {
					return "u-karel", ws.RevalidationValid
				},
				RevalidateEvery: time.Hour, // no tick will come; only registration matters
			})

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for i := 0; i < 2; i++ {
				c, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
				if err != nil {
					t.Fatalf("dial %d: %v", i, err)
				}
				t.Cleanup(func() { _ = c.Close(cws.StatusNormalClosure, "") })
			}
			waitCount(t, hub, 2)

			waitFor(t, hub.TrackedPumpsForTest, tc.wantPumps, "registered revalidation pumps")
			if n := hub.TrackedPumpsForTest(); n != tc.wantPumps {
				t.Errorf("%d pumps, want %d — %s", n, tc.wantPumps, tc.whenItRegresses)
			}
		})
	}
}

// TestRevokedSocketClosesWithAPolicyCode. A revocation is not a restart, and the
// close code is the only place that distinction reaches the browser.
//
// ⚠ Closed as StatusNormalClosure, a socket dropped because its session is gone
// looks exactly like a deploy, so the client reconnects — onto an upgrade that
// will 401 for the rest of the tab's life, once per backoff cap, forever, with
// nothing anywhere in the app saying the session ended.
func TestRevokedSocketClosesWithAPolicyCode(t *testing.T) {
	hub, wsURL := newIdentityServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialAsSession(ctx, t, wsURL, "u-karel", "s-phone")
	waitCount(t, hub, 1)

	hub.DisconnectSession("s-phone")

	_, _, err := conn.Read(ctx)
	if err == nil {
		t.Fatal("read succeeded after the session was revoked, want a close")
	}
	if got := cws.CloseStatus(err); got != cws.StatusPolicyViolation {
		t.Errorf("revoked socket closed with status %v, want StatusPolicyViolation (%v) — a "+
			"normal closure tells the browser to reconnect, and it will be 401ed every time",
			got, cws.StatusPolicyViolation)
	}
}

// TestSessionPumpStopsWhenTheLastSocketGoes pins the refcount, which is the most
// intricate state in the package and the least observable.
//
// ⚠ Nothing else reaches releaseSessionPump's ordinary path. The two tests that
// do tear a pump down go through retireSessionPump (the ticker retiring on a
// verdict); TestOnePumpPerSessionNotPerSocket never disconnects anything. So an
// off-by-one — decrementing on the wrong branch, or skipping the delete — leaks a
// ticker that keeps issuing a Lookup, and past the threshold a Mint, every
// interval for a session with NO sockets left, for the process's lifetime, while
// Count, PublishTo and every other assertion in this file stay correct.
//
// ⚠ And the release must not fire EARLY either: the first tab of two closing has
// to leave its neighbour's ticker running, or the surviving socket silently loses
// the only backstop for a revocation nothing announces.
func TestSessionPumpStopsWhenTheLastSocketGoes(t *testing.T) {
	var calls atomic.Int32
	hub, wsURL := newRevalidatingServer(t, func(context.Context, string) (string, ws.Revalidation) {
		calls.Add(1)
		return "u-karel", ws.RevalidationValid
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dial := func(who string) *cws.Conn {
		c, _, err := cws.Dial(ctx, wsURL+"/ws", nil) // two tabs, one cookie jar
		if err != nil {
			t.Fatalf("dial %s: %v", who, err)
		}
		t.Cleanup(func() { _ = c.Close(cws.StatusNormalClosure, "") })
		return c
	}
	first, second := dial("first tab"), dial("second tab")
	waitCount(t, hub, 2)
	waitFor(t, hub.TrackedPumpsForTest, 1, "registered revalidation pumps")

	// One tab out of two: the ticker stays, because the other still depends on it.
	_ = first.Close(cws.StatusNormalClosure, "")
	waitCount(t, hub, 1)
	if n := hub.TrackedPumpsForTest(); n != 1 {
		t.Fatalf("%d pumps after ONE of two tabs closed, want 1 — the surviving socket "+
			"would be left with no recurring check at all", n)
	}
	// And it is genuinely still ticking, not merely still registered.
	waitAtLeast(t, func() int { return int(calls.Load()) }, int(calls.Load())+2,
		"revalidations after one tab closed")

	// The last one out stops it.
	_ = second.Close(cws.StatusNormalClosure, "")
	waitCount(t, hub, 0)
	waitFor(t, hub.TrackedPumpsForTest, 0, "registered revalidation pumps")

	// ⚠ And the GOROUTINE stopped, not just its registry entry: a pump that lost
	// its entry but kept its context would go on querying forever and the map
	// assertion above would still pass. One in-flight tick may land after the
	// cancel; ~10 intervals of a live ticker could not.
	settled := calls.Load()
	time.Sleep(200 * time.Millisecond)
	if n := calls.Load(); n > settled+1 {
		t.Errorf("the ticker ran %d more times after its last socket closed, want at most 1 "+
			"in-flight — a pump nobody holds must stop querying the store", n-settled)
	}
}

// newRevalidatingServer builds a hub whose /ws accepts anyone as u-karel and
// re-takes the decision through revalidate every 20ms.
func newRevalidatingServer(t *testing.T, revalidate func(context.Context, string) (string, ws.Revalidation)) (*ws.Hub, string) {
	t.Helper()
	return newRevalidatingServerEvery(t, 20*time.Millisecond, revalidate)
}

func newRevalidatingServerEvery(t *testing.T, every time.Duration, revalidate func(context.Context, string) (string, ws.Revalidation)) (*ws.Hub, string) {
	t.Helper()
	return newTwoSeamServer(t, nil, revalidate, every)
}

// newTwoSeamServer builds a hub whose /ws is configured with the given
// connect-time and recurring checks, either of which may be nil.
//
// ⚠ Its Authenticate SIMULATES A REVOCATION LANDING DURING THE UPGRADE — the
// DisconnectSession call moves the hub's revocation epoch inside the window the
// connect-time check exists for (between the upgrade decision and h.add), which
// is the only condition under which the handler runs that check at all. Every
// test that asserts on the connect-time seam depends on it; without it the
// epoch is unchanged and the check is (correctly) skipped as the common case.
func newTwoSeamServer(t *testing.T, recheck, revalidate ws.RevalidateFunc, every time.Duration) (*ws.Hub, string) {
	t.Helper()
	hub := ws.NewHub(testsupport.DiscardLogger())
	wsURL := newWSServer(t, hub, ws.Config{
		Authenticate: func(*http.Request) (ws.Upgrade, bool) {
			hub.DisconnectSession("s-elsewhere") // a revocation lands mid-upgrade
			return ws.Upgrade{
				Actor:     reqctx.Actor{UserID: "u-karel", Type: "user"},
				SessionID: "s-karel",
				Token:     "tok-karel",
			}, true
		},
		Recheck:         recheck,
		Revalidate:      revalidate,
		RevalidateEvery: every,
	})
	return hub, wsURL
}

// TestConnectTimeCheckUsesRecheck pins the SEAM between the two checks, which
// nothing else in this file touches: every other test here configures Revalidate
// alone and so only ever exercises the fallback.
//
// ⚠ Config.Recheck exists for one reason. A deploy drops every socket in the
// household and has every tab redial at once; routing the connect-time check
// through the Mint-capable Revalidate then means a second Lookup AND, whenever
// roles are stale, a Mint PER SOCKET — N concurrent mints on the auth service and
// 2N lookups on a pool of exactly one connection, none of them seeing another's
// roles_refreshed_at stamp. Nothing else asserts the separation, so deleting the
// `recheck` variable and handing cfg.Revalidate to the immediate check leaves the
// whole suite green and the regression invisible until a production deploy.
func TestConnectTimeCheckUsesRecheck(t *testing.T) {
	var recheckCalls, revalidateCalls atomic.Int32
	// An hour: a recurring tick will never come, so every call counted below is
	// the connect-time one.
	_, wsURL := newTwoSeamServer(t,
		func(context.Context, string) (string, ws.Revalidation) {
			recheckCalls.Add(1)
			return "u-karel", ws.RevalidationValid
		},
		func(context.Context, string) (string, ws.Revalidation) {
			revalidateCalls.Add(1)
			return "u-karel", ws.RevalidationValid
		},
		time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(cws.StatusNormalClosure, "") })

	waitAtLeast(t, func() int { return int(recheckCalls.Load()) }, 1, "connect-time Recheck calls")
	if n := revalidateCalls.Load(); n != 0 {
		t.Errorf("the connect-time check called Revalidate %d times, want 0 — it must go through "+
			"Recheck, or a deploy puts a Mint per socket on the auth service", n)
	}
}

// TestConnectTimeCheckFallsBackToRevalidate is the other half of the seam: a
// Config that predates Recheck, or one that simply does not need a cheaper
// connect-time query, must still get the connect-time pass.
func TestConnectTimeCheckFallsBackToRevalidate(t *testing.T) {
	var revalidateCalls atomic.Int32
	hub, wsURL := newTwoSeamServer(t, nil,
		func(context.Context, string) (string, ws.Revalidation) {
			revalidateCalls.Add(1)
			return "u-karel", ws.RevalidationGone
		},
		time.Hour) // a tick will never come; anything that closes did so at connect

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(cws.StatusNormalClosure, "") })

	waitAtLeast(t, func() int { return int(revalidateCalls.Load()) }, 1, "connect-time Revalidate calls")
	waitCount(t, hub, 0)
}

// TestRecheckRunsWithNoRecurringCheckConfigured. The two fields are independent
// knobs, and a Config carrying only Recheck is a legible one: re-take the cheap
// decision at connect, run no ticker.
//
// ⚠ Gated on cfg.Revalidate, that Config got NEITHER check. The race the
// connect-time pass closes — a revocation sweeping bySession between the upgrade
// decision and h.add, which misses a client that is not indexed yet — stayed
// wide open, the configured callback was never once invoked, and nothing
// errored, warned, or failed to say so.
func TestRecheckRunsWithNoRecurringCheckConfigured(t *testing.T) {
	var recheckCalls atomic.Int32
	hub, wsURL := newTwoSeamServer(t,
		func(context.Context, string) (string, ws.Revalidation) {
			recheckCalls.Add(1)
			return "", ws.RevalidationGone
		},
		nil, // no recurring check at all
		time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(cws.StatusNormalClosure, "") })

	waitAtLeast(t, func() int { return int(recheckCalls.Load()) }, 1, "connect-time Recheck calls")
	waitCount(t, hub, 0)
	// And no ticker was registered for a Config that configured none — starting
	// one would run it with a nil revalidate and panic on its first tick.
	if n := hub.TrackedPumpsForTest(); n != 0 {
		t.Errorf("%d revalidation pumps registered with no Revalidate configured, want 0", n)
	}
}

// TestDisconnectSessionClosesEveryTabOfThatSession: the revalidation ticker
// bounds the exposure, but auth knows the moment it revokes, so it says so
// directly — and every tab sharing the revoked cookie has to go.
func TestDisconnectSessionClosesEveryTabOfThatSession(t *testing.T) {
	hub, wsURL := newIdentityServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dialAsSession(ctx, t, wsURL, "u-karel", "s-phone")
	dialAsSession(ctx, t, wsURL, "u-karel", "s-phone") // two tabs, one cookie jar
	marie := dialAs(ctx, t, wsURL, "u-marie")
	waitCount(t, hub, 3)

	hub.DisconnectSession("s-phone")
	waitCount(t, hub, 1)

	if n := hub.TrackedClientsForTest("u-karel"); n != 0 {
		t.Errorf("DisconnectSession left %d socket(s) for the revoked session, want 0", n)
	}
	// Marie is untouched and still reachable, which is the half a blunt "close
	// everything" would get wrong.
	hub.PublishTo([]string{"u-marie"}, ws.Message{Type: "chat.message.created"})
	if got := readType(t, marie, readTimeout); got != "chat.message.created" {
		t.Errorf("marie got %q after another session was disconnected, want chat.message.created", got)
	}
}

// TestDisconnectSessionSparesTheMembersOtherDevices is the reason revocation is
// keyed by session and not by member.
//
// ⚠ Every revocation auth performs is per-session: logout drops the calling
// device's token, the fail-closed re-mint one row. Closing by USER id instead
// would mean logging out on a phone tears down the laptop's socket — a session
// nobody revoked, losing every frame published before the reconnect backoff
// completes, on every logout, forever.
func TestDisconnectSessionSparesTheMembersOtherDevices(t *testing.T) {
	hub, wsURL := newIdentityServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dialAsSession(ctx, t, wsURL, "u-karel", "s-phone")
	laptop := dialAsSession(ctx, t, wsURL, "u-karel", "s-laptop")
	waitTracked(t, hub, "u-karel", 2)

	hub.DisconnectSession("s-phone") // he logged out on the phone, and only there
	waitCount(t, hub, 1)

	if n := hub.TrackedClientsForTest("u-karel"); n != 1 {
		t.Fatalf("u-karel holds %d socket(s) after logging out on ONE device, want 1 — the "+
			"laptop's session was never revoked and its live feed must survive", n)
	}
	hub.PublishTo([]string{"u-karel"}, ws.Message{Type: "chat.message.created"})
	if got := readType(t, laptop, readTimeout); got != "chat.message.created" {
		t.Errorf("the surviving device got %q, want chat.message.created", got)
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
	// The same leak, one map over: remove() drops from the session index too.
	if n := hub.TrackedSessionsForTest(); n != 0 {
		t.Errorf("the per-session index still holds %d ids with no connections, want 0", n)
	}
}

// TestNotifyStampsTheOriginFromTheRequest and its targeted sibling below pin the
// field that moved out of main.go's closure and into the hub.
//
// ⚠ Origin is not decoration. It is how a tab tells its OWN echo apart from a
// change made on another device: the frontend skips the "changed elsewhere" toast
// when origin equals its own client id (PRD 3544). Drop it and nothing fails
// anywhere in the backend — the symptom is every member being toasted for their
// own edits.
func TestNotifyStampsTheOriginFromTheRequest(t *testing.T) {
	hub, wsURL := newIdentityServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialAs(ctx, t, wsURL, "u-karel")
	waitTracked(t, hub, "u-karel", 1)

	reqCtx := reqctx.WithRequest(context.Background(), reqctx.RequestInfo{ClientID: "tab-7"})
	hub.Notify(reqCtx, "card.moved", nil)

	if got := readOrigin(t, conn, readTimeout); got != "tab-7" {
		t.Errorf("Notify stamped origin %q, want tab-7 — without it the tab that made the "+
			"change is toasted for its own echo", got)
	}
}

func TestNotifyToStampsTheOriginFromTheRequest(t *testing.T) {
	hub, wsURL := newIdentityServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialAs(ctx, t, wsURL, "u-karel")
	waitTracked(t, hub, "u-karel", 1)

	reqCtx := reqctx.WithRequest(context.Background(), reqctx.RequestInfo{ClientID: "tab-7"})
	hub.NotifyTo(reqCtx, []string{"u-karel"}, "chat.message.created", nil)

	if got := readOrigin(t, conn, readTimeout); got != "tab-7" {
		t.Errorf("NotifyTo stamped origin %q, want tab-7 — a member-restricted module must "+
			"get the same echo suppression as a broadcasting one", got)
	}
}

// TestNotifyLeavesTheOriginEmptyWithNoRequest: a background job, a cron run or a
// BE→BE call has no tab behind it, and an invented origin would suppress the
// toast on whichever tab happened to match.
func TestNotifyLeavesTheOriginEmptyWithNoRequest(t *testing.T) {
	hub, wsURL := newIdentityServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialAs(ctx, t, wsURL, "u-karel")
	waitTracked(t, hub, "u-karel", 1)

	hub.Notify(context.Background(), "card.moved", nil)

	if got := readOrigin(t, conn, readTimeout); got != "" {
		t.Errorf("origin = %q for a change with no request behind it, want empty", got)
	}
}

// readOrigin reads one frame and returns its Origin. See readMessage.
func readOrigin(t *testing.T, conn *cws.Conn, within time.Duration) string {
	t.Helper()
	return readMessage(t, conn, within).Origin
}
