package documents_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/documents"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// ---- harness ----

// h bundles a migrated db + a filesystem object store + the service, so the
// must-helpers can consume a (value, error) service return directly.
type h struct {
	t       *testing.T
	svc     *documents.Service
	db      *sql.DB
	blob    blobstore.BlobStore
	blobDir string    // the FS store's root, so a test can age an object (see backdate)
	events  *[]string // ws pushes, in order
	enqueue *[]string // document ids handed to the preview worker
}

// backdate sets the given objects' modification times to `age` ago.
//
// ⚠ IT EXISTS SO THE ORPHAN GRACE WINDOW IS NEVER A RACE AGAINST THE CLOCK. The
// reconciliation pass deletes an orphan only when its ModTime is at or before
// `now - OrphanGrace`, and the tests that want "past the window" used to get there
// with `OrphanGrace: time.Nanosecond` — which asks whether an object written
// microseconds ago is more than ONE NANOSECOND old. On a platform whose clock
// advances in ~1 ms steps (Windows), a Put and the RunOnce that follows it can land
// on the SAME tick, making ModTime exactly equal to the cutoff, and
// `ModTime.After(cutoff)` false-negative: the object counts as in-flight and the
// assertion fails. It reproduced roughly once in eight runs, only under parallel
// load, which is the worst kind of flake to chase.
//
// Ageing the object instead states the intent the test actually has — "this object
// is old" — and takes the clock out of the assertion entirely.
func (x *h) backdate(age time.Duration, keys ...string) {
	x.t.Helper()
	when := time.Now().Add(-age)
	for _, k := range keys {
		p := filepath.Join(x.blobDir, filepath.FromSlash(k))
		if err := os.Chtimes(p, when, when); err != nil {
			x.t.Fatalf("backdate %s: %v", k, err)
		}
	}
}

func newH(t *testing.T, opts ...func(*documents.Options)) *h {
	t.Helper()
	db := testsupport.NewDB(t)
	blobDir := t.TempDir()
	store, err := blobstore.NewFS(blobDir)
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	var events []string
	var enqueued []string
	o := documents.Options{
		MaxUploadBytes: 1 << 20, // 1 MB keeps the over-cap test cheap
		PreviewEnabled: true,
		TempDir:        t.TempDir(),
	}
	for _, fn := range opts {
		fn(&o)
	}
	notify := func(_ context.Context, typ string, _ any) { events = append(events, typ) }
	svc := documents.NewService(db, audit.NewSink(), notify, store, o,
		slog.New(slog.NewJSONHandler(io.Discard, nil)))
	svc.SetPreviewEnqueue(func(id string) { enqueued = append(enqueued, id) })
	return &h{t: t, svc: svc, db: db, blob: store, blobDir: blobDir, events: &events, enqueue: &enqueued}
}

func (x *h) doc(d *documents.DocumentDetail, err error) *documents.DocumentDetail {
	x.t.Helper()
	if err != nil {
		x.t.Fatalf("document op failed: %v", err)
	}
	return d
}

func (x *h) folder(f *documents.DocFolderDetail, err error) *documents.DocFolderDetail {
	x.t.Helper()
	if err != nil {
		x.t.Fatalf("folder op failed: %v", err)
	}
	return f
}

// upload puts a file through the real upload pipeline.
func (x *h) upload(ctx context.Context, filename string, body []byte, folderID *string) *documents.DocumentDetail {
	x.t.Helper()
	return x.doc(x.svc.Upload(ctx, documents.UploadInput{
		Filename: filename,
		File:     bytes.NewReader(body),
		FolderID: folderID,
	}))
}

func editorCtx() context.Context { return testsupport.CtxUser("u-editor", "editor") }
func readerCtx() context.Context { return testsupport.CtxUser("u-reader", "reader") }
func adminCtx() context.Context  { return testsupport.CtxUser("u-admin", "admin") }

func status(t *testing.T, err error) int {
	t.Helper()
	var ae *httpx.APIError
	if errors.As(err, &ae) {
		return ae.Status
	}
	t.Fatalf("expected *httpx.APIError, got %v", err)
	return 0
}

// pdfBytes is a minimal byte string that sniffs as application/pdf.
func pdfBytes() []byte { return []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\ntrailer\n%%EOF\n") }

// pngBytes is a 1×1 PNG (sniffs as image/png).
var pngBytes = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05,
	0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00,
	0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

// zipBytes fakes an OOXML container: real ZIP magic, so it sniffs as application/zip
// and only the extension can tell a .docx from an archive.
func zipBytes() []byte {
	b := []byte{'P', 'K', 0x03, 0x04}
	return append(b, bytes.Repeat([]byte{0}, 64)...)
}

// ---- upload ordering & immutability (FR-DOC1, D41) ----

func TestUpload_CommitsRowAndAuditAfterTheObjectIsDurable(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	d := x.upload(ctx, "Smlouva ČEZ.pdf", pdfBytes(), nil)

	if d.Title != "Smlouva ČEZ" {
		t.Errorf("title = %q, want the filename without its extension", d.Title)
	}
	if d.Slug != "smlouva-cez" {
		t.Errorf("slug = %q, want a diacritic-folded slug", d.Slug)
	}
	if d.ContentType != "application/pdf" {
		t.Errorf("content_type = %q, want application/pdf", d.ContentType)
	}
	if d.ByteSize != int64(len(pdfBytes())) {
		t.Errorf("byte_size = %d, want %d", d.ByteSize, len(pdfBytes()))
	}
	if len(d.Checksum) != 64 {
		t.Errorf("checksum = %q, want a SHA-256 hex digest", d.Checksum)
	}
	// The object must exist under the id-based key.
	if _, err := x.blob.Stat(ctx, "documents/"+d.ID+"/original"); err != nil {
		t.Fatalf("the object is not durable at documents/%s/original: %v", d.ID, err)
	}
	// One audit event, in the same transaction, describing what the file is.
	events := auditEvents(t, x.db, "document.create")
	if len(events) != 1 {
		t.Fatalf("document.create events = %d, want 1", len(events))
	}
	changed := auditChangedFields(t, x.db, events[0])
	for _, want := range []string{"title", "slug", "original_filename", "content_type", "byte_size", "checksum"} {
		if !changed[want] {
			t.Errorf("document.create diff is missing %q (have %v)", want, changed)
		}
	}
}

func TestUpload_SniffsTheTypeAndIgnoresTheClientsClaim(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()

	// An .html file claiming to be a PDF must be stored as HTML — which is what makes
	// it download-only rather than something the browser renders in home's origin.
	d := x.upload(ctx, "utok.html", []byte("<!doctype html><script>alert(1)</script>"), nil)
	if !strings.HasPrefix(d.ContentType, "text/html") {
		t.Errorf("content_type = %q, want text/html sniffed from the bytes", d.ContentType)
	}
	if documents.InlineSafe(d.ContentType) {
		t.Error("HTML must never be inline-safe (D48)")
	}

	// A .docx sniffs as application/zip; the extension refines it to the Office type.
	docx := x.upload(ctx, "Podmínky.docx", zipBytes(), nil)
	if docx.ContentType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Errorf("docx content_type = %q, want the OOXML type", docx.ContentType)
	}
	if docx.PreviewKind != "pdf" || docx.PreviewStatus != "pending" {
		t.Errorf("docx preview = %s/%s, want pdf/pending", docx.PreviewKind, docx.PreviewStatus)
	}
	// Only the DOCX has anything to derive: an HTML upload is download-only, so it is
	// never queued for a preview or a thumbnail.
	if len(*x.enqueue) != 1 || (*x.enqueue)[0] != docx.ID {
		t.Errorf("preview enqueues = %v, want just the DOCX (%s)", *x.enqueue, docx.ID)
	}
}

