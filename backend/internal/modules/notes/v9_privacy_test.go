package notes_test

// v9 — the private root, written FROM THE ATTACKER'S SIDE (PRD §V9-4a, §V9-8).
//
// Every test in this file is a second member or a non-owning admin trying to see,
// name, or infer something about a private note. That framing is the point:
// a test written after the handler tends to assert what the handler does, and
// what this feature needs asserted is what the RULE says.
//
// ⚠ The failure mode of this whole version is SILENT. A leak looks exactly like a
// working app — the tree renders, search returns rows, nothing errors. So the
// assertions here are deliberately about the things nobody would look at: a status
// code that is 404 rather than 403, a slug that is `recepty` rather than
// `recepty-2`, a header nobody reads.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/notes"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/optional"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// The household in these tests: two ordinary members and an admin who is neither.
func kajaCtx() context.Context  { return testsupport.CtxUser("u-kaja", "editor") }
func andyCtx() context.Context  { return testsupport.CtxUser("u-andy", "editor") }
func adminCtx() context.Context { return testsupport.CtxUser("u-admin", "admin") }

func privateScope(userID string) notes.Scope { return notes.Scope{Private: true, OwnerID: userID} }

// privateNote creates a note in ctx's own private root.
func (x *h) privateNote(ctx context.Context, title, body string) *notes.NoteDetail {
	x.t.Helper()
	return x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: title, BodyMD: body, Scope: "private"}))
}

// ---- The model ----

// TestCollidingPrivateNameKeepsBothSlugs is §8.1 of the brief, and the single case
// that fails if 06004's index is copied from 06001 unchanged.
//
// ⚠ ASSERT ON THE SLUG. The obvious expectation — that the second member gets a
// 409 — is wrong (D210): freeSlug LOOPS on SiblingSlugTaken and appends a suffix,
// so an un-scoped collision query hands Andy `recepty-2` and BOTH REQUESTS
// SUCCEED. There is no error to catch, nothing in the logs, and the only visible
// trace is a slug that quietly discloses a sibling Andy cannot see. No single-user
// test ever reaches this.
func TestCollidingPrivateNameKeepsBothSlugs(t *testing.T) {
	x := newH(t)

	kaja := x.privateNote(kajaCtx(), "Recepty", "guláš")
	andy := x.privateNote(andyCtx(), "Recepty", "svíčková")

	if kaja.Slug != "recepty" {
		t.Errorf("kaja's private note: slug = %q, want %q", kaja.Slug, "recepty")
	}
	if andy.Slug != "recepty" {
		t.Errorf("andy's private note: slug = %q, want %q — the sibling-slug index or "+
			"Store.SiblingSlugTaken is still un-scoped, so andy's note was suffixed against a "+
			"sibling he cannot see (D178/D210). Both halves have to carry the scope",
			andy.Slug, "recepty")
	}
	// …and a shared note of the same name is a third, independent row.
	shared := x.note(x.svc.CreateNote(kajaCtx(), notes.NoteCreate{Title: "Recepty"}))
	if shared.Slug != "recepty" {
		t.Errorf("the household's shared Recepty: slug = %q, want %q", shared.Slug, "recepty")
	}
	// The index still does its original job WITHIN one root.
	again := x.note(x.svc.CreateNote(kajaCtx(), notes.NoteCreate{Title: "Recepty", Scope: "private"}))
	if again.Slug != "recepty-2" {
		t.Errorf("a second Recepty at kaja's OWN private root: slug = %q, want %q — the "+
			"sibling index has stopped constraining anything", again.Slug, "recepty-2")
	}
}

// TestCollidingPrivateFolderNameKeepsBothSlugs is the same case on `folders`. The
// index is per table, so fixing one proves nothing about the other.
func TestCollidingPrivateFolderNameKeepsBothSlugs(t *testing.T) {
	x := newH(t)
	kaja := x.folder(x.svc.CreateFolder(kajaCtx(), notes.FolderCreate{Name: "Recepty", Scope: "private"}))
	andy := x.folder(x.svc.CreateFolder(andyCtx(), notes.FolderCreate{Name: "Recepty", Scope: "private"}))
	if kaja.Slug != "recepty" || andy.Slug != "recepty" {
		t.Errorf("private folder slugs = %q / %q, want recepty / recepty (D178)", kaja.Slug, andy.Slug)
	}
}

