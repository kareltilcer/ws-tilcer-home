package documents

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The four content endpoints stream bytes from object storage THROUGH the backend
// (FR-DOC8). Two properties matter and are enforced here rather than anywhere else:
//
// HOUSEHOLD-ONLY (D33/D42): these are ordinary session-gated GETs. Storage stays
// private and no presigned URL is ever handed to the browser, so a leaked link is
// useless to anyone outside the household.
//
// UNTRUSTED-CONTENT ISOLATION (D48): the bytes are arbitrary user files served from
// home's own origin. The headers below are the isolation boundary, not decoration:
//   - X-Content-Type-Options: nosniff — the browser must honour our sniffed type and
//     never re-interpret a .txt as HTML.
//   - Content-Disposition: inline ONLY for the small safe set (PDF/image/plain
//     text/Markdown); attachment for everything else, so an uploaded .html or .svg
//     downloads instead of executing in home.tilcer.cz.
//   - Content-Security-Policy: sandbox — the response, if it ever does end up in a
//     document context, is sandboxed: no scripts, no same-origin access, no forms.
//
// Because the bytes are immutable (D41) the responses are aggressively cacheable:
// ETag = the SHA-256 checksum, and `immutable` with a one-year max-age. `private`
// keeps them out of shared caches — the content is household-only.
type contentMode int

const (
	contentRaw contentMode = iota
	contentDownload
	contentPreview
	contentThumbnail
)

// pdfSandbox is the relaxed CSP used for PDF responses that the SPA renders in a
// sandboxed <iframe>. Chrome's built-in PDF viewer is script-driven, so a bare
// `sandbox` directive blocks it and the user sees an empty frame. `allow-scripts`
// re-enables the viewer while the frame stays in an OPAQUE origin — deliberately no
// `allow-same-origin`, so the framed document still cannot touch home's cookies,
// storage, or DOM. Only ever applied to application/pdf, never to a type that could
// itself be an active document (HTML/SVG never reach this path: they are
// download-only, preview_kind "none").
//
// `allow-popups` is there for PHONES, and it is the token the placeholder actually
// wants. Chrome for Android does not render a PDF inside a frame at all: it draws its
// own placeholder — an "Open" button over the word "preview" — and that button calls
// window.open on the framed URL. Without this token the call is refused and the button
// is simply dead, which is the entire mobile preview experience. The refusal is only
// visible in a remote-debugging console, which is why this was first shipped as
// `allow-downloads` on the guess that the button saved the file; the device said
// otherwise, verbatim:
//
//	Blocked opening '…/preview?sandbox=2' in a new window because the request was
//	made in a sandboxed frame whose 'allow-popups' permission is not set.
//
// Deliberately NOT `allow-popups-to-escape-sandbox`: without it the popup INHERITS
// these flags, so the tab the button opens is sandboxed exactly as the frame was. That
// is also why it renders — the same document already opens under this header from the
// SPA's "Otevřít v novém okně" link, which is the one path that worked on a phone
// throughout.
//
// `allow-downloads` is kept, and its reason is NOT the button. It is what lets the
// reader save the PDF out of the tab the button opens (which inherits these flags) and
// out of the fallback link's tab. previewFilename below is what gives that save a
// usable name.
//
// Both this header and the iframe's own `sandbox` attribute must carry every token —
// they are ANDed, so relaxing one alone changes nothing (see PdfPreview in
// frontend/src/modules/documents/DocumentView.tsx). ⚠ CHANGING THIS VALUE IS NOT
// ENOUGH ON ITS OWN: bump previewSandboxVersion below in the same edit.
const pdfSandbox = "sandbox allow-scripts allow-downloads allow-popups"

// previewSandboxVersion is a CACHE KEY for the policy above, and the reason it lives
// HERE rather than in the SPA is the whole point of it. A shared /preview response is
// `private, immutable, max-age=31536000` (D41/D208), so nothing revalidates it for a
// year: a header tightened today would not reach a phone that framed the document
// last month. The only thing that reaches it sooner is a URL no cache has an answer
// for, so urlsFor stamps `?sandbox=<this>` onto the preview URL it hands the client.
// The handler never reads it — the query string is ignored end to end.
//
// Kept in the SPA instead — one language and one build away from the constant it
// busts — changing pdfSandbox and forgetting the bump is a one-line mistake that no
// test on either side can see, and whose only symptom is a security policy quietly
// not taking effect for a year. Next to the value it versions, the two cannot drift.
//
// ⚠ Bump on every change to pdfSandbox. It costs one re-fetch of a PDF the reader
// had cached, once. (2 → 3 when allow-popups was added; the bump is the only reason
// a phone that had already framed the document saw the new policy at all.)
const previewSandboxVersion = "3"

