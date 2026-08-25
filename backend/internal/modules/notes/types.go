// Package notes implements Poznámky (PRD §1/§4 FR-P1–P8, HANDOFF-5): Markdown
// notes in a single-parent folder tree, human-readable slug-path URLs
// (household-only), FTS5 search, two-scope pinning, and the notes.pripnute
// dashboard widget. Markdown is the single canonical body (D30). Every mutation
// writes an audit event in the same transaction — except personal pins, which
// are a per-user view preference (D35).
package notes

// ---- Wire types (match openapi.yaml 0.4.0 schemas) ----

// Folder is a node in the single-parent tree. parent_id NULL = root — of the root
// scope named by visibility/owner_id (v9).
type Folder struct {
	ID       string  `json:"id"`
	ParentID *string `json:"parent_id"`
	Name     string  `json:"name"`
	Slug     string  `json:"slug"`
	Icon     string  `json:"icon"` // optional emoji; "" = client shows the 📁 default
	Position string  `json:"position"`
	Archived bool    `json:"archived"`
	// Visibility is "shared" | "private" (v9, D177). REQUIRED on the wire, not
	// optional: an item whose visibility a client has to INFER is an item some
	// client will get wrong, and the cost of getting it wrong is showing a private
	// title without a lock.
	Visibility string `json:"visibility"`
	// OwnerID is the auth user id when private, NULL when shared.
	OwnerID   *string `json:"owner_id"`
	CreatedBy *string `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// Note carries the single canonical Markdown body (body_md, D30).
type Note struct {
	ID         string  `json:"id"`
	FolderID   *string `json:"folder_id"`
	Title      string  `json:"title"`
	Slug       string  `json:"slug"`
	BodyMD     *string `json:"body_md"`
	Position   string  `json:"position"`
	Archived   bool    `json:"archived"`
	Visibility string  `json:"visibility"` // "shared" | "private" (v9, D177)
	OwnerID    *string `json:"owner_id"`
	CreatedBy  *string `json:"created_by"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

// PinState is the caller's view of a note's pin state.
type PinState struct {
	Household bool `json:"household"`
	Personal  bool `json:"personal"`
}

// NoteSummary is the lightweight note node for the tree/list (no body).
type NoteSummary struct {
	ID         string   `json:"id"`
	FolderID   *string  `json:"folder_id"`
	Title      string   `json:"title"`
	Slug       string   `json:"slug"`
	Position   string   `json:"position"`
	Archived   bool     `json:"archived"`
	Visibility string   `json:"visibility"` // "shared" | "private" (v9) — drives the lock mark
	OwnerID    *string  `json:"owner_id"`
	UpdatedAt  string   `json:"updated_at"`
	Pinned     PinState `json:"pinned"`
}

// PathSegment is one ancestor folder in a breadcrumb.
type PathSegment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// NoteDetail is GET /api/notes/{id}: the note plus its breadcrumb, slug path, and
// the caller's pin state.
type NoteDetail struct {
	Note
	Path     []PathSegment `json:"path"`
	SlugPath string        `json:"slug_path"`
	Pinned   PinState      `json:"pinned"`
}

// FolderDetail is GET /api/notes/folders/{id}.
type FolderDetail struct {
	Folder
	Path       []PathSegment `json:"path"`
	SlugPath   string        `json:"slug_path"`
	Subfolders []Folder      `json:"subfolders"`
	Notes      []NoteSummary `json:"notes"`
}

// FolderNode is one recursive node of GET /api/notes/tree.
type FolderNode struct {
	Folder     Folder        `json:"folder"`
	Subfolders []FolderNode  `json:"subfolders"`
	Notes      []NoteSummary `json:"notes"`
}

// NotesTree is the whole navigation read model: top-level folders plus unfiled
// (root) notes.
type NotesTree struct {
	Roots     []FolderNode  `json:"roots"`
	RootNotes []NoteSummary `json:"root_notes"`
}

// NotePage is a note list / search result. Search is capped (LIMIT 100) and the
// folder listing is a full slice, so there is no cursor to page with.
type NotePage struct {
	Items []NoteSummary `json:"items"`
}

// ResolveResult maps a slug path to a stable id (path→id resolver, FR-P4).
type ResolveResult struct {
	Type     string `json:"type"` // "folder" | "note"
	ID       string `json:"id"`
	SlugPath string `json:"slug_path"`
}

// ---- Request DTOs ----

type NoteCreate struct {
	Title    string  `json:"title"`
	FolderID *string `json:"folder_id"`
	BodyMD   string  `json:"body_md"`
	// Scope selects the root when FolderID is null: "shared" (default) | "private".
	// It is honoured ONLY at the root — with a parent folder the parent's scope
	// governs and a disagreement is a 422, because a tree with mixed visibility
	// inside one folder is exactly what D177 rejected.
	//
	// ⚠ There is no owner_id field here and there must never be one: an owner comes
	// from the session, never from the body (§V9-3).
	Scope string `json:"scope"`
}

type NoteUpdate struct {
	Title    *string `json:"title"`
	BodyMD   *string `json:"body_md"`
	Archived *bool   `json:"archived"`
}

type NoteMoveRequest struct {
	FolderID *string `json:"folder_id"`
	Position string  `json:"position"`
}

type FolderCreate struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
	Icon     string  `json:"icon"`
	// Scope selects the root when ParentID is null — see NoteCreate.Scope. Missing
	// it here would mean the private root could not hold a folder at all.
	Scope string `json:"scope"`
}

