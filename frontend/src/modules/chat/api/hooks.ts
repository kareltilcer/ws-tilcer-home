import { useEffect } from 'react'
import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query'
import { qk } from '@/api/keys'
import { subscribeToFrames, type LiveFrame } from '@/api/ws'
import { clientId } from '@/api/clientId'
import { useOnline } from '@/platform/pwa/offline'
import * as api from './endpoints'
import type {
  ChatMembershipEvent,
  ChatMessage,
  ChatMessageEvent,
  Conversation,
  MessagePage,
} from './types'

// Chat's data layer.
//
// ⚠ EVERY KEY CARRIES THE CONVERSATION ID (api/keys.ts). A key shared across two
// rooms is the single most likely bug in this module — it looks fine until somebody
// switches conversations and TanStack serves the other thread from cache, which here
// means other people's messages under a heading that names yours.
//
// ⚠ AND NOTHING IS FETCHED OFFLINE. Chat is excluded from the PWA persister, so
// there is no cached thread to fall back on and a query firing offline would only
// produce a spinner that never resolves. The route renders an offline state instead.

export function useConversations(state?: 'active' | 'trash') {
  const online = useOnline()
  return useQuery({
    queryKey: qk.chatConversations(state),
    queryFn: () => api.listConversations(state),
    enabled: online,
  })
}

export function useConversation(id: string | undefined) {
  const online = useOnline()
  return useQuery({
    queryKey: qk.chatConversation(id ?? ''),
    queryFn: () => api.getConversation(id as string),
    enabled: online && !!id,
  })
}

export function useMessages(id: string | undefined) {
  const online = useOnline()
  return useQuery({
    queryKey: qk.chatMessages(id ?? ''),
    queryFn: () => api.listMessages(id as string, { limit: 50 }),
    enabled: online && !!id,
  })
}

export function useMembers(id: string | undefined) {
  const online = useOnline()
  return useQuery({
    queryKey: qk.chatMembers(id ?? ''),
    queryFn: () => api.listMembers(id as string),
    enabled: online && !!id,
  })
}

export function useDirectory(enabled = true) {
  const online = useOnline()
  return useQuery({
    queryKey: qk.chatDirectory,
    queryFn: api.getDirectory,
    enabled: online && enabled,
  })
}

export function useChatSearch(q: string, conversationID?: string) {
  const online = useOnline()
  return useQuery({
    queryKey: qk.chatSearch(q, conversationID),
    queryFn: () => api.searchMessages(q, conversationID),
    enabled: online && q.trim().length > 0,
  })
}

// ---- mutations ----

export function useSendMessage(conversationID: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { body: string; replyToID?: string }) =>
      api.sendMessage(conversationID, { body: input.body, reply_to_id: input.replyToID ?? null }),
    onSuccess: (msg) => {
      // The message is applied straight into the thread rather than refetched: the
      // response IS the message, and the /ws echo carries our own client id so it
      // will be ignored (see useChatLiveSync).
      appendMessage(qc, conversationID, msg)
      void qc.invalidateQueries({ queryKey: qk.chatConversations() })
    },
  })
}

export function useEditMessage(conversationID: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; body: string }) => api.editMessage(input.id, input.body),
    onSuccess: (msg) => replaceMessage(qc, conversationID, msg),
  })
}

export function useDeleteMessage(conversationID: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.deleteMessage(id),
    // ⚠ Refetch rather than patch. A delete turns a message into a TOMBSTONE —
    // the row survives with an empty body (D223) — and reconstructing that shape
    // client-side would be a second, drifting definition of what a tombstone is.
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.chatMessages(conversationID) })
    },
  })
}

export function useCreateConversation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: api.createConversation,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.chatAll })
    },
  })
}

export function useRenameConversation(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => api.renameConversation(id, name),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.chatAll })
    },
  })
}

export function useDeleteConversation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; hard?: boolean }) =>
      api.deleteConversation(input.id, input.hard),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.chatAll })
    },
  })
}

export function useRestoreConversation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.restoreConversation(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.chatAll })
    },
  })
}

export function useAddMember(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (userID: string) => api.addMember(id, userID),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.chatMembers(id) })
      void qc.invalidateQueries({ queryKey: qk.chatConversation(id) })
    },
  })
}

export function useRemoveMember(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (userID: string) => api.removeMember(id, userID),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.chatAll })
    },
  })
}

export function useSetMuted(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (muted: boolean) => api.setMuted(id, muted),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.chatConversation(id) })
      void qc.invalidateQueries({ queryKey: qk.chatConversations() })
    },
  })
}

export function useAdvanceRead(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (untilMessageID: string) => api.advanceRead(id, untilMessageID),
    // ⚠ PATCHED, NOT INVALIDATED. The response already carries the new unread count,
    // and this fires on every message that arrives in an open thread — a refetch
    // here is one request per message per tab for a number the server just handed
    // us. The list is invalidated because the badge lives there and the response
    // says nothing about the other rooms.
    onSuccess: (state) => {
      qc.setQueryData<Conversation>(qk.chatConversation(id), (old) =>
        old ? { ...old, unread_count: state.unread_count } : old,
      )
      void qc.invalidateQueries({ queryKey: qk.chatConversations() })
    },
  })
}

// ---- live sync, and the gap check ----

