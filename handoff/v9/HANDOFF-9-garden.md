# Home — Module 9: `garden` (Zahrada)

> Build brief for Claude Code. Source of truth for behaviour: `PRD.md` **§V7-1…§V7-11** (decisions **D101–D132**, **FR-G1–G17**). Data shapes: `openapi.yaml` **0.9.0** (tag `garden`). Read `HANDOFF.md` §3 (module registry) and `HANDOFF-1-logging.md` (the audit spine) first; `HANDOFF-7-admin.md` §6–§7 for `platform/scheduler` and the metrics/lists catalogs, which this module publishes into but never sends through.
>
> **This is the largest module Home has gained** — eleven tables against Finance's one. There is no reference implementation and nothing to port. The difficulty is not volume: it is concentrated in **four pure functions** (§2 timing, §3 variety resolution, §5 occupancy, §6 the check engine) and **one stateful algorithm** (§7 regeneration). Build those first, in isolation, with tests. Everything else is CRUD.

## The model in one paragraph

A **plodina** (crop) holds what the household knows about a species; an **odrůda** overrides only what differs. A **výsadba** puts a crop in a **záhon** for a **sezóna**, with planned dates resolved from the crop's anchored timing windows against that season's frost dates. From each planting the generator derives **práce** — dated work with an od–do window. The **kontrola plánu** reads the whole season and returns warnings that are computed, never stored, and never block a save. Two things are recorded afterwards: **sklizeň** (what came out) and **sklad** (where it went). Closing a season locks it and is the only thing that creates rotation history.

**The module holds no bytes** (D122 — no blob store) and **sends no push** (D113 — it publishes catalog keys and one audit event; Administrace delivers). Every mutation writes an audit event in the same transaction.

## Build order

Do not deviate from this. The first four steps are pure functions with no database and no HTTP, and they are what the rest of the module means.

1. **§2 `timing.go`** — anchored window → real dates. Ten minutes to write, and every later date in the system comes out of it.
2. **§3 `resolve.go`** — species + variety → effective record (D103). One function, four consumers.
3. **§5 `occupancy.go`** — the derived window that defines "shares a bed" (D107).
4. **§6 `check.go`** — C1–C11 over a **loaded snapshot**, no DB access inside a check.
5. **§1 migrations** → **§8 store/service** → **§9 endpoints**.
6. **§7 `generate.go`** — task generation and the conservative regeneration rules (D110). Needs §2/§3 and the store.
7. **§10 module registration + wiring**, **§11 widget**, **§12 metrics/lists**.
8. **§13 the rule seed** + the `testsupport` split — before the frontend, so later tests run against the real shape.
9. **§14 weather + frost publication** (and the one platform hook it needs).
10. **§15 the enum registry → prompt/import/export**.
11. **§16–§17 frontend + Czech copy**, **§19 tests**.

---

## 1. Data model (PRD §V7-5) — eleven tables, block 10

`internal/modules/garden/migrations/10001_garden.sql`. Applied last in the one Goose sequence (`… finance(09) → garden(10)`, see `bootstrap.MigrationSources`). **Nothing is seeded here** — the built-in compatibility rules arrive through a separate migration source (§13).

House conventions: UUIDv7 `id`, `deleted_at` soft delete, `created_by`/`created_at`/`updated_at`, lexorank `position` where users order things, dates as `TEXT` `YYYY-MM-DD` in `HOME_TIMEZONE`.

