package bootstrap_test

// What stands in for the production rehearsal.
//
// ⚠ THE ACCEPTANCE CRITERIA ASKED FOR `08003` TO BE RUN AGAINST A LITESTREAM-
// RESTORED COPY OF PRODUCTION, AND THAT IS NOT BEING DONE. Karel declined to move
// the household's database off the droplet, which is a defensible call rather than
// a corner cut: the point of a restore rehearsal is confidence about a table
// rebuild, and the rehearsal itself puts every message, document title and audit
// row the household owns onto a second machine.
//
// So the confidence has to come from somewhere else. Three properties, below, cover
// what a restored copy would actually have told us:
//
//	SCALE      the rebuild is fine at many times the volume retention permits
//	SHAPE      every column, NULL state and awkward value survives it
//	ATOMICITY  a failure leaves the table exactly as it was
//
// ⚠ AND THE THIRD IS THE ONE THAT MATTERS. A restored-copy run answers "does it
// work on the real thing". Atomicity answers "what happens when it does not", which
// is the question the rehearsal was really a proxy for — migrations run at boot,
// before the server serves, and a failure there crash-loops the container. If the
// rebuild cannot half-finish, the worst case of shipping it unrehearsed is a deploy
// that has to be rolled back, not a household that has lost its delivery log.
//
// ⚠ WHAT THIS STILL DOES NOT COVER, stated plainly rather than left implied:
// SCHEMA DRIFT. If somebody has ALTERed `notification_deliveries` on the droplet by
// hand, outside a migration, the copy step's column list would not match and the
// rebuild would fail. Nothing here can see that and no test can — it is a claim
// about the droplet, not about the code. TestRebuildCopiesEveryColumn is the
// closest available: it catches the same failure arising from a FUTURE MIGRATION,
// which is the version of it anybody is actually likely to cause.

import (
	"database/sql"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/bootstrap"
)

// The values 08001's CHECKs admit, so a fixture built from them exercises every
// branch the constraint allows.
var (
	deliveryKinds      = []string{"broadcast", "trigger", "schedule", "test"}
	deliveryCategories = []string{"broadcast", "triggers", "summaries"}
	deliveryStatuses   = []string{"sent", "failed", "expired"}
)

