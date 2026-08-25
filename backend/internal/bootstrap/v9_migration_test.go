package bootstrap_test

// v9's three migrations are the first in Home's history that alter tables already
// carrying real household data, and two of the four sit under EXTERNAL-CONTENT
// FTS5 indexes keyed on the rowid (PRD D179). `ALTER TABLE ADD COLUMN` does not
// renumber rowids and `DROP INDEX`/`CREATE INDEX` does not touch the table at all,
// so the migrations are safe by construction — but "safe by construction" is the
// claim, and this file is the test of it.
//
// ⚠ AN EMPTY DATABASE CANNOT FAIL THIS TEST, which is why HANDOFF-11 §1.5 says it
// must not be the only one run. TestV9MigrationOnRestoredCopy at the bottom is the
// one that runs against a Litestream restore; it SKIPS unless pointed at one.

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/bootstrap"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
)

// v9Files are the three migrations v9 adds, in ascending version order.
//
// ⚠ They are identified by FILENAME, not by a version cutoff, and that is forced
// by the numbering scheme rather than a preference: every module owns a numeric
// BLOCK inside one Goose sequence, so v9's files (01002, 06004, 07004) interleave
// with migrations production has long since applied. There is no version V such
// that "everything below V" means "everything before v9" — which is the same fact
// that makes appdb.Migrate need WithAllowMissing.
var v9Files = []string{
	"01002_private_meta.sql",
	"06004_notes_private_scope.sql",
	"07004_documents_private_scope.sql",
}

// ---- helpers ----

// preV9MigrationFS is the merged schema WITHOUT v9's three files — i.e. exactly
// the migration set production had applied on the morning v9 ships. Migrating with
// it and then with the full set is the only faithful way to reproduce the upgrade;
// see v9Files for why a version cutoff cannot express it.
func preV9MigrationFS(t *testing.T) fs.FS {
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
	for _, f := range v9Files {
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
	// A renamed v9 migration would silently turn this into "migrate everything,
	// twice" — a test that proves nothing while staying green.
	for name := range skip {
		t.Fatalf("v9 migration %q is not in the merged migration set — was it renamed? "+
			"v9Files must be kept in step or this test stops testing the upgrade at all", name)
	}
	return out
}

// gooseSectionSQL extracts one direction's statements from a goose migration file.
//
// It exists because goose's Up/UpTo/DownTo are LINEAR over versions and v9's three
// files interleave with older blocks (see v9Files), so "roll back exactly these
// three" is not expressible through the library. Running their Down sections
// directly is what actually tests the D200 claim — that a down migration drops its
// indexes BEFORE the columns those indexes reference.
func gooseSectionSQL(t *testing.T, name, direction string) []string {
	t.Helper()
	full, err := bootstrap.MigrationFS()
	if err != nil {
		t.Fatalf("assemble migrations: %v", err)
	}
	b, err := fs.ReadFile(full, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var (
		stmts   []string
		inWant  bool
		inStmt  bool
		current []string
	)
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "-- +goose Up":
			inWant = direction == "Up"
		case trimmed == "-- +goose Down":
			inWant = direction == "Down"
		case trimmed == "-- +goose StatementBegin":
			if inWant {
				inStmt, current = true, nil
			}
		case trimmed == "-- +goose StatementEnd":
			if inWant && inStmt {
				stmts = append(stmts, strings.Join(current, "\n"))
				inStmt = false
			}
		default:
			if inWant && inStmt {
				current = append(current, line)
			}
		}
	}
	if len(stmts) == 0 {
		t.Fatalf("%s has no %s statements — the parser or the file changed shape", name, direction)
	}
	return stmts
}

