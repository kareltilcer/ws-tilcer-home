package httpx_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/reqctx"
)

// spaRouter builds a router serving a throwaway SPA (index.html + one hashed
// asset) from a temp dir, with the auth bypass on so unmatched /api paths reach
// the JSON-404 fallback rather than stopping at the 401.
func spaRouter(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>INDEX-SHELL"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app-abc123.js"), []byte("APP-BUNDLE"), 0o644); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	return httpx.NewRouter(httpx.Deps{
		Logger:    logger,
		DB:        fakePinger{},
		Site:      "home",
		StaticDir: dir,
		Auth:      httpx.AuthConfig{BypassActor: &reqctx.Actor{UserID: "dev", Type: "user", Roles: []string{"admin"}}},
	})
}

func TestSPA_ServesHashedAssetImmutable(t *testing.T) {
	h := spaRouter(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if rr.Body.String() != "APP-BUNDLE" {
		t.Errorf("body = %q, want the asset bytes", rr.Body.String())
	}
	if cc := rr.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable for a hashed asset", cc)
	}
}

func TestSPA_ClientRouteFallsBackToIndex(t *testing.T) {
	h := spaRouter(t)
	for _, p := range []string{"/", "/ukoly", "/okno/some/deep/route"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, p, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", p, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "INDEX-SHELL") {
			t.Errorf("GET %s should fall back to index.html, got %q", p, rr.Body.String())
		}
		if cc := rr.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("GET %s Cache-Control = %q, want no-cache on the shell", p, cc)
		}
	}
}

func TestSPA_UnmatchedAPIReturnsJSON404NotShell(t *testing.T) {
	h := spaRouter(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON — an unmatched API path must not return the SPA shell", ct)
	}
	body := rr.Body.String()
	if strings.Contains(body, "INDEX-SHELL") {
		t.Errorf("unmatched /api path leaked the SPA shell: %s", body)
	}
	if !strings.Contains(body, "not_found") {
		t.Errorf("body = %q, want the shared error envelope", body)
	}
}

func TestSPA_NoStaticDirGivesJSON404(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	h := httpx.NewRouter(httpx.Deps{Logger: logger, DB: fakePinger{}, Site: "home"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/anything", nil))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when no SPA is configured", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}
}
