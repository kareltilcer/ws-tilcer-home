// Administrace / admin (v5, v9, v10) — the module's wire types, mirroring the Go
// backend JSON (openapi.yaml): Web Push broadcasts and their rules and schedules,
// the notification catalog, and the two Úložiště screens.
//
// ⚠ The per-device PUSH types (PushPreferences, PushSubscriptionInfo and friends)
// are NOT here. They stay in src/api/types.ts because push is platform, not this
// module: every member has a subscription and the settings panel that manages one
// is platform/settings, while what this file describes is the admin-only half —
// who gets sent what, and when.

import type { PushCategories } from '@/api/types'

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
  /** v10. Absent when no module reports storage groups (a build without chat). */
  chat?: StorageChat
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

/**
 * The chat block of the storage snapshot (FR-V10-16, D240/D254).
 *
 * ⚠ THERE IS NO WAY IN FROM HERE, AND THE ABSENCE IS THE SPECIFICATION. No thread,
 * no message, no attachment list, no clean-up page and no link: an admin sees which
 * room is heavy and asks its members, because clean-up is member-scoped (D241) and
 * the only two chat verbs an admin has over a room they are not in are restore and
 * purge — neither of which opens it (D255). Do not add a link here.
 */
export interface StorageChat {
  total_bytes: number | null
  threshold_total_mb: number
  exceeded: boolean
  threshold_conversation_mb: number
  thresholds_updated_at: string | null
  thresholds_updated_by: string | null
  /**
   * ⚠ Always false, deliberately (D229). Chat blobs are NOT copied to the backup
   * bucket — they are the most disposable bytes in the application and the module
   * exists under a storage warning, so doubling them into the mirror would be the
   * one place in Home where a background job undermines a threshold. The page
   * renders *Nezálohováno* rather than leaving a gap that reads as zero.
   */
  mirrored: boolean
  conversations: StorageChatConversation[]
}

export interface StorageChatConversation {
  id: string
  name: string
  /** A COUNT. Never the names — an admin sees that a room is heavy, not who is in it. */
  members: number
  bytes: number | null
  objects: number | null
  over_limit: boolean
  /** Non-null ⇒ in the koš, and ⚠ still counted in `total_bytes` (D254). */
  trashed_at: string | null
  purge_after: string | null
}

export interface StorageThresholds {
  chat_total_mb: number
  chat_conversation_mb: number
  updated_at: string | null
  updated_by: string | null
}

export interface StorageThresholdsUpdate {
  chat_total_mb?: number
  chat_conversation_mb?: number
}