// runSection executes one direction of one migration, statement by statement, and
// reports WHICH statement failed. "the migration failed" is not a useful diagnosis
// for an ordering bug that wedges a table halfway through.
func runSection(t *testing.T, sqldb *sql.DB, name, direction string) {
	t.Helper()
	for i, stmt := range gooseSectionSQL(t, name, direction) {
		if _, err := sqldb.Exec(stmt); err != nil {
			t.Fatalf("%s %s statement %d failed: %v\n%s\n\n"+
				"For a Down section the usual cause is dropping a column before the index that "+
				"references it — SQLite refuses, and the table is left wedged halfway (D200).",
				name, direction, i+1, err, strings.TrimSpace(stmt))
		}
	}
}

func openTempDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "home_v9_test.db")
	sqldb, err := appdb.Open(path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	return sqldb, path
}

// migrateWith applies every migration in fsys. Mirrors appdb.Migrate, but takes
// the FS so a test can migrate with the pre-v9 set first.
func migrateWith(t *testing.T, sqldb *sql.DB, fsys fs.FS) {
	t.Helper()
	goose.SetBaseFS(fsys)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set goose dialect: %v", err)
	}
	goose.SetLogger(goose.NopLogger())
	if err := goose.Up(sqldb, ".", goose.WithAllowMissing()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

func migrateFull(t *testing.T, sqldb *sql.DB) {
	t.Helper()
	migFS, err := bootstrap.MigrationFS()
	if err != nil {
		t.Fatalf("assemble migrations: %v", err)
	}
	migrateWith(t, sqldb, migFS)
}

// ftsFingerprint runs a set of full-text queries and returns their result ids, in
// order. It is the "search still returns the same rows" assertion in one value:
// compare the map before and after the migration and any desynchronised FTS index
// shows up as a changed list rather than as a vague suspicion.
func ftsFingerprint(t *testing.T, sqldb *sql.DB, terms []string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, term := range terms {
		out["notes:"+term] = ftsIDs(t, sqldb,
			`SELECT n.id FROM notes_fts f JOIN notes n ON n.rowid = f.rowid
			 WHERE notes_fts MATCH ? ORDER BY n.id`, term)
		out["documents:"+term] = ftsIDs(t, sqldb,
			`SELECT d.id FROM documents_fts f JOIN documents d ON d.rowid = f.rowid
			 WHERE documents_fts MATCH ? ORDER BY d.id`, term)
	}
	return out
}

func ftsIDs(t *testing.T, sqldb *sql.DB, query, term string) []string {
	t.Helper()
	rows, err := sqldb.Query(query, `"`+term+`"`)
	if err != nil {
		t.Fatalf("fts query %q: %v", term, err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	sort.Strings(ids)
	return ids
}

func sameFingerprint(t *testing.T, before, after map[string][]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("fingerprint size changed: %d → %d", len(before), len(after))
	}
	for k, want := range before {
		got, ok := after[k]
		if !ok {
			t.Errorf("fts query %q disappeared after the migration", k)
			continue
		}
		if len(want) != len(got) {
			t.Errorf("fts query %q: %d rows before, %d after (%v → %v)", k, len(want), len(got), want, got)
			continue
		}
		for i := range want {
			if want[i] != got[i] {
				t.Errorf("fts query %q row %d: %q before, %q after", k, i, want[i], got[i])
			}
		}
	}
	// A fingerprint of nothing proves nothing — guard against a fixture that
	// silently stopped inserting rows.
	total := 0
	for _, ids := range before {
		total += len(ids)
	}
	if total == 0 {
		t.Fatal("the FTS fingerprint matched zero rows before the migration — the fixture is not exercising search, " +
			"so this test would pass against a desynchronised index")
	}
}

// seedPreV9Content writes folders, notes, document folders and documents through
// raw SQL — the service layer does not exist at this schema version, and using it
// would test today's code against yesterday's tables.
func seedPreV9Content(t *testing.T, sqldb *sql.DB) {
	t.Helper()
	const ts = "2026-08-01T10:00:00.000000Z"
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sqldb.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, q)
		}
	}
	exec(`INSERT INTO folders (id, parent_id, name, slug, position, archived, created_by, created_at, updated_at)
	      VALUES ('f1', NULL, 'Recepty', 'recepty', 'm', 0, 'u-kaja', ?, ?)`, ts, ts)
	exec(`INSERT INTO notes (id, folder_id, title, slug, body_md, position, archived, created_by, created_at, updated_at)
	      VALUES ('n1', 'f1', 'Guláš', 'gulas', 'Hovězí, cibule, paprika.', 'm', 0, 'u-kaja', ?, ?)`, ts, ts)
	exec(`INSERT INTO notes (id, folder_id, title, slug, body_md, position, archived, created_by, created_at, updated_at)
	      VALUES ('n2', NULL, 'Nákup', 'nakup', 'Cibule a paprika.', 'n', 0, 'u-andy', ?, ?)`, ts, ts)
	exec(`INSERT INTO document_folders (id, parent_id, name, slug, position, archived, created_by, created_at, updated_at)
	      VALUES ('df1', NULL, 'Smlouvy', 'smlouvy', 'm', 0, 'u-kaja', ?, ?)`, ts, ts)
	exec(`INSERT INTO documents (id, folder_id, title, slug, description, original_filename, content_type,
	          byte_size, checksum, storage_key, preview_kind, preview_status, position, archived, created_by, created_at, updated_at)
	      VALUES ('d1', 'df1', 'Nájemní smlouva', 'najemni-smlouva', 'Byt, paprika ulice', 'smlouva.pdf',
	              'application/pdf', 1024, 'abc', 'documents/d1/original', 'pdf', 'ready', 'm', 0, 'u-kaja', ?, ?)`, ts, ts)
}

