package garden

import (
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
)

// TIMING IS ANCHORED, NOT DATED (PRD D102). Build this first — every planned
// date in the system comes out of this file, and it has no dependencies and no
// database.
//
// A knowledge-base window says WHEN relative to something, not WHICH DAY:
//
//   - `week`         — an ISO week of the year, which is how Czech garden
//                      literature states a direct sowing ("výsev: týden 10–13").
//   - `last_frost`   — days relative to the season's last spring frost.
//   - `first_frost`  — days relative to the season's first autumn frost.
//
// Anchors are mixable per window and per crop, and that mixing is the point:
// when a season's frost dates move, frost-anchored windows move and
// week-anchored ones do not — which is the intent in both cases.

// Anchor is what a window's from/to are measured against.
type Anchor = string

const (
	AnchorWeek       Anchor = "week"
	AnchorLastFrost  Anchor = "last_frost"
	AnchorFirstFrost Anchor = "first_frost"
)

// Window is one knowledge-base timing window.
type Window struct {
	Anchor Anchor `json:"anchor"`
	// From is an ISO week (1–53) under AnchorWeek, or a day offset under either
	// frost anchor — negative meaning BEFORE the anchor, which is the common
	// case ("6–8 týdnů před posledním mrazem" is {last_frost, -56, -42}).
	From int `json:"from"`
	To   int `json:"to"`
}

// SeasonAnchors carries the dates a season resolves its windows against. The
// frost dates are pointers because a season legitimately may not have set them
// yet, and a window that needs one then resolves to nothing rather than to a
// guess.
type SeasonAnchors struct {
	Year       int
	LastFrost  *dates.Date
	FirstFrost *dates.Date
}

// Resolve returns the window's real dates for one season, or ok=false when the
// anchor it needs is missing.
//
// A caller that gets ok=false LEAVES THE DATE UNSET. It never guesses, never
// falls back to a default frost date, and never substitutes today — an unset
// planned date is a visible gap the planner can prompt about, whereas a plausible
// wrong date is a sowing that happens two weeks early and nobody knows why.
//
// A window resolves against the SEASON, never against time.Now(). A planting must
// resolve identically whenever the page is loaded; a generator that consults the
// clock produces a plan that changes while you read it.
func (w Window) Resolve(a SeasonAnchors) (from, to dates.Date, ok bool) {
	switch w.Anchor {
	case AnchorWeek:
		if a.Year == 0 || w.From < 1 || w.From > 53 || w.To < 1 || w.To > 53 {
			return dates.Date{}, dates.Date{}, false
		}
		return isoWeekMonday(a.Year, w.From), isoWeekSunday(a.Year, w.To), true

	case AnchorLastFrost:
		if a.LastFrost == nil {
			return dates.Date{}, dates.Date{}, false
		}
		return a.LastFrost.AddDays(w.From), a.LastFrost.AddDays(w.To), true

	case AnchorFirstFrost:
		if a.FirstFrost == nil {
			return dates.Date{}, dates.Date{}, false
		}
		return a.FirstFrost.AddDays(w.From), a.FirstFrost.AddDays(w.To), true

	default:
		return dates.Date{}, dates.Date{}, false
	}
}

// Validate reports whether the window is well-formed, as a Czech message for the
// 422. Range inversion is caught HERE, at write time, rather than sorted out
// during resolution: a resolver that quietly swaps an inverted range hides a
// typo that the person entering it would have fixed in one second.
func (w Window) Validate(field string) error {
	switch w.Anchor {
	case AnchorWeek:
		if w.From < 1 || w.From > 53 || w.To < 1 || w.To > 53 {
			return errUnprocessable(field + ": týden musí být v rozsahu 1–53.")
		}
	case AnchorLastFrost, AnchorFirstFrost:
		// Offsets are unbounded in principle but a year's worth either way is
		// certainly a typo rather than an intention.
		if w.From < -366 || w.From > 366 || w.To < -366 || w.To > 366 {
			return errUnprocessable(field + ": posun vůči mrazu musí být v rozsahu −366 až 366 dní.")
		}
	default:
		return errUnprocessable(field + ": neznámá kotva termínu.")
	}
	if w.To < w.From {
		return errUnprocessable(field + ": konec okna nemůže být dřív než začátek.")
	}
	return nil
}

// isoWeekMonday returns the Monday of ISO week `week` in `year`.
//
// A year with NO ISO week 53 clamps to the Monday of week 52. That is the same
// species of deviation as D19's short-month clamp, taken for the same reason:
// the alternative is a date that silently lands in January of the FOLLOWING
// year, which is a sowing task on the wrong side of the season boundary.
func isoWeekMonday(year, week int) dates.Date {
	if week > weeksInISOYear(year) {
		week = weeksInISOYear(year)
	}
	// 4 January is always in ISO week 1, in every year, by definition. Walk back
	// to its Monday and step forward whole weeks from there.
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
	offset := (int(jan4.Weekday()) + 6) % 7 // Monday = 0 … Sunday = 6
	monday := jan4.AddDate(0, 0, -offset+(week-1)*7)
	return dates.New(monday.Year(), monday.Month(), monday.Day())
}

// isoWeekSunday returns the Sunday that closes ISO week `week` in `year`.
func isoWeekSunday(year, week int) dates.Date {
	return isoWeekMonday(year, week).AddDays(6)
}

// weeksInISOYear returns 52 or 53. Computed rather than table-looked-up: ask
// whether the Monday of a hypothetical week 53 still belongs to this ISO year.
func weeksInISOYear(year int) int {
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
	offset := (int(jan4.Weekday()) + 6) % 7
	week53Monday := jan4.AddDate(0, 0, -offset+52*7)
	if y, _ := week53Monday.ISOWeek(); y == year {
		return 53
	}
	return 52
}

// isoWeekOf returns the ISO year and week a date falls in — the inverse
// direction, used by check C5's workload buckets and by the calendar's grouping.
func isoWeekOf(d dates.Date) (year, week int) {
	return time.Date(d.Y, d.M, d.D, 0, 0, 0, 0, time.UTC).ISOWeek()
}
