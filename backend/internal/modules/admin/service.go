package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/scheduler"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
)

// Service is the admin module's business logic: the configuration surface on top
// of the shared push channel (D53 — this module decides WHAT is sent; each
// member decides whether their device receives it).
type Service struct {
	db        *sql.DB
	store     *Store
	sink      audit.ModuleSink
	sender    push.Sender
	pushStore *push.Store
	metrics   *Registry
	lists     *ListRegistry
	actions   []registry.Action
	location  *time.Location
	logger    *slog.Logger
	now       func() time.Time

	defaultCoalesce time.Duration
	listener        *listener

	// storage is the v9 Úložiště + Soukromé položky service. Nil when no storage
	// catalog was wired (a test that does not need one); the two routes then answer
	// 500 rather than panicking inside a page render.
	storage *StorageService
}

// Registry is the metric registry the composer and scheduler read through.
type Registry = metrics.Registry

// ListRegistry is its counterpart for module lists ("which ones?" beside "how
// many?"). Both are read-only catalogs assembled at composition; the admin
// module reaches feature data through them and never imports a feature module.
type ListRegistry = lists.Registry

// maxCoalesceWindowSeconds caps a trigger rule's coalescing window at a day.
// Anything larger is either a typo or a duration the shutdown flush would cut
// short anyway, and past ~9.2e9 the seconds→Duration multiply overflows.
const maxCoalesceWindowSeconds = 24 * 60 * 60

// Options configures the service.
type Options struct {
	Sender          push.Sender
	PushStore       *push.Store
	Metrics         *Registry
	Lists           *ListRegistry
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
		db: db, store: NewStore(db), sink: audit.For(sink, audit.ModuleAdmin),
		sender: opts.Sender, pushStore: opts.PushStore,
		metrics: opts.Metrics, lists: opts.Lists, actions: opts.Actions,
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
		CreatedBy:        reqctx.ActorID(ctx),
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
	r.Conditions = in.Conditions.normalized()
	r.ActiveFromLocal = trimPtr(in.ActiveFromLocal)
	r.ActiveToLocal = trimPtr(in.ActiveToLocal)
	now := s.now().UTC()
	r.CreatedAt, r.UpdatedAt = now, now

	if err := s.validateRule(r); err != nil {
		return nil, err
	}

	if err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.store.InsertRule(ctx, tx, r); err != nil {
			return err
		}
		return s.sink.Record(ctx, tx, audit.Event{
			Action:     "rule.create",
			EntityType: "notification_rule", EntityID: r.ID,
			Summary: fmt.Sprintf("Vytvořeno pravidlo oznámení „%s“ (%s)", r.Name, ruleTriggerLabel(r)),
		})
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
	if in.Conditions != nil {
		r.Conditions = *in.Conditions // already normalized by the decoder
	}
	if in.ActiveFromLocal != nil {
		r.ActiveFromLocal = trimPtr(*in.ActiveFromLocal)
	}
	if in.ActiveToLocal != nil {
		r.ActiveToLocal = trimPtr(*in.ActiveToLocal)
	}
	r.UpdatedAt = s.now().UTC()

	if err := s.validateRule(r); err != nil {
		return nil, err
	}

	if err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.store.UpdateRule(ctx, tx, r); err != nil {
			return err
		}
		return s.sink.Record(ctx, tx, audit.Event{
			Action:     "rule.update",
			EntityType: "notification_rule", EntityID: r.ID,
			Summary: fmt.Sprintf("Upraveno pravidlo oznámení „%s“", r.Name),
			Changes: ruleChanges(*existing, r),
		})
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
		return s.sink.Record(ctx, tx, audit.Event{
			Action:     "rule.delete",
			EntityType: "notification_rule", EntityID: id,
			Summary: fmt.Sprintf("Smazáno pravidlo oznámení „%s“", existing.Name),
		})
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
		if err := ValidateTemplate(*r.TitleTemplate, ContextTrigger, s.metrics, s.lists); err != nil {
			return httpx.ErrUnprocessable(err.Error())
		}
		// Same rule as summaries: a list is a multi-line block and a title is a
		// one-line headline; refuse the mismatch at save time.
		if len(ListKeysIn(*r.TitleTemplate)) > 0 {
			return httpx.ErrUnprocessable("seznam patří do textu oznámení, ne do nadpisu")
		}
	}
	if r.BodyTemplate != nil {
		if err := ValidateTemplate(*r.BodyTemplate, ContextTrigger, s.metrics, s.lists); err != nil {
			return httpx.ErrUnprocessable(err.Error())
		}
	}
	// A trigger renders ONCE for its whole audience, so a personal metric or
	// list has no recipient to be computed for — refuse it here rather than
	// send Karel's pinned notes to the entire household.
	for _, tmpl := range []*string{r.TitleTemplate, r.BodyTemplate} {
		if tmpl == nil {
			continue
		}
		for _, key := range append(MetricKeysIn(*tmpl), ListKeysIn(*tmpl)...) {
			if s.keyIsPersonal(key) {
				return httpx.ErrUnprocessable(fmt.Sprintf(
					"osobní údaj {{%s}} nelze použít v pravidle — počítá se pro každého příjemce zvlášť", key))
			}
		}
	}
	if err := s.validateConditions(r.Conditions, ContextTrigger); err != nil {
		return err
	}
	if err := validateActiveWindow(r.ActiveFromLocal, r.ActiveToLocal); err != nil {
		return err
	}
	return nil
}

