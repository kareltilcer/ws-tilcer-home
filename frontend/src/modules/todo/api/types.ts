// Úkoly / todo — the module's wire types, mirroring the Go backend JSON
// (openapi.yaml). Split out of the shared src/api/types.ts, which had held eight
// modules' shapes in one 1,087-line file.

import type { ChecklistProgress } from '@/api/types'

export type ColumnKind = 'normal' | 'now' | 'done'

export interface Board {
  id: string
  name: string
  description: string | null
  position: string
  archived: boolean
  created_by: string | null
  created_at: string
}

export interface Column {
  id: string
  board_id: string
  name: string
  priority: number
  position: string
  kind: ColumnKind
  created_at: string
}

export interface Card {
  id: string
  column_id: string
  title: string
  notes: string | null
  position: string
  archived: boolean
  done_at: string | null
  label_ids: string[]
  checklist_progress: ChecklistProgress
  link_count: number
  created_by: string | null
  created_at: string
  updated_at: string
}

export interface CardLink {
  id: string
  card_id: string
  url: string
  title: string | null
  position: string
}

export interface ChecklistItem {
  id: string
  card_id: string
  text: string
  done: boolean
  position: string
}

export interface Label {
  id: string
  board_id: string
  name: string
  color: string
  card_count?: number
}

export interface CardDetail extends Card {
  links: CardLink[]
  checklist: ChecklistItem[]
  labels: Label[]
}

export interface BoardTreeColumn {
  column: Column
  cards: Card[]
  card_count: number // total cards in the column, ignoring tree filters (matches the delete-cascade count)
}

export interface BoardTree {
  board: Board
  columns: BoardTreeColumn[]
}