// RTF is the one Office format with no binary signature: `{\rtf1` sniffs as plain
// text, so the ZIP/opaque-container refinement never sees it and only the extension
// can tell it from a .txt. Miss that and the file is stored as text/plain, which
// makes it "natively previewable" — the viewer then renders the raw control words
// as text instead of the document being converted to a PDF.
func TestUpload_RTFIsRecognisedAlthoughItSniffsAsPlainText(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	d := x.upload(ctx, "Dopis.rtf", []byte(`{\rtf1\ansi Dobrý den\par}`), nil)

	if d.ContentType != "application/rtf" {
		t.Errorf("content_type = %q, want application/rtf", d.ContentType)
	}
	if documents.InlineSafe(d.ContentType) {
		t.Error("RTF is not one of the inline-safe types")
	}
	if d.PreviewKind != "pdf" || d.PreviewStatus != "pending" {
		t.Errorf("rtf preview = %s/%s, want pdf/pending (it goes through the converter)",
			d.PreviewKind, d.PreviewStatus)
	}
}

func TestUpload_OverTheCapWritesNothing(t *testing.T) {
	x := newH(t, func(o *documents.Options) { o.MaxUploadBytes = 1024 })
	ctx := editorCtx()

	_, err := x.svc.Upload(ctx, documents.UploadInput{
		Filename: "velky.bin",
		File:     bytes.NewReader(bytes.Repeat([]byte("x"), 4096)),
	})
	if got := status(t, err); got != 413 {
		t.Fatalf("over-cap upload status = %d, want 413", got)
	}
	if n := testsupport.CountRows(t, x.db, "documents"); n != 0 {
		t.Errorf("documents rows = %d, want 0 — nothing may be committed", n)
	}
	objects, _ := x.blob.List(ctx, "documents/")
	if len(objects) != 0 {
		t.Errorf("objects = %v, want none", objects)
	}
}

func TestUpload_StorageFailureYields502AndNoRow(t *testing.T) {
	x := newH(t)
	// Swap in a store whose Put always fails, exactly as an R2 outage would.
	failing := &failingStore{BlobStore: x.blob, failPut: true}
	svc := documents.NewService(x.db, audit.NewSink(), nil, failing, documents.Options{
		MaxUploadBytes: 1 << 20,
		TempDir:        t.TempDir(),
	}, slog.New(slog.NewJSONHandler(io.Discard, nil)))

	_, err := svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "faktura.pdf",
		File:     bytes.NewReader(pdfBytes()),
	})
	if got := status(t, err); got != 502 {
		t.Fatalf("upload with failing storage = %d, want 502", got)
	}
	if n := testsupport.CountRows(t, x.db, "documents"); n != 0 {
		t.Errorf("documents rows = %d, want 0 — a document must never exist without its bytes", n)
	}
	if n := testsupport.CountRows(t, x.db, "audit_events"); n != 0 {
		t.Errorf("audit events = %d, want 0", n)
	}
}

func TestUpload_AllowlistRejectsWithNothingWritten(t *testing.T) {
	x := newH(t, func(o *documents.Options) { o.AllowedMIME = []string{"application/pdf", "image/*"} })
	ctx := editorCtx()

	if _, err := x.svc.Upload(ctx, documents.UploadInput{
		Filename: "poznamka.txt",
		File:     bytes.NewReader([]byte("plain text")),
	}); status(t, err) != 415 {
		t.Errorf("disallowed type should be 415")
	}
	if n := testsupport.CountRows(t, x.db, "documents"); n != 0 {
		t.Errorf("documents rows = %d, want 0", n)
	}
	// The wildcard entry admits images.
	x.upload(ctx, "foto.png", pngBytes, nil)
}

func TestUpload_ReuploadingIdenticalBytesMakesANewDocument(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	first := x.upload(ctx, "smlouva.pdf", pdfBytes(), nil)
	second := x.upload(ctx, "smlouva.pdf", pdfBytes(), nil)

	if first.ID == second.ID {
		t.Fatal("a re-upload must be a NEW document (immutable bytes, D41)")
	}
	if first.Checksum != second.Checksum {
		t.Error("identical bytes should produce identical checksums")
	}
	// Same title in the same parent → the slug is suffixed, not overwritten.
	if second.Slug != first.Slug+"-2" {
		t.Errorf("second slug = %q, want %q", second.Slug, first.Slug+"-2")
	}
	if first.Urls.Raw == second.Urls.Raw {
		t.Error("each document must have its own permanent content URL")
	}
}

