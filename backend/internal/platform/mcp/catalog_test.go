package mcp_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
)

// The whole catalog, once every provider is registered (PRD §V11-4 FR-M4).
//
// ⚠ THIS IS NOT THE MANIFEST TEST. `internal/arch/mcp_completeness_test.go` and
// the committed `backend/mcp-manifest.json` are PR 3's, and they are the guard
// that a tool's SCHEMA cannot change without somebody seeing it. What this file
// asserts is the shape the PRD specifies: how many tools, which modules publish
// them, and which two publish none.

// The counts §V11-4 FR-M4 fixes: seven cross-cutting tools and thirty-two module
// ones, thirty-nine in all, under a ceiling of forty-five (D293).
var wantToolsPerModule = map[string]int{
	"todo": 5, "events": 4, "notes": 4, "documents": 3, "finance": 3,
	"garden": 6, "electricity": 4, "chat": 2, "admin": 1,
}

const wantCoreTools = 7

func TestCatalogMatchesTheSpecifiedShape(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	names := toolNames(t, h.rpc(secret, "tools/list", nil))

	byModule := map[string]int{}
	core := 0
	for _, name := range names {
		if !strings.HasPrefix(name, "home_") {
			t.Fatalf("tool %q does not follow home_<module>_<verb>", name)
		}
		rest := strings.TrimPrefix(name, "home_")
		module, _, hasModule := strings.Cut(rest, "_")
		// ⚠ A CORE TOOL DROPS THE MODULE, which is why this cannot simply split on
		// the first underscore: `home_whoami` has none and `home_activity` would
		// otherwise register a module called "activity".
		if !hasModule || wantToolsPerModule[module] == 0 {
			core++
			continue
		}
		byModule[module]++
	}

	if core != wantCoreTools {
		t.Errorf("%d cross-cutting tools, want %d", core, wantCoreTools)
	}
	total := core
	for module, want := range wantToolsPerModule {
		if got := byModule[module]; got != want {
			t.Errorf("module %q publishes %d tools, want %d", module, got, want)
		}
		total += byModule[module]
	}
	if total != len(names) {
		t.Errorf("counted %d tools but tools/list returned %d — a module outside the"+
			" specified nine is publishing", total, len(names))
	}
	if len(names) != 39 {
		t.Errorf("%d tools, want the 39 §V11-4 FR-M4 specifies", len(names))
	}
	// ⚠ THE CEILING IS THE POINT OF THE COUNT (D293): 144 REST paths cannot become
	// 144 tools, because a model's selection accuracy collapses well before that
	// and the list alone would spend the context budget before the first question.
	// Every tool past 45 must retire one.
	if len(names) > 45 {
		t.Errorf("%d tools, ceiling is 45", len(names))
	}
}

// ⚠ TWO MODULES PUBLISH NO TOOL, AND THAT IS A DECISION RATHER THAN A GAP
// (FR-M2). `dashboard` implements mcp.Source not at all — the dashboard is the
// member's screen, not their data. `logging` implements it with an EMPTY Tools()
// and a real Search: the Log's questions are already answered by home_activity,
// and a second tool over the same table would be a second redaction path.
func TestTwoModulesPublishNoTool(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	names := toolNames(t, h.rpc(secret, "tools/list", nil))

	for _, name := range names {
		for _, absent := range []string{"home_dashboard_", "home_logging_"} {
			if strings.HasPrefix(name, absent) {
				t.Fatalf("%q publishes a tool: %s", strings.TrimSuffix(absent, "_"), name)
			}
		}
	}

	// ⚠ AND YET `logging` CONTRIBUTES TO SEARCH. A provider with no tools is not an
	// empty provider — if an empty Tools() made something skip it, a fifth of the
	// household's search corpus would vanish silently, which is precisely what
	// FR-M2 warns about.
	h.createSharedNoteAs(memberA, "Zahradní nůžky", "koupit")
	_, result := h.call(secret, "home_search", map[string]any{"query": "Zahradní"})
	if searchCounts(t, result)["logging"] == 0 {
		t.Fatal("logging contributed nothing to the search — a provider with an empty" +
			" Tools() must still be asked, or a fifth of the corpus disappears with" +
			" nothing reporting it")
	}
}

// ⚠ `KnownModules` IS THE CONTRACT'S VOCABULARY AND THE REGISTRY IS THE CODE'S,
// and by the end of v11 they must agree. Between PR 1 and PR 2 they deliberately
// did not — six of the nine had no provider yet, and a token scoped to one of
// them was still valid because the served openapi.yaml said so. That window is
// closed here.
func TestKnownModulesAllPublishTools(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	names := toolNames(t, h.rpc(secret, "tools/list", nil))

	for _, module := range mcp.KnownModules {
		prefix := "home_" + module + "_"
		if !slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(n, prefix) }) {
			t.Errorf("mcp.KnownModules names %q, which publishes no tool. The enum is"+
				" openapi's McpModule and a member may scope a token to it — a module in"+
				" the list with nothing behind it is a token that silently reaches"+
				" nothing.", module)
		}
	}
	if len(mcp.KnownModules) != len(wantToolsPerModule) {
		t.Errorf("KnownModules has %d entries, the specified surface has %d modules",
			len(mcp.KnownModules), len(wantToolsPerModule))
	}
}

