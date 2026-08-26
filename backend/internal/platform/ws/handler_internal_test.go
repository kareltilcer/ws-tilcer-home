package ws

// The two pure helpers behind the revalidation pump's schedule. Neither is
// reachable from the external ws_test package, and neither changes an observable
// behaviour when it breaks — a jitter that skews low just runs the pump more
// often than configured, and a broken fallback silently substitutes a different
// interval — so they are pinned here or not at all.

import (
	"testing"
	"time"
)

// TestRevalidateInterval pins the substitution config.go's range check reasons
// about from the other side of the package boundary.
//
// ⚠ HOME_WS_REVALIDATE_MINUTES is bounded at 1..1440 precisely so a 0 never
// reaches here and silently becomes five minutes while Redacted() prints the
// value the operator set. That bound lives in another package; this is the half
// that keeps the substitution itself honest if the bound is ever relaxed.
func TestRevalidateInterval(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{"zero falls back", 0, defaultRevalidateEvery},
		{"negative falls back", -time.Minute, defaultRevalidateEvery},
		{"configured value is kept", 90 * time.Second, 90 * time.Second},
		{"the default itself is kept", defaultRevalidateEvery, defaultRevalidateEvery},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := revalidateInterval(tc.in); got != tc.want {
				t.Errorf("revalidateInterval(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestJitter pins the spread.
//
// ⚠ The point of jittering is that a household's sessions do NOT tick in phase:
// each tick is a query against a pool of exactly one connection, so a burst every
// interval queues behind user-facing requests. An offset that skews low (say
// every/4 + rand, an easy slip) also silently multiplies the pump's cost, and
// nothing else in the suite would notice.
func TestJitter(t *testing.T) {
	const every = time.Minute
	lo, hi := every*3/4, every*5/4
	var min, max time.Duration = hi, lo
	for i := 0; i < 500; i++ {
		got := jitter(every)
		if got < lo || got >= hi {
			t.Fatalf("jitter(%v) = %v, want [%v, %v)", every, got, lo, hi)
		}
		if got < min {
			min = got
		}
		if got > max {
			max = got
		}
	}
	// And it actually varies — a constant sits inside the range and defeats the
	// whole purpose.
	if min == max {
		t.Errorf("jitter returned %v on all 500 draws; sockets would tick in phase", min)
	}

	// An interval too small to halve has no room to spread and must be returned
	// unchanged rather than collapsing to zero (a timer that fires immediately).
	if got := jitter(time.Nanosecond); got != time.Nanosecond {
		t.Errorf("jitter(1ns) = %v, want 1ns", got)
	}
	if got := jitter(0); got != 0 {
		t.Errorf("jitter(0) = %v, want 0", got)
	}
}
