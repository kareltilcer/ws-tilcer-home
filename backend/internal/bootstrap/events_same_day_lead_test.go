package bootstrap_test

// 04002 widens events.reminder_lead to admit the same-day reminder '0d'. SQLite
// cannot alter a CHECK, so it is a table rebuild — and this one is not 08003's.
//
// ⚠ `events` IS THE FIRST REBUILT TABLE HERE WITH INCOMING FOREIGN KEYS.
// `event_links` and `event_reminder_completions` both REFERENCE events (id) ON
// DELETE CASCADE, and with foreign_keys=ON (platform/db/db.go) a `DROP TABLE
// events` performs an implicit DELETE of every row before dropping — firing both
// cascades. The household's links and its entire completion history would be
// gone and the migration would report success.
//
// ⚠ AN EMPTY DATABASE CANNOT FAIL ANY OF THAT, which is why both tests below seed
// a parent WITH children before migrating. The same split §V9-12 and
// v10_migration_test.go record: the interesting half of a rebuild only exists
// where there are rows.

import (
	"database/sql"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/bootstrap"
)

// sameDayLeadFile is the migration under test, named once.
const sameDayLeadFile = "04002_reminder_lead_same_day.sql"

// preSameDayLeadFS is the merged schema WITHOUT 04002 — the migration set
// production has applied the morning this ships.
func preSameDayLeadFS(t *testing.T) fs.FS {
	t.Helper()
	return migrationFSWithout(t, []string{sameDayLeadFile})
}

// migrationFSWithout is the merged migration set minus the named files.
//
// ⚠ IT FAILS THE TEST IF A NAME IS NOT IN THE SET. A renamed migration would
// otherwise leave an "upgrade" test migrating everything at once and then
// everything again — green, and proving nothing about the upgrade it claims to
// test. That guard is why the exclusion is by FILENAME rather than by a version
// cutoff: the block-per-module numbering means no version V separates one
// release's migrations from an earlier release's.
func migrationFSWithout(t *testing.T, exclude []string) fs.FS {
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
	for _, f := range exclude {
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
	for name := range skip {
		t.Fatalf("migration %q is not in the merged migration set — was it renamed? "+
			"The exclusion list must be kept in step or this test stops testing the upgrade at all", name)
	}
	return out
}

// seedEventWithChildren writes one event and gives it a link and a completion —
// the shape whose survival the rebuild is about.
func seedEventWithChildren(t *testing.T, sqldb *sql.DB, id, title, lead string) {
	t.Helper()
	var leadVal any
	enabled := 0
	if lead != "" {
		leadVal, enabled = lead, 1
	}
	if _, err := sqldb.Exec(`
		INSERT INTO events
		  (id, title, description, starts_on, rrule, timezone, reminder_enabled,
		   reminder_lead, created_by, created_at, updated_at, archived)
		VALUES (?, ?, 'popis', '2026-07-15', 'FREQ=MONTHLY;INTERVAL=1', 'Europe/Prague', ?,
		        ?, 'u-kaja', '2026-07-01T10:00:00.000Z', '2026-07-01T10:00:00.000Z', 0)`,
		id, title, enabled, leadVal); err != nil {
		t.Fatalf("seed event %s: %v", id, err)
	}
	if _, err := sqldb.Exec(`
		INSERT INTO event_links (id, event_id, url, title, position)
		VALUES (?, ?, 'https://example.com', 'Návod', 'a0')`, "l-"+id, id); err != nil {
		t.Fatalf("seed link for %s: %v", id, err)
	}
	if _, err := sqldb.Exec(`
		INSERT INTO event_reminder_completions
		  (id, event_id, occurrence_on, completed_by, completed_at)
		VALUES (?, ?, '2026-07-15', 'u-andy', '2026-07-15T08:00:00.000Z')`,
		"c-"+id, id); err != nil {
		t.Fatalf("seed completion for %s: %v", id, err)
	}
}

func eventFingerprint(t *testing.T, sqldb *sql.DB) []string {
	t.Helper()
	return fingerprint(t, sqldb, `
		SELECT id || '|' || title || '|' || starts_on || '|' || COALESCE(rrule, '<null>') ||
		       '|' || reminder_enabled || '|' || COALESCE(reminder_lead, '<null>') ||
		       '|' || archived
		  FROM events ORDER BY id`)
}

func childFingerprint(t *testing.T, sqldb *sql.DB) []string {
	t.Helper()
	links := fingerprint(t, sqldb, `
		SELECT 'link|' || id || '|' || event_id || '|' || url || '|' || COALESCE(title, '<null>') ||
		       '|' || position FROM event_links ORDER BY id`)
	comps := fingerprint(t, sqldb, `
		SELECT 'completion|' || id || '|' || event_id || '|' || occurrence_on || '|' ||
		       COALESCE(completed_by, '<null>') || '|' || completed_at
		  FROM event_reminder_completions ORDER BY id`)
	return append(links, comps...)
}

func fingerprint(t *testing.T, sqldb *sql.DB, query string) []string {
	t.Helper()
	rows, err := sqldb.Query(query)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan fingerprint: %v", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("fingerprint rows: %v", err)
	}
	return out
}

