package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"
)

// The outbox tailer (PRD §V5-1 D56, HANDOFF-7 §4).
//
// Trigger notifications must fire only after the change they describe has
// COMMITTED, and must never make a mutation wait on a push service. The audit
// spine writes its event inside the caller's transaction, so it cannot know when
// that transaction commits — and trying to find out would couple every module's
// write path to the notifier.
//
// So don't try. A separate reader of audit_events only ever sees committed rows:
// the table already IS a transactional outbox, and tailing it by UUIDv7 keyset
// (the same pattern the log browser pages with) gives "after commit" for free.
//
// Delivery is AT-LEAST-ONCE: the cursor advances after a batch, so a crash
// mid-batch re-processes it. Listeners must be idempotent — dedupe on Entry.ID.

// Entry is one committed audit event as the tailer reads it back. It is the
// read-side mirror of Event (the write-side struct) and carries the identity
// fields the sink derived from the request context.
type Entry struct {
	ID         string
	TS         time.Time
	ActorUser  string
	ActorType  string
	ActorLabel string
	Module     string
	Action     string
	EntityType string
	EntityID   string
	Summary    string
	Level      string
	RequestID  string
	Meta       map[string]any
}

// Qualified returns the module-qualified action key ("todo" + "card.move" →
// "todo.card.move") used when displaying an action in the admin catalog.
func (e Entry) Qualified() string {
	if e.Module == "" {
		return e.Action
	}
	return e.Module + "." + e.Action
}

// Listener receives every committed audit event, oldest first. A module
// registers one at construction; it never imports the logging module to do so
// (D28 — the import-lint acceptance criterion).
type Listener interface {
	// OnEvent handles one event. It must be IDEMPOTENT: the same event id can
	// arrive twice after a crash. An error is logged and the batch continues —
	// one broken listener must not stall the outbox for the others.
	OnEvent(ctx context.Context, e Entry, changes []Change) error
	// NeedsChanges reports whether this listener wants the field diffs for e.
	// Loading them costs a second query per event, so the tailer asks first and
	// only pays for the events some listener will actually use.
	NeedsChanges(e Entry) bool
}

// CursorStore persists the tailer's position.
type CursorStore interface {
	Load(ctx context.Context) (string, error)
	Save(ctx context.Context, lastEventID string) error
}

// NotifierConfig tunes the tailer.
type NotifierConfig struct {
	// Poll is the correctness path: even with no nudges the tailer catches up
	// within this interval. Default 1s.
	Poll time.Duration
	// NudgeDelay debounces a nudge from the sink. Record runs INSIDE the writer's
	// transaction, so a nudge arrives just BEFORE the commit it refers to; a
	// short delay lets that commit land (and coalesces a burst of writes into one
	// scan). The poll still covers the case where it does not.
	NudgeDelay time.Duration
	// Batch bounds one scan. Default 200.
	Batch  int
	Logger *slog.Logger
}

// Notifier tails audit_events and fans committed events out to listeners.
type Notifier struct {
	db        *sql.DB
	cursor    CursorStore
	listeners []Listener
	cfg       NotifierConfig
	logger    *slog.Logger
	nudge     chan struct{}
	// done closes once the tailer goroutine has actually exited. Start always
	// closes it, including on the no-listeners path, so Wait is safe to call
	// unconditionally after Start.
	done chan struct{}
}

