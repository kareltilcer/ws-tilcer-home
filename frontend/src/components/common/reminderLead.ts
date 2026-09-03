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

// DEFAULT_LEAD is the lead the form opens on for a new event, and the one it falls
// back to for a stored lead this build cannot draw a chip for.
export const DEFAULT_LEAD: ReminderLead = '1w'

// knownLead is the ONE guarded read of the table and both accessors below go
// through it. `Record<ReminderLead, …>` types the lookup as total and it is not:
// the key is whatever the API sent, so `LEAD_LABELS[e.reminder_lead].detail` reads
// as safe while being `undefined.detail` for a lead the server has learned first.
//
// ⚠ `lead in LEAD_LABELS` IS NOT THE GUARD. `in` walks the prototype chain, so it
// would admit 'toString' and 'constructor' as leads. Reading a field off the result
// is what separates a real entry from an inherited member.
function knownLead(lead: string | null | undefined): { chip: string; detail: string } | undefined {
  const hit = lead == null ? undefined : LEAD_LABELS[lead as ReminderLead]
  return typeof hit?.detail === 'string' ? hit : undefined
}

// leadDetail is the DETAIL VIEW's line for a lead that came off the wire, and it
// takes a plain string for that reason. An unknown lead falls back to the raw code
// rather than throwing: `🔔 3d` keeps the failure to the one line it belongs to,
// where `undefined.detail` took the whole event detail behind the error boundary
// and the single-string table before it printed a blank.
export function leadDetail(lead: string): string {
  return knownLead(lead)?.detail ?? lead
}

// asLead is the FORM's answer to the same hazard, and it is deliberately a
// DIFFERENT one. The form is a chip selector: a lead it has no chip for is a lead
// it cannot show as chosen, and casting the wire value through `as ReminderLead`
// left every chip unpressed under a ticked checkbox with nothing naming the
// setting. Falling back to DEFAULT_LEAD keeps what the form SHOWS and what it SAVES
// the same value — the detail view can afford to echo a code it does not
// understand, a picker cannot.
export function asLead(lead: string | null | undefined): ReminderLead {
  return knownLead(lead) ? (lead as ReminderLead) : DEFAULT_LEAD
}