```sql
-- +goose Up
-- +goose StatementBegin

-- ---------- knowledge base ----------
-- `family` and `hardiness` are NOT NULL on purpose (PRD D104, D111): the rotation
-- engine joins on the first and the frost logic reads the second. A crop missing
-- either is one this module cannot reason about, so it is refused at write time
-- rather than defaulted to something plausible.
CREATE TABLE garden_plants (
    id                     TEXT PRIMARY KEY,
    name_cs                TEXT NOT NULL,
    name_latin             TEXT,
    family                 TEXT NOT NULL,     -- enum, see §15
    plant_type             TEXT NOT NULL,     -- vegetable|herb|fruit_tree|fruit_bush|perennial|flower
    hardiness              TEXT NOT NULL,     -- tender|half_hardy|hardy
    feeder_class           TEXT,              -- heavy|medium|light|fixer
    root_depth             TEXT,
    sun                    TEXT,
    water_need             TEXT,
    soil_ph_min            REAL,
    soil_ph_max            REAL,
    rotation_break_years   INTEGER,           -- NULL = inherit the family default
    sow_method             TEXT NOT NULL,     -- direct|seedling|both|vegetative
    needs_pricking_out     INTEGER NOT NULL DEFAULT 0,
    needs_support          INTEGER NOT NULL DEFAULT 0,   -- drives the `support` task
    wants_mulch            INTEGER NOT NULL DEFAULT 0,   -- drives the `mulch` task
    wants_pest_check       INTEGER NOT NULL DEFAULT 0,   -- drives the `pest_check` task
    hardening_days         INTEGER,
    sow_depth_cm           REAL CHECK (sow_depth_cm IS NULL OR sow_depth_cm > 0),
    spacing_row_cm         REAL CHECK (spacing_row_cm IS NULL OR spacing_row_cm > 0),
    spacing_plant_cm       REAL CHECK (spacing_plant_cm IS NULL OR spacing_plant_cm > 0),
    plants_per_m2          REAL,              -- NULL = derive from the two spacings
    days_to_germinate_min  INTEGER,
    days_to_germinate_max  INTEGER,
    germination_temp_c     INTEGER,
    days_to_maturity_min   INTEGER,
    days_to_maturity_max   INTEGER,
    -- four windows x (anchor, from, to). Anchor: week|last_frost|first_frost (PRD D102).
    win_sow_indoor_anchor  TEXT, win_sow_indoor_from  INTEGER, win_sow_indoor_to  INTEGER,
    win_sow_direct_anchor  TEXT, win_sow_direct_from  INTEGER, win_sow_direct_to  INTEGER,
    win_transplant_anchor  TEXT, win_transplant_from  INTEGER, win_transplant_to  INTEGER,
    win_harvest_anchor     TEXT, win_harvest_from     INTEGER, win_harvest_to     INTEGER,
    harvest_unit           TEXT NOT NULL,     -- kg|ks|l|svazek
    yield_per_m2           REAL,
    yield_per_plant        REAL,
    storage_methods        TEXT,              -- JSON array of enum members
    storage_temp_c         INTEGER,
    storage_humidity       INTEGER,
    shelf_life_days        INTEGER,
    pests                  TEXT,              -- JSON [{name,symptom,remedy_md}]
    diseases               TEXT,
    notes_md               TEXT,
    source                 TEXT NOT NULL DEFAULT 'manual',  -- manual|llm (PRD D114)
    source_model           TEXT,
    source_at              TEXT,
    verified_by            TEXT,
    verified_at            TEXT,
    deleted_at             TEXT,
    created_by             TEXT,
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL,
    CHECK (days_to_germinate_min IS NULL OR days_to_germinate_max IS NULL
           OR days_to_germinate_min <= days_to_germinate_max),
    CHECK (days_to_maturity_min IS NULL OR days_to_maturity_max IS NULL
           OR days_to_maturity_min <= days_to_maturity_max)
);
-- +goose StatementEnd

CREATE UNIQUE INDEX ux_garden_plants_name ON garden_plants (name_cs) WHERE deleted_at IS NULL;
CREATE INDEX idx_garden_plants_family ON garden_plants (family) WHERE deleted_at IS NULL;
CREATE INDEX idx_garden_plants_type   ON garden_plants (plant_type) WHERE deleted_at IS NULL;

-- Varieties carry a NULLABLE MIRROR of every inherited column; NULL = inherit (PRD D103).
-- Listed short here for readability — mirror the full set in the real migration.
CREATE TABLE garden_varieties (
    id              TEXT PRIMARY KEY,
    plant_id        TEXT NOT NULL REFERENCES garden_plants(id),
    name            TEXT NOT NULL,
    supplier        TEXT,
    description_md  TEXT,
    is_favourite    INTEGER NOT NULL DEFAULT 0,
    retired         INTEGER NOT NULL DEFAULT 0,
    -- … every overridable column from garden_plants, all NULLABLE …
    source TEXT NOT NULL DEFAULT 'manual', source_model TEXT, source_at TEXT,
    verified_by TEXT, verified_at TEXT,
    deleted_at TEXT, created_by TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX ux_garden_varieties_name
    ON garden_varieties (plant_id, name) WHERE deleted_at IS NULL;

-- ---------- beds ----------
-- No neighbour table: adjacency is (zone, position) adjacency (PRD D117). The index
-- below is not a performance index, it IS the adjacency model — do not drop it.
CREATE TABLE garden_beds (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    code          TEXT NOT NULL,
    type          TEXT NOT NULL,
    length_cm     REAL, width_cm REAL,
    area_m2       REAL NOT NULL CHECK (area_m2 > 0),
    sun_exposure  TEXT,
    zone          TEXT,
    soil_notes_md TEXT,
    is_active     INTEGER NOT NULL DEFAULT 1,
    position      TEXT NOT NULL,
    deleted_at TEXT, created_by TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX idx_garden_beds_zone_pos ON garden_beds (zone, position) WHERE deleted_at IS NULL;

-- ---------- seasons ----------
CREATE TABLE garden_seasons (
    id                     TEXT PRIMARY KEY,
    year                   INTEGER NOT NULL,
    status                 TEXT NOT NULL DEFAULT 'planning',  -- planning|active|closed
    last_frost_on          TEXT, first_frost_on TEXT,
    last_frost_actual_on   TEXT, first_frost_actual_on TEXT,
    notes_md               TEXT,
    closed_at TEXT, closed_by TEXT,
    created_by TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX ux_garden_seasons_year ON garden_seasons (year);

-- ---------- plantings ----------
-- season_id NULL ⇒ PERMANENT (a tree, a rhubarb crown) — one table, one code path
-- for occupancy, warnings, tasks and harvests (PRD D106).
CREATE TABLE garden_plantings (
    id             TEXT PRIMARY KEY,
    season_id      TEXT REFERENCES garden_seasons(id),
    bed_id         TEXT REFERENCES garden_beds(id),
    location_label TEXT,
    plant_id       TEXT NOT NULL REFERENCES garden_plants(id),
    variety_id     TEXT REFERENCES garden_varieties(id),
    area_m2        REAL, plant_count INTEGER, rows INTEGER,
    sow_indoor_on  TEXT, sow_direct_on TEXT, transplant_on TEXT,
    harvest_from   TEXT, harvest_to TEXT,
    manual_dates   TEXT NOT NULL DEFAULT '[]',   -- JSON array of planned-date field names
    sowed_on       TEXT, transplanted_on TEXT, first_harvest_on TEXT, cleared_on TEXT,
    status         TEXT NOT NULL DEFAULT 'planned',
    fail_reason    TEXT,
    planted_on     TEXT, rootstock TEXT, removed_on TEXT,
    notes_md       TEXT,
    deleted_at TEXT, created_by TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
    CHECK ((area_m2 IS NULL) <> (plant_count IS NULL))   -- exactly one
);
CREATE INDEX idx_garden_plantings_season_bed ON garden_plantings (season_id, bed_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_garden_plantings_bed        ON garden_plantings (bed_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_garden_plantings_plant      ON garden_plantings (plant_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_garden_plantings_permanent  ON garden_plantings (bed_id) WHERE season_id IS NULL AND deleted_at IS NULL;

-- ---------- tasks ----------
CREATE TABLE garden_tasks (
    id             TEXT PRIMARY KEY,
    kind           TEXT NOT NULL,
    season_id      TEXT REFERENCES garden_seasons(id),
    planting_id    TEXT REFERENCES garden_plantings(id),
    bed_id         TEXT REFERENCES garden_beds(id),
    title_cs       TEXT NOT NULL,
    window_from    TEXT NOT NULL,
    window_to      TEXT NOT NULL,
    due_hint       TEXT,
    status         TEXT NOT NULL DEFAULT 'open',   -- open|done|skipped
    completed_by   TEXT, completed_at TEXT,
    is_generated   INTEGER NOT NULL DEFAULT 0,
    is_edited      INTEGER NOT NULL DEFAULT 0,
    generation_key TEXT,
    suppressed     INTEGER NOT NULL DEFAULT 0,     -- the tombstone (PRD D110)
    notes_md       TEXT,
    position       TEXT NOT NULL,
    deleted_at TEXT, created_by TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
    CHECK (window_to >= window_from)
);
CREATE INDEX idx_garden_tasks_window ON garden_tasks (window_from) WHERE deleted_at IS NULL;
CREATE INDEX idx_garden_tasks_status ON garden_tasks (status, window_to) WHERE deleted_at IS NULL;
CREATE INDEX idx_garden_tasks_planting ON garden_tasks (planting_id) WHERE deleted_at IS NULL;
-- One live task per generation identity. Tombstones are excluded so a suppressed
-- key can coexist with nothing — and a re-created task cannot collide with it.
CREATE UNIQUE INDEX ux_garden_tasks_genkey ON garden_tasks (generation_key)
    WHERE generation_key IS NOT NULL AND suppressed = 0 AND deleted_at IS NULL;

-- ---------- harvest, storage ----------
CREATE TABLE garden_harvests (
    id TEXT PRIMARY KEY,
    planting_id TEXT NOT NULL REFERENCES garden_plantings(id),
    harvested_on TEXT NOT NULL,
    quantity REAL NOT NULL CHECK (quantity >= 0),
    unit TEXT NOT NULL,
    destination TEXT, quality TEXT, note TEXT,
    deleted_at TEXT, created_by TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX idx_garden_harvests_planting ON garden_harvests (planting_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_garden_harvests_date     ON garden_harvests (harvested_on) WHERE deleted_at IS NULL;

CREATE TABLE garden_storage_items (
    id TEXT PRIMARY KEY,
    harvest_id TEXT REFERENCES garden_harvests(id),
    planting_id TEXT REFERENCES garden_plantings(id),
    product_name TEXT NOT NULL,
    method TEXT NOT NULL,
    location TEXT,
    quantity_initial REAL NOT NULL CHECK (quantity_initial >= 0),
    quantity_remaining REAL NOT NULL CHECK (quantity_remaining >= 0),
    unit TEXT NOT NULL,
    stored_on TEXT NOT NULL,
    best_before TEXT,
    status TEXT NOT NULL DEFAULT 'stored',
    note TEXT,
    deleted_at TEXT, created_by TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
    CHECK (quantity_remaining <= quantity_initial)
);
CREATE INDEX idx_garden_storage_status ON garden_storage_items (status, best_before) WHERE deleted_at IS NULL;

-- ---------- rules, dismissals, settings, weather ----------
-- Pairs are stored in CANONICAL (sorted) order so symmetry is structural: the
-- matcher looks up one row, not two, and nobody has to remember to enter both.
CREATE TABLE garden_rules (
    id TEXT PRIMARY KEY,
    scope TEXT NOT NULL,               -- plant_pair|family_pair|succession
    a_ref TEXT NOT NULL, b_ref TEXT NOT NULL,
    verdict TEXT NOT NULL,             -- good|bad
    severity TEXT NOT NULL DEFAULT 'warn',
    min_years_gap INTEGER,
    reason_cs TEXT, source TEXT,
    is_builtin INTEGER NOT NULL DEFAULT 0,
    is_disabled INTEGER NOT NULL DEFAULT 0,
    deleted_at TEXT, created_by TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX ux_garden_rules ON garden_rules (scope, a_ref, b_ref) WHERE deleted_at IS NULL;

CREATE TABLE garden_warning_dismissals (
    id TEXT PRIMARY KEY,
    season_id TEXT NOT NULL REFERENCES garden_seasons(id),
    warning_key TEXT NOT NULL,
    note TEXT,
    dismissed_by TEXT, dismissed_at TEXT NOT NULL
);
CREATE UNIQUE INDEX ux_garden_dismissals ON garden_warning_dismissals (season_id, warning_key);

CREATE TABLE garden_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    default_last_frost TEXT, default_first_frost TEXT,     -- MM-DD
    latitude REAL, longitude REAL, altitude_m INTEGER,
    rotation_break_default INTEGER NOT NULL DEFAULT 4,
    frost_threshold_c REAL NOT NULL DEFAULT 2,
    frost_lookahead_days INTEGER NOT NULL DEFAULT 3,
    workload_week_threshold INTEGER NOT NULL DEFAULT 10,
    checks_config TEXT NOT NULL DEFAULT '{}',
    updated_at TEXT NOT NULL
);
INSERT INTO garden_settings (id, updated_at) VALUES (1, datetime('now'));

CREATE TABLE garden_weather_days (
    day TEXT PRIMARY KEY,
    temp_min REAL, temp_max REAL, precip_mm REAL,
    fetched_at TEXT NOT NULL, source TEXT NOT NULL
);
```

