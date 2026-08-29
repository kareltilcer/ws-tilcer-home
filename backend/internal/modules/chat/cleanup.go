package chat

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
)

// Úklid úložiště chatu — the clean-up listing and *Odstranit* (FR-V10-13/14,
// D241/D242/D243/D246).
//
// ⚠ THREE ACTIONS, AND ONE OF THEM IS THE ABSENCE OF A BUTTON (D242). *Ponechat*
// stages nothing and queues nothing: closing the page is a valid outcome, and
// "not every document has to be dealt with at that moment" is a statement about
// STATE, not about a control. There is no endpoint for it here and there must not
// be one — the only two verbs are DELETE (below) and the move (move.go).
//
// ⚠ A MOVED OR REMOVED ROW IS GONE FROM THIS LISTING ON THE NEXT LOAD (D246), not
// struck through and not greyed. The listing is *what still counts*, which is what
// makes "clean until the number goes down" a workflow rather than a guess.

// CleanupItem is one row of the page.
type CleanupItem struct {
	Attachment            Attachment `json:"attachment"`
	ConversationID        string     `json:"conversation_id"`
	ConversationName      string     `json:"conversation_name"`
	ConversationOverLimit bool       `json:"conversation_over_limit"`
	UploadedByLabel       string     `json:"uploaded_by_label"`
}

// CleanupPage is the listing.
//
// ⚠ TotalBytes COVERS EVERY MATCHING ITEM, NOT THIS PAGE. It is the figure the
// screen exists to act on — a per-page total under `sort=size` would fall every
// time somebody scrolled, which is the opposite of the signal the page is for.
type CleanupPage struct {
	Items      []CleanupItem `json:"items"`
	NextCursor *string       `json:"next_cursor"`
	TotalBytes *int64        `json:"total_bytes"`
}

// Sort orders. `size` is the default because that is the order in which cleaning
// pays.
const (
	sortSize   = "size"
	sortRecent = "recent"
)

// Cleanup lists live attachments from the caller's own conversations.
//
// ⚠ THE GATE IS AN INTERSECTION: member ∧ (editor | admin) (D241). The ROLE half is
// checked here and refuses with 403; the MEMBER half is the listing's own join and
// produces an EMPTY page rather than a refusal — a member of no conversation has
// passed the gate and simply has nothing to clean, which is a different sentence
// from "you may not do this" and gets a different screen.
func (s *Service) Cleanup(ctx context.Context, conversationID, sort, cursor string, limit int) (CleanupPage, error) {
	actor := reqctx.ActorID(ctx)
	if actor == "" {
		return CleanupPage{}, httpx.ErrUnauthorized("")
	}
	if err := s.assertCleanupGate(ctx); err != nil {
		return CleanupPage{}, err
	}
	switch sort {
	case "", sortSize:
		sort = sortSize
	case sortRecent:
	default:
		return CleanupPage{}, httpx.ErrUnprocessable("Parametr sort musí být size nebo recent.")
	}
	// ⚠ A CURSOR UNDER `sort=size` IS REFUSED, NOT IGNORED. The keyset cursor is an
	// id and an id does not locate a position in a size ordering, so honouring it
	// would page over the wrong rows and ignoring it would serve page one forever —
	// which reads as the end of the list. The §V9 `private-items` precedent, and the
	// same answer chat's own search gives a cursor under a rank ordering.
	if sort == sortSize && cursor != "" {
		return CleanupPage{}, httpx.ErrUnprocessable(
			"Řazení podle velikosti se nestránkuje — kurzor zde nelze použít.")
	}

	thresholds, err := storage.LoadThresholds(ctx, s.db)
	if err != nil {
		return CleanupPage{}, err
	}
	labels, err := s.labels(ctx)
	if err != nil {
		return CleanupPage{}, err
	}
	rows, hasMore, total, err := s.store.CleanupItems(ctx, s.db, actor, conversationID, sort, cursor, limit)
	if err != nil {
		return CleanupPage{}, err
	}
	overLimit, err := s.store.ConversationsOverLimit(ctx, s.db, actor,
		storage.MB(thresholds.Conversation.ValueMB))
	if err != nil {
		return CleanupPage{}, err
	}

	page := CleanupPage{Items: make([]CleanupItem, 0, len(rows)), TotalBytes: &total}
	for _, r := range rows {
		page.Items = append(page.Items, CleanupItem{
			Attachment:            r.attachment.wire(labels),
			ConversationID:        r.attachment.ConversationID,
			ConversationName:      r.conversationName,
			ConversationOverLimit: overLimit[r.attachment.ConversationID],
			UploadedByLabel:       label(labels, r.attachment.UploadedBy),
		})
	}
	if hasMore && len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1].Attachment.ID
		page.NextCursor = &last
	}
	return page, nil
}

