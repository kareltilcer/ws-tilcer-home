package todo_test

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
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/testsupport"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/todo"
)

type roleIntrospector struct{ roles []string }

func (r roleIntrospector) Introspect(context.Context, string) (auth.Claims, error) {
	return auth.Claims{Active: true, UserID: "u", Roles: r.roles}, nil
}

func router(t *testing.T, roles ...string) http.Handler {
	t.Helper()
	db := testsupport.NewDB(t)
	h := todo.NewHandler(todo.NewService(db, audit.NewSink(), nil))
	return httpx.NewRouter(httpx.Deps{
		Logger:   slog.New(slog.NewJSONHandler(io.Discard, nil)),
		DB:       db,
		Site:     "home",
		Auth:     httpx.AuthConfig{Introspector: roleIntrospector{roles: roles}},
		MountAPI: func(api chi.Router) { h.Mount(api) },
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
	r.Header.Set("Authorization", "Bearer tok")
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
