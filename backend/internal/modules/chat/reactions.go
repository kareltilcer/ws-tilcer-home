package chat

import (
	"context"
	"database/sql"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Reactions (v10.1, D265) — seven emoji, a chip per emoji under the bubble.
//
// ⚠ A REACTION IS NOT A MESSAGE AND IT IS NOT A NOTIFICATION. It writes no audit
// event, it sends no push, and it moves nobody's unread count. What it does do is
// publish, because a chip that appears only for the person who tapped it is worse
// than no chip at all.
//
// ⚠ AND IT INHERITS THE FLOOR RATHER THAN RESTATING IT. Every read of a reaction is
// keyed on message ids that Thread or MessageByID already resolved through
// memberScope — the same argument AttachmentsForMessages makes one file over. There
// is no reaction query anywhere that starts from a message id the caller has not
// already been scoped against.

// ReactionPalette is the whole vocabulary (D265): seven emoji, in the order the
// chips and the picker render them.
//
// ⚠ IT IS A CLOSED SET, AND THE SERVER IS WHERE THAT IS TRUE. A free-text emoji
// column is a free-text column: it takes kilobytes of anything, it makes the chip
// row unbounded in width, and it turns "who reacted" into a message-sending channel
// that writes no audit event and raises no push — which is precisely the property
// D231 accepted for messages ON THE UNDERSTANDING that messages are the only thing
// carrying it.
//
// ⚠ ❤️ IS TWO CODE POINTS (U+2764 U+FE0F) and the variation selector is part of it.
// Comparing against this slice byte-for-byte is what keeps the stored value, the
// wire value and the double-tap gesture's value one string; normalising it away
// here would store a ❤ the frontend never sends and never matches.
var ReactionPalette = []string{"❤️", "👍", "😂", "😮", "😢", "🙏", "✅"}

// reactionAllowed reports whether emoji is in the palette.
func reactionAllowed(emoji string) bool {
	for _, e := range ReactionPalette {
		if e == emoji {
			return true
		}
	}
	return false
}

// reactionRank orders the chips by the palette rather than by when somebody
// happened to tap — so a bubble's chips do not reshuffle under the reader's finger
// as reactions arrive, and two members looking at the same message see one order.
func reactionRank(emoji string) int {
	for i, e := range ReactionPalette {
		if e == emoji {
			return i
		}
	}
	return len(ReactionPalette)
}

// ---- store ----

// ReactionsForMessages loads every reaction on a page of messages in ONE query.
//
// ⚠ ONE QUERY FOR THE WHOLE PAGE, keyed on ids the caller has already been scoped
// against — the shape AttachmentsForMessages and quoteMap both take, for the reason
// both of them state: a per-message fetch is a per-message place to forget the
// floor.
//
// ⚠ IT TAKES NO ACTOR, AND THAT IS THE DESIGN. `mine` is not computed here and is
// not on the wire: the /ws frame is marshalled ONCE for the whole audience
// (ws.PublishTo), so any per-recipient field in a published Message is a field that
// is right for at most one of them. The reactors ride as (user_id, label) pairs and
// each client answers "is one of them me?" against the identity it already holds —
// which is the same thing every bubble already does to decide whether it is mine.
func (s *Store) ReactionsForMessages(ctx context.Context, q querier, messageIDs []string, labels map[string]string) (map[string][]Reaction, error) {
	out := map[string][]Reaction{}
	if len(messageIDs) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(messageIDs))
	for _, id := range messageIDs {
		args = append(args, id)
	}
	// ORDER BY created_at is the order WITHIN one chip's list of reactors — who
	// reacted first reads first. The chips themselves are ordered by the palette
	// below, which is a different question and deliberately not this one.
	rows, err := q.QueryContext(ctx, `
		SELECT message_id, emoji, user_id
		  FROM chat_reactions
		 WHERE message_id IN (`+appdb.Placeholders(len(messageIDs))+`)
		 ORDER BY created_at, user_id`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	// Indexed while scanning so an emoji's actors accumulate onto one chip rather
	// than producing a chip per row.
	index := map[string]map[string]int{}
	for rows.Next() {
		var messageID, emoji, userID string
		if err := rows.Scan(&messageID, &emoji, &userID); err != nil {
			return nil, err
		}
		byEmoji, ok := index[messageID]
		if !ok {
			byEmoji = map[string]int{}
			index[messageID] = byEmoji
		}
		at, ok := byEmoji[emoji]
		if !ok {
			at = len(out[messageID])
			byEmoji[emoji] = at
			out[messageID] = append(out[messageID], Reaction{Emoji: emoji, By: []ReactionActor{}})
		}
		out[messageID][at].By = append(out[messageID][at].By,
			ReactionActor{UserID: userID, Label: label(labels, userID)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id := range out {
		sortReactions(out[id])
	}
	return out, nil
}

// sortReactions puts the chips in palette order. An emoji outside the palette —
// which only a row written before the list was narrowed could be — sorts last
// rather than being dropped: the service refuses to WRITE one, and silently hiding
// one that exists would make a chip count disagree with what the table holds.
func sortReactions(rs []Reaction) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && reactionRank(rs[j].Emoji) < reactionRank(rs[j-1].Emoji); j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}

// SetReaction adds or removes the caller's one reaction of this emoji.
//
// ⚠ IT TAKES THE DESIRED STATE, NOT A TOGGLE, and the reason is the double tap. A
// gesture fires twice more easily than a button does — a slow network, a retried
// request, a finger that bounced — and a toggle applied twice lands on the opposite
// of what the member meant, silently. `reacted` says what the chip should look like
// when this returns, so a replay is a no-op. The same shape `PATCH …/members/me`
// uses for mute, for the same reason.
//
// Idempotent in both directions: an INSERT OR IGNORE that hits the primary key and
// a DELETE that matches nothing are both success.
func (s *Store) SetReaction(ctx context.Context, q querier, messageID, actor, emoji string, reacted bool, now string) error {
	if !reacted {
		_, err := q.ExecContext(ctx,
			`DELETE FROM chat_reactions WHERE message_id = ? AND user_id = ? AND emoji = ?`,
			messageID, actor, emoji)
		return err
	}
	_, err := q.ExecContext(ctx, `
		INSERT OR IGNORE INTO chat_reactions (message_id, user_id, emoji, created_at)
		VALUES (?, ?, ?, ?)`, messageID, actor, emoji, now)
	return err
}

// ClearReactions drops every reaction on a message.
//
// ⚠ CALLED BY THE DELETE, because a tombstone has nothing left to react TO. D223
// keeps the row so replies do not point at nothing, and blanks the body; chips left
// hanging under *Zpráva byla smazána* would be the one part of a deleted message
// that survived it — six people's ❤️ on a sentence nobody can read any more.
func (s *Store) ClearReactions(ctx context.Context, q querier, messageID string) error {
	_, err := q.ExecContext(ctx, `DELETE FROM chat_reactions WHERE message_id = ?`, messageID)
	return err
}

// ---- service ----

// SetReaction is `PUT /api/chat/messages/{id}/reactions`.
//
// It returns the whole re-rendered message for the reason EditMessage does: the
// /ws frame replaces the cached object outright, so anything this struct omits
// disappears from every other member's bubble until something refetches.
func (s *Service) SetReaction(ctx context.Context, messageID string, in ReactionUpdate) (Message, error) {
	actor := reqctx.ActorID(ctx)
	if !reactionAllowed(in.Emoji) {
		// ⚠ 422 rather than 404. Whether the caller may touch this message is the
		// scope's answer below; what is wrong here is the emoji they sent, which is
		// a fact about their own request and not an oracle about anybody's room.
		return Message{}, httpx.ErrUnprocessable("Tuto emotikonu nelze použít jako reakci.")
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
		// ⚠ THE SAME TWO STEPS EVERY BY-ID ROUTE TAKES, in the same order: the
		// conversation is read from the message, then the CALLER is scoped against
		// it. A message below the caller's floor, in a room they are not in, or in
		// the koš is one answer — not found.
		sc, row, err := s.scopeForMessage(ctx, tx, actor, messageID)
		if err != nil {
			return err
		}
		// ⚠ AND AUTHORSHIP IS NOT CHECKED, WHICH IS THE POINT. Edit and delete are
		// the author's alone; a reaction is every member's, the author included.
		if row.DeletedAt.Valid {
			return httpx.ErrUnprocessable("Na smazanou zprávu nelze reagovat.")
		}
		if err := s.store.SetReaction(ctx, tx, messageID, actor, in.Emoji, in.Reacted, now); err != nil {
			return err
		}
		// The floor bounds the AUDIENCE, not only the read — the correction
		// MemberIDsAbove exists for. This is an existing message, so a member added
		// after it was written may not have it, and this frame carries the whole
		// message, body included.
		audience, err = s.store.MemberIDsAbove(ctx, tx, sc.ConversationID, messageID)
		if err != nil {
			return err
		}
		rendered, err := s.renderMessages(ctx, tx, sc, []messageRow{row}, labels)
		if err != nil {
			return err
		}
		out = rendered[0]
		return nil
	})
	if err != nil {
		return Message{}, mapMessageErr(err)
	}
	// ⚠ chat_message.updated, NOT A REACTION-SHAPED FRAME OF ITS OWN. Every client
	// already answers that type by replacing the cached message, which is exactly
	// what has happened; a second frame type would be a second cache path to keep
	// right. It carries no prev_message_id — a reaction does not extend the thread,
	// so there is no gap for anybody to detect.
	s.notifyTo(ctx, audience, "chat_message.updated", MessageEvent{
		ConversationID: out.ConversationID, Message: out,
	})
	return out, nil
}
