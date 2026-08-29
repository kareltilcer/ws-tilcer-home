import type { FinanceRates, FinanceSplit } from './api/types'

// A MIRROR of the locked split formula, not a second source of truth.
//
// It mirrors backend/internal/modules/finance/split.go (PRD §V6-4 FR-F3, D82),
// and it exists for exactly ONE job: previewing a split in the add/edit form
// before anything is saved, so someone changing a rate sees the money move. That
// is what makes the form worth using.
//
// Everything rendered in the month list, the flow visualisation and the Nástěnka
// widget comes from the SERVER's `split` block. If this file and the Go one ever
// disagree, the Go one is right — and split.test.ts, which runs the same worked
// example the Go test does, is what catches the drift.
//
// The order is the specification: personal rounds FIRST (so personal +
// to_operational === income per person), savings round off the TOTAL, and needs
// is a SUBTRACTION that absorbs every rounding error. needs can come out
// negative by up to 2 Kč; it is deliberately NOT clamped here — the UI decides
// to display 0 Kč with a footnote, which is a rendering choice, not a data one.
//
// JS Math.round is half-UP (−0.5 → −0), Go's math.Round is half-AWAY-from-zero.
// The two agree for every input this formula can produce, because incomes are
// non-negative and the rates are non-negative percentages, so no product is ever
// negative.
export function computeSplit(incomeKaja: number, incomeAndy: number, rates: FinanceRates): FinanceSplit {
  const total = incomeKaja + incomeAndy
  const p = rates.personal / 100
  const f = rates.fun / 100
  const n = rates.no_fun / 100

  const personalKaja = Math.round(incomeKaja * p)
  const personalAndy = Math.round(incomeAndy * p)

  const toOperationalKaja = incomeKaja - personalKaja
  const toOperationalAndy = incomeAndy - personalAndy
  const operationalReceived = toOperationalKaja + toOperationalAndy

  const funSavings = Math.round(total * f)
  const noFunSavings = Math.round(total * n)
  const needs = operationalReceived - funSavings - noFunSavings

  return {
    total_income: total,
    personal_kaja: personalKaja,
    personal_andy: personalAndy,
    to_operational_kaja: toOperationalKaja,
    to_operational_andy: toOperationalAndy,
    operational_received: operationalReceived,
    fun_savings: funSavings,
    no_fun_savings: noFunSavings,
    needs,
  }
}

/** The four rates added together. The form blocks submit until this is 100. */
export function ratesSum(r: FinanceRates): number {
  return r.personal + r.operational + r.fun + r.no_fun
}

/** Pooled savings — fun + no-fun. Never attributed per person. */
export function savingsOf(s: FinanceSplit): number {
  return s.fun_savings + s.no_fun_savings
}
