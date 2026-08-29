package chat

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/cursor"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
)

// Store is chat's data access. Every method that touches a conversation's contents
// goes through memberScope first (see scope.go); the one exception is named for
// what it is — adminScope, for restore and purge.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const (
	kindDefault = "default"
	kindGroup   = "group"

	defaultLimit = 50
	maxLimit     = 200
)

// NormalizeLimit clamps a caller-supplied page size to the house 50/200.
//
// ⚠ IT CLAMPS RATHER THAN REFUSING, and it clamps rather than passing through.
// v8's list endpoints take 100/500 and do NOT clamp, which is a known defect and
// not a precedent to copy — appdb.ClampLimit's doc now carries that warning too,
// since it is the shared arithmetic electricity is the one caller not using.
func NormalizeLimit(n int) int { return appdb.ClampLimit(n, defaultLimit, maxLimit) }

// ---- conversations ----

type conversationRow struct {
	ID         string
	Kind       string
	Name       string
	CreatedBy  sql.NullString
	CreatedAt  string
	UpdatedAt  string
	DeletedAt  sql.NullString
	PurgeAfter sql.NullString
}

// ListConversations returns the caller's rooms.
//
// ⚠ THE MEMBERSHIP JOIN IS THE LIST. There is no "all conversations, filtered": a
// room the caller is not in never enters the query, so there is no result set for a
// count, a cursor or a total to describe (leak rows 10–11). `trash` is the same
// query with the koš predicate inverted — a trashed conversation is listed only
// here, and only for its own members.
//
// The unread count is computed in SQL against BOTH id bounds — the floor and the
// read marker — so that a member added yesterday never opens a conversation to a
// four-figure badge (D250). ⚠ Ids, never timestamps: `created_at` has millisecond
// resolution and a message sharing a millisecond with the marker would be excluded
// permanently, since the marker only moves forward. See UnreadCount.
//
// ⚠ IT IS PAGED, AND `limit`/`cursor` ARE HONOURED RATHER THAN ACCEPTED AND
// DROPPED. The spec has declared both since 0.12.0 along with `next_cursor`, and a
// list that takes a cursor and returns everything is page one dressed as the whole
// result — the failure this module refuses to commit one file away, where Search
// answers a cursor it cannot honour with 422. The keyset rides the ORDER BY it
// already had: (updated_at, id) descending, which is why the cursor carries both.
func (s *Store) ListConversations(ctx context.Context, actor, state, cursor string, limit int) ([]Conversation, bool, error) {
	limit = NormalizeLimit(limit)
	trashed := state == "trash"
	koš := "c.deleted_at IS NULL"
	if trashed {
		koš = "c.deleted_at IS NOT NULL"
	}
	// The unread subquery's `?` is written before the WHERE clause, so `actor`
	// binds twice, in that order.
	args := []any{actor, actor}
	keyset := ""
	if cursor != "" {
		at, id, ok := decodeConversationCursor(cursor)
		if !ok {
			return nil, false, errBadCursor
		}
		// Spelled out rather than as a row value: the comparison has to match the
		// ORDER BY exactly, and an explicit form says so to the next reader.
		keyset = ` AND (c.updated_at < ? OR (c.updated_at = ? AND c.id < ?))`
		args = append(args, at, at, id)
	}
	// One row over the limit answers has_more without a second COUNT that could
	// disagree with it — the same shape Thread uses.
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.kind, c.name, c.created_by, c.created_at, c.updated_at,
		       c.deleted_at,
		       m.effective_from, m.effective_from_id = '', m.muted,
		       (SELECT COUNT(*) FROM chat_members mm WHERE mm.conversation_id = c.id),
		       (SELECT COUNT(*) FROM chat_messages x
		         WHERE x.conversation_id = c.id
		           AND x.deleted_at IS NULL
		           AND x.author_id <> ?
		           AND x.id > m.effective_from_id
		           AND x.id > m.last_read_id),
		       -- v10 PR 3: the room's OWNED bytes — storage.go's rule, restated as a
		       -- subquery because a GROUP BY here would have to carry every selected
		       -- column while the two counts above are already subqueries.
		       --
		       -- ⚠ NOT THE CALLER'S FLOOR, deliberately. This is what the ROOM weighs,
		       -- which is what its threshold is about and what the clean-up page and the
		       -- Administrace block both report. A member added yesterday sees the room's
		       -- real size and can still only clean their own share of it (D241).
		       --
		       -- ⚠ AND IT IS MEASURED, so it is 0 rather than null for an empty room.
		       -- The D161 principle cuts both ways: a figure nobody measured must not
		       -- render as zero, and a figure that IS zero must not render as unmeasured.
		       (SELECT COALESCE(SUM(a.byte_size), 0)
		          FROM chat_attachments a
		          JOIN chat_messages am ON am.id = a.message_id
		         WHERE a.conversation_id = c.id
		           AND a.state = 'live' AND am.deleted_at IS NULL)
		  FROM chat_members m
		  JOIN chat_conversations c ON c.id = m.conversation_id
		 WHERE m.user_id = ? AND `+koš+keyset+`
		 ORDER BY c.updated_at DESC, c.id DESC
		 LIMIT ?`, args...)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()

	// ALWAYS an array, never null (D174) — the empty slice is the empty state, and
	// a nil one serialises as `null` and breaks every `.map` on the other side.
	out := []Conversation{}
	for rows.Next() {
		var (
			r         conversationRow
			effFrom   string
			fromStart bool
			mutedInt  int
			members   int
			unread    int
		)
		var bytes int64
		if err := rows.Scan(&r.ID, &r.Kind, &r.Name, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt,
			&r.DeletedAt, &effFrom, &fromStart, &mutedInt, &members, &unread, &bytes); err != nil {
			return nil, false, err
		}
		measured := bytes
		c := Conversation{
			ID: r.ID, Kind: r.Kind, Name: r.Name,
			CreatedBy: nullStr(r.CreatedBy), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			MemberCount: members, UnreadCount: unread, Muted: mutedInt != 0,
			EffectiveFrom: effFrom, ReadsFromBeginning: fromStart,
			// ⚠ MEASURED FROM PR 3 ON. PR 2 left this null with a note saying PR 3
			// would fill it — and it very nearly shipped still null, which would have
			// left the conversation list rendering *nezměřeno* for every room, forever,
			// on the module whose storage half this PR IS. `over_conversation_limit`
			// goes with it: it is a verdict ABOUT this figure and cannot be more
			// certain than the figure is, which is why both are pointers.
			Bytes: &measured,
		}
		if trashed {
			// PurgeAfter is derived by the service from deleted_at and
			// HOME_CHAT_TRASH_DAYS: the retention window is configuration, so
			// storing it on the row would freeze a setting that can change between
			// a delete and the drain that acts on it.
			c.DeletedAt = nullStr(r.DeletedAt)
		}
		out = append(out, c)
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

// errBadCursor is a cursor that did not come from this endpoint. It is REFUSED
// rather than ignored, for the reason Search states at length: a parameter that is
// silently dropped returns page one forever and reads as the end of the results.
var errBadCursor = errors.New("chat: malformed cursor")

// The conversation cursor carries BOTH ordering columns, because the ORDER BY does:
// `updated_at` alone is not unique — a send bumps it, and two rooms bumped in the
// same millisecond would page over each other or skip one.
func encodeConversationCursor(updatedAt, id string) string {
	return cursor.Encode(updatedAt, id)
}

// The empty-part check is chat's own and stays here: platform/cursor reports only
// that a token was minted with the right arity, so a hand-built token carrying an
// empty column would otherwise decode `ok` and page against "".
func decodeConversationCursor(c string) (updatedAt, id string, ok bool) {
	parts, ok := cursor.Decode(c, 2)
	if !ok || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// GetConversation loads one room for a member, MEMBERSHIP AND KOŠ INCLUDED.
//
// ⚠ IT RESOLVES THE ACCESS RULE ITSELF RATHER THAN TRUSTING A SCOPE, AND THAT IS
// WHAT MAKES IT ONE QUERY (v10 review). It always carried the membership join —
// `m.conversation_id = ? AND m.user_id = ?` — so a caller ran memberScope, threw
// the result away and paid for the SAME join a second time, against a pool capped
// at one connection (platform/db). What it was missing was the one term that made
// the first query load-bearing: `c.deleted_at IS NULL`, the koš. With that term
// here the two queries are one, and `ErrNoRows` still means exactly what
// memberScope means by it — not a member, in the koš, or never issued.
//
// It takes the conversation id rather than a Scope for the same reason: a Scope
// argument would be a claim this query does not need and cannot check.
func (s *Store) GetConversation(ctx context.Context, q querier, actor, conversationID string) (Conversation, error) {
	var (
		r         conversationRow
		effFrom   string
		fromStart bool
		mutedInt  int
		members   int
		unread    int
	)
	err := q.QueryRowContext(ctx, `
		SELECT c.id, c.kind, c.name, c.created_by, c.created_at, c.updated_at,
		       m.effective_from, m.effective_from_id = '', m.muted,
		       (SELECT COUNT(*) FROM chat_members mm WHERE mm.conversation_id = c.id),
		       (SELECT COUNT(*) FROM chat_messages x
		         WHERE x.conversation_id = c.id
		           AND x.deleted_at IS NULL
		           AND x.author_id <> ?
		           AND x.id > m.effective_from_id
		           AND x.id > m.last_read_id)
		  FROM chat_members m
		  JOIN chat_conversations c ON c.id = m.conversation_id
		 WHERE m.conversation_id = ? AND m.user_id = ? AND c.deleted_at IS NULL`,
		actor, conversationID, actor).
		Scan(&r.ID, &r.Kind, &r.Name, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt,
			&effFrom, &fromStart, &mutedInt, &members, &unread)
	if errors.Is(err, sql.ErrNoRows) {
		return Conversation{}, ErrNotMember
	}
	if err != nil {
		return Conversation{}, err
	}
	return Conversation{
		ID: r.ID, Kind: r.Kind, Name: r.Name,
		CreatedBy: nullStr(r.CreatedBy), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		MemberCount: members, UnreadCount: unread, Muted: mutedInt != 0,
		EffectiveFrom: effFrom, ReadsFromBeginning: fromStart, Bytes: nil,
	}, nil
}

// TrashedCount is how many of the caller's conversations are in the koš (v10.1,
// D267).
//
// ⚠ IT IS THE CALLER'S KOŠ, NOT THE HOUSEHOLD'S. The membership join is the same one
// ListConversations is built on, so a room the caller is not in never enters the
// count — a number that leaked how many rooms exist would be the leak table's row 11
// answered in the affirmative by a scalar.
//
// ⚠ AND AN ADMIN GETS NO WIDER ANSWER. Restore and purge are verbs an admin has over
// a room they may not read (D255); this is a READ, so it stays member-scoped like
// every other one in the module.
func (s *Store) TrashedCount(ctx context.Context, q querier, actor string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(*)
		  FROM chat_members m
		  JOIN chat_conversations c ON c.id = m.conversation_id
		 WHERE m.user_id = ? AND c.deleted_at IS NOT NULL`, actor).Scan(&n)
	return n, err
}

