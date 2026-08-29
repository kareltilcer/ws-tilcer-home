// Package electricity is the Elektřina module (v8): VT/NT meter readings taken at
// arbitrary dates, a ceník versioned by effective date, zálohy with a due day,
// user-set settlement periods, and one answer — vyjdou zálohy, nebo doplatím?
//
// THIS FILE IS THE MODULE. Everything else is a shell around it.
//
// compute.go is PURE: no database/sql, no net/http, no time.Now. It takes a
// loaded Snapshot and returns the whole summary; Přehled, Odečty, /intervals and
// Historie all read through it. If any two of those ever disagreed about a
// number the bug would be unfindable, which is why there is exactly one
// implementation of every figure here and no second way to total anything.
//
// compute_purity_test.go asserts the import set against a whitelist (not a
// database/sql blacklist — what actually creeps into a file like this is
// net/http or a store type) and asserts the absence of time.Now: a summary that
// consulted the clock would change while it was being read, and the three views
// would disagree across a midnight. `Today` is resolved by the caller, once.
//
// THE ONE IDEA EVERYTHING RESTS ON (PRD D134): a reading is the state of the
// meter at 00:00 of its read_on. So consumption of day d = reading(d+1) −
// reading(d); an interval between readings on d1 and d2 covers days d1 … d2−1; a
// ceník effective from D prices days D, D+1, … until the next version starts;
// and a period [starts_on, ends_on] is closed by the reading dated ends_on + 1 —
// the same reading that opens the next period, exactly as the distributor treats
// it. Every boundary rule below is a corollary of that sentence.
//
// INTERNALLY EVERY WINDOW IS HALF-OPEN [from, to). The inclusive ends_on is
// converted to ends_on+1 exactly once, in periodEndExclusive. Half the plausible
// bugs in this file would be one function using inclusive bounds while its
// caller uses half-open.
package electricity

import (
	"fmt"
	"sort"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
)

// ---------------------------------------------------------------------------
// Units, and the only rounding routine
// ---------------------------------------------------------------------------

// Money is INTEGER haléře; energy is INTEGER tenths of kWh ("dkwh"); prices are
// haléře per MWh (PRD D148). There is NO FLOAT anywhere in the money path — not
// transiently, not "just for the average". A ceník price of 4 858,65 Kč/MWh
// would lose money as whole koruny, and a float would lose the determinism this
// module's whole value rests on: two people must be able to reproduce the bill.
//
// dkwhPerMWh is not a magic number: 1 dkWh = 10⁻⁴ MWh and a price is haléře per
// MWh, so dkwh · price / 10000 is haléře.
const dkwhPerMWh = 10000

// divRound divides half-away-from-zero. den MUST be > 0; callers guard the one
// place a user-supplied zero can reach it (a ceník price of 0 in headroom).
//
// EVERY rounding in this file goes through this function. There is no other call
// to any rounding routine, and there are exactly THREE rounding points in the
// money path (see intervalEnergy, forecastRunEnergy and feeChunks). A fourth
// would be a second way to total the same numbers, which is the failure this
// file is shaped to prevent — `fin` shipped two contradictory implementations of
// its split, and that is the precedent.
//
// All money here is non-negative, so half-away-from-zero IS half-up; the
// negative branch exists so a future signed input cannot silently truncate
// toward zero and produce an off-by-one that only shows on refunds.
func divRound(num, den int64) int64 {
	if num >= 0 {
		return (num + den/2) / den
	}
	return -((-num + den/2) / den)
}

// Overflow: the widest product below is n · (dVT · price + dNT · price) with
// n ≤ ~400 days, dkwh ≤ 10⁷ (a megawatt-hour meter reading in tenths) and price
// ≤ 10⁶ haléře/MWh (10 000 Kč/MWh). That is ~4·10¹⁵ doubled, three orders of
// magnitude inside int64's 9.2·10¹⁸. Asserted in compute_test.go rather than
// reached for math/big.

// ---------------------------------------------------------------------------
// Inputs — the loaded snapshot
// ---------------------------------------------------------------------------

// Reading is one meter reading: both registers at 00:00 of ReadOn (D134).
type Reading struct {
	ID     string
	ReadOn dates.Date
	VTDkwh int64
	NTDkwh int64

	// The fields below are INERT for the computation, and are on the same struct
	// anyway so that ONE shape per entity serves both the store and the wire. Two
	// shapes — a "compute Reading" and a "row Reading" — would be one refactor
	// away from disagreeing about which register a value belongs to, and nothing
	// would catch it. Every entity below follows the same rule.
	Note      *string
	CreatedBy *string
	CreatedAt string
	UpdatedAt string
}

// Tariff is one ceník version. It governs EVERY day >= EffectiveFrom until the
// next version starts; its end is DERIVED and never stored (D136), because a
// stored end is a second source of truth that eventually disagrees with the next
// row's start. Editing a version therefore moves only its own days.
type Tariff struct {
	ID              string
	EffectiveFrom   dates.Date
	PriceVTHaler    int64 // Kč/MWh in haléře, s DPH a distribucí, used as typed (D135)
	PriceNTHaler    int64
	MonthlyFeeHaler int64 // Kč/měsíc in haléře
	Note            *string
	CreatedBy       *string
	CreatedAt       string
	UpdatedAt       string
}

// Advance is one version of the záloha schedule, versioned exactly like the
// ceník. DueDay is stored RAW (1–31) and clamped at READ time (D155) — a stored
// clamp turns a 31 into a 28 the first February and stays wrong forever.
type Advance struct {
	ID            string
	EffectiveFrom dates.Date
	AmountHaler   int64
	DueDay        int
	Note          *string
	CreatedBy     *string
	CreatedAt     string
	UpdatedAt     string
}

// Payment is what was really paid for one month. A recorded payment WINS over
// the schedule for its month (D144), and attribution is by the Month key rather
// than by PaidOn — a March záloha paid on 2. dubna still belongs to March.
type Payment struct {
	ID          string
	Month       Month
	AmountHaler int64
	PaidOn      *dates.Date
	Note        *string
	CreatedBy   *string
	CreatedAt   string
	UpdatedAt   string
}

