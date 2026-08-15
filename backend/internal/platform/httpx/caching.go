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
