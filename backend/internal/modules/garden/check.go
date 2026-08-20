package garden

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
)

// KONTROLA PLÁNU — the eleven checks (PRD FR-G9, §V7-4).
//
// Check is PURE. It takes a loaded Snapshot and returns results; it opens no
// transaction, reads no clock beyond s.Today, and calls nothing that can fail.
// That is what makes each check a table-driven test with a fixture that fires and
// a fixture that must not — and it is why the store loads everything up front
// (store.go's SeasonSnapshot) instead of letting a check reach for one more row.
//
// A warning is ADVISORY. It never blocks a save (D109). The check exists to be
// read while you plan in February, not to argue with you in July.

// Check IDs, stable and used as dismissal-key components and settings keys.
const (
	C1  = "C1"  // companions in a bed
	C2  = "C2"  // same family in a bed
	C3  = "C3"  // rotation
	C4  = "C4"  // bed over-booked
	C5  = "C5"  // workload spike
	C6  = "C6"  // family concentration
	C7  = "C7"  // empty active bed
	C8  = "C8"  // feeder succession
	C9  = "C9"  // frost-risky planting date
	C10 = "C10" // planned date outside the crop's window
	C11 = "C11" // neighbouring beds
)

// familyConcentrationLimit is C6's threshold: one family over this share of the
// season's planned area is worth mentioning. 40 % is a rule of thumb, not a law,
// which is why C6 is `info` and dismissible rather than a warning.
const familyConcentrationLimit = 0.40

// outOfWindowGraceDays is C10's tolerance. A planned date a few days outside the
// crop's own window is ordinary judgement; a fortnight outside is a mistake.
const outOfWindowGraceDays = 7

// Check runs every enabled check over the snapshot.
//
// Config comes from s.Settings.Checks rather than a second parameter: the
// snapshot already carries the settings row, and two sources for "is C3 on"
// is one source too many.
func Check(s Snapshot) CheckResult {
	res := CheckResult{
		Year:     s.Season.Year,
		Warnings: []Warning{},
		History:  HistoryState{Status: HistoryOK, ClosedSeasons: s.ClosedSeasons},
	}
	// C3 and C8 read CLOSED seasons only, and there is no historical back-fill
	// (D120). With none behind us they emit nothing and say so — a check that
	// cannot run must never look like one that ran and found nothing. The
	// flagship rotation warning does nothing in year one, and the UI says exactly
	// that rather than showing a green tick that has not been earned.
	if s.ClosedSeasons == 0 {
		res.History.Status = HistoryNoHistory
	}

	cfg := s.Settings.Checks
	if cfg == nil {
		cfg = ChecksConfig{}
	}
	ctx := checkCtx{snap: s, cfg: cfg, byBed: groupByBed(s.Plantings)}

	add := func(check string, ws ...Warning) {
		if !cfg.enabled(check) {
			return // a disabled check emits nothing at all
		}
		res.Warnings = append(res.Warnings, ws...)
	}

	add(C1, ctx.c1Companions()...)
	add(C2, ctx.c2SameFamily()...)
	if res.History.Status == HistoryOK {
		add(C3, ctx.c3Rotation()...)
		add(C8, ctx.c8FeederSuccession()...)
	}
	add(C4, ctx.c4OverBooked()...)
	add(C5, ctx.c5Workload()...)
	add(C6, ctx.c6Concentration()...)
	add(C7, ctx.c7EmptyBeds()...)
	add(C9, ctx.c9FrostRisk()...)
	add(C10, ctx.c10OutOfWindow()...)
	add(C11, ctx.c11Neighbours()...)

	// Apply dismissals and count what is left. A dismissed warning stays in the
	// payload flagged rather than being dropped: the design asks for a findable
	// "dismissed" state, and a warning that vanishes cannot be restored from a
	// screen that never shows it.
	for i := range res.Warnings {
		if d, ok := s.Dismissals[res.Warnings[i].Key]; ok {
			res.Warnings[i].Dismissed = true
			res.Warnings[i].DismissedNote = d.Note
			continue
		}
		switch res.Warnings[i].Severity {
		case SeverityError:
			res.Counts.Error++
		case SeverityWarn:
			res.Counts.Warn++
		case SeverityInfo:
			res.Counts.Info++
		case SeverityTip:
			res.Counts.Tip++
		}
	}
	sortWarnings(res.Warnings)
	return res
}

