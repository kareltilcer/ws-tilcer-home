// Package scheduler is the in-process wall-clock ticker that fires scheduled
// summary notifications (PRD §V5-1 D58/D58a/D74, HANDOFF-7 §6).
//
// It is a deliberate, SCOPED reversal of "no scheduler" (D9/D11). Reminders are
// still computed on read — nothing here changes that. But a summary at 08:00
// cannot be computed on read: something server-side has to wake up at 08:00
// Prague and push. That is all this does.
//
// Home is a single binary on a single droplet, so there is exactly one instance
// and no distributed locking to get wrong. What DOES need care is the calendar:
// every decision is made in HOME_TIMEZONE, so a slot stays at 08:00 local across
// a DST boundary rather than drifting to 07:00 or 09:00.
package scheduler

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
)

// Preset day patterns.
const (
	PresetDaily    = "daily"
	PresetWeekdays = "weekdays"
	PresetWeekends = "weekends"
)

// Weekday tokens as stored in a schedule's days_spec.
var weekdayTokens = map[string]time.Weekday{
	"mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday, "sun": time.Sunday,
}

// DaysSpec is when a schedule repeats. Exactly one form is set (openapi
// ScheduleSpec.days).
type DaysSpec struct {
	Preset     string   `json:"preset,omitempty"`   // daily | weekdays | weekends
	Weekdays   []string `json:"weekdays,omitempty"` // mon..sun
	DayOfMonth int      `json:"day_of_month,omitempty"`
}

// Due is one schedule the ticker found ready to fire.
type Due struct {
	Schedule  Schedule
	LocalDate string // YYYY-MM-DD in the scheduler's zone — the fired marker
	CaughtUp  bool   // true when this is a missed slot fired late (D58a)
}

// Schedule is the subset of a persisted schedule the ticker needs. The admin
// module owns the table; this package only reads what it must to decide "now?".
type Schedule struct {
	ID        string
	Name      string
	Enabled   bool
	TimeLocal string // "HH:MM"
	Days      DaysSpec
	// ConfiguredAt is when this schedule last took its current shape (its
	// updated_at). Catch-up is only honest for a slot the schedule was already
	// waiting on: without this, saving a summary at 08:30 for 08:00 would look
	// exactly like a slot missed during a restart and fire immediately.
	ConfiguredAt       time.Time
	LastFiredLocalDate string // "YYYY-MM-DD", empty if never
}

// Store is what the scheduler needs from the admin module's tables. Keeping it
// an interface is what lets platform/scheduler stay module-agnostic.
type Store interface {
	// DueCandidates returns every ENABLED schedule. The set is tiny (a household
	// has a handful), so filtering happens in Go where the calendar rules live.
	DueCandidates(ctx context.Context) ([]Schedule, error)
	// MarkFired persists the fired marker. It is called BEFORE the send, so a
	// crash mid-send can never re-fire the same slot.
	MarkFired(ctx context.Context, scheduleID, localDate string, at time.Time) error
}

// Fire runs one due schedule. It is called after the marker is persisted, so it
// must never assume it can retry.
type Fire func(ctx context.Context, due Due)

// Config tunes the ticker.
type Config struct {
	Location *time.Location // HOME_TIMEZONE; required
	Tick     time.Duration  // default 60s
	// CatchupGrace fires a slot missed while the process was down, if we are back
	// within this window. 0 disables catch-up entirely.
	CatchupGrace time.Duration
	Logger       *slog.Logger
	// Now is injected by tests.
	Now func() time.Time
}

// Job is a plain recurring task registered with RegisterJob.
type job struct {
	name  string
	every time.Duration
	fn    func(ctx context.Context, now time.Time) error
	// running keeps THIS job's passes from piling up without coupling jobs to
	// each other: a pass that outlives a tick makes the next tick skip this job
	// only, and says so in the log. lastRun is only touched while running is
	// held, so the flag is also what serializes access to it.
	running atomic.Bool
	lastRun time.Time
}

// Scheduler evaluates schedules on a minute ticker.
type Scheduler struct {
	store  Store
	fire   Fire
	cfg    Config
	logger *slog.Logger
	now    func() time.Time

	// jobs are plain interval tasks riding the same ticker — see RegisterJob.
	jobs []*job
}