// LastMessages is the newest message each named room has FOR THIS CALLER (v10.1,
// D266) — the conversation row's preview line.
//
// ⚠ THE FLOOR IS IN THE QUERY AND SO IS THE KOŠ, and both are load-bearing. `MAX(id)`
// over a conversation is the newest message the ROOM has, which for a member added
// yesterday is a body they may not read — printed on the row they see before they
// open anything, which is a worse leak than the thread's would be because nobody
// has to click. And the `deleted_at IS NULL` join is memberScope's own term (D253):
// a room in the koš has left every read of its messages, so it has no preview either.
// Without it the koš listing was the one surface in the module handing back a body
// from a trashed room — invisible only because TrashedRow does not draw one.
//
// ⚠ AND A TOMBSTONE IS STILL THE NEWEST MESSAGE. It is not skipped: `body` is
// already blank on one (D223) and the preview says *Zpráva byla smazána*, exactly
// as the thread does. Skipping back to the newest non-deleted message would print a
// line the room no longer ends with.
//
// ⚠ ONE `MAX` PER ROOM, NOT ONE PER MESSAGE (v10.1 review). The obvious spelling —
// `WHERE x.id = (SELECT MAX(y.id) …)` against chat_messages — is a CORRELATED SCALAR
// SUBQUERY that SQLite re-evaluates for every message above the floor, so a page of
// fifty rooms walked every message in all fifty: `EXPLAIN QUERY PLAN` drove the outer
// loop from `idx_chat_messages_conv (conversation_id=? AND id>?)`. This form resolves
// each room's newest id ONCE, from chat_members, and then looks the row up by primary
// key — O(rooms) where the other was O(messages), on a request the list makes again on
// every send, every read-marker advance and every window focus, against a pool capped
// at one connection.
//
// It takes the ids the caller has already been listed, so it inherits the access
// decision rather than making a second one — the AttachmentsForMessages shape.
func (s *Store) LastMessages(ctx context.Context, q querier, actor string, conversationIDs []string) (map[string]ConversationPreview, error) {
	out := map[string]ConversationPreview{}
	if actor == "" || len(conversationIDs) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(conversationIDs)+1)
	args = append(args, actor)
	for _, id := range conversationIDs {
		args = append(args, id)
	}
	rows, err := q.QueryContext(ctx, `
		WITH newest AS (
		  SELECT m.conversation_id AS conversation_id,
		         (SELECT MAX(y.id) FROM chat_messages y
		           WHERE y.conversation_id = m.conversation_id
		             AND y.id > m.effective_from_id) AS message_id
		    FROM chat_members m
		    JOIN chat_conversations c
		      ON c.id = m.conversation_id AND c.deleted_at IS NULL
		   WHERE m.user_id = ?
		     AND m.conversation_id IN (`+appdb.Placeholders(len(conversationIDs))+`)
		)
		SELECT newest.conversation_id, x.id, x.author_id, x.body, x.created_at,
		       x.deleted_at IS NOT NULL,
		       (SELECT COUNT(*) FROM chat_attachments a WHERE a.message_id = x.id)
		  FROM newest
		  JOIN chat_messages x ON x.id = newest.message_id`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			convID string
			p      ConversationPreview
			body   string
		)
		if err := rows.Scan(&convID, &p.ID, &p.AuthorID, &body, &p.CreatedAt,
			&p.Deleted, &p.AttachmentCount); err != nil {
			return nil, err
		}
		// ⚠ THE LEADING BLANK LINES GO FIRST, AND ONLY HERE (v10.1 review). `excerpt`
		// cuts at the first newline, so a body somebody began with a Shift+Enter —
		// validateBody trims only the right-hand end — excerpted to "", and the row
		// read the empty string as "this message is files only" and printed
		// *0 souborů* under a message that carries none. After this trim an empty
		// excerpt means an empty BODY, which a message can only have when it carries
		// files (SendMessage refuses the other case), so the client's fallback is
		// true again. The quote and the push preview keep `excerpt` as it was.
		p.Excerpt = excerpt(strings.TrimLeft(body, " \t\r\n"))
		out[convID] = p
	}
	return out, rows.Err()
}

