# Home v7 — **Zahrada** (`garden`), design brief

> Status: **Resolved — scope frozen 2026-08-18** · Owner: Karel
> All open questions answered (OQ-V7-1…16). This now folds into `PRD.md` as §V7-1 … §V7-11,
> `openapi.yaml` → **0.9.0**, and `HANDOFF-9-garden.md`.
> Decisions **D101–D127**. Migration block **10**. Ninth module.

---

## 0. Scope, frozen

**In:**

1. Crop knowledge base — **druh → odrůda**, two levels, structured + free-form, with an LLM prompt template, JSON import and JSON export.
2. Beds (**záhony**) and per-season planting plan.
3. Compatibility + rotation warnings — per bed and garden-wide.
4. A garden-owned task engine with od–do windows, generated from the plan; **one tickable Nástěnka widget**.
5. Harvest / yield log (**sklizeň**).
6. Frost dates as the calendar anchor + a forecast feed, **delivered through Administrace's existing notification machinery** — see §5.
7. Perennials and fruit trees (**trvalky a dřeviny**), same table as annuals.
8. **"Uzavřít sezónu"** ritual, which is what builds rotation history + "copy last season, shift the families".
9. Storage & processing log (**sklad**) — garden produce only.
10. A **print view** — this month's work with checkboxes, and the season plan on one page.

**Out of v7, deliberately:** seed inventory · drawn garden map · photo journal **and reference photos** (so: *no blob storage at all*) · general household pantry · offline writes · bed sub-sections · auto-generated watering/weeding · green manure as a modelled crop · a second widget · back-filling historical seasons.

**Scale target: up to ~15 beds and ~40 crops.** Every bed fits on one screen as a card; the crop picker needs no pagination; the warnings panel is a flat list, not a grouped tree. The API still pages by the house `Limit`/`Cursor` convention — the *UI* is what gets to be simple.

---

## 1. How it sits in Home

`garden` is the ninth module, registered like the other eight: own routes, own Goose migrations (block **10**), own audit actions, one widget provider, plus metric and list providers via the optional `Source` interfaces built at composition. **No cross-module import** — `internal/arch`'s `TestModulesDoNotImportEachOther` stays green, which is exactly why the task engine lives here rather than inside `todo`.

It touches three existing platform packages and adds nothing new to `platform/`:

| Platform package | Used for |
|---|---|
| `platform/scheduler` | the twice-daily weather poll (the v5 in-process minute ticker) |
| `platform/metrics` + `platform/lists` | catalog contributions — the *only* way garden facts reach a notification (D113) |
| `platform/db`, `audit`, `httpx`, `idgen`, `lexorank`, `dates`, `slug` | the usual |

**No `platform/push` import, no `platform/blobstore`.** The first because Administrace owns delivery; the second because photos are out.

Four **non-registry host maps** must be edited by hand or the module half-appears — the trap v5 and v6 both fell into: `platform/widgets/registry.tsx`, `AppShell`'s `OVERFLOW` nav list, the Log browser's hardcoded `MODULES`, and `admin/listener.go`'s `inAppURL` → `/zahrada`.

---

## 2. Data model

House conventions throughout: UUIDv7 ids, lexorank for user ordering, soft delete, `created_by` / `created_at` / `updated_at`, English ids and Czech UI, every mutation audited in the same transaction.

### 2.1 `garden_plants` — knowledge base, species level

Identity: `name_cs`, `name_latin`, `family` (**enum, not free text** — the key the rotation engine joins on), `plant_type` (`vegetable` · `herb` · `fruit_tree` · `fruit_bush` · `perennial` · `flower`).

Agronomy: `feeder_class` (silný / střední / slabý / vážící dusík) · `root_depth` · `sun` · `water_need` · `soil_ph_min/max` · `hardiness` (`tender` / `half_hardy` / `hardy`, **required** — the frost logic reads it) · `rotation_break_years` (defaults from the family).

Propagation & spacing: `sow_method` (`direct` / `seedling` / `both` / `vegetative`) · `needs_pricking_out` · `hardening_days` · `sow_depth_cm` · `spacing_row_cm` · `spacing_plant_cm` · `plants_per_m2` (derived from spacing, overridable) · `days_to_germinate_min/max` · `germination_temp_c` · `days_to_maturity_min/max`.

