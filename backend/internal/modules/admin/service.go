package admin

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/scheduler"
)

// Service is the admin module's business logic: the configuration surface on top
// of the shared push channel (D53 — this module decides WHAT is sent; each
// member decides whether their device receives it).
type Service struct {
	db        *sql.DB
	store     *Store
	sink      audit.Sink
	sender    push.Sender
	pushStore *push.Store
	metrics   *Registry
	actions   []registry.Action
	location  *time.Location
	logger    *slog.Logger
	now       func() time.Time

	defaultCoalesce time.Duration
	listener        *listener
}

// Registry is the metric registry the composer and scheduler read through.
type Registry = metrics.Registry

// maxCoalesceWindowSeconds caps a trigger rule's coalescing window at a day.
// Anything larger is either a typo or a duration the shutdown flush would cut
// short anyway, and past ~9.2e9 the seconds→Duration multiply overflows.
const maxCoalesceWindowSeconds = 24 * 60 * 60

// Options configures the service.
type Options struct {
	Sender          push.Sender
	PushStore       *push.Store
	Metrics         *Registry
	Actions         []registry.Action
	Location        *time.Location
	DefaultCoalesce time.Duration
	Logger          *slog.Logger
	Now             func() time.Time
}

// NewService builds the admin service.
func NewService(db *sql.DB, sink audit.Sink, opts Options) *Service {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	s := &Service{
		db: db, store: NewStore(db), sink: sink,
		sender: opts.Sender, pushStore: opts.PushStore,
		metrics: opts.Metrics, actions: opts.Actions,
		location: opts.Location, logger: opts.Logger, now: now,
		defaultCoalesce: opts.DefaultCoalesce,
	}
	s.listener = newListener(s)
	return s
}

// Store exposes the store for composition (the scheduler and the push recorder).
func (s *Service) Store() *Store { return s.store }

// Listener is the audit outbox listener this module registers with the platform
// notifier. Registering it is the ONLY connection between admin and the audit
// spine — the module never imports the logging module (D56/D28).
func (s *Service) Listener() audit.Listener { return s.listener }

// FlushPending sends every trigger notification still inside its coalescing
// window, and waits for them. Call it on shutdown, after the background workers
// stop: the outbox cursor has already advanced past those events, so a window
// that dies with the process drops the notification for good rather than
// delaying it.
func (s *Service) FlushPending(ctx context.Context) { s.listener.flush(ctx) }

// ---- rules ----

func (s *Service) ListRules(ctx context.Context, enabled *bool, limit int, cursor string) (RulePage, error) {
	items, next, err := s.store.ListRules(ctx, enabled, limit, cursor)
	if err != nil {
		return RulePage{}, err
	}
	if items == nil {
		items = []Rule{}
	}
	return RulePage{Items: items, NextCursor: next}, nil
}

func (s *Service) GetRule(ctx context.Context, id string) (*Rule, error) {
	r, err := s.store.GetRule(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, httpx.ErrNotFound("pravidlo nenalezeno")
	}
	return r, nil
}

// CreateRule validates and stores a trigger rule, auditing in the same
// transaction.
func (s *Service) CreateRule(ctx context.Context, in RuleCreate) (*Rule, error) {
	r := Rule{
		ID:               idgen.New(),
		Name:             strings.TrimSpace(in.Name),
		Enabled:          true,
		ActionKey:        trimPtr(in.ActionKey),
		ActionPrefix:     trimPtr(in.ActionPrefix),
		FilterModule:     trimPtr(in.FilterModule),
		FilterEntityType: trimPtr(in.FilterEntityType),
		FilterLevel:      trimPtr(in.FilterLevel),
		Audience:         in.Audience,
		TitleTemplate:    in.TitleTemplate,
		BodyTemplate:     in.BodyTemplate,
		ExcludeActor:     false,
		CreatedBy:        actorID(ctx),
	}
	if in.Enabled != nil {
		r.Enabled = *in.Enabled
	}
	if in.ExcludeActor != nil {
		r.ExcludeActor = *in.ExcludeActor
	}
	r.CoalesceWindowSeconds = int(s.defaultCoalesce.Seconds())
	if in.CoalesceWindowSeconds != nil {
		r.CoalesceWindowSeconds = *in.CoalesceWindowSeconds
	}
	now := s.now().UTC()
	r.CreatedAt, r.UpdatedAt = now, now

	if err := s.validateRule(r); err != nil {
		return nil, err
	}

	if err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.store.InsertRule(ctx, tx, r); err != nil {
			return err
		}
		_, err := s.sink.Record(ctx, tx, audit.Event{
			Module: audit.ModuleAdmin, Action: "rule.create",
			EntityType: "notification_rule", EntityID: r.ID,
			Summary: fmt.Sprintf("Vytvořeno pravidlo oznámení „%s“ (%s)", r.Name, ruleTriggerLabel(r)),
		})
		return err
	}); err != nil {
		return nil, err
	}
	s.listener.InvalidateRules()
	return &r, nil
}

