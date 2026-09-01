package documents_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
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

// ---- HTTP harness ----

type api struct {
	t       *testing.T
	handler http.Handler
	svc     *documents.Service
	blob    blobstore.BlobStore
}

func newAPI(t *testing.T, roles ...string) *api {
	t.Helper()
	db := testsupport.NewDB(t)
	store, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	svc := documents.NewService(db, audit.NewSink(), nil, store, documents.Options{
		MaxUploadBytes: 4096, // small, so the 413 test stays cheap
		PreviewEnabled: true,
		TempDir:        t.TempDir(),
	}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	h := documents.NewHandler(svc)
	handler := testsupport.Router(t, db, h.Mount, roles...)
	return &api{t: t, handler: handler, svc: svc, blob: store}
}

func (a *api) do(method, path, body string) *httptest.ResponseRecorder {
	a.t.Helper()
	return testsupport.Send(a.t, a.handler, method, path, body)
}

func (a *api) get(path string, headers ...[2]string) *httptest.ResponseRecorder {
	a.t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for _, h := range headers {
		r.Header.Set(h[0], h[1])
	}
	rr := httptest.NewRecorder()
	a.handler.ServeHTTP(rr, r)
	return rr
}

// uploadMultipart builds a real multipart body. Field order matters: the handler
// streams parts in order, so the metadata fields must precede the file — exactly
// what the frontend sends.
func (a *api) uploadMultipart(filename string, body []byte, fields map[string]string) *httptest.ResponseRecorder {
	a.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			a.t.Fatalf("write field: %v", err)
		}
	}
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		a.t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write(body); err != nil {
		a.t.Fatalf("write file part: %v", err)
	}
	if err := mw.Close(); err != nil {
		a.t.Fatalf("close multipart: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/documents", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	a.handler.ServeHTTP(rr, r)
	return rr
}

func (a *api) uploadOK(filename string, body []byte, fields map[string]string) documents.DocumentDetail {
	a.t.Helper()
	rr := a.uploadMultipart(filename, body, fields)
	if rr.Code != http.StatusCreated {
		a.t.Fatalf("upload %s = %d, want 201 (%s)", filename, rr.Code, rr.Body.String())
	}
	var d documents.DocumentDetail
	if err := json.Unmarshal(rr.Body.Bytes(), &d); err != nil {
		a.t.Fatalf("decode upload response: %v", err)
	}
	return d
}

// ---- upload over HTTP ----

func TestHTTP_UploadStreamsMultipartAndAppliesMetadataFields(t *testing.T) {
	a := newAPI(t, "editor")
	d := a.uploadOK("Smlouva ČEZ.pdf", pdfBytes(), map[string]string{
		"title":       "Smlouva na elektřinu",
		"description": "Zákaznické číslo v záhlaví",
	})

	if d.Title != "Smlouva na elektřinu" {
		t.Errorf("title = %q, want the form's title", d.Title)
	}
	if d.Description == nil || *d.Description != "Zákaznické číslo v záhlaví" {
		t.Errorf("description = %v, want the form's description", d.Description)
	}
	// The response carries the permanent id-based URL block (D42).
	if d.Urls.Raw != "/api/documents/"+d.ID+"/raw" || d.Urls.Permalink != "/d/"+d.ID {
		t.Errorf("urls = %+v, want id-based permanent URLs", d.Urls)
	}
}

func TestHTTP_UploadOverTheCapIs413(t *testing.T) {
	a := newAPI(t, "editor")
	rr := a.uploadMultipart("velky.bin", bytes.Repeat([]byte("x"), 8192), nil)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-cap upload = %d, want 413 (%s)", rr.Code, rr.Body.String())
	}
	objects, _ := a.blob.List(context.Background(), "documents/")
	if len(objects) != 0 {
		t.Errorf("objects after a rejected upload = %v, want none", objects)
	}
}

func TestHTTP_UploadWithoutAFilePartIs422(t *testing.T) {
	a := newAPI(t, "editor")
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", "bez souboru")
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/documents", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	a.handler.ServeHTTP(rr, r)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("upload with no file part = %d, want 422", rr.Code)
	}
}