// TestPrivateItemCarriesOwnerAndVisibility checks the pairing invariant on the
// happy path: private ⇒ owner set, shared ⇒ owner nil (D179).
func TestPrivateItemCarriesOwnerAndVisibility(t *testing.T) {
	x := newH(t)
	priv := x.privateNote(kajaCtx(), "Deník", "")
	if priv.Visibility != "private" {
		t.Errorf("visibility = %q, want private", priv.Visibility)
	}
	if priv.OwnerID == nil || *priv.OwnerID != "u-kaja" {
		t.Errorf("owner_id = %v, want u-kaja — a private item without an owner breaks the "+
			"pairing invariant (D179)", priv.OwnerID)
	}
	shared := x.note(x.svc.CreateNote(kajaCtx(), notes.NoteCreate{Title: "Nákup"}))
	if shared.Visibility != "shared" {
		t.Errorf("visibility = %q, want shared", shared.Visibility)
	}
	if shared.OwnerID != nil {
		t.Errorf("owner_id = %v, want nil — a shared item with an owner breaks the pairing "+
			"invariant in the other direction (D179)", *shared.OwnerID)
	}
}

// TestOwnerIsNeverTakenFromTheRequest: there is no field on the wire that names an
// owner, and the one that names a SCOPE resolves against the session. Asking for
// `private` as Andy can only ever produce Andy's root.
func TestOwnerIsNeverTakenFromTheRequest(t *testing.T) {
	x := newH(t)
	n := x.privateNote(andyCtx(), "Andyho", "")
	if got := deref(n.OwnerID); got != "u-andy" {
		t.Errorf("owner_id = %q, want u-andy — owner comes from reqctx, never the body (§V9-3)", got)
	}
	// Kaja asking for the private scope gets HER root, not Andy's.
	if _, err := x.svc.GetNoteDetail(kajaCtx(), n.ID); status(t, err) != 404 {
		t.Errorf("kaja reading andy's private note: %d, want 404", status(t, err))
	}
}

// ---- Leak table rows 1–3: tree, list, search, resolve ----

// TestTreeReturnsExactlyOneRootScope — never both trees in one response (row 1).
func TestTreeReturnsExactlyOneRootScope(t *testing.T) {
	x := newH(t)
	x.note(x.svc.CreateNote(kajaCtx(), notes.NoteCreate{Title: "Sdílená"}))
	x.privateNote(kajaCtx(), "Kajina soukromá", "")
	x.privateNote(andyCtx(), "Andyho soukromá", "")

	sharedTree, err := x.svc.Tree(kajaCtx(), false, notes.Scope{})
	if err != nil {
		t.Fatalf("shared tree: %v", err)
	}
	if titles := noteTitles(sharedTree.RootNotes); len(titles) != 1 || titles[0] != "Sdílená" {
		t.Errorf("shared tree root notes = %v, want [Sdílená] — a private note reached the "+
			"household tree (row 1)", titles)
	}
	privTree, err := x.svc.Tree(kajaCtx(), false, privateScope("u-kaja"))
	if err != nil {
		t.Fatalf("private tree: %v", err)
	}
	if titles := noteTitles(privTree.RootNotes); len(titles) != 1 || titles[0] != "Kajina soukromá" {
		t.Errorf("kaja's private tree = %v, want [Kajina soukromá] — either the shared tree "+
			"bled in or andy's did (row 1)", titles)
	}
}