// checkCtx carries the snapshot plus the two indexes every check wants, built
// once instead of per check.
type checkCtx struct {
	snap  Snapshot
	cfg   ChecksConfig
	byBed map[string][]Planting
}

func (c checkCtx) eff(p Planting) Effective { return c.snap.Effective[p.ID] }

// ---- C1: companions sharing a bed ----

// c1Companions fires when two plantings share a bed, THEIR OCCUPANCIES OVERLAP,
// and a rule marks the pairing bad.
//
// The overlap condition is the whole check. Without it this is the warning that
// flags spring špenát against autumn pórek and teaches the household to stop
// reading the panel.
func (c checkCtx) c1Companions() []Warning {
	var out []Warning
	for bedID, ps := range c.byBed {
		for i := 0; i < len(ps); i++ {
			for j := i + 1; j < len(ps); j++ {
				a, b := ps[i], ps[j]
				if !Overlap(a, b) {
					continue
				}
				rule, ok := c.matchPairRule(a, b)
				if !ok || rule.Verdict != VerdictBad {
					continue
				}
				ea, eb := c.eff(a), c.eff(b)
				detail := fmt.Sprintf("%s a %s si v jednom záhonu nesvědčí a jejich termíny se překrývají.",
					ea.PlantName, eb.PlantName)
				if rule.ReasonCS != nil && *rule.ReasonCS != "" {
					detail = *rule.ReasonCS
				}
				// The rule's own severity is the default; a per-check override in
				// settings wins, the same as every other check.
				out = append(out, c.warn(C1, c.cfg.severity(C1, rule.Severity), &bedID, []string{a.ID, b.ID}, &rule.ID,
					fmt.Sprintf("%s a %s se v %s nesnesou", ea.PlantName, eb.PlantName, bedLabel(c.snap, bedID)),
					detail))
			}
		}
	}
	return out
}

// matchPairRule finds the rule governing two plantings. AN EXPLICIT plant_pair
// BEATS a family_pair (FR-G13) — that precedence is the entire reason both
// scopes exist: "rajče a bazalka" must be able to contradict "lilkovité a
// hluchavkovité".
func (c checkCtx) matchPairRule(a, b Planting) (Rule, bool) {
	ea, eb := c.eff(a), c.eff(b)
	if r, ok := c.findRule(ScopePlantPair, ea.PlantID, eb.PlantID); ok {
		return r, true
	}
	if ea.Family == eb.Family {
		return Rule{}, false // same family is C2's business, not C1's
	}
	return c.findRule(ScopeFamilyPair, ea.Family, eb.Family)
}

// findRule looks up one rule by canonical (sorted) pair order. Storing pairs
// sorted is what makes symmetry structural: one lookup, not two, and nobody has
// to remember to enter both directions.
//
// A RULE THE HOUSEHOLD TYPED BEATS A BUILT-IN for the same pair. Two rows can
// legitimately name one pairing: a built-in plant_pair references its crops by
// NAME (it ships before any crop exists) and is resolved to ids by
// rulesForMatching, so it can end up carrying the same (scope, a_ref, b_ref) as
// a user rule that stores ids — a collision ux_garden_rules cannot see and
// CreateRule therefore does not refuse. Without this precedence the winner is
// whichever row the unordered rules query happened to return first, which in
// practice is always the seeded one: the user's own rule would have no effect
// anywhere and nothing would say why.
func (c checkCtx) findRule(scope, x, y string) (Rule, bool) {
	lo, hi := canonicalPair(x, y)
	var builtin Rule
	var haveBuiltin bool
	for _, r := range c.snap.Rules {
		if r.IsDisabled || r.Scope != scope {
			continue
		}
		if r.ARef != lo || r.BRef != hi {
			continue
		}
		if r.IsBuiltin {
			if !haveBuiltin {
				builtin, haveBuiltin = r, true
			}
			continue
		}
		return r, true
	}
	return builtin, haveBuiltin
}

// ---- C2: same family sharing a bed ----

