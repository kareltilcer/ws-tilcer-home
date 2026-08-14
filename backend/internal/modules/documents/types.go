// Package documents implements Dokumenty (PRD §1/§4 FR-DOC1–DOC11, HANDOFF-6):
// files in their own folder tree, with the BYTES in object storage and only
// metadata in SQLite. Three properties shape everything here:
//
//   - Bytes are IMMUTABLE (D41). A document is written once and never replaced;
//     there is no PATCH field, no endpoint, and no store method that overwrites
//     content. A changed file is a new document with a new id.
//   - The permanent link is ID-based (D42), not slug-based: /d/{id} and the four
//     /api/documents/{id}/… content endpoints are stable for the document's life,
//     because neither the id nor the bytes ever change. The slug path is
//     navigation only and legitimately 404s after a rename or move.
//   - All content is HOUSEHOLD-ONLY (D33). Every endpoint, including the content
//     streams, is session-gated; R2 stays private and no presigned URL ever
//     reaches the browser.
//
// Every mutation writes an audit event in the same transaction — except personal
// pins, which are a per-user view preference (D47).
package documents

// ---- Wire types (match openapi.yaml 0.5.0 schemas) ----

// DocFolder is a node in the documents tree. parent_id NULL = root. This is the
// module's OWN tree, isolated from Poznámky's folders (D40).
type DocFolder struct {
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

// DocumentUrls are the permanent, household-only, id-based content URLs (D42).
// Stable for the document's life and unaffected by rename/move; all require the
// session cookie.
type DocumentUrls struct {
	Permalink string `json:"permalink"`
	Raw       string `json:"raw"`
	Download  string `json:"download"`
	Preview   string `json:"preview"`
	Thumbnail string `json:"thumbnail"`
}

// Document is one uploaded file's metadata. There is deliberately no field for
// content, and no version columns: the bytes behind checksum/storage_key are
// immutable (D41).
type Document struct {
	ID               string       `json:"id"`
	FolderID         *string      `json:"folder_id"`
	Title            string       `json:"title"`
	Slug             string       `json:"slug"`
	Description      *string      `json:"description"`
	OriginalFilename string       `json:"original_filename"`
	ContentType      string       `json:"content_type"`
	ByteSize         int64        `json:"byte_size"`
	Checksum         string       `json:"checksum"`
	PreviewKind      string       `json:"preview_kind"`
	PreviewStatus    string       `json:"preview_status"`
	Position         string       `json:"position"`
	Archived         bool         `json:"archived"`
	CreatedBy        *string      `json:"created_by"`
	CreatedAt        string       `json:"created_at"`
	UpdatedAt        string       `json:"updated_at"`
	Urls             DocumentUrls `json:"urls"`
}

// PinState is the caller's view of a document's pin state.
type PinState struct {
	Household bool `json:"household"`
	Personal  bool `json:"personal"`
}

// DocumentSummary is the lightweight document node for the tree/list/grid: enough
// to render a row or a card, with no description and no bytes.
type DocumentSummary struct {
	ID            string   `json:"id"`
	FolderID      *string  `json:"folder_id"`
	Title         string   `json:"title"`
	Slug          string   `json:"slug"`
	ContentType   string   `json:"content_type"`
	ByteSize      int64    `json:"byte_size"`
	PreviewKind   string   `json:"preview_kind"`
	PreviewStatus string   `json:"preview_status"`
	Position      string   `json:"position"`
	Archived      bool     `json:"archived"`
	UpdatedAt     string   `json:"updated_at"`
	ThumbnailURL  *string  `json:"thumbnail_url"`
	Pinned        PinState `json:"pinned"`
}

// PathSegment is one ancestor folder in a breadcrumb.
type PathSegment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// DocumentDetail is GET /api/documents/{id}: the document plus its breadcrumb,
// slug path, and the caller's pin state.
type DocumentDetail struct {
	Document
	Path     []PathSegment `json:"path"`
	SlugPath string        `json:"slug_path"`
	Pinned   PinState      `json:"pinned"`
}

// DocFolderDetail is GET /api/documents/folders/{id}.
type DocFolderDetail struct {
	DocFolder
	Path       []PathSegment     `json:"path"`
	SlugPath   string            `json:"slug_path"`
	Subfolders []DocFolder       `json:"subfolders"`
	Documents  []DocumentSummary `json:"documents"`
}

// DocFolderNode is one recursive node of GET /api/documents/tree.
type DocFolderNode struct {
	Folder     DocFolder         `json:"folder"`
	Subfolders []DocFolderNode   `json:"subfolders"`
	Documents  []DocumentSummary `json:"documents"`
}

// DocumentsTree is the whole navigation read model: top-level folders plus
// root-level (unfiled) documents. Assembled from two bounded queries, never N+1.
type DocumentsTree struct {
	Roots         []DocFolderNode   `json:"roots"`
	RootDocuments []DocumentSummary `json:"root_documents"`
	// MaxUploadMB is the operator-configured per-file cap (HOME_DOCS_MAX_UPLOAD_MB).
	// It rides on the tree because the upload UI needs it to refuse an over-cap file
	// before the transfer, and hardcoding it client-side silently breaks the moment an
	// operator raises the server's limit.
	MaxUploadMB int `json:"max_upload_mb"`
}

// DocumentPage is a document list / search result. The plain list is keyset-paged
// on (updated_at, id); search is capped and returns no cursor.
type DocumentPage struct {
	Items      []DocumentSummary `json:"items"`
	NextCursor *string           `json:"next_cursor"`
}

// DocResolveResult maps a slug path to a stable id (FR-DOC5). Note this is the
// NAVIGATION address: it 404s after a rename/move by design (no redirects, D32).
// The permanent address is the id-based one in DocumentUrls.
type DocResolveResult struct {
	Type     string `json:"type"` // "folder" | "document"
	ID       string `json:"id"`
	SlugPath string `json:"slug_path"`
}

// ---- Request DTOs ----

// DocumentUpdate is metadata only — there is no field for the bytes (D41).
type DocumentUpdate struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Archived    *bool   `json:"archived"`
}

