-- 0001_init: the entire v1 schema for `home`, in one migration.
--
-- Deliberate single migration (HANDOFF F2 / PRD §5): this is a greenfield
-- service that has not shipped, so there is no reason to carry four migrations
-- for tables that all arrive before the first deploy.
--
-- Order matters: the logging (audit) tables are created FIRST — every other
-- module writes through the audit spine, and "build everything around it" is
-- the organizing principle. Then the to-do tables, then the events tables.
--
-- Conventions: ids are UUIDv7 stored as TEXT; ordering `position` columns are
-- lexorank-style TEXT keys; DATE columns hold 'YYYY-MM-DD' text and TIMESTAMP
-- columns hold RFC3339 UTC text; booleans are INTEGER 0/1. The default board is
-- NOT seeded here — seeding is done in Go, guarded by an empty-boards check, so
-- a fresh build restored from Litestream does not double-seed.

-- +goose Up

-- +goose StatementBegin
PRAGMA foreign_keys = ON;
-- +goose StatementEnd

-- ============================================================================
-- Logging (audit spine) — created first
-- ============================================================================

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

-- ============================================================================
-- To-do (Úkoly)
-- ============================================================================

-- +goose StatementBegin
CREATE TABLE boards (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT,
    position    TEXT NOT NULL,
    created_by  TEXT,
    created_at  TEXT NOT NULL,
    archived    INTEGER NOT NULL DEFAULT 0
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_boards_position ON boards (position);
-- +goose StatementEnd

-- columns.kind is a free-form, NON-UNIQUE hint (PRD D7): a board may have several
-- 'now' and several 'done' columns. The (kind) index serves Nástěnka's
-- cross-board kind='now' query.
-- +goose StatementBegin
CREATE TABLE columns (
    id         TEXT PRIMARY KEY,
    board_id   TEXT NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    priority   INTEGER NOT NULL DEFAULT 0,
    position   TEXT NOT NULL,
    kind       TEXT NOT NULL DEFAULT 'normal' CHECK (kind IN ('normal', 'now', 'done')),
    created_at TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_columns_board_position ON columns (board_id, position);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_columns_board_priority ON columns (board_id, priority);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_columns_kind ON columns (kind);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE cards (
    id         TEXT PRIMARY KEY,
    column_id  TEXT NOT NULL REFERENCES columns (id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    notes      TEXT,                                              -- markdown
    position   TEXT NOT NULL,
    created_by TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    done_at    TEXT,                                              -- set on entering a done column
    archived   INTEGER NOT NULL DEFAULT 0
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_cards_column_position ON cards (column_id, position);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_cards_updated ON cards (updated_at);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE card_links (
    id       TEXT PRIMARY KEY,
    card_id  TEXT NOT NULL REFERENCES cards (id) ON DELETE CASCADE,
    url      TEXT NOT NULL,
    title    TEXT,
    position TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_card_links_card_position ON card_links (card_id, position);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE checklist_items (
    id       TEXT PRIMARY KEY,
    card_id  TEXT NOT NULL REFERENCES cards (id) ON DELETE CASCADE,
    text     TEXT NOT NULL,
    done     INTEGER NOT NULL DEFAULT 0,
    position TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_checklist_card_position ON checklist_items (card_id, position);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE labels (
    id         TEXT PRIMARY KEY,
    board_id   TEXT NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    color      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (board_id, name)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE card_labels (
    card_id  TEXT NOT NULL REFERENCES cards (id) ON DELETE CASCADE,
    label_id TEXT NOT NULL REFERENCES labels (id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, label_id)
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_card_labels_label ON card_labels (label_id);
-- +goose StatementEnd

-- ============================================================================
-- Events (Okno do budoucnosti)
-- ============================================================================

-- No occurrences table by design (PRD D11): the RRULE is the storage;
-- occurrences are expanded on read. The only per-occurrence row that ever exists
-- is a reminder completion (below).
-- +goose StatementBegin
CREATE TABLE events (
    id               TEXT PRIMARY KEY,
    title            TEXT NOT NULL,
    description      TEXT,                                        -- markdown
    starts_on        TEXT NOT NULL,                               -- DATE 'YYYY-MM-DD', all-day
    rrule            TEXT,                                        -- RFC5545 subset; NULL = one-off
    timezone         TEXT NOT NULL DEFAULT 'Europe/Prague',
    reminder_enabled INTEGER NOT NULL DEFAULT 0,
    reminder_lead    TEXT CHECK (reminder_lead IN ('1d', '2d', '1w', '2w', '1m')),
    created_by       TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    archived         INTEGER NOT NULL DEFAULT 0,
    CHECK (reminder_enabled = 0 OR reminder_lead IS NOT NULL)
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_events_starts_on ON events (starts_on);
-- +goose StatementEnd
-- Module 4's hot query: reminder-enabled, non-archived events.
-- +goose StatementBegin
CREATE INDEX idx_events_reminder ON events (reminder_enabled) WHERE archived = 0;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE event_links (
    id       TEXT PRIMARY KEY,
    event_id TEXT NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    url      TEXT NOT NULL,
    title    TEXT,
    position TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_event_links_event_position ON event_links (event_id, position);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE event_reminder_completions (
    id            TEXT PRIMARY KEY,
    event_id      TEXT NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    occurrence_on TEXT NOT NULL,                                  -- DATE 'YYYY-MM-DD'
    completed_by  TEXT,
    completed_at  TEXT NOT NULL,
    UNIQUE (event_id, occurrence_on)                             -- makes completion idempotent
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_completions_event_occ ON event_reminder_completions (event_id, occurrence_on);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS event_reminder_completions;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS event_links;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS events;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS card_labels;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS labels;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS checklist_items;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS card_links;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS cards;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS columns;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS boards;
-- +goose StatementEnd
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
