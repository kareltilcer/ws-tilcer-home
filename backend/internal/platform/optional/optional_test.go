package optional_test

import (
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/optional"
)

// Of has one job, and the thing worth pinning is that it hands back a pointer to
// its OWN copy. Every caller writes `Archived: optional.Of(true)` into a patch
// struct the store then holds; if the pointer aliased anything of the caller's, a
// second call would reach back into the first patch.
func TestOfReturnsAnIndependentPointer(t *testing.T) {
	a := optional.Of(true)
	b := optional.Of(false)
	if *a != true || *b != false {
		t.Fatalf("Of round-trip: got %v / %v", *a, *b)
	}
	if a == b {
		t.Error("two calls returned the same pointer; a patch could alter another patch's field")
	}

	// The parameter is by value, so mutating the source afterwards must not move it.
	v := "shared"
	p := optional.Of(v)
	v = "private"
	if *p != "shared" {
		t.Errorf("Of aliased its argument: got %q, want %q", *p, "shared")
	}

	// And writing through the pointer must not reach back either.
	*p = "changed"
	if v != "private" {
		t.Errorf("writing through the pointer moved the source: got %q", v)
	}
}

// The generic is the whole reason this replaced six per-type copies: the seventh
// caller, whatever it sets, must not need a seventh function.
func TestOfCarriesAnyType(t *testing.T) {
	if got := *optional.Of(7); got != 7 {
		t.Errorf("int: got %d", got)
	}
	if got := *optional.Of("x"); got != "x" {
		t.Errorf("string: got %q", got)
	}
	type row struct{ ID string }
	if got := *optional.Of(row{ID: "abc"}); got.ID != "abc" {
		t.Errorf("struct: got %+v", got)
	}

	// A pointer to a nil pointer is still a non-nil pointer — which is exactly the
	// distinction a patch struct needs: "set this field to nothing" is not "absent".
	var nilp *string
	pp := optional.Of(nilp)
	if pp == nil {
		t.Fatal("Of returned nil for a nil value")
	}
	if *pp != nil {
		t.Error("Of did not preserve the nil it was given")
	}
}
