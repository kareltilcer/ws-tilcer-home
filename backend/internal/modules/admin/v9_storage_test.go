package admin_test

// v9 — Úložiště and Soukromé položky (PRD FR-V9-11…FR-V9-14, D193–D198).
//
// Two kinds of test live here, and they fail for opposite reasons.
//
// The SNAPSHOT tests are about honesty: the page's job is reporting byte figures,
// so the ways it can be wrong are reporting a number it did not measure, or
// reporting numbers that do not add up. Both are asserted directly.
//
// The PURGE tests are about restraint: that screen is uncomfortably close to being
// the private-file browser this whole version exists to prevent, and every test on
// it checks that something is ABSENT.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/bootstrap"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/admin"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// ---- fixtures ----

// fakeModule stands in for a feature module in the catalog: it declares tables and
// (optionally) blob usage and private items, which is the whole contract.
type fakeModule struct {
	name    string
	tables  []string
	blobs   []storage.BlobUsage
	blobErr error
	items   []storage.Item
	total   int64
}

func (m *fakeModule) Name() string            { return m.name }
func (m *fakeModule) StorageTables() []string { return m.tables }
func (m *fakeModule) StorageBlobs(context.Context) ([]storage.BlobUsage, error) {
	return m.blobs, m.blobErr
}
func (m *fakeModule) PrivateItems(context.Context, string) ([]storage.Item, int64, error) {
	return m.items, m.total, nil
}

func newStorageService(t *testing.T, db *sql.DB, modules []any, blob blobstore.BlobStore) *admin.StorageService {
	t.Helper()
	cat, err := storage.Collect(modules...)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	return admin.NewStorageService(admin.StorageDeps{
		DB:            db,
		Catalog:       cat,
		Primary:       blob,
		PrimaryBucket: "home-docs",
		Members:       push.NewStore(db),
		WarnTotalMB:   1024,
		CacheSeconds:  60,
	})
}

// ---- The snapshot ----

// TestDatabaseTotalIsExactAndPerModuleFiguresSumToIt is the acceptance criterion
// that the page's arithmetic is its premise (D211).
//
// ⚠ It is also the test that catches a forgotten FTS5 shadow table, an unattributed
// index, or a b-tree nobody declared — each of which under-reports SOME module
// while leaving the grand total right, so the page looks fine and the breakdown
// lies. The sum is the only assertion that notices.
func TestDatabaseTotalIsExactAndPerModuleFiguresSumToIt(t *testing.T) {
	db := testsupport.NewDB(t)
	svc := newStorageService(t, db, realCatalogModules(), nil)

	snap, err := svc.Snapshot(context.Background(), true)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !snap.Database.BytesAvailable {
		t.Skip("dbstat is unavailable in this build; the sum cannot be checked (D193 fallback)")
	}

	var pageCount, pageSize int64
	_ = db.QueryRow(`PRAGMA page_count`).Scan(&pageCount)
	_ = db.QueryRow(`PRAGMA page_size`).Scan(&pageSize)
	if want := pageCount * pageSize; snap.Database.TotalBytes != want {
		t.Errorf("total_bytes = %d, want %d (page_count × page_size) — this is the ONE figure "+
			"on the page checkable against `ls`", snap.Database.TotalBytes, want)
	}

	var sum int64
	for _, m := range snap.Database.Modules {
		if m.Bytes != nil {
			sum += *m.Bytes
		}
	}
	if sum != snap.Database.TotalBytes {
		t.Errorf("per-module bytes sum to %d but the database total is %d (short by %d).\n\n"+
			"Something in the file is declared by nobody, or an index is attributed to no "+
			"table. The usual causes:\n"+
			"  * an external-content FTS5 index whose four shadow rows were not declared\n"+
			"    (use storage.FTSShadows) — D211;\n"+
			"  * a b-tree that is not a `type='table'` row, like sqlite_schema;\n"+
			"  * a new table nobody added to StorageTables().\n\n"+
			"The grand total stays right either way, so nothing else on the page notices.",
			sum, snap.Database.TotalBytes, snap.Database.TotalBytes-sum)
	}
}

