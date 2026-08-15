package notes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
)

// Inline images for Poznámky. Pasted/dropped images stream to object storage under
// note-images/{id} and body_md holds only a small `![](/api/notes/images/{id})`
// reference — so a note body stays small (the base64-inline path that blew the 1 MiB
// API cap and froze the editor is gone). The pattern mirrors the documents module's
// object-before-row upload and served-through-the-backend content endpoint, but lean:
// one object per image, no previews/thumbnails/folders/titles.

// imageAllowedMIME is the fixed allowlist — the raster formats a browser renders
// inline. The type is SNIFFED from the bytes, never taken from the client (D48).
var imageAllowedMIME = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// imageSniffLen is how many leading bytes http.DetectContentType inspects.
const imageSniffLen = 512

// UploadImage streams one image to object storage and records its row, returning the
// reference the editor embeds. The ORDER is the contract (as in documents.Upload):
//
//  1. Stream through a hard size cap into a temp file, teeing a SHA-256 + counter.
//     Over the cap → 413 with nothing stored.
//  2. Sniff the type from the leading bytes; outside the image allowlist → 415.
//  3. Put the object. A storage failure → 502 and NO database row.
//  4. Insert the note_images row (owner = the note being edited). A failure best-
//     effort deletes the just-written object.
//
// A crash between 3 and 4 leaves an orphan object the reconciliation pass sweeps; the
// reverse (a row with no bytes) must never happen, which is why the object goes first.
func (s *Service) UploadImage(ctx context.Context, noteID string, file io.Reader) (*NoteImageUploadResult, error) {
	if s.blob == nil {
		return nil, httpx.ErrInternal("image storage is not configured")
	}
	// The owning note must exist and be live — an image can't belong to a phantom
	// (archived) or missing note. GetNote does not filter archived, so check it here.
	n, err := s.store.GetNote(ctx, s.db, noteID)
	if err != nil {
		return nil, err
	}
	if n == nil || n.Archived {
		return nil, httpx.ErrNotFound("note not found")
	}

	id := idgen.New()
	key := NoteImageKey(id)

	tmp, err := os.CreateTemp(s.imgOpts.TempDir, "note-image-*")
	if err != nil {
		return nil, httpx.ErrInternal("cannot buffer the upload: " + err.Error())
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	// 1. Stream with the cap. Reading max+1 detects "over the cap" without trusting
	// Content-Length (the client can lie or omit it).
	hasher := sha256.New()
	limited := io.LimitReader(file, s.imgOpts.MaxUploadBytes+1)
	size, err := io.Copy(io.MultiWriter(tmp, hasher), limited)
	if err != nil {
		return nil, httpx.ErrBadRequest("reading the upload failed: " + err.Error())
	}
	if size > s.imgOpts.MaxUploadBytes {
		return nil, httpx.ErrTooLarge(fmt.Sprintf("image exceeds the %d MB limit", s.imgOpts.MaxUploadBytes>>20))
	}
	if size == 0 {
		return nil, httpx.ErrUnprocessable("the uploaded image is empty")
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))

	// 2. Sniff from the stored bytes.
	head := make([]byte, imageSniffLen)
	nRead, err := tmp.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, httpx.ErrInternal("cannot inspect the upload: " + err.Error())
	}
	contentType := sniffImageType(head[:nRead])
	if !imageAllowedMIME[contentType] {
		return nil, httpx.ErrUnsupportedMedia(fmt.Sprintf("content type %q is not an allowed image", contentType))
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, httpx.ErrInternal("cannot rewind the upload: " + err.Error())
	}

	// 3. Object first. Nothing is written to the database until this returns.
	if err := s.blob.Put(ctx, key, tmp, size, contentType); err != nil {
		s.logger.Error("notes: storing an image object failed — nothing committed", "key", key, "err", err)
		return nil, httpx.ErrBadGateway("image storage is unavailable")
	}

	// 4. Row. A failure orphans the object → delete it now rather than wait for the
	// reconciliation pass.
	if err := s.store.InsertNoteImage(ctx, s.db, id, noteID, contentType, size, checksum, actorID(ctx)); err != nil {
		s.purgeImageObjects(ctx, []string{id})
		return nil, err
	}

	return &NoteImageUploadResult{
		ID:          id,
		URL:         noteImageURL(id),
		ContentType: contentType,
		ByteSize:    size,
	}, nil
}

