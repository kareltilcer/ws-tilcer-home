package garden_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/garden"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	platformmetrics "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// ---- harness ----

type h struct {
	t   *testing.T
	svc *garden.Service
	db  *sql.DB
}

func newH(t *testing.T) *h {
	t.Helper()
	db := testsupport.NewDB(t)
	loc, err := time.LoadLocation("Europe/Prague")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	svc := garden.NewService(db, audit.NewSink(), nil, garden.Options{Location: loc})
	return &h{t: t, svc: svc, db: db}
}

func editorCtx() context.Context { return testsupport.CtxUser("u-editor", "editor") }

func sp(s string) *string   { return &s }
func ip(v int) *int         { return &v }
func fp(v float64) *float64 { return &v }
func bp(v bool) *bool       { return &v }

// status pulls the HTTP status out of the service's error, which is what the
// acceptance criteria are written in terms of.
func status(err error) int {
	var apiErr *httpx.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
}

func (x *h) mustPlant(name, family string, mutate func(*garden.PlantInput)) garden.Plant {
	x.t.Helper()
	in := garden.PlantInput{
		NameCS: sp(name), Family: sp(family),
		PlantType: sp("vegetable"), Hardiness: sp(garden.HardinessHardy),
	}
	in.HarvestUnit = sp(garden.UnitKg)
	if mutate != nil {
		mutate(&in)
	}
	p, err := x.svc.CreatePlant(editorCtx(), in)
	if err != nil {
		x.t.Fatalf("create plant %q: %v", name, err)
	}
	return p
}

func (x *h) mustBed(name, code string, area float64) garden.Bed {
	x.t.Helper()
	b, err := x.svc.CreateBed(editorCtx(), garden.BedInput{
		Name: sp(name), Code: sp(code), Type: sp("ground"), AreaM2: fp(area), Zone: sp("hlavní"),
	})
	if err != nil {
		x.t.Fatalf("create bed %q: %v", code, err)
	}
	return b
}

// mustWeather seeds the forecast cache directly. There is no service-level way
// in on purpose — the only writer is the poll — so the test writes the rows the
// poll would have.
func (x *h) mustWeather(minByDay map[string]float64) {
	x.t.Helper()
	for day, min := range minByDay {
		if _, err := x.db.Exec(
			`INSERT INTO garden_weather_days (day, temp_min, fetched_at, source) VALUES (?, ?, ?, ?)`,
			day, min, "2026-04-12T06:00:00.000Z", "test"); err != nil {
			x.t.Fatalf("seed weather %s: %v", day, err)
		}
	}
}

func (x *h) mustSeason(year int) garden.Season {
	x.t.Helper()
	se, _, err := x.svc.CreateSeason(editorCtx(), garden.SeasonCreateInput{
		Year: year, LastFrostOn: sp(itoa(year) + "-05-15"), FirstFrostOn: sp(itoa(year) + "-10-05"),
	}, false)
	if err != nil {
		x.t.Fatalf("create season %d: %v", year, err)
	}
	return se
}

func itoa(v int) string {
	out := []byte("0000")
	for i := 3; i >= 0; i-- {
		out[i] = byte('0' + v%10)
		v /= 10
	}
	return string(out)
}

// ---- the test that keeps the seed split honest ----

