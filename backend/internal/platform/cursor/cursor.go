// Package cursor encodes and decodes the opaque keyset cursors the list
// endpoints hand back as `next_cursor`.
//
// A keyset cursor names the last row of a page by its SORT TUPLE, so the next
// page resumes exactly after it. When the ORDER BY leads with the id that tuple
// is just the id and no encoding is needed — which is why the modules whose
// collections sort by UUIDv7 do not appear here. This package is for the rest:
// `(ts, id)`, `(updated_at, id)`, `(window_from, position, id)`. Four modules
// had written that out four times, in four encodings, and two of the four were
// plain text a client could read and hand-edit.
//
// The token is base64url (unpadded) over the parts joined by `\x1f`, ASCII unit
// separator. Two properties make it the right separator: it cannot occur in a
// timestamp, a lexorank position or a UUID, so no part can smuggle one in and
// change the arity; and it is invisible to a reader, which keeps the token
// OPAQUE in the way the specs promise. Opaque is the point — the sort tuple is
// the ORDER BY's business, and a cursor a client can parse is a cursor a client
// will eventually build by hand.
//
// # Decode is structural, and deliberately says nothing about meaning
//
// Decode reports only whether the token was minted by Encode with the arity the
// caller asked for. It does NOT reject empty parts, because two of the four
// callers never did, and rejecting them here would quietly turn their malformed
// input into a different status code. A caller that had its own emptiness check
// keeps it beside its own call — chat and documents both do.
//
// The same goes one level up: what `ok == false` MEANS is the caller's to
// decide, and the four callers genuinely disagree. `documents` treats an
// unparseable cursor as "start from the beginning" — a stale bookmark should
// show the first page, not a 422 — while `chat` refuses it, at length and on
// purpose, because a silently dropped parameter returns page one forever and
// reads as the end of the results. Both are recorded decisions. This package
// unifies the ENCODING and nothing else.
package cursor

import (
	"encoding/base64"
	"strings"
)

// sep joins the parts of a sort tuple. ASCII unit separator: it appears in no
// timestamp, lexorank position or UUID, so a part can never introduce one.
const sep = "\x1f"

// Encode packs a sort tuple into one opaque, URL-safe token.
func Encode(parts ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, sep)))
}

// Decode unpacks a token minted by Encode into exactly n parts, reporting
// whether it was one. A token this package did not mint, or one carrying a
// different number of parts than the caller's ORDER BY has terms, is not an
// error here — it is `ok == false`, and the caller decides what that means.
func Decode(cur string, n int) ([]string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(cur)
	if err != nil {
		return nil, false
	}
	parts := strings.Split(string(raw), sep)
	if len(parts) != n {
		return nil, false
	}
	return parts, true
}