**FTS.** `garden_plants_fts` over `name_cs`, `name_latin`, `notes_md` and the flattened pest/disease names, kept in sync with insert/update/delete triggers — the same shape as `notes_fts` and `documents_fts`. Copy that pattern rather than inventing a third.

---

## 2. `timing.go` — anchored windows (PRD D102). **Build this first.**

Every planned date in the system comes out of this file. It has no dependencies and no database.

```go
type Anchor string // "week" | "last_frost" | "first_frost"

type Window struct {
	Anchor Anchor
	From   int // ISO week 1–53, or a day offset (negative = before the anchor)
	To     int
}

type SeasonAnchors struct {
	Year       int
	LastFrost  *time.Time // nil when the season has not set it
	FirstFrost *time.Time
}

// Resolve returns the window's real dates, or ok=false when the anchor it needs is
// missing. A caller that gets ok=false LEAVES THE DATE UNSET — it never guesses.
func (w Window) Resolve(a SeasonAnchors, loc *time.Location) (from, to time.Time, ok bool)
```

Rules, all of which need a test:

- **`week`** — `from` = Monday of ISO week `From` in `a.Year`; `to` = Sunday of ISO week `To`. **A year with no ISO week 53 clamps to the Sunday of week 52** — the same species of deviation as D19's short-month clamp, and for the same reason: the alternative is a date that silently lands in January of the following year.
- **`last_frost` / `first_frost`** — `anchor.AddDate(0, 0, From)` … `AddDate(0, 0, To)`. Nil anchor ⇒ `ok=false`.
- `To < From` is a **validation error at write time**, not something to sort out here.
- All arithmetic in `HOME_TIMEZONE`, all results date-only (midnight local).

