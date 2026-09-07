package mcp

import (
	"context"
	"sync"
	"time"
)

// The pool guards (v11, PRD §V11-8, D304–D306).
//
// ⚠ THE POOL IS THE NON-FUNCTIONAL REQUIREMENT, and it is not a tuning knob.
// `appdb` caps the connection pool at ONE, deliberately, and the whole backend is
// written around it — appdb.Collect closes rows precisely because a leaked
// *sql.Rows deadlocks THE NEXT QUERY IN THE PROCESS, the browser's included.
//
// An MCP client breaks the assumption that produced that cap. A browser makes one
// request per view; Claude issues parallel tool calls routinely, and six at once
// is ordinary. Six tool calls are six-plus queries with nothing pacing them, and
// the household's dashboard is behind them in the same queue.

// concurrencyPerToken is the semaphore width (D304). The third concurrent call
// from one token WAITS; it does not fail.
//
// ⚠ THIS IS BACK-PRESSURE, NOT A RATE LIMIT, and the difference is what makes it
// correct to block rather than refuse: a chatty agent should be slowed to the
// speed of the database, not told to go away. It is what keeps that agent from
// being a self-inflicted outage for everybody else.
const concurrencyPerToken = 2

// semaphores hands out one buffered channel per token id.
//
// ⚠ ENTRIES ARE NEVER REMOVED, and that is a deliberate leak of bounded size: a
// household has at most HOME_MCP_MAX_TOKENS_PER_USER tokens per member, so the
// map is tens of entries for the life of the process. Reference-counting them to
// delete on the last release would be a second synchronisation problem on the one
// path where a race means two calls sharing a slot they should not.
type semaphores struct {
	mu sync.Mutex
	m  map[string]chan struct{}
}

func newSemaphores() *semaphores { return &semaphores{m: map[string]chan struct{}{}} }

func (s *semaphores) for_(tokenID string) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.m[tokenID]
	if !ok {
		ch = make(chan struct{}, concurrencyPerToken)
		s.m[tokenID] = ch
	}
	return ch
}

// acquire blocks until a slot is free or ctx is done. The returned release is
// safe to call exactly once.
func (s *semaphores) acquire(ctx context.Context, tokenID string) (release func(), err error) {
	ch := s.for_(tokenID)
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return func() {}, ctx.Err()
	}
}

// rateLimiter is a per-token SLIDING-WINDOW counter of CALLS MADE.
//
// ⚠ auth.rateLimiter CANNOT BE REUSED, and this is the note most likely to be
// acted on without being read (D306). That type is (a) unexported, (b) a
// FIXED-window counter, and (c) counts FAILURES ONLY — its `allowed` deliberately
// records no attempt, because it exists to throttle failed logins and a
// successful login must not spend anybody's budget. A call-rate limiter counts
// successes. Promoting and generalising auth's would put two callers with
// opposite counting semantics behind one type, which is how the login limiter
// stops working — so this is a small separate one, as decided.
type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
	// lastSweep bounds how often the key eviction below runs — see sweepLocked.
	lastSweep time.Time
	now       func() time.Time
}

// sweepAbove is the key count past which a limiter starts evicting dead keys.
//
// ⚠ THIS MAP IS NOT BOUNDED BY THE HOUSEHOLD THE WAY THE SEMAPHORE MAP IS, and
// that difference is the whole reason eviction exists here and not there. A
// semaphore is keyed by a TOKEN ID, of which a household has tens; a limiter is
// also keyed by a CLIENT IP, which is read from X-Forwarded-For and is therefore
// whatever the caller says it is. `/mcp` is publicly routed, so without eviction
// one flood with a rotating header is one permanent map entry per request until
// the container is OOM-killed.
const sweepAbove = 4096

func newRateLimiter(limit int, window time.Duration, now func() time.Time) *rateLimiter {
	if now == nil {
		now = time.Now
	}
	return &rateLimiter{limit: limit, window: window, hits: map[string][]time.Time{}, now: now}
}

// allow records one call against key and reports whether it may proceed.
//
// Sliding rather than fixed: a fixed window lets a client spend the whole budget
// in the last second of one window and the whole budget again in the first second
// of the next, which for a limit that exists to protect ONE database connection
// is two-thirds of a minute at double the intended rate.
func (l *rateLimiter) allow(key string) bool {
	if l == nil || l.limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if l.spentLocked(key, now) {
		return false
	}
	l.recordLocked(key, now)
	return true
}

// blocked reports whether key has already spent its budget, WITHOUT recording an
// attempt.
//
// ⚠ IT EXISTS SO A LIMIT CAN BE CHECKED BEFORE THE WORK IT BOUNDS. The token
// lookup is a query on the one connection and it happens before there is a token
// id to key anything by, so a bearer that never resolves is keyed by the caller's
// IP alone: `blocked` is asked first, and only a FAILED resolution then calls
// `record`. A legitimate client therefore never spends this budget, and a flood
// of junk bearers is capped at the same figure as everything else.
func (l *rateLimiter) blocked(key string) bool {
	if l == nil || l.limit <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.spentLocked(key, l.now())
}

// record spends one unit of key's budget.
func (l *rateLimiter) record(key string) {
	if l == nil || l.limit <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.spentLocked(key, now) // prunes the window before appending
	l.recordLocked(key, now)
}

// spentLocked prunes key's window and reports whether it is at the limit.
func (l *rateLimiter) spentLocked(key string, now time.Time) bool {
	cut := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		// ⚠ Delete rather than store an empty slice: an idle key that is never
		// asked about again is exactly the entry that would otherwise live forever.
		delete(l.hits, key)
	} else {
		l.hits[key] = kept
	}
	return len(kept) >= l.limit
}

func (l *rateLimiter) recordLocked(key string, now time.Time) {
	l.hits[key] = append(l.hits[key], now)
	l.sweepLocked(now)
}

// sweepLocked evicts keys whose whole window has expired.
//
// ⚠ PRUNING ON READ IS NOT ENOUGH BY ITSELF: it only touches keys somebody asks
// about again, and the keys that matter here are the ones nobody ever will. The
// sweep is O(keys) and runs at most once per window, and only once the map is
// large enough for that to be worth doing — so the ordinary household never pays
// for it and a flood cannot outrun it by more than one window's worth of keys.
func (l *rateLimiter) sweepLocked(now time.Time) {
	if len(l.hits) < sweepAbove || now.Sub(l.lastSweep) < l.window {
		return
	}
	l.lastSweep = now
	cut := now.Add(-l.window)
	for k, ts := range l.hits {
		// Appended in time order, so the last entry is the newest.
		if len(ts) == 0 || !ts[len(ts)-1].After(cut) {
			delete(l.hits, k)
		}
	}
}
