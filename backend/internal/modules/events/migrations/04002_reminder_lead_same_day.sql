-- events module — admit the same-day reminder, reminder_lead = '0d'. Version
-- block 04000.
--
-- WHY A REBUILD. `reminder_lead` is CHECK-constrained and SQLite has no ALTER
-- form for a CHECK, so widening the list is create-wide → copy → drop → rename,
-- the way 08003_chat_delivery.sql widened its two.
--
-- ⚠ AND WHY THREE TABLES INSTEAD OF ONE. `events` is not 08003's table: it is the
-- first rebuilt table here with INCOMING foreign keys. `event_links` and
-- `event_reminder_completions` both REFERENCE events (id) ON DELETE CASCADE, and
-- with foreign_keys=ON (platform/db/db.go) `DROP TABLE events` performs an
-- implicit DELETE of every row before dropping — which FIRES those cascades. The
-- five-step rebuild would take the household's links and its entire completion
-- history with it and then report success.
--
-- So the parent is RENAMED, never dropped while anything points at it. Renaming
-- with foreign_keys=ON rewrites every REFERENCES clause that names the table, so
-- after step 1 both children point at `events_old`; each child is then rebuilt to
-- point back at the new `events`, and only then is `events_old` dropped — by
-- which time nothing references it and no cascade can fire. No row is deleted
-- anywhere in this file; every table moves by copy.
--
-- ⚠ `events_old` KEEPS THE TWO EVENT INDEXES under their own names (a rename
-- carries a table's indexes with it), which is why the new `idx_events_*` are
-- created LAST — after the drop that takes the old ones with it. Creating them
-- any earlier fails on the name.
--
-- Every copy names its columns rather than SELECT *: a positional copy reorders
-- silently if a column ever moves, and these are real household rows.

-- +goose Up

-- +goose StatementBegin
ALTER TABLE events RENAME TO events_old;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TABLE events (
    id               TEXT PRIMARY KEY,
    title            TEXT NOT NULL,
    description      TEXT,                                        -- markdown
    starts_on        TEXT NOT NULL,                               -- DATE 'YYYY-MM-DD', all-day
    rrule            TEXT,                                        -- RFC5545 subset; NULL = one-off
    timezone         TEXT NOT NULL DEFAULT 'Europe/Prague',
    reminder_enabled INTEGER NOT NULL DEFAULT 0,
    reminder_lead    TEXT CHECK (reminder_lead IN ('0d', '1d', '2d', '1w', '2w', '1m')),
    created_by       TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    archived         INTEGER NOT NULL DEFAULT 0,
    CHECK (reminder_enabled = 0 OR reminder_lead IS NOT NULL)
);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO events
    (id, title, description, starts_on, rrule, timezone, reminder_enabled,
     reminder_lead, created_by, created_at, updated_at, archived)
SELECT id, title, description, starts_on, rrule, timezone, reminder_enabled,
       reminder_lead, created_by, created_at, updated_at, archived
  FROM events_old;
-- +goose StatementEnd

-- event_links, re-pointed from events_old back to events.
-- +goose StatementBegin
CREATE TABLE event_links_new (
    id       TEXT PRIMARY KEY,
    event_id TEXT NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    url      TEXT NOT NULL,
    title    TEXT,
    position TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO event_links_new (id, event_id, url, title, position)
SELECT id, event_id, url, title, position FROM event_links;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE event_links;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE event_links_new RENAME TO event_links;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_event_links_event_position ON event_links (event_id, position);
-- +goose StatementEnd

-- event_reminder_completions, same move. The UNIQUE is what makes completion
-- idempotent, so it travels with the table definition rather than being an index
-- somebody could forget to re-create.
-- +goose StatementBegin
CREATE TABLE event_reminder_completions_new (
    id            TEXT PRIMARY KEY,
    event_id      TEXT NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    occurrence_on TEXT NOT NULL,                                  -- DATE 'YYYY-MM-DD'
    completed_by  TEXT,
    completed_at  TEXT NOT NULL,
    UNIQUE (event_id, occurrence_on)
);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO event_reminder_completions_new
    (id, event_id, occurrence_on, completed_by, completed_at)
SELECT id, event_id, occurrence_on, completed_by, completed_at
  FROM event_reminder_completions;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE event_reminder_completions;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE event_reminder_completions_new RENAME TO event_reminder_completions;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_completions_event_occ ON event_reminder_completions (event_id, occurrence_on);
-- +goose StatementEnd

-- Nothing references events_old now, so this drop cascades into nothing. It also
-- takes the two old idx_events_* with it, which is what frees their names below.
-- +goose StatementBegin
DROP TABLE events_old;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_events_starts_on ON events (starts_on);
-- +goose StatementEnd
-- The dashboard's hot query: reminder-enabled, non-archived events.
-- +goose StatementBegin
CREATE INDEX idx_events_reminder ON events (reminder_enabled) WHERE archived = 0;
-- +goose StatementEnd

-- +goose Down

-- ⚠ THE UPDATE IS LOAD-BEARING AND IT MUST COME FIRST — 08003's lesson, with a
-- different answer. By the time anyone runs this down the table may hold
-- reminder_lead = '0d' rows, and the narrow CHECK rejects them on the copy step,
-- failing the migration halfway and leaving a half-built table behind.
--
-- Unlike 08003's delivery rows these are the household's own content: an event
-- somebody wrote, not a log of a feature. So they are REMAPPED, not deleted, and
-- to '1d' rather than to NULL. '1d' reminds a day earlier than asked, which for a
-- household reminder is a harmless over-delivery; NULL would mean the second
-- CHECK (reminder_enabled = 0 OR reminder_lead IS NOT NULL) also has to be
-- satisfied by clearing reminder_enabled, and the event would then quietly stop
-- reminding anybody at all — a rollback that loses a reminder without saying so.
-- +goose StatementBegin
UPDATE events SET reminder_lead = '1d' WHERE reminder_lead = '0d';
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE events RENAME TO events_wide;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TABLE events (
    id               TEXT PRIMARY KEY,
    title            TEXT NOT NULL,
    description      TEXT,
    starts_on        TEXT NOT NULL,
    rrule            TEXT,
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
INSERT INTO events
    (id, title, description, starts_on, rrule, timezone, reminder_enabled,
     reminder_lead, created_by, created_at, updated_at, archived)
SELECT id, title, description, starts_on, rrule, timezone, reminder_enabled,
       reminder_lead, created_by, created_at, updated_at, archived
  FROM events_wide;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TABLE event_links_new (
    id       TEXT PRIMARY KEY,
    event_id TEXT NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    url      TEXT NOT NULL,
    title    TEXT,
    position TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO event_links_new (id, event_id, url, title, position)
SELECT id, event_id, url, title, position FROM event_links;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE event_links;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE event_links_new RENAME TO event_links;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_event_links_event_position ON event_links (event_id, position);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TABLE event_reminder_completions_new (
    id            TEXT PRIMARY KEY,
    event_id      TEXT NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    occurrence_on TEXT NOT NULL,
    completed_by  TEXT,
    completed_at  TEXT NOT NULL,
    UNIQUE (event_id, occurrence_on)
);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO event_reminder_completions_new
    (id, event_id, occurrence_on, completed_by, completed_at)
SELECT id, event_id, occurrence_on, completed_by, completed_at
  FROM event_reminder_completions;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE event_reminder_completions;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE event_reminder_completions_new RENAME TO event_reminder_completions;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_completions_event_occ ON event_reminder_completions (event_id, occurrence_on);
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE events_wide;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_events_starts_on ON events (starts_on);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_events_reminder ON events (reminder_enabled) WHERE archived = 0;
-- +goose StatementEnd
