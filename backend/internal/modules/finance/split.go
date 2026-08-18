package finance

import "math"

// Compute derives the nine split values from a month's two incomes and four
// rates. It is a DIRECT PORT of the retiring `fin` service's
// backend/internal/months/split.go and the formula is LOCKED (PRD §V6-4 FR-F3,
// D82): the arithmetic below was confirmed with Karel on 2026-06-10 and carried
// over unchanged. Only the package name differs from the original.
//
// THE ORDER IS THE SPECIFICATION, not an implementation detail:
//
//  1. personal is rounded FIRST, so personal + toOperational == income holds
//     exactly per person (the transfer is a subtraction, never a second rounding
//     of income × (1−p)).
//  2. the two savings transfers round off the TOTAL income, not off what the
//     operational account received.
//  3. needs is a SUBTRACTION — it is where every rounding error is deliberately
//     parked so both the per-person and the per-account totals reconcile.
//
// Any "tidy-up" that rounds needs directly, or that computes toOperational as
// round(income × (1−p)), silently breaks an invariant and will not be caught by
// the worked example alone. See TestInvariants.
//
// The four invariants (asserted in split_test.go over every fixture and over
// randomised inputs):
//
//	PersonalKaja + ToOperationalKaja == incomeKaja
//	PersonalAndy + ToOperationalAndy == incomeAndy
//	FunSavings + NoFunSavings + Needs == OperationalReceived
//	PersonalKaja + PersonalAndy + OperationalReceived == TotalIncome
//
// KNOWN EDGE CASE, inherited and deliberately preserved: because Needs is the
// remainder it can come out NEGATIVE, by at most 2 CZK. The bound is provable —
// personal rounding moves OperationalReceived by at most 1 and each savings
// rounding by at most ½, so Needs ≥ T×o/100 − 2, and therefore Needs < 0
// requires T × o < 200. That happens at operational = 0 for any income (worst
// case −2) and, for operational ≥ 1, only below 200 Kč of total income.
// DO NOT clamp it: clamping breaks FunSavings + NoFunSavings + Needs ==
// OperationalReceived, the invariant the whole rounding scheme exists to
// protect. The frontend renders a negative Needs as "0 Kč" with a footnote; the
// data stays honest.
//
// Money is whole CZK end to end. The only float64 in the system lives between
// the multiplication and math.Round below — exactly where `fin` put it.
func Compute(incomeKaja, incomeAndy int, r Rates) Split {
	total := incomeKaja + incomeAndy
	p := float64(r.Personal) / 100
	f := float64(r.Fun) / 100
	n := float64(r.NoFun) / 100

	personalKaja := int(math.Round(float64(incomeKaja) * p))
	personalAndy := int(math.Round(float64(incomeAndy) * p))

	toOperationalKaja := incomeKaja - personalKaja
	toOperationalAndy := incomeAndy - personalAndy
	opReceived := toOperationalKaja + toOperationalAndy

	fun := int(math.Round(float64(total) * f))
	noFun := int(math.Round(float64(total) * n))
	needs := opReceived - fun - noFun

	return Split{
		TotalIncome:         total,
		PersonalKaja:        personalKaja,
		PersonalAndy:        personalAndy,
		ToOperationalKaja:   toOperationalKaja,
		ToOperationalAndy:   toOperationalAndy,
		OperationalReceived: opReceived,
		FunSavings:          fun,
		NoFunSavings:        noFun,
		Needs:               needs,
	}
}

// Sum returns the four rates added together. Valid input sums to exactly 100
// (PRD FR-F2); the service rejects anything else with a 422 and the table's own
// CHECK is the second net.
func (r Rates) Sum() int { return r.Personal + r.Operational + r.Fun + r.NoFun }

// Savings is the pooled total that leaves the operational account each month.
// Savings are joint and are never attributed per person.
func (s Split) Savings() int { return s.FunSavings + s.NoFunSavings }
