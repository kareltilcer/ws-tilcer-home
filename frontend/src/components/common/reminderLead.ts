import type { ReminderLead } from '@/api/types'

// LEAD_LABEL is the ONE table of reminder leads: the form offers them and the
// detail view reports the chosen one, and two hand-kept lists in two files were
// what adding '0d' had to edit twice. A Record<ReminderLead, string> is what
// makes the single table safe — the compiler refuses a lead added to the union
// and not labelled here, which an array of { value, label } accepts in silence.
// The wording is the DETAIL view's, so the chip a member picks and the line they
// read back afterwards are the same string; a bare '1 den' under a checkbox that
// no longer says "předem" does not say which side of the day it falls on.
export const LEAD_LABEL: Record<ReminderLead, string> = {
  '0d': 'v den události',
  '1d': '1 den předem',
  '2d': '2 dny předem',
  '1w': 'týden předem',
  '2w': '2 týdny předem',
  '1m': 'měsíc předem',
}

// LEAD_OPTIONS is LEAD_LABEL in the order the form offers: SHORTEST LEAD FIRST,
// from the day itself out to a month before it. (That is the latest-arriving
// reminder first, not the soonest — the chips read as a distance from the event,
// which is the thing a member is choosing.) Derived from the table rather than
// retyped beside it, so a new lead reaches the chips by being labelled and by
// nothing else. Insertion order is the key order here: every key is a non-numeric
// string, so Object.keys preserves it.
export const LEAD_OPTIONS: { value: ReminderLead; label: string }[] = (
  Object.keys(LEAD_LABEL) as ReminderLead[]
).map((value) => ({ value, label: LEAD_LABEL[value] }))
