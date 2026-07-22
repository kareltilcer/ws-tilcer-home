// Package recur expands the RFC 5545 RRULE subset the events module supports
// (PRD D13): FREQ=WEEKLY|MONTHLY|YEARLY, INTERVAL=1, optional UNTIL. It hides the
// rule handling behind a small interface so the library choice never leaks into
// handlers, and it owns the ONE place we deliberately depart from RFC 5545:
// short-month clamping (D19).
package recur

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/dates"
)

// Freq is the recurrence frequency.
type Freq int

const (
	Weekly Freq = iota
	Monthly
	Yearly
)

// Rule is a parsed recurrence. INTERVAL is always 1 in v1, so it is not stored.
type Rule struct {
	Freq  Freq
	Until *dates.Date // inclusive series end; nil = open-ended
}

// ErrUnsupported is returned for any RRULE outside the whitelisted subset.
var ErrUnsupported = errors.New("unsupported or malformed rrule")

// Parse validates and parses an RRULE string. An empty string means a one-off
// event and returns (nil, nil). Anything outside the subset is ErrUnsupported —
// we never accept arbitrary RRULE text.
func Parse(rrule string) (*Rule, error) {
	if strings.TrimSpace(rrule) == "" {
		return nil, nil
	}
	r := &Rule{}
	freqSet := false
	for _, part := range strings.Split(rrule, ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return nil, ErrUnsupported
		}
		key := strings.ToUpper(strings.TrimSpace(kv[0]))
		val := strings.TrimSpace(kv[1])
		switch key {
		case "FREQ":
			switch strings.ToUpper(val) {
			case "WEEKLY":
				r.Freq = Weekly
			case "MONTHLY":
				r.Freq = Monthly
			case "YEARLY":
				r.Freq = Yearly
			default:
				return nil, ErrUnsupported
			}
			freqSet = true
		case "INTERVAL":
			if val != "1" {
				return nil, ErrUnsupported // v1 supports only INTERVAL=1
			}
		case "UNTIL":
			u, err := parseUntil(val)
			if err != nil {
				return nil, ErrUnsupported
			}
			r.Until = &u
		default:
			return nil, ErrUnsupported // reject BYDAY, COUNT, EXDATE, etc.
		}
	}
	if !freqSet {
		return nil, ErrUnsupported
	}
	return r, nil
}

// String renders the canonical RRULE for storage. Empty for a one-off (nil rule).
func (r *Rule) String() string {
	if r == nil {
		return ""
	}
	var freq string
	switch r.Freq {
	case Weekly:
		freq = "WEEKLY"
	case Monthly:
		freq = "MONTHLY"
	case Yearly:
		freq = "YEARLY"
	}
	s := "FREQ=" + freq + ";INTERVAL=1"
	if r.Until != nil {
		s += fmt.Sprintf(";UNTIL=%04d%02d%02d", r.Until.Y, int(r.Until.M), r.Until.D)
	}
	return s
}

// Expand returns the occurrence dates of a series within [from, to] inclusive,
// capped at maxN (<=0 means "window-bounded only"). anchor is starts_on. The
// window is the caller's responsibility to bound (handlers reject over-wide
// windows before calling); Expand additionally caps to guard open-ended series.
func Expand(anchor dates.Date, rule *Rule, from, to dates.Date, maxN int) []dates.Date {
	if to.Before(from) {
		return nil
	}
	// One-off: the single date, only if it lands in the window.
	if rule == nil {
		if !anchor.Before(from) && !anchor.After(to) {
			return []dates.Date{anchor}
		}
		return nil
	}

	var out []dates.Date
	appendIf := func(d dates.Date) bool {
		if rule.Until != nil && d.After(*rule.Until) {
			return false // series ended
		}
		if d.After(to) {
			return false
		}
		if !d.Before(from) {
			out = append(out, d)
		}
		return maxN <= 0 || len(out) < maxN
	}

	switch rule.Freq {
	case Weekly:
		cur := firstWeekly(anchor, from)
		for cur.Compare(anchor) >= 0 {
			if !appendIf(cur) {
				break
			}
			cur = cur.AddDays(7)
		}
	case Monthly:
		occ := func(i int) dates.Date { return monthlyOccurrence(anchor, i) }
		i := firstMonthlyIndex(anchor, from)
		for {
			if !appendIf(occ(i)) {
				break
			}
			i++
		}
	case Yearly:
		occ := func(y int) dates.Date { return yearlyOccurrence(anchor, y) }
		y := anchor.Y
		if from.Y > y {
			y = from.Y
		}
		for {
			if !appendIf(occ(y)) {
				break
			}
			y++
		}
	}
	return out
}

// IsOccurrence reports whether day is a real occurrence of the series. It reuses
// Expand over the single-day window so the clamping logic is identical.
func IsOccurrence(anchor dates.Date, rule *Rule, day dates.Date) bool {
	occ := Expand(anchor, rule, day, day, 2)
	return len(occ) == 1 && occ[0].Equal(day)
}

// firstWeekly returns the earliest weekly occurrence (anchor + 7k) that is >= from.
func firstWeekly(anchor, from dates.Date) dates.Date {
	if !from.After(anchor) {
		return anchor
	}
	n := anchor.DaysUntil(from) / 7
	cur := anchor.AddDays(n * 7)
	for cur.Before(from) {
		cur = cur.AddDays(7)
	}
	return cur
}

// monthlyOccurrence returns the i-th monthly occurrence from the anchor, with the
// D19 clamp applied.
func monthlyOccurrence(anchor dates.Date, i int) dates.Date {
	idx := anchor.Y*12 + int(anchor.M) - 1 + i
	y := idx / 12
	m := time.Month(idx%12 + 1)
	return clampDay(y, m, anchor.D)
}

// firstMonthlyIndex returns the smallest i such that the i-th monthly occurrence
// is >= from (and >= 0, never before the anchor).
func firstMonthlyIndex(anchor, from dates.Date) int {
	i := (from.Y-anchor.Y)*12 + (int(from.M) - int(anchor.M))
	if i < 0 {
		i = 0
	}
	for i > 0 && !monthlyOccurrence(anchor, i-1).Before(from) {
		i--
	}
	for monthlyOccurrence(anchor, i).Before(from) {
		i++
	}
	return i
}

// yearlyOccurrence returns the occurrence in year y, with the D19 clamp (so a
// 29-Feb anchor yields 28 Feb in non-leap years).
func yearlyOccurrence(anchor dates.Date, y int) dates.Date {
	return clampDay(y, anchor.M, anchor.D)
}

// clampDay is the DELIBERATE DEVIATION FROM RFC 5545 (PRD D19): when the anchor
// day-of-month does not exist in the target month (31st in a 30-day month, 29
// Feb in a common year), RFC 5545 SKIPS that month. For household use ("zaplatit
// 31.") a silently skipped month is a bug, so we CLAMP to the last day of the
// month instead. Do not "fix" this back to RFC behaviour.
func clampDay(y int, m time.Month, anchorDay int) dates.Date {
	last := dates.DaysInMonth(y, m)
	if anchorDay < last {
		return dates.New(y, m, anchorDay)
	}
	return dates.New(y, m, last)
}

func parseUntil(s string) (dates.Date, error) {
	if len(s) < 8 {
		return dates.Date{}, ErrUnsupported
	}
	t, err := time.Parse("20060102", s[:8]) // RFC5545 UNTIL date portion
	if err != nil {
		return dates.Date{}, err
	}
	return dates.New(t.Year(), t.Month(), t.Day()), nil
}