// UpdateRule applies a partial update.
func (s *Service) UpdateRule(ctx context.Context, id string, in RuleUpdate) (*Rule, error) {
	existing, err := s.store.GetRule(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, httpx.ErrNotFound("pravidlo nenalezeno")
	}

	r := *existing
	if in.Name != nil {
		r.Name = strings.TrimSpace(*in.Name)
	}
	if in.Enabled != nil {
		r.Enabled = *in.Enabled
	}
	if in.ActionKey != nil {
		r.ActionKey = trimPtr(*in.ActionKey)
	}
	if in.ActionPrefix != nil {
		r.ActionPrefix = trimPtr(*in.ActionPrefix)
	}
	if in.FilterModule != nil {
		r.FilterModule = trimPtr(*in.FilterModule)
	}
	if in.FilterEntityType != nil {
		r.FilterEntityType = trimPtr(*in.FilterEntityType)
	}
	if in.FilterLevel != nil {
		r.FilterLevel = trimPtr(*in.FilterLevel)
	}
	if in.Audience != nil {
		r.Audience = *in.Audience
	}
	if in.TitleTemplate != nil {
		r.TitleTemplate = *in.TitleTemplate
	}
	if in.BodyTemplate != nil {
		r.BodyTemplate = *in.BodyTemplate
	}
	if in.CoalesceWindowSeconds != nil {
		r.CoalesceWindowSeconds = *in.CoalesceWindowSeconds
	}
	if in.ExcludeActor != nil {
		r.ExcludeActor = *in.ExcludeActor
	}
	r.UpdatedAt = s.now().UTC()

	if err := s.validateRule(r); err != nil {
		return nil, err
	}

	if err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.store.UpdateRule(ctx, tx, r); err != nil {
			return err
		}
		_, err := s.sink.Record(ctx, tx, audit.Event{
			Module: audit.ModuleAdmin, Action: "rule.update",
			EntityType: "notification_rule", EntityID: r.ID,
			Summary: fmt.Sprintf("Upraveno pravidlo oznámení „%s“", r.Name),
			Changes: ruleChanges(*existing, r),
		})
		return err
	}); err != nil {
		return nil, err
	}
	s.listener.InvalidateRules()
	return &r, nil
}

func (s *Service) DeleteRule(ctx context.Context, id string) error {
	existing, err := s.store.GetRule(ctx, s.db, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return httpx.ErrNotFound("pravidlo nenalezeno")
	}
	if err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.store.DeleteRule(ctx, tx, id); err != nil {
			return err
		}
		_, err := s.sink.Record(ctx, tx, audit.Event{
			Module: audit.ModuleAdmin, Action: "rule.delete",
			EntityType: "notification_rule", EntityID: id,
			Summary: fmt.Sprintf("Smazáno pravidlo oznámení „%s“", existing.Name),
		})
		return err
	}); err != nil {
		return err
	}
	s.listener.InvalidateRules()
	return nil
}

