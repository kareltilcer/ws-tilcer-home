package notes_test

// v9 — publish, the cross-scope refusals, and the one asymmetry (D181/D182/D186).
//
// Publishing is the only way an item crosses between the two roots, it is one-way,
// and there is no route back. Everything here is about keeping that true: that it
// works, that the things which must survive it do, that nobody but the owner can
// trigger it, and that no OTHER operation quietly achieves the same thing.

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/notes"
)

// TestPublishMovesTheNoteAndKeepsThePin covers the happy path plus the thing that
// must survive it (D183): the personal pin, which is keyed on the item id.
func TestPublishMovesTheNoteAndKeepsThePin(t *testing.T) {
	x := newH(t)
	n := x.privateNote(kajaCtx(), "Recepty", "guláš")
	mustPin(t, x, kajaCtx(), n.ID, "personal")

	pub := x.note(x.svc.PublishNote(kajaCtx(), n.ID, notes.PublishRequest{}))
	if pub.Visibility != "shared" {
		t.Errorf("visibility after publish = %q, want shared", pub.Visibility)
	}
	if pub.OwnerID != nil {
		t.Errorf("owner_id after publish = %v, want nil — publishing clears the owner (D182)", *pub.OwnerID)
	}
	if pub.ID != n.ID {
		t.Errorf("the id changed across a publish (%s to %s) — everything keyed on it breaks, "+
			"including the permanent URL and the pin", n.ID, pub.ID)
	}
	if !pub.Pinned.Personal {
		t.Error("the personal pin did not survive the publish — it is keyed on the item id and " +
			"the id does not change (D183)")
	}
	if got := x.note(x.svc.GetNoteDetail(andyCtx(), n.ID)); got.Title != "Recepty" {
		t.Errorf("after publish andy reads %q, want Recepty — the publish is the point", got.Title)
	}
}

// TestPublishReDerivesACollidingSlug: the destination's siblings are different
// siblings, so the slug may have to move even though the id does not.
func TestPublishReDerivesACollidingSlug(t *testing.T) {
	x := newH(t)
	x.note(x.svc.CreateNote(kajaCtx(), notes.NoteCreate{Title: "Smlouva"})) // already shared
	priv := x.privateNote(kajaCtx(), "Smlouva", "")

	pub := x.note(x.svc.PublishNote(kajaCtx(), priv.ID, notes.PublishRequest{}))
	if pub.Slug != "smlouva-2" {
		t.Errorf("published slug = %q, want smlouva-2 — the slug is re-derived against the "+
			"DESTINATION's siblings (D182)", pub.Slug)
	}
}

// TestPublishRefusesANonOwnerWith404 is D206, and it was specified as 403 in five
// documents before a review pass caught it.
//
// ⚠ The route is RequireWrite, so every editor can call it. A 403-for-private /
// 404-for-unknown pair would answer "does this id exist, and is it private?" for
// any id a caller cares to try — the permalink oracle D180 closes, reopened with a
// different verb. The admin case is the same: this is the one place an admin has
// LESS power than the role implies, and it must not be observable that way.
func TestPublishRefusesANonOwnerWith404(t *testing.T) {
	x := newH(t)
	n := x.privateNote(kajaCtx(), "Kajino", "")

	for _, tc := range []struct {
		who string
		ctx context.Context
	}{
		{"another member", andyCtx()},
		{"an admin", adminCtx()},
	} {
		_, err := x.svc.PublishNote(tc.ctx, n.ID, notes.PublishRequest{})
		if got := status(t, err); got != 404 {
			t.Errorf("%s publishing a foreign private note: %d, want 404 — NOT 403 (D206)", tc.who, got)
			continue
		}
		_, unknownErr := x.svc.PublishNote(tc.ctx, "01900000-0000-7000-8000-000000000000",
			notes.PublishRequest{})
		if err.Error() != unknownErr.Error() {
			t.Errorf("%s: publishing a foreign private id said %q while an unknown id said %q — "+
				"the difference between them IS the oracle (D206)", tc.who, err, unknownErr)
		}
	}
}

