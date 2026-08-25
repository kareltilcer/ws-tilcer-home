package httpx

import (
	"net/http"
	"strings"
)

// ImmutableContentCache is the Cache-Control for bytes served under an id-based,
// immutable-content URL (documents raw/preview/thumbnail, note images): private to
// the household, cached for a year and never revalidated. Pair it with an ETag equal
// to the content checksum. Shared so the documents and notes content endpoints can
// never drift apart on the header they hand out.
const ImmutableContentCache = "private, immutable, max-age=31536000"

// RevalidatedContentCache is the Cache-Control for bytes whose ACCESS CHECK must
// run on every view — v9's private notes and documents (PRD D208, leak table row
// 19).
//
// ⚠ `no-cache` does not mean "do not cache". It means "you may store this, but
// revalidate before every reuse", which is precisely the property needed here:
//
//   - the ownership check executes on every view, so a second member gets 404
//     rather than a cached copy;
//   - the owner re-opening a 30 MB private PDF gets a 304, not a re-download.
//
// The distinction matters because ImmutableContentCache above would defeat the
// feature silently: `private` excludes shared PROXIES, not the other person using
// the same laptop, and `immutable` suppresses revalidation for a year — so the
// refusal would never execute at all.
//
// `no-store` was considered and rejected: marginally stricter (nothing on disk),
// at the cost of re-fetching every preview and thumbnail in the private tree on
// every render — the sort of tax that gets a header quietly removed later.
//
// Callers MUST keep sending the ETag with this header; it is what turns the
// revalidation into a 304 instead of a full transfer.
const RevalidatedContentCache = "private, no-cache, must-revalidate"

// IfNoneMatch reports whether the request's If-None-Match matches etag. Handles the
// "*" wildcard, comma-separated lists, and weak validators (W/). etag must include
// its surrounding quotes. Shared by every immutable-content endpoint so the
// conditional-GET semantics stay identical across modules.
func IfNoneMatch(r *http.Request, etag string) bool {
	header := r.Header.Get("If-None-Match")
	if header == "" {
		return false
	}
	if strings.TrimSpace(header) == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}
