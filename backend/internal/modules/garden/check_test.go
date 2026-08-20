package garden

import (
	"strings"
	"testing"
)

// ---- fixture builder ----

type snapB struct{ s Snapshot }

func newSnap(year int) *snapB {
	return &snapB{Snapshot{
		Season:    Season{ID: "s1", Year: year, Status: SeasonActive, LastFrostOn: sp(itoaYear(year) + "-05-15")},
		Today:     d(itoaYear(year) + "-03-01"),
		Effective: map[string]Effective{},
		History:   map[string][]BedSeasonHistory{},
		// One closed season by default, so history-dependent checks can run. The
		// no_history case is asserted explicitly in its own test.
		ClosedSeasons: 1,
		Dismissals:    map[string]Dismissal{},
		Settings: Settings{
			RotationBreakDefault:  4,
			FrostThresholdC:       2,
			WorkloadWeekThreshold: 10,
			Checks:                ChecksConfig{},
		},
	}}
}

func (b *snapB) bed(id, code string, area float64) *snapB {
	b.s.Beds = append(b.s.Beds, Bed{
		ID: id, Code: code, Name: code, Type: "ground", AreaM2: area,
		IsActive: true, Position: "m" + itoaYear(len(b.s.Beds)), Zone: sp("hlavní"),
	})
	return b
}

// plant adds a planting plus its resolved crop record in one call — the two are
// always written together, and forgetting the Effective entry is the only way to
// make these fixtures lie.
func (b *snapB) plant(id, bedID string, e Effective, mutate func(*Planting)) *snapB {
	p := Planting{
		ID: id, SeasonID: sp("s1"), SeasonYear: ip(b.s.Season.Year), BedID: sp(bedID),
		PlantID: e.PlantID, PlantName: e.PlantName, Family: e.Family, Status: PlantingPlanned,
	}
	if mutate != nil {
		mutate(&p)
	}
	b.s.Plantings = append(b.s.Plantings, p)
	b.s.Effective[id] = e
	return b
}

func (b *snapB) rule(r Rule) *snapB {
	r.ARef, r.BRef = canonicalPair(r.ARef, r.BRef)
	if r.Severity == "" {
		r.Severity = SeverityWarn
	}
	b.s.Rules = append(b.s.Rules, r)
	return b
}

func (b *snapB) history(bedID string, h BedSeasonHistory) *snapB {
	b.s.History[bedID] = append(b.s.History[bedID], h)
	return b
}

func (b *snapB) task(t Task) *snapB { b.s.Tasks = append(b.s.Tasks, t); return b }

func crop(id, name, family string, mutate func(*Effective)) Effective {
	e := Effective{PlantID: id, PlantName: name, Family: family, Hardiness: HardinessHardy}
	if mutate != nil {
		mutate(&e)
	}
	return e
}

// fired reports whether the result contains a warning from `check`.
func fired(res CheckResult, check string) bool {
	for _, w := range res.Warnings {
		if w.Check == check {
			return true
		}
	}
	return false
}

func warningsOf(res CheckResult, check string) []Warning {
	var out []Warning
	for _, w := range res.Warnings {
		if w.Check == check {
			out = append(out, w)
		}
	}
	return out
}

// ---- C1: companions ----

func TestC1CompanionsFires(t *testing.T) {
	rajce := crop("p-rajce", "rajče", FamilySolanaceae, nil)
	brambora := crop("p-brambora", "brambory", FamilySolanaceae, nil)
	fenykl := crop("p-fenykl", "fenykl", FamilyApiaceae, nil)

	b := newSnap(2027).bed("b1", "A1", 10)
	b.plant("x", "b1", rajce, func(p *Planting) {
		p.SowDirectOn, p.HarvestTo = sp("2027-05-01"), sp("2027-09-30")
	})
	b.plant("y", "b1", fenykl, func(p *Planting) {
		p.SowDirectOn, p.HarvestTo = sp("2027-05-15"), sp("2027-10-15")
	})
	b.rule(Rule{ID: "r1", Scope: ScopePlantPair, ARef: "p-rajce", BRef: "p-fenykl",
		Verdict: VerdictBad, Severity: SeverityError, ReasonCS: sp("Fenykl tlumí růst většiny sousedů.")})
	_ = brambora

	res := Check(b.s)
	ws := warningsOf(res, C1)
	if len(ws) != 1 {
		t.Fatalf("C1 fired %d times, want 1: %+v", len(ws), res.Warnings)
	}
	if ws[0].Severity != SeverityError {
		t.Errorf("severity = %s, want the rule's own %s", ws[0].Severity, SeverityError)
	}
	if ws[0].DetailCS != "Fenykl tlumí růst většiny sousedů." {
		t.Errorf("detail = %q, want the rule's reason", ws[0].DetailCS)
	}
	if !strings.Contains(ws[0].TitleCS, "A1") {
		t.Errorf("title %q should name the bed", ws[0].TitleCS)
	}
}