type DocumentMoveRequest struct {
	FolderID *string `json:"folder_id"`
	Position string  `json:"position"`
}

type DocFolderCreate struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
	Icon     string  `json:"icon"`
}

type DocFolderUpdate struct {
	Name     *string `json:"name"`
	Archived *bool   `json:"archived"`
	Icon     *string `json:"icon"` // nil = leave unchanged
}

type DocFolderMoveRequest struct {
	ParentID *string `json:"parent_id"`
	Position string  `json:"position"`
}

type PinRequest struct {
	Scope string `json:"scope"`
}

// UploadMeta carries the multipart form's text fields (everything except the file
// part itself). The handler reads these BEFORE the file part while streaming.
type UploadMeta struct {
	FolderID    *string
	Title       string
	Description string
}

// UploadedFile describes the bytes the handler has already streamed to storage,
// handed to the service so it can commit the metadata row. The object is durable
// by the time this is built (FR-DOC1 ordering).
type UploadedFile struct {
	OriginalFilename string
	ContentType      string // server-sniffed (D48)
	ByteSize         int64
	Checksum         string // SHA-256 hex
	StorageKey       string
	PreviewKind      string
	PreviewStatus    string
	ThumbnailStatus  string
}

// ---- Widget payload (documents.pripnute — matches openapi PinnedDocument) ----

// PinnedDocument is one row of the Připnuté dokumenty widget.
type PinnedDocument struct {
	DocumentID    string  `json:"document_id"`
	Title         string  `json:"title"`
	SlugPath      string  `json:"slug_path"`
	Scope         string  `json:"scope"` // "household" | "personal" | "both"
	ContentType   string  `json:"content_type"`
	ByteSize      int64   `json:"byte_size"`
	PreviewKind   string  `json:"preview_kind"`
	PreviewStatus string  `json:"preview_status"`
	ThumbnailURL  *string `json:"thumbnail_url"`
	UpdatedAt     string  `json:"updated_at"`
	Position      string  `json:"position"`
}

// PripnuteDokumentyWidget is the documents.pripnute payload.
type PripnuteDokumentyWidget struct {
	Documents []PinnedDocument `json:"documents"`
}

// Pin scopes.
const (
	scopeHousehold = "household"
	scopePersonal  = "personal"
	scopeBoth      = "both"
)

// preview_kind values (openapi Document.preview_kind).
const (
	previewKindNative = "native" // the original previews directly: PDF, image, text
	previewKindPDF    = "pdf"    // a derived preview PDF exists (Office→PDF)
	previewKindNone   = "none"   // download-only
)

// preview_status values (openapi Document.preview_status).
const (
	previewPending = "pending"
	previewReady   = "ready"
	previewFailed  = "failed"
	previewNone    = "none"
)

// thumbnail_status values. SERVER-SIDE ONLY — deliberately absent from the API,
// where a thumbnail is announced by thumbnail_url and its absence needs no
// explanation (the UI falls back to a type icon either way).
//
// It exists because preview_status cannot answer "does this document still owe us a
// thumbnail?". An image or PDF is born "native"/"ready" — the original IS its
// preview — so a thumbnail job that dies has nothing left marking it undone, and the
// boot sweep over preview_status = 'pending' never looks at it again. This column is
// what makes a thumbnail retryable: 'pending' until a job settles it, and
// 'failed'/'none' are terminal so a missing cwebp cannot make every boot re-download
// every original.
const (
	thumbPending = "pending"
	thumbReady   = "ready"
	thumbFailed  = "failed"
	thumbNone    = "none"
)

// Storage key layout — id-based, so renames and moves never touch storage (D42).
const (
	keyPrefix        = "documents/"
	keyOriginal      = "original"
	keyPreviewPDF    = "preview.pdf"
	keyThumbnailWebP = "thumb.webp"
)

// OriginalKey, PreviewKey and ThumbnailKey build the three object keys of a document.
func OriginalKey(id string) string  { return keyPrefix + id + "/" + keyOriginal }
func PreviewKey(id string) string   { return keyPrefix + id + "/" + keyPreviewPDF }
func ThumbnailKey(id string) string { return keyPrefix + id + "/" + keyThumbnailWebP }