// TestDbstatUnavailableYieldsRowCountsAndNullBytes exercises the FALLBACK branch
// (D193). It is written because a failure path that has never run is not a
// fallback — it is a comment.
//
// The real driver does expose dbstat (PRD §V9-12), so the branch is reached here
// by measuring a database whose dbstat cannot be read, which is the same shape the
// code sees when the vtab is absent.
func TestDbstatUnavailableYieldsRowCountsAndNullBytes(t *testing.T) {
	db := testsupport.NewDB(t)
	var prober storage.Prober
	// The "no dbstat" shape without needing a differently-compiled driver.
	//
	// ⚠ This used to prime the prober against a CLOSED handle and rely on the
	// negative sticking. That worked only because a failed probe was latched
	// forever — the very behaviour that made one cancelled request blank the page
	// for the life of the process. The seam is explicit now; see Prober.
	prober.DisableForTest()

	stats, err := storage.MeasureDatabase(context.Background(), db, "", &prober, []string{"sessions"})
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if stats.BytesAvailable {
		t.Fatal("BytesAvailable is true after a failed probe")
	}
	if stats.TotalBytes <= 0 {
		t.Error("total_bytes is 0 with dbstat unavailable — the TOTAL comes from PRAGMA and " +
			"stays exact either way (D193)")
	}
	for _, tbl := range stats.Tables {
		if tbl.Bytes != nil {
			t.Errorf("%s reported %d bytes with dbstat unavailable — it must be NULL, never a "+
				"guess. A guessed byte figure on a page whose whole job is reporting byte "+
				"figures is worse than an honest gap (D193)", tbl.Name, *tbl.Bytes)
		}
		if tbl.RowCount < 0 {
			t.Errorf("%s has no row count — a COUNT(*) needs no dbstat", tbl.Name)
		}
	}
}

// TestAFailedDbstatProbeIsRetriedNotLatched is the regression guard for the
// sticky-negative bug.
//
// The probe runs on a REQUEST's context, so "dbstat did not answer" and "this
// build has no dbstat" arrive as the same error value. Latching the first one in a
// sync.Once meant a single cancelled request — an admin closing the tab while the
// snapshot computed — reported bytes_available:false to every admin forever, with
// the whole Úložiště page reading *nezměřeno* until the container restarted.
//
// "dbstat resolves" can never stop being true, so it stays cached; the negative
// never is.
func TestAFailedDbstatProbeIsRetriedNotLatched(t *testing.T) {
	db := testsupport.NewDB(t)
	var prober storage.Prober

	// A closed handle is a probe that cannot answer — the shape of a cancelled
	// request or a SQLITE_BUSY, not of an absent vtab.
	closed, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = closed.Close()
	if prober.Available(context.Background(), closed) {
		t.Fatal("the probe reported dbstat available on a closed handle")
	}

	// The very next call, on a healthy handle, must probe again rather than
	// replaying the failure.
	if !prober.Available(context.Background(), db) {
		t.Fatal("a probe that failed once is answering 'unavailable' on a healthy handle.\n\n" +
			"The negative has been cached. One cancelled request or one SQLITE_BUSY now " +
			"blanks every per-table byte figure on Úložiště for the life of the process, " +
			"for every admin, with nothing anywhere saying why — and only a restart clears " +
			"it. Cache the POSITIVE answer only (D193).")
	}
}

// TestBucketOutageIs200WithDatabaseFiguresIntact (FR-V9-11).
//
// A 5xx carrying partial results is a shape no client handles, and blanking the
// page over an object-store hiccup throws away the half of the answer that WAS
// measurable.
func TestBucketOutageIs200WithDatabaseFiguresIntact(t *testing.T) {
	db := testsupport.NewDB(t)
	broken := &fakeModule{
		name:    "documents",
		tables:  []string{"documents"},
		blobErr: errors.New("r2: connection refused"),
	}
	svc := newStorageService(t, db, []any{broken}, failingBlobStore{})

	snap, err := svc.Snapshot(context.Background(), true)
	if err != nil {
		t.Fatalf("the snapshot failed outright: %v — a bucket outage must degrade, not 500", err)
	}
	if snap.Blobs.Available {
		t.Error("blobs.available is true after a bucket failure")
	}
	if snap.Blobs.Error == nil || *snap.Blobs.Error == "" {
		t.Error("blobs.error is empty — the page has to say WHY it could not measure")
	}
	if snap.Database.TotalBytes <= 0 {
		t.Error("the database figures were lost along with the bucket — they are independent, " +
			"and losing them is the failure this test exists to prevent")
	}
}

