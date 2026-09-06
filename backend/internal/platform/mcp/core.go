package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The seven cross-cutting tools (PRD §V11-4 FR-M4).
//
// They belong to the HOST rather than to any module, and each one earns that:
// whoami is about the credential, search and get fan out across every provider,
// today/metrics/lists read the two catalogs the admin module already publishes
// through, and activity reads the audit spine. None of them touches a module's
// tables — which is the same rule the providers follow, applied to the host.

// coreHandlerFunc is one core tool's implementation.
type coreHandlerFunc func(ctx context.Context, s *callSession, args json.RawMessage) (Result, error)

const (
	toolWhoami   = "home_whoami"
	toolSearch   = "home_search"
	toolGet      = "home_get"
	toolToday    = "home_today"
	toolMetrics  = "home_metrics"
	toolLists    = "home_lists"
	toolActivity = "home_activity"
)

// coreTools is the published catalog half.
//
// ⚠ EVERY DESCRIPTION IS ONE ENGLISH SENTENCE SAYING WHAT THE TOOL RETURNS AND
// WHAT IT COSTS. It is the whole of a model's tool-selection evidence, and a model
// choosing the wrong tool is a documentation defect with no failing test — which
// is why HANDOFF-13 §16.3 budgets a session of actually using this in Czech on
// real household data and fixing the descriptions the model got wrong.
func (h *Host) coreTools() []Tool {
	return []Tool{
		{
			Name:        toolWhoami,
			Title:       "Who am I",
			Description: "Returns the calling member's id, display name, roles, which modules this token exposes, the household timezone and today's date; call it once at the start of a session so every later date is unambiguous.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			ReadOnly:    true,
		},
		{
			Name:        toolSearch,
			Title:       "Search the household",
			Description: "Searches notes, documents, tasks, events, garden records and chat at once and returns a MERGE ordered by exact-title-match then recency — not a relevance ranking, because scores from separate indexes are not comparable — with a per-module budget so one chatty module cannot crowd out the rest.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "The search terms, in Czech, as the member would type them."},
    "in": {"type": "array", "items": {"type": "string"}, "description": "Optional module names to restrict the search to, e.g. [\"notes\",\"documents\"]."},
    "limit": {"type": "integer", "minimum": 1, "description": "Maximum hits PER MODULE."}
  },
  "required": ["query"],
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
		{
			Name:        toolGet,
			Title:       "Get one entity",
			Description: "Returns one entity in full by the kind and id a search hit reported, e.g. kind \"notes.note\"; use it after home_search rather than guessing an id.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "kind": {"type": "string", "description": "The hit's kind, e.g. \"notes.note\" or \"todo.card\"."},
    "id": {"type": "string"}
  },
  "required": ["kind", "id"],
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
		{
			Name:        toolToday,
			Title:       "What is due today",
			Description: "Returns everything due or overdue for the calling member today — reminders, tasks and garden work — composed entirely from the published metric and list catalogs, so it costs no module query; it deliberately carries no chat unread count.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "as_of": {"type": "string", "description": "Optional ISO date (YYYY-MM-DD) to answer for instead of today, in the household timezone."}
  },
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
		{
			Name:        toolMetrics,
			Title:       "Household metrics",
			Description: "With no arguments lists every published metric and what it counts; with keys, resolves those metrics to numbers for the calling member — two jobs in one tool, deliberately, to spend one slot instead of two.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "keys": {"type": "array", "items": {"type": "string"}, "description": "Metric keys to resolve. Omit to list the catalog instead."},
    "as_of": {"type": "string", "description": "Optional ISO date (YYYY-MM-DD), in the household timezone."}
  },
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
		{
			Name:        toolLists,
			Title:       "Household lists",
			Description: "With no arguments lists every published list and what it names; with keys, resolves those lists to their items for the calling member — the WHICH behind home_metrics's HOW MANY.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "keys": {"type": "array", "items": {"type": "string"}, "description": "List keys to resolve. Omit to list the catalog instead."},
    "as_of": {"type": "string", "description": "Optional ISO date (YYYY-MM-DD), in the household timezone."}
  },
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
		{
			Name:        toolActivity,
			Title:       "Recent household activity",
			Description: "Returns a digest of recent changes from the audit log — who changed what, when, and whether it came through an assistant — redacted so another member's private notes and documents show only that something private happened; admin only, as the Log itself is.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "since": {"type": "string", "description": "ISO timestamp or date; only changes at or after this are returned."},
    "until": {"type": "string", "description": "Optional ISO timestamp or date upper bound."},
    "module": {"type": "string", "description": "Optional module name, e.g. \"todo\"."},
    "action": {"type": "string", "description": "Optional bare action verb, e.g. \"card.move\"."},
    "actor": {"type": "string", "description": "Optional member id."},
    "entity_type": {"type": "string"},
    "entity_id": {"type": "string"},
    "limit": {"type": "integer", "minimum": 1}
  },
  "required": ["since"],
  "additionalProperties": false
}`),
			ReadOnly: true,
			// ⚠ THE ONE ADMIN-ONLY TOOL IN PR 1, and it is admin-only because its
			// HTTP twin is: /api/logs/** has been behind httpx.RequireAdmin since D5,
			// so a reader who is refused the Log in the browser must be refused this
			// digest through a token. Redaction (leak row 7) is the SECOND rule here,
			// not the first — it hides another member's private ITEMS from an admin,
			// and it was never the thing keeping the spine away from a reader.
			AdminOnly: true,
		},
	}
}

func (h *Host) coreHandler(name string) (coreHandlerFunc, bool) {
	switch name {
	case toolWhoami:
		return h.whoami, true
	case toolSearch:
		return h.search, true
	case toolGet:
		return h.get, true
	case toolToday:
		return h.today, true
	case toolMetrics:
		return h.metrics, true
	case toolLists:
		return h.lists, true
	case toolActivity:
		return h.activity, true
	default:
		return nil, false
	}
}

// ---- home_whoami ----

func (h *Host) whoami(ctx context.Context, s *callSession, _ json.RawMessage) (Result, error) {
	actor, ok := reqctx.ActorFrom(ctx)
	if !ok {
		// ⚠ AN ACTOR-LESS CTX IS AN ERROR, NEVER A DEFAULT (D302). It cannot happen
		// through the dispatcher — the bearer resolves before dispatch — which is
		// exactly why it must fail loudly rather than answering for nobody.
		return Result{}, fmt.Errorf("mcp: whoami without an actor")
	}
	mods := s.principal.Token.Modules
	scope := "všechny moduly, které vidí vlastník"
	if len(mods) > 0 {
		scope = strings.Join(mods, ", ")
	}
	now := h.now().In(h.deps.Config.Timezone)
	payload := map[string]any{
		"user_id":      actor.UserID,
		"display_name": s.principal.Identity.DisplayName,
		"roles":        actor.Roles,
		"can_write":    reqctx.CanWrite(ctx),
		"token_name":   s.principal.Token.Name,
		"modules":      mods,
		"timezone":     h.deps.Config.TimezoneName,
		"today":        now.Format("2006-01-02"),
		"now":          now.Format(time.RFC3339),
	}
	name := s.principal.Identity.DisplayName
	if name == "" {
		name = actor.UserID
	}
	text := fmt.Sprintf("%s (%s), role: %s. Token: %s. Rozsah: %s. Časové pásmo %s, dnes je %s.",
		name, actor.UserID, strings.Join(actor.Roles, ", "), s.principal.Token.Name, scope,
		h.deps.Config.TimezoneName, now.Format("2. 1. 2006"))
	return jsonResult(text, payload)
}

// ---- home_search ----

type searchArgs struct {
	Query string   `json:"query"`
	In    []string `json:"in"`
	Limit int      `json:"limit"`
}

// search fans out to every provider the token is offered and merges.
//
// ⚠ THERE IS NO CROSS-MODULE SQL AND THERE CANNOT BE (D299): `internal/arch`
// forbids it, and the FTS tables are external-content anyway. Each module answers
// over its own index, viewer-scoped, with its own LIMIT; the host merges.
func (h *Host) search(ctx context.Context, s *callSession, args json.RawMessage) (Result, error) {
	var in searchArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return Result{}, httpx.ErrUnprocessable("Neplatné parametry vyhledávání.")
	}
	if strings.TrimSpace(in.Query) == "" {
		return Result{}, httpx.ErrUnprocessable("Zadejte hledaný výraz.")
	}
	limit := h.deps.Config.SearchLimit
	if in.Limit > 0 && in.Limit < limit {
		limit = in.Limit
	}

	wanted := moduleSet(in.In)
	order := map[string]int{}
	counts := map[string]int{}
	var hits []Hit
	for i, p := range h.searchModules(s.principal.Token) {
		order[p.Module()] = i
		if wanted != nil && !wanted[p.Module()] {
			continue
		}
		got, err := p.Search(ctx, Query{Text: in.Query, Limit: limit})
		if err != nil {
			// ⚠ ONE MODULE'S FAILURE MUST NOT BLANK THE WHOLE SEARCH, and it must not
			// be silent either: a search that quietly returns four modules out of six
			// reads as "there is nothing there", which is the wrong answer to act on.
			h.logTool(ctx, "mcp search failed", p.Module(), err)
			counts[p.Module()] = -1
			continue
		}
		counts[p.Module()] = len(got)
		hits = append(hits, got...)
	}
	sortHits(hits, order)

	return jsonResult(renderHits(in.Query, hits, counts), map[string]any{
		"query":  in.Query,
		"hits":   hits,
		"counts": counts,
	})
}

func renderHits(query string, hits []Hit, counts map[string]int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Nalezeno %d položek pro %q.\n", len(hits), query)
	// ⚠ The per-module counts are REPORTED (D301). A search that returns 40 chat
	// messages and no notes because chat is chattier is a worse answer than 8 of
	// each, and a model that cannot see the budget cannot tell the two apart.
	mods := make([]string, 0, len(counts))
	for m := range counts {
		mods = append(mods, m)
	}
	sort.Strings(mods)
	parts := make([]string, 0, len(mods))
	for _, m := range mods {
		if counts[m] < 0 {
			parts = append(parts, m+": chyba")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %d", m, counts[m]))
	}
	if len(parts) > 0 {
		b.WriteString("Po modulech — " + strings.Join(parts, ", ") + ".\n")
	}
	b.WriteString("Pořadí je sloučení (přesná shoda v názvu, pak podle poslední změny), ne relevance.\n")
	for _, hit := range hits {
		fmt.Fprintf(&b, "\n• [%s] %s (id %s)", hit.Kind, hit.Title, hit.ID)
		if hit.URI != "" {
			fmt.Fprintf(&b, "\n  %s", hit.URI)
		}
		if hit.Snippet != "" {
			fmt.Fprintf(&b, "\n  %s", hit.Snippet)
		}
	}
	return b.String()
}

// ---- home_get ----

type getArgs struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// get resolves one entity through the owning module's own tool surface.
//
// ⚠ IT IS A ROUTER, NOT A SECOND READ PATH. The module already publishes a detail
// verb for anything it makes findable; home_get exists so a model that has a hit
// does not have to know which module's tool to reach for next. Everything it
// returns went through that module's own access checks.
func (h *Host) get(ctx context.Context, s *callSession, args json.RawMessage) (Result, error) {
	var in getArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return Result{}, httpx.ErrUnprocessable("Neplatné parametry.")
	}
	if in.Kind == "" || in.ID == "" {
		return Result{}, httpx.ErrUnprocessable("Zadejte kind i id.")
	}
	module := moduleOf(in.Kind)
	if allowed := moduleSet(s.principal.Token.Modules); allowed != nil && !allowed[module] {
		return NotFoundResult(), nil
	}
	p, ok := h.deps.Registry.Provider(module)
	if !ok {
		return NotFoundResult(), nil
	}
	getter, ok := p.(EntityGetter)
	if !ok {
		return NotFoundResult(), nil
	}
	return getter.Get(ctx, in.Kind, in.ID)
}

// EntityGetter is the optional half of Provider that answers home_get. A module
// with nothing addressable by (kind, id) simply does not implement it, and
// home_get then answers exactly as it does for an id that does not exist.
type EntityGetter interface {
	Get(ctx context.Context, kind, id string) (Result, error)
}

// ---- home_today / home_metrics / home_lists ----

// asOf reads the optional date argument, in the household timezone.
func (h *Host) asOf(raw string) (time.Time, error) {
	loc := h.deps.Config.Timezone
	if raw == "" {
		return h.now().In(loc), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", raw, loc); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.In(loc), nil
	}
	return time.Time{}, httpx.ErrUnprocessable("Datum musí být ve tvaru RRRR-MM-DD.")
}

type todayArgs struct {
	AsOf string `json:"as_of"`
}

// today composes the day from the metric and list catalogs and NOTHING ELSE.
//
// ⚠ THE MOST-CALLED TOOL IN THE SYSTEM TOUCHES NO MODULE TABLE (D310). Every
// figure here is a metric or a list some module already publishes, resolved per
// recipient exactly as a scheduled summary resolves it — so this adds no
// cross-module read and no new query.
//
// ⚠ AND IT CARRIES NO UNREAD COUNT. D252 kept chat out of both catalogs; an
// unread figure here would make the one tool that touches no module table reach
// into one. Unread rides on home_chat_conversations, where it is free.
func (h *Host) today(ctx context.Context, _ *callSession, args json.RawMessage) (Result, error) {
	var in todayArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return Result{}, httpx.ErrUnprocessable("Neplatné parametry.")
		}
	}
	at, err := h.asOf(in.AsOf)
	if err != nil {
		return Result{}, err
	}
	userID := reqctx.ActorID(ctx)
	if userID == "" {
		return Result{}, fmt.Errorf("mcp: home_today without an actor")
	}

	numbers := map[string]any{}
	var b strings.Builder
	fmt.Fprintf(&b, "Přehled na %s.\n", at.Format("2. 1. 2006"))
	for _, d := range h.deps.Metrics.Catalog() {
		v, err := h.deps.Metrics.Resolve(ctx, userID, d.Key, at)
		if err != nil {
			h.logTool(ctx, "mcp home_today metric failed", d.Key, err)
			continue
		}
		numbers[d.Key] = v
		if v == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s: %d %s", d.Label, v, d.Unit)
	}
	items := map[string]any{}
	for _, d := range h.deps.Lists.Catalog() {
		got, err := h.deps.Lists.Resolve(ctx, userID, d.Key, at)
		if err != nil {
			h.logTool(ctx, "mcp home_today list failed", d.Key, err)
			continue
		}
		items[d.Key] = got
		if len(got) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n\n%s:", d.Label)
		for _, line := range got {
			fmt.Fprintf(&b, "\n  • %s", line)
		}
	}
	return jsonResult(b.String(), map[string]any{
		"as_of":   at.Format("2006-01-02"),
		"metrics": numbers,
		"lists":   items,
	})
}

type catalogArgs struct {
	Keys []string `json:"keys"`
	AsOf string   `json:"as_of"`
}

func (h *Host) metrics(ctx context.Context, _ *callSession, args json.RawMessage) (Result, error) {
	var in catalogArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return Result{}, httpx.ErrUnprocessable("Neplatné parametry.")
		}
	}
	if len(in.Keys) == 0 {
		descs := h.deps.Metrics.Catalog()
		var b strings.Builder
		fmt.Fprintf(&b, "%d publikovaných metrik:\n", len(descs))
		for _, d := range descs {
			fmt.Fprintf(&b, "\n• %s — %s (%s)", d.Key, d.Label, d.Unit)
		}
		return jsonResult(b.String(), map[string]any{"catalog": descs})
	}
	at, err := h.asOf(in.AsOf)
	if err != nil {
		return Result{}, err
	}
	userID := reqctx.ActorID(ctx)
	if userID == "" {
		return Result{}, fmt.Errorf("mcp: home_metrics without an actor")
	}
	values := map[string]any{}
	var b strings.Builder
	for _, key := range in.Keys {
		v, err := h.deps.Metrics.Resolve(ctx, userID, key, at)
		if err != nil {
			// An unknown key is the caller's mistake and is worth naming — it is the
			// one error here a model can act on by asking for the catalog.
			return Result{Text: fmt.Sprintf("Neznámá metrika %q. Zavolejte home_metrics bez parametrů pro seznam.", key), IsError: true}, nil
		}
		values[key] = v
		fmt.Fprintf(&b, "%s: %d\n", key, v)
	}
	return jsonResult(strings.TrimRight(b.String(), "\n"), map[string]any{
		"as_of":  at.Format("2006-01-02"),
		"values": values,
	})
}

func (h *Host) lists(ctx context.Context, _ *callSession, args json.RawMessage) (Result, error) {
	var in catalogArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return Result{}, httpx.ErrUnprocessable("Neplatné parametry.")
		}
	}
	if len(in.Keys) == 0 {
		descs := h.deps.Lists.Catalog()
		var b strings.Builder
		fmt.Fprintf(&b, "%d publikovaných seznamů:\n", len(descs))
		for _, d := range descs {
			fmt.Fprintf(&b, "\n• %s — %s", d.Key, d.Label)
		}
		return jsonResult(b.String(), map[string]any{"catalog": descs})
	}
	at, err := h.asOf(in.AsOf)
	if err != nil {
		return Result{}, err
	}
	userID := reqctx.ActorID(ctx)
	if userID == "" {
		return Result{}, fmt.Errorf("mcp: home_lists without an actor")
	}
	values := map[string]any{}
	var b strings.Builder
	for _, key := range in.Keys {
		got, err := h.deps.Lists.Resolve(ctx, userID, key, at)
		if err != nil {
			return Result{Text: fmt.Sprintf("Neznámý seznam %q. Zavolejte home_lists bez parametrů pro seznam.", key), IsError: true}, nil
		}
		values[key] = got
		fmt.Fprintf(&b, "%s:\n", key)
		if len(got) == 0 {
			b.WriteString("  (prázdné)\n")
		}
		for _, line := range got {
			fmt.Fprintf(&b, "  • %s\n", line)
		}
	}
	return jsonResult(strings.TrimRight(b.String(), "\n"), map[string]any{
		"as_of":  at.Format("2006-01-02"),
		"values": values,
	})
}

// ---- home_activity ----

type activityArgs struct {
	Since      string `json:"since"`
	Until      string `json:"until"`
	Module     string `json:"module"`
	Action     string `json:"action"`
	Actor      string `json:"actor"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Limit      int    `json:"limit"`
}

