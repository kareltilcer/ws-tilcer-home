package chat

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
)

// Message operations. Every one of them resolves a Scope first, and the Scope is
// the only thing that carries a floor.

// Thread renders a page of messages for a member.
func (s *Service) Thread(ctx context.Context, conversationID, direction, cursor string, limit int) (MessagePage, error) {
	actor := actorID(ctx)
	if direction != "" && direction != directionBackward && direction != directionForward {
		return MessagePage{}, httpx.ErrUnprocessable("Parametr direction musí být backward nebo forward.")
	}
	sc, err := s.store.memberScope(ctx, s.db, actor, conversationID)
	if err != nil {
		return MessagePage{}, mapScopeErr(err)
	}
	rows, hasMore, err := s.store.Thread(ctx, s.db, sc, direction, cursor, limit)
	if err != nil {
		return MessagePage{}, err
	}
	labels, err := s.labels(ctx)
	if err != nil {
		return MessagePage{}, err
	}
	items, err := s.renderMessages(ctx, s.db, sc, rows, labels)
	if err != nil {
		return MessagePage{}, err
	}

	// ⚠ next_cursor AND has_more COME FROM THE SAME QUERY THAT PRODUCED THE ROWS.
	// They are derived here from `rows`, which was already bounded by the floor in
	// SQL — never from a count taken before filtering, which is the shape that
	// leaks what it removed even with every offending row gone (D218).
	// ⚠ THE CURSOR IS THE FAR END IN THE DIRECTION OF TRAVEL, and the page is always
	// newest-first (Store.Thread), so the two directions take opposite ends of the
	// same slice: backward continues from the OLDEST row it returned, forward from
	// the NEWEST. Reading `items[len-1]` unconditionally would walk a forward page
	// straight back over ground it had already covered.
	page := MessagePage{Items: items, HasMore: hasMore}
	if hasMore && len(items) > 0 {
		if direction == directionForward {
			page.NextCursor = ptr(items[0].ID)
		} else {
			page.NextCursor = ptr(items[len(items)-1].ID)
		}
	}
	return page, nil
}

// renderMessages turns rows into wire messages, resolving every reply quote in ONE
// extra query rather than one per reply.
//
// The batch is not only about speed: a per-message quote lookup is a per-message
// place to forget the floor, and this way there is exactly one.
func (s *Service) renderMessages(ctx context.Context, q querier, sc Scope, rows []messageRow, labels map[string]string) ([]Message, error) {
	quotes, err := s.quoteMap(ctx, q, sc, rows, labels)
	if err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(rows))
	for _, r := range rows {
		m := Message{
			ID: r.ID, ConversationID: r.ConversationID, AuthorID: r.AuthorID,
			AuthorLabel: label(labels, r.AuthorID),
			Body:        r.Body,
			// ALWAYS an array, never null (D174). PR 3 fills it.
			Attachments: []Attachment{},
			CreatedAt:   r.CreatedAt,
			EditedAt:    nullStr(r.EditedAt),
			Deleted:     r.DeletedAt.Valid,
		}
		if r.ReplyToID.Valid {
			m.ReplyTo = quotes[r.ReplyToID.String]
		}
		out = append(out, m)
	}
	return out, nil
}

