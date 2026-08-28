import { describe, expect, it } from 'vitest'
import { qk } from '@/api/keys'
import { mayPersistKey } from '@/platform/pwa/persist'
import { attachmentURL, thumbnailURL } from './api/endpoints'

// PR 3's frontend-side invariants — the ones that are testable without a DOM and
// that a reviewer cannot see by reading a component.

describe('leak row 20 covers PR 3s keys too', () => {
  // ⚠ THE STORAGE PICTURE AND THE CLEAN-UP LISTING ARE CHAT DATA. The listing
  // carries filenames, sizes, uploaders and conversation NAMES from rooms the caller
  // belongs to — exactly the member-restricted content the persister exclusion
  // exists for. The rule is the `chat` prefix, so this passes by construction; the
  // test is what proves the two new keys actually took that prefix.
  it('refuses the storage and cleanup keys', () => {
    expect(mayPersistKey(qk.chatStorage)).toBe(false)
    expect(mayPersistKey(qk.chatCleanup())).toBe(false)
    expect(mayPersistKey(qk.chatCleanup('c-1', 'recent'))).toBe(false)
  })
})

describe('PR 3s keys keep the module invariants', () => {
  const isPrefixOf = (a: readonly unknown[], b: readonly unknown[]) =>
    a.length < b.length && a.every((seg, i) => seg === b[i])

  // ⚠ THE RESOURCE COMES BEFORE THE ID, for the reason §V10-12 records: nesting the
  // id first makes one key a PREFIX of another, and TanStack invalidates by prefix —
  // so a per-conversation cleanup filter would refetch every cleanup listing, and a
  // conversation key would refetch its own thread on every read-marker advance.
  it('never lets one key be a prefix of another', () => {
    const keys = [
      qk.chatStorage,
      qk.chatCleanup(),
      qk.chatCleanup('c-1', 'size'),
      qk.chatConversation('c-1'),
      qk.chatMessages('c-1'),
    ]
    for (const a of keys) {
      for (const b of keys) {
        if (a === b) continue
        expect(
          isPrefixOf(a, b),
          `${JSON.stringify(a)} must not be a prefix of ${JSON.stringify(b)}`,
        ).toBe(false)
      }
    }
  })

  it('separates the two sort orders, which return different pages', () => {
    expect(qk.chatCleanup(undefined, 'size')).not.toEqual(qk.chatCleanup(undefined, 'recent'))
  })

  it('still hangs off the chatAll prefix', () => {
    expect(qk.chatStorage[0]).toBe(qk.chatAll[0])
    expect(qk.chatCleanup()[0]).toBe(qk.chatAll[0])
  })
})

describe('attachment URLs are session-gated backend paths', () => {
  // ⚠ NO PRESIGNED URL ANYWHERE IN HOME, and chat does not introduce the first one
  // (D33/D42). Every view goes through the backend so membership is re-resolved on
  // each request — which is the whole reason the cache policy can be
  // `no-cache, must-revalidate` and mean something.
  it('points at /api/chat and carries no signature', () => {
    const url = attachmentURL('a-1')
    expect(url).toBe('/api/chat/attachments/a-1/raw')
    expect(url).not.toMatch(/https?:\/\//)
    expect(url).not.toMatch(/signature|X-Amz|token/i)
  })

  // `?download=true` is the ONLY download affordance: there is no /download path,
  // because only one of the three kinds ever needs one (FR-V10-7).
  it('forces an attachment disposition with a query flag, not a second path', () => {
    expect(attachmentURL('a-1', { download: true })).toBe(
      '/api/chat/attachments/a-1/raw?download=true',
    )
  })

  it('serves a thumbnail from its own path', () => {
    expect(thumbnailURL('a-1')).toBe('/api/chat/attachments/a-1/thumbnail')
  })
})
