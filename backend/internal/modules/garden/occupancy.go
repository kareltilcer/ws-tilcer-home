package garden

import "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"

// WHAT "SHARES A BED" MEANS (PRD D107).
//
// This is the single most important piece of logic in the check engine, and it
// is eight lines.
//
// The obvious reading of "two crops share a bed" is "both are in bed A1 this
// year". It is also wrong, and wrong in the way that quietly destroys the
// feature: spring špenát and autumn pórek in one bed NEVER MEET, so a companion
// check that flags them is a check nobody reads by April. Once the panel is
// noise it stays noise, and the module's whole value is the warning nobody has to
// look for.
//
// So occupancy is a derived WINDOW — first of (sowed, sown direct, transplanted)
// through cleared-or-harvest-end — and every bed-level rule joins on overlap of
// those windows, not on calendar-year membership.

// OccupancyWindow returns the window this planting holds its bed for.
//
// The FROM side is the day the crop reaches THE BED, and on each route the
// actual date beats the planned one: once a crop is really in the ground, that
// is when it started occupying the bed, whatever February thought. The TO side
// prefers the actual clearing date for the mirror reason.
//
// NO INDOOR DATE EVER DATES A BED. `sowed_on` records whichever sowing the plan
// called for — including the one into a tray (see ComputeDrift, which compares
// it against sow_indoor_on) — so it counts only when the plan has no indoor
// sowing and it therefore really is the direct sowing. Otherwise a seedling
// raised on a windowsill in March would occupy its bed from March: it would
// collide with the spring crop still standing there and be named in that
// month's frost warning while it is indoors.
//
// Either bound may be nil, meaning open-ended — see Overlap for why that
// direction is deliberate.
func (p Planting) OccupancyWindow() (from, to *dates.Date) {
	sowedIntoBed := p.SowedOn
	if p.SowIndoorOn != nil {
		sowedIntoBed = nil // sowed_on names the tray, not the bed
	}
	from = firstDate(sowedIntoBed, p.SowDirectOn, p.TransplantedOn, p.TransplantOn)
	if from == nil {
		// A permanent (a tree, a rhubarb crown) is in the ground from the day it
		// was planted and does not leave — so planted_on is its start and it has
		// no end until it is removed (D106: one table, one code path).
		from = parseDatePtr(p.PlantedOn)
	}
	to = firstDate(p.ClearedOn, p.RemovedOn, p.HarvestTo)
	return from, to
}

// OccupancyWire renders the derived window for the API.
func (p Planting) OccupancyWire() Occupancy {
	from, to := p.OccupancyWindow()
	var out Occupancy
	if from != nil {
		out.From = sp(from.String())
	}
	if to != nil {
		out.To = sp(to.String())
	}
	return out
}

// Overlap reports whether two plantings occupy their bed at the same time.
//
// Half-open at the end so a crop cleared on the very day the next is sown does
// not count as sharing the bed — that is a succession, not a conflict, and it is
// the ordinary way a bed is used twice in a year.
//
// A NIL BOUND IS OPEN-ENDED, which makes a planting with no resolvable dates
// overlap everything in its bed. That is the conservative direction on purpose:
// an unplanned planting should provoke a question, not silence. The failure mode
// we can live with is one warning too many on a half-entered plan; the one we
// cannot is a companion conflict that never fired because nobody had typed a date
// yet.
func Overlap(a, b Planting) bool {
	aFrom, aTo := a.OccupancyWindow()
	bFrom, bTo := b.OccupancyWindow()
	return windowsOverlap(aFrom, aTo, bFrom, bTo)
}

func windowsOverlap(aFrom, aTo, bFrom, bTo *dates.Date) bool {
	// a starts after b ends ⇒ disjoint.
	if aFrom != nil && bTo != nil && !aFrom.Before(*bTo) {
		return false
	}
	// b starts after a ends ⇒ disjoint.
	if bFrom != nil && aTo != nil && !bFrom.Before(*aTo) {
		return false
	}
	return true
}

// occupancyStarts returns the instants worth sampling a bed at: the day each
// planting takes it, plus a nil instant standing for "before any dated planting
// starts", so a bed holding only un-dated plantings is still measured.
//
// Deduplicated and in the order ps arrives (sorted by id), so the warning a
// sample produces is stable across recomputation.
func occupancyStarts(ps []Planting) []*dates.Date {
	var out []*dates.Date
	seen := map[string]bool{}
	undated := false
	for _, p := range ps {
		from, _ := p.OccupancyWindow()
		if from == nil {
			undated = true
			continue
		}
		if key := from.String(); !seen[key] {
			seen[key] = true
			out = append(out, from)
		}
	}
	if undated {
		out = append(out, nil)
	}
	return out
}

// occupiesAt reports whether p holds its bed at one instant — nil meaning the
// open-ended instant before any dated planting starts.
//
// Half-open at the end, exactly like Overlap, so a crop cleared on the day the
// next is sown is a succession rather than a collision. A planting with no start
// date is open-ended at the front and so occupies every instant, which keeps the
// conservative direction Overlap already takes: an unplanned planting should
// provoke a question, not silence.
func occupiesAt(p Planting, at *dates.Date) bool {
	from, to := p.OccupancyWindow()
	if at == nil {
		return from == nil
	}
	if from != nil && at.Before(*from) {
		return false
	}
	if to != nil && !at.Before(*to) {
		return false
	}
	return true
}

// OccupiesOn reports whether the planting holds its bed on day d — used by the
// frost evaluation ("is anything sensitive in the ground tonight") and by the
// pest-check task generator.
func (p Planting) OccupiesOn(d dates.Date) bool {
	from, to := p.OccupancyWindow()
	if from != nil && d.Before(*from) {
		return false
	}
	// The end bound is FIELD-AWARE: cleared/removed name the day the crop LEFT
	// the bed, so that day itself no longer counts (a pulled crop must not be
	// named in that evening's frost warning) — while a planned harvest end is
	// the last day the crop is still standing, so it stays inclusive.
	if actual := firstDate(p.ClearedOn, p.RemovedOn); actual != nil {
		if !d.Before(*actual) {
			return false
		}
	} else if to != nil && d.After(*to) {
		return false
	}
	// A planting with no dates at all is not "in the ground today" — that would
	// put every un-dated 2031 plan into tonight's frost warning. Open-ended is
	// the right answer for CONFLICT checks, where a question is cheap; it is the
	// wrong answer here, where it would cry wolf.
	return from != nil
}

// firstDate returns the first parseable date among the candidates, or nil.
func firstDate(candidates ...*string) *dates.Date {
	for _, c := range candidates {
		if d := parseDatePtr(c); d != nil {
			return d
		}
	}
	return nil
}
