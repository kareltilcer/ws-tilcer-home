package arch_test

// The storage catalog's completeness guard (v9, PRD D192/D211).
//
// Every version before this one tripped on one of the "four non-registry host
// maps" — a hand-maintained table somewhere that a new module had to be added to,
// with nothing but a reviewer to notice when it wasn't. v9 touches none of them
// (D202). What it does instead is open a FIFTH registration surface: a module that
// ships a table and forgets to declare it in platform/storage becomes invisible on
// the Úložiště page, and the page's totals quietly stop adding up.
//
// That one is closable by machine, so it gets closed by machine. A table with no
// home fails the build.
//
// ⚠ THE ALLOW-LIST IS PART OF THE GUARD, NOT AN ESCAPE HATCH (D211). Ship the test
// without it and it red-lights on day one — at which point somebody deletes the
// test rather than fixing the schema, and the surface is open again with a
// comment claiming otherwise. The list covers exactly the tables no feature owns:
// platform/db's own block, plus goose_db_version and sqlite_sequence, which no
// migration creates at all.

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/bootstrap"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// TestEveryTableIsDeclaredByExactlyOneModule enumerates sqlite_master against the
// assembled catalog.
//
// ⚠ VERIFY THIS TEST BY WATCHING IT FAIL. A guard nobody has seen fail is a guard
// nobody knows works: add a throwaway `CREATE TABLE zz_scratch (id TEXT)` to any
// migration, run this, see it name the table, then take it out again. It was
// verified that way when it was written.
func TestEveryTableIsDeclaredByExactlyOneModule(t *testing.T) {
	db := testsupport.NewDB(t)
	ctx := context.Background()

	present, err := storage.UserTables(ctx, db)
	if err != nil {
		t.Fatalf("enumerate sqlite_master: %v", err)
	}
	if len(present) < 20 {
		t.Fatalf("only %d tables in the migrated schema — the fixture is not the real schema, "+
			"so this test would pass against almost anything", len(present))
	}

	reg, err := storage.Collect(bootstrap.StorageSourcesForTest()...)
	if err != nil {
		t.Fatalf("assemble the storage catalog: %v", err)
	}
	owners := reg.TableOwners()
	allowed := map[string]bool{}
	for _, t := range storage.PlatformTables {
		allowed[t] = true
	}

	var undeclared []string
	for _, table := range present {
		if _, ok := owners[table]; ok {
			continue
		}
		if allowed[table] {
			continue
		}
		undeclared = append(undeclared, table)
	}
	if len(undeclared) > 0 {
		sort.Strings(undeclared)
		t.Errorf("these tables exist in the schema and no module declares them:\n\n    %s\n\n"+
			"Add each one to its module's StorageTables(), or — if it genuinely belongs to no\n"+
			"feature — to storage.PlatformTables. Until then the Úložiště page silently omits\n"+
			"them and its per-module totals do not add up to the database total (D192).\n\n"+
			"If the new table is an external-content FTS5 index, remember it materialises FIVE\n"+
			"`type='table'` rows: use storage.FTSShadows(\"x_fts\") rather than listing them by\n"+
			"hand (D211).",
			strings.Join(undeclared, "\n    "))
	}

	// The other direction: a declaration for a table that no longer exists is a
	// stale entry, and it would make the page render an empty row forever.
	presentSet := map[string]bool{}
	for _, p := range present {
		presentSet[p] = true
	}
	var stale []string
	for table, module := range owners {
		if !presentSet[table] {
			stale = append(stale, table+" (declared by "+module+")")
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("these tables are declared but do not exist in the schema:\n\n    %s\n\n"+
			"A renamed or dropped table leaves a row on the storage page that can never be "+
			"measured.", strings.Join(stale, "\n    "))
	}
}

// TestFTSShadowTablesAreAttributedToTheirParentsModule is D211 stated directly.
//
// The shadow rows are frequently the LARGEST b-trees in the file, so dumping them
// into `platform` — the lazy fix when the test above first goes red — would leave
// the per-module breakdown systematically wrong while the totals still summed. The
// arithmetic would look fine and the answer would be useless.
func TestFTSShadowTablesAreAttributedToTheirParentsModule(t *testing.T) {
	reg, err := storage.Collect(bootstrap.StorageSourcesForTest()...)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	owners := reg.TableOwners()

	// Every FTS5 index in Home and the module whose data it indexes. ⚠ There are
	// FOUR, not the three the v9 spec counted: garden_plants_fts arrived in v7 and
	// the spec's "Home's three FTS tables" is stale (recorded in PRD §V9-12).
	for parent, wantModule := range map[string]string{
		"notes_fts":         "notes",
		"documents_fts":     "documents",
		"audit_events_fts":  "logging",
		"garden_plants_fts": "garden",
	} {
		for _, shadow := range storage.FTSShadows(parent) {
			got, ok := owners[shadow]
			if !ok {
				t.Errorf("FTS5 shadow table %q is declared by nobody — five `type='table'` rows "+
					"exist per external-content index, and only one appears in a migration (D211)",
					shadow)
				continue
			}
			if got != wantModule {
				t.Errorf("FTS5 shadow %q is attributed to %q, want %q — a shadow belongs to the "+
					"module owning its parent, or the per-module breakdown is wrong even though "+
					"the totals still add up (D211)", shadow, got, wantModule)
			}
		}
	}
}

// TestPlatformTablesAreNotClaimedByAModule keeps the allow-list honest in the
// other direction: it is for tables no feature owns, not a place to park one.
func TestPlatformTablesAreNotClaimedByAModule(t *testing.T) {
	reg, err := storage.Collect(bootstrap.StorageSourcesForTest()...)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	owners := reg.TableOwners()
	for _, table := range storage.PlatformTables {
		if module, ok := owners[table]; ok {
			t.Errorf("%q is in storage.PlatformTables AND declared by module %q — it would be "+
				"counted twice, or once under the wrong heading", table, module)
		}
	}
}

// TestRealModulesImplementTheStorageCatalog closes the gap that let a silent
// wiring bug ship past a green suite.
//
// ⚠ storage.Collect discovers BlobSource and PrivateInventory BY TYPE ASSERTION,
// and a module that implements neither is not an error — it is "a module that
// holds no bytes", which is the correct answer for eight of the ten. So when
// `notes` and `documents` had those methods on their SERVICE rather than on their
// MODULE, everything compiled, every test passed, and the Úložiště page quietly
// reported 0 B with an empty purge listing. It was found by opening the page.
//
// The assertion has to be POSITIVE — these two modules MUST implement them —
// because the negative case is indistinguishable from correct behaviour.
func TestRealModulesImplementTheStorageCatalog(t *testing.T) {
	byName := map[string]any{}
	for _, m := range bootstrap.StorageSourcesForTest() {
		if n, ok := m.(interface{ Name() string }); ok {
			byName[n.Name()] = m
		}
	}

	// The two modules that hold bytes outside SQLite must attribute them, and must
	// be able to list their private items for the purge screen.
	for _, name := range []string{"notes", "documents"} {
		m, ok := byName[name]
		if !ok {
			t.Fatalf("module %q is missing from the storage source set", name)
		}
		if _, ok := m.(storage.BlobSource); !ok {
			t.Errorf("module %q does not implement storage.BlobSource.\n\n"+
				"Its R2 usage will silently report as ZERO on Úložiště — no error, no "+
				"failing test, just a page that says the bucket is empty. Check that the "+
				"method is on the MODULE (which the catalog collects) and not only on the "+
				"Service (D191/D194).", name)
		}
		if _, ok := m.(storage.PrivateInventory); !ok {
			t.Errorf("module %q does not implement storage.PrivateInventory.\n\n"+
				"Soukromé položky will silently show an empty list, which reads as "+
				"\"nobody has private items\" rather than as a wiring fault (D198).", name)
		}
	}

	// …and every module must declare its tables, or the completeness test above is
	// the only thing standing between a new module and an invisible one.
	for name, m := range byName {
		if _, ok := m.(storage.Source); !ok {
			t.Errorf("module %q does not implement storage.Source — it declares no tables "+
				"(D192)", name)
		}
	}
}
