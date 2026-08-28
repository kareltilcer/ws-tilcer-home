package chat_test

// Úklid úložiště chatu, the storage picture, the two thresholds and the drain
// (FR-V10-12/13/14/22, D237/D241/D242/D243/D246/D247/D254).

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/chat"
)

func nowForTest() time.Time { return time.Now().UTC() }

// tsFormat mirrors the module's own house timestamp (chat/floor.go), which is
// unexported. Restated rather than exported: a test needing to PARSE a wire value is
// not a reason to widen the package's surface.
const tsFormat = "2006-01-02T15:04:05.000Z07:00"

func (hh *storageHousehold) cleanupPage(m member, query string) chat.CleanupPage {
	hh.t.Helper()
	rr := hh.as(m, "GET", "/api/chat/cleanup"+query, "")
	if rr.Code != http.StatusOK {
		hh.t.Fatalf("GET /api/chat/cleanup%s: %d %s", query, rr.Code, rr.Body.String())
	}
	return decode[chat.CleanupPage](hh.t, rr)
}

func (hh *storageHousehold) thread(m member, conversationID string) chat.MessagePage {
	hh.t.Helper()
	rr := hh.as(m, "GET", "/api/chat/conversations/"+conversationID+"/messages", "")
	if rr.Code != http.StatusOK {
		hh.t.Fatalf("GET thread: %d %s", rr.Code, rr.Body.String())
	}
	return decode[chat.MessagePage](hh.t, rr)
}

func (hh *storageHousehold) storagePicture(m member) chat.ChatStorage {
	hh.t.Helper()
	rr := hh.as(m, "GET", "/api/chat/storage", "")
	if rr.Code != http.StatusOK {
		hh.t.Fatalf("GET /api/chat/storage: %d %s", rr.Code, rr.Body.String())
	}
	return decode[chat.ChatStorage](hh.t, rr)
}

// ---- the gate ----

// TestCleanupGateIs403ForAReaderAndEmptyForAMemberOfNothing is D241's intersection,
// and the two halves answer DIFFERENTLY on purpose.
//
// ⚠ A READER GETS 403 WITH THE REASON NAMED. This is the module's one recorded
// asymmetry — a reader can fill storage they can never clean — and the copy should
// be straightforward about it rather than vague. The warning does not offer them the
// link in the first place, which is what `can_clean_up` on the storage picture is
// for.
//
// ⚠ A MEMBER OF NO CONVERSATION GETS AN EMPTY PAGE, NOT A 403. The gate passed;
// there is simply nothing to clean. Refusing them would answer a different question
// from the one they asked.
func TestCleanupGateIs403ForAReaderAndEmptyForAMemberOfNothing(t *testing.T) {
	hh := newStorageHousehold(t, kaja, andy, quiet)
	hh.join(kaja)
	hh.join(quiet)
	room := hh.group(kaja, "Fotky")
	hh.uploadOne(kaja, room.ID, "a.png", pngBytes)

	rr := hh.as(quiet, "GET", "/api/chat/cleanup", "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("a reader answered %d, want 403 with the reason named (D241)", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "zápis") {
		t.Errorf("the reader's 403 should explain rather than scold, got %s", rr.Body.String())
	}

	// `andy` is an editor who has never opened chat, so he belongs to nothing.
	page := hh.cleanupPage(andy, "")
	if len(page.Items) != 0 {
		t.Errorf("a member of no conversation was shown %d row(s) — the listing is "+
			"THEIR conversations only (D241)", len(page.Items))
	}
	if page.TotalBytes == nil || *page.TotalBytes != 0 {
		t.Errorf("total_bytes = %v for a member of nothing, want 0 (measured, not unmeasured)",
			page.TotalBytes)
	}
}

// TestCleanupListsOnlyTheCallersOwnLiveAttachments is leak row 15, four predicates
// at once.
func TestCleanupListsOnlyTheCallersOwnLiveAttachments(t *testing.T) {
	hh := newStorageHousehold(t, kaja, andy)
	hh.join(kaja)
	hh.join(andy)
	mine := hh.group(kaja, "Moje")
	theirs := hh.group(andy, "Jejich")
	hh.uploadOne(kaja, mine.ID, "moje.pdf", pdfBytes())
	hh.uploadOne(andy, theirs.ID, "jejich.pdf", pdfBytes())

	page := hh.cleanupPage(kaja, "")
	if len(page.Items) != 1 {
		t.Fatalf("the listing shows %d rows, want 1 — a conversation the caller is not in "+
			"must not appear (leak row 15)", len(page.Items))
	}
	if got := page.Items[0].Attachment.OriginalFilename; got != "moje.pdf" {
		t.Errorf("the listing shows %q — that is somebody else's room", got)
	}
	if page.Items[0].ConversationName != "Moje" {
		t.Errorf("conversation_name = %q", page.Items[0].ConversationName)
	}

	// A trashed conversation leaves EVERY read, the clean-up listing included (D253).
	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+mine.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("trash the room: %d %s", rr.Code, rr.Body.String())
	}
	if after := hh.cleanupPage(kaja, ""); len(after.Items) != 0 {
		t.Errorf("a trashed conversation is still on the clean-up page (%d rows)", len(after.Items))
	}
}

