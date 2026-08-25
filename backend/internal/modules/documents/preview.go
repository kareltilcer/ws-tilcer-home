package documents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // register GIF for image.Decode (thumbnails)
	_ "image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw" // scaling kernels the stdlib lacks
	_ "golang.org/x/image/webp"     // register WebP decoding (a WebP upload gets a thumbnail too)

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
)

// The preview worker derives the two optional objects a document can have: a
// preview PDF (for Office files) and a thumbnail. It runs OUT of the request path
// in an in-process pool, because conversion takes seconds and an upload must return
// immediately (FR-DOC9/D44).
//
// Because the bytes are immutable, every derivation happens EXACTLY ONCE per
// document and is cached in object storage forever — there is no invalidation
// problem and no reason to ever re-run a successful job.
//
// Failure is never fatal: a document whose conversion times out stays perfectly
// usable as a download. That is the difference between preview_status "failed" and
// losing an upload.
//
// Office→PDF runs in a GOTENBERG SIDECAR rather than a LibreOffice binary inside
// this image (a deliberate deviation from HANDOFF-6 §16, decided with Karel): the
// runtime image stays ~100 MB instead of ~1 GB, and the converter is isolated from
// the app process. Thumbnails still use two small local binaries: pdftoppm
// (poppler-utils) to raster a PDF's first page and cwebp (libwebp-tools) to encode.
// A missing binary degrades to "no thumbnail", never an error.

// PreviewConfig configures the worker.
type PreviewConfig struct {
	Enabled      bool
	GotenbergURL string
	Timeout      time.Duration
	Workers      int
	PdftoppmPath string
	CwebpPath    string
	ThumbMaxPx   int
	// MaxImagePixels caps the DECODED size of a source image (width × height). The
	// upload limit bounds bytes, which says nothing about pixels — see
	// scaleImageFile. <= 0 takes the default.
	MaxImagePixels int
	TempDir        string
	Logger         *slog.Logger
	// HTTPClient is injected in tests (a fake Gotenberg); nil uses a default client
	// bounded by Timeout.
	HTTPClient *http.Client
}

// PreviewWorker consumes document ids and derives their preview objects.
type PreviewWorker struct {
	store  *Store
	blob   blobstore.BlobStore
	notify Notifier
	cfg    PreviewConfig
	logger *slog.Logger

	queue chan string
	// inFlight makes the worker idempotent under concurrency: a document already
	// being processed is not queued again (a boot re-enqueue racing a fresh upload).
	mu       sync.Mutex
	inFlight map[string]bool

	wg      sync.WaitGroup
	started bool
}

const previewQueueSize = 256

// defaultMaxImagePixels bounds a thumbnail decode at 50 MP — comfortably above any
// phone or scanner output, and ~200 MB of RGBA per worker in the worst case.
const defaultMaxImagePixels = 50_000_000

func NewPreviewWorker(store *Store, blob blobstore.BlobStore, notify Notifier, cfg PreviewConfig) *PreviewWorker {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Workers < 1 {
		cfg.Workers = 1
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.ThumbMaxPx < 32 {
		cfg.ThumbMaxPx = 480
	}
	if cfg.MaxImagePixels <= 0 {
		cfg.MaxImagePixels = defaultMaxImagePixels
	}
	if notify == nil {
		notify = func(context.Context, string, any) {}
	}
	return &PreviewWorker{
		store:    store,
		blob:     blob,
		notify:   notify,
		cfg:      cfg,
		logger:   cfg.Logger,
		queue:    make(chan string, previewQueueSize),
		inFlight: map[string]bool{},
	}
}

// Start launches the pool and drains anything left "pending" from a previous run —
// a crash mid-conversion must not leave a document pending forever.
func (w *PreviewWorker) Start(ctx context.Context) {
	if w.started {
		return
	}
	w.started = true
	if !w.cfg.Enabled {
		// Previews are off, but rows uploaded while they were ON may still be pending,
		// and with no pool running nothing would ever resolve them: /preview would answer
		// 409 "still being generated" and the UI would spin forever. Settle them as
		// download-only instead.
		go w.settlePending(ctx)
		return
	}
	for i := 0; i < w.cfg.Workers; i++ {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case id, ok := <-w.queue:
					if !ok {
						return
					}
					w.runJob(ctx, id)
					w.mu.Lock()
					delete(w.inFlight, id)
					w.mu.Unlock()
				}
			}
		}()
	}
	// Sequential, in one goroutine: both sweeps read the single DB connection at boot,
	// and the backfill has to mark its rows pending BEFORE the re-enqueue looks for them
	// (it enqueues its own too — Enqueue is idempotent, so the overlap is free).
	go func() {
		w.backfillSettled(ctx)
		w.requeuePending(ctx)
	}()
}

