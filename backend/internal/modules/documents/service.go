package documents

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"unicode"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/slug"
)

// Notifier publishes a websocket change after commit (mirrors the other modules).
type Notifier func(ctx context.Context, typ string, payload any)

// Options is the service's slice of the documents configuration.
type Options struct {
	MaxUploadBytes int64
	AllowedMIME    []string
	PreviewEnabled bool
	PublicBaseURL  string
	TempDir        string // "" = the OS default temp directory
}

// Service orchestrates documents/folders mutations (WithTx + audit-in-tx +
// notify), slug derivation/uniqueness, the path→id resolver, pinning, and the
// upload pipeline.
//
// Three invariants live here and are commented at the point they are enforced:
//
//   - Bytes are immutable (D41): Upload is the only writer of content, it writes a
//     given key once, and no update path can reach content_type/byte_size/checksum/
//     storage_key.
//   - The object must be durable BEFORE the row exists (FR-DOC1): a crash between
//     the two leaves a harmless orphan object (the reconciliation pass sweeps it),
//     whereas the reverse would leave a document row pointing at nothing.
//   - Every mutation is audited in the same transaction — except personal pins,
//     which are a per-user view preference (D47).
type Service struct {
	db     *sql.DB
	store  *Store
	sink   audit.Sink
	notify Notifier
	blob   blobstore.BlobStore
	opts   Options
	logger *slog.Logger

	// enqueuePreview hands a committed document to the preview worker. Set by
	// SetPreviewEnqueue so the worker can depend on the service without a cycle;
	// nil means "no worker" (previews disabled, or a test that doesn't need one).
	enqueuePreview func(documentID string)
}

