package chat_test

// The decisions v10 records as TESTS rather than as comments.
//
// Each one here exists because the behaviour it pins looks like a bug to somebody
// who has not read the PRD: a module whose main verb writes no audit event, a
// household room that exempts itself from the access rule the rest of the module
// enforces, an admin who can destroy a conversation they cannot open. A comment
// saying "this is deliberate" is not enforcement; a failing build is.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/chat"
)

// ---- D231: messages are not audited ----

// TestChatMessagesAreNotAudited is D231, and it is the breach test for the whole
// decision.
//
// ⚠ CHAT IS THE FIRST MODULE IN HOME WHOSE PRIMARY MUTATION WRITES NOTHING TO THE
// LOG. That reads as missing coverage from every angle except the one that matters:
// audit_events would otherwise become a second, admin-readable copy of every
// conversation in the house, searchable, un-redactable and outliving both the edit
// and the delete. The cost was priced and declined.
//
// It asserts all three verbs, because a later "let's at least log deletions" is
// exactly the shape the fix would take.
func TestChatMessagesAreNotAudited(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Provoz", andy)

	before := auditCount(t, hh.db)

	msg := hh.send(kaja, c.ID, "první")
	if got := auditCount(t, hh.db); got != before {
		t.Errorf("sending a message wrote %d audit event(s) (D231)", got-before)
	}
	if _, err := hh.svc.EditMessage(hh.ctx(kaja), msg.ID, chat.MessageUpdate{Body: "opraveno"}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if got := auditCount(t, hh.db); got != before {
		t.Errorf("editing a message wrote %d audit event(s) (D231)", got-before)
	}
	if err := hh.svc.DeleteMessage(hh.ctx(kaja), msg.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := auditCount(t, hh.db); got != before {
		t.Errorf("deleting a message wrote %d audit event(s) (D231)", got-before)
	}

	// And the counterpart: STRUCTURAL changes ARE audited. Without this, a module
	// that audited nothing at all would pass the assertions above.
	if _, err := hh.svc.RenameConversation(hh.ctx(kaja), c.ID,
		chat.ConversationUpdate{Name: "Provoz 2"}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if got := auditCount(t, hh.db); got != before+1 {
		t.Fatalf("renaming a conversation wrote %d events, want exactly 1 — the message "+
			"assertions above would pass on a module that audits nothing", got-before)
	}
}

// TestNoChatMessageActionIsDeclared is the same decision at the catalog level.
//
// An admin composing a trigger rule must not find "Když někdo pošle zprávu" in the
// picker, because there is no event for it to fire on — a rule bound to a verb that
// never fires is worse than an absent one.
func TestNoChatMessageActionIsDeclared(t *testing.T) {
	m := &chat.Module{}
	for _, action := range m.AuditActions() {
		if strings.HasPrefix(action, "message.") {
			t.Errorf("chat declares the audit action %q, but no message mutation writes "+
				"an event (D231) — a rule bound to it would never fire", action)
		}
	}
}

// ---- D258: the Všichni exemption is a value, not a branch ----

// TestDefaultConversationHasNoHistoryBranch walks the module's own Go files.
//
// ⚠ THE MOMENT HISTORY DEPENDS ON A `kind == "default"` CHECK, IT BECOMES AN
// EXCEPTION SOMEBODY GETS WRONG IN THE FOURTH QUERY THAT NEEDS IT. The household
// room gives every member the whole history not because a read path treats it
// specially, but because its members' floors are written with a different VALUE —
// the conversation's own beginning (floor.go). Read paths never ask what kind of
// conversation they are looking at.
//
// The three write guards that legitimately do ask — Všichni cannot be deleted,
// purged or left — are allowed, and named.
func TestDefaultConversationHasNoHistoryBranch(t *testing.T) {
	const allowedFiles = "service.go" // the delete/leave guards live here, and only here

	root := moduleDir(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read module dir: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(root, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			be, ok := n.(*ast.BinaryExpr)
			if !ok || (be.Op != token.EQL && be.Op != token.NEQ) {
				return true
			}
			if !mentionsDefaultKind(be) {
				return true
			}
			if name == allowedFiles {
				return true
			}
			t.Errorf("%s:%d compares against the default conversation kind.\n\n"+
				"Všichni's full history is a VALUE — the floor written at join time (D258) —\n"+
				"not a branch in a read path. The only file allowed to ask what kind a\n"+
				"conversation is, is %s, where delete and leave are refused for it.",
				name, fset.Position(be.Pos()).Line, allowedFiles)
			return true
		})
	}
}

// mentionsDefaultKind reports whether a comparison has kindDefault or the literal
// "default" on either side.
func mentionsDefaultKind(be *ast.BinaryExpr) bool {
	for _, side := range []ast.Expr{be.X, be.Y} {
		switch v := side.(type) {
		case *ast.Ident:
			if v.Name == "kindDefault" {
				return true
			}
		case *ast.BasicLit:
			if v.Kind == token.STRING && v.Value == `"default"` {
				return true
			}
		}
	}
	return false
}

func moduleDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	return wd
}

// TestLateMemberReadsVsichniInFull is D258 from the outside.
//
// A member the app meets for the first time long after the household room was
// created reads all of it — which is the opposite of what a group add does, and the
// reason the exemption exists at all.
func TestLateMemberReadsVsichniInFull(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	vsichni := hh.defaultConversation(t)

	// Kája is around from the start and says three things.
	hh.join(kaja)
	for _, body := range []string{"dobré ráno", "koupím chleba", "jsem doma"} {
		hh.send(kaja, vsichni, body)
	}

	// Andy shows up only now — their first ever chat request.
	page := decode[chat.MessagePage](t, hh.as(andy, "GET",
		"/api/chat/conversations/"+vsichni+"/messages", ""))
	if len(page.Items) != 3 {
		t.Fatalf("a member joining Všichni sees %d of 3 earlier messages — the household "+
			"room gives everyone its whole history (D258)", len(page.Items))
	}

	// And a GROUP does the opposite, which is what makes the exemption meaningful.
	g := hh.group(kaja, "Skupina")
	hh.send(kaja, g.ID, "něco starého")
	if _, err := hh.svc.AddMember(hh.ctx(kaja), g.ID, chat.ConversationMemberAdd{UserID: andy.id}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	gp := decode[chat.MessagePage](t, hh.as(andy, "GET",
		"/api/chat/conversations/"+g.ID+"/messages", ""))
	if len(gp.Items) != 0 {
		t.Fatalf("a member added to a group sees %d earlier messages, want 0 (D218)", len(gp.Items))
	}
}

// TestVsichniRefusesDeleteAndRemoval is D219.
func TestVsichniRefusesDeleteAndRemoval(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	vsichni := hh.defaultConversation(t)
	hh.join(kaja)
	hh.join(andy)

	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+vsichni, ""); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("deleting Všichni returned %d, want 422 with a Czech reason (D219)", rr.Code)
	}
	if rr := hh.as(kaja, "DELETE", "/api/chat/conversations/"+vsichni+"/members/"+andy.id, ""); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("removing a member from Všichni returned %d, want 422 (D219)", rr.Code)
	}
	// Renaming it IS allowed — only delete and leave are refused.
	if rr := hh.as(kaja, "PATCH", "/api/chat/conversations/"+vsichni, `{"name":"Domácnost"}`); rr.Code != http.StatusOK {
		t.Errorf("renaming Všichni returned %d, want 200 — it is renameable (D219)", rr.Code)
	}
}