**A window resolves against the season, never against `time.Now()`.** A planting must resolve identically whenever the page is loaded; a generator that consults the clock produces a plan that changes while you read it.

---

## 3. `resolve.go` — species + variety (PRD D103). **The `split.go` of this module.**

```go
// Effective is a Plant with every overridable field resolved.
func Resolve(p Plant, v *Variety) Effective
```

*Effective value = variety value if non-null, else species value.* That is the whole function, and it must exist **exactly once**: the planner, the task generator, the check engine and the widget all read through it. Four independent re-implementations of "when do we sow Black Krim" is a bug nobody would ever find.

Two tests, both table-driven: a variety with every override `NULL` must equal the species field-for-field; a variety with every field set must equal itself field-for-field. Add a third that fails the build if a new overridable column is added to `garden_plants` without being mirrored — reflection over the two structs is fine and is the only thing that will catch it in a year.

---

## 4. Store — `store.go`

Ordinary SQL, one file. Two reads carry weight:

- **`SeasonSnapshot(ctx, year) (Snapshot, error)`** — one call loading everything the check engine needs: the season, its plantings with their resolved crops and varieties, the beds in `(zone, position)` order, the enabled rules, the dismissals, and the closed-season history for the beds involved. The check engine takes this struct and touches no database (§6). Four or five queries, not N+1.
- **`BedHistory(ctx, bedID)`** — families per **closed** season only (D120). An open season is not history.

Everything else is CRUD with soft-delete filters and UUIDv7 keyset pagination through the shared `Limit`/`Cursor` components.

---

## 5. `occupancy.go` — what "shares a bed" means (PRD D107)

```go
// From = first non-nil of (sowed_on, sow_direct_on, transplant_on)
// To   = cleared_on, else harvest_to
func (p Planting) Occupancy() (from, to *time.Time)
func Overlap(a, b Planting) bool   // half-open; nil bounds are open-ended
```

**This is the single most important line of logic in the check engine.** Spring špenát and autumn pórek in one bed never meet, and a companion check that flags them is a check nobody reads by April. A planting with no resolvable dates is treated as **open-ended** — it overlaps everything in its bed — which is the conservative direction: an unplanned planting should provoke a question, not silence.

---

## 6. `check.go` — the eleven checks (PRD §V7-4 FR-G9)

```go
func Check(s Snapshot, cfg ChecksConfig) Result
```

**Pure.** It takes the loaded snapshot and returns results; it opens no transaction, reads no clock beyond `s.Today`, and calls nothing that can fail. That is what makes each check a table-driven test with a fixture that fires and a fixture that must not.

**The warning key** is stable across recomputation:

```go
key = hex(sha256(check | ruleID | bedID | strings.Join(sortedPlantingIDs, ",") | year))[:16]
```

The year is in the hash deliberately, even though the dismissal row is already season-scoped: it means **a copied season does not inherit last year's dismissals**. "Vím, letos to risknu" was said about one year.

| # | Check | Fires when | Default severity |
|---|---|---|---|
| C1 | Companions in a bed | two plantings share a bed, their **occupancies overlap**, and a rule matches with `verdict=bad`. `plant_pair` beats `family_pair` | from the rule (`error` for a `bad` pair) |
| C2 | Same family in a bed | two overlapping plantings in one bed share a `family` and no explicit rule already covered them | `info` |
| C3 | Rotation | the family grew in this bed within `rotation_break_years` (crop value, else family default, else `settings.rotation_break_default`), counted over **closed** seasons | `error` if the gap is under half the break, else `warn` |
| C4 | Bed over-booked | Σ (`area_m2`, or `plant_count / plants_per_m2`) of overlapping plantings > `bed.area_m2` | `warn` |
| C5 | Workload spike | more than `settings.workload_week_threshold` `sow_*`/`transplant` tasks land in one ISO week | `info` |
| C6 | Family concentration | one family exceeds 40 % of the season's planned area | `info` |
| C7 | Empty bed | an active bed has no planting in the season | `info` |
| C8 | Feeder succession | `heavy` follows `heavy` in the same bed across seasons → `warn`; a `fixer` immediately before a `heavy` → **`tip`**, phrased as praise, not a warning |
| C9 | Frost risk | a `tender` crop's `transplant_on` (or `sow_direct_on`) falls **before** `season.last_frost_on` | `warn` |
| C10 | Out of window | a planned date falls outside the crop's own resolved window by more than 7 days | `warn` |
| C11 | Neighbouring beds | a `bad` rule between plantings in **adjacent** beds (`(zone, position)` neighbours, D117); or two varieties of one species adjacent | `info` |

**C3 and C8 depend on history that may not exist.** If `snapshot.ClosedSeasons == 0`, they emit **nothing** and `Result.History.Status = "no_history"`. Do not emit a passing result: a check that cannot run must never look like one that ran and found nothing. The frontend renders *"rotaci zatím nelze zkontrolovat, chybí historie"* off that flag (D120).