// TestCleanupRespectsTheFloor is D218 on the storage surface.
//
// ⚠ A MEMBER ADDED MIDWAY CANNOT CLEAN WHAT THEY CANNOT SEE, and the reason is not
// tidiness: the listing carries filenames, sizes and uploaders, so an unfloored
// listing would hand a new member the metadata of every file sent before they
// arrived — and a delete button for it.
func TestCleanupRespectsTheFloor(t *testing.T) {
	hh := newStorageHousehold(t, kaja, andy)
	hh.join(kaja)
	hh.join(andy)
	room := hh.group(kaja, "Historie")
	hh.uploadOne(kaja, room.ID, "pred-pridanim.pdf", pdfBytes())

	if rr := hh.as(kaja, "POST", "/api/chat/conversations/"+room.ID+"/members",
		`{"user_id":"`+andy.id+`"}`); rr.Code != http.StatusOK {
		t.Fatalf("add member: %d %s", rr.Code, rr.Body.String())
	}
	hh.uploadOne(kaja, room.ID, "po-pridani.pdf", pdfBytes())

	page := hh.cleanupPage(andy, "")
	if len(page.Items) != 1 {
		t.Fatalf("a member added midway sees %d rows, want 1", len(page.Items))
	}
	if got := page.Items[0].Attachment.OriginalFilename; got != "po-pridani.pdf" {
		t.Errorf("the floor let %q through", got)
	}
	// ⚠ AND THE TOTAL AGREES WITH THE ROWS. A floor applied after the fetch leaks
	// through exactly this figure even with the row gone.
	if page.TotalBytes == nil || *page.TotalBytes != page.Items[0].Attachment.ByteSize {
		t.Errorf("total_bytes = %v over %d visible byte(s) — the total must be computed from "+
			"the same predicate as the rows", page.TotalBytes, page.Items[0].Attachment.ByteSize)
	}
}

// TestCleanupRefusesACursorUnderSortSize is the §V9 `private-items` precedent, which
// chat's own search already follows for a rank ordering.
func TestCleanupRefusesACursorUnderSortSize(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Velké")
	hh.uploadOne(kaja, room.ID, "a.pdf", pdfBytes())

	if rr := hh.as(kaja, "GET", "/api/chat/cleanup?sort=size&cursor=abc", ""); rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a cursor under sort=size answered %d, want 422 — silently ignoring it "+
			"serves page one forever, which reads as the end of the list", rr.Code)
	}
	// `recent` pages normally.
	if page := hh.cleanupPage(kaja, "?sort=recent"); len(page.Items) != 1 {
		t.Errorf("sort=recent returned %d rows", len(page.Items))
	}
	if rr := hh.as(kaja, "GET", "/api/chat/cleanup?sort=nesmysl", ""); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("an unknown sort answered %d, want 422", rr.Code)
	}
}

// TestCleanupSortSizeNeverOffersAnotherPage — the listing is single-page by
// construction, so `next_cursor` must stay null however many rows exist.
func TestCleanupSortSizeNeverOffersAnotherPage(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Hodně souborů")
	for i := 0; i < 3; i++ {
		hh.uploadOne(kaja, room.ID, "f.pdf", pdfBytes())
	}
	page := hh.cleanupPage(kaja, "?sort=size&limit=1")
	if page.NextCursor != nil {
		t.Errorf("sort=size offered next_cursor %q — there is no cursor that can resume a "+
			"size ordering, so a Load-more here has nothing to send", *page.NextCursor)
	}
}

// ---- Odstranit ----