// ---- D223: the soft delete blanks the body ----

// TestSoftDeleteBlanksTheBodyInTheTableAndTheIndex is the one test in this package
// that reaches past the API on purpose.
//
// ⚠ chat_messages_fts IS EXTERNAL-CONTENT: it reads its text from chat_messages. So
// `deleted_at IS NOT NULL` alone hides a message from the thread and leaves it
// perfectly findable by search, snippet and all. Asserting through the API only
// would pass on exactly that bug, because the thread filter is the part that works.
func TestSoftDeleteBlanksTheBodyInTheTableAndTheIndex(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Vlákno", andy)
	msg := hh.send(kaja, c.ID, "citlivá věta o penězích")

	if err := hh.svc.DeleteMessage(hh.ctx(kaja), msg.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// 1. The row itself.
	var body string
	if err := hh.db.QueryRow(`SELECT body FROM chat_messages WHERE id = ?`, msg.ID).Scan(&body); err != nil {
		t.Fatalf("read the row back: %v", err)
	}
	if body != "" {
		t.Errorf("the deleted message's body is still %q in chat_messages", body)
	}

	// 2. The index, queried directly.
	var hits int
	if err := hh.db.QueryRow(
		`SELECT COUNT(*) FROM chat_messages_fts WHERE chat_messages_fts MATCH 'penězích'`).
		Scan(&hits); err != nil {
		t.Fatalf("query chat_messages_fts: %v", err)
	}
	if hits != 0 {
		t.Errorf("chat_messages_fts still returns %d row(s) for a deleted message's text — "+
			"the update trigger did not fire on the blanking UPDATE (12001, D223)", hits)
	}

	// 3. And through search, which is what a person would notice.
	page := decode[chat.SearchPage](t, hh.as(andy, "GET", "/api/chat/search?q=penězích", ""))
	if len(page.Items) != 0 {
		t.Errorf("search returned %d hit(s) for a deleted message", len(page.Items))
	}

	// 4. The tombstone survives, because removing the row would leave replies
	//    pointing at nothing and reflow a thread somebody is reading.
	thread := decode[chat.MessagePage](t, hh.as(andy, "GET",
		"/api/chat/conversations/"+c.ID+"/messages", ""))
	if len(thread.Items) != 1 || !thread.Items[0].Deleted || thread.Items[0].Body != "" {
		t.Fatalf("want one empty tombstone in the thread, got %+v", thread.Items)
	}
}

// ---- D255: the admin asymmetry ----

// TestAdminMayPurgeAConversationTheyCannotOpen asserts BOTH HALVES IN ONE TEST, so
// the asymmetry is visible rather than looking like an inconsistency somebody should
// tidy up.
//
// An admin has exactly two verbs over a room they are not in — restore and purge —
// because a heavy conversation costs the household money and somebody has to be able
// to act on it. Neither verb opens it. It follows v9's D181 exactly: an admin
// hard-deleting a foreign private item is doing a write to a row they may not read.
func TestAdminMayPurgeAConversationTheyCannotOpen(t *testing.T) {
	hh := newHousehold(t, kaja, andy, boss)
	c := hh.group(kaja, "Soukromá skupina", andy)
	hh.send(kaja, c.ID, "obsah, který šéf nikdy neuvidí")

	// Half one: the admin cannot read it. Not the room, not the thread, not the
	// members.
	for _, path := range []string{
		"/api/chat/conversations/" + c.ID,
		"/api/chat/conversations/" + c.ID + "/messages",
		"/api/chat/conversations/" + c.ID + "/members",
	} {
		if rr := hh.as(boss, "GET", path, ""); rr.Code != http.StatusNotFound {
			t.Errorf("an admin GET %s returned %d, want 404 — admin is not a read "+
				"widening in chat (D255)", path, rr.Code)
		}
	}

	// Half two: the same admin may trash it, restore it and purge it.
	if rr := hh.as(boss, "DELETE", "/api/chat/conversations/"+c.ID, ""); rr.Code != http.StatusNoContent {
		t.Fatalf("an admin trashing a conversation returned %d, want 204 (D255)", rr.Code)
	}
	// ⚠ 204, NOT 200 WITH THE ROOM. The restore is a verb the admin has; the
	// conversation is a read they do not. Returning it in the response body would
	// hand them exactly what the GET above correctly refused.
	if rr := hh.as(boss, "POST", "/api/chat/conversations/"+c.ID+"/restore", ""); rr.Code != http.StatusNoContent {
		t.Fatalf("an admin restoring a conversation returned %d, want 204 (D255)", rr.Code)
	}
	if rr := hh.as(boss, "POST", "/api/chat/conversations/"+c.ID+"/restore", ""); rr.Body.Len() != 0 {
		t.Fatalf("an admin's restore returned a body: %s", rr.Body.String())
	}
	if rr := hh.as(boss, "DELETE", "/api/chat/conversations/"+c.ID+"?hard=true", ""); rr.Code != http.StatusNoContent {
		t.Fatalf("an admin purging a conversation returned %d, want 204 (D255)", rr.Code)
	}

	var n int
	if err := hh.db.QueryRow(`SELECT COUNT(*) FROM chat_conversations WHERE id = ?`, c.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("the purged conversation is still in the table")
	}

	// And an ordinary member is refused the same three verbs on a room they are not
	// in — the admin's ability is a ROLE decision, not a hole in the scope.
	other := hh.group(kaja, "Další")
	for _, req := range [][2]string{
		{"DELETE", "/api/chat/conversations/" + other.ID},
		{"POST", "/api/chat/conversations/" + other.ID + "/restore"},
	} {
		if rr := hh.as(andy, req[0], req[1], ""); rr.Code != http.StatusNotFound {
			t.Errorf("%s %s by a non-member non-admin returned %d, want 404", req[0], req[1], rr.Code)
		}
	}
}

// ---- D222: the reader writes ----

// TestReaderCanWriteInChat is the first time in Home that sentence is true.
//
// Every other module gates writes behind RequireWrite. Chat replaces the role gate
// with MEMBERSHIP, so a `reader` posts, edits and deletes their own messages,
// creates conversations and manages membership. The one thing they cannot do is
// clean up storage — PR 3's /chat/uklid — and that asymmetry is recorded rather than
// hidden: a reader can fill storage they can never clean (D241).
func TestReaderCanWriteInChat(t *testing.T) {
	hh := newHousehold(t, kaja, quiet)

	rr := hh.as(quiet, "POST", "/api/chat/conversations", `{"name":"Čtenářova skupina"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("a reader creating a conversation returned %d, want 201 (D222)", rr.Code)
	}
	c := decode[chat.Conversation](t, rr)

	if rr := hh.as(quiet, "POST", "/api/chat/conversations/"+c.ID+"/messages",
		`{"body":"čtenář píše"}`); rr.Code != http.StatusCreated {
		t.Fatalf("a reader sending a message returned %d, want 201 (D222)", rr.Code)
	}
	if rr := hh.as(quiet, "PATCH", "/api/chat/conversations/"+c.ID,
		`{"name":"Přejmenováno"}`); rr.Code != http.StatusOK {
		t.Fatalf("a reader renaming a conversation returned %d, want 200 (D222)", rr.Code)
	}
	if rr := hh.as(quiet, "POST", "/api/chat/conversations/"+c.ID+"/members",
		`{"user_id":"`+kaja.id+`"}`); rr.Code != http.StatusOK {
		t.Fatalf("a reader adding a member returned %d, want 200 (D222)", rr.Code)
	}
}

// ---- D250: the read marker never moves backwards ----

// TestReadMarkerIsIdempotentAndNeverMovesBackwards is D250.
//
// A replayed older marker — a queued request arriving late, a tab restored from
// history — must not un-read a conversation. The mechanism is MAX() in the UPDATE
// itself, so no caller can skip it.
func TestReadMarkerIsIdempotentAndNeverMovesBackwards(t *testing.T) {
	hh := newHousehold(t, kaja, andy)
	c := hh.group(kaja, "Nepřečtené", andy)

	first := hh.send(kaja, c.ID, "jedna")
	second := hh.send(kaja, c.ID, "dvě")
	third := hh.send(kaja, c.ID, "tři")

	unread := func() int {
		conv := decode[chat.Conversation](t, hh.as(andy, "GET", "/api/chat/conversations/"+c.ID, ""))
		return conv.UnreadCount
	}
	if got := unread(); got != 3 {
		t.Fatalf("unread is %d before reading anything, want 3", got)
	}

	// Read to the newest.
	state := decode[chat.ReadState](t, hh.as(andy, "POST", "/api/chat/conversations/"+c.ID+"/read",
		`{"until_message_id":"`+third.ID+`"}`))
	if state.UnreadCount != 0 {
		t.Fatalf("unread is %d after reading to the newest message, want 0", state.UnreadCount)
	}

	// Replay an OLDER marker twice. Neither may un-read anything.
	for _, older := range []string{first.ID, second.ID} {
		replay := decode[chat.ReadState](t, hh.as(andy, "POST", "/api/chat/conversations/"+c.ID+"/read",
			`{"until_message_id":"`+older+`"}`))
		if replay.UnreadCount != 0 {
			t.Fatalf("replaying an older read marker moved unread back to %d — the marker "+
				"must never move backwards (D250)", replay.UnreadCount)
		}
	}
	if got := unread(); got != 0 {
		t.Fatalf("unread is %d after two replayed markers, want 0", got)
	}

	// A new message still counts, so the marker is not simply stuck.
	hh.send(kaja, c.ID, "čtyři")
	if got := unread(); got != 1 {
		t.Fatalf("unread is %d after a new message, want 1", got)
	}
	// And the author's own message never counts against them.
	hh.send(andy, c.ID, "moje vlastní")
	if got := unread(); got != 1 {
		t.Fatalf("unread is %d after Andy's own message, want 1 — your own message is "+
			"never unread (D250)", got)
	}
}
