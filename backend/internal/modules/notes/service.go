package notes

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
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
	db     *sql.DB
	store  *Store
	sink   audit.Sink
	notify Notifier
}

func NewService(db *sql.DB, sink audit.Sink, notify Notifier) *Service {
	if notify == nil {
		notify = func(context.Context, string, any) {}
	}
	return &Service{db: db, store: NewStore(db), sink: sink, notify: notify}
}

// Store exposes the read store (used by the widget provider).
func (s *Service) Store() *Store { return s.store }

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
		Module:     audit.ModuleNotes,
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
// BOTH tables (the cross-table check + the two COALESCE indexes together are the
// addressing invariant, D32). Runs inside the mutation's tx.
func (s *Service) freeSlug(ctx context.Context, tx DBTX, parentScope *string, base, excludeFolderID, excludeNoteID string) (string, error) {
	if base == "" {
		base = shortID()
	}
	candidate := base
	for i := 2; ; i++ {
		taken, err := s.store.SiblingSlugTaken(ctx, tx, parentScope, candidate, excludeFolderID, excludeNoteID)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}

// ---- Notes ----

func (s *Service) CreateNote(ctx context.Context, in NoteCreate) (*NoteDetail, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, httpx.ErrUnprocessable("title is required")
	}
	var out *Note
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.assertFolder(ctx, tx, in.FolderID); err != nil {
			return err
		}
		sl, err := s.freeSlug(ctx, tx, in.FolderID, slug.Make(title), "", "")
		if err != nil {
			return err
		}
		pos, err := s.store.lastNotePosition(ctx, tx, in.FolderID)
		if err != nil {
			return err
		}
		out, err = s.store.InsertNote(ctx, tx, in.FolderID, title, sl, in.BodyMD, pos, actorID(ctx))
		if err != nil {
			return err
		}
		changes := []audit.Change{{Field: "title", New: ap(title)}, {Field: "slug", New: ap(sl)}}
		if in.BodyMD != "" {
			changes = append(changes, audit.Change{Field: "body_md", New: ap(in.BodyMD)})
		}
		return s.record(ctx, tx, "note.create", "note", out.ID,
			fmt.Sprintf("Vytvořena poznámka „%s“", title), changes, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify(ctx, "note.changed", map[string]string{"id": out.ID})
	return s.noteDetail(ctx, s.db, out)
}

func (s *Service) GetNoteDetail(ctx context.Context, id string) (*NoteDetail, error) {
	n, err := s.store.GetNote(ctx, s.db, id)
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
	var out *Note
	var changed bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetNote(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("note not found")
		}
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
		}
		titleChanged := false
		if in.Title != nil {
			if title := strings.TrimSpace(*in.Title); title != before.Title {
				titleChanged = true
				patch.Title = &title
				// re-derive the slug from the new title, unique in the current parent
				sl, err := s.freeSlug(ctx, tx, before.FolderID, slug.Make(title), "", before.ID)
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
			sl, err := s.freeSlug(ctx, tx, before.FolderID, before.Slug, "", before.ID)
			if err != nil {
				return err
			}
			patch.Slug = &sl
		}
		if err := s.store.UpdateNote(ctx, tx, id, patch); err != nil {
			return err
		}
		out, err = s.store.GetNote(ctx, tx, id)
		if err != nil {
			return err
		}
		var changes []audit.Change
		diff(&changes, "title", ap(before.Title), ap(out.Title))
		diff(&changes, "slug", ap(before.Slug), ap(out.Slug))
		diff(&changes, "body_md", before.BodyMD, out.BodyMD) // full, untruncated (D36)
		diff(&changes, "archived", ap(fmt.Sprint(before.Archived)), ap(fmt.Sprint(out.Archived)))
		// A no-op PATCH leaves no audit trail and doesn't broadcast. The patch above
		// carries only genuinely-changed fields, so an empty diff means the store wrote
		// nothing and updated_at didn't move — there's nothing to tell other clients.
		if len(changes) == 0 {
			return nil
		}
		changed = true
		return s.record(ctx, tx, "note.update", "note", id,
			fmt.Sprintf("Upravena poznámka „%s“", out.Title), changes, metaVia(nil, via))
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notify(ctx, "note.changed", map[string]string{"id": id})
	}
	return s.noteDetail(ctx, s.db, out)
}

