// Wire types mirroring the Go backend JSON (openapi.yaml).

export type ColumnKind = 'normal' | 'now' | 'done'
export type ReminderLead = '1d' | '2d' | '1w' | '2w' | '1m'

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

export interface ChecklistProgress {
  done: number
  total: number
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
}

export interface CardDetail extends Card {
  links: CardLink[]
  checklist: ChecklistItem[]
  labels: Label[]
}

export interface BoardTreeColumn {
  column: Column
  cards: Card[]
}

export interface BoardTree {
  board: Board
  columns: BoardTreeColumn[]
}

// ---- Events ----

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

export interface Occurrence {
  event_id: string
  occurrence_on: string
  title: string
  description: string | null
  recurring: boolean
  reminder_enabled: boolean
  reminder_lead: ReminderLead | null
  reminder_completed: boolean
}

export interface OccurrenceMonths {
  months: { month: string; occurrences: Occurrence[] }[]
}

export interface EventSeriesPage {
  items: EventItem[]
  next_cursor: string | null
}

// ---- Dashboard ----

export interface DashboardReminder {
  event_id: string
  occurrence_on: string
  title: string
  recurring: boolean
  reminder_lead: ReminderLead
  overdue: boolean
  days_until: number
}

export interface DashboardTask {
  card_id: string
  title: string
  board_id: string
  board_name: string
  column_id: string
  column_name: string
  label_ids: string[]
  checklist_progress: ChecklistProgress
  done_column_id: string | null
}

export interface Dashboard {
  reminders: DashboardReminder[]
  tasks: DashboardTask[]
}

// ---- Logs ----

export interface AuditChange {
  field: string
  old_value: string | null
  new_value: string | null
}

export interface AuditEvent {
  id: string
  ts: string
  actor_user_id: string | null
  actor_type: string
  actor_label: string | null
  module: string
  action: string
  entity_type: string | null
  entity_id: string | null
  summary: string
  level: 'info' | 'warn' | 'error'
  request_id: string | null
  site: string
  meta: Record<string, unknown> | null
  change_count: number
}

export interface AuditEventDetail extends AuditEvent {
  changes: AuditChange[]
}

export interface AuditEventPage {
  items: AuditEvent[]
  next_cursor: string | null
}

export interface AuditEventDetailPage {
  items: AuditEventDetail[]
  next_cursor: string | null
}

export interface StatsResponse {
  dimension: string
  bucket: string
  buckets: { ts: string; counts: Record<string, number> }[]
  totals: { key: string; count: number }[]
}
