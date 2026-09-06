package mcp_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/notes"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/todo"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// The v11 test harness.
//
// ⚠ IT ASSEMBLES THE REAL THING — the real router, the real bearer middleware,
// the real three providers over a real migrated database — because every one of
// these tests is written from the ATTACKER's side, and a leak test against a
// mocked provider tests the mock. HANDOFF-13 §16.1: write the adversarial ones
// first; they are cheap against three modules and expensive against nine.
//
// ⚠ THE AUTH SERVICE IS ABSENT AND THAT IS THE PRODUCTION PATH, not a shortcut.
// With `Authr` nil the token path reads the member's identity out of `sessions`,
// which is exactly what it does between re-mints — so a test that wants a
// `reader`'s token gives that member a session row saying `reader`, as a login
// would.

type harness struct {
	t       *testing.T
	db      *sql.DB
	handler http.Handler
	tokens  *auth.MCPTokenStore
	notes   *notes.Service
	todo    *todo.Service
}

const (
	memberA = "user-a"
	memberB = "user-b"
)

// harnessOptions are the knobs a test turns. Both default to "production".
type harnessOptions struct {
	cfg         func(*mcp.Config)
	logs        io.Writer
	apiRoles    []string
	authr       auth.Authenticator
	roleRefresh time.Duration
}

type harnessOpt func(*harnessOptions)

// withConfig overrides the MCP configuration a harness is built with.
func withConfig(fn func(*mcp.Config)) harnessOpt {
	return func(o *harnessOptions) { o.cfg = fn }
}

// withLogSink captures every log line the router and the host write.
//
// ⚠ IT IS ONE SINK FOR BOTH, deliberately: the crash board is fed by the slog
// handler, so "did the token reach a logger" is one question, not two.
func withLogSink(w io.Writer) harnessOpt {
	return func(o *harnessOptions) { o.logs = w }
}

// withAuthenticator gives the token path a real auth service to re-mint against,
// with the re-mint threshold the test wants.
//
// ⚠ WITHOUT IT THE HARNESS RUNS WITH Authr NIL, which is the PRODUCTION path
// between re-mints: the identity comes out of `sessions`. The tests that need this
// are the ones about what a re-mint DECIDES — a closed account, an outage.
//
// ⚠ A ZERO WINDOW IS STORED AS A NEGATIVE ONE, AND THAT IS A BUG FIX RATHER THAN
// A STYLE. Both doors ask `now.Sub(refreshedAt) <= RoleRefresh`, so a window of
// exactly zero does NOT mean "every call is past the threshold": a harness seeds
// its session rows and then calls, and when the stamp and the call land in the
// same tick of a coarse wall clock the difference is 0, `0 <= 0` holds, and the
// call is served from cache with no re-mint at all. It showed up as
// TestClosedAccountRevokesTheTokenRow passing alone and failing after other tests
// had shifted where in the tick it started — the worst shape of red there is. A
// negative window is what "always past the threshold" actually spells.
func withAuthenticator(a auth.Authenticator, refresh time.Duration) harnessOpt {
	if refresh == 0 {
		refresh = -time.Second
	}
	return func(o *harnessOptions) { o.authr, o.roleRefresh = a, refresh }
}

// withAPIRoles authenticates the /api half under a different role set.
func withAPIRoles(roles ...string) harnessOpt {
	return func(o *harnessOptions) { o.apiRoles = roles }
}

