package ws

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Config configures the /ws handler's authentication. In Mode B the websocket is
// authorized from the home SESSION COOKIE (the browser sends it automatically on
// a same-origin upgrade — there is no bearer token). Authenticate resolves the
// request to an actor; BypassActor (dev only) short-circuits it.
type Config struct {
	// Authenticate returns the connecting actor plus the session handle the
	// revalidation pump re-takes the decision with, or ok=false to reject. Built
	// in the composition root over the session store. nil under the dev bypass.
	Authenticate func(r *http.Request) (Upgrade, bool)
	BypassActor  *reqctx.Actor
	// Revalidate re-takes the session decision for an already-open socket, from
	// the opaque token Authenticate handed back at upgrade. nil disables the pump.
	//
	// ⚠ It takes a TOKEN, not the upgrade request. The pump lives as long as the
	// connection, and the upgrade request is hijacked the moment the socket is
	// accepted: retaining a clone of it kept a second http.Request — headers, URL,
	// TLS state, a handle on the hijacked body — alive per connection for days,
	// and handed that dead request to a callback the composition root is free to
	// grow (r.Body, ParseForm, RemoteAddr) with nothing to signal it had gone
	// stale.
	//
	// ⚠ It must be able to say "I could not tell" — see Revalidation.
	Revalidate RevalidateFunc
	// Recheck is the CONNECT-TIME check, taken once per socket right after it is
	// registered. nil falls back to Revalidate.
	//
	// ⚠ It exists so the connect-time check is not the expensive one. The hole it
	// closes is narrow and cheap: the upgrade decision and h.add are not one
	// atomic step, so a DisconnectSession sweeping bySession in between misses
	// this socket entirely. Re-reading the session row is enough to see that.
	// Running the full Revalidate there instead cost a second Lookup AND, when
	// roles were stale, an outbound Mint PER SOCKET — so a deploy, which drops
	// every socket in the household and has every tab redial at once, put N
	// concurrent Mints on the auth service and 2N lookups on a pool of exactly
	// one connection, none of them seeing another's roles_refreshed_at stamp.
	// The fail-closed re-mint belongs on the session's ticker, where it happens
	// once per session per interval.
	Recheck RevalidateFunc
	// RevalidateEvery is how often an already-open socket re-takes that decision
	// (v10). Zero means defaultRevalidateEvery. See runSessionPump.
	RevalidateEvery time.Duration
}

// RevalidateFunc re-takes the session decision from the opaque token the upgrade
// handed back. Named so the hub can pass it around without repeating the
// signature at every seam.
type RevalidateFunc func(ctx context.Context, token string) (userID string, verdict Revalidation)

// Upgrade is what the upgrade decision yields: the actor the connection belongs
// to, plus the session that authorised it.
//
// ⚠ SessionID is what a revocation is keyed by (Hub.DisconnectSession) and Token
// is the only thing Revalidate consumes — both carried as opaque strings so that
// nothing downstream of the upgrade has to hold on to the request they came from.
type Upgrade struct {
	Actor     reqctx.Actor
	SessionID string
	Token     string
}

// Revalidation is a re-check's verdict for an already-open socket.
type Revalidation int

const (
	// RevalidationUnknown means the decision could NOT be taken. The socket is
	// KEPT, and this is the zero value so a caller who forgets to answer errs
	// towards keeping live boards up rather than towards tearing them down.
	//
	// ⚠ A database hiccup is not a revocation. The pool is a single connection
	// (db.SetMaxOpenConns(1)) behind a 5s busy timeout, so treating "the query
	// failed" as "the session is gone" would close every socket in the household
	// over one contended write, and log it against sessions nobody revoked.
	RevalidationUnknown Revalidation = iota
	// RevalidationValid means the session still holds. The user id comes back with
	// it, because a session that now resolves to a DIFFERENT member has to close
	// the socket too.
	RevalidationValid
	// RevalidationGone means the session is revoked, expired, or belongs to an
	// account auth has closed. Close the socket.
	RevalidationGone
)