// quoteMap resolves every distinct parent referenced by this page, THROUGH THE
// FLOOR.
//
// ⚠ A PARENT THAT DOES NOT COME BACK IS `available:false` WITH NOTHING ELSE — no
// author, no date, no excerpt (D226). That covers three different situations with
// one answer, deliberately: below the caller's floor, in another conversation, and
// never existing at all are indistinguishable, because telling them apart is the
// oracle the floor exists to close.
func (s *Service) quoteMap(ctx context.Context, q querier, sc Scope, rows []messageRow, labels map[string]string) (map[string]*MessageQuote, error) {
	wanted := map[string]struct{}{}
	for _, r := range rows {
		if r.ReplyToID.Valid {
			wanted[r.ReplyToID.String] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	ids := make([]any, 0, len(wanted)+2)
	ids = append(ids, sc.ConversationID, sc.Floor.ID)
	for id := range wanted {
		ids = append(ids, id)
	}
	found, err := q.QueryContext(ctx, `
		SELECT id, author_id, body, created_at, deleted_at
		  FROM chat_messages
		 WHERE conversation_id = ? AND id > ? AND id IN (`+appdb.Placeholders(len(wanted))+`)`, ids...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = found.Close() }()

	out := make(map[string]*MessageQuote, len(wanted))
	for found.Next() {
		var (
			id, authorID, body, createdAt string
			deletedAt                     sql.NullString
		)
		if err := found.Scan(&id, &authorID, &body, &createdAt, &deletedAt); err != nil {
			return nil, err
		}
		deleted := deletedAt.Valid
		out[id] = &MessageQuote{
			Available: true, ID: id, AuthorLabel: label(labels, authorID),
			Excerpt: excerpt(body), CreatedAt: createdAt, Deleted: &deleted,
		}
	}
	if err := found.Err(); err != nil {
		return nil, err
	}
	// Everything the page asked for that the floor did not return gets the empty
	// shape. Built here rather than left nil so the wire always carries the field
	// and the frontend renders "Zpráva mimo vaši historii" instead of nothing.
	for id := range wanted {
		if _, ok := out[id]; !ok {
			out[id] = &MessageQuote{Available: false}
		}
	}
	return out, nil
}

// SendMessage writes one message and publishes it.
//
// ⚠ IT WRITES NO AUDIT EVENT (D231). This is the module's primary mutation and it
// leaves nothing in the Log, deliberately — the reasoning is that message text in
// audit_events would be a second, admin-readable copy of every conversation, and
// TestChatMessagesAreNotAudited exists so this is never "fixed" by somebody
// noticing the gap.
//
// ⚠ THE AUDIENCE IS RESOLVED INSIDE THE WRITING TRANSACTION (D233). Resolving it
// after the commit would let a member removed in between still receive the payload,
// and in this module the payload IS the content.
func (s *Service) SendMessage(ctx context.Context, conversationID string, in MessageCreate) (Message, error) {
	actor := actorID(ctx)
	body, err := validateBody(in.Body)
	if err != nil {
		return Message{}, err
	}
	// ⚠ The "body or attachment" invariant, in the write path rather than as a
	// table CHECK — chat_messages carries an explicit rowid alias for the FTS5
	// index and must never be rebuilt, so it can never gain one (D179 precedent).
	// PR 2 has no attachments, so a body is simply required.
	if strings.TrimSpace(body) == "" {
		return Message{}, httpx.ErrUnprocessable("Zpráva nesmí být prázdná.")
	}

	labels, err := s.labels(ctx)
	if err != nil {
		return Message{}, err
	}

	// ⚠ id AND now ARE MINTED INSIDE THE TRANSACTION, BELOW, AND THAT IS THE POINT
	// (v10 review). A UUIDv7 minted before the tx orders by the moment the handler
	// reached this line, not by the moment the row committed — and the pool is
	// capped at ONE connection (platform/db), so a request that mints its id and
	// then waits for that connection can commit AFTER a request whose id is larger.
	// The message then sorts BELOW one already delivered: the thread reorders on
	// the next refetch, and — because created_at is minted with it — a reader whose
	// marker already passed the other message never counts it as unread.
	//
	// The tx is where the conversation's sequence is decided anyway: InsertMessage
	// reads MAX(id) there for prev_message_id. Minting beside that read is what
	// makes id order and commit order the same order.
	var (
		id        string
		now       string
		prev      *string
		audience  []string
		convName  string
		replyTo   *string
		rendered  Message
		sentScope Scope
	)
	if in.ReplyToID != nil && strings.TrimSpace(*in.ReplyToID) != "" {
		replyTo = in.ReplyToID
	}

	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		sc, err := s.store.memberScope(ctx, tx, actor, conversationID)
		if err != nil {
			return err
		}
		id, now = idgen.New(), nowUTC()
		sentScope = sc
		if replyTo != nil {
			// The parent must be one THIS CALLER can read: same conversation, above
			// their floor. Anything else is 422 — and it is the same 422 for a
			// parent in another room and for one below the floor, so the refusal is
			// not a probe.
			if _, err := s.store.MessageByID(ctx, tx, sc, *replyTo); err != nil {
				if errors.Is(err, errMessageNotFound) {
					return httpx.ErrUnprocessable("Odpovídat lze jen na zprávu z této konverzace.")
				}
				return err
			}
		}
		prev, err = s.store.InsertMessage(ctx, tx, id, sc.ConversationID, actor, body, replyTo, now)
		if err != nil {
			return err
		}
		// The list is ordered by activity, so a send is what moves a room to the top.
		if _, err := tx.ExecContext(ctx,
			`UPDATE chat_conversations SET updated_at = ? WHERE id = ?`, now, sc.ConversationID); err != nil {
			return err
		}
		convName, err = s.store.ConversationName(ctx, tx, sc.ConversationID)
		if err != nil {
			return err
		}
		audience, err = s.store.MemberIDs(ctx, tx, sc.ConversationID)
		return err
	})
	if err != nil {
		return Message{}, mapScopeErr(err)
	}

	rendered = Message{
		ID: id, ConversationID: conversationID, AuthorID: actor,
		AuthorLabel: label(labels, actor), Body: body,
		Attachments: []Attachment{}, CreatedAt: now, Deleted: false,
	}
	if replyTo != nil {
		quote, qerr := s.store.Quote(ctx, s.db, sentScope, *replyTo, labels)
		if qerr != nil {
			// The message is committed; a failed quote render must not fail the
			// send. The client refetches the thread and gets it right.
			s.logger.Warn("chat: quote render after send", "err", qerr, "message", id)
		} else {
			rendered.ReplyTo = quote
		}
	}

	s.publishMessage(ctx, audience, rendered, prev)
	s.pushAfterSend(ctx, conversationID, convName, actor, rendered)
	return rendered, nil
}

