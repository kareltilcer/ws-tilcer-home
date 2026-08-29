import { describe, expect, it } from 'vitest'
import { addDays, isoWeekKey, isoWeekMonday, toISO, weeksInISOYear } from './isoWeek'

// ⚠ EVERY EXPECTED VALUE BELOW WAS COMPUTED BY THE BACKEND, not by reading this
// implementation back to itself. `weeksInISOYear` and `isoWeekMonday` mirror
// garden/timing.go; `isoWeekKey` mirrors Go's `time.Time.ISOWeek()`, which
// timing.go's `isoWeekOf` calls directly. Both sides resolve the same window,
// and a preview that disagrees with what the server stores is worse than none.
//
// The week-53 years are the whole point: 2020, 2026 and 2032 have one, and the
// years either side of them do not. That is where a hand-rolled ISO-8601
// implementation goes wrong, and this module exists because there were two of
// them in one feature.

describe('weeksInISOYear', () => {
  it('finds the 53-week years and no others', () => {
    for (const year of [2020, 2026, 2032]) {
      expect(weeksInISOYear(year), `${year} has an ISO week 53`).toBe(53)
    }
    for (const year of [2019, 2021, 2024, 2025, 2027, 2033]) {
      expect(weeksInISOYear(year), `${year} has no ISO week 53`).toBe(52)
    }
  })
})

describe('isoWeekMonday', () => {
  it('opens week 1 on the Monday the backend picks, including the ones in December', () => {
    // ISO week 1 is the week containing 4 January, so it routinely starts in the
    // previous calendar year — the case a naive "first Monday of January" gets
    // wrong by up to four days.
    expect(toISO(isoWeekMonday(2026, 1))).toBe('2025-12-29')
    expect(toISO(isoWeekMonday(2027, 1))).toBe('2027-01-04')
    expect(toISO(isoWeekMonday(2032, 1))).toBe('2031-12-29')
  })

  it('opens week 53 in the years that have one', () => {
    expect(toISO(isoWeekMonday(2020, 53))).toBe('2020-12-28')
    expect(toISO(isoWeekMonday(2026, 53))).toBe('2026-12-28')
    expect(toISO(isoWeekMonday(2032, 53))).toBe('2032-12-27')
  })

  it('clamps a missing week 53 to week 52 rather than sliding into January', () => {
    // The deviation timing.go documents: the alternative silently lands in the
    // following January, which is a sowing task on the wrong side of the season.
    expect(toISO(isoWeekMonday(2027, 53))).toBe('2027-12-27')
    expect(toISO(isoWeekMonday(2027, 53))).toBe(toISO(isoWeekMonday(2027, 52)))
    // 2026 does have a week 53, so nothing is clamped there.
    expect(toISO(isoWeekMonday(2026, 53))).not.toBe(toISO(isoWeekMonday(2026, 52)))
  })

  it('clamps a week below 1 as well — the control lets a number be typed', () => {
    // The backend clamps only the top end; the input is a free number field, so
    // this side also has to survive a 0 or a minus sign mid-typing.
    expect(toISO(isoWeekMonday(2027, 0))).toBe(toISO(isoWeekMonday(2027, 1)))
    expect(toISO(isoWeekMonday(2027, -5))).toBe(toISO(isoWeekMonday(2027, 1)))
  })
})

describe('isoWeekKey', () => {
  it('agrees with the backend on the dates that straddle a year boundary', () => {
    // The ISO year is not the calendar year. Each of these was read off Go's
    // time.Time.ISOWeek().
    expect(isoWeekKey('2026-01-01')).toBe('2026-W01')
    expect(isoWeekKey('2026-03-16')).toBe('2026-W12')
    expect(isoWeekKey('2026-12-28')).toBe('2026-W53')
    expect(isoWeekKey('2026-12-31')).toBe('2026-W53')
    expect(isoWeekKey('2027-01-01')).toBe('2026-W53') // still last year's week
    expect(isoWeekKey('2027-01-04')).toBe('2027-W01')
    expect(isoWeekKey('2020-12-31')).toBe('2020-W53')
    expect(isoWeekKey('2021-01-01')).toBe('2020-W53')
    expect(isoWeekKey('2032-12-31')).toBe('2032-W53')
    expect(isoWeekKey('2033-01-02')).toBe('2032-W53')
  })

  it('sorts as a string in calendar order, which is what the calendar relies on', () => {
    // KalendarTab groups tasks into a Map keyed by this and sorts the keys with
    // localeCompare, so the zero-padding is load-bearing rather than cosmetic.
    const keys = ['2026-12-28', '2026-01-05', '2026-09-07', '2027-01-04'].map(isoWeekKey)
    expect([...keys].sort((a, b) => a.localeCompare(b))).toEqual([
      '2026-W02',
      '2026-W37',
      '2026-W53',
      '2027-W01',
    ])
  })

  it('sends an unparseable date to a key that sorts first rather than dropping it', () => {
    expect(isoWeekKey('not-a-date')).toBe('0000-W00')
    expect('0000-W00'.localeCompare('2026-W01')).toBeLessThan(0)
  })

  it('round-trips against isoWeekMonday for every week of a 53-week year', () => {
    // The two directions were written independently, in different files, and
    // this is the assertion that would have caught them disagreeing.
    for (let w = 1; w <= 53; w++) {
      expect(isoWeekKey(toISO(isoWeekMonday(2026, w)))).toBe(`2026-W${String(w).padStart(2, '0')}`)
    }
  })
})

describe('addDays', () => {
  it('crosses month, year and leap-day boundaries', () => {
    expect(toISO(addDays(new Date(Date.UTC(2026, 0, 31)), 1))).toBe('2026-02-01')
    expect(toISO(addDays(new Date(Date.UTC(2026, 11, 31)), 1))).toBe('2027-01-01')
    expect(toISO(addDays(new Date(Date.UTC(2028, 1, 28)), 1))).toBe('2028-02-29')
    expect(toISO(addDays(new Date(Date.UTC(2026, 0, 1)), -1))).toBe('2025-12-31')
  })
})
