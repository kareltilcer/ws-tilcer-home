package garden

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
)

// TASK GENERATION AND REGENERATION (PRD D110, FR-G7).
//
// This is the module's TRUST SURFACE. Everything else can be wrong in a way you
// notice and fix; a calendar that quietly undoes your work is a calendar you stop
// opening, and then the whole module is dead weight.
//
// So the rules are encoded as ONE guard rather than as scattered `if`s:
//
//	mutable(t) = t.IsGenerated && !t.IsEdited && t.Status == open && !t.Suppressed
//
// A task that fails that guard is left alone SILENTLY — not moved, not deleted,
// not recreated, and not reported as a conflict. A generated task the user
// deleted keeps its row with suppressed = 1 so its key stays taken and it cannot
// resurrect. Manual tasks (is_generated = 0) are never in scope at all.

// TaskMove is one change the generator wants to make to an existing task: the
// window, and the bed/title that must follow the planting it derives from — a
// task pinned to the bed the crop left would send the calendar to an empty bed.
type TaskMove struct {
	ID       string
	From, To dates.Date
	DueHint  *dates.Date
	BedID    *string
	TitleCS  string
}

// TaskLeft records a task the generator deliberately did not touch, with why.
// It feeds the audit event's meta, so the Log can answer "what did that edit do
// to my calendar" — including the half that is "nothing, on purpose".
type TaskLeft struct {
	ID     string
	Reason string
}

// Plan is what a regeneration would do. Nothing is written until the service
// applies it, which is what makes the whole thing testable without a database.
type Plan struct {
	Create []Task
	Move   []TaskMove
	Leave  []TaskLeft
	// Remove are MUTABLE generated tasks whose generation key the plan no longer
	// produces — a transplant job for a transplant date that was cleared.
	//
	// This extends the brief's three-field plan, and only in the direction the
	// guard already permits: every id here passed mutable(), so it is open,
	// generated, unedited and not a tombstone. Leaving them would mean the
	// calendar keeps offering work for a step that no longer exists, which is the
	// same kind of untrustworthy as deleting work that does. They are soft-deleted
	// rather than tombstoned, so restoring the date brings the task back.
	Remove []string
}

// Empty reports whether the plan would change nothing.
func (p Plan) Empty() bool { return len(p.Create) == 0 && len(p.Move) == 0 && len(p.Remove) == 0 }

// mutable is THE guard. Every regeneration decision reads it and nothing
// re-derives it.
func mutable(t Task) bool {
	return t.IsGenerated && !t.IsEdited && t.Status == TaskOpen && !t.Suppressed
}

// generationKey identifies a generated task across recomputations.
func generationKey(plantingID, kind string, index int) string {
	h := sha256.Sum256([]byte(strings.Join([]string{plantingID, kind, strconv.Itoa(index)}, "|")))
	return hex.EncodeToString(h[:])[:16]
}

// desired is one task the derivation table wants to exist.
type desired struct {
	kind     string
	index    int
	from, to dates.Date
	due      *dates.Date
}

// GenerateFor computes what the calendar should hold for one planting.
//
// `existing` must include tombstones — a suppressed key is how the generator
// knows not to recreate a task the user threw away.
func GenerateFor(p Planting, eff Effective, anchors SeasonAnchors, bedCode string, existing []Task) Plan {
	want := derive(p, eff, anchors)

	byKey := map[string]Task{}
	for _, t := range existing {
		if t.GenerationKey != nil {
			byKey[*t.GenerationKey] = t
		}
	}

	var plan Plan
	wanted := map[string]bool{}
	for _, w := range want {
		key := generationKey(p.ID, w.kind, w.index)
		wanted[key] = true
		title := taskTitleCS(w.kind, eff, bedCode)

		cur, ok := byKey[key]
		if !ok {
			plan.Create = append(plan.Create, Task{
				Kind:          w.kind,
				SeasonID:      p.SeasonID,
				PlantingID:    sp(p.ID),
				BedID:         p.BedID,
				TitleCS:       title,
				WindowFrom:    w.from.String(),
				WindowTo:      w.to.String(),
				DueHint:       dateStr(w.due),
				Status:        TaskOpen,
				IsGenerated:   true,
				GenerationKey: sp(key),
			})
			continue
		}
		if !mutable(cur) {
			plan.Leave = append(plan.Leave, TaskLeft{ID: cur.ID, Reason: leaveReason(cur)})
			continue
		}
		// Bed and title are part of "where it should be": a planting moved to
		// another bed (or renamed by a variety change) must carry its open tasks
		// with it, or the calendar's bed filter and the print sheet name a bed
		// the crop is no longer in.
		if cur.WindowFrom == w.from.String() && cur.WindowTo == w.to.String() && derefS(cur.DueHint) == derefS(dateStr(w.due)) &&
			derefS(cur.BedID) == derefS(p.BedID) && cur.TitleCS == title {
			continue // already where it should be
		}
		plan.Move = append(plan.Move, TaskMove{ID: cur.ID, From: w.from, To: w.to, DueHint: w.due, BedID: p.BedID, TitleCS: title})
	}

	// Generated tasks the plan no longer produces.
	for key, t := range byKey {
		if wanted[key] || t.Suppressed {
			continue
		}
		if !t.IsGenerated {
			continue
		}
		if !mutable(t) {
			plan.Leave = append(plan.Leave, TaskLeft{ID: t.ID, Reason: leaveReason(t)})
			continue
		}
		plan.Remove = append(plan.Remove, t.ID)
	}
	return plan
}

