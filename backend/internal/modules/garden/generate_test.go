package garden

import (
	"testing"
)

func tomato() Effective {
	return Effective{
		PlantID: "p-rajce", PlantName: "rajče", Family: FamilySolanaceae, Hardiness: HardinessTender,
		PlantCore: PlantCore{
			WinSowIndoor:  &Window{AnchorLastFrost, -56, -42},
			WinTransplant: &Window{AnchorLastFrost, 0, 14},
			WinHarvest:    &Window{AnchorWeek, 28, 40},
			FeederClass:   sp(FeederHeavy),
			NeedsSupport:  bp(true),
			HarvestUnit:   sp(UnitKg),
		},
	}
}

func tomatoPlanting() Planting {
	return Planting{
		ID: "pl1", SeasonID: sp("s1"), BedID: sp("b1"), PlantID: "p-rajce",
		AreaM2: fp(2), Status: PlantingPlanned,
		SowIndoorOn: sp("2027-03-20"), TransplantOn: sp("2027-05-15"),
		HarvestFrom: sp("2027-07-12"), HarvestTo: sp("2027-10-10"),
	}
}

func anchors2027() SeasonAnchors {
	return SeasonAnchors{Year: 2027, LastFrost: dp("2027-05-15"), FirstFrost: dp("2027-10-05")}
}

func kinds(ts []Task) map[string]Task {
	out := map[string]Task{}
	for _, t := range ts {
		out[t.Kind] = t
	}
	return out
}

func TestGenerateDerivesTheExpectedKinds(t *testing.T) {
	plan := GenerateFor(tomatoPlanting(), tomato(), anchors2027(), "A1", nil)
	got := kinds(plan.Create)

	for _, kind := range []string{KindBedPrep, KindSowIndoor, KindTransplant, KindSupport, KindFeed, KindHarvest, KindClear} {
		if _, ok := got[kind]; !ok {
			t.Errorf("expected a %s task, got kinds %v", kind, keysOfTasks(got))
		}
	}
	// Every generated task carries an identity and the generated flag.
	for _, task := range plan.Create {
		if !task.IsGenerated || task.GenerationKey == nil || *task.GenerationKey == "" {
			t.Errorf("%s: generated tasks need is_generated and a generation key", task.Kind)
		}
		if task.WindowTo < task.WindowFrom {
			t.Errorf("%s: window %s…%s is inverted", task.Kind, task.WindowFrom, task.WindowTo)
		}
	}
	// Titles are Czech and name the crop and the bed.
	if title := got[KindTransplant].TitleCS; title != "Výsadba — rajče (A1)" {
		t.Errorf("transplant title = %q", title)
	}
}

// D118, asserted rather than assumed: a generated watering cadence nobody ticks
// off is how a task list loses its credibility.
func TestGenerateNeverProducesWaterOrWeed(t *testing.T) {
	e := tomato()
	e.WantsMulch, e.WantsPestCheck = bp(true), bp(true)
	plan := GenerateFor(tomatoPlanting(), e, anchors2027(), "A1", nil)
	for _, task := range plan.Create {
		if task.Kind == KindWater || task.Kind == KindWeed {
			t.Errorf("%s must never be generated (D118)", task.Kind)
		}
	}
}

// A row whose inputs are missing produces NO task rather than a guessed date.
func TestGenerateSkipsRowsWithMissingInputs(t *testing.T) {
	bare := Planting{ID: "pl1", SeasonID: sp("s1"), PlantID: "p-rajce", AreaM2: fp(1)}
	plan := GenerateFor(bare, Effective{PlantID: "p-rajce", PlantName: "rajče"}, SeasonAnchors{Year: 2027}, "", nil)
	if len(plan.Create) != 0 {
		t.Errorf("a planting with no dates and no windows generates nothing, got %v", keysOfTasks(kinds(plan.Create)))
	}
}

