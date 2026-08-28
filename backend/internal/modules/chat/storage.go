package chat

import (
	"context"
	"database/sql"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
)

// Chat's storage picture, and its two contributions to the v9 catalog
// (FR-V10-12/16/21, D235/D237/D240/D254).
//
// ⚠ THE MODULE TOTAL IS THE HOUSEHOLD'S, THE ROWS ARE THE CALLER'S. Everyone sees
// the same `total_bytes`, because `chat.total` is a threshold about the BUCKET and
// a warning nobody can see is not a warning. The per-conversation rows cover only
// conversations the caller belongs to, because a room they are not in is not
// theirs to see the name of (D241 for the clean-up page, D217 everywhere else).
//
// ⚠ A TRASHED CONVERSATION IS STILL COUNTED, in both figures (D254). Its bytes are
// still in R2, and reporting them as freed would make the page lie for a week — the
// page's premise is that its figures sum. *Smazat natrvalo* is what exists so that
// never traps anyone.
//
// ⚠ THIS IS A SUM OVER AN INDEX, NOT A BUCKET LISTING. It is on a member-facing
// path and computed per request with no cache; idx_chat_att_conv_state is exactly
// the index for it. The ADMIN snapshot keeps v9's 60-second cache and its bucket
// List, because that one is reconciling objects against rows and has to look at
// the bucket to do it.

// ChatStorage is GET /api/chat/storage.
type ChatStorage struct {
	TotalBytes              *int64                `json:"total_bytes"`
	ThresholdTotalMB        int                   `json:"threshold_total_mb"`
	TotalExceeded           bool                  `json:"total_exceeded"`
	ThresholdConversationMB int                   `json:"threshold_conversation_mb"`
	Conversations           []ConversationStorage `json:"conversations"`
	// CanCleanUp is D241's gate, answered by the server rather than re-derived from
	// the session on the client.
	//
	// ⚠ IT IS WHAT KEEPS THE WARNING FROM OFFERING A READER A LINK THAT WILL 403
	// THEM. The rule is an intersection of a role and a membership and the client
	// holds only half of it, so the banner asks rather than guesses.
	CanCleanUp bool `json:"can_clean_up"`
	// MaxUploadMB is the per-file cap the composer refuses against BEFORE uploading
	// (D228 — it is HOME_DOCS_MAX_UPLOAD_MB, shared with Dokumenty).
	//
	// ⚠ IT RIDES THIS PAYLOAD SO THE CLIENT NEVER HARD-CODES IT. An operator who
	// raises the cap for Dokumenty raises it for chat, and a composer carrying a
	// stale 50 would refuse files the server would happily take — with a message
	// naming a limit that is not the limit. `documents` publishes the same number on
	// its own tree; chat republishes it rather than fetching another module's tree
	// to find out its own cap.
	MaxUploadMB int `json:"max_upload_mb"`
}

// ConversationStorage is one room's line.
type ConversationStorage struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Bytes     *int64 `json:"bytes"`
	Objects   *int64 `json:"objects"`
	OverLimit bool   `json:"over_limit"`
}

// Storage returns the caller's storage picture.
func (s *Service) Storage(ctx context.Context) (ChatStorage, error) {
	actor := actorID(ctx)
	if actor == "" {
		return ChatStorage{}, httpx.ErrUnauthorized("")
	}
	th, err := storage.LoadThresholds(ctx, s.db)
	if err != nil {
		return ChatStorage{}, err
	}
	total, err := s.store.TotalLiveBytes(ctx, s.db)
	if err != nil {
		return ChatStorage{}, err
	}
	rooms, err := s.store.ConversationUsageFor(ctx, s.db, actor)
	if err != nil {
		return ChatStorage{}, err
	}
	limit := storage.MB(th.Conversation.ValueMB)
	out := ChatStorage{
		TotalBytes:              &total,
		ThresholdTotalMB:        th.Total.ValueMB,
		TotalExceeded:           total > storage.MB(th.Total.ValueMB),
		ThresholdConversationMB: th.Conversation.ValueMB,
		Conversations:           make([]ConversationStorage, 0, len(rooms)),
		CanCleanUp:              writeAllowedCtx(ctx),
		MaxUploadMB:             int(s.upload.MaxBytes >> 20),
	}
	for _, r := range rooms {
		bytes, objects := r.Bytes, r.Objects
		out.Conversations = append(out.Conversations, ConversationStorage{
			ID: r.ID, Name: r.Name, Bytes: &bytes, Objects: &objects,
			OverLimit: bytes > limit,
		})
	}
	return out, nil
}