// leaveReason names, in one word, why the generator kept its hands off. It ends
// up in the audit meta, so "why didn't my calendar update" has an answer.
func leaveReason(t Task) string {
	switch {
	case !t.IsGenerated:
		return "manual"
	case t.IsEdited:
		return "edited"
	case t.Status == TaskDone:
		return "done"
	case t.Status == TaskSkipped:
		return "skipped"
	case t.Suppressed:
		return "deleted"
	default:
		return "unknown"
	}
}

// derive is the derivation table (HANDOFF-9 §7). A row whose inputs are missing
// produces NO task — never a guessed date. That is the same discipline as
// Window.Resolve returning ok=false: an absent job is a visible gap, a
// hallucinated one is a wrong instruction.
//
// `water` and `weed` are absent by design (D118): they are decided by looking at
// the garden, and a generated cadence nobody ticks off devalues every other row
// in the list.
func derive(p Planting, eff Effective, a SeasonAnchors) []desired {
	var out []desired
	add := func(kind string, index int, from, to dates.Date, due *dates.Date) {
		if to.Before(from) {
			to = from
		}
		out = append(out, desired{kind: kind, index: index, from: from, to: to, due: due})
	}

	sowIndoor := parseDatePtr(p.SowIndoorOn)
	sowDirect := parseDatePtr(p.SowDirectOn)
	transplant := parseDatePtr(p.TransplantOn)
	harvestFrom := parseDatePtr(p.HarvestFrom)
	harvestTo := parseDatePtr(p.HarvestTo)
	inGround := firstNonNil(transplant, sowDirect)

	// window returns the task window for a planned date: the crop's own
	// recommended range when the date came from it, or the exact day when the
	// user typed it. A manual date is a decision, and offering a ten-day window
	// around it would quietly re-open a question already answered.
	window := func(field string, w *Window, planned *dates.Date) (dates.Date, dates.Date, *dates.Date, bool) {
		if planned == nil {
			return dates.Date{}, dates.Date{}, nil, false
		}
		if !containsStr(p.ManualDates, field) && w != nil {
			if from, to, ok := w.Resolve(a); ok {
				return from, to, planned, true
			}
		}
		return *planned, *planned, planned, true
	}

	// Bed preparation, three weeks to one week before the crop reaches the BED.
	// Indoor sowing deliberately does not anchor it (same distinction as
	// inGround above): a tray on a windowsill in March says nothing about when
	// the outdoor bed needs digging, so until the transplant date resolves there
	// is no bed-prep task — an absent job over a guessed February one.
	if first := firstNonNil(sowDirect, transplant); first != nil && p.BedID != nil {
		add(KindBedPrep, 0, first.AddDays(-21), first.AddDays(-7), nil)
	}

	if from, to, due, ok := window("sow_indoor_on", eff.WinSowIndoor, sowIndoor); ok {
		add(KindSowIndoor, 0, from, to, due)
	}
	if derefB(eff.NeedsPrickingOut) && sowIndoor != nil &&
		eff.DaysToGerminateMin != nil && eff.DaysToGerminateMax != nil {
		add(KindPrickOut, 0,
			sowIndoor.AddDays(*eff.DaysToGerminateMin),
			sowIndoor.AddDays(*eff.DaysToGerminateMax+7), nil)
	}
	if transplant != nil && eff.HardeningDays != nil && *eff.HardeningDays > 0 {
		add(KindHardenOff, 0,
			transplant.AddDays(-*eff.HardeningDays-2),
			transplant.AddDays(-1), nil)
	}
	if from, to, due, ok := window("sow_direct_on", eff.WinSowDirect, sowDirect); ok {
		add(KindSowDirect, 0, from, to, due)
	}
	if from, to, due, ok := window("transplant_on", eff.WinTransplant, transplant); ok {
		add(KindTransplant, 0, from, to, due)
	}
	// Thinning belongs to direct sowings only — there is nothing to thin in a
	// tray that was pricked out.
	if sowDirect != nil {
		add(KindThin, 0, sowDirect.AddDays(14), sowDirect.AddDays(28), nil)
	}
	if derefB(eff.NeedsSupport) && inGround != nil {
		add(KindSupport, 0, *inGround, inGround.AddDays(14), nil)
	}
	if inGround != nil && eff.FeederClass != nil {
		switch *eff.FeederClass {
		case FeederHeavy:
			add(KindFeed, 0, inGround.AddDays(28), inGround.AddDays(42), nil)
		case FeederMedium:
			add(KindFeed, 0, inGround.AddDays(42), inGround.AddDays(56), nil)
			// light and fixer are fed by the soil, not by us.
		}
	}
	if derefB(eff.WantsMulch) && inGround != nil {
		add(KindMulch, 0, inGround.AddDays(7), inGround.AddDays(21), nil)
	}
	// A monthly look for pests across however long the crop is actually in the
	// bed — which is the occupancy window, the same one every check joins on.
	if derefB(eff.WantsPestCheck) {
		if from, to := p.OccupancyWindow(); from != nil && to != nil {
			for i, day := 0, from.AddDays(30); !day.After(*to) && i < 12; i, day = i+1, day.AddDays(30) {
				add(KindPestCheck, i, day, day.AddDays(6), nil)
			}
		}
	}

	// Permanents get yearly care instead of a sowing cycle (D106): the same table
	// serves both because a permanent is just a planting with no season.
	if p.IsPermanent() {
		if w := eff.WinTransplant; w != nil {
			if from, to, ok := w.Resolve(a); ok {
				add(KindPrune, 0, from, to, nil)
			}
		}
		if w := eff.WinSowDirect; w != nil {
			if from, to, ok := w.Resolve(a); ok {
				add(KindSpray, 0, from, to, nil)
			}
		}
	}

	if from, to, due, ok := window("harvest_from", eff.WinHarvest, harvestFrom); ok {
		// Harvest spans to the planned end when there is one — it is a season of
		// picking, not a day.
		if harvestTo != nil && harvestTo.After(to) {
			to = *harvestTo
		}
		add(KindHarvest, 0, from, to, due)
	}
	if harvestFrom != nil && harvestTo != nil && len(eff.StorageMethods) > 0 {
		processTo := harvestTo.AddDays(7)
		add(KindProcess, 0, *harvestFrom, processTo, nil)
		add(KindStore, 0, processTo, processTo.AddDays(7), nil)
	}
	if harvestTo != nil {
		add(KindClear, 0, harvestTo.AddDays(1), harvestTo.AddDays(21), nil)
	}
	return out
}

