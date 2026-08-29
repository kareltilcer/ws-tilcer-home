import { apiFetch, apiUpload } from './client'
import { qs } from './qs'
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
  DocFolder,
  DocFolderDetail,
  DocResolveResult,
  DocumentDetail,
  DocumentPage,
  DocumentsTree,
  EventItem,
  EventLink,
  EventSeriesPage,
  EventWithLinks,
  FinanceMonth,
  FinanceMonthInput,
  FinanceMonthPage,
  Folder,
  FolderDetail,
  Label,
  LayoutItem,
  LayoutItemInput,
  NoteDetail,
  NoteImageUploadResult,
  NotePage,
  NotesTree,
  OccurrenceMonths,
  PinScope,
  PinState,
  ReminderLead,
  ResolveResult,
  SessionUser,
  StatsResponse,
  WidgetCatalogEntry,
  WidgetInstance,
  Audience,
  NotificationCatalog,
  NotificationDeliveryPage,
  NotificationRule,
  NotificationRulePage,
  NotificationSchedule,
  NotificationSchedulePage,
  PushCategories,
  PushPreferences,
  PushSubscriptionInfo,
  PushTestResult,
  SendResult,
  // v9
  PrivateItemPage,
  PublishRequest,
  Scope,
  StorageSnapshot,
  StorageThresholds,
  StorageThresholdsUpdate,
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

// ---- Auth (Mode B) ----
export const login = (email: string, password: string) =>
  apiFetch<SessionUser>('/api/auth/login', { method: 'POST', body: { email, password }, skipAuthRedirect: true })
export const logout = () => apiFetch<void>('/api/auth/logout', { method: 'POST', skipAuthRedirect: true })
export const getSession = () => apiFetch<SessionUser>('/api/auth/session', { skipAuthRedirect: true })

// ---- Dashboard host (widget host) ----
export const getDashboard = () => apiFetch<Dashboard>('/api/dashboard')
export const getDashboardCatalog = () => apiFetch<WidgetCatalogEntry[]>('/api/dashboard/catalog')
export const saveDashboardLayout = (items: LayoutItemInput[]) =>
  apiFetch<LayoutItem[]>('/api/dashboard/layout', { method: 'PUT', body: items })
export const getWidget = (key: string) =>
  apiFetch<WidgetInstance>(`/api/dashboard/widgets/${encodeURIComponent(key)}`)

// ---- Notes (Poznámky, v3; scoped in v9) ----
//
// ⚠ Every notes read takes a SCOPE, and it is a REQUIRED argument rather than one
// defaulting to `shared` here. The WIRE default is `shared` — that is what keeps a
// pre-v9 client working untouched — but a caller inside this app always knows
// which root the user is standing in, and letting it omit the scope is exactly how
// a page ends up reading the household tree while the switcher says otherwise.
export const getNotesTree = (scope: Scope, includeArchived = false) =>
  apiFetch<NotesTree>(`/api/notes/tree${qs({ scope, include_archived: includeArchived })}`)
export const resolveNotePath = (path: string, scope: Scope) =>
  apiFetch<ResolveResult>(`/api/notes/resolve${qs({ path, scope })}`)
export const searchNotes = (q: string, scope: Scope) =>
  apiFetch<NotePage>(`/api/notes${qs({ q, scope })}`)
export const getNote = (id: string) => apiFetch<NoteDetail>(`/api/notes/${encodeURIComponent(id)}`)
export const createNote = (body: {
  title: string
  folder_id?: string | null
  body_md?: string
  /**
   * Selects the root when folder_id is null. With a parent folder the PARENT's
   * scope governs and a disagreement is a 422 — a folder whose contents are half
   * private is exactly the model D177 rejected.
   */
  scope?: Scope
}) => apiFetch<NoteDetail>('/api/notes', { method: 'POST', body })
export const updateNote = (
  id: string,
  body: Partial<{ title: string; body_md: string | null; archived: boolean }>,
  via?: string,
) => apiFetch<NoteDetail>(`/api/notes/${encodeURIComponent(id)}${qs({ via })}`, { method: 'PATCH', body })
export const deleteNote = (id: string, hard = false) =>
  apiFetch<void>(`/api/notes/${encodeURIComponent(id)}${qs({ hard })}`, { method: 'DELETE' })
