package ws

import (
	"context"
	"log/slog"
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
	// Authenticate returns the connecting actor, or ok=false to reject. Built in
	// the composition root over the session store. nil under the dev bypass.
	Authenticate func(r *http.Request) (reqctx.Actor, bool)
	BypassActor  *reqctx.Actor
	// RevalidateEvery is how often an already-open socket re-takes the upgrade
	// decision (v10). Zero means defaultRevalidateEvery. See revalidatePump.
	RevalidateEvery time.Duration
	Logger          *slog.Logger
}

// defaultRevalidateEvery bounds how long a socket may outlive the session that
// opened it when nothing announces the revocation (a TTL that simply expires, a
// row revoked out of band). Auth's own revocation paths call Hub.DisconnectUser
// and do not wait for this tick.
const defaultRevalidateEvery = 5 * time.Minute

// Handler returns the session-authenticated /ws upgrade handler. Reads are open
// to any authenticated user, so connecting only requires a valid session (no role
// gate here).
func (h *Hub) Handler(cfg Config) http.HandlerFunc {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	revalidateEvery := cfg.RevalidateEvery
	if revalidateEvery <= 0 {
		revalidateEvery = defaultRevalidateEvery
	}
	return func(w http.ResponseWriter, r *http.Request) {
		// ⚠ v10: the actor resolved here is KEPT (D232). Until v10 it was resolved
		// purely to decide accept-or-reject and then discarded, which is what made
		// the hub anonymous and every fan-out a broadcast. `chat` publishes message
		// bodies to a member set, so the connection has to know who it belongs to.
		var userID string
		if cfg.BypassActor == nil {
			if cfg.Authenticate == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			actor, ok := cfg.Authenticate(r)
			if !ok {
				http.Error(w, "invalid or missing session", http.StatusUnauthorized)
				return
			}
			userID = actor.UserID
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
		c := &client{conn: conn, send: make(chan []byte, sendBuffer), cancel: cancel, userID: userID}
		h.add(c)
		// The id is logged on both ends: "did that member's socket register, and
		// under what?" is the first question a missing targeted push raises, and
		// byUser is otherwise reachable only from a test.
		logger.Info("ws connected", "user", userID, "clients", h.Count())

		go h.readPump(ctx, c)
		if cfg.BypassActor == nil && cfg.Authenticate != nil {
			// Cloned so the pump can re-run the upgrade decision on its own context
			// after this request's has been hijacked.
			go revalidatePump(ctx, c, cfg.Authenticate, r.Clone(ctx), revalidateEvery, logger)
		}
		h.writePump(ctx, c)

		h.remove(c)
		cancel()
		_ = conn.Close(websocket.StatusNormalClosure, "")
		logger.Info("ws disconnected", "user", userID, "clients", h.Count())
	}
}

// revalidatePump re-takes the upgrade decision periodically and closes the socket
// when it no longer holds.
//
// ⚠ Without it a socket is authenticated exactly once and then trusted forever.
// That cost nothing while every fan-out was a household-wide broadcast; with
// PublishTo carrying message bodies (D233), a session that has expired or been
// revoked would keep receiving private content over a connection nobody can see —
// 401 on every HTTP request, still live on the socket. Auth's own revocations
// call Hub.DisconnectUser immediately; this is the backstop for the ones nothing
// announces.
func revalidatePump(ctx context.Context, c *client, authenticate func(*http.Request) (reqctx.Actor, bool), r *http.Request, every time.Duration, logger *slog.Logger) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// A CHANGED id matters as much as a rejected one: the socket is indexed
			// under the id it opened with, and would go on receiving that user's
			// audience.
			if actor, ok := authenticate(r); !ok || actor.UserID != c.userID {
				logger.Info("ws: session no longer valid — closing the socket", "user", c.userID)
				c.cancel()
				return
			}
		}
	}
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
