package chat_test

// PR 3's harness and its upload/delivery tests.
//
// Every test here is written FROM THE ATTACKER'S SIDE where there is an attacker to
// be: `andy` is not in the room, `boss` is an admin who is not in it either, and
// `quiet` is a reader who may write messages and may not clean up storage. What
// separates a correct implementation from a wrong one is never what a member sees.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/chat"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// ---- the storage harness ----

// storageHousehold is `household` plus the two things PR 3 needs: an object store
// and a sink. It is a separate constructor rather than a change to newHousehold so
// PR 2's tests keep proving that chat works with NEITHER wired — which is a real
// deployment (D239's 501) and not only a test convenience.
type storageHousehold struct {
	*household
	blob *blobstore.FS
	sink *fakeSink
}

func newStorageHousehold(t *testing.T, members ...member) *storageHousehold {
	t.Helper()
	return newStorageHouseholdWith(t, newFakeSink(), members...)
}

// newStorageHouseholdWith builds a household with a specific sink. Passing nil is
// the "no BlobSink configured" deployment (D239).
func newStorageHouseholdWith(t *testing.T, sink *fakeSink, members ...member) *storageHousehold {
	t.Helper()
	db := testsupport.NewDB(t)
	blob, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	if sink != nil {
		sink.blob = blob
	}

	dir := stubDirectory{}
	for _, m := range members {
		dir.members = append(dir.members, push.Member{UserID: m.id, DisplayName: m.name,
			Email: m.id + "@example.test", Roles: m.roles})
	}
	notify := &capturedNotify{}
	pushes := newCapturedPush()
	opts := chat.Options{
		TrashDays: 7,
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Blob:      blob,
		Upload: chat.UploadOptions{
			// 1 MB keeps the over-cap test cheap while staying a real cap.
			MaxBytes: 1 << 20,
			TempDir:  t.TempDir(),
			// ⚠ NO CwebpPath. Thumbnail encoding needs an external binary that is not
			// on a test box, and makeThumbnail refuses without one — which exercises
			// the branch that matters: a failed thumbnail must be NON-FATAL and must
			// still record the intrinsic dimensions, because the dimensions are what
			// stop the thread reflowing.
			Thumb: chat.ThumbOptions{MaxPx: 480, MaxImagePixels: 40_000_000},
		},
	}
	if sink != nil {
		opts.Sink = sink
	}
	svc := chat.NewService(db, audit.NewSink(), notify.fn, pushes, dir, opts)
	h := chat.NewHandler(svc)

	handlers := map[string]http.Handler{}
	for _, m := range members {
		actor := reqctx.Actor{UserID: m.id, Type: "user", Label: m.name, Roles: m.roles}
		handlers[m.id] = testsupport.RouterAs(t, db, actor, h.Mount)
	}
	hh := &household{t: t, db: db, svc: svc, handlers: handlers, notify: notify, pushes: pushes}
	return &storageHousehold{household: hh, blob: blob, sink: sink}
}

// fakeSink stands in for `documents` accepting custody.
//
// ⚠ IT IS WHERE STEPS 2 AND 3 ARE INJECTED, and it can fail them SEPARATELY —
// which is the whole point. "The copy failed" and "the copy worked and the row did
// not" leave different states behind, and only the second one leaves an object
// under the destination prefix.
// ⚠ IT PERFORMS A REAL COPY into the shared bucket, and that is what makes the
// fault-injection matrix mean anything: "bytes in both places, no document row" is
// a claim about OBJECTS, and a sink that only returned an id would let the assertion
// pass over a state that never existed.
type fakeSink struct {
	mu   sync.Mutex
	blob blobstore.BlobStore
	// documents is the id → destination key of every row it committed.
	documents map[string]string
	// failBeforeCopy simulates a crash at step 2: nothing is copied.
	failBeforeCopy bool
	// failAfterCopy simulates a crash at step 3: the object exists, the row does not.
	failAfterCopy bool
	n             int
}

func newFakeSink() *fakeSink { return &fakeSink{documents: map[string]string{}} }