// validateRule enforces everything the composer must surface as a 422 on a
// field, rather than discovering it at fire time.
func (s *Service) validateRule(r Rule) error {
	if r.Name == "" {
		return httpx.ErrUnprocessable("název pravidla je povinný")
	}
	hasKey := r.ActionKey != nil && *r.ActionKey != ""
	hasPrefix := r.ActionPrefix != nil && *r.ActionPrefix != ""
	if hasKey == hasPrefix {
		return httpx.ErrUnprocessable("vyberte právě jednu akci nebo skupinu akcí")
	}
	if hasKey && !s.knownAction(*r.ActionKey, r.FilterModule) {
		return httpx.ErrUnprocessable(fmt.Sprintf("neznámá akce: %s", qualify(r.FilterModule, *r.ActionKey)))
	}
	if hasPrefix && !s.knownActionPrefix(*r.ActionPrefix) {
		return httpx.ErrUnprocessable(fmt.Sprintf("žádná akce nezačíná na %s", *r.ActionPrefix))
	}
	if !r.Audience.Valid() {
		return httpx.ErrUnprocessable("vyberte, komu se má oznámení poslat")
	}
	// The migration's CHECK constraint accepts only these three. Catching a bad
	// value here is what makes it a 422 on the field instead of a constraint
	// violation surfacing as a bare 500 with nothing an admin can act on.
	if r.FilterLevel != nil && *r.FilterLevel != "" && !validFilterLevels[*r.FilterLevel] {
		return httpx.ErrUnprocessable(fmt.Sprintf(
			"neznámá úroveň: %s (povoleno je %s, %s nebo %s)",
			*r.FilterLevel, audit.LevelInfo, audit.LevelWarn, audit.LevelError))
	}
	if r.CoalesceWindowSeconds < 0 {
		return httpx.ErrUnprocessable("okno pro sloučení nemůže být záporné")
	}
	// An UPPER bound matters as much as the lower one. The listener computes
	// time.Duration(seconds) * time.Second, which overflows int64 past ~9.2e9 and
	// comes out NEGATIVE — and a negative window takes the "fire every event"
	// branch, so the rule asking for the most coalescing would produce the most
	// notifications. Long-but-not-overflowing values are no better: a window that
	// outlives a deploy is flushed at shutdown, never at its own deadline.
	if r.CoalesceWindowSeconds > maxCoalesceWindowSeconds {
		return httpx.ErrUnprocessable("okno pro sloučení může být nejvýše 24 hodin")
	}
	if r.TitleTemplate != nil {
		if err := ValidateTemplate(*r.TitleTemplate, ContextTrigger, s.metrics); err != nil {
			return httpx.ErrUnprocessable(err.Error())
		}
	}
	if r.BodyTemplate != nil {
		if err := ValidateTemplate(*r.BodyTemplate, ContextTrigger, s.metrics); err != nil {
			return httpx.ErrUnprocessable(err.Error())
		}
	}
	return nil
}

// validFilterLevels mirrors the CHECK constraint on notification_rules.filter_level.
var validFilterLevels = map[string]bool{
	audit.LevelInfo: true, audit.LevelWarn: true, audit.LevelError: true,
}

// knownAction reports whether any module emits key. When the rule names a module
// the pair must match: an action key is a bare verb shared across the catalog's
// modules, so "todo" + "folder.create" is a rule that could never fire and is
// refused rather than saved as a silent no-op.
func (s *Service) knownAction(key string, module *string) bool {
	for _, a := range s.actions {
		if a.Key != key {
			continue
		}
		if module == nil || *module == "" || a.Module == *module {
			return true
		}
	}
	return false
}

// qualify renders an action the way the catalog displays it, for error text.
func qualify(module *string, key string) string {
	if module == nil || *module == "" {
		return key
	}
	return *module + "." + key
}

func (s *Service) knownActionPrefix(prefix string) bool {
	for _, a := range s.actions {
		if prefixMatches(prefix, a.Key) {
			return true
		}
	}
	return false
}

// ---- schedules ----

func (s *Service) ListSchedules(ctx context.Context, enabled *bool, limit int, cursor string) (SchedulePage, error) {
	items, next, err := s.store.ListSchedules(ctx, enabled, limit, cursor)
	if err != nil {
		return SchedulePage{}, err
	}
	if items == nil {
		items = []Schedule{}
	}
	return SchedulePage{Items: items, NextCursor: next}, nil
}

