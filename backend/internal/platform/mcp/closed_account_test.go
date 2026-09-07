package mcp_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
)

// Leak row 5 — a token outlives its owner's account being closed in auth (D287).
//
// ⚠ THE TWO HALVES ARE NOT THE SAME QUESTION, and conflating them is the failure.
// "auth says this account is closed" must revoke the token row; "auth did not
// answer" must revoke NOTHING — getting the second one wrong turns a five-minute
// outage into every household token needing to be re-minted by hand.

// fakeAuthenticator answers Mint however a test tells it to.
//
// ⚠ Login is never reached on this path: the MCP door has no login, and a token
// is minted through a session on the other door.
type fakeAuthenticator struct {
	mints atomic.Int32
	err   error
	id    auth.Identity
	// hold is how long a Mint takes. ⚠ It is what makes the coalescing test test
	// COALESCING: with an instantaneous Mint, twelve goroutines can each finish
	// before the next one starts, and the singleflight has nothing to collapse.
	hold time.Duration
}

func (f *fakeAuthenticator) Login(context.Context, string, string) (auth.Identity, error) {
	return auth.Identity{}, auth.ErrBadCredentials
}

func (f *fakeAuthenticator) Mint(_ context.Context, userID string) (auth.Identity, error) {
	f.mints.Add(1)
	if f.hold > 0 {
		time.Sleep(f.hold)
	}
	if f.err != nil {
		return auth.Identity{}, f.err
	}
	id := f.id
	id.UserID = userID
	return id, nil
}

func TestClosedAccountRevokesTheTokenRow(t *testing.T) {
	authr := &fakeAuthenticator{err: auth.ErrUserClosed}
	// ⚠ A ZERO refresh window means every call is past the threshold, which is what
	// makes the re-mint observable inside one test rather than in fifteen minutes.
	h := newHarness(t, withAuthenticator(authr, 0))
	secret, tok := h.mintToken(memberA, "Claude", nil, time.Time{})

	if rr := h.rpc(secret, "tools/list", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("a call for a closed account got %d, want 401", rr.Code)
	}
	if h.tokenRow(tok.ID).revokedAt == "" {
		t.Fatal("the token row was NOT revoked for a closed account. A bearer that" +
			" survives its owner being closed in auth is a key to a house whose lock" +
			" was changed (D287).")
	}

	// ⚠ AND THE NEXT CALL IS A CLEAN 401 AGAINST A REVOKED TOKEN, NOT A SECOND
	// RE-MINT. The revoke is what makes the outcome cheap on every subsequent call:
	// without it, a closed account would put a Mint on the auth service forever.
	minted := authr.mints.Load()
	if rr := h.rpc(secret, "tools/list", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("the call after the revoke got %d, want 401", rr.Code)
	}
	if authr.mints.Load() != minted {
		t.Fatalf("the second call re-minted (%d → %d) — it should have stopped at the"+
			" revoked row", minted, authr.mints.Load())
	}
}

func TestTransientAuthOutageRevokesNothing(t *testing.T) {
	authr := &fakeAuthenticator{err: auth.ErrUnreachable}
	h := newHarness(t, withAuthenticator(authr, 0))
	secret, tok := h.mintToken(memberA, "Claude", nil, time.Time{})

	// The cached identity carries on: the member is not closed, auth is merely
	// unreachable, and the call is served on what home already knows.
	if rr := h.rpc(secret, "tools/list", nil); rr.Code != http.StatusOK {
		t.Fatalf("a call during an auth outage got %d, want 200 — the cached identity"+
			" is what an outage is supposed to fall back to", rr.Code)
	}
	if h.tokenRow(tok.ID).revokedAt != "" {
		t.Fatal("a TRANSIENT auth failure revoked the token. That turns a five-minute" +
			" outage into every household token needing to be re-minted by hand (D287).")
	}

	// And once auth answers again, the fresh roles land.
	authr.err = nil
	authr.id = auth.Identity{Email: "karel@example.test", DisplayName: "Karel", Roles: []string{"reader"}}
	_, result := h.call(secret, "home_whoami", nil)
	if isError(result) {
		t.Fatalf("whoami failed after auth recovered: %s", resultText(t, result))
	}
	if !containsString(splitRoles(resultText(t, result)), "reader") {
		t.Fatalf("the re-minted roles did not take effect: %s", resultText(t, result))
	}
}

// ⚠ THE RE-MINT IS COALESCED PER OWNER (D287's cost half). An MCP client fans out
// parallel calls by design; without coalescing every call in a burst past the
// threshold puts its own Mint on the auth service.
func TestConcurrentCallsCoalesceOneRemint(t *testing.T) {
	// ⚠ THE MINT HOLDS, AND WITHOUT THAT THIS TEST PROVED NOTHING. It used to pass
	// on a different mechanism entirely: with a zero window and an instant Mint,
	// the first call stamped `roles_refreshed_at` and the rest read the fresh stamp
	// and skipped the mint — so it would have stayed green with the singleflight
	// deleted. A Mint that takes long enough for the burst to arrive is what puts
	// the callers in flight together, which is the only state coalescing exists for.
	authr := &fakeAuthenticator{
		id:   auth.Identity{Email: "k@example.test", Roles: []string{"admin"}},
		hold: 50 * time.Millisecond,
	}
	h := newHarness(t, withAuthenticator(authr, 0))
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	const calls = 12
	done := make(chan int, calls)
	for range calls {
		go func() { done <- h.rpc(secret, "ping", nil).Code }()
	}
	for range calls {
		if code := <-done; code != http.StatusOK {
			t.Fatalf("a concurrent call got %d", code)
		}
	}
	// ⚠ NOT `== 1`: every call is past the threshold, so one that arrives after a
	// mint has already returned legitimately mints again. What must not happen is
	// one per call — that is the standing stream of requests coalescing exists to
	// stop.
	if got := authr.mints.Load(); got >= calls {
		t.Fatalf("%d concurrent calls produced %d mints — they were not coalesced at all",
			calls, got)
	}
}
