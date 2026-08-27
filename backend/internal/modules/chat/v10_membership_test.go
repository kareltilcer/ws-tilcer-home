package chat_test

// The leak table (PRD §V10-4a), rows 1–4 and 10–11, 18–19.
//
// ⚠ v9's equivalent table went from eighteen rows to twenty-three UNDER REVIEW and
// the build still found two more that no review had listed. Treat twenty-three as a
// floor, and treat every test here as the minimum rather than the coverage.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/chat"
)

// ---- Row 1: a non-member gets the SAME answer an unknown id gets ----

// TestNonMemberAnd UnknownIdAreByteIdentical is row 1, and the assertion is
// deliberately on the BYTES rather than on the status code.
//
// ⚠ A 403 is a yes/no oracle over conversation ids: it says "this exists and you
// may not have it", which is exactly the fact membership is supposed to hide. A 404
// with a DIFFERENT body ("konverzace není vaše" vs "konverzace neexistuje") is the
// same oracle spelled more quietly. So the refusals must be indistinguishable, and
// the only way to know they are is to compare them.
func TestNonMemberAndUnknownIDAreByteIdentical(t *testing.T) {
	hh := newHousehold(t, kaja, andy, boss, quiet)
	c := hh.group(kaja, "Rodiče")

	// Every conversation-scoped route, in both shapes.
	for _, route := range []struct {
		method string
		real   string
		fake   string
		body   string
	}{
		{"GET", "/api/chat/conversations/%s", "/api/chat/conversations/%s", ""},
		{"GET", "/api/chat/conversations/%s/messages", "/api/chat/conversations/%s/messages", ""},
		{"GET", "/api/chat/conversations/%s/members", "/api/chat/conversations/%s/members", ""},
		{"PATCH", "/api/chat/conversations/%s", "/api/chat/conversations/%s", `{"name":"Nové"}`},
		{"POST", "/api/chat/conversations/%s/messages", "/api/chat/conversations/%s/messages", `{"body":"ahoj"}`},
		{"POST", "/api/chat/conversations/%s/read", "/api/chat/conversations/%s/read", `{"until_message_id":"x"}`},
	} {
		mine := hh.as(andy, route.method, fmt.Sprintf(route.real, c.ID), route.body)
		none := hh.as(andy, route.method, fmt.Sprintf(route.fake, "01900000-0000-7000-8000-000000000000"), route.body)

		if mine.Code != http.StatusNotFound {
			t.Errorf("%s %s by a non-member returned %d, want 404 — a 403 confirms the "+
				"conversation exists, which is the fact membership hides (D180 precedent)",
				route.method, route.real, mine.Code)
		}
		if mine.Body.String() != none.Body.String() {
			t.Errorf("%s %s: a non-member's refusal differs from an unknown id's.\n  member-less: %s\n  unknown id:  %s\n"+
				"Two different bodies are a membership oracle spelled quietly.",
				route.method, route.real, mine.Body.String(), none.Body.String())
		}
	}
}

// TestNonMemberSeesNoTraceInTheList is rows 10–11.
//
// The count matters as much as the absence: a conversation the caller is not in
// must never enter the query at all, so there is no total, no cursor and no page
// size for it to show through.
func TestNonMemberSeesNoTraceInTheList(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	secret := hh.group(kaja, "Překvapení")
	hh.send(kaja, secret.ID, "dárek je objednaný")

	page := decode[chat.ConversationPage](t, hh.as(andy, "GET", "/api/chat/conversations", ""))
	for _, c := range page.Items {
		if c.ID == secret.ID {
			t.Fatalf("a conversation Andy is not in appeared in their list")
		}
		if c.Name == "Překvapení" {
			t.Fatalf("a conversation Andy is not in appeared by name")
		}
	}
	// The household room is the only thing they should see, and only because the
	// auto-join put them in it.
	if len(page.Items) != 1 || page.Items[0].Kind != "default" {
		t.Fatalf("expected exactly the Všichni room, got %d items", len(page.Items))
	}
}