// StorageBlobs attributes every object under `chat/` to its UPLOADER (FR-V10-21).
//
// ⚠ `Kind: shared` IS THE WRONG WORD AND IT IS KEPT DELIBERATELY. In
// platform/storage, `shared` means *not a v9 private item*; a chat attachment is
// MEMBER-RESTRICTED, which is a third thing. A fourth `Kind` would change a wire
// enum and every consumer of it — the admin page, its frontend types, the warning's
// contributor list — for a distinction that page does not draw. This sentence is in
// the code so that somebody does not "fix" it (D235's note, FR-V10-16).
//
// ⚠ IT LISTS THE BUCKET RATHER THAN SUMMING byte_size, for the reason
// documents/storage.go states: summing the column would make objects that resolve
// to no live row silently invisible, and those are exactly the orphan backlog this
// page exists to surface. The MEMBER-facing figures (Storage above) do sum the
// column, because they are per-request and must not list a bucket.
func (s *Service) StorageBlobs(ctx context.Context) ([]storage.BlobUsage, error) {
	if s.blob == nil {
		return nil, nil
	}
	objects, err := s.blob.List(ctx, keyPrefix)
	if err != nil {
		return nil, err
	}
	owners, err := s.store.AttachmentUploaders(ctx, s.db)
	if err != nil {
		return nil, err
	}
	return storage.Attribute(objects, keyPrefix, attachmentIDFromKey, owners), nil
}

// StorageGroups reports one row per conversation for the Administrace block
// (FR-V10-16, D235/D240/D254).
//
// ⚠ EVERY CONVERSATION, TRASHED ONES INCLUDED, AND NO MEMBERSHIP FILTER. This is
// the admin's module-level view and it is the ONE place in v10 that sees rooms its
// reader is not in — the accepted disclosure in leak row 14. What it does NOT carry
// is any way in: no thread, no attachment list, no clean-up link, and a member
// COUNT rather than the names. An admin sees which room is heavy and asks its
// members; clean-up is member-scoped (D241) and the admin's only two verbs over a
// room they are not in are restore and purge (D255).
func (s *Service) StorageGroups(ctx context.Context) ([]storage.GroupUsage, error) {
	return s.store.ConversationUsageAll(ctx, s.db)
}

// ---- store ----

// conversationUsage is one room's measured bytes.
type conversationUsage struct {
	ID      string
	Name    string
	Bytes   int64
	Objects int64
}

// TotalLiveBytes is the household's whole live chat attachment total.
//
// `state = 'live'` is the entire rule: `moved` bytes left the `chat/` prefix and
// `removed` bytes are gone, so both leave this figure BY CONSTRUCTION rather than
// by bookkeeping that can drift (D246).
func (s *Store) TotalLiveBytes(ctx context.Context, q querier) (int64, error) {
	var total sql.NullInt64
	err := q.QueryRowContext(ctx,
		`SELECT SUM(byte_size) FROM chat_attachments WHERE state = 'live'`).Scan(&total)
	return total.Int64, err
}

// ConversationUsageFor sums the caller's OWN rooms.
//
// The membership join is the access rule, in SQL, like every other read in this
// module. A trashed room is included — its bytes still count (D254) — which is why
// there is no `c.deleted_at IS NULL` term here and why that absence is worth a
// sentence: it is the one read in the module that deliberately does not carry it.
func (s *Store) ConversationUsageFor(ctx context.Context, q querier, actor string) ([]conversationUsage, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT c.id, c.name,
		       COALESCE(SUM(CASE WHEN a.state = 'live' THEN a.byte_size END), 0),
		       COUNT(CASE WHEN a.state = 'live' THEN 1 END)
		  FROM chat_conversations c
		  JOIN chat_members m ON m.conversation_id = c.id AND m.user_id = ?
		  LEFT JOIN chat_attachments a ON a.conversation_id = c.id
		 GROUP BY c.id, c.name
		 ORDER BY 3 DESC, c.name`, actor)
	if err != nil {
		return nil, err
	}
	return scanUsage(rows)
}

// ConversationUsageAll is the admin's table: every room, trashed ones flagged.
func (s *Store) ConversationUsageAll(ctx context.Context, q querier) ([]storage.GroupUsage, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT c.id, c.name, COALESCE(c.deleted_at, ''),
		       COALESCE((SELECT MIN(k.purge_after) FROM chat_deleted_keys k
		                   JOIN chat_attachments ka ON ka.storage_key = k.key
		                  WHERE ka.conversation_id = c.id), ''),
		       (SELECT COUNT(*) FROM chat_members mm WHERE mm.conversation_id = c.id),
		       COALESCE(SUM(CASE WHEN a.state = 'live' THEN a.byte_size END), 0),
		       COUNT(CASE WHEN a.state = 'live' THEN 1 END)
		  FROM chat_conversations c
		  LEFT JOIN chat_attachments a ON a.conversation_id = c.id
		 GROUP BY c.id, c.name, c.deleted_at
		 ORDER BY 6 DESC, c.name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []storage.GroupUsage{}
	for rows.Next() {
		var g storage.GroupUsage
		if err := rows.Scan(&g.ID, &g.Name, &g.TrashedAt, &g.PurgeAfter,
			&g.Members, &g.Bytes, &g.Objects); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ConversationsOverLimit answers "which of my rooms are over" in one query, so the
// clean-up listing does not run a SUM per row.
func (s *Store) ConversationsOverLimit(ctx context.Context, q querier, actor string, limitBytes int64) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT c.id
		  FROM chat_conversations c
		  JOIN chat_members m ON m.conversation_id = c.id AND m.user_id = ?
		  JOIN chat_attachments a ON a.conversation_id = c.id AND a.state = 'live'
		 GROUP BY c.id
		HAVING SUM(a.byte_size) > ?`, actor, limitBytes)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// AttachmentUploaders maps attachment id → uploader for the blob attribution.