// TestListAndSearchAreScopedToOneRoot covers row 2 — including the ?q= branch,
// where the predicate has to ride inside the FTS join (D184).
func TestListAndSearchAreScopedToOneRoot(t *testing.T) {
	x := newH(t)
	x.privateNote(kajaCtx(), "Kajino zzunikat", "tajemství")
	x.privateNote(andyCtx(), "Andyho zzunikat", "tajemství")
	x.note(x.svc.CreateNote(kajaCtx(), notes.NoteCreate{Title: "Sdílená zzunikat"}))

	// Andy searching the SHARED root sees only the shared note — not his own
	// private one, and certainly not Kaja's.
	page, err := x.svc.List(andyCtx(), "zzunikat", nil, false, notes.Scope{})
	if err != nil {
		t.Fatalf("shared search: %v", err)
	}
	if got := noteTitles(page.Items); len(got) != 1 || got[0] != "Sdílená zzunikat" {
		t.Errorf("shared search = %v, want [Sdílená zzunikat] (row 2, D184)", got)
	}
	// Andy searching his OWN private root sees only his.
	page, err = x.svc.List(andyCtx(), "zzunikat", nil, false, privateScope("u-andy"))
	if err != nil {
		t.Fatalf("private search: %v", err)
	}
	if got := noteTitles(page.Items); len(got) != 1 || got[0] != "Andyho zzunikat" {
		t.Errorf("andy's private search = %v, want [Andyho zzunikat] — kaja's private note "+
			"matched the same term and must not appear (row 2)", got)
	}
}

