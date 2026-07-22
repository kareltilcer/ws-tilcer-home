import { apiFetch } from './client'
import type {
  AuditEventDetail,
  AuditEventDetailPage,
  AuditEventPage,
  Board,
  BoardTree,
  Card,
  CardDetail,
  CardLink,
  ChecklistItem,
  Column,
  ColumnKind,
  Dashboard,
  EventItem,
  EventLink,
  EventSeriesPage,
  EventWithLinks,
  Label,
  OccurrenceMonths,
  ReminderLead,
  StatsResponse,
} from './types'

function qs(params: Record<string, string | boolean | number | string[] | undefined>): string {
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined) continue
    if (Array.isArray(v)) v.forEach((x) => sp.append(k, x))
    else sp.append(k, String(v))
  }
  const s = sp.toString()
  return s ? `?${s}` : ''
}

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

// ---- Cards ----
export const createCard = (columnId: string, body: { title: string; notes?: string }) =>
  apiFetch<Card>(`/api/columns/${columnId}/cards`, { method: 'POST', body })
export const getCard = (id: string) => apiFetch<CardDetail>(`/api/cards/${id}`)
export const updateCard = (id: string, body: Partial<{ title: string; notes: string | null; archived: boolean }>) =>
  apiFetch<Card>(`/api/cards/${id}`, { method: 'PATCH', body })
export const deleteCard = (id: string, hard = false) =>
  apiFetch<void>(`/api/cards/${id}${qs({ hard })}`, { method: 'DELETE' })
export const moveCard = (id: string, body: { column_id: string; position?: string }, via?: string) =>
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

// ---- Events ----
export const listEvents = (params: { include_archived?: boolean; limit?: number; cursor?: string } = {}) =>
  apiFetch<EventSeriesPage>(`/api/events${qs(params)}`)
export const getEvent = (id: string) => apiFetch<EventWithLinks>(`/api/events/${id}`)
export interface EventInput {
  title: string
  description?: string
  starts_on: string
  rrule?: string
  reminder_enabled?: boolean
  reminder_lead?: ReminderLead
}
export const createEvent = (body: EventInput) => apiFetch<EventItem>('/api/events', { method: 'POST', body })
export const updateEvent = (id: string, body: Partial<EventInput & { archived: boolean }>) =>
  apiFetch<EventItem>(`/api/events/${id}`, { method: 'PATCH', body })
export const deleteEvent = (id: string, hard = false) =>
  apiFetch<void>(`/api/events/${id}${qs({ hard })}`, { method: 'DELETE' })
export const getOccurrences = (from: string, to: string, includeArchived = false) =>
  apiFetch<OccurrenceMonths>(`/api/events/occurrences${qs({ from, to, include_archived: includeArchived })}`)
export const addEventLink = (eventId: string, body: { url: string; title?: string }) =>
  apiFetch<EventLink>(`/api/events/${eventId}/links`, { method: 'POST', body })
export const deleteEventLink = (id: string) => apiFetch<void>(`/api/event-links/${id}`, { method: 'DELETE' })
export const completeReminder = (eventId: string, occurrenceOn: string, via?: string) =>
  apiFetch<unknown>(`/api/events/${eventId}/complete${qs({ via })}`, { method: 'POST', body: { occurrence_on: occurrenceOn } })
export const uncompleteReminder = (eventId: string, occurrenceOn: string) =>
  apiFetch<void>(`/api/events/${eventId}/complete${qs({ occurrence_on: occurrenceOn })}`, { method: 'DELETE' })

// ---- Dashboard ----
export const getDashboard = () => apiFetch<Dashboard>('/api/dashboard')

// ---- Logs (admin) ----
export interface LogFilters {
  from?: string
  to?: string
  module?: string
  actor?: string
  action?: string
  entity_type?: string
  entity_id?: string
  level?: string
  q?: string
  limit?: number
  cursor?: string
}
export const listLogs = (f: LogFilters = {}) => apiFetch<AuditEventPage>(`/api/logs${qs(f as Record<string, string>)}`)
export const getLog = (id: string) => apiFetch<AuditEventDetail>(`/api/logs/${id}`)
export const getEntityTimeline = (type: string, id: string, params: { limit?: number; cursor?: string } = {}) =>
  apiFetch<AuditEventDetailPage>(`/api/logs/entity/${type}/${id}${qs(params)}`)
export const getLogStats = (dimension: string, bucket = 'day', from?: string, to?: string) =>
  apiFetch<StatsResponse>(`/api/logs/stats${qs({ dimension, bucket, from, to })}`)