// maxConditionItems caps a condition block. More than a handful stops being a
// rule and starts being a program, which the whitelisted-token design (D61)
// deliberately refuses to be.
const maxConditionItems = 8

// validateConditions checks a condition block the way templates are checked:
// at save time, with a Czech reason for the field. In the trigger context a
// personal-scope key is refused — a trigger send has no recipient to compute
// it for (the same reason validateRule refuses personal template tokens).
func (s *Service) validateConditions(c *Conditions, context string) error {
	if c == nil {
		return nil
	}
	if c.Mode != ModeAll && c.Mode != ModeAny {
		return httpx.ErrUnprocessable("podmínky musí mít režim „všechny“ nebo „aspoň jedna“")
	}
	if len(c.Items) > maxConditionItems {
		return httpx.ErrUnprocessable(fmt.Sprintf("podmínek může být nejvýše %d", maxConditionItems))
	}
	for _, it := range c.Items {
		if conditionOps[it.Op] == nil {
			return httpx.ErrUnprocessable(fmt.Sprintf("neznámé porovnání v podmínce: %q", it.Op))
		}
		if it.Value < 0 {
			return httpx.ErrUnprocessable("podmínka porovnává počty — hodnota nemůže být záporná")
		}
		if !s.metrics.Has(it.Key) && !s.lists.Has(it.Key) {
			return httpx.ErrUnprocessable(fmt.Sprintf("neznámý údaj v podmínce: %s", it.Key))
		}
		if context == ContextTrigger && s.keyIsPersonal(it.Key) {
			return httpx.ErrUnprocessable(fmt.Sprintf(
				"osobní údaj %s nelze použít v podmínce pravidla — počítá se pro každého příjemce zvlášť", it.Key))
		}
	}
	return nil
}

// keyIsPersonal asks BOTH catalogs, because a condition or token key may be
// published as a metric, a list, or (sharing one selection) both.
func (s *Service) keyIsPersonal(key string) bool {
	one := []string{key}
	return s.metrics.HasPersonal(one) || s.lists.HasPersonal(one)
}

