package todo

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/lexorank"
)

// DBTX is satisfied by both *sql.DB and *sql.Tx, so read helpers work on either
// a pooled connection (reads) or the caller's transaction (post-mutation reads).
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Store holds the database handle for reads; mutations take an explicit tx.
type Store struct{ db *sql.DB }

// NewStore returns a todo store over db.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }

func ptr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

// nullableText maps a request string to a nullable SQL value (empty ⇒ NULL).
func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ---- Boards ----

func (s *Store) ListBoards(ctx context.Context) ([]Board, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, position, archived, created_by, created_at
		 FROM boards WHERE archived = 0 ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Board
	for rows.Next() {
		b, err := scanBoard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) GetBoard(ctx context.Context, q DBTX, id string) (*Board, error) {
	row := q.QueryRowContext(ctx,
		`SELECT id, name, description, position, archived, created_by, created_at
		 FROM boards WHERE id = ?`, id)
	b, err := scanBoard(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Store) lastBoardPosition(ctx context.Context, tx DBTX) (string, error) {
	var pos sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT MAX(position) FROM boards`).Scan(&pos); err != nil {
		return "", err
	}
	if !pos.Valid {
		return lexorank.First(), nil
	}
	return lexorank.Tail(pos.String), nil
}

func (s *Store) InsertBoard(ctx context.Context, tx DBTX, name, description, position, createdBy string) (*Board, error) {
	id := idgen.New()
	now := nowUTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO boards (id, name, description, position, created_by, created_at, archived)
		 VALUES (?,?,?,?,?,?,0)`,
		id, name, nullableText(description), position, nullableText(createdBy), now); err != nil {
		return nil, err
	}
	return s.GetBoard(ctx, tx, id)
}

func (s *Store) UpdateBoard(ctx context.Context, tx DBTX, id string, u BoardUpdate) error {
	sets, args := []string{}, []any{}
	if u.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *u.Name)
	}
	if u.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, nullableText(*u.Description))
	}
	if u.Archived != nil {
		sets = append(sets, "archived = ?")
		args = append(args, boolInt(*u.Archived))
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := tx.ExecContext(ctx, `UPDATE boards SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

func (s *Store) DeleteBoard(ctx context.Context, tx DBTX, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM boards WHERE id = ?`, id)
	return err
}

// ---- Columns ----

func (s *Store) ListColumns(ctx context.Context, q DBTX, boardID string) ([]Column, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, board_id, name, priority, position, kind, created_at
		 FROM columns WHERE board_id = ? ORDER BY position, id`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Column
	for rows.Next() {
		c, err := scanColumn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetColumn(ctx context.Context, q DBTX, id string) (*Column, error) {
	row := q.QueryRowContext(ctx,
		`SELECT id, board_id, name, priority, position, kind, created_at FROM columns WHERE id = ?`, id)
	c, err := scanColumn(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) lastColumnPosition(ctx context.Context, tx DBTX, boardID string) (string, error) {
	var pos sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT MAX(position) FROM columns WHERE board_id = ?`, boardID).Scan(&pos); err != nil {
		return "", err
	}
	if !pos.Valid {
		return lexorank.First(), nil
	}
	return lexorank.Tail(pos.String), nil
}