// TestSameDayLeadRebuildKeepsChildrenAndTheCascade is the whole reason 04002 is
// three rebuilds instead of one.
func TestSameDayLeadRebuildKeepsChildrenAndTheCascade(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateWith(t, sqldb, preSameDayLeadFS(t))

	seedEventWithChildren(t, sqldb, "e1", "Zaplatit nájem", "1w")
	seedEventWithChildren(t, sqldb, "e2", "Narozeniny", "") // no reminder at all
	beforeEvents := eventFingerprint(t, sqldb)
	beforeChildren := childFingerprint(t, sqldb)

	migrateFull(t, sqldb)

	if after := eventFingerprint(t, sqldb); !equalStrings(beforeEvents, after) {
		t.Fatalf("the rebuild changed the events table.\n before: %v\n  after: %v", beforeEvents, after)
	}
	if after := childFingerprint(t, sqldb); !equalStrings(beforeChildren, after) {
		t.Fatalf("the rebuild lost or changed a child row — the ON DELETE CASCADE fired on the "+
			"parent drop, which is exactly what renaming instead of dropping is there to prevent.\n"+
			" before: %v\n  after: %v", beforeChildren, after)
	}

	// The indexes come back on all three tables, or the dashboard's hot query and
	// the per-event link and completion lookups quietly go to full scans.
	for table, want := range map[string][]string{
		"events":                     {"idx_events_reminder", "idx_events_starts_on"},
		"event_links":                {"idx_event_links_event_position"},
		"event_reminder_completions": {"idx_completions_event_occ"},
	} {
		if got := indexesOn(t, sqldb, table); !equalStrings(got, want) {
			t.Errorf("after the rebuild %s has indexes %v, want %v", table, got, want)
		}
	}

	// The point of the exercise: '0d' is accepted, and only '0d' joined the list.
	if _, err := sqldb.Exec(`
		INSERT INTO events (id, title, starts_on, reminder_enabled, reminder_lead, created_at, updated_at)
		VALUES ('e3', 'Vynést koš', '2026-07-15', 1, '0d', '2026-07-01T10:00:00.000Z', '2026-07-01T10:00:00.000Z')`); err != nil {
		t.Fatalf("a same-day reminder is still refused after 04002: %v", err)
	}
	if _, err := sqldb.Exec(`
		INSERT INTO events (id, title, starts_on, reminder_enabled, reminder_lead, created_at, updated_at)
		VALUES ('e4', 'X', '2026-07-15', 1, '0w', '2026-07-01T10:00:00.000Z', '2026-07-01T10:00:00.000Z')`); err == nil {
		t.Error("'0w' was accepted — the rebuild widened the CHECK by more than one member")
	}

	// And the children still hang off the NEW events by a live cascade, not off a
	// dangling reference to the table the rebuild dropped. A rebuild that re-pointed
	// them at nothing would pass every assertion above and lose referential
	// integrity for good.
	if _, err := sqldb.Exec(`DELETE FROM events WHERE id = 'e1'`); err != nil {
		t.Fatalf("delete e1: %v", err)
	}
	var orphans int
	if err := sqldb.QueryRow(`
		SELECT (SELECT COUNT(*) FROM event_links WHERE event_id = 'e1')
		     + (SELECT COUNT(*) FROM event_reminder_completions WHERE event_id = 'e1')`).
		Scan(&orphans); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if orphans != 0 {
		t.Errorf("deleting an event left %d orphaned children — the ON DELETE CASCADE did not "+
			"survive the rebuild", orphans)
	}
}

