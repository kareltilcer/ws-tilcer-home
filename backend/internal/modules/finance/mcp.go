package finance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The finance module's MCP provider (v11, PRD §V11-4 FR-M4).
//
// ⚠ WHAT IS ABSENT, BY NAME: the month DELETE. finance has no soft delete at all
// (D87) — a removed month is gone, and the split it recorded with it — so it is
// the one module here where "delete" and "destroy" are the same word. It is not
// gated and not confirmed; it does not exist (D295).
//
// ⚠ THE SPLIT IS DERIVED AND NEVER SENT. `Compute` runs on every read path, and
// the formula is the one v6 ported verbatim from the retiring `fin` service
// (D82). A tool that accepted a split would be a second source of truth for the
// number this module exists to produce.

type mcpProvider struct {
	mcp.NoResources
	svc *Service
}

// MCPProvider implements mcp.Source.
func (m *Module) MCPProvider() mcp.Provider { return &mcpProvider{svc: m.svc} }

func (p *mcpProvider) Module() string { return "finance" }

const (
	toolMonths      = "home_finance_months"
	toolMonthCreate = "home_finance_month_create"
	toolMonthUpdate = "home_finance_month_update"
)

func (p *mcpProvider) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        toolMonths,
			Title:       "Monthly income split",
			Description: "Lists the household's recorded months newest first, each with both incomes, the four rates and the DERIVED split; pass month for one month by its YYYY-MM key.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "month": {"type": "string", "description": "A single month as YYYY-MM. Resolved directly, so any recorded month answers however far back it is. Do not send limit beside it."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 200, "description": "How many months to list, newest first. Only for the listing — not with month."}
  },
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
		{
			Name:        toolMonthCreate,
			Title:       "Record a month",
			Description: "Records one month's two incomes and its four percentage rates, which must be whole numbers summing to 100; the split is computed from them and never sent. Call home_finance_months first — the previous month's rates are usually the ones to repeat.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "month": {"type": "string", "description": "YYYY-MM. Unique — one row per month."},
    "income_kaja": {"type": "integer", "minimum": 0, "description": "In whole Kč."},
    "income_andy": {"type": "integer", "minimum": 0, "description": "In whole Kč."},
    "rates": {
      "type": "object",
      "description": "All four, and they must sum to 100. A month has no split without them, so there is no default.",
      "properties": {
        "personal": {"type": "integer"},
        "operational": {"type": "integer"},
        "fun": {"type": "integer"},
        "no_fun": {"type": "integer"}
      },
      "required": ["personal", "operational", "fun", "no_fun"],
      "additionalProperties": false
    }
  },
  "required": ["month", "income_kaja", "income_andy", "rates"],
  "additionalProperties": false
}`),
		},
		{
			Name:        toolMonthUpdate,
			Title:       "Correct a month",
			Description: "Changes a recorded month's incomes or its whole four-value rate block; a partial rate block is refused rather than silently zeroing the other three.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string"},
    "income_kaja": {"type": "integer", "minimum": 0},
    "income_andy": {"type": "integer", "minimum": 0},
    "rates": {
      "type": "object",
      "properties": {
        "personal": {"type": "integer"},
        "operational": {"type": "integer"},
        "fun": {"type": "integer"},
        "no_fun": {"type": "integer"}
      },
      "required": ["personal", "operational", "fun", "no_fun"],
      "additionalProperties": false
    }
  },
  "required": ["id"],
  "additionalProperties": false
}`),
		},
	}
}

func (p *mcpProvider) Call(ctx context.Context, name string, args json.RawMessage) (mcp.Result, error) {
	switch name {
	case toolMonths:
		return p.months(ctx, args)
	case toolMonthCreate:
		return p.monthCreate(ctx, args)
	case toolMonthUpdate:
		return p.monthUpdate(ctx, args)
	default:
		return mcp.Result{}, mcp.UnknownToolError(name)
	}
}

type monthsArgs struct {
	Month string `json:"month"`
	Limit int    `json:"limit"`
}