// validateActiveWindow checks a rule's wall-clock window: both bounds or
// neither, HH:MM shaped, and not degenerate. from > to is legal and means the
// window wraps midnight (22:00–06:00).
func validateActiveWindow(from, to *string) error {
	hasFrom := from != nil && *from != ""
	hasTo := to != nil && *to != ""
	if hasFrom != hasTo {
		return httpx.ErrUnprocessable("vyplňte oba časy okna (od i do), nebo žádný")
	}
	if !hasFrom {
		return nil
	}
	if !scheduler.ValidateTimeLocal(*from) || !scheduler.ValidateTimeLocal(*to) {
		return httpx.ErrUnprocessable("časy okna musí být ve tvaru HH:MM")
	}
	if *from == *to {
		return httpx.ErrUnprocessable("okno nemůže začínat a končit ve stejný čas")
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
		Conditions:    in.Conditions.normalized(),
		CreatedBy:     reqctx.ActorID(ctx),
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
		return s.sink.Record(ctx, tx, audit.Event{
			Action:     "schedule.create",
			EntityType: "notification_schedule", EntityID: sc.ID,
			Summary: fmt.Sprintf("Vytvořen souhrn „%s“ (%s)", sc.Name, sc.Description),
		})
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
	if in.Conditions != nil {
		sc.Conditions = *in.Conditions // already normalized by the decoder
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
		return s.sink.Record(ctx, tx, audit.Event{
			Action:     "schedule.update",
			EntityType: "notification_schedule", EntityID: sc.ID,
			Summary: fmt.Sprintf("Upraven souhrn „%s“ (%s)", sc.Name, sc.Description),
		})
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
		return s.sink.Record(ctx, tx, audit.Event{
			Action:     "schedule.delete",
			EntityType: "notification_schedule", EntityID: id,
			Summary: fmt.Sprintf("Smazán souhrn „%s“", existing.Name),
		})
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
	if err := ValidateTemplate(sc.TitleTemplate, ContextSummary, s.metrics, s.lists); err != nil {
		return httpx.ErrUnprocessable(err.Error())
	}
	// A list renders as one bulleted line per item. In a body that is the point;
	// in a title it is a headline with newlines in it, clipped at 80 runes by the
	// sender — so the composer's easiest mistake (inserting while the title had
	// focus) is refused here rather than discovered on a phone at 08:00.
	if len(ListKeysIn(sc.TitleTemplate)) > 0 {
		return httpx.ErrUnprocessable("seznam patří do textu oznámení, ne do nadpisu")
	}
	if err := ValidateTemplate(sc.BodyTemplate, ContextSummary, s.metrics, s.lists); err != nil {
		return httpx.ErrUnprocessable(err.Error())
	}
	return s.validateConditions(sc.Conditions, ContextSummary)
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
	if err := ValidateTemplate(title, ContextBroadcast, s.metrics, s.lists); err != nil {
		return SendResult{}, httpx.ErrUnprocessable(err.Error())
	}
	if err := ValidateTemplate(body, ContextBroadcast, s.metrics, s.lists); err != nil {
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
		return s.sink.Record(ctx, tx, audit.Event{
			Action:  "broadcast.send",
			Summary: fmt.Sprintf("Rozesláno oznámení „%s“ (%s)", title, czRecipients(res.Recipients)),
			Meta: map[string]any{
				"recipients": res.Recipients, "subscriptions": res.Subscriptions,
				"audience": in.Audience.Scope,
			},
		})
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
	// The test resolves REAL metric/list values (like TestSchedule does) and
	// deliberately ignores the rule's conditions and active window — the point
	// is "does my text read right", not "would it fire right now".
	rc := RenderContext{Now: s.now().In(s.location), Event: &sample, Changes: sampleChanges(),
		Metrics: map[string]int{}, Lists: map[string]ListValue{}}
	title, body := s.listener.render(ctx, *r, &rc)
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
	actor := reqctx.ActorID(ctx)
	rc := s.summaryContext(ctx, *sc, actor)
	return s.sendTest(ctx, push.Envelope{
		Module: "admin", Type: "summary",
		Title: Render(sc.TitleTemplate, rc), Body: Render(sc.BodyTemplate, rc),
		URL: "/", Category: push.CategorySummaries, RuleID: sc.ID,
	}, fmt.Sprintf("souhrnu „%s“", sc.Name))
}

func (s *Service) sendTest(ctx context.Context, env push.Envelope, what string) (SendResult, error) {
	actor := reqctx.ActorID(ctx)
	if actor == "" {
		return SendResult{}, httpx.ErrUnauthorized("")
	}
	env.Kind = push.KindTest
	env.BypassMutes = true

	results := s.sender.Send(ctx, []string{actor}, env)
	res := SendResult{Recipients: push.DistinctUsers(results), Subscriptions: len(results)}

	if err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		return s.sink.Record(ctx, tx, audit.Event{
			Action:  "notification.test",
			Summary: fmt.Sprintf("Zkušební oznámení %s odesláno na vlastní zařízení", what),
		})
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

	// Conditions whose keys are all household-shared hold for everyone or for
	// no one, so they are judged ONCE — and a summary they fail is skipped for
	// the day (MarkFired already spent the slot, which is what "skipped" should
	// mean: the 20:00 check happened, there was nothing to say).
	//
	// The render context exists BEFORE the check so condition values seed it: a
	// key both gated on and printed costs one read, and the text can never
	// contradict the gate that let it through.
	now := s.now().In(s.location)
	condPersonal := s.conditionsPersonalize(sc.Conditions)
	shared := RenderContext{Now: now, Metrics: map[string]int{}, Lists: map[string]ListValue{}}
	if !condPersonal && !s.conditionsSatisfied(ctx, &shared, sc.Conditions, "", sc.TitleTemplate, sc.BodyTemplate, "schedule "+sc.ID) {
		s.logger.Info("admin: summary skipped, conditions not met", "schedule", sc.ID)
		return
	}

	// A summary whose numbers AND lists are all household-shared renders ONCE for
	// everyone; a single personal token (pinned notes, counted or named) forces a
	// render per recipient (D60). A personal CONDITION forces the same path —
	// spelled out here rather than folded into a combined helper, so the
	// per-recipient skip below cannot silently decouple from the branch that
	// enables it.
	if !condPersonal && !s.templatesPersonalize(*sc) {
		s.resolveInto(ctx, &shared, sc.TitleTemplate, sc.BodyTemplate, "", "schedule "+sc.ID, anyScope)
		s.sender.Send(ctx, recipients, push.Envelope{
			Module: "admin", Type: "summary",
			Title: Render(sc.TitleTemplate, shared), Body: Render(sc.BodyTemplate, shared),
			URL: "/", Category: push.CategorySummaries, Kind: push.KindSchedule, RuleID: sc.ID,
		})
		return
	}

	// Only the PERSONAL tokens differ between recipients, so the household half
	// is resolved ONCE and seeded into every render. Re-resolving it per
	// recipient would repeat each household read N times, and a list read is an
	// event scan plus an expansion — not the single COUNT this loop was shaped
	// around when metrics were the only summary token. Household CONDITION
	// clauses of a personal block are seeded the same way, for the same reason.
	s.resolveInto(ctx, &shared, sc.TitleTemplate, sc.BodyTemplate, "", "schedule "+sc.ID, householdScope)
	if condPersonal {
		for _, key := range sc.Conditions.Keys() {
			if !s.keyIsPersonal(key) {
				// resolveInto has already run, so a printed list is present and
				// reused; nothing left for printsList to reorder.
				// A failure here is retried — and warned about — per recipient.
				_, _ = s.conditionValue(ctx, &shared, "", key, nil)
			}
		}
	}
	for _, userID := range recipients {
		rc := shared.clone()
		// A personal condition ("máš připnuté poznámky") is a per-recipient
		// question, so it skips exactly the recipients it fails for.
		if condPersonal && !s.conditionsSatisfied(ctx, &rc, sc.Conditions, userID, sc.TitleTemplate, sc.BodyTemplate, "schedule "+sc.ID) {
			continue
		}
		s.resolveInto(ctx, &rc, sc.TitleTemplate, sc.BodyTemplate, userID, "schedule "+sc.ID, personalScope)
		s.sender.Send(ctx, []string{userID}, push.Envelope{
			Module: "admin", Type: "summary",
			Title: Render(sc.TitleTemplate, rc), Body: Render(sc.BodyTemplate, rc),
			URL: "/", Category: push.CategorySummaries, Kind: push.KindSchedule, RuleID: sc.ID,
		})
	}
}

// conditionsPersonalize reports whether any condition key is personal-scoped —
// which turns the block from one household question into one per recipient.
func (s *Service) conditionsPersonalize(c *Conditions) bool {
	keys := c.Keys()
	return s.metrics.HasPersonal(keys) || s.lists.HasPersonal(keys)
}

// conditionValue resolves one condition key through the render context: a
// value already there (from the templates or an earlier clause) is reused, and
// one resolved here is left for the render, so a key both gated on and printed
// costs one read. Metrics first — a COUNT is cheaper than a list read —
// falling back to the same-keyed list's length (the platform/lists contract
// makes the two name one selection).
//
// printsList names the keys the templates will print AS A LIST. For those the
// order flips: the list read the render needs anyway also answers the clause,
// where a metric read would leave the list still to fetch — two reads for one
// selection, which is exactly what seeding rc is meant to avoid.
func (s *Service) conditionValue(ctx context.Context, rc *RenderContext, userID, key string, printsList map[string]bool) (int, error) {
	if lv, ok := rc.Lists[key]; ok {
		return len(lv.Items), nil
	}
	if s.metrics.Has(key) && !(printsList[key] && s.lists.Has(key)) {
		if v, ok := rc.Metrics[key]; ok {
			return v, nil
		}
		v, err := s.metrics.Resolve(ctx, userID, key, rc.Now)
		if err != nil {
			return 0, err
		}
		rc.Metrics[key] = v
		return v, nil
	}
	items, err := s.lists.Resolve(ctx, userID, key, rc.Now)
	if err != nil {
		return 0, err
	}
	d, _ := s.lists.Descriptor(key)
	rc.Lists[key] = ListValue{Items: items, Empty: d.Empty}
	return len(items), nil
}

// conditionsSatisfied evaluates a block for one recipient, resolving values
// through rc so the render pass reuses them. It FAILS OPEN PER CLAUSE: a value
// that cannot be resolved satisfies that clause only — a summary suppressed by
// a transient read error is a notification LOST (the same choice Render makes
// when it degrades a broken token to a placeholder), but a clause that read
// fine and evaluated false still counts, so one flaky resolver cannot override
// a healthy clause that alone suppresses the send. The warning is what makes
// the failure findable. Both modes short-circuit: a true clause decides "any",
// a false one decides "all" — with broken clauses counting as true, no later
// clause can change either answer.
// The templates travel with the block so a clause on a key the body PRINTS as
// a list resolves list-first — see conditionValue.
func (s *Service) conditionsSatisfied(ctx context.Context, rc *RenderContext, c *Conditions, userID, titleTmpl, bodyTmpl, what string) bool {
	if c == nil || len(c.Items) == 0 {
		return true
	}
	printsList := map[string]bool{}
	for _, key := range append(ListKeysIn(titleTmpl), ListKeysIn(bodyTmpl)...) {
		printsList[key] = true
	}
	for _, it := range c.Items {
		met := true // fail open: an unresolvable clause never suppresses
		if cmp := conditionOps[it.Op]; cmp == nil {
			// a stored op this build no longer knows (rollback, mixed deploy)
			s.logger.Warn("admin: unknown condition op, treating clause as met",
				"op", it.Op, "key", it.Key, "target", what)
		} else if v, err := s.conditionValue(ctx, rc, userID, it.Key, printsList); errors.Is(err, metrics.ErrNoData) {
			// "No reading" is a KNOWN state, not a transient failure: a clause on
			// missing data is simply not met, so a frost gate stays silent rather
			// than firing on absent data. Said out loud, though — in all-mode this
			// clause suppresses the WHOLE summary, and a permanently dataless key
			// (weather turned off, coordinates never set) would otherwise kill it
			// forever with nothing in the log to find.
			s.logger.Warn("admin: condition has no data, treating clause as not met",
				"key", it.Key, "target", what)
			met = false
		} else if err != nil {
			s.logger.Warn("admin: condition resolve failed, treating clause as met",
				"err", err, "key", it.Key, "target", what)
		} else {
			met = cmp(v, it.Value)
		}
		if c.Mode == ModeAny && met {
			return true
		}
		if c.Mode != ModeAny && !met {
			return false
		}
	}
	return c.Mode != ModeAny
}

// templatesPersonalize reports whether a schedule's TEMPLATES force a render
// per recipient, asking BOTH catalogs — a summary that names only personal
// lists personalizes exactly as much as one that counts them. A personal
// CONDITION forces the per-recipient path too; FireSchedule combines the two
// checks explicitly so the coupling is visible at the call site.
func (s *Service) templatesPersonalize(sc Schedule) bool {
	metricKeys := append(MetricKeysIn(sc.TitleTemplate), MetricKeysIn(sc.BodyTemplate)...)
	if s.metrics.HasPersonal(metricKeys) {
		return true
	}
	listKeys := append(ListKeysIn(sc.TitleTemplate), ListKeysIn(sc.BodyTemplate)...)
	return s.lists.HasPersonal(listKeys)
}

// summaryContext resolves every metric and list token a schedule references, for
// one recipient. A resolver that fails degrades THAT TOKEN to a placeholder and
// logs a warning; it never aborts the summary, because one broken count must not
// cost the household the other four.
func (s *Service) summaryContext(ctx context.Context, sc Schedule, userID string) RenderContext {
	return s.summaryContextScoped(ctx, sc, userID, anyScope)
}

// scopeFilter picks which half of a summary's tokens a resolve pass computes, so
// a personalized send can pay for the household half once and the personal half
// per recipient.
type scopeFilter func(scope string) bool

func anyScope(string) bool             { return true }
func householdScope(scope string) bool { return scope != lists.ScopePersonal }
func personalScope(scope string) bool  { return scope == lists.ScopePersonal }

func (s *Service) summaryContextScoped(ctx context.Context, sc Schedule, userID string, want scopeFilter) RenderContext {
	rc := RenderContext{Now: s.now().In(s.location), Metrics: map[string]int{}, Lists: map[string]ListValue{}}
	s.resolveInto(ctx, &rc, sc.TitleTemplate, sc.BodyTemplate, userID, "schedule "+sc.ID, want)
	return rc
}

// resolveInto fills in the tokens of the wanted scope, leaving the rest for a
// later pass. A key no module publishes counts as household, so its failure is
// warned about once rather than once per recipient. A key mentioned in both
// title and body is attempted once — even when the attempt fails, so a broken
// resolver costs one read and one warning per pass, not one per mention.
//
// It works from the TEMPLATES rather than a schedule, because trigger rules
// resolve their metric/list tokens through the same pass (at send time, so the
// counts are current rather than as-of the event that opened a coalescing
// window). `what` labels the warnings.
//
// Lists resolve FIRST: a list key shared with a metric key names the same
// selection (the platform/lists contract on Descriptor.Key), so the metric loop
// reuses len(items) instead of paying for the same read — for events a full
// event scan plus a recurrence expansion — twice.
func (s *Service) resolveInto(ctx context.Context, rc *RenderContext, titleTmpl, bodyTmpl, userID, what string, want scopeFilter) {
	tried := map[string]bool{}
	for _, key := range append(ListKeysIn(titleTmpl), ListKeysIn(bodyTmpl)...) {
		if _, done := rc.Lists[key]; done || tried[key] {
			continue
		}
		tried[key] = true
		// The empty text is the module's, not the sender's: only events knows that
		// an empty reminder list means "žádné připomínky".
		d, known := s.lists.Descriptor(key)
		scope := lists.ScopeHousehold
		if known {
			scope = d.Scope
		}
		if !want(scope) {
			continue
		}
		items, err := s.lists.Resolve(ctx, userID, key, rc.Now)
		if err != nil {
			s.logger.Warn("admin: list resolve failed", "err", err, "list", key, "target", what)
			continue // ⇒ the placeholder, which reads differently from "there are none"
		}
		rc.Lists[key] = ListValue{Items: items, Empty: d.Empty}
	}

	tried = map[string]bool{}
	for _, key := range append(MetricKeysIn(titleTmpl), MetricKeysIn(bodyTmpl)...) {
		if _, done := rc.Metrics[key]; done || tried[key] {
			continue
		}
		tried[key] = true
		scope := metrics.ScopeHousehold
		if d, ok := s.metrics.Descriptor(key); ok {
			scope = d.Scope
		}
		if !want(scope) {
			continue
		}
		if v, ok := rc.Lists[key]; ok {
			rc.Metrics[key] = len(v.Items) // the same-keyed list already selected these
			continue
		}
		v, err := s.metrics.Resolve(ctx, userID, key, rc.Now)
		if err != nil {
			// ErrNoData is a normal state (an unfetched forecast), not a failure
			// worth a warning — but both degrade the token to the placeholder,
			// never to a number.
			if !errors.Is(err, metrics.ErrNoData) {
				s.logger.Warn("admin: metric resolve failed", "err", err, "metric", key, "target", what)
			}
			continue // leaves the token unresolved ⇒ rendered as the placeholder
		}
		rc.Metrics[key] = v
	}
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

	// The trigger palette carries the household-scoped half only: a trigger
	// renders once for its whole audience, so validateRule refuses personal
	// keys — and a palette must never offer what a save would refuse.
	var (
		mets           []MetricDescriptor
		metKeys, hhMet []string
	)
	for _, d := range s.metrics.Catalog() {
		unit := d.Unit
		var unitPtr *string
		if unit != "" {
			unitPtr = &unit
		}
		mets = append(mets, MetricDescriptor{Key: d.Key, Label: d.Label, Unit: unitPtr, Scope: d.Scope})
		metKeys = append(metKeys, d.Key)
		if d.Scope != metrics.ScopePersonal {
			hhMet = append(hhMet, d.Key)
		}
	}

	var (
		lsts             []ListDescriptor
		listKeys, hhList []string
	)
	for _, d := range s.lists.Catalog() {
		lsts = append(lsts, ListDescriptor{Key: d.Key, Label: d.Label, Empty: d.Empty, Scope: d.Scope})
		listKeys = append(listKeys, d.Key)
		if d.Scope != lists.ScopePersonal {
			hhList = append(hhList, d.Key)
		}
	}

	members, err := s.pushStore.Members(ctx)
	if err != nil {
		return Catalog{}, err
	}
	if members == nil {
		members = []push.Member{}
	}

	if lsts == nil {
		lsts = []ListDescriptor{}
	}
	return Catalog{
		Actions: actions,
		Metrics: mets,
		Lists:   lsts,
		Tokens: map[string]TokenPalette{
			ContextBroadcast: PaletteFor(ContextBroadcast, nil, nil),
			ContextTrigger:   PaletteFor(ContextTrigger, hhMet, hhList),
			ContextSummary:   PaletteFor(ContextSummary, metKeys, listKeys),
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
	add("conditions", conditionsText(old.Conditions), conditionsText(next.Conditions))
	add("active_window", windowText(old), windowText(next))
	return out
}

// conditionsText renders a condition block for the audit diff — compact but
// readable, joined by the combinator so "any" and "all" diff differently.
func conditionsText(c *Conditions) string {
	if c == nil {
		return ""
	}
	parts := make([]string, 0, len(c.Items))
	for _, it := range c.Items {
		parts = append(parts, fmt.Sprintf("%s %s %d", it.Key, it.Op, it.Value))
	}
	sep := " a "
	if c.Mode == ModeAny {
		sep = " nebo "
	}
	return strings.Join(parts, sep)
}

func windowText(r Rule) string {
	if r.ActiveFromLocal == nil || r.ActiveToLocal == nil {
		return ""
	}
	return *r.ActiveFromLocal + "–" + *r.ActiveToLocal
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

// SetStorage wires the Úložiště service in after construction, mirroring how the
// push recorder is attached. It is separate from Options because the storage
// catalog is assembled from the MODULE SET, which does not exist yet when the
// admin service is built.
func (s *Service) SetStorage(st *StorageService) { s.storage = st }

// Storage exposes the snapshot service (tests and the handler).
func (s *Service) Storage() *StorageService { return s.storage }

// RecordPrivateItemsView writes `admin.private_items.view` — THE ONLY READ IN HOME
// THAT WRITES AN AUDIT EVENT (v9, D198).
//
// It is not optional and it is not decoration. Soukromé položky is the one screen
// where another member's private item is addressable at all, and it exists because
// "an admin may hard-delete a foreign private item" (D181) is useless if nothing
// can name the thing to delete. A power like that should leave a trace whether or
// not it is used, and this is that trace: the answer to "who looked".
//
// The filter is recorded with it, because "who looked at WHOSE items" is a
// meaningfully different question from "who opened the screen".
func (s *Service) RecordPrivateItemsView(ctx context.Context, f storage.ItemFilter) error {
	meta := map[string]any{}
	if f.OwnerUserID != "" {
		meta["owner_user_id"] = f.OwnerUserID
	}
	if f.Module != "" {
		meta["module"] = f.Module
	}
	if len(meta) == 0 {
		meta = nil
	}
	return appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		return s.sink.Record(ctx, tx, audit.Event{
			Action:  "private_items.view",
			Summary: "Správce otevřel seznam soukromých položek",
			Meta:    meta,
		})
	})
}
