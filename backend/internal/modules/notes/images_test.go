package notes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/notes"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// A byte slice that sniffs as image/png: http.DetectContentType keys off the 8-byte
// PNG signature, so the trailing payload is arbitrary.
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), []byte("tiny-png-payload")...)

func discardLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// ---- service-level image harness (small cap, immediate GC) ----

type imgH struct {
	t    *testing.T
	svc  *notes.Service
	blob blobstore.BlobStore
}

func newImgH(t *testing.T) *imgH {
	t.Helper()
	db := testsupport.NewDB(t)
	blob, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	svc := notes.NewService(db, audit.NewSink(), nil, blob,
		notes.ImageOptions{MaxUploadBytes: 4096, TempDir: t.TempDir()}, // GCGrace 0 = immediate GC
		discardLogger())
	return &imgH{t: t, svc: svc, blob: blob}
}

func (x *imgH) noteID(ctx context.Context, title, body string) string {
	x.t.Helper()
	n, err := x.svc.CreateNote(ctx, notes.NoteCreate{Title: title, BodyMD: body})
	if err != nil {
		x.t.Fatalf("create note: %v", err)
	}
	return n.ID
}

func (x *imgH) objectExists(key string) bool {
	x.t.Helper()
	_, err := x.blob.Stat(context.Background(), key)
	if err == nil {
		return true
	}
	if errors.Is(err, blobstore.ErrNotFound) {
		return false
	}
	x.t.Fatalf("stat %s: %v", key, err)
	return false
}

func TestUploadImageStoresObjectAndRow(t *testing.T) {
	x := newImgH(t)
	ctx := editorCtx()
	id := x.noteID(ctx, "Recepty", "")

	res, err := x.svc.UploadImage(ctx, id, bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if res.URL != "/api/notes/images/"+res.ID {
		t.Fatalf("url: got %q want /api/notes/images/%s", res.URL, res.ID)
	}
	if res.ContentType != "image/png" {
		t.Fatalf("content type: got %q want image/png", res.ContentType)
	}
	if res.ByteSize != int64(len(pngBytes)) {
		t.Fatalf("size: got %d want %d", res.ByteSize, len(pngBytes))
	}
	if !x.objectExists(notes.NoteImageKey(res.ID)) {
		t.Fatal("object was not stored")
	}
}

func TestUploadImageOverCapIs413(t *testing.T) {
	x := newImgH(t)
	ctx := editorCtx()
	id := x.noteID(ctx, "Recepty", "")

	big := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte("A"), 5000)...) // > 4096 cap
	_, err := x.svc.UploadImage(ctx, id, bytes.NewReader(big))
	if got := status(t, err); got != http.StatusRequestEntityTooLarge {
		t.Fatalf("status: got %d want 413", got)
	}
}

func TestUploadImageNonImageIs415(t *testing.T) {
	x := newImgH(t)
	ctx := editorCtx()
	id := x.noteID(ctx, "Recepty", "")

	_, err := x.svc.UploadImage(ctx, id, bytes.NewReader([]byte("just some plain text, not an image")))
	if got := status(t, err); got != http.StatusUnsupportedMediaType {
		t.Fatalf("status: got %d want 415", got)
	}
}

func TestUploadImageToMissingOrArchivedNoteIs404(t *testing.T) {
	x := newImgH(t)
	ctx := editorCtx()

	_, err := x.svc.UploadImage(ctx, "no-such-note", bytes.NewReader(pngBytes))
	if got := status(t, err); got != http.StatusNotFound {
		t.Fatalf("missing note: got %d want 404", got)
	}

	id := x.noteID(ctx, "Recepty", "")
	if err := x.svc.DeleteNote(ctx, id, false); err != nil { // soft-archive
		t.Fatalf("archive: %v", err)
	}
	_, err = x.svc.UploadImage(ctx, id, bytes.NewReader(pngBytes))
	if got := status(t, err); got != http.StatusNotFound {
		t.Fatalf("archived note: got %d want 404", got)
	}
}

