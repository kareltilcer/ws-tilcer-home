import { describe, expect, it } from 'vitest'
import { fmtWhen, newMessagesAnchor, type DividerCandidate } from './when'

// The conversation row's timestamp and the *Nové zprávy* divider's position — the
// two pieces of the v10 chat design that are decisions rather than markup.

describe('fmtWhen — the row shortens towards today', () => {
  // A fixed "now" rather than the clock: the boundaries are the whole point, and a
  // test that runs at 00:05 must not disagree with one that runs at 23:55.
  const now = new Date(2026, 7, 27, 14, 30) // 27. 8. 2026, 14:30

  it('shows a clock for today', () => {
    expect(fmtWhen(new Date(2026, 7, 27, 9, 5).toISOString(), now)).toBe('09:05')
  })

  // ⚠ CALENDAR DAYS, NOT A 24-HOUR WINDOW. 23:50 last night is "včera" at 00:10 this
  // morning, which is what somebody reading the list means by the word — a rolling
  // window would still be calling it a clock time twenty minutes later.
  it('calls last night včera even twenty minutes after midnight', () => {
    const justAfterMidnight = new Date(2026, 7, 27, 0, 10)
    expect(fmtWhen(new Date(2026, 7, 26, 23, 50).toISOString(), justAfterMidnight)).toBe('včera')
  })

  it('drops the year within this year', () => {
    expect(fmtWhen(new Date(2026, 2, 4, 18, 0).toISOString(), now)).toBe('4. 3.')
  })

  // ⚠ D20's shape when it does print a date: `d. M. yyyy`, no leading zeroes.
  it('spells an older year out in the house format', () => {
    expect(fmtWhen(new Date(2025, 11, 24, 18, 0).toISOString(), now)).toBe('24. 12. 2025')
  })

  it('renders nothing for an unusable timestamp', () => {
    expect(fmtWhen('not a date', now)).toBe('')
  })
})

describe('newMessagesAnchor — which message the Nové zprávy line sits above', () => {
  const ME = 'u_me'
  /** `n` messages from somebody else, oldest first, ids m0…m{n-1}. */
  const theirs = (n: number, from = 0): DividerCandidate[] =>
    Array.from({ length: n }, (_, i) => ({
      id: `m${from + i}`,
      author_id: 'u_other',
      deleted: false,
    }))

  it('counts back from the newest message', () => {
    // 10 loaded, 3 unread → the line sits above the 8th, m7.
    expect(newMessagesAnchor(theirs(10), 3, false, ME)).toBe('m7')
  })

  it('draws nothing when everything has been read', () => {
    expect(newMessagesAnchor(theirs(10), 0, false, ME)).toBeNull()
  })

  it('draws nothing in an empty thread', () => {
    expect(newMessagesAnchor([], 4, false, ME)).toBeNull()
  })

  // ⚠ THE HONEST CASE. More unread than loaded means the real boundary is above the
  // top of the page — anchoring to the oldest LOADED message would claim it is the
  // first unread one, which is a different and false statement.
  it('withholds the line when the boundary is above the loaded page', () => {
    expect(newMessagesAnchor(theirs(50), 62, true, ME)).toBeNull()
  })

  // ...unless there is nothing above it, in which case the top means what it says.
  it('puts it at the top when the whole thread is loaded and all of it is unread', () => {
    expect(newMessagesAnchor(theirs(6), 6, false, ME)).toBe('m0')
    expect(newMessagesAnchor(theirs(6), 9, false, ME)).toBe('m0')
  })

  // ⚠ PREPENDING AN OLDER PAGE MUST NOT MOVE IT. *Načíst starší* adds messages above
  // the line rather than sliding the line down through the thread being read.
  it('keeps the same message under the line after an older page is loaded', () => {
    const page = theirs(20, 50)
    const withOlder = [...theirs(50), ...page]
    expect(newMessagesAnchor(page, 4, true, ME)).toBe('m66')
    expect(newMessagesAnchor(withOlder, 4, true, ME)).toBe('m66')
  })

  // ⚠ AND AN ARRIVING MESSAGE IS WHY IT IS AN ID. A position counted from the end
  // moves under an APPEND — which is what every /ws frame and every send does — so
  // the line slid one row down per message and ended up below what had already been
  // read. An id resolved once on entry survives the same append untouched; the
  // caller is what must resolve it only once (see ThreadView's `dividerAnchor`).
  it('names a message, so an arrival cannot move a line already resolved', () => {
    const before = theirs(10)
    const anchor = newMessagesAnchor(before, 3, false, ME)
    expect(anchor).toBe('m7')

    const after = [...before, { id: 'm10', author_id: 'u_other', deleted: false }]
    expect(after.findIndex((m) => m.id === anchor)).toBe(7)
    // What the position this replaced would have said on the very same render.
    expect(after.length - 3).toBe(8)
  })

  // ⚠ THE BADGE NEVER COUNTED THESE ROWS. UnreadCount is "not mine, not a tombstone",
  // and the thread renders both like any other message — so counting back over every
  // row put the line one position too low for each of them.
  it('skips tombstones and the caller own messages while counting back', () => {
    const messages: DividerCandidate[] = [
      ...theirs(7),
      { id: 'm7', author_id: 'u_other', deleted: false },
      { id: 'm8', author_id: 'u_other', deleted: true },
      { id: 'm9', author_id: ME, deleted: false },
      { id: 'm10', author_id: 'u_other', deleted: false },
    ]
    // The server counts two: m7 and m10. The line belongs above m7, not above m9.
    expect(newMessagesAnchor(messages, 2, false, ME)).toBe('m7')
  })
})