// c2SameFamily fires when two overlapping plantings in one bed share a family and
// no explicit rule has already spoken about them. Informational: it is a habit
// worth noticing, not a mistake.
func (c checkCtx) c2SameFamily() []Warning {
	var out []Warning
	for bedID, ps := range c.byBed {
		for i := 0; i < len(ps); i++ {
			for j := i + 1; j < len(ps); j++ {
				a, b := ps[i], ps[j]
				ea, eb := c.eff(a), c.eff(b)
				if ea.Family != eb.Family || !Overlap(a, b) {
					continue
				}
				if _, covered := c.findRule(ScopePlantPair, ea.PlantID, eb.PlantID); covered {
					continue // an explicit rule already said what it thinks
				}
				out = append(out, c.warn(C2, c.cfg.severity(C2, SeverityInfo), &bedID, []string{a.ID, b.ID}, nil,
					fmt.Sprintf("V %s roste %s zároveň dvakrát", bedLabel(c.snap, bedID), Label(EnumFamily, ea.Family)),
					fmt.Sprintf("%s i %s patří mezi %s. Sdílejí škůdce i nároky na živiny — pokud to není záměr, zvažte jiný záhon.",
						ea.PlantName, eb.PlantName, strings.ToLower(Label(EnumFamily, ea.Family)))))
			}
		}
	}
	return out
}

// ---- C3: rotation ----

// c3Rotation is the flagship warning: this family grew in this bed too recently.
//
// It counts over CLOSED seasons only. An open season is not history — it is a
// plan that may still change, and rotation history that moves when you edit next
// year's plan is not history at all.
func (c checkCtx) c3Rotation() []Warning {
	var out []Warning
	for bedID, ps := range c.byBed {
		history := c.snap.History[bedID]
		if len(history) == 0 {
			continue
		}
		seen := map[string]bool{} // one warning per family per bed, not per planting
		for _, p := range ps {
			e := c.eff(p)
			if seen[e.Family] {
				continue
			}
			breakYears := e.RotationBreak(c.snap.Rules, c.snap.Settings.RotationBreakDefault)
			if breakYears <= 0 {
				continue
			}
			lastYear, found := lastYearOfFamily(history, e.Family)
			if !found {
				continue
			}
			gap := c.snap.Season.Year - lastYear
			// A gap EQUAL to the minimum satisfies it: a classic N-year rotation
			// returns the family after exactly N years, and a check that flags
			// the textbook rotation as "too early" is noise by its second season.
			if gap >= breakYears {
				continue
			}
			seen[e.Family] = true
			// Under half the recommended break is a different kind of mistake
			// from just-short-of-it, and the panel should not flatten the two.
			severity := SeverityWarn
			if gap*2 < breakYears {
				severity = SeverityError
			}
			out = append(out, c.warn(C3, c.cfg.severity(C3, severity), &bedID, []string{p.ID}, nil,
				fmt.Sprintf("%s tu rostly %s", Label(EnumFamily, e.Family), yearsAgoCS(gap)),
				fmt.Sprintf("V %s rostly %s naposledy v roce %d. Doporučená pauza je %s — %s se sem letos vrací dřív.",
					bedLabel(c.snap, bedID), strings.ToLower(Label(EnumFamily, e.Family)), lastYear,
					yearsCS(breakYears), e.PlantName)))
		}
	}
	return out
}

func lastYearOfFamily(history []BedSeasonHistory, family string) (int, bool) {
	best, found := 0, false
	for _, h := range history {
		for _, f := range h.Families {
			if f == family && h.Year > best {
				best, found = h.Year, true
			}
		}
	}
	return best, found
}

// ---- C4: bed over-booked ----

