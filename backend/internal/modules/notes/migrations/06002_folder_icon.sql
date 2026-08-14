-- notes module — folder icon (v4.1). Version block 06002, applied after 06001 in
-- the one Goose sequence.
--
-- A folder gains an optional emoji icon shown in the tree, the browse cards, and the
-- move picker. The column is nullable with no default: existing rows read back NULL,
-- which the client renders as the 📁 default (matching the design's `icon || '📁'`),
-- so nothing needs backfilling. The value is a short emoji string; length is bounded
-- by the service, not a CHECK, because emoji are multi-code-point (ZWJ sequences,
-- flags) and a byte/char CHECK would be both wrong and brittle.
--
-- Must apply cleanly on an empty DB and after a Litestream restore.

-- +goose Up

-- +goose StatementBegin
ALTER TABLE folders ADD COLUMN icon TEXT;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
ALTER TABLE folders DROP COLUMN icon;
-- +goose StatementEnd