func (f *fakeSink) AcceptBlob(ctx context.Context, req storage.AcceptRequest) (storage.AcceptResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failBeforeCopy {
		return storage.AcceptResult{}, errors.New("documents: object storage is unavailable")
	}
	f.n++
	id := fmt.Sprintf("doc-%d", f.n)
	dst := "documents/" + id + "/original"
	// Step 2 — the copy, for real, so the state a step-3 failure leaves behind is
	// the state the recovery table describes.
	if f.blob != nil {
		if err := f.blob.Copy(ctx, req.SourceKey, dst, f.blob); err != nil {
			return storage.AcceptResult{}, err
		}
	}
	if f.failAfterCopy {
		// Step 3 failed. ⚠ The object stays: an unattributed object under
		// `documents/`, reported by v9's machinery and never auto-cleaned.
		return storage.AcceptResult{}, errors.New("documents: the row could not be written")
	}
	f.documents[id] = dst
	return storage.AcceptResult{DocumentID: id, Path: "/api/documents/" + id + "/raw"}, nil
}

func (f *fakeSink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.documents)
}

// ---- request helpers ----

// upload sends a multipart message through the real router.
func (hh *storageHousehold) upload(m member, conversationID, body string, files ...uploadFile) *httptest.ResponseRecorder {
	hh.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if body != "" {
		_ = w.WriteField("body", body)
	}
	for _, f := range files {
		part, err := w.CreateFormFile("files", f.name)
		if err != nil {
			hh.t.Fatalf("multipart: %v", err)
		}
		if _, err := part.Write(f.data); err != nil {
			hh.t.Fatalf("multipart write: %v", err)
		}
	}
	_ = w.Close()

	handler, ok := hh.handlers[m.id]
	if !ok {
		hh.t.Fatalf("member %s was not registered", m.id)
	}
	r := httptest.NewRequest("POST", "/api/chat/conversations/"+conversationID+"/messages", &buf)
	r.Header.Set("Content-Type", w.FormDataContentType())
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, r)
	return rr
}

type uploadFile struct {
	name string
	data []byte
}

// uploadOne sends one file and returns the message it produced.
func (hh *storageHousehold) uploadOne(m member, conversationID, filename string, data []byte) chat.Message {
	hh.t.Helper()
	rr := hh.upload(m, conversationID, "", uploadFile{filename, data})
	if rr.Code != http.StatusCreated {
		hh.t.Fatalf("upload %s: %d %s", filename, rr.Code, rr.Body.String())
	}
	return decode[chat.Message](hh.t, rr)
}

// head issues a HEAD, which `as` cannot: the leak table asks for GET **and** HEAD,
// because a HEAD-only oracle is still an oracle.
func (hh *storageHousehold) head(m member, path string) *httptest.ResponseRecorder {
	hh.t.Helper()
	handler, ok := hh.handlers[m.id]
	if !ok {
		hh.t.Fatalf("member %s was not registered", m.id)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest("HEAD", path, nil))
	return rr
}

// conditional issues a GET carrying an If-None-Match — the request that produced
// v9's 304 leak in `documents` (§V9-12, leak row 5).
func (hh *storageHousehold) conditional(m member, path, etag string) *httptest.ResponseRecorder {
	hh.t.Helper()
	handler, ok := hh.handlers[m.id]
	if !ok {
		hh.t.Fatalf("member %s was not registered", m.id)
	}
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("If-None-Match", etag)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, r)
	return rr
}

// ---- fixtures ----

// pngBytes is a 1×1 PNG: it sniffs as image/png and DecodeConfig reads 1×1 from it.
var pngBytes = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05,
	0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00,
	0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

