package garden

import (
	"database/sql"
	"encoding/json"
	"strings"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
)

// THE SHARED KNOWLEDGE-BASE COLUMNS.
//
// garden_plants and garden_varieties carry the same overridable fields — a
// species states them, a variety overrides the few that differ (D103). In Go
// they are one PlantCore struct, so the mirror cannot drift; this file is the
// SQL half of the same idea, so the two tables' column lists cannot drift
// either.
//
// Everything is a nullable scan target. On a species NULL means "not stated"; on
// a variety it means "inherit". Same column, same nil, two readings — which is
// exactly what lets one struct and one column list serve both tables.

// coreColumns is the shared column list, in the order coreRow scans it. Both
// tables declare these columns identically; changing one without the other is
// caught by the round-trip test.
const coreColumns = `name_latin, feeder_class, root_depth, sun, water_need,
	soil_ph_min, soil_ph_max, rotation_break_years, sow_method,
	needs_pricking_out, needs_support, wants_mulch, wants_pest_check,
	hardening_days, sow_depth_cm, spacing_row_cm, spacing_plant_cm, plants_per_m2,
	days_to_germinate_min, days_to_germinate_max, germination_temp_c,
	days_to_maturity_min, days_to_maturity_max,
	win_sow_indoor_anchor, win_sow_indoor_from, win_sow_indoor_to,
	win_sow_direct_anchor, win_sow_direct_from, win_sow_direct_to,
	win_transplant_anchor, win_transplant_from, win_transplant_to,
	win_harvest_anchor, win_harvest_from, win_harvest_to,
	harvest_unit, yield_per_m2, yield_per_plant,
	storage_methods, storage_temp_c, storage_humidity, shelf_life_days,
	pests, diseases, notes_md`

// coreColumnCount is how many placeholders an INSERT of coreColumns needs.
var coreColumnCount = len(strings.Split(strings.ReplaceAll(coreColumns, "\n", ""), ","))

// coreRow is the scan buffer. Typed sql.Null* rather than raw pointers because
// SQLite hands booleans back as INTEGER and floats as either INTEGER or REAL
// depending on what was stored, and the Null* types convert predictably.
type coreRow struct {
	NameLatin, FeederClass, RootDepth, Sun, WaterNeed sql.NullString
	SoilPHMin, SoilPHMax                              sql.NullFloat64
	RotationBreakYears                                sql.NullInt64
	SowMethod                                         sql.NullString
	NeedsPrickingOut, NeedsSupport                    sql.NullInt64
	WantsMulch, WantsPestCheck                        sql.NullInt64
	HardeningDays                                     sql.NullInt64
	SowDepthCM, SpacingRowCM, SpacingPlantCM, PlantsPerM2 sql.NullFloat64
	DaysToGerminateMin, DaysToGerminateMax, GerminationTempC sql.NullInt64
	DaysToMaturityMin, DaysToMaturityMax                     sql.NullInt64

	WinSowIndoorAnchor            sql.NullString
	WinSowIndoorFrom, WinSowIndoorTo sql.NullInt64
	WinSowDirectAnchor            sql.NullString
	WinSowDirectFrom, WinSowDirectTo sql.NullInt64
	WinTransplantAnchor            sql.NullString
	WinTransplantFrom, WinTransplantTo sql.NullInt64
	WinHarvestAnchor            sql.NullString
	WinHarvestFrom, WinHarvestTo sql.NullInt64

	HarvestUnit               sql.NullString
	YieldPerM2, YieldPerPlant sql.NullFloat64

	StorageMethods                              sql.NullString
	StorageTempC, StorageHumidity, ShelfLifeDays sql.NullInt64

	Pests, Diseases, NotesMD sql.NullString
}

