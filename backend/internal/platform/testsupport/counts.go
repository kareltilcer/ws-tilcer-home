package testsupport

import (
	"database/sql"
	"testing"
)

// CountRows returns the row count of table, failing the test on a query error.
//
// ⚠ IT LIVES HERE BECAUSE IT EXISTED FOUR TIMES — byte-identical in `documents`,
// `electricity`, `finance` and `platform/audit`'s own test. All four are external
// `_test` packages that already import testsupport for NewDB, so adopting it
// costs nothing. (`bootstrap` has a differently-shaped `countRows` returning a
// map of every table; that one is left alone.)
//
// The table name is interpolated — every caller passes a literal, and a test is
// not a request surface.
func CountRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// CountAudit counts rows in audit_events, narrowed by module and action. An
// EMPTY string means "any", so CountAudit(t, db, "", "") is every event.
//
// The four copies this replaces each hard-coded a different narrowing — `chat`
// counted everything, `todo` and `events` filtered on action alone, `electricity`
// on action AND module — which is why the parameters are optional rather than
// required. Each module keeps its own one-line `auditCount` wrapper so its call
// sites still read in its own terms, and so the filter it means stays written
// down in one place per module rather than repeated at every assertion.
//
// ⚠ PASS LITERALS, NOT VARIABLES. The copies used `WHERE action = ?`, where an
// empty action matched nothing and returned 0; here it drops the clause and
// returns EVERY event. An assertion fed an accidentally-empty action (a renamed
// constant, an unset struct field) used to fail loudly and would now pass against
// a database full of unrelated events. Every current call site passes a literal.
func CountAudit(t *testing.T, db *sql.DB, module, action string) int {
	t.Helper()
	q := `SELECT COUNT(*) FROM audit_events WHERE 1 = 1`
	var args []any
	if module != "" {
		q += ` AND module = ?`
		args = append(args, module)
	}
	if action != "" {
		q += ` AND action = ?`
		args = append(args, action)
	}
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("count audit events (module=%q action=%q): %v", module, action, err)
	}
	return n
}
