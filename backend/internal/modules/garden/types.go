package garden

import (
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
)

// Wire and domain types for the garden module (openapi.yaml 0.9.0, tag
// `garden`). Nullable fields are pointers WITHOUT omitempty so they serialise as
// explicit `null`, matching the contract's `type: [string, "null"]`.

// errUnprocessable is the module's 422. One helper so every Czech validation
// message is built the same way and shows up in one grep.
func errUnprocessable(msg string) error { return httpx.ErrUnprocessable(msg) }

// PestIssue is one pest or disease. Structured rather than one prose blob,
// because a model fills these well and they are worth searching.
type PestIssue struct {
	Name     string `json:"name"`
	Symptom  string `json:"symptom,omitempty"`
	RemedyMD string `json:"remedy_md,omitempty"`
}

// Provenance records where a record came from (D114). An LLM-filled crop nobody
// has checked must LOOK different from one a human confirmed — the "neověřeno"
// badge is derived from VerifiedAt being null.
type Provenance struct {
	Source      string  `json:"source"` // manual | llm
	SourceModel *string `json:"source_model"`
	SourceAt    *string `json:"source_at"`
	VerifiedBy  *string `json:"verified_by"`
	VerifiedAt  *string `json:"verified_at"`
}

// PlantCore is EVERY overridable knowledge-base field, and it is deliberately
// ONE type embedded in both Plant and Variety (D103).
//
// The brief asks for a nullable mirror of each column on the variety plus a
// reflection test that fails when a new column is not mirrored. Sharing the
// struct is strictly stronger: mirroring becomes structural, so the failure that
// test guards against cannot be written in the first place. Resolve() then walks
// these fields generically, which means a field added here is inherited
// correctly on the day it is added, with no second edit to remember.
//
// Every field is a pointer or a slice, and nil means "not set" on a species and
// "inherit" on a variety — the same nil, read two ways, which is what lets one
// struct serve both.
type PlantCore struct {
	NameLatin *string `json:"name_latin"`

	FeederClass *string `json:"feeder_class"`
	RootDepth   *string `json:"root_depth"`
	Sun         *string `json:"sun"`
	WaterNeed   *string `json:"water_need"`
	SoilPHMin   *float64 `json:"soil_ph_min"`
	SoilPHMax   *float64 `json:"soil_ph_max"`

	// RotationBreakYears defaults from the family when null (§V7-5).
	RotationBreakYears *int `json:"rotation_break_years"`

	SowMethod        *string `json:"sow_method"`
	NeedsPrickingOut *bool   `json:"needs_pricking_out"`
	// The three care flags below are what turn a crop into work: each one is the
	// sole reason its task kind is ever generated (§7 derivation table).
	NeedsSupport  *bool `json:"needs_support"`
	WantsMulch    *bool `json:"wants_mulch"`
	WantsPestCheck *bool `json:"wants_pest_check"`

	HardeningDays  *int     `json:"hardening_days"`
	SowDepthCM     *float64 `json:"sow_depth_cm"`
	SpacingRowCM   *float64 `json:"spacing_row_cm"`
	SpacingPlantCM *float64 `json:"spacing_plant_cm"`
	// PlantsPerM2 is derived from the two spacings when null, and overridable.
	PlantsPerM2 *float64 `json:"plants_per_m2"`

	DaysToGerminateMin *int `json:"days_to_germinate_min"`
	DaysToGerminateMax *int `json:"days_to_germinate_max"`
	GerminationTempC   *int `json:"germination_temp_c"`
	DaysToMaturityMin  *int `json:"days_to_maturity_min"`
	DaysToMaturityMax  *int `json:"days_to_maturity_max"`

	WinSowIndoor *Window `json:"win_sow_indoor"`
	WinSowDirect *Window `json:"win_sow_direct"`
	WinTransplant *Window `json:"win_transplant"`
	WinHarvest   *Window `json:"win_harvest"`

	HarvestUnit   *string  `json:"harvest_unit"`
	YieldPerM2    *float64 `json:"yield_per_m2"`
	YieldPerPlant *float64 `json:"yield_per_plant"`

	StorageMethods  []string `json:"storage_methods"`
	StorageTempC    *int     `json:"storage_temp_c"`
	StorageHumidity *int     `json:"storage_humidity"`
	ShelfLifeDays   *int     `json:"shelf_life_days"`

	Pests    []PestIssue `json:"pests"`
	Diseases []PestIssue `json:"diseases"`
	NotesMD  *string     `json:"notes_md"`
}