**Timing windows** — four of them (`sow_indoor`, `sow_direct`, `transplant`, `harvest`), each a triple `{anchor, from, to}`:

| Anchor | Meaning | Example |
|---|---|---|
| `week` | ISO week of the year — how Czech garden literature states it | výsev: týden 10–13 |
| `last_frost` | days relative to the season's **last spring frost** | výsev do sadbovačů: −56 … −42 |
| `first_frost` | relative to the **first autumn frost** | sklizeň dýní: −14 … −3 |

Mixable per window, per crop. Frost-anchored windows move when a season's frost dates move; week-anchored ones stay put.

Harvest & storage: `harvest_unit` (kg / ks / l / svazek — per crop, so the harvest form never asks) · `yield_per_m2` and/or `yield_per_plant` · `storage_methods` (multi: sklep · sucho · mrazák · kvašení · zavařování · v písku · v zemi) · `storage_temp_c` · `storage_humidity` · `shelf_life_days`.

Problems: `pests[]` and `diseases[]` as structured rows (`name`, `symptom`, `remedy_md`), not one prose blob — the LLM fills them well and you'll want to search them.

Free form: `notes_md` (Markdown, in FTS alongside names and pests).

Provenance: `source` (`manual` / `llm`), `source_model`, `source_at`, `verified_by`, `verified_at`. An unchecked LLM record is badged **"neověřeno"** until you confirm it.

### 2.2 `garden_varieties` — odrůda

`plant_id`, `name`, `supplier`, `description_md`, `is_favourite`, `retired`, plus **nullable overrides** of every timing / spacing / yield / storage / maturity field. `NULL` = inherit.

The resolution — *effective value = variety value if non-null, else species value* — is **one documented function with its own unit test** (D103), the way the fin split is. The planner, the task generator, the warnings engine and the widget all read it; if those four ever disagree about when to sow Black Krim, the bug is unfindable.

### 2.3 `garden_beds` — záhony

`name` · `code` (short, e.g. `A1`, shown on chips) · `type` (vyvýšený / záhon / skleník / fóliovník / truhlík / sad) · `length_cm` · `width_cm` · `area_m2` (derived; overridable for irregular shapes) · `sun_exposure` · `zone` (free label: "horní zahrada") · `soil_notes_md` · `is_active` · lexorank position.

**Adjacency is inferred, not stored (D117):** two beds are neighbours if they are consecutive in lexorank order *within the same zone*. You get the adjacency checks for free and maintain them by dragging beds into the order they stand in. No neighbour table, no extra form.

### 2.4 `garden_seasons` — sezóna

`year` (unique) · `status` (`planning` / `active` / `closed`) · `last_frost_on` / `first_frost_on` (expected, defaulted from settings, editable) · `last_frost_actual_on` / `first_frost_actual_on` (recorded at close) · `closed_at` / `closed_by` · `notes_md`.

**Season ≠ calendar year (D105).** Česnek goes in the ground in October and comes out the following July, so **a planting belongs to the season of its harvest** and its sow date may fall in the previous calendar year. Occupancy spans the new year correctly and rotation counts it in the right year.

**Closing a season (D120)** is an explicit ritual: one screen that asks for final yields per planting, marks what failed and why, records the actual frost dates, then **locks the season** and writes it into rotation history. Closed seasons are read-only (an admin can re-open, audited).

**No historical back-fill (D120).** There is no importer for 2024/2025. Rotation checks (C3, C8) are therefore *structurally silent* in the first season and only get sharp from the third — that's expected, not a bug, and the UI says so rather than showing a reassuring green tick it hasn't earned.

### 2.5 `garden_plantings` — výsadba, the core row

`season_id` (**NULL ⇒ permanent** — a fruit tree, a rhubarb crown, an asparagus bed) · `bed_id` (nullable, with `location_label` for things outside beds) · `plant_id` · `variety_id` (nullable) · quantity as **either** `area_m2` **or** `plant_count` · `rows`.

Planned: `sow_indoor_on`, `sow_direct_on`, `transplant_on`, `harvest_from`, `harvest_to` — defaulted from the resolved KB windows against the season's frost dates, all overridable, with a flag recording which the user has touched so a re-anchor never clobbers a manual date.

