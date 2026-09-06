package mcp_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
)

// The load test (v11, PRD §V11-8, leak table row 17, HANDOFF-13 §11.4).
//
// ⚠ THE ACCEPTANCE CRITERION IS THIS FILE, and it is written as prose in three
// documents: *"twenty concurrent tool calls from one token, and a browser request
// served throughout."* It is the shape of thing v10.2 proved a green suite does
// not tell you.
//
// ⚠ WHAT IT IS REALLY HUNTING IS ONE LEAKED *sql.Rows. `appdb` caps the pool at
// ONE connection and the whole backend is written around that; a provider that
// forgets `appdb.Collect` pins it, and the next query IN THE WHOLE PROCESS — the
// household's dashboard included — waits forever. `internal/arch` cannot see this
// (row 17 says so outright), a unit test that calls one provider at a time cannot
// see it either, and **`go test -race` is unavailable on the windows/arm64 dev
// host**. What is left is arranging for the deadlock to happen on purpose.
//
// ⚠ AND THE TWENTY CALLS ARE EVERY READ TOOL IN THE CATALOG rather than one tool
// twenty times. Twenty copies of `home_whoami` would exercise the semaphore
// beautifully and prove nothing whatsoever about the other eighteen providers —
// and the leak is a property of a provider, not of the host.

// loadCall is one tool call the load makes.
type loadCall struct {
	tool string
	args map[string]any
}

// loadResult is one call's answer, kept for the assertions after the load.
type loadResult struct {
	tool string
	code int
	body string
}

// loadFixtures are the ids the read tools that take one need.
type loadFixtures struct {
	noteID         string
	conversationID string
}

// readToolCalls is one call per READ tool the catalog publishes.
//
// ⚠ THE LIST IS ASSERTED COMPLETE against the live registry by
// assertCoversEveryReadTool, so a provider added in some later version cannot
// join the catalog without joining the load. A hand-maintained list of "the tools
// we happened to think of" is exactly the list that omits the new one.
func readToolCalls(f loadFixtures) []loadCall {
	return []loadCall{
		{"home_whoami", nil},
		{"home_today", nil},
		{"home_metrics", nil},
		{"home_lists", nil},
		// ⚠ The widest single call there is: `home_search` fans out across every
		// module that publishes a Source, so ONE of these is nine FTS queries.
		{"home_search", map[string]any{"query": "Zálivka"}},
		{"home_get", map[string]any{"kind": "notes.note", "id": f.noteID}},
		{"home_activity", map[string]any{"since": "2020-01-01"}},
		{"home_notes_tree", nil},
		{"home_todo_boards", nil},
		{"home_events_upcoming", nil},
		{"home_documents_tree", nil},
		{"home_finance_months", nil},
		{"home_garden_tasks", nil},
		{"home_garden_plan", nil},
		{"home_electricity_summary", nil},
		{"home_electricity_readings", nil},
		{"home_chat_conversations", nil},
		// ⚠ This one WRITES while it reads — a `chat.read` audit event per thread
		// read (D297) — so the load is not all readers competing for the one
		// connection, which is the easy version of the problem.
		{"home_chat_messages", map[string]any{"conversation_id": f.conversationID}},
		{"home_admin_status", nil},
	}
}

const (
	// loadConcurrency is the criterion's own number: twenty at once, one token.
	loadConcurrency = 20
	// loadRounds is how many calls each of those twenty makes, back to back.
	//
	// ⚠ IT IS NOT ONE, AND THE REASON IS THAT A SINGLE BURST IS TOO FAST TO
	// MEASURE. Twenty calls against a warm SQLite file finish in about thirteen
	// milliseconds on this host, which left the browser probe below eight samples
	// to prove "throughout" with — a number that would fall to two on a quicker
	// machine and fail for being fast. Four rounds is a load that LASTS, with the
	// concurrency still pinned at twenty the whole way through.
	//
	// ⚠ AND 20 × 4 = 80 IS UNDER THE PER-TOKEN RATE LIMIT OF 120/min ON PURPOSE.
	// Raising either number without checking the other turns this into a test of
	// the limiter, which has its own.
	loadRounds = 4
)

