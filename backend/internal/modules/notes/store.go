package notes

import (
	"context"
	"database/sql"
	"strings"
	"time"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lexorank"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
)

// DBTX is satisfied by *sql.DB and *sql.Tx. Reads use the store's *sql.DB;
// mutations take the explicit tx so the audit write commits atomically with them.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// tsFormat is RFC 3339 with fixed-width microseconds. The width is fixed (zero-
// padded) so string comparison stays chronological for SQLite's ORDER BY updated_at,
// and the sub-second component lets the note editor's concurrency guard tell two
// edits within the same wall-clock second apart — it compares updated_at for
// equality, which plain second precision (time.RFC3339) could not disambiguate.
const tsFormat = "2006-01-02T15:04:05.000000Z07:00"

func nowUTC() string { return time.Now().UTC().Format(tsFormat) }

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

// icon is appended after the v1 columns and the v9 pair after that, so existing
// scan positions stay untouched.
const folderCols = `id, parent_id, name, slug, position, archived, created_by, created_at, updated_at, icon, visibility, owner_id`

func scanFolder(r interface{ Scan(...any) error }) (Folder, error) {
	var f Folder
	var parent, createdBy, icon, owner sql.NullString
	var archived int
	if err := r.Scan(&f.ID, &parent, &f.Name, &f.Slug, &f.Position, &archived, &createdBy,
		&f.CreatedAt, &f.UpdatedAt, &icon, &f.Visibility, &owner); err != nil {
		return Folder{}, err
	}
	f.ParentID = ptr(parent)
	f.CreatedBy = ptr(createdBy)
	f.Archived = archived != 0
	f.Icon = icon.String
	f.OwnerID = ptr(owner)
	return f, nil
}

// GetFolder loads a folder BY ID, subject to what viewerID may see: the whole
// shared tree plus that member's own private one (v9). A folder in someone else's
// private tree reads back as (nil, nil) — indistinguishable from an id that was
// never issued, which is what makes the handler's 404 leak nothing (D180).
func (s *Store) GetFolder(ctx context.Context, q DBTX, id, viewerID string) (*Folder, error) {
	cond, args := viewerCond("", viewerID)
	row := q.QueryRowContext(ctx,
		`SELECT `+folderCols+` FROM folders WHERE id = ? AND `+cond,
		append([]any{id}, args...)...)
	f, err := scanFolder(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// GetFolderAnyScope loads a folder ignoring visibility.
//
// ⚠ It exists for exactly two callers and must not grow a third without an
// argument: the admin HARD DELETE (D181 — the one asymmetry: an admin may purge a
// foreign private item and may never read one) and the storage/purge inventory,
// which reports sizes and ids and never a name. Anything that puts the returned
// Name on a response has reintroduced the leak this version closes.
func (s *Store) GetFolderAnyScope(ctx context.Context, q DBTX, id string) (*Folder, error) {
	row := q.QueryRowContext(ctx, `SELECT `+folderCols+` FROM folders WHERE id = ?`, id)
	f, err := scanFolder(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *Store) InsertFolder(ctx context.Context, tx DBTX, parentID *string, name, slug, position, createdBy, icon string, sc Scope) (*Folder, error) {
	id := idgen.New()
	now := nowUTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO folders (id, parent_id, name, slug, position, archived, created_by, created_at, updated_at, icon, visibility, owner_id)
		 VALUES (?,?,?,?,?,0,?,?,?,?,?,?)`,
		id, nullable(deref(parentID)), name, slug, position, nullable(createdBy), now, now, nullable(icon),
		sc.Visibility(), sc.ownerColumn()); err != nil {
		return nil, err
	}
	return s.GetFolderAnyScope(ctx, tx, id)
}

// RenameFolder updates name+slug.
func (s *Store) RenameFolder(ctx context.Context, tx DBTX, id, name, slug string) error {
	_, err := tx.ExecContext(ctx, `UPDATE folders SET name = ?, slug = ?, updated_at = ? WHERE id = ?`,
		name, slug, nowUTC(), id)
	return err
}

// SetFolderIcon updates only the icon (independent of a rename; icon can change on
// its own). Empty string stores NULL, which reads back as "" and lets the client
// fall back to the 📁 default.
func (s *Store) SetFolderIcon(ctx context.Context, tx DBTX, id, icon string) error {
	_, err := tx.ExecContext(ctx, `UPDATE folders SET icon = ?, updated_at = ? WHERE id = ?`,
		nullable(icon), nowUTC(), id)
	return err
}

// MoveFolderRow reparents and/or reorders a folder, possibly with a fresh slug.
func (s *Store) MoveFolderRow(ctx context.Context, tx DBTX, id string, parentID *string, position, slug string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE folders SET parent_id = ?, position = ?, slug = ?, updated_at = ? WHERE id = ?`,
		nullable(deref(parentID)), position, slug, nowUTC(), id)
	return err
}

func (s *Store) SetFolderArchived(ctx context.Context, tx DBTX, id string, archived bool) error {
	_, err := tx.ExecContext(ctx, `UPDATE folders SET archived = ?, updated_at = ? WHERE id = ?`,
		boolInt(archived), nowUTC(), id)
	return err
}

func (s *Store) DeleteFolder(ctx context.Context, tx DBTX, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM folders WHERE id = ?`, id)
	return err
}

// ChildFolderBySlug resolves one slug segment under a parent — or under the ROOT
// OF ONE SCOPE when parentID is nil (v9). Before v9 the root was addressed by the
// bare unscoped root sentinel, which after v9 would collapse every
// member's private root and the household's into one bucket (D178).
func (s *Store) ChildFolderBySlug(ctx context.Context, q DBTX, parentID *string, slug string, sc Scope) (*Folder, error) {
	row := q.QueryRowContext(ctx,
		`SELECT `+folderCols+` FROM folders WHERE `+siblingKeyExpr("", "parent_id")+` = ? AND slug = ? AND archived = 0`,
		sc.parentKey(parentID), slug)
	f, err := scanFolder(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *Store) lastFolderPosition(ctx context.Context, tx DBTX, parentID *string, sc Scope) (string, error) {
	var pos sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(position) FROM folders WHERE `+siblingKeyExpr("", "parent_id")+` = ? AND archived = 0`,
		sc.parentKey(parentID)).Scan(&pos); err != nil {
		return "", err
	}
	if !pos.Valid {
		return lexorank.First(), nil
	}
	return lexorank.Tail(pos.String), nil
}

// FolderChildCounts returns the non-archived subfolder and note counts (for the
// non-empty delete guard).
func (s *Store) FolderChildCounts(ctx context.Context, q DBTX, folderID string) (subfolders, notes int, err error) {
	if err = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM folders WHERE parent_id = ? AND archived = 0`, folderID).Scan(&subfolders); err != nil {
		return
	}
	err = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM notes WHERE folder_id = ? AND archived = 0`, folderID).Scan(&notes)
	return
}

