// Chat (v10) — the `chat` tag of openapi.yaml 0.12.0.
//
// ⚠ THREE FIELDS ON Conversation ARE THE CALLER'S, NOT THE ROOM'S: unreadCount,
// muted and effectiveFrom. This shape is never rendered for anybody else and there
// is no route that would, which is what lets the list be one request rather than a
// room projection plus a per-member overlay.
//
// ⚠ AND NOTHING HERE CARRIES AN EMAIL OR A ROLE. The backend narrows the member
// directory at its own boundary (D230) — `/api/chat/directory` is the first surface
// in Home that shows it to a non-admin — so there is no field here to fill in even
// by accident.

export type ConversationKind = 'default' | 'group'
export type AttachmentState = 'live' | 'moved' | 'removed'
export type AttachmentKind = 'image' | 'video' | 'file'

export interface Conversation {
  id: string
  kind: ConversationKind
  name: string
  created_by: string | null
  created_at: string
  updated_at: string
  /** Non-null ⇔ in the koš. Present only under ?state=trash. */
  deleted_at?: string | null
  /** When the drain will destroy this conversation's bytes. */
  purge_after?: string | null
  member_count: number
  unread_count: number
  muted: boolean
  /**
   * ⚠ The caller's read floor — the instant from which this conversation is theirs
   * to read. NOT "when they joined": for Všichni it is the conversation's own
   * created_at, so a member the app sees for the first time years later reads the
   * household room in full (D258).
   */
  effective_from: string
  /**
   * True when the caller's floor IS the conversation's beginning, so nothing is
   * being withheld.
   *
   * ⚠ BRANCH ON THIS, NEVER ON `effective_from` AGAINST `created_at`. The server's
   * floor is an id bound, and re-deriving it from the clock here is a second
   * spelling of one access rule — the thing floor.go forbids, one layer up. The two
   * disagree: adding somebody to a room that has no messages yet writes
   * `effective_from = now` over an empty bound, so the timestamps claim history was
   * withheld when none existed.
   */
  reads_from_beginning: boolean
  /** Live attachment bytes. Null when unmeasured — never 0 (the D161 principle). */
  bytes: number | null
  /**
   * ⚠ Nullable for the reason `bytes` is: it is a verdict about `bytes`, so it
   * cannot be more certain than the figure it judges. Null until PR 3 measures.
   */
  over_conversation_limit: boolean | null
}

export interface ConversationPage {
  items: Conversation[]
  next_cursor: string | null
}

export interface ConversationCreate {
  name: string
  member_ids?: string[]
}

export interface ConversationMember {
  user_id: string
  display_name: string
  /**
   * This member's read floor. Shown in the panel so the app can say plainly that
   * somebody added yesterday cannot read last week — a fact nothing else in the UI
   * would explain.
   */
  effective_from: string
  /**
   * True when this member's floor IS the conversation's beginning, so they read all
   * of it — see the same field on `Conversation` for why the panel branches on this
   * rather than on the two timestamps.
   */
  reads_from_beginning: boolean
  added_by: string | null
  /** Present only on the caller's own row. Mute is nobody else's business. */
  muted: boolean | null
}

export interface ConversationMemberList {
  items: ConversationMember[]
}

/**
 * The quoted parent of a reply.
 *
 * ⚠ When `available` is false EVERY OTHER FIELD IS ABSENT — no author, no date, no
 * excerpt (D226). Render the fixed empty shape, never a partial quote: "Kája, 3. 8."
 * with the text removed still says who was talking to whom and when.
 */
export interface MessageQuote {
  available: boolean
  id?: string
  author_label?: string
  excerpt?: string
  created_at?: string
  deleted?: boolean
}

export interface Attachment {
  id: string
  kind: AttachmentKind
  state: AttachmentState
  original_filename: string
  content_type: string
  byte_size: number
  width: number | null
  height: number | null
  has_thumbnail: boolean
  document_id: string | null
  document_path: string | null
  uploaded_by: string
  created_at: string
  cleaned_by_label: string | null
  cleaned_at: string | null
}

export interface ChatMessage {
  id: string
  conversation_id: string
  author_id: string
  author_label: string
  /** Empty on a tombstone — the delete blanks it in place (D223). */
  body: string
  reply_to?: MessageQuote
  attachments: Attachment[]
  created_at: string
  /** Non-null ⇒ render *upraveno*. ⚠ There is no record of what it said before. */
  edited_at: string | null
  deleted: boolean
}

export interface MessagePage {
  items: ChatMessage[]
  next_cursor: string | null
  has_more: boolean
}

export interface MessageCreate {
  body: string
  reply_to_id?: string | null
}

export interface ReadState {
  conversation_id: string
  last_read_at: string | null
  unread_count: number
}

