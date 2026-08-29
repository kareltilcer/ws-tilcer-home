package notes

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/foldericon"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/slug"
)

// Notifier publishes a websocket change after commit (mirrors the events module).
type Notifier func(ctx context.Context, typ string, payload any)

// Service orchestrates notes/folders mutations (WithTx + audit-in-tx + notify),
// slug derivation/uniqueness, the path→id resolver, and pinning. The one
// exception to the "every mutation is audited" rule is a personal pin — a per-user
// view preference (D35) — which is written directly, without audit or broadcast.
type Service struct {
	db      *sql.DB
	store   *Store
	sink    audit.Sink
	notify  Notifier
	blob    blobstore.BlobStore // object storage for inline images; nil disables image upload
	imgOpts ImageOptions
	logger  *slog.Logger

	// unrefSince, guarded by unrefMu, is when edit-time GC first saw each image
	// unreferenced — the undo window's clock (see unreferencedDue). Deliberately
	// in-memory and best-effort: forgetting it on restart only costs an image one
	// window, and the reconciliation pass reclaims whatever edit-time GC skips.
	unrefMu    sync.Mutex
	unrefSince map[string]time.Time
}

// ImageOptions configures inline-image uploads (note-images/{id}).
type ImageOptions struct {
	// MaxUploadBytes is the hard per-image cap; over it the upload is rejected 413.
	MaxUploadBytes int64
	// TempDir buffers the streamed upload so size+checksum are known before the
	// single Put (S3 wants a known length). "" = the OS default temp directory.
	TempDir string
	// GCGrace spares an image from edit-time garbage collection until it is at least
	// this old. An upload's POST and the body PATCH that embeds its reference are
	// separate requests, so a body save that races ahead of the reference insertion
	// would otherwise see the just-uploaded image as "owned but unreferenced" and
	// delete it out from under the editor. It doubles as the UNDO window: an image
	// whose reference an edit removed is reclaimed only once it has stayed
	// unreferenced this long (see unreferencedDue). Zero = GC immediately (used in
	// tests); the reconciliation pass is the long-term backstop either way.
	GCGrace time.Duration
}

// defaultImageMaxBytes caps a pasted image when config leaves MaxUploadBytes unset.
const defaultImageMaxBytes = 10 << 20 // 10 MiB