// TestPublishFolderIsAtomicOverTheWholeSubtree — a partial publish is the one
// outcome this endpoint must never produce, because half a published folder is a
// folder whose contents nobody can explain.
func TestPublishFolderIsAtomicOverTheWholeSubtree(t *testing.T) {
	x := newH(t)
	root := x.folder(x.svc.CreateFolder(kajaCtx(), notes.FolderCreate{Name: "Projekt", Scope: "private"}))
	sub := x.folder(x.svc.CreateFolder(kajaCtx(), notes.FolderCreate{Name: "Podsložka", ParentID: &root.ID}))
	deep := x.note(x.svc.CreateNote(kajaCtx(), notes.NoteCreate{Title: "Hluboká", FolderID: &sub.ID}))

	if _, err := x.svc.PublishFolder(kajaCtx(), root.ID, notes.PublishRequest{}); err != nil {
		t.Fatalf("publish folder: %v", err)
	}
	for _, tc := range []struct {
		what string
		read func() error
	}{
		{"the root folder", func() error { _, e := x.svc.GetFolderDetail(andyCtx(), root.ID); return e }},
		{"the subfolder", func() error { _, e := x.svc.GetFolderDetail(andyCtx(), sub.ID); return e }},
		{"the deep note", func() error { _, e := x.svc.GetNoteDetail(andyCtx(), deep.ID); return e }},
	} {
		if err := tc.read(); err != nil {
			t.Errorf("after publishing the folder, andy still cannot read %s: %v — a PARTIAL "+
				"publish is the one outcome this endpoint must never produce (D182)", tc.what, err)
		}
	}

	// ⚠ AND EVERY ROW IT SWEPT UP IS AUDITED SEPARATELY, exactly as a cascade
	// DELETE audits each child. One event naming only the parent would mean a rule
	// on `notes.note.publish` never fires for the notes that just became visible to
	// the household, and each note's entity timeline stays silent about the one
	// change to it that cannot be undone.
	for _, tc := range []struct {
		action, entityID, what string
	}{
		{"folder.publish", root.ID, "the root folder"},
		{"folder.publish", sub.ID, "the subfolder"},
		{"note.publish", deep.ID, "the deep note"},
	} {
		var n int
		if err := x.db.QueryRow(
			`SELECT COUNT(*) FROM audit_events WHERE action = ? AND entity_id = ?`,
			tc.action, tc.entityID).Scan(&n); err != nil {
			t.Fatalf("count %s events: %v", tc.action, err)
		}
		if n != 1 {
			t.Errorf("%s produced %d `%s` events, want 1 — a folder publish is a cascade, and "+
				"every row it makes household-visible needs its own event or the log records an "+
				"irreversible change to N items as a change to one (D182)", tc.what, n, tc.action)
		}
	}
	// The swept-up rows say so; the one the caller asked for does not.
	if via := lastAuditMeta(t, x.db, "note.publish")["via"]; via != "cascade" {
		t.Errorf("the cascaded note.publish meta.via = %v, want \"cascade\"", via)
	}
	if via := lastAuditMeta(t, x.db, "folder.publish")["via"]; via != nil {
		t.Errorf("the target folder's own publish event is marked via=%v — the cascade marker "+
			"belongs on what the cascade swept up, not on what was asked for", via)
	}
}

// TestNothingTurnsASharedItemPrivateAgain states the absence of unpublish as a
// decision (D182) rather than a gap. There is no route, so the test covers the
// operations somebody might reach for instead.
func TestNothingTurnsASharedItemPrivateAgain(t *testing.T) {
	x := newH(t)
	shared := x.note(x.svc.CreateNote(kajaCtx(), notes.NoteCreate{Title: "Sdílená"}))

	if _, err := x.svc.MoveNote(kajaCtx(), shared.ID, notes.NoteMoveRequest{Position: "m"}, ""); err != nil {
		t.Fatalf("an ordinary same-scope move should still work: %v", err)
	}
	if _, err := x.svc.PublishNote(kajaCtx(), shared.ID, notes.PublishRequest{}); status(t, err) != 422 {
		t.Errorf("publishing an already-shared note: %d, want 422", status(t, err))
	}
	after := x.note(x.svc.GetNoteDetail(kajaCtx(), shared.ID))
	if after.Visibility != "shared" {
		t.Errorf("visibility = %q — something turned a shared note private, and no such route "+
			"is supposed to exist (D182)", after.Visibility)
	}
}