// RegisterJob adds a plain recurring task to the SAME Prague-local minute
// ticker the summaries run on, and changes nothing about how they fire.
//
// v5 created this package precisely so that "something has to wake up at a
// certain time" lives in one place. A feature module that needs a poll — v7's
// garden weather is the first — would otherwise start a second ad-hoc ticker
// inside itself, which is the arrangement this package exists to prevent. So the
// hook is one exported function rather than a new package.
//
// The job fires ONCE on the first tick after registration (lastRun is zero) and
// then every `every` after that. A run that returns an error is retried after
// jobRetryDelay rather than a full interval — a DNS blip on the first weather
// poll must not leave the frost gate blind until tomorrow. Timing is otherwise
// best-effort by design: a forecast is not a wall-clock appointment, and a job
// that slips a minute across a restart has lost nothing. Register before Start;
// the set is not mutated afterwards.
func (s *Scheduler) RegisterJob(name string, every time.Duration, fn func(ctx context.Context, now time.Time) error) {
	if fn == nil || every <= 0 {
		return
	}
	s.jobs = append(s.jobs, &job{name: name, every: every, fn: fn})
}

// jobRetryDelay is how soon a FAILED job run is retried. Long enough not to
// hammer a provider that is down, short enough that a transient failure does
// not cost the whole interval.
const jobRetryDelay = 15 * time.Minute

// runJob runs one job if its interval has elapsed. The caller holds j.running.
// lastRun is stamped before the run so a job can never tight-loop, then pulled
// back on failure so the next attempt lands jobRetryDelay from now. A panic is
// NOT retried sooner: a job that panics once will panic again, and the full
// interval is the pacing that keeps that from being a log storm.
func (s *Scheduler) runJob(ctx context.Context, j *job, now time.Time) {
	if !j.lastRun.IsZero() && now.Sub(j.lastRun) < j.every {
		return
	}
	j.lastRun = now
	if err := s.callJob(ctx, j, now); err != nil {
		s.logger.Warn("scheduler: job failed", "job", j.name, "err", err, "retry_in", jobRetryDelay)
		if j.every > jobRetryDelay {
			j.lastRun = now.Add(jobRetryDelay - j.every)
		}
	}
}

// callJob runs one job with its panic contained.
//
// ⚠ THE STACK IS PART OF THE LINE. This Error is also what the crash board
// receives (platform/statusreport lifts a `stack` attr into the report's own
// stack field, whose first frame is what the server groups on), and "scheduler:
// job panicked" plus a job name says which job died and nothing about where.
// httpx.Recover and the preview worker log theirs the same way.
func (s *Scheduler) callJob(ctx context.Context, j *job, now time.Time) (err error) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("scheduler: job panicked", "job", j.name, "panic", r,
				"stack", string(debug.Stack()))
		}
	}()
	return j.fn(ctx, now)
}

// New builds the scheduler.
func New(store Store, fire Fire, cfg Config) *Scheduler {
	if cfg.Tick <= 0 {
		cfg.Tick = time.Minute
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Location == nil {
		cfg.Location = time.UTC
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Scheduler{store: store, fire: fire, cfg: cfg, logger: cfg.Logger, now: now}
}

// Start runs the ticker until ctx is cancelled. Returns immediately.
func (s *Scheduler) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.cfg.Tick)
		defer ticker.Stop()
		s.logger.Info("scheduler: started", "tick", s.cfg.Tick, "tz", s.cfg.Location.String(),
			"catchup_grace", s.cfg.CatchupGrace)
		// Run once immediately so a slot missed during a deploy is caught up
		// without waiting a full tick.
		s.Tick(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.Tick(ctx)
			}
		}
	}()
}

