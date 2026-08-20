package garden

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// THE PROMPT HAND-OFF (PRD FR-G3, D114).
//
// The schema below is GENERATED from the same enum registry the importer
// validates against — not written by hand and not kept in sync by discipline.
// One registry feeds the validator, this schema, and GET /api/garden/enums, so a
// prompt cannot ask a model for a field the importer would reject. That failure
// is the specific reason this design exists.

// PromptContext is what the prompt told the model about this garden.
type PromptContext struct {
	LastFrostOn  *string  `json:"last_frost_on"`
	FirstFrostOn *string  `json:"first_frost_on"`
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
	AltitudeM    *int     `json:"altitude_m"`
}

// PromptTemplate is GET /api/garden/plants/prompt-template.
type PromptTemplate struct {
	Prompt  string         `json:"prompt"`
	Schema  map[string]any `json:"schema"`
	Context PromptContext  `json:"context"`
}

// PromptTemplate builds the ready-to-paste Czech prompt.
//
// With a plantID it includes the crop's CURRENT values, so the model fills gaps
// instead of overwriting what somebody already checked — which is the difference
// between "help me finish this" and "start again".
func (s *Service) PromptTemplate(ctx context.Context, plantID string) (PromptTemplate, error) {
	settings, err := s.store.Settings(ctx, nil)
	if err != nil {
		return PromptTemplate{}, err
	}
	pctx := PromptContext{
		Latitude: settings.Latitude, Longitude: settings.Longitude, AltitudeM: settings.AltitudeM,
	}
	// Frost dates come from the current season when there is one, else from the
	// garden's defaults — the model should reason about this garden's climate,
	// not about Czechia in general.
	if se, err := s.store.SeasonByYear(ctx, nil, s.today().Y); err == nil {
		pctx.LastFrostOn, pctx.FirstFrostOn = se.LastFrostOn, se.FirstFrostOn
	} else {
		pctx.LastFrostOn = frostDateFor(settings.DefaultLastFrost, s.today().Y)
		pctx.FirstFrostOn = frostDateFor(settings.DefaultFirstFrost, s.today().Y)
	}

	var existing *Plant
	if plantID != "" {
		p, err := s.store.GetPlant(ctx, nil, plantID)
		if err != nil {
			return PromptTemplate{}, mapNotFound(err)
		}
		existing = &p
	}
	schema := PlantJSONSchema()
	return PromptTemplate{
		Prompt:  buildPromptCS(pctx, schema, existing),
		Schema:  schema,
		Context: pctx,
	}, nil
}

// PlantJSONSchema generates the JSON Schema the importer validates against.
// Every enum's `enum` array comes from the registry, so the three consumers
// cannot drift.
func PlantJSONSchema() map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	num := func(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
	integer := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	boolean := func(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }
	enumOf := func(name, desc string) map[string]any {
		return map[string]any{"type": "string", "enum": Values(name), "description": desc}
	}
	window := func(desc string) map[string]any {
		return map[string]any{
			"type":        "object",
			"description": desc,
			"required":    []string{"anchor", "from", "to"},
			"properties": map[string]any{
				"anchor": map[string]any{"type": "string", "enum": Values(EnumTimingAnchor),
					"description": "week = ISO týden v roce (from/to 1–53); last_frost / first_frost = počet dní vůči mrazu (záporně = před ním)"},
				"from": integer("začátek okna"),
				"to":   integer("konec okna, nikdy menší než from"),
			},
		}
	}
	issue := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type":     "object",
			"required": []string{"name"},
			"properties": map[string]any{
				"name":      str("název"),
				"symptom":   str("jak se projevuje"),
				"remedy_md": str("co s tím, Markdown"),
			},
		},
	}

	return map[string]any{
		"type":                 "object",
		"required":             []string{"name_cs", "family", "plant_type", "hardiness", "harvest_unit"},
		"additionalProperties": false,
		"properties": map[string]any{
			"name_cs":               str("český název plodiny, jednoslovný pokud to jde"),
			"name_latin":            str("latinský název"),
			"family":                enumOf(EnumFamily, "botanická čeleď — POVINNÉ, řídí se podle ní střídání plodin"),
			"plant_type":            enumOf(EnumPlantType, "typ plodiny"),
			"hardiness":             enumOf(EnumHardiness, "odolnost proti mrazu — POVINNÉ, řídí se podle ní varování před mrazem"),
			"feeder_class":          enumOf(EnumFeederClass, "nárok na živiny"),
			"root_depth":            enumOf(EnumRootDepth, "hloubka kořenů"),
			"sun":                   enumOf(EnumSun, "nárok na osvit"),
			"water_need":            enumOf(EnumWaterNeed, "nárok na vodu"),
			"sow_method":            enumOf(EnumSowMethod, "způsob výsevu"),
			"harvest_unit":          enumOf(EnumHarvestUnit, "v čem se sklizeň měří"),
			"soil_ph_min":           num("dolní vhodné pH půdy"),
			"soil_ph_max":           num("horní vhodné pH půdy"),
			"rotation_break_years":  integer("kolik let počkat, než se čeleď vrátí na stejný záhon"),
			"needs_pricking_out":    boolean("je potřeba pikýrovat sazenice"),
			"needs_support":         boolean("potřebuje oporu nebo vyvazování"),
			"wants_mulch":           boolean("prospěje jí mulčování"),
			"wants_pest_check":      boolean("vyplatí se pravidelná kontrola škůdců"),
			"hardening_days":        integer("kolik dní otužovat před výsadbou ven"),
			"sow_depth_cm":          num("hloubka výsevu v cm"),
			"spacing_row_cm":        num("rozestup řádků v cm"),
			"spacing_plant_cm":      num("rozestup rostlin v řádku v cm"),
			"plants_per_m2":         num("rostlin na m², pokud se liší od výpočtu z rozestupů"),
			"days_to_germinate_min": integer("nejkratší doba klíčení ve dnech"),
			"days_to_germinate_max": integer("nejdelší doba klíčení ve dnech"),
			"germination_temp_c":    integer("ideální teplota klíčení ve °C"),
			"days_to_maturity_min":  integer("nejkratší doba do sklizně ve dnech"),
			"days_to_maturity_max":  integer("nejdelší doba do sklizně ve dnech"),
			"win_sow_indoor":        window("termín výsevu do sadbovačů"),
			"win_sow_direct":        window("termín přímého výsevu na záhon"),
			"win_transplant":        window("termín výsadby ven"),
			"win_harvest":           window("termín sklizně"),
			"yield_per_m2":          num("očekávaný výnos na m² v jednotce harvest_unit"),
			"yield_per_plant":       num("očekávaný výnos na rostlinu v jednotce harvest_unit"),
			"storage_methods": map[string]any{"type": "array",
				"items":       map[string]any{"type": "string", "enum": Values(EnumStorageMethod)},
				"description": "jak se dá skladovat"},
			"storage_temp_c":   integer("skladovací teplota ve °C"),
			"storage_humidity": integer("skladovací vlhkost v procentech"),
			"shelf_life_days":  integer("jak dlouho vydrží ve skladu ve dnech"),
			"pests":            issue,
			"diseases":         issue,
			"notes_md":         str("volné poznámky v Markdownu — odrůdy, zkušenosti, na co si dát pozor"),
		},
	}
}

