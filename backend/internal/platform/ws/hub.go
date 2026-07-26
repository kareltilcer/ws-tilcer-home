// Package ws provides the module-agnostic websocket hub (HANDOFF F5). Feature
// modules publish change events to it after committing; the hub fans them out to
// connected clients so open boards and dashboards stay live. The frontend
// applies pushes via setQueryData / targeted invalidation, with refetch-on-focus
// as the reconnect fallback.
package ws

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/coder/websocket"
)

// Message is a change notification broadcast to clients. Type names the change
// (e.g. "card.moved", "event.completed"); the frontend keys its cache updates
// off it. Payload is optional structured data.
type Message struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

// sendBuffer bounds per-client backpressure; a client that can't keep up drops
// messages (it will refetch on focus/reconnect) rather than stalling the hub.
const sendBuffer = 32

// Hub tracks connected clients and broadcasts messages to them.
type Hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{clients: make(map[*client]struct{})}
}

// Publish marshals m once and delivers it to every connected client without
// blocking; a full client buffer drops the message for that client.
func (h *Hub) Publish(m Message) {
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- data:
		default: // slow client: drop; it recovers via refetch-on-focus
		}
	}
}

// Count returns the number of connected clients (used in tests/metrics).
func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) add(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// client is one connected websocket. send is never closed (removal cancels the
// write pump via context instead), so Publish can never send on a closed channel.
type client struct {
	conn   *websocket.Conn
	send   chan []byte
	cancel context.CancelFunc
}
