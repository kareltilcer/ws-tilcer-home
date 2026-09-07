package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// The protocol surface (PRD §V11-4 FR-M1, D277/D278/D294/D295/D311).

func TestGetIsMethodNotAllowed(t *testing.T) {
	h := newHarness(t)

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /mcp got %d, want 405 (D277)", rr.Code)
	}
	// ⚠ A 405 WITHOUT AN Allow HEADER IS HALF AN ANSWER. A client told 405 with no
	// Allow does not know what to try instead; one told 404 concludes the endpoint
	// is wrong and stops.
	if got := rr.Header().Get("Allow"); got != http.MethodPost {
		t.Fatalf("Allow header is %q, want POST", got)
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("the 405 is not JSON: %q", rr.Header().Get("Content-Type"))
	}
}

func TestInitializeNegotiatesTheVersion(t *testing.T) {
	h := newHarness(t)

	t.Run("the supported version is echoed", func(t *testing.T) {
		rr := h.rpc("", "initialize", map[string]any{"protocolVersion": "2025-06-18"})
		if got := rr.Header().Get("MCP-Protocol-Version"); got != "2025-06-18" {
			t.Fatalf("MCP-Protocol-Version header is %q", got)
		}
		if !strings.Contains(rr.Body.String(), `"protocolVersion":"2025-06-18"`) {
			t.Fatalf("initialize did not report the protocol version: %s", rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), `"name":"home"`) {
			t.Fatalf("initialize did not report serverInfo: %s", rr.Body.String())
		}
	})

	t.Run("an unsupported version is refused, NAMING the supported one", func(t *testing.T) {
		// ⚠ A client that cannot tell whether it is too old or too new retries
		// forever, so the refusal has to say which version home speaks.
		rr := h.rpc("", "initialize", map[string]any{"protocolVersion": "2099-01-01"})
		env := decodeEnvelope(t, rr)
		if env.Error == nil {
			t.Fatalf("an unsupported version was accepted: %s", rr.Body.String())
		}
		if !strings.Contains(env.Error.Message, "2025-06-18") {
			t.Fatalf("the refusal does not name the supported version: %q", env.Error.Message)
		}
	})
}

// ⚠ A BATCH IS REFUSED RATHER THAN PARTIALLY HONOURED. The 2025-06-18 revision
// removed batching, and processing the first element of an array would leave the
// client believing five calls ran.
func TestBatchIsRefused(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	body := `[{"jsonrpc":"2.0","id":1,"method":"tools/list"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, req)

	env := decodeEnvelope(t, rr)
	if env.Error == nil || !strings.Contains(env.Error.Message, "batch") {
		t.Fatalf("a batch was not refused: %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "home_") {
		t.Fatalf("the first element of the batch was processed anyway: %s", rr.Body.String())
	}
}

// ⚠ THE CRITERION FOR AN ABSENT VERB IS THE ABSENCE, NOT A POLITE REFUSAL
// (D294/D295). A conversation must not be able to talk the server into a delete —
// and the way that is guaranteed is that there is nothing to talk it into.
func TestNoDestructiveToolExists(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	// ⚠ THE ASSERTION IS OVER TOOL NAMES, NOT OVER THE WHOLE PAYLOAD. Several
	// descriptions say WHY a verb is absent — "archiving is how a household
	// removes a card, and it is reversible where a delete would not be" — and a
	// substring match over the body fails on the prose that documents the rule.
	names := toolNames(t, h.rpc(secret, "tools/list", nil))

	// Every verb §V11-4 FR-M5 names as ABSENT, by name.
	for _, absent := range []string{
		"delete", "publish", "purge", "upload", "season_close", "season_reopen",
		"layout", "membership", "remove", "archive_all",
	} {
		for _, name := range names {
			if strings.Contains(name, absent) {
				t.Fatalf("the tool %q contains the verb %q, which v11 publishes NOTHING for", name, absent)
			}
		}
	}

	// And calling one is an UNKNOWN TOOL at the protocol level.
	for _, name := range []string{
		"home_notes_delete", "home_notes_publish", "home_todo_card_delete",
		"home_events_delete", "home_notes_folder_delete",
	} {
		rr, result := h.call(secret, name, map[string]any{"id": "x"})
		if result != nil {
			t.Fatalf("%s returned a RESULT rather than a protocol error — the criterion is"+
				" the absence, not a refusal: %#v", name, result)
		}
		env := decodeEnvelope(t, rr)
		if env.Error == nil || !strings.Contains(env.Error.Message, "unknown tool") {
			t.Fatalf("%s: want an unknown-tool protocol error, got %#v", name, env.Error)
		}
	}
}

// ⚠ readOnlyHint IS SET TRUTHFULLY AND destructiveHint NEVER APPEARS — not as
// false, not at all (D294). The absence IS the mechanism, so the test is that the
// key is not in the serialised catalog.
func TestAnnotationsAreTruthfulAndCarryNoDestructiveHint(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	rr := h.rpc(secret, "tools/list", nil)
	if strings.Contains(rr.Body.String(), "destructiveHint") {
		t.Fatalf("a tool carries destructiveHint:\n%s", rr.Body.String())
	}

	var env struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Annotations struct {
					ReadOnly bool `json:"readOnlyHint"`
				} `json:"annotations"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	if len(env.Result.Tools) == 0 {
		t.Fatal("tools/list returned nothing")
	}
	// ⚠ THE LIST IS WRITTEN OUT RATHER THAN DERIVED FROM THE TOOL NAMES, because
	// deriving it would be the same guess the code makes and would agree with any
	// mistake. Sixteen writers across nine modules; everything else is a read.
	writers := map[string]bool{
		"home_todo_card_create": true, "home_todo_card_update": true,
		"home_todo_card_move": true, "home_todo_checklist": true,
		"home_events_create": true, "home_events_update": true, "home_events_complete": true,
		"home_notes_create": true, "home_notes_update": true, "home_notes_pin": true,
		"home_documents_update": true, "home_documents_pin": true,
		"home_finance_month_create": true, "home_finance_month_update": true,
		"home_garden_task_create": true, "home_garden_task_complete": true,
		"home_garden_planting_create": true, "home_garden_harvest_log": true,
		"home_electricity_reading_add": true, "home_electricity_advance_add": true,
	}
	for _, tool := range env.Result.Tools {
		if want := !writers[tool.Name]; tool.Annotations.ReadOnly != want {
			t.Fatalf("%s reports readOnlyHint=%v, want %v", tool.Name, tool.Annotations.ReadOnly, want)
		}
	}
	// The ceiling (D293). Every tool past 45 must retire one.
	if len(env.Result.Tools) > 45 {
		t.Fatalf("%d tools, ceiling is 45", len(env.Result.Tools))
	}
}

