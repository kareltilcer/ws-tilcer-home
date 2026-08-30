package auth_test

// RevalidateSession is the decision an open websocket re-takes on a ticker, and
// it has no HTTP surface: the ws tests all stub ws.Config.Revalidate with a
// closure of their own, so without this file every mapping RevalidateSession
// makes is unpinned and an inverted case ships with the suite green. Each test
// below names the leak its mapping prevents.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
)

// TestRevalidateSession_LiveSessionStaysLive: the ordinary tick. Nothing has
// happened, and the socket must survive it.
func TestRevalidateSession_LiveSessionStaysLive(t *testing.T) {
	h := newHarness(t)
	sess, _ := h.authed(t)

	userID, verdict := h.cfg.RevalidateSession(context.Background(), sess.Value)
	if verdict != auth.SessionLive {
		t.Errorf("verdict = %v, want SessionLive — a healthy session must not lose its socket", verdict)
	}
	if userID != "u1" {
		t.Errorf("userID = %q, want u1 — the caller compares it against the id the socket opened with", userID)
	}
	if h.fake.mintCalls != 0 {
		t.Errorf("mint calls = %d, want 0 — a tick inside the refresh threshold must not reach "+
			"the auth service", h.fake.mintCalls)
	}
}

// TestRevalidateSession_GoneCases covers the revocations nothing announces — the
// ones the pump exists for. A socket that outlives any of them keeps delivering
// member-restricted payloads to a session that is 401 on every HTTP request.
func TestRevalidateSession_GoneCases(t *testing.T) {
	t.Run("revoked by logout", func(t *testing.T) {
		h := newHarness(t)
		sess, csrf := h.authed(t)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
		req.AddCookie(sess)
		req.AddCookie(csrf)
		req.Header.Set("Origin", origin)
		req.Header.Set("X-CSRF-Token", csrf.Value)
		if rr := h.do(t, req); rr.Code != http.StatusNoContent {
			t.Fatalf("logout = %d, want 204", rr.Code)
		}
		if _, verdict := h.cfg.RevalidateSession(context.Background(), sess.Value); verdict != auth.SessionGone {
			t.Errorf("verdict = %v after logout, want SessionGone", verdict)
		}
	})
	t.Run("past the TTL", func(t *testing.T) {
		h := newHarness(t)
		sess, _ := h.authed(t)
		h.clock = h.clock.Add(48 * time.Hour) // the harness TTL is 24h
		if _, verdict := h.cfg.RevalidateSession(context.Background(), sess.Value); verdict != auth.SessionGone {
			t.Errorf("verdict = %v past the TTL, want SessionGone — an expiring TTL is exactly the "+
				"revocation nothing announces, which is why the pump exists", verdict)
		}
	})
	t.Run("token no row matches", func(t *testing.T) {
		h := newHarness(t)
		if _, verdict := h.cfg.RevalidateSession(context.Background(), "not-a-token"); verdict != auth.SessionGone {
			t.Errorf("verdict = %v for an unknown token, want SessionGone", verdict)
		}
	})
}

// TestRevalidateSession_ClosedAccountIsGoneAndAnnounced pins the whole reason
// RevalidateSession exists rather than the caller repeating a Lookup: it runs the
// SAME fail-closed re-mint the request middleware does.
//
// ⚠ A bare row check is strictly weaker. A member disabled in auth keeps a
// perfectly valid session row until something re-mints, and a browser tab that
// issues no HTTP request never triggers one — so a socket checked only against the
// row would keep receiving targeted payloads for the whole session TTL.
func TestRevalidateSession_ClosedAccountIsGoneAndRevoked(t *testing.T) {
	h := newHarness(t)
	sess, _ := h.authed(t)

	h.clock = h.clock.Add(20 * time.Minute) // past the harness's 15m threshold
	h.fake.mintErr = auth.ErrUserClosed

	userID, verdict := h.cfg.RevalidateSession(context.Background(), sess.Value)
	if verdict != auth.SessionGone {
		t.Fatalf("verdict = %v for a closed account, want SessionGone", verdict)
	}
	if userID != "u1" {
		t.Errorf("userID = %q, want u1", userID)
	}
	if h.fake.mintCalls != 1 {
		t.Errorf("mint calls = %d, want 1 — the row was still live, so only a re-mint can "+
			"discover that the account is closed", h.fake.mintCalls)
	}
	// And it went through the shared fail-closed path: the row is revoked in the
	// database, which is what makes every later Lookup — this session's next tick,
	// its next upgrade, its next HTTP request — fail closed too.
	if ids := revokedSessionIDs(t, h.db); len(ids) != 1 {
		t.Errorf("%d session rows revoked, want 1 — the fail-closed re-mint is shared with the "+
			"request middleware and must mark the row from either caller", len(ids))
	}
	// ⚠ AND IT ANNOUNCES NOTHING. SessionGone IS the announcement here: the hook
	// is ws.Hub.DisconnectSession, and the revalidation pump has to retire its
	// ticker BEFORE it disconnects, or a socket arriving in between joins a pump
	// about to be cancelled and never gets a recurring check again. Firing from
	// inside this call put the disconnect two frames deep in the check, where no
	// retire could have happened yet.
	if len(h.revoked) != 0 {
		t.Errorf("revalidation announced %v, want nothing — the caller disconnects, after "+
			"retiring the session's ticker; announcing from here inverts that order", h.revoked)
	}
}