func NewService(db *sql.DB, sink audit.Sink, notify Notifier, blob blobstore.BlobStore, imgOpts ImageOptions, logger *slog.Logger) *Service {
	if notify == nil {
		notify = func(context.Context, string, any) {}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if imgOpts.MaxUploadBytes <= 0 {
		imgOpts.MaxUploadBytes = defaultImageMaxBytes
	}
	return &Service{
		db: db, store: NewStore(db), sink: sink, notify: notify, blob: blob,
		imgOpts: imgOpts, logger: logger, unrefSince: map[string]time.Time{},
	}
}

// Store exposes the read store (used by the widget provider).
func (s *Service) Store() *Store { return s.store }

// notifyScoped publishes a live-change signal, WITHOUT the item id when the item
// is private (v9, D180/D187).
//
// ⚠ ws.Hub.Publish fans out to EVERY connected client, not to an audience — so a
// raw id in the payload of a private mutation is a real-time existence-and-activity
// oracle over another member's tree, handed to every browser in the household. It
// is the same disclosure audit.Redact blanks EntityID to prevent ("an id in a log
// row is an id that can be fed back into the entity timeline"), and the id is a
// usable capability rather than mere metadata.
//
// The TYPE still goes out, because that is what the household is meant to be able
// to learn (D187: that something private happened is not the secret) — and it is
// all the client needs: api/ws.ts's classify() switches on the type and
// invalidates by module prefix, never reading the payload id.
//
// ⚠ THE `private` MARKER IS WHAT KEEPS THE TOAST HONEST. The frame still fans out,
// because the OWNER's other tabs and devices have to refetch — but for everybody
// else it announces a change they will never be able to see, and api/ws.ts turned
// that into "Poznámky byly změněny jinde" on their screen. The marker lets the
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

// record writes one audit event in the caller's transaction.
//
// ⚠ v9 made the Scope a REQUIRED parameter rather than something a caller can add
// to `meta` when they remember (leak table row 11). Every notes.* event now
// carries `meta.visibility`, and a private one also carries `meta.owner_id`; those
// two keys are the ONLY thing that lets a read path tell a private event apart, so
// an event written without them is an event that can never be redacted. Making it
// a parameter means the compiler asks the question at every one of the ~20 call
// sites instead of a reviewer having to.
//
// The summary and the field diffs are written IN FULL, deliberately (D187).
// Redaction happens at READ time, in exactly one function, because a summary
// redacted at write time is redacted forever — including for the person it belongs
// to, whose own history it is.
func (s *Service) record(ctx context.Context, tx *sql.Tx, action, entityType, entityID, summary string, changes []audit.Change, meta map[string]any, sc Scope) error {
	owner := ""
	if sc.Private {
		owner = sc.OwnerID
	}
	_, err := s.sink.Record(ctx, tx, audit.Event{
		Module:     audit.ModuleNotes,
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

// freeSlug returns base, or base-2/base-3/… until free among the siblings of one
// parent IN ONE ROOT SCOPE, across BOTH tables (the cross-table check plus the two
// sibling indexes together are the addressing invariant, D32).
//
// ⚠ This loop is why an un-scoped collision query is a SILENT leak rather than a
// 409 (D210). It does not surface a conflict — it walks around one. So if
// SiblingSlugTaken ever stops carrying the scope, the second member to create a
// private note called "Recepty" is quietly handed `recepty-2`, which discloses a
// sibling they cannot see, and nothing anywhere reports an error.
//
// It also enforces D185 in passing: `soukrome` is a RESERVED slug at both shared
// roots, because the SPA routes /poznamky/soukrome/… as a literal. A shared folder
// named "Soukromé" therefore takes `soukrome-2`.
//
// ⚠ The reservation marks the BARE CANDIDATE as taken; it does not rewrite `base`.
// Rewriting base to base+"-2" up front made the loop count from an already-suffixed
// stem, so a second "Soukromé" got `soukrome-2-2`, then `soukrome-2-3` — a ladder
// that appears nowhere else in the module. Feeding the reservation into the same
// taken/not-taken question the loop already asks gives the ordinary
// `soukrome-2`/`soukrome-3` sequence with no second rule. Mirrors documents.
func (s *Service) freeSlug(ctx context.Context, tx DBTX, parentID *string, sc Scope, base, excludeFolderID, excludeNoteID string) (string, error) {
	if base == "" {
		base = shortID()
	}
	reserved := isReservedRootSlug(base, parentID, sc)
	candidate := base
	for i := 2; ; i++ {
		// The reserved literal is only ever the bare base — `soukrome-2` is a fine
		// slug, it is `soukrome` alone that the SPA route would swallow.
		taken := reserved && candidate == base
		if !taken {
			var err error
			taken, err = s.store.SiblingSlugTaken(ctx, tx, parentID, sc, candidate, excludeFolderID, excludeNoteID)
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
// (/poznamky/soukrome/…). A shared item at the root may not take it, or the route
// would be ambiguous between "the private tree" and "a folder called Soukromé"
// (D185). Only at the ROOT and only in the SHARED scope: deeper paths are
// unambiguous, and the private tree's own root is behind the literal already.
const reservedRootSlug = "soukrome"

func isReservedRootSlug(base string, parentID *string, sc Scope) bool {
	atRoot := parentID == nil || *parentID == ""
	return base == reservedRootSlug && atRoot && !sc.Private
}

// ---- Notes ----

func (s *Service) CreateNote(ctx context.Context, in NoteCreate) (*NoteDetail, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, httpx.ErrUnprocessable("title is required")
	}
	if hasInlineImageData(in.BodyMD) {
		return nil, errInlineImageData
	}
	requested, err := ParseCreateScope(ctx, in.Scope)
	if err != nil {
		return nil, err
	}
	var out *Note
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		sc, err := s.assertFolder(ctx, tx, in.FolderID, requested)
		if err != nil {
			return err
		}
		sl, err := s.freeSlug(ctx, tx, in.FolderID, sc, slug.Make(title), "", "")
		if err != nil {
			return err
		}
		pos, err := s.store.lastNotePosition(ctx, tx, in.FolderID, sc)
		if err != nil {
			return err
		}
		out, err = s.store.InsertNote(ctx, tx, in.FolderID, title, sl, in.BodyMD, pos, actorID(ctx), sc)
		if err != nil {
			return err
		}
		changes := []audit.Change{{Field: "title", New: audit.Ptr(title)}, {Field: "slug", New: audit.Ptr(sl)}}
		if in.BodyMD != "" {
			changes = append(changes, audit.Change{Field: "body_md", New: audit.Ptr(in.BodyMD)})
		}
		return s.record(ctx, tx, "note.create", "note", out.ID,
			fmt.Sprintf("Vytvořena poznámka „%s“", title), changes, nil, sc)
	})
	if err != nil {
		return nil, err
	}
	s.notifyScoped(ctx, "note.changed", out.ID, scopeOfNote(out).Private)
	return s.noteDetail(ctx, s.db, out)
}

// GetNoteDetail is the by-id read. Another member's private note is NOT FOUND —
// never forbidden (D180). A 403 would confirm the id exists, which is all an
// existence oracle over the private tree needs.
func (s *Service) GetNoteDetail(ctx context.Context, id string) (*NoteDetail, error) {
	n, err := s.store.GetNote(ctx, s.db, id, actorID(ctx))
	if err != nil {
		return nil, err
	}
	if n == nil {
		return nil, httpx.ErrNotFound("note not found")
	}
	return s.noteDetail(ctx, s.db, n)
}

func (s *Service) UpdateNote(ctx context.Context, id string, in NoteUpdate, via string) (*NoteDetail, error) {
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		return nil, httpx.ErrUnprocessable("title cannot be empty")
	}
	if in.BodyMD != nil && hasInlineImageData(*in.BodyMD) {
		return nil, errInlineImageData
	}
	var out *Note
	var changed, private bool
	var bodyChanged bool
	var oldBody string // captured for image GC: refs present before this edit but not after
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		// Viewer-scoped: another member's private note is simply not here (D180).
		before, err := s.store.GetNote(ctx, tx, id, actorID(ctx))
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("note not found")
		}
		sc := scopeOfNote(before)
		private = sc.Private
		var patch notePatch
		unarchiving := in.Archived != nil && !*in.Archived && before.Archived
		// A soft-deleted (archived) note is hidden from the tree and unreachable by
		// slug; unarchiving is its only valid mutation. Reject title/body edits so a
		// stale client — e.g. autosave still running on a note whose folder was
		// cascade-deleted — can't silently write (and audit) edits to a phantom note.
		if before.Archived && !unarchiving && (in.Title != nil || in.BodyMD != nil) {
			return httpx.ErrNotFound("note not found")
		}
		if unarchiving {
			// Restoring a note: its parent must still be live — a live note may not sit
			// under an archived ancestor (Resolve walks only live folders).
			if err := s.assertParentLive(ctx, tx, before.FolderID); err != nil {
				return err
			}
		}
		// Only carry a field into the patch when its value actually changes. A PATCH
		// that re-sends an unchanged value (e.g. autosave whose body_md equals what's
		// stored) must leave the row — and updated_at — untouched: a bumped updated_at
		// with no field diff emits no audit/broadcast (both gate on the diff below), so
		// another open editor would later see a drifted timestamp and raise a bogus
		// "changed elsewhere" banner for an edit that never happened.
		if in.Archived != nil && *in.Archived != before.Archived {
			patch.Archived = in.Archived
		}
		if in.BodyMD != nil && *in.BodyMD != deref(before.BodyMD) {
			patch.Body = in.BodyMD
			bodyChanged = true
			oldBody = deref(before.BodyMD)
		}
		titleChanged := false
		if in.Title != nil {
			if title := strings.TrimSpace(*in.Title); title != before.Title {
				titleChanged = true
				patch.Title = &title
				// re-derive the slug from the new title, unique in the current parent
				sl, err := s.freeSlug(ctx, tx, before.FolderID, sc, slug.Make(title), "", before.ID)
				if err != nil {
					return err
				}
				patch.Slug = &sl
			}
		}
		if !titleChanged && unarchiving {
			// Its slug left the live sibling scope while archived and a sibling may
			// have reused it — re-free it (keeps the current slug when still free) so
			// re-entering the partial unique index doesn't fail (would otherwise 500).
			sl, err := s.freeSlug(ctx, tx, before.FolderID, sc, before.Slug, "", before.ID)
			if err != nil {
				return err
			}
			patch.Slug = &sl
		}
		if err := s.store.UpdateNote(ctx, tx, id, patch); err != nil {
			return err
		}
		out, err = s.store.GetNote(ctx, tx, id, actorID(ctx))
		if err != nil {
			return err
		}
		var changes []audit.Change
		audit.Diff(&changes, "title", audit.Ptr(before.Title), audit.Ptr(out.Title))
		audit.Diff(&changes, "slug", audit.Ptr(before.Slug), audit.Ptr(out.Slug))
		audit.Diff(&changes, "body_md", before.BodyMD, out.BodyMD) // full, untruncated (D36)
		audit.Diff(&changes, "archived", audit.Ptr(fmt.Sprint(before.Archived)), audit.Ptr(fmt.Sprint(out.Archived)))
		// A no-op PATCH leaves no audit trail and doesn't broadcast. The patch above
		// carries only genuinely-changed fields, so an empty diff means the store wrote
		// nothing and updated_at didn't move — there's nothing to tell other clients.
		if len(changes) == 0 {
			return nil
		}
		changed = true
		return s.record(ctx, tx, "note.update", "note", id,
			fmt.Sprintf("Upravena poznámka „%s“", out.Title), changes, metaVia(nil, via), sc)
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notifyScoped(ctx, "note.changed", id, private)
	}
	// GC after the commit: an edit that removed an `![](/api/notes/images/{id})`
	// reference orphans that image. Best-effort — never blocks or fails the save.
	if bodyChanged {
		s.gcNoteImages(ctx, id, oldBody, deref(out.BodyMD))
	}
	return s.noteDetail(ctx, s.db, out)
}

func (s *Service) MoveNote(ctx context.Context, id string, in NoteMoveRequest, via string) (*NoteDetail, error) {
	if strings.TrimSpace(in.Position) == "" {
		return nil, httpx.ErrUnprocessable("position is required")
	}
	var out *Note
	var changed, private bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetNote(ctx, tx, id, actorID(ctx))
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("note not found")
		}
		// Reject moving an archived (soft-deleted) note — see assertLiveForMutation.
		if err := assertLiveForMutation(before.Archived, "note not found"); err != nil {
			return err
		}
		sc := scopeOfNote(before)
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
		// silent publish (D186). Publishing is the only crossing there is, it is
		// one-way, and it is a different verb on purpose: an irreversible change of
		// audience must not be reachable by dragging something into a folder.
		if dest != sc {
			return errCrossScopeMove(sc)
		}
		// keep the current slug if it's free in the target parent, else re-derive
		sl, err := s.freeSlug(ctx, tx, in.FolderID, sc, before.Slug, "", before.ID)
		if err != nil {
			return err
		}
		// A move that re-sends the note's current folder/position/slug changes nothing:
		// skip the write (so updated_at doesn't drift), the audit event, and the
		// broadcast — mirroring UpdateNote's no-op gating. Otherwise a re-sent move
		// would log a contentless note.move and make every other client refetch and
		// raise a bogus "changed elsewhere" toast for a change that never happened.
		if audit.EqualPtr(before.FolderID, in.FolderID) && before.Position == in.Position && before.Slug == sl {
			out = before
			return nil
		}
		if err := s.store.MoveNoteRow(ctx, tx, id, in.FolderID, in.Position, sl); err != nil {
			return err
		}
		out, err = s.store.GetNote(ctx, tx, id, actorID(ctx))
		if err != nil {
			return err
		}
		var changes []audit.Change
		audit.Diff(&changes, "folder_id", before.FolderID, out.FolderID)
		audit.Diff(&changes, "slug", audit.Ptr(before.Slug), audit.Ptr(out.Slug))
		audit.Diff(&changes, "position", audit.Ptr(before.Position), audit.Ptr(out.Position))
		changed = true
		return s.record(ctx, tx, "note.move", "note", id,
			fmt.Sprintf("Přesunuta poznámka „%s“", out.Title), changes, metaVia(nil, via), sc)
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notifyScoped(ctx, "note.changed", id, private)
	}
	return s.noteDetail(ctx, s.db, out)
}

