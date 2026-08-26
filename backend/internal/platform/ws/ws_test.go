package ws_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	cws "github.com/coder/websocket"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/ws"
)

// newServer builds a hub whose /ws authenticates via the injected closure (the
// production handler reads the session cookie; the test stubs the decision).
func newServer(t *testing.T, authOK bool) (*ws.Hub, string) {
	t.Helper()
	hub := ws.NewHub(discardLogger())
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.Handler(ws.Config{
		Authenticate: func(*http.Request) (ws.Upgrade, bool) {
			return ws.Upgrade{
				Actor:     reqctx.Actor{UserID: "u1", Type: "user", Roles: []string{"editor"}},
				SessionID: "s1",
				Token:     "t1",
			}, authOK
		},
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return hub, "ws" + strings.TrimPrefix(srv.URL, "http")
}

// waitFor polls get until it EQUALS want. Connect and disconnect are both
// asynchronous (the handler registers after the dial returns, and unregisters
// after its write pump unwinds), so every assertion about hub bookkeeping has to
// wait for it. Equality is right for a value that settles — Count,
// TrackedClientsForTest — and wrong for one that only climbs: see waitAtLeast.
func waitFor(t *testing.T, get func() int, want int, what string) {
	t.Helper()
	poll(t, get, func(n int) bool { return n == want }, "want "+strconv.Itoa(want), what)
}

// waitAtLeast polls get until it REACHES min. A counter that only increases can
// step straight past an exact target when a poll is descheduled (3 -> 5 across
// one slow sleep), and waitFor would then spin out its whole deadline and fail a
// test that had already done what it was waiting for.
func waitAtLeast(t *testing.T, get func() int, min int, what string) {
	t.Helper()
	poll(t, get, func(n int) bool { return n >= min }, "want >= "+strconv.Itoa(min), what)
}

// poll is the one polling policy behind both.
func poll(t *testing.T, get func() int, ok func(int) bool, want, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	// ⚠ The message reports the LAST POLLED value, not a fresh read. On a loaded
	// machine the awaited value routinely lands just after the deadline, and
	// re-reading here printed "hub client count = 0, want 0" — a failure whose own
	// text says the assertion held, sending the reader after a hub bug that is not
	// there.
	//
	// ⚠ For the same reason the check runs AFTER the re-read rather than at the
	// top of the loop: a value that arrives on the final poll used to exit through
	// the deadline and print that same self-contradicting message.
	last := get()
	for {
		if ok(last) {
			return
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
		last = get()
	}
	t.Fatalf("%s = %d, %s", what, last, want)
}

// The log sink and the two loggers live in testsupport: the mutex-guarded buffer
// was already written verbatim in documents' preview tests, and asserting on a
// log line is not a ws-specific need. captureLogger is for the handler's warnings
// about connections that are a BUG STATE rather than a policy — they change no
// observable behaviour, so the log line is the only thing that can be asserted,
// and without asserting it the branch can be deleted whole.
func discardLogger() *slog.Logger { return testsupport.DiscardLogger() }

func captureLogger() (*slog.Logger, *testsupport.SyncBuffer) { return testsupport.CaptureLogger() }

func waitCount(t *testing.T, hub *ws.Hub, want int) {
	t.Helper()
	waitFor(t, hub.Count, want, "hub client count")
}

func TestWS_ConnectRequiresValidSession(t *testing.T) {
	_, wsURL := newServer(t, false) // authenticator rejects
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
	if err == nil {
		conn.Close(cws.StatusNormalClosure, "")
		t.Fatal("expected dial to fail without a valid session")
	}
}

func TestWS_ConnectAndBroadcast(t *testing.T) {
	hub, wsURL := newServer(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dial := func() *cws.Conn {
		c, _, err := cws.Dial(ctx, wsURL+"/ws", nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		t.Cleanup(func() { c.Close(cws.StatusNormalClosure, "") })
		return c
	}

	c1 := dial()
	c2 := dial()
	waitCount(t, hub, 2)

	hub.Publish(ws.Message{Type: "card.moved", Payload: map[string]any{"id": "c1"}})

	for i, c := range []*cws.Conn{c1, c2} {
		typ, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("client %d read: %v", i, err)
		}
		if typ != cws.MessageText {
			t.Errorf("client %d message type = %v", i, typ)
		}
		var m ws.Message
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("client %d unmarshal: %v", i, err)
		}
		if m.Type != "card.moved" {
			t.Errorf("client %d message = %q, want card.moved", i, m.Type)
		}
	}
}
