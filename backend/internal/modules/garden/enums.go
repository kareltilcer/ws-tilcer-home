package garden

import (
	"sort"
	"strings"
)

// ONE ENUM REGISTRY, THREE CONSUMERS (PRD D114, D126, HANDOFF-9 §15).
//
// Everything in this file feeds exactly three things and nothing hand-writes any
// of them:
//
//  1. the importer's validator (importer.go),
//  2. the JSON Schema embedded in the LLM prompt (prompt.go),
//  3. GET /api/garden/enums, which the frontend's pickers render.
//
// That is the whole point of the registry. A prompt that asks a model for a
// value the importer would reject is the specific failure this design exists to
// prevent, and it can only happen if one of the three is written by hand.
//
// Aliases are how a Czech word from a model ("citlivá", "mrazuvzdorná") maps
// onto a code identifier. Matching is lenient over this list — but an input that
// matches NOTHING is a 422 naming field and value, never a silent default
// (FR-G3). A quietly defaulted enum is a wrong crop nobody ever notices.

// Botanical family (D104). A fixed enum, not free text: it is the key the
// rotation engine and the family-pair rules join on, and free text would make
// both a lottery.
const (
	FamilyBrassicaceae   = "brassicaceae"
	FamilySolanaceae     = "solanaceae"
	FamilyFabaceae       = "fabaceae"
	FamilyApiaceae       = "apiaceae"
	FamilyCucurbitaceae  = "cucurbitaceae"
	FamilyAmaryllidaceae = "amaryllidaceae"
	FamilyAsteraceae     = "asteraceae"
	FamilyAmaranthaceae  = "amaranthaceae"
	FamilyPoaceae        = "poaceae"
	FamilyLamiaceae      = "lamiaceae"
	FamilyRosaceae       = "rosaceae"
	FamilyPolygonaceae   = "polygonaceae"
	FamilyGrossulariaceae = "grossulariaceae"
	FamilyValerianaceae  = "valerianaceae"
	FamilyOther          = "other"
)

// Hardiness (D111). REQUIRED on every crop — it is what the frost evaluation
// reads to decide whether a planting is at risk tonight.
const (
	HardinessTender    = "tender"
	HardinessHalfHardy = "half_hardy"
	HardinessHardy     = "hardy"
)

// Feeder class — drives check C8.
const (
	FeederHeavy  = "heavy"
	FeederMedium = "medium"
	FeederLight  = "light"
	FeederFixer  = "fixer"
)

// Sow method.
const (
	SowDirect     = "direct"
	SowSeedling   = "seedling"
	SowBoth       = "both"
	SowVegetative = "vegetative"
)

// Harvest unit. Only `kg` is summable into garden.harvest_season — the metric
// excludes the rest rather than adding apples to bunches.
const (
	UnitKg     = "kg"
	UnitKs     = "ks"
	UnitL      = "l"
	UnitSvazek = "svazek"
)

// Season status.
const (
	SeasonPlanning = "planning"
	SeasonActive   = "active"
	SeasonClosed   = "closed"
)

// Planting status.
const (
	PlantingPlanned    = "planned"
	PlantingSown       = "sown"
	PlantingPlanted    = "planted"
	PlantingGrowing    = "growing"
	PlantingHarvesting = "harvesting"
	PlantingDone       = "done"
	PlantingFailed     = "failed"
)

// Task status.
const (
	TaskOpen    = "open"
	TaskDone    = "done"
	TaskSkipped = "skipped"
)

// Task kinds. `water` and `weed` are MANUAL-ONLY (D118) — they are decided by
// looking at the garden, and a generated cadence nobody ticks off devalues every
// other row in the list.
const (
	KindBedPrep    = "bed_prep"
	KindSowIndoor  = "sow_indoor"
	KindPrickOut   = "prick_out"
	KindHardenOff  = "harden_off"
	KindSowDirect  = "sow_direct"
	KindTransplant = "transplant"
	KindThin       = "thin"
	KindSupport    = "support"
	KindFeed       = "feed"
	KindMulch      = "mulch"
	KindPestCheck  = "pest_check"
	KindPrune      = "prune"
	KindSpray      = "spray"
	KindHarvest    = "harvest"
	KindProcess    = "process"
	KindStore      = "store"
	KindClear      = "clear"
	KindWater      = "water"
	KindWeed       = "weed"
	KindOther      = "other"
)

// Warning severity. Four levels that must read WITHOUT colour (design §v7): an
// icon and a word each, not four shades. `tip` is praise, not a warning.
const (
	SeverityError = "error"
	SeverityWarn  = "warn"
	SeverityInfo  = "info"
	SeverityTip   = "tip"
)

