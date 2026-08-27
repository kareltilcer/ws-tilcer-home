// Chat (v10) — the module's API surface (openapi.yaml 0.12.0).
//
// ⚠ THE ATTACHMENT, STORAGE, CLEAN-UP AND MOVE PATHS ARE ABSENT HERE ON PURPOSE.
// The served contract describes them because it is the whole v10 spec, and PR 3
// implements them. A client function for a route that 404s would be found by
// somebody calling it, not by reading the spec.

import { apiFetch } from '@/api/client'
import type {
  ChatMessage,
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

export function listConversations(state?: 'active' | 'trash'): Promise<ConversationPage> {
  return apiFetch(`${base}/conversations${qs({ state })}`)
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
