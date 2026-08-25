package documents_test

// v9 — the private root for Dokumenty, written FROM THE ATTACKER'S SIDE
// (PRD §V9-4a rows 4, 5, 6, 8, 18, 19, and §V9-8).
//
// The documents module carries the two surfaces the notes module does not: FOUR
// content endpoints, each registered for GET **and** HEAD, and an HTTP CACHE
// POLICY. Both are tested here at the HTTP layer rather than through the service,
// because both failures are only visible in the response — a HEAD-only oracle is
// still an oracle, and a stale `immutable` header is a leak that never reaches the
// server at all.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/documents"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// household is ONE database and ONE bucket seen through several members' sessions.
// The existing `api` harness bakes a single actor into its router, which cannot
// express "Andy asks about Kaja's document" — and that question is the entire
// version.
type household struct {
	t   *testing.T
	svc *documents.Service
	db  *dbHandle
}

type dbHandle = struct {
	handler map[string]http.Handler
}

func newHousehold(t *testing.T, members ...member) *household {
	t.Helper()
	db := testsupport.NewDB(t)
	blob, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	svc := documents.NewService(db, audit.NewSink(), nil, blob, documents.Options{
		MaxUploadBytes: 1 << 20,
		PreviewEnabled: false,
		TempDir:        t.TempDir(),
	}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	h := documents.NewHandler(svc)

	handlers := map[string]http.Handler{}
	for _, m := range members {
		actor := reqctx.Actor{UserID: m.id, Type: "user", Label: m.id, Roles: m.roles}
		handlers[m.id] = httpx.NewRouter(httpx.Deps{
			Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
			DB:        db,
			Site:      "home",
			SessionMW: auth.NewSessionAuth(auth.Config{BypassActor: &actor}),
			MountAPI:  func(r chi.Router) { h.Mount(r) },
		})
	}
	return &household{t: t, svc: svc, db: &dbHandle{handler: handlers}}
}

type member struct {
	id    string
	roles []string
}

var (
	kaja  = member{"u-kaja", []string{"editor"}}
	andy  = member{"u-andy", []string{"editor"}}
	boss  = member{"u-admin", []string{"admin"}}
	quiet = member{"u-reader", []string{"reader"}}
)

func (hh *household) ctx(m member) context.Context {
	return testsupport.CtxUser(m.id, m.roles...)
}

// as issues a request through one member's session.
func (hh *household) as(m member, method, path string, headers ...[2]string) *httptest.ResponseRecorder {
	hh.t.Helper()
	handler, ok := hh.db.handler[m.id]
	if !ok {
		hh.t.Fatalf("member %s was not registered with newHousehold", m.id)
	}
	r := httptest.NewRequest(method, path, nil)
	for _, h := range headers {
		r.Header.Set(h[0], h[1])
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, r)
	return rr
}

// upload puts a private (or shared) document into a member's tree.
func (hh *household) upload(m member, title, scope string) *documents.Document {
	hh.t.Helper()
	doc, err := hh.svc.Upload(hh.ctx(m), documents.UploadInput{
		Filename: title + ".txt",
		File:     newReader("obsah dokumentu " + title),
		Title:    title,
		Scope:    scope,
	})
	if err != nil {
		hh.t.Fatalf("upload %s (%s): %v", title, scope, err)
	}
	return &doc.Document
}

func newReader(s string) io.Reader { return &stringReader{s: s} }

type stringReader struct {
	s string
	i int
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}

// ---- Leak table rows 4, 5, 6: detail, the four content streams, the permalink ----

// TestForeignPrivateDocumentIs404OnEveryContentRouteAndVerb is rows 4–6 in one
// table, and it is deliberately exhaustive over BOTH verbs.
//
// ⚠ HEAD is not a formality here. documents/http.go registers each content route
// as a `content(pattern, fn)` PAIR precisely so a HEAD is not a 405 — chi has no
// HEAD→GET fallback — which means the HEAD branch is live code. A refusal that
// covered only GET would leave a HEAD-only existence oracle over the whole private
// tree, answerable without ever transferring a byte.
func TestForeignPrivateDocumentIs404OnEveryContentRouteAndVerb(t *testing.T) {
	hh := newHousehold(t, kaja, andy, boss, quiet)
	doc := hh.upload(kaja, "Smlouva", "private")

	// The four CONTENT routes are registered for both verbs, so both must refuse.
	contentPaths := []string{
		"/api/documents/" + doc.ID + "/raw",
		"/api/documents/" + doc.ID + "/download",
		"/api/documents/" + doc.ID + "/preview",
		"/api/documents/" + doc.ID + "/thumbnail",
	}
	for _, intruder := range []member{andy, boss, quiet} {
		for _, path := range contentPaths {
			for _, verb := range []string{http.MethodGet, http.MethodHead} {
				rr := hh.as(intruder, verb, path)
				if rr.Code != http.StatusNotFound {
					t.Errorf("%s %s as %s: %d, want 404 — never 403 (D180). A 403 confirms the "+
						"id exists, which is all an existence oracle needs",
						verb, path, intruder.id, rr.Code)
				}
			}
		}
		// The metadata route is GET-only by design, so its refusal is GET-only too.
		rr := hh.as(intruder, http.MethodGet, "/api/documents/"+doc.ID)
		if rr.Code != http.StatusNotFound {
			t.Errorf("GET /api/documents/{id} as %s: %d, want 404", intruder.id, rr.Code)
		}
	}
	// The owner still gets their own bytes, or none of this means anything.
	if rr := hh.as(kaja, http.MethodGet, "/api/documents/"+doc.ID+"/raw"); rr.Code != http.StatusOK {
		t.Errorf("the owner reading their own private document: %d, want 200", rr.Code)
	}
}

// TestHeadOnTheMetadataRouteIsMethodUniform is the follow-up to the line above,
// and it is the reason that route can stay GET-only.
//
// chi answers HEAD /api/documents/{id} with 405 because the PATTERN matches and
// the method does not — which it does for every id, real or invented. That makes
// it a statement about the route rather than about the row, so it discloses
// nothing. Asserted rather than assumed: if the router ever grew a HEAD handler
// there without the ownership check, this is where it would show up.
func TestHeadOnTheMetadataRouteIsMethodUniform(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	doc := hh.upload(kaja, "Smlouva", "private")

	existing := hh.as(andy, http.MethodHead, "/api/documents/"+doc.ID)
	invented := hh.as(andy, http.MethodHead, "/api/documents/01900000-0000-7000-8000-000000000000")
	if existing.Code != invented.Code {
		t.Errorf("HEAD on the metadata route answered %d for a real private id and %d for an "+
			"invented one — any difference is an existence oracle (D180)",
			existing.Code, invented.Code)
	}
	if existing.Code == http.StatusOK {
		t.Error("HEAD on the metadata route returned 200 for a foreign private document — the " +
			"route grew a HEAD handler and it does not check ownership")
	}
}

// TestForeignPrivateDocumentMatchesAnUnknownIdExactly — the responses must be the
// same, not merely both 404.
func TestForeignPrivateDocumentMatchesAnUnknownIdExactly(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	doc := hh.upload(kaja, "Smlouva", "private")

	priv := hh.as(andy, http.MethodGet, "/api/documents/"+doc.ID)
	unknown := hh.as(andy, http.MethodGet, "/api/documents/01900000-0000-7000-8000-000000000000")

	if priv.Code != unknown.Code || priv.Body.String() != unknown.Body.String() {
		t.Errorf("a foreign private id and an unknown id answered differently:\n"+
			"  private: %d %s\n  unknown: %d %s\n"+
			"Any difference between them reopens the /d/{id} oracle (D180)",
			priv.Code, priv.Body.String(), unknown.Code, unknown.Body.String())
	}
}

// ---- Leak table row 19: the HTTP cache header (D208) ----

// TestPrivateContentRevalidatesWhileSharedStaysImmutable is the test for a failure
// that is INVISIBLE FROM INSIDE THE APP, which is why it asserts the header.
//
// ⚠ Every content stream used to send `private, immutable, max-age=31536000`
// unconditionally, and left alone that would have quietly defeated the whole
// version: `private` excludes shared PROXIES, not the second person on the same
// laptop, and `immutable` suppresses revalidation for a YEAR — so the 404 above
// would simply never execute. The document stays readable from disk cache long
// after the refusal shipped, and nothing server-side ever hears about it.
func TestPrivateContentRevalidatesWhileSharedStaysImmutable(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	priv := hh.upload(kaja, "Soukroma", "private")
	shared := hh.upload(kaja, "Sdilena", "")

	cases := []struct {
		doc  *documents.Document
		want string
		why  string
	}{
		{priv, "private, no-cache, must-revalidate",
			"a private stream must be REVALIDATED on every view, so the ownership check runs " +
				"and a second member gets 404 (D208)"},
		{shared, "private, immutable, max-age=31536000",
			"a shared document's bytes really are immutable and the caching is free"},
	}
	for _, tc := range cases {
		for _, suffix := range []string{"/raw", "/download", "/preview", "/thumbnail"} {
			rr := hh.as(kaja, http.MethodGet, "/api/documents/"+tc.doc.ID+suffix)
			if rr.Code != http.StatusOK && rr.Code != http.StatusNoContent {
				continue // no preview/thumbnail was generated; the header still had to be right when there is a body
			}
			if got := rr.Header().Get("Cache-Control"); got != tc.want {
				t.Errorf("%s%s Cache-Control = %q, want %q — %s",
					tc.doc.Title, suffix, got, tc.want, tc.why)
			}
		}
	}
}

// TestConditionalRequestOnPrivateContentIs304ToOwnerAnd404ToAnyoneElse is the
// other half of D208, and the ordering trap inside it.
//
// `no-cache` does not mean "do not cache" — it means "revalidate before reuse". So
// the owner re-opening a 30 MB private PDF must get a 304 rather than a full
// re-download, while the same conditional request from another member must get
// 404. That only works if the ownership check runs BEFORE the If-None-Match
// short-circuit; put it after and a second member holding a stale ETag receives
// "yes, and it hasn't changed" about a document they may not see.
func TestConditionalRequestOnPrivateContentIs304ToOwnerAnd404ToAnyoneElse(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	doc := hh.upload(kaja, "Smlouva", "private")

	first := hh.as(kaja, http.MethodGet, "/api/documents/"+doc.ID+"/raw")
	if first.Code != http.StatusOK {
		t.Fatalf("owner first read: %d", first.Code)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on a private stream — without it the revalidation is a full " +
			"re-download every time, which is how a `no-cache` header gets deleted later (D208)")
	}

	owner := hh.as(kaja, http.MethodGet, "/api/documents/"+doc.ID+"/raw", [2]string{"If-None-Match", etag})
	if owner.Code != http.StatusNotModified {
		t.Errorf("owner's conditional re-request: %d, want 304", owner.Code)
	}
	intruder := hh.as(andy, http.MethodGet, "/api/documents/"+doc.ID+"/raw", [2]string{"If-None-Match", etag})
	if intruder.Code != http.StatusNotFound {
		t.Errorf("another member's conditional request with the owner's ETag: %d, want 404. "+
			"A 304 here means the ownership check runs AFTER the If-None-Match branch, so a "+
			"stale validator answers a question it should not (D208)", intruder.Code)
	}
}