// TestRevalidateSession_StoreErrorIsUnknown is the mapping that keeps the
// household's boards up.
//
// ⚠ The pool is a single connection behind a busy timeout, so one long write can
// make a queued Lookup error. Collapsing that onto SessionGone would close every
// socket in the house at once and log it against sessions nobody revoked.
func TestRevalidateSession_StoreErrorIsUnknown(t *testing.T) {
	h := newHarness(t)
	sess, _ := h.authed(t)
	h.db.Close() // every query from here on fails

	if _, verdict := h.cfg.RevalidateSession(context.Background(), sess.Value); verdict != auth.SessionUnknown {
		t.Errorf("verdict = %v when the store cannot answer, want SessionUnknown — a database "+
			"hiccup is not a revocation", verdict)
	}
}

// TestRevalidateSession_NothingToCheckIsUnknown: SessionUnknown is the zero value
// on purpose, so a caller with nothing to check lands on the outcome that keeps
// the socket rather than the one that drops it.
func TestRevalidateSession_NothingToCheckIsUnknown(t *testing.T) {
	h := newHarness(t)
	if _, verdict := h.cfg.RevalidateSession(context.Background(), ""); verdict != auth.SessionUnknown {
		t.Errorf("verdict = %v for an empty token, want SessionUnknown", verdict)
	}
	var empty auth.Config
	if _, verdict := empty.RevalidateSession(context.Background(), "tok"); verdict != auth.SessionUnknown {
		t.Errorf("verdict = %v with no session store, want SessionUnknown", verdict)
	}
	if auth.SessionUnknown != 0 {
		t.Error("SessionUnknown must stay the zero value, so a caller who forgets to answer errs " +
			"towards keeping live boards up")
	}
}