// Plant is a species (plodina) — what the household knows about a crop.
//
// Family and Hardiness are non-pointer and REQUIRED (D104, D111): the rotation
// engine joins on the first and the frost logic reads the second, so a crop
// missing either is one this module cannot reason about. It is refused at write
// time rather than defaulted to something plausible.
type Plant struct {
	PlantCore
	ID        string `json:"id"`
	NameCS    string `json:"name_cs"`
	Family    string `json:"family"`
	PlantType string `json:"plant_type"`
	Hardiness string `json:"hardiness"`

	VarietyCount int        `json:"variety_count"`
	Provenance   Provenance `json:"provenance"`
	CreatedBy    *string    `json:"created_by"`
	CreatedAt    string     `json:"created_at"`
	UpdatedAt    string     `json:"updated_at"`
}

// Variety (odrůda) carries SPARSE overrides: every inherited field is nil-able
// and nil means inherit from the species (D103).
type Variety struct {
	PlantCore
	ID            string  `json:"id"`
	PlantID       string  `json:"plant_id"`
	Name          string  `json:"name"`
	Supplier      *string `json:"supplier"`
	DescriptionMD *string `json:"description_md"`
	IsFavourite   bool    `json:"is_favourite"`
	Retired       bool    `json:"retired"`

	// Effective is the resolved record — variety value if non-null, else species
	// value — produced by the one shared Resolve() every consumer reads through.
	// Present on reads, never accepted on writes.
	Effective *Effective `json:"effective,omitempty"`

	Provenance Provenance `json:"provenance"`
	CreatedAt  string     `json:"created_at"`
	UpdatedAt  string     `json:"updated_at"`
}

// Effective is a species with every overridable field resolved against a
// variety. It is what the planner, the task generator, the check engine and the
// widget all read — never a Plant and a Variety side by side.
type Effective struct {
	PlantCore
	PlantID     string  `json:"plant_id"`
	PlantName   string  `json:"plant_name"`
	Family      string  `json:"family"`
	PlantType   string  `json:"plant_type"`
	Hardiness   string  `json:"hardiness"`
	VarietyID   *string `json:"variety_id"`
	VarietyName *string `json:"variety_name"`
}

// Bed (záhon). Order within a zone IS the adjacency model (D117) — there is no
// neighbour table, no coordinates and no drawing surface.
type Bed struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Code        string   `json:"code"`
	Type        string   `json:"type"`
	LengthCM    *float64 `json:"length_cm"`
	WidthCM     *float64 `json:"width_cm"`
	AreaM2      float64  `json:"area_m2"`
	SunExposure *string  `json:"sun_exposure"`
	Zone        *string  `json:"zone"`
	SoilNotesMD *string  `json:"soil_notes_md"`
	IsActive    bool     `json:"is_active"`
	Position    string   `json:"position"`
	// Neighbours are the beds immediately before and after this one in its zone
	// — derived from (zone, position), never stored.
	Neighbours []string `json:"neighbours,omitempty"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
}

// Season. Addressed by {year} in the API (D128), keyed by id in the store.
type Season struct {
	ID                 string  `json:"-"`
	Year               int     `json:"year"`
	Status             string  `json:"status"`
	LastFrostOn        *string `json:"last_frost_on"`
	FirstFrostOn       *string `json:"first_frost_on"`
	LastFrostActualOn  *string `json:"last_frost_actual_on"`
	FirstFrostActualOn *string `json:"first_frost_actual_on"`
	PlantingCount      int     `json:"planting_count"`
	ClosedAt           *string `json:"closed_at"`
	ClosedBy           *string `json:"closed_by"`
	NotesMD            *string `json:"notes_md"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

// Anchors turns a season's expected frost dates into what Window.Resolve needs.
// An unparseable or absent date yields a nil anchor, and a window that needs it
// then resolves to nothing — which is the honest answer, not an error.
func (s Season) Anchors() SeasonAnchors {
	return SeasonAnchors{Year: s.Year, LastFrost: parseDatePtr(s.LastFrostOn), FirstFrost: parseDatePtr(s.FirstFrostOn)}
}

// Occupancy is the derived window a planting holds a bed for (D107). Never
// stored — see occupancy.go for why this, and not the calendar year, is what
// "shares a bed" means.
type Occupancy struct {
	From *string `json:"from"`
	To   *string `json:"to"`
}