func NewService(db *sql.DB, sink audit.Sink, notify Notifier, blob blobstore.BlobStore, opts Options, logger *slog.Logger) *Service {
	if notify == nil {
		notify = func(context.Context, string, any) {}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if opts.MaxUploadBytes <= 0 {
		opts.MaxUploadBytes = 50 << 20
	}
	return &Service{
		db:     db,
		store:  NewStore(db),
		sink:   sink,
		notify: notify,
		blob:   blob,
		opts:   opts,
		logger: logger,
	}
}

// Store exposes the read store (used by the widget provider and the mirror job).
func (s *Service) Store() *Store { return s.store }

// Blob exposes the object store (used by the content endpoints and the worker).
func (s *Service) Blob() blobstore.BlobStore { return s.blob }

// Options exposes the service configuration (used by the handler for its own
// streaming cap and by the worker).
func (s *Service) Options() Options { return s.opts }

// SetPreviewEnqueue wires the preview worker in after construction.
func (s *Service) SetPreviewEnqueue(fn func(documentID string)) { s.enqueuePreview = fn }

func actorID(ctx context.Context) string {
	if a, ok := reqctx.ActorFrom(ctx); ok {
		return a.UserID
	}
	return ""
}

func writeAllowed(ctx context.Context) bool {
	if a, ok := reqctx.ActorFrom(ctx); ok {
		return reqctx.HasRole(a.Roles, "editor", "admin")
	}
	return false
}

func ap(s string) *string { return &s }

func eqp(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func diff(changes *[]audit.Change, field string, old, newVal *string) {
	if !eqp(old, newVal) {
		*changes = append(*changes, audit.Change{Field: field, Old: old, New: newVal})
	}
}

func (s *Service) record(ctx context.Context, tx *sql.Tx, action, entityType, entityID, summary string, changes []audit.Change, meta map[string]any) error {
	_, err := s.sink.Record(ctx, tx, audit.Event{
		Module:     audit.ModuleDocuments,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		Summary:    summary,
		Meta:       meta,
		Changes:    changes,
	})
	return err
}

func metaVia(base map[string]any, via string) map[string]any {
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

// freeSlug returns base, or base-2/base-3/… until free in the parent scope across
// BOTH tables (the cross-table check plus the two COALESCE indexes together are the
// addressing invariant, D32/D40). Runs inside the mutation's tx.
func (s *Service) freeSlug(ctx context.Context, tx DBTX, parentScope *string, base, excludeFolderID, excludeDocumentID string) (string, error) {
	if base == "" {
		base = shortID()
	}
	candidate := base
	for i := 2; ; i++ {
		taken, err := s.store.SiblingSlugTaken(ctx, tx, parentScope, candidate, excludeFolderID, excludeDocumentID)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}

// ---- Documents ----

// GetDocumentDetail returns the document with its breadcrumb, slug path, permanent
// URLs, and the caller's pin state.
//
// Archived rows 404 like they do everywhere else. Handing one back would give the
// viewer a phantom: a document it renders as live, with Rename/Move/Delete buttons
// that every mutation then refuses. A 404 is also what the SPA's "this document is
// gone" state keys off, so a permalink to a soft-deleted document says so.
func (s *Service) GetDocumentDetail(ctx context.Context, id string) (*DocumentDetail, error) {
	d, err := s.store.GetDocument(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if d == nil || d.Archived {
		return nil, httpx.ErrNotFound("document not found")
	}
	return s.documentDetail(ctx, s.db, d)
}

// UpdateDocument changes METADATA ONLY: title (which re-derives the slug, so the
// navigation URL changes while the permanent id-based URL does not), description,
// and archived. There is no field for the bytes and no replace endpoint — a changed
// file is a new document (D41).
func (s *Service) UpdateDocument(ctx context.Context, id string, in DocumentUpdate, via string) (*DocumentDetail, error) {
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		return nil, httpx.ErrUnprocessable("title cannot be empty")
	}
	var out *Document
	var changed bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetDocument(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("document not found")
		}
		var patch documentPatch
		unarchiving := in.Archived != nil && !*in.Archived && before.Archived
		// An archived (soft-deleted) document is out of the tree and unreachable by
		// slug; restoring it is the only valid mutation. Reject metadata edits so a
		// stale client cannot silently write — and audit — edits to a phantom.
		if before.Archived && !unarchiving && (in.Title != nil || in.Description != nil) {
			return httpx.ErrNotFound("document not found")
		}
		if unarchiving {
			// A live document may not sit under an archived ancestor: Resolve walks only
			// live folders, so it would be unreachable.
			if err := s.assertParentLive(ctx, tx, before.FolderID); err != nil {
				return err
			}
		}
		// Only carry genuinely-changed fields into the patch, so a re-sent value leaves
		// the row (and updated_at) untouched and emits no audit event or broadcast.
		if in.Archived != nil && *in.Archived != before.Archived {
			patch.Archived = in.Archived
		}
		if in.Description != nil && *in.Description != deref(before.Description) {
			patch.Description = in.Description
		}
		titleChanged := false
		if in.Title != nil {
			if title := strings.TrimSpace(*in.Title); title != before.Title {
				titleChanged = true
				patch.Title = &title
				sl, err := s.freeSlug(ctx, tx, before.FolderID, slug.Make(title), "", before.ID)
				if err != nil {
					return err
				}
				patch.Slug = &sl
			}
		}
		if !titleChanged && unarchiving {
			// Its slug left the live sibling scope while archived and a sibling may have
			// reused it — re-free it (a no-op when still free) so re-entering the partial
			// unique index cannot fail.
			sl, err := s.freeSlug(ctx, tx, before.FolderID, before.Slug, "", before.ID)
			if err != nil {
				return err
			}
			patch.Slug = &sl
		}
		if err := s.store.UpdateDocument(ctx, tx, id, patch); err != nil {
			return err
		}
		out, err = s.store.GetDocument(ctx, tx, id)
		if err != nil {
			return err
		}
		// Metadata diffs only — the bytes are immutable, so there is nothing about the
		// content to diff (D50).
		var changes []audit.Change
		diff(&changes, "title", ap(before.Title), ap(out.Title))
		diff(&changes, "slug", ap(before.Slug), ap(out.Slug))
		diff(&changes, "description", before.Description, out.Description)
		diff(&changes, "archived", ap(fmt.Sprint(before.Archived)), ap(fmt.Sprint(out.Archived)))
		if len(changes) == 0 {
			return nil
		}
		changed = true
		return s.record(ctx, tx, "document.update", "document", id,
			fmt.Sprintf("Upraven dokument „%s“", out.Title), changes, metaVia(nil, via))
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notify(ctx, "document.changed", map[string]string{"id": id})
	}
	return s.documentDetail(ctx, s.db, out)
}

// MoveDocument reparents and/or reorders a document. It rewrites one row and never
// touches object storage: the keys are id-based, so the permanent content URL is
// unaffected (D42).
func (s *Service) MoveDocument(ctx context.Context, id string, in DocumentMoveRequest, via string) (*DocumentDetail, error) {
	if strings.TrimSpace(in.Position) == "" {
		return nil, httpx.ErrUnprocessable("position is required")
	}
	var out *Document
	var changed bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetDocument(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("document not found")
		}
		if err := assertLiveForMutation(before.Archived, "document not found"); err != nil {
			return err
		}
		if err := s.assertFolder(ctx, tx, in.FolderID); err != nil {
			return err
		}
		// Keep the current slug when it is free in the target parent, else re-derive.
		sl, err := s.freeSlug(ctx, tx, in.FolderID, before.Slug, "", before.ID)
		if err != nil {
			return err
		}
		// A move that re-sends the current folder/position/slug changes nothing: skip
		// the write, the audit event, and the broadcast.
		if eqp(before.FolderID, in.FolderID) && before.Position == in.Position && before.Slug == sl {
			out = before
			return nil
		}
		if err := s.store.MoveDocumentRow(ctx, tx, id, in.FolderID, in.Position, sl); err != nil {
			return err
		}
		out, err = s.store.GetDocument(ctx, tx, id)
		if err != nil {
			return err
		}
		var changes []audit.Change
		diff(&changes, "folder_id", before.FolderID, out.FolderID)
		diff(&changes, "slug", ap(before.Slug), ap(out.Slug))
		diff(&changes, "position", ap(before.Position), ap(out.Position))
		changed = true
		return s.record(ctx, tx, "document.move", "document", id,
			fmt.Sprintf("Přesunut dokument „%s“", out.Title), changes, metaVia(nil, via))
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notify(ctx, "document.changed", map[string]string{"id": id})
	}
	return s.documentDetail(ctx, s.db, out)
}

// DeleteDocument soft-deletes by default. hard=true (admin, enforced at the route)
// purges the row AND its object-storage objects.
func (s *Service) DeleteDocument(ctx context.Context, id string, hard bool) error {
	var changed bool
	var purge []string
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetStoredDocument(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("document not found")
		}
		if hard {
			// Collect the keys INSIDE the transaction, while the row still exists; the
			// objects are deleted after commit (see below).
			purge = objectKeysOf(*before)
			if err := s.store.DeleteDocument(ctx, tx, id); err != nil {
				return err
			}
			changed = true
			return s.record(ctx, tx, "document.delete", "document", id,
				fmt.Sprintf("Smazán dokument „%s“", before.Title), nil, metaHard(true))
		}
		// Soft delete is idempotent: archiving an already-archived document writes
		// nothing, emits no bogus false→true diff, and broadcasts nothing.
		if before.Archived {
			return nil
		}
		if err := s.store.UpdateDocument(ctx, tx, id, documentPatch{Archived: boolPtr(true)}); err != nil {
			return err
		}
		changed = true
		return s.record(ctx, tx, "document.delete", "document", id,
			fmt.Sprintf("Smazán dokument „%s“", before.Title),
			[]audit.Change{{Field: "archived", Old: ap("false"), New: ap("true")}}, metaHard(false))
	})
	if err != nil {
		return err
	}
	// Objects are purged only AFTER the row is gone. If this fails the objects are
	// orphans, which the reconciliation pass sweeps — the reverse order would risk a
	// live row pointing at deleted bytes.
	s.purgeObjects(ctx, purge)
	if changed {
		s.notify(ctx, "document.changed", map[string]string{"id": id})
	}
	return nil
}