func (p *mcpProvider) months(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in monthsArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	// ⚠ ONE MONTH IS A ROW LOOKUP, NOT A FILTER OVER A PAGE. `Store.ForMonth` is
	// the indexed read the Rozpočet widget and the *_current metrics already use.
	// Filtering the newest `limit` rows in Go instead answered NOT FOUND for every
	// month older than that page — and finance's history arrived here by migration
	// from `fin` (§V6-12), so "older than the newest two dozen" is most of it. A
	// household asking what it earned in a year it has recorded is not an edge
	// case, and being told the month does not exist is the one answer nobody can
	// argue with.
	if in.Month != "" {
		if err := validMonth(in.Month); err != nil {
			return mcp.Result{}, err
		}
		// ⚠ A PAGE SIZE BESIDE A SINGLE-ROW LOOKUP IS REFUSED RATHER THAN DROPPED.
		// It is the rule DecodeArgs was made strict for — an unknown module is a
		// 422, an unknown `via` is a 422, an expiry on PATCH is a 422 rather than a
		// no-op — applied to an argument that IS known and still cannot be honoured.
		// A caller who sent both asked for two different things, and silence about
		// which one won is how a model learns a parameter it sent does nothing.
		if in.Limit != 0 {
			return mcp.Result{}, httpx.ErrUnprocessable(
				"limit nelze zadat spolu s month — month vrací jediný měsíc.")
		}
		m, ok, err := p.svc.Store().ForMonth(ctx, in.Month)
		if err != nil {
			return mcp.Result{}, err
		}
		if !ok {
			return mcp.NotFoundResult(), nil
		}
		return renderMonths([]Month{m})
	}
	// ⚠ A LIMIT PAST THE SERVICE'S BOUND IS REFUSED, NOT SENT. `Service.List`
	// RESETS anything above 200 back to 50 rather than clamping to it, so a
	// request for 300 comes back with fewer rows than a request for 200 — and
	// there is nothing on this wire that could say the number was changed.
	limit := in.Limit
	if limit <= 0 {
		limit = 24
	}
	if limit > monthsListMax {
		return mcp.Result{}, httpx.ErrUnprocessable(
			fmt.Sprintf("limit smí být nejvýše %d.", monthsListMax))
	}
	page, err := p.svc.List(ctx, limit, "")
	if err != nil {
		return mcp.Result{}, err
	}
	return renderMonths(page.Items)
}

// monthsListMax mirrors the ceiling `Service.List` enforces. ⚠ It is a REFUSAL
// bound here rather than a clamp, because the service's own behaviour past it is
// a reset to 50 — the value a caller is least likely to have meant.
const monthsListMax = 200

func renderMonths(items []Month) (mcp.Result, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n", mcp.Plural(len(items), "měsíc", "měsíce", "měsíců"))
	for _, m := range items {
		fmt.Fprintf(&b, "\n• %s (id %s) — Kája %d Kč, Andy %d Kč", m.Month, m.ID, m.IncomeKaja, m.IncomeAndy)
	}
	return mcp.TextResult(b.String(), items)
}

type monthRates struct {
	Personal    *int `json:"personal"`
	Operational *int `json:"operational"`
	Fun         *int `json:"fun"`
	NoFun       *int `json:"no_fun"`
}

func (r *monthRates) input() *RatesInput {
	if r == nil {
		return nil
	}
	return &RatesInput{Personal: r.Personal, Operational: r.Operational, Fun: r.Fun, NoFun: r.NoFun}
}

type monthCreateArgs struct {
	Month      string      `json:"month"`
	IncomeKaja *int        `json:"income_kaja"`
	IncomeAndy *int        `json:"income_andy"`
	Rates      *monthRates `json:"rates"`
}

