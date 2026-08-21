import { describe, expect, it } from 'vitest'
import { kc, kc2, kcSigned, kwh, kwhWhole, monthLabelShort, plural } from './format'
import { PLURAL } from '@/i18n/plural'

const NBSP = ' ' // non-breaking space, as format.ts emits

// The two money precisions are this module's most drift-prone rule — the kind of
// thing that quietly becomes "whichever the last person wrote" once four screens
// are formatting figures. Pinned here so the rule lives in a test rather than in
// a comment.

describe('money — two precisions, one rule per figure type', () => {
  it('renders TOTALS as whole koruny', () => {
    // The worked example's period total: 2 156 049 h → 21 560 Kč.
    expect(kc(2_156_049)).toBe(`21${NBSP}560${NBSP}Kč`)
    expect(kc(150_000)).toBe(`1${NBSP}500${NBSP}Kč`)
    expect(kc(0)).toBe(`0${NBSP}Kč`)
  })

  it('renders UNIT PRICES and FEES with their haléře', () => {
    // These are the figures the supplier printed on a contract: showing
    // 4 859 Kč/MWh would make the ceník screen disagree with the paper it was
    // typed from.
    expect(kc2(485_865)).toBe(`4${NBSP}858,65${NBSP}Kč`)
    expect(kc2(402_669)).toBe(`4${NBSP}026,69${NBSP}Kč`)
    expect(kc2(64_235)).toBe(`642,35${NBSP}Kč`)
    // Karel's headroom, the first real number he ever sees.
    expect(kc2(85_765)).toBe(`857,65${NBSP}Kč`)
  })

  it('pads the haléře so a round figure is not rendered as 4 858,5', () => {
    expect(kc2(485_800)).toBe(`4${NBSP}858,00${NBSP}Kč`)
    expect(kc2(485_805)).toBe(`4${NBSP}858,05${NBSP}Kč`)
  })

  it('always prints the SIGN on a balance, because colour must not carry it alone', () => {
    expect(kcSigned(3_951)).toBe(`+40${NBSP}Kč`) // the worked example's přeplatek
    expect(kcSigned(-124_000)).toBe(`−1${NBSP}240${NBSP}Kč`)
    // A genuine zero is a real answer and must render as one, not as an absence.
    expect(kcSigned(0)).toBe(`+0${NBSP}Kč`)
  })

  it('groups thousands with a NON-BREAKING space so a figure cannot wrap at 375 px', () => {
    expect(kc(2_156_049)).toContain(NBSP)
    expect(kc(2_156_049)).not.toContain(' ') // no ordinary space anywhere
  })
})

describe('energy — whole kWh, with a tenth only when there is one', () => {
  it('drops the decimal for a whole number, which is every reading from Karel meter', () => {
    expect(kwh(320)).toBe(`32${NBSP}kWh`)
    expect(kwh(6_400)).toBe(`640${NBSP}kWh`)
    expect(kwh(0)).toBe(`0${NBSP}kWh`)
  })

  it('shows the tenth when one is present, rather than truncating it silently', () => {
    // A future decimal meter must never be rounded away on screen.
    expect(kwh(1_234)).toBe(`123,4${NBSP}kWh`)
  })

  it('renders the day-one headroom figures the screen leads with', () => {
    expect(kwh(1765)).toBe(`176,5${NBSP}kWh`)
    expect(kwh(2006)).toBe(`200,6${NBSP}kWh`)
  })
})

describe('plurals', () => {
  it('uses all three Czech forms', () => {
    expect(plural(1, PLURAL.days)).toBe(`1${NBSP}den`)
    expect(plural(2, PLURAL.days)).toBe(`2${NBSP}dny`)
    expect(plural(5, PLURAL.days)).toBe(`5${NBSP}dní`)
    expect(plural(122, PLURAL.days)).toBe(`122${NBSP}dní`)
    expect(plural(1, PLURAL.readings)).toBe(`1${NBSP}odečet`)
    expect(plural(2, PLURAL.readings)).toBe(`2${NBSP}odečty`)
    expect(plural(5, PLURAL.readings)).toBe(`5${NBSP}odečtů`)
  })

  it('keeps the count and its noun on one line', () => {
    expect(plural(47, PLURAL.days)).not.toContain(' ')
  })
})

describe('month labels', () => {
  it('abbreviates for a chart axis, where the full name never fits at 375 px', () => {
    expect(monthLabelShort('2026-07')).toBe(`čvc${NBSP}26`)
    expect(monthLabelShort('2027-06')).toBe(`čvn${NBSP}27`)
    expect(monthLabelShort('2026-01')).toBe(`led${NBSP}26`)
  })
})

describe('kwhWhole — the headroom chips only', () => {
  it('floors to whole kWh, matching the values the spec pins', () => {
    // Karel's day one: 857,65 Kč buys ~176 VT / 213 NT / 200 at the 30/70 mix.
    // 1765 must read 176, NOT the 177 that rounding would give.
    expect(kwhWhole(1765)).toBe(`176${NBSP}kWh`)
    expect(kwhWhole(2130)).toBe(`213${NBSP}kWh`)
    expect(kwhWhole(2006)).toBe(`200${NBSP}kWh`)
  })

  it('understates rather than overstates what the záloha buys', () => {
    expect(kwhWhole(1999)).toBe(`199${NBSP}kWh`)
  })

  it('does NOT replace kwh(), which must never round a meter reading', () => {
    expect(kwh(1765)).toBe(`176,5${NBSP}kWh`)
  })
})