func pdfBytes() []byte { return []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\ntrailer\n%%EOF\n") }

// movBytes fakes an iPhone QuickTime file: real `ftyp` box, brand `qt  `, which
// Go's own mp4 matcher REJECTS — the case videoByExt exists for.
func movBytes() []byte {
	return append([]byte{0x00, 0x00, 0x00, 0x14}, []byte("ftypqt  \x00\x00\x02\x00qt  ")...)
}

// ---- upload ----

// TestUploadClassifiesBySniffedMIME is D48's rule and D227's table.
func TestUploadClassifiesBySniffedMIME(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Fotky")

	// ⚠ A .png RENAMED TO .pdf IS STORED AND RENDERED AS AN IMAGE. The acceptance
	// criterion is written that way because the client's claim is the one input an
	// attacker fully controls.
	msg := hh.uploadOne(kaja, room.ID, "fotka.pdf", pngBytes)
	if len(msg.Attachments) != 1 {
		t.Fatalf("upload produced %d attachments, want 1", len(msg.Attachments))
	}
	a := msg.Attachments[0]
	if a.Kind != "image" {
		t.Errorf("a PNG named .pdf was classified %q, want image — the extension is not the type (D48)", a.Kind)
	}
	if a.ContentType != "image/png" {
		t.Errorf("content_type = %q, want image/png", a.ContentType)
	}
	// ⚠ THE DIMENSIONS ARE WHY THE THREAD DOES NOT REFLOW, and they are recorded
	// even though the thumbnail could not be encoded on this box.
	if a.Width == nil || a.Height == nil || *a.Width != 1 || *a.Height != 1 {
		t.Errorf("intrinsic dimensions = %v×%v, want 1×1 — without them the thread "+
			"reflows as images load", a.Width, a.Height)
	}
	if a.HasThumbnail {
		t.Error("has_thumbnail is true with no cwebp configured — a failed thumbnail " +
			"must be reported as absent, not claimed")
	}

	// A QuickTime file branded `qt  ` is a video, not a download.
	mov := hh.uploadOne(kaja, room.ID, "IMG_0042.mov", movBytes())
	if k := mov.Attachments[0].Kind; k != "video" {
		t.Errorf("an iPhone .mov was classified %q, want video (D227)", k)
	}

	// Everything else is a file, and a PDF is one of them.
	doc := hh.uploadOne(kaja, room.ID, "smlouva.pdf", pdfBytes())
	if k := doc.Attachments[0].Kind; k != "file" {
		t.Errorf("a PDF was classified %q, want file", k)
	}
}

// TestUploadRefusesOverTheCap is D228's cap, named in MB.
//
// ⚠ THE MESSAGE HAS TO NAME THE LIMIT. The composer refuses an over-cap file before
// uploading it, so this response is what a client sees when its own check was
// wrong — and "413" with no number is a dead end for whoever hits it.
func TestUploadRefusesOverTheCap(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Velké soubory")

	oversize := bytes.Repeat([]byte("x"), (1<<20)+1)
	rr := hh.upload(kaja, room.ID, "", uploadFile{"velky.bin", oversize})
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("an over-cap file answered %d, want 413: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "MB") {
		t.Errorf("the 413 does not name the limit in MB: %s", rr.Body.String())
	}
	// Nothing was written: the row goes only after the object, and neither happened.
	if n := hh.countObjects("chat/"); n != 0 {
		t.Errorf("a refused upload left %d object(s) behind", n)
	}
}

// TestUploadRefusesMoreThanTenFiles is D224's ten.
func TestUploadRefusesMoreThanTenFiles(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Fotoalbum")

	files := make([]uploadFile, 0, 11)
	for i := 0; i < 11; i++ {
		files = append(files, uploadFile{fmt.Sprintf("f%d.png", i), pngBytes})
	}
	rr := hh.upload(kaja, room.ID, "", files...)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("eleven files answered %d, want 422: %s", rr.Code, rr.Body.String())
	}
}

// TestUploadAllowsABodylessMessageAndRefusesAnEmptyOne is D224's invariant, which
// lives in the write transaction rather than as a table CHECK.
func TestUploadAllowsABodylessMessageAndRefusesAnEmptyOne(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Bez textu")

	if rr := hh.upload(kaja, room.ID, "", uploadFile{"a.png", pngBytes}); rr.Code != http.StatusCreated {
		t.Fatalf("a message with a file and no body answered %d, want 201: %s", rr.Code, rr.Body.String())
	}
	if rr := hh.upload(kaja, room.ID, ""); rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a message with neither body nor file answered %d, want 422", rr.Code)
	}
}

