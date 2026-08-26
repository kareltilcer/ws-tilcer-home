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
	Logger       *slog.Logger
}

// Handler returns the session-authenticated /ws upgrade handler. Reads are open
// to any authenticated user, so connecting only requires a valid session (no role
// gate here).
func (h *Hub) Handler(cfg Config) http.HandlerFunc {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
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
		} else {
			// The dev bypass has no session and therefore no real actor. It registers
			// under the configured fake id so a developer's own targeted pushes arrive;
			// with no id configured the connection is ANONYMOUS and PublishTo never
			// reaches it — which is the safe direction, and HOME_DEV_AUTH_BYPASS is
			// refused in production regardless.
			userID = cfg.BypassActor.UserID
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return // Accept already wrote the response
		}

		ctx, cancel := context.WithCancel(context.Background())
		c := &client{conn: conn, send: make(chan []byte, sendBuffer), cancel: cancel, userID: userID}
		h.add(c)
		logger.Info("ws connected", "clients", h.Count())

		go h.readPump(ctx, c)
		h.writePump(ctx, c)

		h.remove(c)
		cancel()
		_ = conn.Close(websocket.StatusNormalClosure, "")
		logger.Info("ws disconnected", "clients", h.Count())
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
