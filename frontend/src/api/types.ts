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
  /**
   * True when this event concerns a PRIVATE note or document and the caller is
   * not its owner (v9, D187). The row is still returned — the spine records
   * everything — but `summary` carries the fixed phrase *"Soukromá položka —
   * podrobnosti skryty"*, `entity_id` is dropped and `changes` comes back empty.
   *
   * ⚠ Render it as a STATE, not by string-matching the summary. Without it the
   * browser shows the phrase as though somebody had written it, and there is no
   * way to tell a redacted row from a row about something dull.
   */
  redacted: boolean
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

/**
 * Which root an item lives in (v9, PRD D177).
 *
 * ⚠ REQUIRED on every item, never optional. An item whose visibility a client
 * has to INFER is an item some client will get wrong, and the cost of getting it
 * wrong is rendering a private title with no lock on it.
 */
export type Visibility = 'shared' | 'private'

/**
 * The root scope a read addresses (v9, D184). `private` always means THE
 * CALLER'S OWN root — there is no value that names another member's, and no
 * parameter that could express one.
 *
 * Defaults to `shared` everywhere, which is why nothing that predates v9 had to
 * change to keep working.
 */
export type Scope = 'shared' | 'private'

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

// notes.pripnute widget payload.
export interface PinnedNote {
  note_id: string
  title: string
  slug_path: string
  scope: 'household' | 'personal' | 'both'
  /**
   * Drives the widget row's lock mark (v9, D183). Only ever `private` for the
   * member looking at it — a private note takes personal pins only, and those are
   * filtered to the caller.
   */
  visibility: Visibility
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
  visibility: Visibility
  owner_id: string | null
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

// documents.pripnute widget payload.
export interface PinnedDocument {
  document_id: string
  title: string
  slug_path: string
  scope: 'household' | 'personal' | 'both'
  /**
   * Drives the widget row's lock mark (v9, D183). Only ever `private` for the
   * member looking at it — a private document takes personal pins only, and those
   * are filtered to the caller.
   */
  visibility: Visibility
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
  /** v10's fourth bucket (cat_chat). Defaults on, like the other three. */
  chat: boolean
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

/** ⚠ `chat` is v10's fifth (08003 widens the table's CHECK to admit it). */
export type DeliveryKind = 'broadcast' | 'trigger' | 'schedule' | 'test' | 'chat'
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

// ---- Finance (v6) ----
//
// Column vocabulary is `fin`'s, carried over verbatim (D83): the two income
// slots keep their names. Only the wire style is home's — snake_case (D92).
//
// The `split` block is DERIVED ON READ by the server and never stored (D82).
// Everything the list and the widget render comes from it; the frontend mirror
// in routes/finance/split.ts exists only to preview a split before it is saved.

export interface FinanceRates {
  personal: number
  operational: number
  fun: number
  no_fun: number
}

export interface FinanceSplit {
  total_income: number
  personal_kaja: number
  personal_andy: number
  to_operational_kaja: number
  to_operational_andy: number
  operational_received: number
  fun_savings: number
  no_fun_savings: number
  /** The remainder; absorbs all rounding and can be negative by up to 2 Kč. The
   *  UI shows 0 Kč with a footnote — the DATA is never clamped. */
  needs: number
}

export interface FinanceMonth {
  id: string
  /** YYYY-MM, unique. */
  month: string
  income_kaja: number
  income_andy: number
  rates: FinanceRates
  split: FinanceSplit
  /** NULL for the rows seeded from `fin`, which recorded no actor. */
  created_by: string | null
  created_at: string
  updated_at: string
}

export interface FinanceMonthPage {
  items: FinanceMonth[]
  next_cursor?: string | null
}

export interface FinanceMonthInput {
  month: string
  income_kaja: number
  income_andy: number
  rates: FinanceRates
}

/** The finance.rozpocet widget payload — one of two states (FR-F7). The
 *  "missing" one is the module's most useful output: nobody has entered the
 *  current month yet. */
export interface RozpocetWidget {
  state: 'recorded' | 'missing'
  month: string
  total_income?: number
  personal_kaja?: number
  personal_andy?: number
  needs?: number
  savings?: number
}

// ---- v9: publishing, and the two Administrace storage screens ----

/**
 * Destination for `POST /api/{notes,documents}/{id}/publish` (D182).
 *
 * Both fields optional: an empty body publishes to the shared ROOT, which is the
 * common case — somebody publishing a thing usually wants it visible, not filed.
 *
 * ⚠ There is NO unpublish request type because there is no unpublish route, and
 * the absence is a decision rather than a gap. A document the household has
 * relied on for months must not be able to vanish into one member's tree.
 */
export interface PublishRequest {
  folder_id?: string | null
  position?: string
}

/** The whole Úložiště payload, computed on read (D195). */
export interface StorageSnapshot {
  generated_at: string
  /** True when served from the in-process cache — a stale figure must LOOK stale. */
  cached: boolean
  cache_seconds: number
  database: StorageDatabase
  blobs: StorageBlobs
  replica: StorageReplica
  backup: StorageBackup
  warning: StorageWarning
}

export interface StorageDatabase {
  /** Exact: page_count × page_size. The only figure checkable against `ls`. */
  total_bytes: number
  wal_bytes: number
  free_bytes: number | null
  /**
   * Whether PER-TABLE bytes could be measured at all — i.e. whether the SQLite
   * build exposes `dbstat` (D193). When false every `bytes` below is null and
   * only `row_count` is populated. `total_bytes` stays exact either way.
   *
   * ⚠ Render null as *nezměřeno*, NEVER as `0 B`. A zero somebody did not measure
   * is a lie that looks like good news.
   */
  bytes_available: boolean
  modules: StorageModuleDb[]
}

export interface StorageModuleDb {
  module: string
  bytes: number | null
  tables: StorageTable[]
}

export interface StorageTable {
  name: string
  /** Always populated — a COUNT(*) needs no dbstat. Except on a `virtual` row: see below. */
  row_count: number
  /**
   * A VIRTUAL table — Home's four external-content FTS5 indexes.
   *
   * It owns no b-tree, so `bytes`/`index_bytes` are a measured ZERO (the four shadow
   * tables listed beside it carry every page) and `row_count` is 0 and meaningless:
   * counting one is a full traversal of the index for a figure the content table
   * already states. Render it as a virtual row, not as an empty one.
   */
  virtual: boolean
  bytes: number | null
  /** Reported apart from the rows because an FTS5 index can outweigh what it indexes. */
  index_bytes: number | null
}

export interface StorageBlobs {
  /**
   * False when object storage could not be reached. The endpoint still returns
   * 200 with the database figures intact — a bucket outage must not blank the
   * page, and losing the half that WAS measurable helps nobody.
   */
  available: boolean
  error: string | null
  bucket: string | null
  total_bytes: number | null
  total_objects: number | null
  modules: StorageModuleBlobs[]
}

export interface StorageModuleBlobs {
  module: string
  prefix: string
  bytes: number | null
  objects: number | null
  owners: StorageOwnerUsage[]
}

export interface StorageOwnerUsage {
  /**
   * `unattributed` (**Nezařazené**) is objects that resolve to no live row — the
   * orphan backlog the mirror job reconciles, surfaced for the first time (D194).
   *
   * ⚠ It is an ordinary row, NOT an error: not red, not a warning. It needs one
   * line of copy explaining what it is, because the number is meaningless and
   * mildly alarming without one.
   */
  kind: 'shared' | 'private' | 'unattributed'
  owner_user_id: string | null
  owner_label: string | null
  bytes: number
  objects: number
  /**
   * Present only on `warning.largest_contributors` rows, where every module's
   * usage is flattened into one list and two same-kind rows are otherwise
   * indistinguishable. Inside a module's `owners` block the parent names it.
   */
  module?: string
}

/**
 * The Litestream replica line.
 *
 * ⚠ AS BUILT `configured` IS ALWAYS FALSE, and that is a DECISION, not an
 * unfinished feature (PRD §V9-12). Reading the replica would require the backend
 * to hold the credentials for the household's entire database backup; Karel
 * declined that. The type stays so the shape is stable and so this screen has a
 * state to render — the design covers it ("no backup bucket and no replica
 * configured").
 */
export interface StorageReplica {
  configured: boolean
  prefix: string | null
  bytes: number | null
  objects: number | null
  generations: number | null
  newest_at: string | null
}

/** The mirror bucket as one line (D205) — half the R2 bill, never shown before. */
export interface StorageBackup {
  configured: boolean
  bucket: string | null
  bytes: number | null
  objects: number | null
}

/**
 * One threshold on the MODULES' R2 total (D196).
 *
 * ⚠ NOTHING IS EVER BLOCKED BY IT: no upload fails, there is no quota, there is no
 * new 413. The register is informational — `--attention`, never the destructive
 * red — and the copy says so outright. Nobody has done anything wrong; the bucket
 * is simply larger than a number somebody chose.
 */
export interface StorageWarning {
  threshold_mb: number
  measured_bytes: number | null
  exceeded: boolean
  largest_contributors: StorageOwnerUsage[]
}

/**
 * One row of Soukromé položky (D198).
 *
 * ⚠ THE FIELDS THAT ARE ABSENT ARE THE SPECIFICATION. No title, no filename, no
 * description, no content type, no preview, no download. An admin can name the
 * thing well enough to delete it and not well enough to know what it is — and if
 * browsing this screen ever starts to feel pleasant, something has gone wrong.
 */
export interface PrivateItem {
  id: string
  module: 'notes' | 'documents'
  /**
   * `note_image` rows are INFORMATIONAL and not deletable: an image belongs to
   * its note and goes when the note does (D204/D212). The screen says so rather
   * than offering a control that 405s.
   */
  kind: 'note' | 'document' | 'note_folder' | 'document_folder' | 'note_image'
  owner_user_id: string
  owner_label: string | null
  byte_size: number
  created_at: string
  updated_at: string | null
}

export interface PrivateItemPage {
  items: PrivateItem[]
  next_cursor: string | null
  /** Covers ALL matching items, not just this page — the figure the screen acts on. */
  total_bytes: number | null
}
