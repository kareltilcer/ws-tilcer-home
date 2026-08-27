package documents_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/documents"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// The preview worker is exercised against a FAKE GOTENBERG (an httptest server), so
// these tests need no container and no LibreOffice. The thumbnail helpers (pdftoppm,
// cwebp) are deliberately pointed at a non-existent binary in most cases: a missing
// helper must degrade to "no thumbnail" and never fail a job.

// fakeGotenberg serves a PDF for any conversion request, counting the calls.
func fakeGotenberg(t *testing.T, behaviour func(w http.ResponseWriter, r *http.Request), calls *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			calls.Add(1)
		}
		if r.URL.Path != "/forms/libreoffice/convert" {
			t.Errorf("gotenberg called at %q, want /forms/libreoffice/convert", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// The request must be a multipart body carrying a "files" part; parse it so a
		// malformed body fails the test rather than passing silently.
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Errorf("gotenberg got a malformed body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(r.MultipartForm.File["files"]) == 0 {
			t.Error("gotenberg body has no \"files\" part")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		behaviour(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func servePDF(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/pdf")
	_, _ = w.Write(pdfBytes())
}

// previewHarness bundles everything a worker test needs.
type previewHarness struct {
	db     *sql.DB
	svc    *documents.Service
	blob   blobstore.BlobStore
	worker *documents.PreviewWorker
	events chan string
	// panicOnNextPush makes the next push blow up. The notifier is the only dependency
	// a test can inject INTO the worker's own goroutine, so it stands in for the real
	// hazard there: an image decoder panicking on crafted bytes.
	panicOnNextPush atomic.Bool
}

func newPreviewHarness(t *testing.T, cfg documents.PreviewConfig) *previewHarness {
	t.Helper()
	db := testsupport.NewDB(t)
	store, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	events := make(chan string, 32)
	h := &previewHarness{db: db, blob: store, events: events}
	notify := func(_ context.Context, typ string, _ any) {
		if h.panicOnNextPush.CompareAndSwap(true, false) {
			panic("a push handler blew up mid-job")
		}
		select {
		case events <- typ:
		default:
		}
	}
	svc := documents.NewService(db, audit.NewSink(), notify, store, documents.Options{
		MaxUploadBytes: 1 << 20,
		PreviewEnabled: true,
		TempDir:        t.TempDir(),
	}, slog.New(slog.NewJSONHandler(io.Discard, nil)))

	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	if cfg.TempDir == "" {
		cfg.TempDir = t.TempDir()
	}
	if cfg.PdftoppmPath == "" {
		cfg.PdftoppmPath = "definitely-not-a-real-binary-pdftoppm"
	}
	if cfg.CwebpPath == "" {
		cfg.CwebpPath = "definitely-not-a-real-binary-cwebp"
	}
	worker := documents.NewPreviewWorker(svc.Store(), store, notify, cfg)
	svc.SetPreviewEnqueue(worker.Enqueue)
	h.svc, h.worker = svc, worker
	return h
}

// waitForStatus polls until the document reaches want, or fails the test.
func waitForStatus(t *testing.T, db *sql.DB, id, want string) {
	t.Helper()
	waitForColumn(t, db, "preview_status", id, want)
}

// waitForThumbStatus is the same for the server-side thumbnail_status column.
func waitForThumbStatus(t *testing.T, db *sql.DB, id, want string) {
	t.Helper()
	waitForColumn(t, db, "thumbnail_status", id, want)
}

func waitForColumn(t *testing.T, db *sql.DB, column, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		if err := db.QueryRow(`SELECT `+column+` FROM documents WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", column, err)
		}
		if got == want {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("%s = %q after 5s, want %q", column, got, want)
}

// waitForCount polls a counter the fake converter increments, so a test can wait for
// an ATTEMPT rather than for the state change it deliberately does not make.
func waitForCount(t *testing.T, counter *atomic.Int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if counter.Load() >= want {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("counter = %d after 5s, want at least %d", counter.Load(), want)
}

func columnOf(t *testing.T, db *sql.DB, column, id string) string {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT `+column+` FROM documents WHERE id = ?`, id).Scan(&got); err != nil {
		t.Fatalf("read %s: %v", column, err)
	}
	return got
}

// encodedPNG builds a real w×h PNG — the 1×1 pngBytes fixture is too small to
// exercise anything that reasons about pixel counts.
func encodedPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return out.Bytes()
}

func TestPreviewWorker_OfficeConvertsToPDFAndPushesReady(t *testing.T) {
	var calls atomic.Int64
	srv := fakeGotenberg(t, servePDF, &calls)
	x := newPreviewHarness(t, documents.PreviewConfig{
		Enabled:      true,
		GotenbergURL: srv.URL,
		Timeout:      5 * time.Second,
		Workers:      2,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.worker.Start(ctx)

	d, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "Podmínky pojištění.docx",
		File:     bytes.NewReader(zipBytes()),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if d.PreviewStatus != "pending" {
		t.Fatalf("upload returned preview_status = %q, want pending", d.PreviewStatus)
	}

	waitForStatus(t, x.db, d.ID, "ready")

	// The derived PDF is cached in storage under the id-based key, so it is derived
	// once and reused forever (immutable bytes).
	if _, err := x.blob.Stat(ctx, "documents/"+d.ID+"/preview.pdf"); err != nil {
		t.Errorf("the derived preview PDF is missing: %v", err)
	}
	// /preview now serves the derived PDF rather than 409.
	var kind string
	if err := x.db.QueryRow(`SELECT preview_kind FROM documents WHERE id = ?`, d.ID).Scan(&kind); err != nil {
		t.Fatalf("read preview_kind: %v", err)
	}
	if kind != "pdf" {
		t.Errorf("preview_kind = %q, want pdf", kind)
	}
	if !sawEvent(x.events, "document.preview_ready", 2*time.Second) {
		t.Error("no document.preview_ready push — an open view would keep its skeleton")
	}
	if calls.Load() != 1 {
		t.Errorf("gotenberg calls = %d, want exactly 1 (derive once)", calls.Load())
	}
}

// A 4xx is Gotenberg refusing THIS file — a verdict that cannot change on a later
// boot, so the document is written off as download-only rather than retried forever.
func TestPreviewWorker_ConversionFailureLeavesTheDocumentDownloadOnly(t *testing.T) {
	srv := fakeGotenberg(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unsupported file", http.StatusBadRequest)
	}, nil)
	x := newPreviewHarness(t, documents.PreviewConfig{
		Enabled:      true,
		GotenbergURL: srv.URL,
		Timeout:      2 * time.Second,
		Workers:      1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.worker.Start(ctx)

	d, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "rozbite.docx",
		File:     bytes.NewReader(zipBytes()),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	waitForStatus(t, x.db, d.ID, "failed")

	// The upload itself survives intact — that is the whole point of failing soft.
	detail, err := x.svc.GetDocumentDetail(editorCtx(), d.ID)
	if err != nil {
		t.Fatalf("the document was lost: %v", err)
	}
	if detail.ByteSize == 0 || detail.Checksum == "" {
		t.Error("the original metadata should be untouched by a failed conversion")
	}
	if _, err := x.blob.Stat(ctx, "documents/"+d.ID+"/original"); err != nil {
		t.Errorf("the original bytes must survive a failed conversion: %v", err)
	}
	if !sawEvent(x.events, "document.preview_failed", 2*time.Second) {
		t.Error("no document.preview_failed push")
	}
}

// A converter that is DOWN is not a broken document, and the two arrive at the worker
// as the same non-nil error. Recording "failed" would be permanent — no sweep revisits
// it — so a .docx uploaded during a ten-second sidecar restart would carry "náhled se
// nepodařilo vytvořit" for the rest of its life.
func TestPreviewWorker_AConverterOutageLeavesThePreviewPendingForTheNextBoot(t *testing.T) {
	var attempts atomic.Int64
	down := fakeGotenberg(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "the sidecar is restarting", http.StatusBadGateway)
	}, &attempts)
	x := newPreviewHarness(t, documents.PreviewConfig{
		Enabled:      true,
		GotenbergURL: down.URL,
		Timeout:      2 * time.Second,
		Workers:      1,
	})
	first, cancelFirst := context.WithCancel(context.Background())
	x.worker.Start(first)

	d, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "behem-restartu.docx",
		File:     bytes.NewReader(zipBytes()),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	waitForCount(t, &attempts, 1)
	// The job has been through the converter and come back empty-handed; the row must be
	// untouched rather than written off.
	time.Sleep(150 * time.Millisecond)
	if got := columnOf(t, x.db, "preview_status", d.ID); got != "pending" {
		t.Fatalf("preview_status after a 502 = %q, want pending (a %q row is never retried)", got, got)
	}
	cancelFirst()
	x.worker.Wait()

	// The sidecar comes back. Only the pending status can bring the document back into
	// the pool, which is precisely what the outage had to preserve.
	healthy := fakeGotenberg(t, servePDF, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	next := documents.NewPreviewWorker(x.svc.Store(), x.blob, nil, documents.PreviewConfig{
		Enabled:      true,
		GotenbergURL: healthy.URL,
		Timeout:      5 * time.Second,
		Workers:      1,
		PdftoppmPath: "definitely-not-a-real-binary-pdftoppm",
		CwebpPath:    "definitely-not-a-real-binary-cwebp",
		TempDir:      t.TempDir(),
		Logger:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	next.Start(ctx)
	waitForStatus(t, x.db, d.ID, "ready")
}

// A shutdown mid-conversion is NOT a failed conversion. Recording "failed" would be
// permanent — requeuePending only re-enqueues rows still "pending" — so a redeploy
// timed badly would leave the document with no preview and no retry, forever.
func TestPreviewWorker_ShutdownLeavesTheJobPendingForTheNextBoot(t *testing.T) {
	converting := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := fakeGotenberg(t, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case converting <- struct{}{}:
		default:
		}
		// Hold the conversion open until the client's cancel lands. `release` is the
		// backstop: the server does not always observe an aborted request promptly, and
		// httptest.Server.Close waits for its handlers.
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}, nil)
	// Registered AFTER fakeGotenberg's srv.Close, so LIFO cleanup runs it FIRST.
	t.Cleanup(func() { close(release) })
	x := newPreviewHarness(t, documents.PreviewConfig{
		Enabled:      true,
		GotenbergURL: srv.URL,
		Timeout:      30 * time.Second, // far beyond the test: only the cancel ends this job
		Workers:      1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.worker.Start(ctx)

	d, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "Podmínky.docx",
		File:     bytes.NewReader(zipBytes()),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	select {
	case <-converting:
	case <-time.After(5 * time.Second):
		t.Fatal("the conversion never started")
	}

	cancel() // graceful shutdown, exactly what main.go's stopBackground does
	x.worker.Wait()

	var status string
	if err := x.db.QueryRow(`SELECT preview_status FROM documents WHERE id = ?`, d.ID).Scan(&status); err != nil {
		t.Fatalf("read preview_status: %v", err)
	}
	if status != "pending" {
		t.Errorf("preview_status after shutdown = %q, want pending (a %q row is never retried)", status, status)
	}
}

// Turning previews off must not strand rows that were pending when they were on:
// /preview would answer 409 "still being generated" and the UI would spin forever.
func TestPreviewWorker_DisabledSettlesRowsLeftPending(t *testing.T) {
	x := newPreviewHarness(t, documents.PreviewConfig{Enabled: false})

	// Uploaded while previews were still enabled (the service's own switch is on),
	// with no worker running to derive it.
	d, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "Smlouva.docx",
		File:     bytes.NewReader(zipBytes()),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if d.PreviewStatus != "pending" {
		t.Fatalf("upload returned preview_status = %q, want pending", d.PreviewStatus)
	}

	// The next boot has previews disabled.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.worker.Start(ctx)

	waitForStatus(t, x.db, d.ID, "none")
	var kind string
	if err := x.db.QueryRow(`SELECT preview_kind FROM documents WHERE id = ?`, d.ID).Scan(&kind); err != nil {
		t.Fatalf("read preview_kind: %v", err)
	}
	if kind != "none" {
		t.Errorf("preview_kind = %q, want none (download-only)", kind)
	}
}

// The same sweep must not cost a document a preview that ALREADY succeeded. It is
// keyed on either status, so an Office document whose conversion worked and whose
// thumbnail upload hit a transient storage error is in it — and recomputing the plan
// with previews off would write "none" over a preview PDF sitting ready in the
// bucket, leaving /preview to answer 204 forever.
func TestPreviewWorker_DisabledKeepsAPreviewThatAlreadySucceeded(t *testing.T) {
	x := newPreviewHarness(t, documents.PreviewConfig{Enabled: false})

	d, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "Smlouva.docx",
		File:     bytes.NewReader(zipBytes()),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	// Exactly what a converted Office document with a thumbnail left pending looks
	// like (deriveOfficePreview + encodeAndStoreThumb's thumbPending).
	if _, err := x.db.Exec(
		`UPDATE documents
		    SET preview_kind = 'pdf', preview_status = 'ready',
		        preview_key = 'documents/'||id||'/preview.pdf', thumbnail_status = 'pending'
		  WHERE id = ?`, d.ID); err != nil {
		t.Fatalf("seed the converted row: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.worker.Start(ctx)

	// The thumbnail settles — nothing is left to derive one — but the preview must
	// survive untouched, key included.
	waitForThumbStatus(t, x.db, d.ID, "none")
	if got := columnOf(t, x.db, "preview_status", d.ID); got != "ready" {
		t.Errorf("preview_status = %q, want ready — a working preview was thrown away", got)
	}
	if got := columnOf(t, x.db, "preview_kind", d.ID); got != "pdf" {
		t.Errorf("preview_kind = %q, want pdf", got)
	}
	if got := columnOf(t, x.db, "preview_key", d.ID); got == "" {
		t.Error("preview_key was cleared, so the stored PDF is now unreachable")
	}
}

// updated_at is the document's user-facing DATE and the ORDER BY / keyset key for the
// list, so a preview settling — derived data landing, not an edit — must leave it
// alone. The boot sweeps are where a re-dating bug does real damage: they walk every
// eligible row in one pass, so flipping HOME_DOCS_PREVIEW_ENABLED off and on would
// stamp the WHOLE archive with the boot timestamp — every document dated today, in an
// order nobody chose. Both sweeps get a test; the row is aged BEFORE the worker starts
// so the assertion cannot race the write it is checking.
func TestPreviewWorker_SettlingAPendingRowDoesNotReDateTheDocument(t *testing.T) {
	x := newPreviewHarness(t, documents.PreviewConfig{Enabled: false})

	d, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "Smlouva.docx",
		File:     bytes.NewReader(zipBytes()),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	// Aged the way a real archive is aged: filed a year ago, untouched since.
	const filed = "2025-08-13T09:30:00.000000Z"
	if _, err := x.db.Exec(`UPDATE documents SET updated_at = ? WHERE id = ?`, filed, d.ID); err != nil {
		t.Fatalf("age the row: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.worker.Start(ctx)

	waitForStatus(t, x.db, d.ID, "none")
	if got := columnOf(t, x.db, "updated_at", d.ID); got != filed {
		t.Errorf("updated_at = %q, want %q — the sweep re-dated a document nobody edited", got, filed)
	}
}

// The same for the other sweep: an image is born native/ready and only ever owed a
// thumbnail, so the boot pass re-opens it on thumbnail_status alone. Here the helper
// binaries are missing, which settles the thumbnail "failed" — still a derived write,
// still not a reason to move the document's date.
func TestPreviewWorker_ADerivedThumbnailDoesNotReDateTheDocument(t *testing.T) {
	x := newPreviewHarness(t, documents.PreviewConfig{
		Enabled: true,
		Timeout: 2 * time.Second,
		Workers: 1,
	})

	d, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "foto.png",
		File:     bytes.NewReader(pngBytes),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	const filed = "2025-08-13T09:30:00.000000Z"
	if _, err := x.db.Exec(`UPDATE documents SET updated_at = ? WHERE id = ?`, filed, d.ID); err != nil {
		t.Fatalf("age the row: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.worker.Start(ctx)

	waitForThumbStatus(t, x.db, d.ID, "failed")
	if got := columnOf(t, x.db, "updated_at", d.ID); got != filed {
		t.Errorf("updated_at = %q, want %q — a thumbnail job is not an edit", got, filed)
	}
}

// A panicking job must not take the process with it. Without the recover this test
// does not fail — the whole test BINARY dies, which is precisely the production
// outcome: an unrecovered panic in a worker goroutine kills the backend, todo,
// events, notes and the dashboard included, and the pending row then re-enqueues it
// into the same panic on every boot.
func TestPreviewWorker_APanickingJobDoesNotKillThePool(t *testing.T) {
	x := newPreviewHarness(t, documents.PreviewConfig{
		Enabled: true,
		Timeout: 2 * time.Second,
		Workers: 1,
		// cwebp is missing, so the image job settles quickly either way.
	})

	// Uploaded with no worker running, so the boot sweep picks it up — and the push
	// that reports its outcome panics.
	boom, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "vybuch.png",
		File:     bytes.NewReader(encodedPNG(t, 40, 40)),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	x.panicOnNextPush.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.worker.Start(ctx)

	// Settled, not left pending: a row still 'pending' would be fed back into the same
	// panic by requeuePending at every restart.
	waitForThumbStatus(t, x.db, boom.ID, "failed")

	// And the pool is still there to do the next document.
	next, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "dalsi.png",
		File:     bytes.NewReader(encodedPNG(t, 40, 40)),
	})
	if err != nil {
		t.Fatalf("upload after the panic: %v", err)
	}
	waitForThumbStatus(t, x.db, next.ID, "failed")

	// The second job cannot have run before the first one returned (one worker), so by
	// now the panicking push has definitely fired. Without this the test could pass
	// having proved nothing.
	if x.panicOnNextPush.Load() {
		t.Fatal("no push ever fired, so nothing panicked")
	}
}

func TestPreviewWorker_TimeoutIsTreatedAsAFailure(t *testing.T) {
	srv := fakeGotenberg(t, func(w http.ResponseWriter, r *http.Request) {
		// Outlast the job timeout; the worker's context cancels the request.
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	}, nil)
	x := newPreviewHarness(t, documents.PreviewConfig{
		Enabled:      true,
		GotenbergURL: srv.URL,
		Timeout:      200 * time.Millisecond,
		Workers:      1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.worker.Start(ctx)

	d, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "pomale.docx",
		File:     bytes.NewReader(zipBytes()),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	waitForStatus(t, x.db, d.ID, "failed")
}

func TestPreviewWorker_NoConverterConfiguredMarksOfficeDownloadOnly(t *testing.T) {
	x := newPreviewHarness(t, documents.PreviewConfig{
		Enabled: true, // enabled, but no Gotenberg URL
		Timeout: 2 * time.Second,
		Workers: 1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.worker.Start(ctx)

	// previewPlanFor already decides "none" at upload time when previews are disabled;
	// with previews ON but no converter, the worker settles it rather than leaving the
	// document pending forever.
	d, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "bez-konvertoru.docx",
		File:     bytes.NewReader(zipBytes()),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	waitForStatus(t, x.db, d.ID, "none")
}

func TestPreviewWorker_ReEnqueuesPendingOnBootAndIsIdempotent(t *testing.T) {
	var calls atomic.Int64
	srv := fakeGotenberg(t, servePDF, &calls)

	// First harness: upload with NO worker running, so the row is left pending exactly
	// as a crash mid-conversion would leave it.
	x := newPreviewHarness(t, documents.PreviewConfig{
		Enabled:      true,
		GotenbergURL: srv.URL,
		Timeout:      5 * time.Second,
		Workers:      1,
	})
	d, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "po-restartu.docx",
		File:     bytes.NewReader(zipBytes()),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	var status string
	if err := x.db.QueryRow(`SELECT preview_status FROM documents WHERE id = ?`, d.ID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "pending" {
		t.Fatalf("status before the restart = %q, want pending", status)
	}

	// "Restart": start the worker, which drains the pending backlog.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.worker.Start(ctx)
	waitForStatus(t, x.db, d.ID, "ready")

	// Idempotence: re-enqueueing a finished document simply redoes the same work with
	// the same result — no duplicate rows, no corrupted state.
	x.worker.Enqueue(d.ID)
	time.Sleep(300 * time.Millisecond)
	waitForStatus(t, x.db, d.ID, "ready")
	if n := countRows(t, x.db, "documents"); n != 1 {
		t.Errorf("documents rows = %d, want 1", n)
	}
}

func TestPreviewWorker_MissingThumbnailBinariesDegradeGracefully(t *testing.T) {
	x := newPreviewHarness(t, documents.PreviewConfig{
		Enabled: true,
		Timeout: 2 * time.Second,
		Workers: 1,
		// Both helper paths point at binaries that do not exist.
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.worker.Start(ctx)

	d, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "foto.png",
		File:     bytes.NewReader(pngBytes),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	// An image previews natively, so it is "ready" from the start; the missing cwebp
	// only costs it a thumbnail.
	if d.PreviewStatus != "ready" || d.PreviewKind != "native" {
		t.Errorf("image preview = %s/%s, want native/ready", d.PreviewKind, d.PreviewStatus)
	}
	// A missing binary is TERMINAL: recorded "failed" rather than left pending, so the
	// next boot does not re-download every original to fail the same way again.
	waitForThumbStatus(t, x.db, d.ID, "failed")
	var thumb sql.NullString
	if err := x.db.QueryRow(`SELECT thumbnail_key FROM documents WHERE id = ?`, d.ID).Scan(&thumb); err != nil {
		t.Fatalf("read thumbnail_key: %v", err)
	}
	if thumb.Valid && thumb.String != "" {
		t.Errorf("thumbnail_key = %q, want empty when cwebp is unavailable", thumb.String)
	}
	// And the document is still perfectly usable.
	if _, err := x.blob.Stat(ctx, "documents/"+d.ID+"/original"); err != nil {
		t.Errorf("the original must be intact: %v", err)
	}
}

// The recovery path for a thumbnail that never landed. An image is born
// "native"/"ready" — the original IS its preview — so preview_status can never say
// the document still owes us a thumbnail. Without thumbnail_status, a job lost to a
// transient storage error (or a full queue during a bulk upload) would be skipped by
// every subsequent boot sweep and the document would sit thumbnail-less forever.
func TestPreviewWorker_AnUnfinishedImageThumbnailIsPickedUpOnTheNextBoot(t *testing.T) {
	x := newPreviewHarness(t, documents.PreviewConfig{
		Enabled: true,
		Timeout: 2 * time.Second,
		Workers: 1,
	})
	// No worker started: the row lands exactly as a crash mid-thumbnail leaves it.
	d, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "foto.png",
		File:     bytes.NewReader(pngBytes),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if got := columnOf(t, x.db, "preview_status", d.ID); got != "ready" {
		t.Fatalf("preview_status = %q, want ready — the sweep cannot rely on it", got)
	}
	if got := columnOf(t, x.db, "thumbnail_status", d.ID); got != "pending" {
		t.Fatalf("thumbnail_status = %q, want pending", got)
	}

	// The next boot. A brand-new worker, so its queue is empty and only the boot sweep
	// over the pending statuses can reach this document.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	next := documents.NewPreviewWorker(x.svc.Store(), x.blob, nil, documents.PreviewConfig{
		Enabled:      true,
		Timeout:      2 * time.Second,
		Workers:      1,
		PdftoppmPath: "definitely-not-a-real-binary-pdftoppm",
		CwebpPath:    "definitely-not-a-real-binary-cwebp",
		TempDir:      t.TempDir(),
		Logger:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	next.Start(ctx)
	waitForThumbStatus(t, x.db, d.ID, "failed") // settled by the retry, not still pending
}

// MaxUploadBytes bounds the FILE; nothing in it bounds the decoded pixels, and the
// worker decodes in the app process — so an unbounded decode is an OOM of the whole
// backend rather than a failed thumbnail.
func TestPreviewWorker_RefusesToDecodeAnImageOverThePixelLimit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		limit   int
		refused bool
	}{
		{name: "over the limit", limit: 1, refused: true},
		{name: "within the limit", limit: 1 << 20, refused: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &testsupport.SyncBuffer{}
			x := newPreviewHarness(t, documents.PreviewConfig{
				Enabled:        true,
				Timeout:        2 * time.Second,
				Workers:        1,
				MaxImagePixels: tc.limit,
				Logger:         slog.New(slog.NewJSONHandler(logs, nil)),
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			x.worker.Start(ctx)

			d, err := x.svc.Upload(editorCtx(), documents.UploadInput{
				Filename: "panorama.png",
				File:     bytes.NewReader(encodedPNG(t, 64, 64)), // 4096 pixels in ~300 bytes
			})
			if err != nil {
				t.Fatalf("upload: %v", err)
			}
			// Either way the job settles (cwebp is absent, so it can never produce one) —
			// what differs is WHETHER the decode was attempted at all.
			waitForThumbStatus(t, x.db, d.ID, "failed")
			if refused := strings.Contains(logs.String(), "decode limit"); refused != tc.refused {
				t.Errorf("refused by the pixel guard = %t, want %t; logs: %s", refused, tc.refused, logs.String())
			}
			// A refused thumbnail never touches the document itself.
			if _, err := x.blob.Stat(ctx, "documents/"+d.ID+"/original"); err != nil {
				t.Errorf("the original must be intact: %v", err)
			}
		})
	}
}

// "none" is terminal, which is right when it describes the FILE and wrong when it only
// describes the configuration at upload time. Without the backfill, every document
// uploaded during a week with previews switched off would be permanently download-only
// once they were switched back on, and re-uploading under a fresh id the only way out.
func TestPreviewWorker_ReOpensDocumentsSettledWhilePreviewsWereOff(t *testing.T) {
	x := newPreviewHarness(t, documents.PreviewConfig{Enabled: false})

	// Uploaded with the worker down, then settled to "none" by the disabled boot below —
	// the real path an operator running HOME_DOCS_PREVIEW_ENABLED=false ends up on.
	docx, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "Smlouva.docx",
		File:     bytes.NewReader(zipBytes()),
	})
	if err != nil {
		t.Fatalf("upload docx: %v", err)
	}
	img, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "foto.png",
		File:     bytes.NewReader(encodedPNG(t, 40, 40)),
	})
	if err != nil {
		t.Fatalf("upload png: %v", err)
	}
	zip, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "archiv.zip",
		File:     bytes.NewReader(zipBytes()),
	})
	if err != nil {
		t.Fatalf("upload zip: %v", err)
	}

	off, cancelOff := context.WithCancel(context.Background())
	x.worker.Start(off)
	waitForStatus(t, x.db, docx.ID, "none")
	waitForThumbStatus(t, x.db, docx.ID, "none")
	waitForThumbStatus(t, x.db, img.ID, "none")
	cancelOff()

	// The next boot has previews on and a converter configured. Nothing is 'pending' any
	// more, so only the backfill can reach these rows.
	srv := fakeGotenberg(t, servePDF, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	next := documents.NewPreviewWorker(x.svc.Store(), x.blob, nil, documents.PreviewConfig{
		Enabled:      true,
		GotenbergURL: srv.URL,
		Timeout:      5 * time.Second,
		Workers:      1,
		PdftoppmPath: "definitely-not-a-real-binary-pdftoppm",
		CwebpPath:    "definitely-not-a-real-binary-cwebp",
		TempDir:      t.TempDir(),
		Logger:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	next.Start(ctx)

	// The Office file finally gets its preview PDF...
	waitForStatus(t, x.db, docx.ID, "ready")
	if got := columnOf(t, x.db, "preview_kind", docx.ID); got != "pdf" {
		t.Errorf("preview_kind = %q, want pdf", got)
	}
	// ...and the image's thumbnail is attempted at last (cwebp is absent here, so
	// "failed" is the settled outcome — what matters is that it is no longer "none").
	waitForThumbStatus(t, x.db, img.ID, "failed")

	// A type with nothing to derive is left exactly where it was: its "none" was never
	// about the configuration, and re-opening it would mean a pointless job at every
	// boot for the rest of the archive's life.
	if got := columnOf(t, x.db, "preview_status", zip.ID); got != "none" {
		t.Errorf("preview_status of a .zip = %q, want none", got)
	}
	if got := columnOf(t, x.db, "thumbnail_status", zip.ID); got != "none" {
		t.Errorf("thumbnail_status of a .zip = %q, want none", got)
	}
}

// The same sweep must not churn when the reason for "none" is still in force: with no
// converter configured, re-opening an Office row only has the job write "none" straight
// back, once per document per boot, each time re-downloading the original.
func TestPreviewWorker_DoesNotReOpenOfficeWithoutAConverter(t *testing.T) {
	x := newPreviewHarness(t, documents.PreviewConfig{Enabled: false})
	d, err := x.svc.Upload(editorCtx(), documents.UploadInput{
		Filename: "Smlouva.docx",
		File:     bytes.NewReader(zipBytes()),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	off, cancelOff := context.WithCancel(context.Background())
	x.worker.Start(off)
	waitForStatus(t, x.db, d.ID, "none")
	cancelOff()

	// Previews on, but still no Gotenberg URL.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	next := documents.NewPreviewWorker(x.svc.Store(), x.blob, nil, documents.PreviewConfig{
		Enabled:      true,
		Timeout:      2 * time.Second,
		Workers:      1,
		PdftoppmPath: "definitely-not-a-real-binary-pdftoppm",
		CwebpPath:    "definitely-not-a-real-binary-cwebp",
		TempDir:      t.TempDir(),
		Logger:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	next.Start(ctx)
	time.Sleep(300 * time.Millisecond)
	if got := columnOf(t, x.db, "preview_status", d.ID); got != "none" {
		t.Errorf("preview_status = %q, want none — nothing can convert it yet", got)
	}
	if got := columnOf(t, x.db, "thumbnail_status", d.ID); got != "none" {
		t.Errorf("thumbnail_status = %q, want none — an Office thumbnail comes off the converted PDF", got)
	}
}

func TestPreviewWorker_DisabledDoesNothing(t *testing.T) {
	x := newPreviewHarness(t, documents.PreviewConfig{Enabled: false})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.worker.Start(ctx) // no-op

	x.worker.Enqueue("whatever") // must not panic or block
}

func sawEvent(events chan string, want string, within time.Duration) bool {
	deadline := time.After(within)
	for {
		select {
		case ev := <-events:
			if ev == want {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// ---- mirror + reconciliation (D45) ----

func TestMirror_CopiesOnlyWhatTheBackupIsMissing(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	first := x.upload(ctx, "a.pdf", pdfBytes(), nil)
	second := x.upload(ctx, "b.pdf", pdfBytes(), nil)

	backup, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("backup store: %v", err)
	}
	job := documents.NewMirrorJob(x.svc.Store(), x.blob, documents.MirrorConfig{
		Interval:    24 * time.Hour,
		OrphanGrace: time.Hour,
		Backup:      backup,
		Logger:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})

	report := job.RunOnce(context.Background())
	if report.Copied != 2 {
		t.Errorf("copied = %d, want 2", report.Copied)
	}
	for _, id := range []string{first.ID, second.ID} {
		if _, err := backup.Stat(ctx, "documents/"+id+"/original"); err != nil {
			t.Errorf("document %s was not mirrored: %v", id, err)
		}
	}

	// Objects are immutable, so a second pass is pure copy-if-absent: nothing to do.
	report = job.RunOnce(context.Background())
	if report.Copied != 0 || report.AlreadyThere != 2 {
		t.Errorf("second pass copied=%d already=%d, want 0/2", report.Copied, report.AlreadyThere)
	}
}

func TestReconcile_FlagsOrphansAndDanglingRows(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	d := x.upload(ctx, "smlouva.pdf", pdfBytes(), nil)

	// An orphan: an object with no row, exactly what a crash between the storage write
	// and the commit leaves behind.
	if err := x.blob.Put(ctx, "documents/019ffffff-orphan/original",
		bytes.NewReader(pdfBytes()), int64(len(pdfBytes())), "application/pdf"); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}

	job := documents.NewMirrorJob(x.svc.Store(), x.blob, documents.MirrorConfig{
		Interval: 24 * time.Hour,
		// A long grace window: a freshly-written orphan must be LEFT ALONE, because it
		// might be an upload in flight right now.
		OrphanGrace: time.Hour,
		Logger:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	report := job.RunOnce(context.Background())
	if report.Orphans != 1 {
		t.Errorf("orphans = %d, want 1", report.Orphans)
	}
	if report.OrphansDeleted != 0 {
		t.Error("an orphan inside the grace window must not be deleted — it may be in flight")
	}
	if report.Dangling != 0 {
		t.Errorf("dangling = %d, want 0", report.Dangling)
	}

	// Past the grace window it is safe to reclaim. The object is AGED rather than the
	// window shrunk to a nanosecond — see h.backdate for why that was a race.
	x.backdate(time.Hour, "documents/019ffffff-orphan/original")
	aged := documents.NewMirrorJob(x.svc.Store(), x.blob, documents.MirrorConfig{
		Interval:    24 * time.Hour,
		OrphanGrace: time.Minute,
		Logger:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	report = aged.RunOnce(context.Background())
	if report.OrphansDeleted != 1 {
		t.Errorf("orphans deleted = %d, want 1", report.OrphansDeleted)
	}
	if _, err := x.blob.Stat(ctx, "documents/019ffffff-orphan/original"); !errors.Is(err, blobstore.ErrNotFound) {
		t.Errorf("the aged orphan should be gone, got %v", err)
	}

	// A dangling row: the object vanished under a live row. It is only ever reported —
	// deleting a user's document because a read hiccuped would be far worse.
	if err := x.blob.Delete(ctx, "documents/"+d.ID+"/original"); err != nil {
		t.Fatalf("delete object: %v", err)
	}
	report = aged.RunOnce(context.Background())
	if report.Dangling != 1 {
		t.Errorf("dangling = %d, want 1", report.Dangling)
	}
	if _, err := x.svc.GetDocumentDetail(ctx, d.ID); err != nil {
		t.Error("the row must survive reconciliation — it is reported, never auto-deleted")
	}
}

// An object no row claims must never reach the backup bucket. Reconciliation only
// ever sweeps the PRIMARY, so a mirrored orphan is a copy nothing will ever delete —
// including the bytes of a document an admin explicitly purged.
func TestMirror_DoesNotCopyUnclaimedObjects(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	x.upload(ctx, "smlouva.pdf", pdfBytes(), nil)

	const orphanKey = "documents/019ffffff-orphan/original"
	if err := x.blob.Put(ctx, orphanKey,
		bytes.NewReader(pdfBytes()), int64(len(pdfBytes())), "application/pdf"); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}

	backup, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("backup store: %v", err)
	}
	job := documents.NewMirrorJob(x.svc.Store(), x.blob, documents.MirrorConfig{
		Interval: 24 * time.Hour,
		// A long grace window keeps the orphan on the primary, so this test isolates the
		// mirror's decision rather than reconciliation's.
		OrphanGrace: time.Hour,
		Backup:      backup,
		Logger:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	report := job.RunOnce(context.Background())
	if report.Copied != 1 {
		t.Errorf("copied = %d, want 1 (the claimed object only)", report.Copied)
	}
	if _, err := backup.Stat(ctx, orphanKey); !errors.Is(err, blobstore.ErrNotFound) {
		t.Errorf("the orphan reached the backup bucket, which never sweeps: %v", err)
	}
}

// The reconciliation pass infers "orphan" from a MISSING ROW, so it is only ever as
// trustworthy as the database it just read. These two tests pin the blast-radius
// guard that stands between a half-restored database and the household's archive:
// the backup bucket is optional and object versioning has a retention window, so
// neither of D45's other legs can be assumed to catch this.

func TestReconcile_RefusesToDeleteWhenNoRowClaimsAnyObject(t *testing.T) {
	x := newH(t)
	ctx := context.Background()

	// A full bucket and an empty documents table: a Litestream restore that was
	// skipped or failed, or a rebuilt volume that re-ran the migrations from scratch.
	keys := []string{
		"documents/019aaaaaaaa/original",
		"documents/019bbbbbbbb/original",
		"documents/019cccccccc/original",
	}
	for _, k := range keys {
		if err := x.blob.Put(ctx, k, bytes.NewReader(pdfBytes()), int64(len(pdfBytes())), "application/pdf"); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}

	// Every object is well past the grace window — aged explicitly, because asking
	// whether a just-written file is more than a nanosecond old is a coin flip on a
	// millisecond clock (see h.backdate).
	x.backdate(time.Hour, keys...)

	job := documents.NewMirrorJob(x.svc.Store(), x.blob, documents.MirrorConfig{
		Interval:    24 * time.Hour,
		OrphanGrace: time.Minute,
		Logger:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	report := job.RunOnce(ctx)

	if report.Orphans != len(keys) {
		t.Errorf("orphans = %d, want %d", report.Orphans, len(keys))
	}
	if report.OrphansDeleted != 0 {
		t.Errorf("deleted %d objects — an unrestored database must delete NOTHING", report.OrphansDeleted)
	}
	if report.OrphansBlocked != len(keys) {
		t.Errorf("blocked = %d, want %d", report.OrphansBlocked, len(keys))
	}
	for _, k := range keys {
		if _, err := x.blob.Stat(ctx, k); err != nil {
			t.Errorf("object %s was deleted: %v", k, err)
		}
	}
}

func TestReconcile_RefusesWhenOrphansExceedTheShareLimit(t *testing.T) {
	x := newH(t)
	ctx := context.Background()
	x.upload(editorCtx(), "smlouva.pdf", pdfBytes(), nil) // one live row, so the "no rows at all" guard does not fire

	// Past the floor that lets a small bucket reclaim a crashed upload, and far past
	// any share a healthy bucket produces.
	var keys []string
	for i := 0; i < 25; i++ {
		k := "documents/019orphan" + string(rune('a'+i)) + "/original"
		if err := x.blob.Put(ctx, k, bytes.NewReader(pdfBytes()), int64(len(pdfBytes())), "application/pdf"); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
		keys = append(keys, k)
	}

	x.backdate(time.Hour, keys...) // past the window, without racing the clock

	cfg := documents.MirrorConfig{
		Interval:    24 * time.Hour,
		OrphanGrace: time.Minute,
		Logger:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	report := documents.NewMirrorJob(x.svc.Store(), x.blob, cfg).RunOnce(ctx)
	if report.OrphansDeleted != 0 || report.OrphansBlocked != len(keys) {
		t.Errorf("deleted=%d blocked=%d, want 0/%d", report.OrphansDeleted, report.OrphansBlocked, len(keys))
	}

	// And the guard is exactly what stopped it: raising the limit deletes the same set.
	cfg.MaxOrphanShare = 1
	report = documents.NewMirrorJob(x.svc.Store(), x.blob, cfg).RunOnce(ctx)
	if report.OrphansDeleted != len(keys) {
		t.Errorf("with the guard opened up, deleted = %d, want %d", report.OrphansDeleted, len(keys))
	}
}

// The floor that lets a SMALL bucket reclaim a crashed upload must not become a hole
// the share guard cannot see through. A household of three documents booted against a
// restore that recovered one row leaves the other two documents' objects unclaimed:
// well under any useful floor in absolute terms, and most of the archive in relative
// ones. One live row is enough to get past the "no row claims anything" guard, so this
// bound is the only thing standing between a partial restore and the bytes.
func TestReconcile_RefusesWhenOrphansOutnumberWhatTheRowsStillClaim(t *testing.T) {
	x := newH(t)
	ctx := context.Background()
	x.upload(editorCtx(), "smlouva.pdf", pdfBytes(), nil) // the one document the restore brought back

	// Two documents' worth of objects with no rows left to claim them — four objects, so
	// still far below the floor.
	var keys []string
	for _, k := range []string{
		"documents/019aaaaaaaa/original", "documents/019aaaaaaaa/thumb.webp",
		"documents/019bbbbbbbb/original", "documents/019bbbbbbbb/thumb.webp",
	} {
		if err := x.blob.Put(ctx, k, bytes.NewReader(pdfBytes()), int64(len(pdfBytes())), "application/pdf"); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
		keys = append(keys, k)
	}

	x.backdate(time.Hour, keys...) // every object is well past the grace window

	report := documents.NewMirrorJob(x.svc.Store(), x.blob, documents.MirrorConfig{
		Interval:    24 * time.Hour,
		OrphanGrace: time.Minute,
		Logger:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}).RunOnce(ctx)

	if report.OrphansDeleted != 0 || report.OrphansBlocked != len(keys) {
		t.Errorf("deleted=%d blocked=%d, want 0/%d — a half-restored database must delete nothing",
			report.OrphansDeleted, report.OrphansBlocked, len(keys))
	}
	for _, k := range keys {
		if _, err := x.blob.Stat(ctx, k); err != nil {
			t.Errorf("object %s was deleted: %v", k, err)
		}
	}
}

func TestMirror_WithoutABackupBucketStillReconciles(t *testing.T) {
	x := newH(t)
	x.upload(editorCtx(), "a.pdf", pdfBytes(), nil)
	job := documents.NewMirrorJob(x.svc.Store(), x.blob, documents.MirrorConfig{
		Interval:    24 * time.Hour,
		OrphanGrace: time.Hour,
		Logger:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	report := job.RunOnce(context.Background())
	if report.Copied != 0 || report.Dangling != 0 || report.Orphans != 0 {
		t.Errorf("unexpected report with no backup configured: %+v", report)
	}
}
