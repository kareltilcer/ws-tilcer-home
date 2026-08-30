package chat

// Wire types for the `chat` tag of openapi.yaml 0.12.0.
//
// ⚠ THREE FIELDS ON Conversation ARE THE CALLER'S, NOT THE ROOM'S: unread_count,
// muted and effective_from. This struct is never rendered for anybody else and
// there is no route that would — which is what lets the list be one query per
// caller rather than a room projection plus a per-member overlay.
//
// ⚠ NOTHING HERE CARRIES AN EMAIL OR A ROLE. push.Member has both, and
// `/api/chat/directory` is the first surface in Home that shows the member
// directory to a non-admin (D230). The projection happens in this file's types by
// construction: there is no field to fill in.

// Conversation is a room as one member sees it.
type Conversation struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Name      string  `json:"name"`
	CreatedBy *string `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`

	// Present only under ?state=trash (D253). Elsewhere a trashed conversation is
	// not "listed with a flag" — it is invisible to every read.
	DeletedAt  *string `json:"deleted_at,omitempty"`
	PurgeAfter *string `json:"purge_after,omitempty"`

	MemberCount int  `json:"member_count"`
	UnreadCount int  `json:"unread_count"`
	Muted       bool `json:"muted"`

	// ⚠ The caller's read floor (D218) — NOT "when they joined". For Všichni it is
	// the conversation's own created_at (D258).
	EffectiveFrom string `json:"effective_from"`
	// ReadsFromBeginning is `effective_from_id = ''` — the floor IS the conversation's
	// beginning, so nothing is being withheld.
	//
	// ⚠ IT IS PUBLISHED BECAUSE THE UI WAS DERIVING IT FROM THE CLOCK (v10 review).
	// The floor line and the members panel decided "history was withheld" by comparing
	// effective_from against created_at, which is a SECOND SPELLING of a rule the
	// server states as an id bound — the exact thing floor.go forbids, one layer up.
	// The two disagree: adding somebody to a room with no messages yet writes
	// At = now and ID = "" (MAX(id) over an empty table), so the timestamps say
	// "withheld" over history that never existed. The id bound itself stays off the
	// wire — it names a message the caller may not read — so the ANSWER travels
	// rather than the bound.
	ReadsFromBeginning bool `json:"reads_from_beginning"`

	// Live attachment bytes. NULL when it could not be measured, never 0 — the
	// D161 principle: an unmeasured figure that renders as zero is a lie the page
	// cannot distinguish from an empty room. PR 3 fills it; until then it is null
	// everywhere and the frontend renders the unmeasured state.
	Bytes *int64 `json:"bytes"`
	// ⚠ A POINTER FOR THE REASON Bytes IS ONE (v10 review). It is a verdict ABOUT
	// Bytes, so it cannot be more certain than Bytes is: shipped as a plain bool it
	// serialised `false` on every room while `bytes` was still null, which is the
	// D161 lie one field up in boolean form — a definite "under the limit" derived
	// from a figure nobody has measured. Null until PR 3 sums the bytes.
	OverConversationLimit *bool `json:"over_conversation_limit"`

	// LastMessage is the row's preview line (v10.1, D266) — null when the caller
	// can see no message in this room at all.
	//
	// ⚠ NULL IS NOT "EMPTY ROOM". It is also a member whose floor sits above every
	// message written so far, which is an ordinary state of a group somebody was
	// added to five minutes ago — so the row renders the absence rather than a
	// blank, and never the newest message the ROOM has.
	LastMessage *ConversationPreview `json:"last_message"`
}