// objectKeysOf lists every object a document owns (original + any derived ones).
func objectKeysOf(sd storedDocument) []string {
	keys := []string{sd.StorageKey}
	if sd.PreviewKey != nil && *sd.PreviewKey != "" {
		keys = append(keys, *sd.PreviewKey)
	}
	if sd.ThumbnailKey != nil && *sd.ThumbnailKey != "" {
		keys = append(keys, *sd.ThumbnailKey)
	}
	return keys
}

// purgeObjects deletes objects best-effort. A failure is logged, never returned:
// the row is already gone, the user's delete succeeded, and the leftover bytes are
// an orphan the reconciliation pass will clean up (D45).
func (s *Service) purgeObjects(ctx context.Context, keys []string) {
	if len(keys) == 0 || s.blob == nil {
		return
	}
	if err := s.blob.Delete(ctx, keys...); err != nil {
		s.logger.Warn("documents: purging objects failed — reconciliation will retry",
			"keys", len(keys), "err", err)
	}
}

// ---- Folders ----

func (s *Service) CreateFolder(ctx context.Context, in DocFolderCreate) (*DocFolderDetail, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, httpx.ErrUnprocessable("name is required")
	}
	var out *DocFolder
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.assertFolder(ctx, tx, in.ParentID); err != nil {
			return err
		}
		sl, err := s.freeSlug(ctx, tx, in.ParentID, slug.Make(name), "", "")
		if err != nil {
			return err
		}
		pos, err := s.store.lastFolderPosition(ctx, tx, in.ParentID)
		if err != nil {
			return err
		}
		out, err = s.store.InsertFolder(ctx, tx, in.ParentID, name, sl, pos, actorID(ctx))
		if err != nil {
			return err
		}
		changes := []audit.Change{{Field: "name", New: ap(name)}, {Field: "slug", New: ap(sl)}}
		return s.record(ctx, tx, "document_folder.create", "document_folder", out.ID,
			fmt.Sprintf("Vytvořena složka dokumentů „%s“", name), changes, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify(ctx, "document_folder.changed", map[string]string{"id": out.ID})
	return s.folderDetail(ctx, s.db, out)
}

func (s *Service) GetFolderDetail(ctx context.Context, id string) (*DocFolderDetail, error) {
	f, err := s.store.GetFolder(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	// Archived is soft-deleted: out of the tree, unreachable by slug, and 404 on the
	// document side too (GetDocumentDetail). Answering 200 here would be the one place
	// the API still hands out a phantom.
	if f == nil || f.Archived {
		return nil, httpx.ErrNotFound("folder not found")
	}
	return s.folderDetail(ctx, s.db, f)
}

func (s *Service) UpdateFolder(ctx context.Context, id string, in DocFolderUpdate) (*DocFolderDetail, error) {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return nil, httpx.ErrUnprocessable("name cannot be empty")
	}
	var out *DocFolder
	var changed bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetFolder(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("folder not found")
		}
		archiving := in.Archived != nil && *in.Archived && !before.Archived
		unarchiving := in.Archived != nil && !*in.Archived && before.Archived
		// An archived (soft-deleted) folder is out of the tree and unreachable by slug;
		// restoring it is the only valid mutation. Reject renames so a stale client
		// cannot silently write — and audit — edits to a phantom (UpdateDocument draws
		// the same line for documents).
		if before.Archived && !unarchiving && in.Name != nil {
			return httpx.ErrNotFound("folder not found")
		}
		if archiving {
			// Archiving must cascade, else live children are stranded under an archived
			// ancestor (invisible in the tree, unreachable by slug). Non-empty folders go
			// through DELETE ?cascade=true; this path only archives an empty folder.
			sub, docs, err := s.store.FolderChildCounts(ctx, tx, id)
			if err != nil {
				return err
			}
			if sub > 0 || docs > 0 {
				return httpx.ErrConflict(fmt.Sprintf("folder not empty (%d subfolders, %d documents) — archive via DELETE with cascade=true", sub, docs))
			}
		}
		if unarchiving {
			if err := s.assertParentLive(ctx, tx, before.ParentID); err != nil {
				return err
			}
		}
		if in.Name != nil {
			name := strings.TrimSpace(*in.Name)
			sl, err := s.freeSlug(ctx, tx, before.ParentID, slug.Make(name), before.ID, "")
			if err != nil {
				return err
			}
			if err := s.store.RenameFolder(ctx, tx, id, name, sl); err != nil {
				return err
			}
		} else if unarchiving {
			sl, err := s.freeSlug(ctx, tx, before.ParentID, before.Slug, before.ID, "")
			if err != nil {
				return err
			}
			if err := s.store.RenameFolder(ctx, tx, id, before.Name, sl); err != nil {
				return err
			}
		}
		if in.Archived != nil {
			if err := s.store.SetFolderArchived(ctx, tx, id, *in.Archived); err != nil {
				return err
			}
		}
		out, err = s.store.GetFolder(ctx, tx, id)
		if err != nil {
			return err
		}
		var changes []audit.Change
		diff(&changes, "name", ap(before.Name), ap(out.Name))
		diff(&changes, "slug", ap(before.Slug), ap(out.Slug))
		diff(&changes, "archived", ap(fmt.Sprint(before.Archived)), ap(fmt.Sprint(out.Archived)))
		if len(changes) == 0 {
			return nil
		}
		changed = true
		return s.record(ctx, tx, "document_folder.update", "document_folder", id,
			fmt.Sprintf("Upravena složka dokumentů „%s“", out.Name), changes, nil)
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notify(ctx, "document_folder.changed", map[string]string{"id": id})
	}
	return s.folderDetail(ctx, s.db, out)
}

