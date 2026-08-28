package chat

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
)

// TargetedNotifier publishes a websocket change to a NAMED SET of members after
// commit — the v10 shape, over hub.NotifyTo.
//
// ⚠ EVERY OTHER MODULE IN HOME TAKES `Notifier func(ctx, typ, payload)` AND
// BROADCASTS. Chat cannot: its payload is the thing that must not leak, so the
// audience is an argument. The ctx still carries the originating request's client
// id, which the hub stamps as Origin — that is how the sender's own tab tells its
// echo apart from somebody else's message, and it is why this goes through
// hub.NotifyTo rather than a ws.Message this module assembled itself.
type TargetedNotifier func(ctx context.Context, userIDs []string, typ string, payload any)

// Options are the module's composition-time settings.
type Options struct {
	// TrashDays is HOME_CHAT_TRASH_DAYS — how long a deleted conversation sits in
	// the koš before the drain destroys its bytes. Chat's only env var.
	TrashDays int
	Logger    *slog.Logger
}

// Service orchestrates chat: validate → memberScope → WithTx → notify + push.
//
// ⚠ ITS PRIMARY MUTATION WRITES NO AUDIT EVENT (D231), which makes it the first
// module in Home whose main verb leaves nothing in the Log. Sending, editing and
// deleting a message are all invisible there, deliberately —
// TestChatMessagesAreNotAudited asserts it so that "the missing audit coverage" is
// never fixed by accident. Structural changes (rooms, membership) ARE audited, and
// so are attachments in PR 3: the bytes are what the storage half exists for.
type Service struct {
	db        *sql.DB
	store     *Store
	sink      audit.Sink
	notifyTo  TargetedNotifier
	pusher    push.Sender
	directory DirectorySource
	trashDays int
	logger    *slog.Logger
}

func NewService(db *sql.DB, sink audit.Sink, notifyTo TargetedNotifier, pusher push.Sender, directory DirectorySource, opts Options) *Service {
	if notifyTo == nil {
		notifyTo = func(context.Context, []string, string, any) {}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.TrashDays < 1 {
		opts.TrashDays = 7
	}
	return &Service{
		db: db, store: NewStore(db), sink: sink, notifyTo: notifyTo,
		pusher: pusher, directory: directory,
		trashDays: opts.TrashDays, logger: opts.Logger,
	}
}

// Store exposes the read store to the module's own registrations.
func (s *Service) Store() *Store { return s.store }

// record writes one audit event inside the caller's tx. The error is returned
// unchanged so the transaction rolls back: an action that succeeds unlogged is the
// bug the spine exists to prevent.
//
// ⚠ THERE IS NO MESSAGE EQUIVALENT OF THIS FUNCTION, and there must not be (D231).
func (s *Service) record(ctx context.Context, tx *sql.Tx, action, entityID, summary string, changes []audit.Change) error {
	_, err := s.sink.Record(ctx, tx, audit.Event{
		Module:     audit.ModuleChat,
		Action:     action,
		EntityType: "chat_conversation",
		EntityID:   entityID,
		Summary:    summary,
		Changes:    changes,
	})
	return err
}

// ---- validation ----

const (
	maxNameRunes = 80
	maxBodyRunes = 8000
)

func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", httpx.ErrUnprocessable("Název konverzace nesmí být prázdný.")
	}
	if len([]rune(name)) > maxNameRunes {
		return "", httpx.ErrUnprocessable("Název konverzace může mít nejvýše 80 znaků.")
	}
	return name, nil
}

func validateBody(body string) (string, error) {
	body = strings.TrimRight(body, " \t\r\n")
	if len([]rune(body)) > maxBodyRunes {
		return "", httpx.ErrUnprocessable("Zpráva může mít nejvýše 8 000 znaků.")
	}
	return body, nil
}

// ---- conversations ----

// ListConversations returns the caller's rooms, active or trashed.
//
// The auto-join has already run: it is the router's own middleware (http.go), so
// every chat request carries it rather than each service method remembering to.
func (s *Service) ListConversations(ctx context.Context, state, cursor string, limit int) (ConversationPage, error) {
	actor := actorID(ctx)
	if actor == "" {
		return ConversationPage{}, httpx.ErrUnauthorized("")
	}
	if state != "" && state != "active" && state != "trash" {
		return ConversationPage{}, httpx.ErrUnprocessable("Parametr state musí být active nebo trash.")
	}
	items, hasMore, err := s.store.ListConversations(ctx, actor, state, cursor, limit)
	if errors.Is(err, errBadCursor) {
		return ConversationPage{}, httpx.ErrUnprocessable("Neplatný kurzor.")
	}
	if err != nil {
		return ConversationPage{}, err
	}
	if state == "trash" {
		for i := range items {
			items[i].PurgeAfter = s.purgeAfter(items[i].DeletedAt)
		}
	}
	page := ConversationPage{Items: items}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		page.NextCursor = ptr(encodeConversationCursor(last.UpdatedAt, last.ID))
	}
	return page, nil
}