// C1'S NEGATIVE CASE, named in the acceptance criteria: two crops in one bed
// whose occupancies do not overlap must NOT warn, however bad the pairing.
func TestC1DoesNotFireWithoutOverlap(t *testing.T) {
	spenat := crop("p-spenat", "špenát", FamilyAmaranthaceae, nil)
	porek := crop("p-porek", "pórek", FamilyAmaryllidaceae, nil)

	b := newSnap(2027).bed("b1", "A1", 10)
	b.plant("x", "b1", spenat, func(p *Planting) {
		p.SowDirectOn, p.HarvestTo = sp("2027-03-15"), sp("2027-05-30")
	})
	b.plant("y", "b1", porek, func(p *Planting) {
		p.SowDirectOn, p.HarvestTo = sp("2027-07-01"), sp("2027-11-15")
	})
	b.rule(Rule{ID: "r1", Scope: ScopePlantPair, ARef: "p-spenat", BRef: "p-porek", Verdict: VerdictBad})

	if fired(Check(b.s), C1) {
		t.Error("špenát and pórek never share the bed in time — C1 must stay silent")
	}
}

// An explicit plant_pair beats a family_pair. That precedence is the entire
// reason both scopes exist.
func TestC1PlantPairBeatsFamilyPair(t *testing.T) {
	rajce := crop("p-rajce", "rajče", FamilySolanaceae, nil)
	bazalka := crop("p-bazalka", "bazalka", FamilyLamiaceae, nil)

	b := newSnap(2027).bed("b1", "A1", 10)
	b.plant("x", "b1", rajce, func(p *Planting) { p.SowDirectOn = sp("2027-05-01") })
	b.plant("y", "b1", bazalka, func(p *Planting) { p.SowDirectOn = sp("2027-05-01") })
	// The families are marked bad, but this specific pairing is explicitly good.
	b.rule(Rule{ID: "fam", Scope: ScopeFamilyPair, ARef: FamilySolanaceae, BRef: FamilyLamiaceae, Verdict: VerdictBad})
	b.rule(Rule{ID: "plant", Scope: ScopePlantPair, ARef: "p-rajce", BRef: "p-bazalka", Verdict: VerdictGood})

	if fired(Check(b.s), C1) {
		t.Error("an explicit good plant_pair must override a bad family_pair")
	}
}

func TestC1DisabledRuleEmitsNothing(t *testing.T) {
	a := crop("p-a", "A", FamilySolanaceae, nil)
	bb := crop("p-b", "B", FamilyApiaceae, nil)
	b := newSnap(2027).bed("b1", "A1", 10)
	b.plant("x", "b1", a, func(p *Planting) { p.SowDirectOn = sp("2027-05-01") })
	b.plant("y", "b1", bb, func(p *Planting) { p.SowDirectOn = sp("2027-05-01") })
	b.rule(Rule{ID: "r1", Scope: ScopePlantPair, ARef: "p-a", BRef: "p-b", Verdict: VerdictBad, IsDisabled: true})

	if fired(Check(b.s), C1) {
		t.Error("a disabled rule must emit nothing")
	}
}

// ---- C2: same family in a bed ----