// RemoveAttachment is *Odstranit* (D243/D247).
//
// ⚠ IT DELETES THE OBJECT INLINE, AND IT IS THE ONLY PATH IN THE MODULE THAT DOES.
// Every other destructive path enqueues into chat_deleted_keys for the 15-minute
// drain, because a conversation with four hundred attachments is not a thing to do
// inside a request. This one is the opposite case: the workflow is *clean until the
// number goes down*, and a figure lagging fifteen minutes behind the button makes
// the page unusable. One or two objects per click is cheap.
//
// ⚠ THE ROW SURVIVES AND KEEPS ITS FILENAME AND SIZE (D243). The bubble then renders
// the epitaph, so the thread stays legible, a member can ask for the file again
// knowing exactly what it was, and the clean-up is attributed. Only the bytes go.
func (s *Service) RemoveAttachment(ctx context.Context, attachmentID string) error {
	actor := reqctx.ActorID(ctx)
	if actor == "" {
		return httpx.ErrUnauthorized("")
	}
	if err := s.assertCleanupGate(ctx); err != nil {
		return err
	}
	att, err := s.store.AttachmentForViewer(ctx, s.db, actor, attachmentID)
	if err != nil {
		return mapScopeErr(err)
	}
	if att.State != stateLive {
		return alreadyCleaned(att.State)
	}

	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		name, err := s.store.ConversationName(ctx, tx, att.ConversationID)
		if err != nil {
			return err
		}
		removed, err := s.store.MarkAttachmentRemoved(ctx, tx, attachmentID, actor, nowUTC())
		if err != nil {
			return err
		}
		if !removed {
			// A concurrent click got there first. Refused rather than repeated, so the
			// Log gets one event and the object gets one delete.
			return alreadyCleaned(stateRemoved)
		}
		return s.recordAttachment(ctx, tx, "attachment.removed", attachmentID,
			fmt.Sprintf("Soubor „%s“ odstraněn při úklidu úložiště konverzace „%s“",
				att.OriginalFilename, name),
			[]audit.Change{
				{Field: "state", Old: audit.Ptr(stateLive), New: audit.Ptr(stateRemoved)},
				{Field: "byte_size", Old: audit.Ptr(fmt.Sprint(att.ByteSize))},
				{Field: "conversation", New: audit.Ptr(name)},
			})
	})
	if err != nil {
		return err
	}

	// The bytes, now — with the queue as the fallback rather than the mechanism.
	if s.blob == nil {
		s.queueOrphan(ctx, att.objectKeys())
	} else if err := s.blob.Delete(ctx, att.objectKeys()...); err != nil {
		s.logger.Warn("chat: inline delete failed — queued for the drain",
			"attachment", attachmentID, "err", err)
		s.queueOrphan(ctx, att.objectKeys())
	}
	s.publishAttachmentChanged(ctx, actor, att)
	return nil
}

// writeAllowedCtx is the `editor | admin` half of D241's gate.
//
// ⚠ IT IS THE ONLY ROLE CHECK IN THIS MODULE. Everywhere else the answer is
// membership, in SQL (scope.go). A second one appearing anywhere in `chat` is
// almost certainly a mistake — D222 is explicit that a reader posts, replies,
// edits, creates rooms and manages membership.
//
// The check itself is reqctx.CanWrite, shared with `documents` and `notes` — the
// only other two modules that ask this question at all. The named wrapper stays
// so the warning above has something to sit on.
func writeAllowedCtx(ctx context.Context) bool { return reqctx.CanWrite(ctx) }