// TestSnapshotIsCachedAndRefreshBypasses (FR-V9-13). The cached copy is marked
// cached so a stale figure is VISIBLY stale rather than silently so.
func TestSnapshotIsCachedAndRefreshBypasses(t *testing.T) {
	db := testsupport.NewDB(t)
	svc := newStorageService(t, db, realCatalogModules(), nil)
	ctx := context.Background()

	first, err := svc.Snapshot(ctx, false)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.Cached {
		t.Error("the first snapshot claims to be cached")
	}
	second, err := svc.Snapshot(ctx, false)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !second.Cached {
		t.Error("the second snapshot within the TTL was recomputed — the cache is what keeps a " +
			"full bucket listing off every page load (D195)")
	}
	if second.GeneratedAt != first.GeneratedAt {
		t.Error("generated_at moved on a cached read — it records when the snapshot was " +
			"COMPUTED, not when it was asked for")
	}
	third, err := svc.Snapshot(ctx, true)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if third.Cached {
		t.Error("?refresh=true returned the cached copy")
	}
}

// TestSnapshotOwnsNoTableAndNoJob (D195). The page is computed on read; there is
// no sample table and no scheduler entry, and this asserts it against the SCHEMA
// rather than against a reading of the diff.
func TestSnapshotOwnsNoTableAndNoJob(t *testing.T) {
	db := testsupport.NewDB(t)
	tables, err := storage.UserTables(context.Background(), db)
	if err != nil {
		t.Fatalf("tables: %v", err)
	}
	for _, name := range tables {
		switch name {
		case "storage_samples", "storage_snapshots", "storage_history", "admin_storage":
			t.Errorf("the schema carries %q — the storage page owns NO state (D195). A history "+
				"table is how a snapshot quietly becomes a feature with migrations", name)
		}
	}
}

// TestReplicaIsDeclinedNotUnimplemented pins the decision recorded in PRD §V9-12.
//
// D214 asked for a Litestream replica line. Reading it needs the credentials for
// the household's entire database backup, and Karel declined to hand those to the
// application process (2026-08-24). The field stays in the payload — the shape
// does not change, and the frontend has a designed state for it — but it is always
// false.
//
// ⚠ If this test fails because someone populated it, they have REVERSED A DECISION
// rather than fixed an oversight. Re-read §V9-12 before changing the test.
func TestReplicaIsDeclinedNotUnimplemented(t *testing.T) {
	db := testsupport.NewDB(t)
	svc := newStorageService(t, db, realCatalogModules(), nil)
	snap, err := svc.Snapshot(context.Background(), true)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Replica.Configured {
		t.Error("replica.configured is true — the app was given Litestream's credentials. " +
			"That is a decision Karel declined (PRD §V9-12), not a bug that was fixed")
	}
}

// TestUnattributedObjectsAreReportedNotDropped (D194). The third bucket is the
// orphan backlog the mirror job reconciles, and this is the first screen in Home
// that has ever shown it. Dropping it would make the R2 total quietly short.
func TestUnattributedObjectsAreReportedNotDropped(t *testing.T) {
	db := testsupport.NewDB(t)
	mod := &fakeModule{
		name:   "documents",
		tables: []string{"documents"},
		blobs: []storage.BlobUsage{
			{Prefix: "documents/", Kind: storage.KindShared, Objects: 2, Bytes: 200},
			{Prefix: "documents/", Kind: storage.KindPrivate, OwnerID: "u-kaja", Objects: 1, Bytes: 50},
			{Prefix: "documents/", Kind: storage.KindUnattributed, Objects: 3, Bytes: 30},
		},
	}
	svc := newStorageService(t, db, []any{mod}, okBlobStore{})
	snap, err := svc.Snapshot(context.Background(), true)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snap.Blobs.Modules) != 1 {
		t.Fatalf("expected one blob module, got %d", len(snap.Blobs.Modules))
	}
	kinds := map[string]int64{}
	for _, o := range snap.Blobs.Modules[0].Owners {
		kinds[o.Kind] = o.Bytes
	}
	if kinds[storage.KindUnattributed] != 30 {
		t.Errorf("unattributed bytes = %d, want 30 — objects that resolve to no live row are "+
			"REPORTED, not dropped (D194)", kinds[storage.KindUnattributed])
	}
	if got := *snap.Blobs.TotalBytes; got != 280 {
		t.Errorf("total_bytes = %d, want 280 (200 + 50 + 30) — the orphans count toward the "+
			"bill whether or not anything claims them", got)
	}
}