// ⚠ LEAK ROW 8, THE TRAP ITSELF, ACROSS THE WHOLE CATALOG (D302). PR 1 asserted
// it for three providers; nine is the number that matters, because the one that
// forgets is the one nobody wrote a test for.
func TestEveryRegisteredProviderErrorsWithoutAnActor(t *testing.T) {
	h := newHarness(t)
	bare := context.Background()

	for _, p := range h.registry.Providers() {
		module := p.Module()
		t.Run(module+": Search", func(t *testing.T) {
			hits, err := p.Search(bare, mcp.Query{Text: "cokoliv", Limit: 10})
			if err == nil {
				t.Fatalf("%s.Search returned (%d hits, nil error) on an actor-less"+
					" context. An empty slice is what an unscoped-load bug looks like"+
					" the day somebody 'fixes' the nil — it has to be an ERROR (D302).",
					module, len(hits))
			}
		})
		if len(p.Resources()) == 0 {
			continue
		}
		t.Run(module+": ListResources", func(t *testing.T) {
			if _, err := p.ListResources(bare, 10); err == nil {
				t.Fatalf("%s.ListResources returned no error on an actor-less context", module)
			}
		})
	}
}

// The three resource templates FR-M7 fixes, and no fourth.
func TestResourceTemplates(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	body := h.rpc(secret, "resources/templates/list", nil).Body.String()

	for _, want := range []string{
		"home://notes/{path}",
		"home://documents/{path}",
		"home://chat/{conversation}/attachments/{id}",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the template %q is missing:\n%s", want, body)
		}
	}
	// ⚠ THERE IS NO `sdilene` SEGMENT ANYWHERE, and inventing one would be the only
	// place in the whole application where the shared root is named. A private item
	// is prefixed `soukrome`; a shared one has no prefix at all, exactly as
	// lib/scope.ts parses a URL.
	if strings.Contains(body, "sdilene") {
		t.Errorf("a template names the shared root:\n%s", body)
	}
	if n := strings.Count(body, "uriTemplate"); n != 3 {
		t.Errorf("%d resource templates, want 3", n)
	}
}

// A reader is offered every read tool and refused every write one, across the
// whole catalog rather than the three PR 1 could reach.
func TestReaderIsOfferedReadsAndRefusedWrites(t *testing.T) {
	h := newHarness(t)
	h.seedSession("user-r", "Reader", "reader")
	secret, _ := h.mintToken("user-r", "Claude", nil, time.Time{})

	names := toolNames(t, h.rpc(secret, "tools/list", nil))
	// ⚠ THE WRITE TOOLS ARE STILL *LISTED* FOR A READER, and refused when called.
	// HANDOFF row 6 specifies refusal rather than concealment, and the two differ:
	// a model that cannot see a tool concludes the household has no such feature,
	// where one that is refused reports what actually happened.
	if !slices.Contains(names, "home_notes_create") {
		t.Fatal("a write tool was hidden from a reader rather than refused")
	}
	// ⚠ ADMIN-ONLY IS THE OTHER RULE, and it IS concealment: home_admin_status has
	// no business appearing to somebody its HTTP twin answers 403 to.
	if slices.Contains(names, "home_admin_status") {
		t.Fatal("home_admin_status is offered to a reader")
	}

	for _, tool := range []string{
		"home_documents_update", "home_finance_month_create",
		"home_garden_task_create", "home_electricity_reading_add",
	} {
		_, result := h.call(secret, tool, map[string]any{"id": "x", "title": "x"})
		if result == nil || !isError(result) {
			t.Fatalf("%s was NOT refused for a reader's token: %#v", tool, result)
		}
	}

	// And a read tool from a module PR 1 could not reach still works.
	if _, result := h.call(secret, "home_electricity_readings", nil); isError(result) {
		t.Fatalf("a read tool was refused to a reader: %s", resultText(t, result))
	}

	// The admin tool is refused even when called by name.
	rr, result := h.call(secret, "home_admin_status", nil)
	if result != nil && !isError(result) {
		t.Fatalf("a reader ran home_admin_status: %s", resultText(t, result))
	}
	if result == nil && rr.Code != http.StatusOK {
		t.Fatalf("home_admin_status answered %d rather than a refusal", rr.Code)
	}
}
