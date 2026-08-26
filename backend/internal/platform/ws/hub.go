// Package ws provides the module-agnostic websocket hub (HANDOFF F5). Feature
// modules publish change events to it after committing; the hub fans them out to
// connected clients so open boards and dashboards stay live. The frontend
// applies pushes via setQueryData / targeted invalidation, with refetch-on-focus
// as the reconnect fallback.
//
// ⚠ v10 — THE HUB LEARNS WHO IS CONNECTED (PRD D232, FR-V10-18).
//
// Until v10 the hub was deliberately anonymous: a client was a socket, a send
// channel and a cancel func, and every message reached every connection. That was
// correct while every module published data the whole household could read, and it
// is why v9's private mutations publish `{"private":"1"}` with the id dropped
// (D190) rather than targeting an audience the hub could not express.
//
// `chat` cannot work that way: its payload IS the thing that must not leak, and a
// conversation is readable only by its members. So a connection now carries the
// user id the upgrade handler already resolves, and PublishTo fans out to a named
// set.
//
// ⚠ Publish is UNCHANGED and must stay that way. Ten modules depend on it and none
// of them should need a line touched — TestPublishStillReachesEveryClient is what
// says so. PublishTo is additive.
//
// ⚠ A SOCKET MUST NOT OUTLIVE THE SESSION THAT OPENED IT. While every fan-out was
// a household-wide broadcast a stale connection leaked nothing; now that the
// payload is the private content (D233), a session revoked by logout or by a
// failed role re-mint has to stop reaching its sockets. Two mechanisms, because
// neither covers the other: DisconnectUser closes them the moment auth knows, and
// the handler re-takes the upgrade decision on a ticker for everything auth never
// gets to announce — a TTL that simply expires, a row revoked out of band.
package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/coder/websocket"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Message is a change notification broadcast to clients. Type names the change
// (e.g. "card.moved", "event.completed"); the frontend keys its cache updates
// off it. Payload is optional structured data.
//
// Origin is the client id (X-Client-Id) of the request that caused the change.
// The broadcast still reaches every client including the originator; a client
// compares Origin to its own id to skip the "changed elsewhere" toast for its
// own echo, while still applying the cache invalidation.
type Message struct {
	Type    string `json:"type"`
	Origin  string `json:"origin,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

// sendBuffer bounds per-client backpressure; a client that can't keep up drops
// messages (it will refetch on focus/reconnect) rather than stalling the hub.
//
// ⚠ For a CHAT message a dropped frame is a missing message rather than a missed
// refresh, and there is no replay here. That gap is closed on the message itself
// (D259: every chat payload carries prev_message_id, and the tail is refetched on
// reconnect), NOT by growing this buffer — a bigger buffer moves the cliff, it
// does not remove it.
const sendBuffer = 32

// Hub tracks connected clients and broadcasts messages to them.
type Hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
	// byUser indexes the SAME clients by the user they authenticated as, so
	// PublishTo can reach an audience without walking every connection.
	//
	// It is a SET per id, not a single client: one member has a phone, a laptop and
	// two tabs, and all of them must receive. Anonymous clients (an actor carrying
	// no user id) appear only in `clients`.
	byUser map[string]map[*client]struct{}
	// logger records the fan-out failures that are otherwise invisible. A message
	// dropped for ONE slow client needs no log — refetch-on-focus repairs it — but
	// a message that reached NOBODY leaves no trace anywhere else.
	logger *slog.Logger
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{
		clients: make(map[*client]struct{}),
		byUser:  make(map[string]map[*client]struct{}),
		logger:  slog.Default(),
	}
}

// Publish marshals m once and delivers it to every connected client without
// blocking; a full client buffer drops the message for that client.
//
// ⚠ UNCHANGED BY v10, deliberately. Every module written before chat publishes
// through here and its fan-out must stay byte-identical.
func (h *Hub) Publish(m Message) {
	data, err := json.Marshal(m)
	if err != nil {
		h.logger.Error("ws: broadcast dropped, payload does not marshal", "type", m.Type, "err", err)
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

// PublishTo marshals m ONCE and delivers it to every connection belonging to any
// of userIDs (v10, D232). It is the fan-out a member-restricted module needs: the
// payload can carry content because the audience is resolved before it is sent.
//
// ⚠ ONE MARSHAL FOR THE WHOLE AUDIENCE. That is not only an optimisation — it is
// the reason D259's prev_message_id is computed once per message rather than once
// per recipient. A per-recipient payload would mean a marshal per member and
// defeat the point of a shared frame.
//
// A user with no connections is NOT an error: a phone that is asleep is the normal
// case, and PublishTo with ids nobody is connected under is a no-op. Callers must
// never treat "delivered to nobody" as a failure — the push channel, not the
// socket, is what reaches a closed browser.
//
// ⚠ The caller resolves the audience INSIDE the writing transaction (D233), so a
// member removed a moment earlier is already absent from the set handed here. This
// function does no access control of its own and must not be asked to: it takes
// the ids it is given. What it does NOT cover is a socket whose SESSION has since
// been revoked — that is DisconnectUser's job, and the handler's revalidation loop.
func (h *Hub) PublishTo(userIDs []string, m Message) {
	if len(userIDs) == 0 {
		return
	}
	data, err := json.Marshal(m)
	if err != nil {
		// ⚠ Unlike a dropped broadcast this is silent AND unrepaired: nobody in the
		// audience receives, and D259's gap check only notices on the NEXT message.
		// Without this line the send happens and produces nothing, anywhere.
		h.logger.Error("ws: targeted publish dropped, payload does not marshal",
			"type", m.Type, "recipients", len(userIDs), "err", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// Dedupe the ID LIST, not the client set. A client is filed under exactly one
	// id, so two different ids can never share one and a duplicate delivery can
	// only come from a repeated id — a caller de-duplicating badly, or a member
	// appearing as both author and recipient. A duplicate chat frame is a
	// duplicate bubble. Deduping here costs one small map over a handful of
	// strings rather than one over every recipient socket, and keeps the send loop
	// branchless.
	var seen map[string]struct{}
	if len(userIDs) > 1 {
		seen = make(map[string]struct{}, len(userIDs))
	}
	for _, id := range userIDs {
		if seen != nil {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
		}
		for c := range h.byUser[id] {
			select {
			case c.send <- data:
			default: // slow client: drop; D259's gap check repairs the thread
			}
		}
	}
}

// Notify publishes a broadcast change event stamped with the originating
// request's client id, which is how a tab tells its own echo apart from a change
// made on another device. Modules publish through here rather than assembling a
// Message themselves.
func (h *Hub) Notify(ctx context.Context, typ string, payload any) {
	h.Publish(Message{Type: typ, Origin: originFrom(ctx), Payload: payload})
}

// NotifyTo is Notify's targeted sibling (v10) — the same Origin stamping, over
// PublishTo's member-restricted fan-out.
//
// ⚠ It exists so a member-restricted module never has to reach for reqctx itself.
// Origin is not optional decoration: without it the sender's OWN tab treats the
// message it just sent as somebody else's change (PRD 3544), and a module that
// assembled its own ws.Message would be one forgotten field away from that.
func (h *Hub) NotifyTo(ctx context.Context, userIDs []string, typ string, payload any) {
	h.PublishTo(userIDs, Message{Type: typ, Origin: originFrom(ctx), Payload: payload})
}

// originFrom extracts the X-Client-Id of the request that caused a change; empty
// for a non-browser caller, or a change with no request behind it.
func originFrom(ctx context.Context) string {
	if info, ok := reqctx.RequestFrom(ctx); ok {
		return info.ClientID
	}
	return ""
}

// DisconnectUser closes every socket belonging to userID (v10).
//
// ⚠ A websocket is authenticated ONCE, at upgrade, and then lives for as long as
// the browser keeps it. That was harmless while every fan-out was household-wide;
// with PublishTo carrying message bodies, a session revoked by logout or by a
// failed role re-mint would otherwise keep receiving private content on behalf of
// an account that is 401 on every other request. Auth calls this the moment it
// revokes.
func (h *Hub) DisconnectUser(userID string) {
	if userID == "" {
		return
	}
	h.mu.Lock()
	doomed := make([]*client, 0, len(h.byUser[userID]))
	for c := range h.byUser[userID] {
		doomed = append(doomed, c)
	}
	h.mu.Unlock()
	// Cancel OUTSIDE the lock: cancelling unblocks the write pump, whose exit path
	// calls remove(), which takes this same mutex.
	for _, c := range doomed {
		c.cancel()
	}
	if len(doomed) > 0 {
		h.logger.Info("ws: closing the sockets of a revoked session", "user", userID, "sockets", len(doomed))
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
	if c.userID != "" {
		set, ok := h.byUser[c.userID]
		if !ok {
			set = make(map[*client]struct{})
			h.byUser[c.userID] = set
		}
		set[c] = struct{}{}
	}
	h.mu.Unlock()
}

// remove drops a client from BOTH maps.
//
// ⚠ THE SECOND DELETE IS THE WHOLE POINT, and forgetting it is a leak that never
// errors and never logs: every disconnect would leave a dead *client in
// byUser[id], holding its send channel and its cancel func, and the set would grow
// for the process's lifetime. It surfaces as memory months later, on a household
// app nobody profiles. TestByUserEmptiesOnDisconnect exists for exactly this.
//
// The empty inner map is deleted too, so a member who has not connected since boot
// does not leave an entry behind for every id the hub has ever seen.
func (h *Hub) remove(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	if c.userID != "" {
		if set, ok := h.byUser[c.userID]; ok {
			delete(set, c)
			if len(set) == 0 {
				delete(h.byUser, c.userID)
			}
		}
	}
	h.mu.Unlock()
}

// client is one connected websocket. send is never closed (removal cancels the
// write pump via context instead), so Publish can never send on a closed channel.
//
// userID is the authenticated actor's id (v10). It is EMPTY only for a connection
// whose actor carried no user id, and such a client is reachable by Publish and
// never by PublishTo. It is set once at accept time and never mutated, so it needs
// no lock of its own.
type client struct {
	conn   *websocket.Conn
	send   chan []byte
	cancel context.CancelFunc
	userID string
}
