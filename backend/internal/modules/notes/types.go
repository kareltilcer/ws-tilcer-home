// Package notes implements Poznámky (PRD §1/§4 FR-P1–P8, HANDOFF-5): Markdown
// notes in a single-parent folder tree, human-readable slug-path URLs
// (household-only), FTS5 search, two-scope pinning, and the notes.pripnute
// dashboard widget. Markdown is the single canonical body (D30). Every mutation
// writes an audit event in the same transaction — except personal pins, which
// are a per-user view preference (D35).
package notes

// ---- Wire types (match openapi.yaml 0.4.0 schemas) ----

// Folder is a node in the single-parent tree. parent_id NULL = root.
type Folder struct {
	ID        string  `json:"id"`
	ParentID  *string `json:"parent_id"`
	Name      string  `json:"name"`
	Slug      string  `json:"slug"`
	Icon      string  `json:"icon"` // optional emoji; "" = client shows the 📁 default
	Position  string  `json:"position"`
	Archived  bool    `json:"archived"`
	CreatedBy *string `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// Note carries the single canonical Markdown body (body_md, D30).
type Note struct {
	ID        string  `json:"id"`
	FolderID  *string `json:"folder_id"`
	Title     string  `json:"title"`
	Slug      string  `json:"slug"`
	BodyMD    *string `json:"body_md"`
	Position  string  `json:"position"`
	Archived  bool    `json:"archived"`
	CreatedBy *string `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// PinState is the caller's view of a note's pin state.
type PinState struct {
	Household bool `json:"household"`
	Personal  bool `json:"personal"`
}

// NoteSummary is the lightweight note node for the tree/list (no body).
type NoteSummary struct {
	ID        string   `json:"id"`
	FolderID  *string  `json:"folder_id"`
	Title     string   `json:"title"`
	Slug      string   `json:"slug"`
	Position  string   `json:"position"`
	Archived  bool     `json:"archived"`
	UpdatedAt string   `json:"updated_at"`
	Pinned    PinState `json:"pinned"`
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

type PinRequest struct {
	Scope string `json:"scope"`
}

// ---- Widget payload (notes.pripnute — matches openapi PinnedNote / PripnutePoznamkyWidget) ----

// PinnedNote is one row of the Připnuté poznámky widget.
type PinnedNote struct {
	NoteID    string  `json:"note_id"`
	Title     string  `json:"title"`
	SlugPath  string  `json:"slug_path"`
	Scope     string  `json:"scope"` // "household" | "personal" | "both"
	Excerpt   *string `json:"excerpt"`
	UpdatedAt string  `json:"updated_at"`
	Position  string  `json:"position"`
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
