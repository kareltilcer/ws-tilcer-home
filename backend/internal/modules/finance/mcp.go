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
    "month": {"type": "string", "description": "A single month as YYYY-MM."},
    "limit": {"type": "integer", "minimum": 1}
  },
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
		{
			Name:        toolMonthCreate,
			Title:       "Record a month",
			Description: "Records one month's two incomes and its four percentage rates, which must be whole numbers summing to 100; the split is computed from them and never sent.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "month": {"type": "string", "description": "YYYY-MM. Unique — one row per month."},
    "income_kaja": {"type": "integer", "minimum": 0, "description": "In whole Kč."},
    "income_andy": {"type": "integer", "minimum": 0, "description": "In whole Kč."},
    "rates": {
      "type": "object",
      "description": "All four, or none. They must sum to 100.",
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
  "required": ["month", "income_kaja", "income_andy"],
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
	limit := in.Limit
	if limit <= 0 {
		limit = 24
	}
	page, err := p.svc.List(ctx, limit, "")
	if err != nil {
		return mcp.Result{}, err
	}
	items := page.Items
	if in.Month != "" {
		if err := validMonth(in.Month); err != nil {
			return mcp.Result{}, err
		}
		items = nil
		for _, m := range page.Items {
			if m.Month == in.Month {
				items = append(items, m)
			}
		}
		if len(items) == 0 {
			return mcp.NotFoundResult(), nil
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d měsíců:\n", len(items))
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
	// module: `finance_months` is a few dozen rows and a month key is either a
	// prefix of the term or it is not. There is no index to add and nothing an
	// index would save.
	page, err := p.svc.List(ctx, 500, "")
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
	for name, v := range map[string]int{
		"personal": *r.Personal, "operational": *r.Operational,
		"fun": *r.Fun, "no_fun": *r.NoFun,
	} {
		if v < 0 || v > 100 {
			return httpx.ErrUnprocessable("Sazba " + name + " musí být mezi 0 a 100.")
		}
	}
	if sum := *r.Personal + *r.Operational + *r.Fun + *r.NoFun; sum != 100 {
		return httpx.ErrUnprocessable(fmt.Sprintf("Sazby musí dávat dohromady 100 %%, ne %d %%.", sum))
	}
	return nil
}
