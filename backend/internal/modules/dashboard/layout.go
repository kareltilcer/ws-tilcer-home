package dashboard

import (
	"context"
	"database/sql"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
)

// LayoutStore persists each user's widget layout (the module's only table).
type LayoutStore struct{ db *sql.DB }

// NewLayoutStore returns a layout store over db.
func NewLayoutStore(db *sql.DB) *LayoutStore { return &LayoutStore{db: db} }

// Get returns the user's saved layout in display order (empty when the user has
// never arranged their dashboard — the host then supplies a default).
func (s *LayoutStore) Get(ctx context.Context, userID string) ([]LayoutItem, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT widget_key, visible, position, size FROM user_dashboard_layout
		 WHERE user_id = ? ORDER BY position`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LayoutItem
	for rows.Next() {
		var li LayoutItem
		var visible int
		if err := rows.Scan(&li.WidgetKey, &visible, &li.Position, &li.Size); err != nil {
			return nil, err
		}
		li.Visible = visible != 0
		out = append(out, li)
	}
	return out, rows.Err()
}

// Save replaces the user's layout with items (already ordered + positioned). A
// full replace keeps the row set in sync with the client's arrangement — removed
// widgets drop out. Layout is a personal VIEW preference, not feature data, so it
// is not written through the audit spine (like the client-side column collapse).
func (s *LayoutStore) Save(ctx context.Context, userID string, items []LayoutItem) error {
	return appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_dashboard_layout WHERE user_id = ?`, userID); err != nil {
			return err
		}
		for _, li := range items {
			visible := 0
			if li.Visible {
				visible = 1
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO user_dashboard_layout (user_id, widget_key, visible, position, size)
				 VALUES (?,?,?,?,?)`,
				userID, li.WidgetKey, visible, li.Position, li.Size); err != nil {
				return err
			}
		}
		return nil
	})
}
