package events_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/events"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

// The provider config the fixture module runs with. The mirror test builds the
// widget with the SAME numbers, or it would compare two differently-configured
// selections.
const (
	testLookbackDays = 30
	testMaxOcc       = 500 // matches newFixture's Service (events_test.go)
)

func eventLists(t *testing.T, f fixture) lists.Provider {
	t.Helper()
	return events.NewModule(f.svc, pragueLoc(t), testLookbackDays).ListProvider()
}

// asOfDay is the moment a summary resolves its tokens at. The reminder lists
// select on a DATE, so the tests pin one rather than deriving windows from
// time.Now(): "an event today, reminded about yesterday" is the whole point of
// these lists and it cannot be written against a moving today.
func asOfDay(t *testing.T, day string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", day, pragueLoc(t))
	if err != nil {
		t.Fatalf("parse %q: %v", day, err)
	}
	return d.Add(8 * time.Hour) // 08:00, when the morning summary goes out
}

func mustCreate(t *testing.T, f fixture, title, startsOn, rrule, lead string) events.Event {
	t.Helper()
	in := events.EventCreate{Title: title, StartsOn: startsOn, RRule: rrule}
	if lead != "" {
		in.ReminderEnabled = true
		in.ReminderLead = lead
	}
	ev, err := f.svc.CreateEvent(f.ctx, in)
	if err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
	return *ev
}

func items(t *testing.T, p lists.Provider, key string, at time.Time) []string {
	t.Helper()
	got, err := p.Items(context.Background(), "u1", key, at)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return got
}

func TestEventListsDescriptors(t *testing.T) {
	f := newFixture(t)
	got := eventLists(t, f).Descriptors()

	want := map[string]bool{
		events.ListPripominkyToday:     true,
		events.ListPripominkyTodayOpen: true,
		events.ListPripominkyActive:    true,
		events.ListOverdueOpen:         true,
		events.ListDueWithin7d:         true,
	}
	if len(got) != len(want) {
		t.Fatalf("published %d descriptors, want %d", len(got), len(want))
	}
	for _, d := range got {
		if !want[d.Key] {
			t.Errorf("unexpected list key %q", d.Key)
		}
		if d.Scope != lists.ScopeHousehold {
			t.Errorf("%s scope = %q, want household — events completion is shared", d.Key, d.Scope)
		}
		// An empty list must have something to say; the sender's placeholder means
		// "we could not tell you", which is a different message.
		if strings.TrimSpace(d.Empty) == "" {
			t.Errorf("%s published no empty text", d.Key)
		}
	}
}

// "Připomínky na dnešek" are the CURRENT reminders — the widget's non-overdue
// rows: lead open, day not yet passed (D99). The rent due next week whose 1w
// lead opened this morning belongs here, and so does the trash due today whose
// lead opened yesterday — its reminder date is already past (as it is for any
// event created inside its own lead window), but the reminder is still
// today's business. The revision whose lead opens in August is not.
func TestEventListsPripominkyTodayNamesCurrentReminders(t *testing.T) {
	f := newFixture(t)
	mod := events.NewModule(f.svc, pragueLoc(t), testLookbackDays)
	at := asOfDay(t, "2026-07-15")

	mustCreate(t, f, "Zaplatit nájem", "2026-07-22", "", "1w")      // lead opened today
	mustCreate(t, f, "Kontrola kotle", "2026-07-16", "", "1d")      // lead opened today
	mustCreate(t, f, "Vynést koš", "2026-07-15", "", "1d")          // due today, lead opened yesterday — still current
	mustCreate(t, f, "Revize ještě daleko", "2026-08-10", "", "1w") // lead opens 08-03
	mustCreate(t, f, "Bez připomínky", "2026-07-15", "", "")        // not a připomínka at all

	got := items(t, mod.ListProvider(), events.ListPripominkyToday, at)
	want := []string{"Vynést koš (15. 7.)", "Kontrola kotle (16. 7.)", "Zaplatit nájem (22. 7.)"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("items = %v, want %v (soonest first; the event's own date is what each line carries)", got, want)
	}

	// The list names exactly what the metric counts — the whole point of sharing
	// a key between the two catalogs.
	count, err := mod.MetricProvider().Value(context.Background(), "u1", events.MetricPripominkyToday, at)
	if err != nil {
		t.Fatalf("pripominky_today metric: %v", err)
	}
	if count != len(got) {
		t.Errorf("metric says %d, the list names %d — the two must agree", count, len(got))
	}
}

