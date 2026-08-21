-- electricity module — Elektřina tables (PRD §V8-5, HANDOFF-10 §1). Version
-- block 11000: applied last in the one Goose sequence (… finance(09) →
-- garden(10) → electricity(11), see bootstrap.MigrationSources). NO CHANGE to
-- any existing table.
--
-- THIS IS THE MODULE'S ONLY MIGRATION (PRD D152). There is NO seed source, and
-- deliberately so: unlike finance (block 09900) and garden (10900) there is no
-- historic data to carry and no built-in knowledge to preload — Karel's meter
-- starts at 32 / 70 kWh on the period's first day and whatever history exists is
-- typed in as ordinary readings. Do NOT add a seed source for symmetry with the
-- two modules that have one; there is nothing for testsupport to exclude.
--
-- No blob storage and no settings table. The three things a settings screen
-- would hold — prices, záloha, period — are all first-class DATE-VERSIONED
-- entities, because in this module "the current value" is never the whole truth.
--
-- UNITS ARE ENFORCED BY THE COLUMN NAMES (PRD D148): energy is INTEGER tenths of
-- kWh (`*_dkwh`), money is INTEGER haléře (`*_haler`). Neither alternative
-- works — a ceník price of 4 858,65 Kč/MWh would lose money as finance's whole
-- koruny, and a float would lose the determinism of a formula whose entire point
-- is that two people can reproduce it. No float reaches this database.
--
-- House conventions: UUIDv7 `id`, `deleted_at` soft delete, `created_by` /
-- `created_at` / `updated_at`, dates as TEXT `YYYY-MM-DD` in HOME_TIMEZONE.
-- There is NO lexorank `position` anywhere here: every collection in this module
-- is chronological, and a draggable order over dates would be a second,
-- contradictory truth about which reading comes first.

-- +goose Up

-- ============================== odečty ==============================

-- A reading is the state of the meter at 00:00 of `read_on` (PRD D134). That one
-- sentence is what every boundary rule in the module is a corollary of:
-- consumption of day d = reading(d+1) − reading(d), an interval between readings
-- on d1 and d2 covers days d1 … d2−1, and a period is CLOSED by the reading
-- dated ends_on + 1 — the same reading that opens the next period.
-- +goose StatementBegin
CREATE TABLE electricity_readings (
    id         TEXT PRIMARY KEY,
    read_on    TEXT NOT NULL,
    vt_dkwh    INTEGER NOT NULL CHECK (vt_dkwh >= 0),   -- vysoký tarif, tenths of kWh
    nt_dkwh    INTEGER NOT NULL CHECK (nt_dkwh >= 0),   -- nízký tarif, tenths of kWh
    note       TEXT,                                    -- plain text, NOT Markdown
    deleted_at TEXT,
    created_by TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (read_on GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]')
);
-- +goose StatementEnd

-- PARTIAL, not a table-level UNIQUE: a soft-deleted reading must not hold its
-- date hostage. Delete a mistyped odečet and you have to be able to enter the
-- right one for the same day.
--
-- This index also serves every read that matters: the list walks it backwards
-- (newest first), the keyset cursor pages on it, and store.Snapshot ranges over
-- [starts_on, ends_on+1] with it.
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_electricity_readings_day
    ON electricity_readings (read_on) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- ============================== ceník ==============================