// TestOdstranitDeletesInlineAndLeavesAnEpitaph is D243 and D247's exception.
func TestOdstranitDeletesInlineAndLeavesAnEpitaph(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Úklid")
	msg := hh.uploadOne(kaja, room.ID, "smlouva-2026.pdf", pdfBytes())
	att := msg.Attachments[0]
	sourceKey := "chat/" + att.ID + "/original"

	beforeTotal := hh.storagePicture(kaja).TotalBytes
	if rr := hh.as(kaja, "DELETE", "/api/chat/attachments/"+att.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("Odstranit answered %d: %s", rr.Code, rr.Body.String())
	}

	// ⚠ INLINE, WHICH IS WHAT MAKES THE PAGE USABLE. The workflow is *clean until the
	// number goes down*, and a figure lagging fifteen minutes behind the button is
	// the reason this one path does not enqueue.
	if hh.objectExists(sourceKey) {
		t.Error("Odstranit queued the object instead of deleting it — the figure would lag " +
			"fifteen minutes behind the button (D247)")
	}
	after := hh.storagePicture(kaja)
	if after.TotalBytes == nil || beforeTotal == nil || *after.TotalBytes >= *beforeTotal {
		t.Errorf("the total did not fall immediately: %v → %v", beforeTotal, after.TotalBytes)
	}

	// ⚠ THE EPITAPH SURVIVES. The thread stays legible, a member can ask for the file
	// again knowing exactly what it was, and the clean-up is attributed (D243).
	thread := hh.thread(kaja, room.ID)
	var found bool
	for _, m := range thread.Items {
		for _, a := range m.Attachments {
			if a.ID != att.ID {
				continue
			}
			found = true
			if a.State != "removed" {
				t.Errorf("state = %q, want removed", a.State)
			}
			if a.OriginalFilename != "smlouva-2026.pdf" || a.ByteSize == 0 {
				t.Errorf("the epitaph lost its filename or size: %q / %d", a.OriginalFilename, a.ByteSize)
			}
			if a.CleanedByLabel == nil || *a.CleanedByLabel != kaja.name {
				t.Errorf("cleaned_by_label = %v, want %q — the clean-up is attributed", a.CleanedByLabel, kaja.name)
			}
			if a.CleanedAt == nil {
				t.Error("cleaned_at is null on a removed attachment")
			}
		}
	}
	if !found {
		t.Fatal("the removed attachment vanished from the thread — the row survives (D243)")
	}
	// And it is gone from the clean-up listing: the listing is what still counts.
	if page := hh.cleanupPage(kaja, ""); len(page.Items) != 0 {
		t.Errorf("a removed attachment is still listed for clean-up (%d rows)", len(page.Items))
	}
}

// TestOdstranitIsRefusedTwice — one click, one event, one delete.
func TestOdstranitIsRefusedTwice(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Úklid")
	msg := hh.uploadOne(kaja, room.ID, "a.pdf", pdfBytes())
	path := "/api/chat/attachments/" + msg.Attachments[0].ID

	if rr := hh.as(kaja, "DELETE", path, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("first delete: %d", rr.Code)
	}
	if rr := hh.as(kaja, "DELETE", path, ""); rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a second delete answered %d, want 422", rr.Code)
	}
	var events int
	if err := hh.db.QueryRow(
		`SELECT COUNT(*) FROM audit_events WHERE action = 'attachment.removed'`).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 1 {
		t.Errorf("%d attachment.removed events, want 1", events)
	}
}

// ---- the storage picture ----

// TestStoragePictureShowsTheHouseholdTotalAndOnlyTheCallersRooms is FR-V10-12.
func TestStoragePictureShowsTheHouseholdTotalAndOnlyTheCallersRooms(t *testing.T) {
	hh := newStorageHousehold(t, kaja, andy, quiet)
	hh.join(kaja)
	hh.join(andy)
	mine := hh.group(kaja, "Moje")
	theirs := hh.group(andy, "Jejich")
	hh.uploadOne(kaja, mine.ID, "moje.pdf", pdfBytes())
	hh.uploadOne(andy, theirs.ID, "jejich.pdf", pdfBytes())

	picture := hh.storagePicture(kaja)
	// ⚠ THE TOTAL IS THE HOUSEHOLD'S — everyone sees the same figure, because
	// `chat.total` is a threshold about the BUCKET and a warning nobody can see is
	// not a warning.
	if picture.TotalBytes == nil || *picture.TotalBytes != int64(2*len(pdfBytes())) {
		t.Errorf("total_bytes = %v, want the whole household's %d", picture.TotalBytes, 2*len(pdfBytes()))
	}
	// The ROWS are the caller's own. Všichni is one of them, so the room the caller
	// is not in is what must be missing.
	for _, row := range picture.Conversations {
		if row.ID == theirs.ID {
			t.Errorf("a conversation the caller is not in appears in their storage picture: %q", row.Name)
		}
	}
	if picture.ThresholdTotalMB != 512 || picture.ThresholdConversationMB != 128 {
		t.Errorf("thresholds = %d/%d MB, want the seeded 512/128 (D237)",
			picture.ThresholdTotalMB, picture.ThresholdConversationMB)
	}
	if !picture.CanCleanUp {
		t.Error("can_clean_up is false for an editor — the banner would hide a link they can use")
	}
	// ⚠ THE READER'S BANNER MUST NOT OFFER THE LINK (D241). The server answers the
	// intersection because the client holds only half of it.
	if hh.storagePicture(quietIn(hh)).CanCleanUp {
		t.Error("can_clean_up is true for a reader — the warning would offer a link that 403s them")
	}
}

