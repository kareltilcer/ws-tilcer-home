import { fmtDate, fmtTime } from '@/i18n/format'

/**
 * fmtWhen is the conversation row's timestamp — the design's `c.when`.
 *
 * ⚠ IT SHORTENS TOWARDS TODAY, which is the only reason it exists rather than
 * `fmtDateTime`. The list is read by scanning down the right edge for the room that
 * moved most recently, and `27. 8. 2026 14:20` on every row makes the four
 * characters that actually differ the hardest ones to find. Today is a clock,
 * yesterday is a word, this year drops the year, and only an older room spells the
 * whole date out.
 *
 * ⚠ AND IT KEEPS D20's SHAPE where it prints a date: `d. M. yyyy` through
 * `fmtDate`, `HH:mm` through `fmtTime`. The house format is not re-spelled here —
 * this decides WHICH of them a row gets, nothing more.
 *
 * `now` is injectable so the boundaries can be tested without freezing the clock.
 */
export function fmtWhen(iso: string, now: Date = new Date()): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''

  // Calendar days, not 24-hour windows: a message at 23:50 is "včera" at 00:10 the
  // next morning, which is what somebody reading the list means by the word.
  const days = daysBetween(d, now)
  if (days === 0) return fmtTime(iso)
  if (days === 1) return 'včera'
  if (d.getFullYear() === now.getFullYear()) return `${d.getDate()}. ${d.getMonth() + 1}.`
  return fmtDate(d)
}

/** Whole calendar days from `d` to `now`, local time. Negative for the future. */
function daysBetween(d: Date, now: Date): number {
  const a = new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
  const b = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  return Math.round((b - a) / 86_400_000)
}

/**
 * newMessagesIndex places the *Nové zprávy* divider (§V10-7, fixed vocabulary).
 *
 * It answers ONE question: before which loaded message does the divider go? The
 * thread is rendered oldest-first, the unread messages are the newest ones, so the
 * boundary is counted from the END — which is also what keeps it still while older
 * pages are prepended above it.
 *
 * ⚠ `unread` MUST BE THE COUNT SNAPSHOTTED WHEN THE THREAD WAS OPENED. The read
 * marker advances as soon as the newest message is on screen, so a divider driven
 * by the live count disappears in the same frame it is drawn — the member is told
 * "you left off here" and then has it taken away before they can look.
 *
 * ⚠ AND A BOUNDARY THAT IS NOT IN THE LOADED PAGE IS NOT DRAWN. When more unread
 * messages exist than have been loaded, the true boundary is above the top of the
 * page — putting the divider at index 0 would claim the oldest LOADED message is
 * the first unread one, which is a different and false statement. Only when the
 * whole thread is loaded (`hasMore` false) does index 0 mean what it says.
 *
 * Returns null when there is no divider to draw.
 */
export function newMessagesIndex(
  loaded: number,
  unread: number,
  hasMore: boolean,
): number | null {
  if (unread <= 0 || loaded <= 0) return null
  const i = loaded - unread
  if (i > 0) return i
  // Everything loaded is unread. Honest only when there is nothing above it.
  return hasMore ? null : 0
}
