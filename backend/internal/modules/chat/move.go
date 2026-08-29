package chat

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
)

// Přesunout do Dokumentů — the custody transfer (FR-V10-14, D238/D245/D246).
//
// ⚠ THIS IS THE ONLY PATH IN v10 THAT CAN DESTROY DATA SILENTLY, and everything
// about its shape is chosen for that. Five steps, two SQLite writes and two
// object-store calls, and NO TRANSACTION COVERS ALL FIVE — none can. So the
// ordering is picked so that every crash point leaves the bytes OVER-COUNTED
// rather than lost:
//
//	crash after 2 → bytes in both places, no document row.  An unattributed object
//	                under `documents/`, reported by v9's machinery and never
//	                auto-cleaned. Re-running the move is safe.
//	crash after 3 → the document exists, the attachment is still `live`. In
//	                Dokumenty AND still counted against chat. Visible, re-runnable.
//	crash after 4 → the attachment is `moved`, the chat object is still there.
//	                Chat counts bytes it no longer owns; the drain removes them.
//
// ⚠ NEVER DELETE BEFORE THE COPY IS CONFIRMED. NEVER MARK `moved` BEFORE THE
// DOCUMENT ROW EXISTS. Both inversions lose the file, and neither would fail a
// test that only checks the happy path — which is why the fault-injection matrix
// is an acceptance criterion rather than a nice-to-have.
//
// ⚠ AND A MOVE IS A PUBLISH (D245). The file becomes readable by every household
// member, INCLUDING people who are not in this conversation. That is inherent in
// the requirement that a moved file keep rendering in the thread — its members must
// be able to read it, and the only place in Home where that is true without
// inventing a third `visibility` is the shared tree. The UI states it in words
// before confirming; the server's part is refusing a PRIVATE target, which would
// make the file unreadable to exactly the people the move exists to keep it
// readable for.

// moveStep names the five steps, for the fault-injection seam.
type moveStep int

const (
	// stepValidate covers this module's own guards; the sink runs its own.
	stepValidate moveStep = iota + 1
	// stepCopy and stepInsert happen INSIDE the sink and are injected by a fake
	// sink rather than by the hook — they are not chat's code to fail.
	stepCopy
	stepInsert
	stepMark
	stepDelete
)

func (s *Service) faultAt(step moveStep) error {
	if s.moveFault == nil {
		return nil
	}
	return s.moveFault(step)
}

