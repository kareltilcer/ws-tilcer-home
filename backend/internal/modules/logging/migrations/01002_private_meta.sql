-- logging module — the private-event lookup index (v9, PRD §V9-5, D187/D179).
-- Version block 01002, applied after 01001 and therefore before every feature
-- table, exactly as 01001 is.
--
-- v9's audit events are written IN FULL and redacted at READ time (D187). The
-- marker that lets a read path tell a private event apart lives in the existing
-- `meta` JSON — `meta.visibility` and, when private, `meta.owner_id` — and NOT in
-- two new columns.
--
-- That is not laziness. audit_events carries an external-content FTS5 index of its
-- own (audit_events_fts, keyed on the rowid), so it is under the same D179
-- constraint as `notes` and `documents`: a table rebuild renumbers rowids and
-- desynchronises search silently. An EXPRESSION INDEX over the JSON needs no
-- rebuild at all, which is exactly why the marker was put there.
--
-- No column is added by this migration and no row is touched. It is one index.

-- +goose Up

-- +goose StatementBegin
CREATE INDEX idx_events_private_owner
    ON audit_events (json_extract(meta, '$.owner_id'))
    WHERE json_extract(meta, '$.visibility') = 'private';
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP INDEX IF EXISTS idx_events_private_owner;
-- +goose StatementEnd