func TestHTTP_UploadWithTooManyFieldsIs422(t *testing.T) {
	a := newAPI(t, "editor")
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	// Each field is individually tiny, so the per-field cap never trips. Without a
	// part COUNT the reader would consume them until EOF — an authenticated client
	// could hold the request open indefinitely by never sending a file part.
	for i := 0; i < 200; i++ {
		_ = mw.WriteField("title", "x")
	}
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/documents", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	a.handler.ServeHTTP(rr, r)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("upload with 200 metadata fields = %d, want 422", rr.Code)
	}
}

// ---- content endpoints: isolation, caching, ranges (FR-DOC8, D48) ----

func TestHTTP_RawIsInlineForSafeTypesAndAttachmentOtherwise(t *testing.T) {
	a := newAPI(t, "editor")

	pdf := a.uploadOK("smlouva.pdf", pdfBytes(), nil)
	rr := a.get("/api/documents/" + pdf.ID + "/raw")
	if rr.Code != http.StatusOK {
		t.Fatalf("raw pdf = %d", rr.Code)
	}
	if disp := rr.Header().Get("Content-Disposition"); !strings.HasPrefix(disp, "inline") {
		t.Errorf("pdf disposition = %q, want inline", disp)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("pdf content-type = %q", ct)
	}
	// The isolation headers are security-critical, not cosmetic.
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("raw must carry nosniff")
	}
	if csp := rr.Header().Get("Content-Security-Policy"); csp != "sandbox" {
		t.Errorf("raw CSP = %q, want the strict sandbox", csp)
	}
	if cc := rr.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") || !strings.Contains(cc, "private") {
		t.Errorf("cache-control = %q, want private+immutable", cc)
	}
	if et := rr.Header().Get("ETag"); et != `"`+pdf.Checksum+`"` {
		t.Errorf("ETag = %q, want the checksum", et)
	}

	// An uploaded HTML page must never render in home's origin.
	html := a.uploadOK("utok.html", []byte("<!doctype html><script>alert(1)</script>"), nil)
	rr = a.get("/api/documents/" + html.ID + "/raw")
	if disp := rr.Header().Get("Content-Disposition"); !strings.HasPrefix(disp, "attachment") {
		t.Errorf("html disposition = %q, want attachment (D48)", disp)
	}

	// SVG is a scriptable document, so it is attachment too even though it is an image/*.
	svg := a.uploadOK("obrazek.svg", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), nil)
	rr = a.get("/api/documents/" + svg.ID + "/raw")
	if disp := rr.Header().Get("Content-Disposition"); !strings.HasPrefix(disp, "attachment") {
		t.Errorf("svg disposition = %q, want attachment (D48)", disp)
	}
}

func TestHTTP_DownloadIsAlwaysAnAttachmentWithTheCzechFilename(t *testing.T) {
	a := newAPI(t, "editor")
	d := a.uploadOK("Smlouva ČEZ.pdf", pdfBytes(), nil)
	rr := a.get("/api/documents/" + d.ID + "/download")
	if rr.Code != http.StatusOK {
		t.Fatalf("download = %d", rr.Code)
	}
	disp := rr.Header().Get("Content-Disposition")
	if !strings.HasPrefix(disp, "attachment") {
		t.Errorf("disposition = %q, want attachment", disp)
	}
	// RFC 5987: an ASCII fallback plus the percent-encoded UTF-8 name.
	if !strings.Contains(disp, "filename*=UTF-8''") || !strings.Contains(disp, "%C4%8C") {
		t.Errorf("disposition = %q, want an RFC 5987-encoded Czech filename", disp)
	}
}