func TestTwentyConcurrentToolCallsKeepTheBrowserServed(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	f := seedLoadFixtures(h)

	calls := readToolCalls(f)
	assertCoversEveryReadTool(t, h, calls)
	// ⚠ Nineteen read tools and a criterion of twenty, so the widest call goes
	// round twice rather than the cheapest one — the extra slot is worth spending
	// on the fan-out.
	for len(calls) < loadConcurrency {
		calls = append(calls, loadCall{"home_search", map[string]any{"query": "Zálivka"}})
	}

	// ⚠ EVERY REQUEST BODY IS MARSHALLED ON THIS GOROUTINE. The workers below must
	// not touch *testing.T at all — `h.rpc` and `h.call` both can Fatalf, and a
	// Fatalf off the test goroutine is undefined behaviour that shows up as a
	// passing test. That is why this file has its own raw helpers.
	bodies := make([][]byte, len(calls))
	for i, c := range calls {
		bodies[i] = marshalToolCall(t, c)
	}

	var inFlight, peakInFlight atomic.Int64
	results := make([]loadResult, len(calls)*loadRounds)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for r := range loadRounds {
				recordPeak(&peakInFlight, inFlight.Add(1))
				rr := h.rawRPC(secret, bodies[i])
				inFlight.Add(-1)
				results[i*loadRounds+r] = loadResult{calls[i].tool, rr.Code, rr.Body.String()}
			}
		}()
	}

	// The browser, polling its dashboard while the agent works.
	//
	// ⚠ IT STARTS BEFORE THE BARRIER AND STOPS AFTER THE LAST CALL, so "throughout"
	// means what it says rather than "before and after".
	loadDone := make(chan struct{})
	browserDone := make(chan struct{})
	var served, overlapped, failed atomic.Int64
	var slowestNanos atomic.Int64
	var firstBad atomic.Value // string
	go func() {
		defer close(browserDone)
		for {
			select {
			case <-loadDone:
				return
			default:
			}
			concurrent := inFlight.Load() > 0
			code, took := h.rawGET("/api/boards")
			served.Add(1)
			if concurrent {
				overlapped.Add(1)
			}
			if code != http.StatusOK {
				failed.Add(1)
				firstBad.CompareAndSwap(nil, "GET /api/boards answered "+itoa(code))
			}
			for {
				slowest := slowestNanos.Load()
				if took.Nanoseconds() <= slowest || slowestNanos.CompareAndSwap(slowest, took.Nanoseconds()) {
					break
				}
			}
			// A browser polls; it does not spin. Without a pause the probe loop is
			// itself the load and the thing under test is the probe.
			time.Sleep(time.Millisecond)
		}
	}()

	began := time.Now()
	close(start)

	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()

	// ⚠ THE WATCHDOG IS THE POINT OF THE WHOLE FILE. A leaked *sql.Rows does not
	// fail — it WAITS, and `database/sql` waits forever for a free connection
	// because nothing on this path carries a deadline the pool can see. Without
	// this select the test hangs until `go test` kills the package after ten
	// minutes, with no indication of which call was the last one to run.
	select {
	case <-finished:
	case <-time.After(90 * time.Second):
		close(loadDone)
		t.Fatalf("the load did not finish in 90s: %d calls still in flight.\n\n"+
			"This is leak row 17. The pool is ONE connection (appdb.Open sets"+
			" SetMaxOpenConns(1)), so a *sql.Rows a provider forgot to close pins it and"+
			" every later query in the process — the browser's included — waits forever."+
			" Use appdb.Collect, and see TestEveryReadToolReleasesTheOneConnection for"+
			" which tool it was.", inFlight.Load())
	}
	elapsed := time.Since(began)
	close(loadDone)
	<-browserDone

	// The calls themselves.
	for _, got := range results {
		switch {
		case got.code == http.StatusTooManyRequests:
			t.Errorf("%s was rate-limited. The limiter is per-MINUTE (120) and this load"+
				" spends %d of it — a 429 here means it is counting something other than"+
				" calls.", got.tool, loadConcurrency*loadRounds)
		case got.code == http.StatusServiceUnavailable:
			// ⚠ D304: the semaphore is BACK-PRESSURE, not a rate limit. The third
			// concurrent call from one token WAITS. A chatty agent is slowed to the
			// speed of the database, never told to go away.
			t.Errorf("%s got 503. The per-token semaphore must QUEUE the third concurrent"+
				" call, not refuse it (D304).", got.tool)
		case got.code != http.StatusOK:
			t.Errorf("%s got %d under load: %s", got.tool, got.code, truncate(got.body, 400))
		}
		if strings.Contains(got.body, "Došlo k chybě") {
			t.Errorf("%s answered with the internal-error text under load, which it does not"+
				" answer with when called alone: %s", got.tool, truncate(got.body, 400))
		}
		env := decodeEnvelopeBytes(t, got.tool, got.body)
		if env.Error != nil {
			t.Errorf("%s came back as a PROTOCOL error under load (%d %s) — a model retries"+
				" one of those forever (D311).", got.tool, env.Error.Code, env.Error.Message)
			continue
		}
		assertServedTheTool(t, got.tool, env)
	}

	// ⚠ THE CALLS HAVE TO HAVE OVERLAPPED, or every assertion above is about a
	// sequential loop wearing goroutines. The barrier releases all twenty at once,
	// so the peak should be twenty; ten is the floor a loaded CI box can still
	// clear.
	if peak := peakInFlight.Load(); peak < 10 {
		t.Errorf("peak concurrency was %d of %d — the calls did not actually overlap,"+
			" so this ran as a sequential test with extra steps", peak, loadConcurrency)
	}

	// And the browser.
	if failed.Load() > 0 {
		bad, _ := firstBad.Load().(string)
		t.Errorf("%d of %d browser requests failed while the agent was working: %s",
			failed.Load(), served.Load(), bad)
	}
	// ⚠ THE OVERLAP IS MEASURED, NOT ASSUMED. Without this the test would pass on a
	// run where the browser was served entirely before the barrier opened and
	// entirely after the last call returned — which is the one arrangement the
	// criterion exists to exclude.
	if overlapped.Load() < 5 || overlapped.Load()*2 < served.Load() {
		t.Errorf("only %d of %d browser requests started while a tool call was in flight"+
			" — the criterion is a browser request served THROUGHOUT, and this run mostly"+
			" served the browser when nothing was competing with it",
			overlapped.Load(), served.Load())
	}
	slowest := time.Duration(slowestNanos.Load())
	// ⚠ A CEILING RATHER THAN A BUDGET. The honest figure on this host is single-
	// digit milliseconds; ten seconds is the line between "the semaphore is doing
	// its job" and "the household's dashboard is hung", and it is set where a
	// loaded CI box cannot cross it by being slow.
	if slowest > 10*time.Second {
		t.Errorf("the slowest browser request took %s while the agent worked. The per-token"+
			" semaphore exists so an agent cannot starve the browser on the one"+
			" connection.", slowest)
	}
	t.Logf("%d calls, %d concurrent, in %s (peak %d in flight); %d browser requests served,"+
		" %d of them overlapping, slowest %s",
		len(results), loadConcurrency, elapsed.Round(time.Millisecond), peakInFlight.Load(),
		served.Load(), overlapped.Load(), slowest.Round(time.Millisecond))
}