Per-check enable/severity comes from `settings.checks_config`; a disabled check emits nothing at all.

---

## 7. `generate.go` — task generation and regeneration (PRD D110)

```go
type Plan struct {
	Create []Task
	Move   []TaskMove   // id + new window
	Leave  []string     // ids deliberately untouched, with a reason (for the audit meta)
}

func GenerateFor(p Planting, eff Effective, a SeasonAnchors, existing []Task) Plan
```

`generation_key = hex(sha256(plantingID | kind | index))[:16]`.

**The regeneration rules are the module's trust surface. Encode them as a single guard, not as scattered `if`s:**

```go
func mutable(t Task) bool {
	return t.IsGenerated && !t.IsEdited && t.Status == StatusOpen && !t.Suppressed
}
```

A task the generator may not touch is **left alone silently** — not moved, not deleted, not recreated. A generated task the user deleted keeps its row with `suppressed = 1` so the key stays taken and it cannot resurrect. Manual tasks (`is_generated = 0`) are never in scope at all.

**Derivation table.** Each row produces at most one task; a row whose inputs are missing produces none (never a guessed date).

| Kind | Window |
|---|---|
| `bed_prep` | first planned date − 21 d … − 7 d |
| `sow_indoor` | the resolved `win_sow_indoor` |
| `prick_out` | `sow_indoor_on` + `days_to_germinate_min` … + `days_to_germinate_max` + 7, when `needs_pricking_out` |
| `harden_off` | `transplant_on` − `hardening_days` − 2 … − 1 |
| `sow_direct` | the resolved `win_sow_direct` |
| `transplant` | the resolved `win_transplant` |
| `thin` | `sow_direct_on` + 14 … + 28, direct sowings only |
| `support` | `transplant_on` (or `sow_direct_on`) … + 14, when `needs_support` |
| `feed` | `heavy`: + 28 … + 42; `medium`: + 42 … + 56; `light`/`fixer`: none |
| `mulch` | planting date + 7 … + 21, when `wants_mulch` |
| `pest_check` | monthly across the occupancy window, when `wants_pest_check` |
| `prune` / `spray` | permanents only, from the crop's windows, once per calendar year |
| `harvest` | the resolved `win_harvest` |
| `process` | `harvest_from` … `harvest_to` + 7, when `storage_methods` is non-empty |
| `store` | `process.to` … + 7 |
| `clear` | `harvest_to` + 1 … + 21 |
| `water`, `weed` | **never generated** (D118) |

Titles are Czech and built from the crop and bed: *"Výsev do sadbovačů — rajče Black Krim"*, *"Výsadba — rajče (A1)"*.

**Trigger points:** planting create/update (dates, crop, variety, quantity), season frost-date change (re-anchor, skipping `manual_dates`), and crop timing change (regenerate every open planting that uses it). The audit event's `meta` carries `{created, moved, left}` counts so the Log can answer "what did that edit do to my calendar".

---

## 8. Service — `service.go`

Ordinary shape: validate → `WithTx` → write + audit in the same transaction → notify. Specifics worth stating:

- **Planned-date defaults.** On planting create, resolve each window and fill the planned dates; any date the caller sent explicitly is appended to `manual_dates` and is skipped by every later re-anchor.
- **Actual dates never re-drive planned ones (D119).** Recording `sowed_on` writes exactly one column. The `drift` block on the read model is derived: `sowed_on − (sow_indoor_on ?? sow_direct_on)`, with the Czech sentence built in the service, not the frontend, so the widget, the page and any future summary say it identically.
- **`ShiftTasks(plantingID, days, kinds)`** moves open tasks and sets `is_edited = 1` on each. That flag is the point: it is the user saying "I have decided this", and D110 then protects it forever.
- **Closed seasons are frozen.** Every mutating path checks the planting's / task's season status and returns `409` — one helper, called from every write, not a check remembered per handler.
- **Season close** collects outcomes, stamps `closed_at`/`closed_by`, sets `status='closed'`. **Reopen is `admin` only** and audited as `garden.season.reopen`.
- **Built-in rules**: `PATCH` accepts only `is_disabled` and `severity` (else `422`); `DELETE` is `409` (D130).

---

## 9. Endpoints (see `openapi.yaml` 0.9.0) + role gating

34 paths, tag `garden`. **Reads: any member incl. `reader`. Writes: `editor`/`admin` + CSRF. Exactly one admin-only route: `POST /api/garden/seasons/{year}/reopen`.**

Three shapes that are not boilerplate:

- **Seasons are addressed by `{year}`** (D128), not by id — the shared `GardenYear` parameter. The store still keys on `id` internally; the handler resolves year → id once.
- **`dry_run` is the shared preview idiom** (D129) on `POST /api/garden/plants/import` (**default `true`**) and `POST /api/garden/seasons` (**default `false`**). Note the different defaults, and that they are both deliberate: an import should never apply by accident, and a season creation should not require a second call in the common case.
- **`POST /api/garden/tasks/{id}/complete` is idempotent** (D131) — completing an already-complete task is `200`, not `409`. The dashboard's 2000 ms hold can fire twice on a bad connection and the second send must not look like an error. `reopen` is its inverse and equally idempotent.

---

## 10. Module registration — `module.go` + wiring

```go
//go:embed migrations/*.sql
var MigrationsFS embed.FS

func (m *Module) Name() string                       { return "garden" }
func (m *Module) RegisterRoutes(r chi.Router)        { m.handler.Mount(r) }
func (m *Module) Migrations() fs.FS                  { return MigrationsFS }
func (m *Module) Widgets() []registry.WidgetProvider { return m.widgets }
func (m *Module) MetricProvider() metrics.Provider   { return m.metrics }
func (m *Module) ListProvider() lists.Provider       { return m.lists }
func (m *Module) AuditActions() []string {
	return []string{
		"plant.create", "plant.update", "plant.delete",
		"variety.create", "variety.update", "variety.delete",
		"bed.create", "bed.update", "bed.delete", "bed.move",
		"season.create", "season.update", "season.close", "season.reopen",
		"planting.create", "planting.update", "planting.delete",
		"task.create", "task.update", "task.delete",
		"harvest.create", "harvest.update", "harvest.delete",
		"storage.create", "storage.update", "storage.delete",
		"rule.create", "rule.update", "rule.delete",
		"settings.update", "frost_warning",
	}
}
```

