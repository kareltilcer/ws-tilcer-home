// Dokumenty / documents (v4) — the module's API surface (openapi.yaml).

import { apiFetch, apiUpload } from '@/api/client'
import { qs } from '@/api/qs'
import type {
  DocFolder,
  DocFolderDetail,
  DocResolveResult,
  DocumentDetail,
  DocumentPage,
  DocumentsTree,
} from './types'
import type { PinScope, PinState, PublishRequest, Scope } from '@/api/types'

//
// Two things to know when calling these:
//   1. There is no "replace the file" call, by design — the bytes are immutable, so
//      a changed file is a NEW document (uploadDocument again).
//   2. The permanent link to a document is the id-based `urls` block on the detail
//      response, NOT the slug path. Copy `urls.permalink` ("/d/{id}"), which
//      survives renames and moves; the slug path does not.
//   3. (v9) Every read takes a SCOPE — see the note above getNotesTree for why it
//      is required here rather than defaulted.
// ---- Documents (Dokumenty, v4) ----
//
// Two things to know when calling these:
//   1. There is no "replace the file" call, by design — the bytes are immutable, so
//      a changed file is a NEW document (uploadDocument again).
//   2. The permanent link to a document is the id-based `urls` block on the detail
//      response, NOT the slug path. Copy `urls.permalink` ("/d/{id}"), which
//      survives renames and moves; the slug path does not.
//   3. (v9) Every read takes a SCOPE — see the note above getNotesTree for why it
//      is required here rather than defaulted.
export const getDocumentsTree = (scope: Scope, includeArchived = false) =>
  apiFetch<DocumentsTree>(`/api/documents/tree${qs({ scope, include_archived: includeArchived })}`)

export const resolveDocumentPath = (path: string, scope: Scope) =>
  apiFetch<DocResolveResult>(`/api/documents/resolve${qs({ path, scope })}`)

export const searchDocuments = (q: string, scope: Scope) =>
  apiFetch<DocumentPage>(`/api/documents${qs({ q, scope })}`)

export const listDocuments = (
  params: {
    folder_id?: string
    include_archived?: boolean
    limit?: number
    cursor?: string
    scope?: Scope
  } = {},
) => apiFetch<DocumentPage>(`/api/documents${qs(params)}`)

export const getDocument = (id: string) => apiFetch<DocumentDetail>(`/api/documents/${encodeURIComponent(id)}`)

/** uploadDocument streams one file to the documents bucket.
 *
 *  Field order is significant: the backend reads the multipart parts in order and
 *  applies the text fields it has seen by the time the file part starts (it never
 *  buffers a 50 MB body). So the metadata MUST be appended before the file. */
export const uploadDocument = (
  file: File,
  meta: {
    folder_id?: string | null
    title?: string
    description?: string
    /** v9: which root to upload into when folder_id is absent. */
    scope?: Scope
  } = {},
  onProgress?: (fraction: number) => void,
) => {
  const form = new FormData()
  if (meta.folder_id) form.append('folder_id', meta.folder_id)
  if (meta.title) form.append('title', meta.title)
  if (meta.description) form.append('description', meta.description)
  if (meta.scope) form.append('scope', meta.scope)
  form.append('file', file, file.name) // last, on purpose — see above
  return apiUpload<DocumentDetail>('/api/documents', form, { onProgress })
}

// PATCH is metadata only: title (re-derives the slug), description, archived.
export const updateDocument = (
  id: string,
  body: Partial<{ title: string; description: string | null; archived: boolean }>,
  via?: string,
) => apiFetch<DocumentDetail>(`/api/documents/${encodeURIComponent(id)}${qs({ via })}`, { method: 'PATCH', body })

// hard=true purges the row AND the stored file; it is admin-only server-side.
export const deleteDocument = (id: string, hard = false) =>
  apiFetch<void>(`/api/documents/${encodeURIComponent(id)}${qs({ hard })}`, { method: 'DELETE' })

export const moveDocument = (id: string, body: { folder_id?: string | null; position: string }, via?: string) =>
  apiFetch<DocumentDetail>(`/api/documents/${encodeURIComponent(id)}/move${qs({ via })}`, { method: 'POST', body })

export const pinDocument = (id: string, scope: PinScope, via?: string) =>
  apiFetch<PinState>(`/api/documents/${encodeURIComponent(id)}/pin${qs({ via })}`, { method: 'POST', body: { scope } })

export const unpinDocument = (id: string, scope: PinScope, via?: string) =>
  apiFetch<PinState>(`/api/documents/${encodeURIComponent(id)}/pin${qs({ scope, via })}`, { method: 'DELETE' })

export const createDocumentFolder = (body: {
  name: string
  parent_id?: string | null
  icon?: string
  scope?: Scope
}) => apiFetch<DocFolderDetail>('/api/documents/folders', { method: 'POST', body })

export const updateDocumentFolder = (id: string, body: Partial<{ name: string; archived: boolean; icon: string }>) =>
  apiFetch<DocFolderDetail>(`/api/documents/folders/${encodeURIComponent(id)}`, { method: 'PATCH', body })

export const deleteDocumentFolder = (id: string, opts: { cascade?: boolean; hard?: boolean } = {}) =>
  apiFetch<void>(`/api/documents/folders/${encodeURIComponent(id)}${qs(opts)}`, { method: 'DELETE' })

export const moveDocumentFolder = (id: string, body: { parent_id?: string | null; position: string }) =>
  apiFetch<DocFolder>(`/api/documents/folders/${encodeURIComponent(id)}/move`, { method: 'POST', body })

/** documentContentUrl builds a content URL for an <img>/<iframe>/anchor target.
 *  These are permanent and household-only: they carry the session cookie and are
 *  never presigned storage links. */
export const documentContentUrl = (id: string, kind: 'raw' | 'download' | 'preview' | 'thumbnail') =>
  `/api/documents/${encodeURIComponent(id)}/${kind}`

export const publishDocument = (id: string, body: PublishRequest = {}) =>
  apiFetch<DocumentDetail>(`/api/documents/${encodeURIComponent(id)}/publish`, {
    method: 'POST',
    body,
  })

export const publishDocumentFolder = (id: string, body: PublishRequest = {}) =>
  apiFetch<DocFolderDetail>(`/api/documents/folders/${encodeURIComponent(id)}/publish`, {
    method: 'POST',
    body,
  })