// TestRevalidateSession_FailedRevokeKeepsTheSocket guards against an unbounded
// reconnect loop, and it is deliberately NOT the obvious verdict.
//
// ⚠ SessionGone tells the caller to DROP the connection, while the upgrade path is
// a bare Lookup. When the account is closed but the revoke UPDATE did not land the
// row is still live: the browser reconnects 800ms later (its backoff resets on
// open), is ACCEPTED, and is closed again by the pump's immediate check — forever,
// at two lookups, one mint and one more failing write per cycle, against the very
// single-connection pool whose contention caused the failed write. Closing buys
// nothing, because the reconnect restores the same feed. So the socket is kept and
// the next tick re-mints and retries, roles_refreshed_at never having been stamped.
func TestRevalidateSession_FailedRevokeKeepsTheSocket(t *testing.T) {
	h := newHarness(t)
	sess, _ := h.authed(t)

	// Make the revoke UPDATE — and only that UPDATE — fail. `UPDATE OF revoked_at`
	// fires for RevokeByID and not for the roles or last-seen writes, so Lookup and
	// RefreshIdentity keep working and the failure is exactly the one being modelled.
	if _, err := h.db.Exec(`CREATE TRIGGER test_block_revoke BEFORE UPDATE OF revoked_at ON sessions
		BEGIN SELECT RAISE(ABORT, 'revoke write failed'); END`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	h.clock = h.clock.Add(20 * time.Minute)
	h.fake.mintErr = auth.ErrUserClosed

	if _, verdict := h.cfg.RevalidateSession(context.Background(), sess.Value); verdict != auth.SessionUnknown {
		t.Errorf("verdict = %v when the revoke did not land, want SessionUnknown — SessionGone "+
			"drops a socket the upgrade path would immediately re-accept, which is a reconnect "+
			"loop and not a revocation", verdict)
	}
	if len(h.revoked) != 0 {
		t.Errorf("announced %v for a revocation that did not happen — the hook must fire only "+
			"when the row was actually marked", h.revoked)
	}
	if ids := revokedSessionIDs(t, h.db); len(ids) != 0 {
		t.Fatalf("%d session rows are revoked, want 0 — the premise of this test is that the "+
			"write failed", len(ids))
	}
	// Keeping the SOCKET must not soften the request path: HTTP still fails closed
	// on the same state, and a 401 does not loop.
	req := httptest.NewRequest(http.MethodGet, "/api/things", nil)
	req.AddCookie(sess)
	if rr := h.do(t, req); rr.Code != http.StatusUnauthorized {
		t.Errorf("HTTP request for a closed account = %d, want 401", rr.Code)
	}
}

// TestRevalidateSession_RevokeSurvivesACancelledCaller. The revoke is the
// security-critical half, and from the websocket pump the context it would
// otherwise inherit is the CONNECTION's.
//
// ⚠ A member closing their tab while the mint is in flight cancels that context.
// Run on the caller's context the UPDATE never happens: the row stays live,
// nothing is announced, and the loud "revoke FAILED" error fires for what was a
// normal disconnect — after which no socket is left to run the pump, so the closed
// account's session survives until an HTTP request that may never come.
func TestRevalidateSession_RevokeSurvivesACancelledCaller(t *testing.T) {
	h := newHarness(t)
	sess, _ := h.authed(t)

	h.clock = h.clock.Add(20 * time.Minute)
	h.fake.mintErr = auth.ErrUserClosed

	// Cancel from INSIDE the mint: that is the real window. The lookup has already
	// succeeded and the verdict "this account is closed" has already been taken
	// when the socket goes away, so the revoke is the only step left to lose.
	ctx, cancel := context.WithCancel(context.Background())
	h.fake.onMint = cancel

	_, verdict := h.cfg.RevalidateSession(ctx, sess.Value)

	if ids := revokedSessionIDs(t, h.db); len(ids) != 1 {
		t.Errorf("%d session rows revoked, want 1 — the fail-closed revoke must run on a context "+
			"detached from whoever happened to discover the closure", len(ids))
	}
	// The verdict is what carries the disconnect to the pump (which retires its
	// ticker and then closes every socket of the session), so losing it to a
	// cancelled caller costs the member's other tabs their disconnect just as
	// surely as losing the UPDATE would.
	if verdict != auth.SessionGone {
		t.Errorf("verdict = %v, want SessionGone — the revoke landed, and the caller only "+
			"disconnects the session's sockets if it is told so", verdict)
	}
}

// ⚠ THE PUMP REFRESHES THE IDENTITY TOO, AND IT IS THE PATH THAT USUALLY DOES.
// The re-mint writes the member's email and display name alongside the roles
// (D270), and `push.Store.Members` projects the household directory from whichever
// session was confirmed most recently — so for anybody sitting on an open socket
// rather than issuing requests, THIS tick is what carries a rename to everybody
// else. The HTTP middleware's copy of this is covered in auth_test.go; nothing
// covered the tick, and the two do not share a caller.
//
// ⚠ AND IT IS ASSERTED THROUGH A CANCELLED CALLER, which is the half that is not
// the middleware's. From here ctx is a connection's, so a member closing their tab
// in the window between Mint returning and the UPDATE would cancel the write —
// leaving the identity fetched and thrown away, the stamp unmoved, and the session
// re-minting on every tick for the rest of its life. The write is detached for
// exactly this, the same as the revoke above.
func TestRevalidateSession_ReMintRefreshesTheIdentityOnADetachedWrite(t *testing.T) {
	h := newHarness(t)
	sess, _ := h.authed(t) // logged in as Marie / marie@tilcer.cz

	// Renamed (and re-addressed) in auth since the socket was opened.
	h.fake.mintID = auth.Identity{
		UserID: "u1", Email: "marie.nova@tilcer.cz", DisplayName: "Marie Nová", Roles: []string{"admin"},
	}
	h.clock = h.clock.Add(20 * time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	h.fake.onMint = cancel

	userID, verdict := h.cfg.RevalidateSession(ctx, sess.Value)
	if verdict != auth.SessionLive || userID != "u1" {
		t.Fatalf("RevalidateSession = %q/%v, want u1/SessionLive — a rename is not a revocation",
			userID, verdict)
	}

	email, name, roles := cachedIdentity(t, h.db)
	if name != "Marie Nová" || email != "marie.nova@tilcer.cz" {
		t.Errorf("session row = %q/%q after a tick, want the minted identity — the directory is "+
			"projected from this row and a socket-only member has nothing else refreshing it",
			email, name)
	}
	if roles != `["admin"]` {
		t.Errorf("roles = %s, want [\"admin\"] — the roles half must not regress", roles)
	}
	if h.fake.mintCalls != 1 {
		t.Errorf("mint calls = %d, want 1", h.fake.mintCalls)
	}
}

// TestCheckSession pins the bare row check the websocket's CONNECT-TIME seam is
// built on. Like wsRevalidation's, its three-state mapping is a household-wide
// outage inverted silently: err mapped to SessionGone policy-closes a redialing
// socket on one contended connect-time query, and the frontend answers a policy
// close by signing the member out.
func TestCheckSession(t *testing.T) {
	t.Run("live row is Live, with its member", func(t *testing.T) {
		h := newHarness(t)
		sess, _ := h.authed(t)
		userID, verdict := h.cfg.CheckSession(context.Background(), sess.Value)
		if verdict != auth.SessionLive {
			t.Errorf("verdict = %v, want SessionLive", verdict)
		}
		if userID != "u1" {
			t.Errorf("userID = %q, want u1 — the caller compares it against the id the socket opened with", userID)
		}
	})
	t.Run("closed account with a live row is still Live — and it must not mint", func(t *testing.T) {
		h := newHarness(t)
		sess, _ := h.authed(t)
		h.clock = h.clock.Add(20 * time.Minute) // roles are stale
		h.fake.mintErr = auth.ErrUserClosed
		if _, verdict := h.cfg.CheckSession(context.Background(), sess.Value); verdict != auth.SessionLive {
			t.Errorf("verdict = %v, want SessionLive — CheckSession is deliberately the WEAK half; "+
				"the fail-closed re-mint belongs on the session's ticker", verdict)
		}
		// The whole reason this is not RevalidateSession: a deploy redials every
		// tab at once, and a Mint per socket is the cost the Recheck seam exists
		// to avoid.
		if h.fake.mintCalls != 0 {
			t.Errorf("CheckSession minted %d time(s), want 0 — it must never be the Mint-capable check",
				h.fake.mintCalls)
		}
	})
	t.Run("token no row matches is Gone", func(t *testing.T) {
		h := newHarness(t)
		if _, verdict := h.cfg.CheckSession(context.Background(), "not-a-token"); verdict != auth.SessionGone {
			t.Errorf("verdict = %v for an unknown token, want SessionGone", verdict)
		}
	})
	t.Run("store error is Unknown", func(t *testing.T) {
		h := newHarness(t)
		sess, _ := h.authed(t)
		h.db.Close() // every query from here on fails
		if _, verdict := h.cfg.CheckSession(context.Background(), sess.Value); verdict != auth.SessionUnknown {
			t.Errorf("verdict = %v when the store cannot answer, want SessionUnknown — a database "+
				"hiccup is not a revocation", verdict)
		}
	})
	t.Run("nothing to check is Unknown", func(t *testing.T) {
		h := newHarness(t)
		if _, verdict := h.cfg.CheckSession(context.Background(), ""); verdict != auth.SessionUnknown {
			t.Errorf("verdict = %v for an empty token, want SessionUnknown", verdict)
		}
		var empty auth.Config
		if _, verdict := empty.CheckSession(context.Background(), "tok"); verdict != auth.SessionUnknown {
			t.Errorf("verdict = %v with no session store, want SessionUnknown", verdict)
		}
	})
}