func TestUpdate_CannotTouchTheBytes(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	d := x.upload(ctx, "faktura.pdf", pdfBytes(), nil)

	title := "Faktura plyn březen 2026"
	updated := x.doc(x.svc.UpdateDocument(ctx, d.ID, documents.DocumentUpdate{Title: &title}, ""))

	if updated.Checksum != d.Checksum || updated.ByteSize != d.ByteSize || updated.ContentType != d.ContentType {
		t.Error("a metadata PATCH must not change checksum/byte_size/content_type")
	}
	// The navigation slug follows the title; the permanent URL does not move.
	if updated.Slug != "faktura-plyn-brezen-2026" {
		t.Errorf("slug = %q, want it re-derived from the new title", updated.Slug)
	}
	if updated.Urls.Raw != d.Urls.Raw || updated.Urls.Permalink != d.Urls.Permalink {
		t.Error("the permanent id-based URLs must survive a rename (D42)")
	}
	// Metadata-only diffs (D50): nothing about the bytes is ever diffed.
	changed := auditChangedFields(t, x.db, auditEvents(t, x.db, "document.update")[0])
	for _, forbidden := range []string{"checksum", "byte_size", "content_type", "storage_key"} {
		if changed[forbidden] {
			t.Errorf("document.update diffed %q — bytes are immutable and never diffed", forbidden)
		}
	}
}

func TestMove_KeepsThePermanentURLAndRewritesOneRow(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	parent := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Smlouvy"}))
	child := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Energie", ParentID: &parent.ID}))
	d := x.upload(ctx, "smlouva.pdf", pdfBytes(), nil)

	moved := x.doc(x.svc.MoveDocument(ctx, d.ID, documents.DocumentMoveRequest{FolderID: &child.ID, Position: "m"}, ""))
	if moved.Urls.Raw != d.Urls.Raw {
		t.Error("moving must not change the permanent content URL (id-based keys, D42)")
	}
	if moved.SlugPath != "smlouvy/energie/smlouva" {
		t.Errorf("slug_path = %q, want it to follow the new parent", moved.SlugPath)
	}
	// The object stays exactly where it was — a move never touches storage.
	if _, err := x.blob.Stat(ctx, "documents/"+d.ID+"/original"); err != nil {
		t.Errorf("the object moved with the row: %v", err)
	}
}

// ---- slugs, the addressing invariant, resolve (FR-DOC5, D32/D40) ----

func TestSlugs_CrossTableSiblingUniqueness(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()

	// A folder and a document under the same parent may not share a slug: the slug
	// path would be ambiguous.
	f := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Auto"}))
	d := x.upload(ctx, "Auto.pdf", pdfBytes(), nil)
	if d.Slug == f.Slug {
		t.Fatalf("document and folder share the slug %q under the same parent", d.Slug)
	}
	if d.Slug != "auto-2" {
		t.Errorf("document slug = %q, want the suffixed auto-2", d.Slug)
	}

	// Root-level dedupe relies on the COALESCE index (SQLite treats NULLs as distinct).
	second := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Auto"}))
	if second.Slug != "auto-3" {
		t.Errorf("second root folder slug = %q, want auto-3", second.Slug)
	}
}

func TestFolderIcon(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()

	// Created with an icon → round-trips.
	withIcon := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Smlouvy", Icon: "🧾"}))
	if withIcon.Icon != "🧾" {
		t.Fatalf("create icon: got %q want 🧾", withIcon.Icon)
	}
	// Created without an icon → empty (client renders the 📁 default).
	noIcon := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Ostatní"}))
	if noIcon.Icon != "" {
		t.Fatalf("no icon: got %q want empty", noIcon.Icon)
	}

	// Icon-only PATCH updates it and persists (verify by re-reading the detail).
	newIcon := "🚗"
	x.folder(x.svc.UpdateFolder(ctx, withIcon.ID, documents.DocFolderUpdate{Icon: &newIcon}))
	if got := x.folder(x.svc.GetFolderDetail(ctx, withIcon.ID)); got.Icon != "🚗" {
		t.Fatalf("patched icon not persisted: got %q want 🚗", got.Icon)
	}

	// Empty-string PATCH clears it.
	empty := ""
	x.folder(x.svc.UpdateFolder(ctx, withIcon.ID, documents.DocFolderUpdate{Icon: &empty}))
	if cleared := x.folder(x.svc.GetFolderDetail(ctx, withIcon.ID)); cleared.Icon != "" {
		t.Fatalf("clearing icon: got %q want empty", cleared.Icon)
	}
}

func TestResolve_404sAfterRenameWithNoRedirect(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	f := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Návody"}))
	d := x.upload(ctx, "Kotel.pdf", pdfBytes(), &f.ID)

	res, err := x.svc.Resolve(ctx, "navody/kotel", documents.Scope{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Type != "document" || res.ID != d.ID {
		t.Fatalf("resolve = %+v, want the document", res)
	}

	title := "Kotel Protherm"
	x.doc(x.svc.UpdateDocument(ctx, d.ID, documents.DocumentUpdate{Title: &title}, ""))

	// The old slug path is gone — deliberately no redirect table (D32).
	if _, err := x.svc.Resolve(ctx, "navody/kotel", documents.Scope{}); status(t, err) != 404 {
		t.Error("the pre-rename path must 404")
	}
	if res, err := x.svc.Resolve(ctx, "navody/kotel-protherm", documents.Scope{}); err != nil || res.ID != d.ID {
		t.Errorf("the new path should resolve to the same id: %+v %v", res, err)
	}
	// The permanent id-based address is unaffected by all of this.
	if _, err := x.svc.GetDocumentDetail(ctx, d.ID); err != nil {
		t.Errorf("the id-based lookup must still work: %v", err)
	}
}

func TestMoveFolder_CycleGuardRejectsBeforeAnyWrite(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	parent := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Dokumenty"}))
	child := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Auto", ParentID: &parent.ID}))

	_, err := x.svc.MoveFolder(ctx, parent.ID, documents.DocFolderMoveRequest{ParentID: &child.ID, Position: "m"})
	if got := status(t, err); got != 422 {
		t.Fatalf("moving a folder into its descendant = %d, want 422", got)
	}
	// Nothing may have been written: no move audit event, and the parent is unmoved.
	if len(auditEvents(t, x.db, "document_folder.move")) != 0 {
		t.Error("a rejected move must write no audit event")
	}
	after := x.folder(x.svc.GetFolderDetail(ctx, parent.ID))
	if after.ParentID != nil {
		t.Error("the folder moved despite the cycle guard")
	}

	// Into itself, too.
	if _, err := x.svc.MoveFolder(ctx, parent.ID, documents.DocFolderMoveRequest{ParentID: &parent.ID, Position: "m"}); status(t, err) != 422 {
		t.Error("moving a folder into itself must be 422")
	}
}

func TestMoveFolder_RewritesOneRowNotTheSubtree(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	a := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "A"}))
	b := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "B"}))
	sub := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Sub", ParentID: &a.ID}))
	d := x.upload(ctx, "list.pdf", pdfBytes(), &sub.ID)

	beforeSub := x.folder(x.svc.GetFolderDetail(ctx, sub.ID))
	beforeDoc := x.doc(x.svc.GetDocumentDetail(ctx, d.ID))

	x.folder(x.svc.MoveFolder(ctx, a.ID, documents.DocFolderMoveRequest{ParentID: &b.ID, Position: "m"}))

	afterSub := x.folder(x.svc.GetFolderDetail(ctx, sub.ID))
	afterDoc := x.doc(x.svc.GetDocumentDetail(ctx, d.ID))
	if afterSub.UpdatedAt != beforeSub.UpdatedAt || afterSub.Slug != beforeSub.Slug {
		t.Error("a descendant folder was rewritten — only the moved row may change")
	}
	if afterDoc.UpdatedAt != beforeDoc.UpdatedAt {
		t.Error("a descendant document was rewritten — only the moved row may change")
	}
	// The computed path does follow the move, even though no descendant row changed.
	if afterDoc.SlugPath != "b/a/sub/list" {
		t.Errorf("slug_path = %q, want b/a/sub/list", afterDoc.SlugPath)
	}
}

