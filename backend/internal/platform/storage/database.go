package storage

import (
	"context"
	"database/sql"
	"os"
	"sync"
)

// Measuring the SQLite file (v9, PRD D193).
//
// Two figures, and the difference between them matters:
//
//	the TOTAL is exact — page_count × page_size, plus the WAL file's size on disk.
//	It is the only number on the Úložiště page that can be checked against `ls`.
//
//	the PER-TABLE breakdown comes from `dbstat`, a virtual table that reports real
//	page usage per b-tree. It is a COMPILE-TIME OPTION
//	(SQLITE_ENABLE_DBSTAT_VTAB), so whether the driver exposes it is PROBED ON
//	FIRST USE, never assumed — and re-probed after any probe that did not answer.
//	See Prober for why the negative is never cached.
//
// ⚠ WHERE dbstat IS ABSENT THE PAGE SHOWS ROW COUNTS AND NO BYTES. It never
// estimates. A guessed byte figure on a page whose entire job is reporting byte
// figures is worse than an honest gap — it is a number somebody will act on.
//
// (Answered during the v9 build and recorded in PRD §V9-12: modernc.org/sqlite
// v1.54.0 DOES expose dbstat, and its pgsize column sums to exactly
// page_count × page_size. The probe ships anyway — the driver could be swapped —
// and the fallback branch is exercised by a test rather than merely written.)

// DBStats is the measured state of the SQLite file.
type DBStats struct {
	TotalBytes int64
	WALBytes   int64
	FreeBytes  *int64
	// BytesAvailable reports whether PER-TABLE bytes could be measured at all.
	// When false every Table.Bytes is nil and only RowCount is populated.
	// TotalBytes above stays exact either way.
	BytesAvailable bool
	Tables         []TableStats
}

// TableStats is one b-tree's usage.
type TableStats struct {
	Name string
	// RowCount is always populated — a COUNT(*) needs no dbstat.
	RowCount int64
	// Bytes is real page usage, or nil when dbstat is unavailable. NEVER an estimate.
	Bytes *int64
	// Virtual reports that this name is a VIRTUAL table — in Home, the four
	// external-content FTS5 indexes (notes_fts, documents_fts, audit_events_fts,
	// garden_plants_fts).
	//
	// ⚠ A virtual table is a `type='table'` row in sqlite_master with NO B-TREE OF
	// ITS OWN: dbstat lists its four shadows and never the parent. So Bytes could
	// not be looked up for it, and reporting that as nil made the page render four
	// permanent *nezměřeno* rows on a build where dbstat works perfectly — reading
	// as four measurement failures when the truth is "this thing owns no pages;
	// its shadows carry them, and they are listed right here". Bytes/IndexBytes are
	// therefore a MEASURED ZERO, and this flag is what lets the page say why rather
	// than showing a bare 0 B that looks like an empty index.
	//
	// RowCount is left at zero and NOT counted: see MeasureDatabase.
	Virtual bool
	// IndexBytes is the pages used by this table's indexes, reported apart from
	// the rows because an FTS5 index can outweigh what it indexes.
	//
	// ⚠ It is NOT a dbstat column. dbstat reports every b-tree separately and an
	// index IS its own b-tree, so this is the sum of the b-trees sqlite_master
	// maps back to this table — implicit sqlite_autoindex_* entries included. A
	// per-table figure that ignores that loses every index in the file. (Recorded
	// in PRD §V9-12; the spec did not anticipate it.)
	IndexBytes *int64
}

// Prober answers whether dbstat is usable.
type Prober struct {
	mu       sync.Mutex
	ok       bool
	disabled bool
}

// DisableForTest makes every probe answer "unavailable" without querying.
//
// ⚠ IT EXISTS SO THE FALLBACK BRANCH IS EXERCISED, and it is a seam on purpose. A
// failure path that has never run is not a fallback, it is a comment — but the
// only honest way to reach it used to be priming the probe against a broken handle
// and relying on the negative STICKING, which is exactly the bug this type no
// longer has. Better an explicitly named test seam than a production misfeature
// doing the same job by accident.
func (p *Prober) DisableForTest() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.disabled, p.ok = true, false
}