// TestMoveAcrossScopesIs422 (D186). Publishing is the only crossing and it is a
// different verb on purpose: an irreversible change of audience must not be
// reachable by dragging something into a folder.
func TestMoveAcrossScopesIs422(t *testing.T) {
	x := newH(t)
	sharedFolder := x.folder(x.svc.CreateFolder(kajaCtx(), notes.FolderCreate{Name: "Sdílená složka"}))
	privFolder := x.folder(x.svc.CreateFolder(kajaCtx(), notes.FolderCreate{Name: "Soukromá složka", Scope: "private"}))
	priv := x.privateNote(kajaCtx(), "Soukromá", "")
	shared := x.note(x.svc.CreateNote(kajaCtx(), notes.NoteCreate{Title: "Sdílená"}))

	_, err := x.svc.MoveNote(kajaCtx(), priv.ID,
		notes.NoteMoveRequest{FolderID: &sharedFolder.ID, Position: "m"}, "")
	if status(t, err) != 422 {
		t.Errorf("moving a private note into the shared tree: %d, want 422 (D186)", status(t, err))
	}
	_, err = x.svc.MoveNote(kajaCtx(), shared.ID,
		notes.NoteMoveRequest{FolderID: &privFolder.ID, Position: "m"}, "")
	if status(t, err) != 422 {
		t.Errorf("moving a shared note into the private tree: %d, want 422 — this is the "+
			"direction that has no verb at all (D182/D186)", status(t, err))
	}
}

// TestSoukromeIsReservedAtTheSharedRoot (D185): the SPA routes
// /poznamky/soukrome/… as a literal, so a shared folder named "Soukromé" must not
// take that slug or the path becomes ambiguous.
func TestSoukromeIsReservedAtTheSharedRoot(t *testing.T) {
	x := newH(t)
	f := x.folder(x.svc.CreateFolder(kajaCtx(), notes.FolderCreate{Name: "Soukromé"}))
	if f.Slug == "soukrome" {
		t.Error("a shared root folder named Soukromé took the slug soukrome, which the SPA " +
			"routes as the private tree — the path now names two different things (D185)")
	}
	if f.Slug != "soukrome-2" {
		t.Errorf("slug = %q, want soukrome-2", f.Slug)
	}
	// Deeper down there is no ambiguity, so no reservation.
	parent := x.folder(x.svc.CreateFolder(kajaCtx(), notes.FolderCreate{Name: "Rodina"}))
	sub := x.folder(x.svc.CreateFolder(kajaCtx(), notes.FolderCreate{Name: "Soukromé", ParentID: &parent.ID}))
	if sub.Slug != "soukrome" {
		t.Errorf("nested folder slug = %q, want soukrome — the reservation is root-only", sub.Slug)
	}
}

// ---- Leak table row 18: the one asymmetry ----

// TestAdminMayPurgeButNeverReadAForeignPrivateNote is D181 written as a test,
// because in the source it reads like a bug and somebody will try to "fix" it.
func TestAdminMayPurgeButNeverReadAForeignPrivateNote(t *testing.T) {
	x := newH(t)
	n := x.privateNote(kajaCtx(), "Kajino", "tajné")

	if _, err := x.svc.GetNoteDetail(adminCtx(), n.ID); status(t, err) != 404 {
		t.Fatalf("admin reading a foreign private note: %d, want 404", status(t, err))
	}
	if err := x.svc.DeleteNote(adminCtx(), n.ID, true); err != nil {
		t.Fatalf("admin hard-deleting a foreign private note: %v — this is the ONE asymmetry "+
			"v9 grants (D181): somebody has to be able to reclaim space and remove a departed "+
			"member's files", err)
	}
	if _, err := x.svc.GetNoteDetail(kajaCtx(), n.ID); status(t, err) != 404 {
		t.Errorf("after the purge the owner still reads the note: %d", status(t, err))
	}
	meta := lastAuditMeta(t, x.db, "note.delete")
	if meta["owner_id"] != "u-kaja" {
		t.Errorf("audit meta.owner_id = %v, want u-kaja", meta["owner_id"])
	}
	if meta["by_admin"] != true {
		t.Errorf("audit meta.by_admin = %v, want true — the only trace of the one power that "+
			"crosses the privacy boundary (D181)", meta["by_admin"])
	}
}

