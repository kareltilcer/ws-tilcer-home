package electricity

import (
	"math/rand"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
)

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

func d(s string) dates.Date {
	v, err := dates.Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}

func mo(s string) Month {
	v, err := ParseMonth(s)
	if err != nil {
		panic(err)
	}
	return v
}

func rd(id, on string, vtKwh, ntKwh int64) Reading {
	// The form takes whole kWh (D148); storage keeps tenths so a future decimal
	// meter needs no migration.
	return Reading{ID: id, ReadOn: d(on), VTDkwh: vtKwh * 10, NTDkwh: ntKwh * 10}
}

func tf(id, from string, vtKc, ntKc, feeKc float64) Tariff {
	return Tariff{
		ID: id, EffectiveFrom: d(from),
		PriceVTHaler:    int64(vtKc*100 + 0.5),
		PriceNTHaler:    int64(ntKc*100 + 0.5),
		MonthlyFeeHaler: int64(feeKc*100 + 0.5),
	}
}

func adv(id, from string, kc int64, dueDay int) Advance {
	return Advance{ID: id, EffectiveFrom: d(from), AmountHaler: kc * 100, DueDay: dueDay}
}

func deref(t *testing.T, p *int64, what string) int64 {
	t.Helper()
	if p == nil {
		t.Fatalf("%s: expected a value, got nil", what)
	}
	return *p
}

func eq(t *testing.T, what string, got, want int64) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %d, want %d", what, got, want)
	}
}

// ---------------------------------------------------------------------------
// FIXTURE 1 — the brief's §4.5 worked example. THE GATE.
//
// Period 1. 4. 2026 – 31. 3. 2027. Ceník A od 1. 1. 2026 (VT 3 200, NT 2 400
// Kč/MWh, poplatky 350 Kč/měs); ceník B od 1. 1. 2027 (3 600 / 2 700 / 380).
// Záloha 1 800 Kč, SPLATNOST 15. Readings 1. 4. = 12 000 / 30 000 and
// 1. 8. = 12 640 / 31 480. Today = 20. 8. 2026.
//
// Every figure below is pinned by the brief. If a later change disagrees with
// these numbers, the change is wrong.
// ---------------------------------------------------------------------------

func fixtureGeneral(dueDay int) Snapshot {
	return Snapshot{
		Period: Period{ID: "p1", StartsOn: d("2026-04-01"), EndsOn: d("2027-03-31")},
		Readings: []Reading{
			rd("r1", "2026-04-01", 12000, 30000),
			rd("r2", "2026-08-01", 12640, 31480),
		},
		Tariffs: []Tariff{
			tf("A", "2026-01-01", 3200, 2400, 350),
			tf("B", "2027-01-01", 3600, 2700, 380),
		},
		Advances: []Advance{adv("a1", "2026-01-01", 1800, dueDay)},
		Today:    d("2026-08-20"),
	}
}

func TestSummarizeWorkedExample(t *testing.T) {
	s := Summarize(fixtureGeneral(15))

	if s.Status != StatusOK {
		t.Fatalf("status = %q, want %q (blocking=%+v)", s.Status, StatusOK, s.Blocking)
	}
	if s.Actual == nil || s.Forecast == nil {
		t.Fatal("both spans must be produced")
	}

	eq(t, "Actual.Days", int64(s.Actual.Days), 122)
	eq(t, "Actual.VTDkwh", s.Actual.VTDkwh, 6400)
	eq(t, "Actual.NTDkwh", s.Actual.NTDkwh, 14800)
	eq(t, "Actual.EnergyHaler", s.Actual.EnergyHaler, 560_000)
	eq(t, "Actual.FeeHaler", s.Actual.FeeHaler, 140_000) // 4 whole months × 350 Kč

	eq(t, "Forecast.Days", int64(s.Forecast.Days), 243) // 153 on A, 90 on B
	eq(t, "Forecast.EnergyHaler", s.Forecast.EnergyHaler, 1_167_049)
	eq(t, "Forecast.FeeHaler", s.Forecast.FeeHaler, 289_000) // 5×350 + 3×380

	eq(t, "EnergyTotalHaler", deref(t, s.EnergyTotalHaler, "EnergyTotalHaler"), 1_727_049)
	eq(t, "FeeTotalHaler", deref(t, s.FeeTotalHaler, "FeeTotalHaler"), 429_000)
	eq(t, "CostTotalHaler", deref(t, s.CostTotalHaler, "CostTotalHaler"), 2_156_049)

	eq(t, "AdvancesTotalHaler", s.AdvancesTotalHaler, 2_160_000)
	eq(t, "BalanceHaler", deref(t, s.BalanceHaler, "BalanceHaler"), 3_951) // přeplatek 40 Kč
	eq(t, "RecommendedKc", deref(t, s.RecommendedKc, "RecommendedKc"), 1_795)

	// Karel's counted-month rule, on a period that starts on the 1st.
	if len(s.Months) != 12 {
		t.Errorf("counted months = %d, want 12", len(s.Months))
	}
	eq(t, "MonthsDue", int64(s.MonthsDue), 5) // duben…srpen, splatnost 15.
	eq(t, "AdvancesDueHaler", s.AdvancesDueHaler, 900_000)
}

