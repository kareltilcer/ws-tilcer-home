package bootstrap_test

// v11's two migrations, and the rebuild that must not happen.
//
// ⚠ THE WHOLE POINT OF `01003` IS WHAT IT DOES NOT DO. Attribution could have been
// a fourth `actor_type` — the obvious modelling — and that would mean rebuilding
// `audit_events`: the table holding every mutation the household has ever made,
// and the parent of `audit_changes` with ON DELETE CASCADE. With foreign_keys=ON,
// dropping the parent performs an implicit DELETE of every row, FIRES THAT CASCADE,
// and destroys the household's entire change history WHILE REPORTING SUCCESS.
// `#46`'s `04002` needed 229 lines and a rename-never-drop dance to avoid exactly
// this on a far smaller table.
//
// So D290 chose two nullable columns and a partial index, CONFIRMED rather than
// deferred — and a declined decision needs a test, or the next reader repairs it.
// That is TestActorTypeStaysThree, and it is v9's
// TestReplicaIsDeclinedNotUnimplemented pattern.
//
// ⚠ AN EMPTY DATABASE CANNOT FAIL THE INTERESTING HALF OF THIS FILE, so the
// upgrade test seeds a parent AND a child first: the cascade only destroys
// something when there is something to destroy.

import (
	"database/sql"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/bootstrap"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
)

// v11Files are the two migrations v11 adds.
//
// ⚠ `01003` IS THE SIXTH OUT-OF-ORDER GOOSE VERSION BELOW THE APPLIED `11001` /
// `12003`, and `02005` makes seven — `01002`, `06004` and `07004` shipped in v9,
// `02004` and `08003` in v10. That is the shape this repository has had since
// `01002`; it is expected, not a problem, and the numbering must NOT be "tidied":
// renumbering an applied migration is how a restored copy stops matching
// `goose_db_version`.
var v11Files = []string{
	"01003_audit_via.sql",
	"02005_mcp_tokens.sql",
}