func TestHTTP_RawHonoursIfNoneMatchAndRange(t *testing.T) {
	a := newAPI(t, "editor")
	body := []byte("0123456789abcdefghij")
	d := a.uploadOK("data.txt", body, nil)

	// Immutable bytes make a 304 always correct.
	rr := a.get("/api/documents/"+d.ID+"/raw", [2]string{"If-None-Match", `"` + d.Checksum + `"`})
	if rr.Code != http.StatusNotModified {
		t.Errorf("If-None-Match = %d, want 304", rr.Code)
	}

	rr = a.get("/api/documents/"+d.ID+"/raw", [2]string{"Range", "bytes=4-9"})
	if rr.Code != http.StatusPartialContent {
		t.Fatalf("Range = %d, want 206 (%s)", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); got != "456789" {
		t.Errorf("ranged body = %q, want %q", got, "456789")
	}
	if cr := rr.Header().Get("Content-Range"); cr != "bytes 4-9/20" {
		t.Errorf("Content-Range = %q, want bytes 4-9/20", cr)
	}

	// A suffix range is resolved against the real size.
	rr = a.get("/api/documents/"+d.ID+"/raw", [2]string{"Range", "bytes=-4"})
	if rr.Code != http.StatusPartialContent || rr.Body.String() != "ghij" {
		t.Errorf("suffix range = %d %q, want 206 \"ghij\"", rr.Code, rr.Body.String())
	}

	// Outside the object: 416, not a truncated 206.
	rr = a.get("/api/documents/"+d.ID+"/raw", [2]string{"Range", "bytes=999-1200"})
	if rr.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("out-of-bounds range = %d, want 416", rr.Code)
	}
	if cr := rr.Header().Get("Content-Range"); cr != "bytes */20" {
		t.Errorf("416 Content-Range = %q, want bytes */20", cr)
	}
}

// Every content route answers HEAD as well as GET, so a client can check size, type
// and validator without pulling the object. chi has no HEAD→GET fallback: without
// the explicit registration this is a 405 and the handler's HEAD branch is dead code.
func TestHTTP_ContentRoutesAnswerHEADWithoutABody(t *testing.T) {
	a := newAPI(t, "editor")
	d := a.uploadOK("data.txt", []byte("0123456789abcdefghij"), nil)

	for _, path := range []string{"/raw", "/download"} {
		rr := a.do(http.MethodHead, "/api/documents/"+d.ID+path, "")
		if rr.Code != http.StatusOK {
			t.Errorf("HEAD %s = %d, want 200", path, rr.Code)
			continue
		}
		if got := rr.Header().Get("Content-Length"); got != "20" {
			t.Errorf("HEAD %s Content-Length = %q, want 20", path, got)
		}
		if got := rr.Header().Get("ETag"); got != `"`+d.Checksum+`"` {
			t.Errorf("HEAD %s ETag = %q, want the checksum", path, got)
		}
		if rr.Body.Len() != 0 {
			t.Errorf("HEAD %s wrote %d body bytes, want none", path, rr.Body.Len())
		}
	}
}

// The content URLs are permanent and their bytes immutable, so a cached response is
// never revalidated. An ERROR must therefore never inherit the one-year `immutable`
// lifetime: a single storage blip would break that document in that browser for a
// year, with no URL change to escape it.
func TestHTTP_ErrorResponsesCarryNoImmutableCacheHeaders(t *testing.T) {
	a := newAPI(t, "editor")
	d := a.uploadOK("data.txt", []byte("0123456789"), nil)

	rr := a.get("/api/documents/"+d.ID+"/raw", [2]string{"Range", "bytes=999-1200"})
	if rr.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("out-of-bounds range = %d, want 416", rr.Code)
	}
	assertUncacheable(t, rr, "416")

	// A dangling row: the object vanished under a live row, which is what a storage
	// outage or a half-finished purge looks like from here.
	if err := a.blob.Delete(context.Background(), "documents/"+d.ID+"/original"); err != nil {
		t.Fatalf("delete object: %v", err)
	}
	rr = a.get("/api/documents/" + d.ID + "/raw")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("dangling row = %d, want 404 (%s)", rr.Code, rr.Body.String())
	}
	assertUncacheable(t, rr, "404")
}

func assertUncacheable(t *testing.T, rr *httptest.ResponseRecorder, what string) {
	t.Helper()
	if cc := rr.Header().Get("Cache-Control"); cc != "" {
		t.Errorf("%s carries Cache-Control %q — a cached error on a permanent URL never recovers", what, cc)
	}
	if et := rr.Header().Get("ETag"); et != "" {
		t.Errorf("%s carries ETag %q, so the browser would revalidate against a validator for content it never got", what, et)
	}
}