// TestRecommendedAdvanceDependsOnDueDay pins the brief's warning that the due
// day is LOAD-BEARING in this fixture: a spec that does not state it is not
// reproducible. At splatnost 25. only four zálohy are due on 20. 8. 2026, and
// the answer moves by a koruna.
func TestRecommendedAdvanceDependsOnDueDay(t *testing.T) {
	for _, tc := range []struct {
		dueDay      int
		wantDue     int
		wantKc      int64
		wantDueHler int64
	}{
		{dueDay: 15, wantDue: 5, wantKc: 1795, wantDueHler: 900_000},
		{dueDay: 20, wantDue: 5, wantKc: 1795, wantDueHler: 900_000}, // inclusive at equality
		{dueDay: 25, wantDue: 4, wantKc: 1796, wantDueHler: 720_000},
	} {
		s := Summarize(fixtureGeneral(tc.dueDay))
		eq(t, "MonthsDue", int64(s.MonthsDue), int64(tc.wantDue))
		eq(t, "AdvancesDueHaler", s.AdvancesDueHaler, tc.wantDueHler)
		eq(t, "RecommendedKc", deref(t, s.RecommendedKc, "RecommendedKc"), tc.wantKc)
		// The due day moves the recommendation and NOTHING else (D155).
		eq(t, "CostTotalHaler", deref(t, s.CostTotalHaler, "cost"), 2_156_049)
		eq(t, "AdvancesTotalHaler", s.AdvancesTotalHaler, 2_160_000)
		eq(t, "months", int64(len(s.Months)), 12)
	}
}

// ---------------------------------------------------------------------------
// FIXTURE 2 — Karel's real opening state. The FIRST SCREEN he will ever see.
// ---------------------------------------------------------------------------

func fixtureKarelDayOne() Snapshot {
	return Snapshot{
		Period: Period{ID: "p1", StartsOn: d("2026-06-24"), EndsOn: d("2027-06-23")},
		// A new meter, installed on the period's first day. No second reading,
		// and there will not be one for weeks.
		Readings: []Reading{rd("r1", "2026-06-24", 32, 70)},
		Tariffs:  []Tariff{tf("T", "2026-06-24", 4858.65, 4026.69, 642.35)},
		Advances: []Advance{adv("a1", "2026-06-24", 1500, 15)},
		Today:    d("2026-08-20"),
	}
}

func TestSummarizeKarelDayOne(t *testing.T) {
	s := Summarize(fixtureKarelDayOne())

	if s.Status != StatusInsufficientData {
		t.Fatalf("status = %q, want %q", s.Status, StatusInsufficientData)
	}
	if s.Reason != ReasonNeedSecond {
		t.Errorf("reason = %q, want %q", s.Reason, ReasonNeedSecond)
	}
	if len(s.Blocking) != 0 {
		t.Errorf("blocking = %+v, want empty — nothing is MISSING, there is simply no second reading yet", s.Blocking)
	}
	if s.Forecast != nil {
		t.Error("forecast must not be produced with one reading")
	}

	// D161: nil, NOT 0. A 0 Kč nedoplatek is a lie that looks like good news,
	// and this is the first thing Karel ever sees.
	if s.CostTotalHaler != nil {
		t.Errorf("CostTotalHaler = %d, want nil", *s.CostTotalHaler)
	}
	if s.BalanceHaler != nil {
		t.Errorf("BalanceHaler = %d, want nil", *s.BalanceHaler)
	}
	if s.EnergyTotalHaler != nil || s.FeeTotalHaler != nil {
		t.Error("no total may be produced when the prediction is impossible")
	}
	if s.RecommendedKc != nil {
		t.Errorf("RecommendedKc = %d, want nil", *s.RecommendedKc)
	}

	// D145, verified against the real dates: exactly 12 months, červenec 2026 …
	// červen 2027, and červen 2026 is NOT among them even though the period
	// starts in June.
	if len(s.Months) != 12 {
		t.Fatalf("counted months = %d, want 12", len(s.Months))
	}
	if got := s.Months[0].Month.String(); got != "2026-07" {
		t.Errorf("first counted month = %s, want 2026-07", got)
	}
	if got := s.Months[11].Month.String(); got != "2027-06" {
		t.Errorf("last counted month = %s, want 2027-06", got)
	}
	for _, m := range s.Months {
		if m.Month.String() == "2026-06" {
			t.Error("červen 2026 must not count — the period does not contain 1. 6. 2026")
		}
	}
	eq(t, "AdvancesTotalHaler", s.AdvancesTotalHaler, 1_800_000)

	// The headroom line — the answer available with ZERO consumption data, and
	// the reason this screen is a designed state rather than an empty panel.
	if s.Headroom == nil {
		t.Fatal("headroom must be filled when the prediction is impossible")
	}
	eq(t, "Headroom.EnergyBudgetHaler", s.Headroom.EnergyBudgetHaler, 85_765) // 857,65 Kč
	eq(t, "Headroom.KwhAllVTDkwh", s.Headroom.KwhAllVTDkwh, 1765)
	eq(t, "Headroom.KwhAllNTDkwh", s.Headroom.KwhAllNTDkwh, 2130)
	eq(t, "Headroom.KwhMixDkwh", s.Headroom.KwhMixDkwh, 2006)
	// …and what the screen actually prints (D162).
	eq(t, "kWh all VT", s.Headroom.KwhAllVTDkwh/10, 176)
	eq(t, "kWh all NT", s.Headroom.KwhAllNTDkwh/10, 213)
	eq(t, "kWh mix 30/70", s.Headroom.KwhMixDkwh/10, 200)
}

// ---------------------------------------------------------------------------
// Poplatky (D143)
// ---------------------------------------------------------------------------

