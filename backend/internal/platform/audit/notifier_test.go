package audit_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// capture is an idempotent listener: it records each event once, deduping on id
// exactly as the outbox contract requires of every listener.
type capture struct {
	mu       sync.Mutex
	seen     map[string]int // event id -> times delivered
	order    []string       // ids in delivery order, deduped
	wantDiff bool
	changes  map[string][]audit.Change
	panicOn  string
	err      error
}

func newCapture() *capture {
	return &capture{seen: map[string]int{}, changes: map[string][]audit.Change{}}
}

func (c *capture) OnEvent(_ context.Context, e audit.Entry, changes []audit.Change) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.panicOn != "" && e.Action == c.panicOn {
		panic("listener exploded on " + e.Action)
	}
	c.seen[e.ID]++
	if c.seen[e.ID] == 1 {
		c.order = append(c.order, e.ID)
		c.changes[e.ID] = changes
	}
	return c.err
}

func (c *capture) NeedsChanges(audit.Entry) bool { return c.wantDiff }

func (c *capture) ids() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.order...)
}

func (c *capture) deliveries(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen[id]
}

// writeEvent records one audit event through the real sink, in its own
// transaction — i.e. exactly how a module mutation writes one.
func writeEvent(t *testing.T, sqldb *sql.DB, sink *audit.Writer, action, summary string, changes ...audit.Change) string {
	t.Helper()
	ctx := testsupport.CtxUser("u1", "admin")
	var id string
	err := appdb.WithTx(ctx, sqldb, func(tx *sql.Tx) error {
		var err error
		id, err = sink.Record(ctx, tx, audit.Event{
			Module: audit.ModuleTodo, Action: action, EntityType: "card", EntityID: "card-1",
			Summary: summary, Changes: changes,
		})
		return err
	})
	if err != nil {
		t.Fatalf("write event %s: %v", action, err)
	}
	return id
}

func newNotifier(t *testing.T, sqldb *sql.DB, l audit.Listener) *audit.Notifier {
	t.Helper()
	n := audit.NewNotifier(sqldb, audit.NewSQLCursor(sqldb), audit.NotifierConfig{
		Poll:       20 * time.Millisecond,
		NudgeDelay: time.Millisecond,
		Batch:      2, // small, so batching and the crash case are exercised
	})
	n.Register(l)
	return n
}

// waitFor polls until cond holds or the deadline passes — the tailer is
// asynchronous by design.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

// The core guarantee: an event written by any module reaches the listener only
// once it has COMMITTED, and the events arrive oldest-first.
func TestNotifierDeliversCommittedEventsInOrder(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	sink := audit.NewSink()
	cap := newCapture()
	n := newNotifier(t, sqldb, cap)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	first := writeEvent(t, sqldb, sink, "card.create", "Vytvořena karta")
	second := writeEvent(t, sqldb, sink, "card.move", "Přesunuta karta")
	third := writeEvent(t, sqldb, sink, "card.update", "Upravena karta")

	waitFor(t, func() bool { return len(cap.ids()) == 3 }, "three events to arrive")
	got := cap.ids()
	want := []string{first, second, third}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("delivery order = %v, want %v (oldest first)", got, want)
	}
}

// A rolled-back mutation never reaches a listener — the tailer only ever reads
// committed rows, which is the whole reason the audit table works as an outbox.
func TestNotifierNeverDeliversRolledBackEvents(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	sink := audit.NewSink()
	cap := newCapture()
	n := newNotifier(t, sqldb, cap)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	rollbackCtx := testsupport.CtxUser("u1", "admin")
	_ = appdb.WithTx(rollbackCtx, sqldb, func(tx *sql.Tx) error {
		if _, err := sink.Record(rollbackCtx, tx, audit.Event{
			Module: audit.ModuleTodo, Action: "card.create", Summary: "Nikdy neuložená karta",
		}); err != nil {
			return err
		}
		return sql.ErrConnDone // force a rollback
	})

	committed := writeEvent(t, sqldb, sink, "card.move", "Přesunuta karta")
	waitFor(t, func() bool { return len(cap.ids()) == 1 }, "the committed event to arrive")

	if ids := cap.ids(); len(ids) != 1 || ids[0] != committed {
		t.Errorf("delivered %v, want only the committed event %s", ids, committed)
	}
}

// The cursor persists, so a restart resumes rather than replaying.
func TestNotifierCursorSurvivesRestart(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	sink := audit.NewSink()

	first := newCapture()
	n1 := newNotifier(t, sqldb, first)
	ctx1, cancel1 := context.WithCancel(context.Background())
	n1.Start(ctx1)

	writeEvent(t, sqldb, sink, "card.create", "Jedna")
	waitFor(t, func() bool { return len(first.ids()) == 1 }, "the first event")
	cancel1()
	time.Sleep(50 * time.Millisecond) // let the loop exit

	// A second event lands while nothing is tailing.
	afterRestart := writeEvent(t, sqldb, sink, "card.move", "Dvě")

	second := newCapture()
	n2 := newNotifier(t, sqldb, second)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	n2.Start(ctx2)

	waitFor(t, func() bool { return len(second.ids()) == 1 }, "the missed event after restart")
	if ids := second.ids(); len(ids) != 1 || ids[0] != afterRestart {
		t.Errorf("after restart got %v, want exactly the missed event %s (no gap, no replay)", ids, afterRestart)
	}
}

