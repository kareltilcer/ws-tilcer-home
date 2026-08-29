package admin_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/admin"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/scheduler"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// ---- harness ----

// fakeSender records what would have gone out. The real channel is tested in
// platform/push; here the question is always "what did the admin module decide
// to send, to whom?".
type fakeSender struct {
	mu    sync.Mutex
	sent  []sentEnvelope
	gate  chan struct{} // when non-nil, Send blocks on it
	entry chan struct{} // receives once per Send that reached the gate
}

type sentEnvelope struct {
	recipients []string
	env        push.Envelope
}

// gateSends holds every subsequent Send until release is called. entered fires
// once a Send has actually reached the gate, so a test can synchronise on "a
// send is in flight" instead of sleeping for it.
func (f *fakeSender) gateSends() (entered <-chan struct{}, release func()) {
	f.mu.Lock()
	f.gate = make(chan struct{})
	f.entry = make(chan struct{}, 8)
	gate, entry := f.gate, f.entry
	f.mu.Unlock()
	return entry, func() {
		f.mu.Lock()
		f.gate, f.entry = nil, nil
		f.mu.Unlock()
		close(gate)
	}
}

func (f *fakeSender) Send(_ context.Context, recipients []string, e push.Envelope) []push.DeliveryResult {
	f.mu.Lock()
	gate, entry := f.gate, f.entry
	f.mu.Unlock()
	if gate != nil {
		select {
		case entry <- struct{}{}:
		default:
		}
		<-gate
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentEnvelope{recipients: append([]string(nil), recipients...), env: e})
	out := make([]push.DeliveryResult, 0, len(recipients))
	for _, r := range recipients {
		out = append(out, push.DeliveryResult{
			UserID: r, SubscriptionID: "sub-" + r, Endpoint: "https://push.example/" + r,
			Status: push.StatusSent, Kind: e.Kind, Category: e.Category, RuleID: e.RuleID,
		})
	}
	return out
}

func (f *fakeSender) VAPIDPublicKey() string { return "test-public-key" }
func (f *fakeSender) Enabled() bool          { return true }

func (f *fakeSender) all() []sentEnvelope {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentEnvelope(nil), f.sent...)
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// stubMetrics publishes a fixed catalog so template validation and summary
// rendering can be exercised without wiring real feature modules.
type stubMetrics struct {
	values  map[string]int
	perUser map[string]map[string]int
	fail    map[string]bool
}

func (s *stubMetrics) Descriptors() []metrics.Descriptor {
	return []metrics.Descriptor{
		{Key: "todo.pravedelam_count", Label: "Úkoly v Právě dělám", Unit: "úkolů", Scope: metrics.ScopeHousehold},
		{Key: "todo.done_today", Label: "Hotovo dnes", Unit: "úkolů", Scope: metrics.ScopeHousehold},
		{Key: "events.pripominky_today", Label: "Připomínky na dnešek", Unit: "připomínek", Scope: metrics.ScopeHousehold},
		{Key: "notes.pinned_count", Label: "Připnuté poznámky", Unit: "poznámek", Scope: metrics.ScopePersonal},
	}
}

func (s *stubMetrics) Value(_ context.Context, userID, key string, _ time.Time) (int, error) {
	if s.fail[key] {
		return 0, context.DeadlineExceeded
	}
	if byUser, ok := s.perUser[userID]; ok {
		if v, ok := byUser[key]; ok {
			return v, nil
		}
	}
	return s.values[key], nil
}

// stubLists publishes a fixed list catalog beside the metric one, so the "which
// ones?" half of a summary can be exercised without wiring real feature modules.
type stubLists struct {
	items   map[string][]string
	perUser map[string]map[string][]string
	fail    map[string]bool
	// reads counts resolves per key: a household list read once per recipient is
	// correct output for wasteful work, so only a counter can catch it.
	reads map[string]int
}

func (s *stubLists) Descriptors() []lists.Descriptor {
	return []lists.Descriptor{
		{Key: "events.pripominky_today", Label: "Připomínky na dnešek", Empty: "žádné připomínky", Scope: lists.ScopeHousehold},
		{Key: "todo.pravedelam_count", Label: "Úkoly v Právě dělám", Empty: "nic rozdělaného", Scope: lists.ScopeHousehold},
		{Key: "notes.pinned_count", Label: "Připnuté poznámky", Empty: "nic připnutého", Scope: lists.ScopePersonal},
	}
}

func (s *stubLists) Items(_ context.Context, userID, key string, _ time.Time) ([]string, error) {
	s.reads[key]++
	if s.fail[key] {
		return nil, context.DeadlineExceeded
	}
	if byUser, ok := s.perUser[userID]; ok {
		if v, ok := byUser[key]; ok {
			return v, nil
		}
	}
	return s.items[key], nil
}

type fixture struct {
	t         *testing.T
	db        *sql.DB
	svc       *admin.Service
	sender    *fakeSender
	pushStore *push.Store
	metrics   *stubMetrics
	lists     *stubLists
	now       time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testsupport.NewDB(t)
	sender := &fakeSender{}
	pushStore := push.NewStore(db)
	stub := &stubMetrics{values: map[string]int{}, perUser: map[string]map[string]int{}, fail: map[string]bool{}}
	stubL := &stubLists{
		items: map[string][]string{}, perUser: map[string]map[string][]string{},
		fail: map[string]bool{}, reads: map[string]int{},
	}

	reg := metrics.NewRegistry()
	if err := reg.Register(stub); err != nil {
		t.Fatalf("register metrics: %v", err)
	}
	listReg := lists.NewRegistry()
	if err := listReg.Register(stubL); err != nil {
		t.Fatalf("register lists: %v", err)
	}

	loc, err := time.LoadLocation("Europe/Prague")
	if err != nil {
		t.Fatalf("load tz: %v", err)
	}
	f := &fixture{
		t: t, db: db, sender: sender, pushStore: pushStore, metrics: stub, lists: stubL,
		now: time.Date(2026, time.September, 15, 8, 0, 0, 0, loc),
	}
	f.svc = admin.NewService(db, audit.NewSink(), admin.Options{
		Sender:    sender,
		PushStore: pushStore,
		Metrics:   reg,
		Lists:     listReg,
		Actions: []registry.Action{
			{Key: "card.move", Module: "todo"},
			{Key: "card.create", Module: "todo"},
			{Key: "event.update", Module: "events"},
			{Key: "reminder.complete", Module: "events"},
			{Key: "document.create", Module: "documents"},
			{Key: "document_folder.create", Module: "documents"},
			{Key: "login", Module: "platform"},
			// The admin module's own actions are in the real catalog too — a rule
			// CAN be bound to them, it just never fires (see the listener).
			{Key: "rule.create", Module: "admin"},
		},
		Location:        loc,
		DefaultCoalesce: 60 * time.Second,
		Logger:          slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now:             func() time.Time { return f.now },
	})
	return f
}

func adminCtx() context.Context { return testsupport.CtxUser("u-admin", "admin") }

