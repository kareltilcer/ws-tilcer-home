package todo

import (
	"context"
	"strings"
)

// Tree returns the board read model: columns in position order, each with its
// cards in position order, every card carrying label ids and checklist progress
// (but not full links/checklist — those load with the card detail). Empty
// columns still render so cards can be dropped into them (FR-T7).
//
// Filters narrow the cards: label (any of the given ids), q (title/notes), and
// include_archived. It runs a bounded number of queries (columns, cards,
// label ids, progress) — no per-card round trip.
func (s *Store) Tree(ctx context.Context, boardID string, labelIDs []string, q string, includeArchived bool) (*BoardTree, error) {
	board, err := s.GetBoard(ctx, s.db, boardID)
	if err != nil || board == nil {
		return nil, err
	}
	columns, err := s.ListColumns(ctx, s.db, boardID)
	if err != nil {
		return nil, err
	}

	// Cards for the whole board, filtered, ordered by (column, position).
	sb := strings.Builder{}
	sb.WriteString(`SELECT c.id, c.column_id, c.title, c.notes, c.position, c.archived, c.done_at,
		c.created_by, c.created_at, c.updated_at
		FROM cards c JOIN columns col ON col.id = c.column_id
		WHERE col.board_id = ?`)
	args := []any{boardID}
	if !includeArchived {
		sb.WriteString(" AND c.archived = 0")
	}
	if q != "" {
		sb.WriteString(" AND (c.title LIKE ? OR IFNULL(c.notes,'') LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if len(labelIDs) > 0 {
		sb.WriteString(" AND c.id IN (SELECT card_id FROM card_labels WHERE label_id IN (")
		sb.WriteString(placeholders(len(labelIDs)))
		sb.WriteString("))")
		for _, id := range labelIDs {
			args = append(args, id)
		}
	}
	sb.WriteString(" ORDER BY c.column_id, c.position, c.id")

	rows, err := s.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Accumulate into a stable flat slice; index maps card id -> flat position so
	// batch fills mutate the canonical Card (no pointers into a growing slice).
	var cards []Card
	index := map[string]int{}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		index[c.ID] = len(cards)
		cards = append(cards, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	cardIDs := make([]string, 0, len(cards))
	for i := range cards {
		cardIDs = append(cardIDs, cards[i].ID)
	}
	if err := s.fillLabelIDs(ctx, cardIDs, cards, index); err != nil {
		return nil, err
	}
	if err := s.fillProgress(ctx, cardIDs, cards, index); err != nil {
		return nil, err
	}

	// Group into columns (order preserved by the SQL ORDER BY).
	byColumn := map[string][]Card{}
	for _, c := range cards {
		byColumn[c.ColumnID] = append(byColumn[c.ColumnID], c)
	}

	tree := &BoardTree{Board: *board}
	for _, col := range columns {
		colCards := byColumn[col.ID]
		if colCards == nil {
			colCards = []Card{}
		}
		tree.Columns = append(tree.Columns, BoardTreeColumn{Column: col, Cards: colCards})
	}
	return tree, nil
}

// fillLabelIDs loads label ids for all cards in one query and assigns them.
func (s *Store) fillLabelIDs(ctx context.Context, cardIDs []string, cards []Card, index map[string]int) error {
	if len(cardIDs) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT card_id, label_id FROM card_labels WHERE card_id IN (`+placeholders(len(cardIDs))+`)`, toArgs(cardIDs)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cardID, labelID string
		if err := rows.Scan(&cardID, &labelID); err != nil {
			return err
		}
		if i, ok := index[cardID]; ok {
			cards[i].LabelIDs = append(cards[i].LabelIDs, labelID)
		}
	}
	return rows.Err()
}

// fillProgress loads checklist progress for all cards in one query.
func (s *Store) fillProgress(ctx context.Context, cardIDs []string, cards []Card, index map[string]int) error {
	if len(cardIDs) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT card_id, COUNT(*), COALESCE(SUM(done),0) FROM checklist_items
		 WHERE card_id IN (`+placeholders(len(cardIDs))+`) GROUP BY card_id`, toArgs(cardIDs)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cardID string
		var total, done int
		if err := rows.Scan(&cardID, &total, &done); err != nil {
			return err
		}
		if i, ok := index[cardID]; ok {
			cards[i].ChecklistProgress = ChecklistProgress{Done: done, Total: total}
		}
	}
	return rows.Err()
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func toArgs(ss []string) []any {
	args := make([]any, len(ss))
	for i, s := range ss {
		args[i] = s
	}
	return args
}