// NewNotifier builds the tailer. Listeners register before Start.
func NewNotifier(db *sql.DB, cursor CursorStore, cfg NotifierConfig) *Notifier {
	if cfg.Poll <= 0 {
		cfg.Poll = time.Second
	}
	if cfg.NudgeDelay <= 0 {
		cfg.NudgeDelay = 100 * time.Millisecond
	}
	if cfg.Batch <= 0 {
		cfg.Batch = 200
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Notifier{
		db: db, cursor: cursor, cfg: cfg, logger: cfg.Logger,
		// Buffered and never blocking: see Nudge.
		nudge: make(chan struct{}, 1),
		done:  make(chan struct{}),
	}
}

// Register adds a listener. Call before Start.
func (n *Notifier) Register(l Listener) {
	if l != nil {
		n.listeners = append(n.listeners, l)
	}
}

// Nudge asks for an early scan. It is safe to call from inside a write
// transaction — and that is exactly where it is called from (Sink.Record), which
// is why it can NEVER block: a full channel already means "a scan is pending",
// so dropping the signal loses nothing.
func (n *Notifier) Nudge() {
	select {
	case n.nudge <- struct{}{}:
	default:
	}
}

// Start runs the tailer until ctx is cancelled. It returns immediately. Call it
// exactly once; Wait blocks until the goroutine it starts has exited.
func (n *Notifier) Start(ctx context.Context) {
	if len(n.listeners) == 0 {
		// Nothing to notify: don't spend a goroutine and a query per second.
		n.logger.Info("audit notifier: no listeners registered, tailer not started")
		close(n.done)
		return
	}
	go func() {
		defer close(n.done)
		n.loop(ctx)
	}()
}

// Wait blocks until the tailer goroutine has exited.
//
// Cancelling Start's context only ASKS the tailer to stop — it can still be part
// way through a batch, and every event left in that batch is handed to a
// listener before the loop next looks at ctx.Done. A caller that needs the
// stronger guarantee "no listener will be called again" must join here.
//
// The shutdown flush needs exactly that: it waits on the sends its listener has
// already started, and a listener still running would be able to start another
// one after that wait had decided there was nothing left. The join is bounded —
// once ctx is cancelled every query in the loop fails immediately and the rest
// of a batch is in-memory work.
func (n *Notifier) Wait() { <-n.done }

func (n *Notifier) loop(ctx context.Context) {
	ticker := time.NewTicker(n.cfg.Poll)
	defer ticker.Stop()
	n.logger.Info("audit notifier: tailing audit_events", "poll", n.cfg.Poll, "listeners", len(n.listeners))

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.drain(ctx)
		case <-n.nudge:
			// Let the nudging transaction commit before looking.
			select {
			case <-ctx.Done():
				return
			case <-time.After(n.cfg.NudgeDelay):
			}
			n.drain(ctx)
		}
	}
}

// drain processes every pending batch until the table is caught up.
func (n *Notifier) drain(ctx context.Context) {
	for {
		processed, err := n.scanOnce(ctx)
		if err != nil {
			n.logger.Error("audit notifier: scan failed", "err", err)
			return
		}
		if processed < n.cfg.Batch {
			return
		}
	}
}

