// Úkoly / todo — the module's API surface (openapi.yaml). Split out of the shared
// src/api/endpoints.ts, which had held eight modules' calls in one file.

import { apiFetch } from '@/api/client'
import { qs } from '@/api/qs'
import type {
  Board,
  BoardTree,
  Card,
  CardDetail,
  CardLink,
  ChecklistItem,
  Column,
  ColumnKind,
  Label,
} from './types'

// ---- Boards / tree ----
export const listBoards = () => apiFetch<Board[]>('/api/boards')

export const createBoard = (body: { name: string; description?: string }) =>
  apiFetch<Board>('/api/boards', { method: 'POST', body })

export const updateBoard = (id: string, body: Partial<{ name: string; description: string | null; archived: boolean }>) =>
  apiFetch<Board>(`/api/boards/${id}`, { method: 'PATCH', body })

export const deleteBoard = (id: string, hard = false) =>
  apiFetch<void>(`/api/boards/${id}${qs({ hard })}`, { method: 'DELETE' })

export const getBoardTree = (id: string, filters: { label?: string[]; q?: string; include_archived?: boolean } = {}) =>
  apiFetch<BoardTree>(`/api/boards/${id}/tree${qs(filters)}`)

// ---- Columns ----
export const listColumns = (boardId: string) => apiFetch<Column[]>(`/api/boards/${boardId}/columns`)

export const createColumn = (boardId: string, body: { name: string; priority?: number; kind?: ColumnKind }) =>
  apiFetch<Column>(`/api/boards/${boardId}/columns`, { method: 'POST', body })

export const updateColumn = (id: string, body: Partial<{ name: string; priority: number; kind: ColumnKind }>) =>
  apiFetch<Column>(`/api/columns/${id}`, { method: 'PATCH', body })

export const deleteColumn = (id: string, cascade = false) =>
  apiFetch<void>(`/api/columns/${id}${qs({ cascade })}`, { method: 'DELETE' })

export const moveColumn = (id: string, position: string) =>
  apiFetch<Column>(`/api/columns/${id}/move`, { method: 'POST', body: { position } })

// Atomic multi-column reorder — all positions rewritten in one transaction.
export const reorderColumns = (boardId: string, columns: { id: string; position: string }[]) =>
  apiFetch<Column[]>(`/api/boards/${boardId}/columns/reorder`, { method: 'POST', body: { columns } })

// ---- Cards ----
export const createCard = (columnId: string, body: { title: string; notes?: string }) =>
  apiFetch<Card>(`/api/columns/${columnId}/cards`, { method: 'POST', body })

export const getCard = (id: string) => apiFetch<CardDetail>(`/api/cards/${id}`)

export const updateCard = (id: string, body: Partial<{ title: string; notes: string | null; archived: boolean }>) =>
  apiFetch<Card>(`/api/cards/${id}`, { method: 'PATCH', body })

export const deleteCard = (id: string, hard = false) =>
  apiFetch<void>(`/api/cards/${id}${qs({ hard })}`, { method: 'DELETE' })

export const moveCard = (id: string, body: { column_id: string; position?: string; before_card_id?: string }, via?: string) =>
  apiFetch<Card>(`/api/cards/${id}/move${qs({ via })}`, { method: 'POST', body })

// ---- Card links / checklist ----
export const listCardLinks = (cardId: string) => apiFetch<CardLink[]>(`/api/cards/${cardId}/links`)

export const addCardLink = (cardId: string, body: { url: string; title?: string }) =>
  apiFetch<CardLink>(`/api/cards/${cardId}/links`, { method: 'POST', body })

export const deleteCardLink = (id: string) => apiFetch<void>(`/api/links/${id}`, { method: 'DELETE' })

export const addChecklistItem = (cardId: string, body: { text: string }) =>
  apiFetch<ChecklistItem>(`/api/cards/${cardId}/checklist`, { method: 'POST', body })

export const updateChecklistItem = (id: string, body: Partial<{ text: string; done: boolean; position: string }>) =>
  apiFetch<ChecklistItem>(`/api/checklist/${id}`, { method: 'PATCH', body })

export const deleteChecklistItem = (id: string) => apiFetch<void>(`/api/checklist/${id}`, { method: 'DELETE' })

// ---- Labels ----
export const listLabels = (boardId: string) => apiFetch<Label[]>(`/api/boards/${boardId}/labels`)

export const createLabel = (boardId: string, body: { name: string; color: string }) =>
  apiFetch<Label>(`/api/boards/${boardId}/labels`, { method: 'POST', body })

export const updateLabel = (id: string, body: { name: string; color: string }) =>
  apiFetch<Label>(`/api/labels/${id}`, { method: 'PATCH', body })

export const deleteLabel = (id: string) => apiFetch<void>(`/api/labels/${id}`, { method: 'DELETE' })

export const attachLabel = (cardId: string, labelId: string) =>
  apiFetch<void>(`/api/cards/${cardId}/labels/${labelId}`, { method: 'POST' })

export const detachLabel = (cardId: string, labelId: string) =>
  apiFetch<void>(`/api/cards/${cardId}/labels/${labelId}`, { method: 'DELETE' })
