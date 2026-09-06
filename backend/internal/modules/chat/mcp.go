package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcpctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The chat module's MCP provider (v11, PRD §V11-4 FR-M4, FR-M10; D296, D297).
//
// ⚠ CHAT IS READ-ONLY OVER MCP, FOR TWO INDEPENDENT REASONS (D296), and either
// one alone would have been enough.
//
// The first is blast radius: a message is delivered to somebody else's phone,
// with a push, under your name, and NO operation unsends it. Every other write in
// v11 lands in a list the household can correct at leisure; this one lands in a
// person's pocket.
//
// The second is that it could not have been implemented cleanly anyway. v10
// decided chat messages are not audited (D231/D256, guarded by
// TestChatMessagesAreNotAudited), so an MCP-posted message would be the ONLY
// write in the system with nowhere to put the `via` marker. Honouring v11's
// attribution answer for chat would have meant reversing D231.
//
// ⚠ WHAT IS ABSENT: every message verb, every conversation verb, every membership
// verb, the read-marker advance, the clean-up and the move to Dokumenty.
//
// ⚠ AND READING A THREAD IS AUDITED (D297). The event records the CONTAINER and
// never the content — the conversation id and a count, no body, no snippet, no
// message id — because "what has the assistant seen?" must be answerable on a
// surface deliberately opened onto message bodies, and because the Log must not
// become a second copy of the chat.

type mcpProvider struct{ svc *Service }

// MCPProvider implements mcp.Source.
func (m *Module) MCPProvider() mcp.Provider { return &mcpProvider{svc: m.svc} }

func (p *mcpProvider) Module() string { return "chat" }

const (
	toolConversations = "home_chat_conversations"
	toolMessages      = "home_chat_messages"
)

// uriPrefix is the fixed head of this module's resource URIs.
const uriPrefix = "home://chat/"

func (p *mcpProvider) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:  toolConversations,
			Title: "Chat conversations",
			// ⚠ THE UNREAD COUNT RIDES HERE AND NOWHERE ELSE (D252/D310).
			// `home_today` is composed entirely from the metric and list catalogs, and
			// chat is deliberately in neither — so an unread figure there would be the
			// one exception that breaks the property. Here it is free.
			Description: "Lists the conversations the caller is in, with each one's unread count and how far back they may read; it is the only place an unread count is reported.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"limit": {"type": "integer", "minimum": 1, "maximum": 200}},
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
		{
			Name:  toolMessages,
			Title: "Read a chat thread",
			// ⚠ THE DESCRIPTION SAYS THE READ IS RECORDED, out loud. A member whose
			// assistant reads their conversations should be able to learn that from the
			// tool rather than from the Log after the fact.
			Description: "Returns the newest messages of one conversation the caller is a member of, from their history floor onward; reading a thread is recorded in the Log as a conversation and a count, never as its contents.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "conversation_id": {"type": "string"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 200, "description": "Newest first."}
  },
  "required": ["conversation_id"],
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
	}
}

func (p *mcpProvider) Call(ctx context.Context, name string, args json.RawMessage) (mcp.Result, error) {
	switch name {
	case toolConversations:
		return p.conversations(ctx, args)
	case toolMessages:
		return p.messages(ctx, args)
	default:
		return mcp.Result{}, mcp.UnknownToolError(name)
	}
}

type conversationsArgs struct {
	Limit int `json:"limit"`
}