Actual: `sowed_on`, `transplanted_on`, `first_harvest_on`, `cleared_on`. `status`: `planned` → `sown` → `planted` → `growing` → `harvesting` → `done`, or `failed` with a reason (a failure is data, not an embarrassment).

**Actual dates never re-drive the plan (D119).** Record that you sowed two weeks late and the harvest window stays where you planned it. What you get instead: the planting detail shows the drift explicitly — *"vyseto o 14 dní později, sklizeň v plánu beze změny"* — and a one-click "posunout navazující práce o 14 dní" if you want it. Shifting by hand marks those tasks `is_edited`, so regeneration then leaves them alone forever. Predictable beats clever; the drift line makes sure it isn't also silent.

Permanent plantings additionally: `planted_on`, `rootstock` (podnož), `removed_on`.

**Occupancy window** = first of (`sowed_on` ∥ `sow_direct_on` ∥ `transplant_on`) → (`cleared_on` ∥ `harvest_to`). This is what "share a bed" means (D107) — spring špenát and autumn pórek in the same bed never actually meet and must not be flagged.

One table with a NULL season rather than a separate perennials table (D106), so occupancy, warnings, tasks and harvests have exactly one code path. The **Trvalky** page is a filtered view.

### 2.6 `garden_rules` — snášenlivost a sled

One table, three scopes:

| `scope` | `a_ref` / `b_ref` | Meaning |
|---|---|---|
| `plant_pair` | two plant ids | mrkev ↔ cibule = `good` |
| `family_pair` | two family enums | Solanaceae ↔ Solanaceae = `bad` |
| `succession` | predecessor → successor | brukvovité po brukvovitých: `min_years_gap` = 4 |

Fields: `verdict` (`good` / `bad`), `severity` (`error` / `warn` / `info`), `min_years_gap`, `reason_cs`, `source`, `is_builtin`, `is_disabled`. Pairs stored in canonical order and matched both ways, so symmetry is structural. **Explicit `plant_pair` beats `family_pair`** — that precedence is the whole reason both exist.

**Built-in seed (D115)** ships as `10900_garden_seed.sql` — separate embedded source, excluded from `testsupport`, `INSERT OR IGNORE`, exactly the v6 pattern: the botanical families with their default break years, plus a curated set of ~50–80 well-attested Czech companion pairs, **each carrying its source**. Built-ins are visibly marked, can be disabled, and are never silently overwritten by an update.

### 2.7 `garden_tasks` — práce

`kind` · `season_id` · `planting_id` / `bed_id` (nullable) · `title_cs` · `window_from` / `window_to` · `due_hint` · `status` (`open` / `done` / `skipped`) · `completed_by` / `completed_at` · `is_generated` · `generation_key` · `is_edited` · `notes_md` · lexorank.

| id | Czech | Generated from |
|---|---|---|
| `bed_prep` | Příprava záhonu | planting's first date − lead |
| `sow_indoor` | Výsev do sadbovačů | `sow_indoor` window |
| `prick_out` | Pikýrování | + `days_to_germinate`, when `needs_pricking_out` |
| `harden_off` | Otužování | transplant − `hardening_days` |
| `sow_direct` | Přímý výsev | `sow_direct` window |
| `transplant` | Výsadba | `transplant` window |
| `thin` | Protrhávání | direct sowings only |
| `support` | Opora a vyvazování | when the crop needs support |
| `feed` | Přihnojení | feeder class + days after planting |
| `mulch` | Mulčování | optional per crop |
| `pest_check` | Kontrola škůdců | optional per crop |
| `prune` | Řez | perennials, yearly window from the KB |
| `spray` | Postřik | perennials, yearly window |
| `harvest` | Sklizeň | `harvest` window |
| `process` | Zpracování | after first harvest, if storage methods exist |
| `store` | Uskladnění | after `process` |
| `clear` | Úklid záhonu | after `harvest_to` |
| `water` | Zálivka | **manual only** (D118) |
| `weed` | Plení a okopávka | **manual only** (D118) |
| `other` | Jiné | manual |

