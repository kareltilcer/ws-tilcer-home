package admin

// Tests for internals that have no honest black-box surface: the trigger
// listener's rule cache, whose whole contract is an ordering between an
// off-lock database read and a concurrent invalidation, and the delivery log's
// string clipping.

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	_ "modernc.org/sqlite"
)

// A FAILED rule load is not "no rules match", and OnEvent used to make them
// indistinguishable: it discarded enabledRules' ok flag and returned nil, so the
// tailer logged nothing and every trigger for that event was skipped. Worse, the
// event was already in the dedupe set by then, so a redelivery would skip it too
// and the notification was lost for good rather than retried.
func TestOnEventReportsARuleLoadFailureAndRetriesLater(t *testing.T) {
	// An UNMIGRATED database: EnabledRules fails on the missing table, which is
	// the same shape as any transient read failure.
	sqldb, err := sql.Open("sqlite", "file:onevent-load-fail?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })

	svc := &Service{
		db:     sqldb,
		store:  NewStore(sqldb),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	l := newListener(svc)

	e := audit.Entry{ID: "evt-1", Module: audit.ModuleTodo, Action: "card.move"}
	if err := l.OnEvent(context.Background(), e, nil); err == nil {
		t.Fatal("a failed rule load was reported as success — the tailer logs nothing and the event is silently dropped")
	}

	l.mu.Lock()
	seen, seq := l.seen[e.ID], len(l.seenSeq)
	l.mu.Unlock()
	if seen || seq != 0 {
		t.Errorf("event still marked seen (seen=%t, seq=%d); a redelivery would skip it instead of retrying",
			seen, seq)
	}
}

// The rule cache is loaded OFF the mutex, so an InvalidateRules can land between
// the read starting and its result being stored. Storing unconditionally threw
// that invalidation away: the stale set was marked loaded and the listener kept
// matching a rule the admin had just disabled — for as long as it took some
// unrelated rule edit to invalidate again.
func TestCacheRulesDiscardsASetInvalidatedMidLoad(t *testing.T) {
	l := &listener{seen: map[string]bool{}, buffers: map[string]*coalesceBuffer{}}

	// The generation enabledRules would have stamped before reading the database.
	l.mu.Lock()
	gen := l.generation
	l.mu.Unlock()

	// An admin disables a rule while that read is in flight.
	l.InvalidateRules()

	if l.cacheRules(gen, []Rule{{ID: "stale", Name: "právě vypnuté pravidlo"}}) {
		t.Fatal("a set loaded before the invalidation was cached; the invalidation is lost")
	}
	l.mu.Lock()
	loaded, rules := l.loaded, l.rules
	l.mu.Unlock()
	if loaded {
		t.Error("cache marked loaded after a lost-race load — the next event would match stale rules")
	}
	if rules != nil {
		t.Errorf("rules = %v, want the cache left empty so the next call reloads", rules)
	}
}

// The ordinary path must still cache, or every event pays for a database read.
func TestCacheRulesStoresAnUncontestedLoad(t *testing.T) {
	l := &listener{seen: map[string]bool{}, buffers: map[string]*coalesceBuffer{}}

	l.mu.Lock()
	gen := l.generation
	l.mu.Unlock()

	if !l.cacheRules(gen, []Rule{{ID: "r1"}}) {
		t.Fatal("an uncontested load was not cached")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.loaded || len(l.rules) != 1 {
		t.Errorf("loaded=%t rules=%d, want the set cached", l.loaded, len(l.rules))
	}
}

// Push services answer in their own wording. Clipping at a byte index split a
// multi-byte rune and left invalid UTF-8 in notification_deliveries.error, which
// the Doručení tab then rendered as replacement characters.
func TestTruncateNeverSplitsARune(t *testing.T) {
	// "ř" is two bytes, so an ODD byte-index cut lands squarely inside a rune —
	// which is exactly what the old byte-slicing did.
	long := strings.Repeat("ř", 40)

	got := truncate(long, 11)
	if !utf8.ValidString(got) {
		t.Fatalf("truncate produced invalid UTF-8: %q", got)
	}
	if want := strings.Repeat("ř", 11) + "…"; got != want {
		t.Errorf("truncate = %q, want %q (the limit counts runes, not bytes)", got, want)
	}

	// Short enough to keep whole, counted in runes rather than bytes.
	if got := truncate(long, 40); got != long {
		t.Errorf("a string exactly at the limit was clipped: %q", got)
	}
	if got := truncate("push service returned 503", 500); got != "push service returned 503" {
		t.Errorf("an ASCII error under the limit was altered: %q", got)
	}
}