// publishMessage fans one message out to the member set (D232/D233).
func (s *Service) publishMessage(ctx context.Context, audience []string, m Message, prev *string) {
	s.notifyTo(ctx, audience, "chat_message.created", MessageEvent{
		ConversationID: m.ConversationID, Message: m, PrevMessageID: prev,
	})
}

// pushAfterSend runs the notification OFF the request path.
//
// ⚠ context.WithoutCancel: the request's context is cancelled the moment the
// response is written, and a push started on it would be aborted mid-flight for
// every message ever sent. The recipients were resolved from the committed
// transaction, so nothing here depends on the request still being alive.
func (s *Service) pushAfterSend(ctx context.Context, conversationID, conversationName, author string, m Message) {
	if s.pusher == nil {
		return
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		recipients, err := s.store.PushRecipients(bg, s.db, conversationID, author)
		if err != nil {
			s.logger.Warn("chat: resolve push recipients", "err", err, "conversation", conversationID)
			return
		}
		s.notifyPush(bg, conversationName, recipients, m)
	}()
}

// EditMessage rewrites a body. Own messages only, no time limit (D225).
func (s *Service) EditMessage(ctx context.Context, messageID string, in MessageUpdate) (Message, error) {
	actor := actorID(ctx)
	body, err := validateBody(in.Body)
	if err != nil {
		return Message{}, err
	}
	if strings.TrimSpace(body) == "" {
		return Message{}, httpx.ErrUnprocessable("Zpráva nesmí být prázdná.")
	}
	labels, err := s.labels(ctx)
	if err != nil {
		return Message{}, err
	}

	var (
		now      = nowUTC()
		audience []string
		out      Message
	)
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		sc, row, err := s.scopeForMessage(ctx, tx, actor, messageID)
		if err != nil {
			return err
		}
		if row.AuthorID != actor {
			// ⚠ 404, not 403. "You may not edit this" confirms the message exists
			// and says who it belongs to; an id the caller cannot act on is simply
			// not found, exactly as an unknown one is.
			return errMessageNotFound
		}
		ok, err := s.store.UpdateMessageBody(ctx, tx, messageID, actor, body, now)
		if err != nil {
			return err
		}
		if !ok {
			return errMessageNotFound
		}
		// ⚠ THE FLOOR APPLIES TO THE AUDIENCE, NOT ONLY TO THE READ (v10 review).
		// This is an OLD message, so "every member" is not "every member who may
		// read it": somebody added after it was written is bounded off it by every
		// read path, and publishing the edit to them would hand their socket the
		// body the floor exists to withhold. MemberIDs is right for a send and
		// wrong here, which is exactly why it reads safe.
		audience, err = s.store.MemberIDsAbove(ctx, tx, sc.ConversationID, messageID)
		if err != nil {
			return err
		}
		out = Message{
			ID: messageID, ConversationID: sc.ConversationID, AuthorID: actor,
			AuthorLabel: label(labels, actor), Body: body,
			Attachments: []Attachment{}, CreatedAt: row.CreatedAt,
			EditedAt: ptr(now), Deleted: false,
		}
		return nil
	})
	if err != nil {
		return Message{}, mapMessageErr(err)
	}
	// An edit carries no prev_message_id: it does not extend the thread, so there
	// is no gap for a client to detect and nothing for it to compare against.
	s.notifyTo(ctx, audience, "chat_message.updated", MessageEvent{
		ConversationID: out.ConversationID, Message: out,
	})
	return out, nil
}

// DeleteMessage leaves a tombstone (D223) and writes no audit event (D231).
func (s *Service) DeleteMessage(ctx context.Context, messageID string) error {
	actor := actorID(ctx)
	labels, err := s.labels(ctx)
	if err != nil {
		return err
	}
	var (
		now      = nowUTC()
		audience []string
		out      Message
	)
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		sc, row, err := s.scopeForMessage(ctx, tx, actor, messageID)
		if err != nil {
			return err
		}
		if row.AuthorID != actor {
			return errMessageNotFound
		}
		ok, err := s.store.SoftDeleteMessage(ctx, tx, messageID, actor, now)
		if err != nil {
			return err
		}
		if !ok {
			return errMessageNotFound
		}
		// Below the floor a tombstone is still a disclosure — the id, the author
		// and the time of a message somebody may not read. Same bound as the edit.
		audience, err = s.store.MemberIDsAbove(ctx, tx, sc.ConversationID, messageID)
		if err != nil {
			return err
		}
		out = Message{
			ID: messageID, ConversationID: sc.ConversationID, AuthorID: actor,
			AuthorLabel: label(labels, actor), Body: "",
			Attachments: []Attachment{}, CreatedAt: row.CreatedAt, Deleted: true,
		}
		return nil
	})
	if err != nil {
		return mapMessageErr(err)
	}
	s.notifyTo(ctx, audience, "chat_message.deleted", MessageEvent{
		ConversationID: out.ConversationID, Message: out,
	})
	return nil
}

