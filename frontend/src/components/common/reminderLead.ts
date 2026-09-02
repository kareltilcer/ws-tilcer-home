import type { ReminderLead } from '@/api/types'

// LEAD_LABELS is the ONE table of reminder leads: the form offers them and the
// detail view reports the chosen one, and two hand-kept lists in two files were
// what adding '0d' had to edit twice. A Record<ReminderLead, …> is what makes the
// single table safe — the compiler refuses a lead added to the union and not
// labelled here, which an array of { value, label } accepts in silence.
//
// Each lead carries TWO strings because a chip and a sentence are different
// registers, not because the wording drifted. `detail` says the direction outright
// ("1 den předem") since it is read alone, out of any list. `chip` is terse
// because it is read inside one: 'v den události' sits first and anchors the axis,
// so every chip after it is a distance BEFORE the day, the way every calendar app
// spells this. Making the chips carry "předem" too costs a third row on a phone —
// measured at 375 px: 114 px against 72.7 px — of permanently reserved space under
// a checkbox that is usually unticked, to restate what the first chip already said.
export const LEAD_LABELS: Record<ReminderLead, { chip: string; detail: string }> = {
  '0d': { chip: 'v den události', detail: 'v den události' },
  '1d': { chip: '1 den', detail: '1 den předem' },
  '2d': { chip: '2 dny', detail: '2 dny předem' },
  '1w': { chip: '1 týden', detail: 'týden předem' },
  '2w': { chip: '2 týdny', detail: '2 týdny předem' },
  '1m': { chip: '1 měsíc', detail: 'měsíc předem' },
}

// LEAD_OPTIONS is LEAD_LABELS in the order the form offers: SHORTEST LEAD FIRST,
// from the day itself out to a month before it. (That is the latest-arriving
// reminder first, not the soonest — the chips read as a distance from the event,
// which is the thing a member is choosing.) Derived from the table rather than
// retyped beside it, so a new lead reaches the chips by being labelled and by
// nothing else. Insertion order is the key order here: every key is a non-numeric
// string, so Object.keys preserves it.
export const LEAD_OPTIONS: { value: ReminderLead; label: string }[] = (
  Object.keys(LEAD_LABELS) as ReminderLead[]
).map((value) => ({ value, label: LEAD_LABELS[value].chip }))