// Drift states how far reality has moved from the plan. Present because actual
// dates NEVER re-drive planned windows (D119): the drift is stated in words
// instead, and shift-tasks is the explicit way to act on it.
type Drift struct {
	Days      int    `json:"days"` // positive = later than planned
	Stage     string `json:"stage"`
	MessageCS string `json:"message_cs"`
}

// Planting (výsadba). SeasonID nil ⇒ a PERMANENT planting — a tree, a rhubarb
// crown, an asparagus bed (D106) — rather than a second table, so occupancy,
// warnings, tasks and harvests keep one code path.
type Planting struct {
	ID            string  `json:"id"`
	SeasonID      *string `json:"season_id"`
	SeasonYear    *int    `json:"season_year"`
	BedID         *string `json:"bed_id"`
	BedCode       *string `json:"bed_code"`
	LocationLabel *string `json:"location_label"`
	PlantID       string  `json:"plant_id"`
	PlantName     string  `json:"plant_name"`
	Family        string  `json:"family"`
	VarietyID     *string `json:"variety_id"`
	VarietyName   *string `json:"variety_name"`

	AreaM2     *float64 `json:"area_m2"`
	PlantCount *int     `json:"plant_count"`
	Rows       *int     `json:"rows"`

	// Planned dates.
	SowIndoorOn  *string `json:"sow_indoor_on"`
	SowDirectOn  *string `json:"sow_direct_on"`
	TransplantOn *string `json:"transplant_on"`
	HarvestFrom  *string `json:"harvest_from"`
	HarvestTo    *string `json:"harvest_to"`
	// ManualDates names the planned dates the user set by hand. A re-anchor skips
	// exactly these — which is what stops a frost-date change from overwriting a
	// deliberate decision.
	ManualDates []string `json:"manual_dates"`

	// Actual dates. Recording one changes NO planned window (D119).
	SowedOn        *string `json:"sowed_on"`
	TransplantedOn *string `json:"transplanted_on"`
	FirstHarvestOn *string `json:"first_harvest_on"`
	ClearedOn      *string `json:"cleared_on"`

	Status     string  `json:"status"`
	FailReason *string `json:"fail_reason"`

	// Permanents only.
	PlantedOn *string `json:"planted_on"`
	Rootstock *string `json:"rootstock"`
	RemovedOn *string `json:"removed_on"`

	Occupancy Occupancy `json:"occupancy"`
	Drift     *Drift    `json:"drift,omitempty"`
	// HarvestUnit is the crop's EFFECTIVE unit, carried beside the two yield
	// numbers because both are stated in it. Without it a screen showing a yield
	// is showing a bare number, and one asking for a yield is asking an
	// ambiguous question — which is how a count of onions became kilograms.
	HarvestUnit   *string  `json:"harvest_unit"`
	YieldActual   *float64 `json:"yield_actual"`
	YieldExpected *float64 `json:"yield_expected"`

	NotesMD   *string `json:"notes_md"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// IsPermanent reports whether this is a permanent planting (D106).
func (p Planting) IsPermanent() bool { return p.SeasonID == nil }

// Task (práce). Every generated task has an od–do WINDOW, not a due date,
// because "vysaď rajčata mezi 15. a 25. květnem" is the truth.
type Task struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	SeasonID    *string `json:"season_id"`
	PlantingID  *string `json:"planting_id"`
	BedID       *string `json:"bed_id"`
	BedCode     *string `json:"bed_code"`
	PlantName   *string `json:"plant_name"`
	VarietyName *string `json:"variety_name"`
	TitleCS     string  `json:"title_cs"`
	WindowFrom  string  `json:"window_from"`
	WindowTo    string  `json:"window_to"`
	DueHint     *string `json:"due_hint"`
	Overdue     bool    `json:"overdue"`
	Status      string  `json:"status"`
	CompletedBy *string `json:"completed_by"`
	CompletedAt *string `json:"completed_at"`
	IsGenerated bool    `json:"is_generated"`
	// IsEdited is set by any user edit. Once true, regeneration never touches
	// this task again (D110) — it is the user saying "I have decided this".
	IsEdited      bool    `json:"is_edited"`
	GenerationKey *string `json:"generation_key"`
	// Suppressed is the tombstone: a generated task the user deleted keeps its
	// row so the key stays taken and it cannot resurrect on the next recompute.
	Suppressed bool    `json:"-"`
	NotesMD    *string `json:"notes_md"`
	Position   string  `json:"position"`
}

// Harvest (sklizeň).
type Harvest struct {
	ID          string   `json:"id"`
	PlantingID  string   `json:"planting_id"`
	PlantName   string   `json:"plant_name,omitempty"`
	BedCode     *string  `json:"bed_code"`
	HarvestedOn string   `json:"harvested_on"`
	Quantity    float64  `json:"quantity"`
	Unit        string   `json:"unit"`
	Destination *string  `json:"destination"`
	Quality     *string  `json:"quality"`
	Note        *string  `json:"note"`
	CreatedBy   *string  `json:"created_by"`
	CreatedAt   string   `json:"created_at"`
}

// StorageItem (sklad). Garden produce only (D121) — not a general pantry.
// Consumption is recorded by editing QuantityRemaining in place; the audit
// spine's field diffs already answer "when did we eat the last jar", so there is
// no movements table.
type StorageItem struct {
	ID                string   `json:"id"`
	HarvestID         *string  `json:"harvest_id"`
	PlantingID        *string  `json:"planting_id"`
	ProductName       string   `json:"product_name"`
	Method            string   `json:"method"`
	Location          *string  `json:"location"`
	QuantityInitial   float64  `json:"quantity_initial"`
	QuantityRemaining float64  `json:"quantity_remaining"`
	Unit              string   `json:"unit"`
	StoredOn          string   `json:"stored_on"`
	BestBefore        *string  `json:"best_before"`
	Status            string   `json:"status"`
	Note              *string  `json:"note"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
}