export const moveNote = (id: string, body: { folder_id?: string | null; position: string }) =>
  apiFetch<NoteDetail>(`/api/notes/${encodeURIComponent(id)}/move`, { method: 'POST', body })
export const pinNote = (id: string, scope: PinScope, via?: string) =>
  apiFetch<PinState>(`/api/notes/${encodeURIComponent(id)}/pin${qs({ via })}`, { method: 'POST', body: { scope } })
export const unpinNote = (id: string, scope: PinScope, via?: string) =>
  apiFetch<PinState>(`/api/notes/${encodeURIComponent(id)}/pin${qs({ scope, via })}`, { method: 'DELETE' })

export const createFolder = (body: {
  name: string
  parent_id?: string | null
  icon?: string
  scope?: Scope
}) => apiFetch<FolderDetail>('/api/notes/folders', { method: 'POST', body })
export const updateFolder = (id: string, body: Partial<{ name: string; archived: boolean; icon: string }>) =>
  apiFetch<FolderDetail>(`/api/notes/folders/${encodeURIComponent(id)}`, { method: 'PATCH', body })
export const deleteFolder = (id: string, opts: { cascade?: boolean; hard?: boolean } = {}) =>
  apiFetch<void>(`/api/notes/folders/${encodeURIComponent(id)}${qs(opts)}`, { method: 'DELETE' })
export const moveFolder = (id: string, body: { parent_id?: string | null; position: string }) =>
  apiFetch<Folder>(`/api/notes/folders/${encodeURIComponent(id)}/move`, { method: 'POST', body })

// noteImageUploadTimeoutMs bounds a single image upload. Without it, a request that
// never settles (a stalled connection) would leave the editor's image node stuck on
// its data:/blob: src forever — which suppresses every later autosave emission and
// silently freezes the note for the whole session. A hard deadline makes a hung upload
// reject instead, so the caller can drop the node and surface an error. Generous
// headroom for the 10 MB cap on a slow link (real pasted images are far smaller).
const noteImageUploadTimeoutMs = 120_000

/** uploadNoteImage streams one pasted/dropped image to object storage and returns
 *  the reference URL the editor embeds as `![](url)`. Keeps body_md small — the
 *  bytes never travel inline. Aborts after noteImageUploadTimeoutMs so a stuck
 *  upload can never wedge the editor's autosave. */
export const uploadNoteImage = (noteId: string, file: File | Blob, filename = 'image') => {
  const form = new FormData()
  form.append('file', file, filename)
  const abort = new AbortController()
  const timer = setTimeout(() => abort.abort(), noteImageUploadTimeoutMs)
  return apiUpload<NoteImageUploadResult>(`/api/notes/${encodeURIComponent(noteId)}/images`, form, {
    signal: abort.signal,
  }).finally(() => clearTimeout(timer))
}

// ---- Documents (Dokumenty, v4) ----
//
// Two things to know when calling these:
//   1. There is no "replace the file" call, by design — the bytes are immutable, so
//      a changed file is a NEW document (uploadDocument again).
//   2. The permanent link to a document is the id-based `urls` block on the detail
//      response, NOT the slug path. Copy `urls.permalink` ("/d/{id}"), which
//      survives renames and moves; the slug path does not.
//   3. (v9) Every read takes a SCOPE — see the note above getNotesTree for why it
//      is required here rather than defaulted.
export const getDocumentsTree = (scope: Scope, includeArchived = false) =>
  apiFetch<DocumentsTree>(`/api/documents/tree${qs({ scope, include_archived: includeArchived })}`)
export const resolveDocumentPath = (path: string, scope: Scope) =>
  apiFetch<DocResolveResult>(`/api/documents/resolve${qs({ path, scope })}`)
export const searchDocuments = (q: string, scope: Scope) =>
  apiFetch<DocumentPage>(`/api/documents${qs({ q, scope })}`)