func TestHTTP_PermanentURLSurvivesRenameAndMove(t *testing.T) {
	a := newAPI(t, "editor")
	d := a.uploadOK("smlouva.pdf", pdfBytes(), nil)
	rawURL := "/api/documents/" + d.ID + "/raw"

	before := a.get(rawURL)
	if before.Code != http.StatusOK {
		t.Fatalf("raw before = %d", before.Code)
	}

	if rr := a.do(http.MethodPatch, "/api/documents/"+d.ID, `{"title":"Přejmenováno"}`); rr.Code != http.StatusOK {
		t.Fatalf("rename = %d (%s)", rr.Code, rr.Body.String())
	}
	folderRR := a.do(http.MethodPost, "/api/documents/folders", `{"name":"Smlouvy"}`)
	if folderRR.Code != http.StatusCreated {
		t.Fatalf("create folder = %d", folderRR.Code)
	}
	var folder documents.DocFolderDetail
	if err := json.Unmarshal(folderRR.Body.Bytes(), &folder); err != nil {
		t.Fatalf("decode folder: %v", err)
	}
	if rr := a.do(http.MethodPost, "/api/documents/"+d.ID+"/move",
		`{"folder_id":"`+folder.ID+`","position":"m"}`); rr.Code != http.StatusOK {
		t.Fatalf("move = %d (%s)", rr.Code, rr.Body.String())
	}

	after := a.get(rawURL)
	if after.Code != http.StatusOK {
		t.Fatalf("the permanent URL broke after a rename+move: %d", after.Code)
	}
	if after.Header().Get("ETag") != before.Header().Get("ETag") {
		t.Error("the ETag changed although the bytes did not")
	}
	// The slug path, by contrast, is not permanent: the old one is gone.
	if rr := a.get("/api/documents/resolve?path=smlouva"); rr.Code != http.StatusNotFound {
		t.Errorf("stale slug path = %d, want 404 (no redirects, D32)", rr.Code)
	}
	if rr := a.get("/api/documents/resolve?path=smlouvy/prejmenovano"); rr.Code != http.StatusOK {
		t.Errorf("new slug path = %d, want 200 (%s)", rr.Code, rr.Body.String())
	}
}

// A soft delete takes the permanent URL with it. The bytes and the row survive (it
// is reversible), but every read stops: a link shared before the delete must not go
// on serving the document as if nothing had happened, and the detail endpoint must
// not hand the viewer a live-looking document whose every action then 404s.
func TestHTTP_SoftDeleteEndsEveryReadAndRestoringBringsThemBack(t *testing.T) {
	a := newAPI(t, "editor")
	d := a.uploadOK("smlouva.pdf", pdfBytes(), nil)
	paths := []string{"", "/raw", "/download", "/preview", "/thumbnail"}

	if rr := a.do(http.MethodDelete, "/api/documents/"+d.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("soft delete = %d (%s)", rr.Code, rr.Body.String())
	}
	for _, p := range paths {
		if rr := a.get("/api/documents/" + d.ID + p); rr.Code != http.StatusNotFound {
			t.Errorf("GET %s on a soft-deleted document = %d, want 404", p, rr.Code)
		}
	}
	// The object itself is untouched — nothing about this is a purge.
	if _, err := a.blob.Stat(context.Background(), "documents/"+d.ID+"/original"); err != nil {
		t.Errorf("a soft delete must leave the bytes alone: %v", err)
	}

	if rr := a.do(http.MethodPatch, "/api/documents/"+d.ID, `{"archived":false}`); rr.Code != http.StatusOK {
		t.Fatalf("restore = %d (%s)", rr.Code, rr.Body.String())
	}
	// /thumbnail stays a 404 here (none was generated for this PDF), so it is left out.
	for _, p := range []string{"", "/raw", "/download", "/preview"} {
		if rr := a.get("/api/documents/" + d.ID + p); rr.Code != http.StatusOK {
			t.Errorf("GET %s after a restore = %d, want 200", p, rr.Code)
		}
	}
}