func TestC2SameFamily(t *testing.T) {
	rajce := crop("p-rajce", "rajče", FamilySolanaceae, nil)
	paprika := crop("p-paprika", "paprika", FamilySolanaceae, nil)
	mrkev := crop("p-mrkev", "mrkev", FamilyApiaceae, nil)

	fires := newSnap(2027).bed("b1", "A1", 10)
	fires.plant("x", "b1", rajce, func(p *Planting) { p.SowDirectOn = sp("2027-05-01") })
	fires.plant("y", "b1", paprika, func(p *Planting) { p.SowDirectOn = sp("2027-05-01") })
	if !fired(Check(fires.s), C2) {
		t.Error("two lilkovité sharing a bed at the same time should raise C2")
	}

	quiet := newSnap(2027).bed("b1", "A1", 10)
	quiet.plant("x", "b1", rajce, func(p *Planting) { p.SowDirectOn = sp("2027-05-01") })
	quiet.plant("y", "b1", mrkev, func(p *Planting) { p.SowDirectOn = sp("2027-05-01") })
	if fired(Check(quiet.s), C2) {
		t.Error("different families in a bed must not raise C2")
	}
}

// ---- C3: rotation ----

func TestC3RotationFires(t *testing.T) {
	zeli := crop("p-zeli", "zelí", FamilyBrassicaceae, nil)
	b := newSnap(2027).bed("b1", "A1", 10)
	b.plant("x", "b1", zeli, nil)
	b.history("b1", BedSeasonHistory{Year: 2025, Families: []string{FamilyBrassicaceae}})

	ws := warningsOf(Check(b.s), C3)
	if len(ws) != 1 {
		t.Fatalf("C3 fired %d times, want 1", len(ws))
	}
	// gap 2, break 4 (settings default): 2*2 == 4, not < 4, so a warn not an error.
	if ws[0].Severity != SeverityWarn {
		t.Errorf("severity = %s, want warn", ws[0].Severity)
	}
	if !strings.Contains(ws[0].TitleCS, "předloni") {
		t.Errorf("title %q should say when, in words a person uses", ws[0].TitleCS)
	}
	if !strings.Contains(ws[0].DetailCS, "4 roky") {
		t.Errorf("detail %q should name the recommended break with the right plural", ws[0].DetailCS)
	}
}

func TestC3RotationSeverityEscalatesWhenGapIsFarTooShort(t *testing.T) {
	zeli := crop("p-zeli", "zelí", FamilyBrassicaceae, nil)
	b := newSnap(2027).bed("b1", "A1", 10)
	b.plant("x", "b1", zeli, nil)
	b.history("b1", BedSeasonHistory{Year: 2026, Families: []string{FamilyBrassicaceae}}) // gap 1 of 4

	ws := warningsOf(Check(b.s), C3)
	if len(ws) != 1 || ws[0].Severity != SeverityError {
		t.Fatalf("a gap under half the break should be an error, got %+v", ws)
	}
	if !strings.Contains(ws[0].TitleCS, "loni") {
		t.Errorf("title %q should say 'loni'", ws[0].TitleCS)
	}
}

func TestC3DoesNotFireOutsideTheBreak(t *testing.T) {
	zeli := crop("p-zeli", "zelí", FamilyBrassicaceae, nil)
	b := newSnap(2027).bed("b1", "A1", 10)
	b.plant("x", "b1", zeli, nil)
	b.history("b1", BedSeasonHistory{Year: 2022, Families: []string{FamilyBrassicaceae}}) // gap 5 > 4

	if fired(Check(b.s), C3) {
		t.Error("a family last grown outside the recommended break must not warn")
	}
}

// A CHECK THAT CANNOT RUN MUST NOT LOOK LIKE ONE THAT PASSED (D120).
func TestC3AndC8ReportNoHistoryOnAFreshInstall(t *testing.T) {
	zeli := crop("p-zeli", "zelí", FamilyBrassicaceae, func(e *Effective) { e.FeederClass = sp(FeederHeavy) })
	b := newSnap(2027).bed("b1", "A1", 10)
	b.plant("x", "b1", zeli, nil)
	b.s.ClosedSeasons = 0
	// Even with history rows present, zero CLOSED seasons means the checks are
	// structurally silent — the first season has nothing to rotate against.
	b.history("b1", BedSeasonHistory{Year: 2026, Families: []string{FamilyBrassicaceae}})

	res := Check(b.s)
	if res.History.Status != HistoryNoHistory {
		t.Errorf("history status = %q, want %q", res.History.Status, HistoryNoHistory)
	}
	if res.History.ClosedSeasons != 0 {
		t.Errorf("closed seasons = %d, want 0", res.History.ClosedSeasons)
	}
	if fired(res, C3) || fired(res, C8) {
		t.Error("C3/C8 must emit NOTHING with no closed season — not a passing result")
	}
}