**Exact wiring edits** — the only files outside `modules/garden/` the backend touches:

| File | Edit |
|---|---|
| `internal/bootstrap/bootstrap.go` | add `{Name: "garden", FS: garden.MigrationsFS}` to `MigrationSources()`; add the garden seed to `MigrationSourcesWithSeed()` (§13) |
| `cmd/home/main.go` | `gardenSvc := garden.NewService(sqldb, sink, notify, cfg)` · `gardenMod := garden.NewModule(gardenSvc)` · add to `featureModules`, `registry.CollectWidgets`, `metrics.Collect`, `lists.Collect` · register the weather job with the scheduler (§14) |
| `internal/platform/audit/audit.go` | add **`ModuleGarden = "garden"`** to the module constants — every module passes the constant, never a literal |
| `internal/modules/admin/listener.go` | add **`case audit.ModuleGarden: return "/zahrada"`** to `inAppURL` |
| `internal/platform/scheduler/scheduler.go` | **one additive hook** — see §14 |

**Four host-side maps are NOT registry-driven and each silently no-ops if skipped** — v5 and v6 both tripped on this: the frontend **widget registry** (`platform/widgets/registry.tsx`), the **"Více" overflow** in `AppShell`, the **Log browser's `MODULES`** array, and **`inAppURL`** above. `internal/arch`'s `TestModulesDoNotImportEachOther` must stay green; `garden` imports `platform/*` only. **It must not import `platform/push` or `platform/blobstore`** — add that as an explicit assertion (§19), because both are the kind of thing a later "small improvement" reaches for.

---

## 11. Widget provider — `prace.go` (FR-G16, D123)

```
Key() = "garden.prace"   Title() = "Práce na zahradě"   Module() = "garden"
Description() = "Co je potřeba udělat na zahradě"
DefaultSize() = "wide"   AdminOnly() = false
```

`Data(ctx, u, asOf)` returns open tasks whose **window overlaps** `[asOf, asOf+30d]` — overlaps, not starts inside, or every job already under way disappears from the dashboard on the day it becomes urgent. Overdue first, then grouped by ISO week; each line carries `title_cs`, `bed_code`, the window and `overdue`. Empty state payload `{state:"quiet"}` → *"na zahradě je teď klid"*.

Mark-done calls `POST /api/garden/tasks/{id}/complete` with `meta.via="dashboard"` through the house **2000 ms hold** gesture (§10 D22). **There is no second widget** (D123) — harvest surfaces here as a `harvest` task.

---

## 12. Metric + list providers — `metrics.go`, `lists.go`

All six metrics are `ScopeHousehold`. **Use the `asOf` the registry passes; never call `time.Now()` inside a provider**, or a summary firing at 00:01 disagrees with the widget.

| Key | Label | Unit | Value |
|---|---|---|---|
| `garden.tasks_due_7d` | Práce na zahradě (7 dní) | úkolů | open tasks overlapping `[asOf, asOf+7d]` |
| `garden.tasks_overdue` | Zmeškané práce | úkolů | open tasks with `window_to < asOf` |
| `garden.plan_warnings` | Varování v plánu | varování | non-dismissed `error`+`warn` in the current season |
| `garden.harvest_season` | Letošní sklizeň | kg | Σ current-season harvests **in kg only** — rows in ks/l/svazek are excluded, and the UI says so rather than adding apples to bunches |
| `garden.beds_unplanned` | Nezaplánované záhony | záhonů | active beds with no planting this season |
| `garden.frost_risk_tonight` | Noční minimum | °C | tonight's cached forecast minimum; **null** when no forecast is cached |

Lists mirror the four countable keys **through the same store function** so a count and its list can never disagree (D77), plus two **list-only** keys (the D100 precedent): `garden.harvest_ready` (empty: *"nic není ke sklizni"*) and `garden.frost_sensitive_now` (empty: *"nic citlivého venku"*, items formatted *"rajčata (A1)"*).

**What this unlocks, and it is worth telling Karel when it ships:** a schedule on 1 February at 09:00 conditioned `garden.plan_warnings gt 0`, body `V plánu jsou varování: {{list.garden.plan_warnings}}` — silent once the plan is clean. And the frost alert in §14, which needs no code in this module at all.

---

## 13. The rule seed — `10900_garden_seed.sql` (D115)

**The problem to avoid** is v6's, exactly: a seed in the normal sequence lands built-in rules in **every test database**, because `testsupport.NewDB` builds its schema from `bootstrap.MigrationFS()`. A C1 fixture would then pass because a *seeded* rule matched, not the one the test wrote — a false green that is very hard to see.

So: a **separate embedded migration source** (`garden/seed/`), included by `MigrationSourcesWithSeed()` and excluded by `testsupport`. `INSERT OR IGNORE` against the unique index, so re-running is a no-op rather than a duplicate-key failure. Log inserted-vs-skipped counts.

Contents: the **families with default break years** (Brassicaceae 4, Solanaceae 4, Apiaceae 3, Fabaceae 2, Cucurbitaceae 3, Amaryllidaceae 3, Amaranthaceae 3, Asteraceae 2, others 2) as `succession` rules, plus **~50–80 crop-pair companions**, each with `is_builtin = 1`, a `severity`, a Czech `reason_cs` and a **`source`**. Companion-planting literature contradicts itself; the `source` column is how agronomy and folklore stay tellable apart, and it is why a built-in can be **disabled but never deleted** (D130) — you can always see what you did not type.

---

## 14. Weather + frost publication (D112, D113) — **the module sends no push**

