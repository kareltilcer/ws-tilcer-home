// Package audit is the in-process audit spine (PRD §Architecture, HANDOFF-1):
// every module records a domain event through it, INSIDE the same *sql.Tx as the
// change it describes, so the change and its audit event commit or roll back
// together. There is no code path that mutates without logging.
//
// The Sink interface is deliberately narrow so a second implementation (an HTTP
// client to a future standalone logging service) could be dropped in without
// touching module code. v1 ships only the in-process SQLite writer.
package audit

import (
	"context"
	"database/sql"
)

// Module identifiers (English; also the audit `module` column values).
const (
	ModuleLogging   = "logging"
	ModuleTodo      = "todo"
	ModuleEvents    = "events"
	ModuleDashboard = "dashboard"
)

// Levels.
const (
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// TSLayout is the fixed-width UTC timestamp format used for audit_events.ts.
// Fixed width (always 9 fractional digits, trailing "Z") guarantees lexical
// string order equals chronological order, which keyset pagination relies on.
const TSLayout = "2006-01-02T15:04:05.000000000Z07:00"

// Sink records one audit event (and its field changes) using the caller's tx.
type Sink interface {
	// Record writes one event within tx and returns the new event id. A failure
	// is returned unchanged so the caller's transaction rolls back — an action
	// that succeeds unlogged is the one bug this package exists to prevent.
	Record(ctx context.Context, tx *sql.Tx, e Event) (eventID string, err error)
}

// Event is one domain audit record. Actor and request context (who/where) are
// NOT fields here: they are read from the request context by the sink, so a
// handler cannot forge them.
type Event struct {
	Module     string         // one of the Module* constants
	Action     string         // dotted verb: "card.move", "event.update", "reminder.complete"
	EntityType string         // "card" | "column" | "board" | "label" | "checklist_item" | "event" | ""
	EntityID   string         // entity id, or ""
	Summary    string         // human-readable Czech summary shown in the log browser
	Level      string         // "" defaults to info
	Meta       map[string]any // optional; carries "via" for cross-module triggers
	Changes    []Change       // field diffs (key entities only)
}

// Change is a single field's before/after. Old/New are pointers so a genuine
// NULL (absent value) is distinct from an empty string. Values are full and
// untruncated (PRD D6).
type Change struct {
	Field string
	Old   *string
	New   *string
}

// Ptr is a small helper for building Change values from string literals.
func Ptr(s string) *string { return &s }
