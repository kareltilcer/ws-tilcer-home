package mcp_test

import (
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/chat"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// The two providers PR 2 adds that carry an access axis of their own: `chat`
// (membership, v10) and `documents` (ownership, v9).

// Leak row 12 — a chat read leaves no trace (D297).
//
// ⚠ THE EVENT RECORDS THE CONTAINER AND NEVER THE CONTENT. Without it, "what has
// the assistant seen?" has no answer at all on a surface deliberately opened onto
// message bodies; with a body in it, the Log becomes a second copy of the chat,
// which is exactly what D231 exists to prevent.
func TestChatReadIsAuditedWithoutBodies(t *testing.T) {
	h := newHarness(t)
	conversationID := h.seedConversation("Dovolená s Petrou", memberA)
	h.seedMessage(conversationID, memberA, "tajná zpráva o překvapení")

	secret, tok := h.mintToken(memberA, "Claude", nil, time.Time{})
	if n := testsupport.CountAudit(t, h.db, "chat", "read"); n != 0 {
		t.Fatalf("something wrote a read event before the read: %d", n)
	}

	_, result := h.call(secret, "home_chat_messages", map[string]any{"conversation_id": conversationID})
	if isError(result) {
		t.Fatalf("a member could not read their own conversation: %s", resultText(t, result))
	}
	if n := testsupport.CountAudit(t, h.db, "chat", "read"); n != 1 {
		t.Fatalf("expected exactly one chat.read event, got %d", n)
	}

	var summary, meta, entityID, entityType string
	if err := h.db.QueryRow(`
		SELECT summary, COALESCE(meta, ''), COALESCE(entity_id, ''), COALESCE(entity_type, '')
		  FROM audit_events WHERE module = 'chat' AND action = 'read'`).
		Scan(&summary, &meta, &entityID, &entityType); err != nil {
		t.Fatalf("read the event: %v", err)
	}
	if entityID != conversationID {
		t.Fatalf("entity_id is %q, want the conversation id", entityID)
	}
	// ⚠ THE MODULE'S OWN ENTITY TYPE, not a second spelling of it. The Log's entity
	// timeline selects on `entity_type = ?`, so a read filed under `conversation`
	// while every other chat event is filed under `chat_conversation` is absent from
	// the history of the conversation it records — the one place somebody asking
	// "what has the assistant seen?" would look. Asserted against the same constant
	// v10's own writes use, so the two cannot drift apart silently.
	var v10Type string
	if err := h.db.QueryRow(`
		SELECT entity_type FROM audit_events
		 WHERE module = 'chat' AND action = 'conversation.created' LIMIT 1`).Scan(&v10Type); err != nil {
		t.Fatalf("read v10's own entity type: %v", err)
	}
	if entityType != v10Type {
		t.Fatalf("chat.read is filed under entity_type %q while every other chat event"+
			" uses %q — the Log's entity timeline will not show it", entityType, v10Type)
	}
	if !strings.Contains(meta, tok.ID) || !strings.Contains(meta, "message_count") {
		t.Fatalf("the event does not carry the token and a count: %s", meta)
	}
	// ⚠ NO BODY, NO SNIPPET, NO MESSAGE ID — asserted against audit_events AND
	// audit_changes, because a field diff would carry the text just as surely as a
	// summary would.
	for _, field := range []string{summary, meta} {
		if strings.Contains(field, "tajná zpráva") {
			t.Fatalf("the read event carries a message BODY: %s", field)
		}
	}
	var changes int
	if err := h.db.QueryRow(`
		SELECT COUNT(*) FROM audit_changes c
		  JOIN audit_events e ON e.id = c.event_id
		 WHERE e.module = 'chat' AND e.action = 'read'`).Scan(&changes); err != nil {
		t.Fatalf("count changes: %v", err)
	}
	if changes != 0 {
		t.Fatalf("a chat read event has %d field diffs", changes)
	}

	// ⚠ AND A BROWSER READ WRITES NOTHING. The event fires only when the reader is
	// a token — the Log is not a record of everybody who ever opened a thread.
	ctx := testsupport.CtxUser(memberA, "editor")
	if _, err := h.chat.Thread(ctx, conversationID, "", "", 50); err != nil {
		t.Fatalf("browser thread read: %v", err)
	}
	if n := testsupport.CountAudit(t, h.db, "chat", "read"); n != 1 {
		t.Fatalf("a BROWSER read wrote a chat.read event: now %d", n)
	}
}

// Row 12b — the invariant the row above must not break (D231, D296).
//
// ⚠ D297 DOES NOT REVERSE D231, and this is the assertion that says so from
// v11's side. `TestChatMessagesAreNotAudited` in the chat package is the other
// half and still passes; what is checked here is that the READ event v11 adds did
// not smuggle a message row in beside it.
func TestChatMessagesAreStillNotAudited(t *testing.T) {
	h := newHarness(t)
	conversationID := h.seedConversation("Všichni doma", memberA)
	h.seedMessage(conversationID, memberA, "ahoj")
	h.seedMessage(conversationID, memberA, "tak zítra")

	var n int
	if err := h.db.QueryRow(`
		SELECT COUNT(*) FROM audit_events
		 WHERE module = 'chat' AND action LIKE 'message%'`).Scan(&n); err != nil {
		t.Fatalf("count message events: %v", err)
	}
	if n != 0 {
		t.Fatalf("%d message audit events — D231 says sending, editing and deleting a"+
			" message write NOTHING to the spine, and v11 does not reverse it", n)
	}
}

// A conversation the caller is not in is invisible, and refused the same way an
// unknown id is.
//
// ⚠ MEMBERSHIP IS v10's THIRD ACCESS AXIS and the token does not widen it. A 403
// would confirm the conversation exists, which turns a guessed id into an oracle
// over who in the household talks to whom.
func TestChatMembershipHoldsOverMCP(t *testing.T) {
	h := newHarness(t)
	conversationID := h.seedConversation("Jen pro Petru", memberB)

	aSecret, _ := h.mintToken(memberA, "Claude A", nil, time.Time{})
	_, foreign := h.call(aSecret, "home_chat_messages", map[string]any{"conversation_id": conversationID})
	_, unknown := h.call(aSecret, "home_chat_messages", map[string]any{"conversation_id": "0192f000-0000-7000-8000-000000000000"})

	if !isError(foreign) {
		t.Fatalf("member A read a conversation they are not in: %s", resultText(t, foreign))
	}
	if resultText(t, foreign) != resultText(t, unknown) {
		t.Fatalf("a conversation A is not in is distinguishable from one that does not"+
			" exist:\n%q\n%q", resultText(t, foreign), resultText(t, unknown))
	}
	// ⚠ AND NO READ EVENT IS WRITTEN FOR A REFUSED READ. An audit row for a
	// conversation the caller cannot see would itself be the disclosure — it would
	// prove the id names something.
	if n := testsupport.CountAudit(t, h.db, "chat", "read"); n != 0 {
		t.Fatalf("a REFUSED read wrote %d chat.read events", n)
	}

	// And the conversation is absent from A's listing entirely.
	_, list := h.call(aSecret, "home_chat_conversations", nil)
	if strings.Contains(resultText(t, list), "Jen pro Petru") {
		t.Fatalf("member A's conversation list names a room they are not in:\n%s",
			resultText(t, list))
	}
}

// Rows 10 and 11 for `documents` — the twin of the notes assertions PR 1 made.
//
// ⚠ THE TWO MODULES ARE TWINS BY CONSTRUCTION (D40) AND THEIR TESTS HAVE TO BE
// TOO. One behaviour, two implementations, is only safe while both are exercised;
// a rule proved for notes and assumed for documents is a rule with one
// implementation tested and one hoped for.
func TestDocumentResourcesAreViewerScoped(t *testing.T) {
	h := newHarness(t)
	h.seedDocument(memberB, "Výplatní páska", "private")
	h.seedDocument(memberA, "Návod k myčce", "shared")

	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	t.Run("row 11: the listing enumerates only what the caller may read", func(t *testing.T) {
		body := h.rpc(secret, "resources/list", nil).Body.String()
		if strings.Contains(body, "vyplatni") || strings.Contains(body, "Výplatní") {
			t.Fatalf("member A's listing enumerates member B's private document:\n%s", body)
		}
		if !strings.Contains(body, "navod-k-mycce") {
			t.Fatalf("the shared document is missing, so the scoping proves nothing:\n%s", body)
		}
	})

	// ⚠ AND A LISTED URI ACTUALLY READS. Every other assertion here is about what
	// comes back EMPTY, and a listing whose URIs all resolved to nothing would
	// satisfy all of them — the twin of chat's "the caller reads their own", which
	// is what makes the refusals below mean something.
	t.Run("a listed URI reads back its bytes", func(t *testing.T) {
		rr := h.rpc(secret, "resources/read", map[string]any{"uri": "home://documents/navod-k-mycce"})
		if !strings.Contains(rr.Body.String(), "obsah dokumentu") {
			t.Fatalf("a shared document listed for the caller did not read: %s", rr.Body.String())
		}
	})

	// The owner's own private document reads, under the `soukrome` prefix the SPA
	// parses — which is what makes the borrowed-URI refusal below a refusal rather
	// than a path that never worked.
	t.Run("the owner reads their own private document", func(t *testing.T) {
		bSecret, _ := h.mintToken(memberB, "Claude B", nil, time.Time{})
		rr := h.rpc(bSecret, "resources/read", map[string]any{"uri": "home://documents/soukrome/vyplatni-paska"})
		if !strings.Contains(rr.Body.String(), "obsah dokumentu") {
			t.Fatalf("the OWNER could not read their own private document: %s", rr.Body.String())
		}
	})

	t.Run("row 10: a borrowed URI reads like one that does not exist", func(t *testing.T) {
		borrowed := h.rpc(secret, "resources/read", map[string]any{"uri": "home://documents/soukrome/vyplatni-paska"})
		missing := h.rpc(secret, "resources/read", map[string]any{"uri": "home://documents/soukrome/neexistuje"})
		if borrowed.Body.String() != missing.Body.String() {
			t.Fatalf("a borrowed private URI is distinguishable from one that does not"+
				" exist:\n%s\n%s", borrowed.Body.String(), missing.Body.String())
		}
	})
}

// Documents' search reads both roots the caller may see, and no other.
func TestDocumentSearchIsViewerScoped(t *testing.T) {
	h := newHarness(t)
	h.seedDocument(memberB, "Rozvod elektřiny", "private")

	aSecret, _ := h.mintToken(memberA, "Claude A", nil, time.Time{})
	bSecret, _ := h.mintToken(memberB, "Claude B", nil, time.Time{})

	_, aResult := h.call(aSecret, "home_search", map[string]any{"query": "Rozvod", "in": []string{"documents"}})
	if got := searchCounts(t, aResult)["documents"]; got != 0 {
		t.Fatalf("member A found %d of member B's private documents", got)
	}
	_, bResult := h.call(bSecret, "home_search", map[string]any{"query": "Rozvod", "in": []string{"documents"}})
	if got := searchCounts(t, bResult)["documents"]; got != 1 {
		t.Fatalf("the OWNER found %d of their own private documents, want 1 — the"+
			" scoping is too wide the other way", got)
	}
}

// ---- fixtures ----

// seedConversation creates a conversation owned by one member.
func (h *harness) seedConversation(name, owner string) string {
	h.t.Helper()
	ctx := testsupport.CtxUser(owner, "editor")
	c, err := h.chat.CreateConversation(ctx, chat.ConversationCreate{Name: name})
	if err != nil {
		h.t.Fatalf("seed conversation %q: %v", name, err)
	}
	return c.ID
}

func (h *harness) seedMessage(conversationID, author, body string) {
	h.t.Helper()
	ctx := testsupport.CtxUser(author, "editor")
	if _, err := h.chat.SendMessage(ctx, conversationID, chat.MessageCreate{Body: body}); err != nil {
		h.t.Fatalf("seed message: %v", err)
	}
}

// Rows 10 and 11 for a chat attachment — membership, not ownership.
//
// ⚠ THE CONVERSATION IN THE URI IS NOT TRUSTED. Access is decided by the
// attachment's OWN conversation; the segment is then checked against it. Without
// that check a member of conversation A could read an attachment of conversation
// B by pasting A's id in front of B's attachment id — which is the one way a URI
// made of two ids can be turned into a capability.
func TestChatAttachmentResourceIsMembershipScoped(t *testing.T) {
	h := newHarness(t)
	mine := h.seedConversation("Naše", memberA)
	theirs := h.seedConversation("Jejich", memberB)
	myAttachment := h.seedAttachment(mine, memberA, "recept.txt", "guláš")
	theirAttachment := h.seedAttachment(theirs, memberB, "tajne.txt", "překvapení")

	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	t.Run("the caller reads their own", func(t *testing.T) {
		rr := h.rpc(secret, "resources/read", map[string]any{
			"uri": "home://chat/" + mine + "/attachments/" + myAttachment,
		})
		if !strings.Contains(rr.Body.String(), "guláš") {
			t.Fatalf("a member could not read their own attachment: %s", rr.Body.String())
		}
	})

	t.Run("row 10: another conversation's attachment reads like one that does not exist", func(t *testing.T) {
		foreign := h.rpc(secret, "resources/read", map[string]any{
			"uri": "home://chat/" + theirs + "/attachments/" + theirAttachment,
		})
		missing := h.rpc(secret, "resources/read", map[string]any{
			"uri": "home://chat/" + theirs + "/attachments/0192f000-0000-7000-8000-000000000000",
		})
		if strings.Contains(foreign.Body.String(), "překvapení") {
			t.Fatalf("member A read an attachment from a conversation they are not in: %s",
				foreign.Body.String())
		}
		if foreign.Body.String() != missing.Body.String() {
			t.Fatalf("a foreign attachment is distinguishable from one that does not"+
				" exist:\n%s\n%s", foreign.Body.String(), missing.Body.String())
		}
	})

	t.Run("the conversation segment is checked against the attachment", func(t *testing.T) {
		// ⚠ THE ATTACK: A's own conversation id in front of B's attachment id. The
		// attachment resolves — B uploaded it — so a provider that scoped by the
		// SEGMENT would hand it over.
		rr := h.rpc(secret, "resources/read", map[string]any{
			"uri": "home://chat/" + mine + "/attachments/" + theirAttachment,
		})
		if strings.Contains(rr.Body.String(), "překvapení") {
			t.Fatalf("a mismatched conversation/attachment pair was served: %s", rr.Body.String())
		}
	})

	t.Run("row 11: the listing enumerates only the caller's conversations", func(t *testing.T) {
		body := h.rpc(secret, "resources/list", nil).Body.String()
		if !strings.Contains(body, myAttachment) {
			t.Fatalf("the caller's own attachment is missing from the listing:\n%s", body)
		}
		if strings.Contains(body, theirAttachment) || strings.Contains(body, "tajne.txt") {
			t.Fatalf("the listing enumerates an attachment from a conversation the"+
				" caller is not in:\n%s", body)
		}
	})
}
