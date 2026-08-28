// Chat (v10) — the module's API surface (openapi.yaml 0.12.1).

import { apiFetch, apiUpload } from '@/api/client'
import type {
  Attachment,
  ChatMessage,
  ChatStorage,
  CleanupPage,
  Conversation,
  ConversationCreate,
  ConversationMemberList,
  ConversationPage,
  Directory,
  MessageCreate,
  MessagePage,
  ReadState,
  SearchPage,
} from './types'

const base = '/api/chat'

function qs(params: Record<string, string | number | undefined>): string {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '') p.set(k, String(v))
  }
  const s = p.toString()
  return s ? `?${s}` : ''
}

// ---- conversations ----

/**
 * ⚠ IT PASSES `limit` AND `cursor`, BECAUSE THE SERVER HONOURS BOTH. Store's
 * NormalizeLimit clamps an absent limit to 50 and returns a `next_cursor`; sending
 * neither and consuming neither made the 51st room unreachable — no row, no unread
 * badge, and no line of copy to say so. That is the shape the store refuses one
 * file away: page one dressed as the whole result.
 */
export function listConversations(
  state?: 'active' | 'trash',
  opts: { cursor?: string; limit?: number } = {},
): Promise<ConversationPage> {
  return apiFetch(`${base}/conversations${qs({ state, ...opts })}`)
}

export function getConversation(id: string): Promise<Conversation> {
  return apiFetch(`${base}/conversations/${id}`)
}

export function createConversation(body: ConversationCreate): Promise<Conversation> {
  return apiFetch(`${base}/conversations`, { method: 'POST', body })
}

export function renameConversation(id: string, name: string): Promise<Conversation> {
  return apiFetch(`${base}/conversations/${id}`, { method: 'PATCH', body: { name } })
}

/**
 * Move a conversation to the koš, or purge it.
 *
 * ⚠ `hard` exists so somebody deleting a heavy conversation TO FIX AN OVERRUN is
 * never told to come back in seven days (D253). The copy has to carry the
 * relationship: deleting frees the space in seven days, purging frees it now.
 */
export function deleteConversation(id: string, hard = false): Promise<void> {
  return apiFetch(`${base}/conversations/${id}${qs({ hard: hard ? 'true' : undefined })}`, {
    method: 'DELETE',
  })
}

/**
 * Restore a conversation from the koš.
 *
 * ⚠ Returns 204 with no body when an ADMIN who is not a member restores a room
 * (D255): the restore is a verb they have, the conversation is a read they do not.
 * `apiFetch` resolves that as undefined, which is why the return type is nullable.
 */
export function restoreConversation(id: string): Promise<Conversation | undefined> {
  return apiFetch(`${base}/conversations/${id}/restore`, { method: 'POST' })
}

// ---- membership ----

export function listMembers(id: string): Promise<ConversationMemberList> {
  return apiFetch(`${base}/conversations/${id}/members`)
}

export function addMember(id: string, userID: string): Promise<ConversationMemberList> {
  return apiFetch(`${base}/conversations/${id}/members`, {
    method: 'POST',
    body: { user_id: userID },
  })
}

/** Removing yourself is how leaving works — there is no separate route. */
export function removeMember(id: string, userID: string): Promise<void> {
  return apiFetch(`${base}/conversations/${id}/members/${userID}`, { method: 'DELETE' })
}

export function setMuted(id: string, muted: boolean): Promise<Conversation> {
  return apiFetch(`${base}/conversations/${id}/members/me`, { method: 'PATCH', body: { muted } })
}

// ---- messages ----

export function listMessages(
  id: string,
  opts: { cursor?: string; direction?: 'backward' | 'forward'; limit?: number } = {},
): Promise<MessagePage> {
  return apiFetch(`${base}/conversations/${id}/messages${qs(opts)}`)
}

export function sendMessage(id: string, body: MessageCreate): Promise<ChatMessage> {
  return apiFetch(`${base}/conversations/${id}/messages`, { method: 'POST', body })
}

export function editMessage(messageID: string, body: string): Promise<ChatMessage> {
  return apiFetch(`${base}/messages/${messageID}`, { method: 'PATCH', body: { body } })
}

export function deleteMessage(messageID: string): Promise<void> {
  return apiFetch(`${base}/messages/${messageID}`, { method: 'DELETE' })
}

