package electricity

// Wire DTOs for openapi.yaml 0.10.0, tag `electricity`.
//
// Dates cross the wire as 'YYYY-MM-DD' STRINGS and become dates.Date on the way
// in — the garden precedent (§V7). dates.Date deliberately has no MarshalJSON:
// giving one to a platform type would change every module's wire format at once,
// to save a mapping layer in this one.
//
// Money is haléře and energy is tenths of kWh, all the way to the browser. The
// frontend does the Kč and kWh formatting, because that is a presentation choice
// (21 560 Kč vs 4 858,65 Kč/MWh — this is home's first module with two money
// precisions) and not something to bake into a JSON field.

import "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"

func dstr(d dates.Date) string { return d.String() }

func dstrPtr(d *dates.Date) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

// ---------------------------------------------------------------------------
// Entities
// ---------------------------------------------------------------------------

type readingDTO struct {
	ID        string  `json:"id"`
	ReadOn    string  `json:"read_on"`
	VTDkwh    int64   `json:"vt_dkwh"`
	NTDkwh    int64   `json:"nt_dkwh"`
	Note      *string `json:"note"`
	CreatedBy *string `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func toReadingDTO(r Reading) readingDTO {
	return readingDTO{r.ID, dstr(r.ReadOn), r.VTDkwh, r.NTDkwh, r.Note, r.CreatedBy, r.CreatedAt, r.UpdatedAt}
}

type readingPage struct {
	Items      []readingDTO `json:"items"`
	NextCursor *string      `json:"next_cursor"`
}

type tariffDTO struct {
	ID              string  `json:"id"`
	EffectiveFrom   string  `json:"effective_from"`
	PriceVTHaler    int64   `json:"price_vt_haler"`
	PriceNTHaler    int64   `json:"price_nt_haler"`
	MonthlyFeeHaler int64   `json:"monthly_fee_haler"`
	EffectiveTo     *string `json:"effective_to"`
	Note            *string `json:"note"`
	CreatedBy       *string `json:"created_by"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

func toTariffDTO(t Tariff) tariffDTO {
	return tariffDTO{ID: t.ID, EffectiveFrom: dstr(t.EffectiveFrom), PriceVTHaler: t.PriceVTHaler,
		PriceNTHaler: t.PriceNTHaler, MonthlyFeeHaler: t.MonthlyFeeHaler, Note: t.Note,
		CreatedBy: t.CreatedBy, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
}

type tariffPage struct {
	Items      []tariffDTO `json:"items"`
	NextCursor *string     `json:"next_cursor"`
}

type advanceDTO struct {
	ID            string  `json:"id"`
	EffectiveFrom string  `json:"effective_from"`
	AmountHaler   int64   `json:"amount_haler"`
	DueDay        int     `json:"due_day"`
	Note          *string `json:"note"`
	CreatedBy     *string `json:"created_by"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

func toAdvanceDTO(a Advance) advanceDTO {
	return advanceDTO{a.ID, dstr(a.EffectiveFrom), a.AmountHaler, a.DueDay,
		a.Note, a.CreatedBy, a.CreatedAt, a.UpdatedAt}
}

type advancePage struct {
	Items      []advanceDTO `json:"items"`
	NextCursor *string      `json:"next_cursor"`
}

type paymentDTO struct {
	ID          string  `json:"id"`
	Month       string  `json:"month"`
	AmountHaler int64   `json:"amount_haler"`
	PaidOn      *string `json:"paid_on"`
	Note        *string `json:"note"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func toPaymentDTO(p Payment) paymentDTO {
	return paymentDTO{p.ID, p.Month.String(), p.AmountHaler, dstrPtr(p.PaidOn),
		p.Note, p.CreatedBy, p.CreatedAt, p.UpdatedAt}
}

type paymentPage struct {
	Items      []paymentDTO `json:"items"`
	NextCursor *string      `json:"next_cursor"`
}

type periodDTO struct {
	ID                   string  `json:"id"`
	StartsOn             string  `json:"starts_on"`
	EndsOn               string  `json:"ends_on"`
	EndsOnConfirmed      bool    `json:"ends_on_confirmed"`
	InvoicedTotalHaler   *int64  `json:"invoiced_total_haler"`
	InvoicedBalanceHaler *int64  `json:"invoiced_balance_haler"`
	InvoicedVTDkwh       *int64  `json:"invoiced_vt_dkwh"`
	InvoicedNTDkwh       *int64  `json:"invoiced_nt_dkwh"`
	InvoicedAt           *string `json:"invoiced_at"`
	Note                 *string `json:"note"`
	CreatedBy            *string `json:"created_by"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

func toPeriodDTO(p Period) periodDTO {
	return periodDTO{p.ID, dstr(p.StartsOn), dstr(p.EndsOn), p.EndsOnConfirmed,
		p.InvoicedTotalHaler, p.InvoicedBalanceHaler, p.InvoicedVTDkwh, p.InvoicedNTDkwh,
		p.InvoicedAt, p.Note, p.CreatedBy, p.CreatedAt, p.UpdatedAt}
}

type periodPage struct {
	Items      []periodDTO `json:"items"`
	NextCursor *string     `json:"next_cursor"`
}

// ---------------------------------------------------------------------------
// Computed
// ---------------------------------------------------------------------------

// moneyDTO is the D158 split. vt_haler + nt_haler ALWAYS equals total_haler
// exactly, because NT is derived by subtraction rather than rounded on its own —
// so the two lines on screen can never fail to add up to the headline above them.
type moneyDTO struct {
	VTHaler    int64 `json:"vt_haler"`
	NTHaler    int64 `json:"nt_haler"`
	TotalHaler int64 `json:"total_haler"`
}

type spanDTO struct {
	FromOn    string   `json:"from_on"`
	ToOn      string   `json:"to_on"`
	Days      int      `json:"days"`
	VTDkwh    int64    `json:"vt_dkwh"`
	NTDkwh    int64    `json:"nt_dkwh"`
	Energy    moneyDTO `json:"energy"`
	FeesHaler int64    `json:"fees_haler"`
	TotalHler int64    `json:"total_haler"`
	// Display only, forecast side only, and never priced: the forecast collapses
	// both divisions into one rounding rather than multiplying an average back up.
	AvgVTDkwhPerDay *float64 `json:"avg_vt_dkwh_per_day"`
	AvgNTDkwhPerDay *float64 `json:"avg_nt_dkwh_per_day"`
}

func toSpanDTO(sp *Span, withAvg bool) *spanDTO {
	if sp == nil {
		return nil
	}
	out := &spanDTO{
		FromOn: dstr(sp.FromOn), ToOn: dstr(sp.ToOn), Days: sp.Days,
		VTDkwh: sp.VTDkwh, NTDkwh: sp.NTDkwh,
		Energy:    moneyDTO{sp.EnergyVTHaler, sp.EnergyNTHaler, sp.EnergyHaler},
		FeesHaler: sp.FeeHaler, TotalHler: sp.CostHaler,
	}
	if withAvg {
		// mdkwh (thousandths) → dkWh/day. The float appears ONLY here, at the
		// display edge, and never touches a koruna.
		vt := float64(sp.AvgVTMdkwhPerDay) / 1000
		nt := float64(sp.AvgNTMdkwhPerDay) / 1000
		out.AvgVTDkwhPerDay, out.AvgNTDkwhPerDay = &vt, &nt
	}
	return out
}

type blockingDTO struct {
	Kind              string `json:"kind"`
	RequiredReadingOn string `json:"required_reading_on"`
	MessageCS         string `json:"message_cs"`
}

type countedMonthDTO struct {
	Month       string  `json:"month"`
	AmountHaler int64   `json:"amount_haler"`
	Source      string  `json:"source"`
	DueOn       string  `json:"due_on"`
	IsDue       bool    `json:"is_due"`
	DueClamped  bool    `json:"due_clamped"`
	PaidOn      *string `json:"paid_on"`
}

type advancesSummaryDTO struct {
	CountedMonths      []countedMonthDTO `json:"counted_months"`
	ExpectedTotalHaler int64             `json:"expected_total_haler"`
	DueSoFarHaler      int64             `json:"due_so_far_haler"`
	MonthsCounted      int               `json:"months_counted"`
	MonthsDue          int               `json:"months_due"`
}

type headroomDTO struct {
	AdvanceHaler      int64 `json:"advance_haler"`
	MonthlyFeeHaler   int64 `json:"monthly_fee_haler"`
	EnergyBudgetHaler int64 `json:"energy_budget_haler"`
	KwhAllVTDkwh      int64 `json:"kwh_all_vt_dkwh"`
	KwhAllNTDkwh      int64 `json:"kwh_all_nt_dkwh"`
	KwhMixDkwh        int64 `json:"kwh_mix_dkwh"`
}

type invoiceComparisonDTO struct {
	// True while the closing odečet is missing, so the computed side is still
	// part prediction. The UI hedges the whole panel on this rather than printing
	// a forecast as "spočteno".
	ComputedIsForecast bool   `json:"computed_is_forecast"`
	ComputedTotalHaler int64  `json:"computed_total_haler"`
	InvoicedTotalHaler int64  `json:"invoiced_total_haler"`
	DiffHaler          int64  `json:"diff_haler"`
	ComputedVTDkwh     *int64 `json:"computed_vt_dkwh"`
	ComputedNTDkwh     *int64 `json:"computed_nt_dkwh"`
	InvoicedVTDkwh     *int64 `json:"invoiced_vt_dkwh"`
	InvoicedNTDkwh     *int64 `json:"invoiced_nt_dkwh"`
	DiffVTDkwh         *int64 `json:"diff_vt_dkwh"`
	DiffNTDkwh         *int64 `json:"diff_nt_dkwh"`
}

// summaryDTO is the whole computed picture.
//
// ⚠ CostTotalHaler and BalanceHaler are POINTERS and serialize as `null` — NEVER
// as 0 — in `insufficient_data` and `blocked` (D161). A 0 Kč nedoplatek is a lie
// that looks like good news, and this is the first screen Karel ever sees, so the
// absence is carried by the type rather than by every screen remembering to gate
// on `status`.
type summaryDTO struct {
	Period                 periodDTO             `json:"period"`
	Status                 string                `json:"status"`
	Reason                 string                `json:"reason,omitempty"`
	Blocking               []blockingDTO         `json:"blocking"`
	LastReadingOn          *string               `json:"last_reading_on"`
	DaysSinceLastReading   *int                  `json:"days_since_last_reading"`
	Actual                 *spanDTO              `json:"actual,omitempty"`
	Forecast               *spanDTO              `json:"forecast"`
	CostTotalHaler         *int64                `json:"cost_total_haler"`
	EnergyTotalHaler       *int64                `json:"energy_total_haler"`
	FeeTotalHaler          *int64                `json:"fee_total_haler"`
	Advances               advancesSummaryDTO    `json:"advances"`
	BalanceHaler           *int64                `json:"balance_haler"`
	RecommendedAdvanceKc   *int64                `json:"recommended_advance_kc"`
	RecommendedAdvanceHler *int64                `json:"recommended_advance_haler"`
	Headroom               *headroomDTO          `json:"headroom,omitempty"`
	InvoiceComparison      *invoiceComparisonDTO `json:"invoice_comparison"`
}

func toSummaryDTO(s Summary) summaryDTO {
	out := summaryDTO{
		Period:   toPeriodDTO(s.Period),
		Status:   string(s.Status),
		Reason:   string(s.Reason),
		Blocking: []blockingDTO{},
		Actual:   toSpanDTO(s.Actual, false),
		Forecast: toSpanDTO(s.Forecast, true),
		// D161 — nil travels straight through as JSON null.
		CostTotalHaler:   s.CostTotalHaler,
		EnergyTotalHaler: s.EnergyTotalHaler,
		FeeTotalHaler:    s.FeeTotalHaler,
		BalanceHaler:     s.BalanceHaler,
	}
	for _, b := range s.Blocking {
		out.Blocking = append(out.Blocking, blockingDTO{
			Kind: string(b.Kind), RequiredReadingOn: dstr(b.RequiredReadingOn), MessageCS: b.MessageCS,
		})
	}
	out.LastReadingOn, out.DaysSinceLastReading = dstrPtr(s.LastReadingOn), s.LastReadingAgeDays

	adv := advancesSummaryDTO{
		CountedMonths:      []countedMonthDTO{},
		ExpectedTotalHaler: s.AdvancesTotalHaler,
		DueSoFarHaler:      s.AdvancesDueHaler,
		MonthsCounted:      len(s.Months),
		MonthsDue:          s.MonthsDue,
	}
	for _, m := range s.Months {
		adv.CountedMonths = append(adv.CountedMonths, countedMonthDTO{
			Month: m.Month.String(), AmountHaler: m.AmountHaler, Source: string(m.Source),
			DueOn: dstr(m.DueOn), IsDue: m.IsDue, DueClamped: m.DueClamped,
			PaidOn: dstrPtr(m.PaidOn),
		})
	}
	out.Advances = adv

	if s.RecommendedKc != nil {
		kc := *s.RecommendedKc
		haler := kc * 100
		out.RecommendedAdvanceKc, out.RecommendedAdvanceHler = &kc, &haler
	}
	if s.Headroom != nil {
		out.Headroom = &headroomDTO{
			AdvanceHaler: s.Headroom.AdvanceHaler, MonthlyFeeHaler: s.Headroom.MonthlyFeeHaler,
			EnergyBudgetHaler: s.Headroom.EnergyBudgetHaler,
			KwhAllVTDkwh:      s.Headroom.KwhAllVTDkwh, KwhAllNTDkwh: s.Headroom.KwhAllNTDkwh,
			KwhMixDkwh: s.Headroom.KwhMixDkwh,
		}
	}
	if s.Invoice != nil {
		out.InvoiceComparison = &invoiceComparisonDTO{
			ComputedIsForecast: s.Invoice.ComputedIsForecast,
			ComputedTotalHaler: s.Invoice.ComputedTotalHaler,
			InvoicedTotalHaler: s.Invoice.InvoicedTotalHaler,
			DiffHaler:          s.Invoice.DiffHaler,
			ComputedVTDkwh:     s.Invoice.ComputedVTDkwh, ComputedNTDkwh: s.Invoice.ComputedNTDkwh,
			InvoicedVTDkwh: s.Invoice.InvoicedVTDkwh, InvoicedNTDkwh: s.Invoice.InvoicedNTDkwh,
			DiffVTDkwh: s.Invoice.DiffVTDkwh, DiffNTDkwh: s.Invoice.DiffNTDkwh,
		}
	}
	return out
}

// intervalDTO — energy ONLY. An interval carries no fee: poplatky are chunked by
// (month × ceník version) and belong to no interval (D143). The Odečty row's Kč
// is labelled *energie* for exactly this reason.
type intervalDTO struct {
	FromOn              string    `json:"from_on"`
	ToOn                string    `json:"to_on"`
	Days                int       `json:"days"`
	VTDkwh              *int64    `json:"vt_dkwh"`
	NTDkwh              *int64    `json:"nt_dkwh"`
	Energy              *moneyDTO `json:"energy"`
	TariffID            *string   `json:"tariff_id"`
	TariffEffectiveFrom *string   `json:"tariff_effective_from"`
	Blocked             bool      `json:"blocked"`
	BlockingDate        *string   `json:"blocking_date"`
}

type intervalList struct {
	Items []intervalDTO `json:"items"`
}

func toIntervalList(ivs []Interval) intervalList {
	out := intervalList{Items: []intervalDTO{}}
	for _, iv := range ivs {
		item := intervalDTO{
			FromOn: dstr(iv.FromOn), ToOn: dstr(iv.ToOn), Days: iv.Days, Blocked: iv.Blocked,
		}
		if iv.Blocked {
			// Null costs and null kWh: the split of this interval's consumption
			// across the price change is precisely what is unknown.
			d := dstr(*iv.BlockingDate)
			item.BlockingDate = &d
		} else {
			vt, nt := iv.VTDkwh, iv.NTDkwh
			id, from := iv.TariffID, dstr(iv.TariffFrom)
			item.VTDkwh, item.NTDkwh = &vt, &nt
			item.Energy = &moneyDTO{iv.EnergyVTHaler, iv.EnergyNTHaler, iv.EnergyHaler}
			item.TariffID, item.TariffEffectiveFrom = &id, &from
		}
		out.Items = append(out.Items, item)
	}
	return out
}

type historyMonthDTO struct {
	Month  string `json:"month"`
	VTDkwh int64  `json:"vt_dkwh"`
	NTDkwh int64  `json:"nt_dkwh"`
	// EnergyHaler is an allocation of exact interval costs by day count (D159),
	// never a repricing of the approximate kWh above it.
	EnergyHaler   int64 `json:"energy_haler"`
	FeesHaler     int64 `json:"fees_haler"`
	TotalHaler    int64 `json:"total_haler"`
	IsApproximate bool  `json:"is_approximate"`
}

type historyDTO struct {
	Months []historyMonthDTO `json:"months"`
}

func toHistoryDTO(points []MonthPoint) historyDTO {
	out := historyDTO{Months: []historyMonthDTO{}}
	for _, p := range points {
		out.Months = append(out.Months, historyMonthDTO{
			Month: p.Month.String(), VTDkwh: p.VTDkwh, NTDkwh: p.NTDkwh,
			EnergyHaler: p.EnergyHaler, FeesHaler: p.FeesHaler, TotalHaler: p.TotalHaler,
			IsApproximate: p.IsApproximate,
		})
	}
	return out
}
