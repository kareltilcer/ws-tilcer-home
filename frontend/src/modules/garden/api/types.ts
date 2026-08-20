// Zahrada / garden (v7) — the module's wire types (openapi.yaml 0.9.0, tag
// `garden`).
//
// They live HERE rather than in the shared src/api/types.ts because the module
// is self-contained: eleven entities is more than the shared file should carry
// for one destination, and nothing outside src/modules/garden/ needs them except
// the widget, which imports this file by name.
//
// Reads are open to every member, `reader` included; writes take the ordinary
// editor/admin gate server-side, and ticking a task off is an ordinary write
// (D124) — a reader sees the control disabled, never hidden.

export type GardenAnchor = 'week' | 'last_frost' | 'first_frost'

/** A knowledge-base timing window: WHEN relative to something, not which day.
 *  `week` is an ISO week (1–53); the frost anchors are day offsets, negative
 *  meaning before the frost date (D102). */
export interface GardenWindow {
  anchor: GardenAnchor
  from: number
  to: number
}

export interface GardenPestIssue {
  name: string
  symptom?: string
  remedy_md?: string
}

/** Where a record came from. An LLM-filled crop nobody has checked must LOOK
 *  different from one a human confirmed — the "Neověřeno" badge is derived from
 *  verified_at being null (D114). */
export interface GardenProvenance {
  source: 'manual' | 'llm'
  source_model: string | null
  source_at: string | null
  verified_by: string | null
  verified_at: string | null
}

/** Every overridable knowledge-base field. A species states them; a variety
 *  overrides only what differs, and null means inherit (D103). */
export interface GardenPlantCore {
  name_latin?: string | null
  feeder_class?: string | null
  root_depth?: string | null
  sun?: string | null
  water_need?: string | null
  soil_ph_min?: number | null
  soil_ph_max?: number | null
  rotation_break_years?: number | null
  sow_method?: string | null
  needs_pricking_out?: boolean | null
  needs_support?: boolean | null
  wants_mulch?: boolean | null
  wants_pest_check?: boolean | null
  hardening_days?: number | null
  sow_depth_cm?: number | null
  spacing_row_cm?: number | null
  spacing_plant_cm?: number | null
  plants_per_m2?: number | null
  days_to_germinate_min?: number | null
  days_to_germinate_max?: number | null
  germination_temp_c?: number | null
  days_to_maturity_min?: number | null
  days_to_maturity_max?: number | null
  win_sow_indoor?: GardenWindow | null
  win_sow_direct?: GardenWindow | null
  win_transplant?: GardenWindow | null
  win_harvest?: GardenWindow | null
  harvest_unit?: string | null
  yield_per_m2?: number | null
  yield_per_plant?: number | null
  storage_methods?: string[] | null
  storage_temp_c?: number | null
  storage_humidity?: number | null
  shelf_life_days?: number | null
  pests?: GardenPestIssue[] | null
  diseases?: GardenPestIssue[] | null
  notes_md?: string | null
}

export interface GardenPlant extends GardenPlantCore {
  id: string
  name_cs: string
  family: string
  plant_type: string
  hardiness: string
  variety_count: number
  provenance: GardenProvenance
  created_at: string
  updated_at: string
}

export interface GardenEffective extends GardenPlantCore {
  plant_id: string
  plant_name: string
  family: string
  plant_type: string
  hardiness: string
  variety_id: string | null
  variety_name: string | null
}

export interface GardenVariety extends GardenPlantCore {
  id: string
  plant_id: string
  name: string
  supplier: string | null
  description_md: string | null
  is_favourite: boolean
  retired: boolean
  /** The resolved record — variety value if set, else the species value. */
  effective?: GardenEffective
  provenance: GardenProvenance
}

export interface GardenBed {
  id: string
  name: string
  code: string
  type: string
  length_cm: number | null
  width_cm: number | null
  area_m2: number
  sun_exposure: string | null
  zone: string | null
  soil_notes_md: string | null
  is_active: boolean
  position: string
  /** Derived from (zone, position) — ORDER IS THE ADJACENCY MODEL (D117). */
  neighbours?: string[]
}

export interface GardenBedHistorySeason {
  year: number
  families: string[]
  plantings?: { plant_name: string; variety_name: string | null; yield_kg: number | null }[]
}

export interface GardenBedHistory {
  bed_id: string
  seasons: GardenBedHistorySeason[]
}

export type GardenSeasonStatus = 'planning' | 'active' | 'closed'

export interface GardenSeason {
  year: number
  status: GardenSeasonStatus
  last_frost_on: string | null
  first_frost_on: string | null
  last_frost_actual_on: string | null
  first_frost_actual_on: string | null
  planting_count: number
  closed_at: string | null
  closed_by: string | null
  notes_md: string | null
}

/** Derived, never stored: what "shares a bed" means everywhere in the module
 *  (D107). Spring špenát and autumn pórek in one bed never overlap. */
