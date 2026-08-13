package documents

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lexorank"
)

// DBTX is satisfied by *sql.DB and *sql.Tx. Reads outside a mutation use the
// store's *sql.DB; everything inside a mutation takes the explicit tx so the audit
// write commits atomically with the change.
//
// IMPORTANT: inside a WithTx callback every read must go through the tx. The pool
// is capped at one connection (platform/db), so a pooled read from inside a
// transaction deadlocks against its own write lock.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// tsFormat is RFC 3339 with fixed-width microseconds: zero-padded so string
// comparison stays chronological for ORDER BY updated_at and for the keyset
// cursor, which compares (updated_at, id) pairs lexically.
const tsFormat = "2006-01-02T15:04:05.000000Z07:00"

func nowUTC() string { return time.Now().UTC().Format(tsFormat) }

// ParseTS parses a stored timestamp (used by the reconciliation pass to compare a
// row's age against the orphan grace window).
func ParseTS(s string) (time.Time, error) { return time.Parse(tsFormat, s) }

func ptr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ---- Folders ----

const folderCols = `id, parent_id, name, slug, position, archived, created_by, created_at, updated_at`

func scanFolder(r interface{ Scan(...any) error }) (DocFolder, error) {
	var f DocFolder
	var parent, createdBy sql.NullString
	var archived int
	if err := r.Scan(&f.ID, &parent, &f.Name, &f.Slug, &f.Position, &archived, &createdBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return DocFolder{}, err
	}
	f.ParentID = ptr(parent)
	f.CreatedBy = ptr(createdBy)
	f.Archived = archived != 0
	return f, nil
}

func (s *Store) GetFolder(ctx context.Context, q DBTX, id string) (*DocFolder, error) {
	row := q.QueryRowContext(ctx, `SELECT `+folderCols+` FROM document_folders WHERE id = ?`, id)
	f, err := scanFolder(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *Store) InsertFolder(ctx context.Context, tx DBTX, parentID *string, name, slug, position, createdBy string) (*DocFolder, error) {
	id := idgen.New()
	now := nowUTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO document_folders (id, parent_id, name, slug, position, archived, created_by, created_at, updated_at)
		 VALUES (?,?,?,?,?,0,?,?,?)`,
		id, nullable(deref(parentID)), name, slug, position, nullable(createdBy), now, now); err != nil {
		return nil, err
	}
	return s.GetFolder(ctx, tx, id)
}

// RenameFolder updates name+slug (the folder's URL changes; D32).
func (s *Store) RenameFolder(ctx context.Context, tx DBTX, id, name, slug string) error {
	_, err := tx.ExecContext(ctx, `UPDATE document_folders SET name = ?, slug = ?, updated_at = ? WHERE id = ?`,
		name, slug, nowUTC(), id)
	return err
}

// MoveFolderRow reparents and/or reorders a folder, possibly with a fresh slug.
// Exactly ONE row is written — descendants keep their own slugs, so a subtree is
// never rewritten (and no object storage is touched: keys are id-based).
func (s *Store) MoveFolderRow(ctx context.Context, tx DBTX, id string, parentID *string, position, slug string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE document_folders SET parent_id = ?, position = ?, slug = ?, updated_at = ? WHERE id = ?`,
		nullable(deref(parentID)), position, slug, nowUTC(), id)
	return err
}

func (s *Store) SetFolderArchived(ctx context.Context, tx DBTX, id string, archived bool) error {
	_, err := tx.ExecContext(ctx, `UPDATE document_folders SET archived = ?, updated_at = ? WHERE id = ?`,
		boolInt(archived), nowUTC(), id)
	return err
}