// ⚠ NO WRITE TOOL MAY REACH A SERVICE WITH AN INPUT IT COULD HAVE REFUSED ITSELF
// (D311). The rule exists because an agent RETRIES a 500 and GIVES UP on a 422,
// so a household typo returning the wrong code turns a mistake into a loop.
func TestWriteToolsValidateBeforeService(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"todo: empty title", "home_todo_card_create", map[string]any{"column_id": "c", "title": "   "}},
		{"todo: missing column", "home_todo_card_create", map[string]any{"title": "x"}},
		{"todo: no change at all", "home_todo_card_update", map[string]any{"id": "x"}},
		{"todo: move with no card", "home_todo_card_move", map[string]any{"column_id": "c"}},
		{"todo: move with no column", "home_todo_card_move", map[string]any{"id": "x"}},
		{"todo: move with a blank column", "home_todo_card_move", map[string]any{"id": "x", "column_id": "  "}},
		{"todo: checklist with neither verb", "home_todo_checklist", map[string]any{"card_id": "c"}},
		{"todo: item without done", "home_todo_checklist", map[string]any{"card_id": "c", "item_id": "i"}},
		{"events: empty title", "home_events_create", map[string]any{"title": "", "starts_on": "2026-01-01"}},
		{"events: malformed date", "home_events_create", map[string]any{"title": "x", "starts_on": "01/01/2026"}},
		{"events: out-of-range lead", "home_events_create", map[string]any{"title": "x", "starts_on": "2026-01-01", "reminder_lead": "3y"}},
		{"events: bad occurrence date", "home_events_complete", map[string]any{"event_id": "e", "occurrence_on": "nope"}},
		{"notes: empty title", "home_notes_create", map[string]any{"title": " "}},
		{"notes: bad scope", "home_notes_create", map[string]any{"title": "x", "scope": "everyone"}},
		{"notes: no change at all", "home_notes_update", map[string]any{"id": "x"}},
		{"notes: bad pin scope", "home_notes_pin", map[string]any{"id": "x", "scope": "world", "pinned": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr, result := h.call(secret, tc.tool, tc.args)
			if result == nil {
				t.Fatalf("came back as a PROTOCOL error, which a model retries: %s", rr.Body.String())
			}
			if !isError(result) {
				t.Fatalf("the invalid input was ACCEPTED: %s", resultText(t, result))
			}
			text := resultText(t, result)
			// ⚠ AND NOT AS AN INTERNAL ERROR. "Došlo k chybě, zkuste to prosím znovu"
			// is what a 500 maps to, and it is the one answer that turns a typo into a
			// retry loop.
			if strings.Contains(text, "Došlo k chybě") {
				t.Fatalf("a validation failure surfaced as an INTERNAL error: %s", text)
			}
		})
	}
}