// TestUploadRefusesANonMemberBeforeStagingAnything is leak row 1 applied to the
// upload path.
//
// ⚠ AND IT ASSERTS THE BUCKET IS EMPTY. The membership check runs BEFORE a byte is
// staged, so a non-member cannot spend the droplet's disk discovering they are not
// in the room — and cannot leave an object behind while finding out.
func TestUploadRefusesANonMemberBeforeStagingAnything(t *testing.T) {
	hh := newStorageHousehold(t, kaja, andy)
	hh.join(kaja)
	hh.join(andy)
	room := hh.group(kaja, "Jen Kája")

	rr := hh.upload(andy, room.ID, "", uploadFile{"vloupani.png", pngBytes})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("a non-member's upload answered %d, want 404 (never 403 — D217)", rr.Code)
	}
	if n := hh.countObjects("chat/"); n != 0 {
		t.Errorf("a refused upload staged %d object(s) — membership is checked first", n)
	}
}

// TestUploadIsAudited is the asymmetry D231 leaves deliberately.
//
// ⚠ THE MESSAGE WRITES NOTHING AND THE ATTACHMENT WRITES ONE EVENT. That looks
// inconsistent and is the decision: the bytes are what the thresholds, the clean-up
// page and the storage register exist for, so "who uploaded that 40 MB video, and
// when" has to be answerable — from a Log that says nothing at all about messages.
func TestUploadIsAudited(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Dovolená")

	before := auditCount(t, hh.db)
	hh.uploadOne(kaja, room.ID, "pláž.png", pngBytes)
	if got := auditCount(t, hh.db) - before; got != 1 {
		t.Fatalf("an upload wrote %d audit events, want exactly 1 (the attachment, not the message)", got)
	}
	var action, summary string
	if err := hh.db.QueryRow(
		`SELECT action, summary FROM audit_events ORDER BY id DESC LIMIT 1`).Scan(&action, &summary); err != nil {
		t.Fatalf("read the event: %v", err)
	}
	if action != "attachment.uploaded" {
		t.Errorf("action = %q, want attachment.uploaded", action)
	}
	if !strings.Contains(summary, "pláž.png") || !strings.Contains(summary, "Dovolená") {
		t.Errorf("the summary must carry the filename and the conversation name, got %q", summary)
	}
	// And still nothing about the message itself.
	var messageEvents int
	if err := hh.db.QueryRow(
		`SELECT COUNT(*) FROM audit_events WHERE action LIKE 'message.%'`).Scan(&messageEvents); err != nil {
		t.Fatalf("count message events: %v", err)
	}
	if messageEvents != 0 {
		t.Errorf("%d chat.message.* events exist — D231 says none, ever", messageEvents)
	}
}

// ---- delivery ----

// TestAttachmentDeliveryRefusesNonMembersOnGETAndHEAD is leak row 1 on the bytes.
func TestAttachmentDeliveryRefusesNonMembersOnGETAndHEAD(t *testing.T) {
	hh := newStorageHousehold(t, kaja, andy, boss)
	hh.join(kaja)
	hh.join(andy)
	hh.join(boss)
	room := hh.group(kaja, "Soukromá skupina")
	msg := hh.uploadOne(kaja, room.ID, "tajne.pdf", pdfBytes())
	path := "/api/chat/attachments/" + msg.Attachments[0].ID + "/raw"

	for _, who := range []member{andy, boss} {
		if rr := hh.as(who, "GET", path, ""); rr.Code != http.StatusNotFound {
			t.Errorf("GET by %s answered %d, want 404 (never 403 — D217)", who.id, rr.Code)
		}
		if rr := hh.head(who, path); rr.Code != http.StatusNotFound {
			t.Errorf("HEAD by %s answered %d, want 404 — a HEAD-only oracle is still an oracle",
				who.id, rr.Code)
		}
	}
	if rr := hh.as(kaja, "GET", path, ""); rr.Code != http.StatusOK {
		t.Fatalf("the uploader's own GET answered %d, want 200: %s", rr.Code, rr.Body.String())
	}
}

