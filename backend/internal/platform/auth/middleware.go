package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Cookie names (host-only on home.tilcer.cz).
const (
	cookieSession = "session"
	cookieCSRF    = "csrf"
)

// touchGranularity bounds how often the middleware writes to slide a session's
// expiry, so the landing route doesn't issue a write on every request.
const touchGranularity = time.Hour

// Config configures the session middleware (and is reused by the /api/auth
// handler). In development BypassActor is set and the session store / auth client
// are nil: every request is authenticated as that actor (the app runs offline).
type Config struct {
	Sessions    *SessionStore
	Authr       Authenticator
	RoleRefresh time.Duration // re-mint threshold (HOME_ROLE_REFRESH_MINUTES)
	SessionTTL  time.Duration // sliding window (HOME_SESSION_TTL_DAYS)
	Secure      bool          // Secure cookie attribute (TLS-only; PRD §8)
	Origins     []string      // CSRF Origin allowlist (HOME_ALLOWED_ORIGINS)
	BypassActor *reqctx.Actor // dev bypass; nil in production
	Now         func() time.Time
	Logger      *slog.Logger
	// OnSessionRevoked, when set, is called with the id of the session that was
	// just revoked — by logout, or by failing closed on a re-mint (v10).
	//
	// ⚠ It exists because a websocket is authenticated once, at upgrade, and then
	// lives as long as the browser keeps it. Revoking the session 401s every HTTP
	// request and does nothing at all to an open socket, which since v10 carries
	// private message bodies. The composition root points this at
	// ws.Hub.DisconnectSession.
	//
	// ⚠ It carries the SESSION id, not the user id. Revocation here is always
	// per-session — RevokeByToken drops the calling device's token, RevokeByID
	// one row — so announcing the user would close that member's sockets on every
	// OTHER device too, whose sessions are untouched and still valid: logging out
	// on a phone would tear down the laptop's live feed.
	OnSessionRevoked func(sessionID string)
}

// sessionRevoked notifies the composition root that sessionID is gone.
func (c Config) sessionRevoked(sessionID string) {
	if c.OnSessionRevoked != nil && sessionID != "" {
		c.OnSessionRevoked(sessionID)
	}
}

// refreshRoles re-mints sess's cached roles when they are past the threshold
// (FR-A2), and is the ONE place the fail-closed decision is taken — shared by the
// request middleware and by RevalidateSession, so an open websocket and an HTTP
// request cannot disagree about whether an account is still open.
//
// It returns ErrUserClosed once auth has closed the account, the cached roles
// unchanged on a transient auth outage, and never an error the caller has to map.
// `revoked` reports whether the session row was ACTUALLY revoked, which is a
// different question from whether the account is closed — see RevalidateSession.
func (c Config) refreshRoles(ctx context.Context, sess Session, now time.Time) (roles []string, revoked bool, err error) {
	if c.Authr == nil || now.Sub(sess.RolesRefreshedAt) <= c.RoleRefresh {
		return sess.Roles, false, nil
	}
	id, mintErr := c.Authr.Mint(ctx, sess.UserID)
	switch {
	case errors.Is(mintErr, ErrUserClosed):
		// ⚠ The revoke runs on a context DETACHED from the caller's. From the
		// websocket revalidation pump ctx is the connection's, and the member
		// closing their tab mid-mint would otherwise cancel this UPDATE: the row
		// stays live, nothing is announced, and the loud error below fires for what
		// was a normal disconnect. The decision to revoke has already been taken by
		// then and must not be undone by whoever happened to discover it.
		revokeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revokeTimeout)
		defer cancel()
		// ⚠ The hook fires ONLY when the revoke actually landed. Announcing a
		// revocation that did not happen closes the member's sockets and lets them
		// reconnect straight onto a session that is still live in the DB — which
		// reads as "handled" everywhere while the leak continues. Left un-announced
		// and logged, the next request (and the next revalidation tick) re-mints
		// and retries, because roles_refreshed_at was not stamped.
		if err := c.Sessions.RevokeByID(revokeCtx, sess.ID); err != nil {
			c.logger().Error("session revoke FAILED for a closed user — the session is still live in the database",
				"user", sess.UserID, "session", sess.ID, "err", err)
			return nil, false, ErrUserClosed
		}
		c.sessionRevoked(sess.ID)
		return nil, true, ErrUserClosed
	case mintErr == nil:
		_ = c.Sessions.RefreshRoles(ctx, sess.ID, id.Roles, now)
		return id.Roles, false, nil
	default:
		// Transient auth outage: keep cached roles, retry next request.
		c.logger().Warn("role re-mint failed (transient)", "user", sess.UserID, "err", mintErr)
		return sess.Roles, false, nil
	}
}

// revokeTimeout bounds the detached fail-closed revoke. Long enough to outlast
// SQLite's busy timeout, short enough that a wedged store cannot pin a request
// goroutine (or a revalidation tick) indefinitely.
const revokeTimeout = 10 * time.Second

