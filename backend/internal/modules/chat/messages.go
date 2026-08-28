package chat

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Message reads and writes. Every read here takes a Scope, which is the only thing
// that produced a floor (scope.go) — there is no variant that takes a bare
// conversation id, because that is what a leak looks like when it is written down.

const (
	directionBackward = "backward"
	directionForward  = "forward"
)

// Thread reads a page of messages.
//
// ⚠ THE FLOOR IS A RANGE ON `id`, IN THE SQL. `sc.Floor.ID` is the newest message
// this member may NOT read (floor.go), so `m.id > ?` and idx_chat_messages_conv
// (conversation_id, id) answer the thread, the floor and the cursor from one index
// — and next_cursor and has_more are then computed from the SAME row set the caller
// may read.
//
// ⚠ A FLOOR APPLIED IN GO AFTER THIS RETURNS WOULD LEAK. Not the bodies — those
// would be dropped — but the shape: has_more true over rows that do not exist for
// this caller, a next_cursor pointing into somebody else's history, and a page that
// comes back short for a reason the client can measure. The acceptance criteria
// assert those two fields precisely because a hand test does not see them.
//
// `backward` reads a thread from its end (the default: that is how a thread is
// opened); `forward` walks it again as new messages arrive.
func (s *Store) Thread(ctx context.Context, q querier, sc Scope, direction, cursor string, limit int) ([]messageRow, bool, error) {
	limit = NormalizeLimit(limit)

	where := []string{"m.conversation_id = ?", "m.id > ?"}
	args := []any{sc.ConversationID, sc.Floor.ID}
	order := "DESC"
	if direction == directionForward {
		order = "ASC"
		if cursor != "" {
			where = append(where, "m.id > ?")
			args = append(args, cursor)
		}
	} else if cursor != "" {
		where = append(where, "m.id < ?")
		args = append(args, cursor)
	}

	// One row over the limit is how has_more is answered without a second COUNT
	// over the same predicate — and without a COUNT that could disagree with it.
	args = append(args, limit+1)
	rows, err := q.QueryContext(ctx, `
		SELECT m.id, m.conversation_id, m.author_id, m.body, m.reply_to_id,
		       m.created_at, m.edited_at, m.deleted_at
		  FROM chat_messages m
		 WHERE `+strings.Join(where, " AND ")+`
		 ORDER BY m.id `+order+`
		 LIMIT ?`, args...)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()

	out := []messageRow{}
	for rows.Next() {
		var r messageRow
		if err := rows.Scan(&r.ID, &r.ConversationID, &r.AuthorID, &r.Body, &r.ReplyToID,
			&r.CreatedAt, &r.EditedAt, &r.DeletedAt); err != nil {
			return nil, false, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

// messageRow is one row as the table stores it.
type messageRow struct {
	ID             string
	ConversationID string
	AuthorID       string
	Body           string
	ReplyToID      sql.NullString
	CreatedAt      string
	EditedAt       sql.NullString
	DeletedAt      sql.NullString
}

// MessageByID loads one message THROUGH THE FLOOR.
//
// Used by the edit and delete paths (which then check authorship) and by the read
// marker. A message below the caller's floor is not found, which is the same answer
// an id from another conversation gets.
func (s *Store) MessageByID(ctx context.Context, q querier, sc Scope, id string) (messageRow, error) {
	var r messageRow
	err := q.QueryRowContext(ctx, `
		SELECT m.id, m.conversation_id, m.author_id, m.body, m.reply_to_id,
		       m.created_at, m.edited_at, m.deleted_at
		  FROM chat_messages m
		 WHERE m.id = ? AND m.conversation_id = ? AND m.id > ?`,
		id, sc.ConversationID, sc.Floor.ID).
		Scan(&r.ID, &r.ConversationID, &r.AuthorID, &r.Body, &r.ReplyToID,
			&r.CreatedAt, &r.EditedAt, &r.DeletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return messageRow{}, errMessageNotFound
	}
	return r, err
}

// MessageConversation resolves a message id to its conversation WITHOUT any scope,
// so a by-id route can find the room it must then be scoped against.
//
// ⚠ IT DISCLOSES NOTHING AND IT MUST NOT BE MADE TO. The caller's next step is
// always memberScope on the id this returns, and a caller that skipped that step
// would be reading a message from a conversation they are not in. It exists because
// /api/chat/messages/{id} carries no conversation in its path, and inventing one
// would mean trusting the client for it.
func (s *Store) MessageConversation(ctx context.Context, q querier, id string) (string, error) {
	var convID string
	err := q.QueryRowContext(ctx,
		`SELECT conversation_id FROM chat_messages WHERE id = ?`, id).Scan(&convID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errMessageNotFound
	}
	return convID, err
}

var errMessageNotFound = errors.New("chat: message not found")

// Quote resolves a reply's parent.
//
// ⚠ IT IS THE LEAK THE FLOOR MOST EASILY MISSES (D226), because a quote LOOKS like
// data belonging to the child message — a field on the row you are already allowed
// to read. It is not: it is a second read of a second message, and it takes the
// same bound as the first.
//
// Below the floor it returns `available:false` and NOTHING ELSE — no author, no
// date, no excerpt. Not a redacted excerpt, not the author with the text removed:
// the shape is empty, and the frontend renders "Zpráva mimo vaši historii".
func (s *Store) Quote(ctx context.Context, q querier, sc Scope, parentID string, labels map[string]string) (*MessageQuote, error) {
	r, err := s.MessageByID(ctx, q, sc, parentID)
	if errors.Is(err, errMessageNotFound) {
		return &MessageQuote{Available: false}, nil
	}
	if err != nil {
		return nil, err
	}
	deleted := r.DeletedAt.Valid
	return &MessageQuote{
		Available:   true,
		ID:          r.ID,
		AuthorLabel: label(labels, r.AuthorID),
		Excerpt:     excerpt(r.Body),
		CreatedAt:   r.CreatedAt,
		Deleted:     &deleted,
	}, nil
}

// excerpt is the quote's first line or so.
const excerptRunes = 120

// The truncation itself is truncateRunes (push.go) — the same rune slice, the same
// trailing-space trim, the same ellipsis. Two spellings of it drift, and the two
// callers are the quote excerpt and the push preview, which render the same body.
func excerpt(body string) string {
	if i := strings.IndexAny(body, "\r\n"); i >= 0 {
		body = body[:i]
	}
	return truncateRunes(body, excerptRunes)
}

// InsertMessage writes one message.
//
// It returns the id of the message that preceded it in this conversation —
// `prev_message_id` (D259), computed ONCE, inside the transaction, for the whole
// audience. See MessageEvent for why one value rather than one per recipient.
//
// ⚠ THE PREVIOUS MESSAGE IS READ WITH NO FLOOR, and it has to be: the value
// describes the CONVERSATION's sequence, not any one member's view of it. A member
// whose floor sits above it simply never holds it, refetches once, and matches from
// then on — which is exactly what makes the gap check terminate.
func (s *Store) InsertMessage(ctx context.Context, q querier, id, conversationID, author, body string, replyTo *string, now string) (prev *string, err error) {
	var prevID sql.NullString
	if err := q.QueryRowContext(ctx, `
		SELECT MAX(id) FROM chat_messages WHERE conversation_id = ?`, conversationID).
		Scan(&prevID); err != nil {
		return nil, err
	}
	if _, err := q.ExecContext(ctx, `
		INSERT INTO chat_messages (id, conversation_id, author_id, body, reply_to_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, id, conversationID, author, body, replyTo, now); err != nil {
		return nil, err
	}
	return nullStr(prevID), nil
}

// UpdateMessageBody is the edit: own messages only, no time limit.
//
// ⚠ NO HISTORY IS KEPT ANYWHERE (D225). Do not add a chat_message_versions table
// "while we're here": it would be a private, unsearchable, un-redactable copy of
// every message ever revised, and D231 deliberately keeps message text out of the
// one store Home has for before-and-after values.
//
// The author_id term is the authorisation, in the statement rather than beside it:
// a check in Go and a write in SQL is two places for one rule.
func (s *Store) UpdateMessageBody(ctx context.Context, q querier, id, author, body, now string) (bool, error) {
	res, err := q.ExecContext(ctx, `
		UPDATE chat_messages SET body = ?, edited_at = ?
		 WHERE id = ? AND author_id = ? AND deleted_at IS NULL`, body, now, id, author)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SoftDeleteMessage is the tombstone.
//
// ⚠ THE BODY IS BLANKED IN THE SAME STATEMENT, and that is not tidiness.
// chat_messages_fts is EXTERNAL-CONTENT, so it reads its text from this table:
// `deleted_at IS NOT NULL` would hide the message from the thread and leave it
// perfectly findable by search, snippet and all. The 12001 update trigger fires on
// `old.body IS NOT new.body`, which is what actually removes it from the index —
// which is why the test asserts against chat_messages_fts directly and not only
// through the API.
//
// The row stays. Removing it would leave replies pointing at nothing and silently
// reflow a thread somebody is reading (D223).
func (s *Store) SoftDeleteMessage(ctx context.Context, q querier, id, author, now string) (bool, error) {
	res, err := q.ExecContext(ctx, `
		UPDATE chat_messages SET deleted_at = ?, body = ''
		 WHERE id = ? AND author_id = ? AND deleted_at IS NULL`, now, id, author)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	// Attachments go with it: the filename is blanked for the same reason the body
	// is, and the keys are queued for the drain with purge_after = now. PR 2 has no
	// attachments, so this is a no-op that is correct the day PR 3 lands.
	if _, err := q.ExecContext(ctx, `
		INSERT OR IGNORE INTO chat_deleted_keys (key, queued_at, purge_after)
		SELECT storage_key, ?, ? FROM chat_attachments WHERE message_id = ? AND state = 'live'`,
		now, now, id); err != nil {
		return false, err
	}
	if _, err := q.ExecContext(ctx, `
		INSERT OR IGNORE INTO chat_deleted_keys (key, queued_at, purge_after)
		SELECT thumbnail_key, ?, ? FROM chat_attachments
		 WHERE message_id = ? AND state = 'live' AND thumbnail_key IS NOT NULL`,
		now, now, id); err != nil {
		return false, err
	}
	if _, err := q.ExecContext(ctx,
		`UPDATE chat_attachments SET original_filename = '' WHERE message_id = ?`, id); err != nil {
		return false, err
	}
	return true, nil
}

// ---- unread ----

// UnreadCount is the caller's badge for one conversation.
//
// Unread is "above my floor, after my read marker, not mine, not a tombstone"
// (D250) — and the floor half is the ID BOUND rather than the timestamp, so the
// badge counts exactly the messages the thread will show. A count taken against
// `effective_from` as a timestamp can differ from the thread by a message minted in
// the same millisecond somebody was added, which reads as a badge that will not
// clear.
func (s *Store) UnreadCount(ctx context.Context, q querier, sc Scope, actor string) (int, error) {
	last := ""
	if sc.LastReadAt != nil {
		last = *sc.LastReadAt
	}
	var n int
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM chat_messages
		 WHERE conversation_id = ? AND deleted_at IS NULL AND author_id <> ?
		   AND id > ? AND created_at > ?`,
		sc.ConversationID, actor, sc.Floor.ID, last).Scan(&n)
	return n, err
}

// AdvanceRead moves the caller's marker to the named message's timestamp.
//
// ⚠ IDEMPOTENT AND NEVER BACKWARDS (D250). A client that replays an older marker —
// a queued request arriving late, a tab restored from history — must not un-read a
// conversation. `MAX(COALESCE(last_read_at, ''), ?)` is the whole mechanism, and it
// is in the statement so no caller can skip it.
func (s *Store) AdvanceRead(ctx context.Context, q querier, conversationID, actor, at string) error {
	_, err := q.ExecContext(ctx, `
		UPDATE chat_members
		   SET last_read_at = MAX(COALESCE(last_read_at, ''), ?)
		 WHERE conversation_id = ? AND user_id = ?`, at, conversationID, actor)
	return err
}