export interface SearchHit {
  message_id: string
  conversation_id: string
  conversation_name: string
  author_label: string
  snippet: string
  created_at: string
}

export interface SearchPage {
  items: SearchHit[]
  next_cursor: string | null
}

export interface DirectoryEntry {
  user_id: string
  display_name: string
}

export interface Directory {
  items: DirectoryEntry[]
}

// ---- /ws payloads ----

/**
 * What rides /ws on a send, an edit and a delete.
 *
 * ⚠ THE FIRST /ws PAYLOAD IN HOME THAT CARRIES CONTENT. Every other module publishes
 * "something changed" to every connected client; this one is fanned out to the
 * conversation's members only, which is safe because the hub learned who is
 * connected in v10 (D232/D233).
 */
export interface ChatMessageEvent {
  conversation_id: string
  message: ChatMessage
  /**
   * The id of the message before this one in that conversation, computed ONCE for
   * the whole audience (D259). A client whose held latest does not match refetches
   * the tail — which is how a frame the hub dropped on a saturated socket becomes
   * detectable, since there is no replay.
   *
   * ⚠ THE CHECK IS ONE-SHOT PER RECEIVED MESSAGE. A member whose floor sits above
   * this id can never hold it, so their FIRST message after joining always looks
   * like a gap; they refetch once and match from then on. A client that re-checked
   * after its own refetch would loop on every message forever.
   */
  prev_message_id: string | null
}

export interface ChatMembershipEvent {
  conversation_id: string
  user_id: string
  removed: boolean
}

/**
 * A room's OWN state changed — renamed, moved to the koš, restored, purged.
 *
 * ⚠ IT CARRIES NO NAME. A conversation's name is readable by that conversation's
 * members, so the frame says only that something moved and each client refetches
 * through the membership join that is already the access rule.
 *
 * `gone` is the trash and the purge: the room has left every read its members
 * have, so a client sitting on /chat/{id} leaves rather than staying on a thread
 * whose composer still looks usable and whose next send 404s.
 */
export interface ConversationEvent {
  conversation_id: string
  gone: boolean
}

// ---- PR 3: attachments, storage, clean-up ----

/**
 * The caller's chat storage picture (`GET /api/chat/storage`).
 *
 * ⚠ THE TOTAL IS THE HOUSEHOLD'S AND THE ROWS ARE THE CALLER'S. Everyone sees the
 * same `total_bytes`, because `chat.total` is a threshold about the bucket and a
 * warning nobody can see is not a warning; the per-conversation rows cover only
 * conversations the caller belongs to.
 *
 * ⚠ AND CONVERSATIONS IN THE KOŠ ARE COUNTED. Their bytes are still in R2, and
 * these figures have to sum — which is why *Smazat natrvalo* exists at all.
 */
export interface ChatStorage {
  total_bytes: number | null
  threshold_total_mb: number
  total_exceeded: boolean
  threshold_conversation_mb: number
  conversations: ChatStorageConversation[]
  /**
   * D241's gate, answered by the server rather than re-derived here.
   *
   * ⚠ BRANCH THE WARNING'S LINK ON THIS. The rule is member ∧ (editor|admin) and
   * the client holds only half of it, so a banner that guessed would offer a reader
   * a link that 403s them — the one thing the copy is explicitly not allowed to do.
   */
  can_clean_up: boolean
  /**
   * The per-file cap in MB — Dokumenty's, shared on purpose (D228).
   *
   * ⚠ THE COMPOSER REFUSES AGAINST THIS RATHER THAN AGAINST A CONSTANT. An operator
   * who raises HOME_DOCS_MAX_UPLOAD_MB raises it for chat too, and a hard-coded 50
   * here would refuse files the server would take, naming a limit that is not the
   * limit.
   */
  max_upload_mb: number
  /**
   * Whether a `storage.BlobSink` is wired at all (D239).
   *
   * ⚠ GATE THE MOVE BUTTON ON THIS, NOT ON `can_clean_up`. The two answer different
   * questions — one is a role gate, the other is whether the capability exists in
   * this deployment — and D239 requires an absent capability to LOOK absent rather
   * than to open a dialog and 501 after the confirm.
   */
  move_available: boolean
}

export interface ChatStorageConversation {
  id: string
  name: string
  bytes: number | null
  objects: number | null
  over_limit: boolean
}

/** One row of Úklid úložiště chatu. */
export interface CleanupItem {
  attachment: Attachment
  conversation_id: string
  conversation_name: string
  conversation_over_limit: boolean
  uploaded_by_label: string
}

export interface CleanupPage {
  items: CleanupItem[]
  /** ⚠ Always null under `sort=size`, which is single-page by construction. */
  next_cursor: string | null
  /** Across ALL matching items, not this page — the figure the screen acts on. */
  total_bytes: number | null
}