//
// ⚠ ONLY `live` ROWS ARE HERE, and the omission is what makes the storage page
// honest: an object whose row says `moved` or `removed` but which is still sitting
// in the bucket resolves to no live row and is reported as UNATTRIBUTED — the drain
// backlog, visible instead of quietly folded into somebody's total.
//
// ⚠ THE VALUE IS THE UPLOADER, NOT "" (FR-V10-16). storage.Attribute reads "" as
// "a shared row" and a missing key as "no live row", so returning the uploader puts
// chat's bytes in the per-member breakdown where the question "who uploaded that
// 40 MB video" can be answered — which is the whole reason attachments are audited
// when messages are not.
func (s *Store) AttachmentUploaders(ctx context.Context, q querier) (map[string]string, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, uploaded_by FROM chat_attachments WHERE state = 'live'`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var id, owner string
		if err := rows.Scan(&id, &owner); err != nil {
			return nil, err
		}
		out[id] = owner
	}
	return out, rows.Err()
}

// cleanupRow is one listing row, before labelling.
type cleanupRow struct {
	attachment       attachmentRow
	conversationName string
}

// CleanupItems is the clean-up listing (D241).
//
// ⚠ FOUR PREDICATES, ALL IN SQL, AND EVERY ONE OF THEM IS THE REQUIREMENT: the
// caller's own conversations (the membership join), above their floor (the id bound
// on the parent message), the koš excluded (`c.deleted_at IS NULL`) and `live` only.
// A listing that fetched rows and filtered them in Go would leak through exactly the
// two fields the acceptance criteria check — the cursor and the total.
//
// It also returns the TOTAL over every matching row, not this page: the figure the
// screen acts on has to cover what it is not showing, especially under `sort=size`,
// which is single-page.
func (s *Store) CleanupItems(ctx context.Context, q querier, actor, conversationID, sort, cursor string, limit int) ([]cleanupRow, bool, int64, error) {
	limit = NormalizeLimit(limit)
	where := `a.state = 'live'
		  AND c.deleted_at IS NULL
		  AND msg.deleted_at IS NULL
		  AND msg.id > mem.effective_from_id`
	args := []any{actor}
	if conversationID != "" {
		where += ` AND a.conversation_id = ?`
		args = append(args, conversationID)
	}
	from := `
		  FROM chat_attachments a
		  JOIN chat_messages msg      ON msg.id = a.message_id
		  JOIN chat_conversations c   ON c.id = a.conversation_id
		  JOIN chat_members mem       ON mem.conversation_id = a.conversation_id AND mem.user_id = ?
		 WHERE ` + where

	var total sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT SUM(a.byte_size)`+from, args...).Scan(&total); err != nil {
		return nil, false, 0, err
	}

	order := `ORDER BY a.byte_size DESC, a.id DESC`
	pageArgs := append([]any(nil), args...)
	if sort == sortRecent {
		order = `ORDER BY a.id DESC`
		if cursor != "" {
			from += ` AND a.id < ?`
			pageArgs = append(pageArgs, cursor)
		}
	}
	pageArgs = append(pageArgs, limit+1)
	rows, err := q.QueryContext(ctx,
		`SELECT `+attachmentColumns+`, c.name`+from+` `+order+` LIMIT ?`, pageArgs...)
	if err != nil {
		return nil, false, 0, err
	}
	defer func() { _ = rows.Close() }()

	out := []cleanupRow{}
	for rows.Next() {
		var (
			a    attachmentRow
			name string
		)
		if err := rows.Scan(&a.ID, &a.MessageID, &a.ConversationID, &a.Kind, &a.OriginalFilename,
			&a.ContentType, &a.ByteSize, &a.Checksum, &a.StorageKey, &a.ThumbnailKey,
			&a.Width, &a.Height, &a.State, &a.DocumentID, &a.DocumentPath, &a.UploadedBy,
			&a.CreatedAt, &a.CleanedBy, &a.CleanedAt, &name); err != nil {
			return nil, false, 0, err
		}
		out = append(out, cleanupRow{attachment: a, conversationName: name})
	}
	if err := rows.Err(); err != nil {
		return nil, false, 0, err
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	// ⚠ `sort=size` NEVER REPORTS ANOTHER PAGE, even when one row was fetched past
	// the limit. The listing is single-page by construction (there is no cursor that
	// can resume a size ordering), so reporting has_more would offer a *Načíst
	// další* that has nothing to send.
	if sort != sortRecent {
		hasMore = false
	}
	return out, hasMore, total.Int64, nil
}

func scanUsage(rows *sql.Rows) ([]conversationUsage, error) {
	defer func() { _ = rows.Close() }()
	out := []conversationUsage{}
	for rows.Next() {
		var u conversationUsage
		if err := rows.Scan(&u.ID, &u.Name, &u.Bytes, &u.Objects); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