// ConversationPreview is the newest message the CALLER may read in a room.
//
// ⚠ IT IS A SECOND READ OF A MESSAGE, THROUGH THE SAME FLOOR, and it is the leak
// this feature most easily introduces — the same shape MessageQuote warns about one
// screen over. A "last message" taken as MAX(id) over the conversation is the newest
// message the ROOM has, which for a member added yesterday is a body they may not
// read, printed on the row they see before they open anything.
type ConversationPreview struct {
	ID          string `json:"id"`
	AuthorID    string `json:"author_id"`
	AuthorLabel string `json:"author_label"`
	// Excerpt is the first line, truncated — EMPTY on a tombstone and on a message
	// that carries only files. The client renders both from the flags below rather
	// than from a sentence the server wrote, so the row's copy stays in cs.ts with
	// every other Czech string in the app.
	Excerpt         string `json:"excerpt"`
	CreatedAt       string `json:"created_at"`
	Deleted         bool   `json:"deleted"`
	AttachmentCount int    `json:"attachment_count"`
}

// ConversationPage is the list response. Items is ALWAYS an array, never null (D174).
type ConversationPage struct {
	Items      []Conversation `json:"items"`
	NextCursor *string        `json:"next_cursor"`

	// TrashedCount is how many conversations the CALLER has in the koš — on both
	// listings, and describing the koš rather than this page (v10.1, D267).
	//
	// ⚠ IT RIDES HERE SO THE SIDEBAR CAN HIDE AN EMPTY KOŠ WITHOUT A SECOND REQUEST.
	// The section's rows are still fetched only when it is opened, which is the whole
	// reason `?state=trash` is lazy; knowing whether there is anything to open is a
	// scalar, and asking for it separately would undo that saving to answer a
	// yes/no question.
	TrashedCount int `json:"trashed_count"`
}

type ConversationCreate struct {
	Name      string   `json:"name"`
	MemberIDs []string `json:"member_ids"`
}

type ConversationUpdate struct {
	Name string `json:"name"`
}

// ConversationMember is one row of the members panel.
//
// Muted is a pointer because it is present only on the CALLER'S own row and null
// for everyone else's: mute is nobody else's business, and a bool would serialise
// somebody else's silence as `false`.
type ConversationMember struct {
	UserID        string `json:"user_id"`
	DisplayName   string `json:"display_name"`
	EffectiveFrom string `json:"effective_from"`
	// ReadsFromBeginning is this member's floor answered rather than implied — see
	// Conversation.ReadsFromBeginning for why the panel may not work it out from
	// effective_from itself.
	ReadsFromBeginning bool    `json:"reads_from_beginning"`
	AddedBy            *string `json:"added_by"`
	Muted              *bool   `json:"muted"`
}

type ConversationMemberList struct {
	Items []ConversationMember `json:"items"`
}

type ConversationMemberAdd struct {
	UserID string `json:"user_id"`
}

type ConversationMemberSelfUpdate struct {
	Muted *bool `json:"muted"`
}

// Message is one bubble.
//
// A tombstone is Deleted with an empty Body — the row survives so replies do not
// point at nothing and a thread somebody is reading does not silently reflow (D223).
type Message struct {
	ID             string        `json:"id"`
	ConversationID string        `json:"conversation_id"`
	AuthorID       string        `json:"author_id"`
	AuthorLabel    string        `json:"author_label"`
	Body           string        `json:"body"`
	ReplyTo        *MessageQuote `json:"reply_to,omitempty"`
	Attachments    []Attachment  `json:"attachments"`
	// Reactions is ALWAYS an array, never null (D174) — the empty one is a message
	// nobody has reacted to, which is most of them.
	Reactions []Reaction `json:"reactions"`
	CreatedAt string     `json:"created_at"`
	EditedAt  *string    `json:"edited_at"`
	Deleted   bool       `json:"deleted"`
}

// Reaction is one emoji's chip on one message (v10.1, D265).
//
// ⚠ THERE IS NO `mine` AND NO `count`, DELIBERATELY. The /ws frame carrying a
// Message is marshalled ONCE for the whole audience (ws.PublishTo), so a
// per-recipient field would be right for at most one of them — and a count beside a
// list is a second spelling of `len(By)` that can disagree with it. The client
// answers both questions off `By`, which is the only thing that is true for
// everybody.
type Reaction struct {
	Emoji string `json:"emoji"`
	// By is who reacted, oldest first. ALWAYS an array, never null.
	//
	// ⚠ It carries a display name, and that is not a new disclosure: every bubble
	// already prints `author_label`, and a reactor is a member of a conversation the
	// caller is in. What it must never carry is an email or a role — the D230 rule
	// the whole member directory is narrowed by.
	By []ReactionActor `json:"by"`
}

