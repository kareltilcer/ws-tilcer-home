package mcp_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/electricity"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/garden"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// The six providers PR 2 adds, EXERCISED rather than counted.
//
// ⚠ THE CATALOG TESTS COUNT TOOL NAMES AND THAT IS NOT THE SAME QUESTION. PR 2
// shipped with every provider's shape asserted and not one of its ten write tools
// or six read tools ever called, and three defects lived comfortably underneath
// a green suite because of it: a month lookup that filtered a page instead of
// reading a row, a summary that resolved the wrong billing period, and a task
// tool whose own schema said `kind` was optional against a service that refuses
// an empty one. Each needed exactly one successful call to find.
//
// HANDOFF-13 §16.2 asks for validation cases PER WRITING MODULE and one audited
// write per writing module. This file is that, for the four PR 2 adds.

// ---- the two regressions ----

// ⚠ A MONTH IS RESOLVED BY KEY, NOT BY SCANNING THE NEWEST PAGE. finance's
// history arrived by migration from `fin` (§V6-12), so most of it sits outside
// any default page — and the tool answered NOT FOUND for every month behind it.
func TestFinanceMonthLookupReachesPastTheDefaultPage(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	// Thirty months, so the oldest is comfortably behind the default page of 24.
	for i := 0; i < 30; i++ {
		month := time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC).
			AddDate(0, i, 0).Format("2006-01")
		_, created := h.call(secret, "home_finance_month_create", map[string]any{
			"month": month, "income_kaja": 40000 + i, "income_andy": 30000 + i, "rates": evenRates(),
		})
		if isError(created) {
			t.Fatalf("seed %s: %s", month, resultText(t, created))
		}
	}

	_, result := h.call(secret, "home_finance_months", map[string]any{"month": "2023-01"})
	if isError(result) {
		t.Fatalf("home_finance_months refused an existing month: %s", resultText(t, result))
	}
	text := resultText(t, result)
	if strings.Contains(text, "Nenalezeno") || !strings.Contains(text, "2023-01") {
		t.Fatalf("the oldest recorded month came back as not-found — the lookup is"+
			" filtering the newest page instead of reading the row:\n%s", text)
	}

	// And a month that genuinely does not exist still refuses.
	_, missing := h.call(secret, "home_finance_months", map[string]any{"month": "1999-01"})
	if !isError(missing) && strings.Contains(resultText(t, missing), "1999-01") {
		t.Fatalf("a month nobody recorded was answered: %s", resultText(t, missing))
	}

	// ⚠ AND A LIMIT PAST THE SERVICE'S CEILING IS REFUSED RATHER THAN RESET.
	// Service.List turns anything above 200 into 50, so sending it through would
	// answer a request for 300 with FEWER rows than a request for 200.
	_, tooMany := h.call(secret, "home_finance_months", map[string]any{"limit": 300})
	if !isError(tooMany) {
		t.Fatalf("limit 300 was accepted and silently reset: %s", resultText(t, tooMany))
	}
}

// ⚠ "THE CURRENT BILLING PERIOD" IS THE ONE CONTAINING TODAY, NOT THE ONE THAT
// STARTS LAST. Creating next year's period ahead of the current one is the
// ordinary way a period is set up, and picking the newest `starts_on` then
// answered with a period that has not started — no consumption, no advances —
// under a description that says "current".
func TestElectricitySummaryAnswersTheCurrentPeriod(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	ctx := testsupport.CtxUser(memberA, "editor")

	today := h.elec.Today()
	current := h.seedPeriod(ctx, today.AddDays(-30), today.AddDays(30))
	future := h.seedPeriod(ctx, today.AddDays(31), today.AddDays(365))

	_, result := h.call(secret, "home_electricity_summary", nil)
	if isError(result) {
		t.Fatalf("home_electricity_summary refused: %s", resultText(t, result))
	}
	text := resultText(t, result)
	if !strings.Contains(text, current) {
		t.Fatalf("the summary did not answer for the period containing today:\n%s", text)
	}
	if strings.Contains(text, future) {
		t.Fatalf("the summary answered for a period that has not started — the newest"+
			" starts_on rather than the one being lived in:\n%s", text)
	}

	// An id that was named is still honoured, and one that does not resolve is
	// still a refusal rather than a fallback to whatever is newest.
	_, named := h.call(secret, "home_electricity_summary", map[string]any{"period_id": future})
	if isError(named) || !strings.Contains(resultText(t, named), future) {
		t.Fatalf("a named period was not honoured: %s", resultText(t, named))
	}
	_, unknown := h.call(secret, "home_electricity_summary", map[string]any{"period_id": "nope"})
	if !isError(unknown) {
		t.Fatalf("an unknown period id was answered rather than refused: %s",
			resultText(t, unknown))
	}
}

