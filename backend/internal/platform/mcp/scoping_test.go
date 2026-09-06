package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/todo"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// The four defects a second review round found, each written so the fix cannot
// be undone without going red.

// ⚠ A TOOL THAT TAKES TWO IDS MUST CHECK THEY AGREE. home_todo_checklist takes
// card_id AND item_id, but Service.UpdateChecklistItem resolves an item by id
// alone — right for its HTTP twin, whose route is item-addressed and carries no
// card, and wrong here. A mismatched pair ticked a box on a DIFFERENT card,
// audited it, and answered with the named card's checklist, in which nothing had
// changed: the model reads its own write as having failed and sends it again.
func TestChecklistTickIsScopedToItsCard(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	columnID := h.seedBoard("Domácnost", "Teď")

	ctx := testsupport.CtxUser(memberA, "admin")
	cardA, err := h.todo.CreateCard(ctx, columnID, todo.CardCreate{Title: "Karta A"})
	if err != nil {
		t.Fatalf("create card A: %v", err)
	}
	cardB, err := h.todo.CreateCard(ctx, columnID, todo.CardCreate{Title: "Karta B"})
	if err != nil {
		t.Fatalf("create card B: %v", err)
	}
	itemB, err := h.todo.CreateChecklistItem(ctx, cardB.ID, todo.ChecklistItemCreate{Text: "Bod na kartě B"})
	if err != nil {
		t.Fatalf("create checklist item on card B: %v", err)
	}

	// Card A's id, card B's item. The one shape the tool must refuse.
	_, result := h.call(secret, "home_todo_checklist", map[string]any{
		"card_id": cardA.ID, "item_id": itemB.ID, "done": true,
	})
	if result == nil {
		t.Fatal("the mismatched pair came back as a protocol error, which a model retries")
	}
	if !isError(result) {
		t.Fatalf("ticking card B's item through card A was ACCEPTED: %s", resultText(t, result))
	}

	// ⚠ AND THE WRITE MUST NOT HAVE HAPPENED. A refusal that fires after the update
	// is a refusal that fixes the message and not the bug.
	after, err := h.todo.ListChecklist(ctx, cardB.ID)
	if err != nil {
		t.Fatalf("read card B's checklist: %v", err)
	}
	if len(after) != 1 || after[0].Done {
		t.Fatalf("card B's item was ticked through card A: %#v", after)
	}
	if n := testsupport.CountAudit(t, h.db, "todo", "checklist_item.update"); n != 0 {
		t.Fatalf("the refused tick wrote %d audit events", n)
	}

	// ⚠ AND A CALL CARRYING BOTH VERBS REFUSES BEFORE IT ADDS ANYTHING. This tool
	// does two things in one call, so a check that ran after the add would append
	// the item and then refuse the tick — a half-applied write, which is worse than
	// either whole outcome and the harder of the two to notice.
	_, both := h.call(secret, "home_todo_checklist", map[string]any{
		"card_id": cardA.ID, "add": "Nový bod", "item_id": itemB.ID, "done": true,
	})
	if !isError(both) {
		t.Fatalf("add + a foreign item_id was accepted: %s", resultText(t, both))
	}
	onA, err := h.todo.ListChecklist(ctx, cardA.ID)
	if err != nil {
		t.Fatalf("read card A's checklist: %v", err)
	}
	if len(onA) != 0 {
		t.Fatalf("the refused call still added %d item(s) to card A: %#v", len(onA), onA)
	}

	// The control: the matching pair works, or this test passes against a tool
	// that refuses everything.
	_, ok := h.call(secret, "home_todo_checklist", map[string]any{
		"card_id": cardB.ID, "item_id": itemB.ID, "done": true,
	})
	if isError(ok) {
		t.Fatalf("the MATCHING pair was refused: %s", resultText(t, ok))
	}
	after, _ = h.todo.ListChecklist(ctx, cardB.ID)
	if len(after) != 1 || !after[0].Done {
		t.Fatalf("the matching tick did not land: %#v", after)
	}
}