// scanTargets returns the scan destinations in coreColumns order.
func (r *coreRow) scanTargets() []any {
	return []any{
		&r.NameLatin, &r.FeederClass, &r.RootDepth, &r.Sun, &r.WaterNeed,
		&r.SoilPHMin, &r.SoilPHMax, &r.RotationBreakYears, &r.SowMethod,
		&r.NeedsPrickingOut, &r.NeedsSupport, &r.WantsMulch, &r.WantsPestCheck,
		&r.HardeningDays, &r.SowDepthCM, &r.SpacingRowCM, &r.SpacingPlantCM, &r.PlantsPerM2,
		&r.DaysToGerminateMin, &r.DaysToGerminateMax, &r.GerminationTempC,
		&r.DaysToMaturityMin, &r.DaysToMaturityMax,
		&r.WinSowIndoorAnchor, &r.WinSowIndoorFrom, &r.WinSowIndoorTo,
		&r.WinSowDirectAnchor, &r.WinSowDirectFrom, &r.WinSowDirectTo,
		&r.WinTransplantAnchor, &r.WinTransplantFrom, &r.WinTransplantTo,
		&r.WinHarvestAnchor, &r.WinHarvestFrom, &r.WinHarvestTo,
		&r.HarvestUnit, &r.YieldPerM2, &r.YieldPerPlant,
		&r.StorageMethods, &r.StorageTempC, &r.StorageHumidity, &r.ShelfLifeDays,
		&r.Pests, &r.Diseases, &r.NotesMD,
	}
}

// toCore converts a scanned row into the domain struct.
func (r *coreRow) toCore() PlantCore {
	return PlantCore{
		NameLatin:          nsp(r.NameLatin),
		FeederClass:        nsp(r.FeederClass),
		RootDepth:          nsp(r.RootDepth),
		Sun:                nsp(r.Sun),
		WaterNeed:          nsp(r.WaterNeed),
		SoilPHMin:          nfp(r.SoilPHMin),
		SoilPHMax:          nfp(r.SoilPHMax),
		RotationBreakYears: nip(r.RotationBreakYears),
		SowMethod:          nsp(r.SowMethod),
		NeedsPrickingOut:   nbp(r.NeedsPrickingOut),
		NeedsSupport:       nbp(r.NeedsSupport),
		WantsMulch:         nbp(r.WantsMulch),
		WantsPestCheck:     nbp(r.WantsPestCheck),
		HardeningDays:      nip(r.HardeningDays),
		SowDepthCM:         nfp(r.SowDepthCM),
		SpacingRowCM:       nfp(r.SpacingRowCM),
		SpacingPlantCM:     nfp(r.SpacingPlantCM),
		PlantsPerM2:        nfp(r.PlantsPerM2),
		DaysToGerminateMin: nip(r.DaysToGerminateMin),
		DaysToGerminateMax: nip(r.DaysToGerminateMax),
		GerminationTempC:   nip(r.GerminationTempC),
		DaysToMaturityMin:  nip(r.DaysToMaturityMin),
		DaysToMaturityMax:  nip(r.DaysToMaturityMax),
		WinSowIndoor:       window(r.WinSowIndoorAnchor, r.WinSowIndoorFrom, r.WinSowIndoorTo),
		WinSowDirect:       window(r.WinSowDirectAnchor, r.WinSowDirectFrom, r.WinSowDirectTo),
		WinTransplant:      window(r.WinTransplantAnchor, r.WinTransplantFrom, r.WinTransplantTo),
		WinHarvest:         window(r.WinHarvestAnchor, r.WinHarvestFrom, r.WinHarvestTo),
		HarvestUnit:        nsp(r.HarvestUnit),
		YieldPerM2:         nfp(r.YieldPerM2),
		YieldPerPlant:      nfp(r.YieldPerPlant),
		StorageMethods:     jsonStrings(r.StorageMethods),
		StorageTempC:       nip(r.StorageTempC),
		StorageHumidity:    nip(r.StorageHumidity),
		ShelfLifeDays:      nip(r.ShelfLifeDays),
		Pests:              jsonIssues(r.Pests),
		Diseases:           jsonIssues(r.Diseases),
		NotesMD:            nsp(r.NotesMD),
	}
}