// DeleteNote soft-deletes (archives) a note, or purges it with hard=true.
//
// ⚠ THE TWO BRANCHES BELOW DELIBERATELY LOAD THE NOTE DIFFERENTLY, and it reads
// like a bug unless you know why (D181). This is v9's ONE asymmetry:
//
//	read  — requires OWNERSHIP. Every read path 404s a foreign private item.
//	hard  — requires ADMIN, exactly as it did before v9, and nothing else.
//
// So an `admin` may permanently delete another member's private note while every
// GET of that same id still 404s for them. Somebody has to be able to reclaim
// space and remove a departed member's files, and that power was never coupled to
// being able to read the content. Ownership, conversely, grants NO hard delete —
// that is unchanged from v3 and v9 does not widen it.
//
// Do not "simplify" these two loads into one. The admin branch cannot use the
// viewer-scoped load (it would 404 on exactly the rows it exists to purge), and
// the soft branch must not use the unscoped one (it would let any editor archive a
// note they cannot see).
func (s *Service) DeleteNote(ctx context.Context, id string, hard bool) error {
	var changed, private bool
	var imagesToPurge []string // keys to drop from storage after a hard delete commits
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var before *Note
		var err error
		if hard {
			// The handler has already refused a non-admin, so this branch is
			// admin-only. It reads across scopes ON PURPOSE — see the D181 note above.
			before, err = s.store.GetNoteAnyScope(ctx, tx, id)
		} else {
			before, err = s.store.GetNote(ctx, tx, id, actorID(ctx))
		}
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("note not found")
		}
		sc := scopeOfNote(before)
		private = sc.Private
		if hard {
			// Capture the owned image ids BEFORE the delete: notes' ON DELETE CASCADE
			// removes the note_images rows, so afterwards there is nothing left to derive
			// the objects from. The objects themselves are purged after the commit.
			imageIDs, err := s.store.NoteImageIDsForNote(ctx, tx, id)
			if err != nil {
				return err
			}
			// An owned image may be embedded in ANOTHER note (content copied between
			// notes). Hand those off to a referencing note so the shared object survives
			// the cascade; only the images no surviving note references get purged. One
			// query resolves every referencing note (not an instr scan per image).
			otherRefs, err := s.otherNotesImageRefs(ctx, tx, id)
			if err != nil {
				return err
			}
			var purge []string
			owned := make(map[string]bool, len(imageIDs))
			for _, imgID := range imageIDs {
				owned[imgID] = true
				if target, ok := otherRefs[imgID]; ok {
					if err := s.store.ReassignNoteImage(ctx, tx, imgID, target); err != nil {
						return err
					}
					continue
				}
				purge = append(purge, imgID)
			}
			// This note may also have been the LAST to reference an image OWNED BY ANOTHER
			// note (content copied in, then that owner dropped its own reference). Such a row
			// survives the cascade — its owner isn't the note being deleted — so nothing but
			// the daily sweep would ever reclaim it. Drop the row+object here too, mirroring
			// gcNoteImages' dropped-cross-note-reference case, but only when no surviving note
			// still references it.
			var foreignStale []string
			for imgID := range referencedImageIDs(deref(before.BodyMD)) {
				if owned[imgID] {
					continue // owned by this note — handled above
				}
				if _, ok := otherRefs[imgID]; ok {
					continue // still referenced by another surviving note
				}
				foreignStale = append(foreignStale, imgID)
			}
			if len(foreignStale) > 0 {
				if err := s.store.DeleteNoteImages(ctx, tx, foreignStale); err != nil {
					return err
				}
				purge = append(purge, foreignStale...)
			}
			if err := s.store.DeleteNote(ctx, tx, id); err != nil {
				return err
			}
			imagesToPurge = purge
			changed = true
			// The audit event records the owner and, when an admin purged someone
			// else's private note, that it was an admin who did it. It is the only
			// trace of the one power v9 grants across the privacy boundary, so it has
			// to name both parties.
			return s.record(ctx, tx, "note.delete", "note", id,
				fmt.Sprintf("Smazána poznámka „%s“", before.Title), nil,
				metaByAdmin(ctx, metaHard(true), sc), sc)
		}
		// Soft delete is idempotent: archiving an already-archived note is a no-op —
		// don't rewrite the row, don't emit a bogus false→true audit change, and
		// don't broadcast a change that didn't happen.
		if before.Archived {
			return nil
		}
		if err := s.store.UpdateNote(ctx, tx, id, notePatch{Archived: boolPtr(true)}); err != nil {
			return err
		}
		changed = true
		return s.record(ctx, tx, "note.delete", "note", id,
			fmt.Sprintf("Smazána poznámka „%s“", before.Title),
			[]audit.Change{{Field: "archived", Old: audit.Ptr("false"), New: audit.Ptr("true")}}, metaHard(false), sc)
	})
	if err != nil {
		return err
	}
	if changed {
		s.notifyScoped(ctx, "note.changed", id, private)
	}
	// The rows are gone (cascade); drop their objects. Best-effort — a failure just
	// leaves orphans for the reconciliation pass.
	s.purgeImageObjects(ctx, imagesToPurge)
	return nil
}

// ---- Folders ----

func (s *Service) CreateFolder(ctx context.Context, in FolderCreate) (*FolderDetail, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, httpx.ErrUnprocessable("name is required")
	}
	requested, err := ParseCreateScope(ctx, in.Scope)
	if err != nil {
		return nil, err
	}
	var out *Folder
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
		out, err = s.store.InsertFolder(ctx, tx, in.ParentID, name, sl, pos, actorID(ctx), icon, sc)
		if err != nil {
			return err
		}
		changes := []audit.Change{{Field: "name", New: audit.Ptr(name)}, {Field: "slug", New: audit.Ptr(sl)}}
		if icon != "" {
			changes = append(changes, audit.Change{Field: "icon", New: audit.Ptr(icon)})
		}
		return s.record(ctx, tx, "folder.create", "folder", out.ID,
			fmt.Sprintf("Vytvořena složka „%s“", name), changes, nil, sc)
	})
	if err != nil {
		return nil, err
	}
	s.notifyScoped(ctx, "folder.changed", out.ID, scopeOfFolder(out).Private)
	return s.folderDetail(ctx, s.db, out)
}

func (s *Service) GetFolderDetail(ctx context.Context, id string) (*FolderDetail, error) {
	f, err := s.store.GetFolder(ctx, s.db, id, actorID(ctx))
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, httpx.ErrNotFound("folder not found")
	}
	return s.folderDetail(ctx, s.db, f)
}

func (s *Service) UpdateFolder(ctx context.Context, id string, in FolderUpdate) (*FolderDetail, error) {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return nil, httpx.ErrUnprocessable("name cannot be empty")
	}
	var out *Folder
	var changed, private bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetFolder(ctx, tx, id, actorID(ctx))
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
		// restoring it is the only valid mutation. Reject name/icon edits so a stale
		// client cannot silently write — and audit — edits to a phantom (the documents
		// module draws the same line in UpdateFolder).
		if before.Archived && !unarchiving && (in.Name != nil || in.Icon != nil) {
			return httpx.ErrNotFound("folder not found")
		}
		if archiving {
			// Archiving must cascade to descendants, else live children are stranded
			// under an archived ancestor (unreachable in the tree and by slug path).
			// Route non-empty folders through DELETE (cascade=true), which archives
			// the whole subtree; UpdateFolder only archives an already-empty folder.
			sub, noteCount, err := s.store.FolderChildCounts(ctx, tx, id)
			if err != nil {
				return err
			}
			if sub > 0 || noteCount > 0 {
				return httpx.ErrConflict(fmt.Sprintf("folder not empty (%d subfolders, %d notes) — archive via DELETE with cascade=true", sub, noteCount))
			}
		}
		if unarchiving {
			// Restoring a folder: its parent must still be live (see UpdateNote).
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
			// Re-free its slug against live siblings before it re-enters the live
			// index (a sibling may have reused it while archived — see UpdateNote).
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
		out, err = s.store.GetFolder(ctx, tx, id, actorID(ctx))
		if err != nil {
			return err
		}
		var changes []audit.Change
		audit.Diff(&changes, "name", audit.Ptr(before.Name), audit.Ptr(out.Name))
		audit.Diff(&changes, "slug", audit.Ptr(before.Slug), audit.Ptr(out.Slug))
		audit.Diff(&changes, "icon", audit.Ptr(before.Icon), audit.Ptr(out.Icon))
		audit.Diff(&changes, "archived", audit.Ptr(fmt.Sprint(before.Archived)), audit.Ptr(fmt.Sprint(out.Archived)))
		// A no-op PATCH (nothing actually changed) leaves no audit trail and doesn't
		// broadcast a change that other clients would needlessly refetch on.
		if len(changes) == 0 {
			return nil
		}
		changed = true
		return s.record(ctx, tx, "folder.update", "folder", id,
			fmt.Sprintf("Upravena složka „%s“", out.Name), changes, nil, sc)
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notifyScoped(ctx, "folder.changed", id, private)
	}
	return s.folderDetail(ctx, s.db, out)
}

func (s *Service) MoveFolder(ctx context.Context, id string, in FolderMoveRequest) (*FolderDetail, error) {
	if strings.TrimSpace(in.Position) == "" {
		return nil, httpx.ErrUnprocessable("position is required")
	}
	var out *Folder
	var changed, private bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetFolder(ctx, tx, id, actorID(ctx))
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("folder not found")
		}
		// Reject moving an archived (soft-deleted) folder — see assertLiveForMutation.
		if err := assertLiveForMutation(before.Archived, "folder not found"); err != nil {
			return err
		}
		sc := scopeOfFolder(before)
		private = sc.Private
		// nil requested — see MoveNote: the destination's own scope must come back
		// so a crossing reaches the D186 refusal below.
		dest := sc
		if in.ParentID != nil && *in.ParentID != "" {
			if dest, err = s.assertFolder(ctx, tx, in.ParentID, nil); err != nil {
				return err
			}
		}
		// Cross-scope moves are 422, as for notes — publishing is the only crossing
		// and it is a different verb on purpose (D186).
		if dest != sc {
			return errCrossScopeMove(sc)
		}
		// Cycle guard: a folder may not move into itself or a descendant (D31).
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
		// A move that re-sends the folder's current parent/position/slug changes
		// nothing: skip the write, audit, and broadcast — mirroring UpdateFolder's
		// no-op gating (see MoveNote for the spurious-broadcast rationale).
		if audit.EqualPtr(before.ParentID, in.ParentID) && before.Position == in.Position && before.Slug == sl {
			out = before
			return nil
		}
		if err := s.store.MoveFolderRow(ctx, tx, id, in.ParentID, in.Position, sl); err != nil {
			return err
		}
		out, err = s.store.GetFolder(ctx, tx, id, actorID(ctx))
		if err != nil {
			return err
		}
		var changes []audit.Change
		audit.Diff(&changes, "parent_id", before.ParentID, out.ParentID)
		audit.Diff(&changes, "slug", audit.Ptr(before.Slug), audit.Ptr(out.Slug))
		audit.Diff(&changes, "position", audit.Ptr(before.Position), audit.Ptr(out.Position))
		changed = true
		return s.record(ctx, tx, "folder.move", "folder", id,
			fmt.Sprintf("Přesunuta složka „%s“", out.Name), changes, nil, sc)
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notifyScoped(ctx, "folder.changed", id, private)
	}
	return s.folderDetail(ctx, s.db, out)
}

