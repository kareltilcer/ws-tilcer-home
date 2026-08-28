package chat_test

// The membership harness: ONE database seen through several members' sessions.
//
// Every test in this package is written FROM THE ATTACKER'S SIDE, because that is
// the only side the interesting bugs are visible from. A member reading their own
// conversation works in every wrong implementation too; what separates them is what
// a second member who is NOT in the room gets, and what a member added yesterday
// can see of last week.
//
// Three standing characters, and each one exists to ask a different question:
//
//	kaja / andy   two ordinary members, so "not in this conversation" is testable
//	boss          an admin who is not a member — the D255 asymmetry
//	quiet         a reader, who in this module WRITES (D222)

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/chat"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"

	"database/sql"
)

type member struct {
	id    string
	name  string
	roles []string
}

var (
	kaja  = member{"u-kaja", "Kája", []string{"editor"}}
	andy  = member{"u-andy", "Andy", []string{"editor"}}
	boss  = member{"u-admin", "Šéf", []string{"admin"}}
	quiet = member{"u-reader", "Čtenář", []string{"reader"}}
)

// stubDirectory stands in for push.Store's session projection.
//
// A stub rather than real session rows because the directory is a LOGIN HISTORY:
// building one through the auth package would test auth, and what these tests need
// is a fixed set of names to assert the projection against.
type stubDirectory struct{ members []push.Member }

func (d stubDirectory) Members(context.Context) ([]push.Member, error) { return d.members, nil }

// capturedPush records what would have gone to a device, so the tests can assert an
// ABSENT delivery — which is most of what the leak table asks for.
//
// ⚠ It is concurrency-safe and signals, because chat sends OFF THE REQUEST PATH:
// push.Sender is explicit that no mutation may be slowed or failed by a push
// service, so the send happens in a goroutine after the write commits. A test that
// read these slices straight after SendMessage returned would be racing it.
type capturedPush struct {
	mu         sync.Mutex
	sent       chan struct{}
	recipients [][]string
	envelopes  []push.Envelope
}

func newCapturedPush() *capturedPush {
	return &capturedPush{sent: make(chan struct{}, 16)}
}

func (c *capturedPush) Send(_ context.Context, recipients []string, e push.Envelope) []push.DeliveryResult {
	c.mu.Lock()
	c.recipients = append(c.recipients, recipients)
	c.envelopes = append(c.envelopes, e)
	c.mu.Unlock()
	c.sent <- struct{}{}
	return nil
}
func (c *capturedPush) VAPIDPublicKey() string { return "" }
func (c *capturedPush) Enabled() bool          { return true }

// awaitPush blocks until the next push lands and returns its recipients.
func (hh *household) awaitPush(t *testing.T) []string {
	t.Helper()
	select {
	case <-hh.pushes.sent:
	case <-time.After(2 * time.Second):
		t.Fatalf("no push was sent within 2s")
	}
	hh.pushes.mu.Lock()
	defer hh.pushes.mu.Unlock()
	return hh.pushes.recipients[len(hh.pushes.recipients)-1]
}

// capturedNotify records every targeted /ws publish: who it reached and what it
// carried. The audience is half of what v10 is about, so it is asserted directly.
type capturedNotify struct {
	audiences [][]string
	types     []string
	payloads  []any
}

func (c *capturedNotify) fn(_ context.Context, userIDs []string, typ string, payload any) {
	c.audiences = append(c.audiences, userIDs)
	c.types = append(c.types, typ)
	c.payloads = append(c.payloads, payload)
}

// reset clears the setup's publishes so a test asserts only its own.
//
// ⚠ ALL THREE TOGETHER, WHICH IS WHY IT IS A METHOD (v10 review). The slices are
// parallel and tests index one by a position found in another — `payloads[room]`
// where `room` came from scanning `types`. Two call sites cleared `types` and
// `audiences` by hand and left `payloads` holding the setup's frames, so every
// index was off by however many publishes the setup happened to make. It read as
// passing until a verb started publishing one more (CreateConversation telling the
// members it adds), at which point the offset moved and the assertion failed
// somewhere with nothing to do with the change.
func (c *capturedNotify) reset() {
	c.audiences, c.types, c.payloads = nil, nil, nil
}