func (s *Store) DeleteFolder(ctx context.Context, tx DBTX, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM document_folders WHERE id = ?`, id)
	return err
}

func (s *Store) ChildFolderBySlug(ctx context.Context, q DBTX, parentID *string, slug string) (*DocFolder, error) {
	row := q.QueryRowContext(ctx,
		`SELECT `+folderCols+` FROM document_folders WHERE COALESCE(parent_id,'') = ? AND slug = ? AND archived = 0`,
		deref(parentID), slug)
	f, err := scanFolder(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *Store) lastFolderPosition(ctx context.Context, tx DBTX, parentID *string) (string, error) {
	var pos sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(position) FROM document_folders WHERE COALESCE(parent_id,'') = ? AND archived = 0`,
		deref(parentID)).Scan(&pos); err != nil {
		return "", err
	}
	if !pos.Valid {
		return lexorank.First(), nil
	}
	return lexorank.Tail(pos.String), nil
}

// FolderChildCounts returns the LIVE subfolder and document counts behind the
// non-empty delete guard (409 + counts unless ?cascade=true). Live only, for both
// delete modes: cascade=true is the caller acknowledging the children it was shown,
// and the tree only ever shows live rows. Archived descendants are handled by the
// hard delete's own guard in DeleteFolder, which refuses instead of counting them.
func (s *Store) FolderChildCounts(ctx context.Context, q DBTX, folderID string) (subfolders, documents int, err error) {
	if err = q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM document_folders WHERE parent_id = ? AND archived = 0`, folderID).Scan(&subfolders); err != nil {
		return
	}
	err = q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM documents WHERE folder_id = ? AND archived = 0`, folderID).Scan(&documents)
	return
}

// FolderMeta is the (name, archived) pair the cascade delete needs per folder.
type FolderMeta struct {
	Name     string
	Archived bool
}