// A frost-anchored window that cannot resolve leaves the task out entirely.
func TestGenerateSkipsUnresolvableWindows(t *testing.T) {
	p := Planting{ID: "pl1", SeasonID: sp("s1"), PlantID: "p-rajce", AreaM2: fp(1)}
	plan := GenerateFor(p, tomato(), SeasonAnchors{Year: 2027}, "A1", nil) // no frost dates
	if _, ok := kinds(plan.Create)[KindTransplant]; ok {
		t.Error("with no frost date the transplant window cannot resolve — no task, not a guess")
	}
}

// THE REGENERATION CONTRACT. Moving a planting's transplant date must move an
// open generated task and must not touch anything else.
func TestRegenerationMovesOnlyOpenUneditedGeneratedTasks(t *testing.T) {
	p := tomatoPlanting()
	first := GenerateFor(p, tomato(), anchors2027(), "A1", nil)

	// Materialise the first pass as if it had been saved.
	existing := make([]Task, 0, len(first.Create))
	for i, task := range first.Create {
		task.ID = "t" + itoa(i+1)
		existing = append(existing, task)
	}

	// Now the household moves the transplant a week later.
	p.TransplantOn = sp("2027-05-22")
	p.ManualDates = []string{"transplant_on"}

	cases := []struct {
		name     string
		mutate   func(*Task)
		wantMove bool
		reason   string
	}{
		{"open generated task moves", nil, true, ""},
		{"a done task is untouchable", func(t *Task) { t.Status = TaskDone }, false, "done"},
		{"a skipped task is untouchable", func(t *Task) { t.Status = TaskSkipped }, false, "skipped"},
		{"an edited task is untouchable forever", func(t *Task) { t.IsEdited = true }, false, "edited"},
		{"a manual task is never in scope", func(t *Task) { t.IsGenerated = false }, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := cloneTasks(existing)
			var target *Task
			for i := range pool {
				if pool[i].Kind == KindSupport {
					target = &pool[i]
					break
				}
			}
			if target == nil {
				t.Fatal("fixture should contain a support task, which follows the transplant date")
			}
			id := target.ID
			if tc.mutate != nil {
				tc.mutate(target)
			}

			plan := GenerateFor(p, tomato(), anchors2027(), "A1", pool)
			moved := false
			for _, m := range plan.Move {
				if m.ID == id {
					moved = true
				}
			}
			if moved != tc.wantMove {
				t.Errorf("moved = %v, want %v", moved, tc.wantMove)
			}
			if tc.reason != "" {
				found := ""
				for _, l := range plan.Leave {
					if l.ID == id {
						found = l.Reason
					}
				}
				if found != tc.reason {
					t.Errorf("left-alone reason = %q, want %q", found, tc.reason)
				}
			}
			// Whatever happens, an untouchable task is never deleted either.
			for _, removed := range plan.Remove {
				if removed == id && !tc.wantMove {
					t.Error("an untouchable task must not be removed")
				}
			}
		})
	}
}

// A generated task the user deleted leaves a tombstone, and the tombstone is
// what stops it resurrecting on the next recompute.
func TestTombstonedTaskIsNeverRecreated(t *testing.T) {
	p := tomatoPlanting()
	first := GenerateFor(p, tomato(), anchors2027(), "A1", nil)

	var existing []Task
	var killedKey string
	for i, task := range first.Create {
		task.ID = "t" + itoa(i+1)
		if task.Kind == KindFeed {
			task.Suppressed = true // the user deleted it
			killedKey = *task.GenerationKey
		}
		existing = append(existing, task)
	}
	if killedKey == "" {
		t.Fatal("fixture should contain a feed task")
	}

	plan := GenerateFor(p, tomato(), anchors2027(), "A1", existing)
	for _, task := range plan.Create {
		if task.GenerationKey != nil && *task.GenerationKey == killedKey {
			t.Error("a tombstoned task must not be recreated")
		}
	}
	for _, m := range plan.Move {
		if m.ID == "t"+itoa(indexOfKind(first.Create, KindFeed)+1) {
			t.Error("a tombstoned task must not be moved either")
		}
	}
}

