package chat

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/optional"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
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

	// Blob is the primary object store, under the `chat/` prefix (D229). Nil in a
	// host that wires none: uploads then refuse and the delivery routes 404, which
	// is the same shape `documents` takes.
	Blob blobstore.BlobStore
	// Sink is `documents` accepting custody, handed over at composition as an
	// OPTIONAL dependency (D238/D239).
	//
	// ⚠ NIL IS A SUPPORTED STATE AND IT MEANS 501, NEVER A FALLBACK TO DELETE. A
	// capability that silently becomes a different, DESTRUCTIVE capability is worse
	// than one that is plainly absent — so the move refuses and the UI renders no
	// button.
	Sink storage.BlobSink
	// Upload is the per-file cap and the thumbnail toolchain, all of it read from
	// Dokumenty's configuration (D228). There is deliberately no HOME_CHAT_*
	// equivalent of any of it.
	Upload UploadOptions
}

// UploadOptions bounds an attachment and describes how to thumbnail it.
type UploadOptions struct {
	// MaxBytes is HOME_DOCS_MAX_UPLOAD_MB in bytes — Dokumenty's cap, shared on
	// purpose so every chat attachment is movable into Dokumenty by construction.
	MaxBytes int64
	TempDir  string
	Thumb    ThumbOptions
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
	sink      audit.ModuleSink
	notifyTo  TargetedNotifier
	pusher    push.Sender
	directory DirectorySource
	trashDays int
	logger    *slog.Logger
	blob      blobstore.BlobStore
	blobSink  storage.BlobSink
	upload    UploadOptions
	// moveFault is the fault-injection seam for the custody transfer, nil in
	// production and set only by tests in this package.
	//
	// ⚠ IT EXISTS BECAUSE THE MOVE IS THE ONE THING IN v10 THAT CAN DESTROY DATA
	// SILENTLY, and the acceptance criteria ask for a failure injected at each of
	// its five steps with the resulting state asserted and the move re-run from it.
	// Steps 2 and 3 live inside the sink and a fake sink covers them; steps 4 and 5
	// are chat's own SQLite write and object delete, and there is no honest way to
	// make those fail from outside. A nil-by-default hook is cheaper than the
	// alternative — shipping the matrix untested, on the only path in this version
	// that can lose a file.
	moveFault func(step moveStep) error
	// dir memoises the directory projection for directoryTTL — see push.go. It is
	// why a Service must never be copied.
	dir directoryCache
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
	if opts.Upload.MaxBytes <= 0 {
		// The documents default, restated rather than imported: a host that wires no
		// upload options still gets D228's cap and not an unbounded one.
		opts.Upload.MaxBytes = 50 << 20
	}
	return &Service{
		db: db, store: NewStore(db), sink: audit.For(sink, audit.ModuleChat), notifyTo: notifyTo,
		pusher: pusher, directory: directory,
		trashDays: opts.TrashDays, logger: opts.Logger,
		blob: opts.Blob, blobSink: opts.Sink, upload: opts.Upload,
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
	return s.sink.Record(ctx, tx, audit.Event{
		Action:     action,
		EntityType: "chat_conversation",
		EntityID:   entityID,
		Summary:    summary,
		Changes:    changes,
	})
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
	actor := reqctx.ActorID(ctx)
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
	// ⚠ THE VERDICT LIVES HERE, NOT IN THE STORE — the threshold comparison belongs
	// to the consumer (D235's rule, which is why GroupSource reports bytes and
	// nothing else). The store measures; this decides what the number means.
	//
	// ⚠ AND IT IS WHAT LETS THE LIST FLAG AN OVER-LIMIT ROOM AT ALL. Both fields
	// shipped null from PR 2 with a note saying PR 3 would fill them, and PR 3
	// nearly did not: a list permanently rendering *nezměřeno* beside every room,
	// with no way to see which one is heavy, on the version whose second half is a
	// storage register.
	//
	// A threshold read that fails leaves both verdicts null rather than false — the
	// D161 shape one field up: an unmeasured verdict must not serialise as "under
	// the limit".
	if th, thErr := storage.LoadThresholds(ctx, s.db); thErr == nil {
		limit := storage.MB(th.Conversation.ValueMB)
		for i := range items {
			if items[i].Bytes == nil {
				continue
			}
			over := *items[i].Bytes > limit
			items[i].OverConversationLimit = &over
		}
	} else {
		s.logger.Warn("chat: could not read the conversation threshold for the list", "err", thErr)
	}
	// ⚠ NOT ON THE KOŠ LISTING (v10.1 review round 2). LastMessages carries
	// memberScope's own `deleted_at IS NULL` term, so every row here is one it is
	// guaranteed to answer nothing for — and running it anyway spent the whole
	// directory projection plus the preview CTE, on a pool capped at a single
	// connection, every time somebody opened the section.
	if state != "trash" {
		if err := s.attachPreviews(ctx, actor, items); err != nil {
			return ConversationPage{}, err
		}
	}
	page := ConversationPage{Items: items}
	// ⚠ ON BOTH LISTINGS, because the sidebar hides an empty koš (D267) and the koš
	// listing is fetched only when the section is opened — so the count has to ride
	// the request the sidebar always makes, which is the ACTIVE one. Answering it on
	// `?state=trash` as well costs one indexed count and keeps the field meaning one
	// thing on every response rather than two.
	trashed, err := s.store.TrashedCount(ctx, s.db, actor)
	if err != nil {
		return ConversationPage{}, err
	}
	page.TrashedCount = trashed
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		page.NextCursor = optional.Of(encodeConversationCursor(last.UpdatedAt, last.ID))
	}
	return page, nil
}