// ConversationName reads a room's name for an audit summary or a push title. It
// takes no actor because every caller has already passed a scope; a name is not a
// second access decision.
func (s *Store) ConversationName(ctx context.Context, q querier, id string) (string, error) {
	var name string
	err := q.QueryRowContext(ctx, `SELECT name FROM chat_conversations WHERE id = ?`, id).Scan(&name)
	return name, err
}

func (s *Store) InsertConversation(ctx context.Context, q querier, id, name, actor, now string) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO chat_conversations (id, kind, name, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, id, kindGroup, name, actor, now, now)
	return err
}

func (s *Store) RenameConversation(ctx context.Context, q querier, id, name, now string) error {
	_, err := q.ExecContext(ctx,
		`UPDATE chat_conversations SET name = ?, updated_at = ? WHERE id = ?`, name, now, id)
	return err
}

// TrashConversation is the koš (D253): a soft delete, invisible to every read from
// the moment it commits because memberScope carries the predicate.
func (s *Store) TrashConversation(ctx context.Context, q querier, id, actor, now string) error {
	_, err := q.ExecContext(ctx, `
		UPDATE chat_conversations SET deleted_at = ?, deleted_by = ?, updated_at = ?
		 WHERE id = ? AND deleted_at IS NULL`, now, actor, now, id)
	return err
}