func newHarness(t *testing.T, opts ...harnessOpt) *harness {
	t.Helper()
	var o harnessOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.apiRoles == nil {
		o.apiRoles = []string{"admin"}
	}
	logs := o.logs
	if logs == nil {
		logs = io.Discard
	}
	logger := slog.New(slog.NewJSONHandler(logs, nil))

	db := testsupport.NewDB(t)
	sink := audit.NewSink()
	notify := func(context.Context, string, any) {}

	todoSvc := todo.NewService(db, sink, notify)
	eventsSvc := events.NewService(db, sink, notify, 500, 24)
	notesSvc := notes.NewService(db, sink, notify, nil, notes.ImageOptions{}, logger)

	loc, err := time.LoadLocation("Europe/Prague")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	todoMod := todo.NewModule(todoSvc)
	eventsMod := events.NewModule(eventsSvc, loc, 30)
	notesMod := notes.NewModule(notesSvc)

	registry, err := mcp.Collect(todoMod, eventsMod, notesMod)
	if err != nil {
		t.Fatalf("collect mcp providers: %v", err)
	}
	metricReg, err := metrics.Collect(todoMod, eventsMod, notesMod)
	if err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	listReg, err := lists.Collect(todoMod, eventsMod, notesMod)
	if err != nil {
		t.Fatalf("collect lists: %v", err)
	}

	tokens := auth.NewMCPTokenStore(db)
	cfg := mcp.Config{
		Enabled:          true,
		RatePerMin:       120,
		CallTimeout:      10 * time.Second,
		MaxResultBytes:   256 << 10,
		MaxTokensPerUser: 10,
		SearchLimit:      10,
		AllowedOrigins:   []string{"https://home.tilcer.cz"},
		ServerVersion:    "test",
		Timezone:         loc,
		TimezoneName:     "Europe/Prague",
	}
	if o.cfg != nil {
		o.cfg(&cfg)
	}

	refresh := time.Hour
	if o.authr != nil {
		refresh = o.roleRefresh
	}
	authCfg := auth.Config{
		Sessions:    auth.NewSessionStore(db),
		Authr:       o.authr,
		RoleRefresh: refresh,
		SessionTTL:  24 * time.Hour,
		Logger:      logger,
	}
	host := mcp.New(mcp.Deps{
		Registry: registry,
		Auth:     auth.MCPAuth{Cfg: authCfg, Tokens: tokens},
		Tokens:   tokens,
		DB:       db,
		Sink:     sink,
		Activity: audit.NewActivityReader(db),
		Metrics:  metricReg,
		Lists:    listReg,
		Names: func(context.Context) (map[string]string, error) {
			return map[string]string{memberA: "Karel", memberB: "Petra"}, nil
		},
		Config: cfg,
		Logger: logger,
	})

	h := &harness{t: t, db: db, tokens: tokens, notes: notesSvc, todo: todoSvc}
	h.handler = httpx.NewRouter(httpx.Deps{
		Logger:   logger,
		DB:       db,
		Site:     "home",
		MountMCP: host.Mount,
		// The /api half is mounted as member A. These tests drive the MCP door and
		// use the REST routes only for the token-management assertions; the ones
		// that care who is calling on the REST side build their own router.
		SessionMW: auth.NewSessionAuth(auth.Config{BypassActor: &reqctx.Actor{
			UserID: memberA, Type: "user", Label: "Karel", Roles: o.apiRoles,
		}}),
		MountAPI: func(api chi.Router) {
			host.MountTokens(api)
			todoMod.RegisterRoutes(api)
			eventsMod.RegisterRoutes(api)
			notesMod.RegisterRoutes(api)
		},
		// ⚠ StaticDir IS SET, and it has to be for leak row 2 to mean anything: the
		// bug guarded against is a mistyped /mcp path falling through to the SPA, and
		// a router with no SPA cannot exhibit it.
		StaticDir: spaDir(t),
	})
	h.seedSession(memberA, "Karel", "admin")
	h.seedSession(memberB, "Petra", "editor")
	return h
}

// spaDir writes a one-file SPA so the router has something to fall through TO.
func spaDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/index.html", []byte("<!doctype html><title>home</title>"), 0o600); err != nil {
		t.Fatalf("write spa: %v", err)
	}
	return dir
}

// seedSession gives a member the identity projection the token path reads.
func (h *harness) seedSession(userID, displayName string, roles ...string) {
	h.t.Helper()
	store := auth.NewSessionStore(h.db)
	ctx := context.Background()
	err := appdb.WithTx(ctx, h.db, func(tx *sql.Tx) error {
		_, _, err := store.Create(ctx, tx, auth.Identity{
			UserID:      userID,
			Email:       userID + "@example.test",
			DisplayName: displayName,
			Roles:       roles,
		}, "test", "127.0.0.1", 24*time.Hour, time.Now())
		return err
	})
	if err != nil {
		h.t.Fatalf("seed session for %s: %v", userID, err)
	}
}