// quietIn registers the reader lazily: most tests here do not need one, and a member
// who never makes a request is never enrolled in Všichni.
func quietIn(hh *storageHousehold) member {
	if _, ok := hh.handlers[quiet.id]; !ok {
		hh.t.Fatalf("this household has no reader registered")
	}
	hh.join(quiet)
	return quiet
}

// TestTrashedConversationBytesStillCount is D254, and it is the figure that looks
// wrong and is right.
//
// ⚠ THE BYTES ARE STILL IN R2, so reporting them as freed would make the page lie
// for a week — and the page's whole premise is that its figures sum. *Smazat
// natrvalo* is what exists so that never traps anyone.
func TestTrashedConversationBytesStillCount(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Ke smazání")
	hh.uploadOne(kaja, room.ID, "velky.pdf", pdfBytes())

	before := *hh.storagePicture(kaja).TotalBytes
	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+room.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("trash: %d %s", rr.Code, rr.Body.String())
	}
	after := *hh.storagePicture(kaja).TotalBytes
	if after != before {
		t.Errorf("chat's total fell from %d to %d when a room was TRASHED — the bytes are "+
			"still in R2 and the page has to sum (D254)", before, after)
	}
}

// ---- the drain ----

// TestDrainRemovesQueuedObjectsAndClearsTheQueue is D247's cadence, minus the wait.
func TestDrainRemovesQueuedObjectsAndClearsTheQueue(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Zprávy")
	msg := hh.uploadOne(kaja, room.ID, "a.pdf", pdfBytes())
	key := "chat/" + msg.Attachments[0].ID + "/original"

	// A MESSAGE delete enqueues (purge_after = now) rather than deleting inline —
	// the opposite of the clean-up page, and for the opposite reason.
	if rr := hh.as(kaja, "DELETE", "/api/chat/messages/"+msg.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("delete the message: %d %s", rr.Code, rr.Body.String())
	}
	if !hh.objectExists(key) {
		t.Fatal("a message delete removed the object inline — four hundred attachments is " +
			"not a thing to do inside a request (D247)")
	}
	if queued, _ := hh.svc.QueuedKeysForTest(); queued == 0 {
		t.Fatal("a message delete queued nothing")
	}

	if err := hh.svc.DrainJob(hh.ctx(kaja), nowForTest()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if hh.objectExists(key) {
		t.Error("the drain left the object behind")
	}
	if queued, _ := hh.svc.QueuedKeysForTest(); queued != 0 {
		t.Errorf("the drain left %d row(s) in the queue", queued)
	}
}

// TestRestoreWithdrawsTheQueuedKeys is D253's *Obnovit*: nothing is reconstructed,
// because nothing was ever removed.
func TestRestoreWithdrawsTheQueuedKeys(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Omylem smazané")
	msg := hh.uploadOne(kaja, room.ID, "dulezite.pdf", pdfBytes())
	key := "chat/" + msg.Attachments[0].ID + "/original"

	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+room.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("trash: %d", rr.Code)
	}
	if queued, _ := hh.svc.QueuedKeysForTest(); queued == 0 {
		t.Fatal("trashing a room queued nothing — the drain would never free its bytes")
	}
	if rr := hh.as(kaja, "POST", "/api/chat/conversations/"+room.ID+"/restore", ""); rr.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", rr.Code, rr.Body.String())
	}
	if queued, _ := hh.svc.QueuedKeysForTest(); queued != 0 {
		t.Errorf("restore left %d queued key(s) — the file would be destroyed a week after "+
			"being recovered", queued)
	}
	// A drain now must not touch it.
	if err := hh.svc.DrainJob(hh.ctx(kaja), nowForTest().Add(30*24*time.Hour)); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if !hh.objectExists(key) {
		t.Fatal("THE RESTORED FILE WAS DRAINED — restore withdraws the promise to delete (D253)")
	}
}