// purgeAfter derives when the drain will destroy a trashed conversation's bytes.
//
// Derived rather than stored, because HOME_CHAT_TRASH_DAYS is configuration: a row
// written when the window was seven days would keep claiming seven after somebody
// changed it to three, and the koš's whole promise is a number the member can rely
// on.
func (s *Service) purgeAfter(deletedAt *string) *string {
	if deletedAt == nil {
		return nil
	}
	t, err := time.Parse(tsFormat, *deletedAt)
	if err != nil {
		return nil
	}
	return ptr(t.AddDate(0, 0, s.trashDays).UTC().Format(tsFormat))
}

func (s *Service) GetConversation(ctx context.Context, id string) (Conversation, error) {
	actor := actorID(ctx)
	sc, err := s.store.memberScope(ctx, s.db, actor, id)
	if err != nil {
		return Conversation{}, mapScopeErr(err)
	}
	c, err := s.store.GetConversation(ctx, s.db, actor, sc)
	return c, mapScopeErr(err)
}

// CreateConversation makes a group.
//
// ⚠ NO ROLE GATE (D222). A `reader` creates conversations, adds members and deletes
// rooms — chat is the first module in Home where a reader writes. The one thing
// they cannot do is clean up storage, which is PR 3's `/chat/uklid` and the single
// recorded asymmetry in the module.
//
// Every member — the creator included — joins with `effective_from = now`: nobody
// joins a new conversation with history behind them, and there is no history yet
// anyway.
func (s *Service) CreateConversation(ctx context.Context, in ConversationCreate) (Conversation, error) {
	actor := actorID(ctx)
	if actor == "" {
		return Conversation{}, httpx.ErrUnauthorized("")
	}
	name, err := validateName(in.Name)
	if err != nil {
		return Conversation{}, err
	}
	known, err := s.labels(ctx)
	if err != nil {
		return Conversation{}, err
	}
	for _, id := range in.MemberIDs {
		if id == actor {
			continue
		}
		if _, ok := known[id]; !ok {
			// ⚠ The directory is a login history: an id nobody has ever had a
			// session for is not a member yet. Refusing here is what stops a
			// conversation being created around somebody who does not exist.
			return Conversation{}, httpx.ErrUnprocessable("Tento člen není v adresáři domácnosti.")
		}
	}

	id := idgen.New()
	now := nowUTC()
	// A brand-new room has no history, so every founding member's floor is the
	// conversation's own beginning — the same value the Všichni auto-join uses, for
	// the same reason and through the same constructor. "Nobody joins a new
	// conversation with history behind them" and "everybody reads all of it" are
	// the same statement when there is nothing before the first message.
	f, err := beginningOfConversation(now)
	if err != nil {
		return Conversation{}, err
	}
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.store.InsertConversation(ctx, tx, id, name, actor, now); err != nil {
			return err
		}
		if err := s.store.InsertMember(ctx, tx, id, actor, actor, f); err != nil {
			return err
		}
		for _, m := range in.MemberIDs {
			if m == actor {
				continue
			}
			if err := s.store.InsertMember(ctx, tx, id, m, actor, f); err != nil {
				return err
			}
		}
		return s.record(ctx, tx, "conversation.created", id,
			"Vytvořena konverzace „"+name+"“", nil)
	})
	if err != nil {
		return Conversation{}, err
	}
	return s.GetConversation(ctx, id)
}

func (s *Service) RenameConversation(ctx context.Context, id string, in ConversationUpdate) (Conversation, error) {
	actor := actorID(ctx)
	name, err := validateName(in.Name)
	if err != nil {
		return Conversation{}, err
	}
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		sc, err := s.store.memberScope(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		old, err := s.store.ConversationName(ctx, tx, sc.ConversationID)
		if err != nil {
			return err
		}
		if err := s.store.RenameConversation(ctx, tx, sc.ConversationID, name, nowUTC()); err != nil {
			return err
		}
		// Renaming Všichni is allowed (D219): only delete and leave are refused.
		return s.record(ctx, tx, "conversation.renamed", sc.ConversationID,
			"Konverzace přejmenována na „"+name+"“",
			[]audit.Change{{Field: "name", Old: &old, New: &name}})
	})
	if err != nil {
		return Conversation{}, mapScopeErr(err)
	}
	return s.GetConversation(ctx, id)
}