func firstNonNil(candidates ...*dates.Date) *dates.Date {
	for _, c := range candidates {
		if c != nil {
			return c
		}
	}
	return nil
}

func dateStr(d *dates.Date) *string {
	if d == nil {
		return nil
	}
	return sp(d.String())
}

// PlannedDates fills a planting's planned dates from the crop's resolved windows.
//
// Called on create and on every re-anchor. Two rules make it safe to run again:
// a date the user typed is listed in manual_dates and is never touched, and a
// window that does not resolve leaves its date UNSET rather than guessing.
//
// The `to` side of each window is the planned date for harvest_to; every other
// planned date takes the window's start, because "the recommended range opens
// then" is what a plan wants and the range itself survives on the task.
func PlannedDates(p *Planting, eff Effective, a SeasonAnchors) {
	set := func(field string, target **string, w *Window, pick func(from, to dates.Date) dates.Date) {
		if containsStr(p.ManualDates, field) {
			return // the user decided this one
		}
		if w == nil {
			return
		}
		from, to, ok := w.Resolve(a)
		if !ok {
			return // no anchor ⇒ leave it unset, never guess
		}
		*target = sp(pick(from, to).String())
	}
	start := func(from, _ dates.Date) dates.Date { return from }
	end := func(_, to dates.Date) dates.Date { return to }

	set("sow_indoor_on", &p.SowIndoorOn, eff.WinSowIndoor, start)
	set("sow_direct_on", &p.SowDirectOn, eff.WinSowDirect, start)
	set("transplant_on", &p.TransplantOn, eff.WinTransplant, start)
	set("harvest_from", &p.HarvestFrom, eff.WinHarvest, start)
	set("harvest_to", &p.HarvestTo, eff.WinHarvest, end)
}

// ComputeDrift states how far reality has moved from the plan (D119).
//
// Actual dates never re-drive planned ones — sow two weeks late and the harvest
// window stays where February put it. The compensating control is that the drift
// is never SILENT: it is stated here, in Czech, built once in the service so the
// planting page, the widget and any future summary say it identically.
func ComputeDrift(p Planting) *Drift {
	type stage struct {
		name             string
		actual, planned  *dates.Date
	}
	stages := []stage{
		{"sow", parseDatePtr(p.SowedOn), firstNonNil(parseDatePtr(p.SowIndoorOn), parseDatePtr(p.SowDirectOn))},
		{"transplant", parseDatePtr(p.TransplantedOn), parseDatePtr(p.TransplantOn)},
		{"harvest", parseDatePtr(p.FirstHarvestOn), parseDatePtr(p.HarvestFrom)},
	}
	// The LATEST stage that has both an actual and a plan wins: once you have
	// transplanted, the sowing drift is history and the transplant drift is what
	// the remaining work hangs off.
	var found *Drift
	for _, s := range stages {
		if s.actual == nil || s.planned == nil {
			continue
		}
		days := s.planned.DaysUntil(*s.actual)
		if days == 0 {
			// An on-plan stage still wins: it supersedes an earlier stage's drift
			// with "no drift" rather than letting stale history linger.
			found = nil
			continue
		}
		found = &Drift{Days: days, Stage: s.name, MessageCS: driftMessageCS(s.name, days)}
	}
	return found
}