// ---- C4: bed over-booked ----

func TestC4OverBooked(t *testing.T) {
	big := crop("p-dyne", "dýně", FamilyCucurbitaceae, func(e *Effective) { e.PlantsPerM2 = fp(1) })

	fires := newSnap(2027).bed("b1", "A1", 4)
	fires.plant("x", "b1", big, func(p *Planting) {
		p.AreaM2, p.SowDirectOn, p.HarvestTo = fp(3), sp("2027-05-01"), sp("2027-09-01")
	})
	fires.plant("y", "b1", big, func(p *Planting) {
		p.AreaM2, p.SowDirectOn, p.HarvestTo = fp(3), sp("2027-05-01"), sp("2027-09-01")
	})
	ws := warningsOf(Check(fires.s), C4)
	if len(ws) != 1 {
		t.Fatalf("C4 fired %d times, want 1", len(ws))
	}
	if !strings.Contains(ws[0].DetailCS, "6.0 m²") || !strings.Contains(ws[0].DetailCS, "4.0 m²") {
		t.Errorf("detail %q should state both the demand and the bed's area", ws[0].DetailCS)
	}

	// The SAME two crops in succession fit fine — over-booking is about what is
	// in the bed simultaneously.
	quiet := newSnap(2027).bed("b1", "A1", 4)
	quiet.plant("x", "b1", big, func(p *Planting) {
		p.AreaM2, p.SowDirectOn, p.HarvestTo = fp(3), sp("2027-03-01"), sp("2027-06-01")
	})
	quiet.plant("y", "b1", big, func(p *Planting) {
		p.AreaM2, p.SowDirectOn, p.HarvestTo = fp(3), sp("2027-06-15"), sp("2027-09-01")
	})
	if fired(Check(quiet.s), C4) {
		t.Error("two crops that follow each other in a bed do not over-book it")
	}
}

// ---- C5: workload spike ----

func TestC5WorkloadSpike(t *testing.T) {
	mk := func(id, from string) Task {
		return Task{ID: id, Kind: KindSowIndoor, Status: TaskOpen, WindowFrom: from, WindowTo: from}
	}
	fires := newSnap(2027).bed("b1", "A1", 10)
	for i := 0; i < 11; i++ { // threshold is 10
		fires.task(mk("t"+itoaYear(i), "2027-03-22")) // all in one ISO week
	}
	ws := warningsOf(Check(fires.s), C5)
	if len(ws) != 1 {
		t.Fatalf("C5 fired %d times, want 1", len(ws))
	}
	if !strings.Contains(ws[0].DetailCS, "11 prací") {
		t.Errorf("detail %q needs the 5+ plural form", ws[0].DetailCS)
	}

	quiet := newSnap(2027).bed("b1", "A1", 10)
	for i := 0; i < 11; i++ {
		quiet.task(mk("t"+itoaYear(i), "2027-0"+string(rune('1'+i%9))+"-15")) // spread out
	}
	if fired(Check(quiet.s), C5) {
		t.Error("work spread across the year is not a spike")
	}
}

func TestC5IgnoresDoneAndNonSowingWork(t *testing.T) {
	b := newSnap(2027).bed("b1", "A1", 10)
	for i := 0; i < 11; i++ {
		b.task(Task{ID: "t" + itoaYear(i), Kind: KindWeed, Status: TaskOpen, WindowFrom: "2027-03-22", WindowTo: "2027-03-22"})
	}
	for i := 0; i < 11; i++ {
		b.task(Task{ID: "z" + itoaYear(i), Kind: KindSowIndoor, Status: TaskDone, WindowFrom: "2027-03-22", WindowTo: "2027-03-22"})
	}
	if fired(Check(b.s), C5) {
		t.Error("C5 counts open sowings and transplants only — weeding and finished work are not the spike")
	}
}

// ---- C6: family concentration ----