func (s *Service) MoveFolder(ctx context.Context, id string, in DocFolderMoveRequest) (*DocFolderDetail, error) {
	if strings.TrimSpace(in.Position) == "" {
		return nil, httpx.ErrUnprocessable("position is required")
	}
	var out *DocFolder
	var changed bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetFolder(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("folder not found")
		}
		if err := assertLiveForMutation(before.Archived, "folder not found"); err != nil {
			return err
		}
		if err := s.assertFolder(ctx, tx, in.ParentID); err != nil {
			return err
		}
		// Cycle guard, BEFORE any write: a folder may not move into itself or a
		// descendant, which would detach the subtree from the root entirely.
		cycles, err := s.wouldCycle(ctx, tx, id, in.ParentID)
		if err != nil {
			return err
		}
		if cycles {
			return httpx.ErrUnprocessable("a folder cannot be moved into itself or one of its descendants")
		}
		sl, err := s.freeSlug(ctx, tx, in.ParentID, before.Slug, before.ID, "")
		if err != nil {
			return err
		}
		if eqp(before.ParentID, in.ParentID) && before.Position == in.Position && before.Slug == sl {
			out = before
			return nil
		}
		// Exactly one row is written — descendants keep their slugs, and no object
		// storage is touched.
		if err := s.store.MoveFolderRow(ctx, tx, id, in.ParentID, in.Position, sl); err != nil {
			return err
		}
		out, err = s.store.GetFolder(ctx, tx, id)
		if err != nil {
			return err
		}
		var changes []audit.Change
		diff(&changes, "parent_id", before.ParentID, out.ParentID)
		diff(&changes, "slug", ap(before.Slug), ap(out.Slug))
		diff(&changes, "position", ap(before.Position), ap(out.Position))
		changed = true
		return s.record(ctx, tx, "document_folder.move", "document_folder", id,
			fmt.Sprintf("Přesunuta složka dokumentů „%s“", out.Name), changes, nil)
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notify(ctx, "document_folder.changed", map[string]string{"id": id})
	}
	return s.folderDetail(ctx, s.db, out)
}