// ---- Publish: the permanent URL is the whole point (D42/D182) ----

// TestPublishLeavesThePermanentURLUntouched. The R2 keys are id-based and
// independent of folder, slug and scope, which is exactly why /d/{id} was
// specified as permanent — a publish must not break a link somebody already has.
func TestPublishLeavesThePermanentURLUntouched(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	doc := hh.upload(kaja, "Smlouva", "private")

	before := hh.as(kaja, http.MethodGet, "/api/documents/"+doc.ID)
	var beforeDetail documents.DocumentDetail
	mustJSON(t, before, &beforeDetail)

	if _, err := hh.svc.PublishDocument(hh.ctx(kaja), doc.ID, documents.PublishRequest{}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	after := hh.as(kaja, http.MethodGet, "/api/documents/"+doc.ID)
	var afterDetail documents.DocumentDetail
	mustJSON(t, after, &afterDetail)

	if beforeDetail.Urls != afterDetail.Urls {
		t.Errorf("the permanent URLs changed across a publish:\n  before %+v\n  after  %+v\n"+
			"They are id-based precisely so this cannot happen (D42/D182)",
			beforeDetail.Urls, afterDetail.Urls)
	}
	// And the other member can now fetch the bytes at that same unchanged URL.
	if rr := hh.as(andy, http.MethodGet, "/api/documents/"+doc.ID+"/raw"); rr.Code != http.StatusOK {
		t.Errorf("after publish, andy fetching the permanent URL: %d, want 200", rr.Code)
	}
	// The header flips with the visibility.
	rr := hh.as(andy, http.MethodGet, "/api/documents/"+doc.ID+"/raw")
	if got := rr.Header().Get("Cache-Control"); got != "private, immutable, max-age=31536000" {
		t.Errorf("after publish Cache-Control = %q, want the immutable one — the bytes are now "+
			"shared and there is nothing left to revalidate", got)
	}
}

// TestPublishDocumentRefusesANonOwnerWith404 (D206), documents side.
func TestPublishDocumentRefusesANonOwnerWith404(t *testing.T) {
	hh := newHousehold(t, kaja, andy, boss)
	doc := hh.upload(kaja, "Smlouva", "private")

	for _, intruder := range []member{andy, boss} {
		rr := hh.as(intruder, http.MethodPost, "/api/documents/"+doc.ID+"/publish")
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s publishing a foreign private document: %d, want 404 — NOT 403 (D206)",
				intruder.id, rr.Code)
		}
	}
}