func TestNoteBodyRejectsLargeInlineDataImage(t *testing.T) {
	x := newImgH(t)
	ctx := editorCtx()

	// A real inlined image is (tens of) KB of base64 and up — over the length cap it is
	// rejected: it would blow the API body cap and freeze the editor (the bug the whole
	// feature fixes).
	big := "before ![](data:image/png;base64," + strings.Repeat("A", 5000) + ") after"
	_, err := x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Inspirace", BodyMD: big})
	if got := status(t, err); got != http.StatusUnprocessableEntity {
		t.Fatalf("create: got %d want 422", got)
	}

	id := x.noteID(ctx, "Inspirace", "clean")
	_, err = x.svc.UpdateNote(ctx, id, notes.NoteUpdate{BodyMD: &big}, "")
	if got := status(t, err); got != http.StatusUnprocessableEntity {
		t.Fatalf("update: got %d want 422", got)
	}
}

func TestNoteBodyRejectsBalancedParenInlineDataImage(t *testing.T) {
	// Regression: a markdown image whose data: URI carries BALANCED inner parens (valid
	// per CommonMark's unwrapped destination — SVG path data is full of them) must be
	// measured WHOLE. A naive `[^)]+` stops at the first inner `)`, measuring a handful of
	// chars as "short" and letting a multi-KB inline image slip past the cap.
	x := newImgH(t)
	ctx := editorCtx()

	// ~6 KB of balanced-paren payload — well over the 4 KB cap, but every stretch of it
	// is broken up by `)` that would truncate a first-`)` match.
	svg := "data:image/svg+xml,<svg><path d=\"" + strings.Repeat("M(0 0)L(9 9)", 500) + "\"/></svg>"
	body := "before ![](" + svg + ") after"
	_, err := x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Kresba", BodyMD: body})
	if got := status(t, err); got != http.StatusUnprocessableEntity {
		t.Fatalf("balanced-paren inline data image: got %d want 422", got)
	}
}

func TestNoteBodyAllowsShortQuotedDataImage(t *testing.T) {
	// A short data:image snippet a user legitimately quotes or documents (e.g. a tiny
	// placeholder) must NOT be rejected — the guard targets megabyte inlined images,
	// not the mere presence of the data:image syntax.
	x := newImgH(t)
	ctx := editorCtx()
	snippet := "Placeholder: `![](data:image/png;base64,AAAABBBB)` — replace before publishing."
	if _, err := x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Dokumentace", BodyMD: snippet}); err != nil {
		t.Fatalf("short data:image snippet was wrongly rejected: %v", err)
	}
}

func TestNoteBodyRejectsWrappedInlineDataImage(t *testing.T) {
	// Regression: the size guard must measure the FULL data:image URI even when it
	// contains whitespace. A line-wrapped (MIME-style) base64 image embedded as a raw-
	// HTML <img> once slipped through because the old regex stopped at the first newline,
	// measuring only the first line as "short". The bytes still blow the API body cap.
	x := newImgH(t)
	ctx := editorCtx()

	wrapped := "\x89PNG\r\n"
	for i := 0; i < 200; i++ { // ~200 * 40 = 8000 chars of base64, newline-wrapped
		wrapped += strings.Repeat("A", 40) + "\n"
	}
	body := `<img alt="x" src="data:image/png;base64,` + wrapped + `">`
	if _, err := x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Vlepeno", BodyMD: body}); status(t, err) != http.StatusUnprocessableEntity {
		t.Fatalf("wrapped inline HTML image: got %d want 422", status(t, err))
	}

	// The same for the markdown image form on a single long line.
	mdBody := "text ![](data:image/png;base64," + strings.Repeat("A", 5000) + ") more"
	if _, err := x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Vlepeno2", BodyMD: mdBody}); status(t, err) != http.StatusUnprocessableEntity {
		t.Fatalf("oversized markdown image: got %d want 422", status(t, err))
	}
}

