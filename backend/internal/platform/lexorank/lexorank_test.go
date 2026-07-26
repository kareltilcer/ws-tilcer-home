package lexorank

import (
	"sort"
	"testing"
)

func TestBetween_ProducesStrictlyBetween(t *testing.T) {
	a := First()
	b := Tail(a)
	mid := Between(a, b)
	if !(a < mid && mid < b) {
		t.Fatalf("expected %q < %q < %q", a, mid, b)
	}
}

func TestHeadTail_Ordering(t *testing.T) {
	x := First()
	if h := Head(x); !(h < x) {
		t.Errorf("Head(%q)=%q not < x", x, h)
	}
	if tl := Tail(x); !(x < tl) {
		t.Errorf("Tail(%q)=%q not > x", x, tl)
	}
}

// TestDegenerateInsertAtSameSpot is the spec's required test: 200 inserts at the
// same position must yield strictly ordered, distinct keys (keys lengthen, never
// collide, never exhaust).
func TestDegenerateInsertAtSameSpot(t *testing.T) {
	lo := First()
	hi := Tail(lo)
	seen := map[string]bool{lo: true, hi: true}
	prevHi := hi
	maxLen := 0
	for i := 0; i < 200; i++ {
		k := Between(lo, prevHi)
		if !(lo < k && k < prevHi) {
			t.Fatalf("insert %d: %q not strictly between %q and %q", i, k, lo, prevHi)
		}
		if seen[k] {
			t.Fatalf("insert %d: duplicate key %q", i, k)
		}
		seen[k] = true
		if len(k) > maxLen {
			maxLen = len(k)
		}
		prevHi = k // always insert just below the previous insert (the tight spot)
	}
	if maxLen > 64 {
		t.Errorf("keys grew unexpectedly long: max len %d", maxLen)
	}
	t.Logf("200 inserts at one spot: max key length %d", maxLen)
}

func TestSequenceStaysSorted(t *testing.T) {
	// Build a sequence by repeated tail-append, then insert between random pairs.
	keys := []string{First()}
	for i := 0; i < 50; i++ {
		keys = append(keys, Tail(keys[len(keys)-1]))
	}
	// Insert between every adjacent pair.
	var grown []string
	for i := 0; i < len(keys)-1; i++ {
		grown = append(grown, keys[i], Between(keys[i], keys[i+1]))
	}
	grown = append(grown, keys[len(keys)-1])
	if !sort.StringsAreSorted(grown) {
		t.Fatalf("grown sequence is not sorted: %v", grown)
	}
	// All distinct.
	seen := map[string]bool{}
	for _, k := range grown {
		if seen[k] {
			t.Fatalf("duplicate key %q", k)
		}
		seen[k] = true
	}
}

func TestNKeys_OrderedDistinctEqualWidth(t *testing.T) {
	for _, n := range []int{1, 3, 40, 62, 100, 500} {
		keys := NKeys(n)
		if len(keys) != n {
			t.Fatalf("NKeys(%d) returned %d keys", n, len(keys))
		}
		if !sort.StringsAreSorted(keys) {
			t.Fatalf("NKeys(%d) not sorted: %v", n, keys)
		}
		w := len(keys[0])
		seen := map[string]bool{}
		for _, k := range keys {
			if len(k) != w {
				t.Errorf("NKeys(%d): inconsistent width %q", n, k)
			}
			if seen[k] {
				t.Fatalf("NKeys(%d): duplicate %q", n, k)
			}
			seen[k] = true
		}
		// New keys must be insertable around: something before, between, after.
		if !(Head(keys[0]) < keys[0]) {
			t.Errorf("NKeys(%d): cannot insert before first", n)
		}
		if n >= 2 {
			mid := Between(keys[0], keys[1])
			if !(keys[0] < mid && mid < keys[1]) {
				t.Errorf("NKeys(%d): cannot insert between first two", n)
			}
		}
	}
}