// Completion is shared, so ticking one off shortens the open list for everyone —
// and leaves the plain list alone: completing a current reminder does not unsay
// that it is one of today's.
func TestEventListsPripominkyTodayOpenDropsCompleted(t *testing.T) {
	f := newFixture(t)
	mod := events.NewModule(f.svc, pragueLoc(t), testLookbackDays)
	at := asOfDay(t, "2026-07-15")

	kotel := mustCreate(t, f, "Kontrola kotle", "2026-07-16", "", "1d")
	mustCreate(t, f, "Zaplatit nájem", "2026-07-22", "", "1w")

	if _, err := f.svc.Complete(f.ctx, kotel.ID, "2026-07-16", ""); err != nil {
		t.Fatalf("complete occurrence: %v", err)
	}

	open := items(t, mod.ListProvider(), events.ListPripominkyTodayOpen, at)
	if strings.Join(open, "|") != "Zaplatit nájem (22. 7.)" {
		t.Errorf("open items = %v, want only the uncompleted one", open)
	}
	if all := items(t, mod.ListProvider(), events.ListPripominkyToday, at); len(all) != 2 {
		t.Errorf("plain list = %v, want both — it does not care about completion", all)
	}
}

// The active list IS the Připomínky widget, in words: same rows, same order.
// List and widget resolve through the same selection by construction (D100),
// so the mirror loop below guards the RENDERING plumbing — dates carried
// intact into each line, the widget's order kept — while the hand-written
// expectation at the end is what pins the selection itself.
func TestEventListsActiveMirrorsThePripominkyWidget(t *testing.T) {
	f := newFixture(t)
	mod := events.NewModule(f.svc, pragueLoc(t), testLookbackDays)
	today := dates.New(2026, 7, 15)
	at := asOfDay(t, "2026-07-15")

	mustCreate(t, f, "Propásnutá revize", "2026-07-10", "", "1d") // overdue
	mustCreate(t, f, "Zaplatit nájem", "2026-07-22", "", "1w")    // active (lead open)
	mustCreate(t, f, "Kontrola kotle", "2026-07-30", "", "1d")    // not active yet
	zalit := mustCreate(t, f, "Zalít kytky", "2026-07-01", "FREQ=WEEKLY;INTERVAL=1", "1d")
	if _, err := f.svc.Complete(f.ctx, zalit.ID, "2026-07-01", ""); err != nil {
		t.Fatalf("complete 07-01: %v", err)
	}
	if _, err := f.svc.Complete(f.ctx, zalit.ID, "2026-07-08", ""); err != nil {
		t.Fatalf("complete 07-08: %v", err)
	}

	widget := events.NewPripominkyProviderForTest(events.NewStore(f.db), testLookbackDays, testMaxOcc, func() dates.Date { return today })
	data, err := widget.Data(context.Background(), registry.User{})
	if err != nil {
		t.Fatalf("widget data: %v", err)
	}
	rem := data.(events.PripominkyWidget).Reminders

	got := items(t, mod.ListProvider(), events.ListPripominkyActive, at)
	if len(got) != len(rem) {
		t.Fatalf("list names %d reminders, the widget shows %d: %v vs %+v", len(got), len(rem), got, rem)
	}
	for i, r := range rem {
		on, err := dates.Parse(r.OccurrenceOn)
		if err != nil {
			t.Fatalf("widget occurrence %q: %v", r.OccurrenceOn, err)
		}
		want := r.Title + " (" + czDay(on) + ")"
		if got[i] != want {
			t.Errorf("line %d = %q, want %q — the list must read like the widget, in the widget's order", i, got[i], want)
		}
	}

	// And concretely: overdue first, the not-yet-active one absent, the recurring
	// one advanced past its completions.
	want := []string{"Propásnutá revize (10. 7.)", "Zalít kytky (15. 7.)", "Zaplatit nájem (22. 7.)"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("items = %v, want %v", got, want)
	}
}

// A multi-day window dates every line — "Kontrola kotle" alone says nothing
// about when, and an overdue one has to say how long it has been waiting.
func TestEventListsDateTheirItemsOverAMultiDayWindow(t *testing.T) {
	f := newFixture(t)
	p := eventLists(t, f)
	at := asOfDay(t, "2026-07-15")

	mustCreate(t, f, "Kontrola kotle", "2026-07-18", "", "")
	mustCreate(t, f, "Propásnutá revize", "2026-07-10", "", "1d")

	upcoming := items(t, p, events.ListDueWithin7d, at)
	if len(upcoming) != 1 || upcoming[0] != "Kontrola kotle (18. 7.)" {
		t.Errorf("upcoming = %v, want [Kontrola kotle (18. 7.)]", upcoming)
	}

	overdue := items(t, p, events.ListOverdueOpen, at)
	if len(overdue) != 1 || overdue[0] != "Propásnutá revize (10. 7.)" {
		t.Errorf("overdue = %v, want [Propásnutá revize (10. 7.)]", overdue)
	}
}

// A recurring overdue reminder is ONE line, as it is one row on the dashboard
// and one in the count.
func TestEventListsNameARecurringOverdueReminderOnce(t *testing.T) {
	f := newFixture(t)
	p := eventLists(t, f)
	at := asOfDay(t, "2026-07-15")

	mustCreate(t, f, "Zalít kytky", "2026-06-17", "FREQ=WEEKLY;INTERVAL=1", "1d")

	got := items(t, p, events.ListOverdueOpen, at)
	if len(got) != 1 || !strings.HasPrefix(got[0], "Zalít kytky") {
		t.Errorf("items = %v, want the reminder named once", got)
	}
}

