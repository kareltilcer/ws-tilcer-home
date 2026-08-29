import { describe, expect, it } from 'vitest'
import type { FinanceRates } from '@/api/types'
import { computeSplit, ratesSum, savingsOf } from './split'

const r = (personal: number, operational: number, fun: number, no_fun: number): FinanceRates => ({
  personal,
  operational,
  fun,
  no_fun,
})

describe('computeSplit — the mirror of the locked Go formula', () => {
  // The SAME worked example as backend split_test.go's TestWorkedExample. If this
  // and the Go port ever drift, this is what says so.
  it('reproduces the worked example', () => {
    expect(computeSplit(60000, 40000, r(20, 60, 10, 10))).toEqual({
      total_income: 100000,
      personal_kaja: 12000,
      personal_andy: 8000,
      to_operational_kaja: 48000,
      to_operational_andy: 32000,
      operational_received: 80000,
      fun_savings: 10000,
      no_fun_savings: 10000,
      needs: 60000,
    })
  })

  it.each([
    ['worked example', 60000, 40000, r(20, 60, 10, 10)],
    ['live rates', 67418, 52299, r(20, 60, 10, 10)],
    ['single earner', 83000, 0, r(20, 60, 10, 10)],
    ['no income at all', 0, 0, r(25, 25, 25, 25)],
    ['everything personal', 51234, 48766, r(100, 0, 0, 0)],
    ['all to savings', 40000, 35000, r(0, 0, 50, 50)],
    ['odd money', 12345, 6789, r(50, 20, 20, 10)],
    ['negative needs, operational 0', 6887385, 2030905, r(90, 0, 5, 5)],
    ['negative needs, tiny total', 0, 50, r(1, 1, 1, 97)],
  ])('holds every invariant: %s', (_name, kaja, andy, rates) => {
    const s = computeSplit(kaja, andy, rates)
    expect(s.total_income).toBe(kaja + andy)
    expect(s.personal_kaja + s.to_operational_kaja).toBe(kaja)
    expect(s.personal_andy + s.to_operational_andy).toBe(andy)
    expect(s.operational_received).toBe(s.to_operational_kaja + s.to_operational_andy)
    expect(s.fun_savings + s.no_fun_savings + s.needs).toBe(s.operational_received)
    expect(s.personal_kaja + s.personal_andy + s.operational_received).toBe(s.total_income)
  })

  // needs is the remainder and may go negative by up to 2 Kč. Clamping it here
  // would break fun + no_fun + needs === operational_received, so the DATA stays
  // honest and only the UI shows 0 Kč with a footnote.
  it('does not clamp a negative needs', () => {
    expect(computeSplit(6887385, 2030905, r(90, 0, 5, 5)).needs).toBe(-2)
    expect(computeSplit(0, 50, r(1, 1, 1, 97)).needs).toBe(-1)
  })

  it('sums rates and pools savings', () => {
    expect(ratesSum(r(20, 60, 10, 10))).toBe(100)
    expect(savingsOf(computeSplit(60000, 40000, r(20, 60, 10, 10)))).toBe(20000)
  })
})