func (s *Store) InsertColumn(ctx context.Context, tx DBTX, boardID, name string, priority int, position, kind string) (*Column, error) {
	id := idgen.New()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO columns (id, board_id, name, priority, position, kind, created_at)
		 VALUES (?,?,?,?,?,?,?)`,
		id, boardID, name, priority, position, kind, nowUTC()); err != nil {
		return nil, err
	}
	return s.GetColumn(ctx, tx, id)
}

func (s *Store) UpdateColumn(ctx context.Context, tx DBTX, id string, u ColumnUpdate) error {
	sets, args := []string{}, []any{}
	if u.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *u.Name)
	}
	if u.Priority != nil {
		sets = append(sets, "priority = ?")
		args = append(args, *u.Priority)
	}
	if u.Kind != nil {
		sets = append(sets, "kind = ?")
		args = append(args, *u.Kind)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := tx.ExecContext(ctx, `UPDATE columns SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

func (s *Store) MoveColumn(ctx context.Context, tx DBTX, id, position string) error {
	_, err := tx.ExecContext(ctx, `UPDATE columns SET position = ? WHERE id = ?`, position, id)
	return err
}

func (s *Store) ColumnCardCount(ctx context.Context, q DBTX, columnID string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM cards WHERE column_id = ?`, columnID).Scan(&n)
	return n, err
}

// ColumnCards lists a column's cards (base fields only) using q — usable inside
// a transaction. Used by the cascade delete to log each card before removal;
// unlike Tree, it does NOT touch s.db, so it is safe with a single-connection
// pool while a transaction is open.
func (s *Store) ColumnCards(ctx context.Context, q DBTX, columnID string) ([]Card, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, column_id, title, notes, position, archived, done_at, created_by, created_at, updated_at
		 FROM cards WHERE column_id = ? ORDER BY position, id`, columnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteColumn(ctx context.Context, tx DBTX, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM columns WHERE id = ?`, id)
	return err
}

// ---- Cards ----

func (s *Store) loadCard(ctx context.Context, q DBTX, id string) (*Card, error) {
	row := q.QueryRowContext(ctx,
		`SELECT id, column_id, title, notes, position, archived, done_at, created_by, created_at, updated_at
		 FROM cards WHERE id = ?`, id)
	c, err := scanCard(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	labels, err := s.cardLabelIDs(ctx, q, id)
	if err != nil {
		return nil, err
	}
	c.LabelIDs = labels
	prog, err := s.checklistProgress(ctx, q, id)
	if err != nil {
		return nil, err
	}
	c.ChecklistProgress = prog
	return &c, nil
}

// GetCard returns a card with label ids and checklist progress.
func (s *Store) GetCard(ctx context.Context, q DBTX, id string) (*Card, error) {
	return s.loadCard(ctx, q, id)
}

// GetCardDetail returns a card plus its links, checklist and labels.
func (s *Store) GetCardDetail(ctx context.Context, id string) (*CardDetail, error) {
	card, err := s.loadCard(ctx, s.db, id)
	if err != nil || card == nil {
		return nil, err
	}
	links, err := s.ListCardLinks(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	checklist, err := s.ListChecklist(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	labels, err := s.cardLabels(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	return &CardDetail{Card: *card, Links: links, Checklist: checklist, Labels: labels}, nil
}

func (s *Store) cardLabelIDs(ctx context.Context, q DBTX, cardID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT label_id FROM card_labels WHERE card_id = ?`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) checklistProgress(ctx context.Context, q DBTX, cardID string) (ChecklistProgress, error) {
	var total, done int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(done),0) FROM checklist_items WHERE card_id = ?`, cardID).Scan(&total, &done)
	return ChecklistProgress{Done: done, Total: total}, err
}

func (s *Store) lastCardPosition(ctx context.Context, tx DBTX, columnID string) (string, error) {
	var pos sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT MAX(position) FROM cards WHERE column_id = ?`, columnID).Scan(&pos); err != nil {
		return "", err
	}
	if !pos.Valid {
		return lexorank.First(), nil
	}
	return lexorank.Tail(pos.String), nil
}