func (s *Service) MoveNote(ctx context.Context, id string, in NoteMoveRequest, via string) (*NoteDetail, error) {
	if strings.TrimSpace(in.Position) == "" {
		return nil, httpx.ErrUnprocessable("position is required")
	}
	var out *Note
	var changed bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetNote(ctx, tx, id)
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
		if err := s.assertFolder(ctx, tx, in.FolderID); err != nil {
			return err
		}
		// keep the current slug if it's free in the target parent, else re-derive
		sl, err := s.freeSlug(ctx, tx, in.FolderID, before.Slug, "", before.ID)
		if err != nil {
			return err
		}
		// A move that re-sends the note's current folder/position/slug changes nothing:
		// skip the write (so updated_at doesn't drift), the audit event, and the
		// broadcast — mirroring UpdateNote's no-op gating. Otherwise a re-sent move
		// would log a contentless note.move and make every other client refetch and
		// raise a bogus "changed elsewhere" toast for a change that never happened.
		if eqp(before.FolderID, in.FolderID) && before.Position == in.Position && before.Slug == sl {
			out = before
			return nil
		}
		if err := s.store.MoveNoteRow(ctx, tx, id, in.FolderID, in.Position, sl); err != nil {
			return err
		}
		out, err = s.store.GetNote(ctx, tx, id)
		if err != nil {
			return err
		}
		var changes []audit.Change
		diff(&changes, "folder_id", before.FolderID, out.FolderID)
		diff(&changes, "slug", ap(before.Slug), ap(out.Slug))
		diff(&changes, "position", ap(before.Position), ap(out.Position))
		changed = true
		return s.record(ctx, tx, "note.move", "note", id,
			fmt.Sprintf("Přesunuta poznámka „%s“", out.Title), changes, metaVia(nil, via))
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notify(ctx, "note.changed", map[string]string{"id": id})
	}
	return s.noteDetail(ctx, s.db, out)
}

func (s *Service) DeleteNote(ctx context.Context, id string, hard bool) error {
	var changed bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetNote(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("note not found")
		}
		if hard {
			if err := s.store.DeleteNote(ctx, tx, id); err != nil {
				return err
			}
			changed = true
			return s.record(ctx, tx, "note.delete", "note", id,
				fmt.Sprintf("Smazána poznámka „%s“", before.Title), nil, metaHard(true))
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
			[]audit.Change{{Field: "archived", Old: ap("false"), New: ap("true")}}, metaHard(false))
	})
	if err != nil {
		return err
	}
	if changed {
		s.notify(ctx, "note.changed", map[string]string{"id": id})
	}
	return nil
}

// ---- Folders ----

func (s *Service) CreateFolder(ctx context.Context, in FolderCreate) (*FolderDetail, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, httpx.ErrUnprocessable("name is required")
	}
	var out *Folder
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
		icon := foldericon.Normalize(in.Icon)
		out, err = s.store.InsertFolder(ctx, tx, in.ParentID, name, sl, pos, actorID(ctx), icon)
		if err != nil {
			return err
		}
		changes := []audit.Change{{Field: "name", New: ap(name)}, {Field: "slug", New: ap(sl)}}
		if icon != "" {
			changes = append(changes, audit.Change{Field: "icon", New: ap(icon)})
		}
		return s.record(ctx, tx, "folder.create", "folder", out.ID,
			fmt.Sprintf("Vytvořena složka „%s“", name), changes, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify(ctx, "folder.changed", map[string]string{"id": out.ID})
	return s.folderDetail(ctx, s.db, out)
}

func (s *Service) GetFolderDetail(ctx context.Context, id string) (*FolderDetail, error) {
	f, err := s.store.GetFolder(ctx, s.db, id)
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
			sl, err := s.freeSlug(ctx, tx, before.ParentID, slug.Make(name), before.ID, "")
			if err != nil {
				return err
			}
			if err := s.store.RenameFolder(ctx, tx, id, name, sl); err != nil {
				return err
			}
		} else if unarchiving {
			// Re-free its slug against live siblings before it re-enters the live
			// index (a sibling may have reused it while archived — see UpdateNote).
			sl, err := s.freeSlug(ctx, tx, before.ParentID, before.Slug, before.ID, "")
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
		out, err = s.store.GetFolder(ctx, tx, id)
		if err != nil {
			return err
		}
		var changes []audit.Change
		diff(&changes, "name", ap(before.Name), ap(out.Name))
		diff(&changes, "slug", ap(before.Slug), ap(out.Slug))
		diff(&changes, "icon", ap(before.Icon), ap(out.Icon))
		diff(&changes, "archived", ap(fmt.Sprint(before.Archived)), ap(fmt.Sprint(out.Archived)))
		// A no-op PATCH (nothing actually changed) leaves no audit trail and doesn't
		// broadcast a change that other clients would needlessly refetch on.
		if len(changes) == 0 {
			return nil
		}
		changed = true
		return s.record(ctx, tx, "folder.update", "folder", id,
			fmt.Sprintf("Upravena složka „%s“", out.Name), changes, nil)
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notify(ctx, "folder.changed", map[string]string{"id": id})
	}
	return s.folderDetail(ctx, s.db, out)
}