// defaultRevalidateEvery bounds how long a socket may outlive the session that
// opened it when nothing announces the revocation (a TTL that simply expires, a
// row revoked out of band). Auth's own revocation paths call
// Hub.DisconnectSession and do not wait for this tick. Configurable through
// HOME_WS_REVALIDATE_MINUTES.
const defaultRevalidateEvery = 5 * time.Minute

// minRevalidateEvery is the floor a configured tick is held to.
//
// ⚠ It is not tidiness: runSessionPump re-arms a timer with jitter(every), and
// jitter has to return an interval too small to halve UNCHANGED — a spread of
// zero cannot be drawn from. So a nanosecond or microsecond `every` becomes a
// timer that re-fires as fast as the scheduler allows, issuing a Lookup — and
// past the threshold a Mint — per iteration against a pool of exactly one
// connection, for every connected session, with nothing anywhere reporting an
// error. Ten milliseconds is orders of magnitude below any real setting (the
// env var floor is a minute) and still leaves jitter a spread to draw from,
// while a value below it can only be a unit slip.
const minRevalidateEvery = 10 * time.Millisecond

// revalidateInterval resolves the configured tick, falling back to the default
// for a zero or negative one and refusing to go below minRevalidateEvery.
//
// ⚠ Extracted from Handler so the fallback can be tested. The only thing keeping
// a 0 out of here is a range check that lives in another package
// (HOME_WS_REVALIDATE_MINUTES), so the substitution has to be pinned on this side
// of that boundary too — and RevalidateEvery is an exported field, so a caller
// that never goes through config.Load (a unit slip of time.Millisecond for
// time.Minute, a test harness) reaches this with no range check at all.
func revalidateInterval(every time.Duration) time.Duration {
	switch {
	case every <= 0:
		return defaultRevalidateEvery
	case every < minRevalidateEvery:
		return minRevalidateEvery
	}
	return every
}