// sniffImageType classifies the leading bytes. http.DetectContentType covers all
// four allowed raster types (PNG/JPEG/GIF/WEBP) and returns a bare type for images;
// anything else comes back as text/* or application/octet-stream and the allowlist
// then rejects it.
func sniffImageType(head []byte) string {
	ct := http.DetectContentType(head)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	return ct
}

// ---- HTTP handlers ----

// maxImageParts bounds how many multipart fields the reader consumes looking for the
// "file" part, so an authenticated client can't hold the request open streaming
// endless tiny fields. The frontend sends exactly one part.
const maxImageParts = 8

// maxNonFilePartBytes bounds a single non-"file" field. Only the "file" part carries
// image bytes and is size-capped downstream; a stray field is a tiny form value, so
// draining one to reach the next boundary must never stream unbounded bytes.
const maxNonFilePartBytes = 1 << 20 // 1 MiB — orders of magnitude past any real field

// uploadImage handles POST /api/notes/{id}/images: a multipart/form-data body with a
// single "file" part. RequireWrite-gated at the router.
func (h *Handler) uploadImage(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable("expected a multipart/form-data upload"))
		return
	}
	noteID := chi.URLParam(r, "id")
	for parts := 0; ; parts++ {
		if parts >= maxImageParts {
			httpx.WriteError(w, httpx.ErrUnprocessable("too many multipart fields before the \"file\" part"))
			return
		}
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			httpx.WriteError(w, httpx.ErrUnprocessable("the multipart body has no \"file\" part"))
			return
		}
		if err != nil {
			httpx.WriteError(w, httpx.ErrUnprocessable("malformed multipart body: "+err.Error()))
			return
		}
		if part.FormName() != "file" {
			// Reaching the next boundary consumes this field; cap how much it can stream
			// so an unbounded non-"file" part can't hold the request open (only "file" is
			// size-capped downstream).
			if n, _ := io.Copy(io.Discard, io.LimitReader(part, maxNonFilePartBytes+1)); n > maxNonFilePartBytes {
				_ = part.Close()
				httpx.WriteError(w, httpx.ErrUnprocessable("a non-\"file\" multipart field is too large"))
				return
			}
			_ = part.Close()
			continue
		}
		res, err := h.svc.UploadImage(r.Context(), noteID, part)
		_ = part.Close()
		respond(w, http.StatusCreated, res, err)
		return
	}
}

// getImage handles GET /api/notes/images/{imageId}: it streams the object THROUGH the
// backend so it stays session-gated and same-origin (D33/D42) — no presigned URL ever
// reaches the browser. The bytes are immutable (id-based key), so the response is
// aggressively cacheable with the checksum as ETag; nosniff + a `sandbox` CSP are the
// untrusted-content isolation boundary (D48), matching the documents content endpoint.
func (h *Handler) getImage(w http.ResponseWriter, r *http.Request) {
	if h.svc.blob == nil {
		httpx.WriteError(w, httpx.ErrNotFound("image not found"))
		return
	}
	id := chi.URLParam(r, "imageId")
	im, err := h.svc.store.GetNoteImage(r.Context(), h.svc.db, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if im == nil {
		httpx.WriteError(w, httpx.ErrNotFound("image not found"))
		return
	}

	etag := `"` + im.Checksum + `"`
	if httpx.IfNoneMatch(r, etag) {
		setImageCache(w, etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	body, info, err := h.svc.blob.Get(r.Context(), NoteImageKey(id), nil)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			h.svc.logger.Warn("notes: image object missing for a live row (dangling)", "image_id", id)
			httpx.WriteError(w, httpx.ErrNotFound("image content is not available"))
			return
		}
		h.svc.logger.Error("notes: reading an image object failed", "image_id", id, "err", err)
		httpx.WriteError(w, httpx.ErrBadGateway("image storage is unavailable"))
		return
	}
	defer func() { _ = body.Close() }()

	// Headers only now the bytes are in hand — set earlier they would ride along on a
	// 404/502 and cache a one-year `immutable` lifetime onto an error.
	setImageCache(w, etag)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Content-Type", im.ContentType)
	w.Header().Set("Content-Disposition", "inline")
	if info.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	}
	if _, err := io.Copy(w, body); err != nil {
		// Client went away (closed tab) — the header is already written, so nothing to
		// report.
		h.svc.logger.Debug("notes: streaming an image ended early", "image_id", id, "err", err)
	}
}

func setImageCache(w http.ResponseWriter, etag string) {
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", httpx.ImmutableContentCache)
}
