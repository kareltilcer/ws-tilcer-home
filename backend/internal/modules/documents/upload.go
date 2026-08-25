package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/slug"
)

// UploadInput is one multipart upload: the file stream plus the form's metadata.
// The handler supplies File as the raw multipart part — this file owns the size
// cap, the checksum, the MIME sniff, and the storage write.
type UploadInput struct {
	Filename    string
	File        io.Reader
	FolderID    *string
	Title       string
	Description string
	// Scope selects the root when FolderID is nil (v9): "" or "shared" for the
	// household tree, "private" for the uploader's own. Resolved against the
	// session, never trusted as an owner id.
	Scope string
}

// Upload implements FR-DOC1. The ORDER of these steps is the contract:
//
//  1. Stream the bytes through a hard size cap into a temp file, teeing into a
//     SHA-256 hasher and a byte counter. Over the cap → 413 with nothing written.
//  2. Sniff the content type from the leading bytes — never the client's claim
//     (D48). Outside a configured allowlist → 415, again with nothing written.
//  3. Put the object to storage. A storage failure → 502 and NO database row: a
//     half-committed document must not exist.
//  4. Only then, in ONE transaction, insert the metadata row and its
//     document.create audit event. A slug conflict that cannot be auto-resolved
//     rolls back and best-effort deletes the just-written object.
//  5. Enqueue preview generation AFTER the commit, and respond 201.
//
// A crash between 3 and 4 leaves an orphaned object with no row — harmless, and the
// daily reconciliation pass sweeps it. The reverse (a row with no bytes) must never
// happen, which is why the object goes first.
//
// The temp file exists so the exact size and checksum are known before the single
// PutObject call: the S3 signer wants a known length, and a streaming multipart
// upload would need abort bookkeeping to avoid leaking parts on failure.
func (s *Service) Upload(ctx context.Context, in UploadInput) (*DocumentDetail, error) {
	if s.blob == nil {
		return nil, httpx.ErrInternal("document storage is not configured")
	}
	filename := strings.TrimSpace(in.Filename)
	if filename == "" {
		return nil, httpx.ErrUnprocessable("file is required")
	}

	// The id is minted up front because the storage keys are derived from it — the
	// object must be written to its final, permanent key before the row exists.
	id := idgen.New()
	storageKey := OriginalKey(id)

	tmp, err := os.CreateTemp(s.opts.TempDir, "home-upload-*")
	if err != nil {
		return nil, httpx.ErrInternal("cannot buffer the upload: " + err.Error())
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	// 1. Stream with the cap. Reading max+1 bytes is how we detect "over the cap"
	// without trusting Content-Length (a client can lie or omit it).
	hasher := sha256.New()
	limited := io.LimitReader(in.File, s.opts.MaxUploadBytes+1)
	size, err := io.Copy(io.MultiWriter(tmp, hasher), limited)
	if err != nil {
		return nil, httpx.ErrBadRequest("reading the upload failed: " + err.Error())
	}
	if size > s.opts.MaxUploadBytes {
		return nil, httpx.ErrTooLarge(fmt.Sprintf("file exceeds the %d MB limit", s.opts.MaxUploadBytes>>20))
	}
	if size == 0 {
		return nil, httpx.ErrUnprocessable("the uploaded file is empty")
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))

	// 2. Sniff from the bytes we just stored.
	head := make([]byte, sniffLen)
	n, err := tmp.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, httpx.ErrInternal("cannot inspect the upload: " + err.Error())
	}
	contentType := sniffContentType(head[:n], filename)
	if !mimeAllowed(contentType, s.opts.AllowedMIME) {
		return nil, httpx.ErrUnsupportedMedia(fmt.Sprintf("content type %q is not allowed", contentType))
	}
	previewKind, previewStatus := previewPlanFor(contentType, s.opts.PreviewEnabled)
	thumbStatus := thumbnailPlanFor(contentType, s.opts.PreviewEnabled)

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, httpx.ErrInternal("cannot rewind the upload: " + err.Error())
	}

	// 3. Object first. Nothing is written to the database until this returns.
	if err := s.blob.Put(ctx, storageKey, tmp, size, contentType); err != nil {
		s.logger.Error("documents: storing the uploaded object failed — nothing committed",
			"key", storageKey, "err", err)
		return nil, httpx.ErrBadGateway("document storage is unavailable")
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = titleFromFilename(filename)
	}
	if title == "" {
		title = filename
	}
	file := UploadedFile{
		OriginalFilename: filename,
		ContentType:      contentType,
		ByteSize:         size,
		Checksum:         checksum,
		StorageKey:       storageKey,
		PreviewKind:      previewKind,
		PreviewStatus:    previewStatus,
		ThumbnailStatus:  thumbStatus,
	}

	// 4. Row + audit in one transaction.
	//
	// v9: the requested root scope is resolved BEFORE the transaction so a bad
	// `scope` field is a 422 rather than a rolled-back write. With a folder_id the
	// parent's scope governs anyway; this only matters at the root.
	requested, err := ParseCreateScope(ctx, in.Scope)
	if err != nil {
		s.purgeObjects(ctx, []string{storageKey})
		return nil, err
	}
	var out *Document
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		sc, err := s.assertFolder(ctx, tx, in.FolderID, requested)
		if err != nil {
			return err
		}
		sl, err := s.freeSlug(ctx, tx, in.FolderID, sc, slug.Make(title), "", "")
		if err != nil {
			return err
		}
		pos, err := s.store.lastDocumentPosition(ctx, tx, in.FolderID, sc)
		if err != nil {
			return err
		}
		out, err = s.store.InsertDocument(ctx, tx, id, in.FolderID, title, sl,
			strings.TrimSpace(in.Description), file, pos, actorID(ctx), sc)
		if err != nil {
			return err
		}
		// document.create records what the file IS; there is no "old" value for any of
		// it (null → new), and the bytes themselves are never diffed (D50).
		changes := []audit.Change{
			{Field: "title", New: ap(title)},
			{Field: "slug", New: ap(sl)},
			{Field: "original_filename", New: ap(filename)},
			{Field: "content_type", New: ap(contentType)},
			{Field: "byte_size", New: ap(fmt.Sprint(size))},
			{Field: "checksum", New: ap(checksum)},
		}
		if in.FolderID != nil {
			changes = append(changes, audit.Change{Field: "folder_id", New: in.FolderID})
		}
		if d := strings.TrimSpace(in.Description); d != "" {
			changes = append(changes, audit.Change{Field: "description", New: ap(d)})
		}
		return s.record(ctx, tx, "document.create", "document", id,
			fmt.Sprintf("Nahrán dokument „%s“", title), changes, nil, sc)
	})
	if err != nil {
		// The row never landed, so the object is an orphan: delete it now rather than
		// leaving it for the reconciliation pass.
		s.purgeObjects(ctx, []string{storageKey})
		return nil, err
	}

	// 5. Derive the preview out of the request path (HANDOFF-6 §4).
	if s.enqueuePreview != nil && s.opts.PreviewEnabled && needsDerivedObject(contentType) {
		s.enqueuePreview(id)
	}
	s.notifyScoped(ctx, "document.changed", id, scopeOfDocument(out).Private)
	return s.documentDetail(ctx, s.db, out)
}