// DeleteFolder soft-deletes by default. A non-empty folder needs cascade=true
// whichever mode is used (soft: the subtree is archived, each child logged);
// hard=true purges the rows AND every descendant document's objects (admin,
// enforced at the route).
func (s *Service) DeleteFolder(ctx context.Context, id string, cascade, hard bool) error {
	var changed bool
	var purge []string
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetFolder(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("folder not found")
		}
		// The counts are LIVE ones in both modes, because cascade=true means "yes, take
		// the children I was shown" and the tree the caller confirmed against only ever
		// lists live rows. Archived descendants get their own guard in the hard branch —
		// they can never be acknowledged by a cascade flag, having never been displayed.
		sub, docCount, err := s.store.FolderChildCounts(ctx, tx, id)
		if err != nil {
			return err
		}
		nonEmpty := sub > 0 || docCount > 0

		// The guard is checked BEFORE the hard branch, not after: a hard delete is the
		// irreversible one (rows purged, objects purged), so it is the last place that
		// should be allowed to take out a subtree the caller never acknowledged.
		if nonEmpty && !cascade {
			return httpx.ErrConflict(fmt.Sprintf("folder not empty (%d subfolders, %d documents) — pass cascade=true", sub, docCount))
		}

		if hard {
			// FK ON DELETE CASCADE purges descendant folders/documents/pins. Enumerate
			// the whole physical subtree (including already-archived rows) BEFORE the
			// delete, both to audit every purged descendant — otherwise the cascade
			// destroys them with no trail — and to collect every object key to purge.
			folderIDs, err := s.store.DescendantFolderIDs(ctx, tx, id, true)
			if err != nil {
				return err
			}
			childDocs, err := s.store.DocumentsInFolders(ctx, tx, folderIDs, true)
			if err != nil {
				return err
			}
			metas, err := s.store.FolderMetaByIDs(ctx, tx, folderIDs)
			if err != nil {
				return err
			}
			// Nothing archived may be purged as a side effect. Such rows are absent from
			// the tree the caller confirmed against — at ANY depth, so a live subfolder
			// holding archived documents counts too — which means cascade=true cannot
			// stand as consent for them. Refuse and let the admin deal with each one; the
			// alternative is destroying rows and bytes nobody was ever shown.
			//
			// An ARCHIVED TARGET is the one case that guard must not cover. Purging a
			// soft-deleted folder is a supported operation, and the soft delete that got
			// it here archived its whole subtree on the way — so every descendant is
			// hidden by construction and the guard would refuse forever, leaving no way
			// to purge the folder at all. Consent is taken over the PHYSICAL subtree
			// instead: the live counts above are all zero after a soft cascade and would
			// wave the purge through unacknowledged, so cascade=true is required against
			// what is really there.
			if before.Archived {
				subAll, docAll := len(folderIDs)-1, len(childDocs)
				if (subAll > 0 || docAll > 0) && !cascade {
					return httpx.ErrConflict(fmt.Sprintf(
						"deleted folder still holds %d subfolders and %d documents — pass cascade=true to purge them and their files", subAll, docAll))
				}
			} else {
				hidden := 0
				for _, d := range childDocs {
					if d.Archived {
						hidden++
					}
				}
				for _, fID := range folderIDs {
					if fID != id && metas[fID].Archived {
						hidden++
					}
				}
				if hidden > 0 {
					// Its own code, not the plain "conflict" the cascade refusals use. Both are
					// 409s on this route, and the remedies are opposites: "confirm again with
					// cascade" versus "cascade cannot help you, deal with those rows first". A
					// client that cannot tell them apart names the wrong one.
					return httpx.ErrConflictCode("archived_descendants", fmt.Sprintf(
						"folder holds %d already-deleted item(s) whose files would be purged with it — delete those individually first", hidden))
				}
			}
			for _, d := range childDocs {
				purge = append(purge, objectKeysOf(d)...)
			}
			if err := s.store.DeleteFolder(ctx, tx, id); err != nil {
				return err
			}
			changed = true
			for _, d := range childDocs {
				if err := s.record(ctx, tx, "document.delete", "document", d.ID,
					fmt.Sprintf("Smazán dokument „%s“ (kaskádou)", d.Title), nil,
					map[string]any{"hard": true, "via": "cascade"}); err != nil {
					return err
				}
			}
			// Deepest folders first so the audit reads leaf→root; the target folder
			// records last, marked hard without the cascade via.
			for i := len(folderIDs) - 1; i >= 0; i-- {
				fID := folderIDs[i]
				m := map[string]any{"hard": true}
				if fID != id {
					m["via"] = "cascade"
				}
				if err := s.record(ctx, tx, "document_folder.delete", "document_folder", fID,
					fmt.Sprintf("Smazána složka dokumentů „%s“", metas[fID].Name), nil, m); err != nil {
					return err
				}
			}
			return nil
		}

		// Soft cascade: archive the whole live subtree, logging each child. Objects are
		// untouched — a soft delete is reversible, so the bytes must survive.
		folderIDs, err := s.store.DescendantFolderIDs(ctx, tx, id, false)
		if err != nil {
			return err
		}
		childDocs, err := s.store.DocumentsInFolders(ctx, tx, folderIDs, false)
		if err != nil {
			return err
		}
		for _, d := range childDocs {
			if err := s.store.UpdateDocument(ctx, tx, d.ID, documentPatch{Archived: boolPtr(true)}); err != nil {
				return err
			}
			changed = true
			if err := s.record(ctx, tx, "document.delete", "document", d.ID,
				fmt.Sprintf("Smazán dokument „%s“ (kaskádou)", d.Title),
				[]audit.Change{{Field: "archived", Old: ap("false"), New: ap("true")}},
				map[string]any{"via": "cascade"}); err != nil {
				return err
			}
		}
		metas, err := s.store.FolderMetaByIDs(ctx, tx, folderIDs)
		if err != nil {
			return err
		}
		for i := len(folderIDs) - 1; i >= 0; i-- {
			fID := folderIDs[i]
			meta, ok := metas[fID]
			if !ok || meta.Archived {
				continue // re-deleting an already-archived folder: no bogus false→true
			}
			if err := s.store.SetFolderArchived(ctx, tx, fID, true); err != nil {
				return err
			}
			changed = true
			m := map[string]any{}
			if fID != id {
				m["via"] = "cascade"
			}
			if len(m) == 0 {
				m = nil
			}
			if err := s.record(ctx, tx, "document_folder.delete", "document_folder", fID,
				fmt.Sprintf("Smazána složka dokumentů „%s“", meta.Name),
				[]audit.Change{{Field: "archived", Old: ap("false"), New: ap("true")}}, m); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.purgeObjects(ctx, purge)
	if changed {
		s.notify(ctx, "document_folder.changed", map[string]string{"id": id})
	}
	return nil
}

// wouldCycle reports whether moving movingID under newParent would create a cycle
// (newParent is movingID itself or one of its descendants). Walks up from newParent
// to the root; the depth cap breaks any pre-existing cycle in the data.
func (s *Service) wouldCycle(ctx context.Context, tx DBTX, movingID string, newParent *string) (bool, error) {
	cur := newParent
	for depth := 0; cur != nil && depth < 1000; depth++ {
		if *cur == movingID {
			return true, nil
		}
		f, err := s.store.GetFolder(ctx, tx, *cur)
		if err != nil {
			return false, err
		}
		if f == nil {
			break
		}
		cur = f.ParentID
	}
	return false, nil
}

// assertFolder verifies a referenced parent/folder exists and is live.
func (s *Service) assertFolder(ctx context.Context, q DBTX, folderID *string) error {
	if folderID == nil {
		return nil
	}
	f, err := s.store.GetFolder(ctx, q, *folderID)
	if err != nil {
		return err
	}
	if f == nil || f.Archived {
		return httpx.ErrUnprocessable("folder_id does not reference an existing folder")
	}
	return nil
}

// assertParentLive rejects restoring an item under a missing or archived parent:
// every ancestor of a live item must be live, since Resolve walks only live folders.
// nil = root.
func (s *Service) assertParentLive(ctx context.Context, q DBTX, parentID *string) error {
	if parentID == nil {
		return nil
	}
	f, err := s.store.GetFolder(ctx, q, *parentID)
	if err != nil {
		return err
	}
	if f == nil || f.Archived {
		return httpx.ErrUnprocessable("cannot restore under a missing or archived parent folder")
	}
	return nil
}

// assertLiveForMutation rejects a structural mutation (move/reorder/reparent) of an
// archived item: it is out of the live tree and unreachable by slug, so a stale
// client must 404 rather than mutate a phantom.
func assertLiveForMutation(archived bool, notFoundMsg string) error {
	if archived {
		return httpx.ErrNotFound(notFoundMsg)
	}
	return nil
}

// ---- Tree, list, search, resolve ----

// Tree is the navigation read model: the folder tree with lightweight document
// nodes. Three bounded queries total (folders, document summaries, pin sets) — the
// tree is assembled in memory, never with a query per folder.
func (s *Service) Tree(ctx context.Context, includeArchived bool) (*DocumentsTree, error) {
	folders, err := s.store.AllFolders(ctx, includeArchived)
	if err != nil {
		return nil, err
	}
	summaries, err := s.store.AllDocumentSummaries(ctx, includeArchived)
	if err != nil {
		return nil, err
	}
	hh, pers, err := s.store.PinSets(ctx, actorID(ctx))
	if err != nil {
		return nil, err
	}

	docsByFolder := map[string][]DocumentSummary{}
	for _, d := range summaries {
		d.Pinned = PinState{Household: hh[d.ID], Personal: pers[d.ID]}
		key := deref(d.FolderID)
		docsByFolder[key] = append(docsByFolder[key], d)
	}
	foldersByParent := map[string][]DocFolder{}
	for _, f := range folders {
		key := deref(f.ParentID)
		foldersByParent[key] = append(foldersByParent[key], f)
	}

	// visited breaks any parent_id cycle in the data so build cannot recurse
	// unbounded — mirroring the depth caps in wouldCycle and ancestors.
	visited := map[string]bool{}
	var build func(parentID string) []DocFolderNode
	build = func(parentID string) []DocFolderNode {
		nodes := []DocFolderNode{}
		for _, f := range foldersByParent[parentID] {
			if visited[f.ID] {
				continue
			}
			visited[f.ID] = true
			nodes = append(nodes, DocFolderNode{
				Folder:     f,
				Subfolders: build(f.ID),
				Documents:  orEmptyDocs(docsByFolder[f.ID]),
			})
		}
		return nodes
	}
	return &DocumentsTree{
		Roots:         build(""),
		RootDocuments: orEmptyDocs(docsByFolder[""]),
		MaxUploadMB:   int(s.opts.MaxUploadBytes >> 20),
	}, nil
}

// searchLimit caps an FTS search; listLimitDefault/Max bound the keyset list.
const (
	searchLimit      = 100
	listLimitDefault = 50
	listLimitMax     = 200
)

// List returns document summaries. With q set it runs the FTS5 search over title +
// filename + description (never file contents, D46) and returns no cursor; without
// q it returns a keyset-paged page ordered newest-updated first.
func (s *Service) List(ctx context.Context, q string, folderID *string, includeArchived bool, limit int, cursor string) (DocumentPage, error) {
	q = strings.TrimSpace(q)
	var items []DocumentSummary
	var next *string

	if q != "" {
		match := ftsQuery(q)
		if match == "" {
			// A punctuation-only query has no searchable tokens. Return empty rather than
			// run `documents_fts MATCH ''`, whose empty-phrase behaviour is unspecified.
			return DocumentPage{Items: []DocumentSummary{}}, nil
		}
		var err error
		items, err = s.store.SearchDocuments(ctx, match, folderID, includeArchived, searchLimit)
		if err != nil {
			return DocumentPage{}, err
		}
	} else {
		if limit <= 0 {
			limit = listLimitDefault
		}
		if limit > listLimitMax {
			limit = listLimitMax
		}
		cursorTS, cursorID := splitCursor(cursor)
		var err error
		items, err = s.store.ListDocuments(ctx, folderID, includeArchived, limit, cursorTS, cursorID)
		if err != nil {
			return DocumentPage{}, err
		}
		// A full page means there may be more; the cursor is the composite key of the
		// last row so the next page resumes exactly after it.
		if len(items) == limit {
			last := items[len(items)-1]
			c := last.UpdatedAt + "|" + last.ID
			next = &c
		}
	}

	hh, pers, err := s.store.PinSets(ctx, actorID(ctx))
	if err != nil {
		return DocumentPage{}, err
	}
	for i := range items {
		items[i].Pinned = PinState{Household: hh[items[i].ID], Personal: pers[items[i].ID]}
	}
	if items == nil {
		items = []DocumentSummary{}
	}
	return DocumentPage{Items: items, NextCursor: next}, nil
}

// Resolve maps a slug path to a stable id (FR-DOC5). No redirects: a renamed or
// moved item's old path simply 404s (D32). This is the NAVIGATION address — the
// permanent one is id-based.
func (s *Service) Resolve(ctx context.Context, path string) (*DocResolveResult, error) {
	segs := splitPath(path)
	if len(segs) == 0 {
		return nil, httpx.ErrNotFound("empty path")
	}
	var parent *string
	for i, seg := range segs {
		last := i == len(segs)-1
		f, err := s.store.ChildFolderBySlug(ctx, s.db, parent, seg)
		if err != nil {
			return nil, err
		}
		if !last {
			// Intermediate segments must be folders.
			if f == nil {
				return nil, httpx.ErrNotFound("path not found")
			}
			parent = &f.ID
			continue
		}
		// The final segment may be a folder or a document under this parent.
		if f != nil {
			return &DocResolveResult{Type: "folder", ID: f.ID, SlugPath: strings.Join(segs, "/")}, nil
		}
		d, err := s.store.ChildDocumentBySlug(ctx, s.db, parent, seg)
		if err != nil {
			return nil, err
		}
		if d != nil {
			return &DocResolveResult{Type: "document", ID: d.ID, SlugPath: strings.Join(segs, "/")}, nil
		}
		return nil, httpx.ErrNotFound("path not found")
	}
	return nil, httpx.ErrNotFound("path not found")
}

// ---- Pinning (two scopes, D47) ----

// Pin adds a pin. A household pin ("pro všechny") is shared state: editor/admin,
// audited, broadcast. A personal pin ("jen pro mě") is a per-user view preference:
// allowed for any member INCLUDING a reader, not audited, not broadcast. That
// reader exception is deliberately narrow — it is the only documents write a reader
// may make.
func (s *Service) Pin(ctx context.Context, documentID, scopeStr, via string) (*PinState, error) {
	scope, err := parseScope(scopeStr)
	if err != nil {
		return nil, err
	}
	d, err := s.store.GetDocument(ctx, s.db, documentID)
	if err != nil {
		return nil, err
	}
	if d == nil || d.Archived {
		return nil, httpx.ErrNotFound("document not found")
	}
	uid := actorID(ctx)

	if scope == scopePersonal {
		// No audit and no broadcast (D47), but still run the read-then-write
		// (MAX(position) → INSERT) inside WithTx so BEGIN IMMEDIATE serialises it,
		// matching the household path.
		err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
			exists, err := s.store.PinExists(ctx, tx, documentID, scopePersonal, uid)
			if err != nil || exists {
				return err // idempotent re-pin: skip the position scan and the no-op insert
			}
			pos, err := s.store.lastPinPosition(ctx, tx, scopePersonal, uid)
			if err != nil {
				return err
			}
			_, err = s.store.InsertPin(ctx, tx, documentID, scopePersonal, &uid, uid, pos)
			return err
		})
		if err != nil {
			return nil, err
		}
		return s.pinState(ctx, documentID, uid)
	}

	if !writeAllowed(ctx) {
		return nil, httpx.ErrForbidden("household pin requires editor or admin")
	}
	var changed bool
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		exists, err := s.store.PinExists(ctx, tx, documentID, scopeHousehold, "")
		if err != nil || exists {
			return err
		}
		pos, err := s.store.lastPinPosition(ctx, tx, scopeHousehold, "")
		if err != nil {
			return err
		}
		inserted, err := s.store.InsertPin(ctx, tx, documentID, scopeHousehold, nil, uid, pos)
		if err != nil {
			return err
		}
		changed = inserted
		if inserted { // idempotent: only audit a real change
			return s.record(ctx, tx, "document.pin", "document", documentID,
				fmt.Sprintf("Připnut dokument „%s“ pro všechny", d.Title), nil,
				metaVia(map[string]any{"scope": scopeHousehold}, via))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notify(ctx, "document.pin", map[string]string{"document_id": documentID})
	}
	return s.pinState(ctx, documentID, uid)
}