// Tick evaluates every enabled schedule once. Exported so tests can drive time
// deterministically instead of sleeping.
func (s *Scheduler) Tick(ctx context.Context) {
	now := s.now().In(s.cfg.Location)
	// Plain interval jobs first and unconditionally: they must keep running even
	// if the schedule store is unavailable, since a forecast poll has nothing to
	// do with whether a summary can be loaded. They run OFF the ticker
	// goroutine: a job doing network or DB work can outlast the tick interval,
	// and time.Ticker drops ticks a slow receiver misses — a stalled fetch must
	// never cost a summary its slot minute. Each job carries its own in-flight
	// flag, so a slow or hung job skips only its own later passes — loudly —
	// instead of silently starving every other job.
	for _, j := range s.jobs {
		if !j.running.CompareAndSwap(false, true) {
			s.logger.Warn("scheduler: job pass still running, skipping tick", "job", j.name)
			continue
		}
		go func(j *job) {
			defer j.running.Store(false)
			s.runJob(ctx, j, now)
		}(j)
	}

	schedules, err := s.store.DueCandidates(ctx)
	if err != nil {
		s.logger.Error("scheduler: load schedules", "err", err)
		return
	}

	for _, sc := range schedules {
		due, ok := s.evaluate(sc, now)
		if !ok {
			continue
		}
		// Persist the marker FIRST. If the process dies during the send, the slot
		// is already spent: a missed summary is a nuisance, a summary sent five
		// times because the crash kept re-firing it is a reason to turn
		// notifications off.
		if err := s.store.MarkFired(ctx, sc.ID, due.LocalDate, now); err != nil {
			s.logger.Error("scheduler: mark fired", "err", err, "schedule", sc.ID)
			continue
		}
		s.logger.Info("scheduler: firing", "schedule", sc.ID, "name", sc.Name,
			"local_date", due.LocalDate, "time_local", sc.TimeLocal, "caught_up", due.CaughtUp)
		s.fire(ctx, due)
	}
}

// evaluate decides whether sc should fire at now (already in the local zone).
//
// Two calendar days are considered, not one. A slot is anchored to a local DATE,
// so a 23:50 summary missed during a restart that ran from 23:45 to 00:05 is a
// slot belonging to YESTERDAY: evaluated only against today it looks like a slot
// still 23h55m in the future, and CatchupGrace — the whole point of which is to
// recover exactly that miss — would never see it. So today is tried first (the
// overwhelmingly common case), and yesterday only as a catch-up.
func (s *Scheduler) evaluate(sc Schedule, now time.Time) (Due, bool) {
	if !sc.Enabled {
		return Due{}, false
	}
	hh, mm, ok := parseHHMM(sc.TimeLocal)
	if !ok {
		s.logger.Warn("scheduler: unparseable time_local", "schedule", sc.ID, "value", sc.TimeLocal)
		return Due{}, false
	}

	if due, ok := s.evaluateDay(sc, now, now, hh, mm); ok {
		return due, true
	}
	if s.cfg.CatchupGrace > 0 {
		// AddDate on the local time, not a 24h subtraction: across a DST boundary
		// "yesterday" is a calendar day, not a fixed number of hours.
		if due, ok := s.evaluateDay(sc, now, now.AddDate(0, 0, -1), hh, mm); ok {
			return due, true
		}
	}
	return Due{}, false
}

// evaluateDay decides whether sc's slot on `day` is due at `now`. The two are the
// same date in the normal case and one day apart when catching up across
// midnight; every rule — the fired marker, the day pattern, the configured-at
// guard — is applied against `day`, which is what makes the two cases identical
// apart from which calendar day they name.
func (s *Scheduler) evaluateDay(sc Schedule, now, day time.Time, hh, mm int) (Due, bool) {
	localDate := day.Format("2006-01-02")
	if sc.LastFiredLocalDate == localDate {
		return Due{}, false // already fired for this local day
	}
	if !s.dayMatches(sc.Days, day) {
		return Due{}, false
	}

	slot := time.Date(day.Year(), day.Month(), day.Day(), hh, mm, 0, 0, s.cfg.Location)
	switch {
	case sameMinute(now, slot):
		return Due{Schedule: sc, LocalDate: localDate}, true
	case now.After(slot):
		// The slot passed while we were not running (a deploy, a restart, a
		// hung tick). Fire ONCE if we are back soon enough; older misses are
		// dropped rather than delivered as stale news (D58a).
		//
		// A slot the schedule was NOT yet configured for is not a miss — it is a
		// slot that never applied. Saving "every day at 08:00" at 08:30, or
		// re-timing an existing summary to a time already past, must not push a
		// household-wide summary the moment the form is submitted.
		if !sc.ConfiguredAt.IsZero() && sc.ConfiguredAt.After(slot) {
			return Due{}, false
		}
		if s.cfg.CatchupGrace > 0 && now.Sub(slot) <= s.cfg.CatchupGrace {
			return Due{Schedule: sc, LocalDate: localDate, CaughtUp: true}, true
		}
		return Due{}, false
	default:
		return Due{}, false // still ahead of us on this day
	}
}

