package chat

import (
	"context"
	"strings"

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
// conversation, and a display name changed in the auth service arrives on that
// member's next login and not before. The picker's empty and stale states say so
// rather than looking broken (D230).
//
// Satisfied by *push.Store. It is an interface here so chat depends on the
// projection rather than on the push store's other twenty methods.
type DirectorySource interface {
	Members(ctx context.Context) ([]push.Member, error)
}

// labels builds the id → display name map every render path uses.
//
// ⚠ THE PROJECTION HAPPENS HERE AND IT DISCARDS EMAIL AND ROLES. push.Member
// carries both; `/api/chat/directory` is the first surface in Home that shows the
// directory to a NON-ADMIN (D230), so the narrowing is done once, at the boundary,
// rather than trusted to each handler's choice of fields.
func (s *Service) labels(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	if s.directory == nil {
		return out, nil
	}
	members, err := s.directory.Members(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		name := m.DisplayName
		if name == "" {
			// push.Store.Members already falls back to the email; if even that is
			// empty there is nothing to show but the id, which label() handles.
			continue
		}
		out[m.UserID] = name
	}
	return out, nil
}

// Directory is the add-member picker's data: user id and display name, and
// nothing else.
func (s *Service) Directory(ctx context.Context) (Directory, error) {
	out := Directory{Items: []DirectoryEntry{}}
	if s.directory == nil {
		return out, nil
	}
	members, err := s.directory.Members(ctx)
	if err != nil {
		return Directory{}, err
	}
	for _, m := range members {
		name := m.DisplayName
		if name == "" {
			name = m.UserID
		}
		out.Items = append(out.Items, DirectoryEntry{UserID: m.UserID, DisplayName: name})
	}
	return out, nil
}
