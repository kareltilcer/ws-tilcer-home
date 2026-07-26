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
		if cfg.BypassActor == nil {
			if cfg.Authenticate == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if _, ok := cfg.Authenticate(r); !ok {
				http.Error(w, "invalid or missing session", http.StatusUnauthorized)
				return
			}
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return // Accept already wrote the response
		}

		ctx, cancel := context.WithCancel(context.Background())
		c := &client{conn: conn, send: make(chan []byte, sendBuffer), cancel: cancel}
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
