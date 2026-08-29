package cursor_test

import (
	"strings"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/cursor"
)

func TestRoundTrip(t *testing.T) {
	cases := [][]string{
		{"2026-08-29T12:00:00Z", "0199aaaa-bbbb-7ccc-8ddd-eeeeffff0000"},
		{"2026-08-29", "0|hZk", "0199aaaa-bbbb-7ccc-8ddd-eeeeffff0000"},
		{"", ""},
	}
	for _, want := range cases {
		got, ok := cursor.Decode(cursor.Encode(want...), len(want))
		if !ok {
			t.Fatalf("Decode(Encode(%q), %d) not ok", want, len(want))
		}
		if len(got) != len(want) {
			t.Fatalf("Decode(Encode(%q)) = %q, want %q", want, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("part %d = %q, want %q", i, got[i], want[i])
			}
		}
	}
}

// A cursor is OPAQUE, which is the property the specs promise a client. The
// token must not read as its own contents, or a caller will start building one.
func TestTokenIsOpaqueAndURLSafe(t *testing.T) {
	tok := cursor.Encode("2026-08-29T12:00:00Z", "abc")
	if strings.Contains(tok, "2026") || strings.Contains(tok, "abc") {
		t.Errorf("token %q leaks its parts verbatim", tok)
	}
	if strings.ContainsAny(tok, "+/=|") {
		t.Errorf("token %q is not URL-safe unpadded base64", tok)
	}
}

// The arity check is what makes a cursor from ANOTHER collection a miss rather
// than a wrong page: chat's two-part conversation cursor must not decode as
// garden's three-part task cursor.
func TestArityMismatchIsNotOK(t *testing.T) {
	two := cursor.Encode("a", "b")
	if _, ok := cursor.Decode(two, 3); ok {
		t.Error("a 2-part token decoded as 3 parts")
	}
	three := cursor.Encode("a", "b", "c")
	if _, ok := cursor.Decode(three, 2); ok {
		t.Error("a 3-part token decoded as 2 parts")
	}
}

func TestGarbageIsNotOK(t *testing.T) {
	// "nonsense" is valid base64url — it decodes to bytes carrying no separator,
	// so it fails on ARITY, not on the decode. Both paths must return ok=false;
	// chat's TestConversationListPagesOnLimitAndCursor
	// drives exactly this string to a 422.
	for _, bad := range []string{"nonsense", "not base64!!", "", "a|b", "0199"} {
		if _, ok := cursor.Decode(bad, 2); ok {
			t.Errorf("Decode(%q, 2) = ok, want not ok", bad)
		}
	}
}

// A part cannot smuggle in a separator, because no value the callers page on can
// contain one. Pin it: if a part ever could, the arity check silently shifts and
// a cursor would page against the wrong column.
func TestSeparatorCannotBeForgedFromContent(t *testing.T) {
	parts, ok := cursor.Decode(cursor.Encode("a\x1fb", "c"), 2)
	if ok {
		t.Fatalf("a part containing the separator decoded as 2 parts: %q", parts)
	}
}

// Decode is STRUCTURAL: it reports arity, not meaning. Empty parts come back ok,
// and the callers that care (chat, documents) reject them themselves — which is
// what keeps garden's and logging's malformed-input behaviour unchanged.
func TestEmptyPartsAreStructurallyValid(t *testing.T) {
	parts, ok := cursor.Decode(cursor.Encode("", "x"), 2)
	if !ok {
		t.Fatal("Decode rejected an empty part; that judgement belongs to the caller")
	}
	if parts[0] != "" || parts[1] != "x" {
		t.Errorf("parts = %q, want [\"\" \"x\"]", parts)
	}
}