func TestHTTP_PreviewStates(t *testing.T) {
	a := newAPI(t, "editor")

	// native: the original IS the preview.
	pdf := a.uploadOK("smlouva.pdf", pdfBytes(), nil)
	if rr := a.get("/api/documents/" + pdf.ID + "/preview"); rr.Code != http.StatusOK {
		t.Errorf("native preview = %d, want 200", rr.Code)
	}
	// A PDF preview relaxes the sandbox just enough for the browser's viewer to run
	// and for Chrome for Android's placeholder to hand the file to the platform viewer
	// (allow-downloads — without it that "Open" button is dead), with no
	// allow-same-origin: the frame stays origin-opaque.
	rr := a.get("/api/documents/" + pdf.ID + "/preview")
	csp := rr.Header().Get("Content-Security-Policy")
	if csp != "sandbox allow-scripts allow-downloads" {
		t.Errorf("pdf preview CSP = %q, want \"sandbox allow-scripts allow-downloads\"", csp)
	}
	if strings.Contains(csp, "allow-same-origin") {
		t.Error("a preview must never be granted same-origin access")
	}

	// pending: an Office file whose conversion has not run yet.
	docx := a.uploadOK("Podmínky.docx", zipBytes(), nil)
	if rr := a.get("/api/documents/" + docx.ID + "/preview"); rr.Code != http.StatusConflict {
		t.Errorf("pending preview = %d, want 409", rr.Code)
	}

	// download-only: nothing to preview, which is a normal state, not an error.
	bin := a.uploadOK("video.mov", append([]byte{0x00, 0x00, 0x00, 0x14, 'f', 't', 'y', 'p'}, bytes.Repeat([]byte{1}, 32)...), nil)
	if rr := a.get("/api/documents/" + bin.ID + "/preview"); rr.Code != http.StatusNoContent {
		t.Errorf("download-only preview = %d, want 204", rr.Code)
	}

	// No thumbnail generated yet → 404, and the UI falls back to a type icon.
	if rr := a.get("/api/documents/" + pdf.ID + "/thumbnail"); rr.Code != http.StatusNotFound {
		t.Errorf("missing thumbnail = %d, want 404", rr.Code)
	}
}

func TestHTTP_ContentEndpointsRequireASession(t *testing.T) {
	// No session middleware actor at all: every content endpoint must refuse. This is
	// the household-only guarantee (D33) — a permanent link is useless to a stranger.
	db := testsupport.NewDB(t)
	store, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	svc := documents.NewService(db, audit.NewSink(), nil, store, documents.Options{
		MaxUploadBytes: 4096, TempDir: t.TempDir(),
	}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	h := documents.NewHandler(svc)
	handler := httpx.NewRouter(httpx.Deps{
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		DB:        db,
		Site:      "home",
		SessionMW: auth.NewSessionAuth(auth.Config{}), // no bypass actor, no session store
		MountAPI:  func(r chi.Router) { h.Mount(r) },
	})

	for _, path := range []string{
		"/api/documents/x/raw", "/api/documents/x/download",
		"/api/documents/x/preview", "/api/documents/x/thumbnail",
		"/api/documents/tree", "/api/documents/resolve?path=a",
	} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated GET %s = %d, want 401", path, rr.Code)
		}
	}
}

// ---- role gating at the router (D47's narrow reader exception) ----