-- Three numbers, all INCLUDING DPH AND DISTRIBUCE and used exactly as typed
-- (PRD D135). The itemized model — silová/distribuce per tariff, jistič,
-- systémové služby, POZE, OTE, daň z elektřiny, computed DPH — was specified in
-- full and rejected: ten fields to re-type every January would be ten chances to
-- mistype, in exchange for an itemization the household never reads.
--
-- THERE IS NO END DATE, and that is structural rather than an omission (D136). A
-- version governs every day >= effective_from until the next version starts; a
-- stored end would be a second source of truth that eventually disagrees with
-- the next row's start. It is also what makes "editing a version must not affect
-- anything before it" a property of the schema instead of a rule someone has to
-- remember.
-- +goose StatementBegin
CREATE TABLE electricity_tariffs (
    id                TEXT PRIMARY KEY,
    effective_from    TEXT NOT NULL,
    price_vt_haler    INTEGER NOT NULL CHECK (price_vt_haler >= 0),     -- Kč/MWh in haléře
    price_nt_haler    INTEGER NOT NULL CHECK (price_nt_haler >= 0),     -- Kč/MWh in haléře
    monthly_fee_haler INTEGER NOT NULL CHECK (monthly_fee_haler >= 0),  -- Kč/měsíc in haléře
    note              TEXT,
    deleted_at        TEXT,
    created_by        TEXT,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    CHECK (effective_from GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]')
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_electricity_tariffs_from
    ON electricity_tariffs (effective_from) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- ============================== zálohy ==============================

-- The schedule, versioned exactly like the ceník.
--
-- `due_day` is stored RAW, 1–31, and clamped to the month's last day AT READ
-- TIME (PRD D155, the §10 D74 precedent). A stored clamp turns a 31 into a 28
-- the first February it meets and stays wrong for every March afterwards. It is
-- read in exactly ONE place — whether a counted month is already zaplaceno — so
-- it moves the doporučená záloha and nothing else: not the period total, not the
-- counted months.
-- +goose StatementBegin
CREATE TABLE electricity_advances (
    id             TEXT PRIMARY KEY,
    effective_from TEXT NOT NULL,
    amount_haler   INTEGER NOT NULL CHECK (amount_haler >= 0),
    due_day        INTEGER NOT NULL CHECK (due_day BETWEEN 1 AND 31),
    note           TEXT,
    deleted_at     TEXT,
    created_by     TEXT,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    CHECK (effective_from GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]')
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_electricity_advances_from
    ON electricity_advances (effective_from) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- What was really paid, one optional row per month. A recorded payment WINS over
-- the schedule for its month (PRD D144), so the common case is zero typing and
-- the month where you paid something different is one row.
--
-- Attribution is by the `month` key and NOT by `paid_on`: a March záloha paid on
-- 2. dubna still belongs to March. `paid_on` is a record of when the money left,
-- nothing more — it is deliberately nullable and is never used to decide which
-- month a payment counts toward.
-- +goose StatementBegin
CREATE TABLE electricity_payments (
    id           TEXT PRIMARY KEY,
    month        TEXT NOT NULL,                                  -- YYYY-MM
    amount_haler INTEGER NOT NULL CHECK (amount_haler >= 0),
    paid_on      TEXT,
    note         TEXT,
    deleted_at   TEXT,
    created_by   TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    CHECK (month GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]'
           AND substr(month, 6, 2) BETWEEN '01' AND '12'),
    CHECK (paid_on IS NULL OR paid_on GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]')
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_electricity_payments_month
    ON electricity_payments (month) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- ========================= zúčtovací období =========================

-- User-set, inclusive [starts_on, ends_on], non-overlapping, and NEVER LOCKED
-- (PRD D139). There is no `closed`, no `locked` and no `archived` column, and
-- adding one later would be a change of policy rather than a schema tidy: the
-- audit spine is the compensating control here, exactly as in finance.
--
-- `ends_on` is an EXPECTED date until the supplier confirms it (D153) — they
-- frequently do not state one in advance, and a prediction has to project
-- somewhere. It defaults to starts_on + 1 year − 1 day with ends_on_confirmed
-- false; correcting it later is one field, after which every forecast figure
-- follows and no actual figure moves.
--
-- The four invoiced_* columns are the vyúčtování when it arrives (D154). Storing
-- the supplier's OWN final meter values, not just their money, is what lets a
-- discrepancy be attributed to kWh — which is how an odhadnutý odečet on their
-- side becomes visible instead of looking like a pricing surprise.
--
-- Non-overlap CANNOT be a CHECK: it is a cross-row constraint and SQLite has no
-- exclusion constraints. It is enforced in service.go inside the transaction.
-- +goose StatementBegin
CREATE TABLE electricity_periods (
    id                     TEXT PRIMARY KEY,
    starts_on              TEXT NOT NULL,
    ends_on                TEXT NOT NULL,
    ends_on_confirmed      INTEGER NOT NULL DEFAULT 0,
    invoiced_total_haler   INTEGER,
    invoiced_balance_haler INTEGER,   -- negative = nedoplatek, positive = přeplatek
    invoiced_vt_dkwh       INTEGER CHECK (invoiced_vt_dkwh IS NULL OR invoiced_vt_dkwh >= 0),
    invoiced_nt_dkwh       INTEGER CHECK (invoiced_nt_dkwh IS NULL OR invoiced_nt_dkwh >= 0),
    invoiced_at            TEXT,
    note                   TEXT,
    deleted_at             TEXT,
    created_by             TEXT,
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL,
    CHECK (starts_on GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    CHECK (ends_on   GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    CHECK (invoiced_at IS NULL OR invoiced_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    CHECK (ends_on >= starts_on)
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_electricity_periods_start
    ON electricity_periods (starts_on) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS electricity_periods;
DROP TABLE IF EXISTS electricity_payments;
DROP TABLE IF EXISTS electricity_advances;
DROP TABLE IF EXISTS electricity_tariffs;
DROP TABLE IF EXISTS electricity_readings;
-- +goose StatementEnd