// SessionVerdict is the outcome of re-taking the session decision for a
// connection that was authorised once and has been open ever since.
type SessionVerdict int

const (
	// SessionUnknown means the decision could NOT be taken — the store was
	// unreachable, a query lost its race with a writer. It is deliberately not
	// SessionGone: see RevalidateSession.
	SessionUnknown SessionVerdict = iota
	// SessionLive means the session still authorises the connection.
	SessionLive
	// SessionGone means the session is revoked, expired, or belongs to an account
	// auth has closed. The caller must drop the connection.
	SessionGone
)

// RevalidateSession re-takes the session decision from the raw cookie token, for
// a caller holding a connection that was authenticated once at upgrade (v10).
//
// ⚠ It runs the SAME fail-closed re-mint as the request middleware, and that is
// the point of it existing rather than the caller repeating a Lookup. Checking
// only that the session row is live is strictly weaker than what every HTTP
// request does: a member disabled in auth keeps a perfectly valid row until
// something re-mints, and a browser tab that issues no HTTP request never
// triggers one — so a bare row check would let a closed account keep receiving
// targeted payloads for the whole session TTL (90 days by default).
//
// ⚠ THE THREE OUTCOMES ARE DISTINCT ON PURPOSE. A database failure is
// SessionUnknown, never SessionGone. The pool is a single connection
// (db.SetMaxOpenConns(1)) behind a 5s busy timeout, so one long write — an
// import, a migration, a checkpoint — can make a queued Lookup error; collapsing
// that onto "revoked" would close every socket in the household at once and log
// it against sessions nobody revoked.
//
// ⚠ A CLOSED ACCOUNT WHOSE REVOKE DID NOT LAND IS SessionUnknown, NOT SessionGone.
// SessionGone tells the caller to drop the connection, and the upgrade path is a
// bare Lookup: if the row is still live because the UPDATE failed, the browser
// reconnects 800ms later (its backoff resets on open), is ACCEPTED, and is closed
// again on the pump's immediate check — an unbounded loop spending two lookups, a
// mint and another failing write per cycle against a single-connection pool,
// which is exactly the contention that made the revoke fail. Closing buys nothing
// either: the reconnect restores the same feed. So the connection is kept and the
// next tick re-mints and retries, because roles_refreshed_at was not stamped.
func (c Config) RevalidateSession(ctx context.Context, rawToken string) (userID string, verdict SessionVerdict) {
	if c.Sessions == nil || rawToken == "" {
		return "", SessionUnknown
	}
	now := c.now()
	sess, ok, err := c.Sessions.Lookup(ctx, rawToken, now)
	switch {
	case err != nil:
		c.logger().Warn("session revalidation could not reach the store — keeping the connection", "err", err)
		return "", SessionUnknown
	case !ok:
		return "", SessionGone
	}
	_, revoked, mintErr := c.refreshRoles(ctx, sess, now)
	switch {
	case !errors.Is(mintErr, ErrUserClosed):
		return sess.UserID, SessionLive
	case revoked:
		return sess.UserID, SessionGone
	default:
		c.logger().Warn("session revalidation found a closed account whose revoke did not land — "+
			"keeping the connection so it cannot reconnect-loop onto the still-live row",
			"user", sess.UserID, "session", sess.ID)
		return sess.UserID, SessionUnknown
	}
}

func (c Config) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c Config) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

// NewSessionAuth returns middleware that authorizes every request from the home
// session cookie (FR-A2), refreshing roles by re-minting past the threshold and
// failing closed when the user is disabled/deleted in auth. It runs on the gated
// /api group (everything except /api/auth/*). Under the dev bypass it injects a
// fixed actor and skips the session entirely.
func NewSessionAuth(cfg Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.BypassActor != nil {
				next.ServeHTTP(w, r.WithContext(reqctx.WithActor(r.Context(), *cfg.BypassActor)))
				return
			}
			c, err := r.Cookie(cookieSession)
			if err != nil || c.Value == "" {
				httpx.WriteError(w, httpx.ErrUnauthorized("no session"))
				return
			}
			now := cfg.now()
			sess, ok, err := cfg.Sessions.Lookup(r.Context(), c.Value, now)
			if err != nil {
				httpx.WriteError(w, httpx.ErrInternal(""))
				return
			}
			if !ok {
				clearAuthCookies(w, cfg.Secure)
				httpx.WriteError(w, httpx.ErrUnauthorized("invalid or expired session"))
				return
			}

			// The HTTP path fails closed on the account, not on the revoke: a 401 is
			// correct whether or not the row could be marked, and it does not
			// reconnect-loop the way a dropped socket does (see RevalidateSession).
			roles, _, mintErr := cfg.refreshRoles(r.Context(), sess, now)
			if errors.Is(mintErr, ErrUserClosed) {
				// Fail closed: the user was disabled/deleted in auth (FR-A2).
				clearAuthCookies(w, cfg.Secure)
				httpx.WriteError(w, httpx.ErrUnauthorized("session revoked"))
				return
			}

			// Slide the expiry lazily to avoid a write on every request. The DB
			// slide alone is not enough: the browser cookies were issued with a
			// fixed MaxAge at login, so re-issue them here with a fresh lifetime or
			// the session/csrf cookies would still expire at login+SessionTTL
			// regardless of activity (FR-A2). Runs before next.ServeHTTP so the
			// Set-Cookie headers are written before the body.
			if now.Sub(sess.LastSeenAt) > touchGranularity {
				if err := cfg.Sessions.Touch(r.Context(), sess.ID, now, now.Add(cfg.SessionTTL)); err == nil {
					setSessionCookie(w, c.Value, cfg.SessionTTL, cfg.Secure)
					if csrf, err := r.Cookie(cookieCSRF); err == nil && csrf.Value != "" {
						setCSRFCookie(w, csrf.Value, cfg.SessionTTL, cfg.Secure)
					}
				}
			}

			actor := reqctx.Actor{
				UserID: sess.UserID,
				Type:   "user",
				Label:  labelFor(sess),
				Roles:  roles,
			}
			next.ServeHTTP(w, r.WithContext(reqctx.WithActor(r.Context(), actor)))
		})
	}
}