// The same leak, found sequentially — which is what names the culprit.
//
// ⚠ THE CONCURRENT TEST ABOVE PROVES A LEAK EXISTS AND CANNOT SAY WHOSE. Twenty
// goroutines wedged on one connection produce a stack dump in which every one of
// them is waiting and none of them is the cause. This calls each read tool ALONE
// and then asks the browser a question: the first tool after which the browser
// stops answering is the tool that kept the connection.
func TestEveryReadToolReleasesTheOneConnection(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	f := seedLoadFixtures(h)

	calls := readToolCalls(f)
	assertCoversEveryReadTool(t, h, calls)

	for _, c := range calls {
		rr := h.rawRPC(secret, marshalToolCall(t, c))
		if rr.Code != http.StatusOK {
			t.Errorf("%s got %d: %s", c.tool, rr.Code, truncate(rr.Body.String(), 400))
		}
		assertServedTheTool(t, c.tool, decodeEnvelopeBytes(t, c.tool, rr.Body.String()))

		// ⚠ THE PROBE RUNS IN A GOROUTINE ONLY SO IT CAN BE TIMED OUT. A wedged
		// pool makes ServeHTTP block forever, and a blocked test goroutine is a
		// package that dies at the ten-minute mark saying nothing.
		probe := make(chan int, 1)
		go func() {
			code, _ := h.rawGET("/api/boards")
			probe <- code
		}()
		select {
		case code := <-probe:
			if code != http.StatusOK {
				t.Fatalf("a browser request after %s answered %d", c.tool, code)
			}
		case <-time.After(20 * time.Second):
			// Fatal rather than Error: once the one connection is pinned, every
			// assertion after this one is about a wedged process.
			t.Fatalf("%s DID NOT RELEASE THE CONNECTION — a browser request issued after it"+
				" had not completed 20s later.\n\nThat is leak row 17: the pool is one"+
				" connection, so a *sql.Rows this provider left open blocks the next query"+
				" in the whole process. Every store call in that provider goes through"+
				" appdb.Collect, which closes rows on every path including the error one.",
				c.tool)
		}
	}
}

