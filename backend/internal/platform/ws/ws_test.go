package ws_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cws "github.com/coder/websocket"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/ws"
)

// newServer builds a hub whose /ws authenticates via the injected closure (the
// production handler reads the session cookie; the test stubs the decision).
func newServer(t *testing.T, authOK bool) (*ws.Hub, string) {
	t.Helper()
	hub := ws.NewHub()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.Handler(ws.Config{
		Authenticate: func(*http.Request) (reqctx.Actor, bool) {
			return reqctx.Actor{UserID: "u1", Type: "user", Roles: []string{"editor"}}, authOK
		},
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return hub, "ws" + strings.TrimPrefix(srv.URL, "http")
}

// waitFor polls get until it reaches want. Connect and disconnect are both
// asynchronous (the handler registers after the dial returns, and unregisters
// after its write pump unwinds), so every assertion about hub bookkeeping has to
// wait for it. One polling policy, used by waitCount and by waitTracked.
func waitFor(t *testing.T, get func() int, want int, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if get() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s = %d, want %d", what, get(), want)
}

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
