package mcp_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/admin"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/chat"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/documents"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/electricity"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/finance"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/garden"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/logging"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/notes"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/todo"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
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
	chat    *chat.Service
	docs    *documents.Service
	// elec and garden are held so a fixture can reach the verbs v11 deliberately
	// does NOT publish — a billing period, a season — which is exactly what the
	// tools that read them need in front of them.
	elec   *electricity.Service
	garden *garden.Service
	// notesProv is the notes provider the registry holds, reachable directly so a
	// test can hand it a budget of its own. ⚠ The host always passes
	// resourceListLimit (200), so the only way to assert that a provider reading
	// TWO roots spends ONE budget is to ask it with a number a fixture can reach.
	notesProv mcp.Provider
	// registry is the assembled catalog, so a test can walk EVERY provider rather
	// than the handful it happened to name. ⚠ That is the difference between a
	// rule asserted for three modules and a rule asserted for nine — the one that
	// forgets is the one nobody wrote a test for.
	registry *mcp.Registry
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

	loc, tzErr := time.LoadLocation("Europe/Prague")
	if tzErr != nil {
		t.Fatalf("load timezone: %v", tzErr)
	}

	todoSvc := todo.NewService(db, sink, notify)
	eventsSvc := events.NewService(db, sink, notify, 500, 24)
	notesSvc := notes.NewService(db, sink, notify, nil, notes.ImageOptions{}, logger)
	// ⚠ A REAL (FILESYSTEM) BLOB STORE, not nil. `documents` and `chat` both serve
	// BYTES over the resource surface, and a nil store makes every one of those
	// paths answer "not configured" — which is a green test proving the wrong
	// thing. The temp dir goes with the test.
	blob, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	docsSvc := documents.NewService(db, sink, notify, blob, documents.Options{MaxUploadBytes: 1 << 20}, logger)
	financeSvc := finance.NewService(db, sink, notify)
	// ⚠ THE HOUSEHOLD TIMEZONE, NOT THE ZERO VALUE. garden.NewService defaults a
	// nil Location to UTC, and garden’s own tools default their date window to
	// s.today() — so a harness without it exercises a different day boundary from
	// the one production runs, for an hour every evening.
	gardenSvc := garden.NewService(db, sink, notify, garden.Options{Location: loc})
	chatSvc := chat.NewService(db, sink, nil, nil, nil, chat.Options{TrashDays: 7, Blob: blob, Upload: chat.UploadOptions{MaxBytes: 1 << 20}})
	adminSvc := admin.NewService(db, sink, admin.Options{Logger: logger})

	todoMod := todo.NewModule(todoSvc)
	eventsMod := events.NewModule(eventsSvc, loc, 30)
	notesMod := notes.NewModule(notesSvc)
	docsMod := documents.NewModule(docsSvc)
	financeMod := finance.NewModule(financeSvc, loc)
	gardenMod := garden.NewModule(gardenSvc)
	elecSvc := electricity.NewService(db, sink, notify, loc)
	elecMod := electricity.NewModule(elecSvc)
	chatMod := chat.NewModule(chatSvc)
	loggingMod := logging.New(db)
	adminMod := admin.NewModule(adminSvc)

	// ⚠ THE ORDER IS THE COMPOSITION ROOT'S, and it is load-bearing rather than
	// cosmetic: module registration order is the third tiebreak of the search
	// merge (D300). A harness that collected them in a different order would
	// exercise a different merge from the one production runs.
	allModules := []any{
		loggingMod, todoMod, eventsMod, notesMod, docsMod,
		financeMod, gardenMod, elecMod, chatMod, adminMod,
	}
	registry, err := mcp.Collect(allModules...)
	if err != nil {
		t.Fatalf("collect mcp providers: %v", err)
	}
	// ⚠ THE METRIC AND LIST CATALOGS TAKE THE *CONTRIBUTING* SIX, not all ten —
	// `electricity` (D147) and `chat` (D252) are deliberately absent from both, and
	// so is `admin`, which reads them rather than publishing into them. Collecting
	// everything here would let home_today answer with a figure production cannot
	// produce.
	contributing := []any{todoMod, eventsMod, notesMod, docsMod, financeMod, gardenMod}
	metricReg, err := metrics.Collect(contributing...)
	if err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	listReg, err := lists.Collect(contributing...)
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

	h := &harness{t: t, db: db, tokens: tokens, notes: notesSvc, todo: todoSvc,
		chat: chatSvc, docs: docsSvc, elec: elecSvc, garden: gardenSvc,
		notesProv: notesMod.MCPProvider(), registry: registry}
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
			docsMod.RegisterRoutes(api)
			chatMod.RegisterRoutes(api)
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
		title, _ := hit["title"].(string)
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

