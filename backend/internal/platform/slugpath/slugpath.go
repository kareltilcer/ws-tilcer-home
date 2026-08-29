// Package slugpath parses the slash-separated slug paths the tree modules address
// rows by (/poznamky/prace/projekty/porada, /dokumenty/smlouvy/energie/cez).
//
// A slug path is a WIRE format, not a filesystem path: it arrives percent-encoded
// from the router, its segments are user-typed names run through the slugger, and
// the permanent address of a row is still its id (D32/D42). Parsing it is
// therefore the same job wherever it happens, which is why it is one function.
package slugpath

import (
	"net/url"
	"strings"
)

// Split turns a slug path into its segments, dropping the empty ones a leading,
// trailing or doubled slash produces, and percent-decoding each segment.
//
// ⚠ A segment that fails to decode is kept RAW rather than rejected. That is the
// behaviour both callers had: url.PathUnescape only fails on a malformed escape,
// and a malformed escape in a slug reaches the resolver as a segment that matches
// nothing, which is a 404 — the same answer a well-formed unknown slug gets. An
// error here would make the two distinguishable, which is a difference a caller
// could read a real slug's existence off.
//
// ⚠ IT REPLACES TWO BYTE-IDENTICAL COPIES of `splitPath`, in `notes` and
// `documents`.
func Split(p string) []string {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" {
		return nil
	}
	raw := strings.Split(p, "/")
	out := make([]string, 0, len(raw))
	for _, seg := range raw {
		if seg == "" {
			continue
		}
		if dec, err := url.PathUnescape(seg); err == nil {
			seg = dec
		}
		out = append(out, seg)
	}
	return out
}