// TestListWithoutFolderReturnsTheWholeScope is D203's second half: `notes` never
// had a ?folder_id=root sentinel, so omitting folder_id used to mean "root notes
// only" while the contract said "all". Both halves are fixed now.
func TestListWithoutFolderReturnsTheWholeScope(t *testing.T) {
	x := newH(t)
	f := x.folder(x.svc.CreateFolder(kajaCtx(), notes.FolderCreate{Name: "Složka"}))
	x.note(x.svc.CreateNote(kajaCtx(), notes.NoteCreate{Title: "V kořeni"}))
	x.note(x.svc.CreateNote(kajaCtx(), notes.NoteCreate{Title: "Ve složce", FolderID: &f.ID}))

	all, err := x.svc.List(kajaCtx(), "", nil, false, notes.Scope{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all.Items) != 2 {
		t.Errorf("GET /api/notes with no folder_id returned %d notes, want 2 (both) — D203",
			len(all.Items))
	}
	root := ""
	rootOnly, err := x.svc.List(kajaCtx(), "", &root, false, notes.Scope{})
	if err != nil {
		t.Fatalf("list root: %v", err)
	}
	if got := noteTitles(rootOnly.Items); len(got) != 1 || got[0] != "V kořeni" {
		t.Errorf("?folder_id=root returned %v, want [V kořeni]", got)
	}
}

// TestListWithAForeignPrivateFolderIDReturnsNothing is the case the ?folder_id=
// half of D203 left open.
//
// ⚠ THE `?scope=` PARAMETER IS NOT THE DEFENCE HERE, which is why no single-scope
// test reaches this. The sibling key carries the scope only at the ROOT, where the
// sentinel encodes it; under a named folder it collapses to `folder_id = ?` and
// says nothing about visibility. So the caller's own scope never entered the
// query, and an id passed straight from the request — one Administrace →
// Soukromé položky hands admins BY DESIGN (D198) — returned the titles inside
// another member's private folder, under either value of ?scope=.
//
// Assert on the TITLES, not on a status: the request succeeds either way. An empty
// page is the same answer a folder id that was never issued produces (D180).
func TestListWithAForeignPrivateFolderIDReturnsNothing(t *testing.T) {
	x := newH(t)
	f := x.folder(x.svc.CreateFolder(kajaCtx(), notes.FolderCreate{Name: "Deník", Scope: "private"}))
	x.note(x.svc.CreateNote(kajaCtx(), notes.NoteCreate{Title: "Tajné", FolderID: &f.ID}))

	// Andy holds Kaja's private folder id and asks for its contents from both of
	// the roots he is allowed to name. Neither may answer.
	for _, sc := range []notes.Scope{{}, privateScope("u-andy")} {
		page, err := x.svc.List(andyCtx(), "", &f.ID, false, sc)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if got := noteTitles(page.Items); len(got) != 0 {
			t.Errorf("?folder_id=<kaja's private folder> with scope %+v returned %v, want none",
				sc, got)
		}
	}

	// …and the owner still gets it, or the fix would be a denial of the feature.
	own, err := x.svc.List(kajaCtx(), "", &f.ID, false, privateScope("u-kaja"))
	if err != nil {
		t.Fatalf("owner list: %v", err)
	}
	if got := noteTitles(own.Items); len(got) != 1 || got[0] != "Tajné" {
		t.Errorf("owner's own folder returned %v, want [Tajné]", got)
	}
}

// TestResolveIsScoped covers row 3: the same slug path names different items in
// different roots, so a path without a scope is meaningless.
func TestResolveIsScoped(t *testing.T) {
	x := newH(t)
	x.privateNote(kajaCtx(), "Recepty", "")
	x.privateNote(andyCtx(), "Recepty", "")

	kajaRes, err := x.svc.Resolve(kajaCtx(), "recepty", privateScope("u-kaja"))
	if err != nil {
		t.Fatalf("kaja resolve: %v", err)
	}
	andyRes, err := x.svc.Resolve(andyCtx(), "recepty", privateScope("u-andy"))
	if err != nil {
		t.Fatalf("andy resolve: %v", err)
	}
	if kajaRes.ID == andyRes.ID {
		t.Error("the same slug path resolved to the same id in two different private roots (row 3)")
	}
	// The shared root holds no such path at all.
	if _, err := x.svc.Resolve(kajaCtx(), "recepty", notes.Scope{}); status(t, err) != 404 {
		t.Errorf("resolving a private slug path in the shared scope: %d, want 404", status(t, err))
	}
}

// ---- Leak table row 4: detail by id ----

// TestForeignPrivateItemIs404ForEveryone — including an admin (D180, D181).
//
// ⚠ 404, NOT 403. A 403 confirms the id exists, which turns every by-id route into
// an existence oracle over the whole private tree. The status code IS the feature.
func TestForeignPrivateItemIs404ForEveryone(t *testing.T) {
	x := newH(t)
	n := x.privateNote(kajaCtx(), "Kajino", "tajné")
	f := x.folder(x.svc.CreateFolder(kajaCtx(), notes.FolderCreate{Name: "Kajina složka", Scope: "private"}))

	for _, tc := range []struct {
		who string
		ctx context.Context
	}{
		{"another member", andyCtx()},
		{"an admin", adminCtx()},
		{"a reader", readerCtx()},
	} {
		if _, err := x.svc.GetNoteDetail(tc.ctx, n.ID); status(t, err) != 404 {
			t.Errorf("%s reading a foreign private NOTE: %d, want 404 (never 403 — D180)",
				tc.who, status(t, err))
		}
		if _, err := x.svc.GetFolderDetail(tc.ctx, f.ID); status(t, err) != 404 {
			t.Errorf("%s reading a foreign private FOLDER: %d, want 404 (never 403 — D180)",
				tc.who, status(t, err))
		}
	}
	// The owner still sees it, or the whole thing is pointless.
	if got := x.note(x.svc.GetNoteDetail(kajaCtx(), n.ID)); got.Title != "Kajino" {
		t.Errorf("the owner's own read returned %q", got.Title)
	}
}

// TestForeignPrivateItemIsIndistinguishableFromAnUnknownId is the oracle test
// proper: the two responses must be the same, not merely both 4xx.
func TestForeignPrivateItemIsIndistinguishableFromAnUnknownId(t *testing.T) {
	x := newH(t)
	n := x.privateNote(kajaCtx(), "Kajino", "")

	_, errPrivate := x.svc.GetNoteDetail(andyCtx(), n.ID)
	_, errUnknown := x.svc.GetNoteDetail(andyCtx(), "01900000-0000-7000-8000-000000000000")

	if status(t, errPrivate) != status(t, errUnknown) || errPrivate.Error() != errUnknown.Error() {
		t.Errorf("a foreign private id and an unknown id gave different answers:\n"+
			"  private: %d %v\n  unknown: %d %v\n"+
			"Any difference between them is an existence oracle over the private tree (D180)",
			status(t, errPrivate), errPrivate, status(t, errUnknown), errUnknown)
	}
}

// ---- Leak table row 8: pins ----

// TestHouseholdPinOnPrivateNoteIs422 (D183). 422, NOT 403: the caller has the
// role; the operation is meaningless, because nobody else can open the note.
func TestHouseholdPinOnPrivateNoteIs422(t *testing.T) {
	x := newH(t)
	n := x.privateNote(kajaCtx(), "Kajino", "")

	_, err := x.svc.Pin(kajaCtx(), n.ID, "household", "")
	if status(t, err) != 422 {
		t.Errorf("household pin on a private note: %d, want 422 (not 403 — the role is fine, "+
			"the operation is not; D183)", status(t, err))
	}
	// A personal pin is the allowed one.
	st, err := x.svc.Pin(kajaCtx(), n.ID, "personal", "")
	if err != nil {
		t.Fatalf("personal pin on own private note: %v", err)
	}
	if !st.Personal {
		t.Error("personal pin did not stick")
	}
	// And a non-owner never gets that far: the note does not exist for them.
	if _, err := x.svc.Pin(andyCtx(), n.ID, "personal", ""); status(t, err) != 404 {
		t.Errorf("andy pinning kaja's private note: %d, want 404", status(t, err))
	}
}

// ---- Leak table rows 5/9/10: the widget and the catalogs ----

// TestPinnedWidgetShowsOnlyTheCallersPrivateRows asserts row 10 rather than
// reasoning about it — this is exactly the kind of "already correct" that turns
// out not to be.
func TestPinnedWidgetShowsOnlyTheCallersPrivateRows(t *testing.T) {
	x := newH(t)
	kajasNote := x.privateNote(kajaCtx(), "Kajino soukromé", "")
	andysNote := x.privateNote(andyCtx(), "Andyho soukromé", "")
	shared := x.note(x.svc.CreateNote(kajaCtx(), notes.NoteCreate{Title: "Sdílené"}))

	mustPin(t, x, kajaCtx(), kajasNote.ID, "personal")
	mustPin(t, x, andyCtx(), andysNote.ID, "personal")
	mustPin(t, x, kajaCtx(), shared.ID, "household")

	rows := widgetRows(t, x, "u-kaja")
	titles := map[string]string{}
	for _, r := range rows {
		titles[r.Title] = r.Visibility
	}
	if _, ok := titles["Andyho soukromé"]; ok {
		t.Error("kaja's Připnuté poznámky widget carried ANDY's private note (rows 9/10)")
	}
	if vis, ok := titles["Kajino soukromé"]; !ok {
		t.Error("kaja's own private pinned note is missing from her widget")
	} else if vis != "private" {
		t.Errorf("kaja's private row carries visibility %q, want private — the lock mark has "+
			"nothing to key off (D183)", vis)
	}
	if vis := titles["Sdílené"]; vis != "shared" {
		t.Errorf("the household row carries visibility %q, want shared", vis)
	}
}

// TestPinnedCountMetricIsPerMember is the metric half of row 10.
func TestPinnedCountMetricIsPerMember(t *testing.T) {
	x := newH(t)
	kajas := x.privateNote(kajaCtx(), "Kajino", "")
	andys := x.privateNote(andyCtx(), "Andyho", "")
	mustPin(t, x, kajaCtx(), kajas.ID, "personal")
	mustPin(t, x, andyCtx(), andys.ID, "personal")

	mod := notes.NewModule(x.svc)
	for _, tc := range []struct{ user string }{{"u-kaja"}, {"u-andy"}} {
		got, err := mod.MetricProvider().Value(kajaCtx(), tc.user, "notes.pinned_count", nowForTest())
		if err != nil {
			t.Fatalf("metric for %s: %v", tc.user, err)
		}
		if got != 1 {
			t.Errorf("notes.pinned_count for %s = %d, want 1 — each member sees only their own "+
				"private pin (row 10)", tc.user, got)
		}
	}
}

// ---- Leak table row 15: /ws payloads ----

// TestWebsocketPayloadsCarryNoTitles is the property D190 depends on.
//
// D190 leaves the hub broadcasting to every client because the payloads are
// id-only: what crosses is a UUID and the timing of a change, which was judged too
// small to justify per-user routing on a platform component. That judgement
// EXPIRES the moment a payload grows a title — so the property is a test, not a
// comment.
//
// ⚠ A PRIVATE ITEM'S PAYLOAD NOW CARRIES NO ID EITHER (Service.notifyScoped).
// D190's own reasoning is what argues for it: the trade it accepted was "a UUID and
// the timing", and a private item's UUID is worth more than that — audit.Redact
// blanks EntityID for exactly this reason, and the purge screen hands admins those
// ids by design (D198). The HUB IS STILL UN-SCOPED and no per-user routing was
// added, so D190 stands as decided; what changed is only what a private mutation
// puts on the wire. The type still goes out, which is what the household is meant
// to learn (D187) and all api/ws.ts needs — classify() switches on the type and
// invalidates by module prefix, never reading the payload.
func TestWebsocketPayloadsCarryNoTitles(t *testing.T) {
	x := newHWithNotify(t)
	n := x.privateNote(kajaCtx(), "Velmi tajný název", "tělo poznámky")
	x.note(x.svc.UpdateNote(kajaCtx(), n.ID, notes.NoteUpdate{Title: optional.Of("Ještě tajnější")}, ""))
	f := x.folder(x.svc.CreateFolder(kajaCtx(), notes.FolderCreate{Name: "Tajná složka", Scope: "private"}))

	if len(x.published) == 0 {
		t.Fatal("no websocket messages were published — the test proves nothing")
	}
	for _, p := range x.published {
		for _, forbidden := range []string{"Velmi tajný název", "Ještě tajnější", "Tajná složka",
			"tělo poznámky", "velmi-tajny-nazev", "tajna-slozka"} {
			if strings.Contains(p, forbidden) {
				t.Errorf("a /ws payload carried %q: %s\n\n"+
					"D190 leaves the hub un-scoped ONLY because payloads are id-only. "+
					"A title in a payload means every connected client just received it, "+
					"and that decision has to be revisited rather than this test relaxed.",
					forbidden, p)
			}
		}
		// The ids of the private note and the private folder must not be there
		// either — every message here describes one of them.
		for _, id := range []string{n.ID, f.ID} {
			if strings.Contains(p, id) {
				t.Errorf("a /ws payload carried the id of a PRIVATE item (%s): %s\n\n"+
					"hub.Publish fans out to every connected client, so this is a "+
					"real-time existence-and-activity oracle over another member's tree. "+
					"Route private mutations through Service.notifyScoped.", id, p)
			}
		}
	}
}

// ---- helpers ----

func noteTitles(items []notes.NoteSummary) []string {
	out := make([]string, 0, len(items))
	for _, n := range items {
		out = append(out, n.Title)
	}
	return out
}

func mustPin(t *testing.T, x *h, ctx context.Context, id, scope string) {
	t.Helper()
	if _, err := x.svc.Pin(ctx, id, scope, ""); err != nil {
		t.Fatalf("pin %s (%s): %v", id, scope, err)
	}
}

func widgetRows(t *testing.T, x *h, userID string) []notes.PinnedNote {
	t.Helper()
	mod := notes.NewModule(x.svc)
	providers := mod.Widgets()
	if len(providers) != 1 {
		t.Fatalf("expected exactly one notes widget, got %d", len(providers))
	}
	data, err := providers[0].Data(testsupport.CtxUser(userID, "editor"), registryUser(userID))
	if err != nil {
		t.Fatalf("widget data: %v", err)
	}
	w, ok := data.(notes.PripnutePoznamkyWidget)
	if !ok {
		t.Fatalf("widget payload is %T, want PripnutePoznamkyWidget", data)
	}
	return w.Notes
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// registryUser builds the widget host's view of a caller.
func registryUser(userID string) registry.User {
	return registry.User{ID: userID, Email: userID + "@example.test", Roles: []string{"editor"}}
}

func nowForTest() time.Time { return time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC) }

// hn is h plus a recording notifier, for the /ws payload test. The published
// messages are captured as JSON so the assertion is against what actually crosses
// the wire rather than against a Go value's String().
type hn struct {
	*h
	published []string
}

func newHWithNotify(t *testing.T) *hn {
	t.Helper()
	db := testsupport.NewDB(t)
	blob, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	out := &hn{}
	notify := func(_ context.Context, typ string, payload any) {
		b, err := json.Marshal(map[string]any{"type": typ, "payload": payload})
		if err != nil {
			t.Fatalf("marshal ws payload: %v", err)
		}
		out.published = append(out.published, string(b))
	}
	svc := notes.NewService(db, audit.NewSink(), notify, blob,
		notes.ImageOptions{MaxUploadBytes: 1 << 20},
		slog.New(slog.NewJSONHandler(io.Discard, nil)))
	out.h = &h{t: t, svc: svc, db: db, blob: blob}
	return out
}

// ---- Leak table row 21: a private note's image, reached by reference ----

// TestForeignSharedReferenceCannotUnlockAPrivateImage is the hole the reference
// term in GetNoteImageForViewer left open.
//
// The term exists for a real case: an image uploaded into a PRIVATE note and then
// copied by its owner into a SHARED one keeps its private owner (ReassignNoteImage
// runs only on hard delete), so without it the household sees a permanently broken
// image inside a note they can fully read. But "any live shared note references it"
// made WRITING A REFERENCE — an unprivileged act — into a grant of read access,
// and the ids needed to write one are handed to admins BY DESIGN by Soukromé
// položky (D198). Andy pastes the URL into a note he owns; the image opens.
//
// ⚠ ASSERT ON BOTH HALVES. A fix that simply deletes the term passes the first
// case and silently breaks the second, which is the one nobody would notice for
// months — the broken image is invisible to the only member who could repair it.
func TestForeignSharedReferenceCannotUnlockAPrivateImage(t *testing.T) {
	x := newH(t)
	ctx := context.Background()

	// Kaja uploads an image into her own private note.
	secret := x.privateNote(kajaCtx(), "Soukromá", "")
	img, err := x.svc.UploadImage(kajaCtx(), secret.ID, bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("upload image: %v", err)
	}
	url := "/api/notes/images/" + img.ID

	// Andy, an ordinary editor, learns the id and pastes it into a SHARED note of
	// his own — exactly what an admin can do with a row from Soukromé položky.
	x.note(x.svc.CreateNote(andyCtx(), notes.NoteCreate{
		Title: "Nevinná poznámka", BodyMD: "![](" + url + ")",
	}))

	got, err := x.svc.Store().GetNoteImageForViewer(ctx, x.db, img.ID, "u-andy")
	if err != nil {
		t.Fatalf("load image as andy: %v", err)
	}
	if got != nil {
		t.Errorf("Andy read Kaja's PRIVATE image by referencing it from a note he wrote.\n\n" +
			"Writing a reference must not grant read access: the ids are handed to admins " +
			"by design (D198), so this turns \"can name it well enough to delete it\" into " +
			"\"well enough to open it\" (D197). The reference term must require the OWNER's " +
			"own act — a shared note authored by the private note's owner.")
	}

	// The owner is unaffected: she reads her own image through the ownership branch.
	if mine, err := x.svc.Store().GetNoteImageForViewer(ctx, x.db, img.ID, "u-kaja"); err != nil || mine == nil {
		t.Errorf("the owner lost access to her own image (err=%v) — the fix over-reached", err)
	}

	// And the case the term exists for still works: Kaja copies the image into a
	// SHARED note she writes herself, and now the household can see it.
	x.note(x.svc.CreateNote(kajaCtx(), notes.NoteCreate{
		Title: "Sdílená s obrázkem", BodyMD: "![](" + url + ")",
	}))
	shared, err := x.svc.Store().GetNoteImageForViewer(ctx, x.db, img.ID, "u-andy")
	if err != nil {
		t.Fatalf("load image after the owner shared it: %v", err)
	}
	if shared == nil {
		t.Error("after the OWNER embedded her private-note image in a shared note she wrote, " +
			"the household still cannot load it — a permanently broken image inside a note " +
			"they can fully read, invisible to the one person who could fix it. That is the " +
			"case the reference term exists for; it must survive.")
	}
}
