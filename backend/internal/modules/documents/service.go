package documents

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/cursor"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/foldericon"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/optional"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/slug"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/slugpath"
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
	sink   audit.ModuleSink
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
		sink:   audit.For(sink, audit.ModuleDocuments),
		notify: notify,
		blob:   blob,
		opts:   opts,
		logger: logger,
	}
}

// Store exposes the read store (used by the widget provider and the mirror job).
func (s *Service) Store() *Store { return s.store }

// notifyScoped publishes a live-change signal, WITHOUT the item id when the item
// is private (v9, D180/D187). Mirrors notes.Service.notifyScoped — see there for
// the reasoning in full.
//
// ⚠ ws.Hub.Publish fans out to EVERY connected client, not to an audience, so a
// raw id in the payload of a private mutation is a real-time existence-and-activity
// oracle over another member's tree. It is the same disclosure audit.Redact blanks
// EntityID to prevent. The TYPE still goes out — that something private happened is
// not the secret (D187) — and it is all the client needs: api/ws.ts's classify()
// switches on the type and invalidates by module prefix, never reading the id.
//
// ⚠ THE `private` MARKER IS WHAT KEEPS THE TOAST HONEST. The frame still fans out,
// because the OWNER's other tabs and devices have to refetch — but for everybody
// else it announces a change they will never be able to see, and api/ws.ts turned
// that into "Dokumenty byly změněny jinde" on their screen. The marker lets the
// client invalidate silently instead: a wasted refetch is cheap, a toast about an
// invisible change is both noise and a live activity indicator over another
// member's tree. It names nothing — no id, no owner — so it discloses no more than
// the missing id already does.
func (s *Service) notifyScoped(ctx context.Context, typ, id string, private bool) {
	if private {
		s.notify(ctx, typ, map[string]string{"private": "1"})
		return
	}
	s.notify(ctx, typ, map[string]string{"id": id})
}

// Blob exposes the object store (used by the content endpoints and the worker).
func (s *Service) Blob() blobstore.BlobStore { return s.blob }

// Options exposes the service configuration (used by the handler for its own
// streaming cap and by the worker).
func (s *Service) Options() Options { return s.opts }

// SetPreviewEnqueue wires the preview worker in after construction.
func (s *Service) SetPreviewEnqueue(fn func(documentID string)) { s.enqueuePreview = fn }

// record writes one audit event in the caller's transaction.
//
// ⚠ v9 made the Scope a REQUIRED parameter rather than something a caller adds to
// `meta` when they remember (leak table row 11). Every documents.* event now
// carries `meta.visibility`, and a private one also `meta.owner_id`; those two
// keys are the ONLY thing that lets a read path tell a private event apart, so an
// event written without them can never be redacted. Making it a parameter means
// the compiler asks the question at every call site.
//
// The summary and diffs are written IN FULL (D187) — redaction happens at read
// time, in one function, because a summary redacted at write time is redacted
// forever, including for the person whose history it is.
func (s *Service) record(ctx context.Context, tx *sql.Tx, action, entityType, entityID, summary string, changes []audit.Change, meta map[string]any, sc Scope) error {
	owner := ""
	if sc.Private {
		owner = sc.OwnerID
	}
	return s.sink.Record(ctx, tx, audit.Event{
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		Summary:    summary,
		Meta:       meta,
		Changes:    changes,
		// Typed, not hand-stamped meta keys: the sink owns the spelling of the
		// redaction marker (audit.Event's field comment).
		Visibility: sc.Visibility(),
		OwnerID:    owner,
	})
}

// metaByAdmin stamps `by_admin` when an admin purges a PRIVATE item that is not
// theirs — v9's one asymmetry, and the only place in Home where somebody acts on
// something they may not read (D181). Nothing else sets it: an admin deleting a
// shared document is doing an ordinary admin thing, and marking that would dilute
// the flag until it meant nothing.
func metaByAdmin(ctx context.Context, base map[string]any, sc Scope) map[string]any {
	if !sc.Private || sc.OwnerID == reqctx.ActorID(ctx) || !isAdminCtx(ctx) {
		return base
	}
	if base == nil {
		base = map[string]any{}
	}
	base[audit.MetaByAdmin] = true
	return base
}

