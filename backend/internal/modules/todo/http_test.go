package todo_test

import (
	"net/http"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/todo"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

func router(t *testing.T, roles ...string) http.Handler {
	t.Helper()
	db := testsupport.NewDB(t)
	h := todo.NewHandler(todo.NewService(db, audit.NewSink(), nil))
	return testsupport.Router(t, db, h.Mount, roles...)
}

// send keeps todo's status-code-only shape: every assertion in this file is
// about the code, and threading a recorder through them to read `.Code` at each
// one would be noise.
func send(t *testing.T, h http.Handler, method, path, body string) int {
	t.Helper()
	return testsupport.Send(t, h, method, path, body).Code
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
