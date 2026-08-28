package chat_test

// The custody transfer, and its fault-injection matrix (FR-V10-14, D238/D239/D245/D246).
//
// ⚠ THIS IS THE ONLY PATH IN v10 THAT CAN DESTROY DATA SILENTLY, which is why the
// PRD spends a table on it and why the matrix below is an acceptance criterion
// rather than a nicety. Five steps, two SQLite writes and two object-store calls,
// and no transaction covers all five — none can. What the ordering buys is that
// every crash point leaves the bytes OVER-COUNTED rather than lost:
//
//	crash after 2 → bytes in both places, no document row
//	crash after 3 → the document exists, the attachment is still `live`
//	crash after 4 → the attachment is `moved`, the chat object is still there
//
// Each of the four tests below asserts a state, asserts THE FILE IS STILL READABLE
// FROM SOMEWHERE, and then re-runs the move from that state and requires it to
// succeed. A test that only checked the error would pass against an implementation
// that deletes first.

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/chat"
)

// moveBody is the request the dialog sends after its publish sentence.
func moveBody(folderID string) string { return `{"folder_id":"` + folderID + `"}` }

// injected is what a test's hook returns. Its text is never rendered — the point is
// the state it leaves behind.
var injected = errors.New("chat: injected failure")

// stateOf reads the attachment row straight from the database, because the API
// deliberately cannot show every intermediate state the matrix names.
func (hh *storageHousehold) stateOf(attachmentID string) (state string, documentID string) {
	hh.t.Helper()
	var doc *string
	if err := hh.db.QueryRow(
		`SELECT state, document_id FROM chat_attachments WHERE id = ?`, attachmentID).
		Scan(&state, &doc); err != nil {
		hh.t.Fatalf("read the attachment row: %v", err)
	}
	if doc != nil {
		documentID = *doc
	}
	return state, documentID
}

// movable sets up one live attachment in a room `kaja` owns.
func movable(t *testing.T) (*storageHousehold, chat.Message) {
	t.Helper()
	hh := newStorageHousehold(t, kaja, andy, quiet)
	hh.join(kaja)
	room := hh.group(kaja, "Dovolená")
	return hh, hh.uploadOne(kaja, room.ID, "smlouva.pdf", pdfBytes())
}

