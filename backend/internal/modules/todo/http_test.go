package todo_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/todo"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

func router(t *testing.T, roles ...string) http.Handler {
	t.Helper()
	db := testsupport.NewDB(t)
	h := todo.NewHandler(todo.NewService(db, audit.NewSink(), nil))
	return httpx.NewRouter(httpx.Deps{
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		DB:        db,
		Site:      "home",
		SessionMW: auth.NewSessionAuth(auth.Config{BypassActor: &reqctx.Actor{UserID: "u", Type: "user", Roles: roles}}),
		MountAPI:  func(api chi.Router) { h.Mount(api) },
	})
}

func send(t *testing.T, h http.Handler, method, path, body string) int {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr.Code
}

func TestTodoHTTP_RoleGating(t *testing.T) {
	reader := router(t, "reader")
	if code := send(t, reader, http.MethodGet, "/api/boards", ""); code != http.StatusOK {
		t.Errorf("reader GET /api/boards = %d, want 200", code)
	}
	if code := send(t, reader, http.MethodPost, "/api/boards", `{"name":"X"}`); code != http.StatusForbidden {
		t.Errorf("reader POST /api/boards = %d, want 403", code)
	}

	editor := router(t, "editor")
	if code := send(t, editor, http.MethodPost, "/api/boards", `{"name":"Domácnost"}`); code != http.StatusCreated {
		t.Errorf("editor POST /api/boards = %d, want 201", code)
	}
}
