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
	now    func() time.Time
}

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
	now := l.now()
	cut := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}