// TestSmazatNatrvaloFreesTheBytesOnTheNextPass is D254's escape hatch: deleting
// frees the space in seven days, purging frees it now.
func TestSmazatNatrvaloFreesTheBytesOnTheNextPass(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Natrvalo")
	msg := hh.uploadOne(kaja, room.ID, "velky.pdf", pdfBytes())
	key := "chat/" + msg.Attachments[0].ID + "/original"

	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+room.ID+"?hard=true", ""); rr.Code != http.StatusNoContent {
		t.Fatalf("purge: %d %s", rr.Code, rr.Body.String())
	}
	if err := hh.svc.DrainJob(hh.ctx(kaja), nowForTest()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if hh.objectExists(key) {
		t.Error("Smazat natrvalo did not free the bytes on the next pass — waiting out the koš " +
			"window is not an answer for somebody who purged to fix an overrun (D254)")
	}
	if total := hh.storagePicture(kaja).TotalBytes; total == nil || *total != 0 {
		t.Errorf("the total is %v after a purge and a drain, want 0", total)
	}
}

// TestAMovedAttachmentSurvivesThePurge is the failure the ordering exists to make
// impossible: *"the purge deleted the file I had saved into Dokumenty"*.
func TestAMovedAttachmentSurvivesThePurge(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Přesun a smazání")
	msg := hh.uploadOne(kaja, room.ID, "smlouva.pdf", pdfBytes())
	att := msg.Attachments[0]

	if rr := hh.as(kaja, "POST", "/api/chat/attachments/"+att.ID+"/move", moveBody("folder-1")); rr.Code != http.StatusOK {
		t.Fatalf("move: %d %s", rr.Code, rr.Body.String())
	}
	documentKey := "documents/doc-1/original"
	if !hh.objectExists(documentKey) {
		t.Fatalf("the move did not land a document object; the test proves nothing")
	}

	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+room.ID+"?hard=true", ""); rr.Code != http.StatusNoContent {
		t.Fatalf("purge: %d %s", rr.Code, rr.Body.String())
	}
	if err := hh.svc.DrainJob(hh.ctx(kaja), nowForTest()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if !hh.objectExists(documentKey) {
		t.Fatal("PURGING THE CONVERSATION DESTROYED THE DOCUMENT IT HAD BEEN MOVED INTO. " +
			"The move takes the bytes out of the chat/ prefix precisely so a later purge " +
			"cannot reach them (D246).")
	}
}

// TestKosPurgeIsNotEarly — the drain must respect the window, or the koš is theatre.
func TestKosPurgeIsNotEarly(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Ještě v koši")
	msg := hh.uploadOne(kaja, room.ID, "a.pdf", pdfBytes())
	key := "chat/" + msg.Attachments[0].ID + "/original"

	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+room.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("trash: %d", rr.Code)
	}
	if err := hh.svc.DrainJob(hh.ctx(kaja), nowForTest()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if !hh.objectExists(key) {
		t.Fatal("the drain destroyed a conversation's bytes on the day it was trashed — the " +
			"koš is seven days (D253)")
	}
	// The room is still restorable, which is the property the window buys.
	if rr := hh.as(kaja, "GET", "/api/chat/conversations?state=trash", ""); rr.Code != http.StatusOK {
		t.Fatalf("koš listing: %d", rr.Code)
	}
	// Past the window it goes.
	if err := hh.svc.DrainJob(hh.ctx(kaja), nowForTest().Add(8*24*time.Hour)); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if hh.objectExists(key) {
		t.Error("the drain did not purge a conversation whose window had elapsed")
	}
}