func (s *Service) MoveFolder(ctx context.Context, id string, in FolderMoveRequest) (*FolderDetail, error) {
	if strings.TrimSpace(in.Position) == "" {
		return nil, httpx.ErrUnprocessable("position is required")
	}
	var out *Folder
	var changed bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetFolder(ctx, tx, id)
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
		if err := s.assertFolder(ctx, tx, in.ParentID); err != nil {
			return err
		}
		// Cycle guard: a folder may not move into itself or a descendant (D31).
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
		// A move that re-sends the folder's current parent/position/slug changes
		// nothing: skip the write, audit, and broadcast — mirroring UpdateFolder's
		// no-op gating (see MoveNote for the spurious-broadcast rationale).
		if eqp(before.ParentID, in.ParentID) && before.Position == in.Position && before.Slug == sl {
			out = before
			return nil
		}
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
		return s.record(ctx, tx, "folder.move", "folder", id,
			fmt.Sprintf("Přesunuta složka „%s“", out.Name), changes, nil)
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notify(ctx, "folder.changed", map[string]string{"id": id})
	}
	return s.folderDetail(ctx, s.db, out)
}

// DeleteFolder soft-deletes by default. A non-empty folder needs cascade=true
// (which soft-deletes the subtree, logging each child); hard=true purges (admin,
// enforced at the route).
func (s *Service) DeleteFolder(ctx context.Context, id string, cascade, hard bool) error {
	var changed bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetFolder(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("folder not found")
		}
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
					map[string]any{"hard": true, "via": "cascade"}); err != nil {
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
					fmt.Sprintf("Smazána složka „%s“", metas[fID].Name), nil, m); err != nil {
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
				[]audit.Change{{Field: "archived", Old: ap("false"), New: ap("true")}},
				map[string]any{"via": "cascade"}); err != nil {
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
				[]audit.Change{{Field: "archived", Old: ap("false"), New: ap("true")}}, m); err != nil {
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
		s.notify(ctx, "folder.changed", map[string]string{"id": id})
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
// every ancestor of a live item must itself be live, since Resolve walks only live
// folders (a live descendant of an archived folder is unreachable). nil = root.
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

func (s *Service) Tree(ctx context.Context, includeArchived bool) (*NotesTree, error) {
	folders, err := s.store.AllFolders(ctx, includeArchived)
	if err != nil {
		return nil, err
	}
	summaries, err := s.store.AllNoteSummaries(ctx, includeArchived)
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
// FTS5 search results when q is set (FR-P6).
func (s *Service) List(ctx context.Context, q string, folderID *string, includeArchived bool) (NotePage, error) {
	q = strings.TrimSpace(q)
	var items []NoteSummary
	var err error
	if q != "" {
		match := ftsQuery(q)
		if match == "" {
			// A punctuation-only query has no searchable tokens. Return an empty result
			// rather than run `notes_fts MATCH ''`, whose empty-phrase match behavior is
			// unspecified and varies across SQLite/FTS5 builds.
			return NotePage{Items: []NoteSummary{}}, nil
		}
		items, err = s.store.SearchNotes(ctx, match, folderID, includeArchived, 100)
	} else {
		items, err = s.store.NoteSummariesInFolder(ctx, s.db, folderID, includeArchived)
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
func (s *Service) Resolve(ctx context.Context, path string) (*ResolveResult, error) {
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
		n, err := s.store.ChildNoteBySlug(ctx, s.db, parent, seg)
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

// ---- Pinning (two scopes, D35) ----

// Pin adds a pin. household requires editor/admin and is audited + broadcast;
// personal is allowed for any member (incl. reader), not audited, not broadcast.
func (s *Service) Pin(ctx context.Context, noteID, scopeStr, via string) (*PinState, error) {
	scope, err := parseScope(scopeStr)
	if err != nil {
		return nil, err
	}
	n, err := s.store.GetNote(ctx, s.db, noteID)
	if err != nil {
		return nil, err
	}
	if n == nil || n.Archived {
		return nil, httpx.ErrNotFound("note not found")
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
				metaVia(map[string]any{"scope": scopeHousehold}, via))
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
	scope, err := parseScope(scopeStr)
	if err != nil {
		return nil, err
	}
	n, err := s.store.GetNote(ctx, s.db, noteID)
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
				metaVia(map[string]any{"scope": scopeHousehold}, via))
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
	subs, err := s.store.ChildFolders(ctx, q, &f.ID, false)
	if err != nil {
		return nil, err
	}
	notes, err := s.store.NoteSummariesInFolder(ctx, q, &f.ID, false)
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

// ancestors returns the folder chain root→…→folderID (inclusive). Empty for a
// root item (folderID nil).
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

// ftsQuery turns a free-text query into a safe FTS5 prefix MATCH. Each whitespace
// token becomes a quoted prefix term so punctuation can't break the FTS syntax.
// Returns "" when the query has no searchable tokens (e.g. punctuation-only) — the
// caller must treat that as "matches nothing" and skip the MATCH entirely.
func ftsQuery(q string) string {
	fields := strings.Fields(q)
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.ReplaceAll(f, `"`, "")
		// A token with no letters/digits (e.g. "#" or "!!!") tokenizes to zero
		// terms; a prefix '*' on that empty phrase is an FTS5 syntax error, so
		// drop it — it can't match anything anyway.
		if !strings.ContainsFunc(f, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) {
			continue
		}
		terms = append(terms, `"`+f+`"*`)
	}
	return strings.Join(terms, " ")
}