// Wait blocks until the pool has drained after its context was cancelled.
func (w *PreviewWorker) Wait() { w.wg.Wait() }

// Enqueue schedules a document for derivation. Non-blocking: a full queue drops the
// request and logs it — the row stays "pending" (preview_status, thumbnail_status, or
// both) and the next boot re-enqueues it.
func (w *PreviewWorker) Enqueue(id string) {
	if !w.cfg.Enabled {
		return
	}
	w.mu.Lock()
	if w.inFlight[id] {
		w.mu.Unlock()
		return // already queued or running: idempotent
	}
	w.inFlight[id] = true
	w.mu.Unlock()

	select {
	case w.queue <- id:
	default:
		w.mu.Lock()
		delete(w.inFlight, id)
		w.mu.Unlock()
		w.logger.Warn("documents: preview queue full, deferring to the next boot", "document_id", id)
	}
}

// requeuePending re-enqueues everything still pending after a restart.
func (w *PreviewWorker) requeuePending(ctx context.Context) {
	ids, err := w.store.PendingPreviewIDs(ctx)
	if err != nil {
		w.logger.Error("documents: cannot list pending previews", "err", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	w.logger.Info("documents: re-enqueueing previews left pending by a previous run", "count", len(ids))
	for _, id := range ids {
		w.Enqueue(id)
	}
}

// backfillSettled re-opens documents that were written off as having no preview or no
// thumbnail because DERIVATION WAS OFF when they arrived, not because their type has
// nothing to derive. It is the mirror image of settlePending: that sweep settles rows
// to "none" when the worker shuts down without a converter, and this one un-settles
// them once there is a converter again.
//
// Without it the two states are asymmetric — "none" is terminal, so a household that
// ran with HOME_DOCS_PREVIEW_ENABLED=false (or without HOME_DOCS_GOTENBERG_URL) for a
// week would find every document uploaded in that week permanently download-only,
// with re-uploading under a fresh id the only way out.
//
// The re-open is CONDITIONAL on the configuration that can actually serve it: an
// Office file is left alone while GotenbergURL is empty, because its thumbnail comes
// off the converted PDF too, and flipping it to pending would only have the job write
// "none" straight back — a write and a wasted download at every single boot.
func (w *PreviewWorker) backfillSettled(ctx context.Context) {
	cands, err := w.store.SettledWithoutDerivation(ctx)
	if err != nil {
		w.logger.Error("documents: cannot list documents settled without a preview", "err", err)
		return
	}
	reopened := 0
	for _, c := range cands {
		if !needsDerivedObject(c.ContentType) {
			continue // a .zip has no preview and no thumbnail: "none" is the truth
		}
		if isOffice(c.ContentType) && w.cfg.GotenbergURL == "" {
			continue // still nothing that could convert it
		}
		kind, status, thumb := c.PreviewKind, c.PreviewStatus, c.ThumbnailStatus
		if status == previewNone && !c.HasPreview {
			kind, status = previewPlanFor(c.ContentType, true)
		}
		if thumb == thumbNone && !c.HasThumbnail {
			thumb = thumbPending
		}
		if kind == c.PreviewKind && status == c.PreviewStatus && thumb == c.ThumbnailStatus {
			continue
		}
		if err := w.store.SetPreviewResult(ctx, w.store.db, c.ID, kind, status, thumb, nil, nil); err != nil {
			w.logger.Error("documents: cannot re-open a document for derivation", "document_id", c.ID, "err", err)
			continue
		}
		reopened++
		w.Enqueue(c.ID)
	}
	if reopened > 0 {
		w.logger.Info("documents: re-opening documents that were uploaded while previews were off",
			"count", reopened)
	}
}

// settlePending resolves leftover pending rows when the worker is DISABLED. There is
// no one left to derive them, so they are recorded as having no preview — the honest
// answer, and the same outcome as an Office file with no converter configured.
//
// The plan is recomputed from the content type with previews OFF rather than settled
// flat to "none": an image or PDF whose THUMBNAIL was still pending previews
// perfectly well through /raw, and writing "none" over it would turn a viewable
// document into a download-only one.
//
// Only a preview_status that is ACTUALLY pending gets recomputed, though. This sweep
// is keyed on either status (PendingPreviewIDs), so it also picks up rows whose
// preview finished and whose thumbnail alone is outstanding — and for an Office
// document that means a converted PDF sitting in the bucket, ready, with a plan that
// recomputes to "none" the moment conversion is disabled. Recomputing it would throw
// away a preview that works, which is the very downgrade this comment is about.
func (w *PreviewWorker) settlePending(ctx context.Context) {
	ids, err := w.store.PendingPreviewIDs(ctx)
	if err != nil {
		w.logger.Error("documents: cannot list pending previews", "err", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	w.logger.Info("documents: previews are disabled, settling rows left pending",
		"count", len(ids))
	for _, id := range ids {
		sd, err := w.store.GetStoredDocumentAnyScope(ctx, w.store.db, id)
		if err != nil {
			w.logger.Error("documents: cannot load a pending document", "document_id", id, "err", err)
			continue
		}
		if sd == nil {
			continue // deleted in the meantime
		}
		kind, status := sd.PreviewKind, sd.PreviewStatus
		if status == previewPending {
			kind, status = previewPlanFor(sd.ContentType, false)
		}
		// thumbnail_status settles to "none" unconditionally: with no pool running,
		// nothing will ever derive one. An already-derived thumbnail is unaffected —
		// thumbnail_url is served off thumbnail_key, which SetPreviewResult preserves.
		w.finish(ctx, *sd, kind, status, thumbNone, nil, nil)
	}
}

// runJob isolates one job from the pool it runs in.
//
// The decoders reached from here (WebP, GIF, JPEG, PNG) parse ATTACKER-SUPPLIED
// bytes, and a panic in one of them does not merely fail a thumbnail: an unrecovered
// panic in a goroutine takes the WHOLE process down, todo, events, notes and the
// dashboard with it. httpx's recover middleware cannot help — this runs off the
// request path. This is the same class of harm scaleImageFile's pixel cap guards
// against, arriving by a different door.
//
// The row is then settled instead of being left pending, because requeuePending
// would otherwise feed the same bytes to the same decoder on the next boot: one
// crafted upload would become a permanent crash loop, breakable only by editing the
// database by hand.
func (w *PreviewWorker) runJob(ctx context.Context, id string) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		w.logger.Error("documents: preview job panicked, settling the document as failed",
			"document_id", id, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		w.settlePanicked(ctx, id)
	}()
	w.process(ctx, id)
}

// settlePanicked records a terminal outcome for a job that panicked. Only the
// DERIVATION failed: the original is untouched and still downloads, and an image or
// PDF still previews natively through /raw — so a preview that was already ready
// keeps its status and only the thumbnail is written off.
func (w *PreviewWorker) settlePanicked(parent context.Context, id string) {
	// The pool's context may already be cancelled (a panic racing a shutdown). This
	// write has to land anyway, or the crash loop survives the restart.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer cancel()
	sd, err := w.store.GetStoredDocumentAnyScope(ctx, w.store.db, id)
	if err != nil || sd == nil {
		if err != nil {
			w.logger.Error("documents: cannot load the document of a panicked job",
				"document_id", id, "err", err)
		}
		return
	}
	kind, status := sd.PreviewKind, sd.PreviewStatus
	if status == previewPending {
		status = previewFailed
	}
	w.finish(ctx, *sd, kind, status, thumbFailed, nil, nil)
}

// process derives whatever the document needs. It reads its own state from the DB
// rather than trusting the queued message, so a re-run is always consistent.
//
// ⚠ v9: every load in this worker is the UNSCOPED one, and deliberately so (leak
// table row 17). The worker is a background job with NO ACTOR — there is no
// session, so a viewer-scoped read would see only shared rows and every private
// upload would sit at `pending` forever, with no error anywhere to say why. Like
// the mirror and the reconciliation pass it operates on keys and bytes; nothing it
// reads reaches a response, and its log lines must never grow a title or filename.
func (w *PreviewWorker) process(parent context.Context, id string) {
	ctx, cancel := context.WithTimeout(parent, w.cfg.Timeout)
	defer cancel()

	sd, err := w.store.GetStoredDocumentAnyScope(ctx, w.store.db, id)
	if err != nil || sd == nil {
		if err != nil {
			w.logger.Error("documents: preview job cannot load its document", "document_id", id, "err", err)
		}
		return // deleted between enqueue and processing: nothing to do
	}

	// Both failures below return WITHOUT calling finish, which leaves the row's
	// pending status untouched so the next boot re-enqueues it. That is the whole
	// point of thumbnail_status: for an image or PDF, preview_status is already
	// 'ready' and would never bring the job back.
	dir, err := os.MkdirTemp(w.cfg.TempDir, "home-preview-*")
	if err != nil {
		w.logger.Error("documents: preview job cannot create a temp dir", "document_id", id, "err", err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// Pull the original down once; both the conversion and the thumbnail work from it.
	src := filepath.Join(dir, "original")
	if err := w.fetchObject(ctx, sd.StorageKey, src); err != nil {
		w.logger.Error("documents: preview job cannot read the original", "document_id", id, "err", err)
		return
	}

	switch {
	case isOffice(sd.ContentType):
		w.deriveOfficePreview(ctx, *sd, dir, src)
	case isPDF(sd.ContentType):
		w.derivePDFThumbnail(ctx, *sd, dir, src)
	case isImage(sd.ContentType):
		w.deriveImageThumbnail(ctx, *sd, dir, src)
	}
}

// The thumbnail helpers return the key plus the thumbnail_status to record, which
// separates the two ways a thumbnail can not happen:
//
//	thumbFailed  — terminal. A missing helper binary, an undecodable or oversized
//	               image, a PDF that rasters to nothing. Re-running would fail the
//	               same way, and each attempt re-downloads the original, so this is
//	               never retried.
//	thumbPending — transient. Object storage refused the write. Left pending so the
//	               next boot picks it up.

// deriveOfficePreview converts an Office file to PDF via Gotenberg, stores it as the
// document's preview object, and thumbnails its first page.
func (w *PreviewWorker) deriveOfficePreview(ctx context.Context, sd storedDocument, dir, src string) {
	if w.cfg.GotenbergURL == "" {
		// No converter configured: say so definitively rather than leaving the document
		// pending forever.
		w.finish(ctx, sd, previewKindNone, previewNone, thumbNone, nil, nil)
		w.logger.Info("documents: no converter configured, document stays download-only", "document_id", sd.ID)
		return
	}
	pdfPath := filepath.Join(dir, "preview.pdf")
	if err := w.convertToPDF(ctx, sd.OriginalFilename, src, pdfPath); err != nil {
		// A converter that was unreachable or unhealthy says nothing about the DOCUMENT,
		// and "failed" is permanent here — requeuePending only ever revisits rows still
		// pending, so a document uploaded during a ten-second sidecar restart would carry
		// "náhled se nepodařilo vytvořit" for the rest of its life. Leave it pending
		// instead, exactly as a shutdown does, and let the next boot convert it.
		if errors.Is(err, errRetryablePreview) {
			w.logger.Warn("documents: the converter was unreachable, leaving the preview pending for the next boot",
				"document_id", sd.ID, "err", err)
			return
		}
		w.logger.Warn("documents: Office→PDF conversion failed, document stays download-only",
			"document_id", sd.ID, "err", err)
		w.finish(ctx, sd, previewKindPDF, previewFailed, thumbFailed, nil, nil)
		return
	}
	previewKey := PreviewKey(sd.ID)
	if err := w.putFile(ctx, previewKey, pdfPath, "application/pdf"); err != nil {
		// Storage, not the pipeline — the same call the thumbnail treats as retryable, and
		// the conversion that produced these bytes is repeatable.
		w.logger.Warn("documents: storing the preview PDF failed, leaving it pending",
			"document_id", sd.ID, "err", err)
		return
	}
	// A thumbnail is a bonus: its failure must not downgrade a working preview, so the
	// preview is recorded READY either way. A transient thumbnail failure re-runs this
	// whole conversion at the next boot — wasteful, but the keys are immutable, so the
	// re-derived preview overwrites itself with identical bytes.
	thumbKey, thumbStatus := w.thumbnailFromPDF(ctx, sd, dir, pdfPath)
	w.finish(ctx, sd, previewKindPDF, previewReady, thumbStatus, &previewKey, thumbKey)
}

// derivePDFThumbnail thumbnails a PDF that already previews natively.
func (w *PreviewWorker) derivePDFThumbnail(ctx context.Context, sd storedDocument, dir, src string) {
	thumbKey, thumbStatus := w.thumbnailFromPDF(ctx, sd, dir, src)
	w.finish(ctx, sd, previewKindNative, previewReady, thumbStatus, nil, thumbKey)
}

// deriveImageThumbnail scales a raster image down and stores it as a WebP thumbnail.
func (w *PreviewWorker) deriveImageThumbnail(ctx context.Context, sd storedDocument, dir, src string) {
	pngPath := filepath.Join(dir, "thumb.png")
	if err := w.scaleImageFile(src, pngPath); err != nil {
		w.logger.Warn("documents: image thumbnail failed", "document_id", sd.ID, "err", err)
		w.finish(ctx, sd, previewKindNative, previewReady, thumbFailed, nil, nil)
		return
	}
	thumbKey, thumbStatus := w.encodeAndStoreThumb(ctx, sd.ID, dir, pngPath)
	w.finish(ctx, sd, previewKindNative, previewReady, thumbStatus, nil, thumbKey)
}

// thumbnailFromPDF rasters a PDF's first page and stores it as a WebP thumbnail.
// A nil key means no thumbnail: the UI falls back to a type icon, which is a fine
// outcome, and the returned status says whether it is worth trying again.
func (w *PreviewWorker) thumbnailFromPDF(ctx context.Context, sd storedDocument, dir, pdfPath string) (*string, string) {
	prefix := filepath.Join(dir, "page")
	// pdftoppm appends "-1.png" to the prefix for the first page.
	cmd := exec.CommandContext(ctx, w.cfg.PdftoppmPath,
		"-f", "1", "-l", "1", "-png", "-scale-to", strconv.Itoa(w.cfg.ThumbMaxPx), pdfPath, prefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		w.logger.Warn("documents: pdftoppm unavailable or failed, skipping the thumbnail",
			"document_id", sd.ID, "err", err, "output", trimOutput(out))
		return nil, thumbFailed
	}
	page := prefix + "-1.png"
	if _, err := os.Stat(page); err != nil {
		// Some poppler builds pad the page number ("page-01.png").
		matches, _ := filepath.Glob(prefix + "-*.png")
		if len(matches) == 0 {
			w.logger.Warn("documents: pdftoppm produced no page image", "document_id", sd.ID)
			return nil, thumbFailed
		}
		page = matches[0]
	}
	return w.encodeAndStoreThumb(ctx, sd.ID, dir, page)
}

// encodeAndStoreThumb converts a PNG to WebP with cwebp and stores it.
func (w *PreviewWorker) encodeAndStoreThumb(ctx context.Context, documentID, dir, pngPath string) (*string, string) {
	webpPath := filepath.Join(dir, "thumb.webp")
	cmd := exec.CommandContext(ctx, w.cfg.CwebpPath, "-quiet", "-q", "80", pngPath, "-o", webpPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		w.logger.Warn("documents: cwebp unavailable or failed, skipping the thumbnail",
			"document_id", documentID, "err", err, "output", trimOutput(out))
		return nil, thumbFailed
	}
	key := ThumbnailKey(documentID)
	if err := w.putFile(ctx, key, webpPath, "image/webp"); err != nil {
		// Storage, not the pipeline: retryable, so leave it pending for the next boot.
		w.logger.Warn("documents: storing the thumbnail failed, leaving it pending",
			"document_id", documentID, "err", err)
		return nil, thumbPending
	}
	return &key, thumbReady
}

// finish records the outcome and tells open clients, so a just-uploaded row swaps
// its skeleton for a thumbnail without a manual refresh (FR-DOC9 / §11).
func (w *PreviewWorker) finish(ctx context.Context, sd storedDocument, kind, status, thumbStatus string, previewKey, thumbKey *string) {
	// A shutdown cancels the job's context mid-conversion, which surfaces as a failed
	// step — but it is not a failure of the DOCUMENT. Committing "failed" here would
	// be permanent: requeuePending only re-enqueues rows still "pending", so the next
	// boot would never retry and the document would sit without a preview forever.
	// Leave the row pending instead and let the restart pick it up.
	//
	// Only a cancellation counts. The per-job timeout (DeadlineExceeded) and a
	// converter error are genuine failures and still record as such.
	if (status == previewFailed || thumbStatus == thumbFailed) && errors.Is(ctx.Err(), context.Canceled) {
		w.logger.Info("documents: preview interrupted by shutdown, leaving it pending for the next boot",
			"document_id", sd.ID)
		return
	}
	// The write uses a fresh context: the job's own context may be near its timeout,
	// and losing the RESULT of a completed conversion would be the worst outcome.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := w.store.SetPreviewResult(writeCtx, w.store.db, sd.ID, kind, status, thumbStatus, previewKey, thumbKey); err != nil {
		w.logger.Error("documents: recording the preview result failed", "document_id", sd.ID, "err", err)
		return
	}
	payload := map[string]any{
		"id":             sd.ID,
		"preview_kind":   kind,
		"preview_status": status,
	}
	if thumbKey != nil {
		payload["thumbnail_url"] = thumbnailURL(sd.ID)
	}
	event := "document.preview_ready"
	if status == previewFailed {
		event = "document.preview_failed"
	}
	// No request context here (this is a background job), so the push carries no
	// client origin — every tab treats it as a change made elsewhere.
	w.notify(writeCtx, event, payload)
}

// errRetryablePreview marks a step that failed for a reason OUTSIDE the document —
// the converter restarting, a DNS blip, a dropped connection. Callers leave such a
// row pending instead of writing it off, because the same bytes through the same
// pipeline will succeed once the sidecar is back.
var errRetryablePreview = errors.New("documents: retryable preview failure")

// convertToPDF posts the file to Gotenberg's LibreOffice route and writes the
// returned PDF to dst.
//
// Its errors are split into two classes, which is the whole reason for
// errRetryablePreview: TRANSPORT trouble and a 5xx are the converter's problem and
// come back wrapped as retryable, while a 4xx is Gotenberg refusing THIS file — a
// verdict that will not change on the next boot, so it stays terminal.
func (w *PreviewWorker) convertToPDF(ctx context.Context, filename, src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	// Gotenberg wants a multipart body; stream the file into it through a pipe so a
	// large document is never held in memory twice.
	pr, pw := io.Pipe()
	// The writer goroutine below blocks in io.Copy until something reads pr, and the
	// only reader is the request body. On any path that returns before client.Do
	// hands pr to net/http, closing the reader is what unblocks it — defer f.Close()
	// cannot, since the goroutine is stuck WRITING to the pipe, not reading the file.
	// Closing twice is a no-op, so this stays correct once net/http owns the body.
	defer func() { _ = pr.Close() }()
	mw := multipart.NewWriter(pw)
	go func() {
		var writeErr error
		defer func() { _ = pw.CloseWithError(writeErr) }()
		part, err := mw.CreateFormFile("files", safeConvertName(filename))
		if err != nil {
			writeErr = err
			return
		}
		if _, err := io.Copy(part, f); err != nil {
			writeErr = err
			return
		}
		writeErr = mw.Close()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		w.cfg.GotenbergURL+"/forms/libreoffice/convert", pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	client := w.cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: w.cfg.Timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		// A cancelled or timed-out job is NOT this: finish() already leaves a cancelled
		// one pending, and the per-job timeout is a genuine failure of this document —
		// a file too big to convert inside Timeout is too big on the next boot too.
		if ctx.Err() != nil {
			return err
		}
		return fmt.Errorf("%w: %v", errRetryablePreview, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if resp.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("%w: gotenberg returned %d: %s", errRetryablePreview, resp.StatusCode, trimOutput(body))
		}
		return fmt.Errorf("gotenberg returned %d: %s", resp.StatusCode, trimOutput(body))
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, resp.Body); err != nil {
		// The conversion itself worked; the response was cut off on the way back.
		if ctx.Err() != nil {
			return err
		}
		return fmt.Errorf("%w: %v", errRetryablePreview, err)
	}
	return out.Sync()
}

// safeConvertName keeps the extension (Gotenberg picks its converter from it) while
// stripping any path components from the client-supplied filename.
func safeConvertName(filename string) string {
	base := filepath.Base(filepath.FromSlash(filename))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "document"
	}
	return base
}

// scaleImageFile decodes an image, scales it so its longest edge is ThumbMaxPx (never
// upscaling), and writes a PNG for cwebp to encode.
//
// The header is read FIRST, and an image whose pixel count exceeds MaxImagePixels is
// refused without ever being decoded. MaxUploadBytes bounds the file, which bounds
// nothing here: compression ratio is unlimited, so a ~5 MB PNG can be 30000×30000 and
// decode into ~3.6 GB of RGBA. Workers run in the app process, so that allocation
// would not "fail the thumbnail" — it would OOM-kill the whole backend, taking todo,
// events, notes and the dashboard down with it. A 100 MP panorama reaches this
// honestly; it does not have to be an attack.
func (w *PreviewWorker) scaleImageFile(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	hdr, _, err := image.DecodeConfig(f)
	if err != nil {
		return err
	}
	if hdr.Width <= 0 || hdr.Height <= 0 {
		return fmt.Errorf("image has no pixels")
	}
	if px := int64(hdr.Width) * int64(hdr.Height); px > int64(w.cfg.MaxImagePixels) {
		return fmt.Errorf("image is %dx%d (%d MP), over the %d MP decode limit",
			hdr.Width, hdr.Height, px/1e6, w.cfg.MaxImagePixels/1e6)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	if width <= 0 || height <= 0 {
		return fmt.Errorf("image has no pixels")
	}
	maxPx := w.cfg.ThumbMaxPx
	if width > maxPx || height > maxPx {
		if width >= height {
			height = height * maxPx / width
			width = maxPx
		} else {
			width = width * maxPx / height
			height = maxPx
		}
		if height < 1 {
			height = 1
		}
		if width < 1 {
			width = 1
		}
	}
	dstImg := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(dstImg, dstImg.Bounds(), img, b, xdraw.Over, nil)
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if err := png.Encode(out, dstImg); err != nil {
		return err
	}
	return out.Sync()
}

// fetchObject streams an object from storage into a local file.
func (w *PreviewWorker) fetchObject(ctx context.Context, key, dst string) error {
	body, _, err := w.blob.Get(ctx, key, nil)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(f, body); err != nil {
		return err
	}
	return f.Sync()
}

// putFile stores a local file as an object.
func (w *PreviewWorker) putFile(ctx context.Context, key, path, contentType string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	return w.blob.Put(ctx, key, f, st.Size(), contentType)
}

// trimOutput bounds a subprocess/HTTP error body before it reaches a log line.
func trimOutput(b []byte) string {
	const max = 400
	s := string(bytes.TrimSpace(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