// Unpin removes a pin, mirroring Pin's audit/broadcast rules.
func (s *Service) Unpin(ctx context.Context, documentID, scopeStr, via string) (*PinState, error) {
	scope, err := parseScope(scopeStr)
	if err != nil {
		return nil, err
	}
	d, err := s.store.GetDocument(ctx, s.db, documentID)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, httpx.ErrNotFound("document not found")
	}
	uid := actorID(ctx)

	if scope == scopePersonal {
		if _, err := s.store.DeletePin(ctx, s.db, documentID, scopePersonal, &uid); err != nil {
			return nil, err
		}
		return s.pinState(ctx, documentID, uid)
	}

	if !writeAllowed(ctx) {
		return nil, httpx.ErrForbidden("household pin requires editor or admin")
	}
	var changed bool
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		removed, err := s.store.DeletePin(ctx, tx, documentID, scopeHousehold, nil)
		if err != nil {
			return err
		}
		changed = removed
		if removed {
			return s.record(ctx, tx, "document.unpin", "document", documentID,
				fmt.Sprintf("Odepnut dokument „%s“ (pro všechny)", d.Title), nil,
				metaVia(map[string]any{"scope": scopeHousehold}, via))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notify(ctx, "document.unpin", map[string]string{"document_id": documentID})
	}
	return s.pinState(ctx, documentID, uid)
}