// strictSandbox is the default for everything else: no capabilities at all.
const strictSandbox = "sandbox"

// serveContent implements all four content endpoints; they differ only in which
// object they open and how the disposition is chosen.
//
// The stages are in the order they must run, and two of the orderings are the
// whole point of the endpoint rather than housekeeping: the viewer-scoped load
// comes before the ETag branch, and the response headers go out only after the
// bytes are in hand. Each stage below says why.
func (h *Handler) serveContent(w http.ResponseWriter, r *http.Request, mode contentMode) {
	id := chi.URLParam(r, "id")
	sd, ok := h.loadForContent(w, r, id)
	if !ok {
		return
	}

	key, contentType, err := h.resolveObject(*sd, mode)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if key == "" {
		// preview_kind "none": there is nothing to preview, which is a normal state for
		// a download-only file rather than an error.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	etag := contentETag(*sd, mode)
	// A matching validator means the browser already has these exact bytes — and
	// since they can never change, that answer is always correct.
	if httpx.IfNoneMatch(r, etag) {
		// Reached only after the viewer-scoped load above, which is the ordering that
		// matters: were the check to run before it, a second member holding a stale
		// ETag would get a 304 — "yes, and it hasn't changed" — for a document they
		// may not see.
		setContentCache(w, etag, sd.Visibility == visibilityPrivate)
		// The isolation policy rides on the 304 as well, and that is not decoration:
		// a cache updates a stored response's header fields from the 304 it
		// revalidated with, so a policy sent only alongside the bytes is frozen into
		// every cache entry at the moment it was first stored. Without this line a
		// private document — which revalidates on every single view (D208) — would
		// go on enforcing whatever sandbox it was first served with, and a tightening
		// after some future PDF-viewer CVE would reach nobody who already has the file.
		setIsolationPolicy(w, *sd, mode, contentType)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	rng, hasRange, ok := h.resolveRange(w, r, id, key)
	if !ok {
		return
	}

	var body io.ReadCloser
	var info blobstore.ObjInfo
	if hasRange {
		body, info, err = h.svc.blob.Get(r.Context(), key, &rng)
	} else {
		body, info, err = h.svc.blob.Get(r.Context(), key, nil)
	}
	if err != nil {
		h.writeObjectError(w, id, key, err)
		return
	}
	defer func() { _ = body.Close() }()

	writeContentHeaders(w, *sd, mode, contentType, etag)
	if hasRange {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rng.Offset, rng.Offset+rng.Length-1, info.Size))
		w.Header().Set("Content-Length", strconv.FormatInt(rng.Length, 10))
		w.WriteHeader(http.StatusPartialContent)
	} else if info.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	}

	if r.Method == http.MethodHead {
		return
	}
	// Stream straight through: the object may be 50 MB and must never be buffered.
	if _, err := io.Copy(w, body); err != nil {
		// The client went away (closed tab, cancelled download) — nothing to report,
		// and the header is already written so an error envelope is impossible.
		h.svc.logger.Debug("documents: streaming ended early", "document_id", id, "err", err)
	}
}

// loadForContent resolves the id to a document this viewer may read, writing the
// response and reporting false when it does not.
//
// ⚠ Viewer-scoped (leak table row 5). All four content endpoints share this one
// load and each is registered for GET *and* HEAD, so this is what makes a foreign
// private document 404 on eight routes — including the HEAD branch, which is live
// code and would otherwise be a HEAD-only existence oracle. A miss is
// indistinguishable from an unknown id (D180).
func (h *Handler) loadForContent(w http.ResponseWriter, r *http.Request, id string) (*storedDocument, bool) {
	sd, err := h.svc.Store().GetStoredDocument(r.Context(), h.svc.db, id, reqctx.ActorID(r.Context()))
	if err != nil {
		httpx.WriteError(w, err)
		return nil, false
	}
	// An archived (soft-deleted) document is gone as far as every reader is
	// concerned, and its permanent URL has to go with it: without this check a link
	// shared before the delete keeps streaming the bytes as if nothing happened,
	// while the tree, resolve, search and the pins all agree the document is gone.
	// The row and the objects survive untouched — a soft delete is reversible, so
	// only the reads stop, and restoring the document restores its URL with it.
	if sd == nil || sd.Archived {
		httpx.WriteError(w, httpx.ErrNotFound("document not found"))
		return nil, false
	}
	return sd, true
}

