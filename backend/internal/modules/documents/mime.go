package documents

import (
	"net/http"
	"path/filepath"
	"strings"
)

// Content-type handling for untrusted uploads (D48). Two rules drive everything
// here, and both are security-critical rather than cosmetic:
//
//  1. The stored content type is SNIFFED FROM THE BYTES, never taken from the
//     client's multipart Content-Type header. A client can claim anything.
//  2. Only a small allowlist of types is ever served inline; everything else is
//     served as an attachment. An uploaded .html or .svg must never execute in
//     home.tilcer.cz's origin.

// sniffLen is how many leading bytes http.DetectContentType inspects.
const sniffLen = 512

// officeByExt maps the Office/OpenDocument extensions onto their canonical MIME
// types. This refinement exists because OOXML files are ZIP containers:
// http.DetectContentType sees the ZIP magic and reports application/zip for every
// .docx/.xlsx/.pptx. Trusting the extension here is safe in a way that trusting
// the client's header is not — the extension only ever REFINES a type we already
// sniffed as a ZIP, as opaque bytes, or (for RTF) as plain text, so it cannot turn
// a sniffed HTML file into a "document".
var officeByExt = map[string]string{
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".odt":  "application/vnd.oasis.opendocument.text",
	".ods":  "application/vnd.oasis.opendocument.spreadsheet",
	".odp":  "application/vnd.oasis.opendocument.presentation",
	".doc":  "application/msword",
	".xls":  "application/vnd.ms-excel",
	".ppt":  "application/vnd.ms-powerpoint",
	".rtf":  "application/rtf",
}

// textByExt refines types that sniff as plain text but render better with a more
// specific type. Markdown is the one that matters: the viewer renders text/markdown
// through the Markdown renderer and text/plain as escaped text.
var textByExt = map[string]string{
	".md":       "text/markdown; charset=utf-8",
	".markdown": "text/markdown; charset=utf-8",
	".csv":      "text/csv; charset=utf-8",
}

// sniffContentType determines the stored content type from the leading bytes, with
// extension-based refinement for the container formats sniffing cannot separate.
func sniffContentType(head []byte, filename string) string {
	sniffed := http.DetectContentType(head)
	ext := strings.ToLower(filepath.Ext(filename))
	base := baseType(sniffed)

	switch {
	case base == "application/zip", base == "application/octet-stream", base == "application/x-ole-storage":
		// A ZIP/opaque container: the extension is the only signal that separates a
		// .docx from a .xlsx from a plain archive.
		if ct, ok := officeByExt[ext]; ok {
			return ct
		}
	case base == "text/plain":
		if ct, ok := textByExt[ext]; ok {
			return ct
		}
		// RTF is the one Office format with no binary signature — `{\rtf1` sniffs as
		// plain text, so it never reaches the container branch above. Without this it
		// would be stored as text/plain: previewed "natively" as a wall of escaped
		// control words and never handed to the converter. Refining text/plain into
		// application/rtf is the safe direction (it makes the file an attachment, not
		// an inline one), which is what lets the extension decide here at all.
		if ext == ".rtf" {
			return officeByExt[ext]
		}
	}
	return sniffed
}

// baseType strips any parameters ("text/plain; charset=utf-8" → "text/plain").
func baseType(ct string) string {
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		return strings.TrimSpace(ct[:i])
	}
	return strings.TrimSpace(ct)
}

// inlineSafeTypes is the ONLY set served with Content-Disposition: inline (D48).
// Deliberately excluded: image/svg+xml (SVG is a scriptable document), text/html,
// text/xml, and anything else that a browser would treat as active content.
var inlineSafeTypes = map[string]bool{
	"application/pdf": true,
	"image/png":       true,
	"image/jpeg":      true,
	"image/gif":       true,
	"image/webp":      true,
	"text/plain":      true,
	"text/markdown":   true,
}