// The write path end to end: a card created through MCP carries the member as its
// actor, `via='mcp'`, the token id, and the "<name> · <token>" label (G2, D290,
// D291) — and the audit event is in the same transaction as the change.
func TestWriteIsAttributedAndAuditedInTheSameTx(t *testing.T) {
	h := newHarness(t)
	secret, tok := h.mintToken(memberA, "Claude (notebook)", nil, time.Time{})

	// ⚠ testsupport.NewDB migrates a SCHEMA and seeds nothing — the one board the
	// app seeds is written by db/seed.go when the database is empty, which only
	// the server entrypoint runs. So the fixture is built here.
	columnID := h.seedBoard("Domácnost", "Teď")

	_, created := h.call(secret, "home_todo_card_create", map[string]any{
		"column_id": columnID, "title": "Vynést koš",
	})
	if isError(created) {
		t.Fatalf("the create failed: %s", resultText(t, created))
	}

	// ⚠ THE ROW IS SELECTED BY `via`, NOT BY "the newest". Both writes land inside
	// one test and `ts` is a string compared lexically — an ordering that ties is
	// an ordering that returns the wrong row, and the first draft of this test did
	// exactly that and read the MCP event twice.
	var actorUser, actorLabel, via, viaToken string
	err := h.db.QueryRow(`
		SELECT COALESCE(actor_user_id, ''), COALESCE(actor_label, ''), via, COALESCE(via_token_id, '')
		  FROM audit_events
		 WHERE module = 'todo' AND action = 'card.create' AND via IS NOT NULL
		`).Scan(&actorUser, &actorLabel, &via, &viaToken)
	if err != nil {
		t.Fatalf("read the MCP audit event: %v", err)
	}
	if actorUser != memberA {
		t.Fatalf("actor_user_id is %q, want the MEMBER's id — the token is a second"+
			" credential for the same identity, not a second identity", actorUser)
	}
	if via != audit.ViaMCP {
		t.Fatalf("via is %q, want %q", via, audit.ViaMCP)
	}
	if viaToken != tok.ID {
		t.Fatalf("via_token_id is %q, want %q", viaToken, tok.ID)
	}
	if actorLabel != "Karel · Claude (notebook)" {
		t.Fatalf("actor_label is %q, want \"Karel · Claude (notebook)\" (D291)", actorLabel)
	}

	// ⚠ AND THE SAME WRITE MADE IN THE BROWSER CARRIES via IS NULL, or the filter
	// distinguishes nothing.
	if rr := h.api(http.MethodPost, "/api/columns/"+columnID+"/cards",
		`{"title":"Z prohlížeče"}`); rr.Code != http.StatusCreated {
		t.Fatalf("the browser create got %d: %s", rr.Code, rr.Body.String())
	}
	var nullVia, mcpVia int
	if err := h.db.QueryRow(`
		SELECT SUM(via IS NULL), SUM(via IS NOT NULL) FROM audit_events
		 WHERE module = 'todo' AND action = 'card.create'`).Scan(&nullVia, &mcpVia); err != nil {
		t.Fatalf("count by via: %v", err)
	}
	if nullVia != 1 || mcpVia != 1 {
		t.Fatalf("one browser write and one MCP write produced via NULL=%d, non-NULL=%d —"+
			" the filter distinguishes nothing unless the browser leaves it null", nullVia, mcpVia)
	}
	if n := testsupport.CountAudit(t, h.db, "todo", "card.create"); n != 2 {
		t.Fatalf("expected 2 card.create events, got %d", n)
	}
}

// ⚠ THE PARTIAL INDEX IS THE WHOLE POINT OF THE `via` FILTER (D292): without it
// the Log's newest filter is a scan over the largest table in the database.
func TestViaFilterUsesThePartialIndex(t *testing.T) {
	h := newHarness(t)

	var plan strings.Builder
	rows, err := h.db.Query(`EXPLAIN QUERY PLAN
		SELECT e.id FROM audit_events e WHERE e.via = ? ORDER BY e.ts DESC, e.id DESC LIMIT 50`,
		audit.ViaMCP)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var a, b, c int
		var detail string
		if err := rows.Scan(&a, &b, &c, &detail); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(detail + "\n")
	}
	if !strings.Contains(plan.String(), "idx_events_via") {
		t.Fatalf("the via filter does not use idx_events_via:\n%s", plan.String())
	}
}