// freeSlug returns base, or base-2/base-3/… until free among the siblings of one
// parent IN ONE ROOT SCOPE, across BOTH tables (the cross-table check plus the two
// sibling indexes together are the addressing invariant, D32/D40).
//
// ⚠ This loop is why an un-scoped collision query is a SILENT leak rather than a
// 409 (D210). It does not surface a conflict — it walks around one — so if
// SiblingSlugTaken ever stops carrying the scope, the second member to upload a
// private "Smlouva" is quietly handed `smlouva-2`, which discloses a sibling they
// cannot see, and nothing reports an error.
//
// It also enforces D185: `soukrome` is RESERVED at the shared root, because the
// SPA routes /dokumenty/soukrome/… as a literal. A shared folder named "Soukromé"
// therefore takes `soukrome-2`.
//
// ⚠ The reservation marks the BARE CANDIDATE as taken; it does not rewrite `base`.
// Rewriting base to base+"-2" up front made the loop count from an already-suffixed
// stem, so a second "Soukromé" got `soukrome-2-2`, then `soukrome-2-3` — a ladder
// that appears nowhere else in the module. Feeding the reservation into the same
// taken/not-taken question the loop already asks gives the ordinary
// `soukrome-2`/`soukrome-3` sequence with no second rule.
func (s *Service) freeSlug(ctx context.Context, tx DBTX, parentID *string, sc Scope, base, excludeFolderID, excludeDocumentID string) (string, error) {
	if base == "" {
		base = idgen.Short()
	}
	reserved := isReservedRootSlug(base, parentID, sc)
	candidate := base
	for i := 2; ; i++ {
		// The reserved literal is only ever the bare base — `soukrome-2` is a fine
		// slug, it is `soukrome` alone that the SPA route would swallow.
		taken := reserved && candidate == base
		if !taken {
			var err error
			taken, err = s.store.SiblingSlugTaken(ctx, tx, parentID, sc, candidate, excludeFolderID, excludeDocumentID)
			if err != nil {
				return "", err
			}
		}
		if !taken {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}

// reservedRootSlug is the SPA's route literal for the private tree
// (/dokumenty/soukrome/…). A shared item at the root may not take it, or the route
// would be ambiguous between "the private tree" and "a folder called Soukromé"
// (D185). Only at the ROOT and only in the SHARED scope.
const reservedRootSlug = "soukrome"

func isReservedRootSlug(base string, parentID *string, sc Scope) bool {
	atRoot := parentID == nil || *parentID == ""
	return base == reservedRootSlug && atRoot && !sc.Private
}

// scopeOfDocument / scopeOfFolder read an item's own root scope off its stored
// columns — used wherever a mutation has already loaded the row.
func scopeOfDocument(d *Document) Scope { return callerScopeFor(d.Visibility, d.OwnerID) }
func scopeOfFolder(f *DocFolder) Scope  { return callerScopeFor(f.Visibility, f.OwnerID) }
func scopeOfStored(sd *storedDocument) Scope {
	return callerScopeFor(sd.Visibility, sd.OwnerID)
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
	d, err := s.store.GetDocument(ctx, s.db, id, reqctx.ActorID(ctx))
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
	var changed, private bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		// Viewer-scoped: another member's private document is simply not here (D180).
		before, err := s.store.GetDocument(ctx, tx, id, reqctx.ActorID(ctx))
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("document not found")
		}
		sc := scopeOfDocument(before)
		private = sc.Private
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
				sl, err := s.freeSlug(ctx, tx, before.FolderID, sc, slug.Make(title), "", before.ID)
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
			sl, err := s.freeSlug(ctx, tx, before.FolderID, sc, before.Slug, "", before.ID)
			if err != nil {
				return err
			}
			patch.Slug = &sl
		}
		if err := s.store.UpdateDocument(ctx, tx, id, patch); err != nil {
			return err
		}
		out, err = s.store.GetDocument(ctx, tx, id, reqctx.ActorID(ctx))
		if err != nil {
			return err
		}
		// Metadata diffs only — the bytes are immutable, so there is nothing about the
		// content to diff (D50).
		var changes []audit.Change
		audit.Diff(&changes, "title", audit.Ptr(before.Title), audit.Ptr(out.Title))
		audit.Diff(&changes, "slug", audit.Ptr(before.Slug), audit.Ptr(out.Slug))
		audit.Diff(&changes, "description", before.Description, out.Description)
		audit.Diff(&changes, "archived", audit.Ptr(fmt.Sprint(before.Archived)), audit.Ptr(fmt.Sprint(out.Archived)))
		if len(changes) == 0 {
			return nil
		}
		changed = true
		return s.record(ctx, tx, "document.update", "document", id,
			fmt.Sprintf("Upraven dokument „%s“", out.Title), changes, audit.WithVia(nil, via), sc)
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notifyScoped(ctx, "document.changed", id, private)
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
	var changed, private bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		// Viewer-scoped: another member's private document is simply not here (D180).
		before, err := s.store.GetDocument(ctx, tx, id, reqctx.ActorID(ctx))
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("document not found")
		}
		if err := assertLiveForMutation(before.Archived, "document not found"); err != nil {
			return err
		}
		sc := scopeOfDocument(before)
		private = sc.Private
		// nil requested: a move body carries no `scope` field, so the destination
		// folder's own scope comes back and a cross-scope target reaches the D186
		// refusal below — not assertFolder's create-body message about a field the
		// request doesn't have. An empty folder_id is this item's own root.
		dest := sc
		if in.FolderID != nil && *in.FolderID != "" {
			if dest, err = s.assertFolder(ctx, tx, in.FolderID, nil); err != nil {
				return err
			}
		}
		// ⚠ A move whose destination sits in the OTHER root scope is a 422, never a
		// silent publish (D186). Publishing is the only crossing, it is one-way, and
		// it is a different verb on purpose: an irreversible change of audience must
		// not be reachable by dragging a file into a folder.
		if dest != sc {
			return errCrossScopeMove(sc)
		}
		// Keep the current slug when it is free in the target parent, else re-derive.
		sl, err := s.freeSlug(ctx, tx, in.FolderID, sc, before.Slug, "", before.ID)
		if err != nil {
			return err
		}
		// A move that re-sends the current folder/position/slug changes nothing: skip
		// the write, the audit event, and the broadcast.
		if audit.EqualPtr(before.FolderID, in.FolderID) && before.Position == in.Position && before.Slug == sl {
			out = before
			return nil
		}
		if err := s.store.MoveDocumentRow(ctx, tx, id, in.FolderID, in.Position, sl); err != nil {
			return err
		}
		out, err = s.store.GetDocument(ctx, tx, id, reqctx.ActorID(ctx))
		if err != nil {
			return err
		}
		var changes []audit.Change
		audit.Diff(&changes, "folder_id", before.FolderID, out.FolderID)
		audit.Diff(&changes, "slug", audit.Ptr(before.Slug), audit.Ptr(out.Slug))
		audit.Diff(&changes, "position", audit.Ptr(before.Position), audit.Ptr(out.Position))
		changed = true
		return s.record(ctx, tx, "document.move", "document", id,
			fmt.Sprintf("Přesunut dokument „%s“", out.Title), changes, audit.WithVia(nil, via), sc)
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notifyScoped(ctx, "document.changed", id, private)
	}
	return s.documentDetail(ctx, s.db, out)
}

