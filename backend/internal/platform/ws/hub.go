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
// neither covers the other: DisconnectSession closes them the moment auth knows,
// and the handler re-takes the session decision on a ticker for everything auth
// never gets to announce — a TTL that simply expires, a row revoked out of band.
//
// ⚠ TARGETING AND REVOCATION ARE KEYED DIFFERENTLY, and both keys are needed. A
// chat audience is a set of MEMBERS, so PublishTo walks byUser. A revocation is
// always one SESSION — logout drops the calling device's token, the fail-closed
// re-mint one row — so DisconnectSession walks bySession. Closing by user id
// instead would tear down a member's laptop because they logged out on a phone.
package ws

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

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
	// bySession indexes the SAME clients by the session that authorised them, so a
	// revocation closes exactly the sockets it revoked. A set per id because one
	// session is one cookie jar, which can hold several tabs.
	bySession map[string]map[*client]struct{}
	// logger records the fan-out failures that are otherwise invisible. A message
	// dropped for ONE slow client needs no log — refetch-on-focus repairs it — but
	// a message that reached NOBODY leaves no trace anywhere else.
	//
	// ⚠ It is injected rather than reached for, and it is the package's ONLY
	// logger: the handler logs through it too. Defaulting to slog.Default() inside
	// the hub while the composition root threaded its configured handler in
	// through Config left half this package's output escaping the structured
	// stream the moment anything stopped calling slog.SetDefault.
	logger *slog.Logger
	// pumps holds the recurring revalidation ticker of every connected SESSION,
	// refcounted across that session's sockets. See startSessionPump.
	pumps map[pumpKey]*sessionPump
}

// pumpKey is what a socket must AGREE ON to share a session's ticker: the
// session, the member it opened as, and the token the re-check is taken with.
//
// ⚠ The session id alone is not enough, and keying on it was a silent hazard.
// The pump keeps the FIRST socket's token and openedAs for its whole life, so a
// later socket handing over a different token had its own discarded: the ticker
// would go on re-checking the stale one and, when that stopped resolving, close
// every socket of a session that is perfectly live. The mirror case was already
// reachable — a session whose first socket is an id-less service principal pins
// openedAs to "" and disables the changed-id guard for every later socket on it.
// Keying on all three costs nothing in the ordinary case (every tab of one
// cookie jar agrees on all three, so they still share ONE ticker) and splits the
// pumps only when the sockets genuinely disagree.
//
// ⚠ IT IS THE TOKEN'S DIGEST, NEVER THE TOKEN. The key is a map key in the Hub,
// so whatever it holds is retained for as long as the session has a socket, in a
// struct with no redaction on it: the raw value there is a working session
// cookie, valid for the whole TTL (90 days by default), handed out by any %+v in
// a future debug or panic path, any heap dump taken for a memory investigation,
// any core file. The database itself stores only a hash. A digest compares
// exactly as well — agreeing sockets agree on it, disagreeing ones do not — and
// the raw token reaches the ticker as a parameter instead, where it lives on the
// pump goroutine's stack and nothing reachable from the Hub can render it.
type pumpKey struct {
	sessionID string
	openedAs  string
	tokenHash string
}