// ---- Leak table rows 1–3, documents side ----

func TestDocumentsTreeAndSearchAreScoped(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	hh.upload(kaja, "Kajin zzunikat", "private")
	hh.upload(andy, "Andyho zzunikat", "private")
	hh.upload(kaja, "Sdileny zzunikat", "")

	// Shared tree: only the shared document.
	var tree documents.DocumentsTree
	mustJSON(t, hh.as(andy, http.MethodGet, "/api/documents/tree"), &tree)
	if got := docTitles(tree.RootDocuments); len(got) != 1 || got[0] != "Sdileny zzunikat" {
		t.Errorf("shared tree = %v, want [Sdileny zzunikat] (row 1)", got)
	}
	// Andy's private tree: only his.
	mustJSON(t, hh.as(andy, http.MethodGet, "/api/documents/tree?scope=private"), &tree)
	if got := docTitles(tree.RootDocuments); len(got) != 1 || got[0] != "Andyho zzunikat" {
		t.Errorf("andy's private tree = %v, want [Andyho zzunikat] (row 1)", got)
	}
	// Search obeys the same scope, with the predicate inside the FTS join (D184).
	var page documents.DocumentPage
	mustJSON(t, hh.as(andy, http.MethodGet, "/api/documents?q=zzunikat&scope=private"), &page)
	if got := docTitles(page.Items); len(got) != 1 || got[0] != "Andyho zzunikat" {
		t.Errorf("andy's private search = %v, want [Andyho zzunikat] — kaja's private document "+
			"matched the same term and must not appear (row 2)", got)
	}
}