// member registers a household member (a session row) and optionally a device.
func (f *fixture) member(userID, name string, roles []string, withDevice bool) {
	f.t.Helper()
	rolesJSON, _ := json.Marshal(roles)
	ts := f.now.UTC().Format(time.RFC3339Nano)
	if _, err := f.db.Exec(
		`INSERT INTO sessions (id, user_id, token_hash, email, display_name, roles, roles_refreshed_at,
		                       created_at, last_seen_at, expires_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"sess-"+userID, userID, "hash-"+userID, userID+"@tilcer.cz", name, string(rolesJSON),
		ts, ts, ts, f.now.Add(24*time.Hour).UTC().Format(time.RFC3339Nano)); err != nil {
		f.t.Fatalf("seed member %s: %v", userID, err)
	}
	if !withDevice {
		return
	}
	if _, err := f.db.Exec(
		`INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth, created_at, last_seen_at)
		 VALUES (?,?,?,?,?,?,?)`,
		"sub-"+userID, userID, "https://push.example/"+userID, "p256", "auth", ts, ts); err != nil {
		f.t.Fatalf("seed device %s: %v", userID, err)
	}
}

func (f *fixture) rule(t *testing.T, in admin.RuleCreate) *admin.Rule {
	t.Helper()
	r, err := f.svc.CreateRule(adminCtx(), in)
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	return r
}

// entry builds a committed audit event as the outbox tailer would deliver it.
func entry(id, module, action string) audit.Entry {
	return audit.Entry{
		ID: id, TS: time.Now(), ActorUser: "u-editor", ActorType: "user", ActorLabel: "Karel",
		Module: module, Action: action, EntityType: "card", EntityID: "card-1",
		Summary: "Karel přesunul kartu „Vynést koš“ do Hotovo", Level: audit.LevelInfo,
	}
}

func str(s string) *string { return &s }
func num(n int) *int       { return &n }
func yes() *bool           { b := true; return &b }
func no() *bool            { b := false; return &b }

func schedDaily() scheduler.DaysSpec { return scheduler.DaysSpec{Preset: scheduler.PresetDaily} }

func schedDayOfMonth(n int) scheduler.DaysSpec { return scheduler.DaysSpec{DayOfMonth: n} }

// scheduledDue is what the platform ticker hands FireSchedule when a slot is due.
func scheduledDue(id string) scheduler.Due {
	return scheduler.Due{Schedule: scheduler.Schedule{ID: id}, LocalDate: "2026-09-15"}
}

func statusOf(t *testing.T, err error) int {
	t.Helper()
	var ae *httpx.APIError
	if e, ok := err.(*httpx.APIError); ok {
		ae = e
		return ae.Status
	}
	t.Fatalf("expected *httpx.APIError, got %v", err)
	return 0
}

// waitForSends blocks until the sender has at least n envelopes (coalescing is
// timer-driven), or fails.
func (f *fixture) waitForSends(n int) {
	f.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f.sender.count() >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.t.Fatalf("timed out waiting for %d sends, got %d", n, f.sender.count())
}

// ---- trigger matching ----

func TestRuleMatchingByActionKey(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.rule(t, admin.RuleCreate{
		Name: "Hotové úkoly", ActionKey: str("card.move"),
		Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(0),
	})
	l := f.svc.Listener()

	if err := l.OnEvent(context.Background(), entry("e1", "todo", "card.move"), nil); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	f.waitForSends(1)

	// A different action must not fire the rule.
	if err := l.OnEvent(context.Background(), entry("e2", "todo", "card.create"), nil); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	if got := f.sender.count(); got != 1 {
		t.Errorf("sent %d notifications, want 1 (card.create must not match card.move)", got)
	}
}

// A prefix must match on DOTTED SEGMENT BOUNDARIES: "document." covers
// document.create but never document_folder.create, which is a real action
// family of its own.
func TestRuleMatchingByActionPrefixRespectsSegmentBoundaries(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.rule(t, admin.RuleCreate{
		Name: "Dokumenty", ActionPrefix: str("document."),
		Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(0),
	})
	l := f.svc.Listener()

	_ = l.OnEvent(context.Background(), entry("e1", "documents", "document.create"), nil)
	f.waitForSends(1)

	_ = l.OnEvent(context.Background(), entry("e2", "documents", "document_folder.create"), nil)
	time.Sleep(80 * time.Millisecond)

	if got := f.sender.count(); got != 1 {
		t.Errorf("sent %d, want 1 — document_folder.create must not match the document. prefix", got)
	}
}

func TestRuleFiltersAreAnded(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.rule(t, admin.RuleCreate{
		Name: "Jen todo karty", ActionPrefix: str("card."),
		FilterModule: str("todo"), FilterEntityType: str("card"),
		Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(0),
	})
	l := f.svc.Listener()

	// Right module + entity type: fires.
	_ = l.OnEvent(context.Background(), entry("e1", "todo", "card.move"), nil)
	f.waitForSends(1)

	// Same action, wrong module: does not.
	other := entry("e2", "events", "card.move")
	_ = l.OnEvent(context.Background(), other, nil)

	// Right module, wrong entity type: does not.
	wrongEntity := entry("e3", "todo", "card.move")
	wrongEntity.EntityType = "column"
	_ = l.OnEvent(context.Background(), wrongEntity, nil)

	time.Sleep(80 * time.Millisecond)
	if got := f.sender.count(); got != 1 {
		t.Errorf("sent %d, want 1 — every set filter must have to match", got)
	}
}

// A rule with no body template falls back to the audit event's own Czech
// summary, which is why a rule saved with nothing but a name still reads well.
func TestEmptyBodyTemplateUsesTheAuditSummary(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.rule(t, admin.RuleCreate{
		Name: "Cokoliv", ActionKey: str("card.move"),
		Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(0),
	})

	e := entry("e1", "todo", "card.move")
	_ = f.svc.Listener().OnEvent(context.Background(), e, nil)
	f.waitForSends(1)

	got := f.sender.all()[0].env
	if got.Body != e.Summary {
		t.Errorf("body = %q, want the audit summary %q", got.Body, e.Summary)
	}
	if got.Title != "Cokoliv" {
		t.Errorf("title = %q, want the rule name as the fallback", got.Title)
	}
}

func TestTriggerRendersEventTokens(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.rule(t, admin.RuleCreate{
		Name:          "S tokeny",
		ActionKey:     str("card.move"),
		TitleTemplate: str("Úkoly"),
		BodyTemplate:  str("{{event.actor_label}} → {{event.action}} ({{change.title.new}})"),
		Audience:      push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(0),
	})

	_ = f.svc.Listener().OnEvent(context.Background(), entry("e1", "todo", "card.move"),
		[]audit.Change{{Field: "title", Old: audit.Ptr("Staré"), New: audit.Ptr("Nové")}})
	f.waitForSends(1)

	if got := f.sender.all()[0].env.Body; got != "Karel → card.move (Nové)" {
		t.Errorf("body = %q, want the tokens resolved", got)
	}
}

// An unresolvable token renders as a placeholder — never as a raw {{…}}, and
// never as a failure to send.
func TestUnresolvableTokenRendersAsPlaceholder(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.rule(t, admin.RuleCreate{
		Name: "Chybějící pole", ActionKey: str("card.move"),
		BodyTemplate: str("Nová hodnota: {{change.neexistuje.new}}"),
		Audience:     push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(0),
	})

	_ = f.svc.Listener().OnEvent(context.Background(), entry("e1", "todo", "card.move"), nil)
	f.waitForSends(1)

	body := f.sender.all()[0].env.Body
	if strings.Contains(body, "{{") {
		t.Errorf("body leaked a raw token: %q", body)
	}
	if !strings.Contains(body, admin.Placeholder) {
		t.Errorf("body = %q, want the placeholder for the missing field", body)
	}
}

// ---- coalescing ----

func TestCoalescingCollapsesABurstIntoOnePush(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.rule(t, admin.RuleCreate{
		Name: "Sloučené", ActionKey: str("card.move"),
		Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(1),
	})
	l := f.svc.Listener()

	for i, id := range []string{"e1", "e2", "e3", "e4"} {
		e := entry(id, "todo", "card.move")
		e.Summary = "Změna " + string(rune('A'+i))
		_ = l.OnEvent(context.Background(), e, nil)
	}

	f.waitForSends(1)
	time.Sleep(200 * time.Millisecond) // catch any stragglers

	sent := f.sender.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d notifications for a burst of 4, want 1", len(sent))
	}
	body := sent[0].env.Body
	// The newest event is described, with a count of the rest.
	if !strings.Contains(body, "Změna D") {
		t.Errorf("body = %q, want it to describe the NEWEST event", body)
	}
	if !strings.Contains(body, "3") {
		t.Errorf("body = %q, want a count of the other 3 changes", body)
	}
}

func TestCoalesceZeroSendsEveryEvent(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.rule(t, admin.RuleCreate{
		Name: "Vše", ActionKey: str("card.move"),
		Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(0),
	})
	l := f.svc.Listener()

	for _, id := range []string{"e1", "e2", "e3"} {
		_ = l.OnEvent(context.Background(), entry(id, "todo", "card.move"), nil)
	}
	f.waitForSends(3)

	if got := f.sender.count(); got != 3 {
		t.Errorf("sent %d, want 3 (coalesce=0 means every event)", got)
	}
}

// ---- idempotency ----

// The outbox is at-least-once, so the same event id can arrive twice. The
// listener must turn the second arrival into a no-op.
func TestListenerIsIdempotentOnEventID(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.rule(t, admin.RuleCreate{
		Name: "Jednou", ActionKey: str("card.move"),
		Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(0),
	})
	l := f.svc.Listener()

	e := entry("same-id", "todo", "card.move")
	_ = l.OnEvent(context.Background(), e, nil)
	_ = l.OnEvent(context.Background(), e, nil)
	_ = l.OnEvent(context.Background(), e, nil)
	f.waitForSends(1)
	time.Sleep(100 * time.Millisecond)

	if got := f.sender.count(); got != 1 {
		t.Errorf("sent %d for three deliveries of one event, want 1", got)
	}
}

// The module must never notify about its own configuration changes.
func TestListenerIgnoresItsOwnModule(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.rule(t, admin.RuleCreate{
		Name: "Vše", ActionPrefix: str("rule."),
		Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(0),
	})

	_ = f.svc.Listener().OnEvent(context.Background(), entry("e1", "admin", "rule.create"), nil)
	time.Sleep(80 * time.Millisecond)

	if got := f.sender.count(); got != 0 {
		t.Errorf("sent %d notifications about the admin module's own changes, want 0", got)
	}
}

// ---- audience + exclude_actor ----

func TestAudienceResolutionAndExcludeActor(t *testing.T) {
	t.Run("default includes the actor", func(t *testing.T) {
		f := newFixture(t)
		f.member("u-editor", "Karel", []string{"editor"}, true)
		f.member("u-other", "Eva", []string{"editor"}, true)
		f.rule(t, admin.RuleCreate{
			Name: "Všem", ActionKey: str("card.move"),
			Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(0),
		})

		_ = f.svc.Listener().OnEvent(context.Background(), entry("e1", "todo", "card.move"), nil)
		f.waitForSends(1)

		got := f.sender.all()[0].recipients
		if len(got) != 2 {
			t.Errorf("recipients = %v, want both members (D66: the actor is included by default)", got)
		}
	})

	t.Run("exclude_actor drops the person who did it", func(t *testing.T) {
		f := newFixture(t)
		f.member("u-editor", "Karel", []string{"editor"}, true)
		f.member("u-other", "Eva", []string{"editor"}, true)
		f.rule(t, admin.RuleCreate{
			Name: "Bez původce", ActionKey: str("card.move"),
			Audience:              push.Audience{Scope: push.ScopeAll},
			CoalesceWindowSeconds: num(0), ExcludeActor: yes(),
		})

		_ = f.svc.Listener().OnEvent(context.Background(), entry("e1", "todo", "card.move"), nil)
		f.waitForSends(1)

		got := f.sender.all()[0].recipients
		if len(got) != 1 || got[0] != "u-other" {
			t.Errorf("recipients = %v, want only u-other (the actor u-editor is excluded)", got)
		}
	})
}

// ---- validation (422s the composer surfaces on a field) ----

func TestRuleValidation(t *testing.T) {
	f := newFixture(t)
	base := func() admin.RuleCreate {
		return admin.RuleCreate{Name: "R", ActionKey: str("card.move"), Audience: push.Audience{Scope: push.ScopeAll}}
	}

	cases := []struct {
		name   string
		mutate func(*admin.RuleCreate)
		want   string
	}{
		{"unknown action key", func(c *admin.RuleCreate) { c.ActionKey = str("card.teleport") }, "neznámá akce"},
		{"no action at all", func(c *admin.RuleCreate) { c.ActionKey = nil }, "právě jednu akci"},
		{"both key and prefix", func(c *admin.RuleCreate) { c.ActionPrefix = str("card.") }, "právě jednu akci"},
		{"prefix matching nothing", func(c *admin.RuleCreate) {
			c.ActionKey, c.ActionPrefix = nil, str("nonsense.")
		}, "žádná akce"},
		{"empty name", func(c *admin.RuleCreate) { c.Name = "  " }, "název"},
		{"empty role audience", func(c *admin.RuleCreate) {
			c.Audience = push.Audience{Scope: push.ScopeRoles}
		}, "komu"},
		{"empty user audience", func(c *admin.RuleCreate) {
			c.Audience = push.Audience{Scope: push.ScopeUsers}
		}, "komu"},
		{"unknown token", func(c *admin.RuleCreate) { c.BodyTemplate = str("{{event.nonsense}}") }, "neznámý údaj"},
		// Metric/list tokens are ALLOWED in triggers now — but only known,
		// household-scoped keys, and a list still never belongs in a title.
		{"unknown metric in a trigger", func(c *admin.RuleCreate) {
			c.BodyTemplate = str("{{metric.todo.nonsense}}")
		}, "neznámé číslo"},
		{"personal metric in a trigger", func(c *admin.RuleCreate) {
			c.BodyTemplate = str("{{metric.notes.pinned_count}}")
		}, "osobní údaj"},
		{"personal list in a trigger", func(c *admin.RuleCreate) {
			c.BodyTemplate = str("{{list.notes.pinned_count}}")
		}, "osobní údaj"},
		{"list token in a trigger title", func(c *admin.RuleCreate) {
			c.TitleTemplate = str("{{list.events.pripominky_today}}")
		}, "ne do nadpisu"},
		{"condition with unknown key", func(c *admin.RuleCreate) {
			c.Conditions = &admin.Conditions{Mode: "all", Items: []admin.Condition{{Key: "todo.nonsense", Op: "gt", Value: 0}}}
		}, "neznámý údaj v podmínce"},
		{"condition with unknown op", func(c *admin.RuleCreate) {
			c.Conditions = &admin.Conditions{Mode: "all", Items: []admin.Condition{{Key: "todo.done_today", Op: "between", Value: 0}}}
		}, "neznámé porovnání"},
		{"condition with bad mode", func(c *admin.RuleCreate) {
			c.Conditions = &admin.Conditions{Mode: "some", Items: []admin.Condition{{Key: "todo.done_today", Op: "gt", Value: 0}}}
		}, "režim"},
		{"condition with negative value", func(c *admin.RuleCreate) {
			c.Conditions = &admin.Conditions{Mode: "all", Items: []admin.Condition{{Key: "todo.done_today", Op: "gt", Value: -1}}}
		}, "záporná"},
		{"personal condition in a trigger", func(c *admin.RuleCreate) {
			c.Conditions = &admin.Conditions{Mode: "all", Items: []admin.Condition{{Key: "notes.pinned_count", Op: "gt", Value: 0}}}
		}, "osobní údaj"},
		{"half an active window", func(c *admin.RuleCreate) {
			c.ActiveFromLocal = str("08:00")
		}, "oba časy"},
		{"malformed active window", func(c *admin.RuleCreate) {
			c.ActiveFromLocal, c.ActiveToLocal = str("8am"), str("20:00")
		}, "HH:MM"},
		{"degenerate active window", func(c *admin.RuleCreate) {
			c.ActiveFromLocal, c.ActiveToLocal = str("08:00"), str("08:00")
		}, "stejný čas"},
		{"negative coalesce", func(c *admin.RuleCreate) { c.CoalesceWindowSeconds = num(-1) }, "záporné"},
		// The upper bound matters as much: seconds→time.Duration overflows int64
		// past ~9.2e9 and comes out NEGATIVE, which the listener reads as "no
		// window" and fires on every single event — the loudest possible reading
		// of a rule that asked to be the quietest.
		{"overflowing coalesce", func(c *admin.RuleCreate) {
			c.CoalesceWindowSeconds = num(10_000_000_000)
		}, "nejvýše 24 hodin"},
		{"coalesce past the cap", func(c *admin.RuleCreate) {
			c.CoalesceWindowSeconds = num(24*60*60 + 1)
		}, "nejvýše 24 hodin"},
		// filter_level has a CHECK constraint on the table. Unvalidated, a bad
		// value reached the INSERT and came back as a bare 500 — the one answer a
		// composer cannot put on a field.
		{"unknown filter level", func(c *admin.RuleCreate) { c.FilterLevel = str("debug") }, "neznámá úroveň"},
		// An action key is a bare verb; the module is the other half of its
		// identity. A pair no module emits is a rule that could never fire, so it
		// is refused rather than saved as a silent no-op.
		{"action key under the wrong module", func(c *admin.RuleCreate) {
			c.ActionKey, c.FilterModule = str("card.move"), str("notes")
		}, "neznámá akce"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base()
			tc.mutate(&in)
			_, err := f.svc.CreateRule(adminCtx(), in)
			if err == nil {
				t.Fatalf("expected a 422, got no error")
			}
			if got := statusOf(t, err); got != 422 {
				t.Errorf("status = %d, want 422", got)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), tc.want)
			}
		})
	}

	// The cap itself is legal — a bound that rejects its own boundary is a bug.
	t.Run("the coalesce cap is accepted", func(t *testing.T) {
		in := base()
		in.CoalesceWindowSeconds = num(24 * 60 * 60)
		if _, err := f.svc.CreateRule(adminCtx(), in); err != nil {
			t.Errorf("24h coalesce window was refused: %v", err)
		}
	})

	// The composer sends the module the admin picked the action from, so the
	// matching pair has to pass — and every level the audit spine actually writes.
	t.Run("the matching module and the real levels are accepted", func(t *testing.T) {
		in := base()
		in.FilterModule = str("todo")
		if _, err := f.svc.CreateRule(adminCtx(), in); err != nil {
			t.Errorf("todo + card.move was refused: %v", err)
		}
		for _, level := range []string{audit.LevelInfo, audit.LevelWarn, audit.LevelError} {
			in := base()
			in.FilterLevel = str(level)
			if _, err := f.svc.CreateRule(adminCtx(), in); err != nil {
				t.Errorf("filter_level %q was refused: %v", level, err)
			}
		}
	})
}

func TestScheduleValidation(t *testing.T) {
	f := newFixture(t)
	base := func() admin.ScheduleCreate {
		return admin.ScheduleCreate{
			Name:          "Ranní",
			Schedule:      admin.ScheduleSpec{TimeLocal: "08:00", Days: schedDaily()},
			Audience:      push.Audience{Scope: push.ScopeAll},
			TitleTemplate: "Dobré ráno", BodyTemplate: "Úkolů: {{metric.todo.pravedelam_count}}",
		}
	}

	t.Run("the worked example is accepted", func(t *testing.T) {
		if _, err := f.svc.CreateSchedule(adminCtx(), base()); err != nil {
			t.Fatalf("the PRD's 08:00 example must be expressible: %v", err)
		}
	})

	t.Run("day of month 31 is accepted, not capped at 28", func(t *testing.T) {
		in := base()
		in.Name = "Měsíční"
		in.Schedule.Days = schedDayOfMonth(31)
		if _, err := f.svc.CreateSchedule(adminCtx(), in); err != nil {
			t.Fatalf("D74: 1–31 must be accepted, got %v", err)
		}
	})

	cases := []struct {
		name   string
		mutate func(*admin.ScheduleCreate)
		want   string
	}{
		{"bad time", func(c *admin.ScheduleCreate) { c.Schedule.TimeLocal = "8:00" }, "HH:MM"},
		{"day 32", func(c *admin.ScheduleCreate) { c.Schedule.Days = schedDayOfMonth(32) }, "1–31"},
		{"unknown metric", func(c *admin.ScheduleCreate) { c.BodyTemplate = "{{metric.todo.nonexistent}}" }, "neznámé číslo"},
		{"unknown list", func(c *admin.ScheduleCreate) { c.BodyTemplate = "{{list.todo.nonexistent}}" }, "neznámý seznam"},
		{
			"a list in the title",
			func(c *admin.ScheduleCreate) { c.TitleTemplate = "Dnes: {{list.events.pripominky_today}}" },
			"ne do nadpisu",
		},
		{"event token in a summary", func(c *admin.ScheduleCreate) { c.BodyTemplate = "{{event.summary}}" }, "jen v pravidle"},
		{"empty body", func(c *admin.ScheduleCreate) { c.BodyTemplate = "  " }, "text souhrnu"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base()
			in.Name = tc.name
			tc.mutate(&in)
			_, err := f.svc.CreateSchedule(adminCtx(), in)
			if err == nil {
				t.Fatalf("expected a 422")
			}
			if got := statusOf(t, err); got != 422 {
				t.Errorf("status = %d, want 422", got)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), tc.want)
			}
		})
	}
}

// ---- broadcast ----

func TestBroadcastSendsAndAudits(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.member("u-eva", "Eva", []string{"editor"}, true)
	f.member("u-nikdo", "Nikdo", []string{"reader"}, false) // no device

	res, err := f.svc.Broadcast(adminCtx(), admin.BroadcastRequest{
		Title: "Vypnutá voda", Body: "Dnes od 9:00 ({{date}})",
		Audience: push.Audience{Scope: push.ScopeAll},
	})
	if err != nil {
		t.Fatalf("broadcast: %v", err)
	}
	if res.Recipients != 2 || res.Subscriptions != 2 {
		t.Errorf("result = %+v, want 2 recipients / 2 subscriptions (the member with no device is not counted)", res)
	}

	sent := f.sender.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d, want 1", len(sent))
	}
	if !strings.Contains(sent[0].env.Body, "15. 9. 2026") {
		t.Errorf("body = %q, want the {{date}} token resolved", sent[0].env.Body)
	}
	if sent[0].env.Category != push.CategoryBroadcast {
		t.Errorf("category = %q, want broadcast", sent[0].env.Category)
	}

	var action string
	if err := f.db.QueryRow(
		`SELECT action FROM audit_events WHERE module = 'admin' ORDER BY id DESC LIMIT 1`).Scan(&action); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if action != "broadcast.send" {
		t.Errorf("audit action = %q, want broadcast.send", action)
	}
}

// Composing to an audience nobody is in is a household state, not an error; an
// explicitly EMPTY selection is the mistake worth a 422.
func TestBroadcastEmptyAudienceRules(t *testing.T) {
	f := newFixture(t)

	t.Run("all with nobody subscribed is allowed", func(t *testing.T) {
		res, err := f.svc.Broadcast(adminCtx(), admin.BroadcastRequest{
			Title: "T", Body: "B", Audience: push.Audience{Scope: push.ScopeAll},
		})
		if err != nil {
			t.Fatalf("expected success with zero recipients, got %v", err)
		}
		if res.Recipients != 0 {
			t.Errorf("recipients = %d, want 0", res.Recipients)
		}
	})

	t.Run("an empty explicit selection is a 422", func(t *testing.T) {
		_, err := f.svc.Broadcast(adminCtx(), admin.BroadcastRequest{
			Title: "T", Body: "B", Audience: push.Audience{Scope: push.ScopeUsers},
		})
		if err == nil || statusOf(t, err) != 422 {
			t.Errorf("err = %v, want a 422", err)
		}
	})

	t.Run("empty title is a 422", func(t *testing.T) {
		_, err := f.svc.Broadcast(adminCtx(), admin.BroadcastRequest{
			Title: "  ", Body: "B", Audience: push.Audience{Scope: push.ScopeAll},
		})
		if err == nil || statusOf(t, err) != 422 {
			t.Errorf("err = %v, want a 422", err)
		}
	})
}

// ---- summaries ----

func TestScheduleFiresWithHouseholdMetrics(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.member("u-eva", "Eva", []string{"editor"}, true)
	f.metrics.values["todo.pravedelam_count"] = 4
	f.metrics.values["events.pripominky_today"] = 2

	sc, err := f.svc.CreateSchedule(adminCtx(), admin.ScheduleCreate{
		Name:          "Ranní přehled",
		Schedule:      admin.ScheduleSpec{TimeLocal: "08:00", Days: schedDaily()},
		Audience:      push.Audience{Scope: push.ScopeAll},
		TitleTemplate: "Dobré ráno",
		BodyTemplate:  "Právě dělám: {{metric.todo.pravedelam_count}} úkolů · Připomínky na dnešek: {{metric.events.pripominky_today}}",
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}

	f.svc.FireSchedule(context.Background(), scheduledDue(sc.ID))

	sent := f.sender.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d, want 1 — an all-household summary renders once for everyone", len(sent))
	}
	want := "Právě dělám: 4 úkolů · Připomínky na dnešek: 2"
	if sent[0].env.Body != want {
		t.Errorf("body = %q, want %q", sent[0].env.Body, want)
	}
	if len(sent[0].recipients) != 2 {
		t.Errorf("recipients = %v, want both members in one send", sent[0].recipients)
	}
	if sent[0].env.Category != push.CategorySummaries {
		t.Errorf("category = %q, want summaries", sent[0].env.Category)
	}
}

// A personal metric is what forces a render PER RECIPIENT (D60).
func TestScheduleWithPersonalMetricRendersPerRecipient(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.member("u-eva", "Eva", []string{"editor"}, true)
	f.metrics.perUser["u-karel"] = map[string]int{"notes.pinned_count": 3}
	f.metrics.perUser["u-eva"] = map[string]int{"notes.pinned_count": 7}

	sc, err := f.svc.CreateSchedule(adminCtx(), admin.ScheduleCreate{
		Name:          "Připnuté",
		Schedule:      admin.ScheduleSpec{TimeLocal: "20:00", Days: schedDaily()},
		Audience:      push.Audience{Scope: push.ScopeAll},
		TitleTemplate: "Večerní přehled", BodyTemplate: "Připnuto: {{metric.notes.pinned_count}}",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	f.svc.FireSchedule(context.Background(), scheduledDue(sc.ID))

	sent := f.sender.all()
	if len(sent) != 2 {
		t.Fatalf("sent %d envelopes, want one per recipient (the metric personalizes)", len(sent))
	}
	bodies := map[string]string{}
	for _, s := range sent {
		bodies[s.recipients[0]] = s.env.Body
	}
	if bodies["u-karel"] != "Připnuto: 3" || bodies["u-eva"] != "Připnuto: 7" {
		t.Errorf("per-recipient bodies = %v, want 3 for Karel and 7 for Eva", bodies)
	}
}

// One broken metric must not cost the household the other numbers.
func TestScheduleDegradesAFailingMetricToAPlaceholder(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.metrics.values["todo.pravedelam_count"] = 5
	f.metrics.fail["events.pripominky_today"] = true

	sc, err := f.svc.CreateSchedule(adminCtx(), admin.ScheduleCreate{
		Name:          "Ranní",
		Schedule:      admin.ScheduleSpec{TimeLocal: "08:00", Days: schedDaily()},
		Audience:      push.Audience{Scope: push.ScopeAll},
		TitleTemplate: "Ráno",
		BodyTemplate:  "Úkolů: {{metric.todo.pravedelam_count}} · Připomínek: {{metric.events.pripominky_today}}",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	f.svc.FireSchedule(context.Background(), scheduledDue(sc.ID))

	sent := f.sender.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d, want the summary to go out anyway", len(sent))
	}
	body := sent[0].env.Body
	if !strings.Contains(body, "Úkolů: 5") {
		t.Errorf("body = %q, want the working metric resolved", body)
	}
	if !strings.Contains(body, admin.Placeholder) {
		t.Errorf("body = %q, want the failing metric as a placeholder", body)
	}
}

// ---- summaries: module lists ----

// The point of a list token: a summary that says WHICH reminders are due today,
// not only how many.
func TestScheduleNamesTodaysReminders(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.metrics.values["events.pripominky_today"] = 2
	f.lists.items["events.pripominky_today"] = []string{"Vynést koš", "Zalít kytky"}

	sc, err := f.svc.CreateSchedule(adminCtx(), admin.ScheduleCreate{
		Name:          "Ranní přehled",
		Schedule:      admin.ScheduleSpec{TimeLocal: "08:00", Days: schedDaily()},
		Audience:      push.Audience{Scope: push.ScopeAll},
		TitleTemplate: "Dnes máš {{metric.events.pripominky_today}} připomínky",
		BodyTemplate:  "{{list.events.pripominky_today}}",
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}

	f.svc.FireSchedule(context.Background(), scheduledDue(sc.ID))

	sent := f.sender.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d, want 1 — a household list renders once for everyone", len(sent))
	}
	if want := "• Vynést koš\n• Zalít kytky"; sent[0].env.Body != want {
		t.Errorf("body = %q, want %q", sent[0].env.Body, want)
	}
	if sent[0].env.Title != "Dnes máš 2 připomínky" {
		t.Errorf("title = %q, want the metric beside the list", sent[0].env.Title)
	}
}

// An empty list is good news, not a failure — it must read as the module's own
// words rather than as the placeholder a broken resolver produces.
func TestEmptyListRendersTheModulesWordsAndAFailingOneThePlaceholder(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.lists.items["events.pripominky_today"] = nil
	f.lists.fail["todo.pravedelam_count"] = true

	sc, err := f.svc.CreateSchedule(adminCtx(), admin.ScheduleCreate{
		Name:          "Ranní",
		Schedule:      admin.ScheduleSpec{TimeLocal: "08:00", Days: schedDaily()},
		Audience:      push.Audience{Scope: push.ScopeAll},
		TitleTemplate: "Ráno",
		BodyTemplate:  "Dnes: {{list.events.pripominky_today}} · Rozdělané: {{list.todo.pravedelam_count}}",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	f.svc.FireSchedule(context.Background(), scheduledDue(sc.ID))

	sent := f.sender.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d, want the summary to go out anyway", len(sent))
	}
	want := "Dnes: žádné připomínky · Rozdělané: " + admin.Placeholder
	if sent[0].env.Body != want {
		t.Errorf("body = %q, want %q", sent[0].env.Body, want)
	}
}

// A long list is capped so it cannot eat the sentence the admin wrote around it.
func TestLongListIsCappedWithACount(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.lists.items["events.pripominky_today"] = []string{"A", "B", "C", "D", "E", "F", "G"}

	sc, err := f.svc.CreateSchedule(adminCtx(), admin.ScheduleCreate{
		Name:          "Ranní",
		Schedule:      admin.ScheduleSpec{TimeLocal: "08:00", Days: schedDaily()},
		Audience:      push.Audience{Scope: push.ScopeAll},
		TitleTemplate: "Ráno", BodyTemplate: "{{list.events.pripominky_today}}",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	f.svc.FireSchedule(context.Background(), scheduledDue(sc.ID))

	body := f.sender.all()[0].env.Body
	if want := "• A\n• B\n• C\n• D\n• E\n• …a další 2"; body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

// A personal LIST personalizes a summary exactly as a personal metric does.
func TestScheduleWithPersonalListRendersPerRecipient(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.member("u-eva", "Eva", []string{"editor"}, true)
	f.lists.perUser["u-karel"] = map[string][]string{"notes.pinned_count": {"Wi-Fi heslo"}}
	f.lists.perUser["u-eva"] = map[string][]string{"notes.pinned_count": {"Recept na bábovku"}}

	sc, err := f.svc.CreateSchedule(adminCtx(), admin.ScheduleCreate{
		Name:          "Připnuté",
		Schedule:      admin.ScheduleSpec{TimeLocal: "20:00", Days: schedDaily()},
		Audience:      push.Audience{Scope: push.ScopeAll},
		TitleTemplate: "Večer", BodyTemplate: "{{list.notes.pinned_count}}",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	f.svc.FireSchedule(context.Background(), scheduledDue(sc.ID))

	sent := f.sender.all()
	if len(sent) != 2 {
		t.Fatalf("sent %d envelopes, want one per recipient (the list personalizes)", len(sent))
	}
	bodies := map[string]string{}
	for _, s := range sent {
		bodies[s.recipients[0]] = s.env.Body
	}
	if bodies["u-karel"] != "• Wi-Fi heslo" || bodies["u-eva"] != "• Recept na bábovku" {
		t.Errorf("per-recipient bodies = %v, want each member's own pins", bodies)
	}
}

// One personal token must not make the HOUSEHOLD reads personal too. They are
// identical for everybody, and a list read is an event scan plus an expansion —
// paying for it once per recipient is invisible in the output, so it is pinned
// here instead.
func TestAPersonalizedSummaryReadsItsHouseholdListsOnce(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.member("u-eva", "Eva", []string{"editor"}, true)
	f.member("u-jana", "Jana", []string{"editor"}, true)
	f.lists.items["events.pripominky_today"] = []string{"Vynést koš"}
	f.lists.perUser["u-karel"] = map[string][]string{"notes.pinned_count": {"Wi-Fi heslo"}}

	sc, err := f.svc.CreateSchedule(adminCtx(), admin.ScheduleCreate{
		Name:          "Ranní",
		Schedule:      admin.ScheduleSpec{TimeLocal: "08:00", Days: schedDaily()},
		Audience:      push.Audience{Scope: push.ScopeAll},
		TitleTemplate: "Ráno",
		BodyTemplate:  "Dnes: {{list.events.pripominky_today}} · Připnuté: {{list.notes.pinned_count}}",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	f.svc.FireSchedule(context.Background(), scheduledDue(sc.ID))

	if n := len(f.sender.all()); n != 3 {
		t.Fatalf("sent %d, want one per recipient — the pinned notes personalize", n)
	}
	if got := f.lists.reads["events.pripominky_today"]; got != 1 {
		t.Errorf("household list read %d times, want 1 for all 3 recipients", got)
	}
	if got := f.lists.reads["notes.pinned_count"]; got != 3 {
		t.Errorf("personal list read %d times, want once per recipient", got)
	}
}

// ---- test sends ----

// A self-test reaches only the calling admin, and bypasses their own mutes.
func TestTestSendReachesOnlyTheCaller(t *testing.T) {
	f := newFixture(t)
	f.member("u-admin", "Admin", []string{"admin"}, true)
	f.member("u-eva", "Eva", []string{"editor"}, true)

	r := f.rule(t, admin.RuleCreate{
		Name: "Test", ActionKey: str("card.move"),
		Audience: push.Audience{Scope: push.ScopeAll},
	})

	res, err := f.svc.TestRule(adminCtx(), r.ID)
	if err != nil {
		t.Fatalf("test rule: %v", err)
	}
	if res.Recipients != 1 {
		t.Errorf("recipients = %d, want 1 (only the caller)", res.Recipients)
	}

	sent := f.sender.all()
	if len(sent) != 1 || len(sent[0].recipients) != 1 || sent[0].recipients[0] != "u-admin" {
		t.Fatalf("test reached %v, want only u-admin", sent)
	}
	if !sent[0].env.BypassMutes {
		t.Error("a self-test must bypass mutes — otherwise 'does my device work?' is unanswerable")
	}
	if sent[0].env.Kind != push.KindTest {
		t.Errorf("kind = %q, want test", sent[0].env.Kind)
	}
}

// ---- deliveries ----

func TestDeliveryLogRecordsAndPages(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	store := f.svc.Store()

	for i := 0; i < 5; i++ {
		if err := store.RecordDeliveries(context.Background(), []push.DeliveryResult{{
			UserID: "u-karel", SubscriptionID: "sub-1", Status: push.StatusSent,
			Kind: push.KindBroadcast, Category: push.CategoryBroadcast,
		}}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if err := store.RecordDeliveries(context.Background(), []push.DeliveryResult{{
		UserID: "u-karel", Status: push.StatusExpired, Error: "endpoint gone (410)",
		Kind: push.KindTrigger, Category: push.CategoryTriggers,
	}}); err != nil {
		t.Fatalf("record: %v", err)
	}

	page, err := f.svc.ListDeliveries(context.Background(), admin.DeliveryFilter{Limit: 3})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 3 || page.NextCursor == nil {
		t.Fatalf("first page = %d items, cursor %v; want 3 + a cursor", len(page.Items), page.NextCursor)
	}
	// The log answers "did it reach Eva?", so rows carry a name, not a raw id.
	if page.Items[0].UserLabel != "Karel" {
		t.Errorf("user label = %q, want the member's display name", page.Items[0].UserLabel)
	}

	rest, err := f.svc.ListDeliveries(context.Background(), admin.DeliveryFilter{Limit: 10, Cursor: *page.NextCursor})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(rest.Items) != 3 {
		t.Errorf("second page = %d items, want the remaining 3", len(rest.Items))
	}

	filtered, err := f.svc.ListDeliveries(context.Background(), admin.DeliveryFilter{Limit: 10, Status: push.StatusExpired})
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(filtered.Items) != 1 || filtered.Items[0].Status != push.StatusExpired {
		t.Errorf("status filter returned %+v, want the one expired row", filtered.Items)
	}
}

// The from/to filters are compared as STRINGS against a ts stored as RFC 3339
// with nanoseconds, so a date-only bound has to be widened to the day it names.
// Left raw, `to=<today>` sorted before every timestamp on that day and excluded
// the whole of it — the one day the caller asked about.
func TestDeliveryLogDateOnlyBoundsCoverTheWholeDay(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)

	if err := f.svc.Store().RecordDeliveries(context.Background(), []push.DeliveryResult{{
		UserID: "u-karel", SubscriptionID: "sub-1", Status: push.StatusSent,
		Kind: push.KindBroadcast, Category: push.CategoryBroadcast,
	}}); err != nil {
		t.Fatalf("record: %v", err)
	}
	today := time.Now().UTC().Format("2006-01-02")

	sameDay, err := f.svc.ListDeliveries(context.Background(),
		admin.DeliveryFilter{Limit: 10, From: today, To: today})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sameDay.Items) != 1 {
		t.Errorf("from=to=%s returned %d rows, want the row written today", today, len(sameDay.Items))
	}

	// A bound that already carries a time still means exactly that time.
	none, err := f.svc.ListDeliveries(context.Background(),
		admin.DeliveryFilter{Limit: 10, To: today + "T00:00:00Z"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(none.Items) != 0 {
		t.Errorf("to=midnight returned %d rows, want none — the explicit time must not be widened", len(none.Items))
	}
}

// Every kind platform/push can produce must be one the delivery table accepts.
//
// The recorder writes a whole fan-out in ONE transaction, so a single rejected
// value discards the record of every notification in that send — and the failure
// is only logged, never surfaced. This pins the two vocabularies together from
// the admin side; push_test pins that normalization only ever emits these four.
func TestEveryPushKindIsAcceptedByTheDeliveryLog(t *testing.T) {
	f := newFixture(t)
	store := f.svc.Store()

	pairs := []struct{ kind, category string }{
		{push.KindBroadcast, push.CategoryBroadcast},
		{push.KindTrigger, push.CategoryTriggers},
		{push.KindSchedule, push.CategorySummaries},
		{push.KindTest, push.CategoryBroadcast},
		{push.KindTest, push.CategorySummaries},
	}
	for _, p := range pairs {
		if err := store.RecordDeliveries(context.Background(), []push.DeliveryResult{{
			UserID: "u-karel", Status: push.StatusSent, Kind: p.kind, Category: p.category,
		}}); err != nil {
			t.Errorf("kind %q / category %q was rejected by notification_deliveries: %v", p.kind, p.category, err)
		}
	}
}

// A FAILED member lookup must still degrade the recipient column to the raw user
// id, never to an empty cell. The directory is a nicety on top of the log, so it
// is right that a lookup failure does not fail the page — but "did it reach
// Eva?" is the only question this screen exists to answer, and a blank column
// answers it with nothing at all.
func TestDeliveryLabelsFallBackWhenTheDirectoryFails(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	if err := f.svc.Store().RecordDeliveries(context.Background(), []push.DeliveryResult{{
		UserID: "u-karel", Status: push.StatusSent,
		Kind: push.KindBroadcast, Category: push.CategoryBroadcast,
	}}); err != nil {
		t.Fatalf("record: %v", err)
	}

	// Members() projects the directory from the session store; without it the
	// lookup errors, which is the case the fallback exists for.
	if _, err := f.db.Exec(`DROP TABLE sessions`); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}

	page, err := f.svc.ListDeliveries(context.Background(), admin.DeliveryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("a directory failure must not fail the page: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("got %d rows, want 1", len(page.Items))
	}
	if page.Items[0].UserLabel == "" {
		t.Fatal("recipient label is empty — the Doručení column would render blank for every row")
	}
	if page.Items[0].UserLabel != "u-karel" {
		t.Errorf("user label = %q, want the user id as the fallback", page.Items[0].UserLabel)
	}
}

func TestDeliveryPruneRespectsRetention(t *testing.T) {
	f := newFixture(t)
	store := f.svc.Store()

	// One recent row and one old row.
	if err := store.RecordDeliveries(context.Background(), []push.DeliveryResult{{
		UserID: "u1", Status: push.StatusSent, Kind: push.KindBroadcast, Category: push.CategoryBroadcast,
	}}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := f.db.Exec(
		`INSERT INTO notification_deliveries (id, ts, kind, category, user_id, status)
		 VALUES ('old', ?, 'broadcast', 'broadcast', 'u1', 'sent')`,
		time.Now().AddDate(0, 0, -60).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("seed old row: %v", err)
	}

	pruned, err := store.PruneDeliveries(context.Background(), 30, time.Now())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 1 {
		t.Errorf("pruned %d rows, want 1 (only the 60-day-old one)", pruned)
	}

	// 0 means keep forever.
	kept, err := store.PruneDeliveries(context.Background(), 0, time.Now())
	if err != nil || kept != 0 {
		t.Errorf("retention 0 pruned %d (%v), want 0 — it means keep forever", kept, err)
	}
}

// Deliveries are operational, NOT audit: a delivery must never appear in the log
// browser, or an admin would read best-effort evidence as the source of truth.
func TestDeliveriesAreNotAudited(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)

	before := f.auditCount(t)
	if err := f.svc.Store().RecordDeliveries(context.Background(), []push.DeliveryResult{{
		UserID: "u-karel", Status: push.StatusSent, Kind: push.KindTrigger, Category: push.CategoryTriggers,
	}}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if after := f.auditCount(t); after != before {
		t.Errorf("recording a delivery wrote %d audit events, want 0", after-before)
	}
}

func (f *fixture) auditCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM audit_events`).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	return n
}