// Rule is one compatibility or succession rule. Pairs are stored in CANONICAL
// (sorted) order so symmetry is structural: the matcher looks up one row, not
// two, and nobody has to remember to enter both directions.
type Rule struct {
	ID          string  `json:"id"`
	Scope       string  `json:"scope"`
	ARef        string  `json:"a_ref"`
	BRef        string  `json:"b_ref"`
	ALabel      string  `json:"a_label,omitempty"`
	BLabel      string  `json:"b_label,omitempty"`
	Verdict     string  `json:"verdict"`
	Severity    string  `json:"severity"`
	MinYearsGap *int    `json:"min_years_gap"`
	ReasonCS    *string `json:"reason_cs"`
	// Source is where the claim comes from. Companion-planting literature
	// contradicts itself; this column is how agronomy and folklore stay tellable
	// apart, and it is why a built-in can be disabled but never deleted (D130).
	Source     *string `json:"source"`
	IsBuiltin  bool    `json:"is_builtin"`
	IsDisabled bool    `json:"is_disabled"`
}

// CheckConfig is the per-check enable flag and severity override, keyed C1…C11.
type CheckConfig struct {
	Enabled  *bool   `json:"enabled"`
	Severity *string `json:"severity"`
}

// ChecksConfig is the whole map, as stored in garden_settings.checks_config.
type ChecksConfig map[string]CheckConfig

// enabled reports whether check c should run. Absent = enabled: a settings row
// written before a check existed must not silence it.
func (c ChecksConfig) enabled(check string) bool {
	if cfg, ok := c[check]; ok && cfg.Enabled != nil {
		return *cfg.Enabled
	}
	return true
}

// severity applies a configured override, falling back to the check's own
// default.
func (c ChecksConfig) severity(check, def string) string {
	if cfg, ok := c[check]; ok && cfg.Severity != nil && Valid(EnumSeverity, *cfg.Severity) {
		return *cfg.Severity
	}
	return def
}

// Settings is the module's single settings row. Coordinates live HERE and not in
// Coolify (D112): they are user data rather than secrets, and a typo should not
// need a redeploy. There are no audience or notification settings — that is
// Administrace's (D113).
type Settings struct {
	DefaultLastFrost  *string  `json:"default_last_frost"` // MM-DD
	DefaultFirstFrost *string  `json:"default_first_frost"`
	Latitude          *float64 `json:"latitude"`
	Longitude         *float64 `json:"longitude"`
	AltitudeM         *int     `json:"altitude_m"`

	RotationBreakDefault  int          `json:"rotation_break_default"`
	FrostThresholdC       float64      `json:"frost_threshold_c"`
	FrostLookaheadDays    int          `json:"frost_lookahead_days"`
	WorkloadWeekThreshold int          `json:"workload_week_threshold"`
	Checks                ChecksConfig `json:"checks"`

	// WeatherEnabled reflects HOME_GARDEN_WEATHER_ENABLED. Read-only here.
	WeatherEnabled bool   `json:"weather_enabled"`
	UpdatedAt      string `json:"updated_at"`
}

