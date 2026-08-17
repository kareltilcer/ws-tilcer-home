package admin

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
)

// The trigger listener (PRD FR-ADM2, HANDOFF-7 §5).
//
// It receives every COMMITTED audit event from the platform outbox tailer and
// decides which trigger rules it matches. Two properties matter most:
//
//   - It is IDEMPOTENT. The tailer is at-least-once, so the same event id can
//     arrive twice after a crash; a recently-seen set makes the second arrival a
//     no-op rather than a second push.
//   - It never imports another feature module. It sees audit.Entry (a platform
//     type) and sends through platform/push — which is what keeps the import-lint
//     acceptance criterion (D28) true while still reacting to todo, events, notes
//     and documents alike.

// seenCapacity bounds the dedupe set. Far more than any realistic redelivery
// window, and small enough that the memory never matters.
const seenCapacity = 2048

type listener struct {
	svc *Service

	mu     sync.Mutex
	rules  []Rule
	loaded bool
	// generation counts rule invalidations. enabledRules reads the database OFF
	// the lock, so it stamps the generation it started at and refuses to cache a
	// set that a CRUD has invalidated in the meantime — otherwise the stale set
	// would be written back as "loaded" and the invalidation lost, leaving a
	// disabled rule firing until some unrelated edit happened to reload it.
	generation uint64
	seen       map[string]bool
	seenSeq    []string                   // insertion order, for eviction
	buffers    map[string]*coalesceBuffer // by rule id

	// inflight counts the detached sends dispatch has started but not finished.
	// flush waits on it so a shutdown drains them instead of cutting them off:
	// the outbox cursor has already moved past those events, so a send killed
	// mid-flight is a notification LOST, not delayed.
	inflight sync.WaitGroup
}

// coalesceBuffer collapses a burst of one rule's matches into a single push
// (D57). The first match starts the timer and remembers the event; later
// matches within the window bump the count and swap in the newest event, so
// the notification describes the most recent change and says how many others
// came with it.
//
// It keeps the EVENT, not rendered text: rendering happens at send time, so a
// template's {{metric.…}} counts and the rule's conditions are judged against
// the world as it is when the push goes out — not as it was when the window
// opened, up to 24 hours earlier.
type coalesceBuffer struct {
	rule    Rule
	entry   audit.Entry
	changes []audit.Change
	count   int
	timer   *time.Timer
}

func newListener(svc *Service) *listener {
	return &listener{
		svc:     svc,
		seen:    make(map[string]bool, seenCapacity),
		buffers: map[string]*coalesceBuffer{},
	}
}

// InvalidateRules drops the cached rule set. Called on every rule CRUD so the
// listener never reads the database per event, yet never runs on stale config.
func (l *listener) InvalidateRules() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.loaded = false
	l.rules = nil
	l.generation++
}

// enabledRules returns the cached enabled rules, loading them on first use. ok is
// false only when the LOAD FAILED — which is deliberately distinguishable from a
// household that has no rules, because the two call for opposite behaviour when
// a pending send has to decide whether its rule still exists (see currentRule).
//
// The load runs OFF the lock (it is a database read, and holding the mutex
// across it would block every other event), which is exactly why the generation
// stamp exists: an InvalidateRules landing while the query is in flight must not
// be overwritten by the set the query started before it.
func (l *listener) enabledRules(ctx context.Context) (rules []Rule, ok bool) {
	l.mu.Lock()
	if l.loaded {
		defer l.mu.Unlock()
		return l.rules, true
	}
	gen := l.generation
	l.mu.Unlock()

	loaded, err := l.svc.store.EnabledRules(ctx)
	if err != nil {
		l.svc.logger.Error("admin: load trigger rules", "err", err)
		return nil, false
	}

	// Use this set for the event in hand either way — it is the freshest thing
	// that exists, and dropping the notification would be worse than sending one
	// from a set that went stale a millisecond ago.
	l.cacheRules(gen, loaded)
	return loaded, true
}

// cacheRules stores a freshly loaded rule set, unless a CRUD invalidated the
// cache while the load was in flight. Reports whether the set was cached.
//
// Split out from enabledRules so the interleaving it guards is testable without
// having to win a race: the whole bug it fixes is that this step used to store
// unconditionally, which threw away any invalidation that landed during the load.
func (l *listener) cacheRules(gen uint64, loaded []Rule) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.generation != gen {
		return false
	}
	l.rules, l.loaded = loaded, true
	return true
}