// monthCreate validates before the service (D311).
func (p *mcpProvider) monthCreate(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in monthCreateArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if err := validMonth(in.Month); err != nil {
		return mcp.Result{}, err
	}
	if in.IncomeKaja == nil || in.IncomeAndy == nil {
		return mcp.Result{}, httpx.ErrUnprocessable("Oba příjmy jsou povinné.")
	}
	if err := validIncome(*in.IncomeKaja, "income_kaja"); err != nil {
		return mcp.Result{}, err
	}
	if err := validIncome(*in.IncomeAndy, "income_andy"); err != nil {
		return mcp.Result{}, err
	}
	// ⚠ THE RATES ARE REQUIRED ON A CREATE, AND THE SCHEMA SAYING OTHERWISE MADE
	// THIS TOOL UNUSABLE. `resolveRates(nil)` is "Sazby jsou povinné." — a month
	// with no rates has no split, which is the number this module exists to
	// produce — so a model that read "All four, or none" and sent none was refused
	// every single time. There is nothing to inherit from and nothing to default
	// to: last month's rates are a guess about this month's intent.
	//
	// ⚠ ON AN UPDATE THEY STAY OPTIONAL, because there the stored block is what an
	// omitted one means. That asymmetry is the service's and this mirrors it
	// rather than flattening it.
	if in.Rates == nil {
		return mcp.Result{}, httpx.ErrUnprocessable(
			"Sazby jsou povinné — zadejte všechny čtyři, dohromady 100 %.")
	}
	if err := validRates(in.Rates); err != nil {
		return mcp.Result{}, err
	}
	m, err := p.svc.Create(ctx, MonthCreate{
		Month:      in.Month,
		IncomeKaja: in.IncomeKaja,
		IncomeAndy: in.IncomeAndy,
		Rates:      in.Rates.input(),
	})
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.TextResult(fmt.Sprintf("Měsíc %s zapsán (id %s).", m.Month, m.ID), m)
}

type monthUpdateArgs struct {
	ID         string      `json:"id"`
	IncomeKaja *int        `json:"income_kaja"`
	IncomeAndy *int        `json:"income_andy"`
	Rates      *monthRates `json:"rates"`
}

func (p *mcpProvider) monthUpdate(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in monthUpdateArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.ID) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Chybí id měsíce.")
	}
	if in.IncomeKaja == nil && in.IncomeAndy == nil && in.Rates == nil {
		return mcp.Result{}, httpx.ErrUnprocessable("Zadejte alespoň jednu změnu.")
	}
	if in.IncomeKaja != nil {
		if err := validIncome(*in.IncomeKaja, "income_kaja"); err != nil {
			return mcp.Result{}, err
		}
	}
	if in.IncomeAndy != nil {
		if err := validIncome(*in.IncomeAndy, "income_andy"); err != nil {
			return mcp.Result{}, err
		}
	}
	if err := validRates(in.Rates); err != nil {
		return mcp.Result{}, err
	}
	m, err := p.svc.Update(ctx, in.ID, MonthUpdate{
		IncomeKaja: in.IncomeKaja,
		IncomeAndy: in.IncomeAndy,
		Rates:      in.Rates.input(),
	})
	if err != nil {
		return mcp.Result{}, err
	}
	if m == nil {
		return mcp.NotFoundResult(), nil
	}
	return mcp.TextResult(fmt.Sprintf("Měsíc %s upraven.", m.Month), m)
}

