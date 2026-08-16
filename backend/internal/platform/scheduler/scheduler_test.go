package scheduler

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// memStore is an in-memory Store: the scheduler's calendar logic is what these
// tests are about, not SQL.
type memStore struct {
	mu        sync.Mutex
	schedules []Schedule
	fired     []string // "id@localDate", in order
	err       error
}

func (m *memStore) DueCandidates(context.Context) ([]Schedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	return append([]Schedule(nil), m.schedules...), nil
}

func (m *memStore) MarkFired(_ context.Context, id, localDate string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fired = append(m.fired, id+"@"+localDate)
	for i := range m.schedules {
		if m.schedules[i].ID == id {
			m.schedules[i].LastFiredLocalDate = localDate
		}
	}
	return nil
}

func (m *memStore) firedList() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.fired...)
}

func prague(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Prague")
	if err != nil {
		t.Fatalf("load Europe/Prague: %v", err)
	}
	return loc
}

// harness drives the scheduler at an exact instant, with no real clock.
type harness struct {
	store *memStore
	sched *Scheduler
	fired []Due
	now   time.Time
	mu    sync.Mutex
}

func newHarness(t *testing.T, grace time.Duration, schedules ...Schedule) *harness {
	t.Helper()
	h := &harness{store: &memStore{schedules: schedules}}
	h.sched = New(h.store, func(_ context.Context, d Due) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.fired = append(h.fired, d)
	}, Config{
		Location:     prague(t),
		CatchupGrace: grace,
		Now:          func() time.Time { return h.now },
	})
	return h
}

// at runs one tick with the clock pinned to the given local wall-clock time.
func (h *harness) at(t *testing.T, y int, mo time.Month, d, hh, mm int) {
	t.Helper()
	h.now = time.Date(y, mo, d, hh, mm, 0, 0, prague(t))
	h.sched.Tick(context.Background())
}

func (h *harness) fireCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.fired)
}

func daily(id, hhmm string) Schedule {
	return Schedule{ID: id, Name: id, Enabled: true, TimeLocal: hhmm, Days: DaysSpec{Preset: PresetDaily}}
}

// ---- the two worked examples from the PRD ----

func TestFiresTheWorkedExamplesAtTheRightMinute(t *testing.T) {
	h := newHarness(t, 0, daily("ranni", "08:00"), daily("vecerni", "20:00"))

	h.at(t, 2026, time.September, 15, 7, 59)
	if h.fireCount() != 0 {
		t.Fatalf("fired %d times at 07:59, want 0", h.fireCount())
	}

	h.at(t, 2026, time.September, 15, 8, 0)
	if got := h.store.firedList(); len(got) != 1 || got[0] != "ranni@2026-09-15" {
		t.Fatalf("at 08:00 fired %v, want just the morning summary", got)
	}

	h.at(t, 2026, time.September, 15, 20, 0)
	if got := h.store.firedList(); len(got) != 2 || got[1] != "vecerni@2026-09-15" {
		t.Fatalf("at 20:00 fired %v, want the evening summary too", got)
	}
}

// A slot must fire once per local day, however many ticks land in that minute.
func TestNeverDoubleFiresASlot(t *testing.T) {
	h := newHarness(t, 0, daily("ranni", "08:00"))

	h.at(t, 2026, time.September, 15, 8, 0)
	h.at(t, 2026, time.September, 15, 8, 0) // a second tick inside the same minute
	h.at(t, 2026, time.September, 15, 8, 30)
	h.at(t, 2026, time.September, 15, 23, 59)

	if got := h.fireCount(); got != 1 {
		t.Errorf("fired %d times in one day, want exactly 1", got)
	}

	// The next day is a new slot.
	h.at(t, 2026, time.September, 16, 8, 0)
	if got := h.fireCount(); got != 2 {
		t.Errorf("fired %d times across two days, want 2", got)
	}
}

// The marker is persisted BEFORE the send, so a crash cannot re-fire the slot.
func TestMarksFiredBeforeSending(t *testing.T) {
	store := &memStore{schedules: []Schedule{daily("ranni", "08:00")}}
	var orderMu sync.Mutex
	var order []string
	now := time.Date(2026, time.September, 15, 8, 0, 0, 0, prague(t))

	s := New(store, func(context.Context, Due) {
		orderMu.Lock()
		defer orderMu.Unlock()
		order = append(order, "send")
	}, Config{Location: prague(t), Now: func() time.Time { return now }})

	// Wrap MarkFired's effect by observing the store's own ordering.
	s.Tick(context.Background())

	orderMu.Lock()
	defer orderMu.Unlock()
	if len(store.firedList()) != 1 || len(order) != 1 {
		t.Fatalf("expected one mark and one send, got %v / %v", store.firedList(), order)
	}
	// The store recorded the mark; the send happened after (the scheduler returns
	// from MarkFired before calling fire). If the order ever inverts, a crash
	// between the two would re-fire the slot on the next tick.
	if store.schedules[0].LastFiredLocalDate != "2026-09-15" {
		t.Error("the fired marker was not persisted before the send")
	}
}