export const listDocuments = (
  params: {
    folder_id?: string
    include_archived?: boolean
    limit?: number
    cursor?: string
    scope?: Scope
  } = {},
) => apiFetch<DocumentPage>(`/api/documents${qs(params)}`)
export const getDocument = (id: string) => apiFetch<DocumentDetail>(`/api/documents/${encodeURIComponent(id)}`)

/** uploadDocument streams one file to the documents bucket.
 *
 *  Field order is significant: the backend reads the multipart parts in order and
 *  applies the text fields it has seen by the time the file part starts (it never
 *  buffers a 50 MB body). So the metadata MUST be appended before the file. */
export const uploadDocument = (
  file: File,
  meta: {
    folder_id?: string | null
    title?: string
    description?: string
    /** v9: which root to upload into when folder_id is absent. */
    scope?: Scope
  } = {},
  onProgress?: (fraction: number) => void,
) => {
  const form = new FormData()
  if (meta.folder_id) form.append('folder_id', meta.folder_id)
  if (meta.title) form.append('title', meta.title)
  if (meta.description) form.append('description', meta.description)
  if (meta.scope) form.append('scope', meta.scope)
  form.append('file', file, file.name) // last, on purpose — see above
  return apiUpload<DocumentDetail>('/api/documents', form, { onProgress })
}

// PATCH is metadata only: title (re-derives the slug), description, archived.
export const updateDocument = (
  id: string,
  body: Partial<{ title: string; description: string | null; archived: boolean }>,
  via?: string,
) => apiFetch<DocumentDetail>(`/api/documents/${encodeURIComponent(id)}${qs({ via })}`, { method: 'PATCH', body })
// hard=true purges the row AND the stored file; it is admin-only server-side.
export const deleteDocument = (id: string, hard = false) =>
  apiFetch<void>(`/api/documents/${encodeURIComponent(id)}${qs({ hard })}`, { method: 'DELETE' })
export const moveDocument = (id: string, body: { folder_id?: string | null; position: string }, via?: string) =>
  apiFetch<DocumentDetail>(`/api/documents/${encodeURIComponent(id)}/move${qs({ via })}`, { method: 'POST', body })
export const pinDocument = (id: string, scope: PinScope, via?: string) =>
  apiFetch<PinState>(`/api/documents/${encodeURIComponent(id)}/pin${qs({ via })}`, { method: 'POST', body: { scope } })
export const unpinDocument = (id: string, scope: PinScope, via?: string) =>
  apiFetch<PinState>(`/api/documents/${encodeURIComponent(id)}/pin${qs({ scope, via })}`, { method: 'DELETE' })

export const createDocumentFolder = (body: {
  name: string
  parent_id?: string | null
  icon?: string
  scope?: Scope
}) => apiFetch<DocFolderDetail>('/api/documents/folders', { method: 'POST', body })
export const updateDocumentFolder = (id: string, body: Partial<{ name: string; archived: boolean; icon: string }>) =>
  apiFetch<DocFolderDetail>(`/api/documents/folders/${encodeURIComponent(id)}`, { method: 'PATCH', body })
export const deleteDocumentFolder = (id: string, opts: { cascade?: boolean; hard?: boolean } = {}) =>
  apiFetch<void>(`/api/documents/folders/${encodeURIComponent(id)}${qs(opts)}`, { method: 'DELETE' })
export const moveDocumentFolder = (id: string, body: { parent_id?: string | null; position: string }) =>
  apiFetch<DocFolder>(`/api/documents/folders/${encodeURIComponent(id)}/move`, { method: 'POST', body })

/** documentContentUrl builds a content URL for an <img>/<iframe>/anchor target.
 *  These are permanent and household-only: they carry the session cookie and are
 *  never presigned storage links. */
export const documentContentUrl = (id: string, kind: 'raw' | 'download' | 'preview' | 'thumbnail') =>
  `/api/documents/${encodeURIComponent(id)}/${kind}`