// ---- folder delete (FR-DOC3, D50) ----

func TestDeleteFolder_NonEmptyBlocksThenCascades(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	f := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Zdraví"}))
	sub := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Recepty", ParentID: &f.ID}))
	d := x.upload(ctx, "recept.pdf", pdfBytes(), &sub.ID)

	err := x.svc.DeleteFolder(ctx, f.ID, false, false)
	if got := status(t, err); got != 409 {
		t.Fatalf("non-empty folder delete = %d, want 409", got)
	}
	if !strings.Contains(err.Error(), "1 subfolders") {
		t.Errorf("the 409 should carry the child counts, got %q", err.Error())
	}

	if err := x.svc.DeleteFolder(ctx, f.ID, true, false); err != nil {
		t.Fatalf("cascade delete: %v", err)
	}
	// Soft cascade: rows archived, every child logged, bytes untouched (it's reversible).
	// Read straight from the row — an archived document no longer resolves through any
	// read path, the detail endpoint included.
	if !isArchived(t, x.db, "documents", d.ID) {
		t.Error("the descendant document should be archived")
	}
	if len(auditEvents(t, x.db, "document.delete")) != 1 {
		t.Error("each cascaded document must be logged")
	}
	if len(auditEvents(t, x.db, "document_folder.delete")) != 2 {
		t.Error("both folders in the subtree must be logged")
	}
	if _, err := x.blob.Stat(ctx, "documents/"+d.ID+"/original"); err != nil {
		t.Error("a soft delete must NOT remove the bytes — it is reversible")
	}
}

// The cascade guard applies to the HARD path too — more so, in fact: it is the
// irreversible one, purging rows and their objects with no undo.
func TestDeleteFolder_HardStillRequiresCascadeForANonEmptyFolder(t *testing.T) {
	x := newH(t)
	ctx := adminCtx()
	f := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Staré"}))
	d := x.upload(ctx, "archiv.pdf", pdfBytes(), &f.ID)

	err := x.svc.DeleteFolder(ctx, f.ID, false, true)
	if got := status(t, err); got != 409 {
		t.Fatalf("hard delete of a non-empty folder without cascade = %d, want 409", got)
	}
	if _, err := x.blob.Stat(ctx, "documents/"+d.ID+"/original"); err != nil {
		t.Errorf("the refused purge must leave the bytes alone: %v", err)
	}
	if n := testsupport.CountRows(t, x.db, "documents"); n != 1 {
		t.Errorf("documents rows = %d, want the refused purge to have changed nothing", n)
	}
}

// A folder that is empty only in the TREE: its children were soft-deleted earlier.
// A hard delete purges archived rows and their bytes too, so it is refused —
// cascade=true cannot be consent for rows the tree never displayed.
func TestDeleteFolder_HardIsRefusedWhenTheOnlyChildIsArchived(t *testing.T) {
	x := newH(t)
	ctx := adminCtx()
	f := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Staré"}))
	d := x.upload(ctx, "archiv.pdf", pdfBytes(), &f.ID)
	if err := x.svc.DeleteDocument(ctx, d.ID, false); err != nil {
		t.Fatalf("soft-delete the child: %v", err)
	}

	// Refused both ways: without cascade AND with it. The tree shows this folder as
	// empty, so neither call can be an informed acknowledgement of the archived child.
	for _, cascade := range []bool{false, true} {
		err := x.svc.DeleteFolder(ctx, f.ID, cascade, true)
		if got := status(t, err); got != 409 {
			t.Fatalf("hard delete over an archived child (cascade=%t) = %d, want 409", cascade, got)
		}
		if _, err := x.blob.Stat(ctx, "documents/"+d.ID+"/original"); err != nil {
			t.Errorf("the refused purge must leave the archived document's bytes alone: %v", err)
		}
		if n := testsupport.CountRows(t, x.db, "documents"); n != 1 {
			t.Errorf("documents rows = %d, want the refused purge to have changed nothing", n)
		}
	}

	// A soft delete is reversible, so it keeps counting live children only — a
	// tree-empty folder still archives without an explicit cascade.
	if err := x.svc.DeleteFolder(ctx, f.ID, false, false); err != nil {
		t.Fatalf("soft delete of a tree-empty folder: %v", err)
	}
}