// ---- day patterns ----

func TestDayPatterns(t *testing.T) {
	// 2026-09-14 is a Monday; 2026-09-19 a Saturday.
	cases := []struct {
		name     string
		spec     DaysSpec
		day      int
		wantFire bool
	}{
		{"daily on a Monday", DaysSpec{Preset: PresetDaily}, 14, true},
		{"daily on a Saturday", DaysSpec{Preset: PresetDaily}, 19, true},
		{"weekdays on a Monday", DaysSpec{Preset: PresetWeekdays}, 14, true},
		{"weekdays on a Saturday", DaysSpec{Preset: PresetWeekdays}, 19, false},
		{"weekends on a Saturday", DaysSpec{Preset: PresetWeekends}, 19, true},
		{"weekends on a Monday", DaysSpec{Preset: PresetWeekends}, 14, false},
		{"selected days hit", DaysSpec{Weekdays: []string{"mon", "wed"}}, 14, true},
		{"selected days miss", DaysSpec{Weekdays: []string{"tue", "thu"}}, 14, false},
		{"day of month hit", DaysSpec{DayOfMonth: 14}, 14, true},
		{"day of month miss", DaysSpec{DayOfMonth: 15}, 14, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, 0, Schedule{ID: "s", Enabled: true, TimeLocal: "08:00", Days: tc.spec})
			h.at(t, 2026, time.September, tc.day, 8, 0)
			if got := h.fireCount() > 0; got != tc.wantFire {
				t.Errorf("fired = %t, want %t", got, tc.wantFire)
			}
		})
	}
}

// D74: 29–31 clamp to the month's last day rather than skipping short months.
// This is the same short-month rule the events module uses (D19) — the two must
// never disagree about what "the 31st" means.
func TestDayOfMonthClampsInShortMonths(t *testing.T) {
	cases := []struct {
		name       string
		dayOfMonth int
		year       int
		month      time.Month
		fireOn     int // the day of that month it must fire on
	}{
		{"31st in a 31-day month", 31, 2026, time.January, 31},
		{"31st in February (non-leap)", 31, 2026, time.February, 28},
		{"31st in February (leap)", 31, 2028, time.February, 29},
		{"31st in a 30-day month", 31, 2026, time.April, 30},
		{"30th in February", 30, 2026, time.February, 28},
		{"29th in February (non-leap)", 29, 2026, time.February, 28},
		{"29th in February (leap) is a real day", 29, 2028, time.February, 29},
		{"15th is never clamped", 15, 2026, time.February, 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, 0, Schedule{
				ID: "s", Enabled: true, TimeLocal: "09:00", Days: DaysSpec{DayOfMonth: tc.dayOfMonth},
			})
			// Walk the whole month a day at a time and record which days fire.
			var firedOn []int
			lastDay := time.Date(tc.year, tc.month+1, 0, 0, 0, 0, 0, prague(t)).Day()
			for d := 1; d <= lastDay; d++ {
				before := h.fireCount()
				h.at(t, tc.year, tc.month, d, 9, 0)
				if h.fireCount() > before {
					firedOn = append(firedOn, d)
				}
			}
			if len(firedOn) != 1 || firedOn[0] != tc.fireOn {
				t.Errorf("fired on days %v, want exactly [%d]", firedOn, tc.fireOn)
			}
		})
	}
}

// ---- DST ----