/** Idempotent, and never backwards — a replayed older marker cannot un-read a room. */
export function advanceRead(id: string, untilMessageID: string): Promise<ReadState> {
  return apiFetch(`${base}/conversations/${id}/read`, {
    method: 'POST',
    body: { until_message_id: untilMessageID },
  })
}

// ---- search and directory ----

/**
 * ⚠ SINGLE PAGE, AND THERE IS NO CURSOR PARAMETER HERE. Results are ordered by
 * relevance and a keyset cursor is an id, which does not locate a position in a
 * rank ordering — the server 422s one rather than silently serving page one.
 */
export function searchMessages(q: string, conversationID?: string): Promise<SearchPage> {
  return apiFetch(`${base}/search${qs({ q, conversation_id: conversationID })}`)
}

export function getDirectory(): Promise<Directory> {
  return apiFetch(`${base}/directory`)
}

// ---- PR 3: attachments, storage, clean-up ----

/**
 * Send a message carrying files.
 *
 * ⚠ ONE REQUEST, NEVER AN UPLOAD-THEN-REFERENCE PAIR (D224). A two-step flow
 * orphans an object every time the second step does not happen, and chat has no
 * reconciliation pass to find one — Dokumenty has a mirror job that sweeps its
 * prefix and chat deliberately has neither.
 *
 * ⚠ AND IT GOES THROUGH apiUpload RATHER THAN apiFetch, because fetch has no upload
 * progress event and the composer shows a per-file progress row. The same reason
 * Dokumenty's queue does.
 */
export function sendMessageWithFiles(
  id: string,
  input: { body: string; replyToID?: string; files: File[] },
  opts: { onProgress?: (fraction: number) => void; signal?: AbortSignal } = {},
): Promise<ChatMessage> {
  const form = new FormData()
  if (input.body) form.set('body', input.body)
  if (input.replyToID) form.set('reply_to_id', input.replyToID)
  for (const file of input.files) form.append('files', file, file.name)
  return apiUpload(`${base}/conversations/${id}/messages`, form, opts)
}

/**
 * The URL a bubble renders an attachment from.
 *
 * ⚠ IT IS BUILT HERE AND NOWHERE ELSE, and it is a BACKEND path rather than a
 * presigned link: content is always served through the session (D33/D42), so every
 * view re-resolves membership. `?download=true` forces the attachment disposition;
 * there is no separate `/download` route because only one of the three kinds ever
 * needs one.
 */
export function attachmentURL(id: string, opts: { download?: boolean } = {}): string {
  return `${base}/attachments/${id}/raw${opts.download ? '?download=true' : ''}`
}

/** An image attachment's thumbnail. 404 for video and file kinds, which have none. */
export function thumbnailURL(id: string): string {
  return `${base}/attachments/${id}/thumbnail`
}

export function chatStorage(): Promise<ChatStorage> {
  return apiFetch(`${base}/storage`)
}

/**
 * ⚠ `sort=size` IS SINGLE-PAGE AND THE SERVER REFUSES A CURSOR WITH IT — a keyset
 * cursor is an id, and an id does not locate a position in a size ordering. The
 * screen says so rather than offering a Load-more that would not work.
 */
export function chatCleanup(
  opts: { conversationID?: string; sort?: 'size' | 'recent'; cursor?: string; limit?: number } = {},
): Promise<CleanupPage> {
  return apiFetch(
    `${base}/cleanup${qs({
      conversation_id: opts.conversationID,
      sort: opts.sort,
      cursor: opts.cursor,
      limit: opts.limit,
    })}`,
  )
}

/**
 * Odstranit — removes the bytes and keeps the epitaph (D243).
 *
 * ⚠ IT DELETES THE OBJECT INLINE server-side, which is what makes the figure fall
 * immediately. Every other destructive path in chat enqueues for a 15-minute drain.
 */
export function removeAttachment(id: string): Promise<void> {
  return apiFetch(`${base}/attachments/${id}`, { method: 'DELETE' })
}

/**
 * Přesunout do Dokumentů — a custody transfer, and ⚠ A PUBLISH (D245): the file
 * becomes readable by every household member, including people who are not in this
 * conversation. The dialog says so in words before this is called.
 */
export function moveAttachment(id: string, folderID: string): Promise<Attachment> {
  return apiFetch(`${base}/attachments/${id}/move`, {
    method: 'POST',
    body: { folder_id: folderID },
  })
}