// seedDeliveries writes n rows spanning every kind × category × status, with both
// NULL and non-NULL nullables and deliberately awkward text.
//
// Deterministic: row i's shape is a pure function of i, so a failure is
// reproducible and no seeded RNG is involved.
func seedDeliveries(t *testing.T, sqldb *sql.DB, n int) {
	t.Helper()
	tx, err := sqldb.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	stmt, err := tx.Prepare(`
		INSERT INTO notification_deliveries
		  (id, ts, kind, category, rule_id, user_id, subscription_id, status, error)
		VALUES (?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer func() { _ = stmt.Close() }()

	for i := range n {
		var ruleID, subID, errMsg any
		// Every fourth row leaves the nullables NULL, so the copy is exercised in
		// both states rather than only the populated one.
		if i%4 != 0 {
			ruleID = fmt.Sprintf("r-%d", i%17)
			subID = fmt.Sprintf("s-%d", i%23)
		}
		switch i % 5 {
		case 1:
			// A real push failure body: long, quoted, and not ASCII.
			errMsg = `410 Gone: {"reason":"subscription expired — zarizeni odhlaseno"} ` +
				strings.Repeat("x", 200)
		case 2:
			// The values most likely to break a hand-built copy: quotes and a newline.
			errMsg = "chyba 'apostrof' \"uvozovky\"\nnovy radek"
		}
		if _, err := stmt.Exec(
			fmt.Sprintf("d-%06d", i),
			fmt.Sprintf("2026-08-%02dT%02d:%02d:00.000Z", (i%28)+1, i%24, i%60),
			deliveryKinds[i%len(deliveryKinds)],
			deliveryCategories[i%len(deliveryCategories)],
			ruleID,
			fmt.Sprintf("u-%d", i%5),
			subID,
			deliveryStatuses[i%len(deliveryStatuses)],
			errMsg,
		); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit fixture: %v", err)
	}
}

// TestChatDeliveryRebuildAtProductionScale is the SCALE and SHAPE half.
//
// ⚠ 50 000 ROWS IS A DELIBERATE OVERSHOOT. `notification_deliveries` is pruned on
// every boot at HOME_NOTIF_DELIVERY_RETENTION_DAYS, default 30 (config.go), and it
// takes one row per device per notification. A five-person household with a handful
// of devices each would have to average well over a thousand notifications a day for
// a month to reach this — orders of magnitude past anything the real table can hold.
// So "it works here" covers "it works there" with room to spare, and not one real
// row was involved.
func TestChatDeliveryRebuildAtProductionScale(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateWith(t, sqldb, preV10MigrationFS(t))

	const rows = 50_000
	seedDeliveries(t, sqldb, rows)
	before := deliveryFingerprint(t, sqldb)
	if len(before) != rows {
		t.Fatalf("fixture has %d rows, want %d", len(before), rows)
	}

	start := time.Now()
	migrateFull(t, sqldb)
	elapsed := time.Since(start)

	after := deliveryFingerprint(t, sqldb)
	if !equalStrings(before, after) {
		// Report the FIRST divergence rather than dumping 50 000 rows at somebody.
		for i := range before {
			if i >= len(after) || before[i] != after[i] {
				t.Fatalf("the rebuild changed row %d of %d\n before: %s\n  after: %s",
					i, len(before), before[i], firstOr(after, i, "<missing>"))
			}
		}
		t.Fatalf("the rebuild changed the row COUNT: %d → %d", len(before), len(after))
	}

	// ⚠ Timing is LOGGED, NOT ASSERTED. A threshold here would fail on a busy CI box
	// and teach nothing. What the number is for is the boot-time question: migrations
	// run before the server serves, so a rebuild taking minutes would be a deploy that
	// looks hung. Low seconds at this size is fine, and the real table is far smaller.
	t.Logf("rebuilt %d delivery rows in %s (retention default is 30 days, so the live "+
		"table is orders of magnitude smaller)", rows, elapsed.Round(time.Millisecond))
}

func firstOr(ss []string, i int, fallback string) string {
	if i < len(ss) {
		return ss[i]
	}
	return fallback
}

// TestRebuildCopiesEveryColumn is the SHAPE guard against a FUTURE migration.
//
// ⚠ THE COPY STEP NAMES ITS COLUMNS, which is right — a positional `SELECT *`
// silently reorders if either table changes. But a named list is a list somebody has
// to maintain: add a column to `notification_deliveries` in a later migration, forget
// to add it to 08003, and the rebuild drops that column's data for every row while
// every existing test stays green — because they all check the nine columns they
// already know about.
//
// So this derives the column set from the SCHEMA at runtime and asserts the rebuild
// preserved all of it, whatever "all of it" happens to be by then.
func TestRebuildCopiesEveryColumn(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateWith(t, sqldb, preV10MigrationFS(t))
	seedDeliveries(t, sqldb, 200)

	cols := columnsOf(t, sqldb, "notification_deliveries")
	if len(cols) < 9 {
		t.Fatalf("notification_deliveries has %d columns before v10 — the fixture is wrong", len(cols))
	}
	before := rowsAcrossColumns(t, sqldb, cols)

	migrateFull(t, sqldb)

	after := columnsOf(t, sqldb, "notification_deliveries")
	if !equalStrings(cols, after) {
		t.Fatalf("the rebuild changed the column set:\n before: %v\n  after: %v\n\n"+
			"08003 copies a NAMED column list. A column added to this table by a later "+
			"migration and not added there is dropped for every row, silently.", cols, after)
	}
	if got := rowsAcrossColumns(t, sqldb, cols); !equalStrings(before, got) {
		t.Fatalf("the rebuild changed row data when read across ALL %d columns", len(cols))
	}
}

// TestFailedRebuildLeavesTheTableUntouched is the ATOMICITY half, and it is what
// actually stands in for the production rehearsal.
//
// A rehearsal answers "does it work on the real data". This answers the question the
// rehearsal is a proxy for: WHAT IF IT DOES NOT. If a failure could half-finish, a
// boot-time crash-loop would leave a household with a dropped table, and every
// subsequent boot would fail on the leftover — forever, until somebody went in by
// hand.
//
// This one goes through the REAL goose path, so it proves the failure is surfaced
// and the retry works. ⚠ On its own it does NOT prove atomicity: the obstruction
// makes the very first CREATE TABLE fail, so the table would be intact either way.
// TestRebuildRollsBackAfterTheDrop below is the half that proves it.
func TestFailedRebuildLeavesTheTableUntouched(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateWith(t, sqldb, preV10MigrationFS(t))
	seedDeliveries(t, sqldb, 500)
	before := deliveryFingerprint(t, sqldb)
	beforeIndexes := indexesOn(t, sqldb, "notification_deliveries")

	// A leftover from an earlier attempt — the realistic obstruction.
	if _, err := sqldb.Exec(`CREATE TABLE notification_deliveries_new (id TEXT)`); err != nil {
		t.Fatalf("plant the obstruction: %v", err)
	}
	if err := migrateExpectingFailure(t, sqldb); err == nil {
		t.Fatal("08003 succeeded with notification_deliveries_new already present — the " +
			"injection did not work, so this test proves nothing")
	}

	if !tableExists(t, sqldb, "notification_deliveries") {
		t.Fatal("the original table is gone after a failed rebuild")
	}
	if after := deliveryFingerprint(t, sqldb); !equalStrings(before, after) {
		t.Errorf("a failed rebuild changed the rows: %d before, %d after", len(before), len(after))
	}
	if after := indexesOn(t, sqldb, "notification_deliveries"); !equalStrings(beforeIndexes, after) {
		t.Errorf("a failed rebuild changed the indexes:\n before: %v\n  after: %v", beforeIndexes, after)
	}

	// The recovery a person would actually perform: clear the obstruction, redeploy,
	// and it applies cleanly. A rollback nobody can move past is only half a safety
	// property.
	if _, err := sqldb.Exec(`DROP TABLE notification_deliveries_new`); err != nil {
		t.Fatalf("clear the obstruction: %v", err)
	}
	migrateFull(t, sqldb)
	if after := deliveryFingerprint(t, sqldb); !equalStrings(before, after) {
		t.Errorf("the retry lost rows: %d before, %d after", len(before), len(after))
	}
	if _, err := sqldb.Exec(`
		INSERT INTO notification_deliveries (id, ts, kind, category, user_id, status)
		VALUES ('ok', '2026-08-27T10:00:00.000Z', 'chat', 'chat', 'u-kaja', 'sent')`); err != nil {
		t.Errorf("the retry did not widen the CHECK: %v", err)
	}
}

// TestRebuildRollsBackAfterTheDrop is the assertion the whole file exists for.
//
// ⚠ THE INTERESTING FAILURE CANNOT BE INJECTED FROM OUTSIDE. Everything after
// `DROP TABLE notification_deliveries` depends on names the drop itself frees — the
// four index names are taken by the live table right up until that statement runs —
// so there is no obstruction a test can plant beforehand that first bites afterwards.
//
// So the statements are replayed directly, all of them, inside one transaction that
// is then ROLLED BACK. That is strictly stronger than any injection: it lets every
// destructive step succeed — the copy, the drop, the rename, all four indexes — and
// then asks whether the original comes back. If it does, a failure at ANY point in
// that sequence is safe, because a failure is just a rollback the database performs
// for you.
//
// This is what makes shipping `08003` without a restored-copy rehearsal defensible.
// The worst case stops being "the household's delivery log is gone" and becomes "the
// container crash-loops until the deploy is rolled back".
func TestRebuildRollsBackAfterTheDrop(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateWith(t, sqldb, preV10MigrationFS(t))
	seedDeliveries(t, sqldb, 500)
	before := deliveryFingerprint(t, sqldb)
	beforeIndexes := indexesOn(t, sqldb, "notification_deliveries")

	stmts := gooseSectionSQL(t, "08003_chat_delivery.sql", "Up")
	tx, err := sqldb.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for i, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			_ = tx.Rollback()
			t.Fatalf("statement %d of the Up section failed inside a transaction: %v\n%s",
				i+1, err, strings.TrimSpace(stmt))
		}
	}

	// Inside the transaction the rebuild is complete: the widened CHECK accepts a
	// chat row. This is the proof that the drop and the rename really did execute,
	// rather than the sequence quietly no-opping.
	if _, err := tx.Exec(`
		INSERT INTO notification_deliveries (id, ts, kind, category, user_id, status)
		VALUES ('mid-tx', '2026-08-27T10:00:00.000Z', 'chat', 'chat', 'u-kaja', 'sent')`); err != nil {
		t.Fatalf("inside the transaction the rebuild had not taken effect: %v", err)
	}

	// And now the crash.
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if !tableExists(t, sqldb, "notification_deliveries") {
		t.Fatal("the original table did NOT come back after rolling back a completed " +
			"rebuild — DDL is not transactional here, and a boot-time failure would " +
			"destroy the delivery log")
	}
	if after := deliveryFingerprint(t, sqldb); !equalStrings(before, after) {
		t.Fatalf("the rollback did not restore the rows: %d before, %d after",
			len(before), len(after))
	}
	if after := indexesOn(t, sqldb, "notification_deliveries"); !equalStrings(beforeIndexes, after) {
		t.Errorf("the rollback did not restore the indexes:\n before: %v\n  after: %v",
			beforeIndexes, after)
	}
	// The narrow CHECK is back, so this is genuinely the old schema and not the new
	// one wearing the old name.
	if _, err := sqldb.Exec(`
		INSERT INTO notification_deliveries (id, ts, kind, category, user_id, status)
		VALUES ('leak', '2026-08-27T10:00:00.000Z', 'chat', 'chat', 'u-kaja', 'sent')`); err == nil {
		t.Error("a chat delivery was accepted after the rollback — the widened table survived")
	}
	// And the row written inside the transaction is gone with it.
	var midTx int
	if err := sqldb.QueryRow(
		`SELECT COUNT(*) FROM notification_deliveries WHERE id = 'mid-tx'`).Scan(&midTx); err != nil {
		t.Fatalf("count mid-tx row: %v", err)
	}
	if midTx != 0 {
		t.Error("a row written inside the rolled-back transaction survived")
	}
}

// TestRebuildIsNotMarkedNoTransaction is the other half of the same property, and it
// is a source assertion because that is where the property actually lives.
//
// goose runs a SQL migration inside a transaction UNLESS the file opts out with
// `-- +goose NO TRANSACTION`. TestRebuildRollsBackAfterTheDrop proves the database
// will roll the sequence back; this proves goose is still asking it to. One line
// added to the top of 08003 would silently turn the guarantee off, and every other
// test in this package would stay green.
func TestRebuildIsNotMarkedNoTransaction(t *testing.T) {
	migFS, err := bootstrap.MigrationFS()
	if err != nil {
		t.Fatalf("assemble migrations: %v", err)
	}
	for _, name := range []string{
		"08003_chat_delivery.sql", // rebuilds a live table
		"02004_chat_platform.sql", // ALTER TABLE + CREATE TABLE on the platform block
		"12001_chat.sql",          // the module's own schema
	} {
		b, err := fs.ReadFile(migFS, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(b), "NO TRANSACTION") {
			t.Errorf("%s opts out of goose's transaction.\n\n"+
				"For 08003 that is the difference between a failed deploy and a destroyed "+
				"delivery log: the sequence drops the live table before it renames the new "+
				"one over it, and only the transaction makes that safe.", name)
		}
	}
}

// migrateExpectingFailure runs the full migration set and RETURNS the error rather
// than failing the test, so a caller can assert on what a failure leaves behind.
func migrateExpectingFailure(t *testing.T, sqldb *sql.DB) error {
	t.Helper()
	migFS, err := bootstrap.MigrationFS()
	if err != nil {
		t.Fatalf("assemble migrations: %v", err)
	}
	goose.SetBaseFS(migFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set goose dialect: %v", err)
	}
	goose.SetLogger(goose.NopLogger())
	return goose.Up(sqldb, ".", goose.WithAllowMissing())
}

// columnsOf returns a table's column names in declaration order.
func columnsOf(t *testing.T, sqldb *sql.DB, table string) []string {
	t.Helper()
	rows, err := sqldb.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		t.Fatalf("table_info %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var (
			cid         int
			name, typ   string
			notNull, pk int
			dflt        sql.NullString
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		out = append(out, name)
	}
	return out
}

// rowsAcrossColumns reads every delivery row over the named columns, so the
// comparison covers whatever the table actually holds rather than a fixed list.
func rowsAcrossColumns(t *testing.T, sqldb *sql.DB, cols []string) []string {
	t.Helper()
	sel := make([]string, len(cols))
	for i, c := range cols {
		sel[i] = fmt.Sprintf("COALESCE(CAST(%s AS TEXT), '<null>')", c)
	}
	rows, err := sqldb.Query(
		`SELECT ` + strings.Join(sel, ", ") + ` FROM notification_deliveries ORDER BY id`)
	if err != nil {
		t.Fatalf("read all columns: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		cells := make([]any, len(cols))
		vals := make([]string, len(cols))
		for i := range cells {
			cells[i] = &vals[i]
		}
		if err := rows.Scan(cells...); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		out = append(out, strings.Join(vals, "|"))
	}
	return out
}