// ---- catalog ----

func TestCatalogOffersHumanChoices(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)

	cat, err := f.svc.BuildCatalog(context.Background())
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}

	// An admin must pick "Když někdo dokončí připomínku", never reminder.complete.
	var found bool
	for _, a := range cat.Actions {
		if a.Key == "reminder.complete" && a.Module == "events" {
			found = true
			if a.Label == nil || !strings.Contains(*a.Label, "připomínku") {
				t.Errorf("label = %v, want a human Czech phrase", a.Label)
			}
		}
	}
	if !found {
		t.Error("catalog is missing events.reminder.complete")
	}

	// Platform actions belong to no module and would be unbindable without this.
	var hasLogin bool
	for _, a := range cat.Actions {
		if a.Module == "platform" && a.Key == "login" {
			hasLogin = true
		}
	}
	if !hasLogin {
		t.Error("catalog is missing the platform actions")
	}

	if len(cat.Metrics) != 4 {
		t.Errorf("metrics = %d, want the 4 published by the stub", len(cat.Metrics))
	}
	if len(cat.Lists) != 3 {
		t.Errorf("lists = %d, want the 3 published by the stub", len(cat.Lists))
	}
	// The composer shows the empty text, so an admin picking a list knows what the
	// notification will say on a quiet day.
	for _, l := range cat.Lists {
		if l.Key == "events.pripominky_today" && l.Empty != "žádné připomínky" {
			t.Errorf("list %q empty = %q, want the module's own words", l.Key, l.Empty)
		}
	}
	if len(cat.Members) != 1 || cat.Members[0].DisplayName != "Karel" {
		t.Errorf("members = %+v, want the audience picker's one member", cat.Members)
	}

	// Each context offers only its own palette.
	if len(cat.Tokens["broadcast"].Metric) != 0 || len(cat.Tokens["broadcast"].List) != 0 {
		t.Error("a broadcast must not offer metric or list tokens")
	}
	if len(cat.Tokens["summary"].Metric) != 4 {
		t.Errorf("summary palette = %+v, want one token per metric", cat.Tokens["summary"].Metric)
	}
	if len(cat.Tokens["summary"].List) != 3 {
		t.Errorf("summary list palette = %+v, want one token per list", cat.Tokens["summary"].List)
	}
	if len(cat.Tokens["trigger"].Event) == 0 {
		t.Error("a trigger rule must offer event tokens")
	}
}