// currentRule re-resolves a rule that has been sitting in a coalescing window
// against the CURRENT configuration, and reports whether it may still be sent.
//
// A window can stay open for up to 24 hours, so "the rule as it was when the
// window opened" is not good enough: an admin who deletes or disables a noisy
// rule must not get one more push from it a minute later, and one who widened
// the audience meanwhile should get the widened one. The rule is looked up in
// the enabled set, so a deleted rule and a disabled rule are both simply absent.
//
// A failed load is NOT treated as "deleted": dropping every pending notification
// because of a transient database error would be a worse failure than sending
// one from a stale rule, so the caller's copy is used instead.
func (l *listener) currentRule(ctx context.Context, want Rule) (Rule, bool) {
	rules, ok := l.enabledRules(ctx)
	if !ok {
		return want, true
	}
	for _, r := range rules {
		if r.ID == want.ID {
			return r, true
		}
	}
	return Rule{}, false
}

// NeedsChanges reports whether any matching rule's template references a
// {{change.…}} token — the tailer only pays for the diff query when it does.
func (l *listener) NeedsChanges(e audit.Entry) bool {
	rules, _ := l.enabledRules(context.Background())
	for _, r := range rules {
		if !ruleMatches(r, e) {
			continue
		}
		if r.TitleTemplate != nil && UsesChangeTokens(*r.TitleTemplate) {
			return true
		}
		if r.BodyTemplate != nil && UsesChangeTokens(*r.BodyTemplate) {
			return true
		}
	}
	return false
}

// OnEvent handles one committed audit event.
func (l *listener) OnEvent(ctx context.Context, e audit.Entry, changes []audit.Change) error {
	if l.alreadySeen(e.ID) {
		return nil // at-least-once redelivery: the push already happened
	}

	// Never notify about the notifier's own configuration changes: an admin who
	// saves a rule does not need a push telling them they saved a rule, and a
	// rule matching "admin." would otherwise be able to notify about itself.
	if e.Module == audit.ModuleAdmin {
		return nil
	}

	rules, ok := l.enabledRules(ctx)
	if !ok {
		// A FAILED LOAD is not "no rules match". Treating the two the same would
		// skip every trigger for this event and say nothing about it — and the
		// dedupe mark taken above would then stop a re-scan from ever retrying,
		// turning a transient database error into a permanently missing
		// notification. So drop the mark and report the failure, which is what
		// makes the tailer log it (audit.Notifier.deliver). currentRule makes the
		// mirror-image choice for the same reason: neither path may silently lose
		// a notification to a read that happened to fail.
		l.forget(e.ID)
		return fmt.Errorf("admin: trigger rules unavailable, event %s not evaluated", e.ID)
	}
	for _, r := range rules {
		if !ruleMatches(r, e) {
			continue
		}
		if err := l.handleMatch(ctx, r, e, changes); err != nil {
			l.svc.logger.Warn("admin: trigger rule failed", "err", err, "rule", r.ID, "event", e.ID)
		}
	}
	return nil
}

// alreadySeen records an event id and reports whether it had been handled
// before. Eviction is FIFO.
//
// This set is PROCESS-LOCAL, so it cannot absorb a redelivery that spans a
// restart — and a crash is exactly when the tailer redelivers. The tailer keeps
// that window down to the single in-flight event by advancing its cursor per
// event (see audit.Notifier.scanOnce); closing it entirely would require
// committing a push together with a database write, which is not possible. What
// this set does cover is the same-process case: a cursor save that failed and
// left the batch to be re-scanned on the next poll.
func (l *listener) alreadySeen(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen[id] {
		return true
	}
	l.seen[id] = true
	l.seenSeq = append(l.seenSeq, id)
	if len(l.seenSeq) > seenCapacity {
		oldest := l.seenSeq[0]
		l.seenSeq = l.seenSeq[1:]
		delete(l.seen, oldest)
	}
	return false
}

// forget removes an id from the dedupe set, so an event that was marked seen but
// could NOT actually be evaluated is retried on redelivery instead of skipped.
// Only OnEvent's rule-load failure needs it; a handled event stays marked.
func (l *listener) forget(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.seen[id] {
		return
	}
	delete(l.seen, id)
	for i, v := range l.seenSeq {
		if v == id {
			l.seenSeq = append(l.seenSeq[:i], l.seenSeq[i+1:]...)
			break
		}
	}
}

