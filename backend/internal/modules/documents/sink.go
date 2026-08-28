package documents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/slug"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
)

// This module as a `storage.BlobSink` — the receiving half of v10's move to
// Dokumenty (PRD D238, FR-V10-14).
//
// ⚠ IT IS THE FIRST WRITE THIS MODULE EXPOSES THROUGH THE STORAGE CATALOG. Every
// other catalog method here is a projection: StorageBlobs, PrivateItems. This one
// mints a row, writes an audit event and copies an object — for a caller it may
// not name, because `chat` may not import `documents` and `documents` may not
// import `chat`.
//
// ⚠ WHAT IT DOES *NOT* DO IS THE HALF WORTH READING. It never touches the source
// object. Deleting the chat object is step 5, it belongs to the caller, and it
// happens only AFTER this method has returned successfully — because the ordering
// is what makes every crash point over-count rather than lose the file. A sink
// that helpfully tidied up would be destroying bytes before its own row was known
// to have committed.
//
// ⚠ TestDocumentsImplementsBlobSink IS WRITTEN BEFORE THE CALLER IS, and it exists
// because §V9-12 records `TestRealModulesImplementTheStorageCatalog`: a catalog
// method landed on the wrong type, everything compiled, every test passed, and the
// page reported 0 B. *"It was found by opening the page."*

// Compile-time proof that the sink is on *Service and not on some sibling type.
// The runtime test above still ships — this line catches the wrong RECEIVER, and
// that test catches the wrong VALUE being wired at composition.
var _ storage.BlobSink = (*Service)(nil)

