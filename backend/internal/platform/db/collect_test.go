package db_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
)

// collectDB opens a scratch database holding one two-column table with the rows
// given, so each test below scans real *sql.Rows rather than a stand-in.
func collectDB(t *testing.T, ids ...int) *sql.DB {
	t.Helper()
	sqldb, err := appdb.Open(filepath.Join(t.TempDir(), "collect.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	if _, err := sqldb.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for _, id := range ids {
		if _, err := sqldb.Exec(`INSERT INTO t (id, name) VALUES (?, 'row')`, id); err != nil {
			t.Fatalf("insert %d: %v", id, err)
		}
	}
	return sqldb
}

func scanID(s appdb.Scanner) (int, error) {
	var id int
	var name string
	if err := s.Scan(&id, &name); err != nil {
		return 0, err
	}
	return id, nil
}

func query(t *testing.T, sqldb *sql.DB) *sql.Rows {
	t.Helper()
	rows, err := sqldb.QueryContext(context.Background(), `SELECT id, name FROM t ORDER BY id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	return rows
}

func TestCollect_ScansEveryRowInOrder(t *testing.T) {
	sqldb := collectDB(t, 3, 1, 2)
	got, err := appdb.Collect(query(t, sqldb), scanID)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("expected [1 2 3], got %v", got)
	}
}

// TestCollect_EmptyIsNil pins the property nine call sites had to wrap in
// OrEmpty: an empty result is a NIL slice, which serialises as `null`. Flipping
// this default would silently change those responses to `[]` and the others from
// `null` to `[]` — a wire change, not a refactor.
func TestCollect_EmptyIsNil(t *testing.T) {
	sqldb := collectDB(t)
	got, err := appdb.Collect(query(t, sqldb), scanID)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if got != nil {
		t.Fatalf("expected a nil slice for zero rows, got %#v", got)
	}
	if e := appdb.OrEmpty(got); e == nil || len(e) != 0 {
		t.Fatalf("OrEmpty should turn nil into an empty non-nil slice, got %#v", e)
	}
}

// TestCollect_ScanErrorStopsAndClosesRows covers the two halves the loops it
// replaces had to get right by hand: a scan failure returns immediately with no
// partial slice, and the rows are closed either way. The close matters more here
// than it looks — the pool is capped at ONE connection (appdb.Open), so a leaked
// *sql.Rows deadlocks the next query in the process rather than merely leaking.
func TestCollect_ScanErrorStopsAndClosesRows(t *testing.T) {
	sqldb := collectDB(t, 1, 2, 3)
	boom := errors.New("boom")
	seen := 0
	got, err := appdb.Collect(query(t, sqldb), func(s appdb.Scanner) (int, error) {
		seen++
		if seen == 2 {
			return 0, boom
		}
		return scanID(s)
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected the scan error back, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected no partial slice on failure, got %v", got)
	}
	// A still-open *sql.Rows would hold the only connection and hang this query.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sqldb.QueryRowContext(ctx, `SELECT count(*) FROM t`).Scan(&seen); err != nil {
		t.Fatalf("connection was not released by Collect: %v", err)
	}
}