func TestHTTP_ReaderCanReadEverythingAndWriteOnlyAPersonalPin(t *testing.T) {
	// Seed as an editor, then re-mount the same database as a reader.
	editor := newAPI(t, "editor")
	d := editor.uploadOK("smlouva.pdf", pdfBytes(), nil)

	reader := &api{t: t, svc: editor.svc, blob: editor.blob}
	reader.handler = httpx.NewRouter(httpx.Deps{
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Site:      "home",
		SessionMW: auth.NewSessionAuth(auth.Config{BypassActor: &reqctx.Actor{UserID: "u-reader", Type: "user", Roles: []string{"reader"}}}),
		MountAPI:  func(r chi.Router) { documents.NewHandler(editor.svc).Mount(r) },
	})

	// Reads: all 200.
	for _, path := range []string{
		"/api/documents", "/api/documents/tree", "/api/documents/" + d.ID,
		"/api/documents/" + d.ID + "/raw", "/api/documents/" + d.ID + "/download",
		"/api/documents/" + d.ID + "/preview",
	} {
		if rr := reader.get(path); rr.Code != http.StatusOK {
			t.Errorf("reader GET %s = %d, want 200 (%s)", path, rr.Code, rr.Body.String())
		}
	}

	// Writes: all 403 …
	cases := []struct{ method, path, body string }{
		{http.MethodPatch, "/api/documents/" + d.ID, `{"title":"nope"}`},
		{http.MethodDelete, "/api/documents/" + d.ID, ""},
		{http.MethodPost, "/api/documents/" + d.ID + "/move", `{"position":"m"}`},
		{http.MethodPost, "/api/documents/folders", `{"name":"nope"}`},
	}
	for _, c := range cases {
		if rr := reader.do(c.method, c.path, c.body); rr.Code != http.StatusForbidden {
			t.Errorf("reader %s %s = %d, want 403", c.method, c.path, rr.Code)
		}
	}
	// … except a PERSONAL pin, the one documents write a reader may make.
	if rr := reader.do(http.MethodPost, "/api/documents/"+d.ID+"/pin", `{"scope":"personal"}`); rr.Code != http.StatusOK {
		t.Errorf("reader personal pin = %d, want 200 (%s)", rr.Code, rr.Body.String())
	}
	if rr := reader.do(http.MethodPost, "/api/documents/"+d.ID+"/pin", `{"scope":"household"}`); rr.Code != http.StatusForbidden {
		t.Errorf("reader household pin = %d, want 403", rr.Code)
	}
	if rr := reader.do(http.MethodDelete, "/api/documents/"+d.ID+"/pin?scope=personal", ""); rr.Code != http.StatusOK {
		t.Errorf("reader personal unpin = %d, want 200", rr.Code)
	}
}

func TestHTTP_HardDeleteRequiresAdmin(t *testing.T) {
	editor := newAPI(t, "editor")
	d := editor.uploadOK("smlouva.pdf", pdfBytes(), nil)

	if rr := editor.do(http.MethodDelete, "/api/documents/"+d.ID+"?hard=true", ""); rr.Code != http.StatusForbidden {
		t.Errorf("editor hard delete = %d, want 403", rr.Code)
	}
	// A soft delete is fine for an editor.
	if rr := editor.do(http.MethodDelete, "/api/documents/"+d.ID, ""); rr.Code != http.StatusNoContent {
		t.Errorf("editor soft delete = %d, want 204", rr.Code)
	}
}

// ---- routing order ----

func TestHTTP_StaticRoutesWinOverTheIDParameter(t *testing.T) {
	a := newAPI(t, "editor")
	// "tree", "resolve" and "folders" must never be parsed as a document id.
	if rr := a.get("/api/documents/tree"); rr.Code != http.StatusOK {
		t.Errorf("GET /api/documents/tree = %d, want 200", rr.Code)
	}
	if rr := a.get("/api/documents/resolve?path=nic"); rr.Code != http.StatusNotFound {
		// 404 from the resolver itself (an unmatched path), not from a document lookup.
		if rr.Code != http.StatusNotFound {
			t.Errorf("GET /api/documents/resolve = %d, want the resolver's 404", rr.Code)
		}
	}
	rr := a.do(http.MethodPost, "/api/documents/folders", `{"name":"Auto"}`)
	if rr.Code != http.StatusCreated {
		t.Errorf("POST /api/documents/folders = %d, want 201 (%s)", rr.Code, rr.Body.String())
	}
}

func TestHTTP_ListFolderRootSentinel(t *testing.T) {
	a := newAPI(t, "editor")
	folderRR := a.do(http.MethodPost, "/api/documents/folders", `{"name":"Auto"}`)
	var folder documents.DocFolderDetail
	if err := json.Unmarshal(folderRR.Body.Bytes(), &folder); err != nil {
		t.Fatalf("decode folder: %v", err)
	}
	a.uploadOK("v-korenu.pdf", pdfBytes(), nil)
	a.uploadOK("ve-slozce.pdf", pdfBytes(), map[string]string{"folder_id": folder.ID})

	var page documents.DocumentPage
	rr := a.get("/api/documents?folder_id=root")
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Title != "v-korenu" {
		t.Errorf("?folder_id=root returned %d items (%+v), want just the unfiled one", len(page.Items), page.Items)
	}
}