func TestV11MigrationsApplyOverAnUpgradedDatabase(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateWith(t, sqldb, migrationFSWithout(t, v11Files))

	// Prove we really are at the pre-v11 state, or an upgrade test that migrated
	// the full set twice would pass regardless.
	if columnExists(t, sqldb, "audit_events", "via") {
		t.Fatal("audit_events.via exists before v11 migrated — the exclusion did not work")
	}
	if tableExists(t, sqldb, "mcp_tokens") {
		t.Fatal("mcp_tokens exists before v11 migrated — the exclusion did not work")
	}

	seedAuditWithChanges(t, sqldb, 5)
	beforeEvents := countIn(t, sqldb, "audit_events")
	beforeChanges := countIn(t, sqldb, "audit_changes")
	if beforeChanges == 0 {
		t.Fatal("the fixture wrote no audit_changes rows, so the cascade half proves nothing")
	}

	migrateFull(t, sqldb)

	if !columnExists(t, sqldb, "audit_events", "via") ||
		!columnExists(t, sqldb, "audit_events", "via_token_id") {
		t.Fatal("01003 did not add both columns")
	}
	if !tableExists(t, sqldb, "mcp_tokens") {
		t.Fatal("02005 did not create mcp_tokens")
	}
	// ⚠ THE ROW COUNTS ARE IDENTICAL ON BOTH SIDES. A rebuild that fired the
	// cascade would leave audit_events intact and audit_changes empty, which is
	// precisely the failure that reports success.
	if got := countIn(t, sqldb, "audit_events"); got != beforeEvents {
		t.Fatalf("audit_events lost rows: %d → %d", beforeEvents, got)
	}
	if got := countIn(t, sqldb, "audit_changes"); got != beforeChanges {
		t.Fatalf("audit_changes lost rows: %d → %d — the cascade fired, which is the"+
			" one outcome 01003 exists to prevent", beforeChanges, got)
	}

	// The cascade itself still works: a rebuild that re-pointed the child at
	// nothing would pass every assertion above.
	var id string
	if err := sqldb.QueryRow(`SELECT id FROM audit_events LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("read an event: %v", err)
	}
	if _, err := sqldb.Exec(`DELETE FROM audit_events WHERE id = ?`, id); err != nil {
		t.Fatalf("delete an event: %v", err)
	}
	if got := countIn(t, sqldb, "audit_changes"); got >= beforeChanges {
		t.Fatalf("the ON DELETE CASCADE no longer runs: audit_changes is still %d", got)
	}
}

// ⚠ THE FILE ITSELF IS THE ASSERTION. A migration that adds a column today can be
// "improved" into a rebuild tomorrow by somebody who wants a CHECK constraint on
// `via`, and the diff would look small.
func TestAuditViaMigrationDoesNotRebuild(t *testing.T) {
	body := readMergedMigration(t, "01003_audit_via.sql")
	// ⚠ THE COMMENTS ARE STRIPPED FIRST, and the first draft of this test found
	// out why: the migration EXPLAINS the rebuild it is avoiding, in prose,
	// naming CREATE TABLE and DROP TABLE — so a scan over the raw file fails on
	// the documentation of the rule rather than on a violation of it. What is
	// asserted is the SQL.
	upper := strings.ToUpper(stripSQLComments(body))

	for _, forbidden := range []string{"CREATE TABLE", "DROP TABLE", "RENAME TO"} {
		if strings.Contains(upper, forbidden) {
			t.Fatalf("01003 contains %q. audit_events is the parent of audit_changes with"+
				" ON DELETE CASCADE, so ANY rebuild of it performs an implicit DELETE that"+
				" fires the cascade and destroys the household's whole change history while"+
				" reporting success (D290).", forbidden)
		}
	}
	if !strings.Contains(upper, "ADD COLUMN VIA") {
		t.Fatal("01003 no longer adds the via column")
	}

	// ⚠ The DOWN migration keeps the columns on purpose: DROP COLUMN on SQLite
	// rewrites the table, which is the same operation with the same cascade.
	i := strings.Index(body, "-- +goose Down")
	if i < 0 {
		t.Fatal("01003 has no Down section")
	}
	down := strings.ToUpper(stripSQLComments(body[i:]))
	if strings.Contains(down, "DROP COLUMN") {
		t.Fatal("the down migration drops a column, which is a table rewrite — the one" +
			" operation 01003 exists to avoid")
	}
	if !strings.Contains(down, "DROP INDEX") {
		t.Fatal("the down migration does not drop the index it created")
	}
}

// TestActorTypeStaysThree asserts the CHECK constraint still names exactly
// `user | system | service`.
//
// ⚠ THIS IS A TEST FOR A DECISION, NOT FOR A BEHAVIOUR (D290, confirmed by Karel
// 2026-09-06). Anyone who later finds `actor_type` lacking an `agent` value has
// found a decision rather than an oversight — and the reason is the constraint
// itself: SQLite cannot ALTER a CHECK, so a fourth value costs a rebuild of the
// largest table in the database with a destructive cascade hanging off it. The
// token's owner IS the actor; what names the token is `actor_label` and `via`.
func TestActorTypeStaysThree(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateFull(t, sqldb)

	var ddl string
	if err := sqldb.QueryRow(
		`SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = 'audit_events'`).Scan(&ddl); err != nil {
		t.Fatalf("read the audit_events DDL: %v", err)
	}
	for _, want := range []string{"'user'", "'system'", "'service'"} {
		if !strings.Contains(ddl, want) {
			t.Fatalf("actor_type no longer admits %s:\n%s", want, ddl)
		}
	}
	if strings.Contains(ddl, "'agent'") {
		t.Fatal("actor_type gained a fourth value. That is a DECISION being reversed," +
			" not a gap being filled (D290): the token carries the member's own id and" +
			" roles precisely so that every ownership check in eleven modules keeps" +
			" working, and widening this CHECK means rebuilding audit_events.")
	}
}

// The `via` index is partial, so it costs nothing on the rows where the answer is
// "a person, in the app" — which is almost all of them (D292).
func TestViaIndexIsPartial(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateFull(t, sqldb)

	var ddl string
	if err := sqldb.QueryRow(
		`SELECT sql FROM sqlite_schema WHERE type = 'index' AND name = 'idx_events_via'`).Scan(&ddl); err != nil {
		t.Fatalf("idx_events_via is missing: %v", err)
	}
	if !strings.Contains(strings.ToUpper(ddl), "WHERE") {
		t.Fatalf("idx_events_via is not partial — it would then cover every row of the"+
			" largest table in the database, for a column that is null on almost all of"+
			" them:\n%s", ddl)
	}
}

// Both down sections run, and rolling one back does not cost the other anything.
func TestV11DownMigrationsRun(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateFull(t, sqldb)
	seedAuditWithChanges(t, sqldb, 3)
	events, changes := countIn(t, sqldb, "audit_events"), countIn(t, sqldb, "audit_changes")

	runSection(t, sqldb, "02005_mcp_tokens.sql", "Down")
	if tableExists(t, sqldb, "mcp_tokens") {
		t.Fatal("02005's down did not drop mcp_tokens")
	}
	runSection(t, sqldb, "01003_audit_via.sql", "Down")
	if !columnExists(t, sqldb, "audit_events", "via") {
		t.Fatal("01003's down dropped the columns — see the file's own note on why it" +
			" must not: DROP COLUMN rewrites the table")
	}

	// ⚠ THE AUDIT SPINE IS UNTOUCHED BY EITHER. The whole argument for two nullable
	// columns is that rolling them back costs nothing; if it cost a row, the
	// argument would be wrong.
	if countIn(t, sqldb, "audit_events") != events || countIn(t, sqldb, "audit_changes") != changes {
		t.Fatalf("a down migration lost rows: events %d→%d, changes %d→%d",
			events, countIn(t, sqldb, "audit_events"), changes, countIn(t, sqldb, "audit_changes"))
	}
}

// ---- fixtures ----

// seedAuditWithChanges writes n events, each with one audit_changes row.
//
// ⚠ THE CHILD ROWS ARE THE POINT. An empty audit_changes cannot detect a cascade
// firing, and a cascade firing is the failure this file exists for.
func seedAuditWithChanges(t *testing.T, sqldb *sql.DB, n int) {
	t.Helper()
	for i := range n {
		id := idgen.New()
		ts := time.Now().UTC().Add(time.Duration(i) * time.Second).Format(audit.TSLayout)
		if _, err := sqldb.Exec(`
			INSERT INTO audit_events (id, ts, actor_type, module, action, summary, level, site)
			VALUES (?, ?, 'user', 'todo', 'card.update', 'test', 'info', 'home')`, id, ts); err != nil {
			t.Fatalf("seed event: %v", err)
		}
		if _, err := sqldb.Exec(`
			INSERT INTO audit_changes (id, event_id, field, old_value, new_value)
			VALUES (?, ?, 'title', 'a', 'b')`, idgen.New(), id); err != nil {
			t.Fatalf("seed change: %v", err)
		}
	}
}

func countIn(t *testing.T, sqldb *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := sqldb.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// stripSQLComments drops every `--` line, leaving the statements.
//
// ⚠ It keeps the goose directives, because they ARE the file's structure — but
// they are directives and not SQL, so the forbidden-keyword scan above never
// matches one.
func stripSQLComments(sqlText string) string {
	var b strings.Builder
	for _, line := range strings.Split(sqlText, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			if strings.HasPrefix(trimmed, "-- +goose") {
				b.WriteString(line + "\n")
			}
			continue
		}
		// An inline trailing comment goes too, for the same reason.
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// readMergedMigration reads one migration out of the assembled set, so the test
// needs no filesystem path and cannot drift from what actually ships.
func readMergedMigration(t *testing.T, name string) string {
	t.Helper()
	full, err := bootstrap.MigrationFS()
	if err != nil {
		t.Fatalf("assemble migrations: %v", err)
	}
	b, err := fs.ReadFile(full, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