// TestWholeMonthCostsExactlyTheFee is the invariant that makes the pro-rata
// invisible in the ordinary case: fee · d/d cannot round.
func TestWholeMonthCostsExactlyTheFee(t *testing.T) {
	for _, fee := range []float64{642.35, 350, 380, 0.01, 999.99} {
		tariffs := []Tariff{tf("T", "2020-01-01", 1000, 1000, fee)}
		want := int64(fee*100 + 0.5)
		for m := mo("2026-01"); !mo("2027-01").Before(m); m = m.Next() {
			chunks := feeChunks(tariffs, m.First(), m.EndExclusive())
			if len(chunks) != 1 {
				t.Fatalf("%s: got %d chunks, want 1", m, len(chunks))
			}
			if got := chunks[0].FeeHaler; got != want {
				t.Errorf("%s at fee %v: chunk = %d, want exactly %d — no haléř may be lost to pro-rata",
					m, fee, got, want)
			}
		}
	}
}

func TestFeeChunksProRataAndSplitByTariff(t *testing.T) {
	tariffs := []Tariff{
		tf("A", "2026-01-01", 3200, 2400, 350),
		tf("B", "2026-06-15", 3600, 2700, 380),
	}
	// June 2026 has 30 days and is cut by the 15th: 14 days on A, 16 on B.
	chunks := feeChunks(tariffs, d("2026-06-01"), d("2026-07-01"))
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2 (one per ceník version inside the month)", len(chunks))
	}
	eq(t, "A days", int64(chunks[0].Days), 14)
	eq(t, "B days", int64(chunks[1].Days), 16)
	eq(t, "A fee", chunks[0].FeeHaler, divRound(35000*14, 30))
	eq(t, "B fee", chunks[1].FeeHaler, divRound(38000*16, 30))
}

// ---------------------------------------------------------------------------
// Counted months (D145) at the awkward boundaries
// ---------------------------------------------------------------------------

func TestCountedMonthBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		start, end string
		want       int
		wantFirst  string
	}{
		{"Karel's year from the 24th", "2026-06-24", "2027-06-23", 12, "2026-07"},
		{"a year from the 1st", "2026-01-01", "2026-12-31", 12, "2026-01"},
		{"one day, on the 1st", "2026-03-01", "2026-03-01", 1, "2026-03"},
		{"one day, not on the 1st", "2026-03-02", "2026-03-02", 0, ""},
		{"one month plus a day", "2026-03-01", "2026-04-01", 2, "2026-03"},
		{"a month that misses both 1sts", "2026-03-02", "2026-03-31", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Snapshot{
				Period:   Period{StartsOn: d(tc.start), EndsOn: d(tc.end)},
				Advances: []Advance{adv("a", "2020-01-01", 1000, 15)},
				Today:    d("2026-08-20"),
			}
			got := countedMonths(s)
			if len(got) != tc.want {
				t.Fatalf("counted %d months, want %d: %+v", len(got), tc.want, got)
			}
			if tc.want > 0 && got[0].Month.String() != tc.wantFirst {
				t.Errorf("first month = %s, want %s", got[0].Month, tc.wantFirst)
			}
		})
	}
}

// TestDueDayClamp pins D155's clamp-at-read: a due day of 31 falls on the
// month's last day, and February is where a stored clamp would have gone wrong
// and stayed wrong.
func TestDueDayClamp(t *testing.T) {
	s := Snapshot{
		Period:   Period{StartsOn: d("2027-01-01"), EndsOn: d("2028-12-31")},
		Advances: []Advance{adv("a", "2020-01-01", 1500, 31)},
		Today:    d("2027-01-01"),
	}
	byMonth := map[string]CountedMonth{}
	for _, m := range countedMonths(s) {
		byMonth[m.Month.String()] = m
	}
	for _, tc := range []struct{ month, wantDue string }{
		{"2027-02", "2027-02-28"}, // 28 days
		{"2028-02", "2028-02-29"}, // leap year
		{"2027-04", "2027-04-30"}, // 30-day month
		{"2027-01", "2027-01-31"}, // 31 days — no clamp
	} {
		m := byMonth[tc.month]
		if got := m.DueOn.String(); got != tc.wantDue {
			t.Errorf("%s due_on = %s, want %s", tc.month, got, tc.wantDue)
		}
		if want := tc.month != "2027-01"; m.DueClamped != want {
			t.Errorf("%s clamped = %v, want %v", tc.month, m.DueClamped, want)
		}
	}
}

// TestDueIsInclusiveAtEquality — due on the 15th means paid ON the 15th, not
// the 16th (D155).
func TestDueIsInclusiveAtEquality(t *testing.T) {
	base := func(today string) CountedMonth {
		s := Snapshot{
			Period:   Period{StartsOn: d("2026-08-01"), EndsOn: d("2026-08-31")},
			Advances: []Advance{adv("a", "2020-01-01", 1500, 15)},
			Today:    d(today),
		}
		return countedMonths(s)[0]
	}
	if !base("2026-08-15").IsDue {
		t.Error("a záloha due on the 15th must count as due ON the 15th")
	}
	if base("2026-08-14").IsDue {
		t.Error("it must not count as due on the 14th")
	}
}

// TestChangingDueDayMovesOnlyTheRecommendation is D155's whole claim.
func TestChangingDueDayMovesOnlyTheRecommendation(t *testing.T) {
	a := Summarize(fixtureGeneral(1))
	b := Summarize(fixtureGeneral(31))
	eq(t, "CostTotalHaler", deref(t, a.CostTotalHaler, "a"), deref(t, b.CostTotalHaler, "b"))
	eq(t, "AdvancesTotalHaler", a.AdvancesTotalHaler, b.AdvancesTotalHaler)
	eq(t, "months", int64(len(a.Months)), int64(len(b.Months)))
	if *a.RecommendedKc == *b.RecommendedKc {
		t.Error("the due day must move the doporučená záloha — otherwise the fixture proves nothing")
	}
}