// TestConditionalRequestFromANonMemberIs404Not304 IS LEAK ROW 5, and it is the bug
// v9 shipped in `documents` and caught after the fact (§V9-12).
//
// ⚠ THE ATTACKER HOLDS A VALID ETAG. That is the whole scenario: they were a member
// once, or the tag leaked, and the question is whether the server answers *"yes, and
// it hasn't changed"* about something they may no longer read. The ordering in
// content.go — membership BEFORE the conditional branch — is what makes this 404.
func TestConditionalRequestFromANonMemberIs404Not304(t *testing.T) {
	hh := newStorageHousehold(t, kaja, andy)
	hh.join(kaja)
	hh.join(andy)
	room := hh.group(kaja, "Jen Kája")
	msg := hh.uploadOne(kaja, room.ID, "smlouva.pdf", pdfBytes())
	path := "/api/chat/attachments/" + msg.Attachments[0].ID + "/raw"

	// The member's own request mints the tag the attacker will hold.
	ok := hh.as(kaja, "GET", path, "")
	etag := ok.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on a 200 — the whole revalidation policy depends on one")
	}
	// And the member's own conditional request is a genuine 304, so the test proves
	// the branch works rather than that it is unreachable.
	if rr := hh.conditional(kaja, path, etag); rr.Code != http.StatusNotModified {
		t.Fatalf("a member's conditional request answered %d, want 304", rr.Code)
	}
	if rr := hh.conditional(andy, path, etag); rr.Code != http.StatusNotFound {
		t.Fatalf("a NON-member's conditional request answered %d, want 404. This is leak row 5: "+
			"the membership load must run BEFORE the If-None-Match branch, or a stale ETag "+
			"earns a non-member a 304 about something they may not read.", rr.Code)
	}
}

// TestAttachmentCacheHeadersAreNeverImmutable is leak row 6 (D229).
func TestAttachmentCacheHeadersAreNeverImmutable(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Obrázky")
	msg := hh.uploadOne(kaja, room.ID, "a.png", pngBytes)
	path := "/api/chat/attachments/" + msg.Attachments[0].ID + "/raw"

	rr := hh.as(kaja, "GET", path, "")
	cache := rr.Header().Get("Cache-Control")
	if cache != "private, no-cache, must-revalidate" {
		t.Errorf("Cache-Control = %q, want \"private, no-cache, must-revalidate\" (D229)", cache)
	}
	if strings.Contains(cache, "immutable") {
		t.Error("a chat attachment carries `immutable` — membership can be REVOKED, and a " +
			"year-long cache entry outlives the revocation on a device nobody can reach")
	}
	// The 304 keeps the validators, so revalidation stays cheap.
	notModified := hh.conditional(kaja, path, rr.Header().Get("ETag"))
	if got := notModified.Header().Get("ETag"); got == "" {
		t.Error("the 304 dropped its ETag — the next request re-downloads the whole object")
	}
	if got := notModified.Header().Get("Cache-Control"); got != cache {
		t.Errorf("the 304's Cache-Control (%q) disagrees with the 200's (%q)", got, cache)
	}
}

// TestThumbnailIs404WithoutOne covers video, file, and an image whose encode failed
// — all three of which have no thumbnail and must say so identically.
func TestThumbnailIs404WithoutOne(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Mix")
	doc := hh.uploadOne(kaja, room.ID, "smlouva.pdf", pdfBytes())
	if rr := hh.as(kaja, "GET", "/api/chat/attachments/"+doc.Attachments[0].ID+"/thumbnail", ""); rr.Code != http.StatusNotFound {
		t.Errorf("a PDF's thumbnail answered %d, want 404 — chat runs no preview pipeline (D227)", rr.Code)
	}
}

