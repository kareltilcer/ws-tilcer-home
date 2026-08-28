package documents_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/documents"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
)

// This module as the receiving half of v10's move to Dokumenty (D238/D239/D245).
//
// ⚠ THE TESTS ARE HERE RATHER THAN IN `chat`, and that is deliberate: what can break
// is this module's satisfaction of the interface and its refusal of a private
// target, and a test in the consumer would report both as a chat failure.

// TestDocumentsImplementsBlobSink is the test D239 asks for BY NAME.
//
// ⚠ §V9-12 RECORDS WHY IT EXISTS. `TestRealModulesImplementTheStorageCatalog` was
// written because a catalog method landed on the wrong type: everything compiled,
// every test passed, and the Úložiště page reported 0 B. *"It was found by opening
// the page."* The equivalent failure here is quieter and worse — the move answers
// 501 for a household that has `documents` wired, and the clean-up page's headline
// action is simply absent with nothing on screen explaining why.
//
// The `var _ storage.BlobSink = (*Service)(nil)` line in sink.go catches the wrong
// RECEIVER at compile time. This catches the interface DRIFTING: a signature change
// in platform/storage that this module was not updated for.
func TestDocumentsImplementsBlobSink(t *testing.T) {
	var svc *documents.Service
	var sink storage.BlobSink = svc
	if sink == nil {
		t.Fatal("a typed nil *Service must still satisfy storage.BlobSink")
	}
}

// TestAcceptBlobRefusesAPrivateFolder is leak row 17 and D245's server half.
//
// ⚠ AND IT ASSERTS THAT NO OBJECT WAS WRITTEN, not merely that the call failed. The
// ordering requirement is "validate BEFORE copy": a refusal that happened after the
// copy would leave an unattributed object under `documents/` on every attempt, and
// nothing in the error would say so. The acceptance criterion is worded the same
// way — *"422 with no copy attempted"*.
func TestAcceptBlobRefusesAPrivateFolder(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()

	private := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{
		Name: "Soukromé smlouvy", Scope: "private",
	}))
	x.putChatObject(ctx, "chat/att-1/original", pdfBytes())

	before := x.countUnder(ctx, "documents/")
	_, err := x.svc.AcceptBlob(ctx, storage.AcceptRequest{
		SourceKey:   "chat/att-1/original",
		FolderID:    private.ID,
		Filename:    "smlouva.pdf",
		ContentType: "application/pdf",
		ByteSize:    int64(len(pdfBytes())),
		Checksum:    "abc",
		Via:         "chat",
	})
	if err == nil {
		t.Fatal("a private target folder was accepted — the moved file would be unreadable to " +
			"exactly the conversation the move exists to keep it readable for (D245)")
	}
	if got := status(t, err); got != 422 {
		t.Errorf("private folder answered %d, want 422", got)
	}
	if after := x.countUnder(ctx, "documents/"); after != before {
		t.Errorf("the refusal copied %d object(s) anyway — validate runs BEFORE copy (D238)",
			after-before)
	}
}

// TestAcceptBlobCopiesAndLeavesTheSourceAlone is the happy path, asserted in the
// order the contract claims.
func TestAcceptBlobCopiesAndLeavesTheSourceAlone(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()

	target := x.folder(x.svc.CreateFolder(ctx, documents.DocFolderCreate{Name: "Smlouvy"}))
	const sourceKey = "chat/att-2/original"
	x.putChatObject(ctx, sourceKey, pdfBytes())

	res, err := x.svc.AcceptBlob(ctx, storage.AcceptRequest{
		SourceKey:   sourceKey,
		FolderID:    target.ID,
		Filename:    "smlouva-2026.pdf",
		ContentType: "application/pdf",
		ByteSize:    int64(len(pdfBytes())),
		Checksum:    "deadbeef",
		Via:         "chat",
	})
	if err != nil {
		t.Fatalf("AcceptBlob: %v", err)
	}
	if res.DocumentID == "" || res.Path == "" {
		t.Fatalf("AcceptBlob returned %+v — the chat bubble renders from Path", res)
	}

	// ⚠ THE SOURCE IS UNTOUCHED. Deleting it is the CALLER's step 5, and a sink that
	// tidied up would be destroying bytes before its own row was known to have
	// committed — the one inversion that loses the file.
	if !x.objectExists(ctx, sourceKey) {
		t.Error("AcceptBlob deleted the source object — step 5 belongs to the caller (D238)")
	}
	if !x.objectExists(ctx, documents.OriginalKey(res.DocumentID)) {
		t.Error("the destination object is missing after a successful accept")
	}

	// The document is SHARED, which IS the accepted publish (D245) and the whole
	// reason a private target is refused.
	var visibility string
	if err := x.db.QueryRow(`SELECT visibility FROM documents WHERE id = ?`, res.DocumentID).
		Scan(&visibility); err != nil {
		t.Fatalf("read the document row: %v", err)
	}
	if visibility != "shared" {
		t.Errorf("a moved file landed with visibility %q, want shared (D245)", visibility)
	}

	// `meta.via` is what makes a transferred document distinguishable from an
	// uploaded one a year later (FR-V10-14 step 3).
	var metaJSON string
	if err := x.db.QueryRow(
		`SELECT COALESCE(meta, '{}') FROM audit_events WHERE entity_id = ? AND action = 'document.create'`,
		res.DocumentID).Scan(&metaJSON); err != nil {
		t.Fatalf("read the audit event: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		t.Fatalf("decode meta %q: %v", metaJSON, err)
	}
	if meta["via"] != "chat" {
		t.Errorf("document.create carries meta.via = %v, want \"chat\"", meta["via"])
	}
}

// TestAcceptBlobRefusesAnUnknownFolder keeps the two 422s distinguishable: a private
// folder and a folder that is not there are both refusals the picker should have
// prevented, and both must leave the bucket untouched.
func TestAcceptBlobRefusesAnUnknownFolder(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	x.putChatObject(ctx, "chat/att-3/original", pdfBytes())

	before := x.countUnder(ctx, "documents/")
	_, err := x.svc.AcceptBlob(ctx, storage.AcceptRequest{
		SourceKey: "chat/att-3/original", FolderID: "no-such-folder",
		Filename: "x.pdf", ContentType: "application/pdf", ByteSize: 10, Via: "chat",
	})
	if err == nil {
		t.Fatal("an unknown folder was accepted")
	}
	if got := status(t, err); got != 422 {
		t.Errorf("unknown folder answered %d, want 422", got)
	}
	if after := x.countUnder(ctx, "documents/"); after != before {
		t.Error("the refusal copied an object anyway — validate runs BEFORE copy")
	}
}

// ---- harness helpers ----

// putChatObject writes a fake chat attachment into the shared bucket. The tests
// need a source object and `documents` has no way to make one — which is the point:
// the two modules share a bucket and nothing else.
func (x *h) putChatObject(ctx context.Context, key string, body []byte) {
	x.t.Helper()
	if err := x.blob.Put(ctx, key, strings.NewReader(string(body)), int64(len(body)), "application/pdf"); err != nil {
		x.t.Fatalf("seed the chat object %s: %v", key, err)
	}
}

func (x *h) countUnder(ctx context.Context, prefix string) int {
	x.t.Helper()
	objects, err := x.blob.List(ctx, prefix)
	if err != nil {
		x.t.Fatalf("list %s: %v", prefix, err)
	}
	return len(objects)
}

func (x *h) objectExists(ctx context.Context, key string) bool {
	x.t.Helper()
	_, err := x.blob.Stat(ctx, key)
	return err == nil
}