// The dangerous mix: SOME live children (so the UI's confirm counts them and sends
// cascade=true) plus archived ones it could not see. Acknowledging the visible two
// must not license purging the invisible one — including one buried under a live
// subfolder, which the immediate-child counts would never notice.
func TestDeleteFolder_HardCascadeIsRefusedWhenTheSubtreeHidesArchivedRows(t *testing.T) {
	x := newH(t)
	ctx := adminCtx()
	f := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Smlouvy"}))
	sub := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "2024", ParentID: &f.ID}))
	live := x.upload(ctx, "aktualni.pdf", pdfBytes(), &f.ID)
	buried := x.upload(ctx, "stara.pdf", pdfBytes(), &sub.ID)
	if err := x.svc.DeleteDocument(ctx, buried.ID, false); err != nil {
		t.Fatalf("soft-delete the nested child: %v", err)
	}

	err := x.svc.DeleteFolder(ctx, f.ID, true, true)
	if got := status(t, err); got != 409 {
		t.Fatalf("hard cascade over a hidden archived descendant = %d, want 409", got)
	}
	for _, id := range []string{live.ID, buried.ID} {
		if _, err := x.blob.Stat(ctx, "documents/"+id+"/original"); err != nil {
			t.Errorf("the refused purge must leave every object alone (%s): %v", id, err)
		}
	}
	if n := testsupport.CountRows(t, x.db, "documents"); n != 2 {
		t.Errorf("documents rows = %d, want 2 — the refused purge changes nothing", n)
	}
	if n := testsupport.CountRows(t, x.db, "document_folders"); n != 2 {
		t.Errorf("document_folders rows = %d, want 2 — the refused purge changes nothing", n)
	}

	// Once the hidden row is gone for good, the same call goes through: the admin has
	// now dealt with it explicitly, which is exactly what the guard asks for.
	if err := x.svc.DeleteDocument(ctx, buried.ID, true); err != nil {
		t.Fatalf("purge the archived child individually: %v", err)
	}
	if err := x.svc.DeleteFolder(ctx, f.ID, true, true); err != nil {
		t.Fatalf("hard cascade after the archived child was purged: %v", err)
	}
	if n := testsupport.CountRows(t, x.db, "documents"); n != 0 {
		t.Errorf("documents rows = %d, want 0", n)
	}
}

// An archived folder is itself a legitimate hard-delete TARGET — that is what
// purging a soft-deleted folder means. The guard covers descendants, not the target.
func TestDeleteFolder_HardPurgesAnArchivedTarget(t *testing.T) {
	x := newH(t)
	ctx := adminCtx()
	f := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Zrušené"}))
	if err := x.svc.DeleteFolder(ctx, f.ID, false, false); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if err := x.svc.DeleteFolder(ctx, f.ID, false, true); err != nil {
		t.Fatalf("hard delete of an archived folder: %v", err)
	}
	if n := testsupport.CountRows(t, x.db, "document_folders"); n != 0 {
		t.Errorf("document_folders rows = %d, want 0", n)
	}
}

// Purging a folder that was soft-deleted earlier — the whole point of a soft
// delete being reversible is that the irreversible one still has to be REACHABLE.
// Its subtree is archived by construction (the soft cascade archived every
// descendant on the way down), so the hidden-row guard must not apply to it: it
// would refuse this call forever and leave no way to reclaim the bytes at all.
// Consent is taken over the physical subtree instead, which is what cascade means
// here — the live counts are all zero by now and would wave the purge through.
func TestDeleteFolder_HardPurgesAPreviouslySoftDeletedSubtree(t *testing.T) {
	x := newH(t)
	ctx := adminCtx()
	f := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Zrušené"}))
	sub := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Faktury", ParentID: &f.ID}))
	d := x.upload(ctx, "faktura.pdf", pdfBytes(), &sub.ID)

	if err := x.svc.DeleteFolder(ctx, f.ID, true, false); err != nil {
		t.Fatalf("soft cascade delete: %v", err)
	}
	if !isArchived(t, x.db, "documents", d.ID) || !isArchived(t, x.db, "document_folders", sub.ID) {
		t.Fatal("the soft cascade should have archived the whole subtree")
	}

	// Still not without cascade: the subtree is invisible either way, so the flag is
	// the only acknowledgement there can be that files are about to go.
	err := x.svc.DeleteFolder(ctx, f.ID, false, true)
	if got := status(t, err); got != 409 {
		t.Fatalf("purging a soft-deleted subtree without cascade = %d, want 409", got)
	}
	if n := testsupport.CountRows(t, x.db, "documents"); n != 1 {
		t.Errorf("documents rows = %d, want the refused purge to have changed nothing", n)
	}

	if err := x.svc.DeleteFolder(ctx, f.ID, true, true); err != nil {
		t.Fatalf("purging a soft-deleted subtree with cascade: %v", err)
	}
	if n := testsupport.CountRows(t, x.db, "documents"); n != 0 {
		t.Errorf("documents rows = %d, want 0", n)
	}
	if n := testsupport.CountRows(t, x.db, "document_folders"); n != 0 {
		t.Errorf("document_folders rows = %d, want 0", n)
	}
	if _, err := x.blob.Stat(ctx, "documents/"+d.ID+"/original"); !errors.Is(err, blobstore.ErrNotFound) {
		t.Errorf("the purge must take the archived descendant's bytes too, got %v", err)
	}
}

func TestDeleteFolder_HardPurgesDescendantObjects(t *testing.T) {
	x := newH(t)
	ctx := adminCtx()
	f := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Staré"}))
	d := x.upload(ctx, "archiv.pdf", pdfBytes(), &f.ID)

	if err := x.svc.DeleteFolder(ctx, f.ID, true, true); err != nil {
		t.Fatalf("hard delete: %v", err)
	}
	if _, err := x.blob.Stat(ctx, "documents/"+d.ID+"/original"); !errors.Is(err, blobstore.ErrNotFound) {
		t.Errorf("a hard folder delete must purge every descendant's bytes, got %v", err)
	}
	if n := testsupport.CountRows(t, x.db, "documents"); n != 0 {
		t.Errorf("documents rows = %d, want 0", n)
	}
	// Every purged descendant is audited before the FK cascade destroys it.
	if len(auditEvents(t, x.db, "document.delete")) != 1 {
		t.Error("the cascaded document purge must be logged")
	}
}