**Regeneration rule (D110) — the part that decides whether you trust the calendar.** When a planting's dates change, the generator recomputes tasks matched by `generation_key` = hash(planting id, kind, occurrence index). It may move the window of an **open, unedited, generated** task. It never touches one that is `done`, `skipped` or `is_edited`. A generated task you delete leaves a tombstone so it does not resurrect. Manual tasks are never touched at all.

### 2.8 `garden_harvests` — sklizeň

`planting_id` · `harvested_on` · `quantity` · `unit` (defaulted from the crop) · `destination` (čerstvé / sklad / darováno / kompost) · `quality` · `note`. Summed per planting → actual yield, compared against the KB's expected `yield_per_m2` on the planting detail and at season close. That comparison is what makes next year's plan better than this year's.

### 2.9 `garden_storage_items` — sklad

`harvest_id` (nullable) · `planting_id` (nullable) · `product_name` ("okurky sterilované") · `method` (sklep / sucho / mrazák / kvašení / zavařeno / v písku) · `location` (free text: sklep / mrazák / spajz) · `quantity_initial` · `quantity_remaining` · `unit` · `stored_on` · `best_before` · `status` (`stored` / `consumed` / `spoiled`) · `note`.

**No movements table (D121):** you edit `quantity_remaining` in place and the audit spine's field diffs answer "when did we eat the last jar" for free. Garden produce only.

### 2.10 `garden_settings` — one row

Default frost dates · `latitude` / `longitude` / `altitude` (needed by the forecast, not a secret, so it lives in the UI, not in Coolify) · default rotation break years · **frost threshold °C** and look-ahead days · workload-spike threshold · which warning checks are enabled. **No audience settings** — that's Administrace's job (D113).

### 2.11 `garden_weather_days` — cache, not a feature

`day` · `temp_min` · `temp_max` · `precip_mm` · `fetched_at` · `source`. Written by the scheduler, read by the metric/list providers. Retention ~90 days.

### 2.12 `garden_warning_dismissals`

`season_id` · `warning_key` · `dismissed_by` · `dismissed_at` · `note`. Warnings themselves are **computed on read and never stored** (D108) — the same discipline as RRULE expansion and the fin split. `warning_key` derives from (rule id, bed id, sorted planting ids, season), so it survives recomputation; if the plan changes enough that the key changes, the warning correctly comes back.

---

## 3. The warnings engine

Computed on read, one endpoint (`GET /api/garden/seasons/{year}/check`), rendered in two places: a badge and inline list on each bed in the planner, and a full **"Kontrola plánu"** panel grouped by severity. **A warning never blocks a save (D109).** Each carries `severity`, Czech `title`, Czech `detail`, the entities it points at, and its stable key.

| Key | Check | Default severity |
|---|---|---|
| C1 | Two plantings sharing a bed with a `bad` rule **and overlapping occupancy** | error |
| C2 | Same family twice in one bed at once | info |
| C3 | **Rotation** — this family grew in this bed within `rotation_break_years` | error if under half the gap, else warn |
| C4 | Bed over-booked — Σ area (or count ÷ `plants_per_m2`) > bed area | warn |
| C5 | **Workload spike** — more than N sow/transplant tasks land in one ISO week | info |
| C6 | Family concentration — one family over X % of planned area | info |
| C7 | Active bed with nothing planted this season | info |
| C8 | Heavy feeder following heavy feeder in the same bed; converse tip when a legume precedes one | warn / tip |
| C9 | **Frost risk** — a `tender` crop's planned transplant falls before the season's last frost | warn |
| C10 | Planned date outside the crop's own recommended window | warn |
| C11 | Bad companions in **adjacent** beds (adjacency inferred per D117); two varieties of one species adjacent when you're saving seed | info |

Dismissal is per season and per key, with an optional note ("vím, letos to risknu"). Without dismissal you stop reading them by April.

**C3 and C8 need history**, and there is none before the first "Uzavřít sezónu". The panel says so — *"rotaci zatím nelze zkontrolovat, chybí historie"* — rather than reporting a clean plan it hasn't verified.

**The garden-wide view** is C3, C5, C6 and C8: none of them are visible from inside a single bed, which is why the check endpoint is season-scoped.

---

## 4. Copy last season + rotation shift

`POST /api/garden/seasons` with `{year, copy_from, shift}`:

1. Copy every non-permanent planting from `copy_from`.
2. If `shift` is given, rotate bed assignments by that offset over the ordered list of active beds — the crude but effective "posuň rodiny o jeden záhon". Shift by family group is the alternative mode, so a family moves as a block.
3. Re-anchor planned dates to the **new** season's frost dates (frost-anchored windows move; week-anchored stay).
4. Run the check immediately and show what the shift fixed and what it broke, side by side, **before** the new season is committed.

Step 4 is the point. A copy that silently reproduces last year's rotation error is worse than typing it again.

---

## 5. Weather and frost — delivery belongs to Administrace (D113)

**The garden module sends no push and owns no audience.** Your call: don't reinvent the wheel. What it does instead:

- **Polls** Open-Meteo twice daily via `platform/scheduler` (free, no API key; coordinates from settings; `timezone=Europe/Prague`). Failure is logged and silent — a forecast that didn't load must never make the Zahrada page look broken.
- **Publishes two catalog keys**, which is all the v5 machinery needs:
  - metric **`garden.frost_risk_tonight`** — the forecast minimum °C for tonight;
  - list **`garden.frost_sensitive_now`** — the tender/half-hardy plantings whose occupancy window is open, each line naming crop and bed (*"rajčata (A1)"*).
- **Writes one audit event `garden.frost_warning` per night**, idempotent on the date, when the threshold is crossed — its Czech `summary` already reads *"Dnes v noci −2 °C. Citlivé: rajčata (A1), papriky (A2)."*

You then choose in **Administrace → Oznámení** which mechanism you want, with the audience, active hours and coalescing you already have:

- a **scheduled summary** at, say, 17:00, conditioned `garden.frost_risk_tonight lte 2`, body `{{list.garden.frost_sensitive_now}}` — silent on every night there's nothing to say, exactly the `finance.missing_months gt 0` pattern; **or**
- a **trigger rule** on `garden.frost_warning`, which fires as soon as the poll flips and defaults its body to the event's summary.

Both work on day one because the module publishes for both. Same story in autumn: `garden.first_frost_in_days` gates *"za 3 dny první mráz — sklidit dýně a fazole."*

---

## 6. Widget and catalog contributions

**One widget, `garden.prace` — "Práce na zahradě" (D123).** Tasks whose window overlaps the next 30 days, overdue first, then grouped by week; each line shows crop and bed code. Tick-off via the house **2000 ms hold** gesture (D22), calling the garden module's own complete endpoint with `meta.via="dashboard"`. Empty state: *"na zahradě je teď klid"*. Harvest appears here as a Sklizeň task, so nothing is lost by not having a second widget.

**Metrics (+6, household-scoped, 13 → 19):**

| Key | Czech label | Unit |
|---|---|---|
| `garden.tasks_due_7d` | Práce na zahradě (7 dní) | úkolů |
| `garden.tasks_overdue` | Zmeškané práce | úkolů |
| `garden.plan_warnings` | Varování v plánu | varování |
| `garden.harvest_season` | Letošní sklizeň | kg |
| `garden.beds_unplanned` | Nezaplánované záhony | záhonů |
| `garden.frost_risk_tonight` | Noční minimum | °C |

**Lists (+6, 10 → 16):** the four countable keys above (same key ⇒ same selection, count = `len(items)`, per D77), plus two **list-only** keys on the D100 precedent — **`garden.harvest_ready`** and **`garden.frost_sensitive_now`**.

**Audit actions (+~11):** `plant.*`, `variety.*`, `bed.*`, `season.*`, `season.close`, `planting.*`, `task.*`, `harvest.*`, `storage.*`, `rule.*`, `settings.update`, `frost_warning` — qualified `garden.*` in the log browser and the trigger composer. Entity types `garden_plant`, `garden_planting` and `garden_task` join the field-diff set.

---

## 7. The LLM fill flow, and export