// WeatherDay is one cached forecast day.
type WeatherDay struct {
	Day       string   `json:"day"`
	TempMin   *float64 `json:"temp_min"`
	TempMax   *float64 `json:"temp_max"`
	PrecipMM  *float64 `json:"precip_mm"`
	FetchedAt *string  `json:"fetched_at"`
	Source    *string  `json:"source"`
}

// Warning is one check result. Computed on read, never stored (D108), and
// ADVISORY — it never blocks a save (D109).
type Warning struct {
	// Key is stable across recomputation: hash(check, rule, bed, sorted
	// plantings, year). Change the plan enough and the warning correctly returns.
	Key           string   `json:"key"`
	Check         string   `json:"check"`
	Severity      string   `json:"severity"`
	TitleCS       string   `json:"title_cs"`
	DetailCS      string   `json:"detail_cs,omitempty"`
	BedID         *string  `json:"bed_id"`
	PlantingIDs   []string `json:"planting_ids,omitempty"`
	RuleID        *string  `json:"rule_id"`
	Dismissed     bool     `json:"dismissed"`
	DismissedNote *string  `json:"dismissed_note"`
}

// SeverityCounts is the non-dismissed count per severity.
type SeverityCounts struct {
	Error int `json:"error"`
	Warn  int `json:"warn"`
	Info  int `json:"info"`
	Tip   int `json:"tip"`
}

// History status values.
const (
	HistoryOK        = "ok"
	HistoryNoHistory = "no_history"
)

// HistoryState says whether the history-dependent checks could run at all.
type HistoryState struct {
	Status        string `json:"status"`
	ClosedSeasons int    `json:"closed_seasons"`
}

// CheckResult is the whole Kontrola plánu response.
type CheckResult struct {
	Year     int            `json:"year"`
	Warnings []Warning      `json:"warnings"`
	Counts   SeverityCounts `json:"counts"`
	History  HistoryState   `json:"history"`
}

// Dismissal is one silenced warning, scoped to a season.
type Dismissal struct {
	Key          string  `json:"key"`
	Note         *string `json:"note"`
	DismissedBy  *string `json:"dismissed_by"`
	DismissedAt  string  `json:"dismissed_at"`
}

// BedSeasonHistory is what occupied one bed in one CLOSED season.
type BedSeasonHistory struct {
	Year      int                  `json:"year"`
	Families  []string             `json:"families"`
	Plantings []BedHistoryPlanting `json:"plantings,omitempty"`
}

// BedHistoryPlanting is one row of that history.
type BedHistoryPlanting struct {
	PlantName   string   `json:"plant_name"`
	VarietyName *string  `json:"variety_name"`
	YieldKg     *float64 `json:"yield_kg"`
	// FeederClass is not on the wire — check C8 reads it off the same rows.
	FeederClass string `json:"-"`
	Family      string `json:"-"`
}

// BedHistory is the per-bed rotation record the planner's history column and
// checks C3/C8 read. CLOSED seasons only (D120) — an open season is not history.
type BedHistory struct {
	BedID   string             `json:"bed_id"`
	Seasons []BedSeasonHistory `json:"seasons"`
}

// Snapshot is everything the check engine needs, loaded in one store call. The
// engine takes this struct and touches no database (see check.go).
type Snapshot struct {
	Season    Season
	Today     dates.Date
	Beds      []Bed
	Plantings []Planting
	// Effective is the resolved crop record per planting id — resolved ONCE at
	// load, so no check re-derives it and none of them can disagree.
	Effective map[string]Effective
	Rules     []Rule
	// History is the closed-season record per bed id.
	History map[string][]BedSeasonHistory
	// ClosedSeasons is how many closed seasons exist at all. Zero means C3 and C8
	// cannot run — see HistoryNoHistory.
	ClosedSeasons int
	Dismissals    map[string]Dismissal
	Settings      Settings
	// Tasks are the season's open+done tasks, for the workload check C5.
	Tasks []Task
}

// ---- small shared helpers ----

func parseDatePtr(s *string) *dates.Date {
	if s == nil || *s == "" {
		return nil
	}
	d, err := dates.Parse(*s)
	if err != nil {
		return nil
	}
	return &d
}

func sp(s string) *string { return &s }
func ip(v int) *int       { return &v }
func fp(v float64) *float64 { return &v }
func bp(v bool) *bool     { return &v }

func derefS(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefB(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

func derefI(v *int, def int) int {
	if v == nil {
		return def
	}
	return *v
}
