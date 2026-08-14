package db_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sort"
	"testing"
	"testing/fstest"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

func gooseSQL(table string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(
		"-- +goose Up\nCREATE TABLE " + table + " (id INTEGER PRIMARY KEY);\n" +
			"-- +goose Down\nDROP TABLE " + table + ";\n")}
}

// TestMigrate_AppliesOutOfOrderMigration reproduces the production incident where
// a folder-icon migration in the notes block (06002) shipped after the documents
// block (07xxx) had already advanced the DB past it. The merged sequence numbers
// migrations per-module block, so a new migration in an earlier block is legitimately
// lower than the current DB version; Migrate must apply it rather than fail with
// "missing migrations before current version" (see Migrate's WithAllowMissing).
func TestMigrate_AppliesOutOfOrderMigration(t *testing.T) {
	sqldb, err := appdb.Open(filepath.Join(t.TempDir(), "ooo.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })

	// First deploy: only the higher-block migration exists → DB advances to 07999.
	high := fstest.MapFS{"07999_high.sql": gooseSQL("ooo_high")}
	if err := appdb.Migrate(sqldb, high); err != nil {
		t.Fatalf("first migrate: %v", err)
	}

	// Second deploy adds a lower-numbered migration (06999) after 07999 is applied —
	// exactly the notes-06002-after-documents-07002 shape that broke production.
	both := fstest.MapFS{"06999_low.sql": gooseSQL("ooo_low"), "07999_high.sql": high["07999_high.sql"]}
	if err := appdb.Migrate(sqldb, both); err != nil {
		t.Fatalf("out-of-order migrate failed (the production bug): %v", err)
	}
	if set := tableSet(t, sqldb); !set["ooo_low"] || !set["ooo_high"] {
		t.Fatalf("expected both ooo_low and ooo_high tables, got %v", set)
	}
}

func tableSet(t *testing.T, sqldb *sql.DB) map[string]bool {
	t.Helper()
	rows, err := sqldb.Query(
		"SELECT name FROM sqlite_master WHERE type IN ('table','view') ORDER BY name")
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		set[n] = true
	}
	return set
}

func TestMigrate_CreatesAllTables(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	set := tableSet(t, sqldb)
	want := []string{
		// logging (first)
		"audit_events", "audit_changes", "audit_events_fts",
		// todo
		"boards", "columns", "cards", "card_links", "checklist_items", "labels", "card_labels",
		// events
		"events", "event_links", "event_reminder_completions",
	}
	var missing []string
	for _, tbl := range want {
		if !set[tbl] {
			missing = append(missing, tbl)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("migration did not create tables: %v", missing)
	}
}

func TestProbeFTS5(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	if err := appdb.ProbeFTS5(context.Background(), sqldb); err != nil {
		t.Fatalf("FTS5 probe failed: %v", err)
	}
}

func TestSeedIfEmpty_SeedsOnceOnly(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	ctx := context.Background()

	seeded, err := appdb.SeedIfEmpty(ctx, sqldb)
	if err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if !seeded {
		t.Fatal("first seed should have seeded")
	}

	var boards int
	if err := sqldb.QueryRow("SELECT COUNT(*) FROM boards").Scan(&boards); err != nil {
		t.Fatal(err)
	}
	if boards != 1 {
		t.Fatalf("boards = %d, want 1", boards)
	}

	var name string
	if err := sqldb.QueryRow("SELECT name FROM boards").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Domácnost" {
		t.Errorf("board name = %q, want Domácnost", name)
	}

	// Three columns with the expected kinds, in position order.
	rows, err := sqldb.Query("SELECT name, kind FROM columns ORDER BY position")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type col struct{ name, kind string }
	var got []col
	for rows.Next() {
		var c col
		if err := rows.Scan(&c.name, &c.kind); err != nil {
			t.Fatal(err)
		}
		got = append(got, c)
	}
	want := []col{
		{"Zásobník", "normal"},
		{"Právě dělám", "now"},
		{"Hotovo", "done"},
	}
	if len(got) != len(want) {
		t.Fatalf("columns = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("column[%d] = %v, want %v", i, got[i], want[i])
		}
	}

	// Second seed is a no-op (the double-seed guard for restored builds).
	seeded, err = appdb.SeedIfEmpty(ctx, sqldb)
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if seeded {
		t.Error("second seed should NOT have seeded")
	}
	if err := sqldb.QueryRow("SELECT COUNT(*) FROM boards").Scan(&boards); err != nil {
		t.Fatal(err)
	}
	if boards != 1 {
		t.Fatalf("after second seed boards = %d, want 1", boards)
	}
}

func TestWithTx_RollbackAndCommit(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	ctx := context.Background()
	sentinel := errors.New("boom")

	// Rollback: an error from fn must leave nothing behind.
	err := appdb.WithTx(ctx, sqldb, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO boards (id, name, position, created_at, archived) VALUES ('b1','X','V','2026-01-01T00:00:00Z',0)`); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx err = %v, want sentinel", err)
	}
	var n int
	if err := sqldb.QueryRow("SELECT COUNT(*) FROM boards").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("after rollback boards = %d, want 0", n)
	}

	// Commit: success persists.
	if err := appdb.WithTx(ctx, sqldb, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO boards (id, name, position, created_at, archived) VALUES ('b2','Y','V','2026-01-01T00:00:00Z',0)`)
		return err
	}); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if err := sqldb.QueryRow("SELECT COUNT(*) FROM boards").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("after commit boards = %d, want 1", n)
	}
}
