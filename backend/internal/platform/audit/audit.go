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
	ModulePlatform  = "platform"
	ModuleLogging   = "logging"
	ModuleTodo      = "todo"
	ModuleEvents    = "events"
	ModuleNotes     = "notes"
	ModuleDocuments = "documents"
	ModuleDashboard = "dashboard"
	ModuleAdmin     = "admin"
	ModuleFinance   = "finance"
	ModuleGarden    = "garden"
	// ModuleElectricity is v8's Elektřina. Every module passes the constant and
	// never a literal, so a typo in a module name is a compile error rather than
	// a row that quietly falls out of the log browser's filter.
	ModuleElectricity = "electricity"
	// ModuleChat is v10's Chat. ⚠ It writes NO event for a message — sending,
	// editing and deleting one leave audit_events untouched (D231), which makes
	// chat the first module in Home whose primary mutation is invisible in the
	// Log. What it does write is structural: rooms and membership, plus
	// attachments from PR 3.
	ModuleChat = "chat"
)

// PlatformActions are the action verbs emitted by platform/ packages. They are
// bare verbs qualified by the module column, exactly like a module's
// AuditActions() (logging declares "prune", displayed as "logging.prune").
//
// They belong to no module, so the action catalog (FR-ADM4) merges this list in
// explicitly — without it, a trigger rule could not fire on a login.
var PlatformActions = []string{
	"login",
	"logout",
	"push.subscribe",
	"push.unsubscribe",
	"push.prefs",
	"push.test",
}

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

// ModuleSink is a Sink with one module's identifier bound to it, so a caller
// records `audit.Event{Action: …}` rather than restating which module it is on
// every event.
//
// ⚠ IT IS NOT A WIDER Sink, and that is deliberate. Every service already held
// a `record` wrapper whose whole body was "stamp my Module constant, discard the
// event id, return the error" — eight of them, plus seventeen handlers stamping
// it inline. Binding the constant once at construction is what those wrappers
// wanted. Putting `For` on the Sink INTERFACE would have been the other way to
// spell it, and this package's doc comment is why not: the interface is narrow
// on purpose, so a second implementation (an HTTP client to a standalone logging
// service) can be dropped in without touching module code, and a method every
// implementer must supply to hand back a struct they do not own is not narrow.
//
// ⚠ Record returns only the error. Every one of the twenty-six call sites this
// replaced discarded the event id; a caller that needs it uses the Sink.
type ModuleSink struct {
	sink   Sink
	module string
}

// For binds module to sink. The module is one of the Module* constants — never a
// literal, so a typo is a compile error rather than a row that quietly falls out
// of the log browser's filter.
func For(sink Sink, module string) ModuleSink { return ModuleSink{sink: sink, module: module} }

// For re-binds to another module, for the caller that records on a module's
// behalf rather than its own. There is exactly one — `admin`'s clean-up page
// writes `chat.threshold.update`, because the event belongs in chat's log and
// not in the log of whoever happened to open the page. Written out at the call
// site, so a cross-module event is a visible decision.
func (m ModuleSink) For(module string) ModuleSink { return ModuleSink{sink: m.sink, module: module} }

// Record writes one event with the bound module stamped on it, inside tx.
func (m ModuleSink) Record(ctx context.Context, tx *sql.Tx, e Event) error {
	e.Module = m.module
	_, err := m.sink.Record(ctx, tx, e)
	return err
}