// AcceptBlob takes custody of another module's object: steps 1–3 of the move.
//
//	1. VALIDATE the target folder is shared and this actor may write to it.
//	   ⚠ A private v9 folder is 422 WITH NO COPY ATTEMPTED (D245) — a private
//	   target would make the file unreadable to the conversation's other members,
//	   which is the opposite of what the move is for. The acceptance criterion
//	   asserts it by checking that no object appeared under `documents/`.
//	2. COPY the source object to this module's own key. Same account, so R2 copies
//	   server-side and nothing streams through the app.
//	3. INSERT the row in its OWN transaction, with its own `document.create` event
//	   carrying `meta.via`.
//
// A failure at 3 leaves an unattributed object under `documents/` — reported by
// v9's machinery, never auto-cleaned, and harmless: re-running the move is safe.
// The reverse ordering (row first, bytes second) is the one that loses the file,
// and it is why the copy is not deferred to "after the row exists".
func (s *Service) AcceptBlob(ctx context.Context, req storage.AcceptRequest) (storage.AcceptResult, error) {
	if s.blob == nil {
		// ⚠ NOT a 501: the CALLER decides whether a missing sink means "no button"
		// (it does — D239), and it reaches that decision by holding a nil sink, not
		// by calling one that refuses. Reaching here with no object store is a
		// composition error, which is what 500 is for.
		return storage.AcceptResult{}, httpx.ErrInternal("document storage is not configured")
	}
	folderID := strings.TrimSpace(req.FolderID)
	if folderID == "" {
		return storage.AcceptResult{}, httpx.ErrUnprocessable("folder_id is required")
	}
	if req.SourceKey == "" || req.ByteSize <= 0 {
		return storage.AcceptResult{}, httpx.ErrUnprocessable("the source object is not described")
	}
	if !writeAllowed(ctx) {
		return storage.AcceptResult{}, httpx.ErrForbidden("Nemáte oprávnění zapisovat do Dokumentů.")
	}

	// 1. Validate — outside the transaction, so a refusal is a 422 rather than a
	// rolled-back write, and BEFORE the copy so a private target costs nothing.
	sc, err := s.sinkTarget(ctx, folderID)
	if err != nil {
		return storage.AcceptResult{}, err
	}

	id := idgen.New()
	destKey := OriginalKey(id)

	// 2. Copy. `s.blob` on both sides: same bucket, same account, so the S3
	// implementation issues a server-side CopyObject and the bytes never enter this
	// process.
	if err := s.blob.Copy(ctx, req.SourceKey, destKey, s.blob); err != nil {
		s.logger.Error("documents: accepting custody failed at the copy — nothing committed",
			"source_key", req.SourceKey, "dest_key", destKey, "via", req.Via, "err", err)
		if errors.Is(err, blobstore.ErrNotFound) {
			return storage.AcceptResult{}, httpx.ErrUnprocessable("the source object no longer exists")
		}
		return storage.AcceptResult{}, httpx.ErrBadGateway("document storage is unavailable")
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = titleFromFilename(req.Filename)
	}
	if title == "" {
		title = req.Filename
	}
	// ⚠ The transferred bytes get the same preview plan an upload of them would have
	// got, derived from the SAME sniffed content type. Anything else would make a
	// moved PDF behave differently from an uploaded one in Dokumenty, for no reason
	// a member could see.
	previewKind, previewStatus := previewPlanFor(req.ContentType, s.opts.PreviewEnabled)
	file := UploadedFile{
		OriginalFilename: req.Filename,
		ContentType:      req.ContentType,
		ByteSize:         req.ByteSize,
		Checksum:         req.Checksum,
		StorageKey:       destKey,
		PreviewKind:      previewKind,
		PreviewStatus:    previewStatus,
		ThumbnailStatus:  thumbnailPlanFor(req.ContentType, s.opts.PreviewEnabled),
	}

	// 3. Row + audit, in one transaction.
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		sl, slugErr := s.freeSlug(ctx, tx, &folderID, sc, slug.Make(title), "", "")
		if slugErr != nil {
			return slugErr
		}
		pos, posErr := s.store.lastDocumentPosition(ctx, tx, &folderID, sc)
		if posErr != nil {
			return posErr
		}
		if _, insErr := s.store.InsertDocument(ctx, tx, id, &folderID, title, sl, "",
			file, pos, actorID(ctx), sc); insErr != nil {
			return insErr
		}
		changes := []audit.Change{
			{Field: "title", New: ap(title)},
			{Field: "slug", New: ap(sl)},
			{Field: "original_filename", New: ap(req.Filename)},
			{Field: "content_type", New: ap(req.ContentType)},
			{Field: "byte_size", New: ap(fmt.Sprint(req.ByteSize))},
			{Field: "checksum", New: ap(req.Checksum)},
			{Field: "folder_id", New: ap(folderID)},
		}
		// ⚠ `meta.via` is what makes a transferred document distinguishable from an
		// uploaded one a year later — the same key v5's cross-module triggers use,
		// and the reason FR-V10-14 names it in the step rather than leaving it to
		// the implementer.
		meta := map[string]any{}
		if req.Via != "" {
			meta["via"] = req.Via
		}
		return s.record(ctx, tx, "document.create", "document", id,
			fmt.Sprintf("Nahrán dokument „%s“", title), changes, meta, sc)
	})
	if err != nil {
		// The row never landed, so the copy is an orphan. Delete it here rather than
		// leaving it for the reconciliation pass — the SOURCE is untouched and still
		// authoritative, so this destroys nothing.
		s.purgeObjects(ctx, []string{destKey})
		return storage.AcceptResult{}, err
	}

	// A moved file is a PUBLISH: it is now readable by the whole household, so the
	// frame is the ordinary shared one and carries the id (D245 is exactly why
	// there is no private branch to take here).
	s.notifyScoped(ctx, "document.changed", id, false)
	if s.enqueuePreview != nil && s.opts.PreviewEnabled && needsDerivedObject(req.ContentType) {
		s.enqueuePreview(id)
	}

	return storage.AcceptResult{DocumentID: id, Path: urlsFor(id).Raw}, nil
}

// sinkTarget resolves the destination folder and refuses a private one.
//
// ⚠ THE PRIVATE REFUSAL IS 422 AND NOT A FILTERED PICKER, and the difference is
// the point: the UI offers shared folders only, but the check has to exist on the
// server too, because a client that posts a private folder_id must be refused
// rather than served a document its own conversation cannot read (D245).
//
// It deliberately does NOT reuse assertFolder's `requested` mechanism. That one
// takes the caller's stated scope and compares; here there is no caller-stated
// scope at all — the answer is fixed, and expressing it as "requested = shared"
// would produce the message "scope disagrees with the parent folder", which
// describes a mistake nobody made.
func (s *Service) sinkTarget(ctx context.Context, folderID string) (Scope, error) {
	f, err := s.store.GetFolder(ctx, s.db, folderID, actorID(ctx))
	if err != nil {
		return Scope{}, err
	}
	if f == nil || f.Archived {
		return Scope{}, httpx.ErrUnprocessable("Cílová složka v Dokumentech neexistuje.")
	}
	sc := scopeOfFolder(f)
	if sc.Private {
		return Scope{}, httpx.ErrUnprocessable(
			"Soubor nelze přesunout do soukromé složky — ostatní členové konverzace by ho neotevřeli.")
	}
	return sc, nil
}