func (p *mcpProvider) conversations(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in conversationsArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	page, err := p.svc.ListConversations(ctx, "", "", in.Limit)
	if err != nil {
		return mcp.Result{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n", mcp.Plural(len(page.Items), "konverzace", "konverzace", "konverzací"))
	for _, c := range page.Items {
		fmt.Fprintf(&b, "\n• %s (id %s) — %d členů", c.Name, c.ID, c.MemberCount)
		if c.UnreadCount > 0 {
			fmt.Fprintf(&b, ", %d nepřečtených", c.UnreadCount)
		}
		if c.Muted {
			b.WriteString(", ztlumeno")
		}
	}
	return mcp.TextResult(b.String(), page)
}

type messagesArgs struct {
	ConversationID string `json:"conversation_id"`
	Limit          int    `json:"limit"`
}

func (p *mcpProvider) messages(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in messagesArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.ConversationID) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Chybí conversation_id.")
	}
	// ⚠ MEMBERSHIP, THE FLOOR AND THE KOŠ ARE ALL ENFORCED IN THE SERVICE, in SQL,
	// and a caller who is not a member gets the same refusal an unknown id gets.
	// This provider adds no second predicate: a Go-side membership check here would
	// be a second spelling of v10's rule, and the wrong one the first time the
	// floor moves.
	page, err := p.svc.Thread(ctx, in.ConversationID, "", "", in.Limit)
	if err != nil {
		if mcp.IsNotFound(err) {
			return mcp.NotFoundResult(), nil
		}
		return mcp.Result{}, err
	}
	// ⚠ THE COUNT IS WHAT WAS ACTUALLY READ, NOT WHAT THE PAGE HELD. A tombstone
	// carries no body — the loop below skips it — so counting one is claiming the
	// assistant saw a message it was never shown. `message_count` is the whole of
	// what D297's event says about how much was read, and the header is the same
	// number said to the caller: a page of fifty holding ten tombstones announced
	// "50 zpráv" and then listed forty.
	var read []Message
	for _, m := range page.Items {
		if m.Deleted {
			continue
		}
		read = append(read, m)
	}
	if err := p.svc.RecordTokenThreadRead(ctx, in.ConversationID, len(read)); err != nil {
		return mcp.Result{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n", mcp.Plural(len(read), "zpráva", "zprávy", "zpráv"))
	for _, m := range read {
		fmt.Fprintf(&b, "\n[%s] %s: %s", m.CreatedAt, m.AuthorLabel, m.Body)
		for _, a := range m.Attachments {
			fmt.Fprintf(&b, "\n  📎 %s — %s/attachments/%s", a.OriginalFilename, uriPrefix+in.ConversationID, a.ID)
		}
	}
	return mcp.TextResult(b.String(), page)
}

// Search reads the member-scoped message index.
//
// ⚠ D302: an actor-less ctx is an ERROR, never an empty slice — and here it is
// the sharpest case in the whole catalog, because chat's access rule is
// MEMBERSHIP and an unscoped read would return every member's private
// conversation to whoever asked.
func (p *mcpProvider) Search(ctx context.Context, q mcp.Query) ([]mcp.Hit, error) {
	if _, ok := reqctx.ActorFrom(ctx); !ok {
		return nil, fmt.Errorf("chat: search without an actor")
	}
	page, err := p.svc.Search(ctx, q.Text, "", "", q.Limit)
	if err != nil {
		return nil, err
	}
	hits := make([]mcp.Hit, 0, len(page.Items))
	for _, h := range page.Items {
		created, _ := time.Parse(tsFormat, h.CreatedAt)
		hits = append(hits, mcp.Hit{
			Kind:      "chat.message",
			ID:        h.MessageID,
			Title:     h.ConversationName,
			Snippet:   h.AuthorLabel + ": " + h.Snippet,
			UpdatedAt: created,
		})
	}
	return hits, nil
}

// Get answers home_get for a conversation.
//
// ⚠ A MESSAGE IS NOT ADDRESSABLE BY home_get, deliberately: a search hit already
// carries its snippet, and fetching one message by id would be a read with no
// container to record — which is exactly the shape D297 refuses to leave
// untraced. Reading a thread goes through home_chat_messages, which audits.
func (p *mcpProvider) Get(ctx context.Context, kind, id string) (mcp.Result, error) {
	if kind != "chat.conversation" {
		return mcp.NotFoundResult(), nil
	}
	c, err := p.svc.GetConversation(ctx, id)
	if err != nil {
		if mcp.IsNotFound(err) {
			return mcp.NotFoundResult(), nil
		}
		return mcp.Result{}, err
	}
	return mcp.TextResult(fmt.Sprintf("%s (id %s) — %d členů, %d nepřečtených",
		c.Name, c.ID, c.MemberCount, c.UnreadCount), c)
}

// ---- Resources ----

func (p *mcpProvider) Resources() []mcp.ResourceTemplate {
	return []mcp.ResourceTemplate{{
		URITemplate: uriPrefix + "{conversation}/attachments/{id}",
		Name:        "attachment",
		Title:       "Příloha v chatu",
		Description: "Jedna příloha z konverzace, které je volající členem.",
	}}
}

// ListResources enumerates the attachments the CALLER may read — leak row 11.
//
// ⚠ IT GOES THROUGH THE MEMBER-SCOPED CLEAN-UP QUERY, which already applies
// membership, the history floor and the koš in SQL. A listing built from a
// different query would be a second place the floor has to be remembered.
//
// ⚠ BUT NOT THROUGH `Service.Cleanup`, AND THAT IS THE POINT. That method carries
// `assertCleanupGate` — member ∧ (editor | admin), D241 — because DELETING an
// attachment is a write. A `reader` calling it is refused 403, the host swallows
// one module's listing failure so the rest survives, and the reader's
// resources/list came back with every note and document and no chat attachment at
// all, from conversations they are in and can still read one at a time. A read
// listing gated by a write permission is a refusal nobody can see.
//
// ⚠ AND THE ORDER IS `recent`, NOT THE CLEAN-UP SCREEN'S `size`. This is a
// listing of what is there, not of what is worth deleting; when the cap cuts it,
// what should fall off the end is the oldest, not the smallest.
func (p *mcpProvider) ListResources(ctx context.Context, limit int) ([]mcp.Resource, error) {
	actor, ok := reqctx.ActorFrom(ctx)
	if !ok || actor.UserID == "" {
		return nil, fmt.Errorf("chat: resource listing without an actor")
	}
	rows, _, _, err := p.svc.store.CleanupItems(ctx, p.svc.db, actor.UserID, "", sortRecent, "", limit)
	if err != nil {
		return nil, err
	}
	out := make([]mcp.Resource, 0, len(rows))
	for _, r := range rows {
		out = append(out, mcp.Resource{
			URI:      fmt.Sprintf("%s%s/attachments/%s", uriPrefix, r.attachment.ConversationID, r.attachment.ID),
			Name:     r.attachment.OriginalFilename,
			MIMEType: r.attachment.ContentType,
			Size:     r.attachment.ByteSize,
		})
	}
	return out, nil
}

// Read serves one attachment, re-checking membership from scratch — leak row 10.
//
// ⚠ THE CONVERSATION IN THE URI IS NOT TRUSTED, AND MUST NOT BE. Access is
// decided by `AttachmentForViewer`, which resolves the attachment's OWN
// conversation and applies membership, the floor, the koš and the tombstone; the
// URI's conversation segment is then checked against what came back. A provider
// that scoped by the segment would let a member of conversation A read an
// attachment of conversation B by pasting A's id in front of B's attachment.
func (p *mcpProvider) Read(ctx context.Context, uri string) (mcp.Content, error) {
	actor := reqctx.ActorID(ctx)
	if actor == "" {
		return mcp.Content{}, fmt.Errorf("chat: resource read without an actor")
	}
	conversationID, attachmentID, ok := parseAttachmentURI(uri)
	if !ok {
		return mcp.Content{}, mcp.ErrResourceNotFound
	}
	if p.svc.blob == nil {
		return mcp.Content{}, httpx.ErrNotImplemented("chat attachment storage is not configured")
	}
	att, err := p.svc.store.AttachmentForViewer(ctx, p.svc.db, actor, attachmentID)
	if err != nil {
		return mcp.Content{}, mcp.ErrResourceNotFound
	}
	// A `removed` attachment has no bytes and a `moved` one's bytes are
	// Dokumenty's — both are not-found here rather than a redirect, exactly as the
	// HTTP route decides it.
	if att.State != stateLive {
		return mcp.Content{}, mcp.ErrResourceNotFound
	}
	if att.ConversationID != conversationID {
		return mcp.Content{}, mcp.ErrResourceNotFound
	}

	rng := blobstore.ByteRange{Offset: 0, Length: maxAttachmentResourceBytes}
	body, _, err := p.svc.blob.Get(ctx, att.StorageKey, &rng)
	if err != nil {
		return mcp.Content{}, err
	}
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(body, maxAttachmentResourceBytes))
	if err != nil {
		return mcp.Content{}, err
	}
	content := mcp.Content{URI: uri, MIMEType: att.ContentType}
	if strings.HasPrefix(att.ContentType, "text/") {
		content.Text = string(raw)
		return content, nil
	}
	content.Blob = raw
	return content, nil
}

// maxAttachmentResourceBytes bounds what one read pulls into this process. See
// documents' twin: it is NOT the host's cap on what reaches the client, and it is
// deliberately larger so the host's is the one that decides the answer.
const maxAttachmentResourceBytes = 8 << 20

// parseAttachmentURI splits home://chat/{conversation}/attachments/{id}.
func parseAttachmentURI(uri string) (conversationID, attachmentID string, ok bool) {
	rest, found := strings.CutPrefix(uri, uriPrefix)
	if !found {
		return "", "", false
	}
	conversationID, rest, found = strings.Cut(rest, "/attachments/")
	if !found || conversationID == "" || rest == "" || strings.Contains(rest, "/") {
		return "", "", false
	}
	return conversationID, rest, true
}

// ---- The audited read (D297) ----

// RecordTokenThreadRead writes `chat.read` when a TOKEN reads a thread.
//
// ⚠ THE CONTAINER, NEVER THE CONTENT. `entity_id` is the conversation, the
// summary names it, and `meta` carries the token id and a message COUNT. No body,
// no snippet, no message id — the Log is not a second copy of the chat, which is
// the entire reason D231 exists.
//
// ⚠ IT DOES NOT REVERSE D231. No message row is ever written to the Log, and
// TestChatMessagesAreNotAudited still passes: sending, editing and deleting a
// message remain invisible in audit_events. What is recorded here is a READ, by
// an assistant, of a container — a different fact about a different actor.
//
// ⚠ AND IT FIRES ONLY WHEN THE READER IS A TOKEN. A browser read writes nothing,
// as it always has.
func (s *Service) RecordTokenThreadRead(ctx context.Context, conversationID string, messageCount int) error {
	tokenID, viaToken := mcpctx.TokenFrom(ctx)
	if !viaToken {
		return nil
	}
	return appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		// ⚠ THE NAME COMES FROM THE ONE-COLUMN READ, INSIDE THE TX. `GetConversation`
		// would answer the same question through memberScope, the conversation read
		// and attachPreviews — three-plus queries on the ONE connection to obtain a
		// string for a summary — and the caller has already passed the membership
		// gate to get here. `ConversationName` is what the clean-up path's own audit
		// write uses, for exactly this.
		name, err := s.store.ConversationName(ctx, tx, conversationID)
		if err != nil {
			return err
		}
		return s.sink.Record(ctx, tx, audit.Event{
			Action: "read",
			// ⚠ THE MODULE'S OWN ENTITY TYPE, not a second spelling of it. Every other
			// chat event is filed under `chat_conversation` (Service.record), and the
			// Log's entity timeline selects on `entity_type = ?` — so a read filed as
			// `conversation` is absent from the history of the very conversation it
			// records, which is the one place somebody asking "what has the assistant
			// seen?" would look.
			EntityType: "chat_conversation",
			EntityID:   conversationID,
			Summary:    "Asistent četl konverzaci „" + name + "“",
			Meta:       map[string]any{"token_id": tokenID, "message_count": messageCount},
		})
	})
}
