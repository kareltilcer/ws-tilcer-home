package garden

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
)

// THE LLM ESCAPE HATCH (PRD FR-G3, D114, D126).
//
// Forty crops of structured agronomy is a lot of typing, so the module generates
// a Czech prompt carrying THE JSON SCHEMA PRODUCED BY THIS FILE'S OWN VALIDATOR,
// the answer is pasted back, and it is PREVIEWED before anything is written.
//
// The one design rule everything here follows: an input the importer cannot map
// is REPORTED, never silently dropped and never quietly defaulted. A wrong
// default is a crop that sows at the wrong time and nobody ever finds out why.
//
// Every string is untrusted input from a language model. The defences are the
// size cap (checked by the handler), schema validation, explicit enum mapping,
// and applying only after a preview. Markdown fields are stored raw and rendered
// through the same sanitiser as `notes` — which lives on the frontend
// (components/common/MarkdownView), so there is nothing to sanitise here.

// ImportItemResult is one element's outcome.
type ImportItemResult struct {
	Index          int               `json:"index"`
	Action         string            `json:"action"` // create | update | skip | reject
	OK             bool              `json:"ok"`
	TargetID       *string           `json:"target_id"`
	Name           *string           `json:"name"`
	Diff           map[string]any    `json:"diff,omitempty"`
	UnmappedFields []string          `json:"unmapped_fields,omitempty"`
	Errors         []string          `json:"errors,omitempty"`
}

// ImportSummary counts the outcomes.
type ImportSummary struct {
	Created  int `json:"created"`
	Updated  int `json:"updated"`
	Rejected int `json:"rejected"`
}

// ImportResult is the whole response, dry-run or not.
type ImportResult struct {
	DryRun      bool               `json:"dry_run"`
	Items       []ImportItemResult `json:"items"`
	Summary     ImportSummary      `json:"summary"`
	SourceModel *string            `json:"source_model"`
}

// Import actions.
const (
	ImportCreate = "create"
	ImportUpdate = "update"
	ImportSkip   = "skip"
	ImportReject = "reject"
)

// ImportPlants validates, previews and optionally applies one crop record or an
// array of them.
//
// dry_run defaults TRUE at the handler (D129): an import should never apply by
// accident, and the preview is the screen that makes pasting machine output feel
// safe rather than reckless.
func (s *Service) ImportPlants(ctx context.Context, raw json.RawMessage, model string, dryRun bool) (ImportResult, error) {
	records, err := decodeImportPayload(raw)
	if err != nil {
		return ImportResult{}, err
	}
	if len(records) == 0 {
		return ImportResult{}, errUnprocessable("Vložený JSON neobsahuje žádný záznam.")
	}
	if len(records) > 200 {
		return ImportResult{}, httpx.ErrTooLarge("Najednou lze naimportovat nejvýš 200 plodin.")
	}

	out := ImportResult{DryRun: dryRun, Items: make([]ImportItemResult, 0, len(records))}
	if model != "" {
		out.SourceModel = sp(model)
	}

	// EACH ELEMENT IS VALIDATED INDEPENDENTLY, so three bad rows out of twenty do
	// not cost the other seventeen. That is the point of the per-element result
	// list rather than one all-or-nothing error.
	for i, rec := range records {
		item := s.importOne(ctx, i, rec, model, dryRun)
		switch {
		case !item.OK:
			out.Summary.Rejected++
		case item.Action == ImportCreate:
			out.Summary.Created++
		case item.Action == ImportUpdate:
			out.Summary.Updated++
		}
		out.Items = append(out.Items, item)
	}
	if !dryRun {
		s.notify(ctx, "garden_plant.changed", map[string]any{"imported": len(out.Items)})
	}
	return out, nil
}

// decodeImportPayload accepts an object OR an array, so twenty crops arrive in
// one paste.
func decodeImportPayload(raw json.RawMessage) ([]map[string]any, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, errUnprocessable("Vložte JSON s popisem plodiny.")
	}
	if strings.HasPrefix(trimmed, "[") {
		var list []map[string]any
		if err := json.Unmarshal([]byte(trimmed), &list); err != nil {
			return nil, errUnprocessable("JSON se nepodařilo přečíst: " + err.Error())
		}
		return list, nil
	}
	var one map[string]any
	if err := json.Unmarshal([]byte(trimmed), &one); err != nil {
		return nil, errUnprocessable("JSON se nepodařilo přečíst: " + err.Error())
	}
	return []map[string]any{one}, nil
}