// hashPumpToken reduces a raw session token to the digest pumpKey compares on.
func hashPumpToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewHub returns an empty hub logging through logger (nil means slog.Default()).
func NewHub(logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		clients:   make(map[*client]struct{}),
		byUser:    make(map[string]map[*client]struct{}),
		bySession: make(map[string]map[*client]struct{}),
		pumps:     make(map[pumpKey]*sessionPump),
		logger:    logger,
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
// been revoked — that is DisconnectSession's job (per SESSION, never per member;
// see the package comment), and the handler's revalidation loop.
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
	// Dedupe the ID LIST, not the client set. A client is filed under exactly one
	// id, so two different ids can never share one and a duplicate delivery can
	// only come from a repeated id — a caller de-duplicating badly, or a member
	// appearing as both author and recipient. A duplicate chat frame is a
	// duplicate bubble. Deduping here costs one small map over a handful of
	// strings rather than one over every recipient socket.
	seen := make(map[string]struct{}, len(userIDs))
	dropped := 0
	for _, id := range userIDs {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		for c := range h.byUser[id] {
			select {
			case c.send <- data:
			default: // slow client: drop; counted and logged below
				dropped++
			}
		}
	}
	h.mu.Unlock()
	// ⚠ A TARGETED DROP IS NOT A BROADCAST DROP, and it is the only loss in this
	// file with no other trace. Publish's drop costs a refresh that
	// refetch-on-focus repairs; here the frame IS the content and there is no
	// replay, so a saturated phone simply never sees that message — and D259's
	// gap check only notices on the NEXT one, which may never come. The
	// marshal-failure paths above were given a log on exactly this reasoning.
	//
	// Logged AFTER the unlock, and once per publish rather than once per client:
	// a write to the log handler while holding h.mu would stall Publish, add,
	// remove and Count on whatever stdout is attached to.
	if dropped > 0 {
		h.logger.Warn("ws: targeted publish dropped for a saturated client — the frame is not replayed",
			"type", m.Type, "recipients", len(seen), "dropped", dropped)
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

// DisconnectSession closes every socket that sessionID authorised (v10).
//
// ⚠ A websocket is authenticated ONCE, at upgrade, and then lives for as long as
// the browser keeps it. That was harmless while every fan-out was household-wide;
// with PublishTo carrying message bodies, a session revoked by logout or by a
// failed role re-mint would otherwise keep receiving private content on behalf of
// an account that is 401 on every other request. Auth calls this the moment it
// revokes.
//
// ⚠ ONE SESSION, NOT ONE MEMBER. Every revocation auth performs is per-session,
// so this is the granularity that matches it: a member logging out on their phone
// must not lose the laptop's live feed, whose session was never touched. It does
// close every TAB of that one session, which is correct — they share the cookie
// that was just revoked.
func (h *Hub) DisconnectSession(sessionID string) {
	if sessionID == "" {
		return
	}
	h.mu.Lock()
	doomed := make([]*client, 0, len(h.bySession[sessionID]))
	for c := range h.bySession[sessionID] {
		doomed = append(doomed, c)
	}
	h.mu.Unlock()
	// Cancel OUTSIDE the lock: cancelling unblocks the write pump, whose exit path
	// calls remove(), which takes this same mutex.
	for _, c := range doomed {
		c.revoke()
	}
	if len(doomed) > 0 {
		h.logger.Info("ws: closing the sockets of a revoked session", "session", sessionID, "sockets", len(doomed))
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
	indexAdd(h.byUser, c.userID, c)
	indexAdd(h.bySession, c.sessionID, c)
	h.mu.Unlock()
}

// remove drops a client from ALL THREE maps.
//
// ⚠ THE INDEX DELETES ARE THE WHOLE POINT, and forgetting one is a leak that
// never errors and never logs: every disconnect would leave a dead *client in
// byUser[id] or bySession[id], holding its send channel and its cancel func, and
// the set would grow for the process's lifetime. It surfaces as memory months
// later, on a household app nobody profiles. TestByUserEmptiesOnDisconnect exists
// for exactly this.
//
// The empty inner map is deleted too, so a member who has not connected since boot
// does not leave an entry behind for every id the hub has ever seen.
func (h *Hub) remove(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	indexDelete(h.byUser, c.userID, c)
	indexDelete(h.bySession, c.sessionID, c)
	h.mu.Unlock()
}

// indexAdd files c under key, skipping the empty key (an unidentified connection
// belongs in `clients` and nowhere else). Callers hold h.mu.
func indexAdd(index map[string]map[*client]struct{}, key string, c *client) {
	if key == "" {
		return
	}
	set, ok := index[key]
	if !ok {
		set = make(map[*client]struct{})
		index[key] = set
	}
	set[c] = struct{}{}
}

// indexDelete removes c from index[key], and the now-empty set with it. Callers
// hold h.mu.
func indexDelete(index map[string]map[*client]struct{}, key string, c *client) {
	if key == "" {
		return
	}
	set, ok := index[key]
	if !ok {
		return
	}
	delete(set, c)
	if len(set) == 0 {
		delete(index, key)
	}
}

// client is one connected websocket. send is never closed (removal cancels the
// write pump via context instead), so Publish can never send on a closed channel.
//
// userID is the authenticated actor's id (v10). It is EMPTY only for a connection
// whose actor carried no user id, and such a client is reachable by Publish and
// never by PublishTo. sessionID is the session that authorised the upgrade, and is
// empty under the dev bypass, where there is no session at all. Both are set once
// at accept time and never mutated, so they need no lock of their own.
//
// revoked records that this socket was closed BECAUSE its session no longer
// authorises it, rather than because the process or the browser went away. It
// picks the websocket close code, which is the only thing that tells the two
// apart on the wire: a client that treats a revocation like a restart reconnects
// into an upgrade that will 401 for the rest of the tab's life. Written before
// cancel and read after the write pump has unwound, so it is an atomic rather
// than another field under h.mu.
type client struct {
	conn      *websocket.Conn
	send      chan []byte
	cancel    context.CancelFunc
	userID    string
	sessionID string
	revoked   atomic.Bool
}

// revoke closes the socket AND records why, so the handler can answer with a
// close code the browser can act on. Every path that drops a connection over the
// session — DisconnectSession, and the revalidation checks — goes through here;
// a plain cancel() stays "the connection is over", with no verdict attached.
func (c *client) revoke() {
	c.revoked.Store(true)
	c.cancel()
}

// sessionPump is the recurring revalidation ticker of ONE session, shared by
// every socket that session has open.
//
// ⚠ ONE TICKER PER SESSION, NOT PER SOCKET. Every tab of one browser carries the
// same cookie, so they cannot disagree about the verdict, and a ticker each would
// mean four Lookups per interval for four tabs against a pool of exactly one
// connection (db.SetMaxOpenConns(1)) — and four Mints, not one, whenever a
// re-mint fails transiently, because that path stamps no roles_refreshed_at for a
// second tab to read. refs counts the sockets holding it; the last one out stops
// it.
//
// ⚠ Its context is NOT any connection's. A tick in flight must survive the tab
// that happened to open the session closing mid-query, and must stop when the
// LAST of them goes.
type sessionPump struct {
	// key identifies what this ticker re-checks and what a joining socket has to
	// match to share it. It carries the token's DIGEST, not the token — the raw
	// one is a parameter of the goroutine. See pumpKey.
	key    pumpKey
	cancel context.CancelFunc
	refs   int
}

// startSessionPump joins the caller to the revalidation ticker for its session,
// starting one if no socket agreeing on the same key already has it. The returned
// handle must be passed back to releaseSessionPump when the socket goes.
func (h *Hub) startSessionPump(sessionID, openedAs, token string, revalidate RevalidateFunc, every time.Duration) *sessionPump {
	key := pumpKey{sessionID: sessionID, openedAs: openedAs, tokenHash: hashPumpToken(token)}
	h.mu.Lock()
	defer h.mu.Unlock()
	if p, ok := h.pumps[key]; ok {
		p.refs++
		return p
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &sessionPump{key: key, cancel: cancel, refs: 1}
	h.pumps[key] = p
	// ⚠ The raw token goes to the goroutine, never onto the pump. Sharing is
	// decided by the digest in the key; only the re-check itself needs the real
	// value, and on that stack it is unreachable from the Hub.
	go h.runSessionPump(ctx, p, token, revalidate, every)
	return p
}

// releaseSessionPump drops one socket's hold on the ticker, stopping it when the
// last one lets go.
//
// ⚠ It matches on the HANDLE, not on the key. A pump that retired itself on a
// verdict may already have been replaced by a fresh one for a reconnected
// session, and decrementing that one's refs on behalf of a socket it never
// counted would stop a live pump while sockets still depend on it.
func (h *Hub) releaseSessionPump(p *sessionPump) {
	if p == nil {
		return
	}
	h.mu.Lock()
	cur, ok := h.pumps[p.key]
	if !ok || cur != p {
		h.mu.Unlock() // already retired; its goroutine is gone
		return
	}
	p.refs--
	if p.refs > 0 {
		h.mu.Unlock()
		return
	}
	delete(h.pumps, p.key)
	h.mu.Unlock()
	p.cancel()
}

// retireSessionPump drops the registry entry for a pump that has decided to stop,
// so the session's next socket starts a live ticker instead of joining a dead one.
func (h *Hub) retireSessionPump(p *sessionPump) {
	h.mu.Lock()
	if cur, ok := h.pumps[p.key]; ok && cur == p {
		delete(h.pumps, p.key)
	}
	h.mu.Unlock()
	p.cancel()
}