// Available probes on first use and caches ONLY A POSITIVE ANSWER.
//
// ⚠ THE ASYMMETRY IS THE POINT. "dbstat resolves" is a property of the build and
// can never stop being true, so it is cached forever. "dbstat did not answer" is
// NOT that property — the probe runs on a request's context, so a cancelled
// request (the admin closed the tab), a SQLITE_BUSY under a concurrent write, or
// any other transient error produces exactly the same error value as an absent
// vtab. Latching that negative in a sync.Once made ONE such blip disable
// per-table bytes for every admin for the life of the process, with the whole
// Úložiště page reading *nezměřeno* until the container restarted and nothing
// anywhere saying why.
//
// Re-probing after a failure costs one statement that fails at PREPARE time when
// the vtab is genuinely absent — no scan, no rows — and the snapshot around it is
// cached for a minute anyway. A cheap query on the rare failing path is the right
// trade against a permanent silent degradation.
func (p *Prober) Available(ctx context.Context, db *sql.DB) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.disabled {
		return false
	}
	if p.ok {
		return true
	}
	var n int
	// LIMIT 1 rather than count(*): the point is whether the vtab RESOLVES, and on
	// a large database a full scan to answer that would be a slow probe.
	err := db.QueryRowContext(ctx, `SELECT 1 FROM dbstat LIMIT 1`).Scan(&n)
	p.ok = err == nil || err == sql.ErrNoRows
	return p.ok
}