func (s *Service) GetSchedule(ctx context.Context, id string) (*Schedule, error) {
	sc, err := s.store.GetSchedule(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if sc == nil {
		return nil, httpx.ErrNotFound("souhrn nenalezen")
	}
	return sc, nil
}

func (s *Service) CreateSchedule(ctx context.Context, in ScheduleCreate) (*Schedule, error) {
	now := s.now().UTC()
	sc := Schedule{
		ID:            idgen.New(),
		Name:          strings.TrimSpace(in.Name),
		Enabled:       true,
		Schedule:      in.Schedule,
		Audience:      in.Audience,
		TitleTemplate: in.TitleTemplate,
		BodyTemplate:  in.BodyTemplate,
		CreatedBy:     actorID(ctx),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if in.Enabled != nil {
		sc.Enabled = *in.Enabled
	}
	if err := s.validateSchedule(sc); err != nil {
		return nil, err
	}
	sc.Description = scheduler.Describe(sc.Schedule.TimeLocal, sc.Schedule.Days)

	if err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.store.InsertSchedule(ctx, tx, sc); err != nil {
			return err
		}
		_, err := s.sink.Record(ctx, tx, audit.Event{
			Module: audit.ModuleAdmin, Action: "schedule.create",
			EntityType: "notification_schedule", EntityID: sc.ID,
			Summary: fmt.Sprintf("Vytvořen souhrn „%s“ (%s)", sc.Name, sc.Description),
		})
		return err
	}); err != nil {
		return nil, err
	}
	return &sc, nil
}

func (s *Service) UpdateSchedule(ctx context.Context, id string, in ScheduleUpdate) (*Schedule, error) {
	existing, err := s.store.GetSchedule(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, httpx.ErrNotFound("souhrn nenalezen")
	}

	sc := *existing
	if in.Name != nil {
		sc.Name = strings.TrimSpace(*in.Name)
	}
	if in.Enabled != nil {
		sc.Enabled = *in.Enabled
	}
	if in.Schedule != nil {
		sc.Schedule = *in.Schedule
	}
	if in.Audience != nil {
		sc.Audience = *in.Audience
	}
	if in.TitleTemplate != nil {
		sc.TitleTemplate = *in.TitleTemplate
	}
	if in.BodyTemplate != nil {
		sc.BodyTemplate = *in.BodyTemplate
	}
	sc.UpdatedAt = s.now().UTC()

	if err := s.validateSchedule(sc); err != nil {
		return nil, err
	}
	sc.Description = scheduler.Describe(sc.Schedule.TimeLocal, sc.Schedule.Days)

	if err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.store.UpdateSchedule(ctx, tx, sc); err != nil {
			return err
		}
		_, err := s.sink.Record(ctx, tx, audit.Event{
			Module: audit.ModuleAdmin, Action: "schedule.update",
			EntityType: "notification_schedule", EntityID: sc.ID,
			Summary: fmt.Sprintf("Upraven souhrn „%s“ (%s)", sc.Name, sc.Description),
		})
		return err
	}); err != nil {
		return nil, err
	}
	return &sc, nil
}

func (s *Service) DeleteSchedule(ctx context.Context, id string) error {
	existing, err := s.store.GetSchedule(ctx, s.db, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return httpx.ErrNotFound("souhrn nenalezen")
	}
	return appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.store.DeleteSchedule(ctx, tx, id); err != nil {
			return err
		}
		_, err := s.sink.Record(ctx, tx, audit.Event{
			Module: audit.ModuleAdmin, Action: "schedule.delete",
			EntityType: "notification_schedule", EntityID: id,
			Summary: fmt.Sprintf("Smazán souhrn „%s“", existing.Name),
		})
		return err
	})
}

