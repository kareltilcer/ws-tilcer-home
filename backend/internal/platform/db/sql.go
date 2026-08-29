package db

import (
	"strings"
	"unicode"
)

// Placeholders builds "?,?,?" for an IN clause of n arguments.
//
// ⚠ IT LIVES HERE BECAUSE IT EXISTED FIVE TIMES. `notes`, `documents`, `garden`,
// `todo` and `platform/push` each grew their own copy, and v10's `chat` was writing
// a sixth — at which point the question stops being "is this worth sharing" and
// becomes "which of the six does a fix reach". platform/db is the seam every module
// that speaks SQL already depends on, and all five call sites were migrated to this
// function rather than left beside it: an extraction nobody adopts is a seventh copy
// with a doc comment claiming otherwise.
//
// n <= 0 returns "", which is not a valid IN list: a caller with nothing to match
// must skip the clause rather than emit `IN ()`.
func Placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// FTSQuery turns free text into a safe FTS5 prefix MATCH: each whitespace token
// becomes a QUOTED prefix term, so punctuation cannot break out into FTS5's own
// syntax. Returns "" when nothing searchable is left — the caller MUST treat that
// as "matches nothing" and skip the MATCH entirely, because the behaviour of a
// MATCH on the empty string is unspecified.
//
// ⚠ IT IS NOT DEFENSIVE POLISH. Bound raw, ordinary message and note text is a
// SYNTAX ERROR rather than a search: `mama's` and `co?` are "fts5: syntax error",
// `9:30` and `a-b` are "no such column", a lone `"` is "unterminated string" —
// every one a 500 from a search endpoint.
//
// ⚠ AND IT LIVES HERE BECAUSE THE COPIES WERE ALREADY DISAGREEING. `notes`,
// `documents` and v10's `chat` carried three byte-identical versions of this; the
// next metacharacter somebody discovers has to reach all of them, and under three
// spellings it reaches one. `garden` and `logging` are deliberately NOT migrated:
// garden emits `""` for an unsearchable query and does not drop letterless tokens,
// logging doubles quotes and emits no prefix `*` — both are behaviour changes that
// belong to those modules' own releases, not to a refactor.
func FTSQuery(q string) string {
	fields := strings.Fields(q)
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		// A quote inside a token is dropped rather than doubled: the token is
		// re-quoted below, and FTS5 has no escape that survives a prefix `*`.
		f = strings.ReplaceAll(f, `"`, "")
		// A token with no letter or digit (`:-)`, `!!!`) tokenizes to zero terms,
		// and a prefix `*` on an empty phrase is itself a syntax error.
		if !strings.ContainsFunc(f, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) {
			continue
		}
		terms = append(terms, `"`+f+`"*`)
	}
	return strings.Join(terms, " ")
}

// ClampLimit normalises a caller-supplied page size to def..max: a missing or
// non-positive n becomes def, anything above max becomes max.
//
// ⚠ IT LIVES HERE BECAUSE FIVE PLACES CLAMPED UNDER THREE NAMES. `chat` and
// `garden` spelled it NormalizeLimit, `logging` clampLimit, `admin` folded it
// into a query-string reader called limitOf, and four of the five agreed on
// exactly 50/200 — so the bounds are arguments and not constants here, because
// the numbers are each module's decision and the arithmetic is not.
//
// ⚠ `electricity` DELIBERATELY DOES NOT USE THIS. Its limitOf takes 100/500 and
// falls back to the default on an out-of-range value rather than clamping, which
// chat/store.go records as "a known defect and not a precedent to copy". Making
// it call this would fix that defect — and a fixed defect is a behaviour change,
// which belongs to electricity's own release rather than to a refactor.
func ClampLimit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
