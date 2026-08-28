package audit

// Redaction of private-item events (v9, PRD D187/D188/D189).
//
// v9 gives `notes` and `documents` a second, PRIVATE root per member. A mutation
// there is audited like any other — full summary, full field diffs — and is hidden
// from everyone but its owner ON THE WAY OUT.
//
// Redacting at READ time rather than at write time is the whole design (D187).
// A summary redacted at write time is redacted permanently, including for the
// person whose history it is, and the audit spine stops being a record for them.
// The price is that redaction has to be applied at EVERY read path, which is
// exactly why it is one function in the platform rather than a rule each caller
// re-implements: two implementations of a privacy rule is one implementation and
// one bug.

// Meta keys carrying the redaction marker. They live in the existing `meta` JSON
// rather than in two new columns because audit_events sits under an
// external-content FTS5 index keyed on the rowid, and adding columns to it is the
// one migration shape D179 forbids. An expression index over the JSON needs no
// table rebuild — see 01002_private_meta.sql.
const (
	MetaVisibility = "visibility"
	MetaOwnerID    = "owner_id"
	// MetaByAdmin marks the one asymmetry: an admin hard-deleting a foreign
	// private item (D181). It is not part of redaction; it is here so the three
	// meta keys v9 introduces are defined together.
	MetaByAdmin = "by_admin"

	// VisibilityPrivate is the meta value that triggers redaction.
	VisibilityPrivate = "private"
	// VisibilityShared is the default and is never redacted.
	VisibilityShared = "shared"
)

// Redaction phrases, fixed by PRD §V9-7 (D201). They appear in the Log browser and
// in a push notification, and nowhere else. Deliberately per entity type: "a
// private item happened" is more useful when it at least says which module.
const (
	RedactedNote           = "Soukromá poznámka — podrobnosti skryty"
	RedactedDocument       = "Soukromý dokument — podrobnosti skryty"
	RedactedFolder         = "Soukromá složka — podrobnosti skryty"
	RedactedDefault        = "Soukromá položka — podrobnosti skryty"
	redactedEntityFolder   = "folder"
	redactedEntityDocFold  = "document_folder"
	redactedEntityNote     = "note"
	redactedEntityDocument = "document"

	// RedactedConversation is v10's, and it is a DIFFERENT RULE reusing the same
	// machinery — see RedactMemberScoped.
	RedactedConversation = "Změna v konverzaci — podrobnosti skryty"
)

// MemberScoped reports whether an entry's rendered text names something readable
// by a MEMBERSHIP rather than by the household (v10).
//
// It is not the v9 private marker and must not be confused with it: a chat event
// is not somebody's private item, and its Log row stays UNREDACTED for admins by
// design (leak table row 12 — the Log is admin-only). What it is, is member-scoped:
// a conversation's name is readable by the people in that conversation.
func MemberScoped(e Entry) bool { return e.Module == ModuleChat }

// RedactMemberScoped strips what a member-scoped entry NAMES, for a render whose
// audience is chosen by ROLE rather than by membership.
//
// ⚠ THE PUSH RENDERER IS THE ONLY CALLER, AND THAT IS THE WHOLE POINT (v10
// review). admin's trigger listener already had a chat case in inAppURL, dropping
// /chat/{id} down to /chat because "the id in the notification would itself be the
// disclosure" — while the body template, whose default IS the audit summary,
// carried `Konverzace „Dovolená s Petrou" přejmenována` to every lock screen a
// role-chosen audience owns. The guard was on the strictly less disclosive of the
// two fields. This is the other half.
//
// Changes go for the D207 reason, unchanged: `{{change.name.new}}` is whitelisted
// by SHAPE, so a clean summary is not enough on its own.
//
// ⚠ THE RESULTING NOTIFICATION IS VAGUE ON PURPOSE, AND THAT WAS DECIDED RATHER
// THAN SETTLED FOR (D264). A rule on a chat action banners "něco se změnilo v
// nějaké konverzaci" and no more. Dropping chat's verbs from the trigger composer
// was considered and declined; so was rendering per recipient, which is a platform
// change because the coalescing window builds ONE envelope per rule before the
// audience is resolved.
//
// So do NOT "fix" the unhelpful copy by putting the summary back. That is the leak
// this function exists for, and TestRedactMemberScopedStripsTheConversationName is
// what goes red if anybody tries.
func RedactMemberScoped(e Entry, changes []Change) (Entry, []Change) {
	if !MemberScoped(e) {
		return e, changes
	}
	e.Summary = RedactedConversation
	e.EntityID = ""
	// A fresh map rather than a delete: Entry is a value but its map is shared
	// with the caller's copy, and redaction must never mutate the raw entry.
	e.Meta = map[string]any{}
	e.Redacted = true
	return e, nil
}