// THE SEED MUST NOT REACH A TEST DATABASE (PRD D115).
//
// testsupport.NewDB migrates bootstrap.MigrationFS(), which is schema-only. If
// the built-in rules ever leak into it, a C1 fixture would pass because a SEEDED
// rule matched rather than the one the test wrote — a false green that is very
// hard to see, and impossible to notice from the outside.
func TestSeedExcludedFromTestDB(t *testing.T) {
	x := newH(t)
	var n int
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM garden_rules`).Scan(&n); err != nil {
		t.Fatalf("count rules: %v", err)
	}
	if n != 0 {
		t.Errorf("a fresh test database has %d garden_rules, want 0 — the built-in seed has leaked "+
			"into bootstrap.MigrationSources() and every check fixture is now suspect", n)
	}
}

// The eleven tables and the FTS index all exist after migration.
func TestMigrationCreatesEveryTable(t *testing.T) {
	x := newH(t)
	for _, table := range []string{
		"garden_plants", "garden_varieties", "garden_beds", "garden_seasons",
		"garden_plantings", "garden_tasks", "garden_harvests", "garden_storage_items",
		"garden_rules", "garden_warning_dismissals", "garden_settings", "garden_weather_days",
		"garden_plants_fts",
	} {
		var name string
		err := x.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE name = ? AND type IN ('table','view')`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing after migration: %v", table, err)
		}
	}
	// The settings singleton is created by the migration, so every read is a
	// plain SELECT and no write path has to remember to upsert it first.
	var count int
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM garden_settings`).Scan(&count); err != nil || count != 1 {
		t.Errorf("garden_settings should hold exactly one row, got %d (err %v)", count, err)
	}
}

// ---- crops ----

func TestPlantRequiresFamilyAndHardiness(t *testing.T) {
	x := newH(t)
	cases := []struct {
		name string
		in   garden.PlantInput
	}{
		{"no family", garden.PlantInput{NameCS: sp("rajče"), PlantType: sp("vegetable"), Hardiness: sp(garden.HardinessTender)}},
		{"no hardiness", garden.PlantInput{NameCS: sp("rajče"), PlantType: sp("vegetable"), Family: sp(garden.FamilySolanaceae)}},
		{"no name", garden.PlantInput{Family: sp(garden.FamilySolanaceae), PlantType: sp("vegetable"), Hardiness: sp(garden.HardinessTender)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.HarvestUnit = sp(garden.UnitKg)
			if _, err := x.svc.CreatePlant(editorCtx(), tc.in); status(err) != http.StatusUnprocessableEntity {
				t.Errorf("expected 422, got %v", err)
			}
		})
	}
}

func TestPlantDuplicateNameIsConflict(t *testing.T) {
	x := newH(t)
	x.mustPlant("rajče", garden.FamilySolanaceae, nil)
	_, err := x.svc.CreatePlant(editorCtx(), garden.PlantInput{
		NameCS: sp("rajče"), Family: sp(garden.FamilySolanaceae),
		PlantType: sp("vegetable"), Hardiness: sp(garden.HardinessTender),
		PlantCore: garden.PlantCore{HarvestUnit: sp(garden.UnitKg)},
	})
	if status(err) != http.StatusConflict {
		t.Errorf("expected 409 on a duplicate crop name, got %v", err)
	}
}

func TestVarietyInheritsAndOverrides(t *testing.T) {
	x := newH(t)
	plant := x.mustPlant("rajče", garden.FamilySolanaceae, func(in *garden.PlantInput) {
		in.WinTransplant = &garden.Window{Anchor: garden.AnchorLastFrost, From: 0, To: 14}
		in.SpacingRowCM, in.SpacingPlantCM = fp(60), fp(50)
	})
	inherit, err := x.svc.CreateVariety(editorCtx(), plant.ID, garden.VarietyInput{Name: sp("Black Krim")})
	if err != nil {
		t.Fatalf("create variety: %v", err)
	}
	if inherit.Effective == nil || inherit.Effective.WinTransplant == nil {
		t.Fatal("a variety with no overrides must resolve the species' window")
	}
	if *inherit.Effective.WinTransplant != (garden.Window{Anchor: garden.AnchorLastFrost, From: 0, To: 14}) {
		t.Errorf("inherited window = %+v", *inherit.Effective.WinTransplant)
	}

	override, err := x.svc.CreateVariety(editorCtx(), plant.ID, garden.VarietyInput{
		Name:      sp("Raný"),
		PlantCore: garden.PlantCore{WinTransplant: &garden.Window{Anchor: garden.AnchorLastFrost, From: -7, To: 7}},
	})
	if err != nil {
		t.Fatalf("create variety: %v", err)
	}
	if override.Effective.WinTransplant.From != -7 {
		t.Errorf("override not applied: %+v", *override.Effective.WinTransplant)
	}
	// And the species is untouched by either.
	fresh, err := x.svc.GetPlant(editorCtx(), plant.ID)
	if err != nil || fresh.WinTransplant.From != 0 {
		t.Errorf("the species window changed: %+v (err %v)", fresh.WinTransplant, err)
	}
}

// ---- beds ----

func TestBedAreaDerivedFromDimensions(t *testing.T) {
	x := newH(t)
	b, err := x.svc.CreateBed(editorCtx(), garden.BedInput{
		Name: sp("Horní"), Code: sp("A1"), Type: sp("raised"), LengthCM: fp(400), WidthCM: fp(120),
	})
	if err != nil {
		t.Fatalf("create bed: %v", err)
	}
	if b.AreaM2 != 4.8 {
		t.Errorf("area = %v, want 4.8 m² from 400×120 cm", b.AreaM2)
	}
}

// Bed ORDER is the adjacency model (D117), so neighbours are derived from it and
// a move changes them.
func TestBedNeighboursFollowOrder(t *testing.T) {
	x := newH(t)
	a := x.mustBed("První", "A1", 5)
	b := x.mustBed("Druhý", "A2", 5)
	c := x.mustBed("Třetí", "A3", 5)

	beds, err := x.svc.ListBeds(editorCtx(), false)
	if err != nil {
		t.Fatalf("list beds: %v", err)
	}
	byID := map[string]garden.Bed{}
	for _, bed := range beds {
		byID[bed.ID] = bed
	}
	if got := byID[b.ID].Neighbours; len(got) != 2 || got[0] != a.ID || got[1] != c.ID {
		t.Errorf("middle bed's neighbours = %v, want [%s %s]", got, a.ID, c.ID)
	}
	if got := byID[a.ID].Neighbours; len(got) != 1 || got[0] != b.ID {
		t.Errorf("first bed has one neighbour, got %v", got)
	}
}

func TestDeleteBedWithOpenPlantingsIsConflict(t *testing.T) {
	x := newH(t)
	se := x.mustSeason(2027)
	bed := x.mustBed("Horní", "A1", 5)
	plant := x.mustPlant("mrkev", garden.FamilyApiaceae, nil)
	if _, err := x.svc.CreatePlanting(editorCtx(), garden.PlantingInput{
		SeasonYear: ip(se.Year), BedID: sp(bed.ID), PlantID: sp(plant.ID), AreaM2: fp(1),
	}); err != nil {
		t.Fatalf("create planting: %v", err)
	}
	if err := x.svc.DeleteBed(editorCtx(), bed.ID); status(err) != http.StatusConflict {
		t.Errorf("expected 409 deleting a bed with live plantings, got %v", err)
	}
}

// ---- plantings ----

func TestPlantingRequiresExactlyOneOfAreaOrCount(t *testing.T) {
	x := newH(t)
	se := x.mustSeason(2027)
	bed := x.mustBed("Horní", "A1", 5)
	plant := x.mustPlant("mrkev", garden.FamilyApiaceae, nil)

	base := garden.PlantingInput{SeasonYear: ip(se.Year), BedID: sp(bed.ID), PlantID: sp(plant.ID)}
	for _, tc := range []struct {
		name string
		in   garden.PlantingInput
	}{
		{"neither", base},
		{"both", garden.PlantingInput{SeasonYear: ip(se.Year), BedID: sp(bed.ID), PlantID: sp(plant.ID),
			AreaM2: fp(1), PlantCount: ip(10)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := x.svc.CreatePlanting(editorCtx(), tc.in); status(err) != http.StatusUnprocessableEntity {
				t.Errorf("expected 422, got %v", err)
			}
		})
	}
}

// Creating a planting fills its planned dates from the crop's windows AND
// generates the work — the two halves of "putting a crop in a bed".
func TestPlantingCreateFillsDatesAndGeneratesTasks(t *testing.T) {
	x := newH(t)
	se := x.mustSeason(2027)
	bed := x.mustBed("Horní", "A1", 5)
	plant := x.mustPlant("rajče", garden.FamilySolanaceae, func(in *garden.PlantInput) {
		in.Hardiness = sp(garden.HardinessTender)
		in.WinSowIndoor = &garden.Window{Anchor: garden.AnchorLastFrost, From: -56, To: -42}
		in.WinTransplant = &garden.Window{Anchor: garden.AnchorLastFrost, From: 0, To: 14}
		in.WinHarvest = &garden.Window{Anchor: garden.AnchorWeek, From: 28, To: 40}
		in.NeedsSupport = bp(true)
	})

	p, err := x.svc.CreatePlanting(editorCtx(), garden.PlantingInput{
		SeasonYear: ip(se.Year), BedID: sp(bed.ID), PlantID: sp(plant.ID), AreaM2: fp(2),
	})
	if err != nil {
		t.Fatalf("create planting: %v", err)
	}
	// Last frost 2027-05-15, so sowing indoors opens 56 days earlier.
	if got := deref(p.SowIndoorOn); got != "2027-03-20" {
		t.Errorf("sow_indoor_on = %q, want 2027-03-20 (56 days before the last frost)", got)
	}
	if got := deref(p.TransplantOn); got != "2027-05-15" {
		t.Errorf("transplant_on = %q, want the last frost date", got)
	}

	page, err := x.svc.ListTasks(editorCtx(), garden.TaskFilter{PlantingID: p.ID}, 200, "")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatal("creating a planting must generate its work")
	}
	kinds := map[string]bool{}
	for _, task := range page.Items {
		kinds[task.Kind] = true
		if !task.IsGenerated {
			t.Errorf("%s should be generated", task.Kind)
		}
	}
	for _, want := range []string{garden.KindSowIndoor, garden.KindTransplant, garden.KindSupport, garden.KindHarvest} {
		if !kinds[want] {
			t.Errorf("missing generated %s task", want)
		}
	}
}

// D119, end to end: recording an actual sow date states the drift and moves NO
// planned window.
func TestActualDateDoesNotMovePlannedWindows(t *testing.T) {
	x := newH(t)
	se := x.mustSeason(2027)
	bed := x.mustBed("Horní", "A1", 5)
	plant := x.mustPlant("mrkev", garden.FamilyApiaceae, func(in *garden.PlantInput) {
		in.WinSowDirect = &garden.Window{Anchor: garden.AnchorWeek, From: 12, To: 14}
		in.WinHarvest = &garden.Window{Anchor: garden.AnchorWeek, From: 28, To: 34}
	})
	p, err := x.svc.CreatePlanting(editorCtx(), garden.PlantingInput{
		SeasonYear: ip(se.Year), BedID: sp(bed.ID), PlantID: sp(plant.ID), AreaM2: fp(1),
	})
	if err != nil {
		t.Fatalf("create planting: %v", err)
	}
	plannedSow, plannedHarvest := deref(p.SowDirectOn), deref(p.HarvestFrom)

	late := mustDateAfter(t, plannedSow, 14)
	updated, err := x.svc.UpdatePlanting(editorCtx(), p.ID, garden.PlantingInput{SowedOn: sp(late)})
	if err != nil {
		t.Fatalf("record actual sow date: %v", err)
	}
	if deref(updated.SowDirectOn) != plannedSow || deref(updated.HarvestFrom) != plannedHarvest {
		t.Error("an actual date must not re-drive the plan (D119)")
	}
	if updated.Drift == nil || updated.Drift.Days != 14 {
		t.Fatalf("drift = %+v, want +14 days", updated.Drift)
	}
	if !strings.Contains(updated.Drift.MessageCS, "sklizeň v plánu beze změny") {
		t.Errorf("drift message = %q — it should say the plan did not move", updated.Drift.MessageCS)
	}
}

// shift-tasks is the ONE action offered against the drift, and its effect is
// permanent: the moved tasks are is_edited, so regeneration leaves them alone
// forever after.
func TestShiftTasksMarksEditedAndSurvivesRegeneration(t *testing.T) {
	x := newH(t)
	se := x.mustSeason(2027)
	bed := x.mustBed("Horní", "A1", 5)
	plant := x.mustPlant("rajče", garden.FamilySolanaceae, func(in *garden.PlantInput) {
		in.Hardiness = sp(garden.HardinessTender)
		in.WinTransplant = &garden.Window{Anchor: garden.AnchorLastFrost, From: 0, To: 14}
		in.WinHarvest = &garden.Window{Anchor: garden.AnchorWeek, From: 28, To: 40}
	})
	p, err := x.svc.CreatePlanting(editorCtx(), garden.PlantingInput{
		SeasonYear: ip(se.Year), BedID: sp(bed.ID), PlantID: sp(plant.ID), AreaM2: fp(2),
	})
	if err != nil {
		t.Fatalf("create planting: %v", err)
	}

	shifted, err := x.svc.ShiftTasks(editorCtx(), p.ID, 10, nil)
	if err != nil {
		t.Fatalf("shift tasks: %v", err)
	}
	if len(shifted) == 0 {
		t.Fatal("shift-tasks should move the remaining open work")
	}
	before := map[string]string{}
	for _, task := range shifted {
		if !task.IsEdited {
			t.Errorf("%s should be marked is_edited after a manual shift", task.Kind)
		}
		before[task.ID] = task.WindowFrom
	}

	// Now change the plan. Regeneration must leave every shifted task exactly
	// where the user put it.
	if _, err := x.svc.UpdatePlanting(editorCtx(), p.ID, garden.PlantingInput{
		TransplantOn: sp("2027-06-01"),
	}); err != nil {
		t.Fatalf("update planting: %v", err)
	}
	page, err := x.svc.ListTasks(editorCtx(), garden.TaskFilter{PlantingID: p.ID}, 200, "")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range page.Items {
		if want, ok := before[task.ID]; ok && task.WindowFrom != want {
			t.Errorf("%s moved from %s to %s despite is_edited — D110 protects it permanently",
				task.Kind, want, task.WindowFrom)
		}
	}
}

// A deleted generated task leaves a tombstone and does not come back.
func TestDeletedGeneratedTaskStaysDeleted(t *testing.T) {
	x := newH(t)
	se := x.mustSeason(2027)
	bed := x.mustBed("Horní", "A1", 5)
	plant := x.mustPlant("rajče", garden.FamilySolanaceae, func(in *garden.PlantInput) {
		in.WinTransplant = &garden.Window{Anchor: garden.AnchorLastFrost, From: 0, To: 14}
		in.NeedsSupport = bp(true)
	})
	p, err := x.svc.CreatePlanting(editorCtx(), garden.PlantingInput{
		SeasonYear: ip(se.Year), BedID: sp(bed.ID), PlantID: sp(plant.ID), AreaM2: fp(2),
	})
	if err != nil {
		t.Fatalf("create planting: %v", err)
	}
	page, _ := x.svc.ListTasks(editorCtx(), garden.TaskFilter{PlantingID: p.ID}, 200, "")
	var victim string
	for _, task := range page.Items {
		if task.Kind == garden.KindSupport {
			victim = task.ID
		}
	}
	if victim == "" {
		t.Fatal("fixture should generate a support task")
	}
	if err := x.svc.DeleteTask(editorCtx(), victim); err != nil {
		t.Fatalf("delete task: %v", err)
	}

	// Change the plan enough to force a regeneration.
	if _, err := x.svc.UpdatePlanting(editorCtx(), p.ID, garden.PlantingInput{
		TransplantOn: sp("2027-05-25"),
	}); err != nil {
		t.Fatalf("update planting: %v", err)
	}
	after, _ := x.svc.ListTasks(editorCtx(), garden.TaskFilter{PlantingID: p.ID}, 200, "")
	for _, task := range after.Items {
		if task.Kind == garden.KindSupport {
			t.Error("a deleted generated task must not resurrect — the tombstone holds its key")
		}
	}
}

// ---- tasks ----

// D131: the 2000 ms hold can fire twice on a bad connection, and the second send
// must not look like an error.
func TestCompleteTaskIsIdempotent(t *testing.T) {
	x := newH(t)
	se := x.mustSeason(2027)
	task, err := x.svc.CreateTask(editorCtx(), garden.TaskInput{
		SeasonYear: ip(se.Year), Kind: sp(garden.KindWeed), TitleCS: sp("Vyplet A1"),
		WindowFrom: sp("2027-06-01"), WindowTo: sp("2027-06-10"),
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	first, err := x.svc.CompleteTask(editorCtx(), task.ID, map[string]any{"via": "dashboard"})
	if err != nil || first.Status != garden.TaskDone {
		t.Fatalf("complete = %+v, err %v", first, err)
	}
	second, err := x.svc.CompleteTask(editorCtx(), task.ID, map[string]any{"via": "dashboard"})
	if err != nil {
		t.Fatalf("completing an already-complete task must be a 200, got %v", err)
	}
	if second.CompletedAt == nil || *second.CompletedAt != *first.CompletedAt {
		t.Error("a duplicate completion must not restamp the completion time")
	}
	// And it wrote only one audit event, not two.
	var n int
	if err := x.db.QueryRow(
		`SELECT COUNT(*) FROM audit_events WHERE module = 'garden' AND entity_id = ? AND summary LIKE 'Hotovo%'`,
		task.ID).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n != 1 {
		t.Errorf("a duplicate completion wrote %d audit events, want 1", n)
	}
}

// Editing a GENERATED task takes it over permanently (D110).
func TestEditingAGeneratedTaskSetsIsEdited(t *testing.T) {
	x := newH(t)
	se := x.mustSeason(2027)
	bed := x.mustBed("Horní", "A1", 5)
	plant := x.mustPlant("rajče", garden.FamilySolanaceae, func(in *garden.PlantInput) {
		in.WinTransplant = &garden.Window{Anchor: garden.AnchorLastFrost, From: 0, To: 14}
	})
	p, _ := x.svc.CreatePlanting(editorCtx(), garden.PlantingInput{
		SeasonYear: ip(se.Year), BedID: sp(bed.ID), PlantID: sp(plant.ID), AreaM2: fp(2),
	})
	page, _ := x.svc.ListTasks(editorCtx(), garden.TaskFilter{PlantingID: p.ID}, 200, "")
	if len(page.Items) == 0 {
		t.Fatal("fixture should generate tasks")
	}
	edited, err := x.svc.UpdateTask(editorCtx(), page.Items[0].ID, garden.TaskInput{TitleCS: sp("Moje vlastní práce")})
	if err != nil {
		t.Fatalf("update task: %v", err)
	}
	if !edited.IsEdited {
		t.Error("editing a generated task must set is_edited")
	}
}

// ---- seasons ----

// Closed seasons are FROZEN, and the guard is one helper called from every write
// rather than a check remembered per handler.
func TestClosedSeasonRefusesEveryWrite(t *testing.T) {
	x := newH(t)
	se := x.mustSeason(2027)
	bed := x.mustBed("Horní", "A1", 5)
	plant := x.mustPlant("mrkev", garden.FamilyApiaceae, nil)
	p, err := x.svc.CreatePlanting(editorCtx(), garden.PlantingInput{
		SeasonYear: ip(se.Year), BedID: sp(bed.ID), PlantID: sp(plant.ID), AreaM2: fp(1),
	})
	if err != nil {
		t.Fatalf("create planting: %v", err)
	}
	if _, err := x.svc.CloseSeason(editorCtx(), se.Year, garden.SeasonCloseInput{
		LastFrostActualOn: sp("2027-05-20"),
		Outcomes: []garden.SeasonCloseOutcome{
			{PlantingID: p.ID, Status: garden.PlantingDone, FinalYield: fp(4.5)},
		},
	}); err != nil {
		t.Fatalf("close season: %v", err)
	}

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"update planting", func() error {
			_, err := x.svc.UpdatePlanting(editorCtx(), p.ID, garden.PlantingInput{NotesMD: sp("pozdě")})
			return err
		}},
		{"delete planting", func() error { return x.svc.DeletePlanting(editorCtx(), p.ID) }},
		{"new planting", func() error {
			_, err := x.svc.CreatePlanting(editorCtx(), garden.PlantingInput{
				SeasonYear: ip(se.Year), BedID: sp(bed.ID), PlantID: sp(plant.ID), AreaM2: fp(1)})
			return err
		}},
		{"update season", func() error {
			_, err := x.svc.UpdateSeason(editorCtx(), se.Year, garden.SeasonUpdateInput{NotesMD: sp("x")})
			return err
		}},
		{"close again", func() error {
			_, err := x.svc.CloseSeason(editorCtx(), se.Year, garden.SeasonCloseInput{})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := status(tc.call()); got != http.StatusConflict {
				t.Errorf("expected 409 against a closed season, got %d", got)
			}
		})
	}

	// And the closed season IS now rotation history — which is the only thing
	// that creates any.
	hist, err := x.svc.BedHistory(editorCtx(), bed.ID)
	if err != nil {
		t.Fatalf("bed history: %v", err)
	}
	if len(hist.Seasons) != 1 || hist.Seasons[0].Year != 2027 {
		t.Errorf("closing a season must make it rotation history, got %+v", hist.Seasons)
	}
}

// The season close records the final yield as an ordinary harvest, so the season
// total and the per-planting yield read from one place.
func TestSeasonCloseRecordsFinalYield(t *testing.T) {
	x := newH(t)
	se := x.mustSeason(2027)
	bed := x.mustBed("Horní", "A1", 5)
	plant := x.mustPlant("mrkev", garden.FamilyApiaceae, nil)
	p, _ := x.svc.CreatePlanting(editorCtx(), garden.PlantingInput{
		SeasonYear: ip(se.Year), BedID: sp(bed.ID), PlantID: sp(plant.ID), AreaM2: fp(1),
	})
	if _, err := x.svc.CloseSeason(editorCtx(), se.Year, garden.SeasonCloseInput{
		Outcomes: []garden.SeasonCloseOutcome{{PlantingID: p.ID, Status: garden.PlantingDone, FinalYield: fp(3.5)}},
	}); err != nil {
		t.Fatalf("close season: %v", err)
	}
	page, err := x.svc.ListHarvests(editorCtx(), p.ID, nil, 50, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].Quantity != 3.5 {
		t.Errorf("final yield should become a harvest row, got %+v (err %v)", page.Items, err)
	}
}

// A `failed` planting with a reason is DATA, not an embarrassment — recorded
// plainly, and it still counts as history.
func TestSeasonCloseRecordsFailure(t *testing.T) {
	x := newH(t)
	se := x.mustSeason(2027)
	bed := x.mustBed("Horní", "A1", 5)
	plant := x.mustPlant("mrkev", garden.FamilyApiaceae, nil)
	p, _ := x.svc.CreatePlanting(editorCtx(), garden.PlantingInput{
		SeasonYear: ip(se.Year), BedID: sp(bed.ID), PlantID: sp(plant.ID), AreaM2: fp(1),
	})
	if _, err := x.svc.CloseSeason(editorCtx(), se.Year, garden.SeasonCloseInput{
		Outcomes: []garden.SeasonCloseOutcome{
			{PlantingID: p.ID, Status: garden.PlantingFailed, FailReason: sp("sežrali slimáci")},
		},
	}); err != nil {
		t.Fatalf("close season: %v", err)
	}
	got, err := x.svc.GetPlanting(editorCtx(), p.ID)
	if err != nil || got.Status != garden.PlantingFailed || deref(got.FailReason) != "sežrali slimáci" {
		t.Errorf("failure not recorded: %+v (err %v)", got, err)
	}
}

// Reopening rewrites the record C3 and C8 depend on, so it is the module's ONLY
// admin-gated action. The gate is on the route (RequireAdmin); the service
// itself refuses a season that is not closed.
func TestReopenRequiresAClosedSeason(t *testing.T) {
	x := newH(t)
	se := x.mustSeason(2027)
	if _, err := x.svc.ReopenSeason(editorCtx(), se.Year); status(err) != http.StatusConflict {
		t.Errorf("reopening an open season should be 409, got %v", err)
	}
	if _, err := x.svc.CloseSeason(editorCtx(), se.Year, garden.SeasonCloseInput{}); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, err := x.svc.ReopenSeason(editorCtx(), se.Year)
	if err != nil || reopened.Status == garden.SeasonClosed {
		t.Errorf("reopen failed: %+v (err %v)", reopened, err)
	}
	var n int
	if err := x.db.QueryRow(
		`SELECT COUNT(*) FROM audit_events WHERE module='garden' AND action='season.reopen'`).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n != 1 {
		t.Errorf("reopen must be audited, got %d events", n)
	}
}

// Copy-season with a shift reproduces the plan, re-anchors frost-anchored dates,
// leaves week-anchored ones alone, and dry_run persists NOTHING.
func TestCopySeasonDryRunPersistsNothing(t *testing.T) {
	x := newH(t)
	src := x.mustSeason(2027)
	bed := x.mustBed("Horní", "A1", 5)
	x.mustBed("Dolní", "A2", 5)
	plant := x.mustPlant("rajče", garden.FamilySolanaceae, func(in *garden.PlantInput) {
		in.WinTransplant = &garden.Window{Anchor: garden.AnchorLastFrost, From: 0, To: 14}
		in.WinHarvest = &garden.Window{Anchor: garden.AnchorWeek, From: 28, To: 34}
	})
	if _, err := x.svc.CreatePlanting(editorCtx(), garden.PlantingInput{
		SeasonYear: ip(src.Year), BedID: sp(bed.ID), PlantID: sp(plant.ID), AreaM2: fp(2),
	}); err != nil {
		t.Fatalf("create planting: %v", err)
	}

	_, preview, err := x.svc.CreateSeason(editorCtx(), garden.SeasonCreateInput{
		Year: 2028, LastFrostOn: sp("2028-05-25"), FirstFrostOn: sp("2028-10-05"),
		CopyFrom: ip(2027), Shift: &garden.SeasonShift{Offset: 1},
	}, true)
	if err != nil {
		t.Fatalf("dry-run copy: %v", err)
	}
	if preview == nil || len(preview.Plantings) != 1 {
		t.Fatalf("preview should carry the prospective plan, got %+v", preview)
	}
	// The frost-anchored transplant follows 2028's later frost date; the
	// week-anchored harvest window does not care about frost at all.
	if got := deref(preview.Plantings[0].TransplantOn); got != "2028-05-25" {
		t.Errorf("frost-anchored date = %q, want the new season's frost date", got)
	}
	if got := deref(preview.Plantings[0].HarvestFrom); !strings.HasPrefix(got, "2028-07") {
		t.Errorf("week-anchored harvest = %q, want ISO week 28 of 2028", got)
	}
	// The shift moved it to the next bed.
	if deref(preview.Plantings[0].BedID) == bed.ID {
		t.Error("a shift of 1 should move the planting to the next bed")
	}
	if preview.CheckBefore == nil {
		t.Error("the preview must carry the SOURCE season's check too, so the UI can show what the shift fixed")
	}
	// NOTHING WAS PERSISTED.
	if _, err := x.svc.GetSeason(editorCtx(), 2028); status(err) != http.StatusNotFound {
		t.Error("dry_run must persist nothing")
	}
}

// ---- rules ----

func TestBuiltinRuleCannotBeDeletedButCanBeDisabled(t *testing.T) {
	x := newH(t)
	// The seed is excluded from test databases, so a built-in is inserted here
	// directly — the point under test is the SERVICE's behaviour, not the seed's.
	if _, err := x.db.Exec(
		`INSERT INTO garden_rules (id, scope, a_ref, b_ref, verdict, severity, reason_cs, source,
		    is_builtin, is_disabled, created_at, updated_at)
		 VALUES ('r-builtin','family_pair','amaryllidaceae','fabaceae','bad','warn','Cibule tlumí luskoviny.',
		    'agronomie — alelopatie', 1, 0, '2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed a built-in: %v", err)
	}

	if err := x.svc.DeleteRule(editorCtx(), "r-builtin"); status(err) != http.StatusConflict {
		t.Errorf("deleting a built-in rule must be 409 (D130), got %v", err)
	}
	updated, err := x.svc.UpdateRule(editorCtx(), "r-builtin", garden.RuleInput{IsDisabled: bp(true)})
	if err != nil {
		t.Fatalf("disabling a built-in must succeed: %v", err)
	}
	if !updated.IsDisabled {
		t.Error("the rule should now be disabled")
	}
	// But its CLAIM cannot be rewritten — that is what keeps `source` meaningful.
	if _, err := x.svc.UpdateRule(editorCtx(), "r-builtin", garden.RuleInput{
		ReasonCS: sp("protože"),
	}); status(err) != http.StatusUnprocessableEntity {
		t.Error("rewriting a built-in rule's reason must be refused")
	}
}

