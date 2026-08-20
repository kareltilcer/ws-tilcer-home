-- garden module — Zahrada tables (PRD §V7-5, HANDOFF-9 §1). Version block 10000:
-- applied last in the one Goose sequence (… admin(08) → finance(09) → garden(10),
-- see bootstrap.MigrationSources). NO CHANGE to any existing table.
--
-- Eleven tables plus a weather cache. NOTHING IS SEEDED HERE — the built-in
-- compatibility rules arrive through a SEPARATE migration source (block 10900,
-- PRD D115), which the server entrypoint includes and testsupport must not. A
-- module test whose database is pre-loaded with rules would let a check fixture
-- pass for the wrong reason.
--
-- No blob storage (D122): this is the second module after finance to hold no
-- bytes. The tables ride the existing Litestream `home/` replication.
--
-- House conventions: UUIDv7 `id`, `deleted_at` soft delete, `created_by` /
-- `created_at` / `updated_at`, lexorank `position` where users order things,
-- dates as TEXT `YYYY-MM-DD` in HOME_TIMEZONE.

-- +goose Up

-- ============================ knowledge base ============================

-- `family` and `hardiness` are NOT NULL on purpose (D104, D111): the rotation
-- engine joins on the first and the frost logic reads the second. A crop missing
-- either is one this module cannot reason about, so it is refused at write time
-- rather than defaulted to something plausible.
--
-- `seq` is the FTS5 external-content rowid; `id` stays the logical TEXT key. The
-- same shape as notes/documents, for the same reason — see garden_plants_fts.
-- +goose StatementBegin
CREATE TABLE garden_plants (
    seq                    INTEGER PRIMARY KEY,
    id                     TEXT NOT NULL UNIQUE,
    name_cs                TEXT NOT NULL,
    name_latin             TEXT,
    family                 TEXT NOT NULL,
    plant_type             TEXT NOT NULL,
    hardiness              TEXT NOT NULL,
    feeder_class           TEXT,
    root_depth             TEXT,
    sun                    TEXT,
    water_need             TEXT,
    soil_ph_min            REAL,
    soil_ph_max            REAL,
    rotation_break_years   INTEGER,           -- NULL = inherit the family default
    sow_method             TEXT,
    -- The care flags are NULLABLE here, matching garden_varieties column for
    -- column. One Go struct (PlantCore) and one SQL column list serve both
    -- tables, which is what stops the species and the variety drifting apart —
    -- and that only works if the two DDLs agree.
    --
    -- On a species NULL and 0 mean the same thing ("no, it does not need
    -- support"), and every reader goes through derefB, which maps both to false.
    -- On a variety NULL means "inherit" and 0 means an explicit "no". Same
    -- column, two readings — see corecols.go.
    needs_pricking_out     INTEGER,
    needs_support          INTEGER,   -- drives the `support` task
    wants_mulch            INTEGER,   -- drives the `mulch` task
    wants_pest_check       INTEGER,   -- drives the `pest_check` task
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
    -- Four windows x (anchor, from, to). Anchor: week | last_frost | first_frost
    -- (D102). Stored as three columns rather than JSON because every one of them
    -- is read by the resolver on every plan load.
    win_sow_indoor_anchor  TEXT, win_sow_indoor_from  INTEGER, win_sow_indoor_to  INTEGER,
    win_sow_direct_anchor  TEXT, win_sow_direct_from  INTEGER, win_sow_direct_to  INTEGER,
    win_transplant_anchor  TEXT, win_transplant_from  INTEGER, win_transplant_to  INTEGER,
    win_harvest_anchor     TEXT, win_harvest_from     INTEGER, win_harvest_to     INTEGER,
    harvest_unit           TEXT,
    yield_per_m2           REAL,
    yield_per_plant        REAL,
    storage_methods        TEXT,              -- JSON array of enum members
    storage_temp_c         INTEGER,
    storage_humidity       INTEGER,
    shelf_life_days        INTEGER,
    pests                  TEXT,              -- JSON [{name,symptom,remedy_md}]
    diseases               TEXT,
    notes_md               TEXT,
    -- search_blob flattens the pest and disease NAMES for FTS5, which cannot
    -- index inside the JSON columns above. Written by the store on every save.
    search_blob            TEXT,
    -- Provenance (D114): an LLM-filled crop nobody has checked must LOOK
    -- different from one a human confirmed.
    source                 TEXT NOT NULL DEFAULT 'manual',
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
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_garden_plants_name ON garden_plants (name_cs) WHERE deleted_at IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_garden_plants_family ON garden_plants (family) WHERE deleted_at IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_garden_plants_type ON garden_plants (plant_type) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- Varieties carry a NULLABLE MIRROR of every inherited column; NULL = inherit
-- (D103). The Go side shares ONE PlantCore struct between species and variety,
-- so the mirror cannot fall out of sync in code — this table is its storage.
-- +goose StatementBegin
CREATE TABLE garden_varieties (
    id                     TEXT PRIMARY KEY,
    plant_id               TEXT NOT NULL REFERENCES garden_plants (id),
    name                   TEXT NOT NULL,
    supplier               TEXT,
    description_md         TEXT,
    is_favourite           INTEGER NOT NULL DEFAULT 0,
    retired                INTEGER NOT NULL DEFAULT 0,
    -- every overridable column from garden_plants, all NULLABLE
    name_latin             TEXT,
    feeder_class           TEXT,
    root_depth             TEXT,
    sun                    TEXT,
    water_need             TEXT,
    soil_ph_min            REAL,
    soil_ph_max            REAL,
    rotation_break_years   INTEGER,
    sow_method             TEXT,
    needs_pricking_out     INTEGER,
    needs_support          INTEGER,
    wants_mulch            INTEGER,
    wants_pest_check       INTEGER,
    hardening_days         INTEGER,
    sow_depth_cm           REAL,
    spacing_row_cm         REAL,
    spacing_plant_cm       REAL,
    plants_per_m2          REAL,
    days_to_germinate_min  INTEGER,
    days_to_germinate_max  INTEGER,
    germination_temp_c     INTEGER,
    days_to_maturity_min   INTEGER,
    days_to_maturity_max   INTEGER,
    win_sow_indoor_anchor  TEXT, win_sow_indoor_from  INTEGER, win_sow_indoor_to  INTEGER,
    win_sow_direct_anchor  TEXT, win_sow_direct_from  INTEGER, win_sow_direct_to  INTEGER,
    win_transplant_anchor  TEXT, win_transplant_from  INTEGER, win_transplant_to  INTEGER,
    win_harvest_anchor     TEXT, win_harvest_from     INTEGER, win_harvest_to     INTEGER,
    harvest_unit           TEXT,
    yield_per_m2           REAL,
    yield_per_plant        REAL,
    storage_methods        TEXT,
    storage_temp_c         INTEGER,
    storage_humidity       INTEGER,
    shelf_life_days        INTEGER,
    pests                  TEXT,
    diseases               TEXT,
    notes_md               TEXT,
    source                 TEXT NOT NULL DEFAULT 'manual',
    source_model           TEXT,
    source_at              TEXT,
    verified_by            TEXT,
    verified_at            TEXT,
    deleted_at             TEXT,
    created_by             TEXT,
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_garden_varieties_name ON garden_varieties (plant_id, name) WHERE deleted_at IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_garden_varieties_plant ON garden_varieties (plant_id) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- ================================ beds ================================

-- NO NEIGHBOUR TABLE: adjacency is (zone, position) adjacency (D117). The index
-- below is not a performance index, it IS the adjacency model — do not drop it.
-- Two beds are neighbours iff they are consecutive in one zone, which is why the
-- Záhony screen's drag order changes warnings rather than only tidiness.
-- +goose StatementBegin
CREATE TABLE garden_beds (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    code          TEXT NOT NULL,
    type          TEXT NOT NULL,
    length_cm     REAL,
    width_cm      REAL,
    area_m2       REAL NOT NULL CHECK (area_m2 > 0),
    sun_exposure  TEXT,
    zone          TEXT,
    soil_notes_md TEXT,
    is_active     INTEGER NOT NULL DEFAULT 1,
    position      TEXT NOT NULL,
    deleted_at    TEXT,
    created_by    TEXT,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_garden_beds_zone_pos ON garden_beds (zone, position) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- =============================== seasons ===============================

-- A season's identity IS its year (D128): unique, immutable, user-visible, and
-- what /zahrada/plan/2027 maps onto. The id stays the internal key.
-- +goose StatementBegin
CREATE TABLE garden_seasons (
    id                    TEXT PRIMARY KEY,
    year                  INTEGER NOT NULL,
    status                TEXT NOT NULL DEFAULT 'planning',  -- planning|active|closed
    last_frost_on         TEXT,
    first_frost_on        TEXT,
    last_frost_actual_on  TEXT,
    first_frost_actual_on TEXT,
    notes_md              TEXT,
    closed_at             TEXT,
    closed_by             TEXT,
    created_by            TEXT,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_garden_seasons_year ON garden_seasons (year);
-- +goose StatementEnd

-- ============================== plantings ==============================

-- season_id NULL ⇒ PERMANENT (a tree, a rhubarb crown) — ONE TABLE, one code
-- path for occupancy, warnings, tasks and harvests (D106). Trvalky is a filtered
-- view, not a second entity.
--
-- A planting belongs to the season of its HARVEST (D105): česnek sown in October
-- 2026 is a 2027 planting, and its sow date legitimately falls in the previous
-- calendar year. Nothing here constrains the dates to the season's year.
-- +goose StatementBegin
CREATE TABLE garden_plantings (
    id              TEXT PRIMARY KEY,
    season_id       TEXT REFERENCES garden_seasons (id),
    bed_id          TEXT REFERENCES garden_beds (id),
    location_label  TEXT,
    plant_id        TEXT NOT NULL REFERENCES garden_plants (id),
    variety_id      TEXT REFERENCES garden_varieties (id),
    area_m2         REAL,
    plant_count     INTEGER,
    rows            INTEGER,
    -- planned
    sow_indoor_on   TEXT,
    sow_direct_on   TEXT,
    transplant_on   TEXT,
    harvest_from    TEXT,
    harvest_to      TEXT,
    manual_dates    TEXT NOT NULL DEFAULT '[]',   -- JSON array of planned-date field names
    -- actual: recording one of these changes NO planned window (D119)
    sowed_on        TEXT,
    transplanted_on TEXT,
    first_harvest_on TEXT,
    cleared_on      TEXT,
    status          TEXT NOT NULL DEFAULT 'planned',
    fail_reason     TEXT,
    -- permanents only
    planted_on      TEXT,
    rootstock       TEXT,
    removed_on      TEXT,
    notes_md        TEXT,
    deleted_at      TEXT,
    created_by      TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    CHECK ((area_m2 IS NULL) <> (plant_count IS NULL))   -- exactly one
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_garden_plantings_season_bed ON garden_plantings (season_id, bed_id) WHERE deleted_at IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_garden_plantings_bed ON garden_plantings (bed_id) WHERE deleted_at IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_garden_plantings_plant ON garden_plantings (plant_id) WHERE deleted_at IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_garden_plantings_permanent ON garden_plantings (bed_id)
    WHERE season_id IS NULL AND deleted_at IS NULL;
-- +goose StatementEnd

-- ================================ tasks ================================

-- Every task carries an od–do WINDOW, not a due date: "vysaď rajčata mezi 15. a
-- 25. květnem" is the truth, and a calendar that draws it as a point is lying.
-- +goose StatementBegin
CREATE TABLE garden_tasks (
    id             TEXT PRIMARY KEY,
    kind           TEXT NOT NULL,
    season_id      TEXT REFERENCES garden_seasons (id),
    planting_id    TEXT REFERENCES garden_plantings (id),
    bed_id         TEXT REFERENCES garden_beds (id),
    title_cs       TEXT NOT NULL,
    window_from    TEXT NOT NULL,
    window_to      TEXT NOT NULL,
    due_hint       TEXT,
    status         TEXT NOT NULL DEFAULT 'open',   -- open|done|skipped
    completed_by   TEXT,
    completed_at   TEXT,
    is_generated   INTEGER NOT NULL DEFAULT 0,
    -- is_edited is the user saying "I have decided this". D110 then protects the
    -- task from regeneration forever.
    is_edited      INTEGER NOT NULL DEFAULT 0,
    generation_key TEXT,
    -- suppressed is the TOMBSTONE (D110): a generated task the user deleted keeps
    -- its row so the key stays taken and it cannot resurrect on the next
    -- recompute. A calendar that quietly undoes your work is one you stop opening.
    suppressed     INTEGER NOT NULL DEFAULT 0,
    notes_md       TEXT,
    position       TEXT NOT NULL,
    deleted_at     TEXT,
    created_by     TEXT,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    CHECK (window_to >= window_from)
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_garden_tasks_window ON garden_tasks (window_from) WHERE deleted_at IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_garden_tasks_status ON garden_tasks (status, window_to) WHERE deleted_at IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_garden_tasks_planting ON garden_tasks (planting_id) WHERE deleted_at IS NULL;
-- +goose StatementEnd
-- One LIVE task per generation identity. Tombstones are excluded so a suppressed
-- key can coexist with nothing — and a re-created task cannot collide with it.
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_garden_tasks_genkey ON garden_tasks (generation_key)
    WHERE generation_key IS NOT NULL AND suppressed = 0 AND deleted_at IS NULL;
-- +goose StatementEnd

-- ========================= harvest and storage =========================

-- +goose StatementBegin
CREATE TABLE garden_harvests (
    id           TEXT PRIMARY KEY,
    planting_id  TEXT NOT NULL REFERENCES garden_plantings (id),
    harvested_on TEXT NOT NULL,
    quantity     REAL NOT NULL CHECK (quantity >= 0),
    unit         TEXT NOT NULL,
    destination  TEXT,
    quality      TEXT,
    note         TEXT,
    deleted_at   TEXT,
    created_by   TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_garden_harvests_planting ON garden_harvests (planting_id) WHERE deleted_at IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_garden_harvests_date ON garden_harvests (harvested_on) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- Garden produce only (D121), not a general pantry. Consumption is recorded by
-- editing quantity_remaining IN PLACE: the audit spine's field diffs already
-- answer "when did we eat the last jar", and a movements table would buy only a
-- chart.
-- +goose StatementBegin
CREATE TABLE garden_storage_items (
    id                 TEXT PRIMARY KEY,
    harvest_id         TEXT REFERENCES garden_harvests (id),
    planting_id        TEXT REFERENCES garden_plantings (id),
    product_name       TEXT NOT NULL,
    method             TEXT NOT NULL,
    location           TEXT,
    quantity_initial   REAL NOT NULL CHECK (quantity_initial >= 0),
    quantity_remaining REAL NOT NULL CHECK (quantity_remaining >= 0),
    unit               TEXT NOT NULL,
    stored_on          TEXT NOT NULL,
    best_before        TEXT,
    status             TEXT NOT NULL DEFAULT 'stored',
    note               TEXT,
    deleted_at         TEXT,
    created_by         TEXT,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    CHECK (quantity_remaining <= quantity_initial)
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_garden_storage_status ON garden_storage_items (status, best_before) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- ================== rules, dismissals, settings, weather ==================

-- Pairs are stored in CANONICAL (sorted) order so symmetry is STRUCTURAL: the
-- matcher looks up one row, not two, and nobody has to remember to enter both
-- directions. `source` is how agronomy and folklore stay tellable apart, and it
-- is why a built-in can be disabled but never deleted (D130).
-- +goose StatementBegin
CREATE TABLE garden_rules (
    id            TEXT PRIMARY KEY,
    scope         TEXT NOT NULL,               -- plant_pair|family_pair|succession
    a_ref         TEXT NOT NULL,
    b_ref         TEXT NOT NULL,
    verdict       TEXT NOT NULL,               -- good|bad
    severity      TEXT NOT NULL DEFAULT 'warn',
    min_years_gap INTEGER,
    reason_cs     TEXT,
    source        TEXT,
    is_builtin    INTEGER NOT NULL DEFAULT 0,
    is_disabled   INTEGER NOT NULL DEFAULT 0,
    deleted_at    TEXT,
    created_by    TEXT,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_garden_rules ON garden_rules (scope, a_ref, b_ref) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- Warnings are COMPUTED (D108); only the dismissals are stored. The key carries
-- the season year, so a copied season does not inherit last year's "letos to
-- risknu" — see check.go's warningKey.
-- +goose StatementBegin
CREATE TABLE garden_warning_dismissals (
    id           TEXT PRIMARY KEY,
    season_id    TEXT NOT NULL REFERENCES garden_seasons (id),
    warning_key  TEXT NOT NULL,
    note         TEXT,
    dismissed_by TEXT,
    dismissed_at TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_garden_dismissals ON garden_warning_dismissals (season_id, warning_key);
-- +goose StatementEnd

-- One settings row. Coordinates live HERE and not in Coolify (D112): they are
-- user data rather than secrets, and a typo should not need a redeploy. There are
-- deliberately NO audience or notification settings — that is Administrace's
-- (D113).
-- +goose StatementBegin
CREATE TABLE garden_settings (
    id                      INTEGER PRIMARY KEY CHECK (id = 1),
    default_last_frost      TEXT,     -- MM-DD
    default_first_frost     TEXT,     -- MM-DD
    latitude                REAL,
    longitude               REAL,
    altitude_m              INTEGER,
    rotation_break_default  INTEGER NOT NULL DEFAULT 4,
    frost_threshold_c       REAL NOT NULL DEFAULT 2,
    frost_lookahead_days    INTEGER NOT NULL DEFAULT 3,
    workload_week_threshold INTEGER NOT NULL DEFAULT 10,
    checks_config           TEXT NOT NULL DEFAULT '{}',
    updated_at              TEXT NOT NULL
);
-- +goose StatementEnd
-- The single row is created here rather than lazily, so every read is a plain
-- SELECT and no write path has to remember to upsert it first.
-- +goose StatementBegin
INSERT INTO garden_settings (id, updated_at) VALUES (1, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'));
-- +goose StatementEnd

-- A cache, not a feature. The forecast exists to answer one question — is tonight
-- a frost risk — and is otherwise invisible (D112/D113).
-- +goose StatementBegin
CREATE TABLE garden_weather_days (
    day        TEXT PRIMARY KEY,
    temp_min   REAL,
    temp_max   REAL,
    precip_mm  REAL,
    fetched_at TEXT NOT NULL,
    source     TEXT NOT NULL
);
-- +goose StatementEnd

-- ================================= FTS =================================

-- Same shape as notes_fts / documents_fts — external content over `seq`, with
-- diacritics folded so "rajce" finds "rajče". `search_blob` flattens the pest and
-- disease NAMES into one column at write time: they live in JSON, and FTS5 cannot
-- index inside it.
-- +goose StatementBegin
CREATE VIRTUAL TABLE garden_plants_fts USING fts5 (
    name_cs,
    name_latin,
    notes_md,
    search_blob,
    content='garden_plants',
    content_rowid='seq',
    tokenize='unicode61 remove_diacritics 2'
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER garden_plants_ai AFTER INSERT ON garden_plants BEGIN
    INSERT INTO garden_plants_fts (rowid, name_cs, name_latin, notes_md, search_blob)
    VALUES (new.seq, new.name_cs, new.name_latin, new.notes_md, new.search_blob);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER garden_plants_ad AFTER DELETE ON garden_plants BEGIN
    INSERT INTO garden_plants_fts (garden_plants_fts, rowid, name_cs, name_latin, notes_md, search_blob)
    VALUES ('delete', old.seq, old.name_cs, old.name_latin, old.notes_md, old.search_blob);
END;
-- +goose StatementEnd
-- Re-index only when a searchable column actually changes. A crop row is UPDATEd
-- by far more than edits to its text — verification, provenance and every
-- agronomic field bump updated_at — and re-tokenizing on each is wasted work.
-- `IS NOT` is SQLite's NULL-safe distinct operator (these columns are nullable,
-- so `<>` would miss NULL↔value transitions).
-- +goose StatementBegin
CREATE TRIGGER garden_plants_au AFTER UPDATE ON garden_plants
WHEN old.name_cs IS NOT new.name_cs
  OR old.name_latin IS NOT new.name_latin
  OR old.notes_md IS NOT new.notes_md
  OR old.search_blob IS NOT new.search_blob
BEGIN
    INSERT INTO garden_plants_fts (garden_plants_fts, rowid, name_cs, name_latin, notes_md, search_blob)
    VALUES ('delete', old.seq, old.name_cs, old.name_latin, old.notes_md, old.search_blob);
    INSERT INTO garden_plants_fts (rowid, name_cs, name_latin, notes_md, search_blob)
    VALUES (new.seq, new.name_cs, new.name_latin, new.notes_md, new.search_blob);
END;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS garden_plants_fts;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS garden_weather_days;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS garden_settings;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS garden_warning_dismissals;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS garden_rules;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS garden_storage_items;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS garden_harvests;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS garden_tasks;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS garden_plantings;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS garden_seasons;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS garden_beds;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS garden_varieties;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS garden_plants;
-- +goose StatementEnd
