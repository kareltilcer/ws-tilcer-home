package chat

import (
	"context"
	"database/sql"
	"errors"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The membership scope (v10, PRD D218/D251) — the third access axis in Home.
//
//	v1–v8  everyone      a row is the household's
//	v9     ownership     a private row is one person's
//	v10    MEMBERSHIP    a conversation is readable by the people in it, from
//	                     their effective_from onward
//
// ⚠ ONE FUNCTION RESOLVES IT AND EVERY READ GOES THROUGH THAT FUNCTION. Two
// implementations of an access rule is one implementation and one bug — the v9
// lesson, recorded in documents/scope.go, in a module where the payload IS the
// content.
//
// ⚠ A FLOOR APPLIED AFTER THE ROWS ARE FETCHED IS NOT A FLOOR. It is a place where
// next_cursor, has_more and any total still describe rows the caller may not read.
// That is why memberScope hands back a BOUND rather than a predicate to test in Go,
// and why the acceptance criteria assert the page metadata rather than only the
// visible rows.

// ErrNotMember is what every conversation-scoped load returns when the caller is
// not in the room, when the room is in the koš, and when the id was never issued.
//
// ⚠ ALL THREE RETURN THE SAME ERROR ON PURPOSE, and the handler renders it as 404,
// NEVER 403. A 403 says "this exists and you may not have it", which is a yes/no
// oracle over conversation ids; the response must be byte-identical to the one an
// unknown id produces, and a test asserts exactly that. The v9 D180 precedent.
var ErrNotMember = errors.New("chat: not a member")

// Scope is what a member may read in ONE conversation: the room, plus the floor.
//
// ⚠ There is no zero value that means anything. Unlike v9's Scope — whose zero
// value is deliberately the shared root, so a pre-v9 call site kept working —
// nothing in chat is readable by default, so a Scope must always come from
// memberScope. A store method that accepted a zero Scope would fail OPEN.
type Scope struct {
	ConversationID string
	// Kind is carried for the WRITE guards only (Všichni refuses delete and leave).
	// ⚠ It is never consulted to decide what history to show — see floor.go.
	Kind string
	// Floor bounds every read of this conversation's messages.
	Floor floor
	// Muted and LastReadAt are the caller's own row, loaded here because every
	// caller of memberScope needs at least one of them and a second query for a
	// column already on the joined row is pure cost.
	Muted      bool
	LastReadAt *string
}

// querier is satisfied by both *sql.DB and *sql.Tx.
//
// ⚠ IT IS NOT A CONVENIENCE. The pool is capped at a single connection because
// SQLite is single-writer (platform/db/db.go), so a read issued on *sql.DB while a
// transaction is open waits for the connection that transaction is holding — a
// guaranteed deadlock. Every read that can run inside a write takes the querier
// explicitly, the way push.Store's rowQuerier already does.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// memberScope resolves membership, the floor and the koš in ONE query.
//
// The `deleted_at IS NULL` term is why a trashed conversation is invisible to every
// read rather than merely absent from the list (D253): the thread, search, unread,
// the reply quote and the attachment listing all come through here, so there is one
// place to state it and no surface that can forget.
func (s *Store) memberScope(ctx context.Context, q querier, actor, conversationID string) (Scope, error) {
	if actor == "" || conversationID == "" {
		return Scope{}, ErrNotMember
	}
	var (
		sc         Scope
		effFrom    string
		effID      string
		mutedInt   int
		lastReadAt sql.NullString
	)
	err := q.QueryRowContext(ctx, `
		SELECT c.kind, m.effective_from, m.effective_from_id, m.muted, m.last_read_at
		  FROM chat_members m
		  JOIN chat_conversations c ON c.id = m.conversation_id
		 WHERE m.conversation_id = ? AND m.user_id = ? AND c.deleted_at IS NULL`,
		conversationID, actor).Scan(&sc.Kind, &effFrom, &effID, &mutedInt, &lastReadAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Scope{}, ErrNotMember
	}
	if err != nil {
		return Scope{}, err
	}
	sc.ConversationID = conversationID
	sc.Floor = floor{At: effFrom, ID: effID}
	sc.Muted = mutedInt != 0
	if lastReadAt.Valid {
		sc.LastReadAt = &lastReadAt.String
	}
	return sc, nil
}

// adminScope resolves a conversation for the TWO verbs an admin has over a room
// they are not in: restore and purge (D255).
//
// ⚠ IT IS NOT A READ WIDENING, AND THE ASYMMETRY IS THE POINT. An admin may bring
// a conversation back from the koš and may destroy it, and GET of that same
// conversation must still 404 for them. One test asserts both halves together so
// the asymmetry is visible rather than looking like an inconsistency somebody
// should tidy up. It follows v9's D181 exactly: an admin hard-deleting a foreign
// private item is doing a write to a row they may not read.
//
// It returns the koš state alongside the kind, because the three verbs that come
// here are the only ones that ACT ON the koš and each needs a different answer to
// "is it already in there?" — see conversationForDestructiveVerb.
//
// Nothing here returns a floor, because nothing here reads a message.
func (s *Store) adminScope(ctx context.Context, q querier, conversationID string) (kind string, trashed bool, err error) {
	err = q.QueryRowContext(ctx,
		`SELECT kind, deleted_at IS NOT NULL FROM chat_conversations WHERE id = ?`,
		conversationID).Scan(&kind, &trashed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, ErrNotMember
	}
	return kind, trashed, err
}

// ---- request plumbing ----

func actorID(ctx context.Context) string {
	if a, ok := reqctx.ActorFrom(ctx); ok {
		return a.UserID
	}
	return ""
}

func isAdminCtx(ctx context.Context) bool {
	a, ok := reqctx.ActorFrom(ctx)
	if !ok {
		return false
	}
	return reqctx.HasRole(a.Roles, "admin")
}

// notFound is the ONE refusal this module has for a conversation the caller may
// not see. The detail string is fixed, because a message that varied with the
// reason would re-open the oracle the 404 closes.
func notFound() error { return httpx.ErrNotFound("Konverzace nebyla nalezena.") }

// mapScopeErr turns the store's membership verdict into the wire refusal. Every
// handler funnels through it so no route can invent a 403.
func mapScopeErr(err error) error {
	if errors.Is(err, ErrNotMember) {
		return notFound()
	}
	return err
}