func (s *Store) InsertCard(ctx context.Context, tx DBTX, columnID, title, notes, position, createdBy string) (*Card, error) {
	id := idgen.New()
	now := nowUTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO cards (id, column_id, title, notes, position, created_by, created_at, updated_at, done_at, archived)
		 VALUES (?,?,?,?,?,?,?,?,NULL,0)`,
		id, columnID, title, nullableText(notes), position, nullableText(createdBy), now, now); err != nil {
		return nil, err
	}
	return s.loadCard(ctx, tx, id)
}

func (s *Store) UpdateCard(ctx context.Context, tx DBTX, id string, u CardUpdate) error {
	sets := []string{"updated_at = ?"}
	args := []any{nowUTC()}
	if u.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *u.Title)
	}
	if u.Notes != nil {
		sets = append(sets, "notes = ?")
		args = append(args, nullableText(*u.Notes))
	}
	if u.Archived != nil {
		sets = append(sets, "archived = ?")
		args = append(args, boolInt(*u.Archived))
	}
	args = append(args, id)
	_, err := tx.ExecContext(ctx, `UPDATE cards SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

// MoveCard sets a card's column and position. Entering a kind=done column stamps
// done_at; leaving one clears it. isDone tells whether the target column is done.
func (s *Store) MoveCard(ctx context.Context, tx DBTX, id, columnID, position string, targetIsDone bool) error {
	doneAt := "done_at = CASE WHEN ? = 1 THEN COALESCE(done_at, ?) ELSE NULL END"
	_, err := tx.ExecContext(ctx,
		`UPDATE cards SET column_id = ?, position = ?, updated_at = ?, `+doneAt+` WHERE id = ?`,
		columnID, position, nowUTC(), boolInt(targetIsDone), nowUTC(), id)
	return err
}

func (s *Store) DeleteCard(ctx context.Context, tx DBTX, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM cards WHERE id = ?`, id)
	return err
}

// ---- Card links ----

func (s *Store) ListCardLinks(ctx context.Context, q DBTX, cardID string) ([]CardLink, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, card_id, url, title, position FROM card_links WHERE card_id = ? ORDER BY position, id`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CardLink{}
	for rows.Next() {
		var l CardLink
		var title sql.NullString
		if err := rows.Scan(&l.ID, &l.CardID, &l.URL, &title, &l.Position); err != nil {
			return nil, err
		}
		l.Title = ptr(title)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) InsertCardLink(ctx context.Context, tx DBTX, cardID, url, title, position string) (*CardLink, error) {
	id := idgen.New()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO card_links (id, card_id, url, title, position) VALUES (?,?,?,?,?)`,
		id, cardID, url, nullableText(title), position); err != nil {
		return nil, err
	}
	row := tx.QueryRowContext(ctx, `SELECT id, card_id, url, title, position FROM card_links WHERE id = ?`, id)
	var l CardLink
	var t sql.NullString
	if err := row.Scan(&l.ID, &l.CardID, &l.URL, &t, &l.Position); err != nil {
		return nil, err
	}
	l.Title = ptr(t)
	return &l, nil
}

// LinkCardID returns the parent card id of a link, or "" if the link is unknown.
func (s *Store) LinkCardID(ctx context.Context, q DBTX, linkID string) (string, error) {
	var cardID string
	err := q.QueryRowContext(ctx, `SELECT card_id FROM card_links WHERE id = ?`, linkID).Scan(&cardID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return cardID, err
}

func (s *Store) DeleteCardLink(ctx context.Context, tx DBTX, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM card_links WHERE id = ?`, id)
	return err
}

func (s *Store) lastLinkPosition(ctx context.Context, tx DBTX, cardID string) (string, error) {
	var pos sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT MAX(position) FROM card_links WHERE card_id = ?`, cardID).Scan(&pos); err != nil {
		return "", err
	}
	if !pos.Valid {
		return lexorank.First(), nil
	}
	return lexorank.Tail(pos.String), nil
}

// ---- Checklist ----

func (s *Store) ListChecklist(ctx context.Context, q DBTX, cardID string) ([]ChecklistItem, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, card_id, text, done, position FROM checklist_items WHERE card_id = ? ORDER BY position, id`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChecklistItem{}
	for rows.Next() {
		var it ChecklistItem
		var done int
		if err := rows.Scan(&it.ID, &it.CardID, &it.Text, &done, &it.Position); err != nil {
			return nil, err
		}
		it.Done = done != 0
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) GetChecklistItem(ctx context.Context, q DBTX, id string) (*ChecklistItem, error) {
	row := q.QueryRowContext(ctx, `SELECT id, card_id, text, done, position FROM checklist_items WHERE id = ?`, id)
	var it ChecklistItem
	var done int
	if err := row.Scan(&it.ID, &it.CardID, &it.Text, &done, &it.Position); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	it.Done = done != 0
	return &it, nil
}

func (s *Store) InsertChecklistItem(ctx context.Context, tx DBTX, cardID, text, position string) (*ChecklistItem, error) {
	id := idgen.New()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO checklist_items (id, card_id, text, done, position) VALUES (?,?,?,0,?)`,
		id, cardID, text, position); err != nil {
		return nil, err
	}
	return s.GetChecklistItem(ctx, tx, id)
}

func (s *Store) UpdateChecklistItem(ctx context.Context, tx DBTX, id string, u ChecklistItemUpdate) error {
	sets, args := []string{}, []any{}
	if u.Text != nil {
		sets = append(sets, "text = ?")
		args = append(args, *u.Text)
	}
	if u.Done != nil {
		sets = append(sets, "done = ?")
		args = append(args, boolInt(*u.Done))
	}
	if u.Position != nil {
		sets = append(sets, "position = ?")
		args = append(args, *u.Position)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := tx.ExecContext(ctx, `UPDATE checklist_items SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

func (s *Store) DeleteChecklistItem(ctx context.Context, tx DBTX, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM checklist_items WHERE id = ?`, id)
	return err
}

func (s *Store) lastChecklistPosition(ctx context.Context, tx DBTX, cardID string) (string, error) {
	var pos sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT MAX(position) FROM checklist_items WHERE card_id = ?`, cardID).Scan(&pos); err != nil {
		return "", err
	}
	if !pos.Valid {
		return lexorank.First(), nil
	}
	return lexorank.Tail(pos.String), nil
}

// ---- Labels ----

func (s *Store) ListLabels(ctx context.Context, q DBTX, boardID string) ([]Label, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, board_id, name, color FROM labels WHERE board_id = ? ORDER BY name`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Label{}
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.ID, &l.BoardID, &l.Name, &l.Color); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) GetLabel(ctx context.Context, q DBTX, id string) (*Label, error) {
	var l Label
	err := q.QueryRowContext(ctx, `SELECT id, board_id, name, color FROM labels WHERE id = ?`, id).
		Scan(&l.ID, &l.BoardID, &l.Name, &l.Color)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (s *Store) InsertLabel(ctx context.Context, tx DBTX, boardID, name, color string) (*Label, error) {
	id := idgen.New()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO labels (id, board_id, name, color, created_at) VALUES (?,?,?,?,?)`,
		id, boardID, name, color, nowUTC()); err != nil {
		return nil, err
	}
	return s.GetLabel(ctx, tx, id)
}

func (s *Store) UpdateLabel(ctx context.Context, tx DBTX, id, name, color string) error {
	_, err := tx.ExecContext(ctx, `UPDATE labels SET name = ?, color = ? WHERE id = ?`, name, color, id)
	return err
}

func (s *Store) DeleteLabel(ctx context.Context, tx DBTX, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM labels WHERE id = ?`, id)
	return err
}

func (s *Store) AttachLabel(ctx context.Context, tx DBTX, cardID, labelID string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO card_labels (card_id, label_id) VALUES (?,?)`, cardID, labelID)
	return err
}