func (s *Service) validateSchedule(sc Schedule) error {
	if sc.Name == "" {
		return httpx.ErrUnprocessable("název souhrnu je povinný")
	}
	if !scheduler.ValidateTimeLocal(sc.Schedule.TimeLocal) {
		return httpx.ErrUnprocessable("čas musí být ve tvaru HH:MM")
	}
	if err := scheduler.ValidateDays(sc.Schedule.Days); err != nil {
		return httpx.ErrUnprocessable(err.Error())
	}
	if !sc.Audience.Valid() {
		return httpx.ErrUnprocessable("vyberte, komu se má souhrn poslat")
	}
	if strings.TrimSpace(sc.TitleTemplate) == "" {
		return httpx.ErrUnprocessable("nadpis souhrnu je povinný")
	}
	if strings.TrimSpace(sc.BodyTemplate) == "" {
		return httpx.ErrUnprocessable("text souhrnu je povinný")
	}
	if err := ValidateTemplate(sc.TitleTemplate, ContextSummary, s.metrics); err != nil {
		return httpx.ErrUnprocessable(err.Error())
	}
	if err := ValidateTemplate(sc.BodyTemplate, ContextSummary, s.metrics); err != nil {
		return httpx.ErrUnprocessable(err.Error())
	}
	return nil
}

// ---- broadcast + test sends ----

// Broadcast renders and sends immediately (FR-ADM1).
func (s *Service) Broadcast(ctx context.Context, in BroadcastRequest) (SendResult, error) {
	title := strings.TrimSpace(in.Title)
	body := strings.TrimSpace(in.Body)
	if title == "" || body == "" {
		return SendResult{}, httpx.ErrUnprocessable("nadpis i text jsou povinné")
	}
	if !in.Audience.Valid() {
		return SendResult{}, httpx.ErrUnprocessable("vyberte, komu se má oznámení poslat")
	}
	if err := ValidateTemplate(title, ContextBroadcast, s.metrics); err != nil {
		return SendResult{}, httpx.ErrUnprocessable(err.Error())
	}
	if err := ValidateTemplate(body, ContextBroadcast, s.metrics); err != nil {
		return SendResult{}, httpx.ErrUnprocessable(err.Error())
	}
	url, err := validateInAppURL(in.URL)
	if err != nil {
		return SendResult{}, err
	}

	recipients, err := s.pushStore.ResolveAudience(ctx, in.Audience)
	if err != nil {
		return SendResult{}, err
	}
	rc := RenderContext{Now: s.now().In(s.location)}

	results := s.sender.Send(ctx, push.SortedUserIDs(recipients), push.Envelope{
		Module: "admin", Type: "broadcast",
		Title: Render(title, rc), Body: Render(body, rc), URL: url,
		Category: push.CategoryBroadcast, Kind: push.KindBroadcast,
	})

	res := SendResult{Recipients: push.DistinctUsers(results), Subscriptions: len(results)}
	if err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		_, err := s.sink.Record(ctx, tx, audit.Event{
			Module: audit.ModuleAdmin, Action: "broadcast.send",
			Summary: fmt.Sprintf("Rozesláno oznámení „%s“ (%s)", title, czRecipients(res.Recipients)),
			Meta: map[string]any{
				"recipients": res.Recipients, "subscriptions": res.Subscriptions,
				"audience": in.Audience.Scope,
			},
		})
		return err
	}); err != nil {
		// The push already went out; failing the request now would invite a
		// duplicate send. Record the bookkeeping failure and report success.
		s.logger.Error("admin: audit broadcast", "err", err)
	}
	return res, nil
}

// TestRule sends a rule's rendered text to the CALLING ADMIN's own devices only,
// bypassing both the audience and every mute (FR-ADM6) — the point is to answer
// "does this device work and does my text read right?".
func (s *Service) TestRule(ctx context.Context, id string) (SendResult, error) {
	r, err := s.GetRule(ctx, id)
	if err != nil {
		return SendResult{}, err
	}
	sample := sampleEntry(*r, s.now().In(s.location))
	title, body := s.listener.render(*r, sample, sampleChanges())
	return s.sendTest(ctx, push.Envelope{
		Module: sample.Module, Type: "trigger", Title: title, Body: body,
		URL: inAppURL(sample), Category: push.CategoryTriggers, RuleID: r.ID,
	}, fmt.Sprintf("pravidla „%s“", r.Name))
}

