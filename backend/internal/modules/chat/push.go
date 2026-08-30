package chat

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
)

// Chat's push fan-out (D248).
//
// ⚠ OFF THE REQUEST PATH, ALWAYS. push.Sender is explicit that no mutation may be
// slowed or failed by a push service, so this runs in a goroutine after the write
// has already committed and its /ws frame has already gone out. A push that cannot
// be delivered is a recorded per-endpoint failure, never a failed send.
//
// THREE FILTERS, IN THREE DIFFERENT PLACES, and that is not an accident:
//
//	chat_members.muted        per conversation — applied in the SQL that builds the
//	                          recipient list (Store.PushRecipients)
//	cat_chat                  app-wide, the member's own bucket — applied by
//	                          push.EligibleSubscriptions from the category below
//	author_id <> user_id      nobody is notified about their own message
//
// A member who silenced one busy room still hears about the others, and a member
// who turned chat off in Nastavení hears about none of them. Collapsing the two
// mutes into one would take away whichever half somebody actually wanted.

// pushBodyRunes is the message preview length (D248). Deliberately short: the
// notification is a reason to open the app, not a way to read the thread from the
// lock screen of a phone somebody else may be holding.
const pushBodyRunes = 140

// notifyPush sends one message's notification to everyone but its author.
func (s *Service) notifyPush(ctx context.Context, conversationName string, recipients []string, m Message) {
	if s.pusher == nil || len(recipients) == 0 {
		return
	}
	s.pusher.Send(ctx, recipients, push.Envelope{
		Module: "chat",
		Type:   "chat",
		Title:  pushTitle(conversationName, m.AuthorLabel),
		Body:   pushBody(m),
		URL:    "/chat/" + m.ConversationID,
		// ⚠ Tag COLLAPSES PER CONVERSATION, so twenty messages in one room are one
		// banner that keeps updating rather than twenty banners. Two rooms still
		// produce two, because the id is in the tag.
		Tag:      "chat:" + m.ConversationID,
		Data:     map[string]any{"conversation_id": m.ConversationID, "message_id": m.ID},
		Category: push.CategoryChat,
		Kind:     push.KindChat,
	})
}

// pushTitle names the room and the sender, in that order.
//
// A member is in several conversations and the room is what tells them whether
// this is the household or a group of three — so it leads, and the sender follows.
func pushTitle(conversationName, author string) string {
	if conversationName == "" {
		return author
	}
	return conversationName + " · " + author
}

// pushBody is the preview.
//
// ⚠ An attachment-only message reads "<jméno> poslal soubor" rather than arriving
// as an empty notification. PR 2 sends no attachments, so the branch is unreachable
// today — it is here because the alternative, discovered in PR 3, is a blank banner
// that nobody can interpret.
func pushBody(m Message) string {
	body := strings.TrimSpace(m.Body)
	if body == "" {
		if len(m.Attachments) > 0 {
			return m.AuthorLabel + " poslal soubor"
		}
		return m.AuthorLabel + " poslal zprávu"
	}
	return truncateRunes(body, pushBodyRunes)
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimRight(string(r[:max]), " ") + "…"
}

// ---- the household directory ----

// DirectorySource is the member directory, projected from `sessions`.
//
// ⚠ IT IS A LOGIN HISTORY, not a user table — Home has none, auth owns identity.
// So somebody who has never logged in simply is not here and cannot be added to a
// conversation, and the picker's empty state says so rather than looking broken
// (D230).
//
// ⚠ IT IS NOT FROZEN AT LOGIN, and this comment used to say it was. The
// fifteen-minute re-mint writes the email and display name alongside the roles and
// the projection picks each member's FRESHEST session, so a name changed in auth
// reaches chat within one refresh window (D270). The one thing that still waits
// for the next login is a name ERASED in auth: a mint carrying no name is read as
// "this token did not say", never as a clear.
//
// Satisfied by *push.Store. It is an interface here so chat depends on the
// projection rather than on the push store's other twenty methods.
type DirectorySource interface {
	Members(ctx context.Context) ([]push.Member, error)
}

// directoryTTL is how long one directory projection is reused.
//
// ⚠ IT IS A CACHE BECAUSE labels() IS ON EVERY PATH IN THE MODULE (v10 review).
// push.Store.Members is a self-join over `sessions` with a correlated COUNT over
// push_subscriptions, and chat asked for it on every send, edit, delete, thread
// read, search and member list — against a pool capped at ONE connection, which
// makes it the dominant read on the module's hottest path. The directory only
// changes when somebody logs in or renames themselves, so a few seconds of
// staleness costs a member added in that window one retry and nothing else.
const directoryTTL = 15 * time.Second

// directoryCache memoises the projection. The lock is held ACROSS the query on
// purpose: concurrent requests then collapse into one read rather than each
// starting their own against the single connection.
type directoryCache struct {
	mu      sync.Mutex
	at      time.Time
	members []push.Member
	loaded  bool
}