// Period is a zúčtovací období: user-set, inclusive, non-overlapping, and NEVER
// LOCKED (D139). EndsOn is an EXPECTED date until the supplier confirms it
// (D153) — suppliers frequently do not state one, and a prediction has to
// project somewhere. The four Invoiced* fields are the vyúčtování when it
// arrives, including the supplier's own final meter values, so a discrepancy can
// be attributed to kWh rather than only to Kč (D154).
type Period struct {
	ID                   string
	StartsOn             dates.Date // inclusive
	EndsOn               dates.Date // inclusive
	EndsOnConfirmed      bool
	InvoicedTotalHaler   *int64
	InvoicedBalanceHaler *int64
	InvoicedVTDkwh       *int64
	InvoicedNTDkwh       *int64
	InvoicedAt           *string
	Note                 *string
	CreatedBy            *string
	CreatedAt            string
	UpdatedAt            string
}

// Snapshot is everything one period's computation needs, loaded in five queries
// by store.Snapshot. Summarize, BuildIntervals and BuildHistory all consume this
// same struct: three endpoints, one load path, one truth.
type Snapshot struct {
	Period Period
	// Readings live, within [StartsOn, EndsOn+1] — THROUGH EndsOn+1, not EndsOn,
	// because the closing reading is dated the day AFTER the period (D134/D157).
	Readings []Reading
	// Tariffs is ALL of them, not just those inside the period: the version
	// governing StartsOn normally starts well before it.
	Tariffs  []Tariff
	Advances []Advance
	Payments []Payment
	// Today is resolved by the caller from HOME_TIMEZONE, once. This file never
	// reads a clock.
	Today dates.Date
}

// periodEndExclusive converts the inclusive EndsOn into the half-open bound the
// rest of this file uses. THE ONLY PLACE THIS CONVERSION HAPPENS.
func (s Snapshot) periodEndExclusive() dates.Date { return s.Period.EndsOn.AddDays(1) }

// ---------------------------------------------------------------------------
// Month — a calendar month as a value
// ---------------------------------------------------------------------------

// Month is a calendar month. Fees are chunked per month and zálohy are counted
// per month, so it is worth a type rather than string slicing.
type Month struct {
	Y int
	M time.Month
}

func (m Month) String() string           { return fmt.Sprintf("%04d-%02d", m.Y, int(m.M)) }
func (m Month) First() dates.Date        { return dates.New(m.Y, m.M, 1) }
func (m Month) Days() int                { return dates.DaysInMonth(m.Y, m.M) }
func (m Month) EndExclusive() dates.Date { return m.Next().First() }

func (m Month) Next() Month {
	if m.M == time.December {
		return Month{m.Y + 1, time.January}
	}
	return Month{m.Y, m.M + 1}
}

// Before reports whether m precedes o.
func (m Month) Before(o Month) bool {
	return m.Y < o.Y || (m.Y == o.Y && m.M < o.M)
}

// MonthOf is the month containing d.
func MonthOf(d dates.Date) Month { return Month{d.Y, d.M} }

// ParseMonth reads a 'YYYY-MM' key. It delegates to dates.Parse so an impossible
// month ("2026-13") is rejected by the same code that rejects an impossible day,
// rather than by a second hand-rolled range check that could drift from it.
func ParseMonth(s string) (Month, error) {
	if len(s) != 7 {
		return Month{}, fmt.Errorf("invalid month %q: want YYYY-MM", s)
	}
	d, err := dates.Parse(s + "-01")
	if err != nil {
		return Month{}, fmt.Errorf("invalid month %q: want YYYY-MM", s)
	}
	return Month{d.Y, d.M}, nil
}

// ---------------------------------------------------------------------------
// Outputs
// ---------------------------------------------------------------------------

// Status decides which of the optional blocks a summary carries.
type Status string

const (
	// StatusOK — actual plus forecast.
	StatusOK Status = "ok"
	// StatusComplete — the closing reading dated ends_on+1 exists, so the
	// forecast is empty and the period is ENTIRELY ACTUAL (D157). This is the
	// state in which the computed-vs-invoiced comparison becomes meaningful.
	StatusComplete Status = "complete"
	// StatusBlocked — a required reading is missing. Figures before the earliest
	// gap stay valid and visible; nothing past it is computed.
	StatusBlocked Status = "blocked"
	// StatusInsufficientData — fewer than two readings, both readings on the same
	// day, or no ceník effective on or before the period start.
	StatusInsufficientData Status = "insufficient_data"
)

// Reason names WHAT IS MISSING when the module cannot predict. The module never
// says "zatím nelze předpovědět" without naming the reason: an unexplained
// refusal is indistinguishable from a bug.
type Reason string

const (
	ReasonNone            Reason = ""
	ReasonNoTariff        Reason = "no_tariff"
	ReasonNeedSecond      Reason = "need_second_reading"
	ReasonSameDay         Reason = "second_reading_same_day"
	ReasonBlockedOpening  Reason = "missing_opening_reading"
	ReasonBlockedInterval Reason = "tariff_change_inside_interval"
)

// BlockingKind is the wire's enum. NOTE the deliberate asymmetry with Reason:
// there are THREE internal ways to be unable to compute, but only TWO of them
// are a missing READING and therefore a Blocking. "No ceník at all" is not a
// gap in the measurements — it is missing configuration — so it surfaces as
// StatusInsufficientData with ReasonNoTariff and no Blocking entry.
type BlockingKind string

const (
	// BlockPeriodStart — no reading on the period's first day, so there is no
	// baseline and the period shows NO MONEY AT ALL (D140).
	BlockPeriodStart BlockingKind = "period_start"
	// BlockTariffChange — a ceník change falls STRICTLY inside a reading
	// interval, so everything from that date on is unpriceable while everything
	// before it stays valid (D137). Money is never interpolated.
	BlockTariffChange BlockingKind = "tariff_change"
)

// Blocking is a reading the module requires before it will price anything past a
// given date. RequiredReadingOn pre-fills the reading form — THE DATE ONLY,
// never a value (D137).
type Blocking struct {
	Kind              BlockingKind
	RequiredReadingOn dates.Date
	MessageCS         string
}

// Interval is one consecutive pair of readings, half-open [FromOn, ToOn).
//
// Energy ONLY. An interval carries NO FEE: poplatky are chunked by (calendar
// month × ceník version) and belong to no interval (D143). Allocating a chunk
// across the intervals overlapping it would invent a second rounding rule whose
// only job is keeping two views agreeing, for no gain — the period is presented
// as "energie + poplatky", which is how the supplier states it too.
type Interval struct {
	FromOn        dates.Date
	ToOn          dates.Date
	Days          int
	VTDkwh        int64
	NTDkwh        int64
	TariffID      string
	TariffFrom    dates.Date
	EnergyHaler   int64
	EnergyVTHaler int64
	EnergyNTHaler int64
	// Blocked marks the interval a ceník change falls strictly inside. It is
	// emitted (so Odečty can show the unpriceable row) but carries no costs, and
	// the walk stops after it.
	Blocked      bool
	BlockingDate *dates.Date
}