// TestPaymentWinsOverSchedule pins D144.
func TestPaymentWinsOverSchedule(t *testing.T) {
	s := fixtureGeneral(15)
	paid := d("2026-05-02")
	s.Payments = []Payment{{ID: "pay1", Month: mo("2026-04"), AmountHaler: 250_000, PaidOn: &paid}}
	sum := Summarize(s)

	m := sum.Months[0]
	if m.Source != SourcePayment {
		t.Errorf("source = %q, want %q", m.Source, SourcePayment)
	}
	eq(t, "amount", m.AmountHaler, 250_000)
	// The due day still comes from the SCHEDULE — attribution is by month key,
	// and "how many zálohy remain" is a calendar question.
	if got := m.DueOn.String(); got != "2026-04-15" {
		t.Errorf("due_on = %s, want 2026-04-15", got)
	}
	// 11 scheduled months at 1 800 + one recorded 2 500.
	eq(t, "AdvancesTotalHaler", sum.AdvancesTotalHaler, 11*180_000+250_000)
}

// TestMonthWithNoScheduleContributesZero — "bez předpisu" is 0 Kč, not an error.
func TestMonthWithNoScheduleContributesZero(t *testing.T) {
	s := fixtureGeneral(15)
	s.Advances = []Advance{adv("a", "2026-09-01", 1800, 15)} // nothing before September
	sum := Summarize(s)
	for _, m := range sum.Months[:5] { // duben…srpen
		if m.Source != SourceNone {
			t.Errorf("%s source = %q, want %q", m.Month, m.Source, SourceNone)
		}
		eq(t, m.Month.String()+" amount", m.AmountHaler, 0)
	}
	eq(t, "AdvancesTotalHaler", sum.AdvancesTotalHaler, 7*180_000)
}

// ---------------------------------------------------------------------------
// The hard block (D137) and the missing opening reading (D140)
// ---------------------------------------------------------------------------

func fixtureBlocked() Snapshot {
	return Snapshot{
		Period: Period{ID: "p1", StartsOn: d("2026-04-01"), EndsOn: d("2027-03-31")},
		Readings: []Reading{
			rd("r1", "2026-04-01", 12000, 30000),
			rd("r2", "2026-12-01", 13000, 32000),
			rd("r3", "2027-02-01", 13400, 32900),
		},
		Tariffs: []Tariff{
			tf("A", "2026-01-01", 3200, 2400, 350),
			tf("B", "2027-01-01", 3600, 2700, 380), // strictly inside [1. 12., 1. 2.)
		},
		Advances: []Advance{adv("a1", "2026-01-01", 1800, 15)},
		Today:    d("2027-02-10"),
	}
}

func TestHardBlockRefusesToPriceAcrossAPriceChange(t *testing.T) {
	s := Summarize(fixtureBlocked())

	if s.Status != StatusBlocked {
		t.Fatalf("status = %q, want %q", s.Status, StatusBlocked)
	}
	if len(s.Blocking) != 1 {
		t.Fatalf("blocking = %+v, want exactly one entry", s.Blocking)
	}
	b := s.Blocking[0]
	if b.Kind != BlockTariffChange {
		t.Errorf("kind = %q, want %q", b.Kind, BlockTariffChange)
	}
	if got := b.RequiredReadingOn.String(); got != "2027-01-01" {
		t.Errorf("required reading = %s, want 2027-01-01", got)
	}
	if b.MessageCS == "" || !contains(b.MessageCS, "1. 1. 2027") {
		t.Errorf("message must name the missing date, got %q", b.MessageCS)
	}

	// Everything BEFORE the gap stays valid and visible…
	if s.Actual == nil {
		t.Fatal("the actual side must survive a later block")
	}
	if got := s.Actual.ToOn.String(); got != "2026-12-01" {
		t.Errorf("actual ends %s, want 2026-12-01 — it must stop at the gap", got)
	}
	if s.Actual.EnergyHaler == 0 {
		t.Error("the figures before the gap must still be computed")
	}
	// …and nothing after it is produced. Money is never interpolated.
	if s.CostTotalHaler != nil || s.BalanceHaler != nil || s.RecommendedKc != nil {
		t.Error("no total may be produced while a block is unresolved")
	}
	if s.Headroom == nil {
		t.Error("headroom stands in for the totals whenever they are unavailable")
	}
}

// TestResolvingTheBlockMovesNoEarlierNumber is THE assertion of the hard-block
// design: adding the missing odečet must leave every figure dated before the gap
// byte-identical. If resolving a gap silently repriced history, the strictness
// would have bought nothing.
func TestResolvingTheBlockMovesNoEarlierNumber(t *testing.T) {
	blocked := fixtureBlocked()
	before := Summarize(blocked)
	beforeIvs, _ := BuildIntervals(blocked)

	resolved := fixtureBlocked()
	resolved.Readings = append(resolved.Readings, rd("r4", "2027-01-01", 13180, 32480))
	after := Summarize(resolved)
	afterIvs, _ := BuildIntervals(resolved)

	if after.Status != StatusOK {
		t.Fatalf("status after resolving = %q, want %q", after.Status, StatusOK)
	}
	if len(after.Blocking) != 0 {
		t.Errorf("blocking after resolving = %+v, want empty", after.Blocking)
	}

	// The one interval that existed before the gap is identical, field for field.
	if len(beforeIvs) == 0 || len(afterIvs) == 0 {
		t.Fatal("both runs must produce intervals")
	}
	if beforeIvs[0] != afterIvs[0] {
		t.Errorf("the pre-gap interval moved:\n before %+v\n after  %+v", beforeIvs[0], afterIvs[0])
	}
	// And so is the actual side up to that point.
	eq(t, "pre-gap energy", before.Actual.EnergyHaler, beforeIvs[0].EnergyHaler)
	eq(t, "pre-gap fee", before.Actual.FeeHaler,
		sumFees(feeChunks(blocked.Tariffs, d("2026-04-01"), d("2026-12-01"))))
}

