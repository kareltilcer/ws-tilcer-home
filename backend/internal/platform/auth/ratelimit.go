package auth

import (
	"sync"
	"time"
)

// maxTrackedKeys caps the number of distinct rate-limit keys held at once. The key
// space is attacker-influenced: clientIP derives from X-Forwarded-For, which a client
// can spoof, so each bogus login with a fresh fake IP would otherwise add a permanent
// entry (fixed by the sweep below). The household's real key space is a few dozen, so
// this ceiling is only ever reached under a spoofed-key flood, where wiping the map is
// an acceptable degradation (counters reset) in exchange for a hard memory bound.
const maxTrackedKeys = 4096

// rateLimiter is a small fixed-window counter keyed by an arbitrary string. Login
// uses one per email and one per IP (FR-A5). In-process is sufficient: home runs
// as a single instance and the legitimate key space (household emails + IPs) is tiny.
type rateLimiter struct {
	mu        sync.Mutex
	max       int
	window    time.Duration
	hits      map[string]*hitWindow
	now       func() time.Time
	lastSweep time.Time
}

type hitWindow struct {
	start time.Time
	count int
}

func newRateLimiter(max int, window time.Duration, now func() time.Time) *rateLimiter {
	if now == nil {
		now = time.Now
	}
	return &rateLimiter{max: max, window: window, hits: map[string]*hitWindow{}, now: now}
}

// allowed reports whether key is currently under the limit, WITHOUT recording an
// attempt. Only failed logins are counted (via fail), so a legitimate user is
// never locked out by their own successful sign-ins.
func (r *rateLimiter) allowed(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	w := r.hits[key]
	if w == nil || r.now().Sub(w.start) >= r.window {
		return true
	}
	return w.count < r.max
}

// fail records a failed attempt for key (fixed-window).
func (r *rateLimiter) fail(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	r.sweep(now)
	w := r.hits[key]
	if w == nil || now.Sub(w.start) >= r.window {
		r.hits[key] = &hitWindow{start: now, count: 1}
		return
	}
	w.count++
}

// sweep drops windows that have fully elapsed (a stale window is treated as fresh by
// allowed anyway, so removing it frees memory without changing behaviour). It runs at
// most once per window so fail stays O(1) amortised, EXCEPT while the map is over
// maxTrackedKeys — a spoofed-key flood — where it runs eagerly and, if pruning expired
// entries still leaves it over the cap, wipes the map to keep memory bounded. Callers
// hold r.mu.
func (r *rateLimiter) sweep(now time.Time) {
	overCap := len(r.hits) > maxTrackedKeys
	if !overCap && now.Sub(r.lastSweep) < r.window {
		return
	}
	r.lastSweep = now
	for k, w := range r.hits {
		if now.Sub(w.start) >= r.window {
			delete(r.hits, k)
		}
	}
	if len(r.hits) > maxTrackedKeys {
		r.hits = map[string]*hitWindow{}
	}
}

// reset clears the counter for key after a successful login.
func (r *rateLimiter) reset(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.hits, key)
}