// DeleteDocument soft-deletes by default. hard=true (admin, enforced at the route)
// purges the row AND its object-storage objects.
//
// ⚠ THE TWO BRANCHES BELOW DELIBERATELY LOAD THE DOCUMENT DIFFERENTLY, and it
// reads like a bug unless you know why (D181). This is v9's ONE asymmetry:
//
//	read  — requires OWNERSHIP. Every read path 404s a foreign private document.
//	hard  — requires ADMIN, exactly as before v9, and nothing else.
//
// So an `admin` may permanently delete another member's private document — and
// reclaim its R2 objects — while every GET of that same id still 404s for them.
// Somebody has to be able to reclaim space and remove a departed member's files,
// and that power was never coupled to being able to read the content. Ownership,
// conversely, grants NO hard delete; that is unchanged from v4.
//
// Do not "simplify" these into one load: the admin branch cannot use the
// viewer-scoped read (it would 404 on exactly the rows it exists to purge), and
// the soft branch must not use the unscoped one (it would let any editor archive a
// document they cannot see).
func (s *Service) DeleteDocument(ctx context.Context, id string, hard bool) error {
	var changed, private bool
	var purge []string
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var before *storedDocument
		var err error
		if hard {
			// The handler has already refused a non-admin, so this branch is
			// admin-only. It reads across scopes ON PURPOSE — see the D181 note above.
			before, err = s.store.GetStoredDocumentAnyScope(ctx, tx, id)
		} else {
			before, err = s.store.GetStoredDocument(ctx, tx, id, reqctx.ActorID(ctx))
		}
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("document not found")
		}
		sc := scopeOfStored(before)
		private = sc.Private
		if hard {
			// Collect the keys INSIDE the transaction, while the row still exists; the
			// objects are deleted after commit (see below).
			purge = objectKeysOf(*before)
			if err := s.store.DeleteDocument(ctx, tx, id); err != nil {
				return err
			}
			changed = true
			// The audit event records the owner and, when an admin purged someone
			// else's private document, that an admin did it. It is the only trace of
			// the one power v9 grants across the privacy boundary.
			return s.record(ctx, tx, "document.delete", "document", id,
				fmt.Sprintf("Smazán dokument „%s“", before.Title), nil,
				metaByAdmin(ctx, audit.HardMeta(true), sc), sc)
		}
		// Soft delete is idempotent: archiving an already-archived document writes
		// nothing, emits no bogus false→true diff, and broadcasts nothing.
		if before.Archived {
			return nil
		}
		if err := s.store.UpdateDocument(ctx, tx, id, documentPatch{Archived: optional.Of(true)}); err != nil {
			return err
		}
		changed = true
		return s.record(ctx, tx, "document.delete", "document", id,
			fmt.Sprintf("Smazán dokument „%s“", before.Title),
			[]audit.Change{{Field: "archived", Old: audit.Ptr("false"), New: audit.Ptr("true")}}, audit.HardMeta(false), sc)
	})
	if err != nil {
		return err
	}
	// Objects are purged only AFTER the row is gone. If this fails the objects are
	// orphans, which the reconciliation pass sweeps — the reverse order would risk a
	// live row pointing at deleted bytes.
	s.purgeObjects(ctx, purge)
	if changed {
		s.notifyScoped(ctx, "document.changed", id, private)
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
	requested, err := ParseCreateScope(ctx, in.Scope)
	if err != nil {
		return nil, err
	}
	var out *DocFolder
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		sc, err := s.assertFolder(ctx, tx, in.ParentID, requested)
		if err != nil {
			return err
		}
		sl, err := s.freeSlug(ctx, tx, in.ParentID, sc, slug.Make(name), "", "")
		if err != nil {
			return err
		}
		pos, err := s.store.lastFolderPosition(ctx, tx, in.ParentID, sc)
		if err != nil {
			return err
		}
		icon := foldericon.Normalize(in.Icon)
		out, err = s.store.InsertFolder(ctx, tx, in.ParentID, name, sl, pos, reqctx.ActorID(ctx), icon, sc)
		if err != nil {
			return err
		}
		changes := []audit.Change{{Field: "name", New: audit.Ptr(name)}, {Field: "slug", New: audit.Ptr(sl)}}
		if icon != "" {
			changes = append(changes, audit.Change{Field: "icon", New: audit.Ptr(icon)})
		}
		return s.record(ctx, tx, "document_folder.create", "document_folder", out.ID,
			fmt.Sprintf("Vytvořena složka dokumentů „%s“", name), changes, nil, sc)
	})
	if err != nil {
		return nil, err
	}
	s.notifyScoped(ctx, "document_folder.changed", out.ID, scopeOfFolder(out).Private)
	return s.folderDetail(ctx, s.db, out)
}