// FolderMetaByIDs returns id → (name, archived) for the given ids in one query, so
// a cascade delete can caption its audit events and skip already-archived rows
// without an N+1 GetFolder per descendant.
func (s *Store) FolderMetaByIDs(ctx context.Context, q DBTX, ids []string) (map[string]FolderMeta, error) {
	out := map[string]FolderMeta{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx,
		`SELECT id, name, archived FROM document_folders WHERE id IN (`+placeholders(len(ids))+`)`, toArgs(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		var archived int
		if err := rows.Scan(&id, &name, &archived); err != nil {
			return nil, err
		}
		out[id] = FolderMeta{Name: name, Archived: archived != 0}
	}
	return out, rows.Err()
}

// DescendantFolderIDs returns rootID plus every descendant folder id (BFS over
// parent_id), so a cascade costs one query per tree LEVEL rather than per folder.
// includeArchived=false limits the walk to live folders (soft cascade); true covers
// the whole physical subtree (hard delete, which must find every object to purge).
func (s *Store) DescendantFolderIDs(ctx context.Context, q DBTX, rootID string, includeArchived bool) ([]string, error) {
	filter := " AND archived = 0"
	if includeArchived {
		filter = ""
	}
	out := []string{rootID}
	frontier := []string{rootID}
	// visited breaks any parent_id cycle (a manual DB edit or a restore anomaly) so
	// the BFS cannot loop forever while holding the delete's write lock.
	visited := map[string]bool{rootID: true}
	for len(frontier) > 0 {
		rows, err := q.QueryContext(ctx,
			`SELECT id FROM document_folders WHERE parent_id IN (`+placeholders(len(frontier))+`)`+filter,
			toArgs(frontier)...)
		if err != nil {
			return nil, err
		}
		var next []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			if visited[id] {
				continue
			}
			visited[id] = true
			next = append(next, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		out = append(out, next...)
		frontier = next
	}
	return out, nil
}

// DocumentsInFolders returns the documents contained directly in any of the given
// folders, WITH their object keys — a hard cascade needs both the titles (to audit
// each purged descendant) and the keys (to purge its bytes).
// includeArchived=false returns only live rows (soft cascade); true returns the
// whole physical subtree.
func (s *Store) DocumentsInFolders(ctx context.Context, q DBTX, folderIDs []string, includeArchived bool) ([]storedDocument, error) {
	if len(folderIDs) == 0 {
		return nil, nil
	}
	filter := " AND archived = 0"
	if includeArchived {
		filter = ""
	}
	rows, err := q.QueryContext(ctx,
		`SELECT `+documentCols+` FROM documents WHERE folder_id IN (`+placeholders(len(folderIDs))+`)`+filter,
		toArgs(folderIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storedDocument
	for rows.Next() {
		d, err := scanStoredDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ---- Documents ----

const documentCols = `id, folder_id, title, slug, description, original_filename, content_type, byte_size,
	checksum, storage_key, preview_kind, preview_status, preview_key, thumbnail_key, position, archived,
	created_by, created_at, updated_at`

// storedDocument is a Document plus the columns that stay server-side: the object
// keys. They are never serialised — the browser addresses content through the
// backend's id-based URLs, never through storage keys (D33/D42).
type storedDocument struct {
	Document
	StorageKey   string
	PreviewKey   *string
	ThumbnailKey *string
}

func scanDocument(r interface{ Scan(...any) error }) (Document, error) {
	d, err := scanStoredDocument(r)
	return d.Document, err
}

func scanStoredDocument(r interface{ Scan(...any) error }) (storedDocument, error) {
	var sd storedDocument
	var folder, description, createdBy, previewKey, thumbKey sql.NullString
	var archived int
	if err := r.Scan(&sd.ID, &folder, &sd.Title, &sd.Slug, &description, &sd.OriginalFilename,
		&sd.ContentType, &sd.ByteSize, &sd.Checksum, &sd.StorageKey, &sd.PreviewKind, &sd.PreviewStatus,
		&previewKey, &thumbKey, &sd.Position, &archived, &createdBy, &sd.CreatedAt, &sd.UpdatedAt); err != nil {
		return storedDocument{}, err
	}
	sd.FolderID = ptr(folder)
	sd.Description = ptr(description)
	sd.CreatedBy = ptr(createdBy)
	sd.Archived = archived != 0
	sd.PreviewKey = ptr(previewKey)
	sd.ThumbnailKey = ptr(thumbKey)
	sd.Urls = urlsFor(sd.ID)
	return sd, nil
}

func (s *Store) GetDocument(ctx context.Context, q DBTX, id string) (*Document, error) {
	sd, err := s.GetStoredDocument(ctx, q, id)
	if err != nil || sd == nil {
		return nil, err
	}
	return &sd.Document, nil
}

// GetStoredDocument also returns the object keys — used by the content endpoints,
// the preview worker, and the purge path.
func (s *Store) GetStoredDocument(ctx context.Context, q DBTX, id string) (*storedDocument, error) {
	row := q.QueryRowContext(ctx, `SELECT `+documentCols+` FROM documents WHERE id = ?`, id)
	sd, err := scanStoredDocument(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sd, nil
}

// InsertDocument writes the metadata row for bytes that are ALREADY durable in
// object storage (FR-DOC1 step 4: object first, row second). id must be the id the
// storage keys were built from.
func (s *Store) InsertDocument(ctx context.Context, tx DBTX, id string, folderID *string, title, slug, description string, f UploadedFile, position, createdBy string) (*Document, error) {
	now := nowUTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO documents (id, folder_id, title, slug, description, original_filename, content_type,
		    byte_size, checksum, storage_key, preview_kind, preview_status, thumbnail_status, position, archived,
		    created_by, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,?,?)`,
		id, nullable(deref(folderID)), title, slug, nullable(description), f.OriginalFilename, f.ContentType,
		f.ByteSize, f.Checksum, f.StorageKey, f.PreviewKind, f.PreviewStatus, f.ThumbnailStatus, position,
		nullable(createdBy), now, now); err != nil {
		return nil, err
	}
	return s.GetDocument(ctx, tx, id)
}

// documentPatch carries the fields a PATCH may change. There is deliberately no
// entry for content_type, byte_size, checksum or storage_key: the bytes are
// immutable (D41), so no update path can touch them.
type documentPatch struct {
	Title       *string
	Slug        *string
	Description *string
	Archived    *bool
}

func (s *Store) UpdateDocument(ctx context.Context, tx DBTX, id string, u documentPatch) error {
	var sets []string
	var args []any
	if u.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *u.Title)
	}
	if u.Slug != nil {
		sets = append(sets, "slug = ?")
		args = append(args, *u.Slug)
	}
	if u.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, nullable(*u.Description))
	}
	if u.Archived != nil {
		sets = append(sets, "archived = ?")
		args = append(args, boolInt(*u.Archived))
	}
	// An all-nil patch (PATCH {}) writes nothing, so a no-op never bumps updated_at
	// — which would otherwise reorder the list and invalidate caches for a change
	// that did not happen. Callers gate the audit record on the diff too.
	if len(sets) == 0 {
		return nil
	}
	sets = append([]string{"updated_at = ?"}, sets...)
	args = append([]any{nowUTC()}, args...)
	args = append(args, id)
	_, err := tx.ExecContext(ctx, `UPDATE documents SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

// MoveDocumentRow reparents and/or reorders a document, possibly with a fresh
// slug. One row; the object keys are id-based, so storage is untouched (D42).
func (s *Store) MoveDocumentRow(ctx context.Context, tx DBTX, id string, folderID *string, position, slug string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE documents SET folder_id = ?, position = ?, slug = ?, updated_at = ? WHERE id = ?`,
		nullable(deref(folderID)), position, slug, nowUTC(), id)
	return err
}

func (s *Store) DeleteDocument(ctx context.Context, tx DBTX, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, id)
	return err
}

func (s *Store) ChildDocumentBySlug(ctx context.Context, q DBTX, folderID *string, slug string) (*Document, error) {
	row := q.QueryRowContext(ctx,
		`SELECT `+documentCols+` FROM documents WHERE COALESCE(folder_id,'') = ? AND slug = ? AND archived = 0`,
		deref(folderID), slug)
	d, err := scanDocument(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) lastDocumentPosition(ctx context.Context, tx DBTX, folderID *string) (string, error) {
	var pos sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(position) FROM documents WHERE COALESCE(folder_id,'') = ? AND archived = 0`,
		deref(folderID)).Scan(&pos); err != nil {
		return "", err
	}
	if !pos.Valid {
		return lexorank.First(), nil
	}
	return lexorank.Tail(pos.String), nil
}

// SiblingSlugTaken reports whether a live folder OR document under parentScope
// already uses slug, excluding the given ids (the item being renamed/moved). This
// is the in-transaction cross-table half of the addressing invariant (D32/D40)
// that no single index can express; the two COALESCE partial indexes cover the
// within-table half.
func (s *Store) SiblingSlugTaken(ctx context.Context, q DBTX, parentScope *string, slug, excludeFolderID, excludeDocumentID string) (bool, error) {
	scope := deref(parentScope)
	var n int
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM document_folders WHERE COALESCE(parent_id,'') = ? AND slug = ? AND archived = 0 AND id != ?`,
		scope, slug, excludeFolderID).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM documents WHERE COALESCE(folder_id,'') = ? AND slug = ? AND archived = 0 AND id != ?`,
		scope, slug, excludeDocumentID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// SearchDocuments runs an FTS5 MATCH over title + original_filename + description
// — METADATA ONLY; file contents are not indexed (D46). Newest-updated first,
// capped. Reads only.
func (s *Store) SearchDocuments(ctx context.Context, query string, folderID *string, includeArchived bool, limit int) ([]DocumentSummary, error) {
	conds := []string{"documents_fts MATCH ?"}
	args := []any{query}
	if !includeArchived {
		conds = append(conds, "d.archived = 0")
	}
	if folderID != nil {
		conds = append(conds, "COALESCE(d.folder_id,'') = ?")
		args = append(args, *folderID)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+summaryColsPrefixed+`
		 FROM documents_fts f JOIN documents d ON d.rowid = f.rowid
		 WHERE `+strings.Join(conds, " AND ")+`
		 ORDER BY d.updated_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocumentSummary{}
	for rows.Next() {
		sm, err := scanDocumentSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sm)
	}
	return out, rows.Err()
}

// ListDocuments returns a page of documents ordered newest-updated first, keyset-
// paged on the composite (updated_at, id) so a row is never returned twice or
// skipped when timestamps collide. cursor is "<updated_at>|<id>"; empty starts at
// the newest.
func (s *Store) ListDocuments(ctx context.Context, folderID *string, includeArchived bool, limit int, cursorTS, cursorID string) ([]DocumentSummary, error) {
	conds := []string{"1=1"}
	var args []any
	if !includeArchived {
		conds = append(conds, "d.archived = 0")
	}
	if folderID != nil {
		conds = append(conds, "COALESCE(d.folder_id,'') = ?")
		args = append(args, *folderID)
	}
	if cursorTS != "" {
		conds = append(conds, "(d.updated_at < ? OR (d.updated_at = ? AND d.id < ?))")
		args = append(args, cursorTS, cursorTS, cursorID)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+summaryColsPrefixed+` FROM documents d
		 WHERE `+strings.Join(conds, " AND ")+`
		 ORDER BY d.updated_at DESC, d.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocumentSummary{}
	for rows.Next() {
		sm, err := scanDocumentSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sm)
	}
	return out, rows.Err()
}

// ---- Tree data ----

const summaryCols = `id, folder_id, title, slug, content_type, byte_size, preview_kind, preview_status,
	thumbnail_key, position, archived, updated_at`

const summaryColsPrefixed = `d.id, d.folder_id, d.title, d.slug, d.content_type, d.byte_size, d.preview_kind,
	d.preview_status, d.thumbnail_key, d.position, d.archived, d.updated_at`

func scanDocumentSummary(r interface{ Scan(...any) error }) (DocumentSummary, error) {
	var sm DocumentSummary
	var folder, thumbKey sql.NullString
	var archived int
	if err := r.Scan(&sm.ID, &folder, &sm.Title, &sm.Slug, &sm.ContentType, &sm.ByteSize,
		&sm.PreviewKind, &sm.PreviewStatus, &thumbKey, &sm.Position, &archived, &sm.UpdatedAt); err != nil {
		return DocumentSummary{}, err
	}
	sm.FolderID = ptr(folder)
	sm.Archived = archived != 0
	// thumbnail_url is present only once the worker has actually produced the
	// object, so the UI can tell "no thumbnail yet" from "no thumbnail ever".
	if thumbKey.Valid && thumbKey.String != "" {
		u := thumbnailURL(sm.ID)
		sm.ThumbnailURL = &u
	}
	return sm, nil
}

// AllFolders returns every folder (optionally including archived), ordered.
func (s *Store) AllFolders(ctx context.Context, includeArchived bool) ([]DocFolder, error) {
	where := ""
	if !includeArchived {
		where = " WHERE archived = 0"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+folderCols+` FROM document_folders`+where+` ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DocFolder
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// AllDocumentSummaries returns every document as a lightweight summary, ordered.
// Together with AllFolders this makes the whole tree two queries — never one per
// folder (the no-N+1 requirement).
func (s *Store) AllDocumentSummaries(ctx context.Context, includeArchived bool) ([]DocumentSummary, error) {
	where := ""
	if !includeArchived {
		where = " WHERE archived = 0"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+summaryCols+` FROM documents`+where+` ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DocumentSummary
	for rows.Next() {
		sm, err := scanDocumentSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sm)
	}
	return out, rows.Err()
}

// DocumentSummariesInFolder returns the document summaries directly under a folder
// (or root), ordered — for DocFolderDetail.
func (s *Store) DocumentSummariesInFolder(ctx context.Context, q DBTX, folderID *string, includeArchived bool) ([]DocumentSummary, error) {
	cond := "archived = 0"
	if includeArchived {
		cond = "1=1"
	}
	rows, err := q.QueryContext(ctx,
		`SELECT `+summaryCols+` FROM documents WHERE COALESCE(folder_id,'') = ? AND `+cond+` ORDER BY position, id`,
		deref(folderID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocumentSummary{}
	for rows.Next() {
		sm, err := scanDocumentSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sm)
	}
	return out, rows.Err()
}

// ChildFolders returns the child folders of parentID (or root), ordered.
func (s *Store) ChildFolders(ctx context.Context, q DBTX, parentID *string, includeArchived bool) ([]DocFolder, error) {
	cond := "archived = 0"
	if includeArchived {
		cond = "1=1"
	}
	rows, err := q.QueryContext(ctx,
		`SELECT `+folderCols+` FROM document_folders WHERE COALESCE(parent_id,'') = ? AND `+cond+` ORDER BY position, id`,
		deref(parentID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocFolder{}
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ---- Preview state ----

// SetPreviewResult records the outcome of one preview/thumbnail job. It is written
// OUTSIDE the request path by the worker and carries no audit event: a preview is
// derived data, not a user action. Idempotent — re-running a job for the same
// document simply rewrites the same values.
//
// updated_at is deliberately LEFT ALONE. It is the document's user-facing date and
// the ORDER BY / keyset key for ListDocuments, so stamping it here would re-date a
// document for work the user never did. The boot sweeps make that concrete: toggling
// HOME_DOCS_PREVIEW_ENABLED off then on runs settlePending and then backfillSettled
// over every eligible row, which would re-date the WHOLE archive to the boot
// timestamp and reshuffle the list.
func (s *Store) SetPreviewResult(ctx context.Context, q DBTX, id, kind, status, thumbStatus string, previewKey, thumbKey *string) error {
	_, err := q.ExecContext(ctx,
		`UPDATE documents
		 SET preview_kind     = ?, preview_status = ?,
		     thumbnail_status = ?,
		     preview_key      = COALESCE(?, preview_key),
		     thumbnail_key    = COALESCE(?, thumbnail_key)
		 WHERE id = ?`,
		kind, status, thumbStatus, previewKey, thumbKey, id)
	return err
}

// PendingPreviewIDs lists documents still awaiting derivation. The worker drains
// this on boot so a crash mid-conversion cannot leave a document stuck "pending"
// forever (crash safety, HANDOFF-6 §4).
//
// BOTH statuses matter. preview_status alone would miss every image and PDF: they
// are born 'native'/'ready' and only ever owed a THUMBNAIL, so a job that died on a
// transient storage error would leave nothing behind for this sweep to find.
func (s *Store) PendingPreviewIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM documents
		  WHERE preview_status = 'pending' OR thumbnail_status = 'pending'
		  ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// derivationCandidate is a row the backfill sweep may re-open. It carries just
// enough state to tell the two meanings of "none" apart: a type that has nothing to
// derive, versus a document that arrived while derivation was switched off.
type derivationCandidate struct {
	ID              string
	ContentType     string
	PreviewKind     string
	PreviewStatus   string
	ThumbnailStatus string
	HasPreview      bool
	HasThumbnail    bool
}

// SettledWithoutDerivation lists documents recorded as having no preview or no
// thumbnail and owning no derived object to match.
//
// 'none' is terminal — no sweep revisits it — which is right when it describes the
// FILE (a .zip has no preview) and wrong when it only describes the CONFIGURATION at
// upload time: a .docx uploaded with HOME_DOCS_PREVIEW_ENABLED=false, or with no
// converter URL set, would otherwise stay download-only forever after previews are
// switched on, with re-uploading under a new id the only remedy. The worker filters
// this list by content type and re-opens the rows a converter could now serve.
//
// 'failed' is deliberately NOT included: that verdict came from trying, and retrying
// it at every boot is what thumbnail_status exists to prevent.
func (s *Store) SettledWithoutDerivation(ctx context.Context) ([]derivationCandidate, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, content_type, preview_kind, preview_status, thumbnail_status,
		        preview_key IS NOT NULL, thumbnail_key IS NOT NULL
		   FROM documents
		  WHERE preview_status = 'none' OR thumbnail_status = 'none'
		  ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []derivationCandidate
	for rows.Next() {
		var c derivationCandidate
		if err := rows.Scan(&c.ID, &c.ContentType, &c.PreviewKind, &c.PreviewStatus,
			&c.ThumbnailStatus, &c.HasPreview, &c.HasThumbnail); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- Reconciliation (D45) ----

// ObjectRef is one object the reconciliation pass expects to exist, tied to the
// row that owns it.
type ObjectRef struct {
	DocumentID string
	Key        string
	Kind       string // "original" | "preview" | "thumbnail"
}

// ExpectedObjects lists every object key the live rows claim. The reconciliation
// pass diffs this against the bucket listing: keys here but missing in the bucket
// are DANGLING ROWS (logged, never auto-deleted); keys in the bucket but not here
// are ORPHANED OBJECTS (deletable once past the grace window).
func (s *Store) ExpectedObjects(ctx context.Context) ([]ObjectRef, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, storage_key, preview_key, thumbnail_key FROM documents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ObjectRef
	for rows.Next() {
		var id, storageKey string
		var previewKey, thumbKey sql.NullString
		if err := rows.Scan(&id, &storageKey, &previewKey, &thumbKey); err != nil {
			return nil, err
		}
		out = append(out, ObjectRef{DocumentID: id, Key: storageKey, Kind: "original"})
		if previewKey.Valid && previewKey.String != "" {
			out = append(out, ObjectRef{DocumentID: id, Key: previewKey.String, Kind: "preview"})
		}
		if thumbKey.Valid && thumbKey.String != "" {
			out = append(out, ObjectRef{DocumentID: id, Key: thumbKey.String, Kind: "thumbnail"})
		}
	}
	return out, rows.Err()
}

// ---- Pins ----

// PinSets returns the document ids the caller sees pinned: household pins (shared)
// and this user's personal pins.
func (s *Store) PinSets(ctx context.Context, userID string) (household, personal map[string]bool, err error) {
	household, personal = map[string]bool{}, map[string]bool{}
	rows, err := s.db.QueryContext(ctx,
		`SELECT document_id, scope FROM document_pins
		 WHERE scope = 'household' OR (scope = 'personal' AND user_id = ?)`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var docID, scope string
		if err := rows.Scan(&docID, &scope); err != nil {
			return nil, nil, err
		}
		if scope == scopeHousehold {
			household[docID] = true
		} else {
			personal[docID] = true
		}
	}
	return household, personal, rows.Err()
}

func (s *Store) GetPinState(ctx context.Context, q DBTX, documentID, userID string) (PinState, error) {
	var st PinState
	var n int
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM document_pins WHERE document_id = ? AND scope = 'household'`, documentID).Scan(&n); err != nil {
		return st, err
	}
	st.Household = n > 0
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM document_pins WHERE document_id = ? AND scope = 'personal' AND user_id = ?`,
		documentID, userID).Scan(&n); err != nil {
		return st, err
	}
	st.Personal = n > 0
	return st, nil
}

func (s *Store) lastPinPosition(ctx context.Context, tx DBTX, scope, userID string) (string, error) {
	var pos sql.NullString
	var err error
	if scope == scopeHousehold {
		err = tx.QueryRowContext(ctx, `SELECT MAX(position) FROM document_pins WHERE scope = 'household'`).Scan(&pos)
	} else {
		err = tx.QueryRowContext(ctx,
			`SELECT MAX(position) FROM document_pins WHERE scope = 'personal' AND user_id = ?`, userID).Scan(&pos)
	}
	if err != nil {
		return "", err
	}
	if !pos.Valid {
		return lexorank.First(), nil
	}
	return lexorank.Tail(pos.String), nil
}

// PinExists reports whether the given scope is already pinned, letting Pin
// short-circuit the position scan and the ignored insert on a re-pin.
func (s *Store) PinExists(ctx context.Context, q DBTX, documentID, scope, userID string) (bool, error) {
	var one int
	var err error
	if scope == scopeHousehold {
		err = q.QueryRowContext(ctx,
			`SELECT 1 FROM document_pins WHERE document_id = ? AND scope = 'household' LIMIT 1`, documentID).Scan(&one)
	} else {
		err = q.QueryRowContext(ctx,
			`SELECT 1 FROM document_pins WHERE document_id = ? AND scope = 'personal' AND user_id = ? LIMIT 1`,
			documentID, userID).Scan(&one)
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// InsertPin adds a pin idempotently (the partial unique indexes prevent
// duplicates). Reports whether a new row was actually inserted.
func (s *Store) InsertPin(ctx context.Context, q DBTX, documentID, scope string, userID *string, pinnedBy, position string) (bool, error) {
	res, err := q.ExecContext(ctx,
		`INSERT OR IGNORE INTO document_pins (id, document_id, scope, user_id, pinned_by, position, created_at)
		 VALUES (?,?,?,?,?,?,?)`,
		idgen.New(), documentID, scope, nullable(deref(userID)), nullable(pinnedBy), position, nowUTC())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeletePin removes a pin. Reports whether a row was removed.
func (s *Store) DeletePin(ctx context.Context, q DBTX, documentID, scope string, userID *string) (bool, error) {
	var res sql.Result
	var err error
	if scope == scopeHousehold {
		res, err = q.ExecContext(ctx, `DELETE FROM document_pins WHERE document_id = ? AND scope = 'household'`, documentID)
	} else {
		res, err = q.ExecContext(ctx,
			`DELETE FROM document_pins WHERE document_id = ? AND scope = 'personal' AND user_id = ?`,
			documentID, deref(userID))
	}
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// pinRow is one row of the widget query (join document_pins → documents).
type pinRow struct {
	DocumentID    string
	FolderID      *string
	Title         string
	Slug          string
	ContentType   string
	ByteSize      int64
	PreviewKind   string
	PreviewStatus string
	ThumbnailKey  *string
	UpdatedAt     string
	Scope         string
	Position      string
}

// PinnedRowsFor returns the household pins and this user's personal pins joined to
// their live documents, ordered by pin position within each scope. ONE bounded
// query — the widget must never fan out per pin.
func (s *Store) PinnedRowsFor(ctx context.Context, userID string) ([]pinRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.document_id, d.folder_id, d.title, d.slug, d.content_type, d.byte_size,
		        d.preview_kind, d.preview_status, d.thumbnail_key, d.updated_at, p.scope, p.position
		 FROM document_pins p JOIN documents d ON d.id = p.document_id
		 WHERE d.archived = 0 AND (p.scope = 'household' OR (p.scope = 'personal' AND p.user_id = ?))
		 ORDER BY p.position, d.title`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pinRow
	for rows.Next() {
		var r pinRow
		var folder, thumbKey sql.NullString
		if err := rows.Scan(&r.DocumentID, &folder, &r.Title, &r.Slug, &r.ContentType, &r.ByteSize,
			&r.PreviewKind, &r.PreviewStatus, &thumbKey, &r.UpdatedAt, &r.Scope, &r.Position); err != nil {
			return nil, err
		}
		r.FolderID = ptr(folder)
		r.ThumbnailKey = ptr(thumbKey)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- small SQL helpers ----

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func toArgs(ids []string) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}
