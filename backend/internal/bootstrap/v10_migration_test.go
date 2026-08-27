package bootstrap_test

// v10's three migrations, and the one of them that rewrites live rows.
//
// ⚠ `08003` IS THE ONLY v10 MIGRATION THAT TOUCHES AN EXISTING TABLE WITH DATA IN
// IT. `12001` creates a new block and `02004` is an ALTER TABLE ADD COLUMN plus a
// CREATE TABLE — neither can lose anything. 08003 widens two CHECK constraints on
// `notification_deliveries`, and SQLite cannot alter a CHECK, so it is a full table
// rebuild: create wide, copy every row, drop, rename, re-create four indexes.
//
// ⚠ AN EMPTY DATABASE CANNOT FAIL THE INTERESTING HALF OF THIS FILE, which is why
// every test below seeds rows first — and why TestV10MigrationOnRestoredCopy at the
// bottom exists and skips unless pointed at a Litestream restore. §V9-12 records
// the same split for the `soukrome` backfill.

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/bootstrap"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
)

// v10Files are the three migrations v10 adds, in ascending version order.
//
// ⚠ IDENTIFIED BY FILENAME, NOT BY A VERSION CUTOFF, and that is forced by the
// numbering scheme rather than preferred: every module owns a numeric BLOCK inside
// one sequence, so v10's files interleave with migrations production applied years
// of releases ago. There is no version V such that "everything below V" means
// "everything before v10" — the same fact that makes appdb.Migrate need
// WithAllowMissing.
var v10Files = []string{
	"02004_chat_platform.sql",
	"08003_chat_delivery.sql",
	"12001_chat.sql",
}

