package mcp_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// The two budgets a THIRD review round found being spent twice.
//
// ⚠ THEY ARE THE SAME DEFECT IN TWO PLACES, and it is the shape this version keeps
// producing: a bound is written once, for one thing, and then applied once per
// HALF of that thing. `notes` reads two roots and took the whole listing budget
// from each; `resultWire` writes two halves of one answer and measured each
// against the whole cap. Both return roughly double the figure whose entire
// purpose is to bound what one call costs — the database in the first case, the
// model's context in the second.

// ⚠ ONE BUDGET PER MODULE, NOT ONE PER ROOT. `Provider.ListResources` says "at
// most limit of them" and the host passes a number chosen to bound a query
// against the ONE connection. notes is the only provider reading two roots, and
// taking `limit` from each returned up to twice the cap — which also made the
// host's "this page was cut" warning fire on pages nothing had cut, because two
// roots totalling the cap look exactly like one root that filled it.
//
// ⚠ AND THE PRIVATE ROOT IS SERVED FIRST, so what falls off the end when a
// household outgrows the cap is the shared tree — reachable through home_search
// and home_notes_tree, and announced by the host's Warn — rather than the half
// only its owner can see, which would vanish looking exactly like a member who
// keeps no private notes.
func TestNotesResourceListingSpendsOneBudgetAcrossBothRoots(t *testing.T) {
	h := newHarness(t)
	const perRoot = 4
	for i := range perRoot {
		h.createSharedNoteAs(memberA, fmt.Sprintf("Sdílená %02d", i), "obsah")
		h.createPrivateNoteAs(memberA, fmt.Sprintf("Soukromá %02d", i), "obsah")
	}

	const budget = 5
	got, err := h.notesProv.ListResources(testsupport.CtxUser(memberA, "admin"), budget)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(got) != budget {
		t.Fatalf("a budget of %d returned %d resources — the host's cap bounds the MODULE,"+
			" and notes is the one provider that reads two roots", budget, len(got))
	}
	private := 0
	for _, r := range got {
		if strings.Contains(r.URI, "/soukrome/") {
			private++
		}
	}
	if private != perRoot {
		t.Fatalf("the listing carries %d of the caller's %d private notes — the half only"+
			" its owner can see is not the half a shrinking budget may drop", private, perRoot)
	}
}

// ⚠ ONE BUDGET FOR THE WHOLE RESULT, NOT ONE FOR EACH HALF. The text and the
// structured payload were each measured against HOME_MCP_MAX_RESULT_KB on their
// own, so a tool answering with a capful of each shipped twice the figure. ⚠ And
// when the structured half IS dropped, the result says so — the same rule the
// truncation note and the oversized-blob refusal follow, because a model that
// asked for structuredContent and silently got none cannot tell a payload that
// was too large from a tool that publishes none at all.
func TestOneResultBudgetCoversBothHalves(t *testing.T) {
	const body = "Nákupní seznam na víkend: mléko, chléb, máslo a něco k obědu."

	// Pass 1 measures what this tool actually answers with, under a cap nothing
	// can reach. ⚠ The two halves are read out of the RAW response rather than
	// re-marshalled from the decoded map: the assertion below is arithmetic on
	// byte lengths, and a re-encoded copy is not the same number of bytes.
	big := newHarness(t, withConfig(func(c *mcp.Config) { c.MaxResultBytes = 1 << 20 }))
	bigSecret, _ := big.mintToken(memberA, "Claude", nil, time.Time{})
	bigID := big.createSharedNoteAs(memberA, "Nákup", body)
	text, structured := resultHalves(t, big, bigSecret, bigID)
	if len(structured) == 0 {
		t.Fatal("the fixture tool returned no structured half, so this proves nothing")
	}

	// ⚠ A CAP ONE BYTE SHORT OF THE PAIR AND COMFORTABLY OVER EACH HALF. Under one
	// budget the text fits untouched and the JSON is the half that will not go;
	// under two, both were measured against the whole cap, both passed, and the
	// call shipped `maxResult + 1` bytes.
	maxResult := len(text) + len(structured) - 1
	if maxResult <= len(structured) || maxResult <= len(text) {
		t.Fatalf("the fixture cannot separate one budget from two: text %d, structured %d",
			len(text), len(structured))
	}

	h := newHarness(t, withConfig(func(c *mcp.Config) { c.MaxResultBytes = maxResult }))
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	id := h.createSharedNoteAs(memberA, "Nákup", body)
	got, gotStructured := resultHalves(t, h, secret, id)

	if len(gotStructured) > 0 {
		t.Fatalf("a cap of %d admitted %d bytes of text AND %d of structuredContent —"+
			" that is one budget per half, not one per result",
			maxResult, len(got), len(gotStructured))
	}
	if strings.Contains(got, "Zkráceno") {
		t.Fatalf("the text fits the cap on its own and must not be cut:\n%s", got)
	}
	if !strings.Contains(got, "Strukturovaná část odpovědi byla vynechána") {
		t.Fatalf("the structured half was dropped in silence, which reads to a model like a"+
			" tool that publishes none:\n%s", got)
	}
}

// resultHalves reads one tool call's two halves at the byte lengths the host
// actually wrote them.
func resultHalves(t *testing.T, h *harness, secret, noteID string) (string, json.RawMessage) {
	t.Helper()
	rr, _ := h.call(secret, "home_notes_tree", map[string]any{"note": noteID})
	var env struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode tools/call: %v\n%s", err, rr.Body.String())
	}
	if len(env.Result.Content) == 0 {
		t.Fatalf("the call returned no content: %s", rr.Body.String())
	}
	return env.Result.Content[0].Text, env.Result.StructuredContent
}
