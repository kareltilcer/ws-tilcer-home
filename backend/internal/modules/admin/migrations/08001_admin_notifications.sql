-- admin module — notification configuration (PRD §V5-5, HANDOFF-7 §1).
-- Version block 08000: the admin module is the LAST in the migration sequence
-- (logging → platform → todo → events → dashboard → notes → documents → admin).
--
-- The per-user tables (push_subscriptions, notification_preferences) deliberately
-- live in the PLATFORM block instead: any module may send, and a member's consent
-- must not depend on the admin module existing. What lives here is only the
-- configuration surface — what gets sent, to whom, and when — plus the
-- operational record of what was actually delivered.
--
-- Nothing is seeded. A fresh household starts with no rules and no schedules.

-- +goose Up

-- Trigger rules: "when this audited action happens, push this text" (FR-ADM2).
-- +goose StatementBegin
CREATE TABLE notification_rules (
    id                      TEXT PRIMARY KEY,
    name                    TEXT NOT NULL,
    enabled                 INTEGER NOT NULL DEFAULT 1,
    -- Exactly one of these two: an exact action key ("card.move") or a dotted
    -- prefix matching a family of them ("event."). The CHECK is what stops a rule
    -- from being saved with both (ambiguous) or neither (matches everything).
    action_key              TEXT,
    action_prefix           TEXT,
    filter_module           TEXT,
    filter_entity_type      TEXT,
    filter_level            TEXT CHECK (filter_level IS NULL OR filter_level IN ('info', 'warn', 'error')),
    audience                TEXT NOT NULL,                     -- JSON {scope, roles?, users?}
    title_template          TEXT,
    body_template           TEXT,                              -- NULL ⇒ the audit event's own Czech summary
    coalesce_window_seconds INTEGER NOT NULL DEFAULT 60,
    exclude_actor           INTEGER NOT NULL DEFAULT 0,
    created_by              TEXT,
    created_at              TEXT NOT NULL,
    updated_at              TEXT NOT NULL,
    CHECK ((action_key IS NOT NULL AND action_prefix IS NULL)
        OR (action_key IS NULL AND action_prefix IS NOT NULL))
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_notification_rules_enabled ON notification_rules (enabled);
-- +goose StatementEnd

-- Scheduled summaries: "every day at 08:00, push these counts" (FR-ADM3).
-- last_fired_local_date is the idempotency key: it is the LOCAL (Prague) date, so
-- a slot fires once per local day regardless of UTC offset or DST.
-- +goose StatementBegin
CREATE TABLE notification_schedules (
    id                    TEXT PRIMARY KEY,
    name                  TEXT NOT NULL,
    enabled               INTEGER NOT NULL DEFAULT 1,
    time_local            TEXT NOT NULL,                       -- "HH:MM" wall clock in HOME_TIMEZONE
    days_spec             TEXT NOT NULL,                       -- JSON {preset} | {weekdays[]} | {day_of_month}
    audience              TEXT NOT NULL,                       -- JSON
    title_template        TEXT NOT NULL,
    body_template         TEXT NOT NULL,
    last_fired_at         TEXT,
    last_fired_local_date TEXT,                                -- "YYYY-MM-DD" local
    created_by            TEXT,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_notification_schedules_enabled ON notification_schedules (enabled);
-- +goose StatementEnd

-- Delivery attempts: OPERATIONAL, not audit (D64). One row per endpoint attempt.
-- It is best-effort evidence for "did the morning summary arrive, and to whom?",
-- it is prune-able, and it is deliberately NOT the audit spine — configuration
-- changes are audited, deliveries are not.
-- +goose StatementBegin
CREATE TABLE notification_deliveries (
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

-- +goose StatementBegin
DROP TABLE IF EXISTS notification_deliveries;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS notification_schedules;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS notification_rules;
-- +goose StatementEnd
