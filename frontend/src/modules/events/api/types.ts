// Okno do budoucnosti / events — the module's wire types, mirroring the Go
// backend JSON (openapi.yaml).

import type { Occurrence, ReminderLead } from '@/api/types'

export interface EventItem {
  id: string
  title: string
  description: string | null
  starts_on: string
  rrule: string | null
  timezone: string
  reminder_enabled: boolean
  reminder_lead: ReminderLead | null
  archived: boolean
  created_by: string | null
  created_at: string
  updated_at: string
}

export interface EventLink {
  id: string
  event_id: string
  url: string
  title: string | null
  position: string
}

export interface EventWithLinks extends EventItem {
  links: EventLink[]
}

export interface OccurrenceMonths {
  months: { month: string; occurrences: Occurrence[] }[]
}

export interface EventSeriesPage {
  items: EventItem[]
  next_cursor: string | null
}