// ---- Row 2: the floor is IN the SQL, and next_cursor/has_more agree with it ----

// TestFloorBoundsThePageMetadataAndNotOnlyTheRows is the row-2 assertion that a
// hand test does not make.
//
// ⚠ A FLOOR APPLIED AFTER THE ROWS ARE FETCHED STILL LEAKS. The bodies would be
// gone, but `has_more` would be true over messages that do not exist for this
// caller and `next_cursor` would point into somebody else's history — so the caller
// can measure what was removed without ever seeing it. That is why this asserts the
// two fields and not only the visible messages.
func TestFloorBoundsThePageMetadataAndNotOnlyTheRows(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Plán")

	// Twelve messages before Andy exists in the room.
	for i := range 12 {
		hh.send(kaja, c.ID, fmt.Sprintf("před připojením %d", i))
	}
	if _, err := hh.svc.AddMember(hh.ctx(kaja), c.ID, chat.ConversationMemberAdd{UserID: andy.id}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	// Two after.
	hh.send(kaja, c.ID, "po připojení 1")
	hh.send(kaja, c.ID, "po připojení 2")

	page := decode[chat.MessagePage](t, hh.as(andy, "GET",
		"/api/chat/conversations/"+c.ID+"/messages?limit=50", ""))

	if len(page.Items) != 2 {
		t.Fatalf("Andy sees %d messages, want the 2 sent after they were added — "+
			"the other 12 are above their floor (D218)", len(page.Items))
	}
	for _, m := range page.Items {
		if m.Body == "" || m.Body[:min(len(m.Body), 5)] == "před " {
			t.Errorf("a message from before Andy's floor reached them: %q", m.Body)
		}
	}
	if page.HasMore {
		t.Errorf("has_more is true for a member whose floor leaves nothing more to read — "+
			"it was computed over rows they may not have (%d items returned)", len(page.Items))
	}
	if page.NextCursor != nil {
		t.Errorf("next_cursor is %q for a page with nothing after it — it points into "+
			"history above this member's floor", *page.NextCursor)
	}

	// And the author still sees everything: the floor is per member, not per room.
	full := decode[chat.MessagePage](t, hh.as(kaja, "GET",
		"/api/chat/conversations/"+c.ID+"/messages?limit=50", ""))
	if len(full.Items) != 14 {
		t.Fatalf("Kája sees %d messages, want all 14 — the floor bounded the wrong person", len(full.Items))
	}
}

// TestFloorPagingIsConsistentAcrossPages checks the cursor itself stays inside the
// floor, because a bound applied only to the first page is not a bound.
func TestFloorPagingIsConsistentAcrossPages(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Dlouhá")
	for i := range 5 {
		hh.send(kaja, c.ID, fmt.Sprintf("staré %d", i))
	}
	if _, err := hh.svc.AddMember(hh.ctx(kaja), c.ID, chat.ConversationMemberAdd{UserID: andy.id}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	for i := range 5 {
		hh.send(kaja, c.ID, fmt.Sprintf("nové %d", i))
	}

	seen := 0
	cursor := ""
	for range 10 { // bounded so a paging bug fails rather than hangs
		url := "/api/chat/conversations/" + c.ID + "/messages?limit=2"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		page := decode[chat.MessagePage](t, hh.as(andy, "GET", url, ""))
		for _, m := range page.Items {
			if len(m.Body) >= 5 && m.Body[:5] == "staré" {
				t.Fatalf("paging walked past the floor and returned %q", m.Body)
			}
			seen++
		}
		if !page.HasMore || page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if seen != 5 {
		t.Fatalf("paged through %d messages, want the 5 above Andy's floor", seen)
	}
}

// ---- Row 3: search ----

// TestSearchDoesNotReturnMessagesBelowTheFloorOrOutsideMembership is row 3.
//
// ⚠ A SNIPPET IS A MESSAGE BODY UNDER ANOTHER NAME, which is why this is not
// satisfied by "the id was not returned". The assertion is on the snippet text.
func TestSearchDoesNotReturnMessagesBelowTheFloorOrOutsideMembership(t *testing.T) {
	hh := newHousehold(t, kaja, andy, boss)
	c := hh.group(kaja, "Tajné")
	hh.send(kaja, c.ID, "heslo k trezoru je jahoda")

	// Andy is added AFTER the message, so it is below their floor.
	if _, err := hh.svc.AddMember(hh.ctx(kaja), c.ID, chat.ConversationMemberAdd{UserID: andy.id}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	hh.send(kaja, c.ID, "jahoda je taky ovoce")

	for _, m := range []member{andy, boss} {
		page := decode[chat.SearchPage](t, hh.as(m, "GET", "/api/chat/search?q=trezoru", ""))
		for _, hit := range page.Items {
			t.Errorf("%s found a message they may not read: snippet %q in %q",
				m.id, hit.Snippet, hit.ConversationName)
		}
	}

	// Andy DOES find the one above their floor — otherwise this test would pass on
	// a search that returns nothing at all.
	page := decode[chat.SearchPage](t, hh.as(andy, "GET", "/api/chat/search?q=jahoda", ""))
	if len(page.Items) != 1 {
		t.Fatalf("Andy found %d hits above their floor, want 1 — a search that returns "+
			"nothing would pass the assertions above for the wrong reason", len(page.Items))
	}
}

// TestSearchRefusesACursorRatherThanIgnoringIt records the ordering decision.
//
// The ordering is `rank` and a keyset cursor is an id; ignoring the parameter would
// return page one forever and read as the end of the results. The v9 private-items
// precedent, which the v10 spec invokes again for the clean-up page's sort=size.
func TestSearchRefusesACursorRatherThanIgnoringIt(t *testing.T) {
	hh := newHousehold(t, kaja)
	rr := hh.as(kaja, "GET", "/api/chat/search?q=cokoliv&cursor=0199", "")
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("search with a cursor returned %d, want 422 — silently ignoring it "+
			"looks like the end of the results", rr.Code)
	}
}

// ---- Row 4: the reply quote ----

// TestQuoteAboveTheFloorIsEmptyAndNotMerelyRedacted is row 4, and it asserts on the
// SERIALISED JSON rather than on the struct.
//
// ⚠ The quote is the leak the floor most easily misses, because it LOOKS like a
// field on a message the caller is allowed to read. It is not: it is a second read
// of a second message. And "available: false, author: Kája, date: 3. 8." is still a
// disclosure — who was talking to whom, and when — so every other field must be
// absent from the wire, not merely blank in the UI.
func TestQuoteAboveTheFloorIsEmptyAndNotMerelyRedacted(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Vlákno")
	parent := hh.send(kaja, c.ID, "původní zpráva s podrobnostmi")

	if _, err := hh.svc.AddMember(hh.ctx(kaja), c.ID, chat.ConversationMemberAdd{UserID: andy.id}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if _, err := hh.svc.SendMessage(hh.ctx(kaja), c.ID, chat.MessageCreate{
		Body: "odpověď", ReplyToID: &parent.ID,
	}); err != nil {
		t.Fatalf("reply: %v", err)
	}

	body := hh.as(andy, "GET", "/api/chat/conversations/"+c.ID+"/messages", "").Body.Bytes()

	var page struct {
		Items []struct {
			Body    string          `json:"body"`
			ReplyTo json.RawMessage `json:"reply_to"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("Andy sees %d messages, want only the reply", len(page.Items))
	}
	quote := string(page.Items[0].ReplyTo)
	if quote != `{"available":false}` {
		t.Fatalf("the quote of a message above Andy's floor serialised as %s\n"+
			"want exactly {\"available\":false} — an author or a date here says who was "+
			"talking to whom and when (D226)", quote)
	}
	// Nothing of the parent's text may appear anywhere in the response.
	if idx := indexOf(string(body), "podrobnostmi"); idx >= 0 {
		t.Fatalf("the parent's body leaked into the thread response at byte %d", idx)
	}
}

// ---- Row 18: the koš is invisible to EVERY read ----

// TestTrashedConversationIsInvisibleToEveryReadAndNotMerelyUnlisted is row 18.
//
// "Absent from the list" is the bug this test exists to catch: the thread, the
// members panel, search and the conversation itself all have to refuse too, which
// is why the koš predicate lives in memberScope rather than in the list query.
func TestTrashedConversationIsInvisibleToEveryReadAndNotMerelyUnlisted(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Dočasná", andy)
	hh.send(kaja, c.ID, "zpráva v koši")

	if err := hh.svc.DeleteConversation(hh.ctx(kaja), c.ID, false); err != nil {
		t.Fatalf("trash: %v", err)
	}

	for _, path := range []string{
		"/api/chat/conversations/" + c.ID,
		"/api/chat/conversations/" + c.ID + "/messages",
		"/api/chat/conversations/" + c.ID + "/members",
	} {
		if rr := hh.as(andy, "GET", path, ""); rr.Code != http.StatusNotFound {
			t.Errorf("GET %s on a trashed conversation returned %d, want 404 — the koš is "+
				"invisible to every read, not merely absent from the list (D253)", path, rr.Code)
		}
	}
	hits := decode[chat.SearchPage](t, hh.as(andy, "GET", "/api/chat/search?q=koši", ""))
	if len(hits.Items) != 0 {
		t.Errorf("search returned %d hits from a trashed conversation", len(hits.Items))
	}

	// ?state=trash is the one place it appears, for its own members, with the day
	// the drain will take its bytes.
	page := decode[chat.ConversationPage](t, hh.as(andy, "GET", "/api/chat/conversations?state=trash", ""))
	if len(page.Items) != 1 || page.Items[0].ID != c.ID {
		t.Fatalf("?state=trash returned %d items, want the one trashed conversation", len(page.Items))
	}
	if page.Items[0].DeletedAt == nil || page.Items[0].PurgeAfter == nil {
		t.Errorf("a trashed conversation must carry deleted_at and purge_after so the koš "+
			"can say how many days remain: %+v", page.Items[0])
	}
}

// ---- Row 19: the directory is display names only ----

// TestDirectoryCarriesNoEmailAndNoRoles is row 19.
//
// ⚠ `/api/chat/directory` is THE FIRST SURFACE IN HOME THAT SHOWS THE MEMBER
// DIRECTORY TO A NON-ADMIN. push.Member carries email and roles; the narrowing
// happens in chat's own types, so this asserts against the raw JSON — a struct
// assertion would pass even if the projection were done by an omitted field
// somewhere upstream.
func TestDirectoryCarriesNoEmailAndNoRoles(t *testing.T) {
	hh := newHousehold(t, kaja, andy, boss, quiet)
	raw := hh.as(quiet, "GET", "/api/chat/directory", "").Body.String()

	// The email addresses and the role words, as values. ⚠ Not "admin" and
	// "reader" as bare substrings: the fixture ids are u-admin and u-reader, so
	// those would match the user_id the endpoint is SUPPOSED to serialise. A test
	// that fails on its own fixture teaches nothing.
	for _, forbidden := range []string{"email", "@example.test", "roles"} {
		if indexOf(raw, forbidden) >= 0 {
			t.Errorf("the directory response contains %q:\n%s\n"+
				"user_id and display_name, and nothing else (D230)", forbidden, raw)
		}
	}

	// The stronger form: the KEY SET, so a future field cannot be added without
	// this failing.
	var envelope struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("decode directory: %v", err)
	}
	if len(envelope.Items) != 4 {
		t.Fatalf("directory returned %d entries, want 4 — an empty one would pass the "+
			"assertions above for the wrong reason", len(envelope.Items))
	}
	for _, entry := range envelope.Items {
		for key := range entry {
			if key != "user_id" && key != "display_name" {
				t.Errorf("the directory serialises %q. This is the first surface in Home "+
					"that shows the member directory to a NON-ADMIN (D230) — user_id and "+
					"display_name, and nothing else", key)
			}
		}
	}
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
