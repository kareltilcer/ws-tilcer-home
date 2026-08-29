// ISO-8601 week arithmetic for the garden module, in one place.
//
// ⚠ IT EXISTS BECAUSE THE MODULE HAD TWO OF THESE, and they never met.
// `TimingWindowInput` walked whole weeks forward from 4 January to preview a
// sowing window's real dates; `KalendarTab` walked a date back to its Thursday
// to group tasks into weeks. Two hand-rolled ISO-8601 implementations in one
// module is two chances at the same off-by-one, and an off-by-one here is a
// sowing task on the wrong side of a season boundary.
//
// ⚠ ALL OF IT MUST AGREE WITH THE BACKEND, which uses Go's `time.Time.ISOWeek()`
// (garden/timing.go). The frontend is a PREVIEW — every date the app stores was
// resolved once by the server — but a preview that disagrees with what gets
// saved is worse than no preview at all. `isoWeek.test.ts` pins these answers
// against the backend's for the week-53 years, which is where the two would part.
//
// Everything is UTC. A window is a calendar fact, not an instant, so a local
// timezone would only ever be a way to land on the wrong day.

/** addDays returns d shifted by n whole days. Safe as plain milliseconds
 *  because every date here is UTC, where days are exactly 86 400 000 ms. */
export function addDays(d: Date, n: number): Date {
  return new Date(d.getTime() + n * 86400000)
}

/** toISO renders a date as `YYYY-MM-DD`, the wire format the API uses. */
export function toISO(d: Date): string {
  return d.toISOString().slice(0, 10)
}

/** weeksInISOYear returns 52 or 53. Computed rather than table-looked-up: ask
 *  whether the Thursday of a hypothetical week 53 is still in this year. */
export function weeksInISOYear(year: number): number {
  const jan4 = new Date(Date.UTC(year, 0, 4))
  const offset = (jan4.getUTCDay() + 6) % 7
  const week53 = addDays(jan4, -offset + 52 * 7)
  // Week 53 exists iff its Thursday is still in this year.
  return addDays(week53, 3).getUTCFullYear() === year ? 53 : 52
}

/** isoWeekMonday returns the Monday of an ISO week, clamping a missing week 53
 *  to week 52 — the same deviation the backend makes, for the same reason: the
 *  alternative silently lands in January of the following year. */
export function isoWeekMonday(year: number, week: number): Date {
  const weeks = weeksInISOYear(year)
  const w = Math.min(Math.max(week, 1), weeks)
  // 4 January is in ISO week 1 of every year, by definition.
  const jan4 = new Date(Date.UTC(year, 0, 4))
  const offset = (jan4.getUTCDay() + 6) % 7 // Monday = 0
  return addDays(jan4, -offset + (w - 1) * 7)
}

/** isoWeekKey turns a `YYYY-MM-DD` date into a sortable `YYYY-Www` bucket.
 *
 *  The ISO YEAR is not always the calendar year: 1 January 2027 belongs to
 *  2026-W53, which is exactly why the key carries its own year rather than
 *  pairing a week number with the date's. An unparseable date returns
 *  `0000-W00`, which sorts before every real week — the calendar groups by this
 *  key, and a bad date belongs at the top where it is visible, not dropped. */
export function isoWeekKey(iso: string): string {
  const d = new Date(iso + 'T00:00:00Z')
  if (Number.isNaN(d.getTime())) return '0000-W00'
  // ISO week: Thursday of the current week decides the year.
  const target = addDays(d, 3 - ((d.getUTCDay() + 6) % 7))
  const firstThursday = new Date(Date.UTC(target.getUTCFullYear(), 0, 4))
  const week =
    1 + Math.round(((target.getTime() - firstThursday.getTime()) / 86400000 - 3 + ((firstThursday.getUTCDay() + 6) % 7)) / 7)
  return `${target.getUTCFullYear()}-W${String(week).padStart(2, '0')}`
}
