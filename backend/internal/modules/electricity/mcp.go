package electricity

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The electricity module's MCP provider (v11, PRD §V11-4 FR-M4).
//
// ⚠ electricity IS THE LEANEST MODULE IN HOME and stays that way here. It
// publishes no widget, no metric, no list and no push (D147/D156), and
// `internal/arch` enforces that by banning the imports. Its MCP surface is the
// same shape: four tools, three of which are the two things a person standing at
// the meter cupboard actually does — read the counter, and ask what it will cost.
//
// ⚠ WHAT IS ABSENT, BY NAME: every delete, every tariff verb, every payment verb,
// and — importantly — **every period verb**. That last absence is what makes
// D311's known defect unreachable from here: `POST /api/electricity/periods` with
// a negative `invoiced_vt_dkwh` still answers 500 instead of 422, and v11 does
// not repair it (NG5). It is avoided by ABSENCE rather than by care, which is
// worth writing down because the two look identical from outside.

type mcpProvider struct {
	mcp.NoResources
	svc *Service
}

// MCPProvider implements mcp.Source.
func (m *Module) MCPProvider() mcp.Provider { return &mcpProvider{svc: m.svc} }

func (p *mcpProvider) Module() string { return "electricity" }

const (
	toolReadings   = "home_electricity_readings"
	toolReadingAdd = "home_electricity_reading_add"
	toolAdvanceAdd = "home_electricity_advance_add"
	toolSummary    = "home_electricity_summary"
)

func (p *mcpProvider) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        toolReadings,
			Title:       "Meter readings",
			Description: "Lists the household's meter readings newest first, both registers in kWh, with how many days ago the newest one was taken.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"limit": {"type": "integer", "minimum": 1}},
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
		{
			Name:        toolReadingAdd,
			Title:       "Record a meter reading",
			Description: "Records one dated reading of both registers in kWh; both must be non-decreasing against the readings on EITHER side of the date, because with meter replacement out of scope a falling counter is always a typo.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "read_on": {"type": "string", "description": "ISO date (YYYY-MM-DD). Defaults to today in the household timezone."},
    "vt_kwh": {"type": "number", "minimum": 0, "description": "The high-tariff (VT) register, in kWh as the meter shows it."},
    "nt_kwh": {"type": "number", "minimum": 0, "description": "The low-tariff (NT) register, in kWh."},
    "note": {"type": "string"}
  },
  "required": ["vt_kwh", "nt_kwh"],
  "additionalProperties": false
}`),
		},
		{
			Name:        toolAdvanceAdd,
			Title:       "Record a new advance schedule",
			Description: "Starts a new version of the monthly advance (záloha) from a date, in Kč, with the day of the month it falls due; it governs every month from that date until the next version, and earlier months keep the version they were billed under.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "effective_from": {"type": "string", "description": "ISO date (YYYY-MM-DD)."},
    "amount_kc": {"type": "number", "minimum": 0, "description": "The monthly advance in Kč."},
    "due_day": {"type": "integer", "minimum": 1, "maximum": 31, "description": "Day of the month. Stored raw and clamped when read, so 31 still means \"the last day\" in February."},
    "note": {"type": "string"}
  },
  "required": ["effective_from", "amount_kc", "due_day"],
  "additionalProperties": false
}`),
		},
		{
			Name:        toolSummary,
			Title:       "Electricity balance",
			Description: "Returns the current billing period's consumption, cost so far, advances paid and the resulting overpayment or underpayment, all computed on read; pass period_id for an older period.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"period_id": {"type": "string", "description": "Omit for the newest period."}},
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
	}
}

func (p *mcpProvider) Call(ctx context.Context, name string, args json.RawMessage) (mcp.Result, error) {
	switch name {
	case toolReadings:
		return p.readings(ctx, args)
	case toolReadingAdd:
		return p.readingAdd(ctx, args)
	case toolAdvanceAdd:
		return p.advanceAdd(ctx, args)
	case toolSummary:
		return p.summary(ctx, args)
	default:
		return mcp.Result{}, mcp.UnknownToolError(name)
	}
}

type limitArgs struct {
	Limit int `json:"limit"`
}