func TestC6Concentration(t *testing.T) {
	solan := crop("p-rajce", "rajče", FamilySolanaceae, nil)
	api := crop("p-mrkev", "mrkev", FamilyApiaceae, nil)

	fires := newSnap(2027).bed("b1", "A1", 20).bed("b2", "A2", 20)
	fires.plant("x", "b1", solan, func(p *Planting) { p.AreaM2 = fp(9) })
	fires.plant("y", "b2", api, func(p *Planting) { p.AreaM2 = fp(1) })
	if !fired(Check(fires.s), C6) {
		t.Error("one family on 90 % of the planned area should raise C6")
	}

	// A balanced garden: three families at a third each, all under the limit.
	brass := crop("p-zeli", "zelí", FamilyBrassicaceae, nil)
	quiet := newSnap(2027).bed("b1", "A1", 20).bed("b2", "A2", 20).bed("b3", "A3", 20)
	quiet.plant("x", "b1", solan, func(p *Planting) { p.AreaM2 = fp(3) })
	quiet.plant("y", "b2", api, func(p *Planting) { p.AreaM2 = fp(3) })
	quiet.plant("z", "b3", brass, func(p *Planting) { p.AreaM2 = fp(3) })
	if fired(Check(quiet.s), C6) {
		t.Error("a third each is under the 40 % limit and must not raise C6")
	}
}

// ---- C7: empty active bed ----

func TestC7EmptyBed(t *testing.T) {
	mrkev := crop("p-mrkev", "mrkev", FamilyApiaceae, nil)

	fires := newSnap(2027).bed("b1", "A1", 10).bed("b2", "A2", 10)
	fires.plant("x", "b1", mrkev, nil)
	ws := warningsOf(Check(fires.s), C7)
	if len(ws) != 1 || ws[0].BedID == nil || *ws[0].BedID != "b2" {
		t.Fatalf("C7 should name exactly the empty bed, got %+v", ws)
	}

	quiet := newSnap(2027).bed("b1", "A1", 10)
	quiet.plant("x", "b1", mrkev, nil)
	if fired(Check(quiet.s), C7) {
		t.Error("a bed with a planting is not empty")
	}
}

func TestC7IgnoresInactiveBeds(t *testing.T) {
	b := newSnap(2027).bed("b1", "A1", 10)
	b.s.Beds[0].IsActive = false
	if fired(Check(b.s), C7) {
		t.Error("an inactive bed is deliberately out of use, not an oversight")
	}
}

// ---- C8: feeder succession ----

func TestC8HeavyAfterHeavyWarns(t *testing.T) {
	dyne := crop("p-dyne", "dýně", FamilyCucurbitaceae, func(e *Effective) { e.FeederClass = sp(FeederHeavy) })
	b := newSnap(2027).bed("b1", "A1", 10)
	b.plant("x", "b1", dyne, nil)
	b.history("b1", BedSeasonHistory{Year: 2026, Families: []string{FamilySolanaceae},
		Plantings: []BedHistoryPlanting{{PlantName: "rajče", FeederClass: FeederHeavy}}})

	ws := warningsOf(Check(b.s), C8)
	if len(ws) != 1 || ws[0].Severity != SeverityWarn {
		t.Fatalf("heavy after heavy should warn, got %+v", ws)
	}
}

// A TIP IS NOT A WARNING. C8's legume case is praise, and it must not render in
// the same language as an error — otherwise the panel teaches people that
// everything in it is noise.
func TestC8FixerBeforeHeavyIsATip(t *testing.T) {
	dyne := crop("p-dyne", "dýně", FamilyCucurbitaceae, func(e *Effective) { e.FeederClass = sp(FeederHeavy) })
	b := newSnap(2027).bed("b1", "A1", 10)
	b.plant("x", "b1", dyne, nil)
	b.history("b1", BedSeasonHistory{Year: 2026, Families: []string{FamilyFabaceae},
		Plantings: []BedHistoryPlanting{{PlantName: "hrách", FeederClass: FeederFixer}}})

	ws := warningsOf(Check(b.s), C8)
	if len(ws) != 1 || ws[0].Severity != SeverityTip {
		t.Fatalf("a fixer before a heavy feeder is a tip, got %+v", ws)
	}
	if strings.Contains(ws[0].DetailCS, "Zvažte") || strings.Contains(ws[0].TitleCS, "!") {
		t.Errorf("a tip must read as praise, got %q / %q", ws[0].TitleCS, ws[0].DetailCS)
	}
	if Check(b.s).Counts.Tip != 1 {
		t.Error("tips are counted separately from warnings")
	}
}