export interface GardenOccupancy {
  from: string | null
  to: string | null
}

/** How far reality has moved from the plan. Present because actual dates never
 *  re-drive planned windows (D119) — the drift is stated in words instead, and
 *  "posunout navazující práce" is the one action offered against it. */
export interface GardenDrift {
  days: number
  stage: 'sow' | 'transplant' | 'harvest'
  message_cs: string
}

export interface GardenPlanting {
  id: string
  season_id: string | null
  season_year: number | null
  bed_id: string | null
  bed_code: string | null
  location_label: string | null
  plant_id: string
  plant_name: string
  family: string
  variety_id: string | null
  variety_name: string | null
  area_m2: number | null
  plant_count: number | null
  rows: number | null
  sow_indoor_on: string | null
  sow_direct_on: string | null
  transplant_on: string | null
  harvest_from: string | null
  harvest_to: string | null
  manual_dates: string[] | null
  sowed_on: string | null
  transplanted_on: string | null
  first_harvest_on: string | null
  cleared_on: string | null
  status: string
  fail_reason: string | null
  planted_on: string | null
  rootstock: string | null
  removed_on: string | null
  occupancy: GardenOccupancy
  drift?: GardenDrift
  /** The crop's EFFECTIVE harvest unit. Both yield numbers are stated in it, and
   *  so is anything the household is asked to type into them. */
  harvest_unit: string | null
  yield_actual: number | null
  yield_expected: number | null
  notes_md: string | null
}

export type GardenTaskStatus = 'open' | 'done' | 'skipped'

export interface GardenTask {
  id: string
  kind: string
  season_id: string | null
  planting_id: string | null
  bed_id: string | null
  bed_code: string | null
  plant_name: string | null
  variety_name: string | null
  title_cs: string
  /** Work is a WINDOW, not a due date — "vysaď rajčata mezi 15. a 25. května". */
  window_from: string
  window_to: string
  due_hint: string | null
  overdue: boolean
  status: GardenTaskStatus
  completed_by: string | null
  completed_at: string | null
  is_generated: boolean
  /** Once true, regeneration never touches this task again (D110). */
  is_edited: boolean
  generation_key: string | null
  notes_md: string | null
  position: string
}

export type GardenSeverity = 'error' | 'warn' | 'info' | 'tip'

export interface GardenWarning {
  key: string
  check: string
  severity: GardenSeverity
  title_cs: string
  detail_cs?: string
  bed_id: string | null
  planting_ids?: string[]
  rule_id: string | null
  dismissed: boolean
  dismissed_note: string | null
}

export interface GardenCheckResult {
  year: number
  warnings: GardenWarning[]
  counts: { error: number; warn: number; info: number; tip: number }
  /** `no_history` means C3/C8 COULD NOT RUN — never that they passed (D120). */
  history: { status: 'ok' | 'no_history'; closed_seasons: number }
}

export interface GardenSeasonPreview {
  season: GardenSeason
  plantings: GardenPlanting[]
  check: GardenCheckResult
  check_before?: GardenCheckResult
}

export interface GardenHarvest {
  id: string
  planting_id: string
  plant_name?: string
  bed_code: string | null
  harvested_on: string
  quantity: number
  unit: string
  destination: string | null
  quality: string | null
  note: string | null
}

export interface GardenStorageItem {
  id: string
  harvest_id: string | null
  planting_id: string | null
  product_name: string
  method: string
  location: string | null
  quantity_initial: number
  quantity_remaining: number
  unit: string
  stored_on: string
  best_before: string | null
  status: 'stored' | 'consumed' | 'spoiled'
  note: string | null
}

export interface GardenRule {
  id: string
  scope: 'plant_pair' | 'family_pair' | 'succession'
  a_ref: string
  b_ref: string
  a_label?: string
  b_label?: string
  verdict: 'good' | 'bad'
  severity: GardenSeverity
  min_years_gap: number | null
  reason_cs: string | null
  /** How agronomy and folklore stay tellable apart (D130). */
  source: string | null
  is_builtin: boolean
  is_disabled: boolean
}

export interface GardenCheckConfigEntry {
  enabled?: boolean
  severity?: GardenSeverity
}

export interface GardenSettings {
  default_last_frost: string | null
  default_first_frost: string | null
  latitude: number | null
  longitude: number | null
  altitude_m: number | null
  rotation_break_default: number
  frost_threshold_c: number
  frost_lookahead_days: number
  workload_week_threshold: number
  checks: Record<string, GardenCheckConfigEntry>
  weather_enabled: boolean
}

export interface GardenWeatherDay {
  day: string
  temp_min: number | null
  temp_max: number | null
  precip_mm: number | null
  fetched_at: string | null
  source: string | null
}

export interface GardenWeatherPage {
  items: GardenWeatherDay[]
  stale: boolean
}

export interface GardenEnumValue {
  value: string
  label_cs: string
  note_cs: string | null
}

export type GardenEnums = Record<string, GardenEnumValue[]>