// TestSameDayLeadDownRemapsRatherThanDropping is the half a down migration
// usually gets wrong.
//
// ⚠ THE UPDATE MUST COME FIRST. By the time anyone runs this down the table may
// hold '0d' rows, and the narrow CHECK rejects them on the copy step — failing
// halfway and leaving a half-built table. Unlike 08003's delivery log these are
// the household's own events, so they are remapped to '1d' rather than deleted,
// and the reminder stays enabled.
func TestSameDayLeadDownRemapsRatherThanDropping(t *testing.T) {
	sqldb, _ := openTempDB(t)
	migrateFull(t, sqldb)

	seedEventWithChildren(t, sqldb, "kos", "Vynést koš", "0d")
	seedEventWithChildren(t, sqldb, "najem", "Zaplatit nájem", "1w")
	beforeChildren := childFingerprint(t, sqldb)

	runSection(t, sqldb, sameDayLeadFile, "Down")

	var lead string
	var enabled int
	if err := sqldb.QueryRow(`SELECT reminder_lead, reminder_enabled FROM events WHERE id = 'kos'`).
		Scan(&lead, &enabled); err != nil {
		t.Fatalf("the down migration lost the same-day event entirely: %v", err)
	}
	if lead != "1d" || enabled != 1 {
		t.Errorf("after the down the same-day event is lead=%q enabled=%d, want 1d/1 — a rollback "+
			"that clears the reminder loses it without saying so", lead, enabled)
	}
	if err := sqldb.QueryRow(`SELECT reminder_lead FROM events WHERE id = 'najem'`).Scan(&lead); err != nil {
		t.Fatalf("read najem: %v", err)
	}
	if lead != "1w" {
		t.Errorf("the down rewrote an untouched lead to %q", lead)
	}
	if after := childFingerprint(t, sqldb); !equalStrings(beforeChildren, after) {
		t.Fatalf("the down migration lost a child row.\n before: %v\n  after: %v", beforeChildren, after)
	}

	// The narrow CHECK is genuinely back.
	if _, err := sqldb.Exec(`
		INSERT INTO events (id, title, starts_on, reminder_enabled, reminder_lead, created_at, updated_at)
		VALUES ('z', 'X', '2026-07-15', 1, '0d', '2026-07-01T10:00:00.000Z', '2026-07-01T10:00:00.000Z')`); err == nil {
		t.Error("a same-day reminder was accepted after the down migration — the CHECK was not restored")
	}
	for table, want := range map[string][]string{
		"events":                     {"idx_events_reminder", "idx_events_starts_on"},
		"event_links":                {"idx_event_links_event_position"},
		"event_reminder_completions": {"idx_completions_event_occ"},
	} {
		if got := indexesOn(t, sqldb, table); !equalStrings(got, want) {
			t.Errorf("after the down %s has indexes %v, want %v", table, got, want)
		}
	}

	// And Up runs again over the rolled-back schema, because a down nobody can undo
	// is not a rollback.
	runSection(t, sqldb, sameDayLeadFile, "Up")
	if _, err := sqldb.Exec(`
		INSERT INTO events (id, title, starts_on, reminder_enabled, reminder_lead, created_at, updated_at)
		VALUES ('z2', 'X', '2026-07-15', 1, '0d', '2026-07-01T10:00:00.000Z', '2026-07-01T10:00:00.000Z')`); err != nil {
		t.Fatalf("re-running 04002 Up after its Down failed: %v", err)
	}
}