// ⚠ A LISTING IS NOT A SEARCH, AND IT MUST NOT SPEND THE SEARCH BUDGET.
// resources/list used to be capped at HOME_MCP_SEARCH_LIMIT — ten — so a
// household with forty notes was shown ten, with nothing marking the cut, and the
// model reasoned about the tree it was handed as though it were the tree. The
// search budget is ten because a search wants the best few from each module
// (D301); "what is there" is a different question.
func TestResourceListIsNotCappedAtTheSearchBudget(t *testing.T) {
	h := newHarness(t, withConfig(func(c *mcp.Config) { c.SearchLimit = 3 }))
	const notes = 12
	for i := range notes {
		h.createSharedNoteAs(memberA, fmt.Sprintf("Poznámka %02d", i), "obsah")
	}

	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	rr := h.rpc(secret, "resources/list", nil)

	var env struct {
		Result struct {
			Resources []struct {
				URI string `json:"uri"`
			} `json:"resources"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode resources/list: %v\n%s", err, rr.Body.String())
	}
	if got := len(env.Result.Resources); got != notes {
		t.Fatalf("resources/list returned %d of %d notes — a listing cut to the SEARCH"+
			" budget is a listing the model reads as complete", got, notes)
	}

	// And the search budget itself is untouched by the fix.
	_, result := h.call(secret, "home_search", map[string]any{"query": "Poznámka"})
	if got := len(searchTitles(t, result)); got != 3 {
		t.Fatalf("home_search returned %d hits, want the per-module budget of 3", got)
	}
}

// ⚠ AN UNKNOWN MODULE IN `in` IS A REFUSAL, NOT ZERO HITS. A search silently
// narrowed to nothing is indistinguishable from a household with nothing in it,
// and the caller who typed a Czech module name is the only one who can fix it —
// the same rule, for the same reason, that the token allowlist takes in
// normaliseModules.
func TestSearchModuleFilter(t *testing.T) {
	h := newHarness(t)
	h.createSharedNoteAs(memberA, "Nákup", "mléko")
	columnID := h.seedBoard("Domácnost", "Teď")
	ctx := testsupport.CtxUser(memberA, "admin")
	if _, err := h.todo.CreateCard(ctx, columnID, todo.CardCreate{Title: "Nákup"}); err != nil {
		t.Fatalf("seed card: %v", err)
	}

	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	t.Run("no filter reaches both modules", func(t *testing.T) {
		_, result := h.call(secret, "home_search", map[string]any{"query": "Nákup"})
		if got := len(searchTitles(t, result)); got != 2 {
			t.Fatalf("an unfiltered search found %d hits, want 2", got)
		}
	})

	t.Run("a named module narrows", func(t *testing.T) {
		_, result := h.call(secret, "home_search", map[string]any{
			"query": "Nákup", "in": []string{"notes"},
		})
		if got := len(searchTitles(t, result)); got != 1 {
			t.Fatalf("in=[notes] found %d hits, want 1", got)
		}
	})

	t.Run("an unknown module is refused by name", func(t *testing.T) {
		_, result := h.call(secret, "home_search", map[string]any{
			"query": "Nákup", "in": []string{"poznamky"},
		})
		if result == nil {
			t.Fatal("the unknown module came back as a protocol error, which a model retries")
		}
		if !isError(result) {
			t.Fatalf("in=[poznamky] answered \"0 hits\" instead of refusing: %s", resultText(t, result))
		}
		text := resultText(t, result)
		if !strings.Contains(text, "poznamky") {
			t.Fatalf("the refusal does not name the module the caller got wrong: %s", text)
		}
		if strings.Contains(text, "Došlo k chybě") {
			t.Fatalf("a caller mistake surfaced as an INTERNAL error, which a model retries: %s", text)
		}
	})
}

// ⚠ THE PROTOCOL VERSION IS HOME'S, NOT THE CALLER'S. The response header used to
// echo whatever the request claimed, so a client pinned to a revision home does
// not implement was served in full AND told, in the negotiated-version header,
// that home spoke it too. Home speaks exactly one (D278).
func TestUnsupportedProtocolVersionHeaderIsRefused(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	rr := h.rpcWith(func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+secret)
		r.Header.Set("MCP-Protocol-Version", "2024-11-05")
	}, "tools/list", nil)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("an unsupported protocol header got %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "2025-06-18") {
		t.Fatalf("the refusal does not name the version home speaks: %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "home_") {
		t.Fatalf("the refusal names tools: %s", rr.Body.String())
	}
	// ⚠ AND THE HEADER ON THE WAY OUT IS ALWAYS HOME'S.
	if got := rr.Header().Get("MCP-Protocol-Version"); got != "2025-06-18" {
		t.Fatalf("the response echoed %q rather than naming home's version", got)
	}

	// The supported version passes through untouched.
	ok := h.rpcWith(func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+secret)
		r.Header.Set("MCP-Protocol-Version", "2025-06-18")
	}, "tools/list", nil)
	if ok.Code != http.StatusOK {
		t.Fatalf("the SUPPORTED version got %d, want 200", ok.Code)
	}
}

// ⚠ A JSON-RPC RESPONSE CARRIES EXACTLY ONE OF result AND error.
// `notifications/initialized` is a notification, so it is normally answered with
// 202 and no body — but a client that sends it WITH an id takes the ordinary
// path, and `result` is tagged omitempty, so a nil result produced an envelope
// with neither member: not a JSON-RPC response at all, from a server that was
// working perfectly.
func TestEveryAnsweredCallCarriesAResult(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	for _, method := range []string{"notifications/initialized", "ping"} {
		t.Run(method, func(t *testing.T) {
			rr := h.rpc(secret, method, nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("%s got %d, want 200 — it was sent WITH an id, so it is a request", method, rr.Code)
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode %s: %v\n%s", method, err, rr.Body.String())
			}
			_, hasResult := body["result"]
			_, hasError := body["error"]
			// Fails when BOTH are present and when NEITHER is.
			if hasResult == hasError {
				t.Fatalf("%s answered with result=%t error=%t — a response object must carry"+
					" exactly one of them: %s", method, hasResult, hasError, rr.Body.String())
			}
		})
	}

	// A notification proper — no id — is still answered with 202 and no body.
	rr := h.rpcNotification(secret, "notifications/initialized")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("a real notification got %d, want 202", rr.Code)
	}
	if strings.TrimSpace(rr.Body.String()) != "" {
		t.Fatalf("a notification was answered with a body: %s", rr.Body.String())
	}
}

// ⚠ A TYPED NIL IN A NON-NIL INTERFACE IS STILL NIL. A module whose
// MCPProvider() returns a nil *someProvider hands Collect an interface that is
// NOT == nil, so the plain guard passed and the next line panicked at
// composition — a crash-loop with a stack, where every other failure in that
// function produces a sentence naming the module.
func TestCollectSurvivesATypedNilProvider(t *testing.T) {
	reg, err := mcp.Collect(nilProviderModule{}, notAModule{})
	if err != nil {
		t.Fatalf("Collect returned an error for a nil provider: %v", err)
	}
	if got := len(reg.Providers()); got != 0 {
		t.Fatalf("a nil provider was registered (%d providers)", got)
	}
}

type nilProviderModule struct{}

func (nilProviderModule) MCPProvider() mcp.Provider {
	var p *panicProvider // typed nil
	return p
}

type notAModule struct{}

// panicProvider exists only so the typed nil above has a type. Every method
// would panic if it were ever called, which is the point.
type panicProvider struct{ mcp.NoResources }

func (*panicProvider) Module() string    { panic("the nil provider was used") }
func (*panicProvider) Tools() []mcp.Tool { panic("the nil provider was used") }
func (*panicProvider) Call(context.Context, string, json.RawMessage) (mcp.Result, error) {
	panic("the nil provider was used")
}
func (*panicProvider) Search(context.Context, mcp.Query) ([]mcp.Hit, error) {
	panic("the nil provider was used")
}

// ⚠ NOTHING CHANGED IS NOT A CHANGE. McpTokenUpdate has no required field, so an
// empty PATCH is valid — and writing mcp.token.update for it puts "Upraven token
// pro asistenta" in the Log with no field diffs behind it, which is the one row a
// member auditing their own credential cannot tell from a real rename.
func TestPatchWithNoChangeWritesNoEvent(t *testing.T) {
	h := newHarness(t)
	_, tok := h.mintToken(memberA, "Claude", nil, time.Time{})

	if rr := h.api(http.MethodPatch, "/api/mcp/tokens/"+tok.ID, `{}`); rr.Code != http.StatusOK {
		t.Fatalf("an empty PATCH got %d: %s", rr.Code, rr.Body.String())
	}
	if rr := h.api(http.MethodPatch, "/api/mcp/tokens/"+tok.ID,
		`{"name":"Claude"}`); rr.Code != http.StatusOK {
		t.Fatalf("a PATCH repeating the stored name got %d: %s", rr.Code, rr.Body.String())
	}
	if n := testsupport.CountAudit(t, h.db, "platform", "mcp.token.update"); n != 0 {
		t.Fatalf("%d mcp.token.update events for two PATCHes that changed nothing", n)
	}

	// The control: a real rename still audits, or this test passes against a
	// handler that has stopped auditing altogether.
	if rr := h.api(http.MethodPatch, "/api/mcp/tokens/"+tok.ID,
		`{"name":"Claude na notebooku"}`); rr.Code != http.StatusOK {
		t.Fatalf("the real rename got %d: %s", rr.Code, rr.Body.String())
	}
	if n := testsupport.CountAudit(t, h.db, "platform", "mcp.token.update"); n != 1 {
		t.Fatalf("a real rename wrote %d events, want 1", n)
	}
	if h.tokenRow(tok.ID).name != "Claude na notebooku" {
		t.Fatalf("the rename did not land: %q", h.tokenRow(tok.ID).name)
	}
}