// ---- rule CRUD ----

func TestRuleCRUDIsAudited(t *testing.T) {
	f := newFixture(t)
	r := f.rule(t, admin.RuleCreate{
		Name: "Pravidlo", ActionKey: str("card.move"), Audience: push.Audience{Scope: push.ScopeAll},
	})

	if r.CoalesceWindowSeconds != 60 {
		t.Errorf("coalesce = %d, want the 60s server default", r.CoalesceWindowSeconds)
	}
	if r.ExcludeActor {
		t.Error("exclude_actor should default to false (D66)")
	}

	if _, err := f.svc.UpdateRule(adminCtx(), r.ID, admin.RuleUpdate{Enabled: no()}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := f.svc.DeleteRule(adminCtx(), r.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	rows, err := f.db.Query(`SELECT action FROM audit_events WHERE module = 'admin' ORDER BY id`)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var actions []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, a)
	}
	want := "rule.create,rule.update,rule.delete"
	if strings.Join(actions, ",") != want {
		t.Errorf("audited actions = %v, want %s", actions, want)
	}
}

// A disabled rule stops firing as soon as it is saved — the listener's cache
// must not outlive the change.
func TestDisablingARuleTakesEffectImmediately(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	r := f.rule(t, admin.RuleCreate{
		Name: "Vypínatelné", ActionKey: str("card.move"),
		Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(0),
	})
	l := f.svc.Listener()

	_ = l.OnEvent(context.Background(), entry("e1", "todo", "card.move"), nil)
	f.waitForSends(1)

	if _, err := f.svc.UpdateRule(adminCtx(), r.ID, admin.RuleUpdate{Enabled: no()}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	_ = l.OnEvent(context.Background(), entry("e2", "todo", "card.move"), nil)
	time.Sleep(100 * time.Millisecond)

	if got := f.sender.count(); got != 1 {
		t.Errorf("sent %d after disabling, want 1 (the cache must be invalidated)", got)
	}
}

func TestGetMissingRuleIs404(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.GetRule(context.Background(), "nope")
	if err == nil || statusOf(t, err) != 404 {
		t.Errorf("err = %v, want a 404", err)
	}
}

// ---- regressions ----

// A patch must tell "field omitted" apart from "field explicitly null".
// encoding/json cannot do it for a **string (decoding null nils the OUTER
// pointer), so RuleUpdate decodes the raw key set itself. Without that, clearing
// a template in the composer is a silent no-op: the save returns 200 and the old
// text keeps being pushed.
func TestPatchDistinguishesNullFromOmitted(t *testing.T) {
	f := newFixture(t)
	r := f.rule(t, admin.RuleCreate{
		Name: "S šablonou", ActionKey: str("card.move"),
		Audience:      push.Audience{Scope: push.ScopeAll},
		TitleTemplate: str("Nadpis"), BodyTemplate: str("Text"),
		FilterModule: str("todo"),
	})

	// Omitted: everything is left alone.
	var omitted admin.RuleUpdate
	if err := json.Unmarshal([]byte(`{"name":"Přejmenováno"}`), &omitted); err != nil {
		t.Fatalf("decode omitted patch: %v", err)
	}
	got, err := f.svc.UpdateRule(adminCtx(), r.ID, omitted)
	if err != nil {
		t.Fatalf("update (omitted): %v", err)
	}
	if got.TitleTemplate == nil || *got.TitleTemplate != "Nadpis" {
		t.Errorf("title_template = %v after an omitted key, want it untouched", got.TitleTemplate)
	}
	if got.FilterModule == nil || *got.FilterModule != "todo" {
		t.Errorf("filter_module = %v after an omitted key, want it untouched", got.FilterModule)
	}

	// Explicit null: the field is cleared.
	var cleared admin.RuleUpdate
	if err := json.Unmarshal([]byte(`{"title_template":null,"body_template":null,"filter_module":null}`), &cleared); err != nil {
		t.Fatalf("decode null patch: %v", err)
	}
	got, err = f.svc.UpdateRule(adminCtx(), r.ID, cleared)
	if err != nil {
		t.Fatalf("update (null): %v", err)
	}
	if got.TitleTemplate != nil || got.BodyTemplate != nil || got.FilterModule != nil {
		t.Errorf("title=%v body=%v filter_module=%v after an explicit null, want all cleared",
			got.TitleTemplate, got.BodyTemplate, got.FilterModule)
	}

	// And the clear actually reached the database, not just the returned struct.
	reloaded, err := f.svc.GetRule(context.Background(), r.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.TitleTemplate != nil || reloaded.BodyTemplate != nil {
		t.Errorf("reloaded templates = %v/%v, want both NULL", reloaded.TitleTemplate, reloaded.BodyTemplate)
	}
}

// The composer inserts change tokens as `{{change.<pole>.old}}` for the admin to
// name the field in. Render only substitutes [a-zA-Z0-9_.], so an unedited one
// would be delivered verbatim to a phone — validation has to refuse it at save.
func TestUneditedChangePlaceholderIsRefused(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.CreateRule(adminCtx(), admin.RuleCreate{
		Name: "Se zástupným polem", ActionKey: str("card.move"),
		Audience:     push.Audience{Scope: push.ScopeAll},
		BodyTemplate: str("Změna: {{change.<pole>.old}} → {{change.<pole>.new}}"),
	})
	if err == nil || statusOf(t, err) != 422 {
		t.Fatalf("err = %v, want a 422 for an unedited <pole> placeholder", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "<pole>") {
		t.Errorf("message = %q, want it to name <pole> so the composer can point at it", msg)
	}

	// The same token with a real field name is fine.
	if _, err := f.svc.CreateRule(adminCtx(), admin.RuleCreate{
		Name: "S polem", ActionKey: str("card.move"),
		Audience:     push.Audience{Scope: push.ScopeAll},
		BodyTemplate: str("Změna: {{change.title.old}} → {{change.title.new}}"),
	}); err != nil {
		t.Errorf("named field rejected: %v", err)
	}
}

// A coalesced push describes the NEWEST event in the window, so exclude_actor
// has to filter the person who made THAT change — not whoever happened to open
// the window a minute earlier.
func TestCoalescedExcludeActorTracksTheNewestEvent(t *testing.T) {
	f := newFixture(t)
	f.member("u-editor", "Karel", []string{"editor"}, true)
	f.member("u-other", "Eva", []string{"editor"}, true)
	f.rule(t, admin.RuleCreate{
		Name: "Sloučené bez původce", ActionKey: str("card.move"),
		Audience:              push.Audience{Scope: push.ScopeAll},
		CoalesceWindowSeconds: num(1), ExcludeActor: yes(),
	})

	first := entry("e1", "todo", "card.move") // ActorUser = u-editor
	second := entry("e2", "todo", "card.move")
	second.ActorUser, second.ActorLabel = "u-other", "Eva"

	l := f.svc.Listener()
	_ = l.OnEvent(context.Background(), first, nil)
	_ = l.OnEvent(context.Background(), second, nil)
	f.waitForSends(1)

	sent := f.sender.all()[0]
	if len(sent.recipients) != 1 || sent.recipients[0] != "u-editor" {
		t.Errorf("recipients = %v, want only u-editor — the flush describes Eva's change, so Eva is the one excluded",
			sent.recipients)
	}
}

// A deploy must not eat a notification. The outbox cursor advances as soon as a
// batch is handed to the listeners, so an event sitting in a coalescing window
// will never be redelivered — a timer killed with the process drops it for good.
func TestFlushPendingSendsOpenCoalesceWindows(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.rule(t, admin.RuleCreate{
		Name: "Dlouhé okno", ActionKey: str("card.move"),
		Audience: push.Audience{Scope: push.ScopeAll},
		// Long enough that the timer would never fire during this test.
		CoalesceWindowSeconds: num(3600),
	})

	l := f.svc.Listener()
	_ = l.OnEvent(context.Background(), entry("e1", "todo", "card.move"), nil)
	_ = l.OnEvent(context.Background(), entry("e2", "todo", "card.move"), nil)
	if got := f.sender.count(); got != 0 {
		t.Fatalf("sent %d before the window closed, want 0", got)
	}

	f.svc.FlushPending(context.Background())

	sent := f.sender.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d on flush, want the one open window", len(sent))
	}
	// The window held two events, so the flushed push still says so.
	if !strings.Contains(sent[0].env.Body, "další") {
		t.Errorf("body = %q, want the coalesced 'a N dalších' suffix", sent[0].env.Body)
	}

	// Flushing again sends nothing: the buffer is gone, not merely disarmed.
	f.svc.FlushPending(context.Background())
	if got := f.sender.count(); got != 1 {
		t.Errorf("sent %d after a second flush, want 1", got)
	}
}

// ---- configuration that changed while a window was open ----

// A coalescing window can stay open for up to 24 hours, so the rule that opened
// it is not necessarily the configuration that still applies when it closes. An
// admin who deletes a rule because it is too noisy must not get one more push
// from it a minute later.
func TestDeletedRuleDoesNotFireFromAnOpenWindow(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	r := f.rule(t, admin.RuleCreate{
		Name: "Karty", ActionKey: str("card.move"),
		Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(3600),
	})

	_ = f.svc.Listener().OnEvent(context.Background(), entry("e1", "todo", "card.move"), nil)
	if err := f.svc.DeleteRule(adminCtx(), r.ID); err != nil {
		t.Fatalf("delete rule: %v", err)
	}

	// The flush is what a deploy does with an open window; it must drop this one.
	f.svc.FlushPending(context.Background())
	if got := f.sender.count(); got != 0 {
		t.Errorf("sent %d for a rule that no longer exists, want 0", got)
	}
}

// Disabling is the same decision as deleting, made reversibly — a rule switched
// off while its window is open must be just as quiet.
func TestDisabledRuleDoesNotFireFromAnOpenWindow(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	r := f.rule(t, admin.RuleCreate{
		Name: "Karty", ActionKey: str("card.move"),
		Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(3600),
	})

	_ = f.svc.Listener().OnEvent(context.Background(), entry("e1", "todo", "card.move"), nil)
	if _, err := f.svc.UpdateRule(adminCtx(), r.ID, admin.RuleUpdate{Enabled: no()}); err != nil {
		t.Fatalf("disable rule: %v", err)
	}

	f.svc.FlushPending(context.Background())
	if got := f.sender.count(); got != 0 {
		t.Errorf("sent %d for a disabled rule, want 0", got)
	}
}

// The flip side of the same rule: a rule EDITED while its window is open sends
// with the edit, not with the copy captured when the first event arrived.
func TestOpenWindowPicksUpAnEditedAudience(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.member("u-eva", "Eva", []string{"editor"}, true)
	r := f.rule(t, admin.RuleCreate{
		Name: "Karty", ActionKey: str("card.move"),
		Audience:              push.Audience{Scope: push.ScopeUsers, Users: []string{"u-karel"}},
		CoalesceWindowSeconds: num(3600),
	})

	_ = f.svc.Listener().OnEvent(context.Background(), entry("e1", "todo", "card.move"), nil)
	if _, err := f.svc.UpdateRule(adminCtx(), r.ID, admin.RuleUpdate{
		Audience: &push.Audience{Scope: push.ScopeUsers, Users: []string{"u-eva"}},
	}); err != nil {
		t.Fatalf("retarget rule: %v", err)
	}

	f.svc.FlushPending(context.Background())
	sent := f.sender.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d, want the one open window", len(sent))
	}
	if len(sent[0].recipients) != 1 || sent[0].recipients[0] != "u-eva" {
		t.Errorf("recipients = %v, want only u-eva — the window must send the CURRENT audience",
			sent[0].recipients)
	}
}

// A zero-window rule dispatches immediately, on a detached goroutine. The flush
// has to wait for those too: shutdown cancels the tailer's context first, and the
// outbox cursor has already moved past the event, so a send cut off there is a
// notification lost rather than delayed.
func TestFlushPendingWaitsForZeroWindowSends(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.rule(t, admin.RuleCreate{
		Name: "Každá změna", ActionKey: str("card.move"),
		Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(0),
	})

	entered, release := f.sender.gateSends()
	_ = f.svc.Listener().OnEvent(context.Background(), entry("e1", "todo", "card.move"), nil)
	<-entered // the send is now genuinely in flight

	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		f.svc.FlushPending(context.Background())
	}()

	// While the send is held, the flush must NOT have returned.
	select {
	case <-flushed:
		t.Fatal("FlushPending returned while a send was still in flight")
	case <-time.After(50 * time.Millisecond):
	}

	release()
	select {
	case <-flushed:
	case <-time.After(3 * time.Second):
		t.Fatal("FlushPending did not return after the send completed")
	}
	if got := f.sender.count(); got != 1 {
		t.Errorf("sent %d after the flush returned, want 1", got)
	}
}