// MoveAttachment hands one attachment's bytes to Dokumenty.
//
// Gated member ∧ (editor | admin) — the same gate as the rest of the clean-up
// surface (D241), because this is one of its two actions.
func (s *Service) MoveAttachment(ctx context.Context, attachmentID, folderID string) (Attachment, error) {
	actor := actorID(ctx)
	if actor == "" {
		return Attachment{}, httpx.ErrUnauthorized("")
	}
	// ⚠ 501 AND NOT A FALLBACK TO DELETE (D239). A capability that silently becomes
	// a different, destructive capability is worse than one that is plainly absent —
	// and the UI renders no button at all, so reaching this is a client that has
	// been told otherwise or a deploy that lost its wiring.
	if s.blobSink == nil {
		return Attachment{}, httpx.ErrNotImplemented("Přesun do Dokumentů není v tomto nasazení dostupný.")
	}
	if err := s.assertCleanupGate(ctx); err != nil {
		return Attachment{}, err
	}
	if strings.TrimSpace(folderID) == "" {
		return Attachment{}, httpx.ErrUnprocessable("Vyberte cílovou složku v Dokumentech.")
	}

	// 1. VALIDATE, on this side: membership, the floor, the koš, and that the
	// attachment is still live. The SINK validates the target folder — that is its
	// half of the same step, and it must not be duplicated here, because `chat` has
	// no idea what makes a Dokumenty folder writable.
	att, err := s.store.AttachmentForViewer(ctx, s.db, actor, attachmentID)
	if err != nil {
		return Attachment{}, mapScopeErr(err)
	}
	if att.State != stateLive {
		return Attachment{}, alreadyCleaned(att.State)
	}
	if err := s.faultAt(stepValidate); err != nil {
		return Attachment{}, err
	}

	// 2 + 3. COPY, then INSERT — both inside the sink, in that order, in its own
	// transaction. It never touches our object: deleting it is step 5 and it is ours.
	result, err := s.blobSink.AcceptBlob(ctx, storage.AcceptRequest{
		SourceKey:   att.StorageKey,
		FolderID:    folderID,
		Filename:    att.OriginalFilename,
		ContentType: att.ContentType,
		ByteSize:    att.ByteSize,
		Checksum:    att.Checksum,
		Via:         "chat",
	})
	if err != nil {
		s.logger.Warn("chat: the move stopped at the sink — the attachment is untouched",
			"attachment", attachmentID, "folder", folderID, "err", err)
		return Attachment{}, err
	}
	s.logger.Info("chat: attachment accepted by documents",
		"attachment", attachmentID, "document", result.DocumentID, "bytes", att.ByteSize)

	// 4. MARK. Its own transaction, its own audit event. From here on the document
	// exists, so the worst remaining outcome is chat counting bytes it has given
	// away — which the next drain pass corrects.
	if err := s.faultAt(stepMark); err != nil {
		return Attachment{}, err
	}
	// ⚠ MINTED ONCE, OUTSIDE THE TX, AND REUSED IN THE RESPONSE. The row and the
	// answer have to carry the same instant: a second nowUTC() for the rendered
	// attachment would differ by however long the transaction took, and leaving it
	// off entirely — which the first version did — serialised `cleaned_at: null` on
	// a `moved` attachment whose row had one. The bubble's marker then had no date
	// until something refetched the thread.
	cleanedAt := nowUTC()
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		name, err := s.store.ConversationName(ctx, tx, att.ConversationID)
		if err != nil {
			return err
		}
		moved, err := s.store.MarkAttachmentMoved(ctx, tx, attachmentID,
			result.DocumentID, result.Path, actor, cleanedAt)
		if err != nil {
			return err
		}
		if !moved {
			// Somebody removed or moved it between the load and here. The document
			// that was just created stays — deleting it would be this module reaching
			// into another's tree — and the caller is told the attachment is gone.
			return alreadyCleaned(stateMoved)
		}
		return s.recordAttachment(ctx, tx, "attachment.moved", attachmentID,
			fmt.Sprintf("Soubor „%s“ přesunut z konverzace „%s“ do Dokumentů", att.OriginalFilename, name),
			[]audit.Change{
				{Field: "state", Old: audit.Ptr(stateLive), New: audit.Ptr(stateMoved)},
				{Field: "document_id", New: audit.Ptr(result.DocumentID)},
				{Field: "conversation", New: audit.Ptr(name)},
			})
	})
	if err != nil {
		return Attachment{}, err
	}

	// 5. DELETE THE CHAT OBJECT, LAST, and enqueue on failure.
	//
	// ⚠ The bytes now have a second, durable home and a row pointing at it, so this
	// is the first moment in the sequence at which destroying the original is not a
	// risk. A failure here is not an error the caller should see: the move HAPPENED,
	// the file is in Dokumenty, and what is left is bytes chat no longer owns —
	// which is exactly what the drain is for.
	if err := s.faultAt(stepDelete); err != nil {
		s.queueOrphan(ctx, att.objectKeys())
	} else if s.blob == nil {
		s.queueOrphan(ctx, att.objectKeys())
	} else if err := s.blob.Delete(ctx, att.objectKeys()...); err != nil {
		s.logger.Warn("chat: the moved object could not be deleted — queued for the drain",
			"attachment", attachmentID, "err", err)
		s.queueOrphan(ctx, att.objectKeys())
	}

	labels, err := s.labels(ctx)
	if err != nil {
		labels = map[string]string{}
	}
	att.State = stateMoved
	att.DocumentID = sql.NullString{String: result.DocumentID, Valid: true}
	att.DocumentPath = sql.NullString{String: result.Path, Valid: result.Path != ""}
	att.CleanedBy = sql.NullString{String: actor, Valid: true}
	att.CleanedAt = sql.NullString{String: cleanedAt, Valid: true}
	out := att.wire(labels)
	s.publishAttachmentChanged(ctx, actor, att)
	return out, nil
}

// queueOrphan promises the drain a key whose owner has already let go of it.
//
// It runs on its own connection, outside any transaction — the mark has already
// committed and this is repair, so a failure here is logged and dropped rather than
// unwinding a completed move. The object is then an orphan: reported by
// StorageBlobs as `unattributed`, visible on the storage page, never auto-cleaned.
func (s *Service) queueOrphan(ctx context.Context, keys []string) {
	now := nowUTC()
	if err := s.store.QueueKeys(ctx, s.db, keys, now, now); err != nil {
		s.logger.Error("chat: could not queue an orphaned object for the drain",
			"keys", keys, "err", err)
	}
}

// alreadyCleaned is the refusal for an attachment that has already been dealt with.
//
// ⚠ IT IS 422 RATHER THAN 404, and the difference is deliberate: the caller CAN see
// this attachment — it is in their thread, rendered as an epitaph or as a moved file
// — so hiding it would be a lie about a row they are looking at. 404 is reserved for
// the membership refusal, where the point is that the id may not exist at all.
func alreadyCleaned(state string) error {
	if state == stateMoved {
		return httpx.ErrUnprocessable("Soubor už byl přesunut do Dokumentů.")
	}
	return httpx.ErrUnprocessable("Soubor už byl odstraněn.")
}