// Span is one side of the actual/forecast boundary — which is THE LAST READING
// AND NOT TODAY (D141). Days since the last reading are forecast like any other
// future day, and that is what keeps "actual" meaning measured.
type Span struct {
	FromOn        dates.Date
	ToOn          dates.Date // exclusive
	Days          int
	VTDkwh        int64
	NTDkwh        int64
	EnergyHaler   int64 // AUTHORITATIVE
	EnergyVTHaler int64
	EnergyNTHaler int64 // EnergyHaler − EnergyVTHaler, by construction (D158)
	FeeHaler      int64
	CostHaler     int64
	// AvgVT/NTMdkwhPerDay are THOUSANDTHS of dkWh per day, forecast side only.
	// Display only — never priced. The forecast prices a run in one division
	// (see forecastRunEnergy) precisely so that no rounded per-day average is
	// ever multiplied back up.
	AvgVTMdkwhPerDay int64
	AvgNTMdkwhPerDay int64
}

// CountedMonth is one calendar month belonging to the period. A month counts IFF
// the period contains that month's FIRST DAY (D145) — which makes a year-long
// period exactly 12 months whatever day it starts on.
type CountedMonth struct {
	Month       Month
	AmountHaler int64
	Source      MonthSource
	DueOn       dates.Date
	DueClamped  bool // the schedule's due_day exceeded this month's length
	IsDue       bool
	PaidOn      *dates.Date
}

// MonthSource says where a counted month's amount came from.
type MonthSource string

const (
	SourcePayment  MonthSource = "payment"  // a recorded row, which wins (D144)
	SourceSchedule MonthSource = "schedule" // the version effective on the month's 1st
	SourceNone     MonthSource = "none"     // neither; contributes 0 and renders "bez předpisu"
)

// Headroom is what the current záloha actually buys, computable with ZERO
// consumption data — which is why it is what Přehled shows while the prediction
// is still impossible. It is the first thing Karel ever sees, and it is a real
// answer rather than an apology for an empty panel.
type Headroom struct {
	AdvanceHaler      int64
	MonthlyFeeHaler   int64
	EnergyBudgetHaler int64
	KwhAllVTDkwh      int64
	KwhAllNTDkwh      int64
	// KwhMixDkwh blends at 30 % VT / 70 % NT (D162). A STATED HEURISTIC, never a
	// measurement, and the Czech copy names it as one.
	KwhMixDkwh int64
}

// InvoiceComparison is computed against the recorded vyúčtování (D154). Storing
// the supplier's final readings is what lets a difference be attributed to kWh
// rather than only to Kč — which is how an odhadnutý odečet on their side
// becomes visible instead of looking like a pricing surprise.
type InvoiceComparison struct {
	// ComputedIsForecast marks the case the comparison is NOT yet a like-for-like
	// one: the vyúčtování has arrived but the closing odečet has not, so the
	// computed side is still part prediction. That is the ordinary sequence — the
	// invoice comes in the post before anyone walks to the meter cupboard — and
	// without this flag the screen would state a forecast as "spočteno" and
	// attribute its own projection error to "jiný odečet" on the supplier's side.
	ComputedIsForecast bool
	ComputedTotalHaler int64
	InvoicedTotalHaler int64
	DiffHaler          int64 // computed − invoiced
	ComputedVTDkwh     *int64
	ComputedNTDkwh     *int64
	InvoicedVTDkwh     *int64
	InvoicedNTDkwh     *int64
	DiffVTDkwh         *int64
	DiffNTDkwh         *int64
}

// FeeChunk is one (calendar month × ceník version) slice of the monthly fee. A
// whole calendar month inside one version costs EXACTLY the monthly fee — fee ·
// d/d, no rounding possible — so the pro-rata only ever shows at the ends of a
// period and at a mid-month price change.
type FeeChunk struct {
	Month     Month
	FromOn    dates.Date
	ToOn      dates.Date // exclusive
	Days      int
	DaysInMon int
	TariffID  string
	FeeHaler  int64
}

// MonthPoint is one column of the Historie charts.
type MonthPoint struct {
	Month Month
	// VT/NTDkwh are APPROXIMATE by construction (D138): an interval's consumption
	// is spread evenly across its days so month columns can be drawn at all.
	VTDkwh int64
	NTDkwh int64
	// EnergyHaler is an ALLOCATION of already-exact interval costs by day count
	// (D159), never a repricing of the approximate kWh above. So the kWh columns
	// are approximate and say so, the Kč columns are exact totals cut along an
	// approximate line, and no price ever touches an invented meter value.
	EnergyHaler int64
	FeesHaler   int64 // already per month; no allocation needed
	TotalHaler  int64
	// IsApproximate is true whenever a contributing interval crosses this month's
	// boundary. False when the readings happen to fall on the boundaries, in
	// which case the month is exact and the UI can drop the caveat.
	IsApproximate bool
}

// Summary is the whole computed picture for one period.
//
// CostTotalHaler, EnergyTotalHaler, FeeTotalHaler and BalanceHaler are POINTERS
// and are nil — NEVER 0 — whenever the module has not earned them (D161). The
// zero value of this struct is therefore not a valid summary. A 0 Kč nedoplatek
// is a lie that looks like good news, and this is the first screen Karel ever
// sees, so the absence is carried by the type rather than by every screen
// remembering to gate on Status first.
type Summary struct {
	Period Period
	Today  dates.Date
	Status Status
	Reason Reason // set only when Status is StatusInsufficientData or StatusBlocked
	// Blocking is empty when nothing is missing; the EARLIEST entry is the one
	// that truncates Actual.
	Blocking []Blocking
	Closed   bool

	// Actual is nil only when there is no opening reading — then the period shows
	// no money at all (D140). It stays populated when a LATER interval is
	// blocked: the figures before a gap do not become unknown because the ones
	// after it are.
	Actual   *Span
	Forecast *Span

	EnergyTotalHaler *int64
	FeeTotalHaler    *int64
	CostTotalHaler   *int64

	LastReadingOn      *dates.Date
	LastReadingAgeDays *int // drives the plain in-app line "poslední odečet před N dny" (D156)
	ElapsedDays        int
	RemainingDays      int

	Months             []CountedMonth
	AdvancesTotalHaler int64
	AdvancesDueHaler   int64
	MonthsDue          int

	BalanceHaler  *int64 // advances − cost; >0 přeplatek, <0 nedoplatek (D146)
	RecommendedKc *int64 // nil when no month remains

	Headroom *Headroom // only when the totals are not produced
	Invoice  *InvoiceComparison
}

