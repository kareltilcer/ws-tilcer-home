package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/lexorank"
)

// SeedIfEmpty inserts the default board only when no board exists yet (FR-T1).
// The empty-check — not a migration one-shot — is what prevents a fresh build
// restored from Litestream/R2 from double-seeding: a restored DB already has its
// board, so seeding is skipped.
//
// It seeds exactly one board, "Domácnost", with three columns: Zásobník
// (normal), Právě dělám (now), Hotovo (done). Reports whether it seeded.
func SeedIfEmpty(ctx context.Context, sqldb *sql.DB) (seeded bool, err error) {
	var count int
	if err = sqldb.QueryRowContext(ctx, "SELECT COUNT(*) FROM boards").Scan(&count); err != nil {
		return false, fmt.Errorf("count boards: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	boardID := idgen.New()

	err = WithTx(ctx, sqldb, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO boards (id, name, description, position, created_by, created_at, archived)
			 VALUES (?, ?, NULL, ?, NULL, ?, 0)`,
			boardID, "Domácnost", lexorank.First(), now,
		); err != nil {
			return fmt.Errorf("seed board: %w", err)
		}

		positions := lexorank.NKeys(3)
		cols := []struct {
			name string
			kind string
		}{
			{"Zásobník", "normal"},
			{"Právě dělám", "now"},
			{"Hotovo", "done"},
		}
		for i, c := range cols {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO columns (id, board_id, name, priority, position, kind, created_at)
				 VALUES (?, ?, ?, 0, ?, ?, ?)`,
				idgen.New(), boardID, c.name, positions[i], c.kind, now,
			); err != nil {
				return fmt.Errorf("seed column %q: %w", c.name, err)
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return true, nil
}
