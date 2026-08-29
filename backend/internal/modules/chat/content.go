package chat

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Attachment delivery (FR-V10-7, leak rows 5 and 6).
//
// ⚠ THE MEMBERSHIP LOAD RUNS BEFORE THE `If-None-Match` BRANCH. ALWAYS. That single
// ordering is leak row 5, it has its own acceptance criterion, and it is a bug v9
// SHIPPED in `documents` and caught after the fact (§V9-12) — a stale ETag earned a
// non-member a **304**, which says *"yes, and it hasn't changed"* about something
// they may not read. The conditional branch below is physically after the load for
// that reason, and moving it up is not an optimisation.
//
// ⚠ `Cache-Control` IS `private, no-cache, must-revalidate` AND NEVER `immutable`
// (D229). v9's shared branch can be `immutable` because shared bytes stay shared;
// CHAT MEMBERSHIP CAN BE REVOKED, and a year-long cache entry is a copy that
// outlives the revocation on a device nobody can reach. The ETag is kept on both the
// 200 and the 304, so a repeat view is still a cheap revalidation rather than a
// re-download — the cost of the decision is one conditional request, not the bytes.
//
// ⚠ AND 404 ON GET **AND** HEAD. A HEAD-only oracle is still an oracle: it answers
// "does this attachment exist" for a conversation the caller may not open, which is
// the question D217 closes everywhere else in the module.

// strictSandbox is the CSP every chat object carries.
//
// The bytes are arbitrary user files served from home's own origin, and unlike
// `documents` this module has no PDF-viewer exception to make: a PDF opens in the
// browser's own viewer as a top-level navigation, not in an app-rendered iframe
// (D227 — chat has no preview pipeline), so nothing here ever needs allow-scripts.
const strictSandbox = "sandbox"

type contentMode int

const (
	contentRaw contentMode = iota
	contentThumbnail
)

// serveAttachment streams one attachment's bytes or its thumbnail.
func (h *Handler) serveAttachment(w http.ResponseWriter, r *http.Request, id string, mode contentMode) {
	if h.svc.blob == nil {
		httpx.WriteError(w, httpx.ErrInternal("chat attachment storage is not configured"))
		return
	}
	actor := reqctx.ActorID(r.Context())
	if actor == "" {
		httpx.WriteError(w, httpx.ErrUnauthorized(""))
		return
	}

	// 1. MEMBERSHIP, THE FLOOR, THE KOŠ AND THE TOMBSTONE — before anything else.
	att, err := h.svc.store.AttachmentForViewer(r.Context(), h.svc.db, actor, id)
	if err != nil {
		httpx.WriteError(w, mapScopeErr(err))
		return
	}
	// A `removed` attachment has no bytes and a `moved` one's bytes are Dokumenty's.
	// Both are 404 here rather than a redirect: the bubble already knows where a moved
	// file lives (document_path) and following a redirect out of this module would
	// serve household-readable bytes from a member-restricted route.
	if att.State != stateLive {
		httpx.WriteError(w, notFound())
		return
	}

	key := att.StorageKey
	contentType := att.ContentType
	etag := `"` + att.Checksum + `"`
	if mode == contentThumbnail {
		if att.Kind != kindImage || !att.ThumbnailKey.Valid || att.ThumbnailKey.String == "" {
			// Video and file kinds have none — v10 generates no poster frame and runs
			// no preview pipeline (D227) — and so does an image whose encode failed.
			httpx.WriteError(w, notFound())
			return
		}
		key = att.ThumbnailKey.String
		contentType = "image/webp"
		// ⚠ A DISTINCT ETAG. The thumbnail is a different object with the same
		// checksum column behind it, so sharing the tag would let a browser holding
		// the original answer a conditional thumbnail request with a 304 and render
		// the full image in a 96 px box — or the reverse.
		etag = `"` + att.Checksum + `-thumb"`
	}

	// 2. ONLY NOW the conditional branch. Everything above is the access rule; a 304
	// is an answer about content and may only be given to somebody entitled to it.
	if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, etag) {
		setChatCache(w, etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	var (
		rng      blobstore.ByteRange
		hasRange bool
		start    int64
		length   int64
	)
	if raw := r.Header.Get("Range"); raw != "" && mode == contentRaw {
		var ok bool
		start, length, ok = parseByteRange(raw, att.ByteSize)
		if !ok {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", att.ByteSize))
			httpx.WriteError(w, httpx.ErrRangeNotSatisfiable("requested range is outside the object"))
			return
		}
		rng = blobstore.ByteRange{Offset: start, Length: length}
		hasRange = true
	}

	var (
		body io.ReadCloser
		info blobstore.ObjInfo
	)
	if hasRange {
		body, info, err = h.svc.blob.Get(r.Context(), key, &rng)
	} else {
		body, info, err = h.svc.blob.Get(r.Context(), key, nil)
	}
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			// The row says live and the object is gone: an inconsistency the storage
			// page reports. 404 to the caller, because there is nothing to serve.
			h.svc.logger.Warn("chat: attachment object is missing", "attachment", id, "key", key)
			httpx.WriteError(w, notFound())
			return
		}
		h.svc.logger.Error("chat: reading an attachment failed", "attachment", id, "key", key, "err", err)
		httpx.WriteError(w, httpx.ErrBadGateway("Úložiště souborů není dostupné."))
		return
	}
	defer func() { _ = body.Close() }()

	// The headers go out only with the bytes in hand: set earlier they would ride
	// along on every failure above, and a 404 carrying cache validators is a 404 a
	// browser can serve itself later.
	setChatCache(w, etag)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", strictSandbox)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", dispositionFor(r, mode, contentType, att.OriginalFilename))
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
	// Straight through: an attachment may be 50 MB and must never be buffered.
	if _, err := io.Copy(w, body); err != nil {
		h.svc.logger.Debug("chat: streaming ended early", "attachment", id, "err", err)
	}
}

