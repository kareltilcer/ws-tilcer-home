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
	"time"
	"unicode/utf8"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
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

// formatList is the one place a module's items become notification text, so its
// edges are worth pinning down: they are what a household actually reads.
func TestFormatList(t *testing.T) {
	items := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = string(rune('A' + i))
		}
		return out
	}

	t.Run("one bulleted line per item", func(t *testing.T) {
		got := formatList(ListValue{Items: []string{"Vynést koš", "Zalít kytky"}}, maxListsRunes)
		if want := "• Vynést koš\n• Zalít kytky"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	// A tail of exactly one would read "…a další 1" — bad Czech, and it hides
	// less than the line it costs.
	t.Run("a sixth item is named rather than counted", func(t *testing.T) {
		got := formatList(ListValue{Items: items(6)}, maxListsRunes)
		if strings.Contains(got, "další") {
			t.Errorf("got %q, want all six named", got)
		}
		if lines := strings.Count(got, "\n") + 1; lines != 6 {
			t.Errorf("got %d lines, want 6", lines)
		}
	})

	t.Run("a seventh starts counting the rest", func(t *testing.T) {
		got := formatList(ListValue{Items: items(7)}, maxListsRunes)
		if want := "• A\n• B\n• C\n• D\n• E\n• …a další 2"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	// A note title can be pasted prose. Left alone, its newlines would forge
	// bullets the module never published.
	t.Run("an item's own whitespace cannot forge a line", func(t *testing.T) {
		got := formatList(ListValue{Items: []string{"Nákup\nna víkend"}}, maxListsRunes)
		if want := "• Nákup na víkend"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("an over-long item is clipped on a rune boundary", func(t *testing.T) {
		got := formatList(ListValue{Items: []string{strings.Repeat("ř", 80)}}, maxListsRunes)
		if !utf8.ValidString(got) {
			t.Fatalf("clipping produced invalid UTF-8: %q", got)
		}
		if want := listBullet + strings.Repeat("ř", maxListItemRunes) + "…"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("empty says the module's words, or the placeholder if it published none", func(t *testing.T) {
		if got := formatList(ListValue{Empty: "žádné připomínky"}, maxListsRunes); got != "žádné připomínky" {
			t.Errorf("got %q, want the module's empty text", got)
		}
		if got := formatList(ListValue{}, maxListsRunes); got != Placeholder {
			t.Errorf("got %q, want the placeholder — never an empty string", got)
		}
	})
}

// The composer offers every published list token side by side, so a body with
// two of them is an ordinary thing to write. platform/push clamps a body to
// MaxBodyRunes and clamps it from the END — so unbudgeted lists do not merely
// overflow, they cost exactly the "…a další N" tails that admit something was
// hidden.
func TestTwoListsShareOneAllowanceAndKeepTheirTails(t *testing.T) {
	const senderBodyRunes = push.MaxBodyRunes

	long := make([]string, 9)
	for i := range long {
		long[i] = strings.Repeat("ú", maxListItemRunes) + string(rune('A'+i))
	}
	body := Render(
		"Dnes máš rozděláno:\n{{list.todo.pravedelam_count}}\nA po termínu:\n{{list.events.overdue_open}}",
		RenderContext{Lists: map[string]ListValue{
			"todo.pravedelam_count": {Items: long},
			"events.overdue_open":   {Items: long},
		}},
	)

	if n := utf8.RuneCountInString(body); n > senderBodyRunes {
		t.Errorf("body is %d runes, past the sender's clamp — it would be cut mid-item:\n%s", n, body)
	}
	if got := strings.Count(body, "…a další "); got != 2 {
		t.Errorf("kept %d of the 2 tails; the household must be told what is hidden:\n%s", got, body)
	}
	if strings.Contains(body, "…a další 1") {
		t.Errorf("budgeting produced the tail of one the line cap exists to avoid:\n%s", body)
	}
}

// The pathological end of the same promise: an admin who inserts EVERY list chip
// the composer offers. At that many tokens the per-list share dips below a
// legible line, and the formatter's last resort must still produce a block that
// fits — an over-budget block hands the sender's end-clamp exactly the "…a
// další N" tails it exists to protect.
func TestManyListsStillFitTheSenderClamp(t *testing.T) {
	long := []string{strings.Repeat("ú", maxListItemRunes) + "A", strings.Repeat("ú", maxListItemRunes) + "B"}

	const nLists = 8
	var tokens []string
	rc := RenderContext{Lists: map[string]ListValue{}}
	for i := 0; i < nLists; i++ {
		key := "mod.list" + string(rune('0'+i))
		tokens = append(tokens, "{{list."+key+"}}")
		rc.Lists[key] = ListValue{Items: long}
	}
	body := Render(strings.Join(tokens, "\n"), rc)

	if n := utf8.RuneCountInString(body); n > push.MaxBodyRunes {
		t.Errorf("body is %d runes, past the sender's clamp — it would be cut mid-item:\n%s", n, body)
	}
	if got := strings.Count(body, "…a další "); got != nLists {
		t.Errorf("kept %d of the %d tails; the household must be told what is hidden:\n%s", got, nLists, body)
	}
}

// Budgeting must not shorten the case it was added to protect: household titles
// are short, so a lone list of real ones still names its five and counts the
// rest. Only genuinely long titles — a pinned note, say — trade a line away.
func TestBudgetingLeavesAnOrdinaryListAlone(t *testing.T) {
	body := Render("Dnes: {{list.events.pripominky_today}}",
		RenderContext{Lists: map[string]ListValue{"events.pripominky_today": {Items: []string{
			"Vynést koš", "Zalít kytky", "Umýt nádobí", "Vyluxovat", "Zaplatit nájem",
			"Objednat pneuservis", "Kontrola kotle",
		}}}})

	if want := "Dnes: • Vynést koš\n• Zalít kytky\n• Umýt nádobí\n• Vyluxovat\n• Zaplatit nájem\n• …a další 2"; body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

// ---- the storage snapshot's single-flight (v9) ----

// TestSnapshotWaiterRejectsAPassThatProducedNothing pins the difference between
// "a snapshot exists" and "the pass I waited on produced one".
//
// ⚠ IT HAS NO BLACK-BOX SURFACE, which is why it lives here. The bug it guards is
// invisible from outside: a compute that FAILS leaves `cached` holding whatever
// succeeded last, so a waiter checking only for nil returned figures from an
// arbitrarily earlier time, marked Cached, with a 200 — the failure reported to
// nobody but the one caller that ran the pass, and the admin's Obnovit silently
// not obnovit-ing. Reproducing that needs a pass to fail WHILE another caller is
// joined to it, which no public call can arrange.
//
// The goroutine below does exactly what a failed compute does: clear `computing`
// under the lock, leave `cached` and `generation` untouched, then close the channel.
func TestSnapshotWaiterRejectsAPassThatProducedNothing(t *testing.T) {
	sqldb, err := sql.Open("sqlite", "file:snapshot-waiter?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })

	svc := NewStorageService(StorageDeps{DB: sqldb, CacheSeconds: 60})
	stale := &StorageSnapshot{GeneratedAt: "2026-08-25T09:00:00Z"}
	svc.cached, svc.cachedAt, svc.generation = stale, svc.now(), 1

	done := make(chan struct{})
	svc.computing = done
	go func() {
		// Long enough that the caller below is parked in the select.
		time.Sleep(30 * time.Millisecond)
		svc.mu.Lock()
		svc.computing = nil // cached and generation deliberately NOT touched
		svc.mu.Unlock()
		close(done)
	}()

	got, err := svc.Snapshot(context.Background(), true)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if got.GeneratedAt == stale.GeneratedAt {
		t.Fatalf("the waiter was handed the snapshot from %s — the pass it joined produced "+
			"nothing, so `cached` being non-nil says only that something succeeded at some "+
			"point, not that this request was answered", stale.GeneratedAt)
	}
	if svc.generation != 2 {
		t.Errorf("generation = %d, want 2 — the waiter should have run its own pass", svc.generation)
	}
}

// A pass that DOES produce a snapshot is still shared, which is the whole point of
// coalescing: the waiter takes the answer it would have computed anyway.
func TestSnapshotWaiterTakesAPassThatLanded(t *testing.T) {
	sqldb, err := sql.Open("sqlite", "file:snapshot-waiter-ok?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })

	svc := NewStorageService(StorageDeps{DB: sqldb, CacheSeconds: 60})
	landed := &StorageSnapshot{GeneratedAt: "2026-08-25T11:00:00Z"}

	done := make(chan struct{})
	svc.computing = done
	go func() {
		time.Sleep(30 * time.Millisecond)
		svc.mu.Lock()
		svc.cached, svc.cachedAt = landed, svc.now()
		svc.generation++ // a successful pass moves the counter
		svc.computing = nil
		svc.mu.Unlock()
		close(done)
	}()

	got, err := svc.Snapshot(context.Background(), true)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if got.GeneratedAt != landed.GeneratedAt {
		t.Errorf("generated_at = %q, want %q — a waiter must take the answer of the pass it "+
			"joined rather than starting a second identical one", got.GeneratedAt, landed.GeneratedAt)
	}
	if !got.Cached {
		t.Error("a snapshot somebody else computed came back cached:false — a figure the " +
			"caller did not compute is one it should see marked as shared")
	}
	if svc.generation != 1 {
		t.Errorf("generation = %d, want 1 — the waiter ran a redundant pass", svc.generation)
	}
}
