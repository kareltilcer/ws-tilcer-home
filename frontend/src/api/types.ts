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

// ---- Auth (Mode B) ----

export interface UserPublic {
  id: string
  email: string
  display_name: string | null
  roles: string[]
}

export interface SessionUser {
  user: UserPublic
}

// ---- Dashboard (widget host, v2) ----

export type WidgetSize = 'narrow' | 'wide'

export interface WidgetCatalogEntry {
  key: string
  title: string
  module: string
  description: string | null
  default_size: WidgetSize
  admin_only: boolean
}

export interface LayoutItem {
  widget_key: string
  visible: boolean
  position: string
  size: WidgetSize
}

export interface LayoutItemInput {
  widget_key: string
  visible: boolean
  size: WidgetSize
}

export interface WidgetInstance {
  key: string
  size: WidgetSize
  data: unknown
}

export interface Dashboard {
  layout: LayoutItem[]
  widgets: WidgetInstance[]
}

// Widget payloads (data field of WidgetInstance, keyed by widget key).
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

export interface PravedelamWidget {
  tasks: DashboardTask[]
}
export interface PripominkyWidget {
  reminders: DashboardReminder[]
}
export interface TentoMesicWidget {
  occurrences: Occurrence[]
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

// ---- Notes (Poznámky, v3) ----

// The caller's view of a note's pin state.
export interface PinState {
  household: boolean
  personal: boolean
}

export type PinScope = 'household' | 'personal'

export interface Folder {
  id: string
  parent_id: string | null
  name: string
  slug: string
  /** Optional emoji icon; empty string means "render the 📁 default". */
  icon: string
  position: string
  archived: boolean
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
  created_by: string | null
  created_at: string
  updated_at: string
}

// A folder breadcrumb segment.
export interface PathSegment {
  id: string
  name: string
  slug: string
}

// Lightweight note node for the tree/list (no body).
export interface NoteSummary {
  id: string
  folder_id: string | null
  title: string
  slug: string
  position: string
  archived: boolean
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

// notes.pripnute widget payload.
export interface PinnedNote {
  note_id: string
  title: string
  slug_path: string
  scope: 'household' | 'personal' | 'both'
  excerpt: string | null
  updated_at: string
  position: string
}

export interface PripnutePoznamkyWidget {
  notes: PinnedNote[]
}

// ---- Documents (Dokumenty, v4) ----
//
// Documents live in their OWN folder tree (DocFolder), isolated from Poznámky's
// folders. Two properties shape the client code:
//   - The bytes are immutable: there is no "update content" call, and a changed
//     file is a new document.
//   - The permanent link is the ID-based `urls` block, not the slug path. The slug
//     path is navigation only and 404s after a rename or move.

export interface DocFolder {
  id: string
  parent_id: string | null
  name: string
  slug: string
  /** Optional emoji icon; empty string means "render the 📁 default". */
  icon: string
  position: string
  archived: boolean
  created_by: string | null
  created_at: string
  updated_at: string
}

// native = the original previews directly (PDF/image/text); pdf = a derived
// preview PDF exists (Office→PDF); none = download-only.
export type PreviewKind = 'native' | 'pdf' | 'none'
export type PreviewStatus = 'pending' | 'ready' | 'failed' | 'none'

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
  created_by: string | null
  created_at: string
  updated_at: string
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

// documents.pripnute widget payload.
export interface PinnedDocument {
  document_id: string
  title: string
  slug_path: string
  scope: 'household' | 'personal' | 'both'
  content_type: string
  byte_size: number
  preview_kind: PreviewKind
  preview_status: PreviewStatus
  thumbnail_url: string | null
  updated_at: string
  position: string
}

export interface PripnuteDokumentyWidget {
  documents: PinnedDocument[]
}

// ---- v5: push notifications + admin (Administrace) ----

/** Per-user mute preferences (D53a): a master switch plus one toggle per category. */
export interface PushPreferences {
  enabled: boolean
  categories: PushCategories
  updated_at: string | null
}

export interface PushCategories {
  broadcast: boolean
  triggers: boolean
  summaries: boolean
}

export interface PushSubscriptionInfo {
  id: string
  endpoint: string
  user_agent: string | null
  created_at: string
  last_seen_at: string
}

/** What a self-test reports: endpoints attempted on THIS account, and how many
 *  the push service actually accepted. */
export interface PushTestResult {
  subscriptions: number
  sent: number
}

/** Who a notification goes to. Default scope is "all" (D66). */
export interface Audience {
  scope: 'all' | 'roles' | 'users'
  roles?: string[]
  users?: string[]
}

export type ConditionOp = 'gt' | 'gte' | 'lt' | 'lte' | 'eq' | 'neq'

/** One "count compared to a number" clause. `key` names a metric or list from
 *  the catalog — a list's value is its length. */
export interface NotificationCondition {
  key: string
  op: ConditionOp
  value: number
}

/** A condition block gating a send: "all" ⇒ every clause must hold, "any" ⇒ at
 *  least one. Null ⇒ always send; a block with no items clears on save. */
export interface NotificationConditions {
  mode: 'all' | 'any'
  items: NotificationCondition[]
}

export interface NotificationRule {
  id: string
  name: string
  enabled: boolean
  action_key: string | null
  action_prefix: string | null
  filter_module: string | null
  filter_entity_type: string | null
  filter_level: string | null
  audience: Audience
  title_template: string | null
  body_template: string | null
  coalesce_window_seconds: number
  exclude_actor: boolean
  /** Evaluated at SEND time (after coalescing); household-scoped keys only. */
  conditions: NotificationConditions | null
  /** "HH:MM" wall-clock window; both set or both null; from > to wraps midnight. */
  active_from_local: string | null
  active_to_local: string | null
  created_by: string
  created_at: string
  updated_at: string
}

export interface NotificationRulePage {
  items: NotificationRule[]
  next_cursor: string | null
}

export type DayPreset = 'daily' | 'weekdays' | 'weekends'
export type WeekdayToken = 'mon' | 'tue' | 'wed' | 'thu' | 'fri' | 'sat' | 'sun'

/** Exactly one recurrence form is set. day_of_month is 1–31; 29–31 clamp to the
 *  month's last day in short months (D74) — deliberately NOT capped at 28. */
export interface DaysSpec {
  preset?: DayPreset
  weekdays?: WeekdayToken[]
  day_of_month?: number
}

export interface ScheduleSpec {
  time_local: string
  days: DaysSpec
}

export interface NotificationSchedule {
  id: string
  name: string
  enabled: boolean
  schedule: ScheduleSpec
  audience: Audience
  title_template: string
  body_template: string
  /** Evaluated when the slot fires; a personal-scoped key skips per recipient. */
  conditions: NotificationConditions | null
  last_fired_at: string | null
  /** Server-rendered Czech phrase ("Každý den v 8:00") — one source of truth. */
  description: string
  created_by: string
  created_at: string
  updated_at: string
}

export interface NotificationSchedulePage {
  items: NotificationSchedule[]
  next_cursor: string | null
}

export type DeliveryKind = 'broadcast' | 'trigger' | 'schedule' | 'test'
export type DeliveryStatus = 'sent' | 'failed' | 'expired'

export interface NotificationDelivery {
  id: string
  ts: string
  kind: DeliveryKind
  category: keyof PushCategories
  rule_id: string | null
  user_id: string
  user_label: string
  subscription_id: string | null
  status: DeliveryStatus
  error: string | null
}

export interface NotificationDeliveryPage {
  items: NotificationDelivery[]
  next_cursor: string | null
}

export interface ActionDescriptor {
  key: string
  module: string
  /** Human Czech phrase — "Když někdo dokončí připomínku". */
  label: string | null
}

export interface MetricDescriptor {
  key: string
  label: string
  unit: string | null
  scope: 'household' | 'personal'
}

/** One module list a summary can name — the "which ones?" to a metric's "how
 *  many?". `empty` is what the notification says on a quiet day. */
export interface ListDescriptor {
  key: string
  label: string
  empty: string
  scope: 'household' | 'personal'
}

export interface TokenPalette {
  time: string[]
  event?: string[]
  change?: string[]
  metric?: string[]
  list?: string[]
}

export interface HouseholdMember {
  user_id: string
  email: string
  display_name: string
  roles: string[]
  subscriptions: number
}

/** Everything the composer needs so keys are PICKED, never typed. */
export interface NotificationCatalog {
  actions: ActionDescriptor[]
  metrics: MetricDescriptor[]
  lists: ListDescriptor[]
  tokens: Record<string, TokenPalette>
  members: HouseholdMember[]
}

export interface SendResult {
  recipients: number
  subscriptions: number
}