func TestDeleteDocument_HardPurgesTheObjects(t *testing.T) {
	x := newH(t)
	ctx := adminCtx()
	d := x.upload(ctx, "smazat.pdf", pdfBytes(), nil)

	if err := x.svc.DeleteDocument(ctx, d.ID, true); err != nil {
		t.Fatalf("hard delete: %v", err)
	}
	if _, err := x.blob.Stat(ctx, "documents/"+d.ID+"/original"); !errors.Is(err, blobstore.ErrNotFound) {
		t.Errorf("hard delete must purge the bytes, got %v", err)
	}
}

// An archived folder is out of the tree and unreachable by slug. A stale client that
// renames one anyway would rewrite its name and slug and leave a
// document_folder.update event behind for something nobody can see — the same
// phantom write UpdateDocument refuses, and the same read that GetDocumentDetail
// 404s. Restoring it stays the one mutation that is allowed.
func TestUpdateFolder_ArchivedRejectsRenamesButStillRestores(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	f := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Staré"}))
	if err := x.svc.DeleteFolder(ctx, f.ID, false, false); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	if _, err := x.svc.GetFolderDetail(ctx, f.ID); status(t, err) != 404 {
		t.Errorf("reading an archived folder = %d, want 404", status(t, err))
	}

	name := "Přejmenováno"
	if _, err := x.svc.UpdateFolder(ctx, f.ID, documents.DocFolderUpdate{Name: &name}); status(t, err) != 404 {
		t.Fatalf("renaming an archived folder = %d, want 404", status(t, err))
	}
	var got string
	if err := x.db.QueryRow(`SELECT name FROM document_folders WHERE id = ?`, f.ID).Scan(&got); err != nil {
		t.Fatalf("read the folder name: %v", err)
	}
	if got != "Staré" {
		t.Errorf("folder name = %q, want the rejected rename to have changed nothing", got)
	}
	if n := len(auditEvents(t, x.db, "document_folder.update")); n != 0 {
		t.Errorf("document_folder.update events = %d, want the rejected rename to write none", n)
	}

	// Unarchiving is still allowed, and may carry a new name with it.
	live := false
	restored := x.folder(x.svc.UpdateFolder(ctx, f.ID, documents.DocFolderUpdate{
		Name: &name, Archived: &live,
	}))
	if restored.Archived || restored.Name != name {
		t.Errorf("restore left the folder %+v, want a live folder named %q", restored, name)
	}
}

// ---- pinning (FR-DOC10, D47) ----

func TestPin_HouseholdIsGatedAndAudited_PersonalIsNeither(t *testing.T) {
	x := newH(t)
	ed := editorCtx()
	d := x.upload(ed, "kody.txt", []byte("Wi-Fi: ..."), nil)

	// household: editor+, audited, broadcast.
	if _, err := x.svc.Pin(ed, d.ID, "household", ""); err != nil {
		t.Fatalf("household pin: %v", err)
	}
	if len(auditEvents(t, x.db, "document.pin")) != 1 {
		t.Error("a household pin must be audited")
	}
	// Idempotent: re-pinning the same scope writes nothing more.
	if _, err := x.svc.Pin(ed, d.ID, "household", ""); err != nil {
		t.Fatalf("re-pin: %v", err)
	}
	if n := len(auditEvents(t, x.db, "document.pin")); n != 1 {
		t.Errorf("document.pin events = %d, want 1 — a re-pin is a no-op", n)
	}

	// personal: any member, NOT audited, NOT broadcast.
	rd := readerCtx()
	before := len(*x.events)
	auditBefore := testsupport.CountRows(t, x.db, "audit_events")
	st, err := x.svc.Pin(rd, d.ID, "personal", "")
	if err != nil {
		t.Fatalf("reader personal pin: %v", err)
	}
	if !st.Personal {
		t.Error("the reader's personal pin was not recorded")
	}
	if got := testsupport.CountRows(t, x.db, "audit_events"); got != auditBefore {
		t.Error("a personal pin must not write an audit event (D47)")
	}
	if len(*x.events) != before {
		t.Error("a personal pin must not broadcast")
	}
}

func TestPin_ReaderIsAllowedOnlyThePersonalScope(t *testing.T) {
	x := newH(t)
	d := x.upload(editorCtx(), "info.txt", []byte("text"), nil)
	rd := readerCtx()

	if _, err := x.svc.Pin(rd, d.ID, "household", ""); status(t, err) != 403 {
		t.Error("a reader must not set a household pin")
	}
	if _, err := x.svc.Pin(rd, d.ID, "personal", ""); err != nil {
		t.Errorf("a reader must be able to set a personal pin: %v", err)
	}
	if _, err := x.svc.Unpin(rd, d.ID, "personal", ""); err != nil {
		t.Errorf("a reader must be able to remove their personal pin: %v", err)
	}
	// The household scope is the ONLY gate the service itself enforces — every other
	// documents mutation is gated at the router with httpx.RequireWrite, so the
	// reader-blocked-everywhere-else half of D47 is asserted in http_test.go.
}

func TestPin_PartialIndexesKeepOnePinPerScope(t *testing.T) {
	x := newH(t)
	ed := editorCtx()
	d := x.upload(ed, "a.txt", []byte("a"), nil)

	for i := 0; i < 3; i++ {
		if _, err := x.svc.Pin(ed, d.ID, "household", ""); err != nil {
			t.Fatalf("pin %d: %v", i, err)
		}
		if _, err := x.svc.Pin(ed, d.ID, "personal", ""); err != nil {
			t.Fatalf("personal pin %d: %v", i, err)
		}
	}
	if n := testsupport.CountRows(t, x.db, "document_pins"); n != 2 {
		t.Errorf("document_pins rows = %d, want 2 (one per scope)", n)
	}
}

// ---- the documents.pripnute widget (FR-DOC11) ----