export interface GardenPromptTemplate {
  prompt: string
  schema: Record<string, unknown>
  context: {
    last_frost_on: string | null
    first_frost_on: string | null
    latitude: number | null
    longitude: number | null
    altitude_m: number | null
  }
}

export interface GardenImportItemResult {
  index: number
  action: 'create' | 'update' | 'skip' | 'reject'
  ok: boolean
  target_id: string | null
  name: string | null
  diff?: Record<string, { old: string | null; new: string | null }>
  /** Input fields the importer did not recognise. Reported, never dropped. */
  unmapped_fields?: string[]
  errors?: string[]
}

export interface GardenImportResult {
  dry_run: boolean
  items: GardenImportItemResult[]
  summary: { created: number; updated: number; rejected: number }
  source_model: string | null
}

/** The garden.prace widget payload (D123). There is only one widget — harvest
 *  surfaces here as a `harvest` task rather than as a card that is dead weight
 *  from November to May. */
export interface GardenPraceWidget {
  state: 'work' | 'quiet'
  overdue?: GardenPraceItem[]
  weeks?: { week: string; from: string; items: GardenPraceItem[] }[]
  total: number
}

export interface GardenPraceItem {
  id: string
  title_cs: string
  kind: string
  bed_code: string | null
  plant_name: string | null
  window_from: string
  window_to: string
  window_cs: string
  overdue: boolean
}

export interface GardenPage<T> {
  items: T[]
  next_cursor?: string | null
}

// ---- request bodies ----

export type GardenPlantInput = GardenPlantCore & {
  name_cs?: string
  family?: string
  plant_type?: string
  hardiness?: string
  /** Set true to clear the "Neověřeno" badge; stamps who and when. */
  verified?: boolean
}

export type GardenVarietyInput = GardenPlantCore & {
  name?: string
  supplier?: string | null
  description_md?: string | null
  is_favourite?: boolean
  retired?: boolean
  verified?: boolean
}

export interface GardenBedInput {
  name?: string
  code?: string
  type?: string
  length_cm?: number | null
  width_cm?: number | null
  area_m2?: number | null
  sun_exposure?: string | null
  zone?: string | null
  soil_notes_md?: string | null
  is_active?: boolean
}

export interface GardenSeasonInput {
  year: number
  last_frost_on?: string
  first_frost_on?: string
  notes_md?: string
  copy_from?: number
  shift?: { offset: number; by_family?: boolean }
}

export interface GardenPlantingInput {
  season_year?: number | null
  season_id?: string | null
  bed_id?: string | null
  location_label?: string | null
  plant_id?: string
  variety_id?: string | null
  area_m2?: number | null
  plant_count?: number | null
  rows?: number | null
  sow_indoor_on?: string | null
  sow_direct_on?: string | null
  transplant_on?: string | null
  harvest_from?: string | null
  harvest_to?: string | null
  sowed_on?: string | null
  transplanted_on?: string | null
  first_harvest_on?: string | null
  cleared_on?: string | null
  status?: string
  fail_reason?: string | null
  planted_on?: string | null
  rootstock?: string | null
  removed_on?: string | null
  notes_md?: string | null
}

export interface GardenTaskInput {
  kind?: string
  title_cs?: string
  window_from?: string
  window_to?: string
  due_hint?: string | null
  status?: GardenTaskStatus
  season_year?: number
  planting_id?: string
  bed_id?: string
  notes_md?: string | null
}

export interface GardenHarvestInput {
  planting_id?: string
  harvested_on?: string
  quantity?: number
  unit?: string
  destination?: string | null
  quality?: string | null
  note?: string | null
}

export interface GardenStorageInput {
  harvest_id?: string | null
  planting_id?: string | null
  product_name?: string
  method?: string
  location?: string | null
  quantity_initial?: number
  quantity_remaining?: number
  unit?: string
  stored_on?: string
  best_before?: string | null
  status?: 'stored' | 'consumed' | 'spoiled'
  note?: string | null
}

export interface GardenRuleInput {
  scope?: string
  a_ref?: string
  b_ref?: string
  verdict?: 'good' | 'bad'
  severity?: GardenSeverity
  min_years_gap?: number | null
  reason_cs?: string | null
  source?: string | null
  is_disabled?: boolean
}

export interface GardenSettingsInput {
  default_last_frost?: string | null
  default_first_frost?: string | null
  latitude?: number | null
  longitude?: number | null
  altitude_m?: number | null
  rotation_break_default?: number
  frost_threshold_c?: number
  frost_lookahead_days?: number
  workload_week_threshold?: number
  checks?: Record<string, GardenCheckConfigEntry>
}

export interface GardenSeasonCloseInput {
  last_frost_actual_on?: string
  first_frost_actual_on?: string
  notes_md?: string
  outcomes?: {
    planting_id: string
    status: 'done' | 'failed'
    fail_reason?: string
    final_yield?: number
    final_yield_unit?: string
  }[]
}
