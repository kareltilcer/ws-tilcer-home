import { beforeEach, describe, expect, it, vi } from 'vitest'
import { QueryClient } from '@tanstack/react-query'
import { qk } from '@/api/keys'
import { applyChatFrame } from './api/hooks'
import type { ChatMessage, Conversation, ConversationPage, MessagePage } from './api/types'

// The conversation row's preview line (v10.1, D266) against the live frames.
//
// ⚠ D266 GAVE `chat_message.updated` A SECOND CONSUMER. Before it, an edit moved
// nothing outside the thread and patching the cached message was the whole of the
// work; now the newest message's own text is drawn on the list row, so an edit that
// only patches the thread leaves the sidebar quoting the typo the member just fixed.
//
// ⚠ AND THE SAME FRAME TYPE IS WHAT A REACTION PUBLISHES (D265). Refetching the
// listing on every chip tap is exactly the traffic this module patches rather than
// invalidates to avoid, so the two have to be told apart rather than lumped together.

const CONV = 'c-1'

function msg(id: string, editedAt: string | null = null): ChatMessage {
  return {
    id,
    conversation_id: CONV,
    author_id: 'u-andy',
    author_label: 'Andy',
    body: 'Klíče jsou pod květináčem.',
    attachments: [],
    reactions: [],
    created_at: '2026-08-27T10:00:00.000Z',
    edited_at: editedAt,
    deleted: false,
  }
}

function row(previewID: string | null): Conversation {
  return {
    id: CONV,
    kind: 'group',
    name: 'Rodiče',
    created_by: null,
    created_at: '2026-08-01T00:00:00.000Z',
    updated_at: '2026-08-27T10:00:00.000Z',
    member_count: 2,
    unread_count: 0,
    muted: false,
    effective_from: '2026-08-01T00:00:00.000Z',
    reads_from_beginning: true,
    bytes: 0,
    over_conversation_limit: false,
    last_message:
      previewID === null
        ? null
        : {
            id: previewID,
            author_id: 'u-andy',
            author_label: 'Andy',
            excerpt: 'Klíče jsou pod květináčem.',
            created_at: '2026-08-27T10:00:00.000Z',
            deleted: false,
            attachment_count: 0,
          },
  }
}

function updated(message: ChatMessage, type = 'chat_message.updated') {
  return { type, payload: { conversation_id: CONV, message, prev_message_id: null } }
}

describe('what an update frame does to the conversation listing', () => {
  let qc: QueryClient
  let invalidated: string[][]

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    invalidated = []
    vi.spyOn(qc, 'invalidateQueries').mockImplementation(async (filters) => {
      invalidated.push((filters?.queryKey ?? []) as string[])
    })
    qc.setQueryData<MessagePage>(qk.chatMessages(CONV), {
      items: [msg('m-2'), msg('m-1')],
      next_cursor: null,
      has_more: false,
    })
    qc.setQueryData<ConversationPage>(qk.chatConversations(), {
      items: [row('m-2')],
      next_cursor: null,
      trashed_count: 0,
    })
  })

  const listRefetched = () =>
    invalidated.some((k) => k.join('/') === qk.chatConversations().join('/'))

  it('refetches the list when the previewed message was edited', () => {
    applyChatFrame(qc, updated(msg('m-2', '2026-08-27T11:00:00.000Z')))
    expect(listRefetched()).toBe(true)
  })

  // ⚠ THE ONE THAT COSTS. A reaction publishes the same frame type on the same
  // message, and it changes nothing the row draws.
  it('does not refetch the list when the frame is only a reaction', () => {
    const reacted = msg('m-2')
    reacted.reactions = [{ emoji: '❤️', by: [{ user_id: 'u-kaja', label: 'Kája' }] }]
    applyChatFrame(qc, updated(reacted))
    expect(listRefetched()).toBe(false)
    // It still lands in the thread — that is the half the frame is for.
    const thread = qc.getQueryData<MessagePage>(qk.chatMessages(CONV))
    expect(thread?.items[0].reactions).toHaveLength(1)
  })

  it('does not refetch the list for an edit of a message the row is not previewing', () => {
    applyChatFrame(qc, updated(msg('m-1', '2026-08-27T11:00:00.000Z')))
    expect(listRefetched()).toBe(false)
  })

  // A delete keeps its unconditional refetch: it moves the unread badge too, which is
  // a number the row draws whether or not the message was the previewed one.
  it('always refetches the list on a delete', () => {
    applyChatFrame(qc, updated(msg('m-1'), 'chat_message.deleted'))
    expect(listRefetched()).toBe(true)
  })

  // ⚠ A FRAME FOR A MESSAGE THIS TAB HOLDS NO COPY OF cannot be told apart — there is
  // no `edited_at` to compare against — so it falls back to the row's own answer
  // rather than guessing that nothing moved.
  it('refetches when the previewed message is not in this tab’s thread', () => {
    qc.removeQueries({ queryKey: qk.chatMessages(CONV) })
    applyChatFrame(qc, updated(msg('m-2', '2026-08-27T11:00:00.000Z')))
    expect(listRefetched()).toBe(true)
  })
})