func (s *Service) importOne(ctx context.Context, index int, rec map[string]any, model string, dryRun bool) ImportItemResult {
	item := ImportItemResult{Index: index, Action: ImportReject}

	name, _ := rec["name_cs"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		item.Errors = append(item.Errors, "Chybí povinné pole name_cs.")
		return item
	}
	item.Name = sp(name)

	parsed, unmapped, errs := parsePlantRecord(rec)
	item.UnmappedFields = unmapped
	if len(errs) > 0 {
		item.Errors = errs
		return item
	}

	existing, err := s.store.PlantByName(ctx, nil, name)
	isUpdate := err == nil
	if err != nil && err != errNotFound {
		item.Errors = append(item.Errors, err.Error())
		return item
	}

	target := Plant{PlantCore: parsed.core, NameCS: name,
		Family: parsed.family, PlantType: parsed.plantType, Hardiness: parsed.hardiness}
	if isUpdate {
		// An update MERGES: the model fills gaps rather than wiping fields it did
		// not mention. Every core field is nil-able, so "absent" and "cleared" are
		// genuinely distinguishable — mergeCore keeps the existing value for a
		// field the record omitted.
		merged := existing
		merged.PlantCore = mergeCore(existing.PlantCore, &parsed.core)
		if parsed.family != "" {
			merged.Family = parsed.family
		}
		if parsed.plantType != "" {
			merged.PlantType = parsed.plantType
		}
		if parsed.hardiness != "" {
			merged.Hardiness = parsed.hardiness
		}
		item.Diff = plantDiff(existing, merged)
		target = merged
		item.TargetID = sp(existing.ID)
		item.Action = ImportUpdate
	} else {
		item.Action = ImportCreate
	}

	if err := validatePlant(target); err != nil {
		item.Errors = append(item.Errors, err.Error())
		item.Action = ImportReject
		return item
	}
	if dryRun {
		item.OK = true
		return item
	}

	// Applied rows record their provenance and are badged "neověřeno" until a
	// human confirms them (D114) — VerifiedAt stays nil, which is what the badge
	// reads.
	now := nowUTC()
	target.Provenance.Source = SourceLLM
	target.Provenance.SourceAt = sp(now)
	if model != "" {
		target.Provenance.SourceModel = sp(model)
	}
	target.Provenance.VerifiedBy, target.Provenance.VerifiedAt = nil, nil

	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if isUpdate {
			target.UpdatedAt = now
			if err := s.store.UpdatePlant(ctx, tx, target); err != nil {
				return err
			}
			if err := s.record(ctx, tx, "plant.update", "garden_plant", target.ID,
				"Naimportována plodina "+name+" (neověřeno)", plantChanges(&existing, target), nil); err != nil {
				return err
			}
			// AN IMPORT IS AN EDIT, and must re-anchor the plan exactly as the
			// PATCH route does (FR-G7's third trigger). Correcting a crop's sowing
			// window is the single most likely reason to paste a record at all —
			// leaving the plantings and their generated tasks on the old window
			// would make the crop page and the calendar disagree, with the same
			// edit producing different plans depending on which door it came
			// through.
			if timingChanged(existing.PlantCore, target.PlantCore) {
				return s.regenerateForPlant(ctx, tx, target.ID)
			}
			return nil
		}
		target.ID = idgen.New()
		target.CreatedAt, target.UpdatedAt = now, now
		target.CreatedBy = sp(actorID(ctx))
		if err := s.store.InsertPlant(ctx, tx, target); err != nil {
			return duplicateName(err)
		}
		item.TargetID = sp(target.ID)
		return s.record(ctx, tx, "plant.create", "garden_plant", target.ID,
			"Naimportována plodina "+name+" (neověřeno)", plantChanges(nil, target), nil)
	})
	if err != nil {
		item.OK, item.Action = false, ImportReject
		item.Errors = append(item.Errors, err.Error())
		return item
	}
	item.OK = true
	return item
}

// parsedPlant is one validated record.
type parsedPlant struct {
	core      PlantCore
	family    string
	plantType string
	hardiness string
}