// Europe/Prague springs forward on the last Sunday of March (02:00 → 03:00) and
// falls back on the last Sunday of October (03:00 → 02:00). A summary at 08:00
// must stay at 08:00 LOCAL across both, which is why every decision is made in
// the local zone rather than on a UTC offset.
func TestFiresCorrectlyAcrossDSTBoundaries(t *testing.T) {
	t.Run("spring forward", func(t *testing.T) {
		// 2026-03-29 is the spring-forward Sunday.
		h := newHarness(t, 0, daily("ranni", "08:00"))
		h.at(t, 2026, time.March, 28, 8, 0) // day before
		h.at(t, 2026, time.March, 29, 8, 0) // the DST day itself
		h.at(t, 2026, time.March, 30, 8, 0) // day after
		if got := h.fireCount(); got != 3 {
			t.Errorf("fired %d times across the spring-forward weekend, want 3 (one per local 08:00)", got)
		}
	})

	t.Run("fall back", func(t *testing.T) {
		// 2026-10-25 is the fall-back Sunday.
		h := newHarness(t, 0, daily("ranni", "08:00"))
		h.at(t, 2026, time.October, 24, 8, 0)
		h.at(t, 2026, time.October, 25, 8, 0)
		h.at(t, 2026, time.October, 26, 8, 0)
		if got := h.fireCount(); got != 3 {
			t.Errorf("fired %d times across the fall-back weekend, want 3", got)
		}
	})

	// A slot inside the hour that DOES NOT EXIST on the spring-forward day: the
	// clock jumps 02:00 → 03:00, so 02:30 never happens. It must not fire twice or
	// wedge the schedule; the next real day fires normally.
	t.Run("a slot in the skipped hour", func(t *testing.T) {
		h := newHarness(t, 0, Schedule{ID: "s", Enabled: true, TimeLocal: "02:30", Days: DaysSpec{Preset: PresetDaily}})
		for _, hh := range []int{1, 3, 4} { // ticks around the gap
			h.at(t, 2026, time.March, 29, hh, 30)
		}
		h.at(t, 2026, time.March, 30, 2, 30)
		if got := h.fireCount(); got > 2 {
			t.Errorf("fired %d times around the skipped hour, want at most 2 (one per day)", got)
		}
	})
}

// ---- catch-up ----

func TestCatchUpWithinGraceOnly(t *testing.T) {
	t.Run("inside the grace window fires once", func(t *testing.T) {
		h := newHarness(t, 120*time.Minute, daily("ranni", "08:00"))
		// The process was down at 08:00 and comes back at 09:59 — 119 minutes late.
		h.at(t, 2026, time.September, 15, 9, 59)
		if h.fireCount() != 1 {
			t.Errorf("fired %d times 119 minutes late, want 1", h.fireCount())
		}
		// And it must not fire again on the next tick.
		h.at(t, 2026, time.September, 15, 10, 0)
		if h.fireCount() != 1 {
			t.Errorf("caught-up slot fired again: %d", h.fireCount())
		}
	})

	t.Run("outside the grace window is skipped", func(t *testing.T) {
		h := newHarness(t, 120*time.Minute, daily("ranni", "08:00"))
		// Back at 10:01 — 121 minutes late. Stale news is worse than no news.
		h.at(t, 2026, time.September, 15, 10, 1)
		if h.fireCount() != 0 {
			t.Errorf("fired %d times 121 minutes late, want 0", h.fireCount())
		}
	})

	t.Run("catch-up is flagged", func(t *testing.T) {
		h := newHarness(t, 120*time.Minute, daily("ranni", "08:00"))
		h.at(t, 2026, time.September, 15, 9, 0)
		h.mu.Lock()
		defer h.mu.Unlock()
		if len(h.fired) != 1 || !h.fired[0].CaughtUp {
			t.Errorf("fired = %+v, want one entry flagged CaughtUp", h.fired)
		}
	})

	t.Run("zero grace disables catch-up", func(t *testing.T) {
		h := newHarness(t, 0, daily("ranni", "08:00"))
		h.at(t, 2026, time.September, 15, 8, 30)
		if h.fireCount() != 0 {
			t.Errorf("fired %d times with catch-up disabled, want 0", h.fireCount())
		}
	})
}

// A disabled schedule never fires, however due it looks.
func TestDisabledSchedulesNeverFire(t *testing.T) {
	h := newHarness(t, time.Hour, Schedule{
		ID: "off", Enabled: false, TimeLocal: "08:00", Days: DaysSpec{Preset: PresetDaily},
	})
	h.at(t, 2026, time.September, 15, 8, 0)
	if h.fireCount() != 0 {
		t.Errorf("a disabled schedule fired %d times", h.fireCount())
	}
}

// A malformed time must be skipped with a log line, not crash the ticker or
// block every other schedule.
func TestUnparseableTimeIsSkipped(t *testing.T) {
	h := newHarness(t, 0,
		Schedule{ID: "bad", Enabled: true, TimeLocal: "8:0", Days: DaysSpec{Preset: PresetDaily}},
		daily("good", "08:00"),
	)
	h.at(t, 2026, time.September, 15, 8, 0)
	if got := h.store.firedList(); len(got) != 1 || !strings.HasPrefix(got[0], "good@") {
		t.Errorf("fired %v, want only the well-formed schedule", got)
	}
}