func (s *Service) directoryMembers(ctx context.Context) ([]push.Member, error) {
	if s.directory == nil {
		return nil, nil
	}
	s.dir.mu.Lock()
	defer s.dir.mu.Unlock()
	if s.dir.loaded && time.Since(s.dir.at) < directoryTTL {
		return s.dir.members, nil
	}
	members, err := s.directory.Members(ctx)
	if err != nil {
		return nil, err
	}
	// ⚠ A ROW WITH NO USER ID IS DROPPED HERE, so the picker and the membership
	// set keep saying the same thing. AddMember answers an empty id with 422
	// ("Chybí uživatel."), so listing one is exactly the button that answers every
	// click with an error that labels() was written to prevent — and
	// CreateConversation, which checks this same projection for "is this person in
	// the directory", would have accepted it and written a membership row for
	// nobody. One projection, one membership of it: the filter belongs here, above
	// both callers, and not in either of them.
	//
	// ⚠ TrimSpace, NOT `!= ""` — IT MUST BE AddMember's OWN TEST, spelled the same
	// way. That guard is `strings.TrimSpace(in.UserID) == ""`, so an id of spaces
	// passed an `!= ""` filter here and met the 422 there anyway: the exact
	// button-that-can-only-fail this filter exists to remove, with the phantom
	// membership row through CreateConversation still behind it. push.Store drops
	// these too, one layer down; this is chat's guard because DirectorySource is an
	// INTERFACE and what arrives through it is whatever the implementation sends.
	//
	// Into a NEW slice rather than members[:0]: the projection belongs to whoever
	// implemented DirectorySource, and filtering in place would rewrite it.
	kept := make([]push.Member, 0, len(members))
	for _, m := range members {
		if strings.TrimSpace(m.UserID) != "" {
			kept = append(kept, m)
		}
	}
	s.dir.members, s.dir.at, s.dir.loaded = kept, time.Now(), true
	return kept, nil
}

// labels builds the id → display name map every render path uses.
//
// ⚠ THE PROJECTION HAPPENS HERE AND IT DISCARDS EMAIL AND ROLES. push.Member
// carries both; `/api/chat/directory` is the first surface in Home that shows the
// directory to a NON-ADMIN (D230), so the narrowing is done once, at the boundary,
// rather than trusted to each handler's choice of fields.
//
// ⚠ EVERY DIRECTORY ROW IS IN THE MAP, INCLUDING THE NAMELESS ONES (v10 review).
// It is not only a render table: CreateConversation and AddMember use it as the
// "is this person in the directory" set. An earlier version dropped a member whose
// `sessions.display_name` is NULL — which fixed the email leak and, because
// Directory() still listed them under their id, put a button in the add-member
// picker that answered every click with 422. One projection, one membership of it:
// directoryName is what both call.
func (s *Service) labels(ctx context.Context) (map[string]string, error) {
	members, err := s.directoryMembers(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(members))
	for _, m := range members {
		out[m.UserID] = directoryName(m)
	}
	return out, nil
}

// directoryName is the ONE name chat ever shows for a member: their own if they
// chose one, their id otherwise — never the email push.Store.Members substitutes.
func directoryName(m push.Member) string {
	if isRealDisplayName(m) {
		return strings.TrimSpace(m.DisplayName)
	}
	return m.UserID
}

// isRealDisplayName reports whether a directory row carries a name somebody chose,
// as opposed to push.Store.Members' own fallback.
//
// ⚠ THE PROJECTION IS NOT SAFE BY CONSTRUCTION AFTER ALL, AND THIS IS WHERE THAT
// IS FIXED (v10 review). types.go says chat's wire types carry no email because
// there is no field to fill in — true of the SHAPE, and beside the point: when
// `sessions.display_name` is NULL, push.Store.Members substitutes the EMAIL into
// DisplayName (store.go), so the address arrives inside the one field chat does
// copy. D230 makes /api/chat/directory the first surface in Home to show the
// directory to a non-admin, which is precisely the audience that must not get it.
//
// The comparison is exact rather than a guess at what an address looks like: we
// are detecting one known substitution, not validating an email.
//
// ⚠ AND IT TRIMS, because "   " is not a name either (v10 chat report). The
// projection now trims before its own email fallback, so this is chat's guard
// rather than a second one: DirectorySource is an INTERFACE, and what arrives
// through it is whatever the implementation chose to put in the field. A blank
// name reaching a picker draws a bubble with nothing in it, which is not a member
// somebody can recognise — the id at least is one they can look up.
//
// ⚠ BOTH SIDES OF THE COMPARISON ARE TRIMMED, or the trim above defeats the
// detection it sits beside: push.Store copies the email VERBATIM into DisplayName,
// so padding on `sessions.email` left the trimmed name and the raw address unequal,
// the substitution went undetected, and the address reached every non-admin — the
// one leak this function exists to catch.
func isRealDisplayName(m push.Member) bool {
	name := strings.TrimSpace(m.DisplayName)
	return name != "" && name != strings.TrimSpace(m.Email)
}

// Directory is the add-member picker's data: user id and display name, and
// nothing else.
func (s *Service) Directory(ctx context.Context) (Directory, error) {
	out := Directory{Items: []DirectoryEntry{}}
	members, err := s.directoryMembers(ctx)
	if err != nil {
		return Directory{}, err
	}
	for _, m := range members {
		// The id, never the email — see isRealDisplayName. label()'s reasoning
		// applies here too: one raw id somebody can look up beats an address the
		// member never agreed to publish to the household. ⚠ And it is the SAME
		// call labels() makes, so what this lists is exactly what AddMember accepts.
		out.Items = append(out.Items, DirectoryEntry{UserID: m.UserID, DisplayName: directoryName(m)})
	}
	return out, nil
}
