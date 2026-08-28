package audit_test

import (
	"strings"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
)

// RedactMemberScoped is the second redaction rule (v10), and the reason it exists
// is that the FIRST one did not cover chat.
//
// ⚠ A CHAT EVENT IS NOT PRIVATE IN THE v9 SENSE and must not become so: its Log row
// stays unredacted for admins by design (leak table row 12 — the Log is admin-only).
// What it is, is MEMBER-SCOPED, and the one place that matters is the push renderer,
// whose audience a rule chooses by ROLE. admin's listener already withheld the
// conversation id from the URL on exactly that reasoning while the default body
// template — the audit summary — carried the conversation's NAME to every device.
func TestRedactMemberScopedStripsTheConversationName(t *testing.T) {
	name, newName := "Dovolená s Petrou", "Dovolená 2027"
	e := audit.Entry{
		Module:     audit.ModuleChat,
		Action:     "chat.conversation.renamed",
		EntityType: "chat_conversation",
		EntityID:   "01a04084-3000-7000-8000-000000000009",
		Summary:    "Konverzace „" + name + "“ přejmenována na „" + newName + "“",
	}
	changes := []audit.Change{{Field: "name", Old: &name, New: &newName}}

	got, gotChanges := audit.RedactMemberScoped(e, changes)

	if strings.Contains(got.Summary, name) || strings.Contains(got.Summary, newName) {
		t.Errorf("summary still names the conversation: %q", got.Summary)
	}
	if got.Summary != audit.RedactedConversation {
		t.Errorf("summary = %q, want the fixed phrase %q", got.Summary, audit.RedactedConversation)
	}
	// ⚠ THE DIFFS GO FOR THE D207 REASON, UNCHANGED. `{{change.name.new}}` is
	// whitelisted by SHAPE rather than by field name, so a clean summary on its own
	// is not enough — a rule bodied with that token would deliver the new name.
	if gotChanges != nil {
		t.Errorf("changes survived redaction: %+v", gotChanges)
	}
	// Blanking the id is what makes inAppURL fall back to the module route.
	if got.EntityID != "" {
		t.Errorf("entity id survived redaction: %q", got.EntityID)
	}
	if !got.Redacted {
		t.Error("the copy is not marked Redacted")
	}
	// What remains is deliberate: that somebody changed something in chat at 21:40
	// is not the secret; which conversation, and to what, is.
	if got.Module != audit.ModuleChat || got.Action != "chat.conversation.renamed" {
		t.Errorf("redaction dropped the module or the action: %+v", got)
	}

	// ⚠ AND IT MUST NOT MUTATE THE CALLER'S ENTRY. The Log renders from the raw one.
	if e.Summary == audit.RedactedConversation || e.EntityID == "" || changes == nil {
		t.Error("redaction mutated the original entry; the Log reads that copy")
	}
}

// The other ten modules are untouched by the new rule — a broadcast about a
// document must still say which document.
func TestRedactMemberScopedLeavesOtherModulesAlone(t *testing.T) {
	for _, module := range []string{
		audit.ModuleNotes, audit.ModuleDocuments, audit.ModuleGarden, audit.ModuleElectricity,
	} {
		e := audit.Entry{Module: module, EntityID: "abc", Summary: "Nákupní seznam upraven"}
		got, gotChanges := audit.RedactMemberScoped(e, []audit.Change{{Field: "title"}})
		if got.Summary != e.Summary || got.EntityID != "abc" || len(gotChanges) != 1 {
			t.Errorf("module %q was redacted by the chat rule: %+v", module, got)
		}
	}
}