// coreValues returns the bind values in coreColumns order, for INSERT/UPDATE.
func coreValues(c PlantCore) []any {
	wa, wf, wt := windowCols(c.WinSowIndoor)
	da, df, dt := windowCols(c.WinSowDirect)
	ta, tf, tt := windowCols(c.WinTransplant)
	ha, hf, ht := windowCols(c.WinHarvest)
	return []any{
		c.NameLatin, c.FeederClass, c.RootDepth, c.Sun, c.WaterNeed,
		c.SoilPHMin, c.SoilPHMax, c.RotationBreakYears, c.SowMethod,
		boolCol(c.NeedsPrickingOut), boolCol(c.NeedsSupport), boolCol(c.WantsMulch), boolCol(c.WantsPestCheck),
		c.HardeningDays, c.SowDepthCM, c.SpacingRowCM, c.SpacingPlantCM, c.PlantsPerM2,
		c.DaysToGerminateMin, c.DaysToGerminateMax, c.GerminationTempC,
		c.DaysToMaturityMin, c.DaysToMaturityMax,
		wa, wf, wt, da, df, dt, ta, tf, tt, ha, hf, ht,
		c.HarvestUnit, c.YieldPerM2, c.YieldPerPlant,
		jsonCol(c.StorageMethods), c.StorageTempC, c.StorageHumidity, c.ShelfLifeDays,
		jsonCol(c.Pests), jsonCol(c.Diseases), c.NotesMD,
	}
}

// searchBlob flattens the pest and disease NAMES into the one column FTS5 can
// index — they live in JSON, and FTS5 cannot reach inside it.
func searchBlob(c PlantCore) any {
	var parts []string
	for _, list := range [][]PestIssue{c.Pests, c.Diseases} {
		for _, p := range list {
			if p.Name != "" {
				parts = append(parts, p.Name)
			}
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return strings.Join(parts, " ")
}

// ---- null → pointer converters ----

func nsp(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func nfp(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

func nip(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int64)
	return &i
}

func nbp(v sql.NullInt64) *bool {
	if !v.Valid {
		return nil
	}
	b := v.Int64 != 0
	return &b
}

// boolCol writes a *bool as SQLite's 0/1, preserving NULL. On a variety NULL is
// "inherit" and 0 is an explicit "no" — a distinction a plain bool could not
// carry, and the reason NeedsSupport is a pointer at all.
func boolCol(v *bool) any {
	if v == nil {
		return nil
	}
	if *v {
		return 1
	}
	return 0
}

func window(anchor sql.NullString, from, to sql.NullInt64) *Window {
	if !anchor.Valid || !from.Valid || !to.Valid {
		return nil
	}
	return &Window{Anchor: anchor.String, From: int(from.Int64), To: int(to.Int64)}
}

func windowCols(w *Window) (any, any, any) {
	if w == nil {
		return nil, nil, nil
	}
	return w.Anchor, w.From, w.To
}

// jsonCol marshals a slice column, writing NULL for an empty one so "not stated"
// and "explicitly empty" do not both become `[]`.
func jsonCol(v any) any {
	switch t := v.(type) {
	case []string:
		if len(t) == 0 {
			return nil
		}
	case []PestIssue:
		if len(t) == 0 {
			return nil
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return string(b)
}

func jsonStrings(v sql.NullString) []string {
	if !v.Valid || v.String == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(v.String), &out); err != nil {
		return nil
	}
	return out
}

func jsonIssues(v sql.NullString) []PestIssue {
	if !v.Valid || v.String == "" {
		return nil
	}
	var out []PestIssue
	if err := json.Unmarshal([]byte(v.String), &out); err != nil {
		return nil
	}
	return out
}

// placeholders is appdb.Placeholders — one implementation, five call sites (v10
// review). It was copied into this module, `notes`, `documents`, `todo` and
// `platform/push` before platform/db grew the shared one. The shared spelling has
// no space after the comma; nothing here reads the generated SQL, only SQLite does.
func placeholders(n int) string { return appdb.Placeholders(n) }

// assignments returns "col = ?, col = ?" for a column list, for UPDATE.
func assignments(columns string) string {
	parts := strings.Split(strings.ReplaceAll(columns, "\n", " "), ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p) + " = ?"
	}
	return strings.Join(parts, ", ")
}