// RestoreConversation clears the koš.
//
// ⚠ It also DELETES the queued object keys, in the same transaction. Nothing is
// reconstructed and nothing was ever removed — the queue is a promise to delete
// later, and restoring is what withdraws it. PR 2 queues nothing (there are no
// attachments yet), so the DELETE is a no-op today and correct the day PR 3 lands.
//
// ⚠ IT WITHDRAWS ONLY WHAT THE CONVERSATION DELETE QUEUED (v10 review). A MESSAGE
// delete also enqueues — that message's keys, with purge_after = now (see
// SoftDeleteMessage) — and it does NOT move the attachment off `state = 'live'`,
// so "every live key in this room" catches those too. A message deleted on Monday,
// a room trashed on Tuesday and restored on Wednesday then left the message a
// tombstone forever while its bytes were never destroyed: orphaned in R2 and still
// counted against both thresholds. `m.deleted_at IS NULL` is the term that tells
// the two enqueues apart, because a message delete is the only thing that sets it.
func (s *Store) RestoreConversation(ctx context.Context, q querier, id, now string) error {
	if _, err := q.ExecContext(ctx, `
		DELETE FROM chat_deleted_keys
		 WHERE key IN (SELECT a.storage_key FROM chat_attachments a
		                 JOIN chat_messages m ON m.id = a.message_id
		                WHERE a.conversation_id = ? AND a.state = 'live'
		                  AND m.deleted_at IS NULL)
		    OR key IN (SELECT a.thumbnail_key FROM chat_attachments a
		                 JOIN chat_messages m ON m.id = a.message_id
		                WHERE a.conversation_id = ? AND a.state = 'live'
		                  AND m.deleted_at IS NULL AND a.thumbnail_key IS NOT NULL)`,
		id, id); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `
		UPDATE chat_conversations SET deleted_at = NULL, deleted_by = NULL, updated_at = ?
		 WHERE id = ?`, now, id)
	return err
}