func (s *Service) GetFolderDetail(ctx context.Context, id string) (*DocFolderDetail, error) {
	f, err := s.store.GetFolder(ctx, s.db, id, reqctx.ActorID(ctx))
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
	var changed, private bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetFolder(ctx, tx, id, reqctx.ActorID(ctx))
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("folder not found")
		}
		sc := scopeOfFolder(before)
		private = sc.Private
		archiving := in.Archived != nil && *in.Archived && !before.Archived
		unarchiving := in.Archived != nil && !*in.Archived && before.Archived
		// An archived (soft-deleted) folder is out of the tree and unreachable by slug;
		// restoring it is the only valid mutation. Reject renames so a stale client
		// cannot silently write — and audit — edits to a phantom (UpdateDocument draws
		// the same line for documents).
		if before.Archived && !unarchiving && (in.Name != nil || in.Icon != nil) {
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
			sl, err := s.freeSlug(ctx, tx, before.ParentID, sc, slug.Make(name), before.ID, "")
			if err != nil {
				return err
			}
			if err := s.store.RenameFolder(ctx, tx, id, name, sl); err != nil {
				return err
			}
		} else if unarchiving {
			sl, err := s.freeSlug(ctx, tx, before.ParentID, sc, before.Slug, before.ID, "")
			if err != nil {
				return err
			}
			if err := s.store.RenameFolder(ctx, tx, id, before.Name, sl); err != nil {
				return err
			}
		}
		if in.Icon != nil {
			if err := s.store.SetFolderIcon(ctx, tx, id, foldericon.Normalize(*in.Icon)); err != nil {
				return err
			}
		}
		if in.Archived != nil {
			if err := s.store.SetFolderArchived(ctx, tx, id, *in.Archived); err != nil {
				return err
			}
		}
		out, err = s.store.GetFolder(ctx, tx, id, reqctx.ActorID(ctx))
		if err != nil {
			return err
		}
		var changes []audit.Change
		audit.Diff(&changes, "name", audit.Ptr(before.Name), audit.Ptr(out.Name))
		audit.Diff(&changes, "slug", audit.Ptr(before.Slug), audit.Ptr(out.Slug))
		audit.Diff(&changes, "icon", audit.Ptr(before.Icon), audit.Ptr(out.Icon))
		audit.Diff(&changes, "archived", audit.Ptr(fmt.Sprint(before.Archived)), audit.Ptr(fmt.Sprint(out.Archived)))
		if len(changes) == 0 {
			return nil
		}
		changed = true
		return s.record(ctx, tx, "document_folder.update", "document_folder", id,
			fmt.Sprintf("Upravena složka dokumentů „%s“", out.Name), changes, nil, sc)
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notifyScoped(ctx, "document_folder.changed", id, private)
	}
	return s.folderDetail(ctx, s.db, out)
}

func (s *Service) MoveFolder(ctx context.Context, id string, in DocFolderMoveRequest) (*DocFolderDetail, error) {
	if strings.TrimSpace(in.Position) == "" {
		return nil, httpx.ErrUnprocessable("position is required")
	}
	var out *DocFolder
	var changed, private bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetFolder(ctx, tx, id, reqctx.ActorID(ctx))
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("folder not found")
		}
		sc := scopeOfFolder(before)
		private = sc.Private
		if err := assertLiveForMutation(before.Archived, "folder not found"); err != nil {
			return err
		}
		// nil requested — see MoveDocument: the destination's own scope must come
		// back so a crossing reaches the D186 refusal below.
		dest := sc
		if in.ParentID != nil && *in.ParentID != "" {
			if dest, err = s.assertFolder(ctx, tx, in.ParentID, nil); err != nil {
				return err
			}
		}
		// Cross-scope moves are 422, as for documents — publishing is the only
		// crossing and it is a different verb on purpose (D186).
		if dest != sc {
			return errCrossScopeMove(sc)
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
		sl, err := s.freeSlug(ctx, tx, in.ParentID, sc, before.Slug, before.ID, "")
		if err != nil {
			return err
		}
		if audit.EqualPtr(before.ParentID, in.ParentID) && before.Position == in.Position && before.Slug == sl {
			out = before
			return nil
		}
		// Exactly one row is written — descendants keep their slugs, and no object
		// storage is touched.
		if err := s.store.MoveFolderRow(ctx, tx, id, in.ParentID, in.Position, sl); err != nil {
			return err
		}
		out, err = s.store.GetFolder(ctx, tx, id, reqctx.ActorID(ctx))
		if err != nil {
			return err
		}
		var changes []audit.Change
		audit.Diff(&changes, "parent_id", before.ParentID, out.ParentID)
		audit.Diff(&changes, "slug", audit.Ptr(before.Slug), audit.Ptr(out.Slug))
		audit.Diff(&changes, "position", audit.Ptr(before.Position), audit.Ptr(out.Position))
		changed = true
		return s.record(ctx, tx, "document_folder.move", "document_folder", id,
			fmt.Sprintf("Přesunuta složka dokumentů „%s“", out.Name), changes, nil, sc)
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notifyScoped(ctx, "document_folder.changed", id, private)
	}
	return s.folderDetail(ctx, s.db, out)
}