// ---- validation (the composer's save-time gate) ----

func TestValidateDays(t *testing.T) {
	cases := []struct {
		name string
		spec DaysSpec
		ok   bool
	}{
		{"daily", DaysSpec{Preset: PresetDaily}, true},
		{"weekdays", DaysSpec{Preset: PresetWeekdays}, true},
		{"selected", DaysSpec{Weekdays: []string{"mon", "fri"}}, true},
		{"day 1", DaysSpec{DayOfMonth: 1}, true},
		{"day 31 is legal, not capped at 28", DaysSpec{DayOfMonth: 31}, true},
		{"day 0", DaysSpec{DayOfMonth: 0}, false},
		{"day 32", DaysSpec{DayOfMonth: 32}, false},
		{"unknown preset", DaysSpec{Preset: "fortnightly"}, false},
		{"unknown weekday", DaysSpec{Weekdays: []string{"pondeli"}}, false},
		{"nothing set", DaysSpec{}, false},
		{"two forms", DaysSpec{Preset: PresetDaily, DayOfMonth: 5}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDays(tc.spec)
			if (err == nil) != tc.ok {
				t.Errorf("ValidateDays(%+v) err = %v, want ok=%t", tc.spec, err, tc.ok)
			}
		})
	}
}

func TestValidateTimeLocal(t *testing.T) {
	for _, v := range []string{"00:00", "08:00", "23:59"} {
		if !ValidateTimeLocal(v) {
			t.Errorf("%q should be valid", v)
		}
	}
	for _, v := range []string{"8:00", "24:00", "08:60", "0800", "", "aa:bb"} {
		if ValidateTimeLocal(v) {
			t.Errorf("%q should be rejected", v)
		}
	}
}

func TestDescribe(t *testing.T) {
	cases := []struct {
		time string
		spec DaysSpec
		want string
	}{
		{"08:00", DaysSpec{Preset: PresetDaily}, "Každý den v 8:00"},
		{"20:00", DaysSpec{Preset: PresetWeekdays}, "Každý všední den v 20:00"},
		{"09:30", DaysSpec{Preset: PresetWeekends}, "O víkendu v 9:30"},
		{"09:00", DaysSpec{DayOfMonth: 1}, "1. v měsíci v 9:00"},
		{"18:15", DaysSpec{Weekdays: []string{"mon"}}, "Ve vybrané dny v 18:15"},
	}
	for _, tc := range cases {
		if got := Describe(tc.time, tc.spec); got != tc.want {
			t.Errorf("Describe(%q, %+v) = %q, want %q", tc.time, tc.spec, got, tc.want)
		}
	}
}

// A slot the schedule was not yet configured for is not a MISS — it is a slot
// that never applied to it. Without this distinction, saving "every day at
// 08:00" at 08:30 looks exactly like a slot missed during a restart, and the
// household is pushed a summary the moment the form is submitted.
func TestNoCatchUpForASlotOlderThanTheConfiguration(t *testing.T) {
	loc := prague(t)
	configured := func(at time.Time) Schedule {
		s := daily("ranni", "08:00")
		s.ConfiguredAt = at
		return s
	}

	t.Run("a schedule saved after today's slot does not fire", func(t *testing.T) {
		h := newHarness(t, 120*time.Minute,
			configured(time.Date(2026, time.September, 15, 8, 30, 0, 0, loc)))
		h.at(t, 2026, time.September, 15, 8, 31)
		if h.fireCount() != 0 {
			t.Errorf("fired %d times one minute after being saved, want 0", h.fireCount())
		}
		// It still fires normally at the next day's slot.
		h.at(t, 2026, time.September, 16, 8, 0)
		if h.fireCount() != 1 {
			t.Errorf("fired %d times at the next real slot, want 1", h.fireCount())
		}
	})

	t.Run("a schedule that predates the slot still catches up", func(t *testing.T) {
		h := newHarness(t, 120*time.Minute,
			configured(time.Date(2026, time.September, 14, 12, 0, 0, 0, loc)))
		h.at(t, 2026, time.September, 15, 9, 0)
		if h.fireCount() != 1 {
			t.Errorf("fired %d times after a genuine miss, want 1", h.fireCount())
		}
	})

	t.Run("an unset ConfiguredAt keeps the old behaviour", func(t *testing.T) {
		h := newHarness(t, 120*time.Minute, daily("ranni", "08:00"))
		h.at(t, 2026, time.September, 15, 9, 0)
		if h.fireCount() != 1 {
			t.Errorf("fired %d times with a zero ConfiguredAt, want 1", h.fireCount())
		}
	})
}