func TestNoteBodyAllowsUnterminatedDataImagePrefix(t *testing.T) {
	// Regression: an UNTERMINATED `![](data:image/…` used to capture everything up to the
	// next `)` anywhere below it, so a note whose prose after such a line exceeded the cap
	// was rejected on every single save — permanently unsaveable on text that is not an
	// image at all. The guard now requires the closing terminator; nothing is given up,
	// since an unterminated URI renders as literal text and httpx's body cap still bounds
	// the size.
	x := newImgH(t)
	ctx := editorCtx()

	body := "![](data:image/svg+xml,<svg xmlns=\"http://www.w3.org/2000/svg\"\n\n" +
		strings.Repeat("Poznámka k tomu, jak se to kreslí. ", 200) // ~7000 chars of prose, no `)`
	n, err := x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Kresba", BodyMD: body})
	if err != nil {
		t.Fatalf("unterminated data:image prefix was wrongly rejected: %v", err)
	}

	// And the note stays editable — the wedge was that every subsequent save 422'd too.
	if _, err := x.svc.UpdateNote(ctx, n.ID, notes.NoteUpdate{BodyMD: &body}, ""); err != nil {
		t.Fatalf("update with an unterminated data:image prefix was wrongly rejected: %v", err)
	}
}

func TestGCRemovesImageWhenReferenceDropped(t *testing.T) {
	x := newImgH(t)
	ctx := editorCtx()
	id := x.noteID(ctx, "Recepty", "start")

	res, err := x.svc.UploadImage(ctx, id, bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	key := notes.NoteImageKey(res.ID)

	// Reference it: GC must keep a referenced image.
	withRef := "text\n\n![](" + res.URL + ")"
	if _, err := x.svc.UpdateNote(ctx, id, notes.NoteUpdate{BodyMD: &withRef}, ""); err != nil {
		t.Fatalf("update with ref: %v", err)
	}
	if !x.objectExists(key) {
		t.Fatal("referenced image was wrongly GC'd")
	}

	// Drop the reference: GC must remove it (grace is 0 in this harness).
	noRef := "text only now"
	if _, err := x.svc.UpdateNote(ctx, id, notes.NoteUpdate{BodyMD: &noRef}, ""); err != nil {
		t.Fatalf("update without ref: %v", err)
	}
	if x.objectExists(key) {
		t.Fatal("unreferenced image was not GC'd")
	}
}

func TestGCSparesImageReferencedByAnotherNote(t *testing.T) {
	// An image OWNED by note A but also embedded in note B (content copied between
	// notes) must survive A dropping its reference — deleting it would dangle B.
	x := newImgH(t)
	ctx := editorCtx()
	a := x.noteID(ctx, "Recepty A", "start")
	b := x.noteID(ctx, "Recepty B", "start")

	res, err := x.svc.UploadImage(ctx, a, bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	key := notes.NoteImageKey(res.ID)
	embed := "![](" + res.URL + ")"

	if _, err := x.svc.UpdateNote(ctx, a, notes.NoteUpdate{BodyMD: &embed}, ""); err != nil {
		t.Fatalf("embed in A: %v", err)
	}
	if _, err := x.svc.UpdateNote(ctx, b, notes.NoteUpdate{BodyMD: &embed}, ""); err != nil {
		t.Fatalf("embed in B: %v", err)
	}

	// A drops its reference: B still uses it, so GC must NOT delete the object.
	noRef := "no image here"
	if _, err := x.svc.UpdateNote(ctx, a, notes.NoteUpdate{BodyMD: &noRef}, ""); err != nil {
		t.Fatalf("drop ref in A: %v", err)
	}
	if !x.objectExists(key) {
		t.Fatal("image referenced by note B was wrongly GC'd when its owner A dropped it")
	}

	// B drops it too: now nothing references it, GC reclaims it.
	if _, err := x.svc.UpdateNote(ctx, b, notes.NoteUpdate{BodyMD: &noRef}, ""); err != nil {
		t.Fatalf("drop ref in B: %v", err)
	}
	if x.objectExists(key) {
		t.Fatal("unreferenced image was not GC'd after the last note dropped it")
	}
}

func TestHardDeleteSparesImageReferencedByAnotherNote(t *testing.T) {
	// Hard-deleting the owning note must not purge an image another note still
	// references: it is re-parented so the shared object survives the cascade.
	x := newImgH(t)
	ctx := editorCtx()
	a := x.noteID(ctx, "Recepty A", "start")
	b := x.noteID(ctx, "Recepty B", "start")

	res, err := x.svc.UploadImage(ctx, a, bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	key := notes.NoteImageKey(res.ID)
	embed := "![](" + res.URL + ")"
	if _, err := x.svc.UpdateNote(ctx, a, notes.NoteUpdate{BodyMD: &embed}, ""); err != nil {
		t.Fatalf("embed in A: %v", err)
	}
	if _, err := x.svc.UpdateNote(ctx, b, notes.NoteUpdate{BodyMD: &embed}, ""); err != nil {
		t.Fatalf("embed in B: %v", err)
	}

	if err := x.svc.DeleteNote(ctx, a, true); err != nil { // hard delete the owner
		t.Fatalf("hard delete A: %v", err)
	}
	if !x.objectExists(key) {
		t.Fatal("image still referenced by note B was purged when its owner A was hard-deleted")
	}

	// The row was re-parented to B (not cascaded away): dropping B's reference now
	// GCs it, which only works if B owns the row.
	noRef := "no image here"
	if _, err := x.svc.UpdateNote(ctx, b, notes.NoteUpdate{BodyMD: &noRef}, ""); err != nil {
		t.Fatalf("drop ref in B: %v", err)
	}
	if x.objectExists(key) {
		t.Fatal("re-parented image was not GC'd after B dropped its reference (row not reassigned to B?)")
	}
}

func TestHardDeleteReclaimsLastCrossNoteReference(t *testing.T) {
	// Note A owns image X; B copies it in; A then drops its own reference (X survives
	// because B still uses it). Hard-deleting B removes the LAST reference to X — even
	// though B never owned it — so the row+object must be reclaimed here, not left to leak
	// until the daily sweep.
	x := newImgH(t)
	ctx := editorCtx()
	a := x.noteID(ctx, "Recepty A", "start")
	b := x.noteID(ctx, "Recepty B", "start")

	res, err := x.svc.UploadImage(ctx, a, bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	key := notes.NoteImageKey(res.ID)
	embed := "![](" + res.URL + ")"
	if _, err := x.svc.UpdateNote(ctx, a, notes.NoteUpdate{BodyMD: &embed}, ""); err != nil {
		t.Fatalf("embed in A: %v", err)
	}
	if _, err := x.svc.UpdateNote(ctx, b, notes.NoteUpdate{BodyMD: &embed}, ""); err != nil {
		t.Fatalf("embed in B: %v", err)
	}

	// A drops its reference; B still uses X, so it must survive that edit.
	noRef := "no image here"
	if _, err := x.svc.UpdateNote(ctx, a, notes.NoteUpdate{BodyMD: &noRef}, ""); err != nil {
		t.Fatalf("drop ref in A: %v", err)
	}
	if !x.objectExists(key) {
		t.Fatal("image referenced by B was wrongly reclaimed when its owner A dropped its reference")
	}

	// Hard-delete B, the last note referencing X (which it never owned). X must be reclaimed
	// immediately, not left dangling for the daily sweep.
	if err := x.svc.DeleteNote(ctx, b, true); err != nil {
		t.Fatalf("hard delete B: %v", err)
	}
	if x.objectExists(key) {
		t.Fatal("hard-deleting the last (non-owning) referencer did not reclaim the image")
	}
}

func TestGCGraceSparesFreshImage(t *testing.T) {
	// A dedicated harness with a long grace: an image the body does not (yet) reference
	// must survive, because it may be an in-flight upload.
	db := testsupport.NewDB(t)
	blob, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("blob: %v", err)
	}
	svc := notes.NewService(db, audit.NewSink(), nil, blob,
		notes.ImageOptions{MaxUploadBytes: 4096, TempDir: t.TempDir(), GCGrace: time.Hour}, discardLogger())
	ctx := editorCtx()
	n, err := svc.CreateNote(ctx, notes.NoteCreate{Title: "Recepty", BodyMD: "start"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := svc.UploadImage(ctx, n.ID, bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	body := "edited text, image not embedded yet"
	if _, err := svc.UpdateNote(ctx, n.ID, notes.NoteUpdate{BodyMD: &body}, ""); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := blob.Stat(context.Background(), notes.NoteImageKey(res.ID)); err != nil {
		t.Fatalf("fresh image within grace was GC'd: %v", err)
	}
}

func TestGCUndoWindowSparesAJustDroppedImage(t *testing.T) {
	// Deleting an image in the editor autosaves within a second, and Ctrl+Z puts the
	// reference straight back. Reclaiming on the drop leaves that undo pointing at a
	// permanent 404 — row gone, object gone, and the bucket has no versioning — so a
	// drop only starts the grace clock. The image is reclaimed by a later save, once it
	// has stayed unreferenced for a full window.
	const grace = 60 * time.Millisecond
	db := testsupport.NewDB(t)
	blob, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("blob: %v", err)
	}
	svc := notes.NewService(db, audit.NewSink(), nil, blob,
		notes.ImageOptions{MaxUploadBytes: 4096, TempDir: t.TempDir(), GCGrace: grace}, discardLogger())
	ctx := editorCtx()
	n, err := svc.CreateNote(ctx, notes.NoteCreate{Title: "Recepty", BodyMD: "start"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := svc.UploadImage(ctx, n.ID, bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	key := notes.NoteImageKey(res.ID)
	withRef := "text\n\n![](" + res.URL + ")"
	if _, err := svc.UpdateNote(ctx, n.ID, notes.NoteUpdate{BodyMD: &withRef}, ""); err != nil {
		t.Fatalf("embed: %v", err)
	}

	// Sleep past the upload grace first, so only the undo window can spare it below.
	time.Sleep(2 * grace)
	noRef := "text"
	if _, err := svc.UpdateNote(ctx, n.ID, notes.NoteUpdate{BodyMD: &noRef}, ""); err != nil {
		t.Fatalf("drop ref: %v", err)
	}
	if _, err := blob.Stat(ctx, key); err != nil {
		t.Fatalf("image object was purged on the drop, so an undo could not restore it: %v", err)
	}
	// The ROW is what serves the content URL — without it the restored reference 404s
	// even though the bytes are still in the bucket.
	im, err := svc.Store().GetNoteImage(ctx, db, res.ID)
	if err != nil {
		t.Fatalf("read image row: %v", err)
	}
	if im == nil {
		t.Fatal("image row was deleted on the drop, so an undone reference would 404")
	}

	// Undo inside the window, then drop it again a full window later: the restored
	// reference must earn a FRESH window rather than inherit the by-now expired clock
	// from the first drop.
	if _, err := svc.UpdateNote(ctx, n.ID, notes.NoteUpdate{BodyMD: &withRef}, ""); err != nil {
		t.Fatalf("undo: %v", err)
	}
	time.Sleep(2 * grace)
	if _, err := svc.UpdateNote(ctx, n.ID, notes.NoteUpdate{BodyMD: &noRef}, ""); err != nil {
		t.Fatalf("drop again: %v", err)
	}
	if _, err := blob.Stat(ctx, key); err != nil {
		t.Fatalf("the second drop did not get its own undo window: %v", err)
	}

	// Past the window, the next save does reclaim it.
	time.Sleep(2 * grace)
	edited := "text, edited later"
	if _, err := svc.UpdateNote(ctx, n.ID, notes.NoteUpdate{BodyMD: &edited}, ""); err != nil {
		t.Fatalf("later save: %v", err)
	}
	if _, err := blob.Stat(ctx, key); !errors.Is(err, blobstore.ErrNotFound) {
		t.Fatalf("image unreferenced past the undo window was not reclaimed (stat err %v)", err)
	}
}

func TestHardDeletePurgesImages(t *testing.T) {
	x := newImgH(t)
	ctx := editorCtx()
	id := x.noteID(ctx, "Recepty", "start")

	res, err := x.svc.UploadImage(ctx, id, bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	withRef := "![](" + res.URL + ")"
	if _, err := x.svc.UpdateNote(ctx, id, notes.NoteUpdate{BodyMD: &withRef}, ""); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := x.svc.DeleteNote(ctx, id, true); err != nil { // hard delete
		t.Fatalf("hard delete: %v", err)
	}
	if x.objectExists(notes.NoteImageKey(res.ID)) {
		t.Fatal("image object survived a hard delete")
	}
}

// ---- HTTP harness (serving + auth gating) ----

type imgAPI struct {
	t       *testing.T
	svc     *notes.Service
	blob    blobstore.BlobStore
	handler http.Handler
}

func newImgAPI(t *testing.T, roles ...string) *imgAPI {
	t.Helper()
	if len(roles) == 0 {
		roles = []string{"editor"}
	}
	db := testsupport.NewDB(t)
	blob, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	svc := notes.NewService(db, audit.NewSink(), nil, blob,
		notes.ImageOptions{MaxUploadBytes: 4096, TempDir: t.TempDir()}, discardLogger())
	handler := testsupport.Router(t, db, notes.NewHandler(svc).Mount, roles...)
	return &imgAPI{t: t, svc: svc, blob: blob, handler: handler}
}

func (a *imgAPI) get(path string, headers ...[2]string) *httptest.ResponseRecorder {
	a.t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for _, h := range headers {
		r.Header.Set(h[0], h[1])
	}
	rr := httptest.NewRecorder()
	a.handler.ServeHTTP(rr, r)
	return rr
}

func (a *imgAPI) uploadMultipart(noteID string, body []byte) *httptest.ResponseRecorder {
	a.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "photo.png")
	if err != nil {
		a.t.Fatalf("form file: %v", err)
	}
	if _, err := fw.Write(body); err != nil {
		a.t.Fatalf("write: %v", err)
	}
	if err := mw.Close(); err != nil {
		a.t.Fatalf("close: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/notes/"+noteID+"/images", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	a.handler.ServeHTTP(rr, r)
	return rr
}

func TestServeImageHeadersAndConditional(t *testing.T) {
	a := newImgAPI(t)
	ctx := editorCtx()
	n, err := a.svc.CreateNote(ctx, notes.NoteCreate{Title: "Recepty"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := a.svc.UploadImage(ctx, n.ID, bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	rr := a.get(res.URL)
	if rr.Code != http.StatusOK {
		t.Fatalf("get: got %d want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type: got %q", ct)
	}
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff")
	}
	if rr.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatal("missing sandbox CSP")
	}
	if cc := rr.Header().Get("Cache-Control"); cc == "" {
		t.Fatal("missing cache-control")
	}
	etag := rr.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	if !bytes.Equal(rr.Body.Bytes(), pngBytes) {
		t.Fatal("served bytes differ from stored")
	}

	// Conditional GET with the matching validator → 304.
	rr2 := a.get(res.URL, [2]string{"If-None-Match", etag})
	if rr2.Code != http.StatusNotModified {
		t.Fatalf("conditional get: got %d want 304", rr2.Code)
	}

	// Unknown id → 404.
	rr3 := a.get("/api/notes/images/00000000-0000-7000-8000-000000000000")
	if rr3.Code != http.StatusNotFound {
		t.Fatalf("missing image: got %d want 404", rr3.Code)
	}
}

func TestUploadImageHTTPWriteGate(t *testing.T) {
	// A reader may not upload (RequireWrite).
	reader := newImgAPI(t, "reader")
	ctxR := readerCtx()
	nr, err := reader.svc.CreateNote(ctxR, notes.NoteCreate{Title: "Recepty"})
	// CreateNote is service-level (no role gate), so this succeeds; the HTTP gate is
	// what we're testing.
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rr := reader.uploadMultipart(nr.ID, pngBytes); rr.Code != http.StatusForbidden {
		t.Fatalf("reader upload: got %d want 403", rr.Code)
	}

	// An editor's upload returns 201 with the result JSON.
	ed := newImgAPI(t) // editor
	n, err := ed.svc.CreateNote(editorCtx(), notes.NoteCreate{Title: "Recepty"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rr := ed.uploadMultipart(n.ID, pngBytes)
	if rr.Code != http.StatusCreated {
		t.Fatalf("editor upload: got %d want 201 (body %s)", rr.Code, rr.Body.String())
	}
	var out notes.NoteImageUploadResult
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.URL != "/api/notes/images/"+out.ID {
		t.Fatalf("url: got %q", out.URL)
	}
}

func TestImageMirrorReconciliationIsPrefixScoped(t *testing.T) {
	x := newImgH(t)
	ctx := editorCtx()
	id := x.noteID(ctx, "Recepty", "start")

	// A claimed image (has a row AND a note references it), an orphan under our prefix
	// (no row), and a foreign object under the documents prefix.
	res, err := x.svc.UploadImage(ctx, id, bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	withRef := "![](" + res.URL + ")" // reference it so the unreferenced-row sweep spares it
	if _, err := x.svc.UpdateNote(ctx, id, notes.NoteUpdate{BodyMD: &withRef}, ""); err != nil {
		t.Fatalf("embed ref: %v", err)
	}
	claimedKey := notes.NoteImageKey(res.ID)
	orphanKey := notes.NoteImageKey("00000000-0000-7000-8000-00000000dead")
	if err := x.blob.Put(ctx, orphanKey, bytes.NewReader(pngBytes), int64(len(pngBytes)), "image/png"); err != nil {
		t.Fatalf("put orphan: %v", err)
	}
	const foreignKey = "documents/some-doc/original"
	if err := x.blob.Put(ctx, foreignKey, bytes.NewReader(pngBytes), int64(len(pngBytes)), "image/png"); err != nil {
		t.Fatalf("put foreign: %v", err)
	}

	// A tiny grace + a short sleep so the just-written orphan is past the cutoff.
	time.Sleep(10 * time.Millisecond)
	job := notes.NewImageMirrorJob(x.svc.Store(), x.blob, notes.ImageMirrorConfig{
		OrphanGrace:    time.Millisecond,
		MaxOrphanShare: 1,
		Logger:         discardLogger(),
	})
	report := job.RunOnce(ctx)
	if report.OrphansDeleted != 1 {
		t.Fatalf("orphans deleted: got %d want 1", report.OrphansDeleted)
	}
	if x.objectExists(orphanKey) {
		t.Fatal("orphan under our prefix was not reclaimed")
	}
	if !x.objectExists(claimedKey) {
		t.Fatal("claimed image was wrongly deleted")
	}
	if !x.objectExists(foreignKey) {
		t.Fatal("documents-prefix object was touched by the notes reconciliation")
	}
}

func TestImageMirrorReclaimsUnreferencedRow(t *testing.T) {
	// An image uploaded but whose embedding reference never landed in a saved body (a
	// failed body-save) leaks its row AND object: it is invisible to the orphan-object
	// sweep (the object has a row) and to edit-time GC (which only runs on a body
	// change). The mirror's unreferenced-row sweep reclaims it past the grace window.
	x := newImgH(t)
	ctx := editorCtx()
	id := x.noteID(ctx, "Recepty", "start")

	// One referenced image (must survive) and one unreferenced leak (must be reclaimed).
	kept, err := x.svc.UploadImage(ctx, id, bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("upload kept: %v", err)
	}
	withRef := "![](" + kept.URL + ")"
	if _, err := x.svc.UpdateNote(ctx, id, notes.NoteUpdate{BodyMD: &withRef}, ""); err != nil {
		t.Fatalf("embed kept: %v", err)
	}
	leaked, err := x.svc.UploadImage(ctx, id, bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("upload leaked: %v", err)
	}

	time.Sleep(10 * time.Millisecond) // past the tiny grace below
	job := notes.NewImageMirrorJob(x.svc.Store(), x.blob, notes.ImageMirrorConfig{
		OrphanGrace:    time.Millisecond,
		MaxOrphanShare: 1,
		Logger:         discardLogger(),
	})
	report := job.RunOnce(ctx)
	if report.UnreferencedDeleted != 1 {
		t.Fatalf("unreferenced deleted: got %d want 1", report.UnreferencedDeleted)
	}
	if x.objectExists(notes.NoteImageKey(leaked.ID)) {
		t.Fatal("leaked (unreferenced) image was not reclaimed")
	}
	if !x.objectExists(notes.NoteImageKey(kept.ID)) {
		t.Fatal("referenced image was wrongly reclaimed by the unreferenced-row sweep")
	}
}
