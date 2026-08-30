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
	// onMint runs inside Mint, so a test can make something happen in the window
	// between the session Lookup and the fail-closed revoke that follows.
	onMint func()
}

func (f *fakeAuthr) Login(context.Context, string, string) (auth.Identity, error) {
	return f.loginID, f.loginErr
}
func (f *fakeAuthr) Mint(context.Context, string) (auth.Identity, error) {
	f.mintCalls++
	if f.onMint != nil {
		f.onMint()
	}
	return f.mintID, f.mintErr
}

const origin = "https://home.tilcer.cz"

type harness struct {
	router http.Handler
	fake   *fakeAuthr
	db     *sql.DB
	clock  time.Time
	// cfg is the same Config the router was built from, kept so the tests can call
	// the decisions that have no HTTP surface — RevalidateSession is taken by the
	// websocket pump, not by a request.
	cfg auth.Config
	// revoked records every session id announced through OnSessionRevoked — the
	// hook the composition root points at ws.Hub.DisconnectSession.
	revoked []string
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
		OnSessionRevoked: func(sessionID string) {
			h.revoked = append(h.revoked, sessionID)
		},
	}
	h.cfg = cfg
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
			// Echoes the actor the session middleware resolved, which has no other
			// surface from outside the process — it is what the audit sink stamps on
			// every write, and a re-mint is allowed to change it mid-session.
			api.Get("/whoami", func(w http.ResponseWriter, r *http.Request) {
				a, _ := reqctx.ActorFrom(r.Context())
				_, _ = io.WriteString(w, a.Label)
			})
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