// FolderMeta is the (name, archived) pair the cascade delete needs per folder.
type FolderMeta struct {
	Name     string
	Archived bool
}

// FolderMetaByIDs returns id → (name, archived) for the given folder ids in a
// single query — the cascade delete uses it to caption audit events and to skip
// already-archived rows without an N+1 GetFolder per descendant.
func (s *Store) FolderMetaByIDs(ctx context.Context, q DBTX, ids []string) (map[string]FolderMeta, error) {
	out := map[string]FolderMeta{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := placeholders(len(ids))
	rows, err := q.QueryContext(ctx, `SELECT id, name, archived FROM folders WHERE id IN (`+ph+`)`, toArgs(ids)...)
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
// parent_id) — used to cascade a folder delete without an N+1 walk per level
// beyond the tree depth. includeArchived=false limits the walk to live folders
// (soft cascade); true covers the whole physical subtree (hard delete audit).
//
// v9: it takes NO scope, and that is a property of the tree rather than an
// oversight. Every descendant of a folder is in the SAME root scope as the folder
// — a move across scopes is a 422 (D186) and a publish moves the whole subtree in
// one transaction (D182) — so once the root has been access-checked, the walk
// cannot escape into another member's tree. If a future change ever lets a subtree
// straddle two scopes, this walk is the first thing that has to be revisited.
func (s *Store) DescendantFolderIDs(ctx context.Context, q DBTX, rootID string, includeArchived bool) ([]string, error) {
	filter := " AND archived = 0"
	if includeArchived {
		filter = ""
	}
	out := []string{rootID}
	frontier := []string{rootID}
	// visited breaks any parent_id cycle (manual DB edit, restore anomaly) so the
	// BFS can't loop forever while holding the delete's write lock — mirroring the
	// depth/visited caps in wouldCycle, ancestors, and Tree.build.
	visited := map[string]bool{rootID: true}
	for len(frontier) > 0 {
		ph := placeholders(len(frontier))
		args := toArgs(frontier)
		rows, err := q.QueryContext(ctx,
			`SELECT id FROM folders WHERE parent_id IN (`+ph+`)`+filter, args...)
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

// NotesInFolders returns the notes contained directly in any of the given
// folders. includeArchived=false returns only live notes (soft cascade); true
// returns every note in the subtree (hard delete audit).
func (s *Store) NotesInFolders(ctx context.Context, q DBTX, folderIDs []string, includeArchived bool) ([]Note, error) {
	if len(folderIDs) == 0 {
		return nil, nil
	}
	filter := " AND archived = 0"
	if includeArchived {
		filter = ""
	}
	ph := placeholders(len(folderIDs))
	rows, err := q.QueryContext(ctx,
		`SELECT `+noteCols+` FROM notes WHERE folder_id IN (`+ph+`)`+filter, toArgs(folderIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ---- Notes ----

const noteCols = `id, folder_id, title, slug, body_md, position, archived, created_by, created_at, updated_at, visibility, owner_id`

func scanNote(r interface{ Scan(...any) error }) (Note, error) {
	var n Note
	var folder, body, createdBy, owner sql.NullString
	var archived int
	if err := r.Scan(&n.ID, &folder, &n.Title, &n.Slug, &body, &n.Position, &archived, &createdBy,
		&n.CreatedAt, &n.UpdatedAt, &n.Visibility, &owner); err != nil {
		return Note{}, err
	}
	n.FolderID = ptr(folder)
	n.BodyMD = ptr(body)
	n.CreatedBy = ptr(createdBy)
	n.Archived = archived != 0
	n.OwnerID = ptr(owner)
	return n, nil
}

// GetNote loads a note BY ID, subject to what viewerID may see (v9). Another
// member's private note reads back as (nil, nil) — byte-identical to an id that
// does not exist, which is the whole point (D180).
func (s *Store) GetNote(ctx context.Context, q DBTX, id, viewerID string) (*Note, error) {
	cond, args := viewerCond("", viewerID)
	row := q.QueryRowContext(ctx,
		`SELECT `+noteCols+` FROM notes WHERE id = ? AND `+cond,
		append([]any{id}, args...)...)
	n, err := scanNote(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// GetNoteAnyScope loads a note ignoring visibility. See GetFolderAnyScope: two
// callers only — the admin hard delete (D181) and the storage inventory, which
// never puts a title on a response.
func (s *Store) GetNoteAnyScope(ctx context.Context, q DBTX, id string) (*Note, error) {
	row := q.QueryRowContext(ctx, `SELECT `+noteCols+` FROM notes WHERE id = ?`, id)
	n, err := scanNote(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Store) InsertNote(ctx context.Context, tx DBTX, folderID *string, title, slug, body, position, createdBy string, sc Scope) (*Note, error) {
	id := idgen.New()
	now := nowUTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO notes (id, folder_id, title, slug, body_md, position, archived, created_by, created_at, updated_at, visibility, owner_id)
		 VALUES (?,?,?,?,?,?,0,?,?,?,?,?)`,
		id, nullable(deref(folderID)), title, slug, nullable(body), position, nullable(createdBy), now, now,
		sc.Visibility(), sc.ownerColumn()); err != nil {
		return nil, err
	}
	return s.GetNoteAnyScope(ctx, tx, id)
}

// notePatch carries the fields a PATCH may change. A nil pointer means "leave as
// is"; Body when non-nil is stored nullable (empty string clears the body).
type notePatch struct {
	Title    *string
	Slug     *string
	Body     *string
	Archived *bool
}

func (s *Store) UpdateNote(ctx context.Context, tx DBTX, id string, u notePatch) error {
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
	if u.Body != nil {
		sets = append(sets, "body_md = ?")
		args = append(args, nullable(*u.Body))
	}
	if u.Archived != nil {
		sets = append(sets, "archived = ?")
		args = append(args, boolInt(*u.Archived))
	}
	// Nothing to change (an all-nil patch, e.g. PATCH {}): skip the write so a no-op
	// doesn't bump updated_at — a bumped timestamp would trip every other open
	// editor's "changed elsewhere" guard for a change that didn't happen. Callers
	// gate the audit record and broadcast on the diff, so nothing is logged either.
	if len(sets) == 0 {
		return nil
	}
	sets = append([]string{"updated_at = ?"}, sets...)
	args = append([]any{nowUTC()}, args...)
	args = append(args, id)
	_, err := tx.ExecContext(ctx, `UPDATE notes SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

// MoveNoteRow reparents and/or reorders a note, possibly with a fresh slug.
func (s *Store) MoveNoteRow(ctx context.Context, tx DBTX, id string, folderID *string, position, slug string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE notes SET folder_id = ?, position = ?, slug = ?, updated_at = ? WHERE id = ?`,
		nullable(deref(folderID)), position, slug, nowUTC(), id)
	return err
}

func (s *Store) DeleteNote(ctx context.Context, tx DBTX, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM notes WHERE id = ?`, id)
	return err
}

// PublishNoteRow moves one note from a private root into the shared tree: sets
// visibility, clears owner_id, reparents and re-slugs, all in the caller's
// transaction (v9, D182).
//
// It is a SEPARATE method from MoveNoteRow on purpose. A move that crosses scopes
// is a 422 (D186) and publishing is the only crossing there is; folding the two
// together would make the one irreversible operation in the module reachable from
// the ordinary drag-and-drop path.
func (s *Store) PublishNoteRow(ctx context.Context, tx DBTX, id string, folderID *string, position, slug string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE notes SET folder_id = ?, position = ?, slug = ?, visibility = 'shared', owner_id = NULL, updated_at = ?
		 WHERE id = ?`,
		nullable(deref(folderID)), position, slug, nowUTC(), id)
	return err
}

// PublishFolderRow is the folder equivalent. A folder publish cascades to every
// descendant through PublishDescendants below; this writes the root of the subtree.
func (s *Store) PublishFolderRow(ctx context.Context, tx DBTX, id string, parentID *string, position, slug string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE folders SET parent_id = ?, position = ?, slug = ?, visibility = 'shared', owner_id = NULL, updated_at = ?
		 WHERE id = ?`,
		nullable(deref(parentID)), position, slug, nowUTC(), id)
	return err
}

// PublishDescendants flips visibility on every folder and note in a subtree whose
// root has already been reparented. Slugs are NOT re-derived here and do not need
// to be: a descendant's siblings are the same siblings they were before the
// publish — only the subtree's ROOT lands among strangers.
//
// ⚠ Both statements run in the caller's transaction. A partial publish — half a
// folder visible to the household — is the one outcome this endpoint must never
// produce, and the transaction is what guarantees it.
func (s *Store) PublishDescendants(ctx context.Context, tx DBTX, folderIDs []string) error {
	if len(folderIDs) == 0 {
		return nil
	}
	ph := placeholders(len(folderIDs))
	now := nowUTC()
	if _, err := tx.ExecContext(ctx,
		`UPDATE folders SET visibility = 'shared', owner_id = NULL, updated_at = ? WHERE id IN (`+ph+`)`,
		append([]any{now}, toArgs(folderIDs)...)...); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx,
		`UPDATE notes SET visibility = 'shared', owner_id = NULL, updated_at = ? WHERE folder_id IN (`+ph+`)`,
		append([]any{now}, toArgs(folderIDs)...)...)
	return err
}

// ChildNoteBySlug resolves one slug segment under a folder, or under the root of
// one scope when folderID is nil (v9). See ChildFolderBySlug.
func (s *Store) ChildNoteBySlug(ctx context.Context, q DBTX, folderID *string, slug string, sc Scope) (*Note, error) {
	row := q.QueryRowContext(ctx,
		`SELECT `+noteCols+` FROM notes WHERE `+siblingKeyExpr("", "folder_id")+` = ? AND slug = ? AND archived = 0`,
		sc.parentKey(folderID), slug)
	n, err := scanNote(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Store) lastNotePosition(ctx context.Context, tx DBTX, folderID *string, sc Scope) (string, error) {
	var pos sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(position) FROM notes WHERE `+siblingKeyExpr("", "folder_id")+` = ? AND archived = 0`,
		sc.parentKey(folderID)).Scan(&pos); err != nil {
		return "", err
	}
	if !pos.Valid {
		return lexorank.First(), nil
	}
	return lexorank.Tail(pos.String), nil
}

// SiblingSlugTaken reports whether a live folder or note under the same parent
// already uses slug, excluding the given ids (the item being renamed/moved). This
// is the in-transaction cross-table half of the addressing invariant (D32) that no
// single index can express.
//
// ⚠ THE SCOPE ARGUMENT IS NOT OPTIONAL POLISH — it is the other half of D178, and
// scoping the four indexes without scoping this query fixes nothing visible.
//
// The failure mode if it is dropped is quiet rather than loud, which is why it is
// worth spelling out (D210). Service.freeSlug LOOPS on this predicate, appending
// -2, -3… until it reports free. So when two members each create a private note
// called "Recepty" at their own root, an un-scoped query does NOT raise a 409: it
// tells the second caller that `recepty` is taken, freeSlug quietly hands them
// `recepty-2`, and both requests succeed. The result is a slug that discloses the
// existence of a sibling they are not allowed to see, with no error anywhere and
// nothing in the logs. Assert on the resulting SLUG, never on an error.
//
// The predicate mirrors the sibling-slug index expression exactly, so root-level
// siblings compare per root scope rather than collapsing into one bucket.
func (s *Store) SiblingSlugTaken(ctx context.Context, q DBTX, parentID *string, sc Scope, slug, excludeFolderID, excludeNoteID string) (bool, error) {
	key := sc.parentKey(parentID)
	var n int
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM folders WHERE `+siblingKeyExpr("", "parent_id")+` = ? AND slug = ? AND archived = 0 AND id != ?`,
		key, slug, excludeFolderID).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notes WHERE `+siblingKeyExpr("", "folder_id")+` = ? AND slug = ? AND archived = 0 AND id != ?`,
		key, slug, excludeNoteID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// SearchNotes runs an FTS5 MATCH over title+body, newest-updated first, capped.
// It honors the same folder/archived scoping as the folder listing: nil folderID
// searches every folder IN ONE ROOT SCOPE, and archived rows are excluded unless
// includeArchived. Reads only.
//
// ⚠ v9: the scope predicate rides INSIDE this query, in the same WHERE as the
// MATCH, and never as a filter over the returned slice (D184). A post-filter still
// leaks: the caller learns how many rows matched BEFORE filtering, through short
// pages and the behaviour of the cap, even when every offending row is gone from
// the response. The join to the base table is what makes this possible, which is
// why the two must not be separated.
func (s *Store) SearchNotes(ctx context.Context, query string, folderID *string, includeArchived bool, limit int, sc Scope) ([]NoteSummary, error) {
	scopeSQL, scopeArgs := scopeCond("n.", sc)
	conds := []string{"notes_fts MATCH ?", scopeSQL}
	args := []any{query}
	args = append(args, scopeArgs...)
	if !includeArchived {
		conds = append(conds, "n.archived = 0")
	}
	if folderID != nil {
		conds = append(conds, siblingKeyExpr("n.", "folder_id")+" = ?")
		args = append(args, sc.parentKey(folderID))
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT n.id, n.folder_id, n.title, n.slug, n.position, n.archived, n.updated_at, n.visibility, n.owner_id
		 FROM notes_fts f JOIN notes n ON n.rowid = f.rowid
		 WHERE `+strings.Join(conds, " AND ")+`
		 ORDER BY n.updated_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NoteSummary{}
	for rows.Next() {
		sm, err := scanNoteSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sm)
	}
	return out, rows.Err()
}

// ---- Tree data ----

const noteSummaryCols = `id, folder_id, title, slug, position, archived, updated_at, visibility, owner_id`

func scanNoteSummary(r interface{ Scan(...any) error }) (NoteSummary, error) {
	var sm NoteSummary
	var folder, owner sql.NullString
	var archived int
	if err := r.Scan(&sm.ID, &folder, &sm.Title, &sm.Slug, &sm.Position, &archived, &sm.UpdatedAt,
		&sm.Visibility, &owner); err != nil {
		return NoteSummary{}, err
	}
	sm.FolderID = ptr(folder)
	sm.Archived = archived != 0
	sm.OwnerID = ptr(owner)
	return sm, nil
}

// AllFolders returns every folder IN ONE ROOT SCOPE (optionally including
// archived), ordered.
//
// ⚠ v9: the scope is required even where the caller "obviously" means shared —
// notably both `pripnute` widget providers, which call this to build breadcrumbs.
// The pins those widgets render are already per-caller, so nothing leaked today,
// but an un-scoped call loads every member's private folder NAMES into memory
// beside a response. The next person to put a folder name on a widget row would
// have no way to know (leak table row 9).
func (s *Store) AllFolders(ctx context.Context, includeArchived bool, sc Scope) ([]Folder, error) {
	scopeSQL, args := scopeCond("", sc)
	where := " WHERE " + scopeSQL
	if !includeArchived {
		where += " AND archived = 0"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+folderCols+` FROM folders`+where+` ORDER BY position, id`, args...)
	if err != nil {
		return nil, err
	}
	return scanFolders(rows)
}

// AllFoldersForViewer returns every live folder viewerID may see — the whole
// shared tree plus that member's own private one — in ONE query.
//
// ⚠ It is the VIEWER predicate, not the scope one, and that is the point. The
// `pripnute` widget needs a breadcrumb map spanning both roots the caller can
// reach, which is precisely the question viewerCond answers ("may I see this
// ROW?"); expressing it as two scoped reads made a per-render full folder scan
// into two, on the dashboard, for every member. It is not a tree read and must
// never be used as one — a tree route mixing both roots into one response is
// exactly what D177 rejected (see scope.go's note on the two predicates).
func (s *Store) AllFoldersForViewer(ctx context.Context, includeArchived bool, viewerID string) ([]Folder, error) {
	cond, args := viewerCond("", viewerID)
	where := " WHERE " + cond
	if !includeArchived {
		where += " AND archived = 0"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+folderCols+` FROM folders`+where+` ORDER BY position, id`, args...)
	if err != nil {
		return nil, err
	}
	return scanFolders(rows)
}

func scanFolders(rows *sql.Rows) ([]Folder, error) {
	defer rows.Close()
	var out []Folder
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// AllNoteSummaries returns every note in one root scope as a lightweight summary
// (no body), ordered.
func (s *Store) AllNoteSummaries(ctx context.Context, includeArchived bool, sc Scope) ([]NoteSummary, error) {
	scopeSQL, args := scopeCond("", sc)
	where := " WHERE " + scopeSQL
	if !includeArchived {
		where += " AND archived = 0"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+noteSummaryCols+` FROM notes`+where+` ORDER BY position, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NoteSummary
	for rows.Next() {
		sm, err := scanNoteSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sm)
	}
	return out, rows.Err()
}

// NoteSummariesInFolder returns the note summaries directly under a folder — or
// under the ROOT OF ONE SCOPE when folderID is the root sentinel — ordered.
//
// ⚠ This predicate is the second half of the unscoped-COALESCE family, and it had
// a bug before v9 even existed (D203). `notes` has no ?folder_id=root sentinel:
// the handler passed the parameter straight through and a nil pointer dereferenced
// to the empty string, so OMITTING folder_id already meant "root notes only" rather than the
// "all notes" the 0.10.1 contract advertised. After v9 the same expression would
// collapse EVERY scope's root into one bucket, so `GET /api/notes` would return
// other members' private root notes. Both halves are fixed here: the scope rides
// in the key, and the handler now understands the `root` sentinel documents
// already had.
// ⚠ AND THE SCOPE RIDES IN THE WHERE CLAUSE TOO, not only inside the sibling key.
// The key pins the scope ONLY at the root, where the sentinel carries it; under a
// named folder it degenerates to `folder_id = ?` and says nothing about
// visibility. `GET /api/notes?folder_id=` passes an id straight from the request,
// so without the term below any member could hand it another member's private
// folder id — one the purge screen hands admins by design (D198) — and read back
// every title in it.
func (s *Store) NoteSummariesInFolder(ctx context.Context, q DBTX, folderID *string, includeArchived bool, sc Scope) ([]NoteSummary, error) {
	cond := "archived = 0"
	if includeArchived {
		cond = "1=1"
	}
	scopeSQL, scopeArgs := scopeCond("", sc)
	args := append([]any{sc.parentKey(folderID)}, scopeArgs...)
	rows, err := q.QueryContext(ctx,
		`SELECT `+noteSummaryCols+` FROM notes WHERE `+siblingKeyExpr("", "folder_id")+` = ? AND `+scopeSQL+` AND `+cond+` ORDER BY position, id`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NoteSummary{}
	for rows.Next() {
		sm, err := scanNoteSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sm)
	}
	return out, rows.Err()
}

// ScopedNoteSummaries returns every live note in one root scope, ordered — what
// `GET /api/notes` returns when no folder_id narrows it. Split from
// NoteSummariesInFolder because "all notes in this tree" and "the notes directly
// at its root" are different questions that the pre-v9 code conflated (D203).
func (s *Store) ScopedNoteSummaries(ctx context.Context, q DBTX, includeArchived bool, sc Scope) ([]NoteSummary, error) {
	scopeSQL, args := scopeCond("", sc)
	where := " WHERE " + scopeSQL
	if !includeArchived {
		where += " AND archived = 0"
	}
	rows, err := q.QueryContext(ctx,
		`SELECT `+noteSummaryCols+` FROM notes`+where+` ORDER BY position, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NoteSummary{}
	for rows.Next() {
		sm, err := scanNoteSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sm)
	}
	return out, rows.Err()
}

// ChildFolders returns the child folders of parentID — or of the root of one scope
// — ordered, for FolderDetail.
func (s *Store) ChildFolders(ctx context.Context, q DBTX, parentID *string, includeArchived bool, sc Scope) ([]Folder, error) {
	cond := "archived = 0"
	if includeArchived {
		cond = "1=1"
	}
	// Scope in the WHERE clause too — same fail-closed reasoning as
	// NoteSummariesInFolder above: under a named parent the sibling key
	// degenerates to `parent_id = ?` and says nothing about visibility.
	scopeSQL, scopeArgs := scopeCond("", sc)
	rows, err := q.QueryContext(ctx,
		`SELECT `+folderCols+` FROM folders WHERE `+siblingKeyExpr("", "parent_id")+` = ? AND `+scopeSQL+` AND `+cond+` ORDER BY position, id`,
		append([]any{sc.parentKey(parentID)}, scopeArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Folder{}
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ---- Pins ----

// PinSets returns the set of note ids the caller sees pinned: household pins (all
// users) and this user's personal pins.
func (s *Store) PinSets(ctx context.Context, userID string) (household, personal map[string]bool, err error) {
	household, personal = map[string]bool{}, map[string]bool{}
	rows, err := s.db.QueryContext(ctx,
		`SELECT note_id, scope, user_id FROM note_pins
		 WHERE scope = 'household' OR (scope = 'personal' AND user_id = ?)`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var noteID, scope string
		var uid sql.NullString
		if err := rows.Scan(&noteID, &scope, &uid); err != nil {
			return nil, nil, err
		}
		if scope == scopeHousehold {
			household[noteID] = true
		} else {
			personal[noteID] = true
		}
	}
	return household, personal, rows.Err()
}

func (s *Store) GetPinState(ctx context.Context, q DBTX, noteID, userID string) (PinState, error) {
	var st PinState
	var n int
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM note_pins WHERE note_id = ? AND scope = 'household'`, noteID).Scan(&n); err != nil {
		return st, err
	}
	st.Household = n > 0
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM note_pins WHERE note_id = ? AND scope = 'personal' AND user_id = ?`, noteID, userID).Scan(&n); err != nil {
		return st, err
	}
	st.Personal = n > 0
	return st, nil
}

func (s *Store) lastPinPosition(ctx context.Context, tx DBTX, scope, userID string) (string, error) {
	var pos sql.NullString
	var err error
	if scope == scopeHousehold {
		err = tx.QueryRowContext(ctx, `SELECT MAX(position) FROM note_pins WHERE scope = 'household'`).Scan(&pos)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT MAX(position) FROM note_pins WHERE scope = 'personal' AND user_id = ?`, userID).Scan(&pos)
	}
	if err != nil {
		return "", err
	}
	if !pos.Valid {
		return lexorank.First(), nil
	}
	return lexorank.Tail(pos.String), nil
}

// PersonalPinExists reports whether the note is already personally pinned by the
// user — lets Pin short-circuit the position scan + ignored insert on a re-pin.
func (s *Store) PersonalPinExists(ctx context.Context, tx DBTX, noteID, userID string) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx,
		`SELECT 1 FROM note_pins WHERE note_id = ? AND scope = 'personal' AND user_id = ? LIMIT 1`,
		noteID, userID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// HouseholdPinExists reports whether the note is already pinned for the household —
// lets Pin short-circuit the position scan + ignored insert on a re-pin, mirroring
// PersonalPinExists on the personal path.
func (s *Store) HouseholdPinExists(ctx context.Context, tx DBTX, noteID string) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx,
		`SELECT 1 FROM note_pins WHERE note_id = ? AND scope = 'household' LIMIT 1`,
		noteID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// InsertPin adds a pin idempotently (partial unique indexes prevent duplicates).
// Reports whether a new row was inserted.
func (s *Store) InsertPin(ctx context.Context, tx DBTX, noteID, scope string, userID *string, pinnedBy, position string) (bool, error) {
	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO note_pins (id, note_id, scope, user_id, pinned_by, position, created_at)
		 VALUES (?,?,?,?,?,?,?)`,
		idgen.New(), noteID, scope, nullable(deref(userID)), nullable(pinnedBy), position, nowUTC())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeletePin removes a pin. Reports whether a row was removed.
func (s *Store) DeletePin(ctx context.Context, tx DBTX, noteID, scope string, userID *string) (bool, error) {
	var res sql.Result
	var err error
	if scope == scopeHousehold {
		res, err = tx.ExecContext(ctx, `DELETE FROM note_pins WHERE note_id = ? AND scope = 'household'`, noteID)
	} else {
		res, err = tx.ExecContext(ctx,
			`DELETE FROM note_pins WHERE note_id = ? AND scope = 'personal' AND user_id = ?`, noteID, deref(userID))
	}
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// pinRow is one row from the widget query (join note_pins → notes).
type pinRow struct {
	NoteID     string
	FolderID   *string
	Title      string
	Slug       string
	BodyMD     *string
	UpdatedAt  string
	Scope      string
	Position   string
	Visibility string // v9: drives the widget row's lock mark (D183)
}

// PinnedRowsFor returns the household pins and this user's personal pins joined to
// their (non-archived) notes, ordered by pin position within each scope. One
// bounded query, no N+1.
//
// v9: it needs no visibility predicate, and the reason is worth stating rather
// than trusting. A private note can carry only a PERSONAL pin (a household pin on
// one is a 422, D183), and the personal branch below is already filtered to
// `p.user_id = ?`. So the only private rows this can return are the caller's own.
// The `visibility` column comes back so the widget can draw the lock — it is not
// what enforces anything.
//
// ⚠ That reasoning holds only while the household-pin refusal holds. If a future
// change ever admits a household pin on a private note, this query starts serving
// other members' titles to the widget with no other symptom.
func (s *Store) PinnedRowsFor(ctx context.Context, userID string) ([]pinRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.note_id, n.folder_id, n.title, n.slug, n.body_md, n.updated_at, p.scope, p.position, n.visibility
		 FROM note_pins p JOIN notes n ON n.id = p.note_id
		 WHERE n.archived = 0 AND (p.scope = 'household' OR (p.scope = 'personal' AND p.user_id = ?))
		 ORDER BY p.position, n.title`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pinRow
	for rows.Next() {
		var r pinRow
		var folder, body sql.NullString
		if err := rows.Scan(&r.NoteID, &folder, &r.Title, &r.Slug, &body, &r.UpdatedAt, &r.Scope, &r.Position, &r.Visibility); err != nil {
			return nil, err
		}
		r.FolderID = ptr(folder)
		r.BodyMD = ptr(body)
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountPinnedFor counts the notes pinned visible to one member — household pins
// ∪ their personal pins, DE-DUPLICATED (a note pinned both ways counts once,
// exactly as the widget shows it once). COUNT(DISTINCT note_id) is what makes
// that de-duplication true in SQL rather than in a second pass.
func (s *Store) CountPinnedFor(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT p.note_id)
		   FROM note_pins p JOIN notes n ON n.id = p.note_id
		  WHERE n.archived = 0
		    AND (p.scope = 'household' OR (p.scope = 'personal' AND p.user_id = ?))`,
		userID).Scan(&n)
	return n, err
}

// ---- small SQL helpers ----

// placeholders is appdb.Placeholders — one implementation, five call sites (v10
// review). It was copied into this module, `documents`, `garden`, `todo` and
// `platform/push` before platform/db grew the shared one.
func placeholders(n int) string { return appdb.Placeholders(n) }

func toArgs(ids []string) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

// ---- Note images (inline image blobs, note-images/{id}) ----

// noteImage is a stored image's metadata row: the id → object map the content
// endpoint, the garbage collector, and the reconciliation pass read.
type noteImage struct {
	ID          string
	NoteID      string
	ContentType string
	ByteSize    int64
	Checksum    string
	CreatedBy   *string
	CreatedAt   string
	// NoteVisibility is the OWNING NOTE's visibility, carried along by
	// GetNoteImageForViewer's join and empty on the unscoped loader. It is not a
	// column of note_images and must never become one (D204) — it rides here only
	// so the handler can pick a cache policy without a second round trip for a row
	// the join already read.
	NoteVisibility string
}

// ImageObjectRef is one object the note_images table claims — the mirror pass
// compares these against what the bucket actually holds.
type ImageObjectRef struct {
	ImageID string
	Key     string
}

const noteImageCols = `id, note_id, content_type, byte_size, checksum, created_by, created_at`

// noteImageColsPrefixed is the same list qualified for the join in
// GetNoteImageForViewer, where `notes` is also in scope and `id` would be ambiguous.
const noteImageColsPrefixed = `i.id, i.note_id, i.content_type, i.byte_size, i.checksum, i.created_by, i.created_at`

func scanNoteImage(r interface{ Scan(...any) error }) (noteImage, error) {
	var im noteImage
	var createdBy sql.NullString
	if err := r.Scan(&im.ID, &im.NoteID, &im.ContentType, &im.ByteSize, &im.Checksum, &createdBy, &im.CreatedAt); err != nil {
		return noteImage{}, err
	}
	im.CreatedBy = ptr(createdBy)
	return im, nil
}

func (s *Store) InsertNoteImage(ctx context.Context, q DBTX, id, noteID, contentType string, byteSize int64, checksum, createdBy string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO note_images (id, note_id, content_type, byte_size, checksum, created_by, created_at)
		 VALUES (?,?,?,?,?,?,?)`,
		id, noteID, contentType, byteSize, checksum, nullable(createdBy), nowUTC())
	return err
}

func (s *Store) GetNoteImage(ctx context.Context, q DBTX, id string) (*noteImage, error) {
	row := q.QueryRowContext(ctx, `SELECT `+noteImageCols+` FROM note_images WHERE id = ?`, id)
	im, err := scanNoteImage(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &im, nil
}

// GetNoteImageForViewer loads an image only if viewerID may see the note that owns
// it (v9, D204, leak table rows 7 and 21) — OR some note the viewer may see whose
// body references it.
//
// The OR term exists because ownership and liveness deliberately diverge:
// liveness is by REFERENCE, not ownership (see Service.gcNoteImages), and content
// copied between notes is a supported path. Without it, an image uploaded into a
// private note and then embedded in a shared note stays owned by the private note
// (ReassignNoteImage runs only on hard delete of the owner) and 404s for every
// member except the uploader — a permanently broken image inside a note they can
// fully read, invisible to the one person who could fix it. instr on the fixed
// URL prefix matches how the reference scans in NotesReferencingAnyImage work.
//
// `note_images` gains NO visibility column of its own: an image inherits its
// note's, and a second copy of the same fact is a second thing to keep in step.
// The join below is what enforces it, and idx_note_images_note already supports it.
//
// A miss reads back as (nil, nil) — the same value an unknown image id produces —
// so the handler's 404 is identical either way.
// The join also returns `n.visibility`, which is why this has its own scan rather
// than reusing scanNoteImage: the cache policy for the response depends on the
// owning note's visibility (D208), and the row that decides access is the same row
// that answers it — asking a second time would be two round trips for one fact.
// (An image reached via the OR term keeps its OWNING note's visibility for cache
// policy — private, hence the stricter policy — which errs safe.)
//
// ⚠ THE REFERENCE TERM REQUIRES THE OWNER'S OWN ACT, not merely a shared
// reference. "Any live shared note references it" was NOT enough, and the gap was
// reachable rather than theoretical: the purge screen hands admins the raw ids of
// foreign `note_image` rows BY DESIGN (D198), and writing a reference is an
// unprivileged act — so any member with write access who learned an id could paste
// `/api/notes/images/{id}` as plain text into a shared note (an ARCHIVED one even,
// since liveness was not checked either) and read a foreign private image. That
// turns "an admin can name the thing well enough to delete it" (D197) into "well
// enough to open it".
//
// So the referencing note must be SHARED, LIVE, and AUTHORED BY THE OWNER of the
// private note that holds the image (`r.created_by = n.owner_id`). This term is
// only ever consulted when `cond` has already failed — i.e. the owning note is
// private and the viewer is not its owner — so owner_id is non-null there and the
// comparison is exactly "the owner put this into household-visible content they
// wrote". The legitimate divergence case the term exists for (an image uploaded
// into a private note, then copied by its owner into a shared note, where
// ReassignNoteImage does not run) is precisely that shape; the owner's own reads
// are already granted by `cond` on n.
func (s *Store) GetNoteImageForViewer(ctx context.Context, q DBTX, id, viewerID string) (*noteImage, error) {
	cond, args := viewerCond("n.", viewerID)
	allArgs := append([]any{id}, args...)
	allArgs = append(allArgs, noteImageURL(id))
	row := q.QueryRowContext(ctx,
		`SELECT `+noteImageColsPrefixed+`, n.visibility
		 FROM note_images i JOIN notes n ON n.id = i.note_id
		 WHERE i.id = ? AND (`+cond+` OR EXISTS (
		   SELECT 1 FROM notes r
		   WHERE r.body_md IS NOT NULL AND instr(r.body_md, ?) > 0
		     AND r.visibility = '`+visibilityShared+`'
		     AND r.archived = 0
		     AND r.created_by IS NOT NULL AND r.created_by = n.owner_id))`,
		allArgs...)
	var im noteImage
	var createdBy sql.NullString
	err := row.Scan(&im.ID, &im.NoteID, &im.ContentType, &im.ByteSize, &im.Checksum,
		&createdBy, &im.CreatedAt, &im.NoteVisibility)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	im.CreatedBy = ptr(createdBy)
	return &im, nil
}

// NoteImageIDsForNote returns the ids of every image owned by a note (its upload
// scope) — GC diffs this against the ids still referenced in the note's body_md.
func (s *Store) NoteImageIDsForNote(ctx context.Context, q DBTX, noteID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT id FROM note_images WHERE note_id = ?`, noteID)
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

// NoteImagesForNote returns the full image rows owned by a note — GC needs each
// row's created_at to spare freshly-uploaded images from a racing body save (see
// Service.gcNoteImages).
func (s *Store) NoteImagesForNote(ctx context.Context, q DBTX, noteID string) ([]noteImage, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+noteImageCols+` FROM note_images WHERE note_id = ?`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []noteImage
	for rows.Next() {
		im, err := scanNoteImage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, im)
	}
	return out, rows.Err()
}

// DeleteNoteImages removes the given image rows. Object deletion is a separate,
// best-effort step (the reconciliation pass sweeps any object whose row is gone),
// so a row delete here never has to succeed atomically with a bucket delete.
func (s *Store) DeleteNoteImages(ctx context.Context, q DBTX, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := q.ExecContext(ctx, `DELETE FROM note_images WHERE id IN (`+placeholders(len(ids))+`)`, toArgs(ids)...)
	return err
}

// NoteBodyRef is a note id + its body_md, for the batched cross-note reference
// check (see Service.otherNotesImageRefs).
type NoteBodyRef struct {
	ID   string
	Body string
}

// NotesReferencingAnyImage returns the id + body_md of every note (other than
// excludeID) whose body embeds at least one image content URL. Callers extract the
// referenced image ids in Go, so one scan of the fixed `/api/notes/images/` prefix
// replaces the former per-image instr query (an O(images × notes) full-table scan on
// every save and hard delete). instr sidesteps LIKE-wildcard escaping — the prefix is
// fixed text (no % or _).
func (s *Store) NotesReferencingAnyImage(ctx context.Context, q DBTX, excludeID string) ([]NoteBodyRef, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, body_md FROM notes WHERE id <> ? AND body_md IS NOT NULL AND instr(body_md, ?) > 0`,
		excludeID, noteImageAPIBase)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NoteBodyRef
	for rows.Next() {
		var r NoteBodyRef
		if err := rows.Scan(&r.ID, &r.Body); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UnreferencedImageIDs returns the ids of note_images rows older than cutoff that NO
// note's body_md references — reclaimable leaks (an upload whose embedding body-save
// never landed, or a stray duplicate). cutoff is a tsFormat timestamp; the grace it
// encodes spares a fresh upload still mid-embed.
//
// Two flat scans intersected in Go, not a correlated `NOT EXISTS ... instr(body_md, ?
// || ni.id)` — that re-read every note body once per candidate image (O(images ×
// notes)), the same shape NotesReferencingAnyImage was introduced to replace. The two
// queries run strictly one after the other: the pool is capped at a single connection
// (D-SQLITE), so holding one rows cursor open across another query would deadlock.
func (s *Store) UnreferencedImageIDs(ctx context.Context, cutoff string) ([]string, error) {
	aged, err := s.imageIDsOlderThan(ctx, cutoff)
	if err != nil || len(aged) == 0 {
		return nil, err
	}
	// excludeID "" excludes nothing — every note's body counts here, including the
	// image's own owner.
	bodies, err := s.NotesReferencingAnyImage(ctx, s.db, "")
	if err != nil {
		return nil, err
	}
	referenced := map[string]bool{}
	for _, b := range bodies {
		for id := range referencedImageIDs(b.Body) {
			referenced[id] = true
		}
	}
	var out []string
	for _, id := range aged {
		if !referenced[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// imageIDsOlderThan lists the image rows past the sweep's grace window. Split out so
// its cursor is drained and closed before the caller opens the next query.
func (s *Store) imageIDsOlderThan(ctx context.Context, cutoff string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM note_images WHERE created_at < ?`, cutoff)
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

// CountNotes returns the number of note rows (archived included). The unreferenced-
// image sweep uses it as a safety interlock: an empty table would make every image
// look unreferenced, which reads as an unrestored database rather than a real leak.
func (s *Store) CountNotes(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notes`).Scan(&n)
	return n, err
}

// ReassignNoteImage hands an image to a new owning note. Used when its owner is
// hard-deleted while another note still references it: re-parenting the row spares
// it from the owner's ON DELETE CASCADE so the shared object survives.
func (s *Store) ReassignNoteImage(ctx context.Context, q DBTX, imageID, newNoteID string) error {
	_, err := q.ExecContext(ctx, `UPDATE note_images SET note_id = ? WHERE id = ?`, newNoteID, imageID)
	return err
}

// ExpectedImageObjects returns every object the note_images rows claim, for the
// mirror/reconciliation pass (one key per image).
func (s *Store) ExpectedImageObjects(ctx context.Context) ([]ImageObjectRef, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM note_images`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ImageObjectRef
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, ImageObjectRef{ImageID: id, Key: NoteImageKey(id)})
	}
	return out, rows.Err()
}

// ---- v9 storage catalog (D191/D194/D198) ----

// NoteImageOwners returns image id → owner: "" when the owning note is shared, the
// member's id when it is private. One query with the join, feeding the blob
// attribution — an N+1 over a few thousand objects would make the storage page slow
// enough that nobody opens it.
//
// Archived notes are INCLUDED: a soft-deleted note still holds its images (the
// delete is reversible, so the objects survive), and a page that omitted them would
// report less than the bucket holds.
func (s *Store) NoteImageOwners(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.id, COALESCE(n.owner_id, '')
		   FROM note_images i JOIN notes n ON n.id = i.note_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, owner string
		if err := rows.Scan(&id, &owner); err != nil {
			return nil, err
		}
		out[id] = owner
	}
	return out, rows.Err()
}

// PrivateInventory lists private notes, private folders and the images hanging off
// private notes, plus the total bytes across ALL matching items.
//
// ⚠ The SELECT lists are the specification (D198): id, owner, size, dates. No
// title, no name, no body. A note's `byte_size` is the length of its Markdown body
// — the only size a note has — and a folder's is 0, because a folder holds no bytes
// of its own.
func (s *Store) PrivateInventory(ctx context.Context, ownerID string) ([]storage.Item, int64, error) {
	args := []any{}
	ownerCond := ""
	if ownerID != "" {
		ownerCond = " AND owner_id = ?"
		args = append(args, ownerID)
	}

	out := []storage.Item{}
	var total int64

	noteRows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(owner_id,''), LENGTH(COALESCE(body_md,'')), created_at, updated_at
		   FROM notes WHERE visibility = 'private'`+ownerCond+` ORDER BY id DESC`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer noteRows.Close()
	for noteRows.Next() {
		var it storage.Item
		if err := noteRows.Scan(&it.ID, &it.OwnerID, &it.ByteSize, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, 0, err
		}
		it.Module, it.Kind = "notes", storage.ItemNote
		total += it.ByteSize
		out = append(out, it)
	}
	if err := noteRows.Err(); err != nil {
		return nil, 0, err
	}

	folderRows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(owner_id,''), created_at, updated_at
		   FROM folders WHERE visibility = 'private'`+ownerCond+` ORDER BY id DESC`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer folderRows.Close()
	for folderRows.Next() {
		var it storage.Item
		if err := folderRows.Scan(&it.ID, &it.OwnerID, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, 0, err
		}
		it.Module, it.Kind = "notes", storage.ItemNoteFolder
		out = append(out, it)
	}
	if err := folderRows.Err(); err != nil {
		return nil, 0, err
	}

	// Images inherit their note's visibility (D204), so the filter is on the JOINED
	// note rather than on a column of their own. Listed for accounting and marked
	// non-deletable by their kind (D212).
	imgArgs := []any{}
	imgCond := ""
	if ownerID != "" {
		imgCond = " AND n.owner_id = ?"
		imgArgs = append(imgArgs, ownerID)
	}
	imgRows, err := s.db.QueryContext(ctx,
		`SELECT i.id, COALESCE(n.owner_id,''), i.byte_size, i.created_at
		   FROM note_images i JOIN notes n ON n.id = i.note_id
		  WHERE n.visibility = 'private'`+imgCond+` ORDER BY i.id DESC`, imgArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer imgRows.Close()
	for imgRows.Next() {
		var it storage.Item
		if err := imgRows.Scan(&it.ID, &it.OwnerID, &it.ByteSize, &it.CreatedAt); err != nil {
			return nil, 0, err
		}
		it.Module, it.Kind = "notes", storage.ItemNoteImage
		total += it.ByteSize
		out = append(out, it)
	}
	return out, total, imgRows.Err()
}