// DeleteConversation moves a room to the koš, or purges it (D253).
//
// ⚠ ANY MEMBER MAY DELETE A ROOM CONTAINING EVERYONE ELSE'S FILES. That is the
// decision; the koš is what makes it survivable rather than impossible, which is
// also why the confirmation asks for the conversation's name to be typed.
//
// ⚠ `hard` EXISTS SO SOMEBODY DELETING A HEAVY CONVERSATION TO FIX AN OVERRUN IS
// NEVER TOLD TO COME BACK IN SEVEN DAYS. It rewrites the queued keys' purge_after
// to now rather than deleting anything inline — the drain still does the work.
func (s *Service) DeleteConversation(ctx context.Context, id string, hard bool) error {
	actor := actorID(ctx)
	now := nowUTC()
	return mapScopeErr(appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		kind, trashed, memberErr := s.conversationForDestructiveVerb(ctx, tx, actor, id)
		if memberErr != nil {
			return memberErr
		}
		if kind == kindDefault {
			return httpx.ErrUnprocessable("Konverzaci „Všichni“ nelze smazat.")
		}
		// ⚠ A SOFT DELETE OF SOMETHING ALREADY IN THE KOŠ IS REFUSED, NOT REPEATED.
		// TrashConversation's own `deleted_at IS NULL` makes the UPDATE a no-op, so
		// without this the request still succeeded — writing a second "přesunuta do
		// koše" to the Log and, worse, re-queuing every key with a purge_after of
		// now + TrashDays while deleted_at (and therefore the countdown the koš row
		// renders) stayed put. The promise on screen and the deadline the drain
		// holds would drift apart by a whole retention window, every time.
		//
		// `hard` is deliberately NOT refused: Smazat natrvalo is reached FROM the
		// koš, and it is the one verb whose job is to act on an already-trashed room.
		if trashed && !hard {
			return httpx.ErrUnprocessable("Konverzace už je v koši.")
		}
		name, err := s.store.ConversationName(ctx, tx, id)
		if err != nil {
			return err
		}
		if hard {
			if err := s.queuePurge(ctx, tx, id, now, now); err != nil {
				return err
			}
			if err := s.store.PurgeConversationRows(ctx, tx, id); err != nil {
				return err
			}
			return s.record(ctx, tx, "conversation.purged", id,
				"Konverzace „"+name+"“ smazána natrvalo", nil)
		}
		purgeAt := time.Now().UTC().AddDate(0, 0, s.trashDays).Format(tsFormat)
		if err := s.queuePurge(ctx, tx, id, now, purgeAt); err != nil {
			return err
		}
		if err := s.store.TrashConversation(ctx, tx, id, actor, now); err != nil {
			return err
		}
		return s.record(ctx, tx, "conversation.deleted", id,
			"Konverzace „"+name+"“ přesunuta do koše", nil)
	}))
}

// RestoreConversation brings a room back from the koš and withdraws the promise to
// delete its objects, in one transaction. Nothing is reconstructed; nothing was
// ever removed.
//
// ⚠ IT RETURNS nil FOR AN ADMIN WHO IS NOT A MEMBER, and the handler renders that
// as 204. The restore itself is a storage verb an admin legitimately has over a
// room they are not in (D255) — but the CONVERSATION IS A READ, and handing it back
// in the response body would be the read the very next GET correctly refuses them.
// Returning the room to whoever may see it, and nothing to whoever may not, is what
// keeps those two answers consistent.
func (s *Service) RestoreConversation(ctx context.Context, id string) (*Conversation, error) {
	actor := actorID(ctx)
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		_, trashed, err := s.conversationForDestructiveVerb(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		// ⚠ RESTORING A ROOM THAT IS NOT IN THE KOŠ DOES NOTHING, SILENTLY. The
		// mirror of the guard in DeleteConversation: RestoreConversation's UPDATE
		// already lands on a row that is not trashed without changing it, so the
		// only thing the rest of this block would add is a "obnovena z koše" event
		// for a restore that restored nothing. Idempotent rather than 422, because
		// a double tap on Obnovit has plainly got what it asked for.
		if !trashed {
			return nil
		}
		name, err := s.store.ConversationName(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := s.store.RestoreConversation(ctx, tx, id, nowUTC()); err != nil {
			return err
		}
		return s.record(ctx, tx, "conversation.restored", id,
			"Konverzace „"+name+"“ obnovena z koše", nil)
	})
	if err != nil {
		return nil, mapScopeErr(err)
	}
	// The membership question is asked directly rather than inferred from a 404, so
	// "the admin half of D255" is a branch on the access rule itself and not on the
	// shape of an error some other code path might also produce.
	sc, scErr := s.store.memberScope(ctx, s.db, actor, id)
	if errors.Is(scErr, ErrNotMember) {
		return nil, nil
	}
	if scErr != nil {
		return nil, scErr
	}
	c, err := s.store.GetConversation(ctx, s.db, actor, sc)
	if err != nil {
		return nil, mapScopeErr(err)
	}
	return &c, nil
}