// ⚠ AND A HOUSEHOLD WITH NO PERIOD AT ALL IS A SENTENCE, NOT A REFUSAL. It is a
// normal early state, and it is the one case the old hand-rolled resolution
// existed to serve — so the fix keeps it rather than trading one wrong answer for
// another.
func TestElectricitySummaryWithNoPeriodIsASentence(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	_, result := h.call(secret, "home_electricity_summary", nil)
	if isError(result) {
		t.Fatalf("an empty household got a refusal rather than a sentence: %s",
			resultText(t, result))
	}
	if !strings.Contains(resultText(t, result), "zúčtovací období") {
		t.Fatalf("the empty-household answer is not the expected sentence: %s",
			resultText(t, result))
	}
}

// ⚠ `kind` IS REQUIRED AND THE SCHEMA MUST SAY SO. `validateTask` refuses an
// empty kind, so a model that believed the description's "Optional job kind." was
// refused every single time — the tool could not be used at all for its own
// documented shape.
func TestGardenTaskCreateWorksAndNamesItsKinds(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	ctx := testsupport.CtxUser(memberA, "editor")

	year := h.elec.Today().Y
	if _, _, err := h.garden.CreateSeason(ctx, garden.SeasonCreateInput{Year: year}, false); err != nil {
		t.Fatalf("seed season: %v", err)
	}

	_, created := h.call(secret, "home_garden_task_create", map[string]any{
		"title_cs":    "Zalít rajčata",
		"window_from": dates.Date{Y: year, M: time.June, D: 1}.String(),
		"window_to":   dates.Date{Y: year, M: time.June, D: 7}.String(),
		"kind":        "water",
		"season_year": year,
	})
	if isError(created) {
		t.Fatalf("home_garden_task_create refused a well-formed job: %s", resultText(t, created))
	}
	if !strings.Contains(resultText(t, created), "Zalít rajčata") {
		t.Fatalf("the created job did not come back: %s", resultText(t, created))
	}

	// An unrecognised kind is refused BY THE TOOL, naming the legal values, rather
	// than reaching the service and coming back as "Neznámý druh práce."
	_, bad := h.call(secret, "home_garden_task_create", map[string]any{
		"title_cs":    "Něco",
		"window_from": dates.Date{Y: year, M: time.June, D: 1}.String(),
		"window_to":   dates.Date{Y: year, M: time.June, D: 7}.String(),
		"kind":        "pending",
	})
	if !isError(bad) {
		t.Fatalf("an unknown kind was accepted: %s", resultText(t, bad))
	}
	if !strings.Contains(resultText(t, bad), "water") {
		t.Fatalf("the refusal does not name the legal kinds: %s", resultText(t, bad))
	}
}

// ⚠ AN UNRECOGNISED STATUS FILTER IS REFUSED, NOT BOUND. Bound, it matched no row
// and the tool answered "nothing in this window" — telling a household there is no
// garden work when what was wrong was the filter.
func TestGardenTaskFilterRefusesAnUnknownStatus(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	_, result := h.call(secret, "home_garden_tasks", map[string]any{"status": "pending"})
	if !isError(result) {
		t.Fatalf("an unknown status silently narrowed the query to nothing: %s",
			resultText(t, result))
	}
	if !strings.Contains(resultText(t, result), "open") {
		t.Fatalf("the refusal does not name the legal statuses: %s", resultText(t, result))
	}
	// The unfiltered call still answers.
	if _, ok := h.call(secret, "home_garden_tasks", nil); isError(ok) {
		t.Fatalf("the unfiltered call was refused: %s", resultText(t, ok))
	}
}