// Handler returns the session-authenticated /ws upgrade handler. Reads are open
// to any authenticated user, so connecting only requires a valid session (no role
// gate here).
func (h *Hub) Handler(cfg Config) http.HandlerFunc {
	logger := h.logger
	revalidateEvery := revalidateInterval(cfg.RevalidateEvery)
	// The connect-time check falls back to the recurring one when no cheaper
	// seam is configured, so a Config that predates Recheck behaves as before.
	recheck := cfg.Recheck
	if recheck == nil {
		recheck = cfg.Revalidate
	}
	return func(w http.ResponseWriter, r *http.Request) {
		// ⚠ v10: the actor resolved here is KEPT (D232). Until v10 it was resolved
		// purely to decide accept-or-reject and then discarded, which is what made
		// the hub anonymous and every fan-out a broadcast. `chat` publishes message
		// bodies to a member set, so the connection has to know who it belongs to.
		var userID, sessionID, token string
		if cfg.BypassActor == nil {
			if cfg.Authenticate == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			up, ok := cfg.Authenticate(r)
			if !ok {
				http.Error(w, "invalid or missing session", http.StatusUnauthorized)
				return
			}
			actor := up.Actor
			userID, sessionID, token = actor.UserID, up.SessionID, up.Token
			if userID == "" {
				// ⚠ AUTHENTICATED BUT UNIDENTIFIED — a bug state, not a policy. The
				// connection still works in every observable way (Publish reaches it,
				// Count is right, boards stay live) and PublishTo silently misses it
				// forever. reqctx.Actor documents UserID as "" for system/service, so
				// this is reachable the day a non-user principal opens a socket. It is
				// logged rather than rejected because refusing the upgrade would take
				// out live boards over a targeting problem.
				logger.Warn("ws: authenticated actor has no user id — the connection is "+
					"broadcast-only and no targeted push will ever reach it",
					"actor_type", actor.Type, "actor_label", actor.Label)
			}
			// ⚠ AUTHENTICATED BUT UNREVOCABLE — the same class of bug state, and the
			// more dangerous one. Without a session id the connection is invisible to
			// DisconnectSession (indexAdd skips the empty key); without a token no
			// revalidation pump is started below. Either way the socket keeps a
			// member-restricted feed for its whole lifetime with both revocation
			// mechanisms disabled, and the only other trace is a "ws connected" line
			// with an empty field nobody is looking for. Logged rather than rejected
			// for the same reason as above: an upgrade refused here takes out live
			// boards over a revocation-plumbing problem.
			if sessionID == "" || token == "" {
				logger.Warn("ws: authenticated connection cannot be revoked — Authenticate "+
					"returned no session id and/or no token, so DisconnectSession cannot "+
					"reach it and no revalidation pump will run; keeping it broadcast-only",
					"user", userID, "has_session_id", sessionID != "", "has_token", token != "")
				// ⚠ AND IT IS DEGRADED TO BROADCAST-ONLY, exactly as an actor with no
				// user id is. Logging alone left the worse of the two bug states as the
				// only one that can leak: the socket stayed in byUser, so PublishTo went
				// on handing it member-restricted payloads after a logout, after the TTL
				// expired, after auth closed the account — for the whole life of the
				// connection, with Count, the boards and every other signal healthy.
				// Dropping the targeting removes the only half that can leak; the
				// broadcast half is why the upgrade is still not refused.
				userID = ""
			}
		} else {
			// ⚠ Under HOME_DEV_AUTH_BYPASS there is no session and Authenticate is
			// never called, so EVERY connection — a second browser, a phone on the
			// same LAN — registers under the one configured id and shares its targeted
			// feed. HOME_DEV_ACTOR_ID defaults to "dev-user" and a blank value falls
			// back to that default (config.go), so this is what the bypass always
			// does; there is no anonymous variant to rely on. That is the price of
			// fake authentication, and it is why the bypass is refused in production.
			userID = cfg.BypassActor.UserID
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return // Accept already wrote the response
		}

		ctx, cancel := context.WithCancel(context.Background())
		// ⚠ THE READ PUMP GETS ITS OWN CONTEXT, and it must not be this one. A
		// coder/websocket Read whose context is cancelled ABORTS the connection
		// rather than closing it, so a read pump sharing ctx tears the socket down
		// the instant anything cancels — before the close below can put a status on
		// the wire, leaving every peer with an abnormal closure and no verdict.
		// Read stays blocked until conn.Close completes instead, which the library
		// documents as unblocking every goroutine on the connection; cancelRead is
		// the backstop if that ever fails to.
		readCtx, cancelRead := context.WithCancel(context.Background())
		defer cancelRead()
		c := &client{conn: conn, send: make(chan []byte, sendBuffer), cancel: cancel, userID: userID, sessionID: sessionID}
		h.add(c)
		// ⚠ Deferred AS WELL AS called explicitly below. h.add files this client in
		// THREE maps and only the ordinary path unwinds through the explicit
		// remove, so a panic in the write pump — or a return added between add and
		// writePump one day — would otherwise leave a dead *client in byUser and
		// bySession for the process's lifetime, holding a 32-slot channel and a
		// cancel func, and found by every later DisconnectSession for that session.
		// remove is idempotent, so the second call costs nothing.
		defer h.remove(c)
		// The id is logged on both ends: "did that member's socket register, and
		// under what?" is the first question a missing targeted push raises, and
		// byUser is otherwise reachable only from a test.
		logger.Info("ws connected", "user", userID, "session", sessionID, "clients", h.Count())

		go h.readPump(readCtx, c)
		if cfg.Revalidate != nil && token != "" {
			// ⚠ TWO CHECKS AT TWO GRANULARITIES, and each is the right one for the
			// hole it closes.
			//
			// The IMMEDIATE check is per SOCKET, because the race it closes is: the
			// upgrade decision and h.add are not one atomic step, so a revocation
			// sweeping bySession in between misses this client entirely — it was not
			// indexed yet — and it would otherwise hold an already-revoked session
			// until the first tick, minutes later. It goes through Recheck, which is
			// the CHEAP half of the decision: see Config.Recheck for why the
			// Mint-capable one must not run once per socket.
			go func() {
				if !revalidateOnce(ctx, recheck, token, userID, sessionID, logger) {
					c.revoke()
				}
			}()
			// The RECURRING check is per SESSION, shared by every tab of it. See
			// sessionPump. A connection with no session id gets none — it is
			// unrevocable either way, which is what the warning above says.
			if sessionID != "" {
				p := h.startSessionPump(sessionID, userID, token, cfg.Revalidate, revalidateEvery)
				defer h.releaseSessionPump(p)
			}
		}
		h.writePump(ctx, c)

		h.remove(c)
		cancel()
		// ⚠ A REVOCATION IS NOT A RESTART, and the close code is the only place
		// that distinction survives to the browser. Closed as StatusNormalClosure,
		// a socket dropped because its session is gone is indistinguishable from a
		// deploy, so the client reconnects — onto an upgrade that will 401 for the
		// rest of the tab's life, once every backoff cap, forever. The policy code
		// is what lets it stop and send the member to the login screen instead.
		status, reason := websocket.StatusNormalClosure, ""
		if c.revoked.Load() {
			status, reason = websocket.StatusPolicyViolation, "session revoked"
		}
		_ = conn.Close(status, reason)
		logger.Info("ws disconnected", "user", userID, "session", sessionID, "clients", h.Count())
	}
}