// A month lead is calendar arithmetic with clamping, so the reminder date of a
// 31st is the 28th/29th of the month before — the day the widget starts showing
// it, and so the FIRST day this list names it. It then stays current until its
// day passes.
func TestEventListsPripominkyTodayHandlesAClampedMonthLead(t *testing.T) {
	f := newFixture(t)
	p := eventLists(t, f)

	mustCreate(t, f, "Odečet vody", "2026-03-31", "FREQ=MONTHLY;INTERVAL=1", "1m")

	if got := items(t, p, events.ListPripominkyToday, asOfDay(t, "2026-02-27")); len(got) != 0 {
		t.Errorf("items on 27. 2. = %v, want none — the lead has not opened yet", got)
	}
	if got := items(t, p, events.ListPripominkyToday, asOfDay(t, "2026-02-28")); len(got) != 1 {
		t.Errorf("items on 28. 2. = %v, want the 31. 3. occurrence — 31. 3. − 1m clamps to 28. 2.", got)
	}
	if got := items(t, p, events.ListPripominkyToday, asOfDay(t, "2026-03-15")); len(got) != 1 {
		t.Errorf("items on 15. 3. = %v, want the 31. 3. occurrence — still current until its day passes", got)
	}
}

// Once a reminder turns overdue it leaves "na dnešek" and belongs to
// overdue_open — a summary must name each dashboard row exactly once, never
// the same event twice under two dates (D99).
func TestEventListsPripominkyTodayExcludesOverdueRows(t *testing.T) {
	f := newFixture(t)
	p := eventLists(t, f)
	at := asOfDay(t, "2026-07-15")

	// Weekly with the 8. 7. occurrence left uncompleted: the widget's one row
	// for this event is the overdue 8. 7., even though the 22. 7. occurrence's
	// 1w lead opens today.
	mustCreate(t, f, "Zalít kytky", "2026-07-08", "FREQ=WEEKLY;INTERVAL=1", "1w")

	if got := items(t, p, events.ListPripominkyToday, at); len(got) != 0 {
		t.Errorf("pripominky_today = %v, want none — the event's dashboard row is the overdue one", got)
	}
	if got := items(t, p, events.ListOverdueOpen, at); len(got) != 1 || got[0] != "Zalít kytky (8. 7.)" {
		t.Errorf("overdue_open = %v, want [Zalít kytky (8. 7.)]", got)
	}
}

func TestEventListsUnknownKey(t *testing.T) {
	f := newFixture(t)
	if _, err := eventLists(t, f).Items(context.Background(), "u1", "events.nonsense", time.Now()); err == nil {
		t.Error("expected an error for an unknown list key")
	}
}

// czDay renders the "d. M." suffix the list provider appends.
func czDay(d dates.Date) string { return fmt.Sprintf("%d. %d.", d.D, int(d.M)) }

// A "0d" lead subtracts nothing, so the same-day reminder opens on the event's
// OWN morning. The two neighbours are what pin that: an event tomorrow is not
// active yet and one yesterday is already overdue, so a lead that had drifted by
// a day in either direction would move one of them across the boundary. The
// widget and the "na dnešek" list are both read, because the same-day reminder
// is worth nothing if it reaches the dashboard and not the 08:00 summary.
func TestSameDayLeadOpensOnTheDayItself(t *testing.T) {
	f := newFixture(t)
	mod := events.NewModule(f.svc, pragueLoc(t), testLookbackDays)
	today := dates.New(2026, 7, 15)
	at := asOfDay(t, "2026-07-15")

	mustCreate(t, f, "Vynést koš", "2026-07-15", "", "0d")       // opens this morning
	mustCreate(t, f, "Odvézt kontejner", "2026-07-16", "", "0d") // opens tomorrow — not yet
	mustCreate(t, f, "Zalít kytky", "2026-07-14", "", "0d")      // opened yesterday — overdue

	widget := events.NewPripominkyProviderForTest(events.NewStore(f.db), testLookbackDays, testMaxOcc, func() dates.Date { return today })
	data, err := widget.Data(context.Background(), registry.User{})
	if err != nil {
		t.Fatalf("widget data: %v", err)
	}
	rem := data.(events.PripominkyWidget).Reminders

	if len(rem) != 2 {
		t.Fatalf("widget rows = %+v, want 2 (tomorrow's same-day reminder is not active yet)", rem)
	}
	if rem[0].Title != "Zalít kytky" || !rem[0].Overdue {
		t.Errorf("first row = %+v, want yesterday's overdue", rem[0])
	}
	if rem[1].Title != "Vynést koš" || rem[1].Overdue || rem[1].DaysUntil != 0 || rem[1].ReminderLead != "0d" {
		t.Errorf("second row = %+v, want today's 0d reminder, not overdue, days_until 0", rem[1])
	}

	got := items(t, mod.ListProvider(), events.ListPripominkyToday, at)
	if strings.Join(got, "|") != "Vynést koš (15. 7.)" {
		t.Errorf("pripominky_today = %v, want only today's same-day reminder", got)
	}
}
