import { beforeEach, describe, expect, it, vi } from 'vitest'
import { QueryClient } from '@tanstack/react-query'
import { qk } from '@/api/keys'
import { clientId } from '@/api/clientId'
import { applyChatFrame } from './api/hooks'
import type { ChatMessage, MessagePage } from './api/types'

// D259 — the gap check, and the two properties that make it worth having:
// it DETECTS a dropped frame, and it TERMINATES.
//
// ⚠ The hub drops a frame on a saturated socket by design and there is no replay.
// Every other module in Home is fine with that, because a dropped "something
// changed" is repaired by refetch-on-focus and nothing was lost in the meantime. A
// chat message is different: the loss IS the content, in a thread somebody is
// reading, with nothing on screen to say so.

const CONV = 'c-1'

function msg(id: string, body = 'text'): ChatMessage {
  return {
    id,
    conversation_id: CONV,
    author_id: 'u-andy',
    author_label: 'Andy',
    body,
    attachments: [],
    created_at: '2026-08-27T10:00:00.000Z',
    edited_at: null,
    deleted: false,
  }
}

function frame(message: ChatMessage, prev: string | null, origin?: string) {
  return {
    type: 'chat_message.created',
    origin,
    payload: { conversation_id: CONV, message, prev_message_id: prev },
  }
}

function thread(qc: QueryClient): ChatMessage[] {
  return qc.getQueryData<MessagePage>(qk.chatMessages(CONV))?.items ?? []
}

describe('the gap check', () => {
  let qc: QueryClient
  let invalidated: string[][]

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    invalidated = []
    vi.spyOn(qc, 'invalidateQueries').mockImplementation(async (filters) => {
      invalidated.push((filters?.queryKey ?? []) as string[])
    })
    // A tab holding one message, which is the ordinary state.
    qc.setQueryData<MessagePage>(qk.chatMessages(CONV), {
      items: [msg('m-1')],
      next_cursor: null,
      has_more: false,
    })
  })

  const refetchedThread = () =>
    invalidated.some((k) => k.join('/') === qk.chatMessages(CONV).join('/'))

  it('applies a message whose prev is what this tab holds, without refetching', () => {
    applyChatFrame(qc, frame(msg('m-2'), 'm-1'))
    expect(thread(qc).map((m) => m.id)).toEqual(['m-2', 'm-1'])
    expect(refetchedThread()).toBe(false)
  })

  it('refetches when a frame was dropped', () => {
    // m-2 never arrived, so m-3 names it as its prev and this tab holds m-1.
    applyChatFrame(qc, frame(msg('m-3'), 'm-2'))
    expect(refetchedThread()).toBe(true)
    // ⚠ The message is NOT appended on a gap. Appending m-3 over m-1 would render a
    // thread that silently omits m-2 while looking complete — the exact failure the
    // check exists to catch.
    expect(thread(qc).map((m) => m.id)).toEqual(['m-1'])
  })

  it('TERMINATES: the repair is one refetch, and the next frame matches', () => {
    applyChatFrame(qc, frame(msg('m-3'), 'm-2'))
    expect(invalidated.filter((k) => k.join('/') === qk.chatMessages(CONV).join('/'))).toHaveLength(1)

    // The refetch lands: the tab now holds the tail.
    qc.setQueryData<MessagePage>(qk.chatMessages(CONV), {
      items: [msg('m-3'), msg('m-2'), msg('m-1')],
      next_cursor: null,
      has_more: false,
    })
    invalidated = []

    // Every subsequent frame matches — no second refetch, ever.
    applyChatFrame(qc, frame(msg('m-4'), 'm-3'))
    applyChatFrame(qc, frame(msg('m-5'), 'm-4'))
    expect(refetchedThread()).toBe(false)
    expect(thread(qc).map((m) => m.id)).toEqual(['m-5', 'm-4', 'm-3', 'm-2', 'm-1'])
  })

  it('is one refetch for a member added to a busy conversation', () => {
    // Their floor leaves them holding nothing, and the first frame's prev names a
    // message below it — which they can never hold. That is the case D259 says
    // always looks like a gap exactly once.
    qc.setQueryData<MessagePage>(qk.chatMessages(CONV), {
      items: [],
      next_cursor: null,
      has_more: false,
    })
    applyChatFrame(qc, frame(msg('m-50'), 'm-49'))
    expect(refetchedThread()).toBe(true)

    qc.setQueryData<MessagePage>(qk.chatMessages(CONV), {
      items: [msg('m-50')],
      next_cursor: null,
      has_more: false,
    })
    invalidated = []
    for (const [id, prev] of [
      ['m-51', 'm-50'],
      ['m-52', 'm-51'],
      ['m-53', 'm-52'],
    ] as const) {
      applyChatFrame(qc, frame(msg(id), prev))
    }
    expect(refetchedThread()).toBe(false)
  })

  it('never gap-checks this tab’s own echo', () => {
    // ⚠ By the time our echo arrives our message is already in the cache, so its
    // `prev` legitimately names the message before ours — which is no longer what we
    // hold. Gap-checking it would cost one refetch per message we send.
    qc.setQueryData<MessagePage>(qk.chatMessages(CONV), {
      items: [msg('m-2'), msg('m-1')],
      next_cursor: null,
      has_more: false,
    })
    applyChatFrame(qc, frame(msg('m-2'), 'm-1', clientId))
    expect(refetchedThread()).toBe(false)
    // And it stays idempotent: the echo must not render a second bubble.
    expect(thread(qc).map((m) => m.id)).toEqual(['m-2', 'm-1'])
  })

  it('treats the first message in a conversation as no gap', () => {
    qc.setQueryData<MessagePage>(qk.chatMessages(CONV), {
      items: [],
      next_cursor: null,
      has_more: false,
    })
    applyChatFrame(qc, frame(msg('m-1'), null))
    expect(refetchedThread()).toBe(false)
    expect(thread(qc).map((m) => m.id)).toEqual(['m-1'])
  })

  it('applies an edit and a delete in place, and never as a gap', () => {
    const edited = { ...msg('m-1', 'opraveno'), edited_at: '2026-08-27T11:00:00.000Z' }
    applyChatFrame(qc, {
      type: 'chat_message.updated',
      payload: { conversation_id: CONV, message: edited, prev_message_id: null },
    })
    expect(thread(qc)[0].body).toBe('opraveno')
    expect(refetchedThread()).toBe(false)

    const tombstone = { ...msg('m-1', ''), deleted: true }
    applyChatFrame(qc, {
      type: 'chat_message.deleted',
      payload: { conversation_id: CONV, message: tombstone, prev_message_id: null },
    })
    expect(thread(qc)[0].deleted).toBe(true)
    expect(thread(qc)[0].body).toBe('')
  })
})