// InlineSafe reports whether a content type may be served inline. Everything else
// is an attachment — the browser downloads it instead of rendering it in home's
// origin.
func InlineSafe(contentType string) bool { return inlineSafeTypes[baseType(contentType)] }

// isImage reports whether the type is a raster image we can decode for a thumbnail.
// SVG is excluded on purpose: it is a document, not a bitmap, and rasterising
// untrusted SVG is exactly the attack surface D48 rules out.
func isImage(contentType string) bool {
	switch baseType(contentType) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

func isPDF(contentType string) bool { return baseType(contentType) == "application/pdf" }

// isTextual reports whether the type previews as text/Markdown client-side.
func isTextual(contentType string) bool {
	base := baseType(contentType)
	switch base {
	case "text/plain", "text/markdown", "text/csv":
		return true
	}
	return false
}

// isOffice reports whether the type needs Office→PDF conversion for a preview.
func isOffice(contentType string) bool {
	base := baseType(contentType)
	for _, ct := range officeByExt {
		if baseType(ct) == base {
			return true
		}
	}
	return false
}

// previewPlanFor decides a freshly-uploaded document's preview_kind/preview_status
// (HANDOFF-6 §4). Because the bytes are immutable, this decision — and the derived
// objects it leads to — is made exactly once per document.
//
//   - image / PDF / text  → "native"/"ready": the original previews directly through
//     /raw. A thumbnail is still generated asynchronously; it arrives with a /ws
//     push and until then the UI shows the type icon.
//   - Office              → "pdf"/"pending": a derived preview PDF is being made.
//     On failure the status flips to "failed" and the document is download-only —
//     the upload itself is never lost.
//   - everything else     → "none"/"none": download-only, nothing to derive.
func previewPlanFor(contentType string, previewEnabled bool) (kind, status string) {
	switch {
	case isImage(contentType), isPDF(contentType), isTextual(contentType):
		return previewKindNative, previewReady
	case isOffice(contentType):
		if !previewEnabled {
			// Conversion is off, so no preview will ever appear: say so now rather than
			// leaving the document "pending" forever.
			return previewKindNone, previewNone
		}
		return previewKindPDF, previewPending
	default:
		return previewKindNone, previewNone
	}
}

// needsDerivedObject reports whether the preview worker has anything to do for a
// document: convert Office to PDF, or thumbnail an image/PDF.
func needsDerivedObject(contentType string) bool {
	return isOffice(contentType) || isImage(contentType) || isPDF(contentType)
}

// thumbnailPlanFor decides a freshly-uploaded document's thumbnail_status — the
// server-side half of previewPlanFor. Every type the worker can thumbnail starts
// 'pending' and stays that way until a job settles it, which is what makes an
// interrupted thumbnail retryable at the next boot (a native preview's own status is
// 'ready' from the moment of upload and can never signal outstanding work).
func thumbnailPlanFor(contentType string, previewEnabled bool) string {
	if previewEnabled && needsDerivedObject(contentType) {
		return thumbPending
	}
	return thumbNone
}

// titleFromFilename derives the default display title: the filename without its
// extension ("Smlouva ČEZ.pdf" → "Smlouva ČEZ").
func titleFromFilename(filename string) string {
	base := filepath.Base(strings.ReplaceAll(filename, `\`, "/"))
	if ext := filepath.Ext(base); ext != "" && len(ext) < len(base) {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.TrimSpace(base)
}

// mimeAllowed reports whether the sniffed type passes the configured allowlist. An
// empty allowlist allows everything (the type is still sniffed and still governs
// inline-vs-attachment).
func mimeAllowed(contentType string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return true
	}
	base := baseType(contentType)
	for _, a := range allowlist {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		// Support both exact types ("application/pdf") and family wildcards ("image/*").
		if strings.HasSuffix(a, "/*") {
			if strings.HasPrefix(base, strings.TrimSuffix(a, "*")) {
				return true
			}
			continue
		}
		if baseType(a) == base {
			return true
		}
	}
	return false
}