// PurgeConversationRows removes the conversation and everything cascading off it.
//
// ⚠ THE OBJECTS ARE NOT DELETED HERE. Their keys are queued (PR 3's drain) because
// a conversation with four hundred attachments would otherwise block a request on
// bulk object I/O. The rows go now; the bytes go on the next drain.
func (s *Store) PurgeConversationRows(ctx context.Context, q querier, id string) error {
	_, err := q.ExecContext(ctx, `DELETE FROM chat_conversations WHERE id = ?`, id)
	return err
}

// ---- membership ----

// ListMembers returns the panel's rows, labelled from the directory.
//
// ⚠ Muted is filled ONLY on the caller's own row. Mute is nobody else's business,
// and a bool on every row would publish who has silenced the conversation.
func (s *Store) ListMembers(ctx context.Context, q querier, conversationID, actor string, labels map[string]string) ([]ConversationMember, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT user_id, effective_from, effective_from_id = '', added_by, muted
		  FROM chat_members WHERE conversation_id = ?
		 ORDER BY effective_from, user_id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []ConversationMember{}
	for rows.Next() {
		var (
			m        ConversationMember
			addedBy  sql.NullString
			mutedInt int
		)
		if err := rows.Scan(&m.UserID, &m.EffectiveFrom, &m.ReadsFromBeginning,
			&addedBy, &mutedInt); err != nil {
			return nil, err
		}
		m.AddedBy = nullStr(addedBy)
		m.DisplayName = label(labels, m.UserID)
		if m.UserID == actor {
			muted := mutedInt != 0
			m.Muted = &muted
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MemberIDs is the audience: who a message reaches over /ws and over push.
//
// ⚠ IT IS RESOLVED INSIDE THE WRITING TRANSACTION (D233). Resolving it after the
// commit means a member removed in between still gets the payload — and in this
// module the payload is the content.
//
// ⚠ IT IS THE AUDIENCE FOR A *NEW* MESSAGE ONLY, and that is the whole of why it
// may ignore the floor: a message minted now sorts above every floor in the room,
// so "every member" and "every member who may read it" are the same set. They are
// NOT the same set for an edit or a delete of an OLD message — use
// MemberIDsAbove for those.
func (s *Store) MemberIDs(ctx context.Context, q querier, conversationID string) ([]string, error) {
	return scanIDs(q.QueryContext(ctx,
		`SELECT user_id FROM chat_members WHERE conversation_id = ?`, conversationID))
}

// MemberIDsAbove is the audience for an EXISTING message: the members whose floor
// sits below it, which is exactly the set that may read it (D218/D226).
//
// ⚠ AN EDIT AND A DELETE MUST NOT USE MemberIDs (v10 review). A member added after
// a message was written cannot read it — Thread, MessageByID, quoteMap and Search
// all bound on `id > effective_from_id` — but the /ws frame was published to the
// whole membership, so editing an old message pushed its full new body to the very
// people the floor exists to keep it from. Nothing rendered it, because
// replaceMessage finds no row to replace; it had already reached their browser.
// The same predicate as every read path, in the one place the payload leaves.
func (s *Store) MemberIDsAbove(ctx context.Context, q querier, conversationID, messageID string) ([]string, error) {
	return scanIDs(q.QueryContext(ctx,
		`SELECT user_id FROM chat_members
		  WHERE conversation_id = ? AND ? > effective_from_id`, conversationID, messageID))
}

// PushRecipients is the audience minus the author, minus anyone who muted this
// conversation (D248). The `cat_chat` preference is applied later, by
// push.EligibleSubscriptions — this is the per-conversation half.
func (s *Store) PushRecipients(ctx context.Context, q querier, conversationID, author string) ([]string, error) {
	return scanIDs(q.QueryContext(ctx, `
		SELECT user_id FROM chat_members
		 WHERE conversation_id = ? AND user_id <> ? AND muted = 0`, conversationID, author))
}

// scanIDs drains a one-TEXT-column result set into a slice.
//
// ⚠ ONE LOOP FOR THE THREE AUDIENCE QUERIES (v10 review). MemberIDs,
// MemberIDsAbove and PushRecipients differ only in a WHERE clause and then repeated
// the same twelve lines — three places for a Close, an Err() or the nil-versus-empty
// decision to drift, in the file whose entire subject is who receives a payload, and
// which has already been wrong twice about exactly that. It takes the (rows, err)
// pair so each caller stays a single statement.
func scanIDs(rows *sql.Rows, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	// ALWAYS a slice, never nil: an empty audience is a set nobody is in, and the
	// callers range over it.
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// InsertMember writes one membership row with its floor in both forms.
//
// ⚠ `f` COMES FROM floor.go AND NOWHERE ELSE. Both columns describe one instant,
// and a caller that composed them separately could write a floor half the read
// paths cannot see. Nothing about the conversation's KIND is consulted here: the
// Všichni exemption is the VALUE the caller passes, not a branch (D258).
// ⚠ IT REPORTS WHETHER A ROW WAS ACTUALLY WRITTEN, and callers must act on that
// (v10 review). `DO NOTHING` makes re-adding somebody already in the room a silent
// no-op — which is the right write, and exactly the wrong thing to then narrate as
// "přidán člen" in the Log or to publish over /ws. RemoveMember has always returned
// this; the two verbs now say the same thing about a change that did not happen.
func (s *Store) InsertMember(ctx context.Context, q querier, conversationID, userID, addedBy string, f floor) (bool, error) {
	var by any
	if addedBy != "" {
		by = addedBy
	}
	res, err := q.ExecContext(ctx, `
		INSERT INTO chat_members (conversation_id, user_id, effective_from, effective_from_id, added_by)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (conversation_id, user_id) DO NOTHING`,
		conversationID, userID, f.At, f.ID, by)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// CountMembers is how many people are in a room.
//
// ⚠ IT EXISTS FOR THE LAST-MEMBER GUARD (v10 review), not for the member_count on
// the wire — that one rides the list and get queries as a subquery, so it costs
// nothing there. Removing the last member left a conversation row with no members
// at all: not trashed, so absent from the koš; and every listing is a membership
// JOIN, so absent from every member's list AND from an admin's. The only remaining
// door was conversationForDestructiveVerb's admin fallback, which needs an id
// nothing would ever show. PR 3's attachments would then count against
// `chat.total` forever with no surface that could free them.
func (s *Store) CountMembers(ctx context.Context, q querier, conversationID string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chat_members WHERE conversation_id = ?`, conversationID).Scan(&n)
	return n, err
}

// RemoveMember deletes the row.
//
// ⚠ RE-ADDING WRITES A NEW FLOOR, so a removed-and-re-added member has a PERMANENT
// GAP in the middle of a conversation they otherwise read in full. That is a
// consequence of D218 rather than a bug, and the members screen says so before the
// removal is confirmed — nothing afterwards would explain it. Their messages stay:
// authorship does not depend on membership.
func (s *Store) RemoveMember(ctx context.Context, q querier, conversationID, userID string) (bool, error) {
	res, err := q.ExecContext(ctx,
		`DELETE FROM chat_members WHERE conversation_id = ? AND user_id = ?`, conversationID, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) SetMuted(ctx context.Context, q querier, conversationID, userID string, muted bool) error {
	v := 0
	if muted {
		v = 1
	}
	_, err := q.ExecContext(ctx,
		`UPDATE chat_members SET muted = ? WHERE conversation_id = ? AND user_id = ?`,
		v, conversationID, userID)
	return err
}

// EnsureDefaultMembership is the auto-join (FR-V10-2).
//
// ⚠ IT HAPPENS AT FIRST SIGHT, NOT AT BOOT. The directory is projected from
// `sessions` and a member who has never logged in does not exist yet, so there is
// no set to enrol in advance — the first request that resolves this caller to an
// actor is the first moment they exist at all.
//
// ⚠ AND THE FLOOR IS THE CONVERSATION'S OWN created_at (D258), so a member the app
// meets for the first time in 2028 reads the household room in full. That is the
// exemption, and it is a VALUE passed to InsertMember rather than a branch anywhere.
//
// ⚠ SELECT FIRST, INSERT ONLY ON A MISS. A blind upsert on every request would make
// every GET a write against a single-writer database — the auto-join fires once per
// member in the lifetime of the household, and paying for it on every read forever
// is the wrong trade.
func (s *Store) EnsureDefaultMembership(ctx context.Context, actor string) error {
	if actor == "" {
		return nil
	}
	var (
		convID    string
		createdAt string
		joined    int
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT c.id, c.created_at,
		       EXISTS (SELECT 1 FROM chat_members m
		                WHERE m.conversation_id = c.id AND m.user_id = ?)
		  FROM chat_conversations c WHERE c.kind = 'default'`, actor).
		Scan(&convID, &createdAt, &joined)
	if errors.Is(err, sql.ErrNoRows) {
		// No household room: the seed row was removed by hand, or a down migration
		// ran. Nothing to join, and refusing the request would take the whole module
		// down over a row a migration owns.
		return nil
	}
	if err != nil || joined == 1 {
		return err
	}
	// ⚠ THE FLOOR IS THE CONVERSATION'S OWN BEGINNING (D258), so a member the app
	// meets for the first time in 2028 reads the household room in full. It is the
	// VALUE passed here that makes Všichni different, not a branch anywhere in a
	// read path — floor.go carries the reasoning.
	f, err := beginningOfConversation(createdAt)
	if err != nil {
		return err
	}
	_, err = s.InsertMember(ctx, s.db, convID, actor, "", f)
	return err
}

// ---- directory ----

// label resolves a user id to a display name, falling back to the id.
//
// ⚠ The fallback is the ID, not "Neznámý": a message whose author has no session
// row left still has to be attributable, and a thread full of identical "Neznámý"
// labels is worse than one raw id somebody can look up.
func label(labels map[string]string, userID string) string {
	if n, ok := labels[userID]; ok && n != "" {
		return n
	}
	return userID
}

// nullStr converts a NULL-able column into the pointer the wire types carry.
func nullStr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}