func (p *mcpProvider) readings(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in limitArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	rows, _, err := p.svc.Store().ListReadings(ctx, limit, "")
	if err != nil {
		return mcp.Result{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d odečtů:\n", len(rows))
	today := p.svc.Today()
	for i, r := range rows {
		fmt.Fprintf(&b, "\n• %s — VT %s, NT %s (id %s)",
			r.ReadOn, kwh(r.VTDkwh), kwh(r.NTDkwh), r.ID)
		if i == 0 {
			// The one nudge this module is allowed: a plain in-app line, never a
			// notification (D156).
			fmt.Fprintf(&b, "\n  poslední odečet před %d dny", r.ReadOn.DaysUntil(today))
		}
	}
	return mcp.TextResult(b.String(), readingsWire(rows))
}

type readingAddArgs struct {
	ReadOn string   `json:"read_on"`
	VTKwh  *float64 `json:"vt_kwh"`
	NTKwh  *float64 `json:"nt_kwh"`
	Note   string   `json:"note"`
}

// readingAdd validates before the service (D311).
//
// ⚠ THE REGISTERS ARE ACCEPTED IN kWh AND STORED IN dkWh, and the conversion is
// here rather than in the schema because the person reading the tool description
// is a model relaying what somebody read off a meter — and a meter shows kWh.
// The tenth is what the column keeps, so a value with more precision than that
// is refused rather than rounded: silently dropping a digit off a meter reading
// is how a monotonicity check starts failing for reasons nobody can see.
func (p *mcpProvider) readingAdd(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in readingAddArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if in.VTKwh == nil || in.NTKwh == nil {
		return mcp.Result{}, httpx.ErrUnprocessable("Oba registry (VT i NT) jsou povinné.")
	}
	vt, err := toDkwh(*in.VTKwh, "vt_kwh")
	if err != nil {
		return mcp.Result{}, err
	}
	nt, err := toDkwh(*in.NTKwh, "nt_kwh")
	if err != nil {
		return mcp.Result{}, err
	}
	readOn := p.svc.Today()
	if in.ReadOn != "" {
		readOn, err = dates.Parse(in.ReadOn)
		if err != nil {
			return mcp.Result{}, httpx.ErrUnprocessable("read_on musí být ve tvaru RRRR-MM-DD.")
		}
	}
	input := ReadingInput{ReadOn: &readOn, VTDkwh: &vt, NTDkwh: &nt}
	if in.Note != "" {
		note := in.Note
		input.Note, input.NoteSet = &note, true
	}
	r, err := p.svc.CreateReading(ctx, input)
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.TextResult(
		fmt.Sprintf("Odečet k %s zapsán: VT %s, NT %s (id %s).",
			r.ReadOn, kwh(r.VTDkwh), kwh(r.NTDkwh), r.ID),
		readingWire(r))
}

type advanceAddArgs struct {
	EffectiveFrom string   `json:"effective_from"`
	AmountKc      *float64 `json:"amount_kc"`
	DueDay        *int     `json:"due_day"`
	Note          string   `json:"note"`
}

func (p *mcpProvider) advanceAdd(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in advanceAddArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	from, err := dates.Parse(in.EffectiveFrom)
	if err != nil {
		return mcp.Result{}, httpx.ErrUnprocessable("effective_from musí být ve tvaru RRRR-MM-DD.")
	}
	if in.AmountKc == nil {
		return mcp.Result{}, httpx.ErrUnprocessable("Chybí částka zálohy.")
	}
	amount, err := toHaler(*in.AmountKc, "amount_kc")
	if err != nil {
		return mcp.Result{}, err
	}
	if in.DueDay == nil {
		return mcp.Result{}, httpx.ErrUnprocessable("Chybí den splatnosti.")
	}
	if *in.DueDay < 1 || *in.DueDay > 31 {
		return mcp.Result{}, httpx.ErrUnprocessable("Den splatnosti musí být mezi 1 a 31.")
	}
	input := AdvanceInput{EffectiveFrom: &from, AmountHaler: &amount, DueDay: in.DueDay}
	if in.Note != "" {
		note := in.Note
		input.Note, input.NoteSet = &note, true
	}
	a, err := p.svc.CreateAdvance(ctx, input)
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.TextResult(
		fmt.Sprintf("Záloha od %s: %s, splatnost %d. den (id %s).",
			a.EffectiveFrom, kc(a.AmountHaler), a.DueDay, a.ID),
		advanceWire(a))
}

type summaryArgs struct {
	PeriodID string `json:"period_id"`
}

func (p *mcpProvider) summary(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in summaryArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	periodID := in.PeriodID
	if periodID == "" {
		// ⚠ The newest period, resolved here rather than defaulted in the store: a
		// household with no period at all is a normal early state, not an error, and
		// the answer for it is a sentence rather than a 404.
		periods, _, err := p.svc.Store().ListPeriods(ctx, 1, "")
		if err != nil {
			return mcp.Result{}, err
		}
		if len(periods) == 0 {
			return mcp.Result{Text: "Zatím není založené žádné zúčtovací období."}, nil
		}
		periodID = periods[0].ID
	}
	s, err := p.svc.Summary(ctx, periodID)
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.TextResult(renderSummary(s), summaryWire(s))
}

func renderSummary(s Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Období %s – %s (id %s)\n", s.Period.StartsOn, s.Period.EndsOn, s.Period.ID)
	fmt.Fprintf(&b, "Stav: %s", s.Status)
	if s.Reason != "" {
		fmt.Fprintf(&b, " (%s)", s.Reason)
	}
	if s.LastReadingOn != nil && s.LastReadingAgeDays != nil {
		fmt.Fprintf(&b, "\nPoslední odečet %s, před %d dny", *s.LastReadingOn, *s.LastReadingAgeDays)
	}
	if s.Actual != nil {
		fmt.Fprintf(&b, "\nSpotřeba zatím: VT %s, NT %s", kwh(s.Actual.VTDkwh), kwh(s.Actual.NTDkwh))
	}
	if s.CostTotalHaler != nil {
		fmt.Fprintf(&b, "\nOdhad nákladů za období: %s", kc(*s.CostTotalHaler))
	}
	fmt.Fprintf(&b, "\nZálohy zaplacené: %s (splatné %s, %d měsíců)",
		kc(s.AdvancesTotalHaler), kc(s.AdvancesDueHaler), s.MonthsDue)
	if s.BalanceHaler != nil {
		// ⚠ THE SIGN IS THE ANSWER and it is named in words, because ">0" is not a
		// thing anybody standing at a meter reads correctly.
		word := "přeplatek"
		v := *s.BalanceHaler
		if v < 0 {
			word, v = "nedoplatek", -v
		}
		fmt.Fprintf(&b, "\n%s: %s", strings.ToUpper(word[:1])+word[1:], kc(v))
	}
	if s.RecommendedKc != nil {
		fmt.Fprintf(&b, "\nDoporučená záloha: %d Kč", *s.RecommendedKc)
	}
	for _, blk := range s.Blocking {
		fmt.Fprintf(&b, "\n⚠ chybí: %s", blk.MessageCS)
	}
	return b.String()
}

// Search scans the readings by date.
//
// ⚠ D302: an actor-less ctx is an ERROR, never an empty slice.
func (p *mcpProvider) Search(ctx context.Context, q mcp.Query) ([]mcp.Hit, error) {
	if _, ok := reqctx.ActorFrom(ctx); !ok {
		return nil, fmt.Errorf("electricity: search without an actor")
	}
	term := strings.TrimSpace(q.Text)
	if term == "" {
		return nil, nil
	}
	rows, _, err := p.svc.Store().ListReadings(ctx, 200, "")
	if err != nil {
		return nil, err
	}
	var hits []mcp.Hit
	for _, r := range rows {
		date := r.ReadOn.String()
		note := ""
		if r.Note != nil {
			note = *r.Note
		}
		if !strings.Contains(date, term) && !strings.Contains(strings.ToLower(note), strings.ToLower(term)) {
			continue
		}
		if len(hits) >= q.Limit {
			break
		}
		updated, _ := time.Parse(tsFormat, r.UpdatedAt)
		hits = append(hits, mcp.Hit{
			Kind:      "electricity.reading",
			ID:        r.ID,
			Title:     "Odečet " + date,
			Snippet:   fmt.Sprintf("VT %s, NT %s", kwh(r.VTDkwh), kwh(r.NTDkwh)),
			UpdatedAt: updated,
			ExactHit:  date == term,
		})
	}
	return hits, nil
}

// Get answers home_get.
func (p *mcpProvider) Get(ctx context.Context, kind, id string) (mcp.Result, error) {
	if kind != "electricity.reading" {
		return mcp.NotFoundResult(), nil
	}
	rows, _, err := p.svc.Store().ListReadings(ctx, 500, "")
	if err != nil {
		return mcp.Result{}, err
	}
	for _, r := range rows {
		if r.ID != id {
			continue
		}
		return mcp.TextResult(
			fmt.Sprintf("Odečet %s — VT %s, NT %s (id %s)", r.ReadOn, kwh(r.VTDkwh), kwh(r.NTDkwh), r.ID),
			readingWire(r))
	}
	return mcp.NotFoundResult(), nil
}

// ---- units ----
//
// ⚠ EVERY FIGURE IN THIS MODULE IS AN INTEGER IN THE SMALLEST UNIT — dkWh for
// energy, haléře for money (D135) — precisely so nothing is ever a float in a
// sum. The conversions live here, at the edge, and nowhere else: a model speaks
// kWh and Kč because that is what a meter and an invoice show.

// toDkwh converts kWh to tenths, refusing anything the column cannot hold
// exactly.
func toDkwh(v float64, field string) (int64, error) {
	if v < 0 {
		return 0, httpx.ErrUnprocessable(field + " nesmí být záporné.")
	}
	scaled := v * 10
	rounded := int64(scaled + 0.5)
	// ⚠ Refused rather than rounded. A meter shows tenths; a third decimal in a
	// reading is a typo or a unit mix-up, and quietly dropping it is how a
	// monotonicity check later fails for a reason nobody can reconstruct.
	if diff := scaled - float64(rounded); diff > 0.001 || diff < -0.001 {
		return 0, httpx.ErrUnprocessable(field + " smí mít nejvýše jedno desetinné místo (elektroměr ukazuje desetiny kWh).")
	}
	return rounded, nil
}

// toHaler converts Kč to haléře on the same terms.
func toHaler(v float64, field string) (int64, error) {
	if v < 0 {
		return 0, httpx.ErrUnprocessable(field + " nesmí být záporné.")
	}
	scaled := v * 100
	rounded := int64(scaled + 0.5)
	if diff := scaled - float64(rounded); diff > 0.001 || diff < -0.001 {
		return 0, httpx.ErrUnprocessable(field + " smí mít nejvýše dvě desetinná místa.")
	}
	return rounded, nil
}

// ⚠ THE MODULE ALREADY HAS `kwh` AND `kc`, and this provider uses those rather
// than its own. They are not conveniences: `kwh` keeps the tenth and NEVER
// rounds, because the string on the odečty screen has to be the one the audit
// log froze; `kc` rounds to whole koruny for a TOTAL, where the last haléř is
// the residue of a dozen roundings, while `kc2` keeps two places for a unit
// price typed off the supplier's contract. A second pair here would have
// answered the same questions differently in the one module where the figures
// are the point.

// ---- wire shapes ----
//
// ⚠ The domain structs carry no JSON tags — this module renders through its own
// handler DTOs — so the provider builds its own maps rather than marshalling a
// `Reading` and getting Go field names into `structuredContent`.

func readingWire(r Reading) map[string]any {
	out := map[string]any{
		"id": r.ID, "read_on": r.ReadOn.String(),
		"vt_kwh": float64(r.VTDkwh) / 10, "nt_kwh": float64(r.NTDkwh) / 10,
	}
	if r.Note != nil {
		out["note"] = *r.Note
	}
	return out
}

func readingsWire(rows []Reading) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, readingWire(r))
	}
	return out
}

