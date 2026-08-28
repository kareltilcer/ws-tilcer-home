import { cs } from '@/i18n/cs'
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
  if (days === 1) return cs.chat.whenYesterday
  if (d.getFullYear() === now.getFullYear()) return `${d.getDate()}. ${d.getMonth() + 1}.`
  return fmtDate(d)
}

/** Whole calendar days from `d` to `now`, local time. Negative for the future. */
function daysBetween(d: Date, now: Date): number {
  const a = new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
  const b = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  return Math.round((b - a) / 86_400_000)
}

/** What the divider needs off a loaded message, and nothing more. */
export interface DividerCandidate {
  id: string
  author_id: string
  deleted: boolean
}

/**
 * newMessagesAnchor names the message the *Nové zprávy* divider sits ABOVE
 * (§V10-7, fixed vocabulary).
 *
 * It answers ONE question: which loaded message is the oldest unread one? The
 * thread is rendered oldest-first and the unread messages are the newest, so the
 * walk goes backwards from the end.
 *
 * ⚠ IT RETURNS AN ID, NOT AN INDEX (v10 review). An index counted from the end is
 * stable under a PREPEND — which is what *Načíst starší* does — and moves under an
 * APPEND, which is what every arriving message does. So a divider placed at
 * `loaded - unread` and re-evaluated each render slid one row further down for each
 * message that arrived while the thread was open, ending up below messages the
 * member had already read and eventually labelling their own just-sent message
 * *Nové zprávy*. An id cannot drift under either operation; the caller resolves it
 * once, on entry, and renders it wherever that message ends up.
 *
 * ⚠ AND THE WALK SKIPS WHAT THE SERVER DID NOT COUNT. `unread_count` is "above my
 * floor, after my read marker, NOT MINE, NOT A TOMBSTONE" (messages.go, D250), while
 * the thread renders tombstones and own messages like any other row — so counting
 * back over every row put the line one position too low for each of them.
 *
 * ⚠ `unread` MUST BE THE COUNT SNAPSHOTTED WHEN THE THREAD WAS OPENED. The read
 * marker advances as soon as the newest message is on screen, so a divider driven
 * by the live count disappears in the same frame it is drawn — the member is told
 * "you left off here" and then has it taken away before they can look.
 *
 * ⚠ AND A BOUNDARY THAT IS NOT IN THE LOADED PAGE IS NOT DRAWN. When more unread
 * messages exist than have been loaded, the true boundary is above the top of the
 * page — anchoring to the oldest LOADED message would claim it is the first unread
 * one, which is a different and false statement. Only when the whole thread is
 * loaded (`hasMore` false) does the top of the page mean what it says.
 *
 * Returns null when there is no divider to draw.
 */
export function newMessagesAnchor(
  messages: readonly DividerCandidate[],
  unread: number,
  hasMore: boolean,
  me: string,
): string | null {
  if (unread <= 0 || messages.length === 0) return null
  let remaining = unread
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]
    // A tombstone and one of my own messages are rows the badge never counted, so
    // neither may consume one of its units.
    if (m.deleted || m.author_id === me) continue
    remaining--
    if (remaining === 0) return m.id
  }
  // Everything loaded is unread. Honest only when there is nothing above it.
  return hasMore ? null : messages[0].id
}