// buildPromptCS assembles the Czech prompt around the generated schema.
func buildPromptCS(ctx PromptContext, schema map[string]any, existing *Plant) string {
	var b strings.Builder

	b.WriteString("Jsi zkušený český zahradník a agronom. Vyplň strukturovaný popis plodiny pro domácí zahradu.\n\n")
	b.WriteString("KONTEXT ZAHRADY\n")
	b.WriteString("- Česká republika, mírné pásmo.\n")
	if ctx.LastFrostOn != nil {
		b.WriteString("- Očekávaný poslední jarní mráz: " + *ctx.LastFrostOn + "\n")
	}
	if ctx.FirstFrostOn != nil {
		b.WriteString("- Očekávaný první podzimní mráz: " + *ctx.FirstFrostOn + "\n")
	}
	if ctx.Latitude != nil && ctx.Longitude != nil {
		b.WriteString(fmt.Sprintf("- Poloha: %.3f, %.3f\n", *ctx.Latitude, *ctx.Longitude))
	}
	if ctx.AltitudeM != nil {
		b.WriteString("- Nadmořská výška: " + strconv.Itoa(*ctx.AltitudeM) + " m n. m.\n")
	}

	b.WriteString("\nPRAVIDLA\n")
	b.WriteString("1. Odpověz VÝHRADNĚ platným JSON podle schématu níže. Žádný úvod, žádný komentář, žádné ```json.\n")
	b.WriteString("2. Pole, které neznáš, prostě vynech. Nehádej — vynechané pole je lepší než vymyšlené číslo.\n")
	b.WriteString("3. U výčtových polí použij PŘESNĚ jednu z povolených hodnot.\n")
	b.WriteString("4. Termíny zadávej jako okno {anchor, from, to}. Použij `week` (ISO týden 1–53) tam, kde se to v české literatuře uvádí kalendářně, a `last_frost` / `first_frost` (počet dní, záporně = před mrazem) tam, kde termín na mrazu opravdu závisí — typicky výsev do sadbovačů a výsadba ven.\n")
	b.WriteString("5. `family` a `hardiness` jsou povinné. Bez nich plodinu nelze zařadit do střídání ani hlídat před mrazem.\n")
	b.WriteString("6. Můžeš odpovědět polem více plodin najednou.\n")

	if existing != nil {
		b.WriteString("\nSOUČASNÝ ZÁZNAM — doplň, co chybí, a nepřepisuj to, co už je vyplněné:\n")
		if rec := coreToRecord(existing.PlantCore); len(rec) > 0 {
			rec["name_cs"] = existing.NameCS
			rec["family"] = existing.Family
			rec["plant_type"] = existing.PlantType
			rec["hardiness"] = existing.Hardiness
			b.WriteString(jsonIndent(rec))
			b.WriteString("\n")
		}
	} else {
		b.WriteString("\nPLODINA: (doplň název)\n")
	}

	b.WriteString("\nSCHÉMA ODPOVĚDI\n")
	b.WriteString(jsonIndent(schema))
	b.WriteString("\n")
	return b.String()
}

func jsonIndent(v any) string {
	b, err := jsonMarshalIndent(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func jsonMarshalIndent(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }
