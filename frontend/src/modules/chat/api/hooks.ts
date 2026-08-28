import { useEffect, useState } from 'react'
import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query'
import { toast } from 'sonner'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { ApiError } from '@/api/client'
import { subscribeToFrames, type LiveFrame } from '@/api/ws'
import { clientId } from '@/api/clientId'
import { useOnline } from '@/platform/pwa/offline'
import * as api from './endpoints'
import type {
  ChatMembershipEvent,
  ChatMessage,
  ChatMessageEvent,
  Conversation,
  ConversationEvent,
  ConversationPage,
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

/** CONVERSATION_PAGE is one page of a listing, and the floor of every refetch. */
const CONVERSATION_PAGE = 50
/** The server clamps `limit` at 200 (NormalizeLimit), so asking for more is a lie. */
const CONVERSATION_MAX = 200

/**
 * `enabled` is how the koš section pays for itself only when it is opened — the key
 * still carries the state, so the two listings never share a cache entry.
 *
 * ⚠ A REFETCH RE-REQUESTS WHAT THIS TAB IS HOLDING, exactly as useMessages does and
 * for the same reason: `Načíst další` grows the cached page in place, and a plain
 * one-page queryFn would throw that away on every invalidation — which here is every
 * send, every read-marker advance and every membership frame.
 */
export function useConversations(state?: 'active' | 'trash', enabled = true) {
  const online = useOnline()
  const qc = useQueryClient()
  return useQuery({
    queryKey: qk.chatConversations(state),
    queryFn: () => {
      const held =
        qc.getQueryData<ConversationPage>(qk.chatConversations(state))?.items.length ?? 0
      return api.listConversations(state, {
        limit: Math.min(Math.max(CONVERSATION_PAGE, held), CONVERSATION_MAX),
      })
    },
    enabled: online && enabled,
  })
}

/**
 * useLoadMoreConversations walks a listing's keyset, one page at a time.
 *
 * ⚠ WITHOUT IT THE LIST SIMPLY STOPPED AT FIFTY. The store clamps `limit` and
 * returns a `next_cursor` that nothing consumed, so the 51st room could not be
 * opened and its unread badge never appeared. The thread grew `Načíst starší` for
 * this exact reason; the list is the same defect one pane over.
 */
export function useLoadMoreConversations(state?: 'active' | 'trash') {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      const page = qc.getQueryData<ConversationPage>(qk.chatConversations(state))
      if (!page?.next_cursor) return null
      return api.listConversations(state, {
        cursor: page.next_cursor,
        limit: CONVERSATION_PAGE,
      })
    },
    onError: failed(cs.chat.actionFailed),
    onSuccess: (more) => {
      if (!more) return
      qc.setQueryData<ConversationPage>(qk.chatConversations(state), (old) => {
        if (!old) return old
        // Idempotent by id, for the same reason appendMessage is: a page fetched
        // twice (a double click, a retry) must not double the list.
        const seen = new Set(old.items.map((c) => c.id))
        return {
          items: [...old.items, ...more.items.filter((c) => !seen.has(c.id))],
          next_cursor: more.next_cursor,
        }
      })
    },
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

/** THREAD_PAGE is one page of the thread, and the floor of every refetch. */
const THREAD_PAGE = 50
/** The server clamps `limit` at 200 (NormalizeLimit), so asking for more is a lie. */
const THREAD_MAX = 200

/**
 * useMessages holds the thread.
 *
 * ⚠ A REFETCH RE-REQUESTS WHAT THIS TAB IS HOLDING, NOT ONE PAGE (v10 review).
 * `Načíst starší` grows the cached page in place, so a plain `limit: 50` queryFn
 * silently threw that away every time anything invalidated the key — a message
 * delete, a gap-check repair, or any of the mutations that touch the list. The
 * member watched two hundred messages collapse back to fifty and the scroll jump,
 * for an operation that had nothing to do with the history they had loaded.
 *
 * Reading the held count inside the queryFn is what keeps ONE cache entry: widening
 * the key by a page count would make every load-older a cache miss, which is the
 * cost this module split its key namespace to avoid. Beyond THREAD_MAX the server
 * will not answer in one page and the tail is dropped — a bound worth having in one
 * place rather than a partial restore nobody can predict.
 */
export function useMessages(id: string | undefined) {
  const online = useOnline()
  const qc = useQueryClient()
  return useQuery({
    queryKey: qk.chatMessages(id ?? ''),
    queryFn: () => {
      const held = qc.getQueryData<MessagePage>(qk.chatMessages(id as string))?.items.length ?? 0
      return api.listMessages(id as string, {
        limit: Math.min(Math.max(THREAD_PAGE, held), THREAD_MAX),
      })
    },
    enabled: online && !!id,
  })
}

/**
 * useLoadOlderMessages walks the thread backwards, one page at a time.
 *
 * ⚠ WITHOUT IT THE THREAD SIMPLY STOPPED AT FIFTY. The server has always answered
 * with `has_more` and a `next_cursor` and the spec has always documented the
 * backward/forward paging — nothing consumed either, so the newest fifty messages
 * were the only ones a conversation had, and the floor line at the top of the
 * thread explained the truncation as the membership floor. It was reachable by any
 * room that had been used for a week.
 *
 * ⚠ OLDER MESSAGES ARE APPENDED, NOT PREPENDED. The page is stored newest-first,
 * so the tail is where older goes — which is also what keeps `items[0]` the newest
 * message and therefore keeps the gap check (newestInCache) reading the same thing
 * it read before.
 */
export function useLoadOlderMessages(conversationID: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      const page = qc.getQueryData<MessagePage>(qk.chatMessages(conversationID))
      if (!page?.has_more || !page.next_cursor) return null
      return api.listMessages(conversationID, {
        cursor: page.next_cursor,
        direction: 'backward',
        limit: THREAD_PAGE,
      })
    },
    onError: failed(cs.chat.actionFailed),
    onSuccess: (older) => {
      if (!older) return
      qc.setQueryData<MessagePage>(qk.chatMessages(conversationID), (old) => {
        if (!old) return old
        // Idempotent by id, for the same reason appendMessage is: a page fetched
        // twice (a double click, a retry) must not double the thread.
        const seen = new Set(old.items.map((m) => m.id))
        return {
          items: [...old.items, ...older.items.filter((m) => !seen.has(m.id))],
          next_cursor: older.next_cursor,
          has_more: older.has_more,
        }
      })
    },
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

/** SEARCH_DEBOUNCE_MS is how long typing has to pause before a MATCH is worth running. */
const SEARCH_DEBOUNCE_MS = 250

/** useDebounced trails a value, so a fast typist produces one of it rather than eight. */
function useDebounced<T>(value: T, ms: number): T {
  const [settled, setSettled] = useState(value)
  useEffect(() => {
    const t = setTimeout(() => setSettled(value), ms)
    return () => clearTimeout(t)
  }, [value, ms])
  return settled
}

/**
 * useChatSearch runs the one MATCH that carries membership and the per-row floor.
 *
 * ⚠ THE QUERY IS DEBOUNCED, AND THE DEBOUNCE IS IN HERE (v10 review). Keyed on the
 * raw input, every keystroke was its own request: `dovolená` ran eight FTS5 matches
 * with eight snippet() passes joined against chat_members and chat_conversations,
 * seven of them for prefixes nobody asked for, each cached under its own key for the
 * rest of the session. It sits in the hook rather than in the input so no future
 * call site can reintroduce it by binding straight to a `value`.
 */
export function useChatSearch(q: string, conversationID?: string) {
  const online = useOnline()
  const settled = useDebounced(q.trim(), SEARCH_DEBOUNCE_MS)
  return useQuery({
    queryKey: qk.chatSearch(settled, conversationID),
    queryFn: () => api.searchMessages(settled, conversationID),
    enabled: online && settled.length > 0,
  })
}

// ---- mutations ----

/**
 * failed is the module's ONE mutation error handler.
 *
 * ⚠ EVERY WRITE IN THIS MODULE NEEDS ONE (v10 review). Not a single mutation here
 * had an `onError`, so a send that 404s — the room trashed by another member while
 * the composer was open — stopped its spinner, left the typed text in the box and
 * said nothing. The member reads that as "sent", presses Enter again, and gets the
 * same silence, in the one module whose whole point is that a message either
 * arrived or did not. Every other module in Home already does this
 * (`toast.error(cs.…saveFailed)` in electricity, garden, finance).
 *
 * The server's own detail wins when there is one, because it names WHICH rule
 * refused — `Konverzace nebyla nalezena.`, `Poslední člen nemůže konverzaci
 * opustit — smažte ji místo toho.` — and those sentences were written to be read.
 * The fallback is what a network failure leaves behind.
 */
function failed(fallback: string) {
  return (e: unknown) =>
    toast.error(e instanceof ApiError ? (e.detail ?? fallback) : fallback)
}

export function useSendMessage(conversationID: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { body: string; replyToID?: string }) =>
      api.sendMessage(conversationID, { body: input.body, reply_to_id: input.replyToID ?? null }),
    onError: failed(cs.chat.sendFailed),
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
    onError: failed(cs.chat.actionFailed),
    onSuccess: (msg) => replaceMessage(qc, conversationID, msg),
  })
}

export function useDeleteMessage(conversationID: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.deleteMessage(id),
    onError: failed(cs.chat.actionFailed),
    // ⚠ Refetch rather than patch. A delete turns a message into a TOMBSTONE —
    // the row survives with an empty body (D223) — and reconstructing that shape
    // client-side would be a second, drifting definition of what a tombstone is.
    //
    // ⚠ AND THE LIST GOES WITH IT (v10 review). UnreadCount excludes
    // `deleted_at IS NOT NULL`, so a delete LOWERS every other member's badge —
    // patch the thread and not the list and the row goes on counting a message
    // that is now a tombstone in the thread beside it.
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.chatMessages(conversationID) })
      void qc.invalidateQueries({ queryKey: qk.chatConversations() })
    },
  })
}

/**
 * invalidateLists marks BOTH conversation listings stale and nothing else.
 *
 * ⚠ IT EXISTS BECAUSE `qk.chatAll` WAS THE DEFAULT (v10 review). `['chat']` is a
 * prefix of every key in the module, so renaming one room refetched every open
 * thread, the directory and every search result the session had accumulated — and
 * each of those thread refetches also paid for the whole history the member had
 * loaded. The narrow form was already in this file (useSetMuted); it just was not
 * the habit. A room's name, its koš state and its existence all change what the two
 * lists say, and nothing more.
 */
function invalidateLists(qc: QueryClient): void {
  void qc.invalidateQueries({ queryKey: qk.chatConversations('active') })
  void qc.invalidateQueries({ queryKey: qk.chatConversations('trash') })
}

export function useCreateConversation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: api.createConversation,
    onError: failed(cs.chat.actionFailed),
    onSuccess: () => invalidateLists(qc),
  })
}

export function useRenameConversation(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => api.renameConversation(id, name),
    onError: failed(cs.chat.actionFailed),
    onSuccess: () => {
      // The header reads the name off the conversation, the rows read it off the
      // list. The thread is untouched — a rename does not change a message.
      void qc.invalidateQueries({ queryKey: qk.chatConversation(id) })
      invalidateLists(qc)
    },
  })
}

export function useDeleteConversation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; hard?: boolean }) =>
      api.deleteConversation(input.id, input.hard),
    onError: failed(cs.chat.actionFailed),
    onSuccess: (_data, input) => {
      void qc.invalidateQueries({ queryKey: qk.chatConversation(input.id) })
      invalidateLists(qc)
    },
  })
}

export function useRestoreConversation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.restoreConversation(id),
    onError: failed(cs.chat.actionFailed),
    onSuccess: (_data, id) => {
      void qc.invalidateQueries({ queryKey: qk.chatConversation(id) })
      invalidateLists(qc)
    },
  })
}

export function useAddMember(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (userID: string) => api.addMember(id, userID),
    onError: failed(cs.chat.actionFailed),
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
    onError: failed(cs.chat.actionFailed),
    // The panel, the member count on the room, and the lists — because removing
    // YOURSELF is how leaving works, and the room then leaves your list. Still not
    // the thread: the messages of somebody who left stay exactly where they were.
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.chatMembers(id) })
      void qc.invalidateQueries({ queryKey: qk.chatConversation(id) })
      invalidateLists(qc)
    },
  })
}

export function useSetMuted(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (muted: boolean) => api.setMuted(id, muted),
    onError: failed(cs.chat.actionFailed),
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
    // ⚠ THE ONE MUTATION HERE WITH NO TOAST, AND THAT IS DELIBERATE. It is
    // housekeeping the member never asked for — it fires on every message that
    // arrives in an open thread — so a banner on failure would be noise about
    // something nobody did. ThreadView retries it instead, by only recording the
    // marker once the request has actually landed.
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
  if (frame.type === 'chat_conversation.changed') {
    const ev = frame.payload as ConversationEvent | undefined
    if (!ev) return
    // ⚠ THE STRUCTURAL VERBS PUBLISH NOW (v10 review). A rename left every other
    // member's header naming the old room; a trash left their thread rendering and
    // their composer enabled over a room that had left every read, so their next
    // send answered 404 with nothing on screen to explain it.
    //
    // Invalidated, not patched: the frame deliberately carries no name, because a
    // name is member-scoped content. Each client refetches through the membership
    // join and gets whatever the access rule says it may have — which for a gone
    // room is the 404 the route renders as "this conversation is no longer yours".
    void qc.invalidateQueries({ queryKey: qk.chatConversation(ev.conversation_id) })
    invalidateLists(qc)
    if (ev.gone) {
      // Its thread is unreadable from here on, so drop what this tab holds rather
      // than refetching a page that can only 404.
      qc.removeQueries({ queryKey: qk.chatMessages(ev.conversation_id) })
    }
    return
  }

  if (frame.type === 'chat_membership.changed') {
    const ev = frame.payload as ChatMembershipEvent | undefined
    if (!ev) return
    // The one chat frame that invalidates rather than patches: the caller has just
    // GAINED or LOST the conversation, and the right next state is whatever the
    // server now says — which for a removed member is the 404 the route renders as
    // "this conversation is no longer yours", and for an added one is a room that
    // was not in their list a moment ago.
    //
    // ⚠ Three keys, not the `['chat']` prefix. The thread is never INVALIDATED:
    // a member who just lost the room must not refetch its messages, and one who
    // just gained it has no thread cached to refresh.
    void qc.invalidateQueries({ queryKey: qk.chatConversation(ev.conversation_id) })
    void qc.invalidateQueries({ queryKey: qk.chatMembers(ev.conversation_id) })
    invalidateLists(qc)
    if (ev.removed) {
      // ⚠ BUT A REMOVAL DROPS WHAT THIS TAB IS HOLDING (v10 review), the way the
      // `gone` branch above does and for a sharper reason. Not invalidating is not
      // the same as not keeping: the thread stayed in the cache with a live
      // observer, so a member removed and RE-ADDED an hour later — who now has a
      // NEW floor and may read none of it (D218) — had the whole pre-removal thread
      // rendered straight back at them the moment the conversation query succeeded,
      // and it stood until something happened to refocus the window. The permanent
      // gap the removal dialog promises was invisible.
      qc.removeQueries({ queryKey: qk.chatMessages(ev.conversation_id) })
    }
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
    // ⚠ BUT A DELETE MOVES THE BADGE (v10 review). UnreadCount excludes
    // `deleted_at IS NOT NULL`, so the server's count drops the moment this frame
    // is published — and patching only the thread left the row beside it counting
    // a message the recipient can now see is a tombstone. An EDIT changes no
    // count, which is why only this branch pays for the refetch.
    if (frame.type === 'chat_message.deleted') {
      void qc.invalidateQueries({ queryKey: qk.chatConversations() })
    }
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