// handleMatch either sends the notification now or folds the event into the
// rule's coalescing window. Rendering happens in send, not here.
func (l *listener) handleMatch(ctx context.Context, r Rule, e audit.Entry, changes []audit.Change) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	window := time.Duration(r.CoalesceWindowSeconds) * time.Second
	if window <= 0 {
		// coalesce_window_seconds = 0 means "notify about every single event".
		//
		// Detached from the tailer's context for the same reason the coalesced
		// path is: shutdown cancels that context BEFORE the flush runs, and a
		// send killed there is a notification lost — the cursor has already moved
		// past its event. flush waits for it through l.inflight instead.
		l.dispatch(context.WithoutCancel(ctx), r, e, changes, 1)
		return nil
	}

	if buf, ok := l.buffers[r.ID]; ok {
		buf.count++
		// Describe the NEWEST event: "kdo naposledy co udělal" is more useful
		// than the first thing that happened in the window. The actor moves with
		// it, so exclude_actor keeps filtering the person the text is about.
		buf.entry, buf.changes = e, changes
		return nil
	}

	buf := &coalesceBuffer{rule: r, entry: e, changes: changes, count: 1}
	buf.timer = time.AfterFunc(window, func() {
		// Dispatch UNDER the lock, not after releasing it — see dispatch. A timer
		// that has already fired cannot be stopped, so this is the one path that
		// can still start a send while flush is running, and holding the lock
		// across the hand-off is what orders the two.
		l.mu.Lock()
		defer l.mu.Unlock()
		pending := l.buffers[r.ID]
		delete(l.buffers, r.ID)
		if pending == nil {
			return
		}
		// Detach from the tailer's context: the flush happens after that call
		// returned, and a cancelled request context must not kill the send.
		l.dispatch(context.WithoutCancel(ctx), pending.rule, pending.entry, pending.changes, pending.count)
	})
	l.buffers[r.ID] = buf
	return nil
}

// dispatch resolves the audience and sends, off the tailer's goroutine so a slow
// push service never delays the outbox. Every dispatch is counted in l.inflight
// so the shutdown flush can wait for it.
//
// CALLERS MUST HOLD l.mu. The Add below has to be ordered against flush's drain,
// which runs under the same lock, and a coalescing timer is the reason: it fires
// on its OWN goroutine, so joining the tailer does not bound it, and Stop cannot
// recall one that has already fired. With the Add under the lock the two possible
// interleavings are both correct — the timer registers its send before flush
// looks (so flush waits for it), or flush drains the buffer first (so the timer
// finds nothing pending and never starts one). Adding after unlocking left a
// window where flush saw a zero counter, returned, and let the process exit with
// that send unstarted: the notification is lost, because the outbox cursor has
// already moved past its events. The same window is what could make Add race
// Wait and trip the WaitGroup's own misuse check.
//
// Starting the goroutine under the lock is safe: it blocks on l.mu only inside
// send, which is a different goroutine and simply waits for the caller to unlock.
func (l *listener) dispatch(ctx context.Context, r Rule, e audit.Entry, changes []audit.Change, count int) {
	l.inflight.Add(1)
	go func() {
		defer l.inflight.Done()
		l.send(ctx, r, e, changes, count)
	}()
}

