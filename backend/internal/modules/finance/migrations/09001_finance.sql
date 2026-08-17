-- +goose Up
-- +goose StatementBegin
-- finance module — Finance tables (PRD §V6-5, HANDOFF-8 §1). Version block 09000:
-- applied last in the one Goose sequence (… documents(07) → admin(08) → finance(09),
-- see bootstrap.MigrationSources). Nothing is seeded HERE — the historic months from
-- the retiring `fin` service arrive through a SEPARATE migration source (block 09900,
-- PRD D91), which the server entrypoint includes and testsupport must not.
--
-- Column names are carried over verbatim from `fin` (PRD D83). Only the table is
-- namespaced: home is one database holding eight modules' tables, and a bare
-- `months` would be the most collision-prone name in it.
--
-- Inputs only. The nine-value split is DERIVED ON READ (PRD D82) — storing it
-- would create a second source of truth for a formula that is deliberately locked.
CREATE TABLE finance_months (
    id               TEXT PRIMARY KEY,                    -- UUIDv7; seeded rows keep fin's id
    month            TEXT NOT NULL,                       -- YYYY-MM
    income_kaja      INTEGER NOT NULL CHECK (income_kaja >= 0),
    income_andy      INTEGER NOT NULL CHECK (income_andy >= 0),
    rate_personal    INTEGER NOT NULL CHECK (rate_personal >= 0),
    rate_operational INTEGER NOT NULL CHECK (rate_operational >= 0),
    rate_fun         INTEGER NOT NULL CHECK (rate_fun >= 0),
    rate_nofun       INTEGER NOT NULL CHECK (rate_nofun >= 0),
    created_by       TEXT,                                -- NULL for seeded rows (fin recorded no actor)
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    -- Redundant with the service's validation for normal writes, and that is not
    -- why it is here: it makes a bad SEED row fail at migration time, loudly,
    -- instead of at read time, quietly.
    CHECK (rate_personal + rate_operational + rate_fun + rate_nofun = 100)
);
-- +goose StatementEnd

-- Delete is HARD (PRD D87) — no deleted_at, so a plain unique index, exactly as
-- fin's 0001_init had it: there is no soft-deleted row for a partial index to
-- exclude. The month.delete audit event carries a full-row diff, which is what
-- makes an unrecoverable delete acceptable here.
--
-- This is the table's ONLY index, and it is also what serves the one read that
-- sorts: Store.List's `ORDER BY month DESC LIMIT ?` walks it backwards. A
-- separate (month DESC) index would be a second B-tree over the same column,
-- maintained on every write, changing no query plan.
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_finance_months_month ON finance_months (month);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS finance_months;
-- +goose StatementEnd
