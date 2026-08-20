// Zahrada / garden (v7) — the module's API surface (openapi.yaml 0.9.0).
//
// 34 paths behind one nav destination. Two conventions are worth knowing before
// reading the rest:
//
//   - SEASONS ARE ADDRESSED BY {year} (D128), not by a surrogate id, so
//     /zahrada/plan/2027 maps 1:1 onto /api/garden/seasons/2027.
//   - `dry_run` is a shared preview idiom (D129) with DELIBERATELY DIFFERENT
//     DEFAULTS: true on the import (which must never apply by accident) and
//     false on season creation (which should not need a second call in the
//     common case).

import { apiFetch } from '@/api/client'
import type {
  GardenBed,
  GardenBedHistory,
  GardenBedInput,
  GardenCheckResult,
  GardenEnums,
  GardenHarvest,
  GardenHarvestInput,
  GardenImportResult,
  GardenPage,
  GardenPlant,
  GardenPlantInput,
  GardenPlanting,
  GardenPlantingInput,
  GardenPromptTemplate,
  GardenRule,
  GardenRuleInput,
  GardenSeason,
  GardenSeasonCloseInput,
  GardenSeasonInput,
  GardenSeasonPreview,
  GardenSettings,
  GardenSettingsInput,
  GardenStorageInput,
  GardenStorageItem,
  GardenTask,
  GardenTaskInput,
  GardenVariety,
  GardenVarietyInput,
  GardenWeatherPage,
} from './types'

const base = '/api/garden'

// qs takes a plain object rather than a Record so the typed filter interfaces
// below can be passed straight in — an index signature on each of them would be
// three declarations serving one helper.
function qs(params: object): string {
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params) as [string, unknown][]) {
    if (v === undefined || v === null || v === '') continue
    sp.append(k, String(v))
  }
  const s = sp.toString()
  return s ? `?${s}` : ''
}

// ---- knowledge base ----

export interface PlantFilters {
  q?: string
  family?: string
  plant_type?: string
  unverified?: boolean
  limit?: number
  cursor?: string
}

export const listPlants = (f: PlantFilters = {}) =>
  apiFetch<GardenPage<GardenPlant>>(`${base}/plants${qs(f)}`)
export const getPlant = (id: string) => apiFetch<GardenPlant>(`${base}/plants/${id}`)
export const createPlant = (body: GardenPlantInput) =>
  apiFetch<GardenPlant>(`${base}/plants`, { method: 'POST', body })
export const updatePlant = (id: string, body: GardenPlantInput) =>
  apiFetch<GardenPlant>(`${base}/plants/${id}`, { method: 'PATCH', body })
export const deletePlant = (id: string) => apiFetch<void>(`${base}/plants/${id}`, { method: 'DELETE' })

// Exhaustive: PlantForm and CropPicker render the whole set with no paging UI,
// so a page boundary here would make the 51st variety look deleted — and its
// overrides silently stop being visible or editable.
export const listVarieties = async (plantId: string): Promise<GardenPage<GardenVariety>> => {
  const items: GardenVariety[] = []
  let cursor: string | undefined
  do {
    const page = await apiFetch<GardenPage<GardenVariety>>(
      `${base}/plants/${plantId}/varieties${qs({ limit: 200, cursor })}`,
    )
    items.push(...page.items)
    cursor = page.next_cursor ?? undefined
  } while (cursor)
  return { items, next_cursor: null }
}
export const createVariety = (plantId: string, body: GardenVarietyInput) =>
  apiFetch<GardenVariety>(`${base}/plants/${plantId}/varieties`, { method: 'POST', body })
export const updateVariety = (id: string, body: GardenVarietyInput) =>
  apiFetch<GardenVariety>(`${base}/varieties/${id}`, { method: 'PATCH', body })
export const deleteVariety = (id: string) =>
  apiFetch<void>(`${base}/varieties/${id}`, { method: 'DELETE' })

// ---- the LLM escape hatch ----

/** The ready-to-paste Czech prompt plus the schema generated from the importer's
 *  own validator. With plantId it embeds the crop's current values, so the model
 *  fills gaps instead of overwriting what somebody already checked. */
export const promptTemplate = (plantId?: string) =>
  apiFetch<GardenPromptTemplate>(`${base}/plants/prompt-template${qs({ plant_id: plantId })}`)

/** dryRun DEFAULTS TRUE: the preview is what makes pasting machine output feel
 *  safe rather than reckless, and an import must never apply by accident. */
export const importPlants = (payload: unknown, opts: { dryRun?: boolean; model?: string } = {}) =>
  apiFetch<GardenImportResult>(
    `${base}/plants/import${qs({ dry_run: opts.dryRun !== false, model: opts.model })}`,
    { method: 'POST', body: payload },
  )