// preV10MigrationFS is the merged schema WITHOUT v10's three files — exactly the
// migration set production had applied the morning v10 ships.
func preV10MigrationFS(t *testing.T) fs.FS {
	t.Helper()
	full, err := bootstrap.MigrationFS()
	if err != nil {
		t.Fatalf("assemble migrations: %v", err)
	}
	names, err := fs.Glob(full, "*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	skip := map[string]bool{}
	for _, f := range v10Files {
		skip[f] = true
	}
	out := fstest.MapFS{}
	for _, name := range names {
		if skip[name] {
			delete(skip, name)
			continue
		}
		b, err := fs.ReadFile(full, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		out[name] = &fstest.MapFile{Data: b}
	}
	// A renamed v10 migration would silently turn this into "migrate everything,
	// twice" — a test that proves nothing while staying green.
	for name := range skip {
		t.Fatalf("v10 migration %q is not in the merged migration set — was it renamed? "+
			"v10Files must be kept in step or this test stops testing the upgrade at all", name)
	}
	return out
}

// ---- the out-of-order claim, verified rather than assumed ----

// TestV10OutOfOrderMigrationsApplyOverAnUpgradedDatabase is the claim
// 02004_chat_platform.sql makes in its own header, tested.
//
// Two of v10's three files are numerically BELOW `11001`, which production applied
// when v8 shipped. HANDOFF-12 §9.3 says "the runner tolerates it — verify that
// before writing them, not after", and this is that verification: migrate to the
// pre-v10 set, then migrate again with the full one, and require both to succeed.
func TestV10OutOfOrderMigrationsApplyOverAnUpgradedDatabase(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateWith(t, sqldb, preV10MigrationFS(t))

	// Prove we really are at the pre-v10 state before claiming anything about the
	// upgrade: a test that migrated the full set twice would pass regardless.
	if tableExists(t, sqldb, "chat_conversations") {
		t.Fatalf("chat_conversations exists before v10 migrated — preV10MigrationFS did not exclude 12001")
	}
	if columnExists(t, sqldb, "notification_preferences", "cat_chat") {
		t.Fatalf("cat_chat exists before v10 migrated — preV10MigrationFS did not exclude 02004")
	}

	migrateFull(t, sqldb)

	if !tableExists(t, sqldb, "chat_conversations") {
		t.Errorf("12001 did not apply over an already-migrated database")
	}
	if !columnExists(t, sqldb, "notification_preferences", "cat_chat") {
		t.Errorf("02004 did not apply — an out-of-order version was skipped rather than run. " +
			"appdb.Migrate passes goose.WithAllowMissing() precisely so this works")
	}
	if !tableExists(t, sqldb, "storage_thresholds") {
		t.Errorf("02004 did not create storage_thresholds")
	}

	// The two seeded thresholds, at the values PRD §V10-5 names.
	for key, want := range map[string]int{"chat.total": 512, "chat.conversation": 128} {
		var got int
		if err := sqldb.QueryRow(
			`SELECT value_mb FROM storage_thresholds WHERE key = ?`, key).Scan(&got); err != nil {
			t.Errorf("threshold %s: %v", key, err)
			continue
		}
		if got != want {
			t.Errorf("threshold %s seeded at %d MB, want %d", key, got, want)
		}
	}

	// Exactly one Všichni conversation, and no membership seeded with it: the
	// directory is projected from `sessions`, so a member who has never logged in
	// does not exist yet (FR-V10-2).
	var defaults, members int
	if err := sqldb.QueryRow(`SELECT COUNT(*) FROM chat_conversations WHERE kind = 'default'`).
		Scan(&defaults); err != nil {
		t.Fatalf("count default conversations: %v", err)
	}
	if defaults != 1 {
		t.Errorf("%d default conversations exist, want exactly 1 (ux_chat_default)", defaults)
	}
	if err := sqldb.QueryRow(`SELECT COUNT(*) FROM chat_members`).Scan(&members); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if members != 0 {
		t.Errorf("12001 seeded %d membership rows, want 0 — membership accrues at first "+
			"sight, not at migration time (FR-V10-2)", members)
	}
}

// ---- 08003: the rebuild, over rows that are already there ----

// TestChatDeliveryRebuildPreservesEveryRow is the assertion an empty database
// cannot make.
func TestChatDeliveryRebuildPreservesEveryRow(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateWith(t, sqldb, preV10MigrationFS(t))

	// A household's delivery log the morning v10 ships: every kind, every category,
	// every status, plus the nullable columns exercised in both states.
	seed := []struct{ id, ts, kind, category, ruleID, user, sub, status, errMsg string }{
		{"d1", "2026-08-01T10:00:00.000Z", "broadcast", "broadcast", "", "u-kaja", "s1", "sent", ""},
		{"d2", "2026-08-02T10:00:00.000Z", "trigger", "triggers", "r1", "u-andy", "s2", "failed", "410 Gone"},
		{"d3", "2026-08-03T10:00:00.000Z", "schedule", "summaries", "r2", "u-kaja", "", "expired", "gone"},
		{"d4", "2026-08-04T10:00:00.000Z", "test", "broadcast", "", "u-admin", "s3", "sent", ""},
	}
	for _, r := range seed {
		var ruleID, sub, errMsg any
		if r.ruleID != "" {
			ruleID = r.ruleID
		}
		if r.sub != "" {
			sub = r.sub
		}
		if r.errMsg != "" {
			errMsg = r.errMsg
		}
		if _, err := sqldb.Exec(`
			INSERT INTO notification_deliveries
			  (id, ts, kind, category, rule_id, user_id, subscription_id, status, error)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			r.id, r.ts, r.kind, r.category, ruleID, r.user, sub, r.status, errMsg); err != nil {
			t.Fatalf("seed %s: %v", r.id, err)
		}
	}
	before := deliveryFingerprint(t, sqldb)

	migrateFull(t, sqldb)

	if after := deliveryFingerprint(t, sqldb); !equalStrings(before, after) {
		t.Fatalf("the rebuild changed the delivery log.\n before: %v\n  after: %v\n\n"+
			"The copy step names its columns rather than using SELECT * precisely so a "+
			"reordering cannot do this silently.", before, after)
	}

	// ⚠ FOUR INDEXES, NOT THREE. HANDOFF-12 §9.2 and PRD §V10-5 both say three; the
	// table has carried four since 08001. A rebuild that re-creates three of them
	// leaves the delivery log's status filter on a full scan — nothing fails, it
	// just gets slower every month.
	want := []string{
		"idx_notification_deliveries_kind_ts",
		"idx_notification_deliveries_rule_ts",
		"idx_notification_deliveries_status_ts",
		"idx_notification_deliveries_ts",
	}
	if got := indexesOn(t, sqldb, "notification_deliveries"); !equalStrings(got, want) {
		t.Errorf("after the rebuild notification_deliveries has indexes %v, want %v", got, want)
	}

	// And the point of the whole exercise: 'chat' is now accepted by both CHECKs.
	if _, err := sqldb.Exec(`
		INSERT INTO notification_deliveries (id, ts, kind, category, user_id, status)
		VALUES ('c1', '2026-08-27T10:00:00.000Z', 'chat', 'chat', 'u-kaja', 'sent')`); err != nil {
		t.Fatalf("a chat delivery is still refused after 08003: %v", err)
	}
}

// TestChatDeliveryDownMigrationSurvivesChatRows is the half a down migration
// usually gets wrong.
//
// ⚠ THE DELETE MUST COME BEFORE THE REBUILD. By the time anyone runs this down, the
// table may hold kind='chat' rows — and the narrow table's own CHECK rejects them on
// the copy step, failing the migration halfway and leaving a half-built table
// behind. Discarding chat's delivery rows is the correct answer: they are
// operational records of a feature being rolled back.
func TestChatDeliveryDownMigrationSurvivesChatRows(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateFull(t, sqldb)

	// One ordinary row and one chat row — the state a rollback actually meets.
	if _, err := sqldb.Exec(`
		INSERT INTO notification_deliveries (id, ts, kind, category, user_id, status) VALUES
		  ('keep', '2026-08-01T10:00:00.000Z', 'broadcast', 'broadcast', 'u-kaja', 'sent'),
		  ('drop', '2026-08-27T10:00:00.000Z', 'chat',      'chat',      'u-andy', 'sent')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	runSection(t, sqldb, "08003_chat_delivery.sql", "Down")

	var kept, dropped int
	if err := sqldb.QueryRow(`SELECT COUNT(*) FROM notification_deliveries WHERE id = 'keep'`).
		Scan(&kept); err != nil {
		t.Fatalf("count kept: %v", err)
	}
	if err := sqldb.QueryRow(`SELECT COUNT(*) FROM notification_deliveries WHERE id = 'drop'`).
		Scan(&dropped); err != nil {
		t.Fatalf("count dropped: %v", err)
	}
	if kept != 1 {
		t.Errorf("the down migration lost a non-chat delivery row")
	}
	if dropped != 0 {
		t.Errorf("the down migration kept a chat delivery row, which the narrow CHECK cannot hold")
	}

	// The narrow CHECK is genuinely back.
	if _, err := sqldb.Exec(`
		INSERT INTO notification_deliveries (id, ts, kind, category, user_id, status)
		VALUES ('c2', '2026-08-27T11:00:00.000Z', 'chat', 'chat', 'u-kaja', 'sent')`); err == nil {
		t.Errorf("a chat delivery was accepted after the down migration — the CHECK was not restored")
	}

	// All four indexes come back too, or the rollback leaves the log slower than it
	// was before v10 ever ran.
	want := []string{
		"idx_notification_deliveries_kind_ts",
		"idx_notification_deliveries_rule_ts",
		"idx_notification_deliveries_status_ts",
		"idx_notification_deliveries_ts",
	}
	if got := indexesOn(t, sqldb, "notification_deliveries"); !equalStrings(got, want) {
		t.Errorf("after the down migration the indexes are %v, want %v", got, want)
	}

	// And Up runs again over the rolled-back table, because a down nobody can undo
	// is not a rollback.
	runSection(t, sqldb, "08003_chat_delivery.sql", "Up")
	if _, err := sqldb.Exec(`
		INSERT INTO notification_deliveries (id, ts, kind, category, user_id, status)
		VALUES ('c3', '2026-08-27T12:00:00.000Z', 'chat', 'chat', 'u-kaja', 'sent')`); err != nil {
		t.Fatalf("re-running 08003 Up after its Down failed: %v", err)
	}
}

// TestChatMigrationDownDropsTriggersBeforeTables is 12001's ordering claim.
//
// A down that drops chat_messages before its triggers leaves orphaned triggers
// referencing a missing table, and the next `up` fails — which nobody discovers
// until a restore.
func TestChatMigrationDownDropsTriggersBeforeTables(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateFull(t, sqldb)

	runSection(t, sqldb, "12001_chat.sql", "Down")
	if tableExists(t, sqldb, "chat_messages") {
		t.Fatalf("chat_messages survived the down migration")
	}
	var triggers int
	if err := sqldb.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'chat_%'`).
		Scan(&triggers); err != nil {
		t.Fatalf("count triggers: %v", err)
	}
	if triggers != 0 {
		t.Errorf("%d chat trigger(s) outlived their table — the next `up` would fail on "+
			"a name that already exists", triggers)
	}

	// Up again, which is the assertion that actually matters.
	runSection(t, sqldb, "12001_chat.sql", "Up")
	if !tableExists(t, sqldb, "chat_messages") {
		t.Fatalf("12001 did not re-apply after its own down")
	}
}

// TestChatFTSSurvivesADeleteAndAnEdit checks the trigger set from the schema's side.
//
// The blanking UPDATE is the one the module depends on and the one a "simplified"
// trigger would drop: 06001's twin is guarded `WHEN old.body_md IS NOT new.body_md`,
// and the equivalent guard here has to fire when a delete sets body = ”.
func TestChatFTSSurvivesADeleteAndAnEdit(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateFull(t, sqldb)

	var convID string
	if err := sqldb.QueryRow(`SELECT id FROM chat_conversations WHERE kind = 'default'`).
		Scan(&convID); err != nil {
		t.Fatalf("find Všichni: %v", err)
	}
	if _, err := sqldb.Exec(`
		INSERT INTO chat_messages (id, conversation_id, author_id, body, created_at)
		VALUES ('m1', ?, 'u-kaja', 'původní text o jahodách', '2026-08-27T10:00:00.000Z')`,
		convID); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if n := ftsCount(t, sqldb, "jahodách"); n != 1 {
		t.Fatalf("the insert trigger did not index the body (%d hits)", n)
	}

	if _, err := sqldb.Exec(`UPDATE chat_messages SET body = 'nový text o malinách' WHERE id = 'm1'`); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if n := ftsCount(t, sqldb, "jahodách"); n != 0 {
		t.Errorf("the old body is still indexed after an edit (%d hits)", n)
	}
	if n := ftsCount(t, sqldb, "malinách"); n != 1 {
		t.Errorf("the new body was not indexed after an edit (%d hits)", n)
	}

	// The delete path: deleted_at plus a blanked body, in one statement.
	if _, err := sqldb.Exec(
		`UPDATE chat_messages SET deleted_at = '2026-08-27T11:00:00.000Z', body = '' WHERE id = 'm1'`); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if n := ftsCount(t, sqldb, "malinách"); n != 0 {
		t.Errorf("a soft-deleted message's body is still in chat_messages_fts (%d hits) — "+
			"the index is external-content, so `deleted_at IS NOT NULL` alone hides it from "+
			"the thread and leaves it findable by search (D223)", n)
	}
}

// ---- helpers ----

func tableExists(t *testing.T, sqldb *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := sqldb.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
		t.Fatalf("look up table %s: %v", name, err)
	}
	return n > 0
}

func columnExists(t *testing.T, sqldb *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := sqldb.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		t.Fatalf("table_info %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
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
		if name == column {
			return true
		}
	}
	return false
}

func indexesOn(t *testing.T, sqldb *sql.DB, table string) []string {
	t.Helper()
	rows, err := sqldb.Query(
		`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND name NOT LIKE 'sqlite_%'
		  ORDER BY name`, table)
	if err != nil {
		t.Fatalf("list indexes on %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan index name: %v", err)
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// deliveryFingerprint is every delivery row as one comparable list. Comparing it
// before and after the rebuild turns "did we lose or reorder anything?" into a
// single assertion.
func deliveryFingerprint(t *testing.T, sqldb *sql.DB) []string {
	t.Helper()
	rows, err := sqldb.Query(`
		SELECT id, ts, kind, category, COALESCE(rule_id, '<null>'), user_id,
		       COALESCE(subscription_id, '<null>'), status, COALESCE(error, '<null>')
		  FROM notification_deliveries ORDER BY id`)
	if err != nil {
		t.Fatalf("fingerprint deliveries: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var f [9]string
		if err := rows.Scan(&f[0], &f[1], &f[2], &f[3], &f[4], &f[5], &f[6], &f[7], &f[8]); err != nil {
			t.Fatalf("scan delivery: %v", err)
		}
		out = append(out, strings.Join(f[:], "|"))
	}
	return out
}

func ftsCount(t *testing.T, sqldb *sql.DB, term string) int {
	t.Helper()
	var n int
	if err := sqldb.QueryRow(
		`SELECT COUNT(*) FROM chat_messages_fts WHERE chat_messages_fts MATCH ?`, term).Scan(&n); err != nil {
		t.Fatalf("query chat_messages_fts for %q: %v", term, err)
	}
	return n
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestV10MigrationOnRestoredCopy is Karel's gate before PR 2 merges, expressed as a
// test that skips by default.
//
// ⚠ EVERY OTHER TEST IN THIS FILE RUNS AGAINST A DATABASE THIS PROCESS JUST BUILT.
// That is enough to prove the SQL is well formed and the ordering is right, and it
// is not enough to prove anything about the household's actual delivery log: an
// empty table copies perfectly. §V9-12 records the same split for the `soukrome`
// backfill, and the same runbook applies —
//
//	litestream restore -o /tmp/home-restored.db <replica-url>
//	cp /tmp/home-restored.db /tmp/home-v10-check.db     # NEVER point this at the original
//	HOME_V10_MIGRATION_TEST_DB=/tmp/home-v10-check.db go test ./internal/bootstrap/ -run V10 -v
//
// It exercises the up, then 08003's down, then its up again — because the down is
// the half that only matters on a database with rows in it, and the rollback nobody
// has run is the rollback that does not work.
func TestV10MigrationOnRestoredCopy(t *testing.T) {
	path := os.Getenv("HOME_V10_MIGRATION_TEST_DB")
	if path == "" {
		t.Skip("set HOME_V10_MIGRATION_TEST_DB to a COPY of a Litestream-restored database to run " +
			"the migration check the acceptance criteria require (see this test's comment for the runbook)")
	}
	sqldb, err := appdb.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = sqldb.Close() }()

	beforeRows := countRows(t, sqldb)
	beforeDeliveries := deliveryFingerprint(t, sqldb)
	t.Logf("restored copy carries %d delivery rows", len(beforeDeliveries))

	migrateFull(t, sqldb)

	// Nothing anywhere lost a row. The delivery log is the table being rebuilt, so
	// it is checked value-by-value rather than only by count.
	for table, want := range beforeRows {
		if got := countRows(t, sqldb)[table]; got != want {
			t.Errorf("%s row count changed across the migration: %d → %d", table, want, got)
		}
	}
	if after := deliveryFingerprint(t, sqldb); !equalStrings(beforeDeliveries, after) {
		t.Fatalf("the 08003 rebuild changed the delivery log on real data (%d rows before, %d after)",
			len(beforeDeliveries), len(after))
	}

	// The down, on real rows, then back up. A chat row is written first so the
	// down's DELETE has something to do — that DELETE is the step whose absence
	// wedges the rollback halfway.
	if _, err := sqldb.Exec(`
		INSERT INTO notification_deliveries (id, ts, kind, category, user_id, status)
		VALUES ('v10-check', '2026-08-27T12:00:00.000Z', 'chat', 'chat', 'v10-check', 'sent')`); err != nil {
		t.Fatalf("write a chat delivery row: %v", err)
	}
	runSection(t, sqldb, "08003_chat_delivery.sql", "Down")
	if after := deliveryFingerprint(t, sqldb); !equalStrings(beforeDeliveries, after) {
		t.Fatalf("08003's down changed the pre-existing delivery rows (%d before, %d after)",
			len(beforeDeliveries), len(after))
	}
	runSection(t, sqldb, "08003_chat_delivery.sql", "Up")
	if after := deliveryFingerprint(t, sqldb); !equalStrings(beforeDeliveries, after) {
		t.Fatalf("re-running 08003 after its down changed the delivery rows")
	}

	t.Logf("OK — %v rows preserved, the delivery log survived up → down → up unchanged", beforeRows)
}
