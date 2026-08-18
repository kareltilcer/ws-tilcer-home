package finance

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// monthPattern is the wire format for a month key, matching the OpenAPI schema.
var monthPattern = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// czechMonths are the nominative Czech month names, matching what the frontend's
// monthLabel() renders through Intl ("srpen 2026"). They appear in audit
// summaries — which a v5 trigger rule uses verbatim as its notification body
// (D55) — and in the finance.missing_months list items, so the wording must be
// the sentence you would want to receive on your phone.
var czechMonths = [12]string{
	"leden", "únor", "březen", "duben", "květen", "červen",
	"červenec", "srpen", "září", "říjen", "listopad", "prosinec",
}

// monthLabel renders a YYYY-MM key as "srpen 2026". An unparseable key falls
// back to itself rather than to a wrong month.
func monthLabel(ym string) string {
	y, m, ok := parseMonth(ym)
	if !ok {
		return ym
	}
	return fmt.Sprintf("%s %d", czechMonths[m-1], y)
}

// parseMonth splits a YYYY-MM key. ok is false for anything that does not match
// monthPattern, so callers never index czechMonths out of range.
func parseMonth(ym string) (year, month int, ok bool) {
	if !monthPattern.MatchString(ym) {
		return 0, 0, false
	}
	y, err := strconv.Atoi(ym[:4])
	if err != nil {
		return 0, 0, false
	}
	m, err := strconv.Atoi(ym[5:7])
	if err != nil {
		return 0, 0, false
	}
	return y, m, true
}

// monthIndex maps a YYYY-MM key onto a single ordinal so month arithmetic and
// comparison are plain integer work — no date parsing, no timezone, no
// short-month traps.
func monthIndex(ym string) (int, bool) {
	y, m, ok := parseMonth(ym)
	if !ok {
		return 0, false
	}
	return y*12 + (m - 1), true
}

// monthKey is monthIndex's inverse.
func monthKey(idx int) string {
	return fmt.Sprintf("%04d-%02d", idx/12, idx%12+1)
}

// currentMonth formats asOf as a YYYY-MM key in the configured location. Every
// caller passes the asOf it was handed rather than calling time.Now(), so a
// scheduled summary firing at 00:01 and the widget the household then opens
// cannot disagree about which month "this month" is.
func currentMonth(asOf time.Time, loc *time.Location) string {
	if loc != nil {
		asOf = asOf.In(loc)
	}
	return asOf.Format("2006-01")
}

// maxMissingLookback bounds how far back a gap range may start, in months.
//
// The lists.Provider contract requires a BOUNDED result, and without a floor one
// mistyped year on a single row ("1926-06" for "2026-06") would make every
// resolve walk — and every scheduled summary body derive from — a thousand-odd
// months. Three years also matches the window the add form offers, so a month
// this names is always one the UI can still fill.
const maxMissingLookback = 36

// missingMonths returns the months from the EARLIEST recorded month — or from
// maxMissingLookback before `current`, whichever is later — through `current`
// that have no row, in ascending order.
//
// It is the single selection behind both the finance.missing_months metric and
// the list of the same key: the metric is len() of this, the list is its labels.
// Sharing the computation (over the same store.RecordedMonths read) is what
// makes the number and the items agree by construction — D77's rule.
//
// With nothing recorded at all the answer is empty, not "every month since the
// dawn of time": there is no earliest month to count from, and a brand-new
// household should not be told it is 400 months behind.
func missingMonths(recorded []string, current string) []string {
	if len(recorded) == 0 {
		return nil
	}
	have := make(map[int]bool, len(recorded))
	first := 0
	// Keyed off whether anything has PARSED, not off the slice index: an
	// unparseable first element (the case the continue above exists for) would
	// otherwise leave first at 0 and make the range start at year 0.
	seen := false
	for _, ym := range recorded {
		idx, ok := monthIndex(ym)
		if !ok {
			continue
		}
		have[idx] = true
		if !seen || idx < first {
			first = idx
			seen = true
		}
	}
	last, ok := monthIndex(current)
	if !ok || len(have) == 0 {
		return nil
	}
	// The floor, so a single bad month key cannot stretch the range over
	// centuries (see maxMissingLookback).
	if floor := last - maxMissingLookback; first < floor {
		first = floor
	}
	// A month recorded ahead of "now" (someone entered next month early) must not
	// make the range run backwards.
	if last < first {
		return nil
	}
	var out []string
	for idx := first; idx <= last; idx++ {
		if !have[idx] {
			out = append(out, monthKey(idx))
		}
	}
	return out
}