// ---- broadcast click target ----

// The service worker resolves the envelope's url with `new URL(url, origin)`, and
// a value carrying its own scheme wins over the base — so an unchecked url would
// send a household member off-origin on a tap. An over-long one is the other
// failure: title and body are clamped to fit the ~4 KB Web Push record, and the
// url is the only field left that could blow it.
func TestBroadcastRefusesAUrlThatIsNotAnInAppPath(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)

	for _, bad := range []string{
		"https://elsewhere.example",
		"//elsewhere.example",
		"javascript:alert(1)",
		"ukoly",
		strings.Repeat("/x", 400),
	} {
		_, err := f.svc.Broadcast(adminCtx(), admin.BroadcastRequest{
			Title: "Test", Body: "Text", URL: str(bad),
			Audience: push.Audience{Scope: push.ScopeAll},
		})
		if err == nil {
			t.Errorf("url %q was accepted; a notification click must stay in the app", bad)
			continue
		}
		if got := statusOf(t, err); got != 422 {
			t.Errorf("url %q gave %d, want 422", bad, got)
		}
	}
	if got := f.sender.count(); got != 0 {
		t.Errorf("sent %d despite every url being refused, want 0", got)
	}

	// An in-app path is what the composer actually produces, and it survives whole.
	if _, err := f.svc.Broadcast(adminCtx(), admin.BroadcastRequest{
		Title: "Test", Body: "Text", URL: str("/dokumenty?slozka=1"),
		Audience: push.Audience{Scope: push.ScopeAll},
	}); err != nil {
		t.Fatalf("in-app path was refused: %v", err)
	}
	if got := f.sender.all()[0].env.URL; got != "/dokumenty?slozka=1" {
		t.Errorf("envelope url = %q, want the path unchanged", got)
	}

	// No url at all still defaults to the app root.
	if _, err := f.svc.Broadcast(adminCtx(), admin.BroadcastRequest{
		Title: "Test", Body: "Text", Audience: push.Audience{Scope: push.ScopeAll},
	}); err != nil {
		t.Fatalf("broadcast without a url: %v", err)
	}
	if got := f.sender.all()[1].env.URL; got != "/" {
		t.Errorf("envelope url = %q, want the default \"/\"", got)
	}
}