// activity returns the audit digest, redacted for the caller.
//
// ⚠ LEAK ROW 7 (D298). The Log PAGE redacts — four fixed phrases by entity_type —
// and a tool reading the raw table would bypass every one of those rules, for
// every member's private items, in one call. The redaction is applied by
// audit.ActivityReader, which routes through RedactRendered: one helper, two
// consumers, so the two cannot drift.
func (h *Host) activity(ctx context.Context, _ *callSession, args json.RawMessage) (Result, error) {
	var in activityArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return Result{}, httpx.ErrUnprocessable("Neplatné parametry.")
	}
	since, err := h.parseBound(in.Since, true)
	if err != nil {
		return Result{}, err
	}
	until, err := h.parseBound(in.Until, false)
	if err != nil {
		return Result{}, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	viewer := reqctx.ActorID(ctx)
	if viewer == "" {
		return Result{}, fmt.Errorf("mcp: home_activity without an actor")
	}
	entries, err := h.deps.Activity.Activity(ctx, audit.ActivityFilter{
		Since:      since,
		Until:      until,
		Module:     in.Module,
		Action:     in.Action,
		Actor:      in.Actor,
		EntityType: in.EntityType,
		EntityID:   in.EntityID,
		Limit:      limit,
	}, viewer)
	if err != nil {
		return Result{}, err
	}

	rows := make([]map[string]any, 0, len(entries))
	var b strings.Builder
	fmt.Fprintf(&b, "%d změn od %s.\n", len(entries), since.In(h.deps.Config.Timezone).Format("2. 1. 2006 15:04"))
	for _, e := range entries {
		who := e.ActorLabel
		if who == "" {
			who = e.ActorUser
		}
		row := map[string]any{
			"ts":       e.TS.Format(time.RFC3339),
			"actor":    who,
			"module":   e.Module,
			"action":   e.Action,
			"summary":  e.Summary,
			"redacted": e.Redacted,
			"via":      e.Via,
		}
		if e.EntityID != "" {
			row["entity_id"] = e.EntityID
		}
		rows = append(rows, row)
		via := ""
		if e.Via == audit.ViaMCP {
			via = " [přes asistenta]"
		}
		fmt.Fprintf(&b, "\n• %s — %s: %s%s",
			e.TS.In(h.deps.Config.Timezone).Format("2. 1. 15:04"), who, e.Summary, via)
	}
	return jsonResult(b.String(), map[string]any{"entries": rows})
}

// parseBound reads a date-or-timestamp window bound. A bare date is read as the
// START of that day when it is the lower bound and the END of it when it is the
// upper — so `since: "2026-09-01", until: "2026-09-01"` is that whole day, which
// is what somebody asking for one day means.
func (h *Host) parseBound(raw string, lower bool) (time.Time, error) {
	if raw == "" {
		if lower {
			return time.Time{}, httpx.ErrUnprocessable("Zadejte since (datum nebo čas).")
		}
		return time.Time{}, nil
	}
	loc := h.deps.Config.Timezone
	if t, err := time.ParseInLocation("2006-01-02", raw, loc); err == nil {
		if lower {
			return t, nil
		}
		return t.AddDate(0, 0, 1).Add(-time.Nanosecond), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	return time.Time{}, httpx.ErrUnprocessable("Čas musí být ve tvaru RRRR-MM-DD nebo RFC3339.")
}

// jsonResult is TextResult under the core handlers' own name — the providers'
// helper, used by the host's seven for the same reason and with the same
// degradation rule. One implementation, so the two halves of the catalog cannot
// answer a marshal failure differently.
func jsonResult(text string, payload any) (Result, error) { return TextResult(text, payload) }