// c4OverBooked fires when the plantings occupying a bed AT THE SAME TIME need
// more area than the bed has. Sequential crops in one bed are not over-booking,
// so the sum is taken over each overlap group rather than over the whole season.
func (c checkCtx) c4OverBooked() []Warning {
	var out []Warning
	for bedID, ps := range c.byBed {
		bed, ok := findBed(c.snap.Beds, bedID)
		if !ok || bed.AreaM2 <= 0 {
			continue
		}
		// SAMPLE THE BED AT INSTANTS, not at "everything that overlaps one
		// probe". Overlap is not transitive: špenát can overlap ředkvičky and
		// ředkvičky overlap pórek while špenát is long gone before pórek goes
		// in, and summing all three would report a conflict the bed never has.
		// The busiest moment is always the moment some planting takes the bed,
		// so those are the only instants worth checking.
		worst, worstIDs := 0.0, []string(nil)
		for _, at := range occupancyStarts(ps) {
			sum, ids := 0.0, []string(nil)
			for _, other := range ps {
				if !occupiesAt(other, at) {
					continue
				}
				if area, ok := c.eff(other).AreaOf(other); ok {
					sum += area
					ids = append(ids, other.ID)
				}
			}
			if sum > worst {
				worst, worstIDs = sum, ids
			}
		}
		if worst <= bed.AreaM2 {
			continue
		}
		out = append(out, c.warn(C4, c.cfg.severity(C4, SeverityWarn), &bedID, worstIDs, nil,
			fmt.Sprintf("%s je přeplněný", bedLabel(c.snap, bedID)),
			fmt.Sprintf("Výsadby, které se tu v jednu chvíli potkají, potřebují %.1f m², ale záhon má %.1f m².",
				worst, bed.AreaM2)))
	}
	return out
}

// ---- C5: workload spike ----

// c5Workload fires when too many sowings and transplants land in one ISO week.
// It reads the generated TASKS rather than the plantings, because that is the
// week the household will actually have to work.
func (c checkCtx) c5Workload() []Warning {
	threshold := c.snap.Settings.WorkloadWeekThreshold
	if threshold <= 0 {
		return nil
	}
	type bucket struct {
		count int
		ids   []string
	}
	weeks := map[string]*bucket{}
	for _, t := range c.snap.Tasks {
		if t.Status != TaskOpen {
			continue
		}
		switch t.Kind {
		case KindSowIndoor, KindSowDirect, KindTransplant:
		default:
			continue
		}
		d, err := dates.Parse(t.WindowFrom)
		if err != nil {
			continue
		}
		y, w := isoWeekOf(d)
		key := fmt.Sprintf("%d-W%02d", y, w)
		b := weeks[key]
		if b == nil {
			b = &bucket{}
			weeks[key] = b
		}
		b.count++
		if t.PlantingID != nil {
			b.ids = append(b.ids, *t.PlantingID)
		}
	}
	var out []Warning
	for _, key := range sortedKeys(weeks) {
		b := weeks[key]
		if b.count <= threshold {
			continue
		}
		out = append(out, c.warnKeyed(C5, c.cfg.severity(C5, SeverityInfo), nil, dedupe(b.ids), nil, key,
			fmt.Sprintf("Nabitý týden %s", weekLabelCS(key)),
			fmt.Sprintf("V tomto týdnu vychází %s. Zvažte, jestli něco nepřesunout o týden — v plánu to jde snadno, na zahradě už ne.",
				tasksCS(b.count))))
	}
	return out
}

// ---- C6: family concentration ----

func (c checkCtx) c6Concentration() []Warning {
	total := 0.0
	perFamily := map[string]float64{}
	idsPerFamily := map[string][]string{}
	for _, p := range c.snap.Plantings {
		e := c.eff(p)
		area, ok := e.AreaOf(p)
		if !ok {
			continue
		}
		total += area
		perFamily[e.Family] += area
		idsPerFamily[e.Family] = append(idsPerFamily[e.Family], p.ID)
	}
	if total <= 0 {
		return nil
	}
	var out []Warning
	for _, family := range sortedKeys(perFamily) {
		share := perFamily[family] / total
		if share <= familyConcentrationLimit {
			continue
		}
		out = append(out, c.warnKeyed(C6, c.cfg.severity(C6, SeverityInfo), nil, idsPerFamily[family], nil, family,
			fmt.Sprintf("%s zabírají %.0f %% plochy", Label(EnumFamily, family), share*100),
			"Jedna čeleď na velké části zahrady znamená společné škůdce a těžší střídání v příštích letech."))
	}
	return out
}

// ---- C7: empty active beds ----