// ---- the tests ----

// TestV9MigrationPreservesExistingRowsAndFTS is HANDOFF-11 §1.5 in its synthetic
// form: migrate to the pre-v9 schema, write content, apply v9, and assert that
// every row is `shared`/NULL (D200 — the column default IS the data migration)
// and that full-text search returns exactly the rows it returned before (D179).
func TestV9MigrationPreservesExistingRowsAndFTS(t *testing.T) {
	sqldb, _ := openTempDB(t)

	migrateWith(t, sqldb, preV9MigrationFS(t))
	assertColumnAbsent(t, sqldb, "notes", "visibility")
	seedPreV9Content(t, sqldb)

	terms := []string{"gulas", "paprika", "smlouva", "cibule"}
	before := ftsFingerprint(t, sqldb, terms)

	migrateFull(t, sqldb)

	sameFingerprint(t, before, ftsFingerprint(t, sqldb, terms))

	// D200: every pre-existing row stays exactly as visible as it was.
	for _, table := range []string{"folders", "notes", "document_folders", "documents"} {
		var n int
		if err := sqldb.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE visibility <> 'shared' OR owner_id IS NOT NULL`, table),
		).Scan(&n); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s: %d pre-existing row(s) did not come through the migration as shared/NULL — "+
				"the DEFAULT 'shared' column IS the data migration (D200) and nothing may backfill", table, n)
		}
	}
}

// TestV9SlugIndexesCarryTheRootScope is the colliding-name case at the SCHEMA
// level: two members, one slug, two private roots. The service-level half (that
// freeSlug does not silently hand the second member `recepty-2`) is asserted in
// the notes/documents packages; this one proves the index permits the row at all.
func TestV9SlugIndexesCarryTheRootScope(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateFull(t, sqldb)
	const ts = "2026-08-24T10:00:00.000000Z"

	insertNote := func(id, owner, visibility string) error {
		_, err := sqldb.Exec(
			`INSERT INTO notes (id, folder_id, title, slug, body_md, position, archived,
			     created_by, created_at, updated_at, visibility, owner_id)
			 VALUES (?, NULL, 'Recepty', 'recepty', '', 'm', 0, ?, ?, ?, ?, ?)`,
			id, owner, ts, ts, visibility, sql.NullString{String: owner, Valid: visibility == "private"})
		return err
	}

	if err := insertNote("n-kaja", "u-kaja", "private"); err != nil {
		t.Fatalf("kaja's private Recepty was refused: %v", err)
	}
	if err := insertNote("n-andy", "u-andy", "private"); err != nil {
		t.Fatalf("andy's private Recepty was refused — the sibling-slug index is still keyed on the "+
			"un-scoped COALESCE(folder_id,'') sentinel, so two private roots collide (D178): %v", err)
	}
	if err := insertNote("n-shared", "u-kaja", "shared"); err != nil {
		t.Fatalf("the household's shared Recepty was refused: %v", err)
	}
	// …and the index still does its original job WITHIN one scope.
	if err := insertNote("n-kaja-2", "u-kaja", "private"); err == nil {
		t.Error("a second private Recepty at the SAME member's root was accepted — the sibling-slug " +
			"index has stopped constraining anything")
	}
}

// TestV9DownMigrationsUnwindCleanly is D200's other half: SQLite refuses to drop a
// column an index references, so a down migration that drops columns before
// indexes fails halfway and leaves the table wedged. Rolling all three back and
// re-applying them proves the order is right — and that a rollback is survivable,
// which is the only reason a down migration exists.
func TestV9DownMigrationsUnwindCleanly(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateFull(t, sqldb)
	seedPreV9ContentWithVisibility(t, sqldb)

	for i := len(v9Files) - 1; i >= 0; i-- {
		runSection(t, sqldb, v9Files[i], "Down")
	}
	assertColumnAbsent(t, sqldb, "notes", "visibility")
	assertColumnAbsent(t, sqldb, "folders", "owner_id")
	assertColumnAbsent(t, sqldb, "documents", "owner_id")
	assertColumnAbsent(t, sqldb, "document_folders", "visibility")

	// The rolled-back schema must still be a WORKING schema — the pre-v9 sentinel
	// index restored, the tables queryable, search intact. A down migration that
	// leaves the database unusable is not a rollback.
	var n int
	if err := sqldb.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&n); err != nil {
		t.Fatalf("the rolled-back notes table is not queryable: %v", err)
	}
	if _, err := sqldb.Exec(
		`INSERT INTO notes (id, folder_id, title, slug, body_md, position, archived, created_by, created_at, updated_at)
		 VALUES ('n-rb', NULL, 'Recepty', 'recepty', '', 'z', 0, 'u-kaja',
		         '2026-08-24T10:00:00.000000Z', '2026-08-24T10:00:00.000000Z')`); err != nil {
		t.Fatalf("the rolled-back notes table refuses an ordinary insert: %v", err)
	}
	ftsFingerprint(t, sqldb, []string{"gulas"})

	// …and the whole thing re-applies, which is what makes a rollback survivable
	// rather than a one-way door.
	for _, name := range v9Files {
		runSection(t, sqldb, name, "Up")
	}
	assertColumnPresent(t, sqldb, "notes", "visibility")
	assertColumnPresent(t, sqldb, "documents", "owner_id")
}

func seedPreV9ContentWithVisibility(t *testing.T, sqldb *sql.DB) {
	t.Helper()
	seedPreV9Content(t, sqldb)
	if _, err := sqldb.Exec(
		`UPDATE notes SET visibility = 'private', owner_id = 'u-kaja' WHERE id = 'n2'`); err != nil {
		t.Fatalf("mark a note private: %v", err)
	}
}

func hasColumn(t *testing.T, sqldb *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := sqldb.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == column {
			return true
		}
	}
	return false
}

func assertColumnAbsent(t *testing.T, sqldb *sql.DB, table, column string) {
	t.Helper()
	if hasColumn(t, sqldb, table, column) {
		t.Fatalf("%s.%s exists but should not at this schema version", table, column)
	}
}

func assertColumnPresent(t *testing.T, sqldb *sql.DB, table, column string) {
	t.Helper()
	if !hasColumn(t, sqldb, table, column) {
		t.Fatalf("%s.%s is missing", table, column)
	}
}

// TestV9MigrationOnRestoredCopy is the acceptance criterion HANDOFF-11 §1.5 and
// PRD §V9-11 actually ask for, and the one a synthetic fixture cannot stand in
// for. It SKIPS unless pointed at a restored production database:
//
//	HOME_V9_MIGRATION_TEST_DB=/path/to/restored/home.db \
//	    go test ./internal/bootstrap/ -run TestV9MigrationOnRestoredCopy -v
//
// Point it at a COPY. It migrates the file it is given.
//
// The runbook for producing one:
//
//	litestream restore -o ./restored-home.db s3://$LITESTREAM_R2_BUCKET/home
//	cp ./restored-home.db ./v9-check.db
//	HOME_V9_MIGRATION_TEST_DB=$PWD/v9-check.db go test ./internal/bootstrap/ -run TestV9MigrationOnRestoredCopy -v
//
// It reads the household's real titles to build the fingerprint and prints only
// counts, never content.
func TestV9MigrationOnRestoredCopy(t *testing.T) {
	path := os.Getenv("HOME_V9_MIGRATION_TEST_DB")
	if path == "" {
		t.Skip("set HOME_V9_MIGRATION_TEST_DB to a COPY of a Litestream-restored database to run " +
			"the migration check the acceptance criteria require (see this test's comment for the runbook)")
	}
	sqldb, err := appdb.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = sqldb.Close() }()

	// Terms drawn from the data itself: the most common word-ish token in the live
	// titles, so the fingerprint exercises the real index rather than a guess.
	terms := restoredSearchTerms(t, sqldb)
	if len(terms) == 0 {
		t.Fatal("no searchable titles found in the restored copy — is this the right database?")
	}
	t.Logf("fingerprinting %d term(s) against the restored copy", len(terms))
	before := ftsFingerprint(t, sqldb, terms)
	beforeRows := countRows(t, sqldb)

	migrateFull(t, sqldb)

	sameFingerprint(t, before, ftsFingerprint(t, sqldb, terms))
	for table, want := range beforeRows {
		if got := countRows(t, sqldb)[table]; got != want {
			t.Errorf("%s row count changed across the migration: %d → %d", table, want, got)
		}
	}
	for _, table := range []string{"folders", "notes", "document_folders", "documents"} {
		var n int
		if err := sqldb.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE visibility <> 'shared' OR owner_id IS NOT NULL`, table),
		).Scan(&n); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s: %d row(s) are not shared/NULL after migrating a pre-v9 database (D200)", table, n)
		}
	}
	t.Logf("OK — %v rows preserved, FTS fingerprint identical, every row shared/NULL", beforeRows)
}

