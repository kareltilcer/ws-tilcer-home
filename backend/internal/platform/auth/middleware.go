package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
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

// refreshIdentity re-mints sess's cached identity when it is past the threshold
// (FR-A2), and is the ONE place the fail-closed decision is taken — shared by the
// request middleware and by RevalidateSession, so an open websocket and an HTTP
// request cannot disagree about whether an account is still open.
//
// It returns the identity the caller should act under — the minted one, or the
// session's cached one when nothing was minted — ErrUserClosed once auth has
// closed the account, and never an error the caller has to map on a transient
// auth outage. `revoked` reports whether the session row was ACTUALLY revoked,
// which is a different question from whether the account is closed — see
// RevalidateSession.
//
// ⚠ THE NAME AND THE EMAIL COME BACK WITH THE ROLES, and that is the point of it
// rather than a detail: the mint returns a whole Identity, and this is the only
// thing in Home that ever asks auth who a member is after they logged in. What
// that is worth, and what is deliberately not done with it, is in
// SessionStore.RefreshIdentity and mergeIdentity.
//
// ⚠ viaConnection says WHO IS ASKING, and it is not decoration: the two callers
// need different cancellation and different announcement, and collapsing them
// onto one policy broke whichever one did not write it.
//
//   - false — an HTTP request. Its context dying means the browser went away and
//     the follow-up writes should die with it, and the revocation must be
//     announced from here because nothing else in that path will.
//   - true — a websocket re-check. Its context is a connection's or a pump's, so
//     the writes are detached (a tab closed mid-mint must not undo a decided
//     revoke), and the announcement is left to the caller, which has an ordering
//     constraint this frame cannot see. See RevalidateSession.
func (c Config) refreshIdentity(ctx context.Context, sess Session, now time.Time, viaConnection bool) (id Identity, revoked bool, err error) {
	if c.Authr == nil || now.Sub(sess.RolesRefreshedAt) <= c.RoleRefresh {
		return sess.identity(), false, nil
	}
	// ⚠ COALESCED PER SESSION. The roles_refreshed_at stamp is what normally
	// prevents a duplicate Mint, but it only works for callers who Lookup AFTER
	// it lands: the revalidation pump's tick and a concurrent HTTP request both
	// past the threshold have both already read the stale stamp, and without
	// this each would put its own Mint on the auth service — every refresh
	// window, precisely for a member active enough to be using the app when the
	// tick fires. The joiner shares the leader's outcome, which is the same
	// answer its own call would have fetched.
	minted, mintErr := mints.do(sess.ID, func() (Identity, error) {
		return c.Authr.Mint(ctx, sess.UserID)
	})
	switch {
	case errors.Is(mintErr, ErrUserClosed):
		// ⚠ From a CONNECTION the revoke runs on a context detached from the
		// caller's: ctx is the connection's, and the member closing their tab
		// mid-mint would otherwise cancel this UPDATE — the row stays live, nothing
		// is announced, and the loud error below fires for what was a normal
		// disconnect. The decision to revoke has already been taken by then and
		// must not be undone by whoever happened to discover it. An HTTP request
		// keeps its own context: there the caller going away IS a reason to stop,
		// and a detached 10s write pins a request goroutine — and the sole SQLite
		// connection — long after the browser has navigated on.
		revokeCtx, cancel := c.writeContext(ctx, viaConnection)
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
			return Identity{}, false, ErrUserClosed
		}
		// ⚠ AND IT DOES NOT FIRE FROM INSIDE A CONNECTION'S OWN RE-CHECK. The hook
		// is ws.Hub.DisconnectSession, and the revalidation pump has to RETIRE its
		// ticker before disconnecting — a socket landing between the two joins a
		// pump that is about to be cancelled and then runs with no recurring check
		// for the rest of its life. Announcing from here put the disconnect two
		// frames INSIDE the check, before the retire could possibly happen. The
		// pump calls DisconnectSession itself, in the right order, once the verdict
		// has come back. An HTTP request has no such ordering and no other
		// announcer, so it still announces here.
		if !viaConnection {
			c.sessionRevoked(sess.ID)
		}
		return Identity{}, true, ErrUserClosed
	case mintErr == nil:
		// ⚠ DETACHED FOR A CONNECTION, FOR THE SAME REASON THE REVOKE ABOVE IS.
		// This write is what makes a mint that already succeeded STICK: it stamps
		// roles_refreshed_at, and every other caller reads that stamp to decide not
		// to mint again. From the websocket ctx is a connection's or a pump's, so a
		// member closing the tab in the window between Mint returning and this
		// UPDATE cancelled it, the error was discarded, and the fresh identity was
		// thrown away — leaving the session to re-mint on the very next tick, which
		// is precisely the repeated-Mint cost one-pump-per-session exists to avoid.
		roleCtx, cancel := c.writeContext(ctx, viaConnection)
		defer cancel()
		fresh := mergeIdentity(sess, minted)
		if err := c.Sessions.RefreshIdentity(roleCtx, sess.ID, fresh, now); err != nil {
			// ⚠ THE MINT SUCCEEDED AND THE STAMP DID NOT, which is the one outcome
			// this whole branch exists to protect and was also the only one with no
			// trace. roles_refreshed_at is what every other caller reads to decide
			// NOT to mint, so without it this session re-mints on its very next
			// revalidation tick and on its next HTTP request, indefinitely — a
			// standing stream of calls to the auth service for one session, which is
			// precisely the cost one-pump-per-session was built to avoid. Discarded,
			// it looked identical to a healthy refresh from every angle.
			//
			// Warn rather than Error, and only when the CALLER is still there: the
			// request goes through on the fresh identity either way, and from a
			// connection ctx a member closing their tab cancels this write for a
			// reason that is not a store problem (the same guard the transient branch
			// below uses).
			if ctx.Err() == nil {
				c.logger().Warn("identity write FAILED after a successful re-mint — this session will "+
					"re-mint on every tick and every request until it lands",
					"user", sess.UserID, "session", sess.ID, "err", err)
			}
		}
		return fresh, false, nil
	default:
		// Transient auth outage: keep the cached identity, retry next request.
		//
		// ⚠ Silent when the CALLER went away. From the websocket revalidation pump
		// ctx is the connection's, so a member closing their tab mid-mint lands
		// here with context.Canceled — a normal disconnect logged as an auth-service
		// problem, on a jittered tick across every session in the household. The
		// detached revoke above exists for the same reason.
		if ctx.Err() == nil {
			c.logger().Warn("role re-mint failed (transient)", "user", sess.UserID, "err", mintErr)
		}
		return sess.identity(), false, nil
	}
}