func (s *Service) pinState(ctx context.Context, documentID, userID string) (*PinState, error) {
	st, err := s.store.GetPinState(ctx, s.db, documentID, userID)
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// ---- detail assembly ----

func (s *Service) documentDetail(ctx context.Context, q DBTX, d *Document) (*DocumentDetail, error) {
	path, err := s.ancestors(ctx, q, d.FolderID)
	if err != nil {
		return nil, err
	}
	st, err := s.store.GetPinState(ctx, q, d.ID, actorID(ctx))
	if err != nil {
		return nil, err
	}
	doc := *d
	doc.Urls = s.withPublicBase(doc.Urls)
	return &DocumentDetail{
		Document: doc,
		Path:     path,
		SlugPath: slugPathFrom(path, d.Slug),
		Pinned:   st,
	}, nil
}

// withPublicBase absolutises the permalink when HOME_DOCS_PUBLIC_BASE_URL is set,
// so "Kopírovat odkaz" yields a link that works when pasted anywhere. The content
// URLs stay relative: they are only ever fetched by the SPA, same-origin.
func (s *Service) withPublicBase(u DocumentUrls) DocumentUrls {
	if s.opts.PublicBaseURL != "" {
		u.Permalink = s.opts.PublicBaseURL + u.Permalink
	}
	return u
}

func (s *Service) folderDetail(ctx context.Context, q DBTX, f *DocFolder) (*DocFolderDetail, error) {
	path, err := s.ancestors(ctx, q, &f.ID)
	if err != nil {
		return nil, err
	}
	subs, err := s.store.ChildFolders(ctx, q, &f.ID, false)
	if err != nil {
		return nil, err
	}
	docs, err := s.store.DocumentSummariesInFolder(ctx, q, &f.ID, false)
	if err != nil {
		return nil, err
	}
	hh, pers, err := s.store.PinSets(ctx, actorID(ctx))
	if err != nil {
		return nil, err
	}
	for i := range docs {
		docs[i].Pinned = PinState{Household: hh[docs[i].ID], Personal: pers[docs[i].ID]}
	}
	return &DocFolderDetail{
		DocFolder:  *f,
		Path:       path,
		SlugPath:   slugPathFrom(path, ""),
		Subfolders: subs,
		Documents:  docs,
	}, nil
}

// ancestors returns the folder chain root→…→folderID (inclusive). Empty for a root
// item (folderID nil).
func (s *Service) ancestors(ctx context.Context, q DBTX, folderID *string) ([]PathSegment, error) {
	var chain []PathSegment
	cur := folderID
	for depth := 0; cur != nil && depth < 1000; depth++ {
		f, err := s.store.GetFolder(ctx, q, *cur)
		if err != nil {
			return nil, err
		}
		if f == nil {
			break
		}
		chain = append(chain, PathSegment{ID: f.ID, Name: f.Name, Slug: f.Slug})
		cur = f.ParentID
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

// ---- small helpers ----

func boolPtr(b bool) *bool { return &b }

func metaHard(hard bool) map[string]any {
	if hard {
		return map[string]any{"hard": true}
	}
	return nil
}

func parseScope(s string) (string, error) {
	switch s {
	case scopeHousehold, scopePersonal:
		return s, nil
	}
	return "", httpx.ErrUnprocessable("scope must be household or personal")
}

func shortID() string {
	id := strings.ReplaceAll(idgen.New(), "-", "")
	if len(id) > 8 {
		id = id[:8]
	}
	return id
}

func orEmptyDocs(x []DocumentSummary) []DocumentSummary {
	if x == nil {
		return []DocumentSummary{}
	}
	return x
}

func slugPathFrom(segs []PathSegment, ownSlug string) string {
	parts := make([]string, 0, len(segs)+1)
	for _, s := range segs {
		parts = append(parts, s.Slug)
	}
	if ownSlug != "" {
		parts = append(parts, ownSlug)
	}
	return strings.Join(parts, "/")
}

func splitPath(p string) []string {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" {
		return nil
	}
	raw := strings.Split(p, "/")
	out := make([]string, 0, len(raw))
	for _, seg := range raw {
		if seg == "" {
			continue
		}
		if dec, err := url.PathUnescape(seg); err == nil {
			seg = dec
		}
		out = append(out, seg)
	}
	return out
}

// splitCursor parses the composite "<updated_at>|<id>" list cursor. An unparseable
// cursor is treated as "start from the beginning" rather than an error — a stale
// bookmark should show the first page, not a 422.
func splitCursor(c string) (ts, id string) {
	c = strings.TrimSpace(c)
	if c == "" {
		return "", ""
	}
	i := strings.LastIndex(c, "|")
	if i <= 0 || i == len(c)-1 {
		return "", ""
	}
	return c[:i], c[i+1:]
}

// ftsQuery turns free text into a safe FTS5 prefix MATCH: each whitespace token
// becomes a quoted prefix term, so punctuation cannot break the FTS syntax. Returns
// "" when the query has no searchable tokens — the caller treats that as "matches
// nothing" and skips the MATCH entirely.
func ftsQuery(q string) string {
	fields := strings.Fields(q)
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.ReplaceAll(f, `"`, "")
		// A token with no letters/digits (e.g. "#" or "!!!") tokenizes to zero terms;
		// a prefix '*' on that empty phrase is an FTS5 syntax error.
		if !strings.ContainsFunc(f, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) {
			continue
		}
		terms = append(terms, `"`+f+`"*`)
	}
	return strings.Join(terms, " ")
}