// ---- validation, per writing module (HANDOFF-13 §16.2) ----

// ⚠ ONE NEGATIVE NUMBER, ONE EMPTY STRING AND ONE OUT-OF-RANGE CASE PER WRITING
// MODULE (D311). An agent RETRIES a 500 and gives up on a 422, so a household
// typo answered with the wrong code turns a mistake into a loop — and the four
// modules PR 2 adds carry the most hand-written validation in the catalog
// (validMonth, validIncome, validRates' four-value sum, validGardenDate, and
// toDkwh/toHaler's decimal tolerance), none of which PR 1's table could reach.
func TestPR2WriteToolsValidateBeforeService(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"documents: blank id", "home_documents_update", map[string]any{"id": "  ", "title": "x"}},
		{"documents: empty title", "home_documents_update", map[string]any{"id": "d", "title": "   "}},
		{"documents: no change at all", "home_documents_update", map[string]any{"id": "d"}},
		{"documents: bad pin scope", "home_documents_pin", map[string]any{"id": "d", "scope": "world", "pinned": true}},

		{"finance: malformed month", "home_finance_month_create", map[string]any{"month": "2026-1", "income_kaja": 1, "income_andy": 1, "rates": evenRates()}},
		{"finance: negative income", "home_finance_month_create", map[string]any{"month": "2026-01", "income_kaja": -1, "income_andy": 1, "rates": evenRates()}},
		{"finance: create with no rates at all", "home_finance_month_create", map[string]any{"month": "2026-01", "income_kaja": 1, "income_andy": 1}},
		{"finance: rates out of range", "home_finance_month_create", map[string]any{
			"month": "2026-01", "income_kaja": 1, "income_andy": 1,
			"rates": map[string]any{"personal": 120, "operational": -20, "fun": 0, "no_fun": 0}}},
		{"finance: rates that do not sum to 100", "home_finance_month_create", map[string]any{
			"month": "2026-01", "income_kaja": 1, "income_andy": 1,
			"rates": map[string]any{"personal": 10, "operational": 10, "fun": 10, "no_fun": 10}}},
		{"finance: update with no change", "home_finance_month_update", map[string]any{"id": "m"}},

		{"garden: empty title", "home_garden_task_create", map[string]any{
			"title_cs": "  ", "window_from": "2026-06-01", "window_to": "2026-06-07", "kind": "water"}},
		{"garden: malformed window", "home_garden_task_create", map[string]any{
			"title_cs": "x", "window_from": "01/06/2026", "window_to": "2026-06-07", "kind": "water"}},
		{"garden: window ends before it starts", "home_garden_task_create", map[string]any{
			"title_cs": "x", "window_from": "2026-06-07", "window_to": "2026-06-01", "kind": "water"}},
		{"garden: negative area", "home_garden_planting_create", map[string]any{"plant_id": "p", "area_m2": -1}},
		{"garden: zero harvest", "home_garden_harvest_log", map[string]any{"planting_id": "p", "quantity": 0}},
		{"garden: negative harvest", "home_garden_harvest_log", map[string]any{"planting_id": "p", "quantity": -3}},

		{"electricity: missing a register", "home_electricity_reading_add", map[string]any{"vt_kwh": 1.0}},
		{"electricity: negative register", "home_electricity_reading_add", map[string]any{"vt_kwh": -1.0, "nt_kwh": 1.0}},
		{"electricity: more precision than the meter shows", "home_electricity_reading_add", map[string]any{"vt_kwh": 1.234, "nt_kwh": 1.0}},
		{"electricity: malformed date", "home_electricity_reading_add", map[string]any{"vt_kwh": 1.0, "nt_kwh": 1.0, "read_on": "01/01/2026"}},
		{"electricity: negative advance", "home_electricity_advance_add", map[string]any{"effective_from": "2026-01-01", "amount_kc": -100.0, "due_day": 15}},
		{"electricity: due day out of range", "home_electricity_advance_add", map[string]any{"effective_from": "2026-01-01", "amount_kc": 100.0, "due_day": 32}},
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
			// ⚠ AND NOT AS AN INTERNAL ERROR — the one answer that turns a typo into
			// a retry loop.
			if strings.Contains(resultText(t, result), "Došlo k chybě") {
				t.Fatalf("a validation failure surfaced as an INTERNAL error: %s",
					resultText(t, result))
			}
		})
	}
}

