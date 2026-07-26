package auth_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// fakeAuthr is a scripted Authenticator (no network).
type fakeAuthr struct {
	loginID   auth.Identity
	loginErr  error
	mintID    auth.Identity
	mintErr   error
	mintCalls int
}

func (f *fakeAuthr) Login(context.Context, string, string) (auth.Identity, error) {
	return f.loginID, f.loginErr
}
func (f *fakeAuthr) Mint(context.Context, string) (auth.Identity, error) {
	f.mintCalls++
	return f.mintID, f.mintErr
}

const origin = "https://home.tilcer.cz"

type harness struct {
	router http.Handler
	fake   *fakeAuthr
	db     *sql.DB
	clock  time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db := testsupport.NewDB(t)
	h := &harness{db: db, clock: time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)}
	h.fake = &fakeAuthr{
		loginID: auth.Identity{UserID: "u1", Email: "marie@tilcer.cz", DisplayName: "Marie", Roles: []string{"editor"}},
		mintID:  auth.Identity{UserID: "u1", Email: "marie@tilcer.cz", Roles: []string{"editor"}},
	}
	cfg := auth.Config{
		Sessions:    auth.NewSessionStore(db),
		Authr:       h.fake,
		RoleRefresh: 15 * time.Minute,
		SessionTTL:  24 * time.Hour,
		Secure:      false,
		Origins:     []string{origin},
		Now:         func() time.Time { return h.clock },
		Logger:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	csrf := auth.NewCSRF(cfg.Origins, false)
	handler := auth.NewHandler(cfg, db, audit.NewSink())
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	h.router = httpx.NewRouter(httpx.Deps{
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		DB:        db,
		Site:      "home",
		MountAuth: func(api chi.Router) { handler.Mount(api, csrf) },
		SessionMW: auth.NewSessionAuth(cfg),
		CSRFMW:    csrf,
		MountAPI: func(api chi.Router) {
			api.Get("/things", ok)
			api.With(httpx.RequireWrite).Post("/things", ok)
		},
	})
	return h
}

func (h *harness) do(t *testing.T, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	return rr
}

func loginReq(email string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"email":"`+email+`","password":"x"}`))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func cookie(rr *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rr.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func countEvents(t *testing.T, db *sql.DB, action string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM audit_events WHERE action = ?", action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLogin_HappyPath_SetsCookiesAndAudits(t *testing.T) {
	h := newHarness(t)
	rr := h.do(t, loginReq("marie@tilcer.cz"))
	if rr.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	sess := cookie(rr, "session")
	csrf := cookie(rr, "csrf")
	if sess == nil || sess.Value == "" || !sess.HttpOnly {
		t.Fatalf("session cookie missing or not HttpOnly: %+v", sess)
	}
	if csrf == nil || csrf.Value == "" || csrf.HttpOnly {
		t.Fatalf("csrf cookie must exist and be JS-readable (not HttpOnly): %+v", csrf)
	}
	var body struct {
		User struct {
			ID    string   `json:"id"`
			Email string   `json:"email"`
			Roles []string `json:"roles"`
		} `json:"user"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body.User.ID != "u1" || body.User.Email != "marie@tilcer.cz" {
		t.Errorf("user = %+v, want u1/marie", body.User)
	}
	if n := countEvents(t, h.db, "login"); n != 1 {
		t.Errorf("platform.login events = %d, want 1", n)
	}
	// The raw token is never stored — only its hash.
	var stored int
	_ = h.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE token_hash = ?", sess.Value).Scan(&stored)
	if stored != 0 {
		t.Error("raw token found in token_hash column — token must be hashed at rest")
	}
}

func TestLogin_ErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"bad creds", auth.ErrBadCredentials, http.StatusUnauthorized},
		{"disabled", auth.ErrDisabled, http.StatusForbidden},
		{"mfa", auth.ErrMFARequired, http.StatusConflict},
		{"unreachable", auth.ErrUnreachable, http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.fake.loginErr = tc.err
			rr := h.do(t, loginReq("marie@tilcer.cz"))
			if rr.Code != tc.want {
				t.Errorf("login(%v) = %d, want %d", tc.err, rr.Code, tc.want)
			}
			if tc.err == auth.ErrMFARequired && !strings.Contains(rr.Body.String(), "mfa_required") {
				t.Errorf("mfa body = %s, want mfa_required", rr.Body.String())
			}
		})
	}
}

func TestLogin_RateLimited(t *testing.T) {
	h := newHarness(t)
	h.fake.loginErr = auth.ErrBadCredentials
	var last int
	for i := 0; i < 12; i++ {
		last = h.do(t, loginReq("marie@tilcer.cz")).Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("after many attempts last = %d, want 429", last)
	}
}

// Successful logins must never count toward the limiter, so a user cycling
// through many valid sign-ins in the window is not locked out of their account.
func TestLogin_SuccessDoesNotCountTowardRateLimit(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 15; i++ {
		if rr := h.do(t, loginReq("marie@tilcer.cz")); rr.Code != http.StatusOK {
			t.Fatalf("successful login %d = %d, want 200 (success must not count)", i, rr.Code)
		}
	}
}

