-- platform block — v10's two additions that belong to no feature module
-- (PRD §V10-5, HANDOFF-12 §9.1).
--
-- ⚠ THIS IS AN OUT-OF-ORDER GOOSE VERSION. `02004` is numerically below `11001`,
-- which production applied when v8 shipped, so goose sees it as a migration
-- missing from the middle of the sequence. That is fine and it is by design:
-- every module owns a numeric BLOCK inside one sequence, so a v10 addition to the
-- platform block necessarily lands below every later module's files. v9 shipped
-- exactly this shape with `01002`, `06004` and `07004`, and appdb.Migrate passes
-- goose.WithAllowMissing() for precisely this reason (see platform/db/migrate.go).
-- bootstrap/v10_migration_test.go is the test of it.
--
-- Both changes are ADDITIVE — one ALTER TABLE ADD COLUMN and one CREATE TABLE.
-- Neither rewrites a row, so neither can lose data. `08003` is the one that does,
-- and it is deliberately a separate file.

-- +goose Up

-- ---------------------------------------------------------------------------
-- cat_chat — the fourth mute bucket
-- ---------------------------------------------------------------------------
--
-- Chat push is its own category, not a reuse of `triggers` (D248). Recording chat
-- pushes under an existing bucket would let a member silence chat by muting
-- Administrace's trigger rules, or vice versa — two unrelated things behind one
-- switch, and no way to tell from the settings panel which one you just turned off.
--
-- DEFAULT 1 matches the other three: a missing preferences row means all-on
-- (push.Store.EligibleSubscriptions LEFT JOINs and COALESCEs), so a member who has
-- never opened the panel still gets notified, and an existing row gets the same
-- answer from the column default.
-- +goose StatementBegin
ALTER TABLE notification_preferences ADD COLUMN cat_chat INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- storage_thresholds — platform-owned, and it has to be
-- ---------------------------------------------------------------------------
--
-- ⚠ THE TABLE BELONGS TO NEITHER MODULE THAT USES IT (D236). `admin` writes these
-- values and `chat` reads them, and the two may not import each other — the same
-- constraint that put `notification_preferences` in this block rather than in
-- `admin`. platform/storage is the seam they already share.
--
-- KEYED RATHER THAN COLUMNED, so a later threshold is an INSERT and not a
-- migration. `value_mb > 0` is the only invariant SQLite can hold here; "below
-- current usage" is a legitimate value that the UI explains rather than refuses
-- (D244) — nothing in v10 is ever BLOCKED by a threshold, only warned about.
--
-- ⚠ v9's HOME_STORAGE_WARN_TOTAL_MB stays an environment variable and stays out of
-- this table (D236). Home now has two threshold mechanisms, one in Coolify for the
-- whole application and two rows here for chat. The inconsistency is RECORDED
-- rather than hidden: migrating a live operator setting into a table is a change
-- with its own failure mode, and it is v11's.
-- +goose StatementBegin
CREATE TABLE storage_thresholds (
    key        TEXT PRIMARY KEY,   -- 'chat.total' | 'chat.conversation'
    value_mb   INTEGER NOT NULL CHECK (value_mb > 0),
    updated_at TEXT NOT NULL,
    updated_by TEXT
);
-- +goose StatementEnd

-- The two defaults (PRD §V10-5). `updated_by` is NULL because nobody set them:
-- the Administrace screen distinguishes a seeded default from a value somebody
-- chose, and a fake actor here would make every fresh install look edited.
-- +goose StatementBegin
INSERT INTO storage_thresholds (key, value_mb, updated_at, updated_by) VALUES
    ('chat.total',        512, '2026-08-27T00:00:00.000Z', NULL),
    ('chat.conversation', 128, '2026-08-27T00:00:00.000Z', NULL);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS storage_thresholds;
-- +goose StatementEnd
-- SQLite has supported DROP COLUMN since 3.35; the driver in go.mod is well past
-- it. The column carries a member's mute preference, so dropping it discards a
-- choice rather than derived data — which is what a down migration is for.
-- +goose StatementBegin
ALTER TABLE notification_preferences DROP COLUMN cat_chat;
-- +goose StatementEnd
