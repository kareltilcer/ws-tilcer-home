// Okno do budoucnosti / events — the module's API surface (openapi.yaml).

import { apiFetch } from '@/api/client'
import { qs } from '@/api/qs'
import type {
  EventItem,
  EventLink,
  EventSeriesPage,
  EventWithLinks,
  OccurrenceMonths,
} from './types'
import type { ReminderLead } from '@/api/types'

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