**One additive platform hook.** `platform/scheduler` currently only fires admin summaries. Add a generic job registration beside it — no change to the existing behaviour:

```go
// platform/scheduler
func (s *Scheduler) RegisterJob(name string, every time.Duration, fn func(ctx context.Context, now time.Time))
```

Jobs run on the same Prague-local minute ticker with the same fire-once semantics. `garden` registers `garden.weather` at `HOME_GARDEN_WEATHER_POLL_HOURS` (default 12). *(This corrects PRD §V7-1's "no change to `platform/*`" — it is one exported function, and the alternative, a second ad-hoc ticker inside a feature module, is exactly what v5 created this package to avoid.)*

The job:

1. Read `garden_settings`. **No coordinates ⇒ do nothing** (not an error — a garden that has not set its location simply has no forecast).
2. `GET {HOME_GARDEN_WEATHER_URL}?latitude=…&longitude=…&daily=temperature_2m_min,temperature_2m_max,precipitation_sum&timezone=Europe/Prague`, hard timeout ~10 s. Upsert into `garden_weather_days`; prune beyond 90 days.
3. **Any failure is logged and swallowed.** No user-visible error, no retry storm, no toast. A forecast that did not load is not something anyone can act on.
4. Evaluate the frost condition: tonight's `temp_min ≤ settings.frost_threshold_c` **and** at least one planting whose crop is `tender`/`half_hardy` has an open occupancy window. If so — and **only if no `garden.frost_warning` event exists for that date** — write one audit event, `actor_type = "system"` (the precedent is `logging.prune`), whose Czech `summary` is already a finished notification: *"Dnes v noci −2 °C. Citlivé: rajčata (A1), papriky (A2), cukety (B1)."* Autumn mirror: first-frost forecast within `frost_lookahead_days`.

**That is the entire output.** `garden` imports **no `platform/push`**, stores **no audience**, and has **no notification settings** (D113). Karel then chooses in **Administrace → Oznámení**, at runtime:

- a **scheduled summary** at, say, 17:00, condition `garden.frost_risk_tonight lte 2`, body `{{list.garden.frost_sensitive_now}}` — silent on every night there is nothing to say; **or**
- a **trigger rule** on `garden.frost_warning`, which fires when the poll flips and defaults its body to the event's summary.

Both work on day one because the module publishes for both. Do not add a third path.

---

## 15. The enum registry → prompt, import, export (D114, D126)

**One registry, three consumers.** `enums.go` declares every enum with its Czech label and its accepted aliases. From it, generate: (a) the JSON Schema the importer validates against, (b) the schema embedded in the LLM prompt, (c) `GET /api/garden/enums`. Hand-writing any of the three is how they drift, and a prompt that asks a model for a field the importer rejects is the specific failure this design exists to prevent.

- **`GET /api/garden/plants/prompt-template`** — Czech prompt + generated schema + the garden's context (frost dates, lat/lon, altitude) + "answer with JSON only". With `plant_id`, include the current values so the model fills gaps instead of overwriting.
- **`POST /api/garden/plants/import`** — object **or array**; each element validated independently. Enum matching is lenient over the aliases (Czech words map to members); an unmappable value is a **`422` naming field and value**, never a silent default. `dry_run=true` (the default) returns the parsed record, a **field-level diff** when it updates an existing row, and the input fields it could not map — reported, never dropped. Applying records `source=llm` + model + timestamp; the row is badged **"neověřeno"** until a member verifies it. Cap the payload and treat every string as untrusted: Markdown goes through the **same sanitiser as `notes`**.
- **`GET /api/garden/export`** — plants, varieties and rules in the importer's own shape. The round-trip is a test (§19), not an aspiration.

---

## 16. Frontend — `src/modules/garden/`

**Place it in `src/modules/garden/`, not `src/routes/`.** v6's finance pages followed the legacy `routes/` placement and that is an open cleanup item; do not add to it.

| Route | Page |
|---|---|
| `/zahrada` | Přehled — bed cards, warning badge, this month's work, harvest to date |
| `/zahrada/plodiny` | Plodiny — FTS list, detail, editor, prompt/import/export |
| `/zahrada/zahony` | Záhony — beds, **drag order (which is the adjacency)**, per-bed history |
| `/zahrada/plan/{rok}` | Plán — assignment, inline warnings, Kontrola plánu, copy-season, Uzavřít sezónu |
| `/zahrada/kalendar` | Kalendář — work by month/week, filters, **print target** |
| `/zahrada/sklizen` | Sklizeň — entry, yields vs expected |
| `/zahrada/sklad` | Sklad — stored produce, best-before, remaining |
| `/zahrada/trvalky` | Trvalky a dřeviny — permanents and their yearly care |

Query keys `['garden', <resource>, …]`. **A planting mutation invalidates the plan, the season's check and the affected tasks together** — a stale check is worse than no check, because it is a green tick that has stopped being true.

Nav: **Zahrada joins the "Více" overflow for everyone**, no admin gate, one destination with in-page sub-navigation; the four thumb tabs are untouched. Add it to `AppShell`'s `OVERFLOW` and the widget registry (§10).

**Print (D125).** One stylesheet, two targets: the month of work (real checkboxes, bed codes, windows, grouped by week) and the season plan on one page. This is the accepted answer to reads-only-offline in a garden with no signal, so treat it as a feature with a definition of done, not a `@media print` afterthought.

Offline: pages render read-only from the persisted TanStack Query cache; write controls disabled with the standard *"Změny nelze uložit offline"*.

---

## 17. Czech copy — `cs.ts` additions

The vocabulary is **fixed in PRD §V7-7** and used verbatim so the pages, the widget, the metric labels and the notification tokens all say the same words: **Zahrada · Plodina · Odrůda · Čeleď · Záhon · Část zahrady · Sezóna · Výsadba · Trvalka / dřevina · Kontrola plánu · Varování · Ignorovat · Práce · Termín výsevu · Poslední jarní mráz · první podzimní mráz · citlivá / polootužilá / otužilá · Nárok na živiny · Sklizeň · Sklad · Uzavřít sezónu · Neověřeno.**

Own the rest: every warning's `title_cs` and `detail_cs` (eleven checks × a fired message — these are the module's most-read strings, write them as sentences a person would say); the **"chybí historie"** state; the drift line; the season-close screen; the "neověřeno" badge and its tooltip; the import preview's diff and error copy. Counts need the **three plural forms** (*1 záhon · 2 záhony · 5 záhonů*, *1 práce · 2 práce · 5 prací*). Dates Czech-formatted; quantities with the crop's own unit.