// mintToken inserts a token for userID and returns its secret and row.
//
// ⚠ IT GOES THROUGH THE STORE, NOT THROUGH THE REST ROUTE, so a test can mint for
// member B without pretending to be B on the /api side.
func (h *harness) mintToken(userID, name string, modules []string, expiresAt time.Time) (string, auth.MCPToken) {
	h.t.Helper()
	secret, hash, prefix, err := auth.NewMCPSecret()
	if err != nil {
		h.t.Fatalf("new secret: %v", err)
	}
	ctx := testsupport.CtxUser(userID, "admin")
	var tok auth.MCPToken
	err = appdb.WithTx(ctx, h.db, func(tx *sql.Tx) error {
		created, cerr := h.tokens.Create(ctx, tx, userID, name, hash, prefix, modules, expiresAt, time.Now())
		tok = created
		return cerr
	})
	if err != nil {
		h.t.Fatalf("mint token: %v", err)
	}
	return secret, tok
}

// ---- request helpers ----

// rpc posts one JSON-RPC call with a bearer.
func (h *harness) rpc(bearer, method string, params any) *httptest.ResponseRecorder {
	h.t.Helper()
	return h.rpcWith(func(r *http.Request) {
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
	}, method, params)
}

func (h *harness) rpcWith(decorate func(*http.Request), method string, params any) *httptest.ResponseRecorder {
	h.t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		body["params"] = params
	}
	raw, err := json.Marshal(body)
	if err != nil {
		h.t.Fatalf("marshal rpc: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	decorate(req)
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, req)
	return rr
}

// rpcNotification posts one call with NO id — a JSON-RPC notification, which
// MUST NOT be answered with a response object.
func (h *harness) rpcNotification(bearer, method string) *httptest.ResponseRecorder {
	h.t.Helper()
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method})
	if err != nil {
		h.t.Fatalf("marshal notification: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, req)
	return rr
}

// call runs one tool and returns the response plus the decoded result object.
func (h *harness) call(bearer, tool string, args any) (*httptest.ResponseRecorder, map[string]any) {
	h.t.Helper()
	params := map[string]any{"name": tool}
	if args != nil {
		params["arguments"] = args
	}
	rr := h.rpc(bearer, "tools/call", params)
	return rr, decodeResult(h.t, rr)
}

// api issues a request against the /api half, as member A.
func (h *harness) api(method, path, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, req)
	return rr
}

// ---- fixtures ----

// createPrivateNoteAs writes a note into userID's OWN private root, through the
// module's real service so the audit event carries the ownership marker the
// redaction rules key off.
func (h *harness) createPrivateNoteAs(userID, title, body string) string {
	h.t.Helper()
	return h.createNoteAs(userID, title, body, "private")
}

// createSharedNoteAs writes a note into the household's shared tree.
func (h *harness) createSharedNoteAs(userID, title, body string) string {
	h.t.Helper()
	return h.createNoteAs(userID, title, body, "shared")
}

func (h *harness) createNoteAs(userID, title, body, scope string) string {
	h.t.Helper()
	ctx := testsupport.CtxUser(userID, "editor")
	note, err := h.notes.CreateNote(ctx, notes.NoteCreate{Title: title, BodyMD: body, Scope: scope})
	if err != nil {
		h.t.Fatalf("create %s note for %s: %v", scope, userID, err)
	}
	return note.ID
}

// newHarnessCapturingLogs is newHarness with every log line kept, for row 14.
func newHarnessCapturingLogs(t *testing.T) (*harness, *syncBuffer) {
	t.Helper()
	buf := &syncBuffer{}
	return newHarness(t, withLogSink(buf)), buf
}

// syncBuffer is a bytes.Buffer safe for the handler's goroutines to share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// ---- decoding helpers ----

type rpcEnvelope struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeEnvelope(t *testing.T, rr *httptest.ResponseRecorder) rpcEnvelope {
	t.Helper()
	var env rpcEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope (%d): %v\n%s", rr.Code, err, rr.Body.String())
	}
	return env
}

// decodeResult returns the tools/call result object, or nil when the call came
// back as a protocol error.
func decodeResult(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	env := decodeEnvelope(t, rr)
	if env.Error != nil || len(env.Result) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(env.Result, &out); err != nil {
		t.Fatalf("decode result: %v\n%s", err, rr.Body.String())
	}
	return out
}

