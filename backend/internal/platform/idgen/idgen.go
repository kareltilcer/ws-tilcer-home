// Package idgen mints UUIDv7 identifiers. v7 ids are time-ordered, so they sort
// chronologically and double as keyset-pagination cursors (PRD §5).
package idgen

import (
	"strings"

	"github.com/google/uuid"
)

// New returns a new UUIDv7 as a canonical string. It panics only if the system
// entropy source fails, which is not a recoverable condition.
func New() string {
	id, err := uuid.NewV7()
	if err != nil {
		panic("idgen: cannot generate UUIDv7: " + err.Error())
	}
	return id.String()
}

// Short returns the first 8 hex characters of a fresh UUIDv7, with the dashes
// removed — a short, unique-enough stem for a row whose name slugs to nothing.
//
// Both tree modules reach for this when a title is entirely punctuation or
// emoji: the slugger returns "", and a row with an empty slug is a row with no
// slug path. 8 hex characters of a v7 id are its high timestamp bits, so two
// minted in the same millisecond can collide — which is why the caller is
// `freeSlug`, whose whole job is to walk `-2`, `-3` … until the sibling index
// accepts one. Uniqueness is the index's guarantee, not this function's.
//
// ⚠ IT REPLACES TWO BYTE-IDENTICAL COPIES of `shortID`, in `notes` and
// `documents`.
func Short() string {
	id := strings.ReplaceAll(New(), "-", "")
	if len(id) > 8 {
		id = id[:8]
	}
	return id
}