export const exportKnowledgeBase = () => apiFetch<Record<string, unknown>>(`${base}/export`)

// ---- beds ----

export const listBeds = (includeInactive = false) =>
  apiFetch<{ items: GardenBed[] }>(`${base}/beds${qs({ include_inactive: includeInactive || undefined })}`)
export const createBed = (body: GardenBedInput) =>
  apiFetch<GardenBed>(`${base}/beds`, { method: 'POST', body })
export const updateBed = (id: string, body: GardenBedInput) =>
  apiFetch<GardenBed>(`${base}/beds/${id}`, { method: 'PATCH', body })
export const deleteBed = (id: string) => apiFetch<void>(`${base}/beds/${id}`, { method: 'DELETE' })

/** Reordering a bed CHANGES THE WARNINGS (D117): consecutive beds in one zone
 *  are neighbours, and check C11 reads exactly that. */
export const moveBed = (id: string, body: { zone?: string | null; after_id?: string; before_id?: string }) =>
  apiFetch<GardenBed>(`${base}/beds/${id}/move`, { method: 'POST', body })

export const bedHistory = (id: string) => apiFetch<GardenBedHistory>(`${base}/beds/${id}/history`)

// ---- seasons ----

export const listSeasons = () => apiFetch<{ items: GardenSeason[] }>(`${base}/seasons`)
export const getSeason = (year: number) => apiFetch<GardenSeason>(`${base}/seasons/${year}`)

/** dryRun returns the whole prospective season plus its check WITHOUT
 *  persisting, alongside the source season's check — so the planner can show
 *  what a rotation shift fixed and what it broke, side by side, before the
 *  season exists. */
export const createSeason = (body: GardenSeasonInput, dryRun = false) =>
  apiFetch<GardenSeason | GardenSeasonPreview>(`${base}/seasons${qs({ dry_run: dryRun || undefined })}`, {
    method: 'POST',
    body,
  })

export const updateSeason = (
  year: number,
  body: { status?: string; last_frost_on?: string | null; first_frost_on?: string | null; notes_md?: string | null },
) => apiFetch<GardenSeason>(`${base}/seasons/${year}`, { method: 'PATCH', body })

export const closeSeason = (year: number, body: GardenSeasonCloseInput) =>
  apiFetch<GardenSeason>(`${base}/seasons/${year}/close`, { method: 'POST', body })

/** The module's ONLY admin-gated action: re-opening rewrites the rotation
 *  history the checks depend on. */
export const reopenSeason = (year: number) =>
  apiFetch<GardenSeason>(`${base}/seasons/${year}/reopen`, { method: 'POST' })

// ---- kontrola plánu ----

export const checkSeason = (year: number) => apiFetch<GardenCheckResult>(`${base}/seasons/${year}/check`)

/** "Ignorovat" carries a note and stays findable — it is a first-class action,
 *  not a close button. The response is the recomputed check. */
export const dismissWarning = (year: number, key: string, note?: string) =>
  apiFetch<GardenCheckResult>(`${base}/seasons/${year}/check/dismissals`, {
    method: 'POST',
    body: { key, note },
  })

export const restoreWarning = (year: number, key: string) =>
  apiFetch<void>(`${base}/seasons/${year}/check/dismissals/${encodeURIComponent(key)}`, { method: 'DELETE' })

// ---- plantings ----

export interface PlantingFilters {
  year?: number
  bed_id?: string
  plant_id?: string
  status?: string
  permanent?: boolean
  limit?: number
  cursor?: string
}

export const listPlantings = (f: PlantingFilters = {}) =>
  apiFetch<GardenPage<GardenPlanting>>(`${base}/plantings${qs(f)}`)
export const getPlanting = (id: string) => apiFetch<GardenPlanting>(`${base}/plantings/${id}`)
export const createPlanting = (body: GardenPlantingInput) =>
  apiFetch<GardenPlanting>(`${base}/plantings`, { method: 'POST', body })
export const updatePlanting = (id: string, body: GardenPlantingInput) =>
  apiFetch<GardenPlanting>(`${base}/plantings/${id}`, { method: 'PATCH', body })
export const deletePlanting = (id: string) =>
  apiFetch<void>(`${base}/plantings/${id}`, { method: 'DELETE' })

/** Moves the planting's remaining OPEN work and marks it edited — after which
 *  regeneration leaves it alone permanently (D110). The one action offered
 *  against the drift line. */
export const shiftTasks = (id: string, days: number, kinds?: string[]) =>
  apiFetch<{ items: GardenTask[] }>(`${base}/plantings/${id}/shift-tasks`, {
    method: 'POST',
    body: { days, kinds },
  })

// ---- work calendar ----