// restoredSearchTerms picks a handful of tokens that actually occur in the live
// titles. Returned to the caller, never logged.
func restoredSearchTerms(t *testing.T, sqldb *sql.DB) []string {
	t.Helper()
	seen := map[string]bool{}
	var terms []string
	for _, q := range []string{
		`SELECT title FROM notes WHERE archived = 0 LIMIT 20`,
		`SELECT title FROM documents WHERE archived = 0 LIMIT 20`,
	} {
		rows, err := sqldb.Query(q)
		if err != nil {
			t.Fatalf("read titles: %v", err)
		}
		for rows.Next() {
			var title string
			if err := rows.Scan(&title); err != nil {
				_ = rows.Close()
				t.Fatalf("scan title: %v", err)
			}
			for _, tok := range splitWords(title) {
				if len([]rune(tok)) >= 4 && !seen[tok] && len(terms) < 8 {
					seen[tok] = true
					terms = append(terms, tok)
				}
			}
		}
		_ = rows.Close()
	}
	return terms
}

func splitWords(s string) []string {
	var out []string
	cur := make([]rune, 0, 16)
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		case r == '"' || r == '\'' || r == '(' || r == ')' || r == ',' || r == '.' || r == ':' || r == ';':
			flush()
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return out
}

func countRows(t *testing.T, sqldb *sql.DB) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range []string{"folders", "notes", "document_folders", "documents", "note_images"} {
		var n int
		if err := sqldb.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s`, table)).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		out[table] = n
	}
	return out
}