// TestRemovedAttachmentBytesAre404 — the epitaph is metadata, not a redirect.
func TestRemovedAttachmentBytesAre404(t *testing.T) {
	hh := newStorageHousehold(t, kaja)
	hh.join(kaja)
	room := hh.group(kaja, "Úklid")
	msg := hh.uploadOne(kaja, room.ID, "velky.pdf", pdfBytes())
	id := msg.Attachments[0].ID

	if rr := hh.as(kaja, "DELETE", "/api/chat/attachments/"+id, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("Odstranit answered %d: %s", rr.Code, rr.Body.String())
	}
	if rr := hh.as(kaja, "GET", "/api/chat/attachments/"+id+"/raw", ""); rr.Code != http.StatusNotFound {
		t.Errorf("a removed attachment's bytes answered %d, want 404", rr.Code)
	}
}

// ---- helpers ----

func (hh *storageHousehold) countObjects(prefix string) int {
	hh.t.Helper()
	objects, err := hh.blob.List(context.Background(), prefix)
	if err != nil {
		hh.t.Fatalf("list %s: %v", prefix, err)
	}
	return len(objects)
}

func (hh *storageHousehold) objectExists(key string) bool {
	hh.t.Helper()
	_, err := hh.blob.Stat(context.Background(), key)
	return err == nil
}

// TestByteRoutesResolveOutsideTheAutoJoinGroup pins the router split.
//
// ⚠ IT EXISTS BECAUSE THE FIRST ATTEMPT SILENTLY UNMOUNTED THEM. Moving the byte
// routes off autoJoin by declaring a SECOND `r.Route("/chat/attachments", …)` beside
// the existing `r.Route("/chat", …)` does not compose in chi — the first mounts a
// subtree wildcard and swallows the second — so every attachment path answered
// `{"error":"not_found","detail":"no such endpoint"}` while the code read exactly
// right. A chi Group inside the one Route is the shape that works.
//
// ⚠ AND THE REFUSAL IS UNCHANGED, which is the half that matters. autoJoin only ever
// ADDS a Všichni membership row; it is not a check, and a first-sight enrolment into
// the household room cannot make anybody a member of the conversation an attachment
// belongs to. The access rule is AttachmentForViewer's join either way.
func TestByteRoutesResolveOutsideTheAutoJoinGroup(t *testing.T) {
	hh := newStorageHousehold(t, kaja, andy)
	hh.join(kaja)
	room := hh.group(kaja, "Fotky")
	msg := hh.uploadOne(kaja, room.ID, "a.png", pngBytes)
	raw := "/api/chat/attachments/" + msg.Attachments[0].ID + "/raw"

	// The member's own request resolves — the routes are mounted at all.
	if rr := hh.as(kaja, "GET", raw, ""); rr.Code != http.StatusOK {
		t.Fatalf("the uploader's GET answered %d, want 200 — are the byte routes mounted? %s",
			rr.Code, rr.Body.String())
	}
	if rr := hh.head(kaja, raw); rr.Code != http.StatusOK {
		t.Errorf("HEAD answered %d, want 200", rr.Code)
	}

	// ⚠ `andy` HAS NEVER MADE A CHAT REQUEST, so he has no Všichni row and this route
	// no longer enrols him. He must still be refused, and refused identically.
	for _, method := range []string{"GET", "HEAD"} {
		var rr *httptest.ResponseRecorder
		if method == "HEAD" {
			rr = hh.head(andy, raw)
		} else {
			rr = hh.as(andy, method, raw, "")
		}
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s by a non-member answered %d, want 404 (D217)", method, rr.Code)
		}
	}

	// And the auto-join is still doing its job on the routes that need it: andy's
	// first conversation listing enrols him in Všichni.
	hh.join(andy)
	var rows int
	if err := hh.db.QueryRow(
		`SELECT COUNT(*) FROM chat_members m JOIN chat_conversations c ON c.id = m.conversation_id
		  WHERE m.user_id = ? AND c.kind = 'default'`, andy.id).Scan(&rows); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if rows != 1 {
		t.Errorf("the auto-join did not enrol a member on the listing route (%d rows)", rows)
	}
}