// ReactionActor is one person under one chip.
type ReactionActor struct {
	UserID string `json:"user_id"`
	Label  string `json:"label"`
}

// ReactionUpdate is the body of `PUT /api/chat/messages/{id}/reactions`.
//
// ⚠ Reacted IS THE DESIRED STATE, NOT A TOGGLE — see Store.SetReaction for why the
// double-tap gesture is what settles that.
//
// ⚠ AND IT IS A POINTER SO ITS ABSENCE IS NOT A DECISION (v10.1 review round 2). A
// plain bool decodes a body that never mentioned `reacted` as `false`, which this
// route executes as a REMOVAL — so a client that sent only the emoji watched its own
// chip disappear with a 200. The spec has always said `required: [emoji, reacted]`;
// nil is how the server can tell the difference and answer 422.
type ReactionUpdate struct {
	Emoji   string `json:"emoji"`
	Reacted *bool  `json:"reacted"`
}

// MessageQuote is the quoted parent of a reply.
//
// ⚠ IT IS A READ OF THE PARENT, NOT DATA BELONGING TO THE CHILD, and that is the
// whole reason it goes through the same membership-and-floor predicate as
// everything else (D226). Below the floor, Available is false and EVERY OTHER
// FIELD IS ABSENT — no author, no date, no excerpt. The `omitempty` tags are load-
// bearing rather than cosmetic: they are what makes the empty shape actually empty
// on the wire.
type MessageQuote struct {
	Available   bool   `json:"available"`
	ID          string `json:"id,omitempty"`
	AuthorLabel string `json:"author_label,omitempty"`
	Excerpt     string `json:"excerpt,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	Deleted     *bool  `json:"deleted,omitempty"`
}

// MessagePage is the thread.
//
// ⚠ NextCursor and HasMore are computed from the SAME predicate that produced
// Items — membership, floor, koš. A post-filter would leak what it removed through
// exactly these two fields even with the rows gone, which is why the tests assert
// them and not only the visible rows (D218).
type MessagePage struct {
	Items      []Message `json:"items"`
	NextCursor *string   `json:"next_cursor"`
	HasMore    bool      `json:"has_more"`
}

type MessageCreate struct {
	Body      string  `json:"body"`
	ReplyToID *string `json:"reply_to_id"`
}

type MessageUpdate struct {
	Body string `json:"body"`
}

// Attachment is PR 3's, and it is declared here because the message shape that
// carries it is this PR's contract. Until PR 3 the slice is always empty — never
// nil, because ALWAYS an array (D174).
type Attachment struct {
	ID               string  `json:"id"`
	Kind             string  `json:"kind"`
	State            string  `json:"state"`
	OriginalFilename string  `json:"original_filename"`
	ContentType      string  `json:"content_type"`
	ByteSize         int64   `json:"byte_size"`
	Width            *int    `json:"width"`
	Height           *int    `json:"height"`
	HasThumbnail     bool    `json:"has_thumbnail"`
	DocumentID       *string `json:"document_id"`
	DocumentPath     *string `json:"document_path"`
	UploadedBy       string  `json:"uploaded_by"`
	CreatedAt        string  `json:"created_at"`
	CleanedByLabel   *string `json:"cleaned_by_label"`
	CleanedAt        *string `json:"cleaned_at"`
}

type ReadUpdate struct {
	UntilMessageID string `json:"until_message_id"`
}

type ReadState struct {
	ConversationID string  `json:"conversation_id"`
	LastReadAt     *string `json:"last_read_at"`
	UnreadCount    int     `json:"unread_count"`
}

// SearchHit is one result.
//
// ⚠ Snippet IS a message body by another name. That is why the membership join and
// the floor ride inside the MATCH query rather than filtering its results (D251).
type SearchHit struct {
	MessageID        string `json:"message_id"`
	ConversationID   string `json:"conversation_id"`
	ConversationName string `json:"conversation_name"`
	AuthorLabel      string `json:"author_label"`
	Snippet          string `json:"snippet"`
	CreatedAt        string `json:"created_at"`
}

type SearchPage struct {
	Items      []SearchHit `json:"items"`
	NextCursor *string     `json:"next_cursor"`
}

// DirectoryEntry is the add-member picker's row. Display name only (D230).
type DirectoryEntry struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
}

type Directory struct {
	Items []DirectoryEntry `json:"items"`
}

// ---- websocket payloads ----

// MessageEvent is what rides /ws on a send, an edit and a delete.
//
// ⚠ THE FIRST /ws PAYLOAD IN HOME THAT CARRIES CONTENT, and it is safe only
// because the hub learned who is connected in PR 1 (D232/D233). Every other module
// publishes "something changed" to everybody; this one publishes the message
// itself to a named set.
type MessageEvent struct {
	ConversationID string  `json:"conversation_id"`
	Message        Message `json:"message"`

	// ⚠ PrevMessageID is the id of the message before this one in this
	// conversation, computed ONCE FOR THE WHOLE AUDIENCE (D259). A client whose
	// held latest does not match refetches the tail — which is how a frame the hub
	// dropped on a saturated socket becomes detectable, since there is no replay.
	//
	// ⚠ THE CHECK IS ONE-SHOT PER RECEIVED MESSAGE, and that is what makes it
	// terminate. A per-recipient value would mean one marshal per member and defeat
	// PublishTo's single marshal, so a member whose floor sits above this id can
	// never hold it: their FIRST message after joining always looks like a gap, they
	// refetch once, and from then on it matches. A client that re-checked after its
	// own refetch would loop on every message.
	PrevMessageID *string `json:"prev_message_id"`
}

// ConversationEvent is what rides /ws when a room's OWN state changes — renamed,
// moved to the koš, restored, purged.
//
// ⚠ IT CARRIES NO NAME, AND THAT IS THE POINT. The name is member-scoped content
// (audit.RedactMemberScoped exists for exactly that reason), so the frame says only
// "this room changed" and lets each client refetch through the membership join that
// is already the access rule. It is addressed to the room's members, resolved in
// the writing transaction like every other audience here (D233).
//
// ⚠ AND IT EXISTS BECAUSE THE STRUCTURAL VERBS PUBLISHED NOTHING (v10 review).
// Renaming a room left every other member's header naming the old one; trashing it
// left their thread rendering, their composer enabled, and their next send
// answering 404 from a predicate they had no way to see.
//
// `Gone` distinguishes "refetch this room" from "this room has left every read you
// have" — the trash and the purge — so a client sitting on /chat/{id} can leave
// rather than sit on a thread that will only ever 404.
type ConversationEvent struct {
	ConversationID string `json:"conversation_id"`
	Gone           bool   `json:"gone"`
}

// MembershipEvent is published TO THE REMOVED MEMBER SPECIFICALLY so their client
// can leave a thread that has quietly become forbidden.
//
// ⚠ No socket is force-closed and their already-fetched page is not scrubbed; the
// next request 404s. That is the accepted bound in leak row 22. PR 1's
// DisconnectSession is keyed per SESSION, which is the revocation axis — losing a
// conversation is not losing a session.
type MembershipEvent struct {
	ConversationID string `json:"conversation_id"`
	UserID         string `json:"user_id"`
	Removed        bool   `json:"removed"`
}

// AttachmentMove is the body of POST /api/chat/attachments/{id}/move.
//
// ⚠ ONE FIELD, AND IT MUST NAME A SHARED FOLDER. A private v9 folder is refused with
// 422 rather than being offered and greyed out (D245): a private target would make
// the file unreadable to the conversation's other members, which is the opposite of
// what the move is for. The refusal lives in `documents` — it is the only module
// that knows what makes one of its folders private.
type AttachmentMove struct {
	FolderID string `json:"folder_id"`
}