// Rule scope and verdict.
const (
	ScopePlantPair  = "plant_pair"
	ScopeFamilyPair = "family_pair"
	ScopeSuccession = "succession"

	VerdictGood = "good"
	VerdictBad  = "bad"
)

// Provenance (D114).
const (
	SourceManual = "manual"
	SourceLLM    = "llm"
)

// Storage status.
const (
	StorageStored   = "stored"
	StorageConsumed = "consumed"
	StorageSpoiled  = "spoiled"
)

// EnumValue is one member as the API and the prompt see it.
type EnumValue struct {
	Value   string  `json:"value"`
	LabelCS string  `json:"label_cs"`
	NoteCS  *string `json:"note_cs"`
	// aliases are lowercase, diacritics preserved; matched leniently by Coerce.
	// Not serialised: the frontend picks from label_cs, and publishing the alias
	// list would invite callers to depend on it.
	aliases []string
}

// EnumDef is one named enum in the registry.
type EnumDef struct {
	Name   string
	Values []EnumValue
}

func ev(value, label string, aliases ...string) EnumValue {
	return EnumValue{Value: value, LabelCS: label, aliases: aliases}
}

// Enum names, used as JSON Schema property types and as the keys of
// GET /api/garden/enums.
const (
	EnumFamily        = "family"
	EnumPlantType     = "plant_type"
	EnumHardiness     = "hardiness"
	EnumFeederClass   = "feeder_class"
	EnumRootDepth     = "root_depth"
	EnumSun           = "sun"
	EnumWaterNeed     = "water_need"
	EnumSowMethod     = "sow_method"
	EnumHarvestUnit   = "harvest_unit"
	EnumStorageMethod = "storage_method"
	EnumBedType       = "bed_type"
	EnumTimingAnchor  = "timing_anchor"
	EnumSeverity      = "severity"
	EnumRuleScope     = "rule_scope"
	EnumVerdict       = "verdict"
	EnumTaskKind      = "task_kind"
	EnumTaskStatus    = "task_status"
	EnumPlantingStatus = "planting_status"
	EnumSeasonStatus  = "season_status"
	EnumStorageStatus = "storage_status"
	EnumDestination   = "harvest_destination"
)