// ⚠ ONE AUDITED WRITE PER WRITING MODULE (§16.2), through the module's own
// service, carrying `via='mcp'` and the token. PR 1 asserted this for `todo`; the
// property is G2's and it holds for every module or for none.
func TestPR2WritesAreAttributedToTheToken(t *testing.T) {
	h := newHarness(t)
	secret, tok := h.mintToken(memberA, "Claude (notebook)", nil, time.Time{})
	ctx := testsupport.CtxUser(memberA, "editor")

	docID := h.seedDocument(memberA, "Návod k myčce", "shared")
	year := h.elec.Today().Y
	if _, _, err := h.garden.CreateSeason(ctx, garden.SeasonCreateInput{Year: year}, false); err != nil {
		t.Fatalf("seed season: %v", err)
	}

	writes := []struct {
		module, action, tool string
		args                 map[string]any
	}{
		{"documents", "document.update", "home_documents_update",
			map[string]any{"id": docID, "title": "Návod k myčce Bosch"}},
		{"finance", "month.create", "home_finance_month_create",
			map[string]any{"month": "2026-02", "income_kaja": 42000, "income_andy": 38000, "rates": evenRates()}},
		{"garden", "task.create", "home_garden_task_create", map[string]any{
			"title_cs":    "Zalít skleník",
			"window_from": dates.Date{Y: year, M: time.July, D: 1}.String(),
			"window_to":   dates.Date{Y: year, M: time.July, D: 7}.String(),
			"kind":        "water", "season_year": year}},
		{"electricity", "reading.create", "home_electricity_reading_add",
			map[string]any{"vt_kwh": 12345.6, "nt_kwh": 6789.0}},
	}
	for _, w := range writes {
		t.Run(w.module, func(t *testing.T) {
			_, result := h.call(secret, w.tool, w.args)
			if isError(result) {
				t.Fatalf("%s refused a well-formed write: %s", w.tool, resultText(t, result))
			}
			var via, viaToken, label string
			err := h.db.QueryRow(`
				SELECT via, COALESCE(via_token_id, ''), COALESCE(actor_label, '')
				  FROM audit_events
				 WHERE module = ? AND action = ? AND via IS NOT NULL`,
				w.module, w.action).Scan(&via, &viaToken, &label)
			if err != nil {
				t.Fatalf("no attributed %s.%s event: %v", w.module, w.action, err)
			}
			if via != "mcp" {
				t.Fatalf("via is %q, want \"mcp\"", via)
			}
			if viaToken != tok.ID {
				t.Fatalf("via_token_id is %q, want the token's id", viaToken)
			}
			if !strings.Contains(label, "Claude (notebook)") {
				t.Fatalf("actor_label does not name the token: %q", label)
			}
		})
	}
}

// ⚠ A READER'S RESOURCE LISTING KEEPS ITS CHAT ATTACHMENTS. Going through
// Service.Cleanup imported that screen's WRITE gate — member ∧ (editor|admin),
// D241 — into a read listing, the host swallowed the 403 so the rest of the
// listing survived, and a reader was silently handed a tree with no chat in it.
func TestReaderResourceListingKeepsChatAttachments(t *testing.T) {
	h := newHarness(t)
	conversationID := h.seedConversation("Rodina", "user-r")
	attachmentID := h.seedAttachment(conversationID, "user-r", "recept.txt", "guláš")

	h.seedSession("user-r", "Reader", "reader")
	secret, _ := h.mintToken("user-r", "Claude", nil, time.Time{})

	body := h.rpc(secret, "resources/list", nil).Body.String()
	if !strings.Contains(body, attachmentID) {
		t.Fatalf("a reader's listing lost every chat attachment — the read listing is"+
			" behind the clean-up screen's write gate:\n%s", body)
	}
	// And the read itself still works for them.
	rr := h.rpc(secret, "resources/read", map[string]any{
		"uri": "home://chat/" + conversationID + "/attachments/" + attachmentID,
	})
	if !strings.Contains(rr.Body.String(), "guláš") {
		t.Fatalf("a reader could not read an attachment they are listed: %s", rr.Body.String())
	}
}

