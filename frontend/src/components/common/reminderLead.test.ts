import { describe, expect, it } from 'vitest'
import { asLead, DEFAULT_LEAD, LEAD_LABELS, LEAD_OPTIONS, leadDetail } from './reminderLead'

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
  // LEAD_OPTIONS is `Object.keys(LEAD_LABELS).map(…)`, so this one literal pins the
  // table's KEY SET as well as the order it is read in. Asserting the keys
  // separately would be a third hand-kept copy of the same list — in the file whose
  // whole point is that there is one — and it could not fail without this failing
  // first. The compiler covers the direction neither can: `Record<ReminderLead, …>`
  // refuses a lead added to the union and not labelled here.
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

// asLead is the same guard for the FORM, and it answers differently on purpose: a
// picker that cannot draw a chip for a lead must not report that lead as chosen, so
// what the form shows and what it saves stay one value.
describe('asLead', () => {
  it('keeps a lead the table knows', () => {
    expect(asLead('0d')).toBe('0d')
    expect(asLead('2w')).toBe('2w')
  })

  it('falls back to the default for null, empty and a lead this build has never heard of', () => {
    expect(asLead(null)).toBe(DEFAULT_LEAD)
    expect(asLead(undefined)).toBe(DEFAULT_LEAD)
    expect(asLead('')).toBe(DEFAULT_LEAD)
    expect(asLead('3d')).toBe(DEFAULT_LEAD)
  })

  // Both accessors read a FIELD off the lookup rather than testing `in`, which
  // walks the prototype chain and would make 'toString' a reminder lead.
  it('does not mistake an inherited object property for a lead', () => {
    expect(asLead('toString')).toBe(DEFAULT_LEAD)
    expect(asLead('constructor')).toBe(DEFAULT_LEAD)
    expect(leadDetail('toString')).toBe('toString')
  })
})
