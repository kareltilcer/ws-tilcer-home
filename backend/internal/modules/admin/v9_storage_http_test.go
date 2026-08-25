package admin_test

// v9 — the two Úložiště routes AT THE HTTP LAYER (PRD FR-V9-11…FR-V9-14, D198).
//
// v9_storage_test.go covers StorageService and RecordPrivateItemsView directly,
// which leaves everything the HANDLER does untested — and the handler is where all
// the hand-rolled validation lives:
//
//	the `module` allow-list, now asked of the catalog rather than hardcoded;
//	the `sort` allow-list;
//	the `sort=size` + `cursor` REFUSAL, which exists so a silently-ignored cursor
//	  cannot hand a client page one twice as duplicates;
//	`?refresh=` parsing;
//	the admin gate on both routes;
//	and the ordering guarantee that the audit event is written BEFORE the listing
//	  is assembled, so a read that fails half-way is still recorded.
//
// Every one of those could regress green without this file — "simplifying" the
// cursor refusal into a silent ignore being the obvious way.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/admin"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
)

// withStorage wires a storage service onto the fixture's admin service, the way
// main.go does after the module set exists. The catalog carries ONE inventory
// module on purpose: `notes` is in it and `documents` is not, which is what lets
// the allow-list test below prove the list is read from the catalog rather than
// from a literal that happens to contain both shipped names.
func withStorage(t *testing.T, f *fixture, items []storage.Item) {
	t.Helper()
	mod := &fakeModule{
		name:   "notes",
		tables: []string{"notes"},
		items:  items,
		total:  512,
	}
	f.svc.SetStorage(newStorageService(t, f.db, []any{mod}, okBlobStore{}))
}

func countViewEvents(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(
		`SELECT COUNT(*) FROM audit_events WHERE module = 'admin' AND action = 'private_items.view'`).
		Scan(&n); err != nil {
		t.Fatalf("count view events: %v", err)
	}
	return n
}

// Both storage routes are admin-only, exactly like the rest of Administrace (D62).
func TestStorageRoutesAreAdminOnly(t *testing.T) {
	for _, role := range []string{"reader", "editor"} {
		f := newFixture(t)
		withStorage(t, f, nil)
		h := f.router(role)
		for _, path := range []string{
			"/api/admin/storage",
			"/api/admin/storage/private-items",
		} {
			if rr := do(t, h, http.MethodGet, path, ""); rr.Code != http.StatusForbidden {
				t.Errorf("%s GET %s = %d, want 403", role, path, rr.Code)
			}
		}
		// ⚠ And a refused request must not have been audited. The view event answers
		// "who looked"; a 403 is somebody who did not.
		if n := countViewEvents(t, f); n != 0 {
			t.Errorf("%s: %d private_items.view events written for a refused request, want 0", role, n)
		}
	}
}

// The snapshot route answers, and ?refresh= is parsed rather than ignored.
func TestStorageSnapshotRouteServesAndRefreshes(t *testing.T) {
	f := newFixture(t)
	withStorage(t, f, nil)
	h := f.router("admin")

	rr := do(t, h, http.MethodGet, "/api/admin/storage", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/storage = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var first admin.StorageSnapshot
	if err := json.Unmarshal(rr.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if first.Cached {
		t.Error("the first snapshot came back cached:true — nothing had computed it yet")
	}

	// A second plain read is served from the 60-second cache and says so, because a
	// stale figure must LOOK stale (D195).
	var second admin.StorageSnapshot
	rr = do(t, h, http.MethodGet, "/api/admin/storage", "")
	if err := json.Unmarshal(rr.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode cached snapshot: %v", err)
	}
	if !second.Cached {
		t.Error("the second snapshot is not marked cached — the TTL is not being consulted")
	}

	// ⚠ ?refresh=true has to REACH the service. A dropped query param here is
	// invisible: the response is a valid snapshot either way, just the wrong one,
	// and Obnovit silently stops obnovit-ing.
	var refreshed admin.StorageSnapshot
	rr = do(t, h, http.MethodGet, "/api/admin/storage?refresh=true", "")
	if err := json.Unmarshal(rr.Body.Bytes(), &refreshed); err != nil {
		t.Fatalf("decode refreshed snapshot: %v", err)
	}
	if refreshed.Cached {
		t.Error("?refresh=true came back cached:true — the parameter is not reaching the service")
	}
}

// The `module` filter is validated against THE CATALOG, not a literal.
//
// ⚠ The fixture registers an inventory for `notes` only, so `documents` — a name a
// hardcoded allow-list would wave through — must be refused here. That asymmetry is
// the whole assertion: it fails if anyone puts the module names back in the handler.
func TestPrivateItemsModuleFilterComesFromTheCatalog(t *testing.T) {
	f := newFixture(t)
	withStorage(t, f, nil)
	h := f.router("admin")

	if rr := do(t, h, http.MethodGet, "/api/admin/storage/private-items?module=notes", ""); rr.Code != http.StatusOK {
		t.Errorf("module=notes = %d, want 200 (it is in the catalog): %s", rr.Code, rr.Body.String())
	}
	if rr := do(t, h, http.MethodGet, "/api/admin/storage/private-items?module=documents", ""); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("module=documents = %d, want 422 — this catalog declares no documents "+
			"inventory, so accepting it means the allow-list is hardcoded", rr.Code)
	}
	if rr := do(t, h, http.MethodGet, "/api/admin/storage/private-items?module=zahrada", ""); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("module=zahrada = %d, want 422", rr.Code)
	}
	// Absent means "every module", and must not be refused.
	if rr := do(t, h, http.MethodGet, "/api/admin/storage/private-items", ""); rr.Code != http.StatusOK {
		t.Errorf("no module filter = %d, want 200: %s", rr.Code, rr.Body.String())
	}
}

