package audit_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/testsupport"
)

type roleIntrospector struct{ roles []string }

func (r roleIntrospector) Introspect(context.Context, string) (auth.Claims, error) {
	return auth.Claims{Active: true, UserID: "u", Roles: r.roles}, nil
}

func logRouter(t *testing.T, roles ...string) http.Handler {
	t.Helper()
	db := testsupport.NewDB(t)
	// Seed a couple of events so 200 responses are non-empty.
	record(t, db, testsupport.CtxUser("marie", "admin"),
		audit.Event{Module: "todo", Action: "card.create", EntityType: "card", EntityID: "c1", Summary: "vytvořena karta"})
	record(t, db, testsupport.CtxUser("marie", "admin"),
		audit.Event{Module: "events", Action: "event.create", EntityType: "event", EntityID: "e1", Summary: "vytvořena událost"})

	logs := audit.NewHTTPHandler(audit.NewStore(db))
	return httpx.NewRouter(httpx.Deps{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		DB:     db,
		Site:   "home",
		Auth:   httpx.AuthConfig{Introspector: roleIntrospector{roles: roles}},
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
	r.Header.Set("Authorization", "Bearer tok")
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
	var page audit.EventPage
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
