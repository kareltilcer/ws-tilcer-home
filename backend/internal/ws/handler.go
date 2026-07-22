package ws

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/reqctx"
)

// Config configures the /ws handler's authentication. BypassActor (dev only)
// short-circuits token verification; otherwise Introspector verifies the token.
type Config struct {
	Introspector auth.Introspector
	BypassActor  *reqctx.Actor
	Logger       *slog.Logger
}

// Handler returns the authenticated /ws upgrade handler. A browser cannot send
// an Authorization header on a websocket, so the token is read from (in order):
//
//	!!! ASSUMED CONTRACT — confirm the WS auth transport with the auth docs !!!
//	 1. the "access_token" query parameter (browser-friendly),
//	 2. a "bearer, <token>" Sec-WebSocket-Protocol pair,
//	 3. the Authorization: Bearer header (non-browser clients / tests).
//
// Reads are open to any authenticated user, so connecting only requires a valid
// token (no role gate here).
func (h *Hub) Handler(cfg Config) http.HandlerFunc {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var acceptOpts websocket.AcceptOptions

		if cfg.BypassActor == nil {
			token, viaProto := tokenFromRequest(r)
			if token == "" {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}
			claims, err := cfg.Introspector.Introspect(r.Context(), token)
			if err != nil || !claims.Active {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			if viaProto {
				// Must echo the negotiated subprotocol or the handshake fails.
				acceptOpts.Subprotocols = []string{"bearer"}
			}
		}

		conn, err := websocket.Accept(w, r, &acceptOpts)
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

func tokenFromRequest(r *http.Request) (token string, viaProto bool) {
	if t := r.URL.Query().Get("access_token"); t != "" {
		return t, false
	}
	// Sec-WebSocket-Protocol: "bearer, <token>"
	for _, proto := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, p := range strings.Split(proto, ",") {
			p = strings.TrimSpace(p)
			if p != "" && p != "bearer" {
				return p, true
			}
		}
	}
	const prefix = "Bearer "
	if h := r.Header.Get("Authorization"); len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):]), false
	}
	return "", false
}
