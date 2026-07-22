package audit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/db"
)

// Prune deletes audit events older than retentionDays and, if it deleted
// anything, records its own `logging.prune` event (a system action) in the same
// transaction — the module logging itself (FR-L7). retentionDays <= 0 means keep
// forever (the default), in which case Prune is a no-op.
//
// The DELETE fires the FTS delete trigger per row, so the search index stays in
// sync; the prune event is inserted after the delete and is newer than the
// cutoff, so it is never itself pruned.
func Prune(ctx context.Context, db *sql.DB, sink Sink, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays).Format(TSLayout)

	var deleted int64
	err := appdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "DELETE FROM audit_events WHERE ts < ?", cutoff)
		if err != nil {
			return err
		}
		deleted, _ = res.RowsAffected()
		if deleted > 0 {
			if _, err := sink.Record(ctx, tx, Event{
				Module:  ModuleLogging,
				Action:  "prune",
				Summary: fmt.Sprintf("Vymazáno %d starých záznamů (starších než %d dní)", deleted, retentionDays),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return int(deleted), nil
}