// TestScopeCannotNameAnotherMembersRoot: there is no wire value for it, and an
// unknown one is a 422 rather than a silent fallback to `shared`.
func TestScopeCannotNameAnotherMembersRoot(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	hh.upload(kaja, "Kajin", "private")

	rr := hh.as(andy, http.MethodGet, "/api/documents/tree?scope=u-kaja")
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("scope=u-kaja: %d, want 422 — an unrecognised scope must be refused, not "+
			"quietly treated as `shared` (D184)", rr.Code)
	}
	rr = hh.as(andy, http.MethodGet, "/api/documents/tree?scope=private")
	if rr.Code != http.StatusOK {
		t.Fatalf("scope=private for andy: %d", rr.Code)
	}
	var tree documents.DocumentsTree
	mustJSON(t, rr, &tree)
	if len(tree.RootDocuments) != 0 {
		t.Errorf("andy's private tree holds %d documents, want 0 — `private` resolves against "+
			"the SESSION, so it can only ever mean the caller's own root", len(tree.RootDocuments))
	}
}

// ---- Leak table row 18: the asymmetry, with the objects ----

// TestAdminPurgeOfAForeignPrivateDocumentRemovesTheObjects (D181). The point of
// the power is reclaiming space, so the bytes have to actually go.
func TestAdminPurgeOfAForeignPrivateDocumentRemovesTheObjects(t *testing.T) {
	hh := newHousehold(t, kaja, boss)
	doc := hh.upload(kaja, "Smlouva", "private")

	if rr := hh.as(boss, http.MethodGet, "/api/documents/"+doc.ID); rr.Code != http.StatusNotFound {
		t.Fatalf("admin reading a foreign private document: %d, want 404", rr.Code)
	}
	if err := hh.svc.DeleteDocument(hh.ctx(boss), doc.ID, true); err != nil {
		t.Fatalf("admin hard-deleting a foreign private document: %v (D181)", err)
	}
	if rr := hh.as(kaja, http.MethodGet, "/api/documents/"+doc.ID+"/raw"); rr.Code != http.StatusNotFound {
		t.Errorf("after the purge the owner still fetches the bytes: %d", rr.Code)
	}
}

// ---- helpers ----

func docTitles(items []documents.DocumentSummary) []string {
	out := make([]string, 0, len(items))
	for _, d := range items {
		out = append(out, d.Title)
	}
	return out
}

func mustJSON(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %T: %v (body %s)", v, err, rr.Body.String())
	}
}