func (s *Store) DetachLabel(ctx context.Context, tx DBTX, cardID, labelID string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM card_labels WHERE card_id = ? AND label_id = ?`, cardID, labelID)
	return err
}

func (s *Store) cardLabels(ctx context.Context, q DBTX, cardID string) ([]Label, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT l.id, l.board_id, l.name, l.color
		 FROM labels l JOIN card_labels cl ON cl.label_id = l.id
		 WHERE cl.card_id = ? ORDER BY l.name`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Label{}
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.ID, &l.BoardID, &l.Name, &l.Color); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ---- Scanners ----

type rowScanner interface{ Scan(dest ...any) error }

func scanBoard(r rowScanner) (Board, error) {
	var b Board
	var desc, createdBy sql.NullString
	var archived int
	if err := r.Scan(&b.ID, &b.Name, &desc, &b.Position, &archived, &createdBy, &b.CreatedAt); err != nil {
		return Board{}, err
	}
	b.Description = ptr(desc)
	b.CreatedBy = ptr(createdBy)
	b.Archived = archived != 0
	return b, nil
}

func scanColumn(r rowScanner) (Column, error) {
	var c Column
	if err := r.Scan(&c.ID, &c.BoardID, &c.Name, &c.Priority, &c.Position, &c.Kind, &c.CreatedAt); err != nil {
		return Column{}, err
	}
	return c, nil
}

func scanCard(r rowScanner) (Card, error) {
	var c Card
	var notes, doneAt, createdBy sql.NullString
	var archived int
	if err := r.Scan(&c.ID, &c.ColumnID, &c.Title, &notes, &c.Position, &archived, &doneAt, &createdBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return Card{}, err
	}
	c.Notes = ptr(notes)
	c.DoneAt = ptr(doneAt)
	c.CreatedBy = ptr(createdBy)
	c.Archived = archived != 0
	c.LabelIDs = []string{}
	return c, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