// conversationForDestructiveVerb resolves a conversation for delete, purge and
// restore.
//
// ⚠ THIS IS THE ASYMMETRY, AND IT IS DELIBERATE (D255). An `admin` who is not a
// member MAY restore and purge — those are storage verbs over a room that is
// costing the household money — and GET of that same conversation must STILL 404
// for them. One test asserts both halves together so the asymmetry reads as a
// decision rather than an inconsistency somebody should tidy up. It follows v9's
// D181 exactly: an admin hard-deleting a foreign private item is doing a write to a
// row they may not read.
//
// A member's own membership is checked FIRST, so the ordinary case never depends on
// a role at all.
// It also reports whether the conversation is ALREADY in the koš, because each of
// the three verbs needs a different answer to that: a soft delete refuses, a purge
// proceeds (that is Smazat natrvalo, reached FROM the koš), and a restore of a live
// room does nothing at all.
func (s *Service) conversationForDestructiveVerb(ctx context.Context, tx *sql.Tx, actor, id string) (kind string, trashed bool, err error) {
	// memberScope excludes a trashed conversation, which is right for every read
	// and wrong for exactly these three verbs — restore and purge act ON the koš.
	// So membership is asked without the koš predicate here.
	err = tx.QueryRowContext(ctx, `
		SELECT c.kind, c.deleted_at IS NOT NULL FROM chat_members m
		  JOIN chat_conversations c ON c.id = m.conversation_id
		 WHERE m.conversation_id = ? AND m.user_id = ?`, id, actor).Scan(&kind, &trashed)
	if err == nil {
		return kind, trashed, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	if !isAdminCtx(ctx) {
		return "", false, ErrNotMember
	}
	return s.store.adminScope(ctx, tx, id)
}

// queuePurge enqueues a conversation's object keys for the drain.
//
// ⚠ THE OBJECTS ARE NOT DELETED HERE, and the reason is a request that would
// otherwise block on bulk object I/O: a conversation with four hundred attachments
// is not one or two DELETE calls. PR 2 has no attachments, so this enqueues nothing
// today; it is written now because the delete path is written now, and a queue
// added later is a queue that misses everything deleted in between.
func (s *Service) queuePurge(ctx context.Context, tx *sql.Tx, conversationID, queuedAt, purgeAfter string) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO chat_deleted_keys (key, queued_at, purge_after)
		SELECT storage_key, ?, ? FROM chat_attachments
		 WHERE conversation_id = ? AND state = 'live'
		    ON CONFLICT (key) DO UPDATE SET purge_after = excluded.purge_after`,
		queuedAt, purgeAfter, conversationID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO chat_deleted_keys (key, queued_at, purge_after)
		SELECT thumbnail_key, ?, ? FROM chat_attachments
		 WHERE conversation_id = ? AND state = 'live' AND thumbnail_key IS NOT NULL
		    ON CONFLICT (key) DO UPDATE SET purge_after = excluded.purge_after`,
		queuedAt, purgeAfter, conversationID)
	return err
}

// ---- membership ----

func (s *Service) ListMembers(ctx context.Context, id string) (ConversationMemberList, error) {
	actor := actorID(ctx)
	sc, err := s.store.memberScope(ctx, s.db, actor, id)
	if err != nil {
		return ConversationMemberList{}, mapScopeErr(err)
	}
	labels, err := s.labels(ctx)
	if err != nil {
		return ConversationMemberList{}, err
	}
	items, err := s.store.ListMembers(ctx, s.db, sc.ConversationID, actor, labels)
	if err != nil {
		return ConversationMemberList{}, err
	}
	return ConversationMemberList{Items: items}, nil
}