func advanceWire(a Advance) map[string]any {
	return map[string]any{
		"id": a.ID, "effective_from": a.EffectiveFrom.String(),
		"amount_kc": float64(a.AmountHaler) / 100, "due_day": a.DueDay,
	}
}

func summaryWire(s Summary) map[string]any {
	out := map[string]any{
		"period_id":         s.Period.ID,
		"starts_on":         s.Period.StartsOn.String(),
		"ends_on":           s.Period.EndsOn.String(),
		"status":            string(s.Status),
		"closed":            s.Closed,
		"elapsed_days":      s.ElapsedDays,
		"remaining_days":    s.RemainingDays,
		"advances_total_kc": float64(s.AdvancesTotalHaler) / 100,
		"advances_due_kc":   float64(s.AdvancesDueHaler) / 100,
		"months_due":        s.MonthsDue,
	}
	if s.CostTotalHaler != nil {
		out["cost_total_kc"] = float64(*s.CostTotalHaler) / 100
	}
	if s.BalanceHaler != nil {
		out["balance_kc"] = float64(*s.BalanceHaler) / 100
	}
	if s.RecommendedKc != nil {
		out["recommended_advance_kc"] = *s.RecommendedKc
	}
	if s.LastReadingOn != nil {
		out["last_reading_on"] = s.LastReadingOn.String()
	}
	if s.LastReadingAgeDays != nil {
		out["last_reading_age_days"] = *s.LastReadingAgeDays
	}
	return out
}