// TestSchedule renders a summary with REAL metric values for the calling admin.
func (s *Service) TestSchedule(ctx context.Context, id string) (SendResult, error) {
	sc, err := s.GetSchedule(ctx, id)
	if err != nil {
		return SendResult{}, err
	}
	actor := actorID(ctx)
	rc := s.summaryContext(ctx, *sc, actor)
	return s.sendTest(ctx, push.Envelope{
		Module: "admin", Type: "summary",
		Title: Render(sc.TitleTemplate, rc), Body: Render(sc.BodyTemplate, rc),
		URL: "/", Category: push.CategorySummaries, RuleID: sc.ID,
	}, fmt.Sprintf("souhrnu „%s“", sc.Name))
}

func (s *Service) sendTest(ctx context.Context, env push.Envelope, what string) (SendResult, error) {
	actor := actorID(ctx)
	if actor == "" {
		return SendResult{}, httpx.ErrUnauthorized("")
	}
	env.Kind = push.KindTest
	env.BypassMutes = true

	results := s.sender.Send(ctx, []string{actor}, env)
	res := SendResult{Recipients: push.DistinctUsers(results), Subscriptions: len(results)}

	if err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		_, err := s.sink.Record(ctx, tx, audit.Event{
			Module: audit.ModuleAdmin, Action: "notification.test",
			Summary: fmt.Sprintf("Zkušební oznámení %s odesláno na vlastní zařízení", what),
		})
		return err
	}); err != nil {
		s.logger.Error("admin: audit test send", "err", err)
	}
	return res, nil
}

// ---- scheduled summaries ----

// FireSchedule is the scheduler's callback: resolve the audience, render per
// recipient where the template personalizes, and send.
func (s *Service) FireSchedule(ctx context.Context, due scheduler.Due) {
	sc, err := s.store.GetSchedule(ctx, s.db, due.Schedule.ID)
	if err != nil || sc == nil {
		s.logger.Error("admin: load schedule for firing", "err", err, "schedule", due.Schedule.ID)
		return
	}

	recipients, err := s.pushStore.ResolveAudience(ctx, sc.Audience)
	if err != nil {
		s.logger.Error("admin: resolve summary audience", "err", err, "schedule", sc.ID)
		return
	}
	if len(recipients) == 0 {
		return
	}
	recipients = push.SortedUserIDs(recipients)

	keys := append(MetricKeysIn(sc.TitleTemplate), MetricKeysIn(sc.BodyTemplate)...)
	// A summary whose numbers are all household-shared renders ONCE for everyone;
	// only a personal metric (pinned counts) forces a render per recipient (D60).
	if !s.metrics.HasPersonal(keys) {
		rc := s.summaryContext(ctx, *sc, "")
		s.sender.Send(ctx, recipients, push.Envelope{
			Module: "admin", Type: "summary",
			Title: Render(sc.TitleTemplate, rc), Body: Render(sc.BodyTemplate, rc),
			URL: "/", Category: push.CategorySummaries, Kind: push.KindSchedule, RuleID: sc.ID,
		})
		return
	}

	for _, userID := range recipients {
		rc := s.summaryContext(ctx, *sc, userID)
		s.sender.Send(ctx, []string{userID}, push.Envelope{
			Module: "admin", Type: "summary",
			Title: Render(sc.TitleTemplate, rc), Body: Render(sc.BodyTemplate, rc),
			URL: "/", Category: push.CategorySummaries, Kind: push.KindSchedule, RuleID: sc.ID,
		})
	}
}

// summaryContext resolves every metric token a schedule references, for one
// recipient. A resolver that fails degrades THAT TOKEN to a placeholder and logs
// a warning; it never aborts the summary, because one broken count must not cost
// the household the other four.
func (s *Service) summaryContext(ctx context.Context, sc Schedule, userID string) RenderContext {
	asOf := s.now().In(s.location)
	rc := RenderContext{Now: asOf, Metrics: map[string]int{}}

	keys := append(MetricKeysIn(sc.TitleTemplate), MetricKeysIn(sc.BodyTemplate)...)
	for _, key := range keys {
		if _, done := rc.Metrics[key]; done {
			continue
		}
		v, err := s.metrics.Resolve(ctx, userID, key, asOf)
		if err != nil {
			s.logger.Warn("admin: metric resolve failed", "err", err, "metric", key, "schedule", sc.ID)
			continue // leaves the token unresolved ⇒ rendered as the placeholder
		}
		rc.Metrics[key] = v
	}
	return rc
}

