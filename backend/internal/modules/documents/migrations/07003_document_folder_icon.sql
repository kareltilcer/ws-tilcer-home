-- documents module — folder icon (v4.1). Version block 07003, applied after 07002.
--
-- A document folder gains an optional emoji icon shown in the tree, the browse
-- cards, and the move picker. Isolated from Poznámky (D40): this alters
-- document_folders, not folders. The column is nullable with no default: existing
-- rows read back NULL, which the client renders as the 📁 default (matching the
-- design's `icon || '📁'`), so nothing needs backfilling. Length is bounded by the
-- service, not a CHECK, because emoji are multi-code-point (ZWJ sequences, flags).
--
-- Must apply cleanly on an empty DB and after a Litestream restore.

-- +goose Up

-- +goose StatementBegin
ALTER TABLE document_folders ADD COLUMN icon TEXT;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
ALTER TABLE document_folders DROP COLUMN icon;
-- +goose StatementEnd
