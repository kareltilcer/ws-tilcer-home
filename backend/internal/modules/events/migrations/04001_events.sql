-- events module — Okno do budoucnosti tables (PRD §5, HANDOFF-3). Version block
-- 04000.
--
-- No occurrences table by design (PRD D11): the RRULE is the storage; occurrences
-- are expanded on read. The only per-occurrence row that ever exists is a reminder
-- completion (below).

-- +goose Up

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
-- The dashboard's hot query: reminder-enabled, non-archived events.
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