// ---------------------------------------------------------------------------
// Lookups
// ---------------------------------------------------------------------------

// tariffOn returns the version governing day d — the latest with EffectiveFrom
// <= d — or nil. Tariffs must be ascending.
func tariffOn(tariffs []Tariff, d dates.Date) *Tariff {
	var found *Tariff
	for i := range tariffs {
		if tariffs[i].EffectiveFrom.After(d) {
			break
		}
		found = &tariffs[i]
	}
	return found
}

// nextTariffAfter returns the first EffectiveFrom strictly after d, or nil.
func nextTariffAfter(tariffs []Tariff, d dates.Date) *dates.Date {
	for i := range tariffs {
		if tariffs[i].EffectiveFrom.After(d) {
			f := tariffs[i].EffectiveFrom
			return &f
		}
	}
	return nil
}

// advanceOn returns the schedule version effective on day d, or nil.
func advanceOn(advances []Advance, d dates.Date) *Advance {
	var found *Advance
	for i := range advances {
		if advances[i].EffectiveFrom.After(d) {
			break
		}
		found = &advances[i]
	}
	return found
}

// sorted returns copies of the snapshot's slices in ascending order. The store
// already delivers them sorted; doing it again here costs nothing on the tens of
// rows this module ever holds and makes compute correct for a hand-built
// Snapshot in a test, which is where most of its exercise happens.
func (s Snapshot) sorted() Snapshot {
	out := s
	out.Readings = append([]Reading(nil), s.Readings...)
	out.Tariffs = append([]Tariff(nil), s.Tariffs...)
	out.Advances = append([]Advance(nil), s.Advances...)
	sort.Slice(out.Readings, func(i, j int) bool {
		return out.Readings[i].ReadOn.Before(out.Readings[j].ReadOn)
	})
	sort.Slice(out.Tariffs, func(i, j int) bool {
		return out.Tariffs[i].EffectiveFrom.Before(out.Tariffs[j].EffectiveFrom)
	})
	sort.Slice(out.Advances, func(i, j int) bool {
		return out.Advances[i].EffectiveFrom.Before(out.Advances[j].EffectiveFrom)
	})
	return out
}