export interface TaskFilters {
  from?: string
  to?: string
  /** OVERDUE bound: matches only work whose WHOLE window has passed
   *  (window_to <= to_end). `from`/`to` are an overlap and therefore exclude
   *  exactly the rows this returns, so missed work needs its own query. */
  to_end?: string
  status?: string
  kind?: string
  bed_id?: string
  planting_id?: string
  year?: number
  limit?: number
  cursor?: string
}

export const listTasks = (f: TaskFilters = {}) => apiFetch<GardenPage<GardenTask>>(`${base}/tasks${qs(f)}`)
export const createTask = (body: GardenTaskInput) =>
  apiFetch<GardenTask>(`${base}/tasks`, { method: 'POST', body })
export const updateTask = (id: string, body: GardenTaskInput) =>
  apiFetch<GardenTask>(`${base}/tasks/${id}`, { method: 'PATCH', body })
export const deleteTask = (id: string) => apiFetch<void>(`${base}/tasks/${id}`, { method: 'DELETE' })

/** IDEMPOTENT (D131): the 2000 ms hold can fire twice on a bad connection, and
 *  the second send must not look like an error. */
export const completeTask = (id: string, meta?: Record<string, unknown>) =>
  apiFetch<GardenTask>(`${base}/tasks/${id}/complete`, { method: 'POST', body: { meta } })

export const reopenTask = (id: string) =>
  apiFetch<GardenTask>(`${base}/tasks/${id}/reopen`, { method: 'POST' })

// ---- harvest and storage ----

// Exhaustive, exactly as listVarieties is: both callers SUM these rows into a
// headline figure, and the server's own garden.harvest_season metric sums every
// row in SQL. A page boundary here would make the Přehled tile and the Sklizeň
// header report less than a summary rendering the same season — with nothing on
// screen saying the list was cut. A season picked daily from June to October
// passes 200 rows without being remarkable.
export const listHarvests = async (
  f: { planting_id?: string; year?: number } = {},
): Promise<GardenPage<GardenHarvest>> => {
  const items: GardenHarvest[] = []
  let cursor: string | undefined
  do {
    const page = await apiFetch<GardenPage<GardenHarvest>>(
      `${base}/harvests${qs({ ...f, limit: 200, cursor })}`,
    )
    items.push(...page.items)
    cursor = page.next_cursor ?? undefined
  } while (cursor)
  return { items, next_cursor: null }
}
export const createHarvest = (body: GardenHarvestInput) =>
  apiFetch<GardenHarvest>(`${base}/harvests`, { method: 'POST', body })
export const updateHarvest = (id: string, body: GardenHarvestInput) =>
  apiFetch<GardenHarvest>(`${base}/harvests/${id}`, { method: 'PATCH', body })
export const deleteHarvest = (id: string) =>
  apiFetch<void>(`${base}/harvests/${id}`, { method: 'DELETE' })

export const listStorage = (f: { status?: string; limit?: number } = {}) =>
  apiFetch<GardenPage<GardenStorageItem>>(`${base}/storage${qs(f)}`)
export const createStorage = (body: GardenStorageInput) =>
  apiFetch<GardenStorageItem>(`${base}/storage`, { method: 'POST', body })
export const updateStorage = (id: string, body: GardenStorageInput) =>
  apiFetch<GardenStorageItem>(`${base}/storage/${id}`, { method: 'PATCH', body })
export const deleteStorage = (id: string) =>
  apiFetch<void>(`${base}/storage/${id}`, { method: 'DELETE' })

// ---- rules, settings, reference ----

export const listRules = (f: { scope?: string; limit?: number } = {}) =>
  apiFetch<GardenPage<GardenRule>>(`${base}/rules${qs(f)}`)
export const createRule = (body: GardenRuleInput) =>
  apiFetch<GardenRule>(`${base}/rules`, { method: 'POST', body })
export const updateRule = (id: string, body: GardenRuleInput) =>
  apiFetch<GardenRule>(`${base}/rules/${id}`, { method: 'PATCH', body })
/** A built-in rule is a 409 here — it can be disabled, never deleted (D130). */
export const deleteRule = (id: string) => apiFetch<void>(`${base}/rules/${id}`, { method: 'DELETE' })

export const getSettings = () => apiFetch<GardenSettings>(`${base}/settings`)
export const updateSettings = (body: GardenSettingsInput) =>
  apiFetch<GardenSettings>(`${base}/settings`, { method: 'PATCH', body })

export const getWeather = (f: { from?: string; to?: string } = {}) =>
  apiFetch<GardenWeatherPage>(`${base}/weather${qs(f)}`)

/** The SAME registry that generates the importer's validator and the LLM prompt
 *  schema, so the pickers, the model and the validator cannot disagree about
 *  what a legal value is. */
export const getEnums = () => apiFetch<GardenEnums>(`${base}/enums`)