func TestMissingOpeningReadingShowsNoMoneyAtAll(t *testing.T) {
	s := fixtureGeneral(15)
	s.Readings = []Reading{rd("r2", "2026-08-01", 12640, 31480)} // no reading on 1. 4.
	sum := Summarize(s)

	if sum.Status != StatusBlocked {
		t.Fatalf("status = %q, want %q", sum.Status, StatusBlocked)
	}
	if len(sum.Blocking) != 1 || sum.Blocking[0].Kind != BlockPeriodStart {
		t.Fatalf("blocking = %+v, want one period_start entry", sum.Blocking)
	}
	if got := sum.Blocking[0].RequiredReadingOn.String(); got != "2026-04-01" {
		t.Errorf("required reading = %s, want 2026-04-01", got)
	}
	// D140: no baseline, so NO MONEY AT ALL — not even an actual span.
	if sum.Actual != nil {
		t.Error("without an opening reading there is no baseline and no actual side")
	}
	if sum.CostTotalHaler != nil || sum.BalanceHaler != nil {
		t.Error("no money at all")
	}
	// The zálohy are still known — they are calendar arithmetic.
	eq(t, "AdvancesTotalHaler", sum.AdvancesTotalHaler, 2_160_000)
}

// TestTariffChangeOnAnIntervalBoundaryIsNotABlock — equality at either end is
// not a straddle: == d1 means the version governs the whole interval, == d2
// means the change starts exactly where the next interval starts.
func TestTariffChangeOnAnIntervalBoundaryIsNotABlock(t *testing.T) {
	for _, on := range []string{"2026-04-01", "2026-08-01"} {
		s := fixtureGeneral(15)
		s.Tariffs = []Tariff{
			tf("A", "2026-01-01", 3200, 2400, 350),
			tf("B", on, 3600, 2700, 380),
		}
		sum := Summarize(s)
		if len(sum.Blocking) != 0 {
			t.Errorf("effective_from %s: blocking = %+v, want none", on, sum.Blocking)
		}
		if sum.Status != StatusOK {
			t.Errorf("effective_from %s: status = %q, want ok", on, sum.Status)
		}
	}
}

func TestNoTariffIsInsufficientDataNotABlock(t *testing.T) {
	s := fixtureGeneral(15)
	s.Tariffs = []Tariff{tf("A", "2026-05-01", 3200, 2400, 350)} // starts AFTER the period
	sum := Summarize(s)

	if sum.Status != StatusInsufficientData {
		t.Fatalf("status = %q, want %q", sum.Status, StatusInsufficientData)
	}
	if sum.Reason != ReasonNoTariff {
		t.Errorf("reason = %q, want %q", sum.Reason, ReasonNoTariff)
	}
	// Nothing is missing from the MEASUREMENTS, so this is not a Blocking.
	if len(sum.Blocking) != 0 {
		t.Errorf("blocking = %+v, want empty — a missing ceník is configuration, not a gap in the readings", sum.Blocking)
	}
}

// ---------------------------------------------------------------------------
// Future prices (D142) and editing a version (D136)
// ---------------------------------------------------------------------------

func TestFutureTariffMovesOnlyTheForecast(t *testing.T) {
	withB := Summarize(fixtureGeneral(15))

	noB := fixtureGeneral(15)
	noB.Tariffs = []Tariff{tf("A", "2026-01-01", 3200, 2400, 350)}
	only := Summarize(noB)

	if *withB.Actual != *only.Actual {
		t.Errorf("a future ceník moved the ACTUAL side:\n with %+v\n without %+v", withB.Actual, only.Actual)
	}
	if withB.Forecast.EnergyHaler == only.Forecast.EnergyHaler {
		t.Error("a future ceník must change the forecast — otherwise D142 is not implemented")
	}
	if withB.Forecast.EnergyHaler <= only.Forecast.EnergyHaler {
		t.Error("the January rise must make the forecast larger")
	}
}

func TestEditingATariffMovesOnlyItsOwnDays(t *testing.T) {
	base := Summarize(fixtureGeneral(15))

	edited := fixtureGeneral(15)
	edited.Tariffs[1] = tf("B", "2027-01-01", 5000, 4000, 500) // ceník B repriced
	after := Summarize(edited)

	if *base.Actual != *after.Actual {
		t.Error("editing a FUTURE version must not move a single measured figure")
	}
	if base.Forecast.EnergyHaler == after.Forecast.EnergyHaler {
		t.Error("editing B must move the days B governs")
	}
}

// ---------------------------------------------------------------------------
// D157 — the closing reading
// ---------------------------------------------------------------------------

// fixtureClosed runs the worked example to its end. Note the reading on
// 1. 1. 2027: without it the interval 1. 8. 2026 → 1. 4. 2027 would STRADDLE
// ceník B's start and the period would be blocked, not complete. That is the
// hard block doing its job, and it is why a household has to take a reading when
// the price changes — the fixture has to obey the same rule as the user.
func fixtureClosed() Snapshot {
	s := fixtureGeneral(15)
	s.Readings = append(s.Readings,
		rd("r3", "2027-01-01", 13400, 33200),
		// The reading dated ends_on + 1 — simultaneously the next period's opening.
		rd("r4", "2027-04-01", 14200, 34600))
	s.Today = d("2027-04-05")
	return s
}