// The dev bypass leaves Sessions and Authr nil; login must not touch them.
func TestLogin_DevBypass_NoSessionStore(t *testing.T) {
	db := testsupport.NewDB(t)
	cfg := auth.Config{
		BypassActor: &reqctx.Actor{UserID: "dev", Type: "user", Label: "Dev", Roles: []string{"*"}},
		SessionTTL:  24 * time.Hour,
		Origins:     []string{origin},
		Logger:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	csrf := auth.NewCSRF(cfg.Origins, true)
	handler := auth.NewHandler(cfg, db, audit.NewSink())
	router := httpx.NewRouter(httpx.Deps{
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		DB:        db,
		Site:      "home",
		MountAuth: func(api chi.Router) { handler.Mount(api, csrf) },
		SessionMW: auth.NewSessionAuth(cfg),
		CSRFMW:    csrf,
		MountAPI:  func(chi.Router) {},
	})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, loginReq("someone@tilcer.cz")) // would panic on nil Sessions.Create
	if rr.Code != http.StatusOK {
		t.Fatalf("dev bypass login = %d, want 200 (no panic); body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "someone@tilcer.cz") {
		t.Errorf("bypass login body = %s, want echoed email", rr.Body.String())
	}
}

// The sliding window (FR-A2) must re-issue the browser cookies with a fresh
// MaxAge, not only slide the DB row — otherwise the cookies still expire at
// login+SessionTTL regardless of activity.
func TestSession_SlidingWindow_ReissuesCookies(t *testing.T) {
	h := newHarness(t)
	sess, csrf := h.authed(t)
	login := h.clock

	// A request within touchGranularity must NOT re-issue cookies (avoids a
	// Set-Cookie on every request).
	req := httptest.NewRequest(http.MethodGet, "/api/things", nil)
	req.AddCookie(sess)
	req.AddCookie(csrf)
	if rr := h.do(t, req); cookie(rr, "session") != nil {
		t.Errorf("cookie re-issued within touch granularity, want none")
	}

	// After more than an hour of activity, both cookies are re-issued with a fresh
	// lifetime and the DB expiry slides past the original absolute expiry.
	h.clock = h.clock.Add(2 * time.Hour)
	req = httptest.NewRequest(http.MethodGet, "/api/things", nil)
	req.AddCookie(sess)
	req.AddCookie(csrf)
	rr := h.do(t, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("gated GET = %d, want 200", rr.Code)
	}
	if got := cookie(rr, "session"); got == nil || got.Value != sess.Value || got.MaxAge <= 0 {
		t.Fatalf("session cookie not re-issued with fresh MaxAge: %+v", got)
	}
	if got := cookie(rr, "csrf"); got == nil || got.Value != csrf.Value || got.MaxAge <= 0 {
		t.Errorf("csrf cookie not re-issued with fresh MaxAge: %+v", got)
	}
	var expires string
	if err := h.db.QueryRow("SELECT expires_at FROM sessions LIMIT 1").Scan(&expires); err != nil {
		t.Fatal(err)
	}
	exp, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		t.Fatalf("parse expires_at %q: %v", expires, err)
	}
	if !exp.After(login.Add(24 * time.Hour)) {
		t.Errorf("expires_at = %s, want slid past original %s", exp, login.Add(24*time.Hour))
	}
}

// authed logs in and returns the session + csrf cookies for reuse.
func (h *harness) authed(t *testing.T) (session, csrf *http.Cookie) {
	t.Helper()
	rr := h.do(t, loginReq("marie@tilcer.cz"))
	if rr.Code != http.StatusOK {
		t.Fatalf("setup login = %d", rr.Code)
	}
	return cookie(rr, "session"), cookie(rr, "csrf")
}

func TestSession_GatesAndBootstrap(t *testing.T) {
	h := newHarness(t)
	// No session → 401 on a gated read and on the bootstrap probe.
	if rr := h.do(t, httptest.NewRequest(http.MethodGet, "/api/things", nil)); rr.Code != http.StatusUnauthorized {
		t.Errorf("no session GET /api/things = %d, want 401", rr.Code)
	}
	if rr := h.do(t, httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)); rr.Code != http.StatusUnauthorized {
		t.Errorf("no session bootstrap = %d, want 401", rr.Code)
	}

	sess, _ := h.authed(t)
	req := httptest.NewRequest(http.MethodGet, "/api/things", nil)
	req.AddCookie(sess)
	if rr := h.do(t, req); rr.Code != http.StatusOK {
		t.Errorf("valid session GET /api/things = %d, want 200", rr.Code)
	}
	// Bootstrap reflects the user.
	req = httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.AddCookie(sess)
	rr := h.do(t, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "marie@tilcer.cz") {
		t.Errorf("bootstrap = %d %s, want 200 with user", rr.Code, rr.Body.String())
	}
}

