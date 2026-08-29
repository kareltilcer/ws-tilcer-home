package testsupport

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// RouterAs assembles the REAL application router around one module's routes,
// authenticated as `actor` — the same httpx.NewRouter main.go builds, so a test
// exercises the actual middleware chain (role gates, error mapping, request
// logging) rather than a handler in isolation. That is the point of mounting it
// this way, and it is why every module's HTTP test did it by hand.
//
// ⚠ IT LIVES HERE BECAUSE THE SAME httpx.Deps LITERAL EXISTED TWELVE TIMES, in
// ten packages — `todo`, `events`, `finance`, `electricity`, `documents` (×2),
// `notes`, `chat` (×2), `admin`, `logging` and `platform/push` — identical apart
// from which handler it mounts and who the caller is. Both are parameters here.
//
// ⚠ CSRFMW IS DELIBERATELY LEFT NIL, as all twelve copies left it. httpx.NewRouter
// documents nil as "no CSRF check", which is what a test wants: the double-submit
// token is a browser mechanism, and requiring it here would test the test harness.
//
// Four router literals in the test tree are NOT this one, and each has a reason:
// `dashboard`'s layout PUT sets CSRFMW because the CSRF path is what it asserts;
// `documents` builds one with NO bypass actor (its 401 test) and one with no DB
// (re-mounting an existing store as a reader); and `platform/httpx`'s and
// `platform/auth`'s own suites test this router and that middleware, so building
// them from a helper would be assuming what they exist to check.
//
// The logger discards: a passing test should print nothing, and a failing one
// says what it wanted in its own message.
func RouterAs(t *testing.T, db *sql.DB, actor reqctx.Actor, mount func(chi.Router)) http.Handler {
	t.Helper()
	return httpx.NewRouter(httpx.Deps{
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		DB:        db,
		Site:      "home",
		SessionMW: auth.NewSessionAuth(auth.Config{BypassActor: &actor}),
		MountAPI:  mount,
	})
}

// Router is RouterAs for the common case: one anonymous member "u" holding
// `roles`. Most module tests care only about what a role may do, not about who
// is doing it; the tests that DO care — chat and documents' privacy suites, where
// two members must not see each other's rows — name their members with RouterAs.
func Router(t *testing.T, db *sql.DB, mount func(chi.Router), roles ...string) http.Handler {
	t.Helper()
	return RouterAs(t, db, reqctx.Actor{UserID: "u", Type: "user", Roles: roles}, mount)
}

// Send issues one request against h and returns the recorder.
//
// An EMPTY body means no body and no Content-Type, which is not the same as an
// empty JSON one: a GET with `Content-Type: application/json` and nothing after
// it is not what a browser sends, and the router's decoder would be entitled to
// say so. Every copy of this made that distinction; it is preserved here rather
// than simplified away.
func Send(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
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
	return rr
}