// IsPrivate reports whether an entry describes a private item.
func IsPrivate(e Entry) bool {
	v, _ := e.Meta[MetaVisibility].(string)
	return v == VisibilityPrivate
}

// OwnerOf returns the owning member's user id for a private entry, or "".
func OwnerOf(e Entry) string {
	id, _ := e.Meta[MetaOwnerID].(string)
	return id
}

// Redact returns e as viewerUserID is allowed to see it.
//
// For a private entry viewed by anyone other than its owner it replaces Summary
// with the fixed Czech phrase, BLANKS EntityID, SCRUBS Meta down to the
// visibility marker, and sets Redacted. What remains — who acted, when, which
// module, which action — is deliberately kept: that a member edited something
// private at 21:40 is not the secret; what it was is.
//
// ⚠ Blanking EntityID is not cosmetic. The purge screen hands admins the ids of
// foreign private items BY DESIGN (D198), and an id in a log row is an id that can
// be fed back into the entity timeline. Meta is scrubbed for the same reason:
// owner_id names the member the item belongs to, and a copy that keeps it has
// only half-redacted (the D207 miss). The one thing this function CANNOT reach is
// the field diffs, which are loaded separately from the entry — a read path that
// renders diffs must go through RedactRendered, not call this alone.
//
// Passing an empty viewerUserID means "the anonymous viewer", which is how the
// push renderer gets a copy safe for the whole audience (D189).
func Redact(e Entry, viewerUserID string) Entry {
	if !IsPrivate(e) {
		return e
	}
	if viewerUserID != "" && viewerUserID == OwnerOf(e) {
		return e
	}
	e.Summary = redactionPhrase(e.EntityType)
	e.EntityID = ""
	// A fresh map, not a delete on the old one: Entry is a value but its map is
	// shared with the caller's copy, and redaction must never mutate the raw entry.
	e.Meta = map[string]any{MetaVisibility: VisibilityPrivate}
	e.Redacted = true
	return e
}

// RedactRendered is Redact for the FULL renderable shape: the entry plus its
// separately-loaded field diffs. A redacted copy comes back with nil changes —
// before/after values of a private item's edit are exactly what redaction
// removes, and template tokens select changes by SHAPE, not by field name, so
// no enumeration of fields can make them safe (D207).
//
// ⚠ Any read path that renders diffs — an export, a digest, a webhook — must
// route through THIS, not Redact alone: Redact cannot see the diffs, and a
// caller that forgets them fails open.
func RedactRendered(e Entry, changes []Change, viewerUserID string) (Entry, []Change) {
	e = Redact(e, viewerUserID)
	if e.Redacted {
		changes = nil
	}
	return e, changes
}

func redactionPhrase(entityType string) string {
	switch entityType {
	case redactedEntityNote:
		return RedactedNote
	case redactedEntityDocument:
		return RedactedDocument
	case redactedEntityFolder, redactedEntityDocFold:
		return RedactedFolder
	default:
		return RedactedDefault
	}
}

// VisibleEventsSQL is the SECOND redaction rule (D188) as a SQL predicate over an
// `audit_events` row aliased `e`: unfiltered browsing REDACTS a private event, but
// a `?q=` search EXCLUDES it, because a redacted hit still tells the searcher that
// their term occurs in a private title — which is the thing being protected, not
// the title itself. Exclusion has to happen in the query rather than in Go, or the
// page length discloses what the rows no longer do.
//
// The meta keys come from the Meta* constants and the value is BOUND from
// VisibilityPrivate, never inlined — a marker change that misses this file cannot
// leave a stale literal behind in another package.
//
// ⚠ THERE IS DELIBERATELY NO Go TWIN of this predicate. A `CanSee(entry, viewer)`
// helper stood here, with no caller anywhere in the repo and therefore no test:
// a second spelling of a privacy rule that nothing exercises drifts the moment the
// marker or the ownership rule changes, and the next person to reach for the
// in-memory form — for an export, a digest, a webhook — gets the stale one with
// nothing going red. Two implementations of a privacy rule is one implementation
// and one bug (see the package doc). A read path that needs the decision in Go
// goes through Redact/RedactRendered, which return whether they fired.
//
// `IS NOT` rather than `<>` because it is SQLite's NULL-safe comparison: an
// event with no meta at all (almost all of them) has a NULL visibility, and
// `NULL <> 'private'` is NULL — which would filter out every ordinary row.
func VisibleEventsSQL(viewerUserID string) (string, []any) {
	vis := `json_extract(e.meta, '$.` + MetaVisibility + `')`
	if viewerUserID == "" {
		return vis + ` IS NOT ?`, []any{VisibilityPrivate}
	}
	return `(` + vis + ` IS NOT ?
	         OR json_extract(e.meta, '$.` + MetaOwnerID + `') = ?)`,
		[]any{VisibilityPrivate, viewerUserID}
}
