package notes

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lexorank"
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

// icon is appended last so the existing scan positions are untouched.
const folderCols = `id, parent_id, name, slug, position, archived, created_by, created_at, updated_at, icon`

func scanFolder(r interface{ Scan(...any) error }) (Folder, error) {
	var f Folder
	var parent, createdBy, icon sql.NullString
	var archived int
	if err := r.Scan(&f.ID, &parent, &f.Name, &f.Slug, &f.Position, &archived, &createdBy, &f.CreatedAt, &f.UpdatedAt, &icon); err != nil {
		return Folder{}, err
	}
	f.ParentID = ptr(parent)
	f.CreatedBy = ptr(createdBy)
	f.Archived = archived != 0
	f.Icon = icon.String
	return f, nil
}

func (s *Store) GetFolder(ctx context.Context, q DBTX, id string) (*Folder, error) {
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

func (s *Store) InsertFolder(ctx context.Context, tx DBTX, parentID *string, name, slug, position, createdBy, icon string) (*Folder, error) {
	id := idgen.New()
	now := nowUTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO folders (id, parent_id, name, slug, position, archived, created_by, created_at, updated_at, icon)
		 VALUES (?,?,?,?,?,0,?,?,?,?)`,
		id, nullable(deref(parentID)), name, slug, position, nullable(createdBy), now, now, nullable(icon)); err != nil {
		return nil, err
	}
	return s.GetFolder(ctx, tx, id)
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

func (s *Store) ChildFolderBySlug(ctx context.Context, q DBTX, parentID *string, slug string) (*Folder, error) {
	row := q.QueryRowContext(ctx,
		`SELECT `+folderCols+` FROM folders WHERE COALESCE(parent_id,'') = ? AND slug = ? AND archived = 0`,
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
		`SELECT MAX(position) FROM folders WHERE COALESCE(parent_id,'') = ? AND archived = 0`,
		deref(parentID)).Scan(&pos); err != nil {
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

const noteCols = `id, folder_id, title, slug, body_md, position, archived, created_by, created_at, updated_at`

func scanNote(r interface{ Scan(...any) error }) (Note, error) {
	var n Note
	var folder, body, createdBy sql.NullString
	var archived int
	if err := r.Scan(&n.ID, &folder, &n.Title, &n.Slug, &body, &n.Position, &archived, &createdBy, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return Note{}, err
	}
	n.FolderID = ptr(folder)
	n.BodyMD = ptr(body)
	n.CreatedBy = ptr(createdBy)
	n.Archived = archived != 0
	return n, nil
}

func (s *Store) GetNote(ctx context.Context, q DBTX, id string) (*Note, error) {
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

func (s *Store) InsertNote(ctx context.Context, tx DBTX, folderID *string, title, slug, body, position, createdBy string) (*Note, error) {
	id := idgen.New()
	now := nowUTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO notes (id, folder_id, title, slug, body_md, position, archived, created_by, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,0,?,?,?)`,
		id, nullable(deref(folderID)), title, slug, nullable(body), position, nullable(createdBy), now, now); err != nil {
		return nil, err
	}
	return s.GetNote(ctx, tx, id)
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

func (s *Store) ChildNoteBySlug(ctx context.Context, q DBTX, folderID *string, slug string) (*Note, error) {
	row := q.QueryRowContext(ctx,
		`SELECT `+noteCols+` FROM notes WHERE COALESCE(folder_id,'') = ? AND slug = ? AND archived = 0`,
		deref(folderID), slug)
	n, err := scanNote(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Store) lastNotePosition(ctx context.Context, tx DBTX, folderID *string) (string, error) {
	var pos sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(position) FROM notes WHERE COALESCE(folder_id,'') = ? AND archived = 0`,
		deref(folderID)).Scan(&pos); err != nil {
		return "", err
	}
	if !pos.Valid {
		return lexorank.First(), nil
	}
	return lexorank.Tail(pos.String), nil
}

// SiblingSlugTaken reports whether a live folder or note under parentScope already
// uses slug, excluding the given ids (the item being renamed/moved). This is the
// in-transaction cross-table half of the addressing invariant (D32) that no single
// index can express.
func (s *Store) SiblingSlugTaken(ctx context.Context, q DBTX, parentScope *string, slug, excludeFolderID, excludeNoteID string) (bool, error) {
	scope := deref(parentScope)
	var n int
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM folders WHERE COALESCE(parent_id,'') = ? AND slug = ? AND archived = 0 AND id != ?`,
		scope, slug, excludeFolderID).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notes WHERE COALESCE(folder_id,'') = ? AND slug = ? AND archived = 0 AND id != ?`,
		scope, slug, excludeNoteID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// SearchNotes runs an FTS5 MATCH over title+body, newest-updated first, capped.
// It honors the same folder/archived scoping as the folder listing: nil folderID
// searches all folders, and archived rows are excluded unless includeArchived.
// Reads only.
func (s *Store) SearchNotes(ctx context.Context, query string, folderID *string, includeArchived bool, limit int) ([]NoteSummary, error) {
	conds := []string{"notes_fts MATCH ?"}
	args := []any{query}
	if !includeArchived {
		conds = append(conds, "n.archived = 0")
	}
	if folderID != nil {
		conds = append(conds, "COALESCE(n.folder_id,'') = ?")
		args = append(args, *folderID)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT n.id, n.folder_id, n.title, n.slug, n.position, n.archived, n.updated_at
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

const noteSummaryCols = `id, folder_id, title, slug, position, archived, updated_at`

func scanNoteSummary(r interface{ Scan(...any) error }) (NoteSummary, error) {
	var sm NoteSummary
	var folder sql.NullString
	var archived int
	if err := r.Scan(&sm.ID, &folder, &sm.Title, &sm.Slug, &sm.Position, &archived, &sm.UpdatedAt); err != nil {
		return NoteSummary{}, err
	}
	sm.FolderID = ptr(folder)
	sm.Archived = archived != 0
	return sm, nil
}

// AllFolders returns every folder (optionally including archived), ordered.
func (s *Store) AllFolders(ctx context.Context, includeArchived bool) ([]Folder, error) {
	where := ""
	if !includeArchived {
		where = " WHERE archived = 0"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+folderCols+` FROM folders`+where+` ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
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

// AllNoteSummaries returns every note as a lightweight summary (no body), ordered.
func (s *Store) AllNoteSummaries(ctx context.Context, includeArchived bool) ([]NoteSummary, error) {
	where := ""
	if !includeArchived {
		where = " WHERE archived = 0"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+noteSummaryCols+` FROM notes`+where+` ORDER BY position, id`)
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

// NoteSummariesInFolder returns the non-archived note summaries directly under a
// folder (or root), ordered — for FolderDetail.
func (s *Store) NoteSummariesInFolder(ctx context.Context, q DBTX, folderID *string, includeArchived bool) ([]NoteSummary, error) {
	cond := "archived = 0"
	if includeArchived {
		cond = "1=1"
	}
	rows, err := q.QueryContext(ctx,
		`SELECT `+noteSummaryCols+` FROM notes WHERE COALESCE(folder_id,'') = ? AND `+cond+` ORDER BY position, id`,
		deref(folderID))
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

// ChildFolders returns the non-archived child folders of parentID (or root),
// ordered — for FolderDetail.
func (s *Store) ChildFolders(ctx context.Context, q DBTX, parentID *string, includeArchived bool) ([]Folder, error) {
	cond := "archived = 0"
	if includeArchived {
		cond = "1=1"
	}
	rows, err := q.QueryContext(ctx,
		`SELECT `+folderCols+` FROM folders WHERE COALESCE(parent_id,'') = ? AND `+cond+` ORDER BY position, id`,
		deref(parentID))
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
	NoteID    string
	FolderID  *string
	Title     string
	Slug      string
	BodyMD    *string
	UpdatedAt string
	Scope     string
	Position  string
}

// PinnedRowsFor returns the household pins and this user's personal pins joined to
// their (non-archived) notes, ordered by pin position within each scope. One
// bounded query, no N+1.
func (s *Store) PinnedRowsFor(ctx context.Context, userID string) ([]pinRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.note_id, n.folder_id, n.title, n.slug, n.body_md, n.updated_at, p.scope, p.position
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
		if err := rows.Scan(&r.NoteID, &folder, &r.Title, &r.Slug, &body, &r.UpdatedAt, &r.Scope, &r.Position); err != nil {
			return nil, err
		}
		r.FolderID = ptr(folder)
		r.BodyMD = ptr(body)
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
