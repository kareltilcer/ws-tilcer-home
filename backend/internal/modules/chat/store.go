package chat

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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
// not a precedent to copy.
func NormalizeLimit(n int) int {
	if n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}

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
// The unread count is computed in SQL against MAX(last_read_at, effective_from) so
// that a member added yesterday never opens a conversation to a four-figure badge
// (D250). COALESCE, not a branch: last_read_at is NULL until they first read.
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
		       m.effective_from, m.muted,
		       (SELECT COUNT(*) FROM chat_members mm WHERE mm.conversation_id = c.id),
		       (SELECT COUNT(*) FROM chat_messages x
		         WHERE x.conversation_id = c.id
		           AND x.deleted_at IS NULL
		           AND x.author_id <> ?
		           AND x.id > m.effective_from_id
		           AND x.created_at > COALESCE(m.last_read_at, ''))
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
			r        conversationRow
			effFrom  string
			mutedInt int
			members  int
			unread   int
		)
		if err := rows.Scan(&r.ID, &r.Kind, &r.Name, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt,
			&r.DeletedAt, &effFrom, &mutedInt, &members, &unread); err != nil {
			return nil, false, err
		}
		c := Conversation{
			ID: r.ID, Kind: r.Kind, Name: r.Name,
			CreatedBy: nullStr(r.CreatedBy), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			MemberCount: members, UnreadCount: unread, Muted: mutedInt != 0,
			EffectiveFrom: effFrom,
			// Bytes stays null until PR 3 measures it — never 0. The D161 principle:
			// an unmeasured figure rendered as zero is indistinguishable from an
			// empty room, and the storage half exists to be believed.
			Bytes: nil,
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
func encodeConversationCursor(updatedAt, id string) string { return updatedAt + "|" + id }

func decodeConversationCursor(cursor string) (updatedAt, id string, ok bool) {
	at, rest, found := strings.Cut(cursor, "|")
	if !found || at == "" || rest == "" {
		return "", "", false
	}
	return at, rest, true
}

// GetConversation loads one room for a member. The scope has already refused a
// non-member, so this is a plain row read.
func (s *Store) GetConversation(ctx context.Context, q querier, actor string, sc Scope) (Conversation, error) {
	var (
		r        conversationRow
		effFrom  string
		mutedInt int
		members  int
		unread   int
	)
	err := q.QueryRowContext(ctx, `
		SELECT c.id, c.kind, c.name, c.created_by, c.created_at, c.updated_at,
		       m.effective_from, m.muted,
		       (SELECT COUNT(*) FROM chat_members mm WHERE mm.conversation_id = c.id),
		       (SELECT COUNT(*) FROM chat_messages x
		         WHERE x.conversation_id = c.id
		           AND x.deleted_at IS NULL
		           AND x.author_id <> ?
		           AND x.id > m.effective_from_id
		           AND x.created_at > COALESCE(m.last_read_at, ''))
		  FROM chat_members m
		  JOIN chat_conversations c ON c.id = m.conversation_id
		 WHERE m.conversation_id = ? AND m.user_id = ?`,
		actor, sc.ConversationID, actor).
		Scan(&r.ID, &r.Kind, &r.Name, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt,
			&effFrom, &mutedInt, &members, &unread)
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
		EffectiveFrom: effFrom, Bytes: nil,
	}, nil
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
func (s *Store) RestoreConversation(ctx context.Context, q querier, id, now string) error {
	if _, err := q.ExecContext(ctx, `
		DELETE FROM chat_deleted_keys
		 WHERE key IN (SELECT storage_key FROM chat_attachments WHERE conversation_id = ?)
		    OR key IN (SELECT thumbnail_key FROM chat_attachments
		                WHERE conversation_id = ? AND thumbnail_key IS NOT NULL)`, id, id); err != nil {
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
		SELECT user_id, effective_from, added_by, muted
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
		if err := rows.Scan(&m.UserID, &m.EffectiveFrom, &addedBy, &mutedInt); err != nil {
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
func (s *Store) MemberIDs(ctx context.Context, q querier, conversationID string) ([]string, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT user_id FROM chat_members WHERE conversation_id = ?`, conversationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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

// PushRecipients is the audience minus the author, minus anyone who muted this
// conversation (D248). The `cat_chat` preference is applied later, by
// push.EligibleSubscriptions — this is the per-conversation half.
func (s *Store) PushRecipients(ctx context.Context, q querier, conversationID, author string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT user_id FROM chat_members
		 WHERE conversation_id = ? AND user_id <> ? AND muted = 0`, conversationID, author)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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
func (s *Store) InsertMember(ctx context.Context, q querier, conversationID, userID, addedBy string, f floor) error {
	var by any
	if addedBy != "" {
		by = addedBy
	}
	_, err := q.ExecContext(ctx, `
		INSERT INTO chat_members (conversation_id, user_id, effective_from, effective_from_id, added_by)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (conversation_id, user_id) DO NOTHING`,
		conversationID, userID, f.At, f.ID, by)
	return err
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
	return s.InsertMember(ctx, s.db, convID, actor, "", f)
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

func ptr[T any](v T) *T { return &v }