// ⚠ A GREEN SUITE IS NOT A LOAD TEST (§V11-8), and this one does not pretend to
// be: the acceptance criterion is twenty concurrent calls with a browser request
// served throughout, run by hand against a real deploy. What IS asserted here is
// the property a unit test can hold — that concurrent calls on ONE connection all
// complete rather than deadlocking, and that the browser is still served while
// they run.
func TestConcurrentCallsDoNotStarveTheBrowser(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	const calls = 20
	var wg sync.WaitGroup
	codes := make([]int, calls)
	for i := range calls {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = h.rpc(secret, "tools/list", nil).Code
		}(i)
	}

	browser := make(chan int, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		browser <- h.api(http.MethodGet, "/api/boards", "").Code
	}()

	wg.Wait()
	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("concurrent call %d got %d", i, code)
		}
	}
	if code := <-browser; code != http.StatusOK {
		t.Fatalf("the browser request got %d while the agent was busy", code)
	}
}

// The rate limit refuses the 121st call in a minute (D306).
func TestRateLimitRefusesPastTheWindow(t *testing.T) {
	h := newHarness(t, withConfig(func(c *mcp.Config) { c.RatePerMin = 3 }))
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	for i := range 3 {
		if rr := h.rpc(secret, "ping", nil); rr.Code != http.StatusOK {
			t.Fatalf("call %d got %d, want 200", i+1, rr.Code)
		}
	}
	if rr := h.rpc(secret, "ping", nil); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("the call past the limit got %d, want 429", rr.Code)
	}

	// ⚠ AND IT IS KEYED BY TOKEN, not by member and not globally: one chatty
	// client must not spend another's budget.
	other, _ := h.mintToken(memberA, "Second client", nil, time.Time{})
	if rr := h.rpc(other, "ping", nil); rr.Code != http.StatusOK {
		t.Fatalf("a second token got %d — the limiter is not keyed by token", rr.Code)
	}
}

// A disabled server looks ABSENT, not forbidden (D317).
func TestDisabledServerIs404(t *testing.T) {
	h := newHarness(t, withConfig(func(c *mcp.Config) { c.Enabled = false }))
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	rr := h.rpc(secret, "tools/list", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("a disabled server answered %d, want 404 — a 403 sends the reader"+
			" after a permission problem that does not exist", rr.Code)
	}
	if strings.Contains(rr.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("the disabled endpoint fell through to the SPA: %q", rr.Header().Get("Content-Type"))
	}
	// ⚠ AND IT IS THE HOUSE ENVELOPE, NOT chi's BUILT-IN 404 — WHICH IS THE
	// ASSERTION A REVIEW ROUND'S FINDING WAS DECLINED AGAINST, not a bug being
	// re-tested. The finding read: Mount returns before registering its own
	// NotFound when the server is disabled, which leaves chi an EMPTY sub-mux, and
	// chi's Mount() copies the parent's NotFound onto a subrouter only when the
	// parent already has one at Mount() time — which httpx.NewRouter deliberately
	// does not. That is all true and the conclusion still does not follow: the
	// OTHER half of chi's contract is that Mux.NotFound walks the subroutes it has
	// already mounted and installs the handler on any that lack one, so the empty
	// /mcp sub-mux inherits the router's JSON 404. Mount still returns early, on
	// purpose. This line is what holds the behaviour by TEST rather than by a
	// reading of chi in either direction — which is why it asserts the
	// Content-Type is JSON and not merely that it is "not HTML".
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("the disabled endpoint answered %q, not JSON: %s", ct, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "not_found") {
		t.Fatalf("the disabled endpoint did not answer in Home's error envelope: %s", rr.Body.String())
	}
}

// toolNames reads the NAMES out of a tools/list response.
func toolNames(t *testing.T, rr *httptest.ResponseRecorder) []string {
	t.Helper()
	var env struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode tools/list: %v\n%s", err, rr.Body.String())
	}
	if len(env.Result.Tools) == 0 {
		t.Fatalf("tools/list returned nothing: %s", rr.Body.String())
	}
	out := make([]string, 0, len(env.Result.Tools))
	for _, tool := range env.Result.Tools {
		out = append(out, tool.Name)
	}
	return out
}
