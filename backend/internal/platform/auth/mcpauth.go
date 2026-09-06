package auth

import (
	"context"
	"errors"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Bearer resolution for the MCP front door (v11, PRD §V11-3, D280/D287,
// HANDOFF-13 §3.2–§3.4).
//
// ⚠ THE RULE, ONCE: a token is a SECOND CREDENTIAL FOR THE SAME IDENTITY. It
// carries the member's real id and the member's live roles, re-minted on the same
// HOME_ROLE_REFRESH_MINUTES threshold as a session, and it is refused everything
// the member would be refused. `Actor.Type` therefore stays "user" (D291), which
// is what lets every ownership check, membership check and `created_by` stamp in
// eleven modules keep working with no change at all.
//
// ⚠ THE SESSION COOKIE IS NOT READ ON THIS PATH AT ALL — not even as a fallback
// (D280). `/mcp` sits outside the `/api` group and therefore outside the CSRF
// double-submit check, so a cookie accepted here would make it the only
// cookie-authenticated, CSRF-unprotected endpoint in Home: drivable cross-origin
// by any website a member visits. Nothing in this file takes an *http.Request.

// MCPPrincipal is who a bearer resolved to.
type MCPPrincipal struct {
	Token    MCPToken
	Identity Identity
}

// Actor projects the principal onto the request actor eleven modules already
// understand.
//
// ⚠ Label is "<display name> · <token name>" (D291) and the display name falls
// back to the email's local part, never to the raw user id: the Log already
// renders COALESCE(actor_user_id, actor_label, actor_type), so a label containing
// a uuid would print a uuid twice on one row.
func (p MCPPrincipal) Actor() reqctx.Actor {
	name := p.Identity.DisplayName
	if name == "" {
		name = localPart(p.Identity.Email)
	}
	label := p.Token.Name
	if name != "" {
		label = name + " · " + p.Token.Name
	}
	return reqctx.Actor{
		UserID: p.Identity.UserID,
		Type:   "user",
		Label:  label,
		Roles:  p.Identity.Roles,
	}
}

func localPart(email string) string {
	for i := 0; i < len(email); i++ {
		if email[i] == '@' {
			return email[:i]
		}
	}
	return email
}

// MCPAuth resolves an MCP bearer to a principal. It is a value, built once at
// composition and copied freely, exactly as Config is.
type MCPAuth struct {
	Cfg    Config
	Tokens *MCPTokenStore
}

// Resolve turns a raw bearer secret into the member it acts as.
//
// ⚠ EVERY FAILURE RETURNS ErrTokenNotFound, and the caller turns all of them into
// ONE 401. Unknown, revoked, expired and owner-closed must be indistinguishable
// from outside: which of the four it was is precisely what somebody holding a
// guessed or stale token would like to learn.
//
// The order is the order in HANDOFF-13 §3.2 and each step is cheap before the one
// after it: one indexed seek on the hash, two column checks, then the re-mint,
// which is the only step that can touch the network.
func (m MCPAuth) Resolve(ctx context.Context, secret, ip string) (MCPPrincipal, error) {
	if m.Tokens == nil || secret == "" {
		return MCPPrincipal{}, ErrTokenNotFound
	}
	now := m.Cfg.now()

	tok, err := m.Tokens.LookupBySecret(ctx, secret)
	if err != nil {
		return MCPPrincipal{}, err // ErrTokenNotFound, or a real store failure
	}
	if !tok.Live(now) {
		return MCPPrincipal{}, ErrTokenNotFound
	}

	id, err := m.identity(ctx, tok, now)
	if err != nil {
		if errors.Is(err, ErrUserClosed) {
			// ⚠ THE ONE PLACE THE TOKEN PATH DIFFERS FROM THE SESSION PATH: there is
			// no session to revoke and no socket to tear down (contrast v10's
			// OnSessionRevoked → hub.DisconnectSession), so the TOKEN ROW is stamped.
			// The next call is then a clean 401 against a revoked token rather than a
			// second re-mint against a closed account.
			if rerr := m.Tokens.RevokeByID(ctx, tok.ID, now); rerr != nil {
				m.Cfg.logger().Error("mcp token revoke FAILED for a closed user — the token is still live in the database",
					"user", tok.UserID, "token", tok.ID, "err", rerr)
			}
		}
		return MCPPrincipal{}, ErrTokenNotFound
	}

	m.touch(ctx, tok, now, ip)
	return MCPPrincipal{Token: tok, Identity: id}, nil
}

// identity answers who the token's owner is right now.
//
// ⚠ IT READS THE SAME CACHE THE SESSION PATH READS, and that is deliberate rather
// than convenient. Home has no user table — `sessions` is the only record it keeps
// of who a member is, which is why SessionStore.RefreshIdentity writes the whole
// identity and not only the roles. Giving the token path its own cache would give
// one member two answers to "what are your roles", and the two would disagree for
// up to one refresh window every time either side re-minted. Reading the newest
// session row instead means a re-mint from EITHER door serves both.
//
// ⚠ A REVOKED OR EXPIRED SESSION ROW IS STILL READ, and must be: logging out on a
// laptop is not a statement about a token, and the alternative — treating a member
// with no live session as unauthenticated — would make a token stop working the
// moment its owner signed out of the browser, which is the opposite of the point.
// What a revoked row cannot do is skip the freshness check below.
func (m MCPAuth) identity(ctx context.Context, tok MCPToken, now time.Time) (Identity, error) {
	cached, refreshedAt, sessionID, found, err := m.cachedIdentity(ctx, tok.UserID)
	if err != nil {
		return Identity{}, err
	}
	// The dev bypass runs with no auth service and no session store; the fake
	// actor's roles are the only answer there is, and refusing here would make the
	// whole MCP surface unusable offline.
	if m.Cfg.Authr == nil {
		if m.Cfg.BypassActor != nil {
			return Identity{
				UserID: tok.UserID,
				Roles:  append([]string(nil), m.Cfg.BypassActor.Roles...),
				Email:  m.Cfg.BypassActor.Label,
			}, nil
		}
		if !found {
			return Identity{}, ErrUserClosed
		}
		return cached, nil
	}
	if found && now.Sub(refreshedAt) <= m.Cfg.RoleRefresh {
		return cached, nil
	}

	// ⚠ COALESCED PER TOKEN OWNER, for the reason the session path coalesces per
	// session: an MCP client fans out parallel tool calls by design, and without
	// this every call in a burst past the threshold would put its own Mint on the
	// auth service. The key is namespaced so it cannot collide with a session id.
	minted, mintErr := mints.do("mcp-user:"+tok.UserID, func() (Identity, error) {
		return m.Cfg.Authr.Mint(ctx, tok.UserID)
	})
	switch classifyMint(mintErr) {
	case mintClosed:
		return Identity{}, ErrUserClosed
	case mintFresh:
		fresh := minted
		fresh.UserID = tok.UserID
		if found {
			fresh = mergeMintedInto(cached, minted)
			// Writing the refreshed identity back into the session row is what makes
			// this mint STICK — every other caller, session middleware included, reads
			// roles_refreshed_at to decide not to mint again. Warned rather than
			// errored: the call goes through on the fresh identity either way.
			if werr := m.Cfg.Sessions.RefreshIdentity(ctx, sessionID, fresh, now); werr != nil && ctx.Err() == nil {
				m.Cfg.logger().Warn("identity write FAILED after a successful re-mint on the MCP path — "+
					"this member will re-mint on every call until it lands",
					"user", tok.UserID, "session", sessionID, "err", werr)
			}
		}
		return fresh, nil
	default:
		// ⚠ A TRANSIENT AUTH OUTAGE MUST NOT REVOKE ANYTHING. Getting this wrong
		// turns a five-minute outage into every household token needing to be
		// re-minted by hand. With no cached identity there is nothing to fall back
		// on, so the call is refused — refused, not revoked.
		if ctx.Err() == nil {
			m.Cfg.logger().Warn("role re-mint failed on the MCP path (transient)", "user", tok.UserID, "err", mintErr)
		}
		if !found {
			return Identity{}, ErrUnreachable
		}
		return cached, nil
	}
}

// cachedIdentity reads the member's freshest identity projection out of
// `sessions`. found is false when the member has never had a session — which,
// for a token owner, means the sessions table came from a restore predating
// their login, since minting a token requires one.
func (m MCPAuth) cachedIdentity(ctx context.Context, userID string) (Identity, time.Time, string, bool, error) {
	if m.Cfg.Sessions == nil {
		return Identity{}, time.Time{}, "", false, nil
	}
	return m.Cfg.Sessions.NewestIdentity(ctx, userID)
}

// mergeMintedInto is mergeIdentity's shape for a caller that holds an identity
// rather than a Session: roles wholesale, email and display name only where the
// token actually carried one. ⚠ An empty claim is read as "this token did not
// say", never as "the member cleared it" — the same asymmetry, for the same
// reason, and deliberately not a second rule.
func mergeMintedInto(cached, minted Identity) Identity {
	out := cached
	out.Roles = minted.Roles
	out.Email = firstNonEmpty(minted.Email, cached.Email)
	out.DisplayName = firstNonEmpty(minted.DisplayName, cached.DisplayName)
	return out
}

// touch records the call against the token, at most once per hour.
//
// ⚠ touchGranularity is the SESSION constant, reused rather than restated (D286):
// the two are the same trade — a lazy write against a pool of one connection —
// and a second number here would drift. Failure is logged at debug level by being
// ignored: a token whose last-used stamp did not land still authenticated a call,
// and failing the call over a bookkeeping write would be worse than the stale
// column it prevents.
func (m MCPAuth) touch(ctx context.Context, tok MCPToken, now time.Time, ip string) {
	if !tok.LastUsedAt.IsZero() && now.Sub(tok.LastUsedAt) <= touchGranularity {
		return
	}
	if err := m.Tokens.Touch(ctx, tok.ID, now, ip); err != nil && ctx.Err() == nil {
		m.Cfg.logger().Warn("mcp token last-used write failed", "token", tok.ID, "err", err)
	}
}
