package garden

import (
	"reflect"
	"testing"
)

// fillCore builds a PlantCore with EVERY field set, using `seed` to make the
// values distinguishable between two fillings.
//
// It is reflective on purpose. The brief asks for a test that fails when a new
// overridable column is added without being mirrored on the variety; sharing one
// PlantCore struct makes the mirror structural, so what is left to guard is that
// the TEST still covers every field. fillCore does that by construction: a new
// field of a type it does not know how to fill is left zero, and
// TestFillCoreCoversEveryField fails naming it.
func fillCore(t *testing.T, seed int) PlantCore {
	t.Helper()
	var core PlantCore
	v := reflect.ValueOf(&core).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		name := coreType.Field(i).Name
		switch f.Interface().(type) {
		case *string:
			f.Set(reflect.ValueOf(sp(name + "-" + itoa(seed))))
		case *int:
			f.Set(reflect.ValueOf(ip(seed)))
		case *float64:
			f.Set(reflect.ValueOf(fp(float64(seed) * 1.5)))
		case *bool:
			// The two seeds must DIFFER, or the full-override test cannot tell an
			// override from a leak. A pointer to false is still a non-nil pointer,
			// so it counts as set — which is exactly the distinction a plain bool
			// could not make (see TestResolveExplicitFalseOverridesTrue).
			f.Set(reflect.ValueOf(bp(seed == 1)))
		case *Window:
			f.Set(reflect.ValueOf(&Window{Anchor: AnchorWeek, From: seed, To: seed + 1}))
		case []string:
			f.Set(reflect.ValueOf([]string{name + itoa(seed)}))
		case []PestIssue:
			f.Set(reflect.ValueOf([]PestIssue{{Name: name + itoa(seed)}}))
		default:
			// Deliberately left zero — TestFillCoreCoversEveryField reports it.
		}
	}
	return core
}

func itoa(n int) string { return string(rune('0' + n)) }

func TestFillCoreCoversEveryField(t *testing.T) {
	core := fillCore(t, 1)
	v := reflect.ValueOf(core)
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).IsZero() {
			t.Errorf("PlantCore.%s is not covered by the resolve tests — add its type to fillCore, "+
				"or the inheritance of this field is untested", coreType.Field(i).Name)
		}
	}
}

// A variety with every override NULL must equal the species field for field.
func TestResolveFullInheritance(t *testing.T) {
	plant := Plant{
		PlantCore: fillCore(t, 1),
		ID:        "p1", NameCS: "rajče", Family: FamilySolanaceae,
		PlantType: "vegetable", Hardiness: HardinessTender,
	}
	variety := &Variety{ID: "v1", PlantID: "p1", Name: "Black Krim"} // every override nil

	eff := Resolve(plant, variety)

	if !reflect.DeepEqual(eff.PlantCore, plant.PlantCore) {
		t.Error("a variety with no overrides must resolve to the species field for field")
	}
	if eff.Family != FamilySolanaceae || eff.Hardiness != HardinessTender || eff.PlantName != "rajče" {
		t.Error("species identity must survive resolution")
	}
	if eff.VarietyName == nil || *eff.VarietyName != "Black Krim" {
		t.Error("the resolved record must still name the variety it came from")
	}
}

// A variety with every field set must equal itself field for field.
func TestResolveFullOverride(t *testing.T) {
	plant := Plant{PlantCore: fillCore(t, 1), ID: "p1", NameCS: "rajče", Family: FamilySolanaceae}
	variety := &Variety{PlantCore: fillCore(t, 2), ID: "v1", PlantID: "p1", Name: "Black Krim"}

	eff := Resolve(plant, variety)

	if !reflect.DeepEqual(eff.PlantCore, variety.PlantCore) {
		t.Error("a fully-overriding variety must resolve to its own values field for field")
	}
	// And nothing of the species' core leaked through.
	v := reflect.ValueOf(eff.PlantCore)
	base := reflect.ValueOf(plant.PlantCore)
	for i := 0; i < v.NumField(); i++ {
		if reflect.DeepEqual(v.Field(i).Interface(), base.Field(i).Interface()) {
			t.Errorf("PlantCore.%s kept the species value despite an override", coreType.Field(i).Name)
		}
	}
}