// enumRegistry is the single declaration. Order inside each enum is the order the
// pickers render and the prompt lists, so it is deliberate rather than
// alphabetical: the common members come first.
var enumRegistry = []EnumDef{
	{EnumFamily, []EnumValue{
		ev(FamilyBrassicaceae, "Brukvovité", "brukvovite", "brassica", "košťáloviny", "kostaloviny", "zelí", "kapusta"),
		ev(FamilySolanaceae, "Lilkovité", "lilkovite", "solanum", "rajčatovité"),
		ev(FamilyFabaceae, "Bobovité", "bobovite", "luskoviny", "legume", "motýlokvěté", "motylokvete"),
		ev(FamilyApiaceae, "Miříkovité", "mirikovite", "okoličnaté", "okolicnate", "umbelliferae", "celerovité"),
		ev(FamilyCucurbitaceae, "Tykvovité", "tykvovite", "cucurbita", "dýňovité", "dynovite"),
		ev(FamilyAmaryllidaceae, "Amarylkovité", "amarylkovite", "cibulovité", "cibulovite", "alliaceae", "allium"),
		ev(FamilyAsteraceae, "Hvězdnicovité", "hvezdnicovite", "compositae", "salátovité", "salatovite"),
		ev(FamilyAmaranthaceae, "Laskavcovité", "laskavcovite", "chenopodiaceae", "merlíkovité", "merlikovite", "špenátovité"),
		ev(FamilyPoaceae, "Lipnicovité", "lipnicovite", "trávy", "travy", "gramineae", "obiloviny"),
		ev(FamilyLamiaceae, "Hluchavkovité", "hluchavkovite", "labiatae", "bylinky"),
		ev(FamilyRosaceae, "Růžovité", "ruzovite", "rosacea"),
		ev(FamilyPolygonaceae, "Rdesnovité", "rdesnovite", "šťovíkovité", "stovikovite"),
		ev(FamilyGrossulariaceae, "Meruzalkovité", "meruzalkovite", "rybízovité", "rybizovite"),
		ev(FamilyValerianaceae, "Kozlíkovité", "kozlikovite", "polníčkovité", "polnickovite"),
		ev(FamilyOther, "Jiná", "jina", "ostatní", "ostatni", "neznámá", "neznama"),
	}},
	{EnumPlantType, []EnumValue{
		ev("vegetable", "Zelenina", "zelenina"),
		ev("herb", "Bylinka", "bylinka", "bylina", "koření", "koreni"),
		ev("fruit_tree", "Ovocný strom", "ovocny strom", "strom", "dřevina", "drevina"),
		ev("fruit_bush", "Ovocný keř", "ovocny ker", "keř", "ker"),
		ev("perennial", "Trvalka", "trvalka", "vytrvalá", "vytrvala"),
		ev("flower", "Květina", "kvetina", "okrasná", "okrasna"),
	}},
	{EnumHardiness, []EnumValue{
		ev(HardinessTender, "citlivá", "citliva", "mrazlivá", "mrazliva", "netolerantní k mrazu", "teplomilná", "teplomilna"),
		ev(HardinessHalfHardy, "polootužilá", "polootuzila", "polotužilá", "polotuzila", "částečně otužilá", "castecne otuzila"),
		ev(HardinessHardy, "otužilá", "otuzila", "mrazuvzdorná", "mrazuvzdorna", "odolná mrazu", "odolna mrazu"),
	}},
	{EnumFeederClass, []EnumValue{
		ev(FeederHeavy, "vysoký", "vysoky", "silný", "silny", "náročná", "narocna", "hodně živin", "hodne zivin"),
		ev(FeederMedium, "střední", "stredni", "průměrný", "prumerny"),
		ev(FeederLight, "nízký", "nizky", "slabý", "slaby", "nenáročná", "nenarocna"),
		ev(FeederFixer, "váže dusík", "vaze dusik", "fixátor", "fixator", "luskovina"),
	}},
	{EnumRootDepth, []EnumValue{
		ev("shallow", "mělké", "melke", "mělký", "melky"),
		ev("medium", "střední", "stredni"),
		ev("deep", "hluboké", "hluboke", "hluboký", "hluboky"),
	}},
	{EnumSun, []EnumValue{
		ev("full", "plné slunce", "plne slunce", "slunce", "slunné", "slunne"),
		ev("partial", "polostín", "polostin", "částečný stín", "castecny stin"),
		ev("shade", "stín", "stin", "stinné", "stinne"),
	}},
	{EnumWaterNeed, []EnumValue{
		ev("low", "nízká", "nizka", "málo", "malo"),
		ev("medium", "střední", "stredni"),
		ev("high", "vysoká", "vysoka", "hodně", "hodne"),
	}},
	{EnumSowMethod, []EnumValue{
		ev(SowDirect, "přímý výsev", "primy vysev", "přímo", "primo", "na místo", "na misto"),
		ev(SowSeedling, "předpěstování", "predpestovani", "sadba", "ze sadby", "do sadbovačů", "do sadbovacu"),
		ev(SowBoth, "obojí", "oboji", "přímo i ze sadby", "primo i ze sadby"),
		ev(SowVegetative, "vegetativně", "vegetativne", "sazenice", "hlízy", "hlizy", "cibule", "řízky", "rizky"),
	}},
	{EnumHarvestUnit, []EnumValue{
		ev(UnitKg, "kg", "kilogram", "kilogramy"),
		ev(UnitKs, "ks", "kus", "kusy", "kusů", "kusu"),
		ev(UnitL, "l", "litr", "litry"),
		ev(UnitSvazek, "svazek", "svazky", "svazků", "svazku"),
	}},
	{EnumStorageMethod, []EnumValue{
		ev("cellar", "sklep", "ve sklepě", "ve sklepe", "chladno"),
		ev("dry", "sušení", "suseni", "sušit", "susit", "sušené", "susene"),
		ev("freeze", "mražení", "mrazeni", "zmrazit", "mrazák", "mrazak"),
		ev("ferment", "kvašení", "kvaseni", "fermentace", "kysané", "kysane"),
		ev("can", "zavařování", "zavarovani", "sterilace", "sterilování", "sterilovani", "konzervování"),
		ev("sand", "v písku", "v pisku", "zapískování", "zapiskovani"),
		ev("in_ground", "v zemi", "na záhonu", "na zahonu", "přezimování", "prezimovani"),
	}},
	{EnumBedType, []EnumValue{
		ev("raised", "vyvýšený", "vyvyseny", "vyvýšený záhon", "bedýnka"),
		ev("ground", "na zemi", "klasický", "klasicky", "rovina"),
		ev("greenhouse", "skleník", "sklenik"),
		ev("polytunnel", "fóliovník", "foliovnik", "fólie", "folie"),
		ev("container", "nádoba", "nadoba", "květináč", "kvetinac", "truhlík", "truhlik"),
		ev("orchard", "sad", "ovocný sad", "ovocny sad"),
	}},
	{EnumTimingAnchor, []EnumValue{
		ev(AnchorWeek, "týden v roce", "tyden", "tyden v roce", "iso týden", "iso tyden"),
		ev(AnchorLastFrost, "vůči poslednímu jarnímu mrazu", "posledni mraz", "jarní mráz", "jarni mraz", "last frost"),
		ev(AnchorFirstFrost, "vůči prvnímu podzimnímu mrazu", "prvni mraz", "podzimní mráz", "podzimni mraz", "first frost"),
	}},
	{EnumSeverity, []EnumValue{
		ev(SeverityError, "chyba", "chyba", "závažné", "zavazne"),
		ev(SeverityWarn, "varování", "varovani", "upozornění", "upozorneni"),
		ev(SeverityInfo, "informace", "info", "poznámka", "poznamka"),
		ev(SeverityTip, "tip", "doporučení", "doporuceni", "pochvala"),
	}},
	{EnumRuleScope, []EnumValue{
		ev(ScopePlantPair, "dvojice plodin", "plodiny", "plant pair"),
		ev(ScopeFamilyPair, "dvojice čeledí", "celedi", "family pair"),
		ev(ScopeSuccession, "následnost", "nasledost", "nasledovnost", "rotace", "succession"),
	}},
	{EnumVerdict, []EnumValue{
		ev(VerdictGood, "vhodné", "vhodne", "dobré", "dobre", "prospívá", "prospiva"),
		ev(VerdictBad, "nevhodné", "nevhodne", "špatné", "spatne", "škodí", "skodi"),
	}},
	{EnumTaskKind, []EnumValue{
		ev(KindBedPrep, "příprava záhonu", "priprava zahonu", "příprava", "priprava"),
		ev(KindSowIndoor, "výsev do sadbovačů", "vysev do sadbovacu", "předpěstování", "predpestovani"),
		ev(KindPrickOut, "pikýrování", "pikyrovani", "přepichování", "prepichovani"),
		ev(KindHardenOff, "otužování", "otuzovani", "zakalování", "zakalovani"),
		ev(KindSowDirect, "přímý výsev", "primy vysev", "výsev", "vysev"),
		ev(KindTransplant, "výsadba", "vysadba", "sázení", "sazeni"),
		ev(KindThin, "protrhání", "protrhani", "jednocení", "jednoceni"),
		ev(KindSupport, "opora a vyvazování", "opora", "vyvazování", "vyvazovani"),
		ev(KindFeed, "přihnojení", "prihnojeni", "hnojení", "hnojeni"),
		ev(KindMulch, "mulčování", "mulcovani", "mulč", "mulc"),
		ev(KindPestCheck, "kontrola škůdců", "kontrola skudcu", "škůdci", "skudci"),
		ev(KindPrune, "řez", "rez", "prořez", "prorez", "stříhání", "strihani"),
		ev(KindSpray, "postřik", "postrik", "ošetření", "osetreni"),
		ev(KindHarvest, "sklizeň", "sklizen", "sklizení", "sklizeni"),
		ev(KindProcess, "zpracování", "zpracovani", "zavařování", "zavarovani"),
		ev(KindStore, "uskladnění", "uskladneni", "uložení", "ulozeni"),
		ev(KindClear, "úklid záhonu", "uklid zahonu", "vyklizení", "vyklizeni"),
		ev(KindWater, "zálivka", "zalivka", "zalévání", "zalevani"),
		ev(KindWeed, "plení", "pleni", "odplevelení", "odpleveleni"),
		ev(KindOther, "jiné", "jine", "ostatní", "ostatni"),
	}},
	{EnumTaskStatus, []EnumValue{
		ev(TaskOpen, "otevřená", "otevrena", "nesplněná", "nesplnena"),
		ev(TaskDone, "hotová", "hotova", "splněná", "splnena"),
		ev(TaskSkipped, "přeskočená", "preskocena", "vynechaná", "vynechana"),
	}},
	{EnumPlantingStatus, []EnumValue{
		ev(PlantingPlanned, "naplánováno", "naplanovano", "plán", "plan"),
		ev(PlantingSown, "vyseto", "vysetá", "zaseto"),
		ev(PlantingPlanted, "vysazeno", "zasazeno"),
		ev(PlantingGrowing, "roste", "rostoucí", "rostouci"),
		ev(PlantingHarvesting, "sklízí se", "sklizi se", "ve sklizni"),
		ev(PlantingDone, "hotovo", "sklizeno", "dokončeno", "dokonceno"),
		ev(PlantingFailed, "nepovedlo se", "neúspěch", "neuspech", "selhalo"),
	}},
	{EnumSeasonStatus, []EnumValue{
		ev(SeasonPlanning, "plánuje se", "planuje se", "plánování", "planovani"),
		ev(SeasonActive, "probíhá", "probiha", "aktivní", "aktivni"),
		ev(SeasonClosed, "uzavřená", "uzavrena", "uzavřeno", "uzavreno"),
	}},
	{EnumStorageStatus, []EnumValue{
		ev(StorageStored, "uskladněno", "uskladneno", "ve skladu"),
		ev(StorageConsumed, "spotřebováno", "spotrebovano", "snědeno", "snedeno"),
		ev(StorageSpoiled, "zkazilo se", "zkažené", "zkazene", "plesnivé", "plesnive"),
	}},
	{EnumDestination, []EnumValue{
		ev("fresh", "čerstvé", "cerstve", "na přímo", "hned"),
		ev("storage", "sklad", "do skladu", "uskladnit"),
		ev("gift", "darováno", "darovano", "rozdáno", "rozdano"),
		ev("compost", "kompost", "na kompost", "vyhozeno"),
	}},
}

// byName indexes the registry once at init. The registry is read-only after
// this, so no lock is needed.
var byName = func() map[string]EnumDef {
	m := make(map[string]EnumDef, len(enumRegistry))
	for _, d := range enumRegistry {
		m[d.Name] = d
	}
	return m
}()

// Enums returns the whole registry, for GET /api/garden/enums and for the
// generated prompt schema. Sorted by name so the response is stable across
// restarts (the pickers' own order is preserved WITHIN each enum).
func Enums() []EnumDef {
	out := make([]EnumDef, len(enumRegistry))
	copy(out, enumRegistry)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Values returns the legal code values of one enum, in registry order. This is
// what the generated JSON Schema's `enum` array contains.
func Values(name string) []string {
	d, ok := byName[name]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(d.Values))
	for _, v := range d.Values {
		out = append(out, v.Value)
	}
	return out
}

// Valid reports whether v is an exact code value of the named enum. Used by
// ordinary API validation, which is strict — leniency belongs to the importer,
// where the input came from a language model rather than from our own UI.
func Valid(name, v string) bool {
	d, ok := byName[name]
	if !ok {
		return false
	}
	for _, m := range d.Values {
		if m.Value == v {
			return true
		}
	}
	return false
}

// Coerce maps a loose input onto an enum member (FR-G3). It matches, in order:
// the exact code value, the Czech label, then any alias — each case-folded and
// with diacritics stripped, so "Citlivá", "citliva" and "CITLIVÁ" all land on
// `tender`.
//
// It returns ok=false rather than a default. The caller turns that into a 422
// naming the field AND the value, because "we silently picked `other` for you"
// is a wrong crop nobody ever notices.
func Coerce(name, input string) (string, bool) {
	d, ok := byName[name]
	if !ok {
		return "", false
	}
	nInput := normalize(input)
	if nInput == "" {
		return "", false
	}
	for _, m := range d.Values {
		if normalize(m.Value) == nInput || normalize(m.LabelCS) == nInput {
			return m.Value, true
		}
		for _, a := range m.aliases {
			if normalize(a) == nInput {
				return m.Value, true
			}
		}
	}
	return "", false
}

// Label returns the Czech label of a member, or the raw value when unknown — a
// log line or an export should degrade to something readable rather than blank.
func Label(name, value string) string {
	d, ok := byName[name]
	if !ok {
		return value
	}
	for _, m := range d.Values {
		if m.Value == value {
			return m.LabelCS
		}
	}
	return value
}

// diacritics maps the Czech letters we fold away when matching. Kept explicit
// rather than pulled from golang.org/x/text: the alphabet is closed, this is
// twenty lines, and it avoids a dependency for a lookup table.
var diacritics = strings.NewReplacer(
	"á", "a", "č", "c", "ď", "d", "é", "e", "ě", "e", "í", "i", "ň", "n",
	"ó", "o", "ř", "r", "š", "s", "ť", "t", "ú", "u", "ů", "u", "ý", "y", "ž", "z",
	"Á", "a", "Č", "c", "Ď", "d", "É", "e", "Ě", "e", "Í", "i", "Ň", "n",
	"Ó", "o", "Ř", "r", "Š", "s", "Ť", "t", "Ú", "u", "Ů", "u", "Ý", "y", "Ž", "z",
)

// normalize case-folds, strips Czech diacritics, and collapses whitespace and
// underscores, so "Půlotužilá", "half hardy" and "half_hardy" compare equal.
func normalize(s string) string {
	s = diacritics.Replace(strings.ToLower(strings.TrimSpace(s)))
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	return strings.Join(strings.Fields(s), " ")
}