// Regeneration with nothing changed must be a no-op — otherwise every page load
// would churn the calendar and the audit log.
func TestRegenerationIsIdempotent(t *testing.T) {
	p := tomatoPlanting()
	first := GenerateFor(p, tomato(), anchors2027(), "A1", nil)
	var existing []Task
	for i, task := range first.Create {
		task.ID = "t" + itoa(i+1)
		existing = append(existing, task)
	}
	second := GenerateFor(p, tomato(), anchors2027(), "A1", existing)
	if !second.Empty() {
		t.Errorf("a second pass with no change should do nothing, got create=%d move=%d remove=%d",
			len(second.Create), len(second.Move), len(second.Remove))
	}
}

// Clearing a planned date removes the work it implied — but only work the guard
// permits touching.
func TestRemovingADateRemovesItsGeneratedTask(t *testing.T) {
	p := tomatoPlanting()
	first := GenerateFor(p, tomato(), anchors2027(), "A1", nil)
	var existing []Task
	for i, task := range first.Create {
		task.ID = "t" + itoa(i+1)
		existing = append(existing, task)
	}

	e := tomato()
	e.WinTransplant = nil
	p.TransplantOn = nil
	plan := GenerateFor(p, e, anchors2027(), "A1", existing)

	transplantID := "t" + itoa(indexOfKind(first.Create, KindTransplant)+1)
	found := false
	for _, id := range plan.Remove {
		if id == transplantID {
			found = true
		}
	}
	if !found {
		t.Error("a transplant task whose date was cleared should be removed, not left as stale work")
	}
}

// PlannedDates must never overwrite a date the user typed.
func TestPlannedDatesRespectManualDates(t *testing.T) {
	p := Planting{ID: "pl1", TransplantOn: sp("2027-06-01"), ManualDates: []string{"transplant_on"}}
	PlannedDates(&p, tomato(), anchors2027())
	if derefS(p.TransplantOn) != "2027-06-01" {
		t.Errorf("a manual date was overwritten: %v", p.TransplantOn)
	}
	// The non-manual ones do get filled.
	if p.SowIndoorOn == nil {
		t.Error("a non-manual date should be filled from the crop's window")
	}
}

func TestPlannedDatesLeaveUnresolvableWindowsUnset(t *testing.T) {
	p := Planting{ID: "pl1"}
	PlannedDates(&p, tomato(), SeasonAnchors{Year: 2027}) // no frost dates
	if p.TransplantOn != nil || p.SowIndoorOn != nil {
		t.Errorf("frost-anchored dates must stay unset without a frost date: %v %v", p.SowIndoorOn, p.TransplantOn)
	}
	// The week-anchored harvest window still resolves, because it needs no frost date.
	if p.HarvestFrom == nil {
		t.Error("a week-anchored window resolves without frost dates")
	}
}

// D119: recording an actual date states the drift and changes no planned window.
func TestComputeDrift(t *testing.T) {
	p := tomatoPlanting()
	if ComputeDrift(p) != nil {
		t.Error("no actual dates means no drift")
	}

	p.SowedOn = sp("2027-04-03") // planned 2027-03-20, so fourteen days late
	drift := ComputeDrift(p)
	if drift == nil || drift.Days != 14 || drift.Stage != "sow" {
		t.Fatalf("drift = %+v, want +14 days at the sow stage", drift)
	}
	if drift.MessageCS != "vyseto o 14 dní později, sklizeň v plánu beze změny" {
		t.Errorf("message = %q", drift.MessageCS)
	}
	// And the plan is untouched — the whole point of D119.
	if derefS(p.HarvestFrom) != "2027-07-12" {
		t.Error("an actual date must not move a planned window")
	}

	// A later stage supersedes an earlier one.
	p.TransplantedOn = sp("2027-05-10") // planned 05-15, five days early
	if drift := ComputeDrift(p); drift == nil || drift.Stage != "transplant" || drift.Days != -5 {
		t.Errorf("drift = %+v, want -5 days at the transplant stage", drift)
	}
}

// ---- helpers ----

func keysOfTasks(m map[string]Task) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func cloneTasks(in []Task) []Task {
	out := make([]Task, len(in))
	copy(out, in)
	return out
}

func indexOfKind(ts []Task, kind string) int {
	for i, t := range ts {
		if t.Kind == kind {
			return i
		}
	}
	return -1
}