// Search scans the month keys.
//
// ⚠ D302: an actor-less ctx is an ERROR, never an empty slice — the rule is the
// same in every provider whether or not this one's data happens to be
// household-visible.
func (p *mcpProvider) Search(ctx context.Context, q mcp.Query) ([]mcp.Hit, error) {
	if _, ok := reqctx.ActorFrom(ctx); !ok {
		return nil, fmt.Errorf("finance: search without an actor")
	}
	term := strings.TrimSpace(q.Text)
	if term == "" {
		return nil, nil
	}
	// ⚠ A LIST-AND-FILTER RATHER THAN A QUERY, and it is the honest read for this
	// module: `finance_months` is one row per month and a month key is either a
	// substring of the term or it is not. There is no index to add and nothing an
	// index would save.
	//
	// ⚠ THE BOUND IS THE SERVICE'S OWN CEILING, and asking past it made this scan
	// SMALLER rather than larger: `Service.List` resets anything above 200 back to
	// 50, so the 500 written here was a fifty-row scan wearing a five-hundred-row
	// comment, and a month older than the fiftieth was unfindable.
	page, err := p.svc.List(ctx, monthsListMax, "")
	if err != nil {
		return nil, err
	}
	var hits []mcp.Hit
	for _, m := range page.Items {
		if !strings.Contains(m.Month, term) {
			continue
		}
		if len(hits) >= q.Limit {
			break
		}
		updated, _ := time.Parse(tsFormat, m.UpdatedAt)
		hits = append(hits, mcp.Hit{
			Kind:      "finance.month",
			ID:        m.ID,
			Title:     m.Month,
			Snippet:   fmt.Sprintf("Kája %d Kč, Andy %d Kč", m.IncomeKaja, m.IncomeAndy),
			UpdatedAt: updated,
			ExactHit:  m.Month == term,
		})
	}
	return hits, nil
}

// Get answers home_get.
func (p *mcpProvider) Get(ctx context.Context, kind, id string) (mcp.Result, error) {
	if kind != "finance.month" {
		return mcp.NotFoundResult(), nil
	}
	m, err := p.svc.Get(ctx, id)
	if err != nil {
		return mcp.Result{}, err
	}
	if m == nil {
		return mcp.NotFoundResult(), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (id %s)\nKája %d Kč, Andy %d Kč\nSazby: osobní %d %%, provoz %d %%, zábava %d %%, nezábava %d %%",
		m.Month, m.ID, m.IncomeKaja, m.IncomeAndy,
		m.Rates.Personal, m.Rates.Operational, m.Rates.Fun, m.Rates.NoFun)
	return mcp.TextResult(b.String(), m)
}

// ---- validation ----

func validMonth(month string) error {
	if strings.TrimSpace(month) == "" {
		return httpx.ErrUnprocessable("Chybí měsíc.")
	}
	if _, err := time.Parse("2006-01", month); err != nil {
		return httpx.ErrUnprocessable("Měsíc musí být ve tvaru RRRR-MM.")
	}
	return nil
}

// validIncome refuses a negative before the service sees it (D311).
func validIncome(v int, field string) error {
	if v < 0 {
		return httpx.ErrUnprocessable("Příjem " + field + " nesmí být záporný.")
	}
	return nil
}

// validRates enforces the all-or-nothing block and the sum, in the tool.
//
// ⚠ THE SERVICE CHECKS BOTH AGAIN, and that is the point rather than a
// duplication: this half exists so a model gets a 422-shaped answer it can act
// on instead of whatever the service would have produced, and the service's half
// exists because it is reached from the browser too.
func validRates(r *monthRates) error {
	if r == nil {
		return nil
	}
	if r.Personal == nil || r.Operational == nil || r.Fun == nil || r.NoFun == nil {
		return httpx.ErrUnprocessable("Sazby se zadávají všechny čtyři najednou.")
	}
	// ⚠ AN ORDERED SLICE, NOT A MAP. Ranging a map picks the offending field by
	// Go's randomised iteration order, so two identical calls with two bad rates
	// name two different fields — and a model that corrects the one it was told
	// about is refused again about another. The order here is the schema's.
	for _, f := range []struct {
		name string
		v    int
	}{
		{"personal", *r.Personal}, {"operational", *r.Operational},
		{"fun", *r.Fun}, {"no_fun", *r.NoFun},
	} {
		if f.v < 0 || f.v > 100 {
			return httpx.ErrUnprocessable("Sazba " + f.name + " musí být mezi 0 a 100.")
		}
	}
	if sum := *r.Personal + *r.Operational + *r.Fun + *r.NoFun; sum != 100 {
		return httpx.ErrUnprocessable(fmt.Sprintf("Sazby musí dávat dohromady 100 %%, ne %d %%.", sum))
	}
	return nil
}
