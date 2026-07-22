package events_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/testsupport"
)

type roleIntrospector struct{ roles []string }

func (r roleIntrospector) Introspect(context.Context, string) (auth.Claims, error) {
	return auth.Claims{Active: true, UserID: "u", Roles: r.roles}, nil
}

func router(t *testing.T, roles ...string) http.Handler {
	t.Helper()
	db := testsupport.NewDB(t)
	h := events.NewHandler(events.NewService(db, audit.NewSink(), nil, 500, 24))
	return httpx.NewRouter(httpx.Deps{
		Logger:   slog.New(slog.NewJSONHandler(io.Discard, nil)),
		DB:       db,
		Site:     "home",
		Auth:     httpx.AuthConfig{Introspector: roleIntrospector{roles: roles}},
		MountAPI: func(api chi.Router) { h.Mount(api) },
	})
}

func send(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

// The static /events/occurrences route must win over /events/{id}.
func TestOccurrencesRouteResolvesBeforeID(t *testing.T) {
	h := router(t, "editor")
	rr := send(t, h, http.MethodGet, "/api/events/occurrences?from=2026-07-01&to=2026-07-31", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/events/occurrences = %d, want 200 (not parsed as an event id)", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "months") {
		t.Errorf("occurrences body missing 'months': %s", rr.Body.String())
	}
}

func TestEventsHTTP_RoleGating(t *testing.T) {
	reader := router(t, "reader")
	if rr := send(t, reader, http.MethodGet, "/api/events", ""); rr.Code != http.StatusOK {
		t.Errorf("reader GET /api/events = %d, want 200", rr.Code)
	}
	if rr := send(t, reader, http.MethodPost, "/api/events", `{"title":"X","starts_on":"2026-07-01"}`); rr.Code != http.StatusForbidden {
		t.Errorf("reader POST /api/events = %d, want 403", rr.Code)
	}
	editor := router(t, "editor")
	if rr := send(t, editor, http.MethodPost, "/api/events", `{"title":"Narozeniny","starts_on":"2026-07-01"}`); rr.Code != http.StatusCreated {
		t.Errorf("editor POST /api/events = %d, want 201", rr.Code)
	}
}
