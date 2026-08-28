package chat

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// The read floor, and the one place its two forms are minted (D218).
//
// ⚠ THE FLOOR HAS TO BE EXPRESSIBLE TWO WAYS, and that is the whole reason this
// file exists:
//
//   - As a bound on `id`, so the thread reads straight from
//     idx_chat_messages_conv (conversation_id, id) with the floor as a range
//     rather than a filter.
//   - As a per-row join predicate, because a SEARCH result set spans conversations
//     whose floors all differ — there is no single bound to pass as an argument.
//
// Deriving one from the other at query time would mean TWO SPELLINGS OF ONE ACCESS
// RULE. So both are minted here, stored side by side on chat_members, and every
// read path compares ids only.
//
// ⚠ THE BOUND IS A MESSAGE ID, NOT A TIMESTAMP CONVERTED INTO ONE, and that is the
// correction this file exists to record. The obvious implementation — take
// effective_from, build the lowest UUIDv7 for its millisecond, compare `id >= that`
// — is wrong by up to one millisecond in the wrong direction: a message minted in
// the SAME millisecond a member was added has a larger random suffix than the
// synthetic bound, so it sorts above it and the new member reads it. It is a
// one-millisecond window, it is real, and the first test written against the floor
// found it.
//
// So the bound is the id of THE NEWEST MESSAGE THIS MEMBER MAY NOT READ, taken from
// the table itself, and the predicate is strictly `>`. There is no clock in it and
// therefore no resolution to lose.
//
// ⚠ AND THE VŠICHNI EXEMPTION FALLS OUT OF THAT RATHER THAN BRANCHING ON IT (D258).
// The household room's floor is the conversation's own beginning, where no message
// existed yet — so its bound is the empty string, and every id sorts above it. One
// rule, two values, no `kind == "default"` anywhere in a read path.
// TestDefaultConversationHasNoHistoryBranch is what keeps it that way.

// tsFormat is the house timestamp: RFC 3339 with milliseconds, matching every
// other module's `nowUTC`.
const tsFormat = "2006-01-02T15:04:05.000Z07:00"

func nowUTC() string { return time.Now().UTC().Format(tsFormat) }

// floor is a member's read floor in both its forms.
//
// They always describe the same instant; nothing constructs one without the other,
// because an effective_from written without its bound is a floor half the read
// paths cannot see.
type floor struct {
	// At is the human-facing value: shown in the members panel, and the reason the
	// app can say plainly that somebody added yesterday cannot read last week.
	At string
	// ID is the newest message id this member may NOT read. Compared with strict
	// `>`, so the empty string means "the conversation's beginning" — every UUID
	// sorts above it.
	ID string
}

// beginningOfConversation is the floor for a member whose history starts where the
// conversation does: the auto-join to Všichni (D258), and everybody present when a
// group is created.
//
// ⚠ It is a VALUE, not a branch. The caller decides which floor a member gets; no
// read path ever asks what kind of conversation it is looking at.
func beginningOfConversation(createdAt string) (floor, error) {
	at, err := normaliseTS(createdAt)
	if err != nil {
		return floor{}, err
	}
	return floor{At: at, ID: ""}, nil
}

// floorNow is the floor for a group add: nobody joins a conversation with history
// behind them (D218).
//
// It reads the conversation's newest message id inside the caller's transaction, so
// a message committing concurrently either precedes the add (and is excluded) or
// follows it (and is included) — never both and never neither.
func floorNow(ctx context.Context, q querier, conversationID string) (floor, error) {
	var newest sql.NullString
	if err := q.QueryRowContext(ctx,
		`SELECT MAX(id) FROM chat_messages WHERE conversation_id = ?`, conversationID).
		Scan(&newest); err != nil {
		return floor{}, err
	}
	return floor{At: nowUTC(), ID: newest.String}, nil
}

// normaliseTS parses a stored timestamp and re-renders it in the house format, so
// a value written by a migration literal and one written by nowUTC() compare and
// display identically.
func normaliseTS(ts string) (string, error) {
	t, err := time.Parse(tsFormat, ts)
	if err != nil {
		// Fall back to RFC 3339 without the fixed millisecond width: timestamps
		// written by this module always carry it, but a hand-written migration
		// literal might not.
		t, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return "", fmt.Errorf("chat: unparseable floor %q: %w", ts, err)
		}
	}
	return t.UTC().Format(tsFormat), nil
}