// mergeIdentity is what a successful mint is allowed to change about a session:
// the roles wholesale, and the email and display name only where the token
// actually carried one.
//
// ⚠ AN EMPTY CLAIM IS READ AS "THIS TOKEN DID NOT SAY", NOT AS "THE MEMBER
// CLEARED IT", and the asymmetry is deliberate. Home cannot tell those two apart
// from one token, and the two mistakes are not the same size: treating an absent
// claim as a clear would blank `display_name` on EVERY session in the household
// within one refresh window if auth's mint token ever stops carrying `name` — and
// blank names are exactly what the directory falls back from, so the whole
// household would go back to being labelled by raw user ids, silently, with no
// deploy of Home to blame it on. The cost of the other mistake is one member who
// erased their name in auth still being shown under it in Home until they log in
// again, which is precisely the behaviour everyone had before this existed.
func mergeIdentity(sess Session, minted Identity) Identity {
	out := sess.identity()
	out.Roles = minted.Roles
	if minted.Email != "" {
		out.Email = minted.Email
	}
	if minted.DisplayName != "" {
		out.DisplayName = minted.DisplayName
	}
	return out
}

// mintFlight coalesces concurrent re-mints of one session: the first caller in
// runs the fetch, everyone who arrives while it is in flight waits for and
// shares that result. Keyed by session id, which is globally unique, so the one
// package-level instance serves every Config without a constructor to hang
// per-process state on (Config is a value copied into the handler and the
// middleware).
type mintFlight struct {
	mu       sync.Mutex
	inflight map[string]*mintCall
}

type mintCall struct {
	done chan struct{}
	id   Identity
	err  error
}

var mints = &mintFlight{inflight: make(map[string]*mintCall)}

func (f *mintFlight) do(key string, fetch func() (Identity, error)) (Identity, error) {
	f.mu.Lock()
	if c, ok := f.inflight[key]; ok {
		f.mu.Unlock()
		<-c.done
		return c.id, c.err
	}
	c := &mintCall{done: make(chan struct{})}
	f.inflight[key] = c
	f.mu.Unlock()
	c.id, c.err = fetch()
	f.mu.Lock()
	delete(f.inflight, key)
	f.mu.Unlock()
	close(c.done)
	return c.id, c.err
}

// writeContext returns the context refreshIdentity's two follow-up writes run on
// — the fail-closed revoke, and the identity write that makes a successful mint
// stick.
//
// Detached (and bounded) only for a websocket caller, whose context is a
// connection's and can die for a reason that has nothing to do with the write.
// An HTTP request keeps its own: cancellation there means the client is gone,
// which is exactly when a write should stop rather than run on for another ten
// seconds holding the one SQLite connection.
func (c Config) writeContext(ctx context.Context, viaConnection bool) (context.Context, context.CancelFunc) {
	if !viaConnection {
		return ctx, func() {}
	}
	return context.WithTimeout(context.WithoutCancel(ctx), detachedWriteTimeout)
}