type household struct {
	t        *testing.T
	db       *sql.DB
	svc      *chat.Service
	handlers map[string]http.Handler
	notify   *capturedNotify
	pushes   *capturedPush
}

func newHousehold(t *testing.T, members ...member) *household {
	t.Helper()
	db := testsupport.NewDB(t)

	dir := stubDirectory{}
	for _, m := range members {
		dir.members = append(dir.members, push.Member{UserID: m.id, DisplayName: m.name,
			Email: m.id + "@example.test", Roles: m.roles})
	}
	notify := &capturedNotify{}
	pushes := newCapturedPush()
	svc := chat.NewService(db, audit.NewSink(), notify.fn, pushes, dir, chat.Options{
		TrashDays: 7,
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	h := chat.NewHandler(svc)

	handlers := map[string]http.Handler{}
	for _, m := range members {
		actor := reqctx.Actor{UserID: m.id, Type: "user", Label: m.name, Roles: m.roles}
		handlers[m.id] = httpx.NewRouter(httpx.Deps{
			Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
			DB:        db,
			Site:      "home",
			SessionMW: auth.NewSessionAuth(auth.Config{BypassActor: &actor}),
			MountAPI:  func(r chi.Router) { h.Mount(r) },
		})
	}
	return &household{t: t, db: db, svc: svc, handlers: handlers, notify: notify, pushes: pushes}
}

func (hh *household) ctx(m member) context.Context {
	return testsupport.CtxUser(m.id, m.roles...)
}

// as issues a request through one member's session.
func (hh *household) as(m member, method, path, body string) *httptest.ResponseRecorder {
	hh.t.Helper()
	handler, ok := hh.handlers[m.id]
	if !ok {
		hh.t.Fatalf("member %s was not registered with newHousehold", m.id)
	}
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, r)
	return rr
}

// group creates a conversation owned by `owner` containing `others`.
func (hh *household) group(owner member, name string, others ...member) chat.Conversation {
	hh.t.Helper()
	ids := make([]string, 0, len(others))
	for _, m := range others {
		ids = append(ids, m.id)
	}
	c, err := hh.svc.CreateConversation(hh.ctx(owner), chat.ConversationCreate{Name: name, MemberIDs: ids})
	if err != nil {
		hh.t.Fatalf("create conversation %q: %v", name, err)
	}
	return c
}

// send posts a message and returns it.
func (hh *household) send(m member, conversationID, body string) chat.Message {
	hh.t.Helper()
	msg, err := hh.svc.SendMessage(hh.ctx(m), conversationID, chat.MessageCreate{Body: body})
	if err != nil {
		hh.t.Fatalf("send %q: %v", body, err)
	}
	return msg
}

// defaultConversation is the seeded Všichni room.
func (hh *household) defaultConversation(t *testing.T) string {
	t.Helper()
	var id string
	if err := hh.db.QueryRow(`SELECT id FROM chat_conversations WHERE kind = 'default'`).Scan(&id); err != nil {
		t.Fatalf("the Všichni conversation is missing from the migrated schema: %v", err)
	}
	return id
}

// join makes a member's first chat request, which is what enrols them in Všichni.
//
// ⚠ It goes through the HTTP router on purpose. The auto-join is the chat router's
// own middleware (http.go) rather than a step inside each service method, so a
// helper that called the service directly would test a path production never takes
// — and would quietly hide the middleware falling out of Mount.
func (hh *household) join(m member) {
	hh.t.Helper()
	if rr := hh.as(m, "GET", "/api/chat/conversations", ""); rr.Code != http.StatusOK {
		hh.t.Fatalf("auto-join %s: %d %s", m.id, rr.Code, rr.Body.String())
	}
}

func decode[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %T from %s: %v", v, rr.Body.String(), err)
	}
	return v
}

func auditCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events`).Scan(&n); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	return n
}