// attachPreviews fills each row's `last_message` (v10.1, D266).
//
// ⚠ ONE QUERY FOR THE WHOLE PAGE, not one per room, and it is the same argument
// renderMessages makes about quotes and attachments: fifty rooms is fifty places to
// forget the floor, against a pool capped at a single connection.
//
// ⚠ A ROW WITH NO PREVIEW IS LEFT NULL RATHER THAN GIVEN AN EMPTY ONE. Null covers
// two situations that look the same from here and read the same on the row — a room
// nobody has written in, and a member whose floor sits above everything written so
// far — and neither is "" (D226's shape: an empty answer is a shape, not a blank).
func (s *Service) attachPreviews(ctx context.Context, actor string, items []Conversation) error {
	if len(items) == 0 {
		return nil
	}
	labels, err := s.labels(ctx)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(items))
	for _, c := range items {
		ids = append(ids, c.ID)
	}
	previews, err := s.store.LastMessages(ctx, s.db, actor, ids)
	if err != nil {
		return err
	}
	for i := range items {
		p, ok := previews[items[i].ID]
		if !ok {
			continue
		}
		p.AuthorLabel = label(labels, p.AuthorID)
		items[i].LastMessage = &p
	}
	return nil
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
	return optional.Of(t.AddDate(0, 0, s.trashDays).UTC().Format(tsFormat))
}

