import { beforeEach, describe, expect, it } from 'vitest'
import type { Conversation } from './api/types'
import { pickConversationToOpen, readLastOpened, rememberLastOpened } from './lastOpened'

// Which room `/chat` opens (v10.1, D269).
//
// ⚠ THE ORDER IS THE WHOLE FEATURE, and it is asserted here rather than read off the
// component, because every step of it exists to avoid a specific bad landing: a 404
// on a room that was trashed since last time, an arbitrary room when nothing is
// remembered, and an empty pane when there was something to show all along.

function room(id: string, kind: Conversation['kind'] = 'group'): Conversation {
  return {
    id,
    kind,
    name: id,
    created_by: null,
    created_at: '2026-08-01T00:00:00.000Z',
    updated_at: '2026-08-29T00:00:00.000Z',
    member_count: 2,
    unread_count: 0,
    muted: false,
    effective_from: '2026-08-01T00:00:00.000Z',
    reads_from_beginning: true,
    bytes: 0,
    over_conversation_limit: false,
    last_message: null,
  }
}

const VSICHNI = room('c-all', 'default')

describe('pickConversationToOpen', () => {
  it('prefers the remembered room', () => {
    expect(pickConversationToOpen([VSICHNI, room('c-2')], 'c-2')).toBe('c-2')
  })

  // ⚠ THE ONE THAT MATTERS. A room can be left, trashed or purged between two
  // visits, and following a stored id blindly navigates straight into the 404 this
  // whole mechanism exists to keep members off.
  it('ignores a remembered room that is no longer in the list', () => {
    expect(pickConversationToOpen([VSICHNI, room('c-2')], 'c-gone')).toBe(VSICHNI.id)
  })

  it('falls back to Všichni when nothing is remembered', () => {
    expect(pickConversationToOpen([room('c-2'), VSICHNI, room('c-3')], '')).toBe(VSICHNI.id)
  })

  // Ordered by updated_at descending, so the first row is the room that moved most
  // recently — the best answer available when even Všichni is not in the list, which
  // is what a member who has left it looks like.
  it('falls back to the most recently active room when Všichni is absent', () => {
    expect(pickConversationToOpen([room('c-2'), room('c-3')], '')).toBe('c-2')
  })

  // ⚠ "ONE ROOM ALWAYS OPENS" FALLS OUT OF THE ORDER RATHER THAN BRANCHING ON IT —
  // it is either Všichni or the only row, and both are already covered above. This
  // asserts the outcome so the absence of a `length === 1` case is a decision rather
  // than a gap.
  it('always opens the only room there is, whichever kind it is', () => {
    expect(pickConversationToOpen([VSICHNI], '')).toBe(VSICHNI.id)
    expect(pickConversationToOpen([room('c-only')], '')).toBe('c-only')
    expect(pickConversationToOpen([room('c-only')], 'c-gone')).toBe('c-only')
  })

  // A member in no conversation at all sees the empty state, which says what the
  // module is. Navigating them somewhere would be navigating them nowhere.
  it('answers empty for a member with no rooms', () => {
    expect(pickConversationToOpen([], 'c-gone')).toBe('')
  })
})

describe('what is remembered', () => {
  beforeEach(() => localStorage.clear())

  // ⚠ AN ID, NOT CONTENT. Chat is excluded from the PWA persister because message
  // bodies and other members' names on a shared laptop's disk are worth less than
  // offline reading (leak row 20) — and that argument is about content. A UUID says
  // nothing to somebody reading the disk.
  it('round-trips per user, so a shared laptop does not cross the two over', () => {
    rememberLastOpened('u-kaja', 'c-2')
    rememberLastOpened('u-andy', 'c-3')
    expect(readLastOpened('u-kaja')).toBe('c-2')
    expect(readLastOpened('u-andy')).toBe('c-3')
  })

  it('answers empty for somebody who has never opened anything', () => {
    expect(readLastOpened('u-new')).toBe('')
    expect(readLastOpened('')).toBe('')
  })

  it('writes nothing when there is no user or no room', () => {
    rememberLastOpened('', 'c-2')
    rememberLastOpened('u-kaja', '')
    expect(localStorage.length).toBe(0)
  })
})
