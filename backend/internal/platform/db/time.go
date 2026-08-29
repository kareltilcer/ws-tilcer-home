package db

import "time"

// NowUTC returns the current instant in UTC, rendered with format. It exists so
// that `time.Now().UTC().Format(…)` is written down once instead of eight times;
// each caller still reads its own "now".
//
// ⚠ THE FORMAT IS THE CALLER'S AND MUST STAY THAT WAY. Home stores timestamps as
// TEXT and SQLite compares TEXT lexically, so a column's format IS its sort order
// — in `ORDER BY updated_at` and in every keyset cursor that carries a timestamp.
// FIVE layouts are in use across the backend. The eight callers of this function
// account for the first three; the last two belong to packages that still format
// inline, and the warning covers them just the same:
//
//	time.RFC3339                         events, todo            (callers)
//	2006-01-02T15:04:05.000Z07:00        chat, electricity,      (callers)
//	                                     finance, garden
//	2006-01-02T15:04:05.000000Z07:00     documents, notes        (callers)
//	time.RFC3339Nano                     admin, platform/push,   (inline)
//	                                     platform/auth
//	2006-01-02T15:04:05.000000000Z07:00  platform/audit          (inline)
//
// (db/seed.go writes RFC 3339 inline as well.)
//
// Harmonising them looks like tidying and is not: it changes how a table's
// EXISTING rows sort against the ones written after the deploy, so it is a data
// migration with a rewrite of every affected column, not a refactor. Each module
// keeps its own `tsFormat` next to the comment explaining why its width is what
// it is (notes' microseconds, for one, are what let the editor's concurrency
// guard tell two edits inside one second apart).
func NowUTC(format string) string { return time.Now().UTC().Format(format) }