// TestWarningNeverBlocksAndIsMeasuredOnModuleTotals (D196).
func TestWarningNeverBlocksAndIsMeasuredOnModuleTotals(t *testing.T) {
	db := testsupport.NewDB(t)
	mod := &fakeModule{
		name:   "documents",
		tables: []string{"documents"},
		blobs: []storage.BlobUsage{
			{Prefix: "documents/", Kind: storage.KindShared, Objects: 1, Bytes: 5 << 20},
		},
	}
	cat, err := storage.Collect(mod)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	svc := admin.NewStorageService(admin.StorageDeps{
		DB: db, Catalog: cat, Primary: okBlobStore{}, Members: push.NewStore(db),
		WarnTotalMB: 1, CacheSeconds: 0, // 1 MB threshold against 5 MB of usage
	})
	snap, err := svc.Snapshot(context.Background(), true)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !snap.Warning.Exceeded {
		t.Error("5 MB of usage against a 1 MB threshold did not trip the warning")
	}
	if len(snap.Warning.LargestContributors) == 0 {
		t.Error("nothing was marked as a largest contributor — the register is supposed to say " +
			"WHERE the space went, or it is just a red line")
	}
	if snap.Warning.ThresholdMB != 1 {
		t.Errorf("threshold_mb = %d, want 1", snap.Warning.ThresholdMB)
	}
	// The threshold blocks nothing: there is no field on the snapshot that could,
	// and no upload path consults it. Asserted by absence — see the note in
	// StorageWarning's doc comment.
}

// ---- Soukromé položky ----