// MeasureDatabase computes the whole database picture for one file.
//
// dbPath is used only for the WAL stat; an empty path (an in-memory or unknown
// database) yields WALBytes 0 rather than an error, because a missing WAL is a
// normal state and not a failure to report.
func MeasureDatabase(ctx context.Context, db *sql.DB, dbPath string, prober *Prober, tables []string) (DBStats, error) {
	var out DBStats
	var pageCount, pageSize, freelist int64
	if err := db.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pageCount); err != nil {
		return out, err
	}
	if err := db.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&pageSize); err != nil {
		return out, err
	}
	if err := db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&freelist); err != nil {
		return out, err
	}
	out.TotalBytes = pageCount * pageSize
	free := freelist * pageSize
	out.FreeBytes = &free
	if dbPath != "" {
		if fi, err := os.Stat(dbPath + "-wal"); err == nil {
			out.WALBytes = fi.Size()
		}
	}

	out.BytesAvailable = prober.Available(ctx, db)
	var pages map[string]int64
	if out.BytesAvailable {
		var err error
		if pages, err = pagesByBTree(ctx, db); err != nil {
			// A dbstat that probes clean but fails on a real query is not something to
			// guess around: degrade to row counts and say so, which is the same honest
			// gap the absent-vtab path produces.
			out.BytesAvailable = false
			pages = nil
		}
	}
	// Only ever read inside the `pages != nil` branch below, so on a build without
	// dbstat — the fallback path this file ships a probe and a test for — the whole
	// sqlite_master index scan is skipped rather than run and discarded.
	var indexOwners map[string][]string
	if pages != nil {
		var err error
		if indexOwners, err = indexOwnersByTable(ctx, db); err != nil {
			return out, err
		}
	}

	// Which declared names are VIRTUAL tables. Read from sqlite_master rather than
	// matched on an `_fts` suffix: the platform is not supposed to know what a
	// module's tables mean, and a suffix convention is a guess where `sql LIKE
	// 'CREATE VIRTUAL TABLE%'` is the fact.
	virtual, err := virtualTables(ctx, db)
	if err != nil {
		return out, err
	}

	for _, t := range tables {
		ts := TableStats{Name: t, Virtual: virtual[t]}
		if ts.Virtual {
			// ⚠ NO COUNT(*), AND NO PAGE LOOKUP — both would be wrong work.
			//
			// A COUNT(*) over an external-content FTS5 table is a full traversal of
			// the index, not the cheap b-tree count it looks like. audit_events_fts
			// indexes the audit spine — "usually the largest thing in the file" — so
			// on a database with a few hundred thousand events this walked the whole
			// index, once per cache miss, on a request an admin is waiting on. Times
			// four, for a figure that only restates the content table's own count
			// listed a few rows above.
			//
			// The bytes are a measured zero: the parent b-tree does not exist, and its
			// four shadows are declared separately and carry every page. The totals
			// were already right; what was wrong was calling that nil.
			//
			// Only when dbstat answered, though — on the fallback path NOTHING has a
			// byte figure, and a lone 0 B among a page of *nezměřeno* would read as
			// the one table somebody managed to measure.
			if pages != nil {
				zero, alsoZero := int64(0), int64(0)
				ts.Bytes, ts.IndexBytes = &zero, &alsoZero
			}
			out.Tables = append(out.Tables, ts)
			continue
		}
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "`+t+`"`).Scan(&ts.RowCount); err != nil {
			// ⚠ THE ROW STILL SHIPS. A declared table that does not exist is a stale
			// declaration, not a reason to fail the page — it appears with no counts,
			// and the completeness test is what turns it into a build failure.
			//
			// Dropping it here instead (a `continue`) was worse than under-reporting:
			// a COUNT(*) can also fail for reasons that have nothing to do with the
			// table existing — a cancelled request, an I/O error, a locked table — and
			// silently removing a b-tree (notes_fts_data is routinely one of the
			// largest) makes the per-module figures stop summing to the database
			// total. That arithmetic is the page's entire premise, and nothing on the
			// screen would have said a table went missing.
			//
			// Scan leaves RowCount at zero on error; the byte figures below come from
			// dbstat and are unaffected, which is what keeps the sum whole.
			ts.RowCount = 0
		}
		if pages != nil {
			if b, ok := pages[t]; ok {
				ts.Bytes = &b
			}
			var idx int64
			for _, ix := range indexOwners[t] {
				idx += pages[ix]
			}
			ts.IndexBytes = &idx
		}
		out.Tables = append(out.Tables, ts)
	}
	return out, nil
}

// pagesByBTree reads real page usage per b-tree name.
func pagesByBTree(ctx context.Context, db *sql.DB) (map[string]int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT name, SUM(pgsize) FROM dbstat GROUP BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var name string
		var size int64
		if err := rows.Scan(&name, &size); err != nil {
			return nil, err
		}
		out[name] = size
	}
	return out, rows.Err()
}

// indexOwnersByTable maps a table to the index b-trees that belong to it —
// including SQLite's implicit sqlite_autoindex_* entries, which have no CREATE
// INDEX anywhere and are easy to lose.
func indexOwnersByTable(ctx context.Context, db *sql.DB) (map[string][]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT name, tbl_name FROM sqlite_master WHERE type = 'index'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var name, tbl string
		if err := rows.Scan(&name, &tbl); err != nil {
			return nil, err
		}
		out[tbl] = append(out[tbl], name)
	}
	return out, rows.Err()
}

// virtualTables reports which `type='table'` rows are VIRTUAL tables.
//
// They are the one kind of declared table dbstat cannot price and COUNT(*) cannot
// price cheaply, so MeasureDatabase has to tell them apart — see TableStats.Virtual.
// Detected from the stored DDL rather than from a name convention: a module could
// name an FTS index anything, and `sqlite_master.sql` is the authority either way.
func virtualTables(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND sql LIKE 'CREATE VIRTUAL TABLE%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}

// UserTables enumerates every table actually present in the file — what the
// completeness test compares the catalog against.
func UserTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// PlatformTables are the tables no feature module owns (v9, D192/D211).
//
// ⚠ THIS ALLOW-LIST IS NOT OPTIONAL, and shipping the completeness test without it
// is how a guard like that dies: a test that red-lights on day one gets deleted
// rather than fixed. Three of these are not in any migration at all —
//
//	goose_db_version  the migration ledger goose creates for itself
//	sqlite_sequence   SQLite's own AUTOINCREMENT bookkeeping
//	sqlite_schema     not a `type='table'` row, but a real b-tree in dbstat
//
// — and none of them belongs to a feature.
var PlatformTables = []string{
	// platform/db's own block: the session store, the per-member push tables (any
	// module may send, so consent cannot belong to `admin`), and the outbox cursor.
	"sessions",
	"push_subscriptions",
	"notification_preferences",
	"audit_notify_cursor",
	// Not created by any migration.
	"goose_db_version",
	"sqlite_sequence",
	// ⚠ sqlite_schema is NOT a `type='table'` row, so the completeness test never
	// sees it — but it IS a b-tree with real pages in dbstat, and leaving it out
	// makes the per-module totals fall short of the database total by exactly its
	// size. Small, but the arithmetic is the page's premise: a breakdown that does
	// not add up is a breakdown nobody can trust for the figures that DO matter.
	"sqlite_schema",
}