// resultText flattens a tools/call result's content blocks.
func resultText(t *testing.T, result map[string]any) string {
	t.Helper()
	if result == nil {
		t.Fatal("no result object — the call came back as a protocol error")
	}
	blocks, _ := result["content"].([]any)
	var b strings.Builder
	for _, raw := range blocks {
		block, _ := raw.(map[string]any)
		text, _ := block["text"].(string)
		b.WriteString(text)
	}
	return b.String()
}

// searchTitles reads the TITLES out of a home_search result.
//
// ⚠ IT READS structuredContent AND NOT THE TEXT, and the first draft of the
// viewer-scoping test found out why the hard way: the text block echoes the
// QUERY back — `Nalezeno 0 položek pro "Zmrzlina"` — so a `strings.Contains`
// over it matches the search term whether or not anything was found. The test
// failed for the right reason and against the wrong evidence.
func searchTitles(t *testing.T, result map[string]any) []string {
	t.Helper()
	raw, ok := result["structuredContent"]
	if !ok {
		t.Fatalf("the search result carries no structuredContent: %#v", result)
	}
	payload, _ := raw.(map[string]any)
	hits, _ := payload["hits"].([]any)
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		hit, _ := h.(map[string]any)
		title, _ := hit["Title"].(string)
		out = append(out, title)
	}
	return out
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func isError(result map[string]any) bool {
	v, _ := result["isError"].(bool)
	return v
}

// seedBoard creates a board with one column and returns the column id.
//
// ⚠ testsupport.NewDB migrates a SCHEMA and seeds nothing. The one board the app
// starts with is written by db/seed.go when the database is empty, and only the
// server entrypoint runs that — so a test that needs somewhere to put a card
// builds it.
func (h *harness) seedBoard(boardName, columnName string) string {
	h.t.Helper()
	ctx := testsupport.CtxUser(memberA, "admin")
	board, err := h.todo.CreateBoard(ctx, todo.BoardCreate{Name: boardName})
	if err != nil {
		h.t.Fatalf("seed board: %v", err)
	}
	col, err := h.todo.CreateColumn(ctx, board.ID, todo.ColumnCreate{Name: columnName, Kind: todo.KindNormal})
	if err != nil {
		h.t.Fatalf("seed column: %v", err)
	}
	return col.ID
}

// newHarnessAs is newHarness with the /api half authenticated under a different
// role, for the routes whose whole subject is the role gate.
func newHarnessAs(t *testing.T, roles ...string) *harness {
	t.Helper()
	return newHarness(t, withAPIRoles(roles...))
}

// tokenRowView is the handful of columns the lifecycle tests read back.
type tokenRowView struct {
	name      string
	expiresAt string
	lastUsed  string
	revokedAt string
}

// tokenRow reads one token straight out of the table.
//
// ⚠ IT READS THE COLUMNS AND NOT THE API, because the properties under test are
// what is STORED: an expiry that does not slide, a last-used stamp written at most
// once an hour, a revoke that stamps rather than deletes. The wire shape is a
// separate question with its own assertions.
func (h *harness) tokenRow(id string) tokenRowView {
	h.t.Helper()
	var v tokenRowView
	err := h.db.QueryRow(`
		SELECT name, COALESCE(expires_at, ''), COALESCE(last_used_at, ''), COALESCE(revoked_at, '')
		  FROM mcp_tokens WHERE id = ?`, id).Scan(&v.name, &v.expiresAt, &v.lastUsed, &v.revokedAt)
	if err != nil {
		h.t.Fatalf("read token row %s: %v", id, err)
	}
	return v
}

// noteIDByTitle finds a note by title within one member's private root.
func (h *harness) noteIDByTitle(userID, title string) string {
	h.t.Helper()
	var id string
	err := h.db.QueryRow(
		`SELECT id FROM notes WHERE title = ? AND visibility = 'private' AND owner_id = ?`,
		title, userID).Scan(&id)
	if err != nil {
		h.t.Fatalf("find note %q for %s: %v", title, userID, err)
	}
	return id
}

// splitRoles pulls the comma-separated role list out of a whoami answer.
func splitRoles(text string) []string {
	const marker = "role: "
	i := strings.Index(text, marker)
	if i < 0 {
		return nil
	}
	rest := text[i+len(marker):]
	if end := strings.Index(rest, "."); end >= 0 {
		rest = rest[:end]
	}
	parts := strings.Split(rest, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
