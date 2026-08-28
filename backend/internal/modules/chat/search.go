package chat

import (
	"context"
	"strings"
	"unicode"
)

// Full-text search across the caller's conversations (D251).
//
// ⚠ THE PLACEMENT OF THE MEMBERSHIP JOIN IS THE REQUIREMENT, NOT AN OPTIMISATION.
// FTS5 ranks over what it MATCHED, so a membership filter applied to the result set
// leaves an ordering computed from other people's messages — the caller learns that
// something more relevant exists, and roughly how much, from the shape of what
// comes back. And a snippet IS a message body under another name, so the filter has
// to happen before snippet() ever runs.
//
// Four predicates ride inside the one query and all four are load-bearing:
//
//	mem.user_id = ?                  membership                        (leak row 3)
//	m.id > mem.effective_from_id    the floor, PER ROW — every hit may sit in a
//	                                 different conversation with a different floor,
//	                                 which is why the bound is a COLUMN and not an
//	                                 argument (see floor.go)
//	c.deleted_at IS NULL             the koš
//	m.deleted_at IS NULL             tombstones — a blanked body matches nothing
//	                                 anyway, but a message deleted between the index
//	                                 write and this read is excluded by its row
//
// ⚠ SINGLE PAGE, AND THE CURSOR IS REFUSED RATHER THAN IGNORED. The ordering is
// `rank`, and a keyset cursor is an id: an id does not locate a position in a
// relevance ordering. The handler answers a supplied cursor with 422, following the
// v9 `private-items` precedent that the spec itself invokes for the clean-up page's
// sort=size — a parameter that cannot be honoured is refused, because silently
// ignoring it returns page one forever and looks like the end of the results.
// ftsQuery turns free text into a safe FTS5 prefix MATCH: each whitespace token
// becomes a QUOTED prefix term, so punctuation cannot break out into FTS5's own
// syntax. Returns "" when nothing searchable is left — the caller must treat that
// as "matches nothing" and skip the MATCH entirely.
//
// ⚠ IT IS NOT AN OPTIMISATION AND NOT DEFENSIVE POLISH. Bound raw, ordinary
// message text is a SYNTAX ERROR rather than a search: `mama's` and `co?` are
// "fts5: syntax error", `9:30` and `a-b` are "no such column", a lone `"` is
// "unterminated string" — every one a 500 from /api/chat/search. notes, documents
// and logging each already carry this function for the same reason; chat is the
// fifth FTS5 index in Home and was the only one without it.
func ftsQuery(q string) string {
	fields := strings.Fields(q)
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		// A quote inside a token is dropped rather than doubled: the token is
		// re-quoted below, and FTS5 has no escape that survives a prefix `*`.
		f = strings.ReplaceAll(f, `"`, "")
		// A token with no letter or digit (`:-)`, `!!!`) tokenizes to zero terms,
		// and a prefix `*` on an empty phrase is itself a syntax error.
		if !strings.ContainsFunc(f, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) {
			continue
		}
		terms = append(terms, `"`+f+`"*`)
	}
	return strings.Join(terms, " ")
}

// Search takes a query ALREADY THROUGH ftsQuery — the service does that, because
// it is also the service that turns an empty result into an empty page without
// running the MATCH at all.
func (s *Store) Search(ctx context.Context, actor, query, conversationID string, limit int, labels map[string]string) ([]SearchHit, error) {
	limit = NormalizeLimit(limit)

	// Argument order follows the query text: the membership join's `?` is written
	// before the WHERE clause, so `actor` is first.
	args := []any{actor, query}
	conds := []string{
		"chat_messages_fts MATCH ?",
		"m.id > mem.effective_from_id",
		"m.deleted_at IS NULL",
	}
	if conversationID != "" {
		// Restricting to one room NARROWS the same predicate; it is not a second
		// access decision. A caller naming a conversation they are not in gets an
		// empty result from the membership join — the same answer an unknown id
		// gets, with no separate 404 to disagree with it.
		conds = append(conds, "m.conversation_id = ?")
		args = append(args, conversationID)
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.conversation_id, c.name, m.author_id,
		       snippet(chat_messages_fts, 0, '', '', '…', 12),
		       m.created_at
		  FROM chat_messages_fts f
		  JOIN chat_messages m      ON m.seq = f.rowid
		  JOIN chat_members mem     ON mem.conversation_id = m.conversation_id AND mem.user_id = ?
		  JOIN chat_conversations c ON c.id = m.conversation_id AND c.deleted_at IS NULL
		 WHERE `+strings.Join(conds, " AND ")+`
		 ORDER BY rank
		 LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []SearchHit{}
	for rows.Next() {
		var (
			h        SearchHit
			authorID string
		)
		if err := rows.Scan(&h.MessageID, &h.ConversationID, &h.ConversationName, &authorID,
			&h.Snippet, &h.CreatedAt); err != nil {
			return nil, err
		}
		h.AuthorLabel = label(labels, authorID)
		out = append(out, h)
	}
	return out, rows.Err()
}
