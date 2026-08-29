package todo

import (
	"context"
	"database/sql"
	"strings"
	"time"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
)

// CountCardsInKind counts non-archived cards sitting in columns of the given
// kinds, across non-archived boards — the counting half of the Právě dělám
// population, for the metrics catalog (D69).
func (s *Store) CountCardsInKind(ctx context.Context, kinds []string) (int, error) {
	if len(kinds) == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(kinds))
	for _, k := range kinds {
		args = append(args, k)
	}
	q := `SELECT COUNT(*)
	        FROM cards c
	        JOIN columns col ON col.id = c.column_id AND col.kind IN (` +
		strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",") + `)
	        JOIN boards b ON b.id = col.board_id AND b.archived = 0
	       WHERE c.archived = 0`
	var n int
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

// CountDoneBetween counts cards stamped done within [from, to). done_at is set
// on the move into a done column and cleared on the way out, so this is "how
// many were finished in this window" without touching the audit log.
func (s *Store) CountDoneBetween(ctx context.Context, from, to time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		  FROM cards c
		  JOIN columns col ON col.id = c.column_id
		  JOIN boards b ON b.id = col.board_id AND b.archived = 0
		 WHERE c.archived = 0
		   AND c.done_at IS NOT NULL
		   AND c.done_at >= ? AND c.done_at < ?`,
		from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339)).Scan(&n)
	return n, err
}

// TitlesInKind returns the titles of the same cards CountCardsInKind counts, in
// board → column → card order (the order the board itself reads in), for the
// list catalog. The count and the list must select identically, so the WHERE
// clause here is the count's clause — only the projection differs.
func (s *Store) TitlesInKind(ctx context.Context, kinds []string) ([]string, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(kinds))
	for _, k := range kinds {
		args = append(args, k)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.title
		  FROM cards c
		  JOIN columns col ON col.id = c.column_id AND col.kind IN (`+appdb.Placeholders(len(kinds))+`)
		  JOIN boards b ON b.id = col.board_id AND b.archived = 0
		 WHERE c.archived = 0
		 ORDER BY b.position, col.priority, col.position, c.position, c.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTitles(rows)
}

// TitlesDoneBetween returns the titles of the cards CountDoneBetween counts,
// newest first — an evening summary reads best ending with what was just done.
func (s *Store) TitlesDoneBetween(ctx context.Context, from, to time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.title
		  FROM cards c
		  JOIN columns col ON col.id = c.column_id
		  JOIN boards b ON b.id = col.board_id AND b.archived = 0
		 WHERE c.archived = 0
		   AND c.done_at IS NOT NULL
		   AND c.done_at >= ? AND c.done_at < ?
		 ORDER BY c.done_at DESC, c.id`,
		from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTitles(rows)
}

func scanTitles(rows *sql.Rows) ([]string, error) {
	var out []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			return nil, err
		}
		out = append(out, title)
	}
	return out, rows.Err()
}

// NowCard is a card sitting in a kind='now' column, with its board/column
// context — the shape the dashboard aggregates (FR-N1). Defined here (not in the
// dashboard package) so the SQL stays with the todo domain; the dashboard maps
// it to its own wire type.
type NowCard struct {
	CardID            string
	Title             string
	BoardID           string
	BoardName         string
	ColumnID          string
	ColumnName        string
	LabelIDs          []string
	ChecklistProgress ChecklistProgress
}

// NowCards returns every non-archived card in any kind='now' column across all
// non-archived boards, sorted by board order → column priority → card position.
// One join across boards (uses the columns(kind) index), then batched label-id
// and checklist-progress fills — no per-card round trip (the landing route must
// not N+1).
func (s *Store) NowCards(ctx context.Context) ([]NowCard, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.title, b.id, b.name, col.id, col.name
		FROM cards c
		JOIN columns col ON col.id = c.column_id AND col.kind = 'now'
		JOIN boards b ON b.id = col.board_id AND b.archived = 0
		WHERE c.archived = 0
		ORDER BY b.position, col.priority, col.position, c.position, c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cards []NowCard
	index := map[string]int{}
	var ids []string
	for rows.Next() {
		var n NowCard
		if err := rows.Scan(&n.CardID, &n.Title, &n.BoardID, &n.BoardName, &n.ColumnID, &n.ColumnName); err != nil {
			return nil, err
		}
		n.LabelIDs = []string{}
		index[n.CardID] = len(cards)
		cards = append(cards, n)
		ids = append(ids, n.CardID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return cards, nil
	}

	// Batch label ids.
	lrows, err := s.db.QueryContext(ctx,
		`SELECT card_id, label_id FROM card_labels WHERE card_id IN (`+appdb.Placeholders(len(ids))+`)`, toArgs(ids)...)
	if err != nil {
		return nil, err
	}
	defer lrows.Close()
	for lrows.Next() {
		var cardID, labelID string
		if err := lrows.Scan(&cardID, &labelID); err != nil {
			return nil, err
		}
		if i, ok := index[cardID]; ok {
			cards[i].LabelIDs = append(cards[i].LabelIDs, labelID)
		}
	}
	if err := lrows.Err(); err != nil {
		return nil, err
	}

	// Batch checklist progress.
	prows, err := s.db.QueryContext(ctx,
		`SELECT card_id, COUNT(*), COALESCE(SUM(done),0) FROM checklist_items
		 WHERE card_id IN (`+appdb.Placeholders(len(ids))+`) GROUP BY card_id`, toArgs(ids)...)
	if err != nil {
		return nil, err
	}
	defer prows.Close()
	for prows.Next() {
		var cardID string
		var total, done int
		if err := prows.Scan(&cardID, &total, &done); err != nil {
			return nil, err
		}
		if i, ok := index[cardID]; ok {
			cards[i].ChecklistProgress = ChecklistProgress{Done: done, Total: total}
		}
	}
	return cards, prows.Err()
}

// FirstDoneColumns returns, per non-archived board, the id of its first
// kind='done' column (by position). Boards with no done column are absent — the
// dashboard archives such a board's card instead of moving it (D15).
func (s *Store) FirstDoneColumns(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT col.board_id, col.id
		FROM columns col
		JOIN boards b ON b.id = col.board_id AND b.archived = 0
		WHERE col.kind = 'done'
		ORDER BY col.board_id, col.position, col.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var boardID, colID string
		if err := rows.Scan(&boardID, &colID); err != nil {
			return nil, err
		}
		if _, seen := out[boardID]; !seen { // first (lowest position) wins
			out[boardID] = colID
		}
	}
	return out, rows.Err()
}