// DeleteFolder soft-deletes by default. A non-empty folder needs cascade=true
// whichever mode is used (soft: the subtree is archived, each child logged);
// hard=true purges the rows AND every descendant document's objects (admin,
// enforced at the route).
//
// ⚠ Two-branch load, same as notes.DeleteFolder and DeleteDocument (D181): the
// hard branch must read across scopes — it is the route the purge screen leans
// on to reclaim a whole private subtree (D212) — and the soft branch must not,
// or any editor could archive a folder they cannot see.
func (s *Service) DeleteFolder(ctx context.Context, id string, cascade, hard bool) error {
	var changed, private bool
	var purge []string
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var before *DocFolder
		var err error
		if hard {
			// The handler has already refused a non-admin, so this branch is
			// admin-only. It reads across scopes ON PURPOSE — see the D181 note above.
			before, err = s.store.GetFolderAnyScope(ctx, tx, id)
		} else {
			before, err = s.store.GetFolder(ctx, tx, id, reqctx.ActorID(ctx))
		}
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("folder not found")
		}
		sc := scopeOfFolder(before)
		private = sc.Private
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
					metaByAdmin(ctx, map[string]any{"hard": true, "via": "cascade"}, sc), sc); err != nil {
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
					fmt.Sprintf("Smazána složka dokumentů „%s“", metas[fID].Name), nil, metaByAdmin(ctx, m, sc), sc); err != nil {
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
			if err := s.store.UpdateDocument(ctx, tx, d.ID, documentPatch{Archived: optional.Of(true)}); err != nil {
				return err
			}
			changed = true
			if err := s.record(ctx, tx, "document.delete", "document", d.ID,
				fmt.Sprintf("Smazán dokument „%s“ (kaskádou)", d.Title),
				[]audit.Change{{Field: "archived", Old: audit.Ptr("false"), New: audit.Ptr("true")}},
				map[string]any{"via": "cascade"}, sc); err != nil {
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
				[]audit.Change{{Field: "archived", Old: audit.Ptr("false"), New: audit.Ptr("true")}}, m, sc); err != nil {
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
		s.notifyScoped(ctx, "document_folder.changed", id, private)
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
		f, err := s.store.GetFolder(ctx, tx, *cur, reqctx.ActorID(ctx))
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
// v9: assertFolder also returns THE SCOPE THE FOLDER LIVES IN, which is the scope
// anything created under it must take.
//
// requested is the caller's `scope` field, honoured ONLY at the root: with a
// parent folder the PARENT'S scope governs, and a disagreement is a 422 rather
// than a silent correction — a folder whose contents are half private is exactly
// the model D177 rejected, so it must be impossible to build one by accident.
//
// ⚠ requested is a POINTER because "the caller said shared" and "the caller said
// nothing" are different questions here, and the zero Scope cannot tell them
// apart. nil defers to the parent; a non-nil value that disagrees with the parent
// is the 422. Passing Scope{} for "unspecified" is what made `scope:"shared"`
// into a private folder a silent correction rather than a refusal — the one
// direction of the check that never fired.
//
// A folder in another member's private tree reads back as nil (the store's viewer
// predicate saw to that), so it is reported as a nonexistent folder_id and never
// as a permission problem (D180).
func (s *Service) assertFolder(ctx context.Context, q DBTX, folderID *string, requested *Scope) (Scope, error) {
	if folderID == nil || *folderID == "" {
		root := Scope{}
		if requested != nil {
			root = *requested
		}
		return root, assertPairing(root)
	}
	f, err := s.store.GetFolder(ctx, q, *folderID, reqctx.ActorID(ctx))
	if err != nil {
		return Scope{}, err
	}
	if f == nil || f.Archived {
		return Scope{}, httpx.ErrUnprocessable("folder_id does not reference an existing folder")
	}
	parent := scopeOfFolder(f)
	if requested != nil && *requested != parent {
		return Scope{}, httpx.ErrUnprocessable(
			"scope disagrees with the parent folder — an item takes the scope of the folder it is filed in")
	}
	return parent, nil
}

// assertParentLive rejects restoring an item under a missing or archived parent:
// every ancestor of a live item must be live, since Resolve walks only live folders.
// nil = root.
func (s *Service) assertParentLive(ctx context.Context, q DBTX, parentID *string) error {
	if parentID == nil {
		return nil
	}
	f, err := s.store.GetFolder(ctx, q, *parentID, reqctx.ActorID(ctx))
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
//
// v9: returns ONE root scope, never both (leak table row 1). A response carrying
// the shared tree and a private one together would be a response the frontend has
// to filter, and the frontend is the wrong place for that.
func (s *Service) Tree(ctx context.Context, includeArchived bool, sc Scope) (*DocumentsTree, error) {
	folders, err := s.store.AllFolders(ctx, includeArchived, sc)
	if err != nil {
		return nil, err
	}
	summaries, err := s.store.AllDocumentSummaries(ctx, includeArchived, sc)
	if err != nil {
		return nil, err
	}
	hh, pers, err := s.store.PinSets(ctx, reqctx.ActorID(ctx))
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
//
// ⚠ v9: scoped to ONE root (D184), and the predicate lives in the SQL — for both
// branches. A post-filter would leak through short pages and cursor behaviour even
// with every offending row removed.
func (s *Service) List(ctx context.Context, q string, folderID *string, includeArchived bool, limit int, cursor string, sc Scope) (DocumentPage, error) {
	q = strings.TrimSpace(q)
	var items []DocumentSummary
	var next *string

	if q != "" {
		match := appdb.FTSQuery(q)
		if match == "" {
			// A punctuation-only query has no searchable tokens. Return empty rather than
			// run `documents_fts MATCH ''`, whose empty-phrase behaviour is unspecified.
			return DocumentPage{Items: []DocumentSummary{}}, nil
		}
		var err error
		items, err = s.store.SearchDocuments(ctx, match, folderID, includeArchived, searchLimit, sc)
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
		items, err = s.store.ListDocuments(ctx, folderID, includeArchived, limit, cursorTS, cursorID, sc)
		if err != nil {
			return DocumentPage{}, err
		}
		// A full page means there may be more; the cursor is the composite key of the
		// last row so the next page resumes exactly after it.
		if len(items) == limit {
			last := items[len(items)-1]
			c := encodeCursor(last.UpdatedAt, last.ID)
			next = &c
		}
	}

	hh, pers, err := s.store.PinSets(ctx, reqctx.ActorID(ctx))
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
//
// v9: a slug path is MEANINGLESS without a scope (leak table row 3) — the same
// path names a different item in the shared tree and in each private one — so the
// walk starts from the named root and never leaves it.
func (s *Service) Resolve(ctx context.Context, path string, sc Scope) (*DocResolveResult, error) {
	segs := slugpath.Split(path)
	if len(segs) == 0 {
		return nil, httpx.ErrNotFound("empty path")
	}
	var parent *string
	for i, seg := range segs {
		last := i == len(segs)-1
		f, err := s.store.ChildFolderBySlug(ctx, s.db, parent, seg, sc)
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
		d, err := s.store.ChildDocumentBySlug(ctx, s.db, parent, seg, sc)
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

// ---- Publish: private → shared (v9, D182) ----

// PublishDocument moves one private document into the shared tree.
//
// ⚠ A NON-OWNER GETS 404, NOT 403 — AN ADMIN INCLUDED (D206), byte-identical to
// the response for an id that was never issued. This route is RequireWrite, so
// every editor can call it; a 403-for-private / 404-for-unknown pair would answer
// "does this id exist, and is it private?" for any id a caller cares to try —
// the permalink oracle D180 closes, reopened with a different verb. It was
// specified as 403 in five documents before a review pass caught it. Yes, this
// leaves an admin with less power than the role implies (publishing something you
// cannot read is not a power that should exist), but it must not be OBSERVABLE.
//
// ⚠ THE PERMANENT URL DOES NOT CHANGE. The R2 keys are id-based and independent of
// folder, slug and scope (D42), so /d/{id}, /raw, /download, /preview and
// /thumbnail all keep working across a publish — which is the entire reason that
// URL was specified as permanent. No object is copied, moved or rewritten. Only
// the slug path moves.
//
// The personal pin survives too (D183): it is keyed on the item id.
//
// There is no unpublish, and the absence is a decision (D182): a document the
// household has relied on for six months must not be able to vanish into one
// member's tree. Re-uploading privately and deleting the shared copy leaves both
// facts in the log, which is the honest version of the same wish.
func (s *Service) PublishDocument(ctx context.Context, id string, in PublishRequest) (*DocumentDetail, error) {
	var out *Document
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetDocument(ctx, tx, id, reqctx.ActorID(ctx))
		if err != nil {
			return err
		}
		// Not found, not mine, or archived — one answer (D206).
		if before == nil || before.Archived {
			return errPublishNotFound
		}
		if before.Visibility != visibilityPrivate {
			return httpx.ErrUnprocessable("dokument už je sdílený")
		}
		// Restated for the future: the viewer-scoped load already refuses a foreign
		// private document, so this cannot trigger today. It guards a later change
		// that swaps the load for an unscoped one.
		if deref(before.OwnerID) != reqctx.ActorID(ctx) {
			return errPublishNotFound
		}
		dest, err := s.assertFolder(ctx, tx, in.FolderID, nil)
		if err != nil {
			return err
		}
		if dest.Private {
			return httpx.ErrUnprocessable("cílová složka musí být ve sdíleném stromu")
		}
		sl, err := s.freeSlug(ctx, tx, in.FolderID, dest, before.Slug, "", before.ID)
		if err != nil {
			return err
		}
		pos := strings.TrimSpace(in.Position)
		if pos == "" {
			if pos, err = s.store.lastDocumentPosition(ctx, tx, in.FolderID, dest); err != nil {
				return err
			}
		}
		if err := s.store.PublishDocumentRow(ctx, tx, id, in.FolderID, pos, sl); err != nil {
			return err
		}
		out, err = s.store.GetDocument(ctx, tx, id, reqctx.ActorID(ctx))
		if err != nil {
			return err
		}
		changes := []audit.Change{
			{Field: "visibility", Old: audit.Ptr(visibilityPrivate), New: audit.Ptr(visibilityShared)},
			{Field: "owner_id", Old: before.OwnerID, New: nil},
		}
		audit.Diff(&changes, "folder_id", before.FolderID, out.FolderID)
		audit.Diff(&changes, "slug", audit.Ptr(before.Slug), audit.Ptr(out.Slug))
		// Audited against the item's NEW scope (shared): the event describes a
		// document the household can now see, so there is nothing left to redact —
		// and redacting the one event that says "this became visible to everyone"
		// would hide the most consequential thing that can happen to a private item.
		return s.record(ctx, tx, "document.publish", "document", id,
			fmt.Sprintf("Publikován dokument „%s“ do sdílených", out.Title), changes,
			map[string]any{"published_from_owner": deref(before.OwnerID)}, Scope{})
	})
	if err != nil {
		return nil, err
	}
	s.notify(ctx, "document.changed", map[string]string{"id": id})
	return s.documentDetail(ctx, s.db, out)
}

// PublishFolder publishes a private folder AND EVERY DESCENDANT in one
// transaction. A partial publish — half a folder visible to the household — is the
// one outcome this endpoint must never produce. No R2 object is touched.
func (s *Service) PublishFolder(ctx context.Context, id string, in PublishRequest) (*DocFolderDetail, error) {
	var out *DocFolder
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetFolder(ctx, tx, id, reqctx.ActorID(ctx))
		if err != nil {
			return err
		}
		if before == nil || before.Archived {
			return errPublishFolderNotFound
		}
		if before.Visibility != visibilityPrivate {
			return httpx.ErrUnprocessable("složka už je sdílená")
		}
		if deref(before.OwnerID) != reqctx.ActorID(ctx) {
			return errPublishFolderNotFound
		}
		dest, err := s.assertFolder(ctx, tx, in.FolderID, nil)
		if err != nil {
			return err
		}
		if dest.Private {
			return httpx.ErrUnprocessable("cílová složka musí být ve sdíleném stromu")
		}
		// ⚠ NO CYCLE GUARD HERE, and its absence is the point rather than an omission.
		// A move needs one (D31); a publish cannot: the refusal above has already
		// established that the destination is SHARED, while this folder is private and
		// a subtree never straddles two scopes (see DescendantFolderIDs), so the
		// destination can never be inside the subtree being published. The guard that
		// stood here always returned false, and paid for it with one viewer-scoped
		// GetFolder per ancestor level inside the irreversible path's transaction.
		//
		// Restore it the day a subtree CAN straddle scopes — which is the same day
		// DescendantFolderIDs' own comment says to revisit that walk.
		//
		// Enumerate the subtree BEFORE the root moves, so the walk still sees the
		// private tree it belongs to. Includes id itself.
		folderIDs, err := s.store.DescendantFolderIDs(ctx, tx, id, true)
		if err != nil {
			return err
		}
		// ⚠ AND ITS CONTENTS, because EVERY ROW THE CASCADE PUBLISHES IS AUDITED
		// SEPARATELY — the same rule the cascade delete follows, applied to the one
		// cascade that cannot be undone (D182). Without it a subtree becomes
		// household-visible behind a single log line naming only its parent: a rule
		// on `documents.document.publish` never fires for any of the documents, and
		// each one's entity timeline is silent about the most consequential change
		// it can undergo. Archived descendants are included because
		// PublishDescendants publishes them too.
		childDocs, err := s.store.DocumentsInFolders(ctx, tx, folderIDs, true)
		if err != nil {
			return err
		}
		metas, err := s.store.FolderMetaByIDs(ctx, tx, folderIDs)
		if err != nil {
			return err
		}
		sl, err := s.freeSlug(ctx, tx, in.FolderID, dest, before.Slug, before.ID, "")
		if err != nil {
			return err
		}
		pos := strings.TrimSpace(in.Position)
		if pos == "" {
			if pos, err = s.store.lastFolderPosition(ctx, tx, in.FolderID, dest); err != nil {
				return err
			}
		}
		if err := s.store.PublishFolderRow(ctx, tx, id, in.FolderID, pos, sl); err != nil {
			return err
		}
		// Descendants keep their slugs: their siblings are the same siblings. Only
		// the subtree's ROOT lands among strangers, which is why only it re-derives.
		if err := s.store.PublishDescendants(ctx, tx, folderIDs); err != nil {
			return err
		}
		out, err = s.store.GetFolder(ctx, tx, id, reqctx.ActorID(ctx))
		if err != nil {
			return err
		}
		// The cascade's own events, children first and the target folder last — the
		// order the cascade delete uses, so the two read alike. All audited against
		// the NEW scope (shared): nothing is left to redact once the household can
		// see it.
		owner := deref(before.OwnerID)
		for _, d := range childDocs {
			if err := s.record(ctx, tx, "document.publish", "document", d.ID,
				fmt.Sprintf("Publikován dokument „%s“ do sdílených (kaskádou)", d.Title),
				publishChanges(before.OwnerID), publishMeta(owner, true), Scope{}); err != nil {
				return err
			}
		}
		for _, fID := range folderIDs {
			if fID == id {
				continue // the target folder records below, without the cascade via
			}
			if err := s.record(ctx, tx, "document_folder.publish", "document_folder", fID,
				fmt.Sprintf("Publikována složka dokumentů „%s“ do sdílených (kaskádou)", metas[fID].Name),
				publishChanges(before.OwnerID), publishMeta(owner, true), Scope{}); err != nil {
				return err
			}
		}
		changes := publishChanges(before.OwnerID)
		audit.Diff(&changes, "parent_id", before.ParentID, out.ParentID)
		audit.Diff(&changes, "slug", audit.Ptr(before.Slug), audit.Ptr(out.Slug))
		meta := publishMeta(owner, false)
		meta["folders"] = len(folderIDs)
		meta["documents"] = len(childDocs)
		return s.record(ctx, tx, "document_folder.publish", "document_folder", id,
			fmt.Sprintf("Publikována složka dokumentů „%s“ do sdílených", out.Name), changes, meta, Scope{})
	})
	if err != nil {
		return nil, err
	}
	s.notify(ctx, "document_folder.changed", map[string]string{"id": id})
	return s.folderDetail(ctx, s.db, out)
}

// publishChanges is the field diff every publish event carries: the two columns
// that define the root scope, moving in the only direction they ever move. Shared
// by the document, the folder and the whole cascade so the log has one shape.
func publishChanges(oldOwner *string) []audit.Change {
	return []audit.Change{
		{Field: "visibility", Old: audit.Ptr(visibilityPrivate), New: audit.Ptr(visibilityShared)},
		{Field: "owner_id", Old: oldOwner, New: nil},
	}
}

// publishMeta stamps who it belonged to, and whether this row is the one the
// caller asked for or something the folder cascade swept up with it.
func publishMeta(owner string, viaCascade bool) map[string]any {
	m := map[string]any{"published_from_owner": owner}
	if viaCascade {
		m["via"] = "cascade"
	}
	return m
}

// errPublishNotFound is the single refusal every publish failure that is not a
// validation problem maps to. Deliberately ONE value per route: an unknown id, a
// foreign private item and an admin's attempt must be indistinguishable, and the
// surest way to keep them so is to make them literally the same error (D206).
//
// ⚠ D206 asks for one message per ROUTE, not one across entity types — the folder
// route gets its own, because "document not found" for a folder id buys no
// indistinguishability and misnames what was not found.
var (
	errPublishNotFound       = httpx.ErrNotFound("document not found")
	errPublishFolderNotFound = httpx.ErrNotFound("folder not found")
)

// ---- Pinning (two scopes, D47) ----

// Pin adds a pin. A household pin ("pro všechny") is shared state: editor/admin,
// audited, broadcast. A personal pin ("jen pro mě") is a per-user view preference:
// allowed for any member INCLUDING a reader, not audited, not broadcast. That
// reader exception is deliberately narrow — it is the only documents write a reader
// may make.
//
// v9 adds one rule: a HOUSEHOLD pin on a PRIVATE document is a 422 (D183). Note
// the status — 422, not 403. The caller has the role; the operation is
// meaningless, because a household pin means "put this on everyone's Nástěnka" and
// nobody else can open it. A personal pin by a non-owner never reaches the check:
// the document 404s first.
func (s *Service) Pin(ctx context.Context, documentID, scopeStr, via string) (*PinState, error) {
	scope, err := parsePinScope(scopeStr)
	if err != nil {
		return nil, err
	}
	d, err := s.store.GetDocument(ctx, s.db, documentID, reqctx.ActorID(ctx))
	if err != nil {
		return nil, err
	}
	if d == nil || d.Archived {
		return nil, httpx.ErrNotFound("document not found")
	}
	if scope == scopeHousehold && d.Visibility == visibilityPrivate {
		return nil, httpx.ErrUnprocessable(
			"soukromý dokument nelze připnout pro všechny — ostatní ho nevidí")
	}
	uid := reqctx.ActorID(ctx)

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

	if !reqctx.CanWrite(ctx) {
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
				audit.WithVia(map[string]any{"scope": scopeHousehold}, via), scopeOfDocument(d))
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
	scope, err := parsePinScope(scopeStr)
	if err != nil {
		return nil, err
	}
	d, err := s.store.GetDocument(ctx, s.db, documentID, reqctx.ActorID(ctx))
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, httpx.ErrNotFound("document not found")
	}
	uid := reqctx.ActorID(ctx)

	if scope == scopePersonal {
		if _, err := s.store.DeletePin(ctx, s.db, documentID, scopePersonal, &uid); err != nil {
			return nil, err
		}
		return s.pinState(ctx, documentID, uid)
	}

	if !reqctx.CanWrite(ctx) {
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
				audit.WithVia(map[string]any{"scope": scopeHousehold}, via), scopeOfDocument(d))
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
	st, err := s.store.GetPinState(ctx, q, d.ID, reqctx.ActorID(ctx))
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
	sc := scopeOfFolder(f)
	subs, err := s.store.ChildFolders(ctx, q, &f.ID, false, sc)
	if err != nil {
		return nil, err
	}
	docs, err := s.store.DocumentSummariesInFolder(ctx, q, &f.ID, false, sc)
	if err != nil {
		return nil, err
	}
	hh, pers, err := s.store.PinSets(ctx, reqctx.ActorID(ctx))
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

// ancestors returns the folder chain root→…→folderID (inclusive). Empty (but never
// nil, so it marshals as `[]` not `null` — a root document's path is an empty array,
// and clients read path.length) for a root item (folderID nil).
func (s *Service) ancestors(ctx context.Context, q DBTX, folderID *string) ([]PathSegment, error) {
	chain := []PathSegment{}
	cur := folderID
	for depth := 0; cur != nil && depth < 1000; depth++ {
		f, err := s.store.GetFolder(ctx, q, *cur, reqctx.ActorID(ctx))
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

// parsePinScope parses the PIN scope ("household" | "personal"), a different axis
// from the v9 root scope that ParseScope in scope.go handles. Renamed in v9 so the
// two cannot be confused at a call site.
func parsePinScope(s string) (string, error) {
	switch s {
	case scopeHousehold, scopePersonal:
		return s, nil
	}
	return "", httpx.ErrUnprocessable("scope must be household or personal")
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

// encodeCursor packs the `(updated_at, id)` pair the ORDER BY pages on. `updated_at`
// alone is not unique, so both columns travel; splitCursor is the other half.
func encodeCursor(updatedAt, id string) string { return cursor.Encode(updatedAt, id) }

// splitCursor parses the composite `(updated_at, id)` list cursor. An unparseable
// cursor is treated as "start from the beginning" rather than an error — a stale
// bookmark should show the first page, not a 422. The empty-part check is this
// module's own: platform/cursor reports only that a token was minted with the right
// arity, and paging against "" is not what a caller meant by either column.
func splitCursor(c string) (ts, id string) {
	parts, ok := cursor.Decode(strings.TrimSpace(c), 2)
	if !ok || parts[0] == "" || parts[1] == "" {
		return "", ""
	}
	return parts[0], parts[1]
}