// TestChatBlobsAreReportedSharedNotPrivate is §11.2/D235, and it is the one place
// the word being wrong actually matters.
//
// ⚠ THE ALTERNATIVE IS NOT "a better word", IT IS THE WRONG SECTION. `Kind:
// private` is what carries an OwnerID through storage.Attribute, so attributing to
// the uploader would file chat's bytes under Úložiště's *Soukromé* breakdown beside
// v9 private notes and documents — a section that means "items the purge screen
// owns". Chat implements no PrivateInventory, so those rows would name bytes that
// screen can neither list nor delete.
func TestChatBlobsAreReportedSharedNotPrivate(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Fotky")
	hh.uploadOne(kaja, room.ID, "a.png", pngBytes)

	usage, err := hh.svc.StorageBlobs(hh.ctx(kaja))
	if err != nil {
		t.Fatalf("StorageBlobs: %v", err)
	}
	var sharedBytes, privateRows int64
	for _, u := range usage {
		switch u.Kind {
		case "shared":
			sharedBytes += u.Bytes
		case "private":
			privateRows++
		}
		if u.Prefix != "chat/" {
			t.Errorf("a usage row claims prefix %q, want chat/", u.Prefix)
		}
	}
	if privateRows != 0 {
		t.Errorf("%d private row(s) — a chat attachment is member-restricted, which is a "+
			"THIRD thing, and reporting it as a v9 private item files it under a screen "+
			"that cannot touch it (D235)", privateRows)
	}
	if sharedBytes != int64(len(pngBytes)) {
		t.Errorf("shared bytes = %d, want %d", sharedBytes, len(pngBytes))
	}

	// ⚠ AND THE ORPHAN ROW IS ALWAYS EMITTED, empty or not. An absent row reads as
	// "nobody looked"; a zero reads as "no orphan backlog", which is good news worth
	// stating on a maintenance page.
	var sawOrphanRow bool
	for _, u := range usage {
		if u.Kind == "unattributed" {
			sawOrphanRow = true
			if u.Objects != 0 {
				t.Errorf("%d unattributed object(s) after a clean upload", u.Objects)
			}
		}
	}
	if !sawOrphanRow {
		t.Error("no unattributed row — its absence and its zero mean different things")
	}
}

// TestDeletedMessageBytesLeaveTheTotal is the drift the review found, pinned.
//
// ⚠ A MESSAGE DELETE QUEUES ITS ATTACHMENT'S KEYS AND LEAVES THE ROW `live` — the
// row survives so replies do not point at nothing, and the attachment is not an
// epitaph (the MESSAGE is the tombstone). So a figure that filters on `state` alone
// keeps counting bytes the drain has already destroyed, forever: the clean-up
// listing correctly excludes a deleted message's rows, so nothing on that page can
// ever bring the number down, and the threshold warns about bytes nobody can free.
//
// The predicate is `state = 'live' AND the message is not deleted`, in one place
// (storage.go's ownedBytes) and in every figure built from it.
func TestDeletedMessageBytesLeaveTheTotal(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Smazané zprávy")
	msg := hh.uploadOne(kaja, room.ID, "velky.pdf", pdfBytes())

	if before := *hh.storagePicture(kaja).TotalBytes; before == 0 {
		t.Fatal("the fixture measured nothing; the test would pass vacuously")
	}
	if rr := hh.as(kaja, "DELETE", "/api/chat/messages/"+msg.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("delete the message: %d %s", rr.Code, rr.Body.String())
	}

	// ⚠ IMMEDIATELY, NOT AFTER THE DRAIN. The bytes are promised to the drain the
	// moment the message is deleted, and a member watching the figure should not have
	// to wait fifteen minutes to see the room they just emptied reported as empty.
	picture := hh.storagePicture(kaja)
	if picture.TotalBytes == nil || *picture.TotalBytes != 0 {
		t.Errorf("total_bytes = %v after deleting the only message that carried a file, want 0",
			picture.TotalBytes)
	}
	for _, c := range picture.Conversations {
		if c.ID != room.ID {
			continue
		}
		// ⚠ THE ROOM STILL HAS A ROW, reporting zero. Dropping the row instead would
		// make the caller's two figures cover different sets of rooms.
		if c.Bytes == nil || *c.Bytes != 0 {
			t.Errorf("the room reports %v bytes, want a measured 0", c.Bytes)
		}
	}

	// The admin's block agrees, because it is the same rule.
	groups, err := hh.svc.StorageGroups(hh.ctx(kaja))
	if err != nil {
		t.Fatalf("StorageGroups: %v", err)
	}
	for _, g := range groups {
		if g.ID == room.ID && g.Bytes != 0 {
			t.Errorf("Administrace reports %d bytes for a room whose only file is in a "+
				"deleted message", g.Bytes)
		}
	}

	// And the drain then really does destroy them, so the figure was telling the truth.
	if err := hh.svc.DrainJob(hh.ctx(kaja), nowForTest()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if n := hh.countObjects("chat/"); n != 0 {
		t.Errorf("%d object(s) survived the drain", n)
	}
}

// TestTrashedRoomReportsItsPurgeDeadline — the admin's *v koši · zbývá N dní*.
//
// ⚠ IT IS DERIVED FROM deleted_at, NOT READ OUT OF THE DRAIN'S QUEUE. The first
// version looked it up by joining chat_deleted_keys to chat_attachments on
// `storage_key`, a column with no index, once per conversation — for a value that is
// just the room's own deleted_at plus HOME_CHAT_TRASH_DAYS.
func TestTrashedRoomReportsItsPurgeDeadline(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Do koše")
	hh.uploadOne(kaja, room.ID, "a.pdf", pdfBytes())
	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+room.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("trash: %d", rr.Code)
	}

	groups, err := hh.svc.StorageGroups(hh.ctx(kaja))
	if err != nil {
		t.Fatalf("StorageGroups: %v", err)
	}
	for _, g := range groups {
		if g.ID != room.ID {
			continue
		}
		if g.TrashedAt == "" {
			t.Fatal("a trashed room is not flagged")
		}
		if g.PurgeAfter == "" {
			t.Fatal("a trashed room reports no deadline — the koš countdown has nothing to render")
		}
		trashed, err := time.Parse(tsFormat, g.TrashedAt)
		if err != nil {
			t.Fatalf("parse trashed_at %q: %v", g.TrashedAt, err)
		}
		purge, err := time.Parse(tsFormat, g.PurgeAfter)
		if err != nil {
			t.Fatalf("parse purge_after %q: %v", g.PurgeAfter, err)
		}
		if days := purge.Sub(trashed).Hours() / 24; days < 6.9 || days > 7.1 {
			t.Errorf("the deadline is %.1f days after the delete, want HOME_CHAT_TRASH_DAYS (7)", days)
		}
		// ⚠ AND ITS BYTES ARE STILL COUNTED (D254). A trashed room's files are still in
		// R2, and this page's premise is that its figures sum.
		if g.Bytes == 0 {
			t.Error("a trashed room reports 0 bytes — they are still in R2 until the drain runs (D254)")
		}
	}
}