// ---- conditions + active window ----

// The motivating case: a 20:00 summary that only arrives when something is
// still open — "any" of the named counts above zero.
func TestScheduleSkippedWhenConditionsNotMet(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.metrics.values["events.pripominky_today"] = 0
	f.metrics.values["todo.pravedelam_count"] = 0

	sc, err := f.svc.CreateSchedule(adminCtx(), admin.ScheduleCreate{
		Name:          "Večerní kontrola",
		Schedule:      admin.ScheduleSpec{TimeLocal: "20:00", Days: schedDaily()},
		Audience:      push.Audience{Scope: push.ScopeAll},
		TitleTemplate: "Ještě něco zbývá",
		BodyTemplate:  "Připomínky: {{metric.events.pripominky_today}} · Rozdělané: {{metric.todo.pravedelam_count}}",
		Conditions: &admin.Conditions{Mode: "any", Items: []admin.Condition{
			{Key: "events.pripominky_today", Op: "gt", Value: 0},
			{Key: "todo.pravedelam_count", Op: "gt", Value: 0},
		}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Nothing open ⇒ a quiet evening, no push.
	f.svc.FireSchedule(context.Background(), scheduledDue(sc.ID))
	if got := f.sender.count(); got != 0 {
		t.Fatalf("sent %d, want 0 — both counts are zero and the mode is any", got)
	}

	// One count above zero ⇒ the summary goes out.
	f.metrics.values["events.pripominky_today"] = 2
	f.svc.FireSchedule(context.Background(), scheduledDue(sc.ID))
	sent := f.sender.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d, want 1 — one branch of an any-condition holds", len(sent))
	}
	if want := "Připomínky: 2 · Rozdělané: 0"; sent[0].env.Body != want {
		t.Errorf("body = %q, want %q", sent[0].env.Body, want)
	}
}

// "all" requires every clause; one failing clause suppresses the send.
func TestScheduleAllModeRequiresEveryCondition(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.metrics.values["events.pripominky_today"] = 3
	f.metrics.values["todo.pravedelam_count"] = 0

	sc, err := f.svc.CreateSchedule(adminCtx(), admin.ScheduleCreate{
		Name:          "Obojí",
		Schedule:      admin.ScheduleSpec{TimeLocal: "20:00", Days: schedDaily()},
		Audience:      push.Audience{Scope: push.ScopeAll},
		TitleTemplate: "T", BodyTemplate: "B",
		Conditions: &admin.Conditions{Mode: "all", Items: []admin.Condition{
			{Key: "events.pripominky_today", Op: "gt", Value: 0},
			{Key: "todo.pravedelam_count", Op: "gt", Value: 0},
		}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	f.svc.FireSchedule(context.Background(), scheduledDue(sc.ID))
	if got := f.sender.count(); got != 0 {
		t.Errorf("sent %d, want 0 — all-mode with one false clause", got)
	}
}

// A personal condition skips exactly the recipients it fails for.
func TestSchedulePersonalConditionSkipsPerRecipient(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.member("u-eva", "Eva", []string{"editor"}, true)
	f.metrics.perUser["u-karel"] = map[string]int{"notes.pinned_count": 2}
	f.metrics.perUser["u-eva"] = map[string]int{"notes.pinned_count": 0}

	sc, err := f.svc.CreateSchedule(adminCtx(), admin.ScheduleCreate{
		Name:          "Připnuté večer",
		Schedule:      admin.ScheduleSpec{TimeLocal: "20:00", Days: schedDaily()},
		Audience:      push.Audience{Scope: push.ScopeAll},
		TitleTemplate: "Máš připnuté poznámky", BodyTemplate: "Připnuto: {{metric.notes.pinned_count}}",
		Conditions: &admin.Conditions{Mode: "all", Items: []admin.Condition{
			{Key: "notes.pinned_count", Op: "gt", Value: 0},
		}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	f.svc.FireSchedule(context.Background(), scheduledDue(sc.ID))

	sent := f.sender.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d envelopes, want 1 — Eva has nothing pinned", len(sent))
	}
	if len(sent[0].recipients) != 1 || sent[0].recipients[0] != "u-karel" {
		t.Errorf("recipients = %v, want just Karel", sent[0].recipients)
	}
	if want := "Připnuto: 2"; sent[0].env.Body != want {
		t.Errorf("body = %q, want %q", sent[0].env.Body, want)
	}
}

// A condition that cannot be resolved FAILS OPEN: a transient read error must
// suppress nothing — the same choice Render makes for a broken token.
func TestScheduleConditionFailsOpenOnResolverError(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.metrics.fail["events.pripominky_today"] = true

	sc, err := f.svc.CreateSchedule(adminCtx(), admin.ScheduleCreate{
		Name:          "S podmínkou",
		Schedule:      admin.ScheduleSpec{TimeLocal: "20:00", Days: schedDaily()},
		Audience:      push.Audience{Scope: push.ScopeAll},
		TitleTemplate: "T", BodyTemplate: "B",
		Conditions: &admin.Conditions{Mode: "all", Items: []admin.Condition{
			{Key: "events.pripominky_today", Op: "gt", Value: 0},
		}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	f.svc.FireSchedule(context.Background(), scheduledDue(sc.ID))
	if got := f.sender.count(); got != 1 {
		t.Errorf("sent %d, want 1 — a broken resolver must not suppress the summary", got)
	}
}

// A condition key resolves metric-first (a COUNT is cheaper than a list read).
func TestConditionResolvesMetricFirst(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	// events.pripominky_today is in BOTH stub catalogs; make them disagree to
	// prove which one a condition reads.
	f.metrics.values["events.pripominky_today"] = 0
	f.lists.items["events.pripominky_today"] = []string{"Vynést koš"}

	sc, err := f.svc.CreateSchedule(adminCtx(), admin.ScheduleCreate{
		Name:          "Kontrola",
		Schedule:      admin.ScheduleSpec{TimeLocal: "20:00", Days: schedDaily()},
		Audience:      push.Audience{Scope: push.ScopeAll},
		TitleTemplate: "T", BodyTemplate: "B",
		Conditions: &admin.Conditions{Mode: "all", Items: []admin.Condition{
			{Key: "events.pripominky_today", Op: "eq", Value: 0},
		}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	f.svc.FireSchedule(context.Background(), scheduledDue(sc.ID))
	if got := f.sender.count(); got != 1 {
		t.Errorf("sent %d, want 1 — the metric (0) satisfies eq 0; metrics resolve first", got)
	}
}

// Trigger rules: conditions are judged at SEND time against current counts.
func TestTriggerRuleConditionSuppressesSend(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.metrics.values["todo.pravedelam_count"] = 0

	f.rule(t, admin.RuleCreate{
		Name: "Jen když něco zbývá", ActionKey: str("card.move"),
		Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(0),
		Conditions: &admin.Conditions{Mode: "all", Items: []admin.Condition{
			{Key: "todo.pravedelam_count", Op: "gt", Value: 0},
		}},
	})
	l := f.svc.Listener()

	if err := l.OnEvent(context.Background(), entry("e1", "todo", "card.move"), nil); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	if got := f.sender.count(); got != 0 {
		t.Fatalf("sent %d, want 0 — the condition is false", got)
	}

	f.metrics.values["todo.pravedelam_count"] = 3
	if err := l.OnEvent(context.Background(), entry("e2", "todo", "card.move"), nil); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	f.waitForSends(1)
}

// Trigger templates may print household counts, resolved at send time.
func TestTriggerRuleRendersMetricTokens(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)
	f.metrics.values["todo.pravedelam_count"] = 4

	f.rule(t, admin.RuleCreate{
		Name: "S počtem", ActionKey: str("card.move"),
		Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(0),
		BodyTemplate: str("{{event.actor_label}} · rozdělaných úkolů: {{metric.todo.pravedelam_count}}"),
	})
	l := f.svc.Listener()
	if err := l.OnEvent(context.Background(), entry("e1", "todo", "card.move"), nil); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	f.waitForSends(1)
	if want := "Karel · rozdělaných úkolů: 4"; f.sender.all()[0].env.Body != want {
		t.Errorf("body = %q, want %q", f.sender.all()[0].env.Body, want)
	}
}

// The active window drops sends outside it — including across midnight.
func TestTriggerRuleActiveWindow(t *testing.T) {
	f := newFixture(t)
	f.member("u-karel", "Karel", []string{"admin"}, true)

	f.rule(t, admin.RuleCreate{
		Name: "Jen večer", ActionKey: str("card.move"),
		Audience: push.Audience{Scope: push.ScopeAll}, CoalesceWindowSeconds: num(0),
		ActiveFromLocal: str("20:00"), ActiveToLocal: str("06:00"),
	})
	l := f.svc.Listener()

	// The fixture clock reads 08:00 Prague — outside 20:00–06:00.
	if err := l.OnEvent(context.Background(), entry("e1", "todo", "card.move"), nil); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	if got := f.sender.count(); got != 0 {
		t.Fatalf("sent %d, want 0 — 08:00 is outside a 20:00–06:00 window", got)
	}

	// 22:30 is inside the wrapped window.
	f.now = f.now.Add(14*time.Hour + 30*time.Minute)
	if err := l.OnEvent(context.Background(), entry("e2", "todo", "card.move"), nil); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	f.waitForSends(1)
}

// Clearing conditions through a PATCH must actually clear them.
func TestRuleUpdateClearsConditions(t *testing.T) {
	f := newFixture(t)
	r := f.rule(t, admin.RuleCreate{
		Name: "S podmínkou", ActionKey: str("card.move"),
		Audience: push.Audience{Scope: push.ScopeAll},
		Conditions: &admin.Conditions{Mode: "all", Items: []admin.Condition{
			{Key: "todo.pravedelam_count", Op: "gt", Value: 0},
		}},
	})
	if r.Conditions == nil {
		t.Fatalf("created rule lost its conditions")
	}

	var patch admin.RuleUpdate
	if err := json.Unmarshal([]byte(`{"conditions": null}`), &patch); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	upd, err := f.svc.UpdateRule(adminCtx(), r.ID, patch)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Conditions != nil {
		t.Errorf("conditions = %+v, want cleared", upd.Conditions)
	}

	// An empty block clears too — the composer sends the full object each save.
	r2 := f.rule(t, admin.RuleCreate{
		Name: "S podmínkou 2", ActionKey: str("card.create"),
		Audience: push.Audience{Scope: push.ScopeAll},
		Conditions: &admin.Conditions{Mode: "any", Items: []admin.Condition{
			{Key: "todo.pravedelam_count", Op: "gt", Value: 0},
		}},
	})
	if err := json.Unmarshal([]byte(`{"conditions": {"mode":"all","items":[]}}`), &patch); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	upd2, err := f.svc.UpdateRule(adminCtx(), r2.ID, patch)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd2.Conditions != nil {
		t.Errorf("conditions = %+v, want cleared by the empty block", upd2.Conditions)
	}
}

// Conditions round-trip through the store, and the schedule PATCH clears with
// an empty block.
func TestScheduleConditionsRoundTrip(t *testing.T) {
	f := newFixture(t)
	sc, err := f.svc.CreateSchedule(adminCtx(), admin.ScheduleCreate{
		Name:          "Večerní",
		Schedule:      admin.ScheduleSpec{TimeLocal: "20:00", Days: schedDaily()},
		Audience:      push.Audience{Scope: push.ScopeAll},
		TitleTemplate: "T", BodyTemplate: "B",
		Conditions: &admin.Conditions{Items: []admin.Condition{ // mode omitted ⇒ "all"
			{Key: "events.pripominky_today", Op: "gt", Value: 0},
		}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := f.svc.GetSchedule(adminCtx(), sc.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Conditions == nil || got.Conditions.Mode != "all" || len(got.Conditions.Items) != 1 {
		t.Fatalf("conditions did not round-trip: %+v", got.Conditions)
	}

	// Both ways of saying "no conditions" clear: the empty block the composer
	// sends, and the JSON null a read-modify-write of the GET response would.
	for _, tc := range []struct{ name, body string }{
		{"empty block", `{"conditions": {"mode":"all","items":[]}}`},
		{"explicit null", `{"conditions": null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.svc.UpdateSchedule(adminCtx(), sc.ID, admin.ScheduleUpdate{
				Conditions: condPatch(t, `{"conditions": {"mode":"all","items":[{"key":"events.pripominky_today","op":"gt","value":0}]}}`),
			}); err != nil {
				t.Fatalf("restore: %v", err)
			}
			upd, err := f.svc.UpdateSchedule(adminCtx(), sc.ID, admin.ScheduleUpdate{Conditions: condPatch(t, tc.body)})
			if err != nil {
				t.Fatalf("update: %v", err)
			}
			if upd.Conditions != nil {
				t.Errorf("conditions = %+v, want cleared", upd.Conditions)
			}
		})
	}

	// An omitted key still KEEPS — that is the third state.
	if _, err := f.svc.UpdateSchedule(adminCtx(), sc.ID, admin.ScheduleUpdate{
		Conditions: condPatch(t, `{"conditions": {"mode":"all","items":[{"key":"events.pripominky_today","op":"gt","value":0}]}}`),
	}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	kept, err := f.svc.UpdateSchedule(adminCtx(), sc.ID, admin.ScheduleUpdate{Conditions: condPatch(t, `{"name":"Jiné jméno"}`)})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if kept.Conditions == nil {
		t.Error("conditions were cleared by a patch that never mentioned them")
	}
}

// condPatch decodes a schedule patch body and hands back just its conditions
// field, so a test can exercise the absent/null/block distinction the way the
// HTTP handler does.
func condPatch(t *testing.T, body string) **admin.Conditions {
	t.Helper()
	var patch admin.ScheduleUpdate
	if err := json.Unmarshal([]byte(body), &patch); err != nil {
		t.Fatalf("unmarshal patch %s: %v", body, err)
	}
	return patch.Conditions
}
