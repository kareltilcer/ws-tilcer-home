package garden

import (
	"math"
	"reflect"
	"strings"
)

// SPECIES + VARIETY → EFFECTIVE (PRD D103). This is the split.go of this module:
// small, boring, and the one thing that must not exist twice.
//
// The rule is a single sentence — *effective value = variety value if non-null,
// else species value* — and the reason it lives in one exported function is that
// the planner, the task generator, the check engine and the widget all need it.
// Four independent re-implementations of "when do we sow Black Krim" is a bug
// nobody would ever find, because each one would look correct on its own.
//
// The merge walks PlantCore by reflection rather than assigning 37 fields by
// hand. That is not cleverness for its own sake: PlantCore is shared by Plant and
// Variety, so a field added there is inherited correctly on the day it is added,
// with no second edit to remember and no mirror to fall out of sync. A
// hand-written merge is exactly where the 38th field gets forgotten.

// coreType is resolved once; reflect.TypeOf on every call would be wasteful in
// the check engine's inner loops.
var coreType = reflect.TypeOf(PlantCore{})

// Resolve returns the effective record for a species and an optional variety.
//
// A nil variety yields the species unchanged — which is the common case, since
// most plantings name no variety at all.
func Resolve(p Plant, v *Variety) Effective {
	eff := Effective{
		PlantCore: mergeCore(p.PlantCore, varietyCore(v)),
		PlantID:   p.ID,
		PlantName: p.NameCS,
		Family:    p.Family,
		PlantType: p.PlantType,
		Hardiness: p.Hardiness,
	}
	if v != nil {
		eff.VarietyID = sp(v.ID)
		eff.VarietyName = sp(v.Name)
	}
	return eff
}

func varietyCore(v *Variety) *PlantCore {
	if v == nil {
		return nil
	}
	return &v.PlantCore
}

// mergeCore overlays the non-nil fields of `over` onto `base`.
//
// "Non-nil" is spelled as IsZero on the field, which is correct for every shape
// PlantCore uses: a nil pointer and a nil slice are both zero, and any set value
// — including a pointer to false or to 0 — is not. That distinction matters:
// `needs_support: false` on a variety must OVERRIDE a species that needs
// support, and only a pointer type can say that at all.
func mergeCore(base PlantCore, over *PlantCore) PlantCore {
	if over == nil {
		return base
	}
	out := base
	outV := reflect.ValueOf(&out).Elem()
	overV := reflect.ValueOf(*over)
	for i := 0; i < coreType.NumField(); i++ {
		f := overV.Field(i)
		if f.IsZero() {
			continue // nil ⇒ inherit
		}
		outV.Field(i).Set(f)
	}
	return out
}

// coreJSONNames maps each PlantCore field index to its JSON key, resolved once
// from the struct tags. It lets a PATCH's set of present keys be matched against
// the struct without a second, hand-kept table to fall out of sync.
var coreJSONNames = func() []string {
	out := make([]string, coreType.NumField())
	for i := range out {
		name, _, _ := strings.Cut(coreType.Field(i).Tag.Get("json"), ",")
		out[i] = name
	}
	return out
}()

// mergeCorePatch is mergeCore for an UPDATE rather than an inheritance.
//
// The difference is the whole of "explicitly null" vs "omitted", which both
// decode to a nil pointer and which mergeCore alone cannot tell apart: a field
// the payload ACTUALLY CARRIED wins even when it carried null, so a value can be
// cleared; a field the payload omitted is inherited from base, which is what
// keeps a PATCH partial. Without this, a rotation_break_years typed by mistake
// could be changed but never removed.
func mergeCorePatch(base PlantCore, over *PlantCore, present presentFields) PlantCore {
	out := mergeCore(base, over)
	if over == nil || len(present) == 0 {
		return out
	}
	outV := reflect.ValueOf(&out).Elem()
	overV := reflect.ValueOf(*over)
	for i := 0; i < coreType.NumField(); i++ {
		if name := coreJSONNames[i]; name == "" || !present[name] {
			continue
		}
		outV.Field(i).Set(overV.Field(i))
	}
	return out
}

// ---- derived values that belong with resolution, not with their callers ----

// Density returns the effective planting density in plants per m²: the explicit
// override when set, otherwise derived from the two spacings. Used by check C4's
// over-booking arithmetic and by the expected-yield comparison.
//
// Returns ok=false when neither is available — a bed whose occupancy cannot be
// sized simply is not checked for over-booking, rather than being checked
// against a made-up number.
func (e Effective) Density() (float64, bool) {
	if e.PlantsPerM2 != nil && *e.PlantsPerM2 > 0 {
		return *e.PlantsPerM2, true
	}
	if e.SpacingRowCM != nil && e.SpacingPlantCM != nil && *e.SpacingRowCM > 0 && *e.SpacingPlantCM > 0 {
		// cm × cm → m²: 10 000 cm² in a square metre.
		return 10000 / (*e.SpacingRowCM * *e.SpacingPlantCM), true
	}
	return 0, false
}

// AreaOf returns how much bed area a planting occupies, from whichever of
// area_m2 / plant_count it carries (the schema guarantees exactly one).
func (e Effective) AreaOf(p Planting) (float64, bool) {
	if p.AreaM2 != nil && *p.AreaM2 > 0 {
		return *p.AreaM2, true
	}
	if p.PlantCount != nil && *p.PlantCount > 0 {
		if density, ok := e.Density(); ok && density > 0 {
			return float64(*p.PlantCount) / density, true
		}
	}
	return 0, false
}

// ExpectedYield returns the KB's expected yield for a planting, in the crop's own
// unit — yield_per_m2 × area, or yield_per_plant × count. This is the number the
// planting detail shows the actual harvest against; without it, recording a
// harvest has no payoff.
func (e Effective) ExpectedYield(p Planting) *float64 {
	if e.YieldPerPlant != nil && p.PlantCount != nil {
		return fp(round2(*e.YieldPerPlant * float64(*p.PlantCount)))
	}
	if e.YieldPerM2 != nil {
		if area, ok := e.AreaOf(p); ok {
			return fp(round2(*e.YieldPerM2 * area))
		}
	}
	return nil
}

// RotationBreak returns how many seasons must pass before this crop's family
// returns to a bed: the crop's own value, else the family default, else the
// settings default. Three levels, resolved in one place so C3 cannot disagree
// with the planner's history column.
func (e Effective) RotationBreak(rules []Rule, fallback int) int {
	if e.RotationBreakYears != nil {
		return *e.RotationBreakYears
	}
	for _, r := range rules {
		if r.Scope == ScopeSuccession && !r.IsDisabled && r.ARef == e.Family && r.BRef == e.Family && r.MinYearsGap != nil {
			return *r.MinYearsGap
		}
	}
	return fallback
}

// Unit returns the crop's harvest unit, defaulting to kg. The default exists so
// a half-filled crop still records a harvest rather than blocking the form; kg is
// the only unit the season metric sums, so it is also the honest fallback.
func (e Effective) Unit() string {
	if e.HarvestUnit != nil && *e.HarvestUnit != "" {
		return *e.HarvestUnit
	}
	return UnitKg
}

// IsFrostSensitive reports whether a crop is at risk on a frost night — read by
// the weather job and by check C9.
func (e Effective) IsFrostSensitive() bool {
	return e.Hardiness == HardinessTender || e.Hardiness == HardinessHalfHardy
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