// send is the synchronous half of dispatch. Flush needs to WAIT for a send, so
// the work cannot live inside the goroutine that dispatch starts.
func (l *listener) send(ctx context.Context, r Rule, e audit.Entry, changes []audit.Change, count int) {
	// The rule may have been edited, disabled or deleted while this notification
	// sat in its coalescing window — up to 24 hours of it. Re-resolve before
	// sending so a rule an admin has just turned off cannot get one last word in.
	r, ok := l.currentRule(ctx, r)
	if !ok {
		return
	}
	// The active window and the conditions are judged HERE, at send time: "jen
	// večer" means when the push would arrive, and "jen když něco zbývá" means
	// against the counts as they are now — not as they were when the coalescing
	// window opened. Both skips log at Info (like the schedule path) — a drop
	// here may be an entire coalesced batch, and "why didn't my rule fire" has
	// to be answerable from the logs.
	now := l.svc.now().In(l.svc.location)
	if !ruleActiveAt(r, now) {
		l.svc.logger.Info("admin: trigger skipped, outside active window", "rule", r.ID, "count", count)
		return
	}
	// The render context exists BEFORE the condition check so condition values
	// seed it: a key both gated on and printed costs one read, and the text can
	// never contradict the gate that let it through.
	rc := RenderContext{Now: now, Event: &e, Changes: changes,
		Metrics: map[string]int{}, Lists: map[string]ListValue{}}
	if !l.svc.conditionsSatisfied(ctx, &rc, r.Conditions, "",
		derefOr(r.TitleTemplate, ""), derefOr(r.BodyTemplate, ""), "rule "+r.ID) {
		l.svc.logger.Info("admin: trigger skipped, conditions not met", "rule", r.ID, "count", count)
		return
	}
	title, body := l.render(ctx, r, &rc)
	if count > 1 {
		body = withMoreSuffix(body, count-1)
	}
	recipients, err := l.svc.pushStore.ResolveAudience(ctx, r.Audience)
	if err != nil {
		l.svc.logger.Error("admin: resolve trigger audience", "err", err, "rule", r.ID)
		return
	}
	if r.ExcludeActor && e.ActorUser != "" {
		recipients = withoutUser(recipients, e.ActorUser)
	}
	if len(recipients) == 0 {
		return
	}
	l.svc.sender.Send(ctx, push.SortedUserIDs(recipients), push.Envelope{
		// The envelope carries the ORIGINATING module so a click lands where
		// the change happened, not on a generic admin screen.
		Module:   e.Module,
		Type:     "trigger",
		Title:    title,
		Body:     body,
		URL:      inAppURL(e),
		Category: push.CategoryTriggers,
		Kind:     push.KindTrigger,
		RuleID:   r.ID,
	})
}

// ruleActiveAt applies a rule's wall-clock window. "HH:MM" strings compare
// correctly as strings, and from > to means the window wraps midnight
// (22:00–06:00 covers late evening AND early morning).
func ruleActiveAt(r Rule, now time.Time) bool {
	if r.ActiveFromLocal == nil || r.ActiveToLocal == nil {
		return true
	}
	from, to := *r.ActiveFromLocal, *r.ActiveToLocal
	if from == "" || to == "" {
		return true
	}
	cur := now.Format("15:04")
	if from < to {
		return cur >= from && cur < to
	}
	return cur >= from || cur < to
}

// flush sends every coalescing window still open, and waits for them AND for any
// send already in flight.
//
// It exists because the outbox cursor has ALREADY advanced past the events in
// those windows: they will never be redelivered, so a timer that dies with the
// process does not delay a notification, it loses it. With a 60-second default
// window and a deploy-on-merge pipeline, that is a routine occurrence rather
// than an edge case. The same is true of a zero-window rule's send that happened
// to be mid-flight when the signal arrived, which is why the wait covers
// l.inflight rather than just the buffers drained here.
//
// The caller must have JOINED the outbox tailer (audit.Notifier.Wait), not
// merely cancelled it, so no new dispatch can begin while this runs — which is
// what makes waiting on l.inflight well-defined. A cancel alone would not: the
// tailer finishes handing out the batch it is in before it looks at ctx.Done,
// and a WaitGroup whose counter goes 0→1 concurrently with Wait either lets
// Wait through early or panics.
//
// Joining the tailer is NOT sufficient on its own, though: a coalescing timer
// runs on its own goroutine and can be mid-hand-off right now. The drain below
// therefore dispatches while STILL HOLDING l.mu, which every dispatch is
// required to do, so a timer either got there first (its send is already
// registered in l.inflight and is waited for) or arrives to find its buffer
// gone (and starts nothing). See dispatch.
func (l *listener) flush(ctx context.Context) {
	l.mu.Lock()
	for id, buf := range l.buffers {
		if buf.timer != nil {
			buf.timer.Stop()
		}
		delete(l.buffers, id)
		// ctx, not context.WithoutCancel(ctx): the caller's deadline is what keeps
		// the shutdown inside the container's grace period.
		l.dispatch(ctx, buf.rule, buf.entry, buf.changes, buf.count)
	}
	l.mu.Unlock()

	// Wait for those AND for anything already in flight — but never past the
	// caller's deadline. A zero-window send dispatched moments before shutdown
	// carries a detached context, so its only other bound is the push client's
	// 15s timeout; blocking on it would overrun the 10s the container allows
	// between SIGTERM and SIGKILL and cut the HTTP drain short as well.
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.inflight.Wait()
	}()
	select {
	case <-done:
	case <-ctx.Done():
		l.svc.logger.Warn("admin: flush deadline reached with notifications still in flight")
	}
}