// setChatCache stamps the validators, and it has exactly one policy.
//
// ⚠ THERE IS NO VISIBILITY BRANCH HERE, unlike documents/content.go's (D208). Every
// chat object is member-restricted and membership is revocable, so every one of them
// gets the revalidating policy. A branch would be a place for a future "optimisation"
// to put `immutable` back on the half somebody thinks is safe.
func setChatCache(w http.ResponseWriter, etag string) {
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache, must-revalidate")
}

// dispositionFor decides inline vs attachment.
//
// `?download=true` always forces an attachment; there is no separate `/download`
// path, because only one of the three kinds ever needs one (FR-V10-7).
func dispositionFor(r *http.Request, mode contentMode, contentType, filename string) string {
	// A thumbnail is OURS, not user-authored, so it is always inline.
	if mode == contentThumbnail {
		return "inline"
	}
	if queryBool(r, "download") || !inlineSafe(contentType) {
		return "attachment; " + rfc5987(filename)
	}
	return "inline; " + rfc5987(filename)
}

// rfc5987 encodes a filename for Content-Disposition. Czech filenames are not
// Latin-1, so the header carries both a sanitised ASCII fallback and the UTF-8
// `filename*` form that modern browsers prefer.
//
// ⚠ A THIRD COPY OF documents/http.go's ENCODER WOULD BE ONE TOO MANY, so this one
// escapes through net/url and keeps only the ASCII fallback by hand. `PathEscape`
// leaves `$&+:=@` unescaped, which RFC 5987's attr-char set does not include — so
// they are escaped afterwards rather than trusted. A filename that survives a
// header wrong is a download that saves under the wrong name; one that breaks the
// header is a response no browser parses.
func rfc5987(filename string) string {
	if filename == "" {
		filename = "soubor"
	}
	return `filename="` + asciiFallback(filename) + `"; filename*=UTF-8''` + attrEscape(filename)
}

// asciiFallback is the legacy `filename=` value for clients that ignore RFC 5987.
func asciiFallback(name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '\\' || r > 126 {
			return '_'
		}
		return r
	}, name)
	if ascii == "" {
		return "soubor"
	}
	return ascii
}

// attrEscape percent-encodes to RFC 5987's attr-char set.
func attrEscape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0x0f])
	}
	return b.String()
}

// etagMatches implements the If-None-Match comparison, weak-tag tolerant.
func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag {
			return true
		}
		if strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

// parseByteRange handles the single-range form, which is what a <video> element
// sends. A multi-range request falls back to the whole object rather than erroring:
// nothing in Home has ever needed one and refusing it would break a player that
// merely asked politely.
func parseByteRange(header string, size int64) (start, length int64, ok bool) {
	spec, found := strings.CutPrefix(strings.TrimSpace(header), "bytes=")
	if !found || strings.Contains(spec, ",") {
		return 0, 0, false
	}
	first, last, _ := strings.Cut(spec, "-")
	first, last = strings.TrimSpace(first), strings.TrimSpace(last)
	switch {
	case first == "" && last == "":
		return 0, 0, false
	case first == "":
		// A suffix range: the final N bytes.
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, n, true
	default:
		s, err := strconv.ParseInt(first, 10, 64)
		if err != nil || s < 0 || s >= size {
			return 0, 0, false
		}
		end := size - 1
		if last != "" {
			e, err := strconv.ParseInt(last, 10, 64)
			if err != nil || e < s {
				return 0, 0, false
			}
			if e < end {
				end = e
			}
		}
		return s, end - s + 1, true
	}
}