func TestClosingReadingMakesThePeriodEntirelyActual(t *testing.T) {
	sum := Summarize(fixtureClosed())

	if sum.Status != StatusComplete {
		t.Fatalf("status = %q, want %q", sum.Status, StatusComplete)
	}
	if !sum.Closed {
		t.Error("Closed must be true once the closing reading exists")
	}
	if sum.Forecast == nil || sum.Forecast.Days != 0 {
		t.Errorf("forecast must be empty, got %+v", sum.Forecast)
	}
	eq(t, "CostTotalHaler", deref(t, sum.CostTotalHaler, "cost"), sum.Actual.CostHaler)
	if got := sum.LastReadingOn.String(); got != "2027-04-01" {
		t.Errorf("last reading = %s, want 2027-04-01", got)
	}
}

func TestInvoiceComparisonAppearsOnlyWithARecordedInvoice(t *testing.T) {
	s := fixtureClosed()

	if Summarize(s).Invoice != nil {
		t.Error("no comparison before the vyúčtování arrives")
	}

	total := int64(2_189_000)
	vt, nt := int64(21_500), int64(45_000)
	s.Period.InvoicedTotalHaler = &total
	s.Period.InvoicedVTDkwh = &vt
	s.Period.InvoicedNTDkwh = &nt
	sum := Summarize(s)

	if sum.Invoice == nil {
		t.Fatal("the comparison must appear once the invoice is recorded")
	}
	eq(t, "diff", sum.Invoice.DiffHaler, *sum.CostTotalHaler-total)
	// The kWh line is what attributes a discrepancy to an odhadnutý odečet
	// rather than to a pricing surprise (D154).
	if sum.Invoice.DiffVTDkwh == nil || sum.Invoice.DiffNTDkwh == nil {
		t.Fatal("the kWh comparison must be produced when the supplier's readings are stored")
	}
	eq(t, "computed VT", *sum.Invoice.ComputedVTDkwh, 22_000) // 14 200 − 12 000 kWh
	eq(t, "diff VT", *sum.Invoice.DiffVTDkwh, 22_000-vt)
}

// TestChangingUnconfirmedEndReprojectsButMovesNoActual pins acceptance #10.
func TestChangingUnconfirmedEndReprojectsButMovesNoActual(t *testing.T) {
	a := Summarize(fixtureGeneral(15))
	b := fixtureGeneral(15)
	b.Period.EndsOn = d("2027-05-31") // the supplier's real end, two months later
	after := Summarize(b)

	if *a.Actual != *after.Actual {
		t.Error("correcting ends_on must not move a measured figure")
	}
	if a.Forecast.Days == after.Forecast.Days {
		t.Error("a later end must lengthen the forecast")
	}
	if len(after.Months) != 14 {
		t.Errorf("counted months = %d, want 14", len(after.Months))
	}
}

// ---------------------------------------------------------------------------
// D158 — the displayed VT/NT split
// ---------------------------------------------------------------------------

