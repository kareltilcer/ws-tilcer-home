package auth

import (
	"strconv"
	"testing"
	"time"
)

// TestRateLimiterBlocksAfterMax verifies the fixed-window still limits a single key.
func TestRateLimiterBlocksAfterMax(t *testing.T) {
	now := time.Unix(0, 0)
	rl := newRateLimiter(3, time.Minute, func() time.Time { return now })

	for i := 0; i < 3; i++ {
		if !rl.allowed("k") {
			t.Fatalf("attempt %d should be allowed", i)
		}
		rl.fail("k")
	}
	if rl.allowed("k") {
		t.Fatal("key should be blocked after reaching max")
	}
	// Window rolls over -> counter is fresh again.
	now = now.Add(time.Minute)
	if !rl.allowed("k") {
		t.Fatal("key should be allowed once its window elapses")
	}
}

// TestRateLimiterPrunesExpiredWindows is the memory-DoS regression: distinct keys
// (spoofed X-Forwarded-For) must not accumulate forever. Once a window has elapsed the
// throttled sweep on the next fail must drop the stale entries.
func TestRateLimiterPrunesExpiredWindows(t *testing.T) {
	now := time.Unix(0, 0)
	rl := newRateLimiter(10, time.Minute, func() time.Time { return now })

	for i := 0; i < 1000; i++ {
		rl.fail("ip:" + strconv.Itoa(i))
	}
	if got := len(rl.hits); got != 1000 {
		t.Fatalf("expected 1000 tracked keys, got %d", got)
	}

	// Advance past the window so every entry above is stale, then a single fail
	// triggers the sweep (it runs at most once per window).
	now = now.Add(2 * time.Minute)
	rl.fail("ip:new")
	if got := len(rl.hits); got != 1 {
		t.Fatalf("expected stale windows pruned to just the new key, got %d", got)
	}
}

// TestRateLimiterCapsUnderBurst verifies the hard cap bounds memory even inside a
// single window, where no entry is stale enough to be pruned by time alone.
func TestRateLimiterCapsUnderBurst(t *testing.T) {
	now := time.Unix(0, 0)
	rl := newRateLimiter(10, time.Minute, func() time.Time { return now })

	// All failures land within one window, so none are ever time-expired; only the
	// cap keeps the map bounded.
	for i := 0; i < maxTrackedKeys*3; i++ {
		rl.fail("ip:" + strconv.Itoa(i))
	}
	if got := len(rl.hits); got > maxTrackedKeys+1 {
		t.Fatalf("map exceeded cap: got %d, want <= %d", got, maxTrackedKeys+1)
	}
}