// TestTrashingARoomDoesNotDelayAnAlreadyDueKey is the queue's MIN rule.
//
// ⚠ THE QUEUE HOLDS THE EARLIEST PROMISE, and an overwrite broke that in the one
// direction that matters. A message delete queues at `purge_after = now`, due on the
// next pass; trashing the room afterwards used to rewrite those keys to
// `now + HOME_CHAT_TRASH_DAYS`, so bytes a member watched leave the thread on Monday
// were still in R2 the following week with nothing on screen explaining it.
func TestTrashingARoomDoesNotDelayAnAlreadyDueKey(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Nejprve zpráva")
	msg := hh.uploadOne(kaja, room.ID, "smazany.pdf", pdfBytes())
	key := "chat/" + msg.Attachments[0].ID + "/original"

	// Monday: the message goes. Its key is queued at `now`.
	if rr := hh.as(kaja, "DELETE", "/api/chat/messages/"+msg.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("delete the message: %d", rr.Code)
	}
	// Monday, a moment later: the whole room goes to the koš, which queues at now+7d.
	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+room.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("trash the room: %d", rr.Code)
	}

	// The next pass must still collect the message's bytes: they were promised for
	// today, and nothing that happened afterwards un-promised them.
	if err := hh.svc.DrainJob(hh.ctx(kaja), nowForTest()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if hh.objectExists(key) {
		t.Error("trashing the room pushed an already-due key out by the koš window — the " +
			"queue must hold the EARLIEST promise, not the most recent one")
	}
}

// TestSmazatNatrvaloStillBringsADeadlineForward is the other direction of the same
// rule: MIN must not stop *Smazat natrvalo* from pulling a pending deadline in.
func TestSmazatNatrvaloStillBringsADeadlineForward(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Napřed do koše")
	msg := hh.uploadOne(kaja, room.ID, "velky.pdf", pdfBytes())
	key := "chat/" + msg.Attachments[0].ID + "/original"

	// Into the koš first: the key is queued seven days out.
	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+room.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("trash: %d", rr.Code)
	}
	if err := hh.svc.DrainJob(hh.ctx(kaja), nowForTest()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if !hh.objectExists(key) {
		t.Fatal("the drain took a key that was not due yet")
	}
	// Then purged: the deadline has to come forward to now.
	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+room.ID+"?hard=true", ""); rr.Code != http.StatusNoContent {
		t.Fatalf("purge: %d %s", rr.Code, rr.Body.String())
	}
	if err := hh.svc.DrainJob(hh.ctx(kaja), nowForTest()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if hh.objectExists(key) {
		t.Error("Smazat natrvalo did not bring the deadline forward — waiting out the window " +
			"is not an answer for somebody who purged to fix an overrun (D254)")
	}
}

