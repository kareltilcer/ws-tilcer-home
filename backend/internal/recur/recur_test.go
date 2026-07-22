package recur_test

import (
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/recur"
)

func d(y int, m time.Month, day int) dates.Date { return dates.New(y, m, day) }

func expandStrs(anchor dates.Date, rrule string, from, to dates.Date, cap int) ([]string, error) {
	rule, err := recur.Parse(rrule)
	if err != nil {
		return nil, err
	}
	occ := recur.Expand(anchor, rule, from, to, cap)
	out := make([]string, len(occ))
	for i, o := range occ {
		out[i] = o.String()
	}
	return out, nil
}

func eq(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// ---- The clamping matrix (D19) — the priority tests ----

func TestClamp_Monthly31_CommonYear(t *testing.T) {
	got, err := expandStrs(d(2026, 1, 31), "FREQ=MONTHLY;INTERVAL=1", d(2026, 1, 1), d(2026, 5, 31), 500)
	if err != nil {
		t.Fatal(err)
	}
	// 31 Jan → 28 Feb (2026 is common), 31 Mar, 30 Apr, 31 May. NOT a skipped Feb.
	eq(t, got, "2026-01-31", "2026-02-28", "2026-03-31", "2026-04-30", "2026-05-31")
}

func TestClamp_Monthly31_LeapFebruary(t *testing.T) {
	got, _ := expandStrs(d(2024, 1, 31), "FREQ=MONTHLY;INTERVAL=1", d(2024, 2, 1), d(2024, 2, 29), 500)
	eq(t, got, "2024-02-29") // leap year → 29 Feb, not skipped
}

func TestClamp_Yearly29Feb(t *testing.T) {
	got, _ := expandStrs(d(2024, 2, 29), "FREQ=YEARLY;INTERVAL=1", d(2024, 1, 1), d(2027, 12, 31), 500)
	// 29 Feb → 28 Feb in non-leap years, 29 Feb in leap years.
	eq(t, got, "2024-02-29", "2025-02-28", "2026-02-28", "2027-02-28")
}

func TestClamp_Monthly30th(t *testing.T) {
	got, _ := expandStrs(d(2026, 1, 30), "FREQ=MONTHLY;INTERVAL=1", d(2026, 1, 1), d(2026, 4, 30), 500)
	eq(t, got, "2026-01-30", "2026-02-28", "2026-03-30", "2026-04-30")
}

// ---- Expansion mechanics ----

func TestWeeklyExpansion(t *testing.T) {
	got, _ := expandStrs(d(2026, 7, 1), "FREQ=WEEKLY", d(2026, 7, 1), d(2026, 7, 31), 500)
	eq(t, got, "2026-07-01", "2026-07-08", "2026-07-15", "2026-07-22", "2026-07-29")
}

func TestUntilTerminates(t *testing.T) {
	got, _ := expandStrs(d(2026, 1, 15), "FREQ=MONTHLY;INTERVAL=1;UNTIL=20260331", d(2026, 1, 1), d(2026, 12, 31), 500)
	eq(t, got, "2026-01-15", "2026-02-15", "2026-03-15")
}

func TestOneOff(t *testing.T) {
	// In window.
	got, _ := expandStrs(d(2026, 7, 10), "", d(2026, 7, 1), d(2026, 7, 31), 500)
	eq(t, got, "2026-07-10")
	// Out of window.
	got, _ = expandStrs(d(2026, 6, 10), "", d(2026, 7, 1), d(2026, 7, 31), 500)
	eq(t, got)
}

func TestCapBoundsOpenEnded(t *testing.T) {
	got, _ := expandStrs(d(2026, 1, 1), "FREQ=WEEKLY", d(2026, 1, 1), d(2030, 1, 1), 10)
	if len(got) != 10 {
		t.Fatalf("cap not enforced: got %d occurrences, want 10", len(got))
	}
}

func TestWindowStartsNearFrom_NotAnchor(t *testing.T) {
	// Anchor years in the past; a one-month window yields just that month's occurrence.
	got, _ := expandStrs(d(2020, 1, 15), "FREQ=MONTHLY;INTERVAL=1", d(2026, 7, 1), d(2026, 7, 31), 500)
	eq(t, got, "2026-07-15")
}

func TestParseRejectsUnsupported(t *testing.T) {
	for _, bad := range []string{
		"FREQ=DAILY",
		"FREQ=MONTHLY;INTERVAL=2",
		"FREQ=WEEKLY;BYDAY=MO",
		"FREQ=WEEKLY;COUNT=5",
		"INTERVAL=1",
		"garbage",
	} {
		if _, err := recur.Parse(bad); err == nil {
			t.Errorf("Parse(%q) should have failed", bad)
		}
	}
	// Empty ⇒ one-off (nil rule, no error).
	if r, err := recur.Parse(""); err != nil || r != nil {
		t.Errorf("Parse(\"\") = %v, %v; want nil, nil", r, err)
	}
}

func TestRoundTripString(t *testing.T) {
	for _, s := range []string{"FREQ=WEEKLY;INTERVAL=1", "FREQ=MONTHLY;INTERVAL=1", "FREQ=YEARLY;INTERVAL=1;UNTIL=20301231"} {
		r, err := recur.Parse(s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		if r.String() != s {
			t.Errorf("round-trip %q → %q", s, r.String())
		}
	}
}

func TestIsOccurrence(t *testing.T) {
	rule, _ := recur.Parse("FREQ=MONTHLY;INTERVAL=1")
	anchor := d(2026, 1, 31)
	if !recur.IsOccurrence(anchor, rule, d(2026, 2, 28)) {
		t.Error("28 Feb should be the clamped occurrence of a 31st monthly series")
	}
	if recur.IsOccurrence(anchor, rule, d(2026, 2, 27)) {
		t.Error("27 Feb is not an occurrence")
	}
	if !recur.IsOccurrence(anchor, rule, d(2026, 3, 31)) {
		t.Error("31 Mar should be an occurrence")
	}

	weekly, _ := recur.Parse("FREQ=WEEKLY")
	wa := d(2026, 7, 1)
	if !recur.IsOccurrence(wa, weekly, d(2026, 7, 15)) {
		t.Error("15 Jul is +14d from the anchor — an occurrence")
	}
	if recur.IsOccurrence(wa, weekly, d(2026, 7, 16)) {
		t.Error("16 Jul is off-cadence")
	}
}
