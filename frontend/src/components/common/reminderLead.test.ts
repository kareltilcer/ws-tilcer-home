import { describe, expect, it } from 'vitest'
import type { ReminderLead } from '@/api/types'
import { LEAD_LABELS, LEAD_OPTIONS, leadDetail } from './reminderLead'

// ⚠ THE TWO REGISTERS ARE THE POINT OF THIS TABLE, AND ONLY THIS FILE HOLDS THEM
// APART. EventForm.test.tsx asserts the chips because chips are what the form draws;
// the detail view has no test of its own, so before this file the `detail` column
// could have been quietly re-unified with `chip` — the exact edit reminderLead.ts's
// comment argues against — with the whole suite still green.
//
// The other half is ORDER. The chips are terse because 'v den události' comes first
// and anchors the axis: everything after it reads as a distance BEFORE the day. Sort
// the table alphabetically, or append '0d' at the end, and every remaining chip
// silently loses the direction it never spelled out.

describe('LEAD_LABELS', () => {
  it('labels every lead the union admits, and no others', () => {
    const leads: ReminderLead[] = ['0d', '1d', '2d', '1w', '2w', '1m']
    expect(Object.keys(LEAD_LABELS).sort()).toEqual([...leads].sort())
  })

  it('spells the direction in every detail except the same-day one, and in no chip', () => {
    for (const [lead, { chip, detail }] of Object.entries(LEAD_LABELS)) {
      expect(chip).not.toContain('předem')
      if (lead === '0d') {
        expect(detail).not.toContain('předem') // there is no "before" to name
      } else {
        expect(detail).toContain('předem')
      }
    }
  })
})

describe('LEAD_OPTIONS', () => {
  it('opens with the same-day lead, then runs outwards to a month', () => {
    expect(LEAD_OPTIONS.map((o) => o.value)).toEqual(['0d', '1d', '2d', '1w', '2w', '1m'])
  })

  it('offers the chip wording, not the detail wording', () => {
    expect(LEAD_OPTIONS[0]).toEqual({ value: '0d', label: LEAD_LABELS['0d'].chip })
    const oneDay = LEAD_OPTIONS.find((o) => o.value === '1d')
    expect(oneDay?.label).toBe('1 den')
    expect(oneDay?.label).not.toBe(LEAD_LABELS['1d'].detail)
  })
})

describe('leadDetail', () => {
  it('reads a known lead out of the table', () => {
    expect(leadDetail('1d')).toBe('1 den předem')
    expect(leadDetail('0d')).toBe('v den události')
  })

  // The reason the accessor exists: the argument comes off the wire, and the
  // Record<ReminderLead, …> type says the lookup is total when it is not.
  it('falls back to the raw code for a lead this build has never heard of', () => {
    expect(() => leadDetail('3d')).not.toThrow()
    expect(leadDetail('3d')).toBe('3d')
  })
})
