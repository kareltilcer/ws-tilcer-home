// The wire types more than one place speaks, mirroring the Go backend JSON
// (openapi.yaml).
//
// ⚠ WHAT IS LEFT HERE IS A CONTRACT, NOT A REMAINDER. Each module's own shapes
// now live beside that module, in src/modules/<x>/api/types.ts. What stayed is
// the vocabulary shared ACROSS modules, and it is the frontend's answer to the
// same question internal/platform answers on the backend:
//
//   - the v9 scope and pinning vocabulary (Scope, Visibility, PinScope, PinState)
//     — notes, documents, admin, lib/scope.ts and components/common all read it;
//   - PublishRequest, the one-way private→shared contract notes and documents
//     share (D182);
//   - the WIDGET-HOST CONTRACT — the payload each contributed widget returns. The
//     dashboard host owns no feature data, so these types are precisely the seam
//     between it and the modules that fill them in, and platform/widgets/registry
//     is what wires the two together. Splitting them per module would put one
//     contract in six files;
//   - auth (Mode B) and per-device push, which are platform rather than any
//     module's.

// '0d' is the same-day reminder — a lead of nothing, opening on the event’s own date.
export type ReminderLead = '0d' | '1d' | '2d' | '1w' | '2w' | '1m'

export interface ChecklistProgress {
  done: number
  total: number
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

export interface UserPublic {
  id: string
  email: string
  display_name: string | null
  roles: string[]
}

export interface SessionUser {
  user: UserPublic
}

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

// A folder breadcrumb segment.
export interface PathSegment {
  id: string
  name: string
  slug: string
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

// native = the original previews directly (PDF/image/text); pdf = a derived
// preview PDF exists (Office→PDF); none = download-only.
//
// ⚠ CENTRAL RATHER THAN documents/, BECAUSE THE WIDGET PAYLOAD EMBEDS THEM.
// PinnedDocument below is the documents.pripnute contract the dashboard host
// renders, and it carries both of these — so leaving them in the module would
// make this file import FROM documents.
export type PreviewKind = 'native' | 'pdf' | 'none'

export type PreviewStatus = 'pending' | 'ready' | 'failed' | 'none'

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