// TestCleanupRepublishesTheBubble is D243 for everybody who is not clicking.
//
// ⚠ A `chat_conversation.changed` FRAME IS NOT ENOUGH, and sending only that left
// every other member looking at a dead file. That frame means "refetch this room",
// and the client answers it by invalidating the conversation and the two listings —
// deliberately NOT the thread. So a removal left other members' open threads still
// rendering the attachment, with /raw now 404 because the object was deleted inline,
// and the epitaph appearing only for the person who clicked.
func TestCleanupRepublishesTheBubble(t *testing.T) {
	hh := newStorageHousehold(t, kaja, andy)
	hh.join(kaja)
	hh.join(andy)
	room := hh.group(kaja, "Společná", andy)
	msg := hh.uploadOne(kaja, room.ID, "velky.pdf", pdfBytes())
	hh.notify.reset()

	if rr := hh.as(kaja, "DELETE", "/api/chat/attachments/"+msg.Attachments[0].ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("Odstranit: %d %s", rr.Code, rr.Body.String())
	}

	var (
		sawMessageFrame bool
		epitaph         chat.Attachment
		audience        []string
	)
	for i, typ := range hh.notify.types {
		if typ != "chat_message.updated" {
			continue
		}
		ev, ok := hh.notify.payloads[i].(chat.MessageEvent)
		if !ok || len(ev.Message.Attachments) == 0 {
			continue
		}
		sawMessageFrame = true
		epitaph = ev.Message.Attachments[0]
		audience = hh.notify.audiences[i]
	}
	if !sawMessageFrame {
		t.Fatal("a removal published no chat_message.updated — every other member's open " +
			"thread goes on rendering a file whose bytes are gone, and the epitaph (D243) " +
			"never reaches them")
	}
	// The frame carries the epitaph, so the other member's bubble becomes legible
	// without a refetch.
	if epitaph.State != "removed" {
		t.Errorf("the republished attachment is %q, want removed", epitaph.State)
	}
	if epitaph.OriginalFilename != "velky.pdf" || epitaph.ByteSize == 0 {
		t.Errorf("the republished epitaph lost its filename or size: %+v", epitaph)
	}
	if epitaph.CleanedByLabel == nil {
		t.Error("the republished epitaph is not attributed")
	}
	// ⚠ AND IT REACHES BOTH MEMBERS. The audience is MemberIDsAbove — the floor — so
	// a member added AFTER this message would be excluded, but andy was there.
	if len(audience) != 2 {
		t.Errorf("the frame reached %v, want both members", audience)
	}
}

// TestCleanupFrameRespectsTheFloor — the audience is bounded by D218, not by
// membership alone.
//
// ⚠ IT IS AN OLD MESSAGE. Somebody added to the room afterwards is bounded off it by
// every read path, so publishing its body to them would hand their socket exactly
// what the floor exists to withhold — the mistake EditMessage was corrected for in
// PR 2, in a verb that did not exist yet.
func TestCleanupFrameRespectsTheFloor(t *testing.T) {
	hh := newStorageHousehold(t, kaja, andy)
	hh.join(kaja)
	hh.join(andy)
	room := hh.group(kaja, "Historie")
	msg := hh.uploadOne(kaja, room.ID, "stary.pdf", pdfBytes())

	// andy joins AFTER the message, so his floor sits above it.
	if rr := hh.as(kaja, "POST", "/api/chat/conversations/"+room.ID+"/members",
		`{"user_id":"`+andy.id+`"}`); rr.Code != http.StatusOK {
		t.Fatalf("add member: %d %s", rr.Code, rr.Body.String())
	}
	hh.notify.reset()

	if rr := hh.as(kaja, "DELETE", "/api/chat/attachments/"+msg.Attachments[0].ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("Odstranit: %d", rr.Code)
	}
	for i, typ := range hh.notify.types {
		if typ != "chat_message.updated" {
			continue
		}
		for _, who := range hh.notify.audiences[i] {
			if who == andy.id {
				t.Fatal("the cleanup frame carried an old message's body to a member whose " +
					"floor sits above it — MemberIDs, not MemberIDsAbove (D218)")
			}
		}
	}
}
