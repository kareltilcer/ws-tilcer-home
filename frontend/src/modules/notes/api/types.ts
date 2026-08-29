// Poznámky / notes (v3) — the module's wire types, mirroring the Go backend
// JSON (openapi.yaml).

import type { PathSegment, PinState, Visibility } from '@/api/types'

export interface Folder {
  id: string
  parent_id: string | null
  name: string
  slug: string
  /** Optional emoji icon; empty string means "render the 📁 default". */
  icon: string
  position: string
  archived: boolean
  visibility: Visibility
  /** The owning member when private; null when shared. */
  owner_id: string | null
  created_by: string | null
  created_at: string
  updated_at: string
}

export interface Note {
  id: string
  folder_id: string | null
  title: string
  slug: string
  body_md: string | null
  position: string
  archived: boolean
  visibility: Visibility
  owner_id: string | null
  created_by: string | null
  created_at: string
  updated_at: string
}

// Lightweight note node for the tree/list (no body).
export interface NoteSummary {
  id: string
  folder_id: string | null
  title: string
  slug: string
  position: string
  archived: boolean
  /** Drives the lock mark on tree rows, search hits and widget rows (D183). */
  visibility: Visibility
  owner_id: string | null
  updated_at: string
  pinned: PinState
}

export interface NoteDetail extends Note {
  path: PathSegment[]
  slug_path: string
  pinned: PinState
}

export interface FolderDetail extends Folder {
  path: PathSegment[]
  slug_path: string
  subfolders: Folder[]
  notes: NoteSummary[]
}

// Recursive node of GET /api/notes/tree.
export interface FolderNode {
  folder: Folder
  subfolders: FolderNode[]
  notes: NoteSummary[]
}

export interface NotesTree {
  roots: FolderNode[]
  root_notes: NoteSummary[]
}

// Search is capped at 100 and the folder listing is a full slice, so there is no
// cursor to page with.
export interface NotePage {
  items: NoteSummary[]
}

export interface ResolveResult {
  type: 'folder' | 'note'
  id: string
  slug_path: string
}

// POST /api/notes/{id}/images: a pasted/dropped image streamed to object storage.
// The editor embeds `url` as `![](url)`; the bytes never live inline in body_md.
export interface NoteImageUploadResult {
  id: string
  url: string
  content_type: string
  byte_size: number
}