// runSessionPump re-takes one SESSION's decision periodically and closes every
// socket it authorised when it no longer holds.
//
// ⚠ Without it a socket is authenticated exactly once and then trusted forever.
// That cost nothing while every fan-out was a household-wide broadcast; with
// PublishTo carrying message bodies (D233), a session that has expired or been
// revoked would keep receiving private content over a connection nobody can see —
// 401 on every HTTP request, still live on the socket. Auth's own revocations
// call Hub.DisconnectSession immediately; this is the backstop for the ones
// nothing announces.
//
// ⚠ It does NOT check immediately. The connect-time race is per socket, so the
// handler runs that check itself, once, for each connection; this loop is only
// the recurring half, and a second tab joining an existing session must not reset
// its neighbours' schedule.
func (h *Hub) runSessionPump(ctx context.Context, p *sessionPump, revalidate RevalidateFunc, every time.Duration) {
	// The session, the member it opened as and the token to re-check with are the
	// pump's identity, not incidental parameters — every socket sharing this
	// ticker agreed on all three. See pumpKey.
	sessionID, openedAs, token := p.key.sessionID, p.key.openedAs, p.key.token
	// ⚠ The interval is JITTERED. Every session a page load opens would otherwise
	// tick in phase for as long as it lives, and each tick is a query against a
	// pool of exactly one connection, so the household's sessions would queue
	// their re-checks ahead of user-facing requests in one burst every interval.
	t := time.NewTimer(jitter(every))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !revalidateOnce(ctx, revalidate, token, openedAs, sessionID, h.logger) {
				// ⚠ RETIRE BEFORE DISCONNECTING, not after. Retiring second left a
				// window in which a socket that connected after DisconnectSession
				// snapshotted bySession still found this pump registered, joined it,
				// and was then left running against a ticker retireSessionPump was
				// about to cancel — no recurring check for the rest of its life, and a
				// releaseSessionPump that silently no-ops because the entry is gone.
				// Retiring first means such a socket starts a LIVE pump of its own.
				h.retireSessionPump(p)
				// Every socket of this session goes.
				h.DisconnectSession(sessionID)
				return
			}
			t.Reset(jitter(every))
		}
	}
}