func TestWidget_DedupesWithHouseholdPrecedenceAndOrdersBlocks(t *testing.T) {
	x := newH(t)
	ed := editorCtx()
	f := x.folder(x.svc.CreateFolder(ed, documents.DocFolderCreate{Name: "Smlouvy"}))
	both := x.upload(ed, "cez.pdf", pdfBytes(), &f.ID)
	householdOnly := x.upload(ed, "pojisteni.pdf", pdfBytes(), nil)
	personalOnly := x.upload(ed, "zelena-karta.pdf", pdfBytes(), nil)

	mustPin(t, x.svc, ed, both.ID, "household")
	mustPin(t, x.svc, ed, both.ID, "personal")
	mustPin(t, x.svc, ed, householdOnly.ID, "household")
	mustPin(t, x.svc, ed, personalOnly.ID, "personal")

	data := widgetData(t, x.svc, "u-editor")
	if len(data.Documents) != 3 {
		t.Fatalf("widget rows = %d, want 3 (deduplicated)", len(data.Documents))
	}
	// Household block first, then personal.
	if data.Documents[0].DocumentID != both.ID || data.Documents[0].Scope != "both" {
		t.Errorf("row 0 = %+v, want the doubly-pinned document as scope=both", data.Documents[0])
	}
	if data.Documents[1].Scope != "household" {
		t.Errorf("row 1 scope = %q, want household", data.Documents[1].Scope)
	}
	if last := data.Documents[2]; last.Scope != "personal" || last.DocumentID != personalOnly.ID {
		t.Errorf("row 2 = %+v, want the personal-only document last", last)
	}
	// Rows carry the navigation path plus what the tile needs to render.
	if data.Documents[0].SlugPath != "smlouvy/cez" {
		t.Errorf("slug_path = %q, want smlouvy/cez", data.Documents[0].SlugPath)
	}
	if data.Documents[0].ContentType != "application/pdf" || data.Documents[0].ByteSize == 0 {
		t.Error("widget rows must carry content_type and byte_size")
	}
}

func TestWidget_PersonalPinsAreNotVisibleToOtherUsers(t *testing.T) {
	x := newH(t)
	ed := editorCtx()
	d := x.upload(ed, "moje.pdf", pdfBytes(), nil)
	mustPin(t, x.svc, ed, d.ID, "personal")

	if data := widgetData(t, x.svc, "u-other"); len(data.Documents) != 0 {
		t.Errorf("another user sees %d rows, want 0 — a personal pin is per-user", len(data.Documents))
	}
	// A household pin, by contrast, shows for everyone.
	mustPin(t, x.svc, ed, d.ID, "household")
	if data := widgetData(t, x.svc, "u-other"); len(data.Documents) != 1 {
		t.Errorf("another user sees %d rows after a household pin, want 1", len(data.Documents))
	}
}

// ---- search (FR-DOC7, D46) ----

