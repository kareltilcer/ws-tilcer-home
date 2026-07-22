package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/reqctx"
)

// fakeIntrospector returns fixed claims and counts calls (for the cache test).
type fakeIntrospector struct {
	claims auth.Claims
	err    error
	calls  int
}

func (f *fakeIntrospector) Introspect(context.Context, string) (auth.Claims, error) {
	f.calls++
	return f.claims, f.err
}

func protectedRouter(ac httpx.AuthConfig) http.Handler {
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	r := chi.NewRouter()
	r.Group(func(g chi.Router) {
		g.Use(httpx.Authenticate(ac))
		g.Get("/read", ok)
		g.With(httpx.RequireWrite).Post("/write", ok)
		g.With(httpx.RequireAdmin).Get("/logs", ok)
	})
	return r
}

func do(t *testing.T, h http.Handler, method, path, token string) int {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Code
}

func activeClaims(roles ...string) auth.Claims {
	return auth.Claims{Active: true, UserID: "u1", Roles: roles, Label: "u1"}
}

func TestAuth_NoToken(t *testing.T) {
	h := protectedRouter(httpx.AuthConfig{Introspector: &fakeIntrospector{claims: activeClaims("admin")}})
	if code := do(t, h, http.MethodGet, "/read", ""); code != http.StatusUnauthorized {
		t.Errorf("no token /read = %d, want 401", code)
	}
}

func TestAuth_InactiveToken(t *testing.T) {
	h := protectedRouter(httpx.AuthConfig{Introspector: &fakeIntrospector{claims: auth.Claims{Active: false}}})
	if code := do(t, h, http.MethodGet, "/read", "tok"); code != http.StatusUnauthorized {
		t.Errorf("inactive token /read = %d, want 401", code)
	}
}

func TestAuth_RoleGating(t *testing.T) {
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
			h := protectedRouter(httpx.AuthConfig{Introspector: &fakeIntrospector{claims: activeClaims(tc.roles...)}})
			if code := do(t, h, http.MethodGet, "/read", "tok"); code != tc.read {
				t.Errorf("/read = %d, want %d", code, tc.read)
			}
			if code := do(t, h, http.MethodPost, "/write", "tok"); code != tc.write {
				t.Errorf("/write = %d, want %d", code, tc.write)
			}
			if code := do(t, h, http.MethodGet, "/logs", "tok"); code != tc.logsStatus {
				t.Errorf("/logs = %d, want %d", code, tc.logsStatus)
			}
		})
	}
}

func TestAuth_DevBypass(t *testing.T) {
	bypass := &reqctx.Actor{UserID: "dev-user", Type: "user", Roles: []string{"admin"}}
	h := protectedRouter(httpx.AuthConfig{BypassActor: bypass})
	// No token at all, yet admin-only route is reachable.
	if code := do(t, h, http.MethodGet, "/logs", ""); code != http.StatusOK {
		t.Errorf("dev bypass /logs = %d, want 200", code)
	}
}

func TestAuth_CacheAvoidsSecondIntrospect(t *testing.T) {
	inner := &fakeIntrospector{claims: activeClaims("editor")}
	cached := auth.NewCachingIntrospector(inner, 15*time.Minute)
	h := protectedRouter(httpx.AuthConfig{Introspector: cached})

	for i := 0; i < 3; i++ {
		if code := do(t, h, http.MethodGet, "/read", "same-token"); code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200", i, code)
		}
	}
	if inner.calls != 1 {
		t.Errorf("introspect called %d times, want 1 (cache miss then hits)", inner.calls)
	}
}
