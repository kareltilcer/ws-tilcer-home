-- platform core — the audit outbox tailer's cursor (PRD §V5-5, HANDOFF-7 §1/§4,
-- D56). Version block 02000.
--
-- Trigger notifications must fire AFTER the change commits. The audit sink
-- writes inside the caller's transaction, so it cannot know when that commits —
-- but a separate reader only ever SEES committed rows. That makes audit_events a
-- transactional outbox, and this single row is the tailer's keyset position in it.

-- +goose Up

-- +goose StatementBegin
CREATE TABLE audit_notify_cursor (
    id            INTEGER PRIMARY KEY CHECK (id = 1),   -- exactly one row, ever
    last_event_id TEXT NOT NULL,                        -- last processed audit_events.id (UUIDv7)
    updated_at    TEXT NOT NULL
);
-- +goose StatementEnd

-- Seed the cursor to the NEWEST existing event, not to the empty string.
--
-- This is the difference between a quiet upgrade and a notification storm: home
-- already holds months of audit history, and an unseeded cursor would replay
-- every past card move and note edit as a push the moment the first trigger rule
-- is enabled. Starting at the current maximum means v5 only ever notifies about
-- what happens AFTER it was deployed.
--
-- COALESCE covers the fresh-install case (no events yet): the empty string sorts
-- before every UUIDv7, so the tailer starts from the beginning of an empty table.
-- +goose StatementBegin
INSERT INTO audit_notify_cursor (id, last_event_id, updated_at)
SELECT 1, COALESCE(MAX(id), ''), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  FROM audit_events;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS audit_notify_cursor;
-- +goose StatementEnd
