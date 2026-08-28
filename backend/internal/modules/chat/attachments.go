package chat

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
)

// Attachment reads and writes (PR 3).
//
// ⚠ EVERY READ HERE TAKES A Scope, exactly as messages.go does, and for the same
// reason: the bytes are conversation content, so "which attachments" is the same
// question as "which messages" with the same two terms — membership and the floor
// — and a variant that took a bare attachment id would be the second spelling of
// an access rule this module keeps in one place (scope.go).
//
// ⚠ THE ONE EXCEPTION IS EXPLICIT AND NAMED. The drain and the koš purge have NO
// ACTOR — they are a scheduler job, and there is no member whose floor they could
// read through. They use AnyMembership* variants, which say so in their name so
// that "this one skips the join" is a decision on the page rather than an
// oversight three call sites deep (§V10-4a's note about background jobs).

// Attachment states.
const (
	stateLive    = "live"
	stateMoved   = "moved"
	stateRemoved = "removed"
)

// Attachment kinds, decided by SERVER-SNIFFED MIME and never the client's claim
// (D48/D227).
const (
	kindImage = "image"
	kindVideo = "video"
	kindFile  = "file"
)

// Storage keys — `chat/{attachment_id}/original` and `/thumb.webp`, in the primary
// bucket under a new prefix (D229).
//
// ⚠ THE PREFIX IS THE UNIT OF EVERYTHING ELSE: StorageBlobs walks it, the backup
// mirror is scoped away from it (chat blobs are deliberately NOT mirrored), and
// the move's whole "the bytes left the chat/ prefix" argument is about this string.
const (
	keyPrefix   = "chat/"
	keyOriginal = "original"
	keyThumb    = "thumb.webp"
)

func originalKey(attachmentID string) string { return keyPrefix + attachmentID + "/" + keyOriginal }
func thumbnailKey(attachmentID string) string { return keyPrefix + attachmentID + "/" + keyThumb }

// attachmentIDFromKey extracts the id from `chat/{id}/original` and friends. A key
// that does not have that shape yields "", which buckets it as unattributed — the
// honest answer for an object this module does not recognise.
func attachmentIDFromKey(key string) string {
	rest, ok := strings.CutPrefix(key, keyPrefix)
	if !ok {
		return ""
	}
	id, _, _ := strings.Cut(rest, "/")
	return id
}

// attachmentRow is one stored attachment, including the columns the wire type
// deliberately does not carry: the two object keys and the checksum.
//
// ⚠ THE KEYS NEVER LEAVE THE SERVER (the D33 rule, unchanged since v4). There is
// no presigned URL anywhere in Home and this module does not introduce the first
// one; a bubble renders `/api/chat/attachments/{id}/raw`, which is session-gated
// and re-resolves membership on every request.
type attachmentRow struct {
	ID               string
	MessageID        string
	ConversationID   string
	Kind             string
	OriginalFilename string
	ContentType      string
	ByteSize         int64
	Checksum         string
	StorageKey       string
	ThumbnailKey     sql.NullString
	Width            sql.NullInt64
	Height           sql.NullInt64
	State            string
	DocumentID       sql.NullString
	DocumentPath     sql.NullString
	UploadedBy       string
	CreatedAt        string
	CleanedBy        sql.NullString
	CleanedAt        sql.NullString
}

const attachmentColumns = `a.id, a.message_id, a.conversation_id, a.kind, a.original_filename,
	a.content_type, a.byte_size, a.checksum, a.storage_key, a.thumbnail_key,
	a.width, a.height, a.state, a.document_id, a.document_path, a.uploaded_by,
	a.created_at, a.cleaned_by, a.cleaned_at`

func scanAttachment(rows interface{ Scan(...any) error }) (attachmentRow, error) {
	var a attachmentRow
	err := rows.Scan(&a.ID, &a.MessageID, &a.ConversationID, &a.Kind, &a.OriginalFilename,
		&a.ContentType, &a.ByteSize, &a.Checksum, &a.StorageKey, &a.ThumbnailKey,
		&a.Width, &a.Height, &a.State, &a.DocumentID, &a.DocumentPath, &a.UploadedBy,
		&a.CreatedAt, &a.CleanedBy, &a.CleanedAt)
	return a, err
}