// NewCSRF returns double-submit CSRF middleware (FR-A5): every cookie-authenticated
// state-changing request must carry an X-CSRF-Token header equal to the csrf
// cookie AND an Origin/Referer within the allowlist. Safe methods pass through.
// Under the dev bypass it is a no-op (there is no session or csrf cookie in dev).
func NewCSRF(origins []string, bypass bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if bypass || safeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			if !originAllowed(r, origins) {
				httpx.WriteError(w, httpx.ErrForbidden("origin not allowed"))
				return
			}
			header := r.Header.Get("X-CSRF-Token")
			cookie, err := r.Cookie(cookieCSRF)
			if header == "" || err != nil || cookie.Value == "" || header != cookie.Value {
				httpx.WriteError(w, httpx.ErrForbidden("csrf token mismatch"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func safeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// originAllowed checks the request's Origin (or, absent that, Referer) host
// against the allowlist. Entries may be exact origins ("https://home.tilcer.cz")
// or wildcards ("https://*.tilcer.cz").
func originAllowed(r *http.Request, allowed []string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		if ref := r.Header.Get("Referer"); ref != "" {
			if u, err := url.Parse(ref); err == nil {
				origin = u.Scheme + "://" + u.Host
			}
		}
	}
	if origin == "" {
		return false // cannot verify a cookie-authenticated mutation
	}
	for _, a := range allowed {
		if originMatches(origin, a) {
			return true
		}
	}
	return false
}

func originMatches(origin, pattern string) bool {
	if origin == pattern {
		return true
	}
	// Wildcard "<scheme>://*.<domain>" matches any single-level subdomain.
	scheme, host, ok := splitOrigin(origin)
	pScheme, pHost, ok2 := splitOrigin(pattern)
	if !ok || !ok2 || scheme != pScheme {
		return false
	}
	if strings.HasPrefix(pHost, "*.") {
		suffix := pHost[1:] // ".tilcer.cz"
		return strings.HasSuffix(host, suffix) && host != suffix[1:]
	}
	return false
}

func splitOrigin(o string) (scheme, host string, ok bool) {
	i := strings.Index(o, "://")
	if i < 0 {
		return "", "", false
	}
	return o[:i], o[i+3:], true
}

func labelFor(s Session) string {
	if s.DisplayName != "" {
		return s.DisplayName
	}
	return s.Email
}

// setAuthCookies sets the session (HttpOnly) and csrf (JS-readable) cookies,
// host-only on home.tilcer.cz (no Domain), Secure + SameSite=Lax (PRD §8, D29).
func setAuthCookies(w http.ResponseWriter, sessionToken, csrfToken string, ttl time.Duration, secure bool) {
	setSessionCookie(w, sessionToken, ttl, secure)
	setCSRFCookie(w, csrfToken, ttl, secure)
}

func setSessionCookie(w http.ResponseWriter, token string, ttl time.Duration, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieSession, Value: token, Path: "/",
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: int(ttl.Seconds()),
	})
}

func setCSRFCookie(w http.ResponseWriter, token string, ttl time.Duration, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieCSRF, Value: token, Path: "/",
		HttpOnly: false, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: int(ttl.Seconds()),
	})
}

func clearAuthCookies(w http.ResponseWriter, secure bool) {
	for _, name := range []string{cookieSession, cookieCSRF} {
		http.SetCookie(w, &http.Cookie{
			Name: name, Value: "", Path: "/",
			HttpOnly: name == cookieSession, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1,
		})
	}
}

// newCSRFToken mints a random token for the csrf cookie (double-submit).
func newCSRFToken() (string, error) {
	raw, _, err := newToken()
	return raw, err
}