func (c checkCtx) c7EmptyBeds() []Warning {
	var out []Warning
	for _, bed := range c.snap.Beds {
		if !bed.IsActive || len(c.byBed[bed.ID]) > 0 {
			continue
		}
		id := bed.ID
		out = append(out, c.warn(C7, c.cfg.severity(C7, SeverityInfo), &id, nil, nil,
			fmt.Sprintf("%s je zatím prázdný", bedLabel(c.snap, bed.ID)),
			"V letošní sezóně tu nic neroste. Pokud to je záměr — třeba odpočinek nebo zelené hnojení — varování ignorujte."))
	}
	return out
}

// ---- C8: feeder succession ----

// c8FeederSuccession looks at what the bed grew LAST closed season against what
// it grows now. Two outcomes, and they are deliberately not the same shape:
//
//   - heavy after heavy is a `warn` — the bed is being asked for two big meals
//     in a row;
//   - a nitrogen fixer immediately before a heavy feeder is a `tip`, PHRASED AS
//     PRAISE. If tips render in the same language as errors, the panel teaches
//     people that everything in it is noise.
func (c checkCtx) c8FeederSuccession() []Warning {
	var out []Warning
	for bedID, ps := range c.byBed {
		history := c.snap.History[bedID]
		prev, ok := mostRecentSeason(history)
		if !ok {
			continue
		}
		prevHeavy, prevFixer := feederClassesOf(prev)
		seen := map[string]bool{}
		for _, p := range ps {
			e := c.eff(p)
			if e.FeederClass == nil || seen[*e.FeederClass] {
				continue
			}
			switch *e.FeederClass {
			case FeederHeavy:
				if prevFixer {
					seen[FeederHeavy] = true
					out = append(out, c.warn(C8, c.cfg.severity(C8, SeverityTip), &bedID, []string{p.ID}, nil,
						fmt.Sprintf("Dobrý sled v %s", bedLabel(c.snap, bedID)),
						// `prev` is the most recent CLOSED season for this bed,
						// which is not necessarily last year: a bed that rested,
						// or a season nobody closed, puts it further back. Naming
						// the year says the same thing the heavy-after-heavy
						// branch below says, rather than a confident "loni" that
						// can be two seasons wrong.
						fmt.Sprintf("V roce %d tu rostly luskoviny, které půdě dodaly dusík — %s po nich má přesně to, co potřebuje.",
							prev.Year, e.PlantName)))
					continue
				}
				if prevHeavy {
					seen[FeederHeavy] = true
					out = append(out, c.warn(C8, c.cfg.severity(C8, SeverityWarn), &bedID, []string{p.ID}, nil,
						fmt.Sprintf("Druhý náročný rok v %s", bedLabel(c.snap, bedID)),
						fmt.Sprintf("V roce %d tu už rostla plodina s vysokým nárokem na živiny. %s bude potřebovat pořádné pohnojení, nebo raději jiný záhon.",
							prev.Year, e.PlantName)))
				}
			}
		}
	}
	return out
}

func feederClassesOf(h BedSeasonHistory) (heavy, fixer bool) {
	for _, p := range h.Plantings {
		switch p.FeederClass {
		case FeederHeavy:
			heavy = true
		case FeederFixer:
			fixer = true
		}
	}
	return heavy, fixer
}

func mostRecentSeason(history []BedSeasonHistory) (BedSeasonHistory, bool) {
	best, found := BedSeasonHistory{}, false
	for _, h := range history {
		if !found || h.Year > best.Year {
			best, found = h, true
		}
	}
	return best, found
}

// ---- C9: frost-risky planting date ----

// c9FrostRisk fires when a tender crop is planned to go outside BEFORE the
// season's expected last frost. It reads the season's own frost date, not
// tonight's forecast: this is a planning check, answered in February.
func (c checkCtx) c9FrostRisk() []Warning {
	lastFrost := parseDatePtr(c.snap.Season.LastFrostOn)
	if lastFrost == nil {
		return nil // no expected frost date ⇒ nothing to compare against
	}
	var out []Warning
	for _, p := range c.snap.Plantings {
		e := c.eff(p)
		if e.Hardiness != HardinessTender {
			continue
		}
		outside := firstDate(p.TransplantOn, p.SowDirectOn)
		if outside == nil || !outside.Before(*lastFrost) {
			continue
		}
		days := outside.DaysUntil(*lastFrost)
		out = append(out, c.warn(C9, c.cfg.severity(C9, SeverityWarn), p.BedID, []string{p.ID}, nil,
			fmt.Sprintf("%s ven před posledním mrazem", e.PlantName),
			fmt.Sprintf("%s je citlivá na mráz a v plánu jde ven %s — %s před očekávaným posledním jarním mrazem (%s).",
				e.PlantName, czDate(*outside), daysCS(days), czDate(*lastFrost))))
	}
	return out
}