// searchCounts reads the PER-MODULE counts out of a home_search result.
//
// ⚠ IT EXISTS BECAUSE A TOTAL IS THE WRONG ASSERTION. The budget is per module
// (D301) and the corpus spans nine providers — one of which, `logging`, finds the
// audit event every other module's fixture wrote. A test that counted hits would
// move every time an unrelated provider learned to answer, which is the kind of
// test that gets its number bumped rather than read.
func searchCounts(t *testing.T, result map[string]any) map[string]int {
	t.Helper()
	raw, ok := result["structuredContent"]
	if !ok {
		t.Fatalf("the search result carries no structuredContent: %#v", result)
	}
	payload, _ := raw.(map[string]any)
	counts, _ := payload["counts"].(map[string]any)
	out := make(map[string]int, len(counts))
	for module, v := range counts {
		n, _ := v.(float64)
		out[module] = int(n)
	}
	return out
}

// seedDocument uploads one small text document into a member's chosen root.
//
// ⚠ IT GOES THROUGH THE REAL UPLOAD PIPELINE rather than inserting a row, because
// what the resource tests read back are BYTES: a hand-written row would point at
// an object that does not exist, and every read would fail for a reason that has
// nothing to do with the rule under test.
func (h *harness) seedDocument(userID, title, scope string) string {
	h.t.Helper()
	ctx := testsupport.CtxUser(userID, "editor")
	d, err := h.docs.Upload(ctx, documents.UploadInput{
		Filename: title + ".txt",
		File:     strings.NewReader("obsah dokumentu " + title),
		Title:    title,
		Scope:    scope,
	})
	if err != nil {
		h.t.Fatalf("seed %s document %q: %v", scope, title, err)
	}
	return d.ID
}

// seedAttachment sends one file into a conversation, through the real multipart
// route, and returns the attachment's id.
//
// ⚠ IT GOES THROUGH THE ROUTE rather than the store because the upload pipeline
// is what puts BYTES under the key the resource read then fetches. A row written
// by hand would point at nothing, and every read would fail for a reason
// unrelated to the access rule under test.
func (h *harness) seedAttachment(conversationID, author, filename, body string) string {
	h.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("files", filename)
	if err != nil {
		h.t.Fatalf("multipart: %v", err)
	}
	if _, err := part.Write([]byte(body)); err != nil {
		h.t.Fatalf("multipart write: %v", err)
	}
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/chat/conversations/"+conversationID+"/messages", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rr := httptest.NewRecorder()
	// ⚠ THE ROUTER IS BUILT FOR THIS ONE AUTHOR. The harness's own /api half is
	// member A; an attachment belonging to member B has to be uploaded AS B, or
	// the membership assertions are testing A reading A's own file.
	h.routerAs(author).ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		h.t.Fatalf("seed attachment: %d %s", rr.Code, rr.Body.String())
	}
	var msg struct {
		Attachments []struct {
			ID string `json:"id"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &msg); err != nil {
		h.t.Fatalf("decode message: %v", err)
	}
	if len(msg.Attachments) != 1 {
		h.t.Fatalf("expected one attachment, got %d", len(msg.Attachments))
	}
	return msg.Attachments[0].ID
}

// routerAs builds a second router authenticated as another member, for the
// fixtures that must be written by somebody other than member A.
//
// ⚠ IT IS A SECOND ROUTER RATHER THAN A SWITCHED ACTOR, because the harness's own
// SessionMW is built once with a fixed bypass actor — which is exactly what makes
// the /api half a fixture tool rather than a thing under test.
func (h *harness) routerAs(userID string) http.Handler {
	h.t.Helper()
	return testsupport.RouterAs(h.t, h.db,
		reqctx.Actor{UserID: userID, Type: "user", Label: userID, Roles: []string{"editor"}},
		func(api chi.Router) { chat.NewModule(h.chat).RegisterRoutes(api) })
}