// ---- helpers ----

// seedLoadFixtures gives the read tools rows to read.
//
// ⚠ EVERY TOOL GETS SOMETHING TO FIND. A provider that returns zero rows never
// opens a *sql.Rows to leak, so a load test against empty tables is a load test
// of the host and nothing else.
func seedLoadFixtures(h *harness) loadFixtures {
	h.t.Helper()
	h.seedBoard("Nákup", "Dnes")
	noteID := h.createSharedNoteAs(memberA, "Zálivka", "Zalít skleník obden.")
	h.createPrivateNoteAs(memberA, "Soukromá", "Jen pro mě.")
	h.seedDocument(memberA, "Smlouva", "shared")
	conversationID := h.seedConversation("Rodina", memberA)
	h.seedMessage(conversationID, memberA, "Ahoj")
	return loadFixtures{noteID: noteID, conversationID: conversationID}
}

// assertCoversEveryReadTool fails when the catalog publishes a read tool the load
// does not call.
//
// ⚠ THIS IS WHAT KEEPS THE FILE HONEST AS THE CATALOG GROWS. Row 17 is a rule
// about PROVIDERS, and a provider added in v12 that never appears in the load is
// a provider whose rows nobody ever counted.
func assertCoversEveryReadTool(t *testing.T, h *harness, calls []loadCall) {
	t.Helper()
	called := map[string]bool{}
	for _, c := range calls {
		called[c.tool] = true
	}
	var missing []string
	for _, tool := range mcp.BuildManifest(h.host).Tools {
		if tool.ReadOnly && !called[tool.Name] {
			missing = append(missing, tool.Name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("the load calls no %s. Every READ tool belongs in readToolCalls — leak row"+
			" 17 is a property of a provider, and a provider nobody loads is a provider"+
			" whose *sql.Rows nobody counted.", strings.Join(missing, ", "))
	}
}

// recordPeak raises high to n when n is larger.
func recordPeak(high *atomic.Int64, n int64) {
	for {
		seen := high.Load()
		if n <= seen || high.CompareAndSwap(seen, n) {
			return
		}
	}
}

// marshalToolCall renders one tools/call request body.
func marshalToolCall(t *testing.T, c loadCall) []byte {
	t.Helper()
	params := map[string]any{"name": c.tool}
	if c.args != nil {
		params["arguments"] = c.args
	}
	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": params,
	})
	if err != nil {
		t.Fatalf("marshal %s: %v", c.tool, err)
	}
	return raw
}

// rawRPC posts an already-marshalled body.
//
// ⚠ IT TOUCHES NO *testing.T, WHICH IS THE ONLY REASON IT EXISTS. `h.rpc` marshals
// on the caller's goroutine and can Fatalf; the workers in this file run off the
// test goroutine, where a Fatalf ends the wrong goroutine and leaves the test
// passing.
func (h *harness) rawRPC(secret string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, req)
	return rr
}