// AddMember adds somebody, with the floor at now.
//
// ⚠ THE FLOOR IS WHY THIS IS SAFE TO LET ANY MEMBER DO. Being added to a group is
// one person's decision about another person's access to a THIRD person's history,
// so the new member starts reading from the moment they were added and never sees
// what came before (D218).
func (s *Service) AddMember(ctx context.Context, id string, in ConversationMemberAdd) (ConversationMemberList, error) {
	actor := actorID(ctx)
	if strings.TrimSpace(in.UserID) == "" {
		return ConversationMemberList{}, httpx.ErrUnprocessable("Chybí uživatel.")
	}
	labels, err := s.labels(ctx)
	if err != nil {
		return ConversationMemberList{}, err
	}
	if _, ok := labels[in.UserID]; !ok {
		return ConversationMemberList{}, httpx.ErrUnprocessable("Tento člen není v adresáři domácnosti.")
	}
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		sc, err := s.store.memberScope(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		name, err := s.store.ConversationName(ctx, tx, sc.ConversationID)
		if err != nil {
			return err
		}
		// ⚠ The floor is read INSIDE this transaction, so a message committing
		// concurrently either precedes the add and is excluded, or follows it and is
		// included — never both and never neither.
		f, err := floorNow(ctx, tx, sc.ConversationID)
		if err != nil {
			return err
		}
		if err := s.store.InsertMember(ctx, tx, sc.ConversationID, in.UserID, actor, f); err != nil {
			return err
		}
		return s.record(ctx, tx, "member.added", sc.ConversationID,
			"Do konverzace „"+name+"“ přidán člen "+label(labels, in.UserID), nil)
	})
	if err != nil {
		return ConversationMemberList{}, mapScopeErr(err)
	}
	return s.ListMembers(ctx, id)
}

// RemoveMember takes somebody out — including the caller themselves, which is how
// leaving works.
//
// ⚠ RE-ADDING LEAVES A PERMANENT GAP (D218) and the members screen says so before
// this is confirmed, because nothing afterwards would explain it.
//
// The removed member is told over /ws, TO THEM SPECIFICALLY, so their client can
// leave a thread that has quietly become forbidden. No socket is force-closed and
// their already-fetched page is not scrubbed; the next request 404s. That is the
// accepted bound in leak row 22.
func (s *Service) RemoveMember(ctx context.Context, id, userID string) error {
	actor := actorID(ctx)
	var (
		removed bool
		labels  map[string]string
	)
	labels, err := s.labels(ctx)
	if err != nil {
		return err
	}
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		sc, err := s.store.memberScope(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if sc.Kind == kindDefault {
			// Nobody leaves the household room and nobody is removed from it
			// (D219): it is the one conversation whose membership is the household.
			return httpx.ErrUnprocessable("Z konverzace „Všichni“ nelze nikoho odebrat.")
		}
		name, err := s.store.ConversationName(ctx, tx, sc.ConversationID)
		if err != nil {
			return err
		}
		removed, err = s.store.RemoveMember(ctx, tx, sc.ConversationID, userID)
		if err != nil {
			return err
		}
		if !removed {
			// Not a member of this room. Same answer as an unknown id — a removal
			// that reported "they were not in it" would be a membership oracle.
			return ErrNotMember
		}
		return s.record(ctx, tx, "member.removed", sc.ConversationID,
			"Z konverzace „"+name+"“ odebrán člen "+label(labels, userID), nil)
	})
	if err != nil {
		return mapScopeErr(err)
	}
	s.notifyTo(ctx, []string{userID}, "chat_membership.changed", MembershipEvent{
		ConversationID: id, UserID: userID, Removed: true,
	})
	return nil
}

// UpdateSelf sets the caller's own per-conversation mute (D248).
func (s *Service) UpdateSelf(ctx context.Context, id string, in ConversationMemberSelfUpdate) (Conversation, error) {
	actor := actorID(ctx)
	if in.Muted == nil {
		return Conversation{}, httpx.ErrUnprocessable("Není co změnit.")
	}
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		sc, err := s.store.memberScope(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		// ⚠ NOT AUDITED. A mute is a personal preference, not a structural change,
		// and the Log is admin-only — recording who silenced which room would put a
		// private choice in front of an audience it was never meant for.
		return s.store.SetMuted(ctx, tx, sc.ConversationID, actor, *in.Muted)
	})
	if err != nil {
		return Conversation{}, mapScopeErr(err)
	}
	return s.GetConversation(ctx, id)
}