// ---- v9: publish (private → shared) ----
//
// ⚠ ONE-WAY AND OWNER-ONLY (D182). There is deliberately no `unpublishNote` below
// and there is no route behind one: a note the household has relied on for months
// must not be able to vanish into one member's tree. Somebody who wants it back
// re-creates it privately and deletes the shared copy, which leaves both facts in
// the audit log.
//
// ⚠ A non-owner — an ADMIN INCLUDED — gets 404, not 403 (D206). The UI must never
// render "you may not publish this" for a foreign item: the ordinary nenalezeno
// screen is the whole answer, because a permission message is itself the
// disclosure.
export const publishNote = (id: string, body: PublishRequest = {}) =>
  apiFetch<NoteDetail>(`/api/notes/${encodeURIComponent(id)}/publish`, { method: 'POST', body })
export const publishNoteFolder = (id: string, body: PublishRequest = {}) =>
  apiFetch<FolderDetail>(`/api/notes/folders/${encodeURIComponent(id)}/publish`, {
    method: 'POST',
    body,
  })
export const publishDocument = (id: string, body: PublishRequest = {}) =>
  apiFetch<DocumentDetail>(`/api/documents/${encodeURIComponent(id)}/publish`, {
    method: 'POST',
    body,
  })
export const publishDocumentFolder = (id: string, body: PublishRequest = {}) =>
  apiFetch<DocFolderDetail>(`/api/documents/folders/${encodeURIComponent(id)}/publish`, {
    method: 'POST',
    body,
  })

// ---- v9: Administrace → Úložiště and Soukromé položky (admin only) ----

/**
 * The storage snapshot. `refresh` bypasses the server's 60-second cache (D195).
 *
 * ⚠ A 200 does NOT mean everything was measurable: check `database.bytes_available`
 * and `blobs.available` before rendering a figure, and render null as *nezměřeno*,
 * never as 0.
 */
export const getStorageSnapshot = (refresh = false) =>
  apiFetch<StorageSnapshot>(`/api/admin/storage${qs({ refresh: refresh || undefined })}`)

/**
 * The two chat storage thresholds (v10, D236/D263).
 *
 * ⚠ THERE IS DELIBERATELY NO GET. The current values ride the snapshot above, so a
 * second endpoint would be a second answer to one question — and the two would
 * disagree for exactly as long as one of their caches was warmer than the other.
 * The PUT returns what it saved, which is what the fields render back.
 *
 * ⚠ A VALUE BELOW CURRENT USAGE IS SAVED, NOT REFUSED (D237/D244). Nothing in v10
 * is ever blocked by a threshold — the whole register is warn-only — so the screen
 * says what it just switched on rather than arguing about it.
 */
export const setStorageThresholds = (body: StorageThresholdsUpdate) =>
  apiFetch<StorageThresholds>('/api/admin/storage/thresholds', { method: 'PUT', body })

/**
 * The purge screen's listing (D198).
 *
 * ⚠ OPENING IT IS AUDITED server-side — `admin.private_items.view`, the only read
 * in Home that writes an audit event. The screen says so out loud; calling this
 * function speculatively (a prefetch, a poll) would put noise in that record.
 *
 * ⚠ `sort: 'size'` is SINGLE-PAGE by design: a keyset cursor is an id, and an id
 * does not locate a position in a size ordering. `total_bytes` still covers every
 * matching item, so the figure the screen acts on stays complete.
 */
export const listPrivateItems = (
  params: {
    owner_user_id?: string
    module?: 'notes' | 'documents'
    sort?: 'recent' | 'size'
    limit?: number
    cursor?: string
  } = {},
) => apiFetch<PrivateItemPage>(`/api/admin/storage/private-items${qs(params)}`)

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

// ---- v5: push (every member, incl. reader) ----

export const getVapidKey = () => apiFetch<{ key: string }>('/api/push/vapid-key')

export const registerPushSubscription = (body: {
  endpoint: string
  keys: { p256dh: string; auth: string }
  user_agent?: string
}) => apiFetch<PushSubscriptionInfo>('/api/push/subscriptions', { method: 'POST', body })

export const removePushSubscription = (endpoint: string) =>
  apiFetch<void>('/api/push/subscriptions', { method: 'DELETE', body: { endpoint } })

/** sendPushTest pushes to the CALLER's own devices only, bypassing their mutes —
 *  the "does this device actually work?" answer the settings panel promises. */
export const sendPushTest = () =>
  apiFetch<PushTestResult>('/api/push/test', { method: 'POST' })