// wire renders the row for the API.
//
// ⚠ A `removed` ATTACHMENT KEEPS ITS FILENAME AND SIZE (D243), which is why they
// are columns rather than something derived from an object that no longer exists.
// The thread stays legible, a member can ask for the file again knowing exactly
// what it was, and the clean-up is attributed.
func (a attachmentRow) wire(labels map[string]string) Attachment {
	out := Attachment{
		ID:               a.ID,
		Kind:             a.Kind,
		State:            a.State,
		OriginalFilename: a.OriginalFilename,
		ContentType:      a.ContentType,
		ByteSize:         a.ByteSize,
		HasThumbnail:     a.ThumbnailKey.Valid && a.ThumbnailKey.String != "",
		DocumentID:       nullStr(a.DocumentID),
		DocumentPath:     nullStr(a.DocumentPath),
		UploadedBy:       a.UploadedBy,
		CreatedAt:        a.CreatedAt,
		CleanedAt:        nullStr(a.CleanedAt),
	}
	if a.Width.Valid {
		w := int(a.Width.Int64)
		out.Width = &w
	}
	if a.Height.Valid {
		h := int(a.Height.Int64)
		out.Height = &h
	}
	if a.CleanedBy.Valid {
		l := label(labels, a.CleanedBy.String)
		out.CleanedByLabel = &l
	}
	return out
}

// AttachmentsForMessages loads every attachment on a page of messages, in ONE
// query.
//
// ⚠ IT TAKES THE MESSAGE IDS THE CALLER ALREADY RESOLVED THROUGH THE FLOOR, which
// is what makes the floor unnecessary here rather than merely absent: the rows it
// keys on are exactly the rows Thread returned, and Thread's predicate is the
// access rule. Re-deriving membership from `a.conversation_id` would be a second
// spelling of it.
//
// All three states come back. A `removed` attachment is an epitaph and a `moved`
// one still renders — the thread is the one surface where every state is visible
// (the clean-up listing is the one that is `live` only).
func (s *Store) AttachmentsForMessages(ctx context.Context, q querier, messageIDs []string) (map[string][]attachmentRow, error) {
	out := map[string][]attachmentRow{}
	if len(messageIDs) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(messageIDs))
	for _, id := range messageIDs {
		args = append(args, id)
	}
	rows, err := q.QueryContext(ctx, `
		SELECT `+attachmentColumns+`
		  FROM chat_attachments a
		 WHERE a.message_id IN (`+appdb.Placeholders(len(messageIDs))+`)
		 ORDER BY a.created_at, a.id`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out[a.MessageID] = append(out[a.MessageID], a)
	}
	return out, rows.Err()
}

// AttachmentForViewer loads one attachment for a member who may read it.
//
// ⚠ ONE QUERY CARRIES THE WHOLE RULE: the membership join, the floor as an id bound
// on the PARENT MESSAGE, the koš, and the tombstone. Four terms, and every one of
// them has to hold before a single byte is streamed — which is also why this runs
// BEFORE the `If-None-Match` branch in the handler (leak row 5). A conditional
// request that reached the ETag comparison first would answer a non-member 304:
// *"yes, and it hasn't changed"*, about something they may not read.
//
// ErrNotMember covers every refusal, so "not a member", "below the floor", "in the
// koš", "the message was deleted" and "no such id" are one answer (D217).
func (s *Store) AttachmentForViewer(ctx context.Context, q querier, actor, attachmentID string) (attachmentRow, error) {
	row := q.QueryRowContext(ctx, `
		SELECT `+attachmentColumns+`
		  FROM chat_attachments a
		  JOIN chat_messages m       ON m.id = a.message_id
		  JOIN chat_conversations c  ON c.id = a.conversation_id
		  JOIN chat_members mem      ON mem.conversation_id = a.conversation_id AND mem.user_id = ?
		 WHERE a.id = ?
		   AND c.deleted_at IS NULL
		   AND m.deleted_at IS NULL
		   AND m.id > mem.effective_from_id`, actor, attachmentID)
	a, err := scanAttachment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return attachmentRow{}, ErrNotMember
	}
	return a, err
}