// DeleteFolder soft-deletes by default. A non-empty folder needs cascade=true
// (which soft-deletes the subtree, logging each child); hard=true purges (admin,
// enforced at the route).
//
// ⚠ Same two-branch load as DeleteNote, for the same reason (D181), and it matters
// MORE here: `DELETE …/folders/{id}?hard=true&cascade=true` is what actually
// reclaims a whole private subtree, so it is the route the purge screen leans on
// (D212). Reads require ownership; the hard purge requires admin and reads across
// scopes deliberately.
func (s *Service) DeleteFolder(ctx context.Context, id string, cascade, hard bool) error {
	var changed, private bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var before *Folder
		var err error
		if hard {
			before, err = s.store.GetFolderAnyScope(ctx, tx, id)
		} else {
			before, err = s.store.GetFolder(ctx, tx, id, actorID(ctx))
		}
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("folder not found")
		}
		sc := scopeOfFolder(before)
		private = sc.Private
		sub, noteCount, err := s.store.FolderChildCounts(ctx, tx, id)
		if err != nil {
			return err
		}
		nonEmpty := sub > 0 || noteCount > 0

		if hard {
			// FK ON DELETE CASCADE purges descendant folders/notes/pins. Enumerate the
			// whole physical subtree (incl. already-archived rows) BEFORE the delete so
			// every purged descendant is audited — otherwise the cascade destroys them
			// with no trail, breaking "every mutation is audited" (D35). folderIDs
			// includes id itself and is ordered shallowest-first.
			folderIDs, err := s.store.DescendantFolderIDs(ctx, tx, id, true)
			if err != nil {
				return err
			}
			childNotes, err := s.store.NotesInFolders(ctx, tx, folderIDs, true)
			if err != nil {
				return err
			}
			// Capture folder names before the cascade destroys the rows (one query,
			// not an N+1 GetFolder over the subtree we already enumerated).
			metas, err := s.store.FolderMetaByIDs(ctx, tx, folderIDs)
			if err != nil {
				return err
			}
			if err := s.store.DeleteFolder(ctx, tx, id); err != nil {
				return err
			}
			changed = true
			for _, n := range childNotes {
				if err := s.record(ctx, tx, "note.delete", "note", n.ID,
					fmt.Sprintf("Smazána poznámka „%s“ (kaskádou)", n.Title), nil,
					metaByAdmin(ctx, map[string]any{"hard": true, "via": "cascade"}, sc), sc); err != nil {
					return err
				}
			}
			// deepest folders first so the audit reads leaf→root; the target folder
			// (id) records last, marked hard without the cascade via.
			for i := len(folderIDs) - 1; i >= 0; i-- {
				fID := folderIDs[i]
				m := map[string]any{"hard": true}
				if fID != id {
					m["via"] = "cascade"
				}
				if err := s.record(ctx, tx, "folder.delete", "folder", fID,
					fmt.Sprintf("Smazána složka „%s“", metas[fID].Name), nil, metaByAdmin(ctx, m, sc), sc); err != nil {
					return err
				}
			}
			return nil
		}

		if nonEmpty && !cascade {
			return httpx.ErrConflict(fmt.Sprintf("folder not empty (%d subfolders, %d notes) — pass cascade=true", sub, noteCount))
		}

		// Soft cascade: archive the whole (live) subtree, logging each child.
		folderIDs, err := s.store.DescendantFolderIDs(ctx, tx, id, false)
		if err != nil {
			return err
		}
		childNotes, err := s.store.NotesInFolders(ctx, tx, folderIDs, false)
		if err != nil {
			return err
		}
		for _, n := range childNotes {
			if err := s.store.UpdateNote(ctx, tx, n.ID, notePatch{Archived: boolPtr(true)}); err != nil {
				return err
			}
			changed = true
			if err := s.record(ctx, tx, "note.delete", "note", n.ID,
				fmt.Sprintf("Smazána poznámka „%s“ (kaskádou)", n.Title),
				[]audit.Change{{Field: "archived", Old: audit.Ptr("false"), New: audit.Ptr("true")}},
				map[string]any{"via": "cascade"}, sc); err != nil {
				return err
			}
		}
		// Names + archived state for the whole subtree in one query (no N+1); the
		// root id can already be archived (re-deleting an archived folder), so keep
		// skipping already-archived rows to avoid a bogus false→true transition.
		metas, err := s.store.FolderMetaByIDs(ctx, tx, folderIDs)
		if err != nil {
			return err
		}
		// deepest folders first so the audit reads leaf→root; order isn't required.
		for i := len(folderIDs) - 1; i >= 0; i-- {
			fID := folderIDs[i]
			meta, ok := metas[fID]
			if !ok || meta.Archived {
				continue
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
			if err := s.record(ctx, tx, "folder.delete", "folder", fID,
				fmt.Sprintf("Smazána složka „%s“", meta.Name),
				[]audit.Change{{Field: "archived", Old: audit.Ptr("false"), New: audit.Ptr("true")}}, m, sc); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Skip the broadcast when the delete was a no-op (e.g. re-deleting an already-
	// archived empty folder wrote nothing), matching DeleteNote — a spurious
	// folder.changed makes every other client refetch and can raise a bogus
	// "changed elsewhere" toast.
	if changed {
		s.notifyScoped(ctx, "folder.changed", id, private)
	}
	return nil
}

// wouldCycle reports whether moving movingID under newParent would create a cycle
// (newParent is movingID itself or a descendant). Walks up from newParent to root.
func (s *Service) wouldCycle(ctx context.Context, tx DBTX, movingID string, newParent *string) (bool, error) {
	cur := newParent
	for depth := 0; cur != nil && depth < 1000; depth++ {
		if *cur == movingID {
			return true, nil
		}
		f, err := s.store.GetFolder(ctx, tx, *cur, actorID(ctx))
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

// assertFolder verifies a referenced parent/folder exists, is live, and is one the
// caller may see — and returns THE SCOPE THE FOLDER LIVES IN, which is the scope
// anything created under it must take (v9).
//
// requested is the caller's `scope` field, honoured ONLY at the root: with a
// parent folder the PARENT'S scope governs, and a disagreement is a 422 rather
// than a silent correction. A folder whose contents are half private is exactly
// the model D177 rejected, so it must be impossible to build one by accident.
//
// ⚠ requested is a POINTER because "the caller said shared" and "the caller said
// nothing" are different questions here, and the zero Scope cannot tell them
// apart. nil defers to the parent; a non-nil value that disagrees with the parent
// is the 422. Passing Scope{} for "unspecified" is what made `scope:"shared"`
// into a private folder a silent correction rather than a refusal — the one
// direction of the check that never fired.
//
// A folder in another member's private tree reads back as nil here — the store's
// viewer predicate saw to that — so it is reported as a nonexistent folder_id,
// never as a permission problem (D180).
func (s *Service) assertFolder(ctx context.Context, q DBTX, folderID *string, requested *Scope) (Scope, error) {
	if folderID == nil || *folderID == "" {
		root := Scope{}
		if requested != nil {
			root = *requested
		}
		return root, assertPairing(root)
	}
	f, err := s.store.GetFolder(ctx, q, *folderID, actorID(ctx))
	if err != nil {
		return Scope{}, err
	}
	if f == nil || f.Archived {
		return Scope{}, httpx.ErrUnprocessable("folder_id does not reference an existing folder")
	}
	parent := callerScopeFor(f.Visibility, f.OwnerID)
	if requested != nil && *requested != parent {
		return Scope{}, httpx.ErrUnprocessable(
			"scope disagrees with the parent folder — an item takes the scope of the folder it is filed in")
	}
	return parent, nil
}

// assertParentLive rejects restoring an item under a missing or archived parent:
// every ancestor of a live item must itself be live, since Resolve walks only live
// folders (a live descendant of an archived folder is unreachable). nil = root.
func (s *Service) assertParentLive(ctx context.Context, q DBTX, parentID *string) error {
	if parentID == nil {
		return nil
	}
	f, err := s.store.GetFolder(ctx, q, *parentID, actorID(ctx))
	if err != nil {
		return err
	}
	if f == nil || f.Archived {
		return httpx.ErrUnprocessable("cannot restore under a missing or archived parent folder")
	}
	return nil
}

// scopeOfNote / scopeOfFolder read an item's own root scope off its stored columns.
// Used wherever a mutation has already loaded the row and needs to name the scope
// for a sibling query or an audit event.
func scopeOfNote(n *Note) Scope     { return callerScopeFor(n.Visibility, n.OwnerID) }
func scopeOfFolder(f *Folder) Scope { return callerScopeFor(f.Visibility, f.OwnerID) }

// assertLiveForMutation rejects a structural mutation (move/reorder/reparent) of an
// archived (soft-deleted) item: it's out of the live tree and unreachable by slug,
// so a stale client mutating a cascade-archived item must 404 rather than silently
// mutate a phantom. UpdateNote permits one archived mutation (unarchiving) and so
// inlines its own, narrower guard instead of calling this.
func assertLiveForMutation(archived bool, notFoundMsg string) error {
	if archived {
		return httpx.ErrNotFound(notFoundMsg)
	}
	return nil
}

// ---- Tree, list, search, resolve ----

// Tree returns ONE root scope — never both (leak table row 1). A response that
// carried the shared tree and a private one together would be a response the
// frontend has to filter, and the frontend is the wrong place for that: the
// switcher chooses a root, the API returns that root, and there is no state in
// which both are on the wire at once.
func (s *Service) Tree(ctx context.Context, includeArchived bool, sc Scope) (*NotesTree, error) {
	folders, err := s.store.AllFolders(ctx, includeArchived, sc)
	if err != nil {
		return nil, err
	}
	summaries, err := s.store.AllNoteSummaries(ctx, includeArchived, sc)
	if err != nil {
		return nil, err
	}
	hh, pers, err := s.store.PinSets(ctx, actorID(ctx))
	if err != nil {
		return nil, err
	}

	notesByFolder := map[string][]NoteSummary{}
	for _, n := range summaries {
		n.Pinned = PinState{Household: hh[n.ID], Personal: pers[n.ID]}
		key := deref(n.FolderID)
		notesByFolder[key] = append(notesByFolder[key], n)
	}
	foldersByParent := map[string][]Folder{}
	for _, f := range folders {
		key := deref(f.ParentID)
		foldersByParent[key] = append(foldersByParent[key], f)
	}

	// visited breaks any parent_id cycle (manual DB edit, restore anomaly) so build
	// can't recurse unbounded — mirroring the depth caps in ancestors/wouldCycle.
	visited := map[string]bool{}
	var build func(parentID string) []FolderNode
	build = func(parentID string) []FolderNode {
		nodes := []FolderNode{}
		for _, f := range foldersByParent[parentID] {
			if visited[f.ID] {
				continue
			}
			visited[f.ID] = true
			nodes = append(nodes, FolderNode{
				Folder:     f,
				Subfolders: build(f.ID),
				Notes:      orEmptyNotes(notesByFolder[f.ID]),
			})
		}
		return nodes
	}
	return &NotesTree{Roots: build(""), RootNotes: orEmptyNotes(notesByFolder[""])}, nil
}

// List returns note summaries (with the caller's pin state) for a folder, or the
// FTS5 search results when q is set (FR-P6) — always within ONE root scope.
//
// ⚠ Search is scoped to the tree you are standing in (D184), and the predicate
// lives in the SQL rather than out here. Filtering the returned slice would still
// leak: the cap is applied in the query, so a post-filtered page tells the caller
// how many rows matched before filtering just by being short.
//
// folderID semantics (D203, a bug that predates v9): nil means "every note in this
// scope"; the root sentinel (an empty string, from ?folder_id=root) means "the
// notes directly at this scope's root". Before v9 `notes` had no sentinel at all —
// a nil pointer dereferenced to the empty string and quietly meant "root only", which is not
// what the 0.10.1 contract said.
func (s *Service) List(ctx context.Context, q string, folderID *string, includeArchived bool, sc Scope) (NotePage, error) {
	q = strings.TrimSpace(q)
	var items []NoteSummary
	var err error
	switch {
	case q != "":
		match := appdb.FTSQuery(q)
		if match == "" {
			// A punctuation-only query has no searchable tokens. Return an empty result
			// rather than run `notes_fts MATCH ''`, whose empty-phrase match behavior is
			// unspecified and varies across SQLite/FTS5 builds.
			return NotePage{Items: []NoteSummary{}}, nil
		}
		items, err = s.store.SearchNotes(ctx, match, folderID, includeArchived, 100, sc)
	case folderID == nil:
		items, err = s.store.ScopedNoteSummaries(ctx, s.db, includeArchived, sc)
	default:
		items, err = s.store.NoteSummariesInFolder(ctx, s.db, folderID, includeArchived, sc)
	}
	if err != nil {
		return NotePage{}, err
	}
	hh, pers, err := s.store.PinSets(ctx, actorID(ctx))
	if err != nil {
		return NotePage{}, err
	}
	for i := range items {
		items[i].Pinned = PinState{Household: hh[items[i].ID], Personal: pers[items[i].ID]}
	}
	if items == nil {
		items = []NoteSummary{}
	}
	return NotePage{Items: items}, nil
}

// Resolve maps a slug path to a stable id (FR-P4). No redirects: a renamed/moved
// item's old path just 404s.
//
// v9: a slug path is MEANINGLESS without a scope (leak table row 3). The same path
// names a different item in the shared tree and in each member's private one, so
// the walk starts from the named root and never leaves it.
func (s *Service) Resolve(ctx context.Context, path string, sc Scope) (*ResolveResult, error) {
	segs := splitPath(path)
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
			if f == nil {
				return nil, httpx.ErrNotFound("path not found")
			}
			parent = &f.ID
			continue
		}
		// final segment: a folder or a note under this parent
		if f != nil {
			return &ResolveResult{Type: "folder", ID: f.ID, SlugPath: strings.Join(segs, "/")}, nil
		}
		n, err := s.store.ChildNoteBySlug(ctx, s.db, parent, seg, sc)
		if err != nil {
			return nil, err
		}
		if n != nil {
			return &ResolveResult{Type: "note", ID: n.ID, SlugPath: strings.Join(segs, "/")}, nil
		}
		return nil, httpx.ErrNotFound("path not found")
	}
	return nil, httpx.ErrNotFound("path not found")
}

// ---- Publish: private → shared (v9, D182) ----

// PublishNote moves one private note into the shared tree.
//
// ⚠ A NON-OWNER GETS 404, NOT 403 — AN ADMIN INCLUDED (D206), byte-identical to
// the response for an id that was never issued. This route is RequireWrite, so
// every editor can call it; a 403-for-private / 404-for-unknown pair would answer
// "does this id exist, and is it private?" for any id a caller cares to try. That
// is the permalink oracle D180 closes, reopened with a different verb, and it was
// specified as 403 in five documents before a review pass caught it. It is true
// that this leaves an admin with less power than the role implies — publishing
// something you cannot read is not a power that should exist — but it must not be
// OBSERVABLE that way.
//
// Everything happens in one transaction: flip visibility, clear the owner,
// reparent, and re-derive the slug, because the destination's siblings are
// different siblings.
//
// Two things deliberately do NOT change. The PERSONAL PIN survives (D183) — it is
// keyed on the item id and the id does not move. And for documents the permanent
// /d/{id} URL survives too, because the R2 key is id-based and independent of
// folder, slug and scope (D42); that is the whole reason it was specified as
// permanent.
//
// There is no unpublish, and the absence is a decision (D182): a note the
// household has relied on for months must not be able to vanish into one member's
// tree. Someone who wants it back re-creates it privately and deletes the shared
// copy, leaving both facts in the log.
func (s *Service) PublishNote(ctx context.Context, id string, in PublishRequest) (*NoteDetail, error) {
	var out *Note
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetNote(ctx, tx, id, actorID(ctx))
		if err != nil {
			return err
		}
		// Not found, not mine, or archived — all the same answer (D206).
		if before == nil || before.Archived {
			return errPublishNotFound
		}
		if before.Visibility != visibilityPrivate {
			return httpx.ErrUnprocessable("poznámka už je sdílená")
		}
		// Ownership, restated: the viewer-scoped load above already refuses another
		// member's private note, so reaching here with a foreign owner is impossible.
		// The check stays as a guard against a future load that forgets.
		if deref(before.OwnerID) != actorID(ctx) {
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
			if pos, err = s.store.lastNotePosition(ctx, tx, in.FolderID, dest); err != nil {
				return err
			}
		}
		if err := s.store.PublishNoteRow(ctx, tx, id, in.FolderID, pos, sl); err != nil {
			return err
		}
		out, err = s.store.GetNote(ctx, tx, id, actorID(ctx))
		if err != nil {
			return err
		}
		changes := []audit.Change{
			{Field: "visibility", Old: audit.Ptr(visibilityPrivate), New: audit.Ptr(visibilityShared)},
			{Field: "owner_id", Old: before.OwnerID, New: nil},
		}
		audit.Diff(&changes, "folder_id", before.FolderID, out.FolderID)
		audit.Diff(&changes, "slug", audit.Ptr(before.Slug), audit.Ptr(out.Slug))
		// Audited against the item's NEW scope (shared): the event describes a note
		// the household can now see, so there is nothing left to redact — and
		// redacting the one event that says "this became visible to everyone" would
		// hide the single most consequential thing that can happen to a private item.
		return s.record(ctx, tx, "note.publish", "note", id,
			fmt.Sprintf("Publikována poznámka „%s“ do sdílených", out.Title), changes,
			map[string]any{"published_from_owner": deref(before.OwnerID)}, Scope{})
	})
	if err != nil {
		return nil, err
	}
	s.notify(ctx, "note.changed", map[string]string{"id": id})
	return s.noteDetail(ctx, s.db, out)
}