type FolderUpdate struct {
	Name     *string `json:"name"`
	Archived *bool   `json:"archived"`
	Icon     *string `json:"icon"` // nil = leave unchanged
}

type FolderMoveRequest struct {
	ParentID *string `json:"parent_id"`
	Position string  `json:"position"`
}

// PinRequest's Scope is the PIN scope ("household" | "personal"), which is a
// different axis from the v9 root scope on NoteCreate/FolderCreate. The two
// interact in exactly one place: a household pin on a private note is a 422 (D183).
type PinRequest struct {
	Scope string `json:"scope"`
}

// PublishRequest is the destination of POST /api/notes/{id}/publish (v9, D182).
// Both fields optional: an empty body publishes to the shared ROOT, which is the
// common case — a member publishing something usually wants it visible, not filed.
type PublishRequest struct {
	FolderID *string `json:"folder_id"`
	Position string  `json:"position"`
}

// ---- Widget payload (notes.pripnute — matches openapi PinnedNote / PripnutePoznamkyWidget) ----

// PinnedNote is one row of the Připnuté poznámky widget.
type PinnedNote struct {
	NoteID   string `json:"note_id"`
	Title    string `json:"title"`
	SlugPath string `json:"slug_path"`
	Scope    string `json:"scope"` // "household" | "personal" | "both"
	// Visibility drives the widget row's lock mark (v9, D183). A private note can
	// only carry a PERSONAL pin, and only its owner's — so a row is only ever
	// "private" for the member looking at it.
	Visibility string  `json:"visibility"`
	Excerpt    *string `json:"excerpt"`
	UpdatedAt  string  `json:"updated_at"`
	Position   string  `json:"position"`
}

// PripnutePoznamkyWidget is the notes.pripnute payload.
type PripnutePoznamkyWidget struct {
	Notes []PinnedNote `json:"notes"`
}

const (
	scopeHousehold = "household"
	scopePersonal  = "personal"
	scopeBoth      = "both"
)

// ---- Inline images (note-images/{id}) ----

// Storage/URL layout is id-based, mirroring documents (D42): the id and the bytes
// never change, so the content URL is stable for the image's life. The bytes live
// in object storage under this prefix; body_md holds only the small reference URL.
const (
	noteImageKeyPrefix = "note-images/"
	noteImageAPIBase   = "/api/notes/images/"
)

// NoteImageKey is the object-storage key for an image id (one object per image;
// no previews or thumbnails, unlike documents).
func NoteImageKey(id string) string { return noteImageKeyPrefix + id }

// noteImageURL is the household-only content URL embedded in body_md as
// `![](<url>)`. Served THROUGH the backend so it stays session-gated (D33/D42).
func noteImageURL(id string) string { return noteImageAPIBase + id }

// NoteImageUploadResult is the POST /api/notes/{id}/images response: enough for the
// editor to insert `![](url)` and show what it stored.
type NoteImageUploadResult struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	ByteSize    int64  `json:"byte_size"`
}