/**
 * applyChatFrame is the whole live-sync rule, as a pure-ish function over a
 * QueryClient — exported so the gap check can be TESTED rather than eyeballed.
 *
 * ⚠ THIS IS THE ONE MODULE THAT APPLIES ITS PAYLOAD INSTEAD OF INVALIDATING. The
 * frame carries the message, so a refetch would ask the server for what it just
 * pushed — on a busy thread, one request per message per open tab.
 *
 * ⚠ THE GAP CHECK IS ONE-SHOT PER RECEIVED MESSAGE, AND THAT IS WHAT MAKES IT
 * TERMINATE (D259). The hub drops a frame on a saturated socket by design and there
 * is no replay. Every other module is fine with that — a dropped "something changed"
 * is repaired by refetch-on-focus — but a chat message's loss IS the content, in a
 * thread somebody is reading, with nothing on screen to say so.
 *
 * So each payload carries `prev_message_id`, computed ONCE for the whole audience. A
 * member whose floor sits above it can never hold it, so their FIRST message after
 * joining always looks like a gap: one refetch, and from then on it matches.
 *
 * ⚠ WHAT THIS TAB HOLDS IS READ FROM THE CACHE, NOT FROM A SEPARATE MAP. The first
 * implementation kept its own "newest message id" beside the cache and seeded it
 * from the thread query — and the two drifted the moment a message was appended,
 * because the seed re-ran on every cache mutation and moved the marker the frame was
 * about to be compared against. One source of truth removes the whole class.
 *
 * ⚠ AND OUR OWN ECHO IS NEVER GAP-CHECKED. A frame we caused cannot be evidence
 * that we missed one, and by the time it arrives our own message is already in the
 * cache — so its `prev` legitimately names the message before ours, which is no
 * longer what we hold.
 */
export function applyChatFrame(qc: QueryClient, frame: LiveFrame): void {
  if (frame.type === 'chat_membership.changed') {
    const ev = frame.payload as ChatMembershipEvent | undefined
    if (!ev) return
    // The one chat frame that invalidates rather than patches: the caller may have
    // just LOST the conversation, and the right next state is whatever the server
    // now says — which for a removed member is the 404 the route renders as "this
    // conversation is no longer yours".
    void qc.invalidateQueries({ queryKey: qk.chatAll })
    return
  }

  const ev = frame.payload as ChatMessageEvent | undefined
  if (!ev?.message) return
  const convID = ev.conversation_id

  if (frame.type === 'chat_message.created') {
    const mine = frame.origin === clientId
    const prev = ev.prev_message_id ?? ''
    const have = newestInCache(qc, convID)
    // A gap: this tab's newest is not the message the server says came before this
    // one, so at least one frame never arrived. Refetch the tail and RETURN without
    // re-comparing — re-checking after our own repair is what would loop forever.
    if (!mine && prev !== '' && prev !== have) {
      void qc.invalidateQueries({ queryKey: qk.chatMessages(convID) })
      void qc.invalidateQueries({ queryKey: qk.chatConversations() })
      return
    }
    appendMessage(qc, convID, ev.message)
    // ⚠ Not for our own echo: the send that caused it already invalidated the list,
    // and the read marker is about to as well. Three refetches of the same list per
    // message sent is what this line costs if it is unconditional.
    if (!mine) void qc.invalidateQueries({ queryKey: qk.chatConversations() })
    return
  }

  if (frame.type === 'chat_message.updated' || frame.type === 'chat_message.deleted') {
    // Neither extends the thread, so neither carries a prev_message_id and neither
    // can represent a gap.
    replaceMessage(qc, convID, ev.message)
  }
}

/** useChatLiveSync subscribes the chat caches to /ws for as long as the route lives. */
export function useChatLiveSync(): void {
  const qc = useQueryClient()
  useEffect(() => subscribeToFrames((frame) => applyChatFrame(qc, frame)), [qc])
}

/** newestInCache is what this tab currently holds for a conversation, or ''. */
function newestInCache(qc: QueryClient, conversationID: string): string {
  const page = qc.getQueryData<MessagePage>(qk.chatMessages(conversationID))
  return page?.items?.[0]?.id ?? ''
}

// ---- cache surgery ----

/**
 * appendMessage inserts one message at the head of a thread page.
 *
 * ⚠ IDEMPOTENT BY ID. A message arrives twice on the normal path — once as the
 * mutation's response, once as this tab's own /ws echo — and a naive push would
 * render it twice. Being idempotent here is cheaper than suppressing the echo,
 * which would also have to be right about every future path that publishes.
 *
 * The thread is stored newest-first (`backward` is the default direction), so the
 * newest message goes at index 0.
 */
function appendMessage(qc: QueryClient, conversationID: string, msg: ChatMessage): void {
  qc.setQueryData<MessagePage>(qk.chatMessages(conversationID), (old) => {
    if (!old) return old
    if (old.items.some((m) => m.id === msg.id)) return old
    return { ...old, items: [msg, ...old.items] }
  })
}

function replaceMessage(qc: QueryClient, conversationID: string, msg: ChatMessage): void {
  qc.setQueryData<MessagePage>(qk.chatMessages(conversationID), (old) => {
    if (!old) return old
    return { ...old, items: old.items.map((m) => (m.id === msg.id ? msg : m)) }
  })
}

/** unreadTotal is the nav badge's number: every conversation's unread, summed. */
export function unreadTotal(conversations: Conversation[] | undefined): number {
  if (!conversations) return 0
  return conversations.reduce((sum, c) => sum + c.unread_count, 0)
}