// PublishFolder publishes a private folder AND EVERY DESCENDANT in one
// transaction. A partial publish — half a folder visible to the household — is the
// one outcome this endpoint must never produce, because half a published folder is
// a folder whose contents nobody can explain.
func (s *Service) PublishFolder(ctx context.Context, id string, in PublishRequest) (*FolderDetail, error) {
	var out *Folder
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetFolder(ctx, tx, id, actorID(ctx))
		if err != nil {
			return err
		}
		if before == nil || before.Archived {
			return errPublishFolderNotFound
		}
		if before.Visibility != visibilityPrivate {
			return httpx.ErrUnprocessable("složka už je sdílená")
		}
		if deref(before.OwnerID) != actorID(ctx) {
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
		// An ordinary move needs one (D31); a publish cannot: the refusal above has
		// already established that the destination is SHARED, while this folder is
		// private and a subtree never straddles two scopes (see DescendantFolderIDs),
		// so the destination can never be inside the subtree being published. The
		// guard that stood here always returned false, and paid for it with one
		// viewer-scoped GetFolder per ancestor level inside the irreversible path's
		// transaction.
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
		// ⚠ AND ENUMERATE ITS CONTENTS, because EVERY ONE OF THEM IS AUDITED
		// SEPARATELY. A folder publish is a cascade like a cascade delete, and it is
		// the irreversible one (D182) — so it follows the same rule the delete path
		// follows: every row the cascade touches gets its own event, or the subtree
		// becomes household-visible with a single log line naming only its parent.
		//
		// Two things break without this. A rule subscribed to `notes.note.publish`
		// — an action this module declares and admin/labels.go labels — never fires
		// for any of the forty notes that just became visible to everyone. And each
		// published note's entity timeline shows nothing at all for the single most
		// consequential change that can happen to it.
		//
		// Archived descendants are included because PublishDescendants publishes
		// them too; what is audited has to be what was written.
		childNotes, err := s.store.NotesInFolders(ctx, tx, folderIDs, true)
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
		// Descendants keep their slugs: their siblings are the same siblings they
		// had. Only the subtree's ROOT lands among strangers, which is why only it
		// re-derives one above.
		if err := s.store.PublishDescendants(ctx, tx, folderIDs); err != nil {
			return err
		}
		out, err = s.store.GetFolder(ctx, tx, id, actorID(ctx))
		if err != nil {
			return err
		}
		// The cascade's own events, children first and the target folder last —
		// the same order the cascade delete records in, so the two read alike.
		// Everything here is audited against the NEW scope (shared): there is
		// nothing left to redact once the household can see it, and redacting the
		// event that says "this became visible to everyone" would hide the one
		// thing the log is for.
		owner := deref(before.OwnerID)
		for _, n := range childNotes {
			if err := s.record(ctx, tx, "note.publish", "note", n.ID,
				fmt.Sprintf("Publikována poznámka „%s“ do sdílených (kaskádou)", n.Title),
				publishChanges(before.OwnerID), publishMeta(owner, true), Scope{}); err != nil {
				return err
			}
		}
		for _, fID := range folderIDs {
			if fID == id {
				continue // the target folder records below, without the cascade via
			}
			if err := s.record(ctx, tx, "folder.publish", "folder", fID,
				fmt.Sprintf("Publikována složka „%s“ do sdílených (kaskádou)", metas[fID].Name),
				publishChanges(before.OwnerID), publishMeta(owner, true), Scope{}); err != nil {
				return err
			}
		}
		changes := publishChanges(before.OwnerID)
		audit.Diff(&changes, "parent_id", before.ParentID, out.ParentID)
		audit.Diff(&changes, "slug", audit.Ptr(before.Slug), audit.Ptr(out.Slug))
		meta := publishMeta(owner, false)
		meta["folders"] = len(folderIDs)
		meta["notes"] = len(childNotes)
		return s.record(ctx, tx, "folder.publish", "folder", id,
			fmt.Sprintf("Publikována složka „%s“ do sdílených", out.Name), changes, meta, Scope{})
	})
	if err != nil {
		return nil, err
	}
	s.notify(ctx, "folder.changed", map[string]string{"id": id})
	return s.folderDetail(ctx, s.db, out)
}

