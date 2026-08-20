import { describe, expect, it } from 'vitest'
import { resolveWindow } from './TimingWindowInput'
import type { GardenSeason } from '../api/types'

// The live date echo is what makes {anchor, from, to} land — so it has to be
// RIGHT, and it has to agree with the backend's timing.go. These cases mirror
// the Go tests one for one, including the week-53 clamp.

function season(overrides: Partial<GardenSeason> = {}): GardenSeason {
  return {
    year: 2027,
    status: 'planning',
    last_frost_on: '2027-05-15',
    first_frost_on: '2027-10-05',
    last_frost_actual_on: null,
    first_frost_actual_on: null,
    planting_count: 0,
    closed_at: null,
    closed_by: null,
    notes_md: null,
    ...overrides,
  }
}

describe('resolveWindow', () => {
  it('resolves an ISO week window to its Monday and Sunday', () => {
    // 2027 starts on a Friday, so ISO week 1 is 4–10 January and week 10 opens
    // on 8 March.
    expect(resolveWindow({ anchor: 'week', from: 10, to: 13 }, season())).toBe('8. 3. – 4. 4. 2027')
  })

  it('resolves a frost-anchored window against the season', () => {
    // "six to eight weeks before the last frost" — the way a Czech seed packet
    // would be read by somebody who thinks in frost dates.
    expect(resolveWindow({ anchor: 'last_frost', from: -56, to: -42 }, season())).toBe('20. 3. – 3. 4. 2027')
  })

  it('moves a frost-anchored window when the frost date moves, and leaves a week-anchored one alone', () => {
    const early = season({ last_frost_on: '2027-05-15' })
    const late = season({ last_frost_on: '2027-05-25' })

    const frost = { anchor: 'last_frost', from: -14, to: 0 } as const
    expect(resolveWindow(frost, early)).not.toBe(resolveWindow(frost, late))

    const week = { anchor: 'week', from: 20, to: 21 } as const
    expect(resolveWindow(week, early)).toBe(resolveWindow(week, late))
  })

  it('says nothing when the anchor the window needs is missing', () => {
    // The echo must not invent a date. The control then explains WHICH anchor is
    // missing rather than showing a plausible one.
    const noFrost = season({ last_frost_on: null })
    expect(resolveWindow({ anchor: 'last_frost', from: -14, to: 0 }, noFrost)).toBeNull()
    // A week-anchored window needs no frost date at all, so it still resolves.
    expect(resolveWindow({ anchor: 'week', from: 10, to: 13 }, noFrost)).not.toBeNull()
  })

  it('clamps a missing ISO week 53 to week 52 rather than sliding into January', () => {
    // 2027 has 52 ISO weeks; 2026 has 53.
    const in2027 = season({ year: 2027 })
    expect(resolveWindow({ anchor: 'week', from: 53, to: 53 }, in2027)).toBe(
      resolveWindow({ anchor: 'week', from: 52, to: 52 }, in2027),
    )
    const in2026 = season({ year: 2026, last_frost_on: '2026-05-15' })
    expect(resolveWindow({ anchor: 'week', from: 53, to: 53 }, in2026)).not.toBe(
      resolveWindow({ anchor: 'week', from: 52, to: 52 }, in2026),
    )
  })

  it('refuses an inverted range rather than swapping it', () => {
    // A typo somebody would fix in one second, if they were told.
    expect(resolveWindow({ anchor: 'week', from: 13, to: 10 }, season())).toBeNull()
  })

  it('resolves nothing without a season', () => {
    expect(resolveWindow({ anchor: 'week', from: 10, to: 13 }, undefined)).toBeNull()
  })
})
