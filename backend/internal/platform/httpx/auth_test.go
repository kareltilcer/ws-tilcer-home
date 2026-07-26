package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// injectActor stands in for the session middleware (platform/auth): it places a
// fixed actor in context so the role gates can be exercised in isolation. A nil
// actor leaves the request unauthenticated.
func injectActor(actor *reqctx.Actor) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if actor != nil {
				r = r.WithContext(reqctx.WithActor(r.Context(), *actor))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func protectedRouter(actor *reqctx.Actor) http.Handler {
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	r := chi.NewRouter()
	r.Group(func(g chi.Router) {
		g.Use(injectActor(actor))
		g.Get("/read", ok)                            // no role gate — any authenticated user
		g.With(httpx.RequireWrite).Post("/write", ok) // editor/admin
		g.With(httpx.RequireAdmin).Get("/logs", ok)   // admin only
	})
	return r
}

func do(t *testing.T, h http.Handler, method, path string) int {
	t.Helper()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(method, path, nil))
	return rr.Code
}

// Without an actor (session middleware would have rejected earlier), the role
// gates themselves refuse mutations with 401.
func TestRoleGates_NoActor(t *testing.T) {
	h := protectedRouter(nil)
	if code := do(t, h, http.MethodPost, "/write"); code != http.StatusUnauthorized {
		t.Errorf("no actor /write = %d, want 401", code)
	}
	if code := do(t, h, http.MethodGet, "/logs"); code != http.StatusUnauthorized {
		t.Errorf("no actor /logs = %d, want 401", code)
	}
}

func TestRoleGates_ByRole(t *testing.T) {
	cases := []struct {
		name                    string
		roles                   []string
		read, write, logsStatus int
	}{
		{"reader", []string{"reader"}, 200, 403, 403},
		{"editor", []string{"editor"}, 200, 200, 403},
		{"admin", []string{"admin"}, 200, 200, 200},
		{"superuser", []string{"*"}, 200, 200, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := protectedRouter(&reqctx.Actor{UserID: "u1", Type: "user", Roles: tc.roles})
			if code := do(t, h, http.MethodGet, "/read"); code != tc.read {
				t.Errorf("/read = %d, want %d", code, tc.read)
			}
			if code := do(t, h, http.MethodPost, "/write"); code != tc.write {
				t.Errorf("/write = %d, want %d", code, tc.write)
			}
			if code := do(t, h, http.MethodGet, "/logs"); code != tc.logsStatus {
				t.Errorf("/logs = %d, want %d", code, tc.logsStatus)
			}
		})
	}
}
