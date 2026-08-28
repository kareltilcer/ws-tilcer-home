package chat

import (
	"net/http"
	"path/filepath"
	"strings"
)

// Content types for untrusted chat uploads (D48's rule, D227's three kinds).
//
// ⚠ THIS IS A SECOND COPY OF A DISCIPLINE `documents` ALREADY HAS, and it is a copy
// on purpose. internal/arch forbids `chat` from importing `documents`, and the two
// policies genuinely differ: Dokumenty refuses anything outside a configured
// allowlist, while chat accepts EVERYTHING and classifies it — a chat that refuses
// a file type is a chat somebody works around by emailing it. What must stay
// identical is the safety half: sniff from the bytes, never the client's claim, and
// serve inline only for a small set that cannot execute in home's origin.
//
// The three kinds (D227):
//
//	image  png · jpeg · gif · webp        original + thumb.webp + recorded dimensions
//	video  mp4 · webm · quicktime         original only, played inline, NO transcoding
//	file   anything else                  download; a PDF opens in the browser's viewer

const sniffLen = 512

// videoByExt refines an OPAQUE sniff into a video type.
//
// ⚠ IT EXISTS FOR THE IPHONE, and the iPhone is named in the spec (D227). Go's
// mp4 matcher checks the `ftyp` brand for "mp4", so a QuickTime file branded
// `qt  ` — which is what an iPhone `.mov` is — falls through to
// application/octet-stream and would be classified `file`: an inline video the
// household actually sends, rendered as a download link.
//
// ⚠ IT ONLY EVER REFINES AN ALREADY-OPAQUE TYPE, the same discipline
// documents/mime.go states for its Office map. The extension can turn "we could not
// tell" into "video"; it can never turn a sniffed text/html into something served
// inline, which is the direction that matters.
var videoByExt = map[string]string{
	".mov": "video/quicktime",
	".mp4": "video/mp4",
	".m4v": "video/mp4",
	".webm": "video/webm",
}

// sniffChatType determines the stored content type from the leading bytes.
func sniffChatType(head []byte, filename string) string {
	sniffed := http.DetectContentType(head)
	if baseType(sniffed) != "application/octet-stream" {
		return sniffed
	}
	if ct, ok := videoByExt[strings.ToLower(filepath.Ext(filename))]; ok {
		return ct
	}
	return sniffed
}

// kindFor classifies a sniffed type into one of the three kinds (D227).
func kindFor(contentType string) string {
	switch baseType(contentType) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return kindImage
	case "video/mp4", "video/webm", "video/quicktime":
		return kindVideo
	}
	return kindFile
}

// inlineSafeTypes is the ONLY set served with `Content-Disposition: inline`.
//
// ⚠ image/svg+xml, text/html and text/xml are ABSENT and must stay absent. An SVG
// is a scriptable document, not a bitmap, and serving one inline from
// home.tilcer.cz would run its script in home's origin with home's cookies. The
// same list, for the same reason, as documents/mime.go — plus the three video types
// chat plays inline, which are containers and not documents.
var inlineSafeTypes = map[string]bool{
	"application/pdf": true,
	"image/png":       true,
	"image/jpeg":      true,
	"image/gif":       true,
	"image/webp":      true,
	"video/mp4":       true,
	"video/webm":      true,
	"video/quicktime": true,
	"text/plain":      true,
	"text/markdown":   true,
}

func inlineSafe(contentType string) bool { return inlineSafeTypes[baseType(contentType)] }

// baseType strips any parameters ("text/plain; charset=utf-8" → "text/plain").
func baseType(ct string) string {
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		return strings.TrimSpace(ct[:i])
	}
	return strings.TrimSpace(ct)
}

// safeFilename strips path components from a client-supplied name.
//
// A multipart part can claim `../../etc/passwd`; nothing here builds a filesystem
// path from it — the storage key is derived from the attachment id (D42's rule) —
// but the name is echoed into a Content-Disposition header and into the epitaph,
// so it is cleaned at the boundary rather than at each use.
func safeFilename(name string) string {
	base := filepath.Base(filepath.FromSlash(strings.TrimSpace(name)))
	base = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, base)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "soubor"
	}
	return base
}