// THE upgrade-safety test. Home already holds months of audit history; if the
// cursor started empty, enabling v5 would replay every past change as a push.
func TestNotifierDoesNotReplayPreExistingHistory(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	sink := audit.NewSink()

	// Pretend these were written long before v5 was deployed.
	for i := 0; i < 5; i++ {
		writeEvent(t, sqldb, sink, "card.create", "Historie")
	}
	// Re-seed the cursor the way the migration does on the first boot after it.
	if _, err := sqldb.Exec(
		`UPDATE audit_notify_cursor
		    SET last_event_id = (SELECT COALESCE(MAX(id), '') FROM audit_events)`); err != nil {
		t.Fatalf("seed cursor: %v", err)
	}

	cap := newCapture()
	n := newNotifier(t, sqldb, cap)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	newEvent := writeEvent(t, sqldb, sink, "card.move", "Až po nasazení v5")
	waitFor(t, func() bool { return len(cap.ids()) == 1 }, "the post-deploy event")
	time.Sleep(80 * time.Millisecond) // give any replay a chance to show up

	if ids := cap.ids(); len(ids) != 1 || ids[0] != newEvent {
		t.Errorf("got %d deliveries (%v), want ONLY the event written after the cursor was seeded", len(ids), ids)
	}
}

// The migration itself must seed the cursor, not leave it empty.
func TestMigrationSeedsCursorToMaxEventID(t *testing.T) {
	sqldb := testsupport.NewDB(t)

	var last string
	if err := sqldb.QueryRow(`SELECT last_event_id FROM audit_notify_cursor WHERE id = 1`).Scan(&last); err != nil {
		t.Fatalf("the migration must create exactly one cursor row: %v", err)
	}
	// A fresh database has no events, so the seed is the empty string — every
	// UUIDv7 sorts after it, so nothing is skipped on a new install.
	if last != "" {
		t.Errorf("fresh install cursor = %q, want empty", last)
	}
}

// At-least-once: a crash between handing a batch over and saving the cursor
// re-delivers it. The listener's dedupe is what turns that into one push.
func TestNotifierAtLeastOnceWithIdempotentListener(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	sink := audit.NewSink()
	cap := newCapture()

	id := writeEvent(t, sqldb, sink, "card.create", "Jedna")

	// Two independent notifier "processes" over the same DB, the second starting
	// from a cursor that was never advanced — exactly a crash mid-batch.
	for i := 0; i < 2; i++ {
		n := audit.NewNotifier(sqldb, &frozenCursor{}, audit.NotifierConfig{Poll: 10 * time.Millisecond, Batch: 10})
		n.Register(cap)
		ctx, cancel := context.WithCancel(context.Background())
		n.Start(ctx)
		waitFor(t, func() bool { return cap.deliveries(id) >= i+1 }, "redelivery")
		cancel()
		time.Sleep(30 * time.Millisecond)
	}

	if got := cap.deliveries(id); got < 2 {
		t.Fatalf("expected the event to be re-delivered at least twice, got %d", got)
	}
	if ids := cap.ids(); len(ids) != 1 {
		t.Errorf("an idempotent listener must record it once, got %d distinct: %v", len(ids), ids)
	}
}

// frozenCursor never advances — it simulates a process that dies before saving.
type frozenCursor struct{}

func (frozenCursor) Load(context.Context) (string, error) { return "", nil }
func (frozenCursor) Save(context.Context, string) error   { return nil }

// Field diffs cost an extra query per event, so they are loaded only when a
// listener says it wants them.
func TestNotifierLoadsChangesOnlyWhenWanted(t *testing.T) {
	t.Run("wanted", func(t *testing.T) {
		sqldb := testsupport.NewDB(t)
		sink := audit.NewSink()
		cap := newCapture()
		cap.wantDiff = true
		n := newNotifier(t, sqldb, cap)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		n.Start(ctx)

		id := writeEvent(t, sqldb, sink, "card.update", "Upravena karta",
			audit.Change{Field: "title", Old: audit.Ptr("Staré"), New: audit.Ptr("Nové")})

		waitFor(t, func() bool { return len(cap.ids()) == 1 }, "the event")
		cap.mu.Lock()
		defer cap.mu.Unlock()
		got := cap.changes[id]
		if len(got) != 1 || got[0].Field != "title" || *got[0].New != "Nové" {
			t.Errorf("changes = %+v, want the title diff", got)
		}
	})

	t.Run("not wanted", func(t *testing.T) {
		sqldb := testsupport.NewDB(t)
		sink := audit.NewSink()
		cap := newCapture() // wantDiff stays false
		n := newNotifier(t, sqldb, cap)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		n.Start(ctx)

		id := writeEvent(t, sqldb, sink, "card.update", "Upravena karta",
			audit.Change{Field: "title", Old: audit.Ptr("Staré"), New: audit.Ptr("Nové")})

		waitFor(t, func() bool { return len(cap.ids()) == 1 }, "the event")
		cap.mu.Lock()
		defer cap.mu.Unlock()
		if len(cap.changes[id]) != 0 {
			t.Errorf("changes were loaded for a listener that does not want them: %+v", cap.changes[id])
		}
	})
}

