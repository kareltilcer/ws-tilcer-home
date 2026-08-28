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