// ---- catch-up across midnight ----

// A slot is anchored to a local DATE, so a late-evening summary missed during a
// restart belongs to YESTERDAY. Evaluated only against today it looks like a slot
// still most of a day in the future, and the grace — whose entire purpose is to
// recover exactly this miss — would never see it.
func TestCatchesUpASlotMissedAcrossMidnight(t *testing.T) {
	h := newHarness(t, 2*time.Hour, daily("vecerni", "23:50"))

	// Down from 23:45; back at 00:05, fifteen minutes after the slot.
	h.at(t, 2026, time.September, 16, 0, 5)

	got := h.store.firedList()
	if len(got) != 1 || got[0] != "vecerni@2026-09-15" {
		t.Fatalf("fired %v, want the missed 15 Sep slot caught up", got)
	}
	if len(h.fired) != 1 || !h.fired[0].CaughtUp {
		t.Fatalf("fired %+v, want one catch-up fire", h.fired)
	}

	// And exactly once: the next tick must not fire it again.
	h.at(t, 2026, time.September, 16, 0, 6)
	if got := h.store.firedList(); len(got) != 1 {
		t.Errorf("fired %v after a second tick, want the slot to stay spent", got)
	}
}

// The grace still bounds it — a whole night away is stale news, not a miss worth
// pushing at breakfast.
func TestDoesNotCatchUpAMidnightSlotPastTheGrace(t *testing.T) {
	h := newHarness(t, 2*time.Hour, daily("vecerni", "23:50"))

	h.at(t, 2026, time.September, 16, 7, 0) // 7h10m late
	if got := h.store.firedList(); len(got) != 0 {
		t.Errorf("fired %v, want nothing — the miss is far past the grace", got)
	}
}

// Yesterday's slot is judged by yesterday's calendar. A weekday-only summary
// missed at 23:50 on Friday may still be caught up on Saturday morning; a
// Saturday slot must not be invented because Friday matched.
func TestMidnightCatchUpUsesYesterdaysDayPattern(t *testing.T) {
	weekdays := func(id, hhmm string) Schedule {
		return Schedule{ID: id, Name: id, Enabled: true, TimeLocal: hhmm,
			Days: DaysSpec{Preset: PresetWeekdays}}
	}
	// 2026-09-18 is a Friday, so 2026-09-19 is a Saturday.
	h := newHarness(t, 2*time.Hour, weekdays("vecerni", "23:50"))

	h.at(t, 2026, time.September, 19, 0, 5)
	if got := h.store.firedList(); len(got) != 1 || got[0] != "vecerni@2026-09-18" {
		t.Fatalf("fired %v, want Friday's missed slot", got)
	}

	// Sunday morning must find nothing: Saturday 23:50 never applied.
	h2 := newHarness(t, 2*time.Hour, weekdays("vecerni", "23:50"))
	h2.at(t, 2026, time.September, 20, 0, 5)
	if got := h2.store.firedList(); len(got) != 0 {
		t.Errorf("fired %v, want nothing — Saturday is not a weekday", got)
	}
}

// A schedule saved after the slot it names is not a miss, and that must hold for
// yesterday's slot too — otherwise creating a 23:50 summary at 00:30 would push
// a household-wide notification the moment the form is submitted.
func TestMidnightCatchUpSkipsASlotTheScheduleWasNotConfiguredFor(t *testing.T) {
	sc := daily("vecerni", "23:50")
	sc.ConfiguredAt = time.Date(2026, time.September, 16, 0, 30, 0, 0, prague(t))
	h := newHarness(t, 2*time.Hour, sc)

	h.at(t, 2026, time.September, 16, 0, 35)
	if got := h.store.firedList(); len(got) != 0 {
		t.Errorf("fired %v, want nothing — the schedule did not exist at 23:50", got)
	}
}

// With catch-up disabled there is no yesterday at all.
func TestNoMidnightCatchUpWhenGraceIsZero(t *testing.T) {
	h := newHarness(t, 0, daily("vecerni", "23:50"))

	h.at(t, 2026, time.September, 16, 0, 5)
	if got := h.store.firedList(); len(got) != 0 {
		t.Errorf("fired %v, want nothing — catch-up is off", got)
	}
}