// render produces the notification text for the event carried by rc, resolving
// any {{metric.…}} / {{list.…}} tokens the templates use — values a condition
// check already seeded into rc are reused, not re-read. Trigger tokens are
// household-scoped (validateRule refuses the personal ones), so one resolve
// with no recipient serves the whole audience.
func (l *listener) render(ctx context.Context, r Rule, rc *RenderContext) (title, body string) {
	l.svc.resolveInto(ctx, rc, derefOr(r.TitleTemplate, ""), derefOr(r.BodyTemplate, ""), "", "rule "+r.ID, anyScope)

	title = "Home"
	if r.TitleTemplate != nil && strings.TrimSpace(*r.TitleTemplate) != "" {
		title = Render(*r.TitleTemplate, *rc)
	} else if r.Name != "" {
		title = r.Name
	}

	// D55: an empty body template means "use the audit event's own Czech
	// summary" — which is already a human sentence, written by the module that
	// made the change. That is why a rule with no body at all still reads well.
	if r.BodyTemplate != nil && strings.TrimSpace(*r.BodyTemplate) != "" {
		body = Render(*r.BodyTemplate, *rc)
	} else {
		body = rc.Event.Summary
	}
	return title, body
}

// ruleMatches applies the action match and every configured filter.
//
// The action key is a BARE VERB and bare verbs are not unique across modules
// (labels.go keys its phrases by "module.action" for exactly that reason), so
// what qualifies a rule is FilterModule. The composer sets it from the module of
// the action the admin picked, and validateRule refuses a key/module pair no
// module actually emits — without that, the first module to reuse an existing
// verb would silently start firing every rule bound to it.
func ruleMatches(r Rule, e audit.Entry) bool {
	switch {
	case r.ActionKey != nil && *r.ActionKey != "":
		if e.Action != *r.ActionKey {
			return false
		}
	case r.ActionPrefix != nil && *r.ActionPrefix != "":
		if !prefixMatches(*r.ActionPrefix, e.Action) {
			return false
		}
	default:
		// A rule with neither is a broken row (the CHECK constraint forbids it);
		// matching everything would be the worst possible interpretation.
		return false
	}

	if r.FilterModule != nil && *r.FilterModule != "" && e.Module != *r.FilterModule {
		return false
	}
	if r.FilterEntityType != nil && *r.FilterEntityType != "" && e.EntityType != *r.FilterEntityType {
		return false
	}
	if r.FilterLevel != nil && *r.FilterLevel != "" && effectiveLevel(e.Level) != *r.FilterLevel {
		return false
	}
	return true
}

// prefixMatches matches on DOTTED SEGMENT BOUNDARIES, so "document." matches
// "document.create" but never "document_folder.create" — which would otherwise
// be a genuinely surprising false positive between two real action families.
func prefixMatches(prefix, action string) bool {
	p := strings.TrimSuffix(prefix, ".")
	if p == "" {
		return false
	}
	return action == p || strings.HasPrefix(action, p+".")
}

func effectiveLevel(level string) string {
	if level == "" {
		return audit.LevelInfo
	}
	return level
}

// withMoreSuffix appends the Czech "and N more" clause, with the three plural
// forms (1 / 2–4 / 5+) the rest of the UI uses.
func withMoreSuffix(body string, more int) string {
	if more <= 0 {
		return body
	}
	var word string
	switch {
	case more == 1:
		word = "další změna"
	case more < 5:
		word = "další změny"
	default:
		word = "dalších změn"
	}
	return fmt.Sprintf("%s (a %d %s)", strings.TrimRight(body, " "), more, word)
}

func withoutUser(ids []string, exclude string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != exclude {
			out = append(out, id)
		}
	}
	return out
}

// inAppURL maps an audited entity to the SPA route a click should open. A
// notification that lands on the right screen is the difference between useful
// and annoying; anything unmapped falls back to the dashboard.
func inAppURL(e audit.Entry) string {
	switch e.Module {
	case audit.ModuleTodo:
		return "/ukoly"
	case audit.ModuleEvents:
		return "/okno"
	case audit.ModuleNotes:
		return "/poznamky"
	case audit.ModuleDocuments:
		if e.EntityType == "document" && e.EntityID != "" {
			// Documents have a permanent id-based link (D42), so a click can open
			// the exact file rather than the folder tree.
			return "/d/" + e.EntityID
		}
		return "/dokumenty"
	case audit.ModuleFinance:
		return "/finance"
	default:
		return "/"
	}
}