// contentETag is the cache validator: the content checksum for the original and a
// derived-object marker otherwise, so a preview PDF and its original never share
// a validator.
func contentETag(sd storedDocument, mode contentMode) string {
	switch mode {
	case contentPreview:
		if sd.PreviewKind == previewKindPDF {
			return `"` + sd.Checksum + `-preview"`
		}
	case contentThumbnail:
		return `"` + sd.Checksum + `-thumb"`
	}
	return `"` + sd.Checksum + `"`
}

// resolveRange reads the Range header and resolves it against the object's real
// size, writing the response and reporting false when it cannot be satisfied.
//
// The size lookup happens BEFORE the body is opened, so an unsatisfiable range is
// a clean 416 rather than a half-written 206. It also converts a suffix range
// ("bytes=-500") into the absolute window the store expects — storage APIs take
// an offset, not a negative one.
func (h *Handler) resolveRange(w http.ResponseWriter, r *http.Request, id, key string) (blobstore.ByteRange, bool, bool) {
	rng, hasRange, err := parseRange(r.Header.Get("Range"))
	if err != nil {
		httpx.WriteError(w, err)
		return rng, false, false
	}
	if !hasRange {
		return rng, false, true
	}
	stat, err := h.svc.blob.Stat(r.Context(), key)
	if err != nil {
		h.writeObjectError(w, id, key, err)
		return rng, true, false
	}
	start, length, ok := clampRange(rng, stat.Size)
	if !ok {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", stat.Size))
		httpx.WriteError(w, httpx.ErrRangeNotSatisfiable("requested range is outside the object"))
		return rng, true, false
	}
	return blobstore.ByteRange{Offset: start, Length: length}, true, true
}

// writeContentHeaders stamps the cache validators and the untrusted-content
// isolation headers this file's package comment describes.
//
// ⚠ IT IS CALLED ONLY WITH THE BYTES ALREADY IN HAND. Set any earlier these would
// ride along on every failure before it — and a 404/416/502 carrying a one-year
// `immutable` lifetime, on a URL whose content never changes, means one transient
// storage blip breaks that document in that browser for a year.
func writeContentHeaders(w http.ResponseWriter, sd storedDocument, mode contentMode, contentType, etag string) {
	setContentCache(w, etag, sd.Visibility == visibilityPrivate)
	setIsolationPolicy(w, sd, mode, contentType)
	w.Header().Set("Content-Type", mimeTypeHeader(contentType))
	w.Header().Set("Accept-Ranges", "bytes")
}

// setIsolationPolicy stamps the THREE headers that ARE the D48 policy, as opposed to
// the ones that merely describe the bytes. They are split out because they go on the
// 304 too — see the If-None-Match branch in serveContent for why a policy that only
// ever travels with the bytes cannot be changed afterwards.
//
// Content-Disposition is one of the three — the package comment above lists it as
// such — and it is here for exactly the reason the other two are. It is what makes an
// uploaded .html download instead of executing in home.tilcer.cz, so a stored
// `inline` that could never be tightened to `attachment` would be the same freeze the
// sandbox had. A 304 may carry it: RFC 9110 §15.4.5 permits representation metadata
// whose purpose is guiding a cache update, and Go strips only Content-Type,
// Content-Length and Transfer-Encoding from a 304.
func setIsolationPolicy(w http.ResponseWriter, sd storedDocument, mode contentMode, contentType string) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", dispositionFor(mode, contentType, sd.OriginalFilename))
	if isPDF(contentType) && mode == contentPreview {
		w.Header().Set("Content-Security-Policy", pdfSandbox)
		return
	}
	w.Header().Set("Content-Security-Policy", strictSandbox)
}

// setContentCache stamps the cache validators. Only ever called on a response that
// is certain to succeed (200/206/304): httpx.WriteError rewrites the body and
// Content-Type but leaves the rest of the header alone, so an error emitted after
// these were set would be cached as if it were the content.
//
// ⚠ Since v9 the policy DEPENDS ON VISIBILITY (D208, leak table row 19), and this
// is one of the few v9 changes that is invisible from inside the app — which is
// why the test asserts the HEADER rather than a behaviour.
//
// The shipped header was `private, immutable, max-age=31536000` unconditionally,
// and left alone it would have quietly defeated the whole feature: `private`
// excludes shared PROXIES, not the second person using the same laptop, and
// `immutable` suppresses revalidation for a year — so the new 404 would simply
// never execute. A private document would stay readable from disk cache for twelve
// months after the refusal shipped.
//
// `no-cache` for private items does NOT mean "do not cache". It means "revalidate
// before every reuse": the ownership check runs on every view, a second member
// gets 404, and the owner's repeat view of a 30 MB PDF is a 304 rather than a full
// re-download. `no-store` was considered and rejected — marginally stricter, at
// the cost of re-fetching every preview and thumbnail in the private tree on every
// render, which is the sort of tax that gets a header quietly removed later.
//
// The ETag rides on BOTH paths; it is what makes the 304 possible at all.
func setContentCache(w http.ResponseWriter, etag string, private bool) {
	w.Header().Set("ETag", etag)
	if private {
		w.Header().Set("Cache-Control", httpx.RevalidatedContentCache)
		return
	}
	w.Header().Set("Cache-Control", httpx.ImmutableContentCache)
}

// writeObjectError maps a storage failure onto a response. A missing object behind
// a live row is a DANGLING ROW: the user gets an honest 404 and the condition is
// logged for the daily reconciliation pass, which reports it rather than silently
// deleting anything.
func (h *Handler) writeObjectError(w http.ResponseWriter, id, key string, err error) {
	if errors.Is(err, blobstore.ErrNotFound) {
		h.svc.logger.Warn("documents: object missing for a live row (dangling)",
			"document_id", id, "key", key)
		httpx.WriteError(w, httpx.ErrNotFound("document content is not available"))
		return
	}
	h.svc.logger.Error("documents: reading the object failed", "document_id", id, "key", key, "err", err)
	httpx.WriteError(w, httpx.ErrBadGateway("document storage is unavailable"))
}

// resolveObject picks which object to stream and with what content type. It returns
// an empty key for "no content" (a 204 case) and an error for the states the spec
// maps to 409.
func (h *Handler) resolveObject(sd storedDocument, mode contentMode) (key, contentType string, err error) {
	switch mode {
	case contentRaw, contentDownload:
		return sd.StorageKey, sd.ContentType, nil

	case contentPreview:
		switch sd.PreviewKind {
		case previewKindNative:
			// PDF, image, or text: the original IS the preview.
			return sd.StorageKey, sd.ContentType, nil
		case previewKindPDF:
			switch sd.PreviewStatus {
			case previewReady:
				if sd.PreviewKey == nil || *sd.PreviewKey == "" {
					return "", "", httpx.ErrConflict("preview is not available")
				}
				return *sd.PreviewKey, "application/pdf", nil
			case previewPending:
				return "", "", httpx.ErrConflict("preview is still being generated")
			default: // failed
				return "", "", httpx.ErrConflict("preview generation failed — download the original instead")
			}
		default: // previewKindNone → 204, download-only
			return "", "", nil
		}

	case contentThumbnail:
		if sd.ThumbnailKey == nil || *sd.ThumbnailKey == "" {
			// No thumbnail was generated (unsupported type, or the helper binaries are
			// absent). The UI falls back to a type icon.
			return "", "", httpx.ErrNotFound("no thumbnail")
		}
		return *sd.ThumbnailKey, "image/webp", nil
	}
	return "", "", httpx.ErrInternal("unknown content mode")
}