// ---- C10: planned date outside the crop's own window ----

func (c checkCtx) c10OutOfWindow() []Warning {
	anchors := c.snap.Season.Anchors()
	var out []Warning
	for _, p := range c.snap.Plantings {
		e := c.eff(p)
		for _, f := range []struct {
			label  string
			window *Window
			value  *string
		}{
			{"Výsev do sadbovačů", e.WinSowIndoor, p.SowIndoorOn},
			{"Přímý výsev", e.WinSowDirect, p.SowDirectOn},
			{"Výsadba", e.WinTransplant, p.TransplantOn},
		} {
			if f.window == nil || f.value == nil {
				continue
			}
			from, to, ok := f.window.Resolve(anchors)
			if !ok {
				continue // the anchor is missing; C10 has nothing to say
			}
			planned := parseDatePtr(f.value)
			if planned == nil {
				continue
			}
			var off int
			switch {
			case planned.Before(from):
				off = planned.DaysUntil(from)
			case planned.After(to):
				off = to.DaysUntil(*planned)
			default:
				continue
			}
			if off <= outOfWindowGraceDays {
				continue
			}
			out = append(out, c.warnKeyed(C10, c.cfg.severity(C10, SeverityWarn), p.BedID, []string{p.ID}, nil, f.label,
				fmt.Sprintf("%s: %s mimo doporučený termín", e.PlantName, f.label),
				fmt.Sprintf("V plánu je %s, ale doporučený termín pro tuhle plodinu je %s. Rozdíl je %s.",
					czDate(*planned), czRange(from, to), daysCS(off))))
		}
	}
	return out
}

// ---- C11: neighbouring beds ----

// c11Neighbours reads adjacency off the bed ORDER within a zone (D117): two beds
// are neighbours iff they are consecutive. There is no neighbour table and no
// coordinates — you drag beds into the order they physically stand in, and this
// check becomes possible for free.
func (c checkCtx) c11Neighbours() []Warning {
	var out []Warning
	for _, pair := range adjacentBedPairs(c.snap.Beds) {
		left, right := c.byBed[pair[0]], c.byBed[pair[1]]
		for _, a := range left {
			for _, b := range right {
				if !Overlap(a, b) {
					continue
				}
				ea, eb := c.eff(a), c.eff(b)
				// Two varieties of ONE species side by side cross-pollinate,
				// which only matters if you save seed — hence `info`, always.
				if ea.PlantID == eb.PlantID && ea.VarietyID != nil && eb.VarietyID != nil && *ea.VarietyID != *eb.VarietyID {
					out = append(out, c.warnKeyed(C11, c.cfg.severity(C11, SeverityInfo), &pair[0], []string{a.ID, b.ID}, nil, "cross",
						fmt.Sprintf("Dvě odrůdy %s vedle sebe", ea.PlantName),
						fmt.Sprintf("%s (%s) a %s (%s) stojí v sousedních záhonech. Pokud si necháváte semínka, mohou se zkřížit.",
							derefS(ea.VarietyName), bedLabel(c.snap, pair[0]), derefS(eb.VarietyName), bedLabel(c.snap, pair[1]))))
					continue
				}
				rule, ok := c.matchPairRule(a, b)
				if !ok || rule.Verdict != VerdictBad {
					continue
				}
				out = append(out, c.warnKeyed(C11, c.cfg.severity(C11, SeverityInfo), &pair[0], []string{a.ID, b.ID}, &rule.ID, "adj",
					fmt.Sprintf("%s a %s v sousedních záhonech", ea.PlantName, eb.PlantName),
					fmt.Sprintf("%s je v %s a %s hned vedle v %s. Přes hranici záhonu to vadí míň než ve společném záhonu, ale stojí za zmínku.",
						ea.PlantName, bedLabel(c.snap, pair[0]), eb.PlantName, bedLabel(c.snap, pair[1]))))
			}
		}
	}
	return out
}