---

## 18. Audit (D-arch, `HANDOFF-1`)

Every mutation writes in the same transaction. `garden_plant`, `garden_planting` and `garden_task` join the **field-diff set** — "who moved the tomato transplant date and to what" is the question the Log exists to answer here. `frost_warning` is the module's only **`system`**-actor event.

## 19. Security

No new inbound surface. Reads member-gated, writes `editor`/`admin` + CSRF, one admin-only route. Two things are new to Home and both are in this module:

- **Untrusted JSON from a model** (§15) — size-capped, schema-validated, enum-mapped explicitly, Markdown sanitised through the `notes` sanitiser, and applied only after a preview.
- **Outbound HTTP** (§14) — one fixed host, no credentials, no user data in the query beyond coordinates, hard timeout so a hanging forecast cannot hold a scheduler tick.

## 20. Tests

- **`timing_test.go`** — each anchor; a frost-anchored window moving when the season's frost date moves and a week-anchored one not moving; **the week-53 clamp**; `ok=false` on a missing anchor, and the caller leaving the date unset.
- **`resolve_test.go`** — full inheritance, full override, and the reflection test that fails when a new overridable column is not mirrored on `garden_varieties`.
- **`occupancy_test.go`** — overlap, non-overlap, open-ended, and the špenát/pórek case by name.
- **`check_test.go`** — **every check C1–C11 with a fixture that fires and one that must not.** C1's negative is two crops in one bed whose occupancies do not overlap. C3/C8 assert `no_history` on zero closed seasons. `plant_pair` beats `family_pair`. A disabled rule and a disabled check both emit nothing. Warning keys are stable across recomputation and **change when the season changes**.
- **`generate_test.go`** — move a planting's transplant date: an open generated task moves; `done`, `skipped` and `is_edited` do not; a deleted one stays deleted (tombstone); manual tasks are untouched. `water`/`weed` are never generated.
- **Service** — actual date changes no planned window (D119); `shift-tasks` moves open tasks and sets `is_edited`, after which regeneration leaves them alone; closed-season writes → `409`; reopen as `editor` → `403`; built-in rule `DELETE` → `409`, `PATCH is_disabled` → `200`; bed delete with plantings → `409`; exactly one of area/count → `422`.
- **Season copy** — plan reproduced, frost-anchored dates re-anchored and week-anchored ones not, `dry_run=true` persists nothing and returns both checks.
- **Weather/frost** — one `garden.frost_warning` per date survives a catch-up tick; disabled or failing fetch produces no error and a null metric; the event's summary names crops and beds.
- **Import/export** — valid JSON previews with a diff; unmappable enum `422` naming field and value; a 20-element array reports per-element status; **export re-imports to an equivalent state**.
- **Seed exclusion** — `SELECT COUNT(*) FROM garden_rules` is **0** on a fresh `testsupport.NewDB()`. This is the test that keeps §13 honest as the codebase changes.
- **Catalog agreement** — each countable metric equals `len(items)` of its list over a fixture.
- **Architecture** — `TestModulesDoNotImportEachOther` green, plus an explicit assertion that `garden` imports **neither `platform/push` nor `platform/blobstore`**.

## 21. Definition of done

- All PRD §V7-11 acceptance criteria pass; endpoints conform to `openapi.yaml` 0.9.0.
- The four pure functions (§2, §3, §5, §6) are each covered by their own table-driven tests, and nothing else re-implements them.
- Regeneration cannot destroy completed, skipped, edited or deleted work — proven by test, not by reading.
- The check reports `no_history` rather than passing, and the UI says so.
- `garden` registers through `registry.Module`; import-lint green; **no push, no blobstore**.
- Migrations run `… finance(09) → garden(10)` on an empty DB and after a Litestream restore; a fresh test DB has **zero** seeded rules.
- Widget in the catalog for every role, both states, 2000 ms hold; six metrics and six lists in the admin composer; `garden.*` in the log filter.
- A frost alert reaches a phone **configured entirely in Administrace**, with no notification code in this module.
- Frontend complete in Czech per §17, family colour **always paired with a label or pattern**, no new colour values; Zahrada in "Více" for all roles; print targets produced.
- Live sync toast: *"Zahrada byla mezitím upravena"*.

## 22. Module packaging

```
backend/internal/modules/garden/
    module.go        # registry.Module: routes, migrations, audit actions, widget, providers
    http.go          # chi mount + handlers, role gates, dry_run plumbing
    service.go       # validation, WithTx + audit-in-tx, closed-season guard, notify
    store.go         # SQL incl. SeasonSnapshot and BedHistory
    timing.go        # anchored windows  ← build first
    resolve.go       # species + variety  ← build second
    occupancy.go     # the derived window
    check.go         # C1–C11 over a snapshot, pure
    generate.go      # task generation + regeneration guard
    weather.go       # the scheduler job + frost evaluation (no push)
    enums.go         # the one enum registry → schema, prompt, /enums
    importer.go      # validate, preview, apply, export
    metrics.go lists.go prace.go
    types.go
    *_test.go
    migrations/10001_garden.sql
    seed/
        embed.go
        migrations/10900_garden_seed.sql
frontend/src/modules/garden/
    pages/  widgets/  api/  print.css
```

Build the four pure functions first, the seed plumbing before the frontend, and resist every opportunity to give this module its own notification path.