// TestSoftDeleteStaysReversible is the asymmetry's other direction: owning
// something does not make you an admin, and a soft delete must not purge.
func TestSoftDeleteStaysReversible(t *testing.T) {
	x := newH(t)
	n := x.privateNote(kajaCtx(), "Kajino", "")
	if err := x.svc.DeleteNote(kajaCtx(), n.ID, false); err != nil {
		t.Fatalf("owner soft-deleting their own private note: %v", err)
	}
	var live int
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE id = ?`, n.ID).Scan(&live); err != nil {
		t.Fatalf("count: %v", err)
	}
	if live != 1 {
		t.Error("a soft delete removed the row — it must archive, so the delete stays reversible")
	}
}

// ---- Leak table row 11: the audit write ----

// TestPrivateMutationAuditsInFullWithTheMarker (row 11, D187). The summary and the
// diffs are written IN FULL — redaction is a READ-time concern, or the owner's own
// history is redacted from them permanently.
func TestPrivateMutationAuditsInFullWithTheMarker(t *testing.T) {
	x := newH(t)
	x.privateNote(kajaCtx(), "Velmi tajný název", "")

	summary, meta := lastAuditSummaryMeta(t, x.db, "note.create")
	if !strings.Contains(summary, "Velmi tajný název") {
		t.Errorf("stored summary = %q — it must carry the REAL title. Redacting at write time "+
			"would redact the record for the person whose history it is, permanently (D187)", summary)
	}
	if meta["visibility"] != "private" {
		t.Errorf("meta.visibility = %v, want private — without it no read path can tell this "+
			"event apart, so it could never be redacted (row 11)", meta["visibility"])
	}
	if meta["owner_id"] != "u-kaja" {
		t.Errorf("meta.owner_id = %v, want u-kaja", meta["owner_id"])
	}

	x.note(x.svc.CreateNote(kajaCtx(), notes.NoteCreate{Title: "Sdílená"}))
	_, sharedMeta := lastAuditSummaryMeta(t, x.db, "note.create")
	if sharedMeta["visibility"] != "shared" {
		t.Errorf("a shared event's meta.visibility = %v, want shared", sharedMeta["visibility"])
	}
	if _, ok := sharedMeta["owner_id"]; ok {
		t.Error("a shared event carries meta.owner_id — a shared item has no owner (D179)")
	}
}

// TestPublishIsAuditedWithTheVisibilityDiff: "this became visible to the
// household" is the most consequential thing that can happen to a private item, so
// it is its own action with an explicit diff rather than a flavour of move.
func TestPublishIsAuditedWithTheVisibilityDiff(t *testing.T) {
	x := newH(t)
	n := x.privateNote(kajaCtx(), "Recepty", "")
	if _, err := x.svc.PublishNote(kajaCtx(), n.ID, notes.PublishRequest{}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	var eventID string
	if err := x.db.QueryRow(
		`SELECT id FROM audit_events WHERE action = 'note.publish' ORDER BY id DESC LIMIT 1`).
		Scan(&eventID); err != nil {
		t.Fatalf("no note.publish event was written: %v", err)
	}
	rows, err := x.db.Query(
		`SELECT field, old_value, new_value FROM audit_changes WHERE event_id = ?`, eventID)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var field string
		var oldV, newV sql.NullString
		if err := rows.Scan(&field, &oldV, &newV); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if field == "visibility" {
			found = true
			if oldV.String != "private" || newV.String != "shared" {
				t.Errorf("visibility diff = %q to %q, want private to shared", oldV.String, newV.String)
			}
		}
	}
	if !found {
		t.Error("the publish event carries no `visibility` field diff (D182)")
	}
}

func lastAuditSummaryMeta(t *testing.T, db *sql.DB, action string) (string, map[string]any) {
	t.Helper()
	var summary string
	var meta sql.NullString
	err := db.QueryRow(
		`SELECT summary, meta FROM audit_events WHERE action = ? ORDER BY id DESC LIMIT 1`, action).
		Scan(&summary, &meta)
	if err != nil {
		t.Fatalf("read the last %s event: %v", action, err)
	}
	out := map[string]any{}
	if meta.Valid && meta.String != "" {
		if err := json.Unmarshal([]byte(meta.String), &out); err != nil {
			t.Fatalf("parse meta: %v", err)
		}
	}
	return summary, out
}

func lastAuditMeta(t *testing.T, db *sql.DB, action string) map[string]any {
	t.Helper()
	_, m := lastAuditSummaryMeta(t, db, action)
	return m
}