// ---- catalog + deliveries ----

// BuildCatalog assembles what the composer offers (FR-ADM4). Everything comes
// from the LIVE registry, so a key can never be offered that no module emits.
func (s *Service) BuildCatalog(ctx context.Context) (Catalog, error) {
	actions := make([]ActionDescriptor, 0, len(s.actions))
	for _, a := range s.actions {
		label := ActionLabel(a.Module, a.Key)
		actions = append(actions, ActionDescriptor{Key: a.Key, Module: a.Module, Label: &label})
	}

	var (
		mets    []MetricDescriptor
		metKeys []string
	)
	for _, d := range s.metrics.Catalog() {
		unit := d.Unit
		var unitPtr *string
		if unit != "" {
			unitPtr = &unit
		}
		mets = append(mets, MetricDescriptor{Key: d.Key, Label: d.Label, Unit: unitPtr, Scope: d.Scope})
		metKeys = append(metKeys, d.Key)
	}

	members, err := s.pushStore.Members(ctx)
	if err != nil {
		return Catalog{}, err
	}
	if members == nil {
		members = []push.Member{}
	}

	return Catalog{
		Actions: actions,
		Metrics: mets,
		Tokens: map[string]TokenPalette{
			ContextBroadcast: PaletteFor(ContextBroadcast, nil),
			ContextTrigger:   PaletteFor(ContextTrigger, nil),
			ContextSummary:   PaletteFor(ContextSummary, metKeys),
		},
		Members: members,
	}, nil
}

// ListDeliveries pages the operational delivery log, labelling each row with the
// recipient's display name (the log is for answering "did it reach Eva?").
func (s *Service) ListDeliveries(ctx context.Context, f DeliveryFilter) (DeliveryPage, error) {
	items, next, err := s.store.ListDeliveries(ctx, f)
	if err != nil {
		return DeliveryPage{}, err
	}
	if items == nil {
		items = []Delivery{}
	}
	// The directory is a NICETY on top of the log, so a failed lookup degrades to
	// the raw user id rather than failing the page — but it must degrade to
	// something. Leaving the label empty renders a blank Příjemce column for every
	// row, which is the one question this screen exists to answer.
	labels := map[string]string{}
	members, err := s.pushStore.Members(ctx)
	if err != nil {
		s.logger.Warn("admin: label deliveries", "err", err)
	} else {
		for _, m := range members {
			labels[m.UserID] = m.DisplayName
		}
	}
	for i := range items {
		if label, ok := labels[items[i].UserID]; ok && label != "" {
			items[i].UserLabel = label
		} else {
			items[i].UserLabel = items[i].UserID
		}
	}
	return DeliveryPage{Items: items, NextCursor: next}, nil
}

// PruneDeliveries drops delivery rows past the retention window (D64).
func (s *Service) PruneDeliveries(ctx context.Context, retentionDays int) (int64, error) {
	return s.store.PruneDeliveries(ctx, retentionDays, s.now())
}

// ---- helpers ----

func actorID(ctx context.Context) string {
	if a, ok := reqctx.ActorFrom(ctx); ok {
		return a.UserID
	}
	return ""
}

// maxInAppURLBytes caps a broadcast's click target. Web Push allows about 4 KB
// per ENCRYPTED record and the whole envelope is JSON inside it: title and body
// are already clamped to 80 and 300 runes for that reason, and an unbounded URL
// would be the one field able to blow the budget — turning a well-formed
// broadcast into a 413 from every push service at once, for every device.
const maxInAppURLBytes = 512

