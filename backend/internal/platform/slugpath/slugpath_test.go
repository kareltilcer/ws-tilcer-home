package slugpath_test

import (
	"reflect"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/slugpath"
)

// Split is the parser both tree modules' Resolve runs on the path a member typed
// into the address bar, so what it does with a malformed one decides between a
// 404 and a wrong row. Every case below is behaviour the two copies it replaced
// already had.
func TestSplit(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"a plain path", "smlouvy/energie/cez", []string{"smlouvy", "energie", "cez"}},
		{"one segment", "cez", []string{"cez"}},
		{"leading and trailing slashes", "/smlouvy/energie/", []string{"smlouvy", "energie"}},
		{"doubled slashes collapse", "smlouvy//energie", []string{"smlouvy", "energie"}},
		{"surrounding whitespace", "  smlouvy/energie  ", []string{"smlouvy", "energie"}},

		// Nil rather than an empty slice: Resolve tests `len(segs) == 0` and refuses,
		// so both spellings work — but nil is what the callers were written against.
		{"empty", "", nil},
		{"only whitespace", "   ", nil},
		{"only slashes", "///", nil},

		// The router hands over a percent-encoded path, and Czech slugs are ASCII
		// only after slugging — a name that was not slugged still arrives encoded.
		{"percent-decodes each segment", "slo%C5%BEka/p%C3%ADsmo", []string{"složka", "písmo"}},
		{"decodes an encoded space", "m%C3%A1%20slo%C5%BEka", []string{"má složka"}},

		// ⚠ A malformed escape is kept RAW rather than rejected. An error here would
		// make an unresolvable slug distinguishable from an unknown one, which is a
		// difference a caller could read a real slug's existence off.
		{"keeps a non-hex escape raw", "smlouvy/%zz", []string{"smlouvy", "%zz"}},
		{"keeps a truncated escape raw", "smlouvy/%C", []string{"smlouvy", "%C"}},

		// ⚠ "Malformed" here means SYNTACTICALLY malformed, and nothing more.
		// `%C5` is two valid hex digits, so it decodes — to the lone byte 0xC5,
		// which is half a UTF-8 rune and matches no slug in either table. That is
		// the same 404 an unknown slug gets, which is the point: this function
		// does not validate, it parses, and the store is what decides existence.
		{"decodes a valid escape even into invalid UTF-8", "%C5", []string{"\xc5"}},

		// An encoded slash does NOT split: it was one segment on the wire and stays
		// one segment, or a name containing "/" would address a different depth.
		{"an encoded slash stays inside its segment", "a%2Fb/c", []string{"a/b", "c"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slugpath.Split(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Split(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}