func TestVTPlusNTAlwaysEqualsTheEnergyTotal(t *testing.T) {
	snaps := map[string]Snapshot{
		"worked example": fixtureGeneral(15),
		"blocked":        fixtureBlocked(),
		"day one":        fixtureKarelDayOne(),
		// Built so two INDEPENDENT roundings would disagree with the sum: both
		// products land on a half-haléř.
		"adversarial rounding": {
			Period:   Period{StartsOn: d("2026-01-01"), EndsOn: d("2026-12-31")},
			Readings: []Reading{rd("r1", "2026-01-01", 0, 0), rd("r2", "2026-03-01", 1, 1)},
			Tariffs:  []Tariff{tf("A", "2025-01-01", 3333.33, 6666.67, 100)},
			Advances: []Advance{adv("a", "2025-01-01", 1000, 15)},
			Today:    d("2026-03-05"),
		},
	}
	for name, s := range snaps {
		sum := Summarize(s)
		for _, sp := range []struct {
			side string
			span *Span
		}{{"actual", sum.Actual}, {"forecast", sum.Forecast}} {
			if sp.span == nil {
				continue
			}
			if got := sp.span.EnergyVTHaler + sp.span.EnergyNTHaler; got != sp.span.EnergyHaler {
				t.Errorf("%s/%s: VT+NT = %d, want %d — the two lines on screen must sum to the headline above them",
					name, sp.side, got, sp.span.EnergyHaler)
			}
		}
		ivs, _ := BuildIntervals(s)
		for i, iv := range ivs {
			if iv.Blocked {
				continue
			}
			if got := iv.EnergyVTHaler + iv.EnergyNTHaler; got != iv.EnergyHaler {
				t.Errorf("%s interval %d: VT+NT = %d, want %d", name, i, got, iv.EnergyHaler)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// D159 — Historie allocates exact costs, never reprices interpolated kWh
// ---------------------------------------------------------------------------

func TestHistoryCostsSumToTheActualTotal(t *testing.T) {
	s := fixtureClosed()
	sum := Summarize(s)

	months := BuildHistory(s, mo("2026-04"), mo("2027-03"))
	var energy, fees int64
	for _, m := range months {
		energy += m.EnergyHaler
		fees += m.FeesHaler
	}
	// Fees are already per month and are allocated to nothing, so they must
	// reconcile exactly.
	eq(t, "Σ history fees", fees, deref(t, sum.FeeTotalHaler, "FeeTotalHaler"))
	// Energy is cut along day counts, so the split may lose a haléř per cut but
	// must stay within one haléř per contributing month.
	if diff := energy - sum.Actual.EnergyHaler; diff > int64(len(months)) || diff < -int64(len(months)) {
		t.Errorf("Σ history energy = %d, actual = %d — allocation drifted by %d",
			energy, sum.Actual.EnergyHaler, diff)
	}
}

func TestHistoryMarksInterpolatedMonths(t *testing.T) {
	s := fixtureGeneral(15)
	months := BuildHistory(s, mo("2026-04"), mo("2026-08"))
	// The single interval 1. 4. → 1. 8. spans four month boundaries, so every
	// month it touches is approximate and the UI must say so (D138).
	for _, m := range months[:4] {
		if !m.IsApproximate {
			t.Errorf("%s must be marked approximate — its kWh are spread across days", m.Month)
		}
	}
}

func TestHistoryExactWhenReadingsFallOnBoundaries(t *testing.T) {
	s := Snapshot{
		Period: Period{StartsOn: d("2026-01-01"), EndsOn: d("2026-03-31")},
		Readings: []Reading{
			rd("r1", "2026-01-01", 1000, 2000),
			rd("r2", "2026-02-01", 1100, 2200),
			rd("r3", "2026-03-01", 1200, 2400),
		},
		Tariffs:  []Tariff{tf("A", "2025-01-01", 3200, 2400, 350)},
		Advances: []Advance{adv("a", "2025-01-01", 1800, 15)},
		Today:    d("2026-03-10"),
	}
	for _, m := range BuildHistory(s, mo("2026-01"), mo("2026-02")) {
		if m.IsApproximate {
			t.Errorf("%s: readings fall on the boundaries, so the month is exact and the caveat must be dropped", m.Month)
		}
	}
}

// ---------------------------------------------------------------------------
// Invariants — finance's TestInvariants in another domain, for the same reason:
// to make it impossible for a later refactor to introduce a SECOND way of
// totalling the same numbers.
// ---------------------------------------------------------------------------

func TestInvariantsOverRandomisedSequences(t *testing.T) {
	rng := rand.New(rand.NewSource(20260820))

	for iter := 0; iter < 1000; iter++ {
		s := randomSnapshot(rng)

		sum := Summarize(s) // must not panic on any shape
		ivs, _ := BuildIntervals(s)

		// 1. Actual energy is DEFINED as the sum of the interval costs.
		if sum.Actual != nil {
			var want, wantVT, vtDkwh, ntDkwh int64
			for _, iv := range ivs {
				if iv.Blocked {
					break
				}
				want += iv.EnergyHaler
				wantVT += iv.EnergyVTHaler
				vtDkwh += iv.VTDkwh
				ntDkwh += iv.NTDkwh
			}
			if sum.Actual.EnergyHaler != want {
				t.Fatalf("iter %d: Σ interval energy = %d, Actual.EnergyHaler = %d", iter, want, sum.Actual.EnergyHaler)
			}
			if sum.Actual.EnergyVTHaler != wantVT {
				t.Fatalf("iter %d: Σ interval VT = %d, Actual.EnergyVTHaler = %d", iter, wantVT, sum.Actual.EnergyVTHaler)
			}
			if sum.Actual.VTDkwh != vtDkwh || sum.Actual.NTDkwh != ntDkwh {
				t.Fatalf("iter %d: kWh do not reconcile", iter)
			}

			// 2. The actual fee is exactly the sum of its chunks.
			wantFee := sumFees(feeChunks(s.Tariffs, s.Period.StartsOn, sum.Actual.ToOn))
			if sum.Actual.FeeHaler != wantFee {
				t.Fatalf("iter %d: Σ fee chunks = %d, Actual.FeeHaler = %d", iter, wantFee, sum.Actual.FeeHaler)
			}

			// 3. VT + NT is the energy, on both sides.
			if sum.Actual.EnergyVTHaler+sum.Actual.EnergyNTHaler != sum.Actual.EnergyHaler {
				t.Fatalf("iter %d: actual VT+NT != energy", iter)
			}
			if sum.Forecast != nil &&
				sum.Forecast.EnergyVTHaler+sum.Forecast.EnergyNTHaler != sum.Forecast.EnergyHaler {
				t.Fatalf("iter %d: forecast VT+NT != energy", iter)
			}
		}

		// 4. The three totals reconcile, and so does the balance.
		if sum.CostTotalHaler != nil {
			if *sum.EnergyTotalHaler+*sum.FeeTotalHaler != *sum.CostTotalHaler {
				t.Fatalf("iter %d: energy + fee != cost", iter)
			}
			if sum.AdvancesTotalHaler-*sum.CostTotalHaler != *sum.BalanceHaler {
				t.Fatalf("iter %d: advances − cost != balance", iter)
			}
			if sum.Forecast == nil {
				t.Fatalf("iter %d: totals without a forecast span", iter)
			}
			if *sum.EnergyTotalHaler != sum.Actual.EnergyHaler+sum.Forecast.EnergyHaler {
				t.Fatalf("iter %d: energy total is not the sum of its two sides", iter)
			}
		} else {
			// 5. D161: absent, never zero — and the headroom stands in for it
			// whenever a ceník and a záloha exist to compute one.
			if sum.BalanceHaler != nil || sum.RecommendedKc != nil {
				t.Fatalf("iter %d: a balance without a cost", iter)
			}
		}

		// 6. The counted months always reconcile with their two sums.
		var total, due int64
		for _, m := range sum.Months {
			total += m.AmountHaler
			if m.IsDue {
				due += m.AmountHaler
			}
		}
		if total != sum.AdvancesTotalHaler || due != sum.AdvancesDueHaler {
			t.Fatalf("iter %d: counted months do not reconcile", iter)
		}

		// 7. Nothing is ever negative that cannot be.
		if sum.AdvancesTotalHaler < 0 || sum.AdvancesDueHaler < 0 {
			t.Fatalf("iter %d: negative zálohy", iter)
		}
		if sum.RecommendedKc != nil && *sum.RecommendedKc < 0 {
			t.Fatalf("iter %d: negative doporučená záloha", iter)
		}
	}
}

func randomSnapshot(rng *rand.Rand) Snapshot {
	start := d("2026-01-01").AddDays(rng.Intn(400))
	length := 1 + rng.Intn(500)
	period := Period{ID: "p", StartsOn: start, EndsOn: start.AddDays(length)}

	// 1–4 ceník versions, some before the period and some inside it.
	nT := 1 + rng.Intn(4)
	tariffs := make([]Tariff, 0, nT)
	tStart := start.AddDays(-rng.Intn(400) - 1)
	for i := 0; i < nT; i++ {
		tariffs = append(tariffs, Tariff{
			ID:              string(rune('A' + i)),
			EffectiveFrom:   tStart,
			PriceVTHaler:    int64(1 + rng.Intn(1_000_000)),
			PriceNTHaler:    int64(1 + rng.Intn(1_000_000)),
			MonthlyFeeHaler: int64(rng.Intn(200_000)),
		})
		tStart = tStart.AddDays(1 + rng.Intn(300))
	}

	// 3–20 readings, monotonically non-decreasing in both registers (D150 — the
	// store refuses anything else, so compute never sees it).
	nR := 3 + rng.Intn(18)
	readings := make([]Reading, 0, nR)
	on := start
	var vt, nt int64 = int64(rng.Intn(1_000_000)), int64(rng.Intn(1_000_000))
	for i := 0; i < nR; i++ {
		readings = append(readings, Reading{ID: string(rune('a' + i)), ReadOn: on, VTDkwh: vt, NTDkwh: nt})
		on = on.AddDays(1 + rng.Intn(90))
		if on.After(period.EndsOn.AddDays(1)) {
			break
		}
		vt += int64(rng.Intn(50_000))
		nt += int64(rng.Intn(50_000))
	}
	// Occasionally drop the opening reading, to exercise D140.
	if rng.Intn(10) == 0 && len(readings) > 1 {
		readings = readings[1:]
	}

	advances := []Advance{{
		ID: "adv", EffectiveFrom: start.AddDays(-rng.Intn(60)),
		AmountHaler: int64(rng.Intn(500_000)), DueDay: 1 + rng.Intn(31),
	}}

	return Snapshot{
		Period: period, Readings: readings, Tariffs: tariffs, Advances: advances,
		Today: start.AddDays(rng.Intn(length + 60)),
	}
}

// TestOverflowHeadroom asserts the int64 bound the file's comment claims, with
// absurd-but-legal inputs: a 10 GWh meter delta, a 10 000 Kč/MWh price and a
// 400-day forecast.
func TestOverflowHeadroom(t *testing.T) {
	s := Snapshot{
		Period: Period{StartsOn: d("2026-01-01"), EndsOn: d("2027-01-30")},
		Readings: []Reading{
			{ID: "a", ReadOn: d("2026-01-01"), VTDkwh: 0, NTDkwh: 0},
			{ID: "b", ReadOn: d("2026-02-01"), VTDkwh: 10_000_000, NTDkwh: 10_000_000},
		},
		Tariffs: []Tariff{{ID: "T", EffectiveFrom: d("2025-01-01"),
			PriceVTHaler: 1_000_000, PriceNTHaler: 1_000_000, MonthlyFeeHaler: 100_000}},
		Advances: []Advance{adv("a", "2025-01-01", 1500, 15)},
		Today:    d("2026-02-05"),
	}
	sum := Summarize(s)
	if sum.CostTotalHaler == nil {
		t.Fatal("expected totals")
	}
	if *sum.CostTotalHaler <= 0 {
		t.Errorf("cost total overflowed to %d", *sum.CostTotalHaler)
	}
	if sum.Forecast.EnergyHaler <= 0 {
		t.Errorf("forecast energy overflowed to %d", sum.Forecast.EnergyHaler)
	}
}

func TestDivRoundHalfAwayFromZero(t *testing.T) {
	for _, tc := range []struct{ num, den, want int64 }{
		{10, 4, 3}, {11, 4, 3}, {14, 4, 4}, // 2.5 → 3, 2.75 → 3, 3.5 → 4
		{-10, 4, -3}, {-14, 4, -4},
		{0, 7, 0}, {7, 7, 1},
	} {
		if got := divRound(tc.num, tc.den); got != tc.want {
			t.Errorf("divRound(%d, %d) = %d, want %d", tc.num, tc.den, got, tc.want)
		}
	}
}

func TestParseMonthRejectsImpossibleMonths(t *testing.T) {
	for _, bad := range []string{"2026-13", "2026-00", "2026-1", "202607", "", "abcd-ef"} {
		if _, err := ParseMonth(bad); err == nil {
			t.Errorf("ParseMonth(%q) must fail", bad)
		}
	}
	m, err := ParseMonth("2026-07")
	if err != nil || m != (Month{2026, time.July}) {
		t.Errorf("ParseMonth(2026-07) = %v, %v", m, err)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
