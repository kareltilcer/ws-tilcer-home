package httpx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cws "github.com/coder/websocket"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
)

type fakePinger struct{ err error }

func (f fakePinger) PingContext(context.Context) error { return f.err }

func newRouter(t *testing.T, ping error) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := httpx.NewRouter(httpx.Deps{
		Logger: logger,
		DB:     fakePinger{err: ping},
		Site:   "home",
	})
	return h, &buf
}

func TestHealthz(t *testing.T) {
	h, _ := newRouter(t, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Errorf("body = %v, want status ok", body)
	}
}

func TestReadyz_OK(t *testing.T) {
	h, _ := newRouter(t, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestReadyz_DBDown(t *testing.T) {
	h, _ := newRouter(t, errors.New("db down"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

func TestReadyz_InsecureAuthFlag(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := httpx.NewRouter(httpx.Deps{Logger: logger, DB: fakePinger{}, Site: "home", InsecureAuth: true})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if !strings.Contains(rr.Body.String(), "insecure_auth") {
		t.Errorf("readyz should surface insecure_auth when bypass active: %s", rr.Body.String())
	}
}

func TestRequestID_GeneratedAndEchoed(t *testing.T) {
	h, _ := newRouter(t, nil)

	// Generated when absent.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if got := rr.Header().Get("X-Request-Id"); got == "" {
		t.Error("expected a generated X-Request-Id header")
	}

	// Echoed when supplied.
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-Id", "abc-123")
	h.ServeHTTP(rr, req)
	if got := rr.Header().Get("X-Request-Id"); got != "abc-123" {
		t.Errorf("X-Request-Id = %q, want abc-123", got)
	}
}

// TestWebsocketUpgradeThroughMiddleware guards the /ws 501 regression: the
// Logger middleware wraps the ResponseWriter to record the status, and that
// wrapper must still forward Hijack. Without it, coder/websocket's Accept can't
// take over the connection and fails the upgrade with 501 Not Implemented.
// (The ws package's own tests mount the handler WITHOUT this middleware, so the
// break only shows through the real router — exercised here.)
func TestWebsocketUpgradeThroughMiddleware(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	// accepted closes once the server-side hijack has returned, which is also
	// when Logger emits the 101 access line; the close establishes a
	// happens-before so the later buf read below is race-free.
	accepted := make(chan struct{})
	wsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := cws.Accept(w, r, nil)
		if err != nil {
			return // Accept already wrote the failing response
		}
		close(accepted)
		c.Close(cws.StatusNormalClosure, "")
	})
	h := httpx.NewRouter(httpx.Deps{Logger: logger, DB: fakePinger{}, Site: "home", WS: wsHandler})
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := cws.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatalf("websocket upgrade through the router middleware failed: %v", err)
	}
	conn.Close(cws.StatusNormalClosure, "")

	select {
	case <-accepted:
	case <-ctx.Done():
		t.Fatal("server never completed the websocket accept")
	}
	// The upgrade must be logged exactly once, as a 101 on /ws. Logger fires
	// this from the hijack (not from WriteHeader), so it lands only on a real
	// handshake — a hijack that failed after the 101 would log its true status.
	log := buf.String()
	if !strings.Contains(log, `"status":101`) {
		t.Errorf("access log missing the 101 upgrade line:\n%s", log)
	}
	if !strings.Contains(log, `"path":"/ws"`) {
		t.Errorf("access log missing /ws path:\n%s", log)
	}
	if n := strings.Count(log, `"status":101`); n != 1 {
		t.Errorf("expected exactly one 101 upgrade line, got %d:\n%s", n, log)
	}
}

func TestAccessLogCarriesRequestID(t *testing.T) {
	h, buf := newRouter(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-Id", "log-me-42")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.Contains(buf.String(), "log-me-42") {
		t.Errorf("access log missing request_id:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "\"status\":200") {
		t.Errorf("access log missing status:\n%s", buf.String())
	}
}

// TestLimit pins the CLAMPING semantics — the property that distinguishes this
// helper from electricity's limitOf, which falls back to its default on an
// out-of-range value instead. Both spellings existed when the helper was
// extracted; only this one is shared, and the difference is behaviour, not style.
func TestLimit(t *testing.T) {
	cases := []struct {
		query string
		want  int
	}{
		{"", 50},           // absent → default
		{"?limit=", 50},    // present but empty → default
		{"?limit=abc", 50}, // unparseable → default
		{"?limit=0", 50},   // non-positive → default
		{"?limit=-3", 50},
		{"?limit=1", 1},
		{"?limit=200", 200},
		{"?limit=201", 200}, // above the ceiling → CLAMPED, not defaulted
		{"?limit=99999", 200},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/x"+c.query, nil)
		if got := httpx.Limit(r, 50, 200); got != c.want {
			t.Errorf("Limit(%q) = %d, want %d", c.query, got, c.want)
		}
	}
}