// GetConversation is ONE query, and the store's own predicate is the access rule.
//
// ⚠ IT NO LONGER RESOLVES A SCOPE FIRST (v10 review). memberScope and
// Store.GetConversation join the same two tables on the same two columns, so
// running both meant every conversation read — the handler's, and the tail of
// Create, Rename and UpdateSelf after their transaction commits — paid two round
// trips on a single-connection pool for one row. Store.GetConversation now carries
// the koš term memberScope contributed, which was the only part of that first query
// this path used. scope.go's rule is unchanged: this is still one predicate, in
// SQL, refusing non-member, trashed and unknown with the same answer.
func (s *Service) GetConversation(ctx context.Context, id string) (Conversation, error) {
	actor := reqctx.ActorID(ctx)
	c, err := s.store.GetConversation(ctx, s.db, actor, id)
	if err != nil {
		return Conversation{}, mapScopeErr(err)
	}
	// ⚠ THE SAME SHAPE THE LIST HANDS BACK, and that is not tidiness. The client
	// holds this room under its own key and the list rows under another; a
	// `Conversation` whose `last_message` is present on one and absent on the other
	// is a row that loses its preview line the moment anything refetches the single
	// room — which every send, rename and read-marker advance does.
	single := []Conversation{c}
	if err := s.attachPreviews(ctx, actor, single); err != nil {
		return Conversation{}, err
	}
	return single[0], nil
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
	actor := reqctx.ActorID(ctx)
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
	// The founding members other than the creator, collected as they are written so
	// the /ws fan-out below matches the rows that actually landed.
	var joined []string
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		joined = joined[:0]
		if err := s.store.InsertConversation(ctx, tx, id, name, actor, now); err != nil {
			return err
		}
		if _, err := s.store.InsertMember(ctx, tx, id, actor, actor, f); err != nil {
			return err
		}
		for _, m := range in.MemberIDs {
			if m == actor {
				continue
			}
			added, err := s.store.InsertMember(ctx, tx, id, m, actor, f)
			if err != nil {
				return err
			}
			// A duplicate id in the request writes one row and is announced once —
			// the same thing InsertMember's ON CONFLICT already says about the write.
			if added {
				joined = append(joined, m)
			}
		}
		return s.record(ctx, tx, "conversation.created", id,
			"Vytvořena konverzace „"+name+"“", nil)
	})
	if err != nil {
		return Conversation{}, err
	}
	// ⚠ THE FOUNDING MEMBERS ARE TOLD, EXACTLY AS AN ADDED ONE IS (v10 review).
	// AddMember publishes a MembershipEvent to the person it happened to precisely
	// because "their client is holding a conversation list that does not contain
	// this room and has no reason to refetch" — and creating a room AROUND somebody
	// puts them in the identical position, so the room and its unread badge stayed
	// invisible to them until a refetch-on-focus or the first message. The creator
	// gets nothing here: they are holding the response.
	for _, m := range joined {
		s.notifyTo(ctx, []string{m}, "chat_membership.changed", MembershipEvent{
			ConversationID: id, UserID: m, Removed: false,
		})
	}
	return s.GetConversation(ctx, id)
}