// scanOnce reads one batch past the cursor, delivers it, and advances.
func (n *Notifier) scanOnce(ctx context.Context) (int, error) {
	cursor, err := n.cursor.Load(ctx)
	if err != nil {
		return 0, err
	}

	rows, err := n.db.QueryContext(ctx,
		`SELECT id, ts, actor_user_id, actor_type, actor_label, module, action,
		        entity_type, entity_id, summary, level, request_id, meta
		   FROM audit_events
		  WHERE id > ?
		  ORDER BY id
		  LIMIT ?`, cursor, n.cfg.Batch)
	if err != nil {
		return 0, err
	}
	entries := make([]Entry, 0, n.cfg.Batch)
	for rows.Next() {
		var (
			e                                                  Entry
			ts                                                 string
			actorUser, actorLabel, entityType, entityID, level sql.NullString
			requestID, meta                                    sql.NullString
		)
		if err := rows.Scan(&e.ID, &ts, &actorUser, &e.ActorType, &actorLabel, &e.Module, &e.Action,
			&entityType, &entityID, &e.Summary, &level, &requestID, &meta); err != nil {
			_ = rows.Close()
			return 0, err
		}
		e.TS, _ = time.Parse(TSLayout, ts)
		e.ActorUser, e.ActorLabel = actorUser.String, actorLabel.String
		e.EntityType, e.EntityID = entityType.String, entityID.String
		e.Level, e.RequestID = level.String, requestID.String
		if meta.Valid && meta.String != "" {
			_ = json.Unmarshal([]byte(meta.String), &e.Meta)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	if len(entries) == 0 {
		return 0, nil
	}

	processed := 0
	for _, e := range entries {
		var changes []Change
		if n.anyListenerNeedsChanges(e) {
			loaded, cerr := n.loadChanges(ctx, e.ID)
			if cerr != nil {
				// A missing diff must not stop the event: the notification just
				// renders its change tokens as placeholders.
				n.logger.Warn("audit notifier: load changes", "err", cerr, "event_id", e.ID)
			}
			changes = loaded
		}
		for _, l := range n.listeners {
			n.deliver(ctx, l, e, changes)
		}

		// Advance PER EVENT, not once per batch. The cursor can only be written
		// after a listener has been handed the event (a crash before it must
		// re-deliver, never skip), so some redelivery window is unavoidable — a
		// push is an external side effect and cannot be committed with a database
		// write. What IS avoidable is the size of that window: saving per batch
		// meant a crash could re-deliver up to Batch (200) events, and the
		// listeners' dedupe is process-local, so exactly the crash it exists to
		// absorb is the one that restarts with an empty set. Per event, at most
		// the single in-flight event can be duplicated.
		if err := n.cursor.Save(ctx, e.ID); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

// deliver hands one event to one listener, containing both errors and panics.
// The tailer is a single long-lived goroutine shared by every listener: an
// unrecovered panic in one would take down the whole outbox and silently end all
// notifications, so it is contained and logged like any other failure.
func (n *Notifier) deliver(ctx context.Context, l Listener, e Entry, changes []Change) {
	defer func() {
		if p := recover(); p != nil {
			n.logger.Error("audit notifier: listener panicked", "panic", p, "event_id", e.ID, "action", e.Qualified())
		}
	}()
	if err := l.OnEvent(ctx, e, changes); err != nil {
		n.logger.Warn("audit notifier: listener failed", "err", err, "event_id", e.ID, "action", e.Qualified())
	}
}

func (n *Notifier) anyListenerNeedsChanges(e Entry) bool {
	for _, l := range n.listeners {
		if l.NeedsChanges(e) {
			return true
		}
	}
	return false
}

func (n *Notifier) loadChanges(ctx context.Context, eventID string) ([]Change, error) {
	rows, err := n.db.QueryContext(ctx,
		`SELECT field, old_value, new_value FROM audit_changes WHERE event_id = ? ORDER BY field`, eventID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Change
	for rows.Next() {
		var (
			c        Change
			old, new sql.NullString
		)
		if err := rows.Scan(&c.Field, &old, &new); err != nil {
			return nil, err
		}
		if old.Valid {
			v := old.String
			c.Old = &v
		}
		if new.Valid {
			v := new.String
			c.New = &v
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- cursor persistence ----

// SQLCursor is the audit_notify_cursor-backed CursorStore.
type SQLCursor struct{ db *sql.DB }

// NewSQLCursor returns the cursor store over the platform table.
func NewSQLCursor(db *sql.DB) *SQLCursor { return &SQLCursor{db: db} }

func (c *SQLCursor) Load(ctx context.Context) (string, error) {
	var last string
	err := c.db.QueryRowContext(ctx, `SELECT last_event_id FROM audit_notify_cursor WHERE id = 1`).Scan(&last)
	if err == sql.ErrNoRows {
		// The migration seeds the row; an absent one means a hand-edited DB.
		// Starting from empty would replay all history, so refuse to guess.
		return "", sql.ErrNoRows
	}
	return last, err
}

func (c *SQLCursor) Save(ctx context.Context, lastEventID string) error {
	_, err := c.db.ExecContext(ctx,
		`UPDATE audit_notify_cursor SET last_event_id = ?, updated_at = ? WHERE id = 1`,
		lastEventID, time.Now().UTC().Format(TSLayout))
	return err
}