// rawGET is one browser request, timed. Also *testing.T-free.
func (h *harness) rawGET(path string) (int, time.Duration) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	began := time.Now()
	h.handler.ServeHTTP(rr, req)
	return rr.Code, time.Since(began)
}

// assertServedTheTool fails when a call came back as a TOOL error.
//
// ⚠ A `isError: true` RESULT IS AN HTTP 200 AND A JSON-RPC SUCCESS, and without
// this both tests above would count one as a call that ran. They would not be
// merely lenient: a tool that is refused never opens a `*sql.Rows` at all, so the
// leak sweep it is supposed to perform becomes vacuous for that provider — green,
// and proving nothing — which is the exact failure TestAnOpenRowsWedgesTheBrowser
// exists to keep the other two from having.
func assertServedTheTool(t *testing.T, tool string, env rpcEnvelope) {
	t.Helper()
	if len(env.Result) == 0 {
		t.Errorf("%s came back with no result at all", tool)
		return
	}
	var res struct {
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(env.Result, &res); err != nil {
		t.Errorf("%s returned a result that is not an object: %v", tool, err)
		return
	}
	if res.IsError {
		t.Errorf("%s answered isError — a refused tool never reaches the database, so"+
			" the leak sweep this test performs is vacuous for its provider: %s",
			tool, truncate(string(env.Result), 400))
	}
}

// decodeEnvelopeBytes is decodeEnvelope over a body already read back.
func decodeEnvelopeBytes(t *testing.T, tool, body string) rpcEnvelope {
	t.Helper()
	var env rpcEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Errorf("%s returned something that is not JSON-RPC: %v\n%s", tool, err, truncate(body, 400))
	}
	return env
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// The negative control (v11, leak row 17).
//
// ⚠ THE TWO TESTS ABOVE ARE DETECTORS, AND A DETECTOR NOBODY HAS EVER SEEN FIRE
// IS A DETECTOR NOBODY KNOWS IS WIRED UP. Both of them are written on one premise:
// that a `*sql.Rows` left open pins the whole pool and the next query in the
// process waits forever. If that premise ever stops holding — a pool widened to
// two connections, a second `*sql.DB` opened somewhere — those tests go on passing
// while proving nothing at all, which is the worst failure a guard has.
//
// So this one leaks a `*sql.Rows` DELIBERATELY and asserts the browser stops being
// served. Then it closes it and asserts the browser comes back.
func TestAnOpenRowsWedgesTheBrowser(t *testing.T) {
	h := newHarness(t)
	h.seedBoard("Nákup", "Dnes")

	// Held open on purpose. Never ranged over — `Rows` releases the connection when
	// Next returns false, so a loop here would defeat the whole test.
	rows, err := h.db.Query(`SELECT id FROM mcp_tokens`)
	if err != nil {
		t.Fatalf("open a rows to pin the connection: %v", err)
	}

	probe := make(chan int, 1)
	go func() {
		code, _ := h.rawGET("/api/boards")
		probe <- code
	}()

	select {
	case code := <-probe:
		_ = rows.Close()
		t.Fatalf("a browser request completed (%d) while a *sql.Rows was open.\n\n"+
			"The one-connection pool is what makes leak row 17 a real risk and what makes"+
			" TestTwentyConcurrentToolCallsKeepTheBrowserServed and"+
			" TestEveryReadToolReleasesTheOneConnection mean anything. If appdb.Open no"+
			" longer sets SetMaxOpenConns(1), or this handler no longer reaches that pool,"+
			" then both of those tests have been passing for free.", code)
	case <-time.After(2 * time.Second):
		// Wedged, as designed.
	}

	if err := rows.Close(); err != nil {
		t.Fatalf("close the pinned rows: %v", err)
	}
	select {
	case code := <-probe:
		if code != http.StatusOK {
			t.Fatalf("the browser request answered %d once the connection was released", code)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the browser request did not complete even after the rows were closed")
	}
}