// parsePlantRecord maps a loose JSON object onto PlantCore.
//
// It returns THREE things and all three matter: the record, the input fields it
// did not recognise (reported, never dropped), and the errors. An unmappable
// ENUM is an error naming field AND value — never a silent default.
func parsePlantRecord(rec map[string]any) (parsedPlant, []string, []string) {
	var out parsedPlant
	var unmapped, errs []string

	known := map[string]bool{"name_cs": true}
	str := func(field string) *string {
		known[field] = true
		v, ok := rec[field]
		if !ok || v == nil {
			return nil
		}
		s, ok := v.(string)
		if !ok {
			// A WRONG TYPE IS AN ERROR, exactly as it is for num and boolean.
			// Returning nil silently would drop the field with ok=true, no error
			// and no unmapped_fields entry — `known` is already set above — so a
			// model that wrote `"feeder_class": 3` would produce a crop that never
			// generates a feeding task, previewed as a clean green tick.
			errs = append(errs, fmt.Sprintf("Pole %s: očekáváme text.", field))
			return nil
		}
		if strings.TrimSpace(s) == "" {
			return nil // an empty string is "not stated", not a malformed value
		}
		return sp(strings.TrimSpace(s))
	}
	enum := func(field, enumName string) *string {
		raw := str(field)
		if raw == nil {
			return nil
		}
		mapped, ok := Coerce(enumName, *raw)
		if !ok {
			errs = append(errs, fmt.Sprintf("Pole %s: hodnotu „%s“ neznáme.", field, *raw))
			return nil
		}
		return sp(mapped)
	}
	num := func(field string) *float64 {
		known[field] = true
		v, ok := rec[field]
		if !ok || v == nil {
			return nil
		}
		f, ok := v.(float64)
		if !ok {
			errs = append(errs, fmt.Sprintf("Pole %s: očekáváme číslo.", field))
			return nil
		}
		return &f
	}
	integer := func(field string) *int {
		f := num(field)
		if f == nil {
			return nil
		}
		return ip(int(*f))
	}
	boolean := func(field string) *bool {
		known[field] = true
		v, ok := rec[field]
		if !ok || v == nil {
			return nil
		}
		b, ok := v.(bool)
		if !ok {
			errs = append(errs, fmt.Sprintf("Pole %s: očekáváme true nebo false.", field))
			return nil
		}
		return &b
	}
	window := func(field string) *Window {
		known[field] = true
		v, ok := rec[field]
		if !ok || v == nil {
			return nil
		}
		obj, ok := v.(map[string]any)
		if !ok {
			errs = append(errs, fmt.Sprintf("Pole %s: očekáváme objekt {anchor, from, to}.", field))
			return nil
		}
		anchorRaw, _ := obj["anchor"].(string)
		anchor, ok := Coerce(EnumTimingAnchor, anchorRaw)
		if !ok {
			errs = append(errs, fmt.Sprintf("Pole %s.anchor: hodnotu „%s“ neznáme.", field, anchorRaw))
			return nil
		}
		from, okF := obj["from"].(float64)
		to, okT := obj["to"].(float64)
		if !okF || !okT {
			errs = append(errs, fmt.Sprintf("Pole %s: from a to musí být čísla.", field))
			return nil
		}
		w := &Window{Anchor: anchor, From: int(from), To: int(to)}
		if err := w.Validate(field); err != nil {
			errs = append(errs, err.Error())
			return nil
		}
		return w
	}

	if v := enum("family", EnumFamily); v != nil {
		out.family = *v
	}
	if v := enum("plant_type", EnumPlantType); v != nil {
		out.plantType = *v
	}
	if v := enum("hardiness", EnumHardiness); v != nil {
		out.hardiness = *v
	}

	c := &out.core
	c.NameLatin = str("name_latin")
	c.FeederClass = enum("feeder_class", EnumFeederClass)
	c.RootDepth = enum("root_depth", EnumRootDepth)
	c.Sun = enum("sun", EnumSun)
	c.WaterNeed = enum("water_need", EnumWaterNeed)
	c.SowMethod = enum("sow_method", EnumSowMethod)
	c.HarvestUnit = enum("harvest_unit", EnumHarvestUnit)
	c.SoilPHMin, c.SoilPHMax = num("soil_ph_min"), num("soil_ph_max")
	c.RotationBreakYears = integer("rotation_break_years")
	c.NeedsPrickingOut = boolean("needs_pricking_out")
	c.NeedsSupport = boolean("needs_support")
	c.WantsMulch = boolean("wants_mulch")
	c.WantsPestCheck = boolean("wants_pest_check")
	c.HardeningDays = integer("hardening_days")
	c.SowDepthCM = num("sow_depth_cm")
	c.SpacingRowCM, c.SpacingPlantCM = num("spacing_row_cm"), num("spacing_plant_cm")
	c.PlantsPerM2 = num("plants_per_m2")
	c.DaysToGerminateMin, c.DaysToGerminateMax = integer("days_to_germinate_min"), integer("days_to_germinate_max")
	c.GerminationTempC = integer("germination_temp_c")
	c.DaysToMaturityMin, c.DaysToMaturityMax = integer("days_to_maturity_min"), integer("days_to_maturity_max")
	c.WinSowIndoor = window("win_sow_indoor")
	c.WinSowDirect = window("win_sow_direct")
	c.WinTransplant = window("win_transplant")
	c.WinHarvest = window("win_harvest")
	c.YieldPerM2, c.YieldPerPlant = num("yield_per_m2"), num("yield_per_plant")
	c.StorageTempC, c.StorageHumidity = integer("storage_temp_c"), integer("storage_humidity")
	c.ShelfLifeDays = integer("shelf_life_days")
	c.NotesMD = str("notes_md")

	// storage_methods is an enum ARRAY; each member is mapped and an unmappable
	// one is an error rather than a dropped element.
	known["storage_methods"] = true
	if v, ok := rec["storage_methods"]; ok && v != nil {
		if arr, ok := v.([]any); ok {
			for _, m := range arr {
				sv, _ := m.(string)
				mapped, ok := Coerce(EnumStorageMethod, sv)
				if !ok {
					errs = append(errs, fmt.Sprintf("Pole storage_methods: hodnotu „%s“ neznáme.", sv))
					continue
				}
				c.StorageMethods = append(c.StorageMethods, mapped)
			}
		} else {
			errs = append(errs, "Pole storage_methods: očekáváme seznam.")
		}
	}
	c.Pests = parseIssues(rec, "pests", known, &errs)
	c.Diseases = parseIssues(rec, "diseases", known, &errs)

	for field := range rec {
		if !known[field] {
			unmapped = append(unmapped, field)
		}
	}
	sort.Strings(unmapped)
	return out, unmapped, errs
}

