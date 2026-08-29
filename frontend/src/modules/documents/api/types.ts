// Dokumenty / documents (v4) — the module's wire types, mirroring the Go backend
// JSON (openapi.yaml).
//
// Documents live in their OWN folder tree (DocFolder), isolated from Poznámky's
// folders. Two properties shape the client code:
//   - The bytes are immutable: there is no "update content" call, and a changed
//     file is a new document.
//   - The permanent link is the ID-based `urls` block, not the slug path. The slug
//     path is navigation only and 404s after a rename or move.

import type { PathSegment, PinState, PreviewKind, PreviewStatus, Visibility } from '@/api/types'

export interface DocFolder {
  id: string
  parent_id: string | null
  name: string
  slug: string
  /** Optional emoji icon; empty string means "render the 📁 default". */
  icon: string
  position: string
  archived: boolean
  visibility: Visibility
  owner_id: string | null
  created_by: string | null
  created_at: string
  updated_at: string
}

// Permanent, household-only, id-based content URLs. Stable for the document's
// life; unaffected by rename/move. All require the session cookie.
export interface DocumentUrls {
  permalink: string
  raw: string
  download: string
  preview: string
  thumbnail: string
}

export interface DocumentItem {
  id: string
  folder_id: string | null
  title: string
  slug: string
  description: string | null
  original_filename: string
  content_type: string
  byte_size: number
  checksum: string
  preview_kind: PreviewKind
  preview_status: PreviewStatus
  position: string
  archived: boolean
  visibility: Visibility
  owner_id: string | null
  created_by: string | null
  created_at: string
  updated_at: string
  /**
   * ⚠ UNCHANGED BY A PUBLISH (D42/D182). The R2 keys are id-based and independent
   * of folder, slug and scope, so a document keeps the exact URL it was shared
   * with when it moves from a private root into the household tree.
   */
  urls: DocumentUrls
}

// Lightweight document node for the tree/list/grid.
export interface DocumentSummary {
  id: string
  folder_id: string | null
  title: string
  slug: string
  content_type: string
  byte_size: number
  preview_kind: PreviewKind
  preview_status: PreviewStatus
  position: string
  archived: boolean
  visibility: Visibility
  owner_id: string | null
  updated_at: string
  thumbnail_url: string | null
  pinned: PinState
}

export interface DocumentDetail extends DocumentItem {
  path: PathSegment[]
  slug_path: string
  pinned: PinState
}

export interface DocFolderDetail extends DocFolder {
  path: PathSegment[]
  slug_path: string
  subfolders: DocFolder[]
  documents: DocumentSummary[]
}

// Recursive node of GET /api/documents/tree.
export interface DocFolderNode {
  folder: DocFolder
  subfolders: DocFolderNode[]
  documents: DocumentSummary[]
}

export interface DocumentsTree {
  roots: DocFolderNode[]
  root_documents: DocumentSummary[]
  /** Server's per-file upload cap (HOME_DOCS_MAX_UPLOAD_MB), for the client pre-check. */
  max_upload_mb?: number
}

export interface DocumentPage {
  items: DocumentSummary[]
  next_cursor: string | null
}

export interface DocResolveResult {
  type: 'folder' | 'document'
  id: string
  slug_path: string
}