func TestC8QuietAfterALightFeeder(t *testing.T) {
	dyne := crop("p-dyne", "dýně", FamilyCucurbitaceae, func(e *Effective) { e.FeederClass = sp(FeederHeavy) })
	b := newSnap(2027).bed("b1", "A1", 10)
	b.plant("x", "b1", dyne, nil)
	b.history("b1", BedSeasonHistory{Year: 2026, Families: []string{FamilyApiaceae},
		Plantings: []BedHistoryPlanting{{PlantName: "mrkev", FeederClass: FeederLight}}})

	if fired(Check(b.s), C8) {
		t.Error("a heavy feeder after a light one is unremarkable")
	}
}

// ---- C9: frost-risky planting date ----

func TestC9FrostRisk(t *testing.T) {
	rajce := crop("p-rajce", "rajče", FamilySolanaceae, func(e *Effective) { e.Hardiness = HardinessTender })
	zeli := crop("p-zeli", "zelí", FamilyBrassicaceae, func(e *Effective) { e.Hardiness = HardinessHardy })

	fires := newSnap(2027).bed("b1", "A1", 10) // last frost 2027-05-15
	fires.plant("x", "b1", rajce, func(p *Planting) { p.TransplantOn = sp("2027-05-01") })
	ws := warningsOf(Check(fires.s), C9)
	if len(ws) != 1 {
		t.Fatalf("C9 fired %d times, want 1", len(ws))
	}
	if !strings.Contains(ws[0].DetailCS, "14 dní") {
		t.Errorf("detail %q should say how early, with the 5+ plural", ws[0].DetailCS)
	}

	// A hardy crop out on the same date is fine.
	quiet := newSnap(2027).bed("b1", "A1", 10)
	quiet.plant("x", "b1", zeli, func(p *Planting) { p.TransplantOn = sp("2027-05-01") })
	if fired(Check(quiet.s), C9) {
		t.Error("an otužilá crop before the last frost is not a risk")
	}

	// And a tender crop planted after it is fine too.
	after := newSnap(2027).bed("b1", "A1", 10)
	after.plant("x", "b1", rajce, func(p *Planting) { p.TransplantOn = sp("2027-05-25") })
	if fired(Check(after.s), C9) {
		t.Error("a tender crop after the last frost is exactly right")
	}
}

// ---- C10: planned date outside the crop's own window ----

func TestC10OutOfWindow(t *testing.T) {
	// Recommended direct sowing: ISO weeks 10–13 of 2027 = 8 March … 4 April.
	mrkev := crop("p-mrkev", "mrkev", FamilyApiaceae, func(e *Effective) {
		e.WinSowDirect = &Window{AnchorWeek, 10, 13}
	})

	fires := newSnap(2027).bed("b1", "A1", 10)
	fires.plant("x", "b1", mrkev, func(p *Planting) { p.SowDirectOn = sp("2027-05-20") })
	ws := warningsOf(Check(fires.s), C10)
	if len(ws) != 1 {
		t.Fatalf("C10 fired %d times, want 1", len(ws))
	}
	if !strings.Contains(ws[0].DetailCS, "8.–4.") && !strings.Contains(ws[0].DetailCS, "8. 3.") {
		t.Errorf("detail %q should state the recommended range", ws[0].DetailCS)
	}

	// Inside the window: silent.
	inside := newSnap(2027).bed("b1", "A1", 10)
	inside.plant("x", "b1", mrkev, func(p *Planting) { p.SowDirectOn = sp("2027-03-20") })
	if fired(Check(inside.s), C10) {
		t.Error("a date inside the crop's window must not warn")
	}

	// A few days out is ordinary judgement, not a mistake.
	grace := newSnap(2027).bed("b1", "A1", 10)
	grace.plant("x", "b1", mrkev, func(p *Planting) { p.SowDirectOn = sp("2027-04-08") }) // 4 days late
	if fired(Check(grace.s), C10) {
		t.Error("within the grace period C10 must stay silent")
	}
}