// InsertAttachment writes one row for bytes that are ALREADY durable in object
// storage (the FR-DOC1 ordering, reused: object first, row second).
func (s *Store) InsertAttachment(ctx context.Context, q querier, a attachmentRow) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO chat_attachments (id, message_id, conversation_id, kind, original_filename,
		    content_type, byte_size, checksum, storage_key, thumbnail_key, width, height,
		    state, uploaded_by, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,'live',?,?)`,
		a.ID, a.MessageID, a.ConversationID, a.Kind, a.OriginalFilename,
		a.ContentType, a.ByteSize, a.Checksum, a.StorageKey, nullOrString(a.ThumbnailKey),
		nullOrInt(a.Width), nullOrInt(a.Height), a.UploadedBy, a.CreatedAt)
	return err
}

// MarkAttachmentRemoved is *Odstranit* (D243).
//
// The `state = 'live'` term is the authorisation against a double click: the second
// request affects no rows and the service turns that into 422 rather than a second
// audit event and a second object delete.
// ⚠ `thumbnail_key` IS CLEARED IN THE SAME STATEMENT, because the object it names is
// deleted by the very next step. Leaving it set made `has_thumbnail` serialise true
// for bytes that are gone — harmless today, since the epitaph is text and the client
// branches on `state` first, but it is exactly the shape of the bug this PR already
// fixed one file over: a row asserting an object the bucket does not have, waiting
// for the first consumer that trusts the flag before checking the state.
//
// ⚠ `storage_key` STAYS, because the epitaph is not the only reader: the drain's
// fallback path (queueOrphan) is handed the keys from the row that was loaded BEFORE
// this ran, and keeping the column is what lets a later reconciliation say which
// object this row used to own.
func (s *Store) MarkAttachmentRemoved(ctx context.Context, q querier, id, actor, now string) (bool, error) {
	res, err := q.ExecContext(ctx, `
		UPDATE chat_attachments
		   SET state = 'removed', cleaned_by = ?, cleaned_at = ?, thumbnail_key = NULL
		 WHERE id = ? AND state = 'live'`, actor, now, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// MarkAttachmentMoved is step 4 of the custody transfer (FR-V10-14).
//
// ⚠ IT RUNS ONLY AFTER THE DOCUMENT ROW EXISTS, and the inverse loses the file.
// The `state = 'live'` guard makes a re-run of a completed move a no-op rather than
// a second transfer.
func (s *Store) MarkAttachmentMoved(ctx context.Context, q querier, id, documentID, documentPath, actor, now string) (bool, error) {
	res, err := q.ExecContext(ctx, `
		UPDATE chat_attachments
		   SET state = 'moved', document_id = ?, document_path = ?, cleaned_by = ?, cleaned_at = ?
		 WHERE id = ? AND state = 'live'`, documentID, documentPath, actor, now, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// QueueKeys enqueues object keys for the drain (D247).
//
// ⚠ THE QUEUE HOLDS THE EARLIEST PROMISE, and this takes the same MIN as queuePurge
// for that reason. An earlier version used `INSERT OR IGNORE`, which keeps whichever
// deadline happened to be written first — the opposite rule to the one queuePurge
// enforces, in the same table. No path reaches the conflict today (both callers
// require a live attachment in a live room, which excludes every other enqueue), so
// this is not a bug being fixed; it is the second spelling of one rule being removed
// before somebody adds a third caller and inherits the wrong half.
func (s *Store) QueueKeys(ctx context.Context, q querier, keys []string, queuedAt, purgeAfter string) error {
	for _, k := range keys {
		if k == "" {
			continue
		}
		if _, err := q.ExecContext(ctx, `
			INSERT INTO chat_deleted_keys (key, queued_at, purge_after)
			VALUES (?, ?, ?)
			    ON CONFLICT (key) DO UPDATE
			       SET purge_after = MIN(chat_deleted_keys.purge_after, excluded.purge_after)`,
			k, queuedAt, purgeAfter); err != nil {
			return err
		}
	}
	return nil
}

// objectKeys is every object this attachment owns — the original, plus the
// thumbnail when there is one.
func (a attachmentRow) objectKeys() []string {
	keys := []string{a.StorageKey}
	if a.ThumbnailKey.Valid && a.ThumbnailKey.String != "" {
		keys = append(keys, a.ThumbnailKey.String)
	}
	return keys
}

func nullOrString(s sql.NullString) any {
	if !s.Valid || s.String == "" {
		return nil
	}
	return s.String
}

func nullOrInt(n sql.NullInt64) any {
	if !n.Valid {
		return nil
	}
	return n.Int64
}
