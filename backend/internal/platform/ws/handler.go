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
	Revalidate func(ctx context.Context, token string) (userID string, verdict Revalidation)
	// RevalidateEvery is how often an already-open socket re-takes that decision
	// (v10). Zero means defaultRevalidateEvery. See revalidatePump.
	RevalidateEvery time.Duration
}

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

// Handler returns the session-authenticated /ws upgrade handler. Reads are open
// to any authenticated user, so connecting only requires a valid session (no role
// gate here).
func (h *Hub) Handler(cfg Config) http.HandlerFunc {
	logger := h.logger
	revalidateEvery := cfg.RevalidateEvery
	if revalidateEvery <= 0 {
		revalidateEvery = defaultRevalidateEvery
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
		c := &client{conn: conn, send: make(chan []byte, sendBuffer), cancel: cancel, userID: userID, sessionID: sessionID}
		h.add(c)
		// The id is logged on both ends: "did that member's socket register, and
		// under what?" is the first question a missing targeted push raises, and
		// byUser is otherwise reachable only from a test.
		logger.Info("ws connected", "user", userID, "session", sessionID, "clients", h.Count())

		go h.readPump(ctx, c)
		if cfg.Revalidate != nil && token != "" {
			go revalidatePump(ctx, c, cfg.Revalidate, token, revalidateEvery, logger)
		}
		h.writePump(ctx, c)

		h.remove(c)
		cancel()
		_ = conn.Close(websocket.StatusNormalClosure, "")
		logger.Info("ws disconnected", "user", userID, "session", sessionID, "clients", h.Count())
	}
}

// revalidatePump re-takes the session decision periodically and closes the socket
// when it no longer holds.
//
// ⚠ Without it a socket is authenticated exactly once and then trusted forever.
// That cost nothing while every fan-out was a household-wide broadcast; with
// PublishTo carrying message bodies (D233), a session that has expired or been
// revoked would keep receiving private content over a connection nobody can see —
// 401 on every HTTP request, still live on the socket. Auth's own revocations
// call Hub.DisconnectSession immediately; this is the backstop for the ones
// nothing announces.
func revalidatePump(ctx context.Context, c *client, revalidate func(context.Context, string) (string, Revalidation), token string, every time.Duration, logger *slog.Logger) {
	// ⚠ CHECK ONCE IMMEDIATELY, before waiting out an interval. The upgrade
	// decision and h.add are not one atomic step: a revocation that swept
	// bySession in between misses this client entirely — it was not indexed yet —
	// and without this first pass the socket would hold an already-revoked session
	// until the first tick, minutes later.
	if !stillValid(ctx, c, revalidate, token, logger) {
		return
	}
	// ⚠ The interval is JITTERED. Every socket a page load opens would otherwise
	// tick in phase for as long as it lives, and each tick is a query against a
	// pool of exactly one connection, so the household's sockets would queue their
	// re-checks ahead of user-facing requests in one burst every interval.
	t := time.NewTimer(jitter(every))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !stillValid(ctx, c, revalidate, token, logger) {
				return
			}
			t.Reset(jitter(every))
		}
	}
}

// stillValid re-takes the decision once and reports whether the socket lives on,
// cancelling it when it must not.
//
// ⚠ ONLY RevalidationGone CLOSES THE SOCKET. "I could not tell" keeps the
// connection: see RevalidationUnknown.
func stillValid(ctx context.Context, c *client, revalidate func(context.Context, string) (string, Revalidation), token string, logger *slog.Logger) bool {
	userID, verdict := revalidate(ctx, token)
	switch {
	case verdict == RevalidationGone:
		logger.Info("ws: session revoked or expired — closing the socket",
			"user", c.userID, "session", c.sessionID)
	case verdict == RevalidationValid && userID != c.userID:
		// A CHANGED id matters as much as a rejected one: the socket is indexed
		// under the id it opened with, and would go on receiving that user's
		// audience.
		logger.Warn("ws: session now resolves to a different member — closing the socket",
			"opened_as", c.userID, "now", userID, "session", c.sessionID)
	default:
		return true
	}
	c.cancel()
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
func (h *Hub) writePump(ctx context.Context, c *client) {
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-c.send:
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.conn.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				c.cancel()
				return
			}
		}
	}
}
