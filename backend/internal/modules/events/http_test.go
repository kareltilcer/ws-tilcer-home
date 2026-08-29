package events_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

func router(t *testing.T, roles ...string) http.Handler {
	t.Helper()
	db := testsupport.NewDB(t)
	h := events.NewHandler(events.NewService(db, audit.NewSink(), nil, 500, 24))
	return testsupport.Router(t, db, h.Mount, roles...)
}

func send(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return testsupport.Send(t, h, method, path, body)
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