1. `GET /api/garden/plants/prompt-template?name=Rajče` returns a ready Czech prompt containing **the JSON schema generated from the same source the importer validates against** — so prompt and validator cannot drift — plus your context (Czech climate, the garden's frost dates, altitude) and an instruction to answer with JSON only.
2. Paste it into whichever model; paste the answer into the import box.
3. `POST /api/garden/plants/import?dry_run=true` validates, normalises, returns a **preview**: the parsed record, a field-by-field diff if it updates an existing crop, and the fields it couldn't map. Enum matching is lenient (accepts Czech words); an unmappable enum is a `422` naming the field, never a silent default.
4. Confirm → create/update, `source = llm`, model and date recorded, crop badged **"neověřeno"** until you tick it off.
5. An array is accepted, so twenty crops arrive in one paste. The same path serves varieties and the rules table.
6. **`GET /api/garden/export`** returns the whole KB (plants, varieties, rules) in the *same* JSON shape the importer accepts (D126) — a backup you can read, and a re-import that round-trips.

---

## 8. Frontend

| Route | Page |
|---|---|
| `/zahrada` | **Přehled** — season at a glance: beds and what's in them, warning badge, this month's work, harvest to date |
| `/zahrada/plodiny` | **Plodiny** — KB list, detail, editor, import/export; varieties nested under the species |
| `/zahrada/zahony` | **Záhony** — beds, dimensions, drag-order (which *is* the adjacency), per-bed rotation history |
| `/zahrada/plan/{rok}` | **Plán** — the planner: bed-by-bed assignment, inline warnings, Kontrola plánu panel, copy-season, Uzavřít sezónu |
| `/zahrada/kalendar` | **Kalendář** — tasks by month and week, filterable by bed / crop / kind; **print** |
| `/zahrada/sklizen` | **Sklizeň** — harvest entry, yields vs expected |
| `/zahrada/sklad` | **Sklad** — stored produce, best-before, remaining |
| `/zahrada/trvalky` | **Trvalky a dřeviny** — permanent plantings and their care |

At ~15 beds the planner is a single grid of bed cards with a crop picker — no two-pane layout, no virtualisation, no pagination in the UI.

**Print (D125):** one stylesheet, two targets — this month's tasks with real checkboxes and bed codes, and the season plan on one page. It's the answer to "no signal in the garden", since writes stay offline-disabled.

Czech UI vocabulary gets fixed in the PRD the way §V6-7 did for Finance, so page, widget, metric labels and notification tokens all say the same words. Colours come from `--c1…--c5` under the **Path A** aliasing already resolved in `HANDOFF-design.md`, with mandatory secondary encoding — which matters here, because "colour by botanical family" is exactly the kind of chart that fails a CVD check.

Offline: everything reads from the persisted TanStack Query cache; write controls disable.

---

## 9. Decisions D101–D127

| # | Decision |
|---|---|
| **D101** | `garden` is a self-contained ninth module. Garden work is **not** mirrored into `todo` or `events` — §10 D25/D28 forbid the import, and a card has no window and no planting link. |
| **D102** | Timing stored as `{anchor, from, to}`, anchors `week` / `last_frost` / `first_frost`, mixable per window. |
| **D103** | Species → variety with nullable overrides, resolved by **one documented function** with its own test. |
| **D104** | `family` is a fixed enum — rotation and family rules join on it. |
| **D105** | A planting belongs to the season of its **harvest**; sow dates may fall in the previous calendar year. |
| **D106** | Permanents are `garden_plantings` rows with `season_id IS NULL`, not a second table. |
| **D107** | Bed sharing is judged on **overlapping occupancy windows**, not calendar-year membership. |
| **D108** | Warnings computed on read; only dismissals persist, keyed stably. |
| **D109** | Warnings never block a save. Severity `error` / `warn` / `info` / `tip`. |
| **D110** | Generated tasks regenerate but never overwrite `done` / `skipped` / `is_edited`; deletes leave tombstones. |
| **D111** | `hardiness` is required on every crop — the frost logic reads it. |
| **D112** | Weather = Open-Meteo, polled by the existing scheduler, failing silently; coordinates in settings, not env. |
| **D113** | **The module sends no push.** It publishes `garden.frost_risk_tonight` + `garden.frost_sensitive_now` and writes one idempotent `garden.frost_warning` audit event per night; **Administrace** decides audience, timing, conditions and active hours through the machinery v5 already built. |
| **D114** | The LLM prompt embeds a schema generated from the importer's own validator; imports preview with a diff and carry provenance + a "neověřeno" badge. |
| **D115** | Built-in rules ship as a seed source excluded from `testsupport`: families + break years, plus ~50–80 sourced Czech companion pairs. Built-ins are marked and disableable, never silently overwritten. |
| **D116** | No bed sub-sections — a bed is one unit; everything in it counts as adjacent. |
| **D117** | Bed adjacency is **inferred from lexorank order within a zone**. No neighbour table, no extra data entry. |
| **D118** | No auto-generated watering or weeding. `water` and `weed` exist as manual kinds only. |
| **D119** | Actual dates are recorded but **never re-drive planned windows**. The planting detail states the drift and offers a one-click shift of downstream tasks, which marks them `is_edited`. |
| **D120** | **"Uzavřít sezónu"** is explicit: final yields, failures, actual frost dates, then the season locks and becomes rotation history. No historical back-fill; C3/C8 stay silent — and say so — until history accrues. |
| **D121** | Storage tracks `quantity_remaining` edited in place; the audit spine is the consumption history. |
| **D122** | No photos and therefore **no blob storage** in `garden`. |
| **D123** | One widget, `garden.prace`. Harvest surfaces as a Sklizeň task. |
| **D124** | Ticking a task is an ordinary `editor` write. No role exception; `reader` stays read-only. |
| **D125** | A print stylesheet: this month's work with checkboxes, and the season plan on one page. |
| **D126** | `GET /api/garden/export` emits the KB in the importer's own JSON shape — backup and round-trip. |
| **D127** | Green manure is not modelled in v7 — no crop type, no task kind. |

---

## 10. Two consequences worth having said out loud

**Rotation is the flagship warning and it will do nothing in year one.** D120 (no back-fill) plus the season-close ritual means C3 and C8 have no data until the end of the first season, and aren't really sharp until the third. The panel states this explicitly instead of showing an unearned green tick. Everything else — companions, capacity, frost, timing, workload — works on day one.

**"Planned windows stay put" means the calendar is a plan, not a tracker.** Sow late and the harvest task stays where February said it would. The drift line and the one-click shift are what keep that from being silently wrong; if after a season it feels like too much manual nudging, the recompute-from-actuals behaviour is a contained change to the generator and nothing else.

---

## 11. Acceptance criteria (draft, to be finalised in the PRD)

- [ ] `garden` registers through `registry.Module`; `TestModulesDoNotImportEachOther` stays green; **no import of `platform/push` or `platform/blobstore`**.
- [ ] The species→variety resolution function has a unit test with an inheritance table and an override table.
- [ ] Every check C1–C11 has a fixture that triggers it and one that doesn't; C1 has an explicit non-overlapping-occupancy case that must **not** warn; C3 asserts the "chybí historie" state on a fresh install.
- [ ] Regeneration: move a planting's transplant date — open generated tasks move, a `done` one doesn't, an edited one doesn't, a deleted one stays deleted.
- [ ] Recording an actual sow date **does not** move any planned window; the drift line appears; the one-click shift marks the shifted tasks `is_edited`.
- [ ] Copy-season with shift reproduces the plan, re-anchors frost-anchored dates only, and shows the check diff before commit.
- [ ] Season close: locks the season, writes rotation history, and a subsequent C3 in the same bed fires.
- [ ] Frost: `garden.frost_risk_tonight` and `garden.frost_sensitive_now` resolve; one `garden.frost_warning` per night survives a catch-up tick; a v5 schedule conditioned on the metric sends exactly once and stays silent above threshold.
- [ ] Import: valid JSON previews with a diff; an unmappable enum 422s naming the field; provenance and "neověřeno" recorded. Export re-imports byte-for-byte equivalent.
- [ ] All six metrics resolve through the provider contract; each list agrees with its metric by construction.
- [ ] `garden.prace` appears in the widget catalog, renders both states, ticks via the 2000 ms hold.
- [ ] The four non-registry host maps are updated.
- [ ] Migrations run `… finance(09) → garden(10)` cleanly on an empty DB and after a Litestream restore; the rule seed is excluded from `testsupport`.
- [ ] OpenAPI **0.9.0** validates, reusing the shared `Limit` / `Cursor` / `responses` / security components.
- [ ] Print: the month view prints with checkboxes and bed codes; the plan prints on one page.
- [ ] Offline: every garden page renders read-only from cache, write controls disabled.
