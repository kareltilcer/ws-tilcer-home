// Package httpx holds the HTTP layer shared across modules: JSON rendering,
// the shared Error shape, middleware (request id, logging, recovery, auth, role
// gates), and the health probes.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

// maxBodyBytes caps request bodies to a sane size for this API (notes/diffs can
// be long, but not unbounded).
const maxBodyBytes = 1 << 20 // 1 MiB

// JSON writes v as a JSON response with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// DecodeJSON strictly decodes a JSON request body into dst (unknown fields are
// rejected). Returns a client-facing error suitable for a 422.
func DecodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return errors.New("empty request body")
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	// Reject trailing content after the first JSON value.
	if dec.More() {
		return errors.New("unexpected trailing content in request body")
	}
	return nil
}

// Respond is the terminal step of a handler: WriteError on a failure, otherwise
// JSON at status. v may be nil for a body-less success.
//
// ⚠ IT LIVES HERE BECAUSE IT EXISTED EIGHT TIMES. `chat`, `documents`,
// `electricity`, `events`, `finance`, `garden`, `notes` and `todo` each carried a
// byte-identical copy of this pair beside the two functions it calls, which are
// already in this package — so every module was one hop from the shared spelling
// and took the copy instead. Sixteen copies fed 178 call sites, and all 178 were
// migrated rather than left beside it: an extraction nobody adopts is a ninth copy
// with a doc comment claiming otherwise (the platform/db precedent).
func Respond(w http.ResponseWriter, status int, v any, err error) {
	if err != nil {
		WriteError(w, err)
		return
	}
	JSON(w, status, v)
}

// NoContent is Respond for a mutation that returns nothing: WriteError on a
// failure, otherwise a bare 204. Seven modules spelled it `respondNoContent` and
// `garden` spelled it `noContent`; this is the one name.
func NoContent(w http.ResponseWriter, err error) {
	if err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Limit reads ?limit from the query string and clamps it to def..max. An absent,
// unparseable or non-positive value becomes def; anything above max becomes max.
//
// It is the request-side half of appdb.ClampLimit, which does the same
// arithmetic for callers holding an int already (a store method taking a limit
// from its service). `admin` was the only module reading the parameter and
// clamping it in one function; the bounds stay arguments for the same reason
// they do there.
func Limit(r *http.Request, def, max int) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