// ⚠ `logging` IS SEARCHABLE, SO IT IS NAMEABLE. Its hits come back labelled
// `logging.event`; a model handed those and then refused `in: ["logging"]` has
// been told the module both exists and does not.
func TestSearchAcceptsTheModulesItCanActuallySearch(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})
	h.createSharedNoteAs(memberA, "Zahradní nůžky", "koupit")

	_, result := h.call(secret, "home_search", map[string]any{
		"query": "Zahradní", "in": []string{"logging"},
	})
	if isError(result) {
		t.Fatalf("in:[\"logging\"] was refused, though logging answers an unfiltered"+
			" search: %s", resultText(t, result))
	}
	if searchCounts(t, result)["logging"] == 0 {
		t.Fatalf("the filtered search reached logging and found nothing: %s",
			resultText(t, result))
	}
	// A name that belongs to no provider is still refused, and the refusal still
	// lists what is allowed.
	_, unknown := h.call(secret, "home_search", map[string]any{
		"query": "Zahradní", "in": []string{"poznamky"},
	})
	if !isError(unknown) {
		t.Fatalf("an unknown module name was accepted: %s", resultText(t, unknown))
	}
}

// evenRates is a valid four-value block summing to 100 — what a create needs and
// what the tests under test are not about.
func evenRates() map[string]any {
	return map[string]any{"personal": 25, "operational": 25, "fun": 25, "no_fun": 25}
}

// ---- fixtures ----

// seedPeriod creates one billing period directly through the service. ⚠ v11
// publishes NO period tool (D295/D311), so this is the only way to put one in
// front of home_electricity_summary — which is the whole reason the summary's
// choice of period had never been exercised.
func (h *harness) seedPeriod(ctx context.Context, startsOn, endsOn dates.Date) string {
	h.t.Helper()
	p, err := h.elec.CreatePeriod(ctx, electricity.PeriodInput{StartsOn: &startsOn, EndsOn: &endsOn})
	if err != nil {
		h.t.Fatalf("seed period %s–%s: %v", startsOn, endsOn, err)
	}
	return p.ID
}

// ⚠ A TOOL'S `required` LIST MUST AGREE WITH WHAT ITS SERVICE ACTUALLY REFUSES,
// and this is the assertion PR 2 was missing. Two tools shipped declaring a field
// optional that their service demands — garden's `kind` (validateTask refuses an
// empty one) and finance's `rates` (resolveRates(nil) is "Sazby jsou povinné.") —
// so a model following the published schema was refused on every attempt, with a
// message about a field it had been told it could leave out.
//
// ⚠ IT IS WRITTEN OUT RATHER THAN DERIVED, because deriving it would repeat the
// guess the schema makes and agree with any mistake in it. These are the fields a
// call cannot succeed without.
func TestWriteSchemasDeclareWhatTheServiceDemands(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	var env struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				InputSchema struct {
					Required []string `json:"required"`
				} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(h.rpc(secret, "tools/list", nil).Body.Bytes(), &env); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	want := map[string][]string{
		"home_garden_task_create":      {"title_cs", "window_from", "window_to", "kind"},
		"home_finance_month_create":    {"month", "income_kaja", "income_andy", "rates"},
		"home_garden_harvest_log":      {"planting_id", "quantity"},
		"home_electricity_reading_add": {"vt_kwh", "nt_kwh"},
		"home_electricity_advance_add": {"effective_from", "amount_kc", "due_day"},
		"home_documents_pin":           {"id", "scope", "pinned"},
	}
	seen := map[string]bool{}
	for _, tool := range env.Result.Tools {
		fields, ok := want[tool.Name]
		if !ok {
			continue
		}
		seen[tool.Name] = true
		for _, f := range fields {
			if !slices.Contains(tool.InputSchema.Required, f) {
				t.Errorf("%s does not declare %q required, though a call without it is"+
					" refused: %v", tool.Name, f, tool.InputSchema.Required)
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s is not in tools/list at all", name)
		}
	}
}