export const getPushPreferences = () => apiFetch<PushPreferences>('/api/push/preferences')

export const updatePushPreferences = (body: {
  enabled?: boolean
  categories?: Partial<PushCategories>
}) => apiFetch<PushPreferences>('/api/push/preferences', { method: 'PATCH', body })

// ---- v5: admin notifications (admin only) ----

const adminBase = '/api/admin/notifications'

export const getNotificationCatalog = () => apiFetch<NotificationCatalog>(`${adminBase}/catalog`)

export const sendBroadcast = (body: { title: string; body: string; url?: string; audience: Audience }) =>
  apiFetch<SendResult>(`${adminBase}/broadcast`, { method: 'POST', body })

export const listNotificationRules = (params: { enabled?: boolean; limit?: number; cursor?: string } = {}) =>
  apiFetch<NotificationRulePage>(`${adminBase}/rules${qs(params)}`)
export const getNotificationRule = (id: string) => apiFetch<NotificationRule>(`${adminBase}/rules/${id}`)
export const createNotificationRule = (body: Record<string, unknown>) =>
  apiFetch<NotificationRule>(`${adminBase}/rules`, { method: 'POST', body })
export const updateNotificationRule = (id: string, body: Record<string, unknown>) =>
  apiFetch<NotificationRule>(`${adminBase}/rules/${id}`, { method: 'PATCH', body })
export const deleteNotificationRule = (id: string) =>
  apiFetch<void>(`${adminBase}/rules/${id}`, { method: 'DELETE' })
export const testNotificationRule = (id: string) =>
  apiFetch<SendResult>(`${adminBase}/rules/${id}/test`, { method: 'POST' })

export const listNotificationSchedules = (params: { enabled?: boolean; limit?: number; cursor?: string } = {}) =>
  apiFetch<NotificationSchedulePage>(`${adminBase}/schedules${qs(params)}`)
export const getNotificationSchedule = (id: string) => apiFetch<NotificationSchedule>(`${adminBase}/schedules/${id}`)
export const createNotificationSchedule = (body: Record<string, unknown>) =>
  apiFetch<NotificationSchedule>(`${adminBase}/schedules`, { method: 'POST', body })
export const updateNotificationSchedule = (id: string, body: Record<string, unknown>) =>
  apiFetch<NotificationSchedule>(`${adminBase}/schedules/${id}`, { method: 'PATCH', body })
export const deleteNotificationSchedule = (id: string) =>
  apiFetch<void>(`${adminBase}/schedules/${id}`, { method: 'DELETE' })
export const testNotificationSchedule = (id: string) =>
  apiFetch<SendResult>(`${adminBase}/schedules/${id}/test`, { method: 'POST' })

export interface DeliveryFilters {
  kind?: string
  status?: string
  rule_id?: string
  user?: string
  from?: string
  to?: string
  limit?: number
  cursor?: string
}
export const listDeliveries = (f: DeliveryFilters = {}) =>
  apiFetch<NotificationDeliveryPage>(`${adminBase}/deliveries${qs(f as Record<string, string>)}`)

// ---- Finance (v6) ----
//
// Reads are open to every member, `reader` included (D84); writes and the
// PERMANENT delete take the ordinary editor/admin gate server-side.

export const listFinanceMonths = (params: { limit?: number; cursor?: string } = {}) =>
  apiFetch<FinanceMonthPage>(`/api/finance/months${qs(params)}`)
export const getFinanceMonth = (id: string) => apiFetch<FinanceMonth>(`/api/finance/months/${id}`)
export const createFinanceMonth = (body: FinanceMonthInput) =>
  apiFetch<FinanceMonth>('/api/finance/months', { method: 'POST', body })
export const updateFinanceMonth = (id: string, body: Partial<FinanceMonthInput>) =>
  apiFetch<FinanceMonth>(`/api/finance/months/${id}`, { method: 'PATCH', body })
/** Removes the row outright — there is no archive and no restore (D87). */
export const deleteFinanceMonth = (id: string) =>
  apiFetch<void>(`/api/finance/months/${id}`, { method: 'DELETE' })
