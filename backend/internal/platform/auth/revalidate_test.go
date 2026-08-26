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
func TestRevalidateSession_ClosedAccountIsGoneAndAnnounced(t *testing.T) {
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
	// And it went through the shared fail-closed path: revoked in the database and
	// announced, so the member's other tabs on this session close with it.
	if want := revokedSessionIDs(t, h.db); len(want) != 1 || len(h.revoked) != 1 || h.revoked[0] != want[0] {
		t.Errorf("revalidation announced %v, want exactly the revoked session %v", h.revoked, want)
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
	// RefreshRoles keep working and the failure is exactly the one being modelled.
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

	h.cfg.RevalidateSession(ctx, sess.Value)

	if ids := revokedSessionIDs(t, h.db); len(ids) != 1 {
		t.Errorf("%d session rows revoked, want 1 — the fail-closed revoke must run on a context "+
			"detached from whoever happened to discover the closure", len(ids))
	}
	if len(h.revoked) != 1 {
		t.Errorf("announced %v, want the one revoked session — a cancelled caller must not cost "+
			"the member's other tabs their disconnect", h.revoked)
	}
}