// detachedWriteTimeout bounds the two writes refreshIdentity runs on a context
// detached from a CONNECTION's — the fail-closed revoke, and the identity write
// that makes a successful mint stick. Long enough to outlast SQLite's busy timeout,
// short enough that a wedged store cannot pin a revalidation tick indefinitely.
const detachedWriteTimeout = 10 * time.Second

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
//
// ⚠ IT DOES NOT ANNOUNCE THE REVOCATION — SessionGone IS THE ANNOUNCEMENT, and
// acting on it is the caller's. OnSessionRevoked is ws.Hub.DisconnectSession, and
// the revalidation pump must retire its ticker BEFORE it disconnects, or a socket
// arriving in between joins a pump about to be cancelled and never gets a
// recurring check again. Firing the hook from inside refreshIdentity put that
// disconnect two frames deep inside the check, where no retire could have
// happened yet. So this returns the verdict and the pump does both, in order.
// (A connect-time check that falls back to this function therefore revokes only
// its own socket; the session's other sockets go on the next tick, which is the
// bound this design already accepts.)
// CheckSession re-takes only the ROW half of the session decision: is rawToken
// still backed by a live session row? It is deliberately WEAKER than
// RevalidateSession — no fail-closed re-mint — which is exactly the shape the
// websocket's connect-time re-check needs: the Mint-capable path must not run
// once per socket (see the composition root's wsCfg.Recheck). A member disabled
// in auth whose row is still live therefore comes back SessionLive here; the
// session's ticker is what discovers the closure.
//
// The verdict vocabulary is shared with RevalidateSession so one pinned bridge
// (the composition root's wsRevalidation) serves both, and an inverted arm here
// is caught by TestCheckSession the same way TestWSRevalidation catches the
// bridge's.
func (c Config) CheckSession(ctx context.Context, rawToken string) (userID string, verdict SessionVerdict) {
	if c.Sessions == nil || rawToken == "" {
		return "", SessionUnknown
	}
	sess, ok, err := c.Sessions.Lookup(ctx, rawToken, c.now())
	switch {
	case err != nil:
		return "", SessionUnknown // could not tell: the caller keeps the socket
	case !ok:
		return "", SessionGone
	}
	return sess.UserID, SessionLive
}

func (c Config) RevalidateSession(ctx context.Context, rawToken string) (userID string, verdict SessionVerdict) {
	if c.Sessions == nil || rawToken == "" {
		return "", SessionUnknown
	}
	now := c.now()
	sess, ok, err := c.Sessions.Lookup(ctx, rawToken, now)
	switch {
	case err != nil:
		// ⚠ Silent when the CALLER went away. ctx here is the websocket
		// connection's, so a member closing their tab while a tick is inside
		// Lookup fails it with context.Canceled — and logging that as "could not
		// reach the store" points an operator at a database problem that is really
		// a closed browser tab, once per tab, forever. The verdict is unchanged:
		// an undecidable check keeps the connection either way.
		if ctx.Err() == nil {
			c.logger().Warn("session revalidation could not reach the store — keeping the connection", "err", err)
		}
		return "", SessionUnknown
	case !ok:
		return "", SessionGone
	}
	_, revoked, mintErr := c.refreshIdentity(ctx, sess, now, true)
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
			//
			// viaConnection is false: the follow-up writes ride r.Context() and die
			// with the request, and the revocation is announced from inside
			// refreshIdentity because no caller further up this path will do it.
			id, _, mintErr := cfg.refreshIdentity(r.Context(), sess, now, false)
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

			// ⚠ THE ACTOR IS BUILT FROM THE REFRESHED IDENTITY, NOT FROM THE ROW THAT
			// WAS LOOKED UP. On the request that re-mints, `sess` is the identity as
			// it was BEFORE the mint — so labelling from it stamps the audit trail of
			// the very request that learned the new name with the old one, and any
			// later request would disagree with it for no reason a reader could see.
			//
			// ⚠ EVERY FIELD FROM `id`, INCLUDING THE ONE THAT CANNOT DIFFER.
			// mergeIdentity seeds its result from sess.identity() and never takes the
			// mint's subject, so id.UserID IS sess.UserID on every path that reaches
			// here — but reading one field from the pre-mint row and two from the
			// post-mint identity makes a reader go and prove that before they can
			// trust the line above. One source, nothing to prove.
			actor := reqctx.Actor{
				UserID: id.UserID,
				Type:   "user",
				Label:  labelForIdentity(id),
				Roles:  id.Roles,
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
