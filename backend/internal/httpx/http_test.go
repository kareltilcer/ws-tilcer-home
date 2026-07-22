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

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/httpx"
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