// readingsInPeriod keeps the readings inside [StartsOn, EndsOn+1] — INCLUSIVE of
// the closing reading, which is dated the day after the period ends.
func (s Snapshot) readingsInPeriod() []Reading {
	endExcl := s.periodEndExclusive()
	var out []Reading
	for _, r := range s.Readings {
		if r.ReadOn.Before(s.Period.StartsOn) || r.ReadOn.After(endExcl) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// ---------------------------------------------------------------------------
// ROUNDING POINT 1 — energy of one actual interval, one ceník version
// ---------------------------------------------------------------------------

// intervalEnergy prices one interval. ONE ROUNDING, on the VT+NT SUM — not per
// register, not per tariff, not per day. The VT part is then rounded on its own
// and NT TAKES THE REMAINDER (D158), so the two displayed parts sum to the whole
// by construction rather than by luck: the `needs` pattern from the fin split,
// where one component absorbs the rounding error by subtraction.
func intervalEnergy(vtDkwh, ntDkwh int64, t Tariff) (total, vt, nt int64) {
	total = divRound(vtDkwh*t.PriceVTHaler+ntDkwh*t.PriceNTHaler, dkwhPerMWh)
	vt = divRound(vtDkwh*t.PriceVTHaler, dkwhPerMWh)
	nt = total - vt
	return total, vt, nt
}

// ---------------------------------------------------------------------------
// ROUNDING POINT 2 — energy of one forecast run of n days, one ceník version
// ---------------------------------------------------------------------------

// forecastRunEnergy prices n forecast days at one version, from the period's
// measured deltas over elapsedDays.
//
// Note what this does NOT do: compute a per-day average, round it, then multiply
// by n. Both divisions collapse into ONE rounding — a rounded per-day average
// times 243 days drifts by tens of koruna, which is the single easiest way to
// make this module quietly wrong.
func forecastRunEnergy(n int64, dVT, dNT, elapsedDays int64, t Tariff) (total, vt, nt int64) {
	den := elapsedDays * dkwhPerMWh
	total = divRound(n*(dVT*t.PriceVTHaler+dNT*t.PriceNTHaler), den)
	vt = divRound(n*dVT*t.PriceVTHaler, den)
	nt = total - vt
	return total, vt, nt
}

// ---------------------------------------------------------------------------
// ROUNDING POINT 3 — one fee chunk, keyed (calendar month × ceník version)
// ---------------------------------------------------------------------------

// feeChunks slices [from, to) by calendar month and then by ceník version,
// pro-rating the monthly fee by days (D143):
//
//	chunk = round(monthly_fee · days_in_chunk / days_in_month)
//
// A whole calendar month inside one version therefore costs EXACTLY the monthly
// fee, to the haléř — fee · d/d cannot round — and the pro-rata only shows up at
// the ends of a period and at a mid-month price change.
func feeChunks(tariffs []Tariff, from, to dates.Date) []FeeChunk {
	var out []FeeChunk
	if !from.Before(to) {
		return out
	}
	cur := from
	for cur.Before(to) {
		m := MonthOf(cur)
		monthEnd := m.EndExclusive()
		segEnd := monthEnd
		if to.Before(segEnd) {
			segEnd = to
		}
		a := cur
		for a.Before(segEnd) {
			b := segEnd
			if nx := nextTariffAfter(tariffs, a); nx != nil && nx.Before(segEnd) {
				b = *nx
			}
			days := int64(a.DaysUntil(b))
			chunk := FeeChunk{
				Month: m, FromOn: a, ToOn: b,
				Days: int(days), DaysInMon: m.Days(),
			}
			if t := tariffOn(tariffs, a); t != nil {
				chunk.TariffID = t.ID
				chunk.FeeHaler = divRound(t.MonthlyFeeHaler*days, int64(m.Days()))
			}
			out = append(out, chunk)
			a = b
		}
		cur = segEnd
	}
	return out
}

func sumFees(chunks []FeeChunk) int64 {
	var total int64
	for _, c := range chunks {
		total += c.FeeHaler
	}
	return total
}

// ---------------------------------------------------------------------------
// Intervals and the hard block (D137)
// ---------------------------------------------------------------------------

// BuildIntervals walks the readings pairwise and prices each interval, stopping
// at the first hard block. Shared with GET /intervals so the "show your work"
// view behind Odečty cannot drift from the summary above it.
//
// Block detection is ONE predicate: a ceník EffectiveFrom STRICTLY inside the
// interval, d1 < ef < d2. Boundary equality is NOT a block — ef == d1 means the
// version governs the whole interval, ef == d2 means the change starts exactly
// where the next interval starts. Each surviving interval is therefore priced by
// exactly one version; there cannot be a second, or it would be a block.
//
// The blocked interval IS emitted, flagged and costless, so Odečty can show the
// unpriceable row — that row is how a straddled price change becomes visible.
// The walk then stops: nothing past the gap is priced, because the split of that
// interval's consumption across the price change is exactly what is unknown.
func BuildIntervals(s Snapshot) ([]Interval, []Blocking) {
	s = s.sorted()
	rs := s.readingsInPeriod()

	if len(rs) == 0 || !rs[0].ReadOn.Equal(s.Period.StartsOn) {
		return nil, []Blocking{{
			Kind:              BlockPeriodStart,
			RequiredReadingOn: s.Period.StartsOn,
			MessageCS: fmt.Sprintf(
				"Chybí odečet k %s — bez počátečního stavu elektroměru nelze v tomto období spočítat nic.",
				czDate(s.Period.StartsOn)),
		}}
	}

	// No ceník governs the period's first day, so nothing here can be priced at
	// all. Return EMPTY rather than reporting a tariff_change block: with no
	// version in force, "a version starts inside this interval" is a misleading
	// thing to say — the reading is not what is missing. Summarize reports this
	// as insufficient_data/no_tariff, which is what it is: missing configuration,
	// not a gap in the measurements.
	if tariffOn(s.Tariffs, s.Period.StartsOn) == nil {
		return nil, nil
	}

	var out []Interval
	var blocking []Blocking
	for i := 0; i+1 < len(rs); i++ {
		a, b := rs[i], rs[i+1]
		iv := Interval{
			FromOn: a.ReadOn, ToOn: b.ReadOn,
			Days:   a.ReadOn.DaysUntil(b.ReadOn),
			VTDkwh: b.VTDkwh - a.VTDkwh,
			NTDkwh: b.NTDkwh - a.NTDkwh,
		}

		if inside := firstTariffStrictlyInside(s.Tariffs, a.ReadOn, b.ReadOn); inside != nil {
			iv.Blocked = true
			iv.BlockingDate = inside
			iv.VTDkwh, iv.NTDkwh = 0, 0 // the split across the change is what is unknown
			out = append(out, iv)
			blocking = append(blocking, Blocking{
				Kind:              BlockTariffChange,
				RequiredReadingOn: *inside,
				MessageCS: fmt.Sprintf(
					"Chybí odečet k %s (změna ceny). Bez něj nelze spočítat spotřebu po tomto datu.",
					czDate(*inside)),
			})
			break
		}

		t := tariffOn(s.Tariffs, a.ReadOn)
		if t == nil {
			// Unreachable once Summarize has checked for a version effective on
			// StartsOn, since versions are cumulative forward. Guarded rather
			// than dereferenced so a future caller cannot turn a configuration
			// gap into a panic.
			break
		}
		iv.TariffID = t.ID
		iv.TariffFrom = t.EffectiveFrom
		iv.EnergyHaler, iv.EnergyVTHaler, iv.EnergyNTHaler = intervalEnergy(iv.VTDkwh, iv.NTDkwh, *t)
		out = append(out, iv)
	}
	return out, blocking
}

// firstTariffStrictlyInside returns the earliest EffectiveFrom with d1 < ef < d2.
func firstTariffStrictlyInside(tariffs []Tariff, d1, d2 dates.Date) *dates.Date {
	for i := range tariffs {
		f := tariffs[i].EffectiveFrom
		if f.After(d1) && f.Before(d2) {
			return &f
		}
	}
	return nil
}

// czDate renders a date the way a Czech person writes it: 24. 6. 2026.
func czDate(d dates.Date) string { return fmt.Sprintf("%d. %d. %d", d.D, int(d.M), d.Y) }

// ---------------------------------------------------------------------------
// Summarize — the whole picture
// ---------------------------------------------------------------------------

// Summarize computes everything Přehled shows. Nothing here is stored or cached
// (D152).
//
// The five stages below run in order and each one narrows the answer: what is
// owed regardless of any reading, whether the period can be priced at all, the
// measured half, the projected half, and the balance that exists only once a
// total does. They share `c`, which holds the derived inputs — computing those
// once is what stops two stages disagreeing about which readings are in the
// period.
func Summarize(s Snapshot) Summary {
	c := newSummarizer(s)
	sum := Summary{Period: c.s.Period, Today: c.s.Today, Blocking: c.blocking}

	c.countAdvances(&sum)
	c.setBlockedStatus(&sum)
	c.measureActual(&sum)
	c.project(&sum)
	c.settle(&sum)
	return sum
}

// summarizer holds the values every stage of Summarize derives from.
type summarizer struct {
	s         Snapshot
	rs        []Reading // the readings inside the period, in order
	endExcl   dates.Date
	intervals []Interval
	blocking  []Blocking

	// hasOpening and openTariff are the two facts the shape of the whole answer
	// turns on: without a reading ON the period's first day there is no measured
	// side at all (D140), and without a tariff covering that day nothing can be
	// priced.
	hasOpening bool
	openTariff *Tariff

	// forecastFrom is the reading the average is measured TO, and therefore where
	// the forecast starts. measureActual sets it; it stays zero when the measured
	// side does not exist, which is exactly when project does not read it.
	forecastFrom dates.Date
}

func newSummarizer(s Snapshot) *summarizer {
	s = s.sorted()
	c := &summarizer{
		s:       s,
		rs:      s.readingsInPeriod(),
		endExcl: s.periodEndExclusive(),
	}
	c.intervals, c.blocking = BuildIntervals(s)
	c.hasOpening = len(c.rs) > 0 && c.rs[0].ReadOn.Equal(s.Period.StartsOn)
	c.openTariff = tariffOn(s.Tariffs, s.Period.StartsOn)
	return c
}

// countAdvances fills the counted months and their three totals.
//
// It runs FIRST because it depends on none of the rest: it is calendar
// arithmetic over the period's bounds, and it is the one thing this module can
// always answer. So even the emptiest screen has it.
func (c *summarizer) countAdvances(sum *Summary) {
	sum.Months = countedMonths(c.s)
	for _, m := range sum.Months {
		sum.AdvancesTotalHaler += m.AmountHaler
		if m.IsDue {
			sum.AdvancesDueHaler += m.AmountHaler
			sum.MonthsDue++
		}
	}
}

// setBlockedStatus records why the period cannot be priced, if it cannot. A
// period that CAN be priced is left with an empty Status here — project below
// sets the successful one, and it is the only stage that does.
func (c *summarizer) setBlockedStatus(sum *Summary) {
	switch {
	case !c.hasOpening:
		sum.Status = StatusBlocked
		sum.Reason = ReasonBlockedOpening
	case c.openTariff == nil:
		// NOT a Blocking: nothing is missing from the MEASUREMENTS. This is
		// missing configuration, and the Czech copy says so.
		sum.Status = StatusInsufficientData
		sum.Reason = ReasonNoTariff
	case len(c.blocking) > 0:
		sum.Status = StatusBlocked
		sum.Reason = ReasonBlockedInterval
	}
}

// measureActual fills the measured side — the span up to the last usable
// reading — along with the nudge line's pair, and sets c.forecastFrom.
func (c *summarizer) measureActual(sum *Summary) {
	if !c.hasOpening || c.openTariff == nil {
		// Blocked or unpriced, but readings exist: the nudge must still say when
		// the last one was, not claim there has never been one.
		if len(c.rs) > 0 {
			c.noteLastReading(sum)
		}
		return
	}

	priced := pricedIntervals(c.intervals)
	lastActual := c.s.Period.StartsOn
	if len(priced) > 0 {
		lastActual = priced[len(priced)-1].ToOn
	}

	actual := &Span{FromOn: c.s.Period.StartsOn, ToOn: lastActual,
		Days: c.s.Period.StartsOn.DaysUntil(lastActual)}
	for _, iv := range priced {
		actual.VTDkwh += iv.VTDkwh
		actual.NTDkwh += iv.NTDkwh
		actual.EnergyHaler += iv.EnergyHaler
		actual.EnergyVTHaler += iv.EnergyVTHaler
	}
	// EnergyHaler is DEFINED as the sum of the interval costs, never
	// recomputed from a span-wide delta: that definition is what makes the
	// Odečty list and the Přehled headline provably the same number.
	actual.EnergyNTHaler = actual.EnergyHaler - actual.EnergyVTHaler
	actual.FeeHaler = sumFees(feeChunks(c.s.Tariffs, c.s.Period.StartsOn, lastActual))
	actual.CostHaler = actual.EnergyHaler + actual.FeeHaler
	sum.Actual = actual

	// THE FORECAST BASIS. When an interval is blocked it is the reading that
	// OPENS the blocked interval, not the newest one on file — everything
	// after the gap is unusable, so the average cannot be measured to it.
	basis := c.rs[len(c.rs)-1]
	if len(c.blocking) > 0 && c.blocking[0].Kind == BlockTariffChange {
		for _, r := range c.rs {
			if !r.ReadOn.After(lastActual) {
				basis = r
			}
		}
	}
	c.forecastFrom = basis.ReadOn
	sum.ElapsedDays = c.s.Period.StartsOn.DaysUntil(c.forecastFrom)
	sum.Closed = c.forecastFrom.Equal(c.endExcl)

	// ⚠ LastReadingOn is DELIBERATELY NOT the basis above. This pair is the
	// only source for the plain nudge line "poslední odečet před N dny"
	// (D156), so it must always name the NEWEST reading actually on file.
	// Rewinding it past a block would tell someone no odečet has been taken
	// since January while the one they entered this morning sits on Odečty —
	// and it would contradict the unpriced branch above, which reports the
	// newest reading for the other blocked state.
	c.noteLastReading(sum)
}

// noteLastReading fills the pair behind "poslední odečet před N dny" (D156) from
// the NEWEST reading on file. Both branches of measureActual call this one
// function, which is the point: the two states must not disagree about which
// reading that is — see the ⚠ above.
func (c *summarizer) noteLastReading(sum *Summary) {
	lastOn := c.rs[len(c.rs)-1].ReadOn
	age := lastOn.DaysUntil(c.s.Today)
	sum.LastReadingOn = &lastOn
	sum.LastReadingAgeDays = &age
}

// project fills the forecast half and the three totals — or refuses to, and
// says what is missing instead.
//
// The boundary between fact and forecast is the LAST USABLE READING
// (c.forecastFrom), not today, and not LastReadingOn, which reports the newest
// reading on file for the nudge line (D141, D142).
func (c *summarizer) project(sum *Summary) {
	canForecast := c.hasOpening && c.openTariff != nil && len(c.blocking) == 0 &&
		sum.ElapsedDays >= 1 && !sum.Closed && c.forecastFrom.Before(c.endExcl)

	switch {
	case canForecast:
		sum.Forecast = buildForecast(c.s, c.forecastFrom, c.endExcl,
			sum.Actual.VTDkwh, sum.Actual.NTDkwh, int64(sum.ElapsedDays))
		// The fee is chunked over the WHOLE period once and the forecast side
		// takes the difference — ONE SUBTRACTION, not a second helper, so
		// FeeTotalHaler cannot disagree with its two halves.
		feeTotal := sumFees(feeChunks(c.s.Tariffs, c.s.Period.StartsOn, c.endExcl))
		sum.Forecast.FeeHaler = feeTotal - sum.Actual.FeeHaler
		sum.Forecast.CostHaler = sum.Forecast.EnergyHaler + sum.Forecast.FeeHaler
		sum.setTotals(feeTotal)
		sum.Status = StatusOK
		sum.RemainingDays = sum.Forecast.Days

	case sum.Closed && sum.Actual != nil:
		// D157: the closing reading exists, so the forecast span is EMPTY and the
		// period is entirely actual. Without this case the module would go on
		// forecasting a period it already knows the answer to.
		sum.Forecast = &Span{FromOn: c.endExcl, ToOn: c.endExcl}
		sum.setTotals(sum.Actual.FeeHaler)
		sum.Status = StatusComplete

	default:
		// No totals. The module never shows a number it has not earned — not a
		// zero, not a spinner, not a dash (D161).
		if sum.Status == "" {
			sum.Status = StatusInsufficientData
			switch {
			case len(c.rs) < 2:
				sum.Reason = ReasonNeedSecond
			case sum.ElapsedDays < 1:
				sum.Reason = ReasonSameDay
			default:
				sum.Reason = ReasonNeedSecond
			}
		}
		sum.Headroom = buildHeadroom(c.s)
	}
}

// settle derives what exists only once a cost total does: the balance against
// the zálohy, the recommendation for the months still to come, and the
// comparison against the supplier's invoice.
func (c *summarizer) settle(sum *Summary) {
	if sum.CostTotalHaler == nil {
		return
	}
	balance := sum.AdvancesTotalHaler - *sum.CostTotalHaler
	sum.BalanceHaler = &balance
	sum.RecommendedKc = recommendedAdvanceKc(*sum.CostTotalHaler, sum.AdvancesDueHaler,
		len(sum.Months)-sum.MonthsDue)
	sum.Invoice = buildInvoiceComparison(c.s, *sum)
}

// setTotals fills the three totals together, so a caller cannot set one and
// forget another.
func (sum *Summary) setTotals(feeTotal int64) {
	energy := sum.Actual.EnergyHaler
	if sum.Forecast != nil {
		energy += sum.Forecast.EnergyHaler
	}
	cost := energy + feeTotal
	sum.EnergyTotalHaler = &energy
	sum.FeeTotalHaler = &feeTotal
	sum.CostTotalHaler = &cost
}

// pricedIntervals drops the blocked tail.
func pricedIntervals(ivs []Interval) []Interval {
	var out []Interval
	for _, iv := range ivs {
		if iv.Blocked {
			break
		}
		out = append(out, iv)
	}
	return out
}

// buildForecast splits [from, to) into RUNS, one per ceník version effective in
// that window (D142), and prices each at rounding point 2. A price rise already
// entered with a future date is therefore honoured automatically — you can see
// the effect of a new ceník the moment you type it.
//
// The forecast is NEVER hard-blocked. A future version inside the window is the
// normal case: the block is a fact about MEASUREMENT, and there is nothing to
// measure in the future.
func buildForecast(s Snapshot, from, to dates.Date, dVT, dNT, elapsedDays int64) *Span {
	sp := &Span{FromOn: from, ToOn: to, Days: from.DaysUntil(to)}
	if elapsedDays > 0 {
		sp.AvgVTMdkwhPerDay = divRound(dVT*1000, elapsedDays)
		sp.AvgNTMdkwhPerDay = divRound(dNT*1000, elapsedDays)
	}
	a := from
	for a.Before(to) {
		b := to
		if nx := nextTariffAfter(s.Tariffs, a); nx != nil && nx.Before(to) {
			b = *nx
		}
		t := tariffOn(s.Tariffs, a)
		if t == nil {
			break
		}
		n := int64(a.DaysUntil(b))
		total, vt, _ := forecastRunEnergy(n, dVT, dNT, elapsedDays, *t)
		sp.EnergyHaler += total
		sp.EnergyVTHaler += vt
		sp.VTDkwh += divRound(n*dVT, elapsedDays)
		sp.NTDkwh += divRound(n*dNT, elapsedDays)
		a = b
	}
	sp.EnergyNTHaler = sp.EnergyHaler - sp.EnergyVTHaler
	return sp
}

// ---------------------------------------------------------------------------
// Counted months, zálohy, balance, doporučená záloha
// ---------------------------------------------------------------------------

// countedMonths lists the calendar months belonging to the period: a month
// counts IFF the period contains that month's FIRST DAY (D145). Karel's
// 24. 6. 2026 – 23. 6. 2027 therefore counts červenec 2026 … červen 2027, and
// červen 2026 is NOT among them. Přehled lists these with their amounts so the
// rule is visible rather than folklore.
func countedMonths(s Snapshot) []CountedMonth {
	var out []CountedMonth
	last := MonthOf(s.Period.EndsOn)
	for m := MonthOf(s.Period.StartsOn); !last.Before(m); m = m.Next() {
		first := m.First()
		if first.Before(s.Period.StartsOn) || first.After(s.Period.EndsOn) {
			continue
		}
		cm := CountedMonth{Month: m, Source: SourceNone}

		// A recorded payment WINS for its month (D144).
		for i := range s.Payments {
			if s.Payments[i].Month == m {
				cm.AmountHaler = s.Payments[i].AmountHaler
				cm.Source = SourcePayment
				cm.PaidOn = s.Payments[i].PaidOn
				break
			}
		}

		// DueOn always comes from the SCHEDULE's due_day, clamped into this month
		// (D155) — the one and only place due_day is read. It is deliberately not
		// taken from a payment's paid_on: "how many zálohy are still to come" is a
		// calendar question, and a payment recorded without a date would otherwise
		// never count as due and would quietly skew the doporučená záloha.
		sch := advanceOn(s.Advances, first)
		if sch != nil {
			if cm.Source == SourceNone {
				cm.AmountHaler = sch.AmountHaler
				cm.Source = SourceSchedule
			}
			dim := m.Days()
			day := sch.DueDay
			if day > dim {
				day = dim
				cm.DueClamped = true
			}
			cm.DueOn = dates.New(m.Y, m.M, day)
		} else {
			// No schedule governs this month, so there is no prescribed day. The
			// honest answer to "has this month's záloha come due?" is then "once
			// the month is over" — which keeps a past unscheduled month out of the
			// remaining-months divisor and a future one in it.
			cm.DueOn = dates.New(m.Y, m.M, m.Days())
		}
		// INCLUSIVE at equality: due on the 15th means paid on the 15th (D155).
		cm.IsDue = !cm.DueOn.After(s.Today)
		out = append(out, cm)
	}
	return out
}

// recommendedAdvanceKc is the záloha that would land the period at zero (D146):
// the shortfall spread over the months not yet due, ROUNDED UP to whole koruny,
// floored at 0, and omitted when no month remains.
//
// ONE ceil-div straight from haléře to Kč. Do NOT divide to haléře, round, then
// convert — the double rounding moves the answer by a koruna in exactly the
// fixture compute_test.go pins.
func recommendedAdvanceKc(costTotal, advancesDue int64, remainingMonths int) *int64 {
	if remainingMonths <= 0 {
		return nil
	}
	num := costTotal - advancesDue
	if num <= 0 {
		zero := int64(0)
		return &zero
	}
	den := int64(remainingMonths) * 100
	kc := (num + den - 1) / den
	return &kc
}

// ---------------------------------------------------------------------------
// Headroom (D162)
// ---------------------------------------------------------------------------

// buildHeadroom answers "will the zálohy cover it" with ZERO consumption data.
// It is filled only when the real prediction is unavailable, so the first screen
// Karel ever sees carries a real number rather than an empty panel.
func buildHeadroom(s Snapshot) *Headroom {
	t := tariffOn(s.Tariffs, s.Today)
	if t == nil {
		t = tariffOn(s.Tariffs, s.Period.StartsOn)
	}
	adv := advanceOn(s.Advances, s.Today)
	if adv == nil {
		adv = advanceOn(s.Advances, s.Period.StartsOn)
	}
	if t == nil || adv == nil {
		return nil
	}
	budget := adv.AmountHaler - t.MonthlyFeeHaler
	if budget < 0 {
		budget = 0
	}
	h := &Headroom{
		AdvanceHaler:      adv.AmountHaler,
		MonthlyFeeHaler:   t.MonthlyFeeHaler,
		EnergyBudgetHaler: budget,
	}
	if t.PriceVTHaler > 0 {
		h.KwhAllVTDkwh = divRound(budget*dkwhPerMWh, t.PriceVTHaler)
	}
	if t.PriceNTHaler > 0 {
		h.KwhAllNTDkwh = divRound(budget*dkwhPerMWh, t.PriceNTHaler)
	}
	// The 30/70 mix in ONE division with no intermediate rounding (D162):
	// rounding a blended price first and dividing into it moves the answer by a
	// whole kWh. Note this is a divRound OUTSIDE the money path — the
	// three-rounding-point rule governs Kč, and no koruna passes through here.
	if mix := 3*t.PriceVTHaler + 7*t.PriceNTHaler; mix > 0 {
		h.KwhMixDkwh = divRound(budget*dkwhPerMWh*10, mix)
	}
	return h
}

// ---------------------------------------------------------------------------
// The vyúčtování comparison (D154)
// ---------------------------------------------------------------------------

func buildInvoiceComparison(s Snapshot, sum Summary) *InvoiceComparison {
	if s.Period.InvoicedTotalHaler == nil || sum.CostTotalHaler == nil {
		return nil
	}
	ic := &InvoiceComparison{
		// Anything short of `complete` still carries forecast days on the computed
		// side (D157): the closing reading dated ends_on+1 is what makes the two
		// sides measure the same thing.
		ComputedIsForecast: sum.Status != StatusComplete,
		ComputedTotalHaler: *sum.CostTotalHaler,
		InvoicedTotalHaler: *s.Period.InvoicedTotalHaler,
		DiffHaler:          *sum.CostTotalHaler - *s.Period.InvoicedTotalHaler,
		InvoicedVTDkwh:     s.Period.InvoicedVTDkwh,
		InvoicedNTDkwh:     s.Period.InvoicedNTDkwh,
	}
	// The kWh line is the one that says whether a discrepancy was a price
	// surprise or an odhadnutý odečet on the supplier's side.
	if sum.Actual != nil {
		vt := sum.Actual.VTDkwh
		nt := sum.Actual.NTDkwh
		if sum.Forecast != nil {
			vt += sum.Forecast.VTDkwh
			nt += sum.Forecast.NTDkwh
		}
		ic.ComputedVTDkwh, ic.ComputedNTDkwh = &vt, &nt
		if s.Period.InvoicedVTDkwh != nil {
			d := vt - *s.Period.InvoicedVTDkwh
			ic.DiffVTDkwh = &d
		}
		if s.Period.InvoicedNTDkwh != nil {
			d := nt - *s.Period.InvoicedNTDkwh
			ic.DiffNTDkwh = &d
		}
	}
	return ic
}

// ---------------------------------------------------------------------------
// Historie (D138 / D159)
// ---------------------------------------------------------------------------

// BuildHistory produces the per-month series for the Historie charts. It lives
// in this file so it cannot become a second interval walk with its own opinion
// about what a month costs.
//
// The two halves of D138/D159 must be read together:
//
//   - kWh per month SPREADS an interval's consumption evenly over its days, so
//     month columns can be drawn at all. Display only, approximate by
//     construction, labelled as such, and it never feeds a Kč figure.
//   - Kč per month is an ALLOCATION of the already-exact interval cost by day
//     count — never a repricing of the invented kWh above, which is precisely
//     what D137 forbids. Fee chunks are already per month and need no allocation.
//
// So the kWh columns are approximate and say so, the Kč columns are exact totals
// cut along an approximate line, and no price ever touches an invented meter
// value.
func BuildHistory(s Snapshot, from, to Month) []MonthPoint {
	s = s.sorted()
	intervals, _ := BuildIntervals(s)
	byMonth := map[Month]*MonthPoint{}
	touch := func(m Month) *MonthPoint {
		if p, ok := byMonth[m]; ok {
			return p
		}
		p := &MonthPoint{Month: m}
		byMonth[m] = p
		return p
	}

	for _, iv := range intervals {
		if iv.Blocked || iv.Days == 0 {
			continue
		}
		// An interval is approximate for the months it touches exactly when it
		// spans a boundary; when readings happen to fall on the 1st the month is
		// exact and the UI can drop the caveat.
		crossesBoundary := MonthOf(iv.FromOn) != MonthOf(iv.ToOn.AddDays(-1))
		a := iv.FromOn
		for a.Before(iv.ToOn) {
			m := MonthOf(a)
			b := m.EndExclusive()
			if iv.ToOn.Before(b) {
				b = iv.ToOn
			}
			n := int64(a.DaysUntil(b))
			p := touch(m)
			p.VTDkwh += divRound(iv.VTDkwh*n, int64(iv.Days))
			p.NTDkwh += divRound(iv.NTDkwh*n, int64(iv.Days))
			p.EnergyHaler += divRound(iv.EnergyHaler*n, int64(iv.Days))
			if crossesBoundary {
				p.IsApproximate = true
			}
			a = b
		}
	}

	// FEES STOP WHERE THE MEASUREMENTS STOP, exactly as the energy above does.
	// Chunking the whole period instead would bill months that have not happened
	// yet: on a running period every month after the last reading would come back
	// with a full poplatek and no kWh, and Historie — a chart of what HAPPENED —
	// would draw eleven future bars at 642 Kč. The bound is the end of the last
	// priced interval, which is the same `lastActual` Summarize measures to, so
	// a closed period still reconciles to FeeTotalHaler exactly.
	feeEnd := s.Period.StartsOn
	if priced := pricedIntervals(intervals); len(priced) > 0 {
		feeEnd = priced[len(priced)-1].ToOn
	}
	for _, c := range feeChunks(s.Tariffs, s.Period.StartsOn, feeEnd) {
		touch(c.Month).FeesHaler += c.FeeHaler
	}

	var out []MonthPoint
	for m := from; !to.Before(m); m = m.Next() {
		p := touch(m)
		p.TotalHaler = p.EnergyHaler + p.FeesHaler
		out = append(out, *p)
	}
	return out
}