func TestC10SilentWhenTheAnchorIsMissing(t *testing.T) {
	rajce := crop("p-rajce", "rajče", FamilySolanaceae, func(e *Effective) {
		e.WinTransplant = &Window{AnchorLastFrost, 0, 14}
	})
	b := newSnap(2027).bed("b1", "A1", 10)
	b.s.Season.LastFrostOn = nil // the season has not set its frost date
	b.plant("x", "b1", rajce, func(p *Planting) { p.TransplantOn = sp("2027-01-01") })

	if fired(Check(b.s), C10) {
		t.Error("with no anchor there is no window to be outside of — C10 must not invent one")
	}
}

// ---- C11: neighbouring beds ----

func TestC11Neighbours(t *testing.T) {
	rajce := crop("p-rajce", "rajče", FamilySolanaceae, nil)
	fenykl := crop("p-fenykl", "fenykl", FamilyApiaceae, nil)

	// b1 and b2 are consecutive in the same zone ⇒ neighbours (D117).
	fires := newSnap(2027).bed("b1", "A1", 10).bed("b2", "A2", 10)
	fires.plant("x", "b1", rajce, func(p *Planting) { p.SowDirectOn = sp("2027-05-01") })
	fires.plant("y", "b2", fenykl, func(p *Planting) { p.SowDirectOn = sp("2027-05-01") })
	fires.rule(Rule{ID: "r1", Scope: ScopePlantPair, ARef: "p-rajce", BRef: "p-fenykl", Verdict: VerdictBad})
	ws := warningsOf(Check(fires.s), C11)
	if len(ws) != 1 || ws[0].Severity != SeverityInfo {
		t.Fatalf("adjacent beds with a bad pairing should raise one info, got %+v", ws)
	}

	// Put a third bed between them and they are no longer neighbours.
	quiet := newSnap(2027).bed("b1", "A1", 10).bed("mid", "A2", 10).bed("b2", "A3", 10)
	quiet.plant("x", "b1", rajce, func(p *Planting) { p.SowDirectOn = sp("2027-05-01") })
	quiet.plant("y", "b2", fenykl, func(p *Planting) { p.SowDirectOn = sp("2027-05-01") })
	quiet.rule(Rule{ID: "r1", Scope: ScopePlantPair, ARef: "p-rajce", BRef: "p-fenykl", Verdict: VerdictBad})
	if fired(Check(quiet.s), C11) {
		t.Error("beds with another bed between them are not neighbours")
	}
}

func TestC11CrossPollination(t *testing.T) {
	a := crop("p-dyne", "dýně", FamilyCucurbitaceae, func(e *Effective) {
		e.VarietyID, e.VarietyName = sp("v1"), sp("Hokkaido")
	})
	b2 := crop("p-dyne", "dýně", FamilyCucurbitaceae, func(e *Effective) {
		e.VarietyID, e.VarietyName = sp("v2"), sp("Muškátová")
	})
	b := newSnap(2027).bed("b1", "A1", 10).bed("b2", "A2", 10)
	b.plant("x", "b1", a, func(p *Planting) { p.SowDirectOn = sp("2027-05-01") })
	b.plant("y", "b2", b2, func(p *Planting) { p.SowDirectOn = sp("2027-05-01") })

	ws := warningsOf(Check(b.s), C11)
	if len(ws) != 1 || !strings.Contains(ws[0].TitleCS, "odrůdy") {
		t.Fatalf("two varieties of one species side by side should raise the cross-pollination note, got %+v", ws)
	}
}

// ---- dismissals, config, key stability ----

func TestDismissedWarningIsFlaggedNotDropped(t *testing.T) {
	mrkev := crop("p-mrkev", "mrkev", FamilyApiaceae, nil)
	b := newSnap(2027).bed("b1", "A1", 10).bed("b2", "A2", 10)
	b.plant("x", "b1", mrkev, nil)

	res := Check(b.s)
	ws := warningsOf(res, C7)
	if len(ws) != 1 {
		t.Fatalf("expected one C7, got %d", len(ws))
	}
	key := ws[0].Key
	before := res.Counts.Info

	b.s.Dismissals[key] = Dismissal{Key: key, Note: sp("vím, letos to risknu")}
	res2 := Check(b.s)
	ws2 := warningsOf(res2, C7)
	if len(ws2) != 1 {
		t.Fatal("a dismissed warning stays in the payload so it remains findable and restorable")
	}
	if !ws2[0].Dismissed || derefS(ws2[0].DismissedNote) != "vím, letos to risknu" {
		t.Errorf("dismissal not reflected: %+v", ws2[0])
	}
	if res2.Counts.Info != before-1 {
		t.Errorf("a dismissed warning must not be counted: %d, want %d", res2.Counts.Info, before-1)
	}
}

