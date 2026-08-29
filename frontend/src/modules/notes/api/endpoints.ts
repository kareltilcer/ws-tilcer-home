// Poznámky / notes (v3, scoped in v9) — the module's API surface (openapi.yaml).

import { apiFetch, apiUpload } from '@/api/client'
import { qs } from '@/api/qs'
import type {
  Folder,
  FolderDetail,
  NoteDetail,
  NoteImageUploadResult,
  NotePage,
  NotesTree,
  ResolveResult,
} from './types'
import type { PinScope, PinState, PublishRequest, Scope } from '@/api/types'

//
// ⚠ Every notes read takes a SCOPE, and it is a REQUIRED argument rather than one
// defaulting to `shared` here. The WIRE default is `shared` — that is what keeps a
// pre-v9 client working untouched — but a caller inside this app always knows
// which root the user is standing in, and letting it omit the scope is exactly how
// a page ends up reading the household tree while the switcher says otherwise.
// ---- Notes (Poznámky, v3; scoped in v9) ----
//
// ⚠ Every notes read takes a SCOPE, and it is a REQUIRED argument rather than one
// defaulting to `shared` here. The WIRE default is `shared` — that is what keeps a
// pre-v9 client working untouched — but a caller inside this app always knows
// which root the user is standing in, and letting it omit the scope is exactly how
// a page ends up reading the household tree while the switcher says otherwise.
export const getNotesTree = (scope: Scope, includeArchived = false) =>
  apiFetch<NotesTree>(`/api/notes/tree${qs({ scope, include_archived: includeArchived })}`)

export const resolveNotePath = (path: string, scope: Scope) =>
  apiFetch<ResolveResult>(`/api/notes/resolve${qs({ path, scope })}`)

export const searchNotes = (q: string, scope: Scope) =>
  apiFetch<NotePage>(`/api/notes${qs({ q, scope })}`)

export const getNote = (id: string) => apiFetch<NoteDetail>(`/api/notes/${encodeURIComponent(id)}`)

export const createNote = (body: {
  title: string
  folder_id?: string | null
  body_md?: string
  /**
   * Selects the root when folder_id is null. With a parent folder the PARENT's
   * scope governs and a disagreement is a 422 — a folder whose contents are half
   * private is exactly the model D177 rejected.
   */
  scope?: Scope
}) => apiFetch<NoteDetail>('/api/notes', { method: 'POST', body })

export const updateNote = (
  id: string,
  body: Partial<{ title: string; body_md: string | null; archived: boolean }>,
  via?: string,
) => apiFetch<NoteDetail>(`/api/notes/${encodeURIComponent(id)}${qs({ via })}`, { method: 'PATCH', body })

export const deleteNote = (id: string, hard = false) =>
  apiFetch<void>(`/api/notes/${encodeURIComponent(id)}${qs({ hard })}`, { method: 'DELETE' })

export const moveNote = (id: string, body: { folder_id?: string | null; position: string }) =>
  apiFetch<NoteDetail>(`/api/notes/${encodeURIComponent(id)}/move`, { method: 'POST', body })

export const pinNote = (id: string, scope: PinScope, via?: string) =>
  apiFetch<PinState>(`/api/notes/${encodeURIComponent(id)}/pin${qs({ via })}`, { method: 'POST', body: { scope } })

export const unpinNote = (id: string, scope: PinScope, via?: string) =>
  apiFetch<PinState>(`/api/notes/${encodeURIComponent(id)}/pin${qs({ scope, via })}`, { method: 'DELETE' })

export const createFolder = (body: {
  name: string
  parent_id?: string | null
  icon?: string
  scope?: Scope
}) => apiFetch<FolderDetail>('/api/notes/folders', { method: 'POST', body })

export const updateFolder = (id: string, body: Partial<{ name: string; archived: boolean; icon: string }>) =>
  apiFetch<FolderDetail>(`/api/notes/folders/${encodeURIComponent(id)}`, { method: 'PATCH', body })

export const deleteFolder = (id: string, opts: { cascade?: boolean; hard?: boolean } = {}) =>
  apiFetch<void>(`/api/notes/folders/${encodeURIComponent(id)}${qs(opts)}`, { method: 'DELETE' })

export const moveFolder = (id: string, body: { parent_id?: string | null; position: string }) =>
  apiFetch<Folder>(`/api/notes/folders/${encodeURIComponent(id)}/move`, { method: 'POST', body })

// noteImageUploadTimeoutMs bounds a single image upload. Without it, a request that
// never settles (a stalled connection) would leave the editor's image node stuck on
// its data:/blob: src forever — which suppresses every later autosave emission and
// silently freezes the note for the whole session. A hard deadline makes a hung upload
// reject instead, so the caller can drop the node and surface an error. Generous
// headroom for the 10 MB cap on a slow link (real pasted images are far smaller).
const noteImageUploadTimeoutMs = 120_000

/** uploadNoteImage streams one pasted/dropped image to object storage and returns
 *  the reference URL the editor embeds as `![](url)`. Keeps body_md small — the
 *  bytes never travel inline. Aborts after noteImageUploadTimeoutMs so a stuck
 *  upload can never wedge the editor's autosave. */
export const uploadNoteImage = (noteId: string, file: File | Blob, filename = 'image') => {
  const form = new FormData()
  form.append('file', file, filename)
  const abort = new AbortController()
  const timer = setTimeout(() => abort.abort(), noteImageUploadTimeoutMs)
  return apiUpload<NoteImageUploadResult>(`/api/notes/${encodeURIComponent(noteId)}/images`, form, {
    signal: abort.signal,
  }).finally(() => clearTimeout(timer))
}

//
// ⚠ ONE-WAY AND OWNER-ONLY (D182). There is deliberately no `unpublishNote` below
// and there is no route behind one: a note the household has relied on for months
// must not be able to vanish into one member's tree. Somebody who wants it back
// re-creates it privately and deletes the shared copy, which leaves both facts in
// the audit log.
//
// ⚠ A non-owner — an ADMIN INCLUDED — gets 404, not 403 (D206). The UI must never
// render "you may not publish this" for a foreign item: the ordinary nenalezeno
// screen is the whole answer, because a permission message is itself the
// disclosure.
// ---- v9: publish (private → shared) ----
//
// ⚠ ONE-WAY AND OWNER-ONLY (D182). There is deliberately no `unpublishNote` below
// and there is no route behind one: a note the household has relied on for months
// must not be able to vanish into one member's tree. Somebody who wants it back
// re-creates it privately and deletes the shared copy, which leaves both facts in
// the audit log.
//
// ⚠ A non-owner — an ADMIN INCLUDED — gets 404, not 403 (D206). The UI must never
// render "you may not publish this" for a foreign item: the ordinary nenalezeno
// screen is the whole answer, because a permission message is itself the
// disclosure.
export const publishNote = (id: string, body: PublishRequest = {}) =>
  apiFetch<NoteDetail>(`/api/notes/${encodeURIComponent(id)}/publish`, { method: 'POST', body })

export const publishNoteFolder = (id: string, body: PublishRequest = {}) =>
  apiFetch<FolderDetail>(`/api/notes/folders/${encodeURIComponent(id)}/publish`, {
    method: 'POST',
    body,
  })
