package db

import "time"

// NowUTC returns the current instant in UTC, rendered with format. Every module
// that writes a timestamp column goes through here, so "now" is read once and
// converted to UTC once rather than eight times.
//
// ⚠ THE FORMAT IS THE CALLER'S AND MUST STAY THAT WAY. Home stores timestamps as
// TEXT and SQLite compares TEXT lexically, so a column's format IS its sort order
// — in `ORDER BY updated_at` and in every keyset cursor that carries a timestamp.
// The eight callers use five different ones on purpose:
//
//	time.RFC3339                      events, todo
//	time.RFC3339Nano                  admin, platform/push
//	2006-01-02T15:04:05.000Z07:00     chat, electricity, finance, garden
//	2006-01-02T15:04:05.000000Z07:00  documents, notes
//
// Harmonising them looks like tidying and is not: it changes how a table's
// EXISTING rows sort against the ones written after the deploy, so it is a data
// migration with a rewrite of every affected column, not a refactor. Each module
// keeps its own `tsFormat` next to the comment explaining why its width is what
// it is (notes' microseconds, for one, are what let the editor's concurrency
// guard tell two edits inside one second apart).
func NowUTC(format string) string { return time.Now().UTC().Format(format) }