func TestSearch_MatchesMetadataDiacriticInsensitivelyButNotFileContents(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	x.doc(x.svc.Upload(ctx, documents.UploadInput{
		Filename:    "Smlouva ČEZ.pdf",
		File:        bytes.NewReader(pdfBytes()),
		Description: "Zákaznické číslo v záhlaví",
	}))
	// The body text below must NOT be searchable: contents are not indexed.
	x.upload(ctx, "jiny.txt", []byte("tajne slovo kotelna"), nil)

	for _, q := range []string{"smlouva", "SMLOUVA", "cez", "čez", "zakaznicke", "Smlouva ČEZ.pdf"} {
		page, err := x.svc.List(ctx, q, nil, false, 0, "", documents.Scope{})
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		if len(page.Items) != 1 {
			t.Errorf("search %q returned %d items, want 1", q, len(page.Items))
		}
	}
	page, err := x.svc.List(ctx, "kotelna", nil, false, 0, "", documents.Scope{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("file contents must not be indexed (D46), but %q matched", "kotelna")
	}
}

func TestSearch_TriggersStaySyncedOnUpdateAndDelete(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	d := x.upload(ctx, "puvodni.pdf", pdfBytes(), nil)

	title := "Přejmenováno"
	x.doc(x.svc.UpdateDocument(ctx, d.ID, documents.DocumentUpdate{Title: &title}, ""))
	if page, _ := x.svc.List(ctx, "prejmenovano", nil, false, 0, "", documents.Scope{}); len(page.Items) != 1 {
		t.Error("the FTS index did not follow the rename")
	}
	if page, _ := x.svc.List(ctx, "puvodni", nil, false, 0, "", documents.Scope{}); len(page.Items) != 1 {
		// The original FILENAME is still indexed — only the title changed.
		t.Error("the original filename should still match")
	}

	if err := x.svc.DeleteDocument(adminCtx(), d.ID, true); err != nil {
		t.Fatalf("hard delete: %v", err)
	}
	if page, _ := x.svc.List(ctx, "prejmenovano", nil, false, 0, "", documents.Scope{}); len(page.Items) != 0 {
		t.Error("the FTS index kept a purged document")
	}
}

func TestList_KeysetPagesWithoutRepeatingARow(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	for i := 0; i < 5; i++ {
		x.upload(ctx, fmt.Sprintf("dok-%d.pdf", i), pdfBytes(), nil)
	}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 10; page++ {
		p, err := x.svc.List(ctx, "", nil, false, 2, cursor, documents.Scope{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, it := range p.Items {
			if seen[it.ID] {
				t.Fatalf("document %s returned twice across pages", it.ID)
			}
			seen[it.ID] = true
		}
		if p.NextCursor == nil {
			break
		}
		cursor = *p.NextCursor
	}
	if len(seen) != 5 {
		t.Errorf("paged through %d documents, want 5", len(seen))
	}
}

// ---- tree (no N+1) ----

func TestTree_IsBoundedAndCarriesPinState(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	root := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Smlouvy"}))
	for i := 0; i < 20; i++ {
		sub := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{
			Name: fmt.Sprintf("Podsložka %d", i), ParentID: &root.ID,
		}))
		x.upload(ctx, fmt.Sprintf("dok-%d.pdf", i), pdfBytes(), &sub.ID)
	}
	unfiled := x.upload(ctx, "nezalozeno.pdf", pdfBytes(), nil)
	mustPin(t, x.svc, ctx, unfiled.ID, "household")

	// Shape and pin state here; the statement-count proof that this is bounded lives
	// in TestTreeAndWidget_CostDoesNotGrowWithTheTree (nplusone_test.go).
	tree, err := x.svc.Tree(ctx, false, documents.Scope{})
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	if len(tree.Roots) != 1 || len(tree.Roots[0].Subfolders) != 20 {
		t.Fatalf("tree shape = %d roots / %d subfolders, want 1/20", len(tree.Roots), len(tree.Roots[0].Subfolders))
	}
	if len(tree.RootDocuments) != 1 || !tree.RootDocuments[0].Pinned.Household {
		t.Error("root documents should carry the caller's pin state")
	}
}

// ---- audit attribution ----

func TestAudit_DashboardOriginatedEditsCarryViaMeta(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	d := x.upload(ctx, "smlouva.pdf", pdfBytes(), nil)

	title := "Smlouva 2026"
	x.doc(x.svc.UpdateDocument(ctx, d.ID, documents.DocumentUpdate{Title: &title}, "dashboard"))
	if _, err := x.svc.Pin(ctx, d.ID, "household", "dashboard"); err != nil {
		t.Fatalf("pin: %v", err)
	}

	for _, action := range []string{"document.update", "document.pin"} {
		id := auditEvents(t, x.db, action)[0]
		var meta sql.NullString
		if err := x.db.QueryRow(`SELECT meta FROM audit_events WHERE id = ?`, id).Scan(&meta); err != nil {
			t.Fatalf("read meta: %v", err)
		}
		if !strings.Contains(meta.String, `"via":"dashboard"`) {
			t.Errorf("%s meta = %q, want via=dashboard", action, meta.String)
		}
	}
}

func TestAudit_EveryMutationExceptPersonalPinsIsLogged(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	f := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Auto"}))
	d := x.upload(ctx, "stk.pdf", pdfBytes(), &f.ID)
	title := "STK 2026"
	x.doc(x.svc.UpdateDocument(ctx, d.ID, documents.DocumentUpdate{Title: &title}, ""))
	x.doc(x.svc.MoveDocument(ctx, d.ID, documents.DocumentMoveRequest{Position: "z"}, ""))
	mustPin(t, x.svc, ctx, d.ID, "household")
	if _, err := x.svc.Unpin(ctx, d.ID, "household", ""); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	mustPin(t, x.svc, ctx, d.ID, "personal") // the one unaudited mutation
	if err := x.svc.DeleteDocument(ctx, d.ID, false); err != nil {
		t.Fatalf("delete: %v", err)
	}

	want := []string{
		"document_folder.create", "document.create", "document.update",
		"document.move", "document.pin", "document.unpin", "document.delete",
	}
	for _, action := range want {
		if len(auditEvents(t, x.db, action)) == 0 {
			t.Errorf("no audit event for %s", action)
		}
	}
	// 7 events exactly: the personal pin adds nothing.
	if n := testsupport.CountRows(t, x.db, "audit_events"); n != len(want) {
		t.Errorf("audit events = %d, want %d (personal pins emit nothing)", n, len(want))
	}
}

func TestModule_AuditActionsMatchWhatItEmits(t *testing.T) {
	x := newH(t)
	mod := documents.NewModule(x.svc)
	declared := map[string]bool{}
	for _, a := range mod.AuditActions() {
		declared[a] = true
	}
	for _, emitted := range []string{
		"document.create", "document.update", "document.move", "document.delete",
		"document.pin", "document.unpin",
		"document_folder.create", "document_folder.update", "document_folder.move", "document_folder.delete",
	} {
		if !declared[emitted] {
			t.Errorf("AuditActions() omits %q, so the log filter cannot offer it", emitted)
		}
	}
	if mod.Name() != "documents" {
		t.Errorf("module name = %q", mod.Name())
	}
	if len(mod.Widgets()) != 1 || mod.Widgets()[0].Key() != "documents.pripnute" {
		t.Error("the module must contribute exactly the documents.pripnute widget")
	}
}

// ---- helpers ----

func mustPin(t *testing.T, svc *documents.Service, ctx context.Context, id, scope string) {
	t.Helper()
	if _, err := svc.Pin(ctx, id, scope, ""); err != nil {
		t.Fatalf("pin %s: %v", scope, err)
	}
}

func widgetData(t *testing.T, svc *documents.Service, userID string) documents.PripnuteDokumentyWidget {
	t.Helper()
	mod := documents.NewModule(svc)
	data, err := mod.Widgets()[0].Data(context.Background(), registry.User{ID: userID, Roles: []string{"editor"}})
	if err != nil {
		t.Fatalf("widget data: %v", err)
	}
	w, ok := data.(documents.PripnuteDokumentyWidget)
	if !ok {
		t.Fatalf("widget payload type = %T", data)
	}
	return w
}

// isArchived reads the archived flag straight off the row, for the assertions the
// service's own read paths can no longer make (they all hide archived rows).
func isArchived(t *testing.T, db *sql.DB, table, id string) bool {
	t.Helper()
	var archived int
	if err := db.QueryRow(`SELECT archived FROM `+table+` WHERE id = ?`, id).Scan(&archived); err != nil {
		t.Fatalf("read %s.archived: %v", table, err)
	}
	return archived != 0
}

// auditEvents returns the ids of events with the given action, oldest first.
func auditEvents(t *testing.T, db *sql.DB, action string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT id FROM audit_events WHERE action = ? ORDER BY ts, id`, action)
	if err != nil {
		t.Fatalf("query audit events: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, id)
	}
	return out
}

func auditChangedFields(t *testing.T, db *sql.DB, eventID string) map[string]bool {
	t.Helper()
	rows, err := db.Query(`SELECT field FROM audit_changes WHERE event_id = ?`, eventID)
	if err != nil {
		t.Fatalf("query audit changes: %v", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[f] = true
	}
	return out
}

// failingStore wraps a BlobStore and fails the chosen operation, standing in for an
// object-storage outage.
type failingStore struct {
	blobstore.BlobStore
	failPut bool
	failGet bool
}

func (f *failingStore) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if f.failPut {
		// Drain the reader first, as a real client would before failing on the response.
		_, _ = io.Copy(io.Discard, r)
		return errors.New("simulated storage outage")
	}
	return f.BlobStore.Put(ctx, key, r, size, contentType)
}

func (f *failingStore) Get(ctx context.Context, key string, rng *blobstore.ByteRange) (io.ReadCloser, blobstore.ObjInfo, error) {
	if f.failGet {
		return nil, blobstore.ObjInfo{}, errors.New("simulated storage outage")
	}
	return f.BlobStore.Get(ctx, key, rng)
}
