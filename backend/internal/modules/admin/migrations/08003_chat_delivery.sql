-- admin module — widen notification_deliveries for chat (PRD §V10-5, HANDOFF-12 §9.2).
--
-- ⚠ THIS IS THE ONLY v10 MIGRATION THAT TOUCHES AN EXISTING TABLE WITH LIVE DATA.
-- Everything else in v10 creates tables or adds a column. This one rewrites every
-- row of a table the household has been filling since v5, and it is small,
-- operational, non-audit data — which makes it LOW-RISK, not no-risk. It wants the
-- care §V9-12 gave the `soukrome` backfill: a down that survives, and a run
-- against a Litestream-restored copy of production before it merges.
--
-- ⚠ AND IT IS AN OUT-OF-ORDER GOOSE VERSION. `08003` is numerically below the
-- applied `11001`; see 02004_chat_platform.sql for why that is by design and what
-- makes it work (goose.WithAllowMissing, bootstrap/v10_migration_test.go).
--
-- WHY A REBUILD. `kind` and `category` are CHECK-constrained, and SQLite cannot
-- alter a CHECK: there is no ALTER TABLE form for it, so widening the two lists is
-- create-wide → copy → drop → rename → re-create the indexes.
--
-- ⚠ FOUR INDEXES, NOT THREE. HANDOFF-12 §9.2 and PRD §V10-5 both say three; the
-- table actually carries `_ts`, `_kind_ts`, `_rule_ts` and `_status_ts`
-- (08001_admin_notifications.sql). A rebuild that re-creates three of them leaves
-- the delivery log's status filter on a full scan, which nothing would ever fail
-- on — it would just get slower every month.
--
-- ⚠ DO NOT DODGE THIS by recording chat pushes as kind='trigger'/category='triggers'.
-- That would let a member silence chat by muting Administrace's trigger rules, and
-- it would make the delivery log unable to answer "did the chat push go out?"
-- separately from "did the rule fire?" — which is the question the log exists for.
--
-- Nothing references this table by foreign key and it holds none of its own, so
-- the plain five-step rebuild is safe with foreign_keys=ON (platform/db/db.go).

-- +goose Up

-- +goose StatementBegin
CREATE TABLE notification_deliveries_new (
    id              TEXT PRIMARY KEY,
    ts              TEXT NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('broadcast', 'trigger', 'schedule', 'test', 'chat')),
    category        TEXT NOT NULL CHECK (category IN ('broadcast', 'triggers', 'summaries', 'chat')),
    rule_id         TEXT,
    user_id         TEXT NOT NULL,
    subscription_id TEXT,
    status          TEXT NOT NULL CHECK (status IN ('sent', 'failed', 'expired')),
    error           TEXT
);
-- +goose StatementEnd
-- Column list written out rather than INSERT INTO … SELECT *: a positional copy
-- silently reorders if either table's columns ever move, and this is the one
-- migration in v10 where "silently" means real rows.
-- +goose StatementBegin
INSERT INTO notification_deliveries_new
    (id, ts, kind, category, rule_id, user_id, subscription_id, status, error)
SELECT id, ts, kind, category, rule_id, user_id, subscription_id, status, error
  FROM notification_deliveries;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE notification_deliveries;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE notification_deliveries_new RENAME TO notification_deliveries;
-- +goose StatementEnd
-- All four, exactly as 08001 declared them.
-- +goose StatementBegin
CREATE INDEX idx_notification_deliveries_ts ON notification_deliveries (ts DESC);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_notification_deliveries_kind_ts ON notification_deliveries (kind, ts DESC);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_notification_deliveries_rule_ts ON notification_deliveries (rule_id, ts DESC);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_notification_deliveries_status_ts ON notification_deliveries (status, ts DESC);
-- +goose StatementEnd

-- +goose Down

-- ⚠ THE DELETE IS LOAD-BEARING AND IT MUST COME FIRST. By the time anyone runs
-- this down, the table may hold rows with kind='chat' — and the narrow table's own
-- CHECK would reject them on the copy step below, failing the down migration
-- halfway and leaving the database with a half-built table. Discarding chat's
-- delivery rows is the correct answer: they are operational records of a feature
-- being rolled back, and the alternative (rewriting them to 'trigger') would
-- fabricate history in the one log that exists to be believed.
-- +goose StatementBegin
DELETE FROM notification_deliveries WHERE kind = 'chat' OR category = 'chat';
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TABLE notification_deliveries_old (
    id              TEXT PRIMARY KEY,
    ts              TEXT NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('broadcast', 'trigger', 'schedule', 'test')),
    category        TEXT NOT NULL CHECK (category IN ('broadcast', 'triggers', 'summaries')),
    rule_id         TEXT,
    user_id         TEXT NOT NULL,
    subscription_id TEXT,
    status          TEXT NOT NULL CHECK (status IN ('sent', 'failed', 'expired')),
    error           TEXT
);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO notification_deliveries_old
    (id, ts, kind, category, rule_id, user_id, subscription_id, status, error)
SELECT id, ts, kind, category, rule_id, user_id, subscription_id, status, error
  FROM notification_deliveries;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE notification_deliveries;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE notification_deliveries_old RENAME TO notification_deliveries;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_notification_deliveries_ts ON notification_deliveries (ts DESC);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_notification_deliveries_kind_ts ON notification_deliveries (kind, ts DESC);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_notification_deliveries_rule_ts ON notification_deliveries (rule_id, ts DESC);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_notification_deliveries_status_ts ON notification_deliveries (status, ts DESC);
-- +goose StatementEnd