// publishAttachmentChanged re-publishes the BUBBLE an attachment belongs to.
//
// ⚠ A `chat_conversation.changed` FRAME IS NOT ENOUGH, AND SENDING ONLY THAT LEFT
// EVERY OTHER MEMBER LOOKING AT A DEAD FILE. That frame means "refetch this room",
// and the client answers it by invalidating the conversation and the two listings —
// deliberately NOT the thread, because a room-level change is not a message change.
// So a removal left other members' open threads still rendering the attachment, with
// `/raw` now 404 because the object was deleted inline, and the epitaph — filename,
// size, who, when — appearing only for the person who clicked. D243's whole point is
// that the thread stays legible for everybody else.
//
// The right frame already exists: an attachment state change IS a message change, so
// this publishes `chat_message.updated` with the re-rendered message, which
// replaceMessage on the client already knows how to apply.
//
// ⚠ THE AUDIENCE IS MemberIDsAbove, NOT MemberIDs, and the distinction is the floor
// (D218). This is an OLD message: somebody added to the room afterwards is bounded
// off it by every read path, and publishing its body to them would hand their socket
// exactly what the floor exists to withhold. EditMessage learned this in PR 2; the
// clean-up verbs are the same shape.
func (s *Service) publishAttachmentChanged(ctx context.Context, actor string, att attachmentRow) {
	// The acting member's own scope, which is the one that produced the row this is
	// about — so the re-render goes through the same floor every other read of it
	// does, and a reply quote is resolved exactly as EditMessage resolves one.
	sc, err := s.store.memberScope(ctx, s.db, actor, att.ConversationID)
	if err != nil {
		s.logger.Warn("chat: could not resolve a scope for an attachment frame",
			"conversation", att.ConversationID, "err", err)
		return
	}
	messageID := att.MessageID
	audience, err := s.store.MemberIDsAbove(ctx, s.db, sc.ConversationID, messageID)
	if err != nil {
		s.logger.Warn("chat: could not resolve the audience for an attachment frame",
			"conversation", sc.ConversationID, "message", messageID, "err", err)
		return
	}
	if len(audience) == 0 {
		return
	}
	labels, err := s.labels(ctx)
	if err != nil {
		labels = map[string]string{}
	}
	row, err := s.store.MessageByID(ctx, s.db, sc, messageID)
	if err != nil {
		s.logger.Warn("chat: could not re-render a message after a cleanup",
			"message", messageID, "err", err)
		return
	}
	rendered, err := s.renderMessages(ctx, s.db, sc, []messageRow{row}, labels)
	if err != nil || len(rendered) == 0 {
		s.logger.Warn("chat: could not re-render a message after a cleanup",
			"message", messageID, "err", err)
		return
	}
	// No prev_message_id: this does not extend the thread, so there is no gap for a
	// client to detect and nothing to compare against (the EditMessage rule).
	s.notifyTo(ctx, audience, "chat_message.updated", MessageEvent{
		ConversationID: sc.ConversationID, Message: rendered[0],
	})
	// The room's own figures moved too — its byte total and its over-limit flag —
	// and those live on the conversation, not on the bubble.
	if members, err := s.store.MemberIDs(ctx, s.db, sc.ConversationID); err == nil {
		s.publishConversation(ctx, members, sc.ConversationID, false)
	}
}

// assertCleanupGate is D241, in one place.
//
// ⚠ THE ONLY PLACE A ROLE IS CONSULTED IN THIS MODULE. Chat is the first module in
// Home where a `reader` writes (D222) — they post, reply, edit, create rooms and
// manage membership — and the single exception is cleaning up storage. The
// asymmetry is recorded rather than resolved: a reader can fill storage they can
// never clean, and an editor in the same conversation has to do it for them. If it
// ever bites, the fix is to drop this gate, not to remove readers from chat.
//
// ⚠ AND IT GATES ON THE ROLE ONLY. "Member of the conversation" is the OTHER half
// of the intersection and it is enforced per row — by the listing's join and by
// AttachmentForViewer — because a member of NO conversation must get an empty page
// with an explanation rather than a 403: the gate passed, there is simply nothing
// to clean.
func (s *Service) assertCleanupGate(ctx context.Context) error {
	if !writeAllowedCtx(ctx) {
		return httpx.ErrForbidden(
			"Úklid úložiště mohou provádět jen členové s právem zápisu. Požádejte někoho z konverzace.")
	}
	return nil
}