// publishChanges is the field diff every publish event carries: the two columns
// that define the root scope, moving in the only direction they ever move.
// Shared by the note, the folder and the whole cascade so a reader of the log sees
// one shape rather than three.
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
// ⚠ D206 asks for one message per ROUTE, not one message across entity types —
// the folder route gets its own, because telling a client "note not found" for a
// folder id buys no indistinguishability and misnames the thing that was not
// found. `GET /api/notes/folders/{id}` already answers "folder not found".
var (
	errPublishNotFound       = httpx.ErrNotFound("note not found")
	errPublishFolderNotFound = httpx.ErrNotFound("folder not found")
)

// ---- Pinning (two scopes, D35) ----

// Pin adds a pin. household requires editor/admin and is audited + broadcast;
// personal is allowed for any member (incl. reader), not audited, not broadcast.
//
// v9 adds one rule: a HOUSEHOLD pin on a PRIVATE note is a 422 (D183). Note the
// status carefully — 422, not 403. The caller has the role; the operation is
// meaningless, because a household pin means "put this on everyone's Nástěnka"
// and nobody else can open it. A personal pin by a non-owner never reaches that
// check: the note 404s first.
func (s *Service) Pin(ctx context.Context, noteID, scopeStr, via string) (*PinState, error) {
	scope, err := parsePinScope(scopeStr)
	if err != nil {
		return nil, err
	}
	n, err := s.store.GetNote(ctx, s.db, noteID, actorID(ctx))
	if err != nil {
		return nil, err
	}
	if n == nil || n.Archived {
		return nil, httpx.ErrNotFound("note not found")
	}
	if scope == scopeHousehold && n.Visibility == visibilityPrivate {
		return nil, httpx.ErrUnprocessable(
			"soukromou poznámku nelze připnout pro všechny — ostatní ji nevidí")
	}
	uid := actorID(ctx)

	if scope == scopePersonal {
		// No audit/broadcast (D35), but still run the read-then-write (MAX(position)
		// → INSERT) inside WithTx so it's serialized by BEGIN IMMEDIATE, matching the
		// household path — otherwise two concurrent personal pins could collide on the
		// same position if MaxOpenConns ever grows (see db.go's _txlock note).
		err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
			// Idempotent re-pin: if it's already personally pinned, skip the
			// MAX(position) scan and the no-op INSERT OR IGNORE entirely (mirrors the
			// household path gating its work on `inserted`).
			exists, err := s.store.PersonalPinExists(ctx, tx, noteID, uid)
			if err != nil || exists {
				return err
			}
			pos, err := s.store.lastPinPosition(ctx, tx, scopePersonal, uid)
			if err != nil {
				return err
			}
			_, err = s.store.InsertPin(ctx, tx, noteID, scopePersonal, &uid, uid, pos)
			return err
		})
		if err != nil {
			return nil, err
		}
		return s.pinState(ctx, noteID, uid)
	}

	// household
	if !writeAllowed(ctx) {
		return nil, httpx.ErrForbidden("household pin requires editor or admin")
	}
	var changed bool
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		// Idempotent re-pin: if it's already pinned for the household, skip the
		// MAX(position) scan and the no-op INSERT OR IGNORE entirely (mirrors the
		// personal path gating its work on PersonalPinExists). changed stays false, so
		// no audit/broadcast fires for a no-op.
		exists, err := s.store.HouseholdPinExists(ctx, tx, noteID)
		if err != nil || exists {
			return err
		}
		pos, err := s.store.lastPinPosition(ctx, tx, scopeHousehold, "")
		if err != nil {
			return err
		}
		inserted, err := s.store.InsertPin(ctx, tx, noteID, scopeHousehold, nil, uid, pos)
		if err != nil {
			return err
		}
		changed = inserted
		if inserted { // idempotent: only audit a real change
			return s.record(ctx, tx, "note.pin", "note", noteID,
				fmt.Sprintf("Připnuta poznámka „%s“ pro všechny", n.Title), nil,
				metaVia(map[string]any{"scope": scopeHousehold}, via), scopeOfNote(n))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if changed { // idempotent: don't broadcast a no-op re-pin
		s.notify(ctx, "note.pin", map[string]string{"note_id": noteID})
	}
	return s.pinState(ctx, noteID, uid)
}

