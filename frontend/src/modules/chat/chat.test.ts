import { describe, expect, it } from 'vitest'
import { qk } from '@/api/keys'
import { mayPersistKey } from '@/platform/pwa/persist'

// v10 leak table, the two rows that are frontend-side and testable without a DOM.

describe('leak row 20 — chat never reaches the disk', () => {
  // ⚠ ASSERTED AGAINST THE PERSISTER'S CONFIGURATION, not against a comment. Chat
  // is excluded from the PWA persister ENTIRELY — a deliberate departure from every
  // other module, which all render read-only from cache when the network is gone.
  // Message bodies and other members' display names on a shared laptop's disk are
  // worth less than the offline convenience.
  it('refuses every chat query key', () => {
    const chatKeys = [
      qk.chatConversations(),
      qk.chatConversations('trash'),
      qk.chatConversation('c-1'),
      qk.chatMessages('c-1'),
      qk.chatMembers('c-1'),
      qk.chatSearch('heslo', 'c-1'),
      qk.chatDirectory,
      qk.chatAll,
    ]
    for (const key of chatKeys) {
      expect(mayPersistKey(key), `${JSON.stringify(key)} must never be persisted`).toBe(false)
    }
  })

  // ⚠ A KEY ADDED LATER IS COVERED TOO. The rule is the `chat` PREFIX rather than a
  // list, so a pinned-message list or a draft cannot reach disk just because nobody
  // remembered to extend an allow-list.
  it('refuses a chat key that does not exist yet', () => {
    expect(mayPersistKey(['chat', 'drafts', 'c-1'])).toBe(false)
  })

  // The counterpart: without it, a persister that refused EVERYTHING would pass the
  // assertions above while quietly breaking offline for the other ten modules.
  it('still persists the modules that are meant to work offline', () => {
    expect(mayPersistKey(qk.dashboard)).toBe(true)
    expect(mayPersistKey(qk.boards)).toBe(true)
    expect(mayPersistKey(qk.electricityTariffs)).toBe(true)
  })
})

describe('every chat key carries its conversation id', () => {
  // ⚠ THE SINGLE MOST LIKELY FRONTEND BUG IN THIS MODULE, and the wrong version
  // still works: two conversations sharing one cache key look fine until somebody
  // switches rooms and TanStack serves the other thread from cache — which here
  // means other people's messages under a heading that names yours. The v9 `scope`
  // lesson, in a module where the payload IS the content.
  it('produces different keys for different conversations', () => {
    expect(qk.chatMessages('a')).not.toEqual(qk.chatMessages('b'))
    expect(qk.chatMembers('a')).not.toEqual(qk.chatMembers('b'))
    expect(qk.chatConversation('a')).not.toEqual(qk.chatConversation('b'))
  })

  it('keeps the id as its own segment rather than concatenated into one', () => {
    // A key of ['chat', 'messages:a'] would also differ between rooms, and would
    // break prefix invalidation — qk.chatAll could no longer reach it.
    expect(qk.chatMessages('a')).toContain('a')
    for (const segment of qk.chatMessages('a')) {
      expect(typeof segment).toBe('string')
      expect(segment).not.toMatch(/:/)
    }
  })

  // ⚠ TanStack invalidates by PREFIX. The obvious nesting —
  // ['chat','conversation',id,'messages'] — makes chatConversation(id) a prefix of
  // chatMessages(id), so advancing the read marker refetches the whole thread on
  // every message in every open tab. Both keys look correct on their own, which is
  // why this is a test and not a review note; it was found by counting requests.
  it('never lets one conversation key be a prefix of another', () => {
    const isPrefixOf = (a: readonly unknown[], b: readonly unknown[]) =>
      a.length < b.length && a.every((seg, i) => seg === b[i])

    const keys = [
      qk.chatConversation('c-1'),
      qk.chatMessages('c-1'),
      qk.chatMembers('c-1'),
      qk.chatConversations(),
    ]
    for (const a of keys) {
      for (const b of keys) {
        if (a === b) continue
        expect(isPrefixOf(a, b), `${JSON.stringify(a)} must not be a prefix of ${JSON.stringify(b)}`).toBe(false)
      }
    }
  })

  it('hangs every key off the chatAll prefix, so one invalidation reaches them all', () => {
    for (const key of [
      qk.chatConversations(),
      qk.chatConversation('c-1'),
      qk.chatMessages('c-1'),
      qk.chatMembers('c-1'),
      qk.chatSearch('q'),
      qk.chatDirectory,
    ]) {
      expect(key[0]).toBe(qk.chatAll[0])
    }
  })
})
