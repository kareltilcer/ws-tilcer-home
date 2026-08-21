package logging_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/logging"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

func logRouter(t *testing.T, roles ...string) http.Handler {
	t.Helper()
	db := testsupport.NewDB(t)
	// Seed a couple of events so 200 responses are non-empty.
	record(t, db, testsupport.CtxUser("marie", "admin"),
		audit.Event{Module: "todo", Action: "card.create", EntityType: "card", EntityID: "c1", Summary: "vytvořena karta"})
	record(t, db, testsupport.CtxUser("marie", "admin"),
		audit.Event{Module: "events", Action: "event.create", EntityType: "event", EntityID: "e1", Summary: "vytvořena událost"})

	logs := logging.NewHTTPHandler(logging.NewStore(db))
	return httpx.NewRouter(httpx.Deps{
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		DB:        db,
		Site:      "home",
		SessionMW: auth.NewSessionAuth(auth.Config{BypassActor: &reqctx.Actor{UserID: "u", Type: "user", Roles: roles}}),
		MountAPI: func(api chi.Router) {
			api.Route("/logs", func(r chi.Router) {
				r.Use(httpx.RequireAdmin)
				logs.Mount(r)
			})
		},
	})
}

func req(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

func TestLogHTTP_RoleGating(t *testing.T) {
	for _, role := range []string{"reader", "editor"} {
		h := logRouter(t, role)
		if rr := req(t, h, http.MethodGet, "/api/logs"); rr.Code != http.StatusForbidden {
			t.Errorf("%s GET /api/logs = %d, want 403", role, rr.Code)
		}
	}
	h := logRouter(t, "admin")
	if rr := req(t, h, http.MethodGet, "/api/logs"); rr.Code != http.StatusOK {
		t.Fatalf("admin GET /api/logs = %d, want 200", rr.Code)
	}
}

func TestLogHTTP_ListAndStats(t *testing.T) {
	h := logRouter(t, "admin")

	rr := req(t, h, http.MethodGet, "/api/logs")
	if rr.Code != http.StatusOK {
		t.Fatalf("list = %d", rr.Code)
	}
	var page logging.EventPage
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Errorf("list items = %d, want 2", len(page.Items))
	}

	// Filter narrows results.
	rr = req(t, h, http.MethodGet, "/api/logs?module=todo")
	_ = json.Unmarshal(rr.Body.Bytes(), &page)
	if len(page.Items) != 1 {
		t.Errorf("module=todo items = %d, want 1", len(page.Items))
	}

	// Stats requires a dimension.
	if rr := req(t, h, http.MethodGet, "/api/logs/stats"); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("stats without dimension = %d, want 422", rr.Code)
	}
	if rr := req(t, h, http.MethodGet, "/api/logs/stats?dimension=module"); rr.Code != http.StatusOK {
		t.Errorf("stats dimension=module = %d, want 200", rr.Code)
	}
}

// A JSON null where the client expects an array crashes the log detail view, so
// empty collections must serialise as [] on every log endpoint.
func TestLogHTTP_EmptyCollectionsAreArrays(t *testing.T) {
	h := logRouter(t, "admin")

	rr := req(t, h, http.MethodGet, "/api/logs")
	var page logging.EventPage
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) == 0 {
		t.Fatal("expected the seeded events")
	}

	// The seeded events carry no field changes.
	rr = req(t, h, http.MethodGet, "/api/logs/"+page.Items[0].ID)
	if rr.Code != http.StatusOK {
		t.Fatalf("detail = %d", rr.Code)
	}
	if body := rr.Body.String(); !strings.Contains(body, `"changes":[]`) {
		t.Errorf("detail = %s, want changes as an empty array", body)
	}

	for _, path := range []string{"/api/logs?module=zadny", "/api/logs/entity/card/neznama"} {
		rr := req(t, h, http.MethodGet, path)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, rr.Code)
		}
		if body := rr.Body.String(); !strings.Contains(body, `"items":[]`) {
			t.Errorf("GET %s = %s, want items as an empty array", path, body)
		}
	}

	// Stats over a range that predates every event: both collections stay arrays.
	rr = req(t, h, http.MethodGet, "/api/logs/stats?dimension=module&from=2000-01-01T00:00:00Z&to=2000-01-02T00:00:00Z")
	if rr.Code != http.StatusOK {
		t.Fatalf("stats = %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"buckets":[]`) || !strings.Contains(body, `"totals":[]`) {
		t.Errorf("stats = %s, want buckets and totals as empty arrays", body)
	}
}