func (s *Service) RenameConversation(ctx context.Context, id string, in ConversationUpdate) (Conversation, error) {
	actor := reqctx.ActorID(ctx)
	name, err := validateName(in.Name)
	if err != nil {
		return Conversation{}, err
	}
	var audience []string
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
		audience, err = s.store.MemberIDs(ctx, tx, sc.ConversationID)
		if err != nil {
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
	s.publishConversation(ctx, audience, id, false)
	return s.GetConversation(ctx, id)
}

// publishConversation tells a room's members that the room itself changed.
//
// ⚠ THE FRAME CARRIES NO NAME (ConversationEvent). Every recipient refetches
// through the membership join, so what they are told is only that something moved;
// what they get back is whatever the access rule says they may have.
func (s *Service) publishConversation(ctx context.Context, audience []string, id string, gone bool) {
	if len(audience) == 0 {
		return
	}
	s.notifyTo(ctx, audience, "chat_conversation.changed", ConversationEvent{
		ConversationID: id, Gone: gone,
	})
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
	actor := reqctx.ActorID(ctx)
	now := nowUTC()
	var audience []string
	err := mapScopeErr(appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		// ⚠ `hard` is what opens the admin door, and only `hard`. Purging frees the
		// bytes an admin is answerable for; moving a room to the koš frees nothing,
		// so a non-member admin gets the same 404 there that GET already gives them.
		kind, trashed, memberErr := s.conversationForDestructiveVerb(ctx, tx, actor, id, hard)
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
		// ⚠ THE AUDIENCE IS READ BEFORE THE ROWS GO. A purge cascades chat_members
		// away, so resolving it afterwards would find nobody to tell that the room
		// they have open no longer exists.
		audience, err = s.store.MemberIDs(ctx, tx, id)
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
	if err != nil {
		return err
	}
	// Gone: the room has left every read its members have — the thread, the list,
	// the search. Their tabs leave it rather than sitting on a thread that 404s.
	s.publishConversation(ctx, audience, id, true)
	return nil
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
	actor := reqctx.ActorID(ctx)
	var audience []string
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		_, trashed, err := s.conversationForDestructiveVerb(ctx, tx, actor, id, true)
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
		audience, err = s.store.MemberIDs(ctx, tx, id)
		if err != nil {
			return err
		}
		return s.record(ctx, tx, "conversation.restored", id,
			"Konverzace „"+name+"“ obnovena z koše", nil)
	})
	if err != nil {
		return nil, mapScopeErr(err)
	}
	// Empty when the restore was a no-op (the room was not in the koš), which is
	// what keeps a double tap on Obnovit from publishing a second frame.
	s.publishConversation(ctx, audience, id, false)
	// The membership question is asked directly rather than inferred from a 404, so
	// "the admin half of D255" is a branch on the access rule itself and not on the
	// shape of an error some other code path might also produce. ErrNotMember IS
	// that rule's answer — Store.GetConversation resolves membership and the koš in
	// its own predicate (v10 review), so the question costs the one read that
	// answers it rather than a scope resolution and then the same join again.
	c, err := s.store.GetConversation(ctx, s.db, actor, id)
	if errors.Is(err, ErrNotMember) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// ⚠ AND IT CARRIES THE PREVIEW LIKE EVERY OTHER `Conversation` (v10.1 review
	// round 2). This is the one response in the module built from the store's row
	// directly rather than through Service.GetConversation, so it was the one that
	// serialised `last_message: null` for a room full of messages — the exact
	// disagreement between two Conversation-shaped answers that GetConversation's own
	// note says must not exist.
	restored := []Conversation{c}
	if err := s.attachPreviews(ctx, actor, restored); err != nil {
		return nil, err
	}
	return &restored[0], nil
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
//
// ⚠ `adminMayAct` IS PER VERB, AND ONLY restore AND purge PASS true (v10 review).
// The admin fallback is justified by STORAGE — a room that is costing the household
// money — so it covers the two verbs that free bytes and the SOFT DELETE IS NOT ONE
// OF THEM: it frees nothing, it is reversible, and letting it through here meant an
// admin could make a conversation vanish for all its members while GET of that same
// room still, correctly, 404'd for them. That is not the D255 asymmetry; it is the
// asymmetry leaking into a third verb because one helper served all three.
func (s *Service) conversationForDestructiveVerb(ctx context.Context, tx *sql.Tx, actor, id string, adminMayAct bool) (kind string, trashed bool, err error) {
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
	if !adminMayAct || !isAdminCtx(ctx) {
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
// ⚠ THE QUEUE ALWAYS HOLDS THE EARLIEST PROMISE, which is why the conflict clause
// takes a MIN rather than overwriting (v10 PR 3 review). A MESSAGE delete queues its
// keys at `purge_after = now`, due on the very next drain pass; a plain overwrite
// meant that trashing the room afterwards pushed those same keys out to
// `now + HOME_CHAT_TRASH_DAYS`, so bytes a member had watched leave the thread on
// Monday were still in R2 the following week for a reason nothing on screen
// explained. MIN keeps the hard purge working too — *Smazat natrvalo* passes `now`,
// which is smaller than any pending deadline and therefore still brings it forward.
func (s *Service) queuePurge(ctx context.Context, tx *sql.Tx, conversationID, queuedAt, purgeAfter string) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO chat_deleted_keys (key, queued_at, purge_after)
		SELECT storage_key, ?, ? FROM chat_attachments
		 WHERE conversation_id = ? AND state = 'live'
		    ON CONFLICT (key) DO UPDATE
		       SET purge_after = MIN(chat_deleted_keys.purge_after, excluded.purge_after)`,
		queuedAt, purgeAfter, conversationID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO chat_deleted_keys (key, queued_at, purge_after)
		SELECT thumbnail_key, ?, ? FROM chat_attachments
		 WHERE conversation_id = ? AND state = 'live' AND thumbnail_key IS NOT NULL
		    ON CONFLICT (key) DO UPDATE
		       SET purge_after = MIN(chat_deleted_keys.purge_after, excluded.purge_after)`,
		queuedAt, purgeAfter, conversationID)
	return err
}

// ---- membership ----

func (s *Service) ListMembers(ctx context.Context, id string) (ConversationMemberList, error) {
	labels, err := s.labels(ctx)
	if err != nil {
		return ConversationMemberList{}, err
	}
	return s.listMembersWith(ctx, id, labels)
}

// listMembersWith is ListMembers with the directory projection already built.
//
// ⚠ IT TAKES THE MAP RATHER THAN FETCHING ONE (v10 review). AddMember has already
// built the projection twice — once for the directory gate and once for the audit
// summary — and then returned ListMembers, which built a third and re-ran
// memberScope. The directory cache exists because this module was measured asking
// for the projection on every path; the fix belongs one level up from the SQL.
func (s *Service) listMembersWith(ctx context.Context, id string, labels map[string]string) (ConversationMemberList, error) {
	actor := reqctx.ActorID(ctx)
	sc, err := s.store.memberScope(ctx, s.db, actor, id)
	if err != nil {
		return ConversationMemberList{}, mapScopeErr(err)
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
//
// ⚠ ADDING SOMEBODY ALREADY IN THE ROOM CHANGES NOTHING AND SAYS NOTHING (v10
// review). InsertMember's ON CONFLICT makes the write a no-op — correctly, since
// re-running it would move an existing member's floor forward and cut them off from
// history they can already read — but the audit event and the /ws frame used to go
// out anyway, so the Log recorded an addition that never happened. Idempotent rather
// than 422, on RestoreConversation's precedent one file up: a double tap has plainly
// got what it asked for.
func (s *Service) AddMember(ctx context.Context, id string, in ConversationMemberAdd) (ConversationMemberList, error) {
	actor := reqctx.ActorID(ctx)
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
	var (
		added    bool
		audience []string
	)
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		sc, err := s.store.memberScope(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if sc.Kind == kindDefault {
			// ⚠ NOBODY IS ADDED TO VŠICHNI EITHER, AND THIS IS THE MIRROR OF
			// RemoveMember's GUARD (D219/D258). Membership of the household room
			// ACCRUES — EnsureDefaultMembership enrols each member at first sight
			// with the conversation's own beginning as their floor, so they read all
			// of it. This verb writes floorNow() instead, and it wrote it here with
			// nothing to stop it: anybody could put a member into Všichni with a
			// floor of "now", and because removal from Všichni is refused one
			// function down, the row could never be deleted and the auto-join could
			// never re-run. A member cut off from the household room's history,
			// permanently, with FloorLine suppressed for kind='default' so nothing on
			// screen would even say why.
			return httpx.ErrUnprocessable(
				"Do konverzace „Všichni“ nelze nikoho přidat — je v ní celá domácnost.")
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
		added, err = s.store.InsertMember(ctx, tx, sc.ConversationID, in.UserID, actor, f)
		if err != nil {
			return err
		}
		if !added {
			// Already in the room. Nothing changed, so nothing is recorded.
			return nil
		}
		// ⚠ THE REST OF THE ROOM IS TOLD TOO (v10 review) — resolved here, inside the
		// writing transaction, like every other audience in this module (D233). The
		// added member gets their own frame below; everybody else needs one because
		// the members panel and the room's member_count are theirs as well, and
		// without it they hold a list that is one person out of date until something
		// happens to refocus the window. It is the same defect publishConversation
		// was added for one verb up — a membership change is a structural verb.
		audience, err = s.store.MemberIDs(ctx, tx, sc.ConversationID)
		if err != nil {
			return err
		}
		return s.record(ctx, tx, "member.added", sc.ConversationID,
			"Do konverzace „"+name+"“ přidán člen "+label(labels, in.UserID), nil)
	})
	if err != nil {
		return ConversationMemberList{}, mapScopeErr(err)
	}
	if added {
		// ⚠ THE ADDED MEMBER IS TOLD, exactly as the removed one is (v10 review).
		// Their client is holding a conversation list that does not contain this room
		// and has no reason to refetch — MembershipEvent's `Removed: false` case was
		// declared and then never published, so the frame the receiving half already
		// handles simply never arrived. Addressed TO THEM: nobody else's list changes
		// shape, and the audience for a membership change is the person it happened to.
		s.notifyTo(ctx, []string{in.UserID}, "chat_membership.changed", MembershipEvent{
			ConversationID: id, UserID: in.UserID, Removed: false,
		})
		// Everybody who was already here gets the room's own frame instead: their
		// membership did not change, the room's roster did. The added member is
		// excluded because the frame above already refetches the same keys for them.
		s.publishConversation(ctx, withoutMember(audience, in.UserID), id, false)
	}
	// The projection this request already built, rather than a third one.
	return s.listMembersWith(ctx, id, labels)
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
	actor := reqctx.ActorID(ctx)
	var (
		removed  bool
		audience []string
		labels   map[string]string
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
		// ⚠ THE LAST MEMBER MAY NOT LEAVE, AND THE ROOM IS WHY (v10 review). A group
		// emptied of members is not deleted — it is a live row that has left every
		// read there is: the koš lists only TRASHED conversations, and every listing
		// is a membership join, so neither its former member nor an admin can ever
		// see it again. Its bytes go on counting against chat.total with nothing that
		// could free them. Deleting the conversation is the verb that was wanted, and
		// the koš is what makes that survivable.
		//
		// ⚠ THE COUNT IS ONLY CONSULTED WHEN THE TARGET IS THE CALLER. memberScope has
		// already proved the caller is in the room, so a single-member room's one
		// member IS the caller: refusing on the count alone would answer 422 where a
		// non-member target must get the same 404 an unknown id gets, and that
		// difference is a membership oracle.
		if userID == actor {
			n, err := s.store.CountMembers(ctx, tx, sc.ConversationID)
			if err != nil {
				return err
			}
			if n <= 1 {
				return httpx.ErrUnprocessable(
					"Poslední člen nemůže konverzaci opustit — smažte ji místo toho.")
			}
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
		// ⚠ WHO IS LEFT, read after the DELETE and inside the transaction (v10
		// review). The removed member is told separately below; the ones still here
		// need the room's own frame, because the panel and the member_count are
		// theirs too — and a stale panel goes on offering a remove button for
		// somebody who has already gone, whose click answers 404.
		audience, err = s.store.MemberIDs(ctx, tx, sc.ConversationID)
		if err != nil {
			return err
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
	// The DELETE has already run, so `audience` cannot contain the removed member —
	// no exclusion is needed here, unlike the add.
	s.publishConversation(ctx, audience, id, false)
	return nil
}

// withoutMember returns ids minus one. It exists so AddMember can tell the room
// without telling the added member twice.
func withoutMember(ids []string, exclude string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != exclude {
			out = append(out, id)
		}
	}
	return out
}

// UpdateSelf sets the caller's own per-conversation mute (D248).
func (s *Service) UpdateSelf(ctx context.Context, id string, in ConversationMemberSelfUpdate) (Conversation, error) {
	actor := reqctx.ActorID(ctx)
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