// adjacentBedPairs returns consecutive (zone, position) bed-id pairs. Beds with
// no zone form one implicit group — a single-zone garden should still get C11.
func adjacentBedPairs(beds []Bed) [][2]string {
	byZone := map[string][]Bed{}
	for _, b := range beds {
		if !b.IsActive {
			continue
		}
		byZone[derefS(b.Zone)] = append(byZone[derefS(b.Zone)], b)
	}
	var out [][2]string
	for _, zone := range sortedKeys(byZone) {
		list := byZone[zone]
		sort.Slice(list, func(i, j int) bool { return list[i].Position < list[j].Position })
		for i := 0; i+1 < len(list); i++ {
			out = append(out, [2]string{list[i].ID, list[i+1].ID})
		}
	}
	return out
}

// ---- warning construction ----

// warn builds a warning with the standard key.
func (c checkCtx) warn(check, severity string, bedID *string, plantingIDs []string, ruleID *string, title, detail string) Warning {
	return c.warnKeyed(check, severity, bedID, plantingIDs, ruleID, "", title, detail)
}

// warnKeyed adds a discriminator for checks that can fire more than once for the
// same (bed, plantings) tuple — C10 per date field, C11 per reason.
func (c checkCtx) warnKeyed(check, severity string, bedID *string, plantingIDs []string, ruleID *string, extra, title, detail string) Warning {
	ids := append([]string(nil), plantingIDs...)
	sort.Strings(ids)
	return Warning{
		Key:         warningKey(check, derefS(ruleID), derefS(bedID), ids, c.snap.Season.Year, extra),
		Check:       check,
		Severity:    severity,
		TitleCS:     title,
		DetailCS:    detail,
		BedID:       bedID,
		PlantingIDs: ids,
		RuleID:      ruleID,
	}
}

// warningKey is stable across recomputation, so a dismissal survives a page
// reload and an unrelated edit.
//
// THE YEAR IS IN THE HASH DELIBERATELY, even though the dismissal row is already
// season-scoped: it means a COPIED SEASON does not inherit last year's
// dismissals. "Vím, letos to risknu" was said about one year, and a copy that
// silently carries that decision forward is how a knowingly-taken risk becomes
// an invisible one.
func warningKey(check, ruleID, bedID string, sortedPlantingIDs []string, year int, extra string) string {
	h := sha256.Sum256([]byte(strings.Join([]string{
		check, ruleID, bedID, strings.Join(sortedPlantingIDs, ","), fmt.Sprint(year), extra,
	}, "|")))
	return hex.EncodeToString(h[:])[:16]
}

// sortWarnings orders the panel: severity first (errors at the top), then check
// id, then key — so a recomputation with no change produces a byte-identical
// list and the panel does not reshuffle under the reader.
func sortWarnings(ws []Warning) {
	rank := map[string]int{SeverityError: 0, SeverityWarn: 1, SeverityInfo: 2, SeverityTip: 3}
	sort.SliceStable(ws, func(i, j int) bool {
		if rank[ws[i].Severity] != rank[ws[j].Severity] {
			return rank[ws[i].Severity] < rank[ws[j].Severity]
		}
		if ws[i].Check != ws[j].Check {
			return ws[i].Check < ws[j].Check
		}
		return ws[i].Key < ws[j].Key
	})
}

// ---- small helpers ----

func groupByBed(ps []Planting) map[string][]Planting {
	out := map[string][]Planting{}
	for _, p := range ps {
		if p.BedID == nil {
			continue // a planting with no bed cannot conflict with one
		}
		out[*p.BedID] = append(out[*p.BedID], p)
	}
	// Deterministic order inside each bed keeps warning ids stable across runs.
	for k := range out {
		list := out[k]
		sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
		out[k] = list
	}
	return out
}

func findBed(beds []Bed, id string) (Bed, bool) {
	for _, b := range beds {
		if b.ID == id {
			return b, true
		}
	}
	return Bed{}, false
}

func bedLabel(s Snapshot, id string) string {
	if b, ok := findBed(s.Beds, id); ok {
		return b.Code
	}
	return "záhonu"
}

func canonicalPair(a, b string) (string, string) {
	if a <= b {
		return a, b
	}
	return b, a
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
