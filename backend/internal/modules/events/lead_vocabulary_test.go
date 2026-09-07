package events

import (
	"encoding/json"
	"testing"
)

// The reminder-lead vocabulary is written in two shapes and must stay one
// vocabulary (v11 review).
//
// ⚠ `validLeads` is what the SERVICE enforces and `leadValues` is what the MCP
// tool schemas OFFER, and neither can see the other. A seventh lead added to the
// map alone is accepted by every write path and never proposed by a model,
// because the schema it reads does not name it — a documentation defect with no
// other failing test, which is precisely what mcp.Tool.Description's own comment
// warns about. This is that failing test.
func TestLeadVocabularyMatchesTheValidator(t *testing.T) {
	if len(leadValues) != len(validLeads) {
		t.Fatalf("leadValues has %d entries and validLeads has %d", len(leadValues), len(validLeads))
	}
	for _, v := range leadValues {
		if !validLeads[v] {
			t.Fatalf("the tool schemas offer %q, which the service refuses", v)
		}
	}
	for v := range validLeads {
		var found bool
		for _, lv := range leadValues {
			if lv == v {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("the service accepts %q, which no tool schema offers", v)
		}
	}
}

// leadEnum is spliced into two raw-string JSON schemas, so it has to BE JSON.
func TestLeadEnumIsAJSONArrayOfTheVocabulary(t *testing.T) {
	var got []string
	if err := json.Unmarshal([]byte(leadEnum), &got); err != nil {
		t.Fatalf("leadEnum is not valid JSON (%q): %v", leadEnum, err)
	}
	if len(got) != len(leadValues) {
		t.Fatalf("leadEnum has %d entries, leadValues has %d", len(got), len(leadValues))
	}
	for i, v := range leadValues {
		if got[i] != v {
			t.Fatalf("leadEnum[%d] is %q, want %q — the ORDER is part of it", i, got[i], v)
		}
	}

	// ⚠ AND THE SCHEMAS THEMSELVES PARSE. The splice is string concatenation
	// inside a raw literal, which is exactly the kind of edit that produces a
	// schema no client can read while the package still compiles.
	p := &mcpProvider{}
	for _, tool := range p.Tools() {
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("%s has an unparseable input schema: %v\n%s", tool.Name, err, tool.InputSchema)
		}
	}
}
