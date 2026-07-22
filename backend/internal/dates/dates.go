// Package dates provides a civil (all-day, no time-of-day) Date type. Events are
// all-day (PRD D18), so there is no clock-time DST hazard — but "today", month
// boundaries, and lead-time arithmetic must still be evaluated in the configured
// timezone, never UTC, or occurrences flip a day around midnight. This type keeps
// all arithmetic in calendar space and only touches time.Time at the boundaries.
package dates

import (
	"fmt"
	"time"
)

// Date is a timezone-independent calendar date.
type Date struct {
	Y int
	M time.Month
	D int
}

// New constructs a Date.
func New(y int, m time.Month, d int) Date { return Date{Y: y, M: m, D: d} }

// Parse reads a 'YYYY-MM-DD' date.
func Parse(s string) (Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return Date{}, fmt.Errorf("invalid date %q: %w", s, err)
	}
	return Date{t.Year(), t.Month(), t.Day()}, nil
}

// String renders the date as 'YYYY-MM-DD'.
func (d Date) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Y, int(d.M), d.D) }

func (d Date) toTime() time.Time { return time.Date(d.Y, d.M, d.D, 0, 0, 0, 0, time.UTC) }

// Compare returns -1, 0, or 1.
func (d Date) Compare(o Date) int {
	switch {
	case d.Y != o.Y:
		return sign(d.Y - o.Y)
	case d.M != o.M:
		return sign(int(d.M) - int(o.M))
	default:
		return sign(d.D - o.D)
	}
}

func (d Date) Before(o Date) bool { return d.Compare(o) < 0 }
func (d Date) After(o Date) bool  { return d.Compare(o) > 0 }
func (d Date) Equal(o Date) bool  { return d.Compare(o) == 0 }

// AddDays returns the date n days later (n may be negative).
func (d Date) AddDays(n int) Date {
	t := d.toTime().AddDate(0, 0, n)
	return Date{t.Year(), t.Month(), t.Day()}
}

// DaysUntil returns the number of whole days from d to o (negative if o is before d).
func (d Date) DaysUntil(o Date) int {
	return int(o.toTime().Sub(d.toTime()).Hours() / 24)
}

// Today returns the current date in loc.
func Today(loc *time.Location) Date {
	now := time.Now().In(loc)
	return Date{now.Year(), now.Month(), now.Day()}
}

// DaysInMonth returns the number of days in month m of year y (leap-aware).
func DaysInMonth(y int, m time.Month) int {
	// Day 0 of the next month is the last day of this month.
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}