// One misbehaving listener must not end notifications for the whole service.
func TestNotifierSurvivesAPanickingListener(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	sink := audit.NewSink()
	boom := newCapture()
	boom.panicOn = "card.create"
	n := newNotifier(t, sqldb, boom)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	writeEvent(t, sqldb, sink, "card.create", "Tenhle listener vybuchne")
	survivor := writeEvent(t, sqldb, sink, "card.move", "A tenhle musí dorazit")

	waitFor(t, func() bool {
		for _, id := range boom.ids() {
			if id == survivor {
				return true
			}
		}
		return false
	}, "the tailer to keep running after a panic")
}

// A listener returning an error is logged and skipped, not retried forever.
func TestNotifierContinuesPastListenerErrors(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	sink := audit.NewSink()
	cap := newCapture()
	cap.err = context.DeadlineExceeded
	n := newNotifier(t, sqldb, cap)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	writeEvent(t, sqldb, sink, "card.create", "Jedna")
	writeEvent(t, sqldb, sink, "card.move", "Dvě")
	waitFor(t, func() bool { return len(cap.ids()) == 2 }, "both events despite errors")
}

// The nudge wakes the tailer early; it must never block the caller, since it is
// invoked from inside a write transaction.
func TestNudgeNeverBlocks(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	n := audit.NewNotifier(sqldb, audit.NewSQLCursor(sqldb), audit.NotifierConfig{})

	done := make(chan struct{})
	go func() {
		// Far more nudges than the channel can hold: every one must return at once.
		for i := 0; i < 1000; i++ {
			n.Nudge()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Nudge blocked — it runs inside a write transaction and must never wait")
	}
}

// With no listeners the tailer must not start: a query every second, forever,
// for nobody.
func TestNotifierWithoutListenersDoesNotRun(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	sink := audit.NewSink()
	n := audit.NewNotifier(sqldb, audit.NewSQLCursor(sqldb), audit.NotifierConfig{Poll: 10 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)
	writeEvent(t, sqldb, sink, "card.create", "Nikdo neposlouchá")
	time.Sleep(60 * time.Millisecond)

	var cursor string
	if err := sqldb.QueryRow(`SELECT last_event_id FROM audit_notify_cursor WHERE id = 1`).Scan(&cursor); err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	if cursor != "" {
		t.Errorf("cursor advanced to %q with no listeners registered", cursor)
	}

	// Wait must still return, or a shutdown that joins the tailer would hang on
	// a household with no trigger rules.
	joined := make(chan struct{})
	go func() { defer close(joined); n.Wait() }()
	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait blocked even though the tailer was never started")
	}
}

// Cancelling the tailer's context only ASKS it to stop. The shutdown flush waits
// on the sends its listener has already started, so it needs the stronger
// guarantee that no listener can be called again — otherwise a dispatch racing
// that wait is either abandoned mid-send (the notification is lost: its cursor
// has already advanced) or trips the WaitGroup's own misuse check.
func TestNotifierWaitJoinsTheTailer(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	sink := audit.NewSink()
	cap := newCapture()
	n := newNotifier(t, sqldb, cap)

	ctx, cancel := context.WithCancel(context.Background())
	n.Start(ctx)
	writeEvent(t, sqldb, sink, "card.create", "Něco se stalo")
	waitFor(t, func() bool { return len(cap.ids()) == 1 }, "the first event")

	cancel()
	joined := make(chan struct{})
	go func() { defer close(joined); n.Wait() }()
	select {
	case <-joined:
	case <-time.After(3 * time.Second):
		t.Fatal("Wait did not return after the context was cancelled")
	}

	// Once Wait has returned the tailer goroutine is gone, so nothing can reach
	// the listener any more — which is the property the flush depends on.
	before := len(cap.ids())
	writeEvent(t, sqldb, sink, "card.move", "Po zastavení")
	time.Sleep(80 * time.Millisecond) // several poll intervals
	if after := len(cap.ids()); after != before {
		t.Errorf("listener received %d more events after Wait returned; the tailer was still running", after-before)
	}
}