// dispositionFor decides inline vs attachment (D48). /download is always an
// attachment; /raw is inline only for the safe set; a derived preview PDF and a
// thumbnail are ours, not user-authored, so they are inline.
func dispositionFor(mode contentMode, contentType, filename string) string {
	switch mode {
	case contentDownload:
		return "attachment; " + rfc5987(filename)
	case contentThumbnail:
		return "inline"
	case contentPreview:
		name := previewFilename(contentType, filename)
		if InlineSafe(contentType) {
			return "inline; " + rfc5987(name)
		}
		return "attachment; " + rfc5987(name)
	default: // contentRaw
		if InlineSafe(contentType) {
			return "inline; " + rfc5987(filename)
		}
		// Anything that could execute in home's origin downloads instead.
		return "attachment; " + rfc5987(filename)
	}
}

// previewFilename names the bytes /preview actually serves, which are NOT always the
// uploaded ones: a derived Office→PDF preview streams application/pdf out of a row
// whose original_filename still ends in .docx.
//
// The name matters because a preview is DOWNLOADED and not only rendered — Chrome for
// Android's in-frame placeholder hands the file to the platform viewer by saving it
// first, which is what pdfSandbox's allow-downloads exists for. With a bare `inline`
// the browser has no name to use and falls back to the last path segment, so every
// document a phone opens this way lands in Downloads as `preview.pdf`, then
// `preview (1).pdf`, and the reader cannot tell one from another. Handing back
// "Podmínky.docx" for a PDF would be the opposite mistake, so a derived preview keeps
// the stem and takes the .pdf.
func previewFilename(contentType, filename string) string {
	if !isPDF(contentType) || strings.HasSuffix(strings.ToLower(filename), ".pdf") {
		return filename
	}
	if i := strings.LastIndexByte(filename, '.'); i > 0 {
		return filename[:i] + ".pdf"
	}
	return filename + ".pdf"
}

// parseRange parses a single-range `bytes=` header. Multi-range requests are not
// supported (they need a multipart/byteranges response and no real client needs one
// here), so they are answered as a full 200 rather than an error.
func parseRange(header string) (rng blobstore.ByteRange, ok bool, err error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return blobstore.ByteRange{}, false, nil
	}
	if !strings.HasPrefix(header, "bytes=") {
		return blobstore.ByteRange{}, false, nil
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if strings.Contains(spec, ",") {
		return blobstore.ByteRange{}, false, nil // multi-range: serve the whole object
	}
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return blobstore.ByteRange{}, false, httpx.ErrRangeNotSatisfiable("malformed Range header")
	}
	startStr, endStr := strings.TrimSpace(spec[:dash]), strings.TrimSpace(spec[dash+1:])

	if startStr == "" {
		// Suffix range ("bytes=-500"): the last N bytes. Signalled with a negative
		// offset and resolved against the object size in clampRange.
		n, convErr := strconv.ParseInt(endStr, 10, 64)
		if convErr != nil || n <= 0 {
			return blobstore.ByteRange{}, false, httpx.ErrRangeNotSatisfiable("malformed Range header")
		}
		return blobstore.ByteRange{Offset: -n}, true, nil
	}
	start, convErr := strconv.ParseInt(startStr, 10, 64)
	if convErr != nil || start < 0 {
		return blobstore.ByteRange{}, false, httpx.ErrRangeNotSatisfiable("malformed Range header")
	}
	if endStr == "" {
		return blobstore.ByteRange{Offset: start}, true, nil // open-ended
	}
	end, convErr := strconv.ParseInt(endStr, 10, 64)
	if convErr != nil || end < start {
		return blobstore.ByteRange{}, false, httpx.ErrRangeNotSatisfiable("malformed Range header")
	}
	return blobstore.ByteRange{Offset: start, Length: end - start + 1}, true, nil
}

// clampRange resolves a parsed range against the real object size, returning the
// concrete start and length for the Content-Range header.
func clampRange(rng blobstore.ByteRange, size int64) (start, length int64, ok bool) {
	if size <= 0 {
		return 0, 0, false
	}
	if rng.Offset < 0 { // suffix range
		n := -rng.Offset
		if n > size {
			n = size
		}
		return size - n, n, true
	}
	if rng.Offset >= size {
		return 0, 0, false
	}
	length = rng.Length
	if length <= 0 || rng.Offset+length > size {
		length = size - rng.Offset
	}
	return rng.Offset, length, true
}