func TestPrivateItemsSortValidationAndCursorRefusal(t *testing.T) {
	f := newFixture(t)
	withStorage(t, f, nil)
	h := f.router("admin")

	for _, sort := range []string{"", "recent", "size"} {
		path := "/api/admin/storage/private-items?sort=" + sort
		if rr := do(t, h, http.MethodGet, path, ""); rr.Code != http.StatusOK {
			t.Errorf("sort=%q = %d, want 200: %s", sort, rr.Code, rr.Body.String())
		}
	}
	if rr := do(t, h, http.MethodGet, "/api/admin/storage/private-items?sort=title", ""); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("sort=title = %d, want 422 — sorting by anything nameable is exactly what "+
			"this screen refuses to be (D198)", rr.Code)
	}

	// ⚠ THE COMBINATION IS REFUSED, NOT IGNORED. A keyset cursor is an id and an id
	// does not locate a position in a size ordering, so honouring the cursor is
	// impossible — and silently dropping it returns page one again, which a client
	// following next_cursor accumulates as duplicate rows. On the screen that
	// decides what to permanently delete.
	rr := do(t, h, http.MethodGet, "/api/admin/storage/private-items?sort=size&cursor=abc", "")
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("sort=size&cursor= = %d, want 422 — a silently ignored cursor hands the "+
			"client page one twice", rr.Code)
	}
	// The same cursor is fine under the ordering that can actually resume.
	if rr := do(t, h, http.MethodGet, "/api/admin/storage/private-items?sort=recent&cursor=abc", ""); rr.Code != http.StatusOK {
		t.Errorf("sort=recent&cursor= = %d, want 200: %s", rr.Code, rr.Body.String())
	}
}

