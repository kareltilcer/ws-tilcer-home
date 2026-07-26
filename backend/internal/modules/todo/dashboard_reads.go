package todo

import "context"

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
		`SELECT card_id, label_id FROM card_labels WHERE card_id IN (`+placeholders(len(ids))+`)`, toArgs(ids)...)
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
		 WHERE card_id IN (`+placeholders(len(ids))+`) GROUP BY card_id`, toArgs(ids)...)
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
