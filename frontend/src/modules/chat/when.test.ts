import { describe, expect, it } from 'vitest'
import { fmtWhen, newMessagesIndex } from './when'

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

describe('newMessagesIndex — where the Nové zprávy line goes', () => {
  it('counts back from the newest message', () => {
    // 10 loaded, 3 unread → the line sits above the 8th (index 7).
    expect(newMessagesIndex(10, 3, false)).toBe(7)
  })

  it('draws nothing when everything has been read', () => {
    expect(newMessagesIndex(10, 0, false)).toBeNull()
  })

  it('draws nothing in an empty thread', () => {
    expect(newMessagesIndex(0, 4, false)).toBeNull()
  })

  // ⚠ THE HONEST CASE. More unread than loaded means the real boundary is above the
  // top of the page — putting the line at index 0 would claim the oldest LOADED
  // message is the first unread one, which is a different and false statement.
  it('withholds the line when the boundary is above the loaded page', () => {
    expect(newMessagesIndex(50, 62, true)).toBeNull()
  })

  // ...unless there is nothing above it, in which case index 0 means what it says.
  it('puts it at the top when the whole thread is loaded and all of it is unread', () => {
    expect(newMessagesIndex(6, 6, false)).toBe(0)
    expect(newMessagesIndex(6, 9, false)).toBe(0)
  })

  // ⚠ PREPENDING AN OLDER PAGE MUST NOT MOVE IT. The index is counted from the end
  // precisely so that *Načíst starší* adds messages above the line rather than
  // sliding the line down through the thread the member is reading.
  it('keeps the same message under the line after an older page is loaded', () => {
    const before = newMessagesIndex(20, 4, true)
    const after = newMessagesIndex(70, 4, true)
    expect(before).toBe(16)
    expect(after).toBe(66)
    // Same distance from the newest message in both cases — the same message.
    expect(20 - (before as number)).toBe(70 - (after as number))
  })
})
