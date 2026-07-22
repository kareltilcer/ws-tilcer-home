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
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/ws"
)

type fakeIntrospector struct {
	active bool
}

func (f fakeIntrospector) Introspect(context.Context, string) (auth.Claims, error) {
	return auth.Claims{Active: f.active, UserID: "u1", Roles: []string{"editor"}}, nil
}

func newServer(t *testing.T, active bool) (*ws.Hub, string) {
	t.Helper()
	hub := ws.NewHub()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.Handler(ws.Config{Introspector: fakeIntrospector{active: active}}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return hub, "ws" + strings.TrimPrefix(srv.URL, "http")
}

func waitCount(t *testing.T, hub *ws.Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.Count() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hub client count = %d, want %d", hub.Count(), want)
}

func TestWS_ConnectRequiresValidToken(t *testing.T) {
	_, wsURL := newServer(t, false) // introspector rejects
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, wsURL+"/ws?access_token=bad", nil)
	if err == nil {
		conn.Close(cws.StatusNormalClosure, "")
		t.Fatal("expected dial to fail with an invalid token")
	}
}

func TestWS_ConnectAndBroadcast(t *testing.T) {
	hub, wsURL := newServer(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dial := func() *cws.Conn {
		c, _, err := cws.Dial(ctx, wsURL+"/ws?access_token=good", nil)
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
