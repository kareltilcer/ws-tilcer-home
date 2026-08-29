package db

import (
	"context"
	"database/sql"
)

// DBTX is satisfied by both *sql.DB and *sql.Tx, so a store method works on
// either a pooled connection (reads outside a mutation) or the caller's
// transaction. Everything inside a mutation takes the explicit tx, so the change
// and the audit event that records it commit atomically — the WithTx contract,
// expressed in the store's signatures.
//
// ⚠ INSIDE A WithTx CALLBACK EVERY READ MUST GO THROUGH THE TX. The pool is
// capped at a single connection (Open), so a pooled read from inside a
// transaction deadlocks against that transaction's own write lock.
//
// ⚠ IT LIVES HERE BECAUSE IT EXISTED SEVEN TIMES. `documents`, `electricity`,
// `events`, `finance`, `garden`, `notes` and `todo` each declared these same
// three methods, and every one of them already imports this package — DBTX *is*
// the seam onto platform/db, so this is where the seam belongs. Each module keeps
// `type DBTX = appdb.DBTX`, an ALIAS and not a forwarder: the name stays that
// module's own vocabulary across ~250 signatures while there is exactly one type,
// so the seven are the same Go type rather than seven that merely look alike.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