func TestUserRuleRoundTrip(t *testing.T) {
	x := newH(t)
	a := x.mustPlant("rajče", garden.FamilySolanaceae, nil)
	b := x.mustPlant("fenykl", garden.FamilyApiaceae, nil)

	// Deliberately entered in the "wrong" order: canonical storage is what makes
	// symmetry structural rather than a discipline.
	created, err := x.svc.CreateRule(editorCtx(), garden.RuleInput{
		Scope: sp(garden.ScopePlantPair), ARef: sp(b.ID), BRef: sp(a.ID),
		Verdict: sp(garden.VerdictBad), ReasonCS: sp("Fenykl tlumí růst sousedů."),
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if created.ARef > created.BRef {
		t.Errorf("pair not stored in canonical order: %s > %s", created.ARef, created.BRef)
	}
	// A duplicate in either direction is a conflict, not a second row.
	if _, err := x.svc.CreateRule(editorCtx(), garden.RuleInput{
		Scope: sp(garden.ScopePlantPair), ARef: sp(a.ID), BRef: sp(b.ID), Verdict: sp(garden.VerdictBad),
	}); status(err) != http.StatusConflict {
		t.Errorf("a duplicate pair must be 409, got %v", err)
	}
	if err := x.svc.DeleteRule(editorCtx(), created.ID); err != nil {
		t.Errorf("a user rule must be deletable: %v", err)
	}
}

// ---- storage ----

func TestStorageConsumptionAndAutoConsumed(t *testing.T) {
	x := newH(t)
	it, err := x.svc.CreateStorage(editorCtx(), garden.StorageInput{
		ProductName: sp("okurky sterilované"), Method: sp("can"), Unit: sp(garden.UnitKs),
		QuantityInitial: fp(12), StoredOn: sp("2027-08-20"),
	})
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	if it.QuantityRemaining != 12 || it.Status != garden.StorageStored {
		t.Errorf("a new item starts full and stored, got %+v", it)
	}
	if _, err := x.svc.UpdateStorage(editorCtx(), it.ID, garden.StorageInput{
		QuantityRemaining: fp(20),
	}); status(err) != http.StatusUnprocessableEntity {
		t.Error("remaining above initial must be refused")
	}
	// Reaching zero flips the status by itself — asking someone to also change a
	// dropdown is how the list goes stale.
	empty, err := x.svc.UpdateStorage(editorCtx(), it.ID, garden.StorageInput{QuantityRemaining: fp(0)})
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if empty.Status != garden.StorageConsumed {
		t.Errorf("status = %s, want consumed once remaining hits zero", empty.Status)
	}
}

// ---- import and export ----

func TestImportPreviewsThenApplies(t *testing.T) {
	x := newH(t)
	payload := `{
		"name_cs": "rajče",
		"family": "lilkovité",
		"plant_type": "zelenina",
		"hardiness": "citlivá",
		"harvest_unit": "kg",
		"feeder_class": "vysoký",
		"win_transplant": {"anchor": "last_frost", "from": 0, "to": 14},
		"spacing_row_cm": 60,
		"barvicka": "červená"
	}`
	// dry_run first: nothing is written, and the unmapped field is REPORTED.
	preview, err := x.svc.ImportPlants(editorCtx(), json.RawMessage(payload), "test-model", true)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(preview.Items) != 1 || !preview.Items[0].OK || preview.Items[0].Action != garden.ImportCreate {
		t.Fatalf("preview = %+v", preview.Items)
	}
	if len(preview.Items[0].UnmappedFields) != 1 || preview.Items[0].UnmappedFields[0] != "barvicka" {
		t.Errorf("an unrecognised field must be reported, got %v", preview.Items[0].UnmappedFields)
	}
	page, _ := x.svc.ListPlants(editorCtx(), garden.PlantFilter{}, 50, "")
	if len(page.Items) != 0 {
		t.Fatal("dry_run must persist nothing")
	}

	// Now apply. The Czech words map onto code values, and the row is badged
	// unverified.
	applied, err := x.svc.ImportPlants(editorCtx(), json.RawMessage(payload), "test-model", false)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied.Summary.Created != 1 {
		t.Fatalf("summary = %+v", applied.Summary)
	}
	page, _ = x.svc.ListPlants(editorCtx(), garden.PlantFilter{}, 50, "")
	if len(page.Items) != 1 {
		t.Fatalf("expected one crop, got %d", len(page.Items))
	}
	got := page.Items[0]
	if got.Family != garden.FamilySolanaceae || got.Hardiness != garden.HardinessTender {
		t.Errorf("Czech enum words did not map: family=%s hardiness=%s", got.Family, got.Hardiness)
	}
	if got.Provenance.Source != garden.SourceLLM || got.Provenance.VerifiedAt != nil {
		t.Errorf("an imported crop is llm-sourced and NOT verified: %+v", got.Provenance)
	}

	// A second apply UPDATES rather than duplicating, and reports the diff.
	second, err := x.svc.ImportPlants(editorCtx(),
		json.RawMessage(`{"name_cs":"rajče","family":"lilkovité","plant_type":"zelenina","hardiness":"citlivá","harvest_unit":"kg","spacing_row_cm":80}`),
		"test-model", true)
	if err != nil {
		t.Fatalf("second preview: %v", err)
	}
	if second.Items[0].Action != garden.ImportUpdate {
		t.Errorf("a known crop must update, got %s", second.Items[0].Action)
	}
}

// An unmappable enum is a NAMED error, never a silent default.
func TestImportRejectsUnmappableEnum(t *testing.T) {
	x := newH(t)
	res, err := x.svc.ImportPlants(editorCtx(), json.RawMessage(
		`{"name_cs":"cosi","family":"vymyšlenovité","plant_type":"zelenina","hardiness":"citlivá","harvest_unit":"kg"}`),
		"", true)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	item := res.Items[0]
	if item.OK || item.Action != garden.ImportReject {
		t.Fatalf("expected a rejection, got %+v", item)
	}
	joined := strings.Join(item.Errors, " ")
	if !strings.Contains(joined, "family") || !strings.Contains(joined, "vymyšlenovité") {
		t.Errorf("the error must name the field AND the value, got %q", joined)
	}
}

// A twenty-element array reports per-element status, so three bad rows do not
// cost the other seventeen.
func TestImportArrayReportsPerElement(t *testing.T) {
	x := newH(t)
	var parts []string
	for i := 0; i < 20; i++ {
		family := "lilkovité"
		if i%7 == 0 {
			family = "nesmysl" // three of the twenty
		}
		parts = append(parts, `{"name_cs":"plodina`+string(rune('a'+i))+`","family":"`+family+
			`","plant_type":"zelenina","hardiness":"otužilá","harvest_unit":"kg"}`)
	}
	res, err := x.svc.ImportPlants(editorCtx(), json.RawMessage("["+strings.Join(parts, ",")+"]"), "", false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.Items) != 20 {
		t.Fatalf("expected 20 results, got %d", len(res.Items))
	}
	if res.Summary.Rejected != 3 || res.Summary.Created != 17 {
		t.Errorf("summary = %+v, want 17 created and 3 rejected", res.Summary)
	}
}

// THE ROUND TRIP IS A TEST, NOT AN ASPIRATION (D126): an export re-imports to an
// equivalent state.
func TestExportReImports(t *testing.T) {
	source := newH(t)
	source.mustPlant("rajče", garden.FamilySolanaceae, func(in *garden.PlantInput) {
		in.Hardiness = sp(garden.HardinessTender)
		in.WinTransplant = &garden.Window{Anchor: garden.AnchorLastFrost, From: 0, To: 14}
		in.WinHarvest = &garden.Window{Anchor: garden.AnchorWeek, From: 28, To: 40}
		in.SpacingRowCM, in.SpacingPlantCM = fp(60), fp(50)
		in.FeederClass = sp(garden.FeederHeavy)
		in.NeedsSupport = bp(true)
		in.StorageMethods = []string{"cellar", "freeze"}
		in.NotesMD = sp("Nejlíp se daří u zdi.")
	})
	source.mustPlant("mrkev", garden.FamilyApiaceae, func(in *garden.PlantInput) {
		in.WinSowDirect = &garden.Window{Anchor: garden.AnchorWeek, From: 12, To: 14}
	})

	dump, err := source.svc.Export(context.Background())
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	blob, err := json.Marshal(dump.Plants)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}

	// A fresh install imports the file and ends up with the same knowledge base.
	target := newH(t)
	res, err := target.svc.ImportPlants(editorCtx(), blob, "", false)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if res.Summary.Rejected != 0 {
		t.Fatalf("an export must re-import cleanly, got %+v: %+v", res.Summary, res.Items)
	}

	before, _ := source.svc.ListPlants(editorCtx(), garden.PlantFilter{}, 50, "")
	after, _ := target.svc.ListPlants(editorCtx(), garden.PlantFilter{}, 50, "")
	if len(before.Items) != len(after.Items) {
		t.Fatalf("crop count %d → %d", len(before.Items), len(after.Items))
	}
	byName := map[string]garden.Plant{}
	for _, p := range after.Items {
		byName[p.NameCS] = p
	}
	for _, want := range before.Items {
		got, ok := byName[want.NameCS]
		if !ok {
			t.Errorf("crop %q did not survive the round trip", want.NameCS)
			continue
		}
		if got.Family != want.Family || got.Hardiness != want.Hardiness || got.PlantType != want.PlantType {
			t.Errorf("%s: identity changed", want.NameCS)
		}
		if !sameWindowPtr(got.WinTransplant, want.WinTransplant) || !sameWindowPtr(got.WinHarvest, want.WinHarvest) ||
			!sameWindowPtr(got.WinSowDirect, want.WinSowDirect) {
			t.Errorf("%s: timing windows changed", want.NameCS)
		}
		if len(got.StorageMethods) != len(want.StorageMethods) {
			t.Errorf("%s: storage methods changed", want.NameCS)
		}
		if deref(got.NotesMD) != deref(want.NotesMD) {
			t.Errorf("%s: notes changed", want.NameCS)
		}
	}
}

// ---- catalogs ----

// Each countable metric equals len(items) of its list — BY CONSTRUCTION, since
// both go through the same selection. This test is what keeps that true as the
// selections change.
func TestMetricsAgreeWithLists(t *testing.T) {
	x := newH(t)
	now := time.Now()
	year := now.Year()
	se := x.mustSeason(year)
	bed := x.mustBed("Horní", "A1", 5)
	x.mustBed("Dolní", "A2", 5) // left empty, so beds_unplanned has something to say
	plant := x.mustPlant("mrkev", garden.FamilyApiaceae, func(in *garden.PlantInput) {
		in.WinSowDirect = &garden.Window{Anchor: garden.AnchorWeek, From: 1, To: 52}
		in.WinHarvest = &garden.Window{Anchor: garden.AnchorWeek, From: 1, To: 52}
	})
	if _, err := x.svc.CreatePlanting(editorCtx(), garden.PlantingInput{
		SeasonYear: ip(se.Year), BedID: sp(bed.ID), PlantID: sp(plant.ID), AreaM2: fp(1),
	}); err != nil {
		t.Fatalf("create planting: %v", err)
	}

	mod := garden.NewModule(x.svc)
	metrics, lists := mod.MetricProvider(), mod.ListProvider()

	for _, key := range []string{
		garden.MetricTasksDue7d, garden.MetricTasksOverdue,
		garden.MetricPlanWarnings, garden.MetricBedsUnplanned,
	} {
		t.Run(key, func(t *testing.T) {
			count, err := metrics.Value(editorCtx(), "", key, now)
			if err != nil {
				t.Fatalf("metric: %v", err)
			}
			items, err := lists.Items(editorCtx(), "", key, now)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if count != len(items) {
				t.Errorf("metric says %d, list has %d items — they must agree by construction (D77)",
					count, len(items))
			}
		})
	}

	// Every published descriptor resolves rather than erroring. ErrNoData is a
	// resolution, not an error: the frost metric has no forecast cached here.
	for _, d := range metrics.Descriptors() {
		if _, err := metrics.Value(editorCtx(), "", d.Key, now); err != nil && !errors.Is(err, platformmetrics.ErrNoData) {
			t.Errorf("metric %s does not resolve: %v", d.Key, err)
		}
	}
	for _, d := range lists.Descriptors() {
		if _, err := lists.Items(editorCtx(), "", d.Key, now); err != nil {
			t.Errorf("list %s does not resolve: %v", d.Key, err)
		}
		if d.Empty == "" {
			t.Errorf("list %s needs a Czech empty string — a bare placeholder reads like a bug", d.Key)
		}
	}
}

// With no forecast cached the frost metric must NOT read as a temperature at
// all — it resolves to metrics.ErrNoData, which conditions treat as "not met"
// and rendering degrades to the placeholder, never to a number.
func TestFrostMetricWithNoForecast(t *testing.T) {
	x := newH(t)
	mod := garden.NewModule(x.svc)
	got, err := mod.MetricProvider().Value(editorCtx(), "", garden.MetricFrostRiskTonight, time.Now())
	if !errors.Is(err, platformmetrics.ErrNoData) {
		t.Errorf("with no forecast the metric must resolve to ErrNoData, got %d °C (err %v) — any number "+
			"here leaks into summaries and conditions as if it were a reading", got, err)
	}
}

// A night SPANS MIDNIGHT, so the row that answers "is tonight a frost risk"
// depends on the time of day: in the evening it is TOMORROW's, in the small
// hours today's. Reading both and taking the colder would republish a minimum
// that was reached before dawn and has already passed — telling the household to
// cover up on a night that is actually mild.
func TestFrostMetricReadsTonightsRowOnly(t *testing.T) {
	x := newH(t)
	loc, _ := time.LoadLocation("Europe/Prague")
	// Yesterday was bitter, tonight is mild.
	cold, mild := -3.0, 6.0
	x.mustWeather(map[string]float64{"2026-04-12": cold, "2026-04-13": mild})

	mod := garden.NewModule(x.svc)
	evening := time.Date(2026, 4, 12, 18, 0, 0, 0, loc)
	got, err := mod.MetricProvider().Value(editorCtx(), "", garden.MetricFrostRiskTonight, evening)
	if err != nil {
		t.Fatalf("metric: %v", err)
	}
	if got != int(mild) {
		t.Errorf("at 18:00 the metric must report tomorrow's row (%d °C), got %d °C — today's minimum "+
			"was reached before dawn and is not something anyone can still act on", int(mild), got)
	}

	// And the mirror case: in the small hours the night in progress is today's.
	smallHours := time.Date(2026, 4, 12, 3, 0, 0, 0, loc)
	got, err = mod.MetricProvider().Value(editorCtx(), "", garden.MetricFrostRiskTonight, smallHours)
	if err != nil {
		t.Fatalf("metric: %v", err)
	}
	if got != int(cold) {
		t.Errorf("at 03:00 the metric must report today's row (%d °C), got %d °C", int(cold), got)
	}
}

// ---- widget ----

func TestWidgetQuietState(t *testing.T) {
	x := newH(t)
	mod := garden.NewModule(x.svc)
	widgets := mod.Widgets()
	if len(widgets) != 1 {
		t.Fatalf("the module contributes exactly one widget (D123), got %d", len(widgets))
	}
	w := widgets[0]
	if w.Key() != "garden.prace" || w.AdminOnly() || w.DefaultSize() != "wide" {
		t.Errorf("widget descriptor = %s admin=%v size=%s", w.Key(), w.AdminOnly(), w.DefaultSize())
	}
	payload, err := w.Data(editorCtx(), registryUser())
	if err != nil {
		t.Fatalf("widget data: %v", err)
	}
	data, ok := payload.(garden.PraceWidget)
	if !ok {
		t.Fatalf("unexpected payload %T", payload)
	}
	// From November to February this IS the normal state, and it is a correct
	// answer rather than an empty one.
	if data.State != "quiet" {
		t.Errorf("an empty garden must report the quiet state, got %q", data.State)
	}
}

// ---- helpers ----

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func sameWindowPtr(a, b *garden.Window) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func mustDateAfter(t *testing.T, base string, days int) string {
	t.Helper()
	d, err := time.Parse("2006-01-02", base)
	if err != nil {
		t.Fatalf("parse %q: %v", base, err)
	}
	return d.AddDate(0, 0, days).Format("2006-01-02")
}

// registryUser is the identity the dashboard host passes to a widget provider.
func registryUser() registry.User {
	return registry.User{ID: "u-editor", Roles: []string{"editor"}}
}