// Event is one domain audit record. Actor and request context (who/where) are
// NOT fields here: they are read from the request context by the sink, so a
// handler cannot forge them.
type Event struct {
	Module     string         // one of the Module* constants
	Action     string         // dotted verb: "card.move", "event.update", "reminder.complete"
	EntityType string         // "card" | "column" | "board" | "label" | "checklist_item" | "event" | "note" | "folder" | "document" | "document_folder" | ""
	EntityID   string         // entity id, or ""
	Summary    string         // human-readable Czech summary shown in the log browser
	Level      string         // "" defaults to info
	Meta       map[string]any // optional; carries "via" for cross-module triggers
	Changes    []Change       // field diffs (key entities only)

	// Visibility and OwnerID are the typed form of the v9 redaction marker.
	// When Visibility is set, the sink stamps it into Meta under MetaVisibility
	// (and OwnerID under MetaOwnerID) — the only keys the read paths recognise —
	// so a module recording scoped items cannot misspell or forget the marker.
	// An event written without it has NULL visibility and is NEVER redacted,
	// which is why modules with private data must set these rather than compose
	// the meta keys by hand.
	Visibility string // "" (unscoped), VisibilityShared, or VisibilityPrivate
	OwnerID    string // owning member's user id; meaningful only for private
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
//
// ⚠ IT WAS ALREADY HERE while six modules spelled it `ap` — `chat`, `documents`,
// `events`, `finance`, `notes` and `todo`, 162 call sites between them. That is
// the failure platform/db/sql.go records, in its most literal form: an extraction
// nobody adopts is a seventh copy with a doc comment claiming otherwise. All six
// now call this one.
func Ptr(s string) *string { return &s }

// EqualPtr reports whether two optional strings are equal, treating a nil and an
// empty-string pointer as DIFFERENT — an absent value is not a blank one, which
// is the whole reason Change.Old/New are pointers.
//
// ⚠ IT LIVES HERE BECAUSE IT EXISTED FOUR TIMES. `documents`, `events`, `notes`
// and `todo` each carried a byte-identical `eqp`, and all four already import this
// package to name the Change it exists to build. Same for Diff below.
func EqualPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Diff appends a Change to changes when old and newVal differ, and does nothing
// when they match — so an untouched field never reaches the audit log, which is
// what makes a diff readable as "what the writer actually altered".
func Diff(changes *[]Change, field string, old, newVal *string) {
	if !EqualPtr(old, newVal) {
		*changes = append(*changes, Change{Field: field, Old: old, New: newVal})
	}
}

// HardMeta is the `meta` a delete event carries: `{"hard": true}` for a purge,
// and nothing at all for a soft delete.
//
// Nil rather than `{"hard": false}` is the point. The Log browser renders meta as
// it finds it, so a false flag on every ordinary delete is a line of noise on the
// majority of rows to mark the case that is NOT happening — and `hard` is read as
// "was this irreversible", which absence answers just as well.
//
// ⚠ IT REPLACES FOUR BYTE-IDENTICAL COPIES of `metaHard`, in `documents`,
// `events`, `notes` and `todo`.
func HardMeta(hard bool) map[string]any {
	if hard {
		return map[string]any{"hard": true}
	}
	return nil
}

// WithVia stamps the `via` origin marker onto an event's meta when there is one,
// and collapses an empty result back to nil.
//
// `via` is the ?via= query parameter the frontend sets when a mutation came from
// somewhere other than the item's own screen — a dashboard widget, a pin — so the
// log can say where a change was made from. It is absent far more often than it is
// present, hence both halves: no marker when via is "", and no empty map when
// there was nothing else in the meta either.
//
// ⚠ IT REPLACES TWO BYTE-IDENTICAL COPIES of `metaVia`, in `notes` and
// `documents`. Callers that build a combined literal by hand
// (`{"hard": true, "via": "cascade"}`) are a different shape and are left alone.
func WithVia(base map[string]any, via string) map[string]any {
	if via != "" {
		if base == nil {
			base = map[string]any{}
		}
		base["via"] = via
	}
	if len(base) == 0 {
		return nil
	}
	return base
}

// Exists reports whether an event with this module/action/entity id was already
// recorded. It lives HERE so audit_events stays this package's contract: a
// module that needs write-once semantics (garden's one-warning-per-frost-night)
// asks the spine rather than querying the table itself — the same boundary
// Record enforces on the write side.
func Exists(ctx context.Context, tx *sql.Tx, module, action, entityID string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_events WHERE module = ? AND action = ? AND entity_id = ?`,
		module, action, entityID).Scan(&n)
	return n > 0, err
}