// TestMoveHappyPath is the shape everything else is a deviation from (D245/D246).
func TestMoveHappyPath(t *testing.T) {
	hh, msg := movable(t)
	att := msg.Attachments[0]
	sourceKey := "chat/" + att.ID + "/original"

	rr := hh.as(kaja, "POST", "/api/chat/attachments/"+att.ID+"/move", moveBody("folder-1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("move answered %d, want 200: %s", rr.Code, rr.Body.String())
	}
	moved := decode[chat.Attachment](t, rr)
	if moved.State != "moved" {
		t.Errorf("state = %q, want moved", moved.State)
	}
	if moved.DocumentID == nil || moved.DocumentPath == nil {
		t.Fatalf("a moved attachment must name where it went, got %+v", moved)
	}

	// ⚠ THE BYTES LEFT THE `chat/` PREFIX, WHICH IS WHY THEY LEAVE BOTH THRESHOLDS
	// BY CONSTRUCTION (D246) rather than by bookkeeping that can drift.
	if hh.objectExists(sourceKey) {
		t.Error("the chat object survived a completed move — step 5 deletes it LAST, but it does delete it")
	}
	if state, _ := hh.stateOf(att.ID); state != "moved" {
		t.Errorf("the row says %q after a completed move", state)
	}

	// And it is gone from the clean-up listing on the next load (D246): the listing
	// is *what still counts*, not a history of what was done.
	page := hh.cleanupPage(kaja, "")
	if len(page.Items) != 0 {
		t.Errorf("a moved attachment is still listed for clean-up (%d rows) — the listing "+
			"must be what still counts", len(page.Items))
	}
}

// TestMoveFaultAtStep2 — the COPY fails. Nothing has happened anywhere.
func TestMoveFaultAtStep2(t *testing.T) {
	hh, msg := movable(t)
	att := msg.Attachments[0]
	sourceKey := "chat/" + att.ID + "/original"

	hh.sink.failBeforeCopy = true
	rr := hh.as(kaja, "POST", "/api/chat/attachments/"+att.ID+"/move", moveBody("folder-1"))
	if rr.Code == http.StatusOK {
		t.Fatal("a failed copy reported success")
	}

	// State: the attachment is untouched and the bytes are exactly where they were.
	if state, doc := hh.stateOf(att.ID); state != "live" || doc != "" {
		t.Errorf("after a failed copy the attachment is %q/%q, want live with no document", state, doc)
	}
	if !hh.objectExists(sourceKey) {
		t.Fatal("THE FILE IS GONE after a failed copy — never delete before the copy is confirmed (D238)")
	}
	if n := hh.countObjects("documents/"); n != 0 {
		t.Errorf("a failed copy left %d object(s) under documents/", n)
	}

	// Re-run: it succeeds.
	hh.sink.failBeforeCopy = false
	if rr := hh.as(kaja, "POST", "/api/chat/attachments/"+att.ID+"/move", moveBody("folder-1")); rr.Code != http.StatusOK {
		t.Fatalf("re-running the move after a failed copy answered %d: %s", rr.Code, rr.Body.String())
	}
	if state, _ := hh.stateOf(att.ID); state != "moved" {
		t.Errorf("the re-run left the attachment %q", state)
	}
}

// TestMoveFaultAtStep3 — the copy landed and the DOCUMENT ROW did not.
//
// The recovery table's first line: bytes in both places, no document row. An
// unattributed object under `documents/`, reported by v9's machinery and never
// auto-cleaned. ⚠ Re-running is safe, and it is what a member would do.
func TestMoveFaultAtStep3(t *testing.T) {
	hh, msg := movable(t)
	att := msg.Attachments[0]
	sourceKey := "chat/" + att.ID + "/original"

	hh.sink.failAfterCopy = true
	rr := hh.as(kaja, "POST", "/api/chat/attachments/"+att.ID+"/move", moveBody("folder-1"))
	if rr.Code == http.StatusOK {
		t.Fatal("a failed insert reported success")
	}

	if state, doc := hh.stateOf(att.ID); state != "live" || doc != "" {
		t.Errorf("after a failed insert the attachment is %q/%q, want live with no document — "+
			"never mark `moved` before the document row exists (D238)", state, doc)
	}
	if !hh.objectExists(sourceKey) {
		t.Fatal("THE FILE IS GONE after a failed insert")
	}
	// The over-count the ordering buys: an orphan under documents/, which is a
	// reported number rather than a lost file.
	if n := hh.countObjects("documents/"); n != 1 {
		t.Errorf("documents/ holds %d object(s) after a failed insert, want the 1 orphan "+
			"the recovery table describes", n)
	}

	hh.sink.failAfterCopy = false
	if rr := hh.as(kaja, "POST", "/api/chat/attachments/"+att.ID+"/move", moveBody("folder-1")); rr.Code != http.StatusOK {
		t.Fatalf("re-running the move after a failed insert answered %d: %s", rr.Code, rr.Body.String())
	}
	if state, _ := hh.stateOf(att.ID); state != "moved" {
		t.Errorf("the re-run left the attachment %q", state)
	}
}

// TestMoveFaultAtStep4 — the document exists and the MARK fails.
//
// The recovery table's second line: the file is in Dokumenty AND still counted
// against chat. Visible, fixable, re-runnable — and nothing is lost.
func TestMoveFaultAtStep4(t *testing.T) {
	hh, msg := movable(t)
	att := msg.Attachments[0]
	sourceKey := "chat/" + att.ID + "/original"

	hh.svc.InjectMoveFault(func(step chat.MoveStep) error {
		if step == chat.StepMark {
			return injected
		}
		return nil
	})
	rr := hh.as(kaja, "POST", "/api/chat/attachments/"+att.ID+"/move", moveBody("folder-1"))
	if rr.Code == http.StatusOK {
		t.Fatal("a failed mark reported success")
	}

	if state, doc := hh.stateOf(att.ID); state != "live" || doc != "" {
		t.Errorf("after a failed mark the attachment is %q/%q, want live with no document", state, doc)
	}
	if !hh.objectExists(sourceKey) {
		t.Fatal("THE FILE IS GONE after a failed mark — step 5 must not run when step 4 did not")
	}
	if hh.sink.count() != 1 {
		t.Errorf("the sink committed %d document(s), want 1 — the file IS in Dokumenty and "+
			"chat is still counting it, which is the over-count the ordering buys", hh.sink.count())
	}

	hh.svc.InjectMoveFault(nil)
	if rr := hh.as(kaja, "POST", "/api/chat/attachments/"+att.ID+"/move", moveBody("folder-1")); rr.Code != http.StatusOK {
		t.Fatalf("re-running the move after a failed mark answered %d: %s", rr.Code, rr.Body.String())
	}
	if state, _ := hh.stateOf(att.ID); state != "moved" {
		t.Errorf("the re-run left the attachment %q", state)
	}
	// ⚠ The re-run made a SECOND document, and that is the accepted cost. It
	// over-counts; it does not lose. The alternative — reusing whatever the previous
	// attempt left — would mean chat reasoning about rows in another module's tree.
	if hh.sink.count() != 2 {
		t.Errorf("the sink holds %d documents after a re-run, want 2 (over-count, never loss)",
			hh.sink.count())
	}
}

// TestMoveFaultAtStep5 — everything committed and the chat object could not be
// DELETED.
//
// The recovery table's third line: chat counts bytes it no longer owns, and the
// drain removes them next pass. ⚠ THE CALLER SEES SUCCESS, because the move
// genuinely happened — the file is in Dokumenty and the thread renders it from
// there. Reporting an error here would tell a member to retry a move that is done.
func TestMoveFaultAtStep5(t *testing.T) {
	hh, msg := movable(t)
	att := msg.Attachments[0]
	sourceKey := "chat/" + att.ID + "/original"

	hh.svc.InjectMoveFault(func(step chat.MoveStep) error {
		if step == chat.StepDelete {
			return injected
		}
		return nil
	})
	rr := hh.as(kaja, "POST", "/api/chat/attachments/"+att.ID+"/move", moveBody("folder-1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("a failed delete surfaced as %d — the move HAPPENED: %s", rr.Code, rr.Body.String())
	}
	if state, doc := hh.stateOf(att.ID); state != "moved" || doc == "" {
		t.Errorf("after a failed delete the attachment is %q/%q, want moved with a document", state, doc)
	}
	if !hh.objectExists(sourceKey) {
		t.Fatal("the object is gone although the delete was injected to fail — the test proves nothing")
	}
	// The queue is what turns a stale object into a scheduled one rather than an
	// orphan nothing will ever come back for.
	queued, err := hh.svc.QueuedKeysForTest()
	if err != nil {
		t.Fatalf("count the queue: %v", err)
	}
	if queued == 0 {
		t.Error("a failed step-5 delete queued nothing — chat would count those bytes forever")
	}

	// And the drain collects it, which is the recovery the table promises.
	hh.svc.InjectMoveFault(nil)
	if err := hh.svc.DrainJob(hh.ctx(kaja), nowForTest()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if hh.objectExists(sourceKey) {
		t.Error("the drain did not collect the object a failed move left behind")
	}
}

// TestMoveIsRefusedWithoutASink is D239.
//
// ⚠ 501, AND NEVER A FALLBACK TO DELETE. A capability that silently becomes a
// different, DESTRUCTIVE capability is worse than one that is plainly absent — and
// the assertion that matters is not the status code but that the file is still there
// afterwards.
func TestMoveIsRefusedWithoutASink(t *testing.T) {
	hh := newStorageHouseholdWith(t, nil, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Bez Dokumentů")
	msg := hh.uploadOne(kaja, room.ID, "smlouva.pdf", pdfBytes())
	att := msg.Attachments[0]
	sourceKey := "chat/" + att.ID + "/original"

	rr := hh.as(kaja, "POST", "/api/chat/attachments/"+att.ID+"/move", moveBody("folder-1"))
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("a move with no sink answered %d, want 501: %s", rr.Code, rr.Body.String())
	}
	if !hh.objectExists(sourceKey) {
		t.Fatal("THE MOVE FELL BACK TO A DELETE. That is the failure D239 exists to prevent: " +
			"an absent capability must stay absent, not become a destructive one.")
	}
	if state, _ := hh.stateOf(att.ID); state != "live" {
		t.Errorf("the attachment is %q after a 501, want live", state)
	}
}

// TestMoveIsRefusedForANonMemberAndAReader covers both halves of D241's gate on the
// action that widens access.
func TestMoveIsRefusedForANonMemberAndAReader(t *testing.T) {
	hh, msg := movable(t)
	att := msg.Attachments[0]
	path := "/api/chat/attachments/" + att.ID + "/move"

	// A non-member gets 404, not 403: a 403 would confirm the attachment exists.
	if rr := hh.as(andy, "POST", path, moveBody("folder-1")); rr.Code != http.StatusNotFound {
		t.Errorf("a non-member's move answered %d, want 404 (D217)", rr.Code)
	}
	// A reader IS told why — they can see the file, so hiding it would be a lie
	// about a row in front of them (D241's recorded asymmetry).
	hh.svc.InjectMoveFault(nil)
	rr := hh.as(quiet, "POST", path, moveBody("folder-1"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("a reader's move answered %d, want 403 with the reason named", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "zápis") {
		t.Errorf("the 403 should explain rather than scold, got %s", rr.Body.String())
	}
	if hh.sink.count() != 0 {
		t.Error("a refused move reached the sink")
	}
}

// TestMoveIsIdempotentlyRefusedOnASecondClick — 422 rather than a second transfer.
//
// ⚠ 422 AND NOT 404. The caller can SEE this attachment: it is in their thread,
// rendered as a moved file. Hiding it would be a lie about a row they are looking
// at, and 404 is reserved for the membership refusal where the point is that the id
// may not exist at all.
func TestMoveIsIdempotentlyRefusedOnASecondClick(t *testing.T) {
	hh, msg := movable(t)
	att := msg.Attachments[0]
	path := "/api/chat/attachments/" + att.ID + "/move"

	if rr := hh.as(kaja, "POST", path, moveBody("folder-1")); rr.Code != http.StatusOK {
		t.Fatalf("first move: %d %s", rr.Code, rr.Body.String())
	}
	rr := hh.as(kaja, "POST", path, moveBody("folder-1"))
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a second move answered %d, want 422", rr.Code)
	}
	if hh.sink.count() != 1 {
		t.Errorf("the second click transferred again (%d documents)", hh.sink.count())
	}
}

// TestMoveRequiresAFolder — the picker always sends one; a client that does not is
// refused rather than defaulted into somebody's root.
func TestMoveRequiresAFolder(t *testing.T) {
	hh, msg := movable(t)
	rr := hh.as(kaja, "POST", "/api/chat/attachments/"+msg.Attachments[0].ID+"/move", `{"folder_id":""}`)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a move with no folder answered %d, want 422", rr.Code)
	}
}