// scopeForMessage resolves a bare message id to (scope, row).
//
// ⚠ TWO STEPS, IN THIS ORDER, AND NEITHER MAY BE SKIPPED. /api/chat/messages/{id}
// carries no conversation in its path, so the conversation is read from the message
// — and then the CALLER is scoped against it. Trusting the client for the
// conversation instead would let it name one it belongs to and act on a message
// from one it does not.
func (s *Service) scopeForMessage(ctx context.Context, q querier, actor, messageID string) (Scope, messageRow, error) {
	convID, err := s.store.MessageConversation(ctx, q, messageID)
	if err != nil {
		return Scope{}, messageRow{}, err
	}
	sc, err := s.store.memberScope(ctx, q, actor, convID)
	if err != nil {
		return Scope{}, messageRow{}, err
	}
	row, err := s.store.MessageByID(ctx, q, sc, messageID)
	return sc, row, err
}

// mapMessageErr renders both refusals as the same 404. A message the caller may not
// see, may not act on, or that never existed are one answer.
func mapMessageErr(err error) error {
	if errors.Is(err, errMessageNotFound) || errors.Is(err, ErrNotMember) {
		return httpx.ErrNotFound("Zpráva nebyla nalezena.")
	}
	return err
}

// ---- unread ----

// AdvanceRead moves the caller's marker (D250).
func (s *Service) AdvanceRead(ctx context.Context, conversationID string, in ReadUpdate) (ReadState, error) {
	actor := actorID(ctx)
	if strings.TrimSpace(in.UntilMessageID) == "" {
		return ReadState{}, httpx.ErrUnprocessable("Chybí zpráva, po kterou se má značka posunout.")
	}
	var state ReadState
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		sc, err := s.store.memberScope(ctx, tx, actor, conversationID)
		if err != nil {
			return err
		}
		row, err := s.store.MessageByID(ctx, tx, sc, in.UntilMessageID)
		if err != nil {
			return err
		}
		if err := s.store.AdvanceRead(ctx, tx, sc.ConversationID, actor, row.CreatedAt, row.ID); err != nil {
			return err
		}
		// Re-read the scope so the count below uses the marker just written rather
		// than the one loaded before it.
		sc, err = s.store.memberScope(ctx, tx, actor, conversationID)
		if err != nil {
			return err
		}
		n, err := s.store.UnreadCount(ctx, tx, sc, actor)
		if err != nil {
			return err
		}
		state = ReadState{ConversationID: sc.ConversationID, LastReadAt: sc.LastReadAt, UnreadCount: n}
		return nil
	})
	if err != nil {
		return ReadState{}, mapMessageErr(err)
	}
	return state, nil
}

// ---- search ----

// Search runs one MATCH with membership and the floor inside it (D251).
//
// ⚠ A CURSOR IS REFUSED, NOT IGNORED. The ordering is `rank` and a keyset cursor is
// an id, which does not locate a position in a relevance ordering. Silently
// ignoring it would return page one forever and read as the end of the results —
// the v9 `private-items` precedent, which this spec invokes again for the clean-up
// page's sort=size.
func (s *Service) Search(ctx context.Context, query, conversationID, cursor string, limit int) (SearchPage, error) {
	actor := actorID(ctx)
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchPage{}, httpx.ErrUnprocessable("Zadejte, co hledat.")
	}
	if cursor != "" {
		return SearchPage{}, httpx.ErrUnprocessable(
			"Výsledky hledání se řadí podle relevance a stránkovat je kurzorem nelze.")
	}
	// ⚠ SANITISED BEFORE IT REACHES THE MATCH, and an unsearchable query is an
	// EMPTY PAGE rather than a MATCH on the empty string, whose behaviour FTS5
	// leaves unspecified. Somebody searching `:-)` gets no results; before this
	// they got a 500.
	match := ftsQuery(query)
	if match == "" {
		return SearchPage{Items: []SearchHit{}, NextCursor: nil}, nil
	}
	labels, err := s.labels(ctx)
	if err != nil {
		return SearchPage{}, err
	}
	items, err := s.store.Search(ctx, actor, match, conversationID, limit, labels)
	if err != nil {
		return SearchPage{}, err
	}
	return SearchPage{Items: items, NextCursor: nil}, nil
}