// A nil variety is the common case — most plantings name no variety at all.
func TestResolveNilVariety(t *testing.T) {
	plant := Plant{PlantCore: fillCore(t, 1), ID: "p1", NameCS: "mrkev", Family: FamilyApiaceae}
	eff := Resolve(plant, nil)
	if !reflect.DeepEqual(eff.PlantCore, plant.PlantCore) {
		t.Error("resolving against no variety must return the species unchanged")
	}
	if eff.VarietyID != nil || eff.VarietyName != nil {
		t.Error("no variety means no variety identity on the resolved record")
	}
}

// A variety saying "no, this one does NOT need support" must win over a species
// that does. This is why every core field is a pointer: a plain bool could not
// distinguish "false" from "not set".
func TestResolveExplicitFalseOverridesTrue(t *testing.T) {
	plant := Plant{PlantCore: PlantCore{NeedsSupport: bp(true)}, ID: "p1", NameCS: "rajče"}
	bush := &Variety{PlantCore: PlantCore{NeedsSupport: bp(false)}, ID: "v1", PlantID: "p1", Name: "keříčkové"}

	if got := Resolve(plant, bush); derefB(got.NeedsSupport) {
		t.Error("a variety that explicitly needs no support must override a species that does")
	}
}

func TestEffectiveDensityAndArea(t *testing.T) {
	// 40 cm rows × 50 cm spacing ⇒ 10000/(40×50) = 5 plants/m².
	e := Effective{PlantCore: PlantCore{SpacingRowCM: fp(40), SpacingPlantCM: fp(50)}}
	density, ok := e.Density()
	if !ok || density != 5 {
		t.Fatalf("density = %v (ok=%v), want 5", density, ok)
	}
	// An explicit override wins over the derivation.
	e.PlantsPerM2 = fp(9)
	if density, _ := e.Density(); density != 9 {
		t.Errorf("an explicit plants_per_m2 must win, got %v", density)
	}
	// 18 plants at 9/m² is 2 m².
	if area, ok := e.AreaOf(Planting{PlantCount: ip(18)}); !ok || area != 2 {
		t.Errorf("AreaOf(count) = %v (ok=%v), want 2", area, ok)
	}
	// An explicit area is taken as given.
	if area, ok := e.AreaOf(Planting{AreaM2: fp(3.5)}); !ok || area != 3.5 {
		t.Errorf("AreaOf(area) = %v (ok=%v), want 3.5", area, ok)
	}
	// Neither ⇒ not sized, and C4 then simply does not check this planting
	// rather than checking it against a made-up number.
	if _, ok := (Effective{}).AreaOf(Planting{PlantCount: ip(10)}); ok {
		t.Error("a crop with no spacing and no density must not report an area")
	}
}

func TestEffectiveRotationBreak(t *testing.T) {
	rules := []Rule{{Scope: ScopeSuccession, ARef: FamilyBrassicaceae, BRef: FamilyBrassicaceae, MinYearsGap: ip(4)}}

	// The crop's own value wins.
	own := Effective{PlantCore: PlantCore{RotationBreakYears: ip(2)}, Family: FamilyBrassicaceae}
	if got := own.RotationBreak(rules, 3); got != 2 {
		t.Errorf("crop value = %d, want 2", got)
	}
	// Else the family default.
	fam := Effective{Family: FamilyBrassicaceae}
	if got := fam.RotationBreak(rules, 3); got != 4 {
		t.Errorf("family default = %d, want 4", got)
	}
	// Else the settings fallback.
	other := Effective{Family: FamilyLamiaceae}
	if got := other.RotationBreak(rules, 3); got != 3 {
		t.Errorf("settings fallback = %d, want 3", got)
	}
	// A DISABLED family rule is not a rule.
	disabled := []Rule{{Scope: ScopeSuccession, ARef: FamilyBrassicaceae, BRef: FamilyBrassicaceae, MinYearsGap: ip(4), IsDisabled: true}}
	if got := fam.RotationBreak(disabled, 3); got != 3 {
		t.Errorf("disabled rule = %d, want the settings fallback 3", got)
	}
}