func parseIssues(rec map[string]any, field string, known map[string]bool, errs *[]string) []PestIssue {
	known[field] = true
	v, ok := rec[field]
	if !ok || v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("Pole %s: očekáváme seznam.", field))
		return nil
	}
	var out []PestIssue
	for _, raw := range arr {
		obj, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := obj["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		symptom, _ := obj["symptom"].(string)
		remedy, _ := obj["remedy_md"].(string)
		out = append(out, PestIssue{Name: name, Symptom: symptom, RemedyMD: remedy})
	}
	return out
}

// plantDiff renders a field-level before/after for the preview screen. It is
// what turns "paste and hope" into "look at what will change".
func plantDiff(before, after Plant) map[string]any {
	diff := map[string]any{}
	for _, ch := range plantChanges(&before, after) {
		diff[ch.Field] = map[string]any{"old": ch.Old, "new": ch.New}
	}
	return diff
}

// ================================ export ================================

// Export is the round-trip shape (D126): plants, varieties and rules in THE
// IMPORTER'S OWN form, so an export re-imports to an equivalent state. That is a
// readable backup and a way to move the knowledge base between installs — and it
// is a test, not an aspiration.
type Export struct {
	Version    string           `json:"version"`
	ExportedAt string           `json:"exported_at"`
	Plants     []map[string]any `json:"plants"`
	Varieties  []map[string]any `json:"varieties"`
	Rules      []map[string]any `json:"rules"`
}

const exportVersion = "garden-kb/1"