// The key is stable across recomputation — otherwise a dismissal would not
// survive a page reload — and CHANGES WITH THE YEAR, so a copied season does not
// inherit last year's "letos to risknu".
func TestWarningKeyStabilityAndYearScope(t *testing.T) {
	build := func(year int) CheckResult {
		mrkev := crop("p-mrkev", "mrkev", FamilyApiaceae, nil)
		b := newSnap(year).bed("b1", "A1", 10).bed("b2", "A2", 10)
		b.plant("x", "b1", mrkev, nil)
		return Check(b.s)
	}
	a1 := warningsOf(build(2027), C7)[0].Key
	a2 := warningsOf(build(2027), C7)[0].Key
	if a1 != a2 {
		t.Error("the same plan must produce the same warning key twice")
	}
	if next := warningsOf(build(2028), C7)[0].Key; next == a1 {
		t.Error("a warning key must change with the season — a copied season must not inherit dismissals")
	}
}

func TestDisabledCheckEmitsNothing(t *testing.T) {
	mrkev := crop("p-mrkev", "mrkev", FamilyApiaceae, nil)
	b := newSnap(2027).bed("b1", "A1", 10).bed("b2", "A2", 10)
	b.plant("x", "b1", mrkev, nil)
	if !fired(Check(b.s), C7) {
		t.Fatal("fixture should raise C7 before being disabled")
	}
	b.s.Settings.Checks = ChecksConfig{C7: {Enabled: bp(false)}}
	if fired(Check(b.s), C7) {
		t.Error("a disabled check must emit nothing at all")
	}
}

func TestSeverityOverrideFromSettings(t *testing.T) {
	mrkev := crop("p-mrkev", "mrkev", FamilyApiaceae, nil)
	b := newSnap(2027).bed("b1", "A1", 10).bed("b2", "A2", 10)
	b.plant("x", "b1", mrkev, nil)
	b.s.Settings.Checks = ChecksConfig{C7: {Severity: sp(SeverityWarn)}}

	ws := warningsOf(Check(b.s), C7)
	if len(ws) != 1 || ws[0].Severity != SeverityWarn {
		t.Fatalf("settings must be able to raise a check's severity, got %+v", ws)
	}
}

// A clean plan says so, and says it with an empty list rather than a nil one —
// the frontend distinguishes "no warnings" from "check did not run" through
// History, not through a missing array.
func TestCleanPlanReturnsEmptyNotNil(t *testing.T) {
	mrkev := crop("p-mrkev", "mrkev", FamilyApiaceae, nil)
	b := newSnap(2027).bed("b1", "A1", 10)
	b.plant("x", "b1", mrkev, nil)

	res := Check(b.s)
	if res.Warnings == nil {
		t.Error("Warnings must serialise as [] rather than null")
	}
	if len(res.Warnings) != 0 {
		t.Errorf("expected a clean plan, got %+v", res.Warnings)
	}
	if res.History.Status != HistoryOK {
		t.Errorf("with a closed season behind it the check ran: %+v", res.History)
	}
}

func TestWarningsSortedBySeverity(t *testing.T) {
	rajce := crop("p-rajce", "rajče", FamilySolanaceae, func(e *Effective) { e.Hardiness = HardinessTender })
	b := newSnap(2027).bed("b1", "A1", 10).bed("b2", "A2", 10)
	b.plant("x", "b1", rajce, func(p *Planting) { p.TransplantOn = sp("2027-05-01") }) // C9 warn
	// b2 empty ⇒ C7 info.

	res := Check(b.s)
	if len(res.Warnings) < 2 {
		t.Fatalf("expected at least two warnings, got %+v", res.Warnings)
	}
	rank := map[string]int{SeverityError: 0, SeverityWarn: 1, SeverityInfo: 2, SeverityTip: 3}
	for i := 1; i < len(res.Warnings); i++ {
		if rank[res.Warnings[i-1].Severity] > rank[res.Warnings[i].Severity] {
			t.Errorf("warnings out of severity order at %d: %+v", i, res.Warnings)
		}
	}
}