// revokedSessionIDs returns the ids of every session row marked revoked, so an
// announcement can be checked against what the database actually did.
func revokedSessionIDs(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query("SELECT id FROM sessions WHERE revoked_at IS NOT NULL ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
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
	// ⚠ And the revocation was ANNOUNCED. 401ing every HTTP request does nothing
	// to a websocket that was authenticated once at upgrade and has been trusted
	// ever since; this hook is what closes it. Nothing else in the suite covers
	// the call, so deleting it would leave the socket of a disabled account live
	// with every test still green.
	if want := revokedSessionIDs(t, h.db); len(want) != 1 || len(h.revoked) != 1 || h.revoked[0] != want[0] {
		t.Errorf("fail-closed re-mint announced %v, want exactly the revoked session %v", h.revoked, want)
	}
}

// ⚠ THE SESSION ROW IS THE ONLY RECORD HOME HAS OF WHO A MEMBER IS, and it used
// to be written exactly once, at login. `push.Store.Members` projects the whole
// household directory from it — the author label on every chat message, every
// members-panel row, the add-member picker, the "Vybraným lidem" audience and the
// delivery log — so somebody who renamed themselves in auth went on being shown to
// everybody under the old name until they next logged in, which behind a 90-day
// sliding session is effectively never. The re-mint that refreshes the roles has
// always returned the whole identity; everything but the roles was thrown away.
func TestSession_ReMintRefreshesTheCachedIdentity(t *testing.T) {
	h := newHarness(t)
	sess, _ := h.authed(t)

	// Renamed (and re-addressed) in auth after the session was created.
	h.fake.mintID = auth.Identity{
		UserID: "u1", Email: "marie.nova@tilcer.cz", DisplayName: "Marie Nová", Roles: []string{"admin"},
	}
	h.clock = h.clock.Add(20 * time.Minute)

	req := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	req.AddCookie(sess)
	rr := h.do(t, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("gated GET after the re-mint = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	// ⚠ The request that LEARNED the new name also acts under it. Labelling the
	// actor from the row that was looked up stamps the audit trail of the one
	// request that fetched the rename with the name it replaced, and every request
	// after it disagrees for no reason a reader of the log could see.
	if got := rr.Body.String(); got != "Marie Nová" {
		t.Errorf("actor label on the re-minting request = %q, want %q", got, "Marie Nová")
	}

	// And it landed in the row, which is what every other surface reads.
	var email string
	var dn sql.NullString
	if err := h.db.QueryRow("SELECT email, display_name FROM sessions LIMIT 1").Scan(&email, &dn); err != nil {
		t.Fatal(err)
	}
	if dn.String != "Marie Nová" || email != "marie.nova@tilcer.cz" {
		t.Errorf("session row = %q/%q, want the minted identity — the directory is projected "+
			"from this row and nothing else refreshes it", email, dn.String)
	}

	// The bootstrap probe is the frontend's own copy of the same row.
	req = httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.AddCookie(sess)
	rr = h.do(t, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("bootstrap = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		User struct {
			Email       string   `json:"email"`
			DisplayName *string  `json:"display_name"`
			Roles       []string `json:"roles"`
		} `json:"user"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body.User.DisplayName == nil || *body.User.DisplayName != "Marie Nová" || body.User.Email != "marie.nova@tilcer.cz" {
		t.Errorf("bootstrap user = %+v, want the refreshed identity", body.User)
	}
	if len(body.User.Roles) != 1 || body.User.Roles[0] != "admin" {
		t.Errorf("bootstrap roles = %v, want the minted [admin] — the roles half must not regress", body.User.Roles)
	}
}

// ⚠ AN ABSENT CLAIM IS NOT A CLEARED FIELD, AND HOME CANNOT TELL THEM APART FROM
// ONE TOKEN — so it treats the quiet one as "this token did not say". The two
// mistakes are not the same size. Reading an absent `name` as a clear would blank
// `display_name` on EVERY session in the household within one refresh window if
// auth's mint token ever stopped carrying it, and a blank name is exactly what the
// directory falls back FROM: the whole household would go back to being labelled
// by raw user ids, silently, with no deploy of Home to blame it on. Reading it the
// other way costs one member who erased their name in auth still being shown under
// it here until they log in again — which is what everybody had before any of this.
func TestSession_ReMintNeverClearsACachedField(t *testing.T) {
	h := newHarness(t)
	sess, _ := h.authed(t) // logged in as Marie / marie@tilcer.cz

	// A token that carries the address and no name.
	h.fake.mintID = auth.Identity{UserID: "u1", Email: "marie.nova@tilcer.cz", Roles: []string{"editor"}}
	h.clock = h.clock.Add(20 * time.Minute)
	req := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	req.AddCookie(sess)
	if rr := h.do(t, req); rr.Body.String() != "Marie" {
		t.Errorf("actor label = %q after a mint with no name, want the cached %q", rr.Body.String(), "Marie")
	}
	email, name := cachedIdentity(t, h.db)
	if name != "Marie" {
		t.Errorf("display_name = %q after a mint that carried none, want the cached %q", name, "Marie")
	}
	if email != "marie.nova@tilcer.cz" {
		t.Errorf("email = %q, want the minted one — a field the token DID carry still lands", email)
	}

	// And a token that carries neither leaves both exactly as they were.
	h.fake.mintID = auth.Identity{UserID: "u1", Roles: []string{"reader"}}
	h.clock = h.clock.Add(20 * time.Minute)
	req = httptest.NewRequest(http.MethodGet, "/api/things", nil)
	req.AddCookie(sess)
	if rr := h.do(t, req); rr.Code != http.StatusOK {
		t.Fatalf("gated GET = %d, want 200", rr.Code)
	}
	if email, name = cachedIdentity(t, h.db); email != "marie.nova@tilcer.cz" || name != "Marie" {
		t.Errorf("cached identity = %q/%q after an identity-less mint, want it untouched", email, name)
	}
	// The roles half is unconditional either way, or a demotion in auth would be
	// ignored by whichever token happened to omit an email.
	var roles string
	if err := h.db.QueryRow("SELECT roles FROM sessions LIMIT 1").Scan(&roles); err != nil {
		t.Fatal(err)
	}
	if roles != `["reader"]` {
		t.Errorf("roles = %s, want [\"reader\"] — the roles are replaced by every successful mint", roles)
	}
}

// cachedIdentity reads the identity the session row currently holds — the one
// every directory in the app is projected from.
func cachedIdentity(t *testing.T, db *sql.DB) (email, displayName string) {
	t.Helper()
	var dn sql.NullString
	if err := db.QueryRow("SELECT email, display_name FROM sessions LIMIT 1").Scan(&email, &dn); err != nil {
		t.Fatal(err)
	}
	return email, dn.String
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
	// ⚠ And the revocation was ANNOUNCED, so the member's open sockets close with
	// it. Without this assertion the hook could be deleted and the whole suite
	// would still pass, while a logged-out tab kept receiving private payloads.
	if want := revokedSessionIDs(t, h.db); len(want) != 1 || len(h.revoked) != 1 || h.revoked[0] != want[0] {
		t.Errorf("logout announced %v, want exactly the revoked session %v", h.revoked, want)
	}
}

// TestLogout_AnnouncesOnlyTheDeviceThatLoggedOut. Revocation is per-session and
// the announcement has to be too.
//
// ⚠ Logout revokes the CALLING device's token and nothing else. Announcing the
// member instead of the session would close their sockets everywhere — the
// laptop's session is untouched and still valid, and it would lose every frame
// published before its reconnect backoff completed, on every logout.
func TestLogout_AnnouncesOnlyTheDeviceThatLoggedOut(t *testing.T) {
	h := newHarness(t)
	phone, phoneCSRF := h.authed(t)
	laptop, _ := h.authed(t) // same member, second device, second session

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(phone)
	req.AddCookie(phoneCSRF)
	req.Header.Set("Origin", origin)
	req.Header.Set("X-CSRF-Token", phoneCSRF.Value)
	if rr := h.do(t, req); rr.Code != http.StatusNoContent {
		t.Fatalf("logout = %d, want 204", rr.Code)
	}

	revoked := revokedSessionIDs(t, h.db)
	if len(revoked) != 1 {
		t.Fatalf("%d session rows revoked, want 1 — logout must not touch the other device", len(revoked))
	}
	if len(h.revoked) != 1 || h.revoked[0] != revoked[0] {
		t.Errorf("announced %v, want exactly the one revoked session %v", h.revoked, revoked)
	}
	// And the other device is genuinely still signed in.
	req = httptest.NewRequest(http.MethodGet, "/api/things", nil)
	req.AddCookie(laptop)
	if rr := h.do(t, req); rr.Code != http.StatusOK {
		t.Errorf("the other device = %d after logging out elsewhere, want 200", rr.Code)
	}
}