func (s *Service) Export(ctx context.Context) (Export, error) {
	out := Export{
		Version: exportVersion, ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Plants: []map[string]any{}, Varieties: []map[string]any{}, Rules: []map[string]any{},
	}
	// EXHAUSTIVE, not one page. This file is a backup and a way to move the
	// knowledge base between installs, so a cursor dropped after 200 crops would
	// restore to a smaller garden than the one exported — silently.
	plants, err := s.store.allPlants(ctx, PlantFilter{})
	if err != nil {
		return Export{}, err
	}
	for _, p := range plants {
		rec := coreToRecord(p.PlantCore)
		rec["name_cs"] = p.NameCS
		rec["family"] = p.Family
		rec["plant_type"] = p.PlantType
		rec["hardiness"] = p.Hardiness
		out.Plants = append(out.Plants, rec)

		varieties, err := s.store.allVarieties(ctx, p.ID)
		if err != nil {
			return Export{}, err
		}
		for _, v := range varieties {
			vrec := coreToRecord(v.PlantCore)
			vrec["plant_name_cs"] = p.NameCS
			vrec["name"] = v.Name
			if v.Supplier != nil {
				vrec["supplier"] = *v.Supplier
			}
			if v.DescriptionMD != nil {
				vrec["description_md"] = *v.DescriptionMD
			}
			out.Varieties = append(out.Varieties, vrec)
		}
	}
	rules, err := s.store.allRules(ctx, nil, "", true)
	if err != nil {
		return Export{}, err
	}
	if err := s.labelRules(ctx, rules); err != nil {
		return Export{}, err
	}
	for _, r := range rules {
		rec := map[string]any{
			"scope": r.Scope, "verdict": r.Verdict, "severity": r.Severity,
			"is_builtin": r.IsBuiltin, "is_disabled": r.IsDisabled,
		}
		// Pairs export by LABEL rather than by id, so the file means something in
		// another install — an id from this database is meaningless in the next.
		if r.Scope == ScopePlantPair {
			rec["a_ref"], rec["b_ref"] = r.ALabel, r.BLabel
		} else {
			rec["a_ref"], rec["b_ref"] = r.ARef, r.BRef
		}
		if r.MinYearsGap != nil {
			rec["min_years_gap"] = *r.MinYearsGap
		}
		if r.ReasonCS != nil {
			rec["reason_cs"] = *r.ReasonCS
		}
		if r.Source != nil {
			rec["source"] = *r.Source
		}
		out.Rules = append(out.Rules, rec)
	}
	return out, nil
}

// coreToRecord renders PlantCore in the importer's field names, omitting nils —
// so the export is a valid import and reads like something a person wrote.
func coreToRecord(c PlantCore) map[string]any {
	rec := map[string]any{}
	put := func(k string, v any) {
		switch t := v.(type) {
		case *string:
			if t != nil {
				rec[k] = *t
			}
		case *int:
			if t != nil {
				rec[k] = *t
			}
		case *float64:
			if t != nil {
				rec[k] = *t
			}
		case *bool:
			if t != nil {
				rec[k] = *t
			}
		case *Window:
			if t != nil {
				rec[k] = map[string]any{"anchor": t.Anchor, "from": t.From, "to": t.To}
			}
		}
	}
	put("name_latin", c.NameLatin)
	put("feeder_class", c.FeederClass)
	put("root_depth", c.RootDepth)
	put("sun", c.Sun)
	put("water_need", c.WaterNeed)
	put("soil_ph_min", c.SoilPHMin)
	put("soil_ph_max", c.SoilPHMax)
	put("rotation_break_years", c.RotationBreakYears)
	put("sow_method", c.SowMethod)
	put("needs_pricking_out", c.NeedsPrickingOut)
	put("needs_support", c.NeedsSupport)
	put("wants_mulch", c.WantsMulch)
	put("wants_pest_check", c.WantsPestCheck)
	put("hardening_days", c.HardeningDays)
	put("sow_depth_cm", c.SowDepthCM)
	put("spacing_row_cm", c.SpacingRowCM)
	put("spacing_plant_cm", c.SpacingPlantCM)
	put("plants_per_m2", c.PlantsPerM2)
	put("days_to_germinate_min", c.DaysToGerminateMin)
	put("days_to_germinate_max", c.DaysToGerminateMax)
	put("germination_temp_c", c.GerminationTempC)
	put("days_to_maturity_min", c.DaysToMaturityMin)
	put("days_to_maturity_max", c.DaysToMaturityMax)
	put("win_sow_indoor", c.WinSowIndoor)
	put("win_sow_direct", c.WinSowDirect)
	put("win_transplant", c.WinTransplant)
	put("win_harvest", c.WinHarvest)
	put("harvest_unit", c.HarvestUnit)
	put("yield_per_m2", c.YieldPerM2)
	put("yield_per_plant", c.YieldPerPlant)
	put("storage_temp_c", c.StorageTempC)
	put("storage_humidity", c.StorageHumidity)
	put("shelf_life_days", c.ShelfLifeDays)
	put("notes_md", c.NotesMD)
	if len(c.StorageMethods) > 0 {
		rec["storage_methods"] = c.StorageMethods
	}
	if len(c.Pests) > 0 {
		rec["pests"] = c.Pests
	}
	if len(c.Diseases) > 0 {
		rec["diseases"] = c.Diseases
	}
	return rec
}
