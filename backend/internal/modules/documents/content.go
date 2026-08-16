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
const pdfSandbox = "sandbox allow-scripts"

// strictSandbox is the default for everything else: no capabilities at all.
const strictSandbox = "sandbox"

// serveContent implements all four content endpoints; they differ only in which
// object they open and how the disposition is chosen.
func (h *Handler) serveContent(w http.ResponseWriter, r *http.Request, mode contentMode) {
	id := chi.URLParam(r, "id")
	sd, err := h.svc.Store().GetStoredDocument(r.Context(), h.svc.db, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	// An archived (soft-deleted) document is gone as far as every reader is
	// concerned, and its permanent URL has to go with it: without this check a link
	// shared before the delete keeps streaming the bytes as if nothing happened,
	// while the tree, resolve, search and the pins all agree the document is gone.
	// The row and the objects survive untouched — a soft delete is reversible, so
	// only the reads stop, and restoring the document restores its URL with it.
	if sd == nil || sd.Archived {
		httpx.WriteError(w, httpx.ErrNotFound("document not found"))
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

	// Cache validators. The ETag is the content checksum for the original and a
	// derived-object marker otherwise, so a preview PDF and its original never share
	// a validator.
	etag := `"` + sd.Checksum + `"`
	switch mode {
	case contentPreview:
		if sd.PreviewKind == previewKindPDF {
			etag = `"` + sd.Checksum + `-preview"`
		}
	case contentThumbnail:
		etag = `"` + sd.Checksum + `-thumb"`
	}

	// A matching validator means the browser already has these exact bytes — and
	// since they can never change, that answer is always correct.
	if httpx.IfNoneMatch(r, etag) {
		setImmutableCache(w, etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	rng, hasRange, err := parseRange(r.Header.Get("Range"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}

	// A Range request is resolved against the object's real size BEFORE the body is
	// opened, so an unsatisfiable range is a clean 416 rather than a half-written
	// 206. It also converts a suffix range ("bytes=-500") into the absolute window
	// the store expects — storage APIs take an offset, not a negative one.
	var start, length int64
	if hasRange {
		stat, serr := h.svc.blob.Stat(r.Context(), key)
		if serr != nil {
			h.writeObjectError(w, id, key, serr)
			return
		}
		var ok bool
		start, length, ok = clampRange(rng, stat.Size)
		if !ok {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", stat.Size))
			httpx.WriteError(w, httpx.ErrRangeNotSatisfiable("requested range is outside the object"))
			return
		}
		rng = blobstore.ByteRange{Offset: start, Length: length}
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

	// Only NOW, with the bytes actually in hand, do the response headers go out. Set
	// any earlier they would ride along on every failure above — and a 404/416/502
	// carrying a one-year `immutable` lifetime on a URL whose content never changes
	// means one transient storage blip breaks that document in that browser for a
	// year.
	setImmutableCache(w, etag)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", mimeTypeHeader(contentType))
	if isPDF(contentType) && mode == contentPreview {
		w.Header().Set("Content-Security-Policy", pdfSandbox)
	} else {
		w.Header().Set("Content-Security-Policy", strictSandbox)
	}
	w.Header().Set("Content-Disposition", dispositionFor(mode, contentType, sd.OriginalFilename))
	w.Header().Set("Accept-Ranges", "bytes")

	if hasRange {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, info.Size))
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
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

// setImmutableCache stamps the cache validators. Only ever called on a response
// that is certain to succeed (200/206/304): httpx.WriteError rewrites the body and
// Content-Type but leaves the rest of the header alone, so an error emitted after
// these were set would be cached as if it were the content.
func setImmutableCache(w http.ResponseWriter, etag string) {
	w.Header().Set("ETag", etag)
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
		if InlineSafe(contentType) {
			return "inline"
		}
		return "attachment; " + rfc5987(filename)
	default: // contentRaw
		if InlineSafe(contentType) {
			return "inline; " + rfc5987(filename)
		}
		// Anything that could execute in home's origin downloads instead.
		return "attachment; " + rfc5987(filename)
	}
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