// Unpin removes a pin, mirroring Pin's audit/broadcast rules.
func (s *Service) Unpin(ctx context.Context, noteID, scopeStr, via string) (*PinState, error) {
	scope, err := parsePinScope(scopeStr)
	if err != nil {
		return nil, err
	}
	n, err := s.store.GetNote(ctx, s.db, noteID, actorID(ctx))
	if err != nil {
		return nil, err
	}
	if n == nil {
		return nil, httpx.ErrNotFound("note not found")
	}
	uid := actorID(ctx)

	if scope == scopePersonal {
		if _, err := s.store.DeletePin(ctx, s.db, noteID, scopePersonal, &uid); err != nil {
			return nil, err
		}
		return s.pinState(ctx, noteID, uid)
	}

	if !writeAllowed(ctx) {
		return nil, httpx.ErrForbidden("household pin requires editor or admin")
	}
	var changed bool
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		removed, err := s.store.DeletePin(ctx, tx, noteID, scopeHousehold, nil)
		if err != nil {
			return err
		}
		changed = removed
		if removed {
			return s.record(ctx, tx, "note.unpin", "note", noteID,
				fmt.Sprintf("Odepnuta poznámka „%s“ (pro všechny)", n.Title), nil,
				metaVia(map[string]any{"scope": scopeHousehold}, via), scopeOfNote(n))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if changed { // idempotent: don't broadcast a no-op unpin
		s.notify(ctx, "note.unpin", map[string]string{"note_id": noteID})
	}
	return s.pinState(ctx, noteID, uid)
}

func (s *Service) pinState(ctx context.Context, noteID, userID string) (*PinState, error) {
	st, err := s.store.GetPinState(ctx, s.db, noteID, userID)
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// ---- detail assembly ----

func (s *Service) noteDetail(ctx context.Context, q DBTX, n *Note) (*NoteDetail, error) {
	path, err := s.ancestors(ctx, q, n.FolderID)
	if err != nil {
		return nil, err
	}
	st, err := s.store.GetPinState(ctx, q, n.ID, actorID(ctx))
	if err != nil {
		return nil, err
	}
	return &NoteDetail{
		Note:     *n,
		Path:     path,
		SlugPath: slugPathFrom(path, n.Slug),
		Pinned:   st,
	}, nil
}

func (s *Service) folderDetail(ctx context.Context, q DBTX, f *Folder) (*FolderDetail, error) {
	path, err := s.ancestors(ctx, q, &f.ID)
	if err != nil {
		return nil, err
	}
	sc := scopeOfFolder(f)
	subs, err := s.store.ChildFolders(ctx, q, &f.ID, false, sc)
	if err != nil {
		return nil, err
	}
	notes, err := s.store.NoteSummariesInFolder(ctx, q, &f.ID, false, sc)
	if err != nil {
		return nil, err
	}
	hh, pers, err := s.store.PinSets(ctx, actorID(ctx))
	if err != nil {
		return nil, err
	}
	for i := range notes {
		notes[i].Pinned = PinState{Household: hh[notes[i].ID], Personal: pers[notes[i].ID]}
	}
	return &FolderDetail{
		Folder:     *f,
		Path:       path,
		SlugPath:   slugPathFrom(path, ""),
		Subfolders: subs,
		Notes:      notes,
	}, nil
}

// ancestors returns the folder chain root→…→folderID (inclusive). Empty (but never
// nil, so it marshals as `[]` not `null` — a root note's path is an empty array, and
// clients read path.length) for a root item (folderID nil).
func (s *Service) ancestors(ctx context.Context, q DBTX, folderID *string) ([]PathSegment, error) {
	chain := []PathSegment{}
	cur := folderID
	for depth := 0; cur != nil && depth < 1000; depth++ {
		f, err := s.store.GetFolder(ctx, q, *cur, actorID(ctx))
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

// metaByAdmin stamps `by_admin` when an admin purges a PRIVATE item that is not
// theirs — v9's one asymmetry, and the only place in Home where somebody acts on
// something they are not allowed to read (D181). Nothing else sets it: an admin
// deleting a shared item is doing an ordinary admin thing, and marking that would
// dilute the flag until it stopped meaning anything.
func metaByAdmin(ctx context.Context, base map[string]any, sc Scope) map[string]any {
	if !sc.Private || sc.OwnerID == actorID(ctx) || !isAdminCtx(ctx) {
		return base
	}
	if base == nil {
		base = map[string]any{}
	}
	base[audit.MetaByAdmin] = true
	return base
}

// parsePinScope parses the PIN scope ("household" | "personal"), which is a
// different axis from the v9 root scope that ParseScope in scope.go handles.
// Renamed in v9 so the two cannot be confused at a call site.
func parsePinScope(s string) (string, error) {
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

func orEmptyNotes(x []NoteSummary) []NoteSummary {
	if x == nil {
		return []NoteSummary{}
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

// ---- Inline-image helpers ----

// errInlineImageData rejects a body_md carrying a base64 data: image (see
// hasInlineImageData). Shared so Create and Update return the identical 422.
var errInlineImageData = httpx.ErrUnprocessable(
	"inline base64 images are not allowed; paste uploads the image and embeds a reference URL")

// noteImageRefRE matches the content URLs embedded in body_md as
// `![](/api/notes/images/{id})`. The id is a UUIDv7 (canonical 8-4-4-4-12 hex).
var noteImageRefRE = regexp.MustCompile(
	`/api/notes/images/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`)

// referencedImageIDs is the set of image ids body_md still points at.
func referencedImageIDs(body string) map[string]bool {
	out := map[string]bool{}
	for _, m := range noteImageRefRE.FindAllStringSubmatch(body, -1) {
		out[m[1]] = true
	}
	return out
}

// inlineImageDataURIRE matches a data:image URI in IMAGE-REFERENCE position — a
// markdown image `![](data:image…)` (group 1) or a raw-HTML image source
// `<img src="data:image…">` (group 2) — capturing the URI so its length can be
// measured (see hasInlineImageData). Each payload class stops only at its STRUCTURAL
// terminator (`)` for the markdown form, the closing quote for the HTML form) and
// deliberately allows whitespace, so a line-wrapped or multi-line base64 image can't
// slip under the cap by being measured only up to its first newline (the bug a naive
// `[^)"'\s]*` had). The markdown form additionally spans BALANCED inner parens (`(…)`),
// which CommonMark permits in an unwrapped destination, so a data URI whose payload
// carries them — SVG path data, say — is measured WHOLE instead of being truncated at
// the first inner `)` and under-counted below the cap. Anchoring on image position —
// not a bare `data:` token — mirrors the editor's "image nodes only" upload rule and
// spares a short snippet in prose.
//
// The terminator is REQUIRED, not merely where the capture stops. Without it an
// unterminated `![](data:image/…` (a hand-written snippet someone never closed) captured
// everything to the next `)` anywhere below — so once the trailing prose passed the cap,
// EVERY save of that note 422'd on text that is not an image at all, with no way out but
// deleting the line. An unterminated URI renders as literal text rather than an image, so
// requiring the terminator gives up no protection: the harm this guards is a real inlined
// image, and a body big enough to hurt is already refused by httpx's 1 MiB cap. It also
// restores parity with the editor's imageRefDataURIRE, which matches the same shapes.
var inlineImageDataURIRE = regexp.MustCompile(
	`!\[[^\]]*\]\((data:image/(?:[^()]|\([^()]*\))+)\)|<img[^>]+src=["'](data:image/[^"']+)["']`)

// maxInlineImageDataLen is the longest a data:image URI may be before body_md is
// rejected. The harm this guards against is a MULTI-MEGABYTE inlined image blowing
// the API body cap and freezing the editor (the bug this feature fixes); a real
// pasted raster is tens of KB of base64 and up. Below the threshold sits the harmless
// case a blunt substring match used to reject too: a short data:image snippet a user
// legitimately quotes or documents (e.g. a 1×1 placeholder), which stays well under
// the body cap and does not freeze anything.
const maxInlineImageDataLen = 4096

// hasInlineImageData reports whether body_md carries a real image inlined as a
// data:image URI. The editor uploads images to object storage and embeds only a small
// reference URL, so a large data: image means a broken or outdated client. Rejecting
// keys on the URI's LENGTH, not its mere presence, so a short illustrative snippet is
// not mistaken for a megabyte of inlined bytes.
func hasInlineImageData(body string) bool {
	// One alternative matches per hit, so exactly one of the two capture groups is
	// non-empty; check both lengths against the cap.
	for _, m := range inlineImageDataURIRE.FindAllStringSubmatch(body, -1) {
		if len(m[1]) > maxInlineImageDataLen || len(m[2]) > maxInlineImageDataLen {
			return true
		}
	}
	return false
}

// gcNoteImages reclaims images this committed edit stopped keeping alive — called
// after a body_md change with the body before and after. Liveness is by REFERENCE,
// not ownership: an image is deleted only when NO note still embeds it, so an image
// copied into a second note survives its owner dropping (or hard-deleting) it. Two
// sources of candidates:
//   - images this note OWNS that its new body no longer references (also covers an
//     upload that was never embedded), and
//   - references this edit DROPPED that point at an image OWNED BY ANOTHER note
//     (content copied between notes, then removed here) — without this, the last
//     note to drop such a reference would leak it, since owner-scoped GC never looks
//     at it again.
//
// Either way a reference this edit just removed is NOT reclaimed on the spot — see
// unreferencedDue for the undo window that keeps a Ctrl+Z from resurrecting a 404.
//
// Best-effort and never fatal: rows go first so a failed bucket delete degrades to a
// harmless orphan (swept by reconciliation) rather than a dangling row. No audit
// event — the note.update body_md diff is the trail.
func (s *Service) gcNoteImages(ctx context.Context, noteID, oldBody, newBody string) {
	if s.blob == nil {
		return
	}
	// Resolve the candidate set AND delete the rows inside one write transaction. The DB
	// runs with _txlock=immediate over a single connection, so the "is this still
	// referenced by another note?" scan and the row delete are atomic against any
	// concurrent note save: a reference copied into another note either commits before
	// this transaction (and the scan spares the image) or after it (a reference to an
	// already-deleted image — inherent, not a race this GC can lose). Running the scan
	// and the delete as two separate statements on s.db left a window where such a copy
	// slipped between them and a still-live image was deleted out from under it.
	var stale []string
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		// referencedNow comes from the note's CURRENT committed body, not the newBody this
		// edit produced. gcNoteImages runs AFTER the update transaction commits, so a
		// concurrent save may have (re-)referenced an image in the gap; reading it back
		// inside this single-connection immediate-lock transaction makes the check
		// authoritative. Trusting the stale newBody could reclaim an image the live body
		// still points at — a permanent 404, since the bucket has no versioning. oldBody
		// stays the pre-edit set: it is what the undo window and the dropped-cross-note-
		// reference scan below are defined against.
		//
		// v9: deliberately the UNSCOPED load (leak table row 17). Image GC is
		// visibility-blind by design — it reasons about keys and bytes, and it runs
		// after a commit that may have been an admin purging a foreign private note,
		// where the actor is not the owner. A viewer-scoped read here would silently
		// stop reclaiming private images. Nothing it reads reaches a response, and
		// nothing it logs may grow a title.
		current, err := s.store.GetNoteAnyScope(ctx, tx, noteID)
		if err != nil {
			return err
		}
		currentBody := newBody // note vanished (a racing hard delete); its rows cascade away anyway
		if current != nil {
			currentBody = deref(current.BodyMD)
		}
		referencedNow := referencedImageIDs(currentBody)
		owned, err := s.store.NoteImagesForNote(ctx, tx, noteID)
		if err != nil {
			return err
		}
		now := time.Now()
		cutoff := now.Add(-s.imgOpts.GCGrace)
		referencedBefore := referencedImageIDs(oldBody)

		considered := map[string]bool{}
		var provisional []string

		// 1. Images this note owns but no longer references. The grace window spares a
		// just-uploaded image whose embedding reference this save hasn't caught up to yet.
		for _, im := range owned {
			if referencedNow[im.ID] {
				continue
			}
			considered[im.ID] = true
			if s.imgOpts.GCGrace > 0 {
				if t, perr := time.Parse(tsFormat, im.CreatedAt); perr == nil && t.After(cutoff) {
					continue
				}
			}
			provisional = append(provisional, im.ID)
		}

		// 2. References this edit dropped that point at another note's image. The
		// created-at grace above does not apply — a dropped reference was in the old
		// body, so the image was already embedded, not a fresh in-flight upload — but
		// the undo window below does.
		for id := range referencedBefore {
			if referencedNow[id] || considered[id] {
				continue
			}
			provisional = append(provisional, id)
		}

		// "Is it still referenced by SOME OTHER note?" needs an instr scan over every
		// note body — the one expensive query in this transaction. Run it ONLY when this
		// edit actually produced candidates; the overwhelmingly common save touches no
		// image at all, and it must not pay for a full-table scan. When the scan does run
		// it is still inside this single-connection immediate-lock transaction, so it stays
		// atomic against a concurrent note (re-)referencing an image (see the tx comment).
		var candidates []string
		if len(provisional) > 0 {
			otherRefs, err := s.otherNotesImageRefs(ctx, tx, noteID)
			if err != nil {
				return err
			}
			for _, id := range provisional {
				if _, ok := otherRefs[id]; ok {
					continue // still embedded elsewhere — liveness is by reference, not ownership
				}
				candidates = append(candidates, id)
			}
		}

		// Hold back anything this edit only just unreferenced: undo puts the reference
		// straight back, and the purge is permanent. This runs even with no candidates —
		// a re-reference (undo) yields none but is exactly the save whose undo-window
		// clock must be cleared (see unreferencedDue).
		candidates = s.unreferencedDue(candidates, referencedBefore, referencedNow, now)
		if len(candidates) == 0 {
			return nil
		}
		if err := s.store.DeleteNoteImages(ctx, tx, candidates); err != nil {
			return err
		}
		stale = candidates
		return nil
	})
	// On any error, spare everything (abort GC): a transient DB failure must never
	// delete a maybe-live object. The committed row delete is what authorizes the purge,
	// so a failed bucket delete afterwards degrades to a harmless orphan (swept by
	// reconciliation) rather than a dangling row.
	if err != nil {
		s.logger.Warn("notes: image GC transaction failed; skipping", "note_id", noteID, "err", err)
		return
	}
	s.purgeImageObjects(ctx, stale)
}

// unrefPruneAt is how many undo-window clocks may accumulate before the stale ones
// (images whose note was never saved again, so no later GC pass consumed the entry)
// are swept. Small: a handful of dropped images per editing session is the norm.
const unrefPruneAt = 256

// unrefForget is how old a clock must be to be pruned — comfortably past any GCGrace,
// and by then the reconciliation pass owns the image anyway.
const unrefForget = 24 * time.Hour

// unreferencedDue narrows edit-time GC candidates to those that have been unreferenced
// for at least GCGrace. A candidate the PREVIOUS body still referenced is one this edit
// just dropped — exactly what an undo puts back — so the drop only STARTS the clock
// instead of deleting: purging it here would leave the restored `![](…)` pointing at a
// permanent 404 (the row goes, the object goes, and the bucket has no versioning). Such
// an image is reclaimed by a later save once the window has passed, or by the
// reconciliation pass if the note is never saved again. A candidate NO body referenced
// (an upload whose embedding save never landed) has nothing to undo back to and is
// returned straight away — the created-at grace in gcNoteImages is what covers it while
// its embed is still in flight.
//
// The clocks are in-memory and best-effort: a restart forgets them, and the next save
// then treats the image as long-unreferenced. That degrades to the previous behavior
// (immediate reclaim) rather than to a leak.
func (s *Service) unreferencedDue(candidates []string, referencedBefore, referencedNow map[string]bool, now time.Time) []string {
	s.unrefMu.Lock()
	defer s.unrefMu.Unlock()

	// An image that came back (undo, or a re-paste of the same reference) gets a fresh
	// window if it is dropped again. This runs even with nothing to reclaim — the save
	// that restores a reference has no candidates at all, and it is precisely the one
	// whose clock must be cleared.
	for id := range referencedNow {
		delete(s.unrefSince, id)
	}
	if len(s.unrefSince) > unrefPruneAt {
		for id, t := range s.unrefSince {
			if now.Sub(t) > unrefForget {
				delete(s.unrefSince, id)
			}
		}
	}

	due := make([]string, 0, len(candidates))
	for _, id := range candidates {
		if referencedBefore[id] {
			if _, running := s.unrefSince[id]; !running {
				s.unrefSince[id] = now // this edit dropped it — start the clock
			}
		}
		if since, running := s.unrefSince[id]; running && now.Sub(since) < s.imgOpts.GCGrace {
			continue // still inside the undo window
		}
		delete(s.unrefSince, id)
		due = append(due, id)
	}
	return due
}

// otherNotesImageRefs maps each image id referenced by SOME note other than excludeID
// to one such note's id (the reassignment target for a hard delete). Built from a
// single query (see Store.NotesReferencingAnyImage) so GC and hard delete don't run an
// instr full-table scan per image.
func (s *Service) otherNotesImageRefs(ctx context.Context, q DBTX, excludeID string) (map[string]string, error) {
	rows, err := s.store.NotesReferencingAnyImage(ctx, q, excludeID)
	if err != nil {
		return nil, err
	}
	refs := map[string]string{}
	for _, r := range rows {
		for id := range referencedImageIDs(r.Body) {
			if _, ok := refs[id]; !ok {
				refs[id] = r.ID
			}
		}
	}
	return refs, nil
}

// purgeImageObjects best-effort deletes the given image objects from storage. A
// failure leaves them as orphans the reconciliation pass reclaims.
func (s *Service) purgeImageObjects(ctx context.Context, ids []string) {
	if s.blob == nil || len(ids) == 0 {
		return
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = NoteImageKey(id)
	}
	if err := s.blob.Delete(ctx, keys...); err != nil {
		s.logger.Warn("notes: deleting image objects failed; left for reconciliation", "count", len(keys), "err", err)
	}
}