// The limit is clamped THROUGH THE ROUTE, not merely in the service.
func TestPrivateItemsLimitIsClampedAtTheRoute(t *testing.T) {
	f := newFixture(t)
	items := make([]storage.Item, 0, 250)
	for i := range 250 {
		items = append(items, storage.Item{
			ID:        string(rune('a'+i%26)) + string(rune('a'+i/26)),
			Module:    "notes",
			Kind:      storage.ItemNote,
			OwnerID:   "u-kaja",
			ByteSize:  1,
			CreatedAt: "2026-08-01T00:00:00Z",
		})
	}
	withStorage(t, f, items)
	h := f.router("admin")

	rr := do(t, h, http.MethodGet, "/api/admin/storage/private-items?limit=9999", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("limit=9999 = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var page admin.PrivateItemPage
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Items) > 200 {
		t.Errorf("limit=9999 returned %d items — the route is not clamping to maxLimit", len(page.Items))
	}
	if page.NextCursor == nil {
		t.Error("a truncated page carries no next_cursor: item 201 is unreachable, on the " +
			"one screen that reclaims space")
	}
}

// ⚠ THE AUDIT EVENT IS WRITTEN PER REQUEST, AND BEFORE THE LISTING (D198).
//
// "Who looked" is the answer this screen owes the household, so paging deeper is a
// person choosing to look again and is recorded again. What must never happen is
// the reverse — a listing served without the trace.
func TestEveryPrivateItemsRequestIsAudited(t *testing.T) {
	f := newFixture(t)
	withStorage(t, f, nil)
	h := f.router("admin")

	if n := countViewEvents(t, f); n != 0 {
		t.Fatalf("started with %d view events, want 0", n)
	}
	for i := 1; i <= 3; i++ {
		if rr := do(t, h, http.MethodGet, "/api/admin/storage/private-items?owner_user_id=u-kaja", ""); rr.Code != http.StatusOK {
			t.Fatalf("request %d = %d: %s", i, rr.Code, rr.Body.String())
		}
		if n := countViewEvents(t, f); n != i {
			t.Errorf("after %d requests there are %d view events, want %d", i, n, i)
		}
	}
	// And the filter rides along: "who looked at WHOSE items" is a different
	// question from "who opened the screen".
	var meta string
	if err := f.db.QueryRow(
		`SELECT meta FROM audit_events WHERE module = 'admin' AND action = 'private_items.view' LIMIT 1`).
		Scan(&meta); err != nil {
		t.Fatalf("read meta: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	if m["owner_user_id"] != "u-kaja" {
		t.Errorf("meta.owner_user_id = %v, want u-kaja", m["owner_user_id"])
	}
}

// A host that wired no catalog answers 500 rather than panicking inside a page
// render — the reason Service.storage is allowed to be nil at all.
func TestStorageRoutesWithoutACatalogAre500NotAPanic(t *testing.T) {
	f := newFixture(t)
	h := f.router("admin") // no SetStorage
	for _, path := range []string{
		"/api/admin/storage",
		"/api/admin/storage/private-items",
	} {
		if rr := do(t, h, http.MethodGet, path, ""); rr.Code != http.StatusInternalServerError {
			t.Errorf("GET %s with no catalog = %d, want 500", path, rr.Code)
		}
	}
}

// ⚠ PAGE TWO MUST BEGIN EXACTLY WHERE PAGE ONE ENDED, and until this test nothing
// asserted it (D198).
//
// The keyset is hand-rolled across three files that have to agree: the registry
// truncates the merged list to Limit+1, StorageService takes items[limit-1].ID as
// next_cursor, and the registry resumes on a STRICT `it.ID < cursor`. An off-by-one
// in any one of them drops or duplicates exactly one row per page boundary — and
// nothing on the screen would contradict it, because the only figure beside the
// list is total_bytes, which is summed independently and stays correct either way.
// A dropped row on this screen is a private item nobody can reclaim.
func TestPrivateItemsPagingCoversEveryItemExactlyOnce(t *testing.T) {
	const total, limit = 12, 5

	f := newFixture(t)
	items := make([]storage.Item, 0, total)
	for i := range total {
		// Zero-padded so the ids order the same way as the UUIDv7s they stand in for:
		// the cursor comparison is a string compare, so a test on unpadded ids would
		// pass against an ordering production never sees.
		items = append(items, storage.Item{
			ID:        fmt.Sprintf("p-%03d", i),
			Module:    "notes",
			Kind:      storage.ItemNote,
			OwnerID:   "u-kaja",
			ByteSize:  1,
			CreatedAt: "2026-08-01T00:00:00Z",
		})
	}
	withStorage(t, f, items)
	h := f.router("admin")

	seen := map[string]int{}
	var order []string
	cursor := ""
	for page := 1; ; page++ {
		if page > total+1 {
			t.Fatalf("still paging after %d requests — next_cursor is not advancing", page)
		}
		path := fmt.Sprintf("/api/admin/storage/private-items?sort=recent&limit=%d", limit)
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		rr := do(t, h, http.MethodGet, path, "")
		if rr.Code != http.StatusOK {
			t.Fatalf("page %d = %d: %s", page, rr.Code, rr.Body.String())
		}
		var got admin.PrivateItemPage
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode page %d: %v", page, err)
		}
		if len(got.Items) > limit {
			t.Fatalf("page %d returned %d items, want at most %d — the Limit+1 spare row "+
				"is leaking into the response instead of only setting the cursor", page, len(got.Items), limit)
		}
		for _, it := range got.Items {
			seen[it.ID]++
			order = append(order, it.ID)
		}
		if got.NextCursor == nil {
			break
		}
		cursor = *got.NextCursor
	}

	if len(order) != total {
		t.Errorf("paging returned %d rows over %d items — one of the three keyset "+
			"arithmetics disagrees with the other two", len(order), total)
	}
	for _, it := range items {
		switch n := seen[it.ID]; {
		case n == 0:
			t.Errorf("%s was never returned: a private item the purge screen cannot reach", it.ID)
		case n > 1:
			t.Errorf("%s was returned %d times — the cursor overlaps the previous page", it.ID, n)
		}
	}
	// Newest-first, strictly: a cursor can only resume an ordering the page actually
	// used, so a break here would mean the cursor is being compared against a
	// sequence it does not describe.
	for i := 1; i < len(order); i++ {
		if order[i-1] <= order[i] {
			t.Fatalf("row %d (%s) does not sort before row %d (%s) — the page is not "+
				"strictly newest-first", i-1, order[i-1], i, order[i])
		}
	}
}