// dayMatches applies the day pattern in the LOCAL calendar.
func (s *Scheduler) dayMatches(spec DaysSpec, now time.Time) bool {
	switch {
	case spec.DayOfMonth > 0:
		// D74: the composer accepts 1–31 and a day the month lacks CLAMPS to that
		// month's last day — so "31st of each month" fires on 28/29 Feb and 30
		// Apr rather than silently skipping those months. This is the same
		// short-month clamp the events module applies to monthly recurrence (D19),
		// deliberately consistent so the two features never disagree about what
		// "the 31st" means.
		effective := spec.DayOfMonth
		if last := dates.DaysInMonth(now.Year(), now.Month()); effective > last {
			effective = last
		}
		return now.Day() == effective
	case len(spec.Weekdays) > 0:
		for _, token := range spec.Weekdays {
			if wd, ok := weekdayTokens[token]; ok && wd == now.Weekday() {
				return true
			}
		}
		return false
	case spec.Preset == PresetWeekdays:
		return now.Weekday() >= time.Monday && now.Weekday() <= time.Friday
	case spec.Preset == PresetWeekends:
		return now.Weekday() == time.Saturday || now.Weekday() == time.Sunday
	case spec.Preset == PresetDaily, spec.Preset == "":
		return true
	default:
		return false
	}
}

// sameMinute reports whether two local times fall in the same wall-clock minute.
func sameMinute(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day() &&
		a.Hour() == b.Hour() && a.Minute() == b.Minute()
}

// parseHHMM parses "HH:MM" without pulling in a layout parse (and without
// accepting "8:5" as 08:05).
func parseHHMM(v string) (hh, mm int, ok bool) {
	if len(v) != 5 || v[2] != ':' {
		return 0, 0, false
	}
	for i, c := range v {
		if i == 2 {
			continue
		}
		if c < '0' || c > '9' {
			return 0, 0, false
		}
	}
	hh = int(v[0]-'0')*10 + int(v[1]-'0')
	mm = int(v[3]-'0')*10 + int(v[4]-'0')
	if hh > 23 || mm > 59 {
		return 0, 0, false
	}
	return hh, mm, true
}

// ValidateDays reports whether a days spec is usable. Exported so the admin
// module can reject a bad schedule at SAVE time rather than at fire time.
func ValidateDays(spec DaysSpec) error {
	forms := 0
	if spec.Preset != "" {
		forms++
		if spec.Preset != PresetDaily && spec.Preset != PresetWeekdays && spec.Preset != PresetWeekends {
			return &InvalidDaysError{Reason: "neznámý předvolený rozvrh: " + spec.Preset}
		}
	}
	if len(spec.Weekdays) > 0 {
		forms++
		for _, token := range spec.Weekdays {
			if _, ok := weekdayTokens[token]; !ok {
				return &InvalidDaysError{Reason: "neznámý den v týdnu: " + token}
			}
		}
	}
	if spec.DayOfMonth != 0 {
		forms++
		// 1–31, NOT capped at 28: a day the month lacks clamps to its last day.
		if spec.DayOfMonth < 1 || spec.DayOfMonth > 31 {
			return &InvalidDaysError{Reason: "den v měsíci musí být 1–31"}
		}
	}
	if forms == 0 {
		return &InvalidDaysError{Reason: "rozvrh musí určit, kdy se má opakovat"}
	}
	if forms > 1 {
		return &InvalidDaysError{Reason: "rozvrh smí použít jen jednu formu opakování"}
	}
	return nil
}

// InvalidDaysError describes an unusable day pattern, in Czech, for the composer.
type InvalidDaysError struct{ Reason string }

func (e *InvalidDaysError) Error() string { return e.Reason }

// ValidateTimeLocal reports whether "HH:MM" is well-formed.
func ValidateTimeLocal(v string) bool {
	_, _, ok := parseHHMM(v)
	return ok
}

// Describe renders a schedule as the Czech phrase the admin list shows
// ("Každý den v 8:00"). It lives here because the rules it describes live here —
// a second implementation on the frontend would be a second source of truth.
func Describe(timeLocal string, spec DaysSpec) string {
	when := " v " + trimLeadingZero(timeLocal)
	switch {
	case spec.DayOfMonth > 0:
		return itoa(spec.DayOfMonth) + ". v měsíci" + when
	case len(spec.Weekdays) > 0:
		return "Ve vybrané dny" + when
	case spec.Preset == PresetWeekdays:
		return "Každý všední den" + when
	case spec.Preset == PresetWeekends:
		return "O víkendu" + when
	default:
		return "Každý den" + when
	}
}

func trimLeadingZero(hhmm string) string {
	if len(hhmm) == 5 && hhmm[0] == '0' {
		return hhmm[1:]
	}
	return hhmm
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
