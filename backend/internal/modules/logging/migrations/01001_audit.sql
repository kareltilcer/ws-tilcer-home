-- logging module — the audit spine tables (PRD §5, HANDOFF-1). Created FIRST in
-- the global sequence (version block 01000): every other module writes through
-- the audit spine, so its tables must exist before any feature table.
--
-- The AuditSink writer lives in platform/audit (so every module can call it); the
-- TABLES it writes to are owned here by the logging module (HANDOFF-1 §1).

-- +goose Up

-- +goose StatementBegin
CREATE TABLE audit_events (
    id            TEXT PRIMARY KEY,
    ts            TEXT NOT NULL,                                   -- RFC3339 UTC
    actor_user_id TEXT,
    actor_type    TEXT NOT NULL CHECK (actor_type IN ('user', 'system', 'service')),
    actor_label   TEXT,
    module        TEXT NOT NULL,
    action        TEXT NOT NULL,
    entity_type   TEXT,
    entity_id     TEXT,
    summary       TEXT NOT NULL,
    level         TEXT NOT NULL DEFAULT 'info' CHECK (level IN ('info', 'warn', 'error')),
    request_id    TEXT,
    ip            TEXT,
    user_agent    TEXT,
    site          TEXT NOT NULL DEFAULT 'home',
    meta          TEXT                                             -- JSON, nullable
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_events_ts ON audit_events (ts DESC);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_events_module_ts ON audit_events (module, ts DESC);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_events_actor_ts ON audit_events (actor_user_id, ts DESC);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_events_entity ON audit_events (entity_type, entity_id, ts DESC);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_events_action ON audit_events (action);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE audit_changes (
    id        TEXT PRIMARY KEY,
    event_id  TEXT NOT NULL REFERENCES audit_events (id) ON DELETE CASCADE,
    field     TEXT NOT NULL,
    old_value TEXT,                                               -- full value, no truncation
    new_value TEXT                                                -- full value, no truncation
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_changes_event ON audit_changes (event_id);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_changes_field ON audit_changes (field);
-- +goose StatementEnd

-- Free-text search over summary + meta (FR-L3 `q`). External-content FTS5 table
-- backed by audit_events' implicit rowid; kept in sync by triggers below.
-- remove_diacritics 2 folds Czech diacritics symmetrically so "kotlík" is findable.
-- +goose StatementBegin
CREATE VIRTUAL TABLE audit_events_fts USING fts5 (
    summary,
    meta,
    content='audit_events',
    content_rowid='rowid',
    tokenize='unicode61 remove_diacritics 2'
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER audit_events_ai AFTER INSERT ON audit_events BEGIN
    INSERT INTO audit_events_fts (rowid, summary, meta)
    VALUES (new.rowid, new.summary, new.meta);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_events_ad AFTER DELETE ON audit_events BEGIN
    INSERT INTO audit_events_fts (audit_events_fts, rowid, summary, meta)
    VALUES ('delete', old.rowid, old.summary, old.meta);
END;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TRIGGER IF EXISTS audit_events_ad;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TRIGGER IF EXISTS audit_events_ai;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS audit_events_fts;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS audit_changes;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS audit_events;
-- +goose StatementEnd