// validateInAppURL checks a broadcast's click target and returns the URL to put
// on the envelope ("/" when none was given).
//
// It must be an IN-APP path. The service worker resolves this string with
// `new URL(url, origin)`, and a value carrying its own scheme wins over the
// base — so "https://elsewhere.example" would make a notification click leave
// the app entirely. Every other sender already produces a path (inAppURL for
// triggers, "/" for summaries); this is the only one an admin types.
func validateInAppURL(raw *string) (string, error) {
	if raw == nil {
		return "/", nil
	}
	url := strings.TrimSpace(*raw)
	if url == "" {
		return "/", nil
	}
	if len(url) > maxInAppURLBytes {
		return "", httpx.ErrUnprocessable(fmt.Sprintf("odkaz může mít nejvýše %d znaků", maxInAppURLBytes))
	}
	// A single leading slash, and nothing that could be read as an authority
	// ("//host" is a protocol-relative URL) or a scheme.
	if !strings.HasPrefix(url, "/") || strings.HasPrefix(url, "//") {
		return "", httpx.ErrUnprocessable(`odkaz musí být cesta v aplikaci, například "/ukoly" nebo "/dokumenty"`)
	}
	if strings.ContainsAny(url, " \t\r\n") {
		return "", httpx.ErrUnprocessable("odkaz nesmí obsahovat mezery")
	}
	return url, nil
}

func trimPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	return &v
}

// ruleTriggerLabel renders what a rule reacts to, for the audit summary.
func ruleTriggerLabel(r Rule) string {
	if r.ActionKey != nil {
		return *r.ActionKey
	}
	if r.ActionPrefix != nil {
		return *r.ActionPrefix + "*"
	}
	return "?"
}

// ruleChanges diffs the fields an admin can actually change, so the log browser
// shows what was edited rather than a bare "upraveno".
func ruleChanges(old, next Rule) []audit.Change {
	var out []audit.Change
	add := func(field, o, n string) {
		if o != n {
			out = append(out, audit.Change{Field: field, Old: audit.Ptr(o), New: audit.Ptr(n)})
		}
	}
	add("name", old.Name, next.Name)
	add("enabled", boolText(old.Enabled), boolText(next.Enabled))
	add("action", ruleTriggerLabel(old), ruleTriggerLabel(next))
	add("audience", old.Audience.Scope, next.Audience.Scope)
	add("coalesce_window_seconds", itoa(old.CoalesceWindowSeconds), itoa(next.CoalesceWindowSeconds))
	add("exclude_actor", boolText(old.ExcludeActor), boolText(next.ExcludeActor))
	add("title_template", derefOr(old.TitleTemplate, ""), derefOr(next.TitleTemplate, ""))
	add("body_template", derefOr(old.BodyTemplate, ""), derefOr(next.BodyTemplate, ""))
	return out
}

func boolText(b bool) string {
	if b {
		return "ano"
	}
	return "ne"
}

func derefOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// czRecipients renders a recipient count with the three Czech plural forms.
func czRecipients(n int) string {
	switch {
	case n == 1:
		return "1 příjemce"
	case n >= 2 && n <= 4:
		return fmt.Sprintf("%d příjemci", n)
	default:
		return fmt.Sprintf("%d příjemců", n)
	}
}

// sampleEntry fabricates a representative event so "Poslat test" shows the admin
// what a rule will actually look like, tokens resolved, before it ever fires.
func sampleEntry(r Rule, now time.Time) audit.Entry {
	action := "card.move"
	if r.ActionKey != nil && *r.ActionKey != "" {
		action = *r.ActionKey
	} else if r.ActionPrefix != nil && *r.ActionPrefix != "" {
		action = strings.TrimSuffix(*r.ActionPrefix, ".") + ".update"
	}
	module := "todo"
	if r.FilterModule != nil && *r.FilterModule != "" {
		module = *r.FilterModule
	}
	entity := "card"
	if r.FilterEntityType != nil && *r.FilterEntityType != "" {
		entity = *r.FilterEntityType
	}
	return audit.Entry{
		ID: "sample", TS: now, ActorUser: "sample", ActorType: "user", ActorLabel: "Karel",
		Module: module, Action: action, EntityType: entity, EntityID: "sample",
		Summary: "Ukázka: Karel přesunul kartu „Vynést koš“ do Hotovo",
		Level:   audit.LevelInfo,
	}
}

func sampleChanges() []audit.Change {
	return []audit.Change{
		{Field: "title", Old: audit.Ptr("Vynést koš"), New: audit.Ptr("Vynést koš a papír")},
	}
}