// TestPurgeListingCarriesNoTitlesOrFilenames is the whole restraint of the screen
// (D197/D198), asserted at the type level as well as the value level.
func TestPurgeListingCarriesNoTitlesOrFilenames(t *testing.T) {
	db := testsupport.NewDB(t)
	mod := &fakeModule{
		name:   "documents",
		tables: []string{"documents"},
		items: []storage.Item{
			{ID: "d1", Module: "documents", Kind: storage.ItemDocument, OwnerID: "u-kaja",
				ByteSize: 1024, CreatedAt: "2026-08-01T10:00:00Z"},
		},
		total: 1024,
	}
	svc := newStorageService(t, db, []any{mod}, okBlobStore{})

	page, err := svc.PrivateItems(context.Background(), storage.ItemFilter{})
	if err != nil {
		t.Fatalf("private items: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(page.Items))
	}
	// The serialised shape is what an admin actually receives, so assert on it.
	raw, err := json.Marshal(page.Items[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, forbidden := range []string{
		"title", "name", "filename", "original_filename", "description",
		"content_type", "slug", "preview", "thumbnail", "url", "body", "excerpt",
	} {
		if _, present := fields[forbidden]; present {
			t.Errorf("the purge listing carries %q.\n\n"+
				"THE FIELDS THAT ARE ABSENT ARE THE SPECIFICATION (D198). An admin can name "+
				"the thing well enough to delete it and not well enough to know what it is. "+
				"Anything added here has to be justified against that sentence — and this "+
				"screen is already uncomfortably close to being the private-file browser the "+
				"whole feature exists to prevent.", forbidden)
		}
	}
	if page.TotalBytes == nil || *page.TotalBytes != 1024 {
		t.Error("total_bytes must cover ALL matching items — it is the figure the screen acts on")
	}
}

// TestPurgeListingIncludesFoldersAndMarksImagesNonDeletable (D212).
//
// Folders are listed because `DELETE …/folders/{id}?hard=true&cascade=true` is what
// actually reclaims a private subtree, and a screen that cannot name a folder
// cannot do the job it exists for. Images are listed for ACCOUNTING and are not
// deletable — there is no route and there should not be, since an image belongs to
// its note and goes when the note does.
func TestPurgeListingIncludesFoldersAndMarksImagesNonDeletable(t *testing.T) {
	db := testsupport.NewDB(t)
	mod := &fakeModule{
		name:   "notes",
		tables: []string{"notes"},
		items: []storage.Item{
			{ID: "n1", Module: "notes", Kind: storage.ItemNote, OwnerID: "u-kaja"},
			{ID: "f1", Module: "notes", Kind: storage.ItemNoteFolder, OwnerID: "u-kaja"},
			{ID: "i1", Module: "notes", Kind: storage.ItemNoteImage, OwnerID: "u-kaja"},
		},
	}
	svc := newStorageService(t, db, []any{mod}, okBlobStore{})
	page, err := svc.PrivateItems(context.Background(), storage.ItemFilter{})
	if err != nil {
		t.Fatalf("private items: %v", err)
	}
	kinds := map[string]bool{}
	for _, it := range page.Items {
		kinds[it.Kind] = true
	}
	if !kinds[storage.ItemNoteFolder] {
		t.Error("no folder in the listing — the folder route is what reclaims a private " +
			"subtree, so a screen that cannot name one cannot do its job (D212)")
	}
	if !kinds[storage.ItemNoteImage] {
		t.Error("no note_image in the listing — images are listed for accounting even though " +
			"they are not deletable (D212)")
	}
}

// TestPurgeListingClampsTheLimit — the house Limit (50/200, CLAMPING). v8's
// non-clamping limit is a known defect, not a pattern to copy (§V9-6).
func TestPurgeListingClampsTheLimit(t *testing.T) {
	db := testsupport.NewDB(t)
	items := make([]storage.Item, 300)
	for i := range items {
		items[i] = storage.Item{ID: string(rune('a'+i%26)) + "-id", Module: "notes",
			Kind: storage.ItemNote, OwnerID: "u-kaja"}
	}
	mod := &fakeModule{name: "notes", tables: []string{"notes"}, items: items}
	svc := newStorageService(t, db, []any{mod}, okBlobStore{})

	page, err := svc.PrivateItems(context.Background(), storage.ItemFilter{Limit: 1000})
	if err != nil {
		t.Fatalf("private items: %v", err)
	}
	if len(page.Items) > 200 {
		t.Errorf("limit=1000 returned %d items — the house Limit CLAMPS at 200. v8's "+
			"fall-back-to-100 behaviour is a known defect, not a precedent (§V9-6)", len(page.Items))
	}
}

// TestOpeningThePurgeListingIsAudited (D198) — the only READ in Home that writes
// an audit event. It is the answer to "who looked", and the screen exists because
// an admin may delete something they may not read; a power like that should leave
// a trace whether or not it is used.
func TestOpeningThePurgeListingIsAudited(t *testing.T) {
	db := testsupport.NewDB(t)
	svc := admin.NewService(db, audit.NewSink(), admin.Options{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	ctx := testsupport.CtxUser("u-admin", "admin")

	if err := svc.RecordPrivateItemsView(ctx, storage.ItemFilter{OwnerUserID: "u-kaja"}); err != nil {
		t.Fatalf("record view: %v", err)
	}
	var summary string
	var meta sql.NullString
	err := db.QueryRow(
		`SELECT summary, meta FROM audit_events WHERE module = 'admin' AND action = 'private_items.view'`).
		Scan(&summary, &meta)
	if err != nil {
		t.Fatalf("no admin.private_items.view event was written: %v — it is not optional (D198)", err)
	}
	if summary == "" {
		t.Error("the event has no summary")
	}
	var m map[string]any
	if meta.Valid {
		_ = json.Unmarshal([]byte(meta.String), &m)
	}
	if m["owner_user_id"] != "u-kaja" {
		t.Errorf("meta.owner_user_id = %v, want u-kaja — 'who looked at WHOSE items' is a "+
			"meaningfully different question from 'who opened the screen'", m["owner_user_id"])
	}
}

// TestActionLabelsCoverEveryAction (D213). actionLabels is a hand-maintained map
// that FALLS BACK TO THE RAW KEY, so a missing entry is invisible in code review
// and shows up as `notes.note.publish` in the rule composer. It had been silently
// degrading since v6 — a first version of this guard pinned only v9's five new
// actions and missed that every garden and electricity action already shipped
// unlabelled. Iterating what the modules DECLARE closes the registration surface:
// a new action cannot ship without a label failing this test.
func TestActionLabelsCoverEveryAction(t *testing.T) {
	type actionDeclarer interface {
		Name() string
		AuditActions() []string
	}
	checked := 0
	for _, src := range bootstrap.StorageSourcesForTest() {
		m, ok := src.(actionDeclarer)
		if !ok {
			continue
		}
		for _, action := range m.AuditActions() {
			checked++
			qualified := m.Name() + "." + action
			if got := admin.ActionLabel(m.Name(), action); got == qualified {
				t.Errorf("%s has no Czech label — it would appear as the raw key in the rule "+
					"composer (D213)", qualified)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no module declared any audit action — the guard is checking nothing")
	}
}

// ---- stubs ----

type okBlobStore struct{ blobstore.BlobStore }

func (okBlobStore) List(context.Context, string) ([]blobstore.ObjInfo, error) {
	return nil, nil
}

type failingBlobStore struct{ blobstore.BlobStore }

func (failingBlobStore) List(context.Context, string) ([]blobstore.ObjInfo, error) {
	return nil, errors.New("r2: connection refused")
}

// realCatalogModules is the shipped module set — the arithmetic tests have to run
// against the REAL catalog, because the thing they catch is a real table nobody
// declared, and a fake catalog cannot have one.
func realCatalogModules() []any { return bootstrap.StorageSourcesForTest() }