// revalidateOnce re-takes the decision once and reports whether the connections
// it covers live on. It closes nothing itself: the caller decides whether that
// means one socket (the connect-time check) or a whole session (the pump).
//
// ⚠ ONLY RevalidationGone CLOSES. "I could not tell" keeps the connection: see
// RevalidationUnknown.
func revalidateOnce(ctx context.Context, revalidate RevalidateFunc, token, openedAs, sessionID string, logger *slog.Logger) bool {
	userID, verdict := revalidate(ctx, token)
	switch {
	case verdict == RevalidationGone:
		logger.Info("ws: session revoked or expired — closing the socket",
			"user", openedAs, "session", sessionID)
	case verdict == RevalidationValid && openedAs != "" && userID != openedAs:
		// A CHANGED id matters as much as a rejected one: the socket is indexed
		// under the id it opened with, and would go on receiving that user's
		// audience.
		//
		// ⚠ NO PRODUCTION Revalidate CAN REACH THIS TODAY, and it is here as a
		// property of the interface rather than a live scenario. The composition
		// root resolves the id from the session row the raw token hashes to, and
		// nothing updates sessions.user_id in place: a second member signing in on
		// the same browser gets a NEW row and a NEW token, so this socket's token
		// either finds its original row (same id) or finds nothing (Gone). The
		// branch exists so that a Revalidate whose id CAN move — a future store, a
		// different auth mode — cannot silently leave a socket indexed under the
		// wrong member. Do not read its presence as evidence the case occurs.
		//
		// ⚠ AN EMPTY openedAs IS NOT A CHANGED ID, and the guard is load-bearing.
		// The upgrade handler deliberately KEEPS a connection whose actor carries
		// no user id — reqctx.Actor documents "" for system/service principals — as
		// broadcast-only, because refusing it would take out live boards over a
		// targeting problem. Without this clause every such connection is closed by
		// its own first check, milliseconds after the 101: the client's backoff has
		// already been reset by `open`, the upgrade path never looks at the id, so
		// it is re-accepted and re-closed forever. There is nothing to protect
		// there anyway — a client that is in no byUser set receives no targeted
		// payload to leak.
		logger.Warn("ws: session now resolves to a different member — closing the socket",
			"opened_as", openedAs, "now", userID, "session", sessionID)
	default:
		return true
	}
	return false
}

// jitter spreads the pumps out, returning a duration in [every*3/4, every*5/4).
func jitter(every time.Duration) time.Duration {
	spread := int64(every / 2)
	if spread <= 0 {
		return every
	}
	return every*3/4 + time.Duration(rand.Int64N(spread))
}

// readPump drains inbound frames (the client isn't expected to send anything)
// so we notice a closed connection; any read error cancels the connection.
func (h *Hub) readPump(ctx context.Context, c *client) {
	for {
		if _, _, err := c.conn.Read(ctx); err != nil {
			c.cancel()
			return
		}
	}
}

// writePump delivers queued broadcasts until the connection is cancelled.
//
// ⚠ THE WRITE DEADLINE IS NOT DERIVED FROM ctx, for the same reason the read
// pump has a context of its own. coder/websocket arms a context.AfterFunc for
// the duration of every Write that tears the raw connection down — no close
// frame, no status — the moment that context is cancelled. Derived from ctx, a
// revocation landing while a Write is in flight therefore ABORTS the socket
// before the handler can put StatusPolicyViolation on the wire, and the browser
// sees an abnormal closure and reconnects into an upgrade that 401s for the rest
// of the tab's life. The window is not hypothetical: a backgrounded phone on a
// bad network keeps a Write blocked for the whole 5s, and a stale client is
// exactly what a revocation is aimed at. Cancellation is observed by the select
// below instead, so a revoked socket waits out at most one in-flight write.
func (h *Hub) writePump(ctx context.Context, c *client) {
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-c.send:
			wctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := c.conn.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				c.cancel()
				return
			}
		}
	}
}