// A user whose auth identity has no display name is stored with display_name =
// NULL (Create uses nullStr). Lookup must scan that NULL without erroring —
// regression for the 500 where display_name was scanned into a plain string.
func TestSession_NullDisplayName_NoError(t *testing.T) {
	h := newHarness(t)
	h.fake.loginID = auth.Identity{UserID: "u1", Email: "marie@tilcer.cz", Roles: []string{"editor"}}

	rr := h.do(t, loginReq("marie@tilcer.cz"))
	if rr.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200", rr.Code)
	}
	sess := cookie(rr, "session")

	// Confirm the row really stored NULL, so the test exercises the NULL path.
	var dn sql.NullString
	if err := h.db.QueryRow("SELECT display_name FROM sessions LIMIT 1").Scan(&dn); err != nil {
		t.Fatal(err)
	}
	if dn.Valid {
		t.Fatalf("display_name = %q, want NULL", dn.String)
	}

	// Bootstrap must succeed (200), not 500. Gated reads must also work.
	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.AddCookie(sess)
	if rr := h.do(t, req); rr.Code != http.StatusOK {
		t.Fatalf("bootstrap with NULL display_name = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/things", nil)
	req.AddCookie(sess)
	if rr := h.do(t, req); rr.Code != http.StatusOK {
		t.Errorf("gated GET with NULL display_name = %d, want 200", rr.Code)
	}
}

func TestSession_RoleRefreshFailClosed(t *testing.T) {
	h := newHarness(t)
	sess, _ := h.authed(t)

	// Before the threshold: no mint, still authorized.
	req := httptest.NewRequest(http.MethodGet, "/api/things", nil)
	req.AddCookie(sess)
	if rr := h.do(t, req); rr.Code != http.StatusOK {
		t.Fatalf("pre-threshold = %d, want 200", rr.Code)
	}
	if h.fake.mintCalls != 0 {
		t.Fatalf("mint called %d times before threshold, want 0", h.fake.mintCalls)
	}

	// Advance past the refresh threshold and make the user closed in auth.
	h.clock = h.clock.Add(20 * time.Minute)
	h.fake.mintErr = auth.ErrUserClosed
	req = httptest.NewRequest(http.MethodGet, "/api/things", nil)
	req.AddCookie(sess)
	if rr := h.do(t, req); rr.Code != http.StatusUnauthorized {
		t.Fatalf("fail-closed mint = %d, want 401", rr.Code)
	}
	if h.fake.mintCalls != 1 {
		t.Errorf("mint calls = %d, want 1", h.fake.mintCalls)
	}
	// Session is now revoked: even a fresh request stays out.
	req = httptest.NewRequest(http.MethodGet, "/api/things", nil)
	req.AddCookie(sess)
	if rr := h.do(t, req); rr.Code != http.StatusUnauthorized {
		t.Errorf("after revoke = %d, want 401", rr.Code)
	}
}

func TestCSRF_OnMutations(t *testing.T) {
	h := newHarness(t)
	sess, csrf := h.authed(t)

	// Missing CSRF header → 403 even with a valid session (editor could write).
	req := httptest.NewRequest(http.MethodPost, "/api/things", nil)
	req.AddCookie(sess)
	req.AddCookie(csrf)
	req.Header.Set("Origin", origin)
	if rr := h.do(t, req); rr.Code != http.StatusForbidden {
		t.Errorf("missing csrf header = %d, want 403", rr.Code)
	}

	// Bad origin → 403.
	req = httptest.NewRequest(http.MethodPost, "/api/things", nil)
	req.AddCookie(sess)
	req.AddCookie(csrf)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("X-CSRF-Token", csrf.Value)
	if rr := h.do(t, req); rr.Code != http.StatusForbidden {
		t.Errorf("bad origin = %d, want 403", rr.Code)
	}

	// Matching token + allowed origin → passes CSRF (editor may write → 200).
	req = httptest.NewRequest(http.MethodPost, "/api/things", nil)
	req.AddCookie(sess)
	req.AddCookie(csrf)
	req.Header.Set("Origin", origin)
	req.Header.Set("X-CSRF-Token", csrf.Value)
	if rr := h.do(t, req); rr.Code != http.StatusOK {
		t.Errorf("valid csrf = %d, want 200", rr.Code)
	}
}

func TestLogout_RevokesAndAudits(t *testing.T) {
	h := newHarness(t)
	sess, csrf := h.authed(t)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(sess)
	req.AddCookie(csrf)
	req.Header.Set("Origin", origin)
	req.Header.Set("X-CSRF-Token", csrf.Value)
	if rr := h.do(t, req); rr.Code != http.StatusNoContent {
		t.Fatalf("logout = %d, want 204", rr.Code)
	}
	if n := countEvents(t, h.db, "logout"); n != 1 {
		t.Errorf("platform.logout events = %d, want 1", n)
	}
	// The session no longer authorizes.
	req = httptest.NewRequest(http.MethodGet, "/api/things", nil)
	req.AddCookie(sess)
	if rr := h.do(t, req); rr.Code != http.StatusUnauthorized {
		t.Errorf("after logout GET = %d, want 401", rr.Code)
	}
}
