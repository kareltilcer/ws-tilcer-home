-- documents module — thumbnail_status, the retry marker for derived thumbnails.
--
-- 07001 tracked derivation with preview_status alone, which cannot express "this
-- document still owes us a thumbnail". An image or PDF is inserted 'native'/'ready'
-- — the original IS its preview — so a thumbnail job that died on a transient
-- storage error left NOTHING marking the work undone, and the boot sweep over
-- preview_status = 'pending' (which only ever matches Office conversions) skipped it
-- on every restart. That document had no thumbnail forever, with no way to retry.
--
-- The column is server-side only: it is absent from the API, where a thumbnail is
-- announced by thumbnail_url and its absence needs no explanation. 'failed' and
-- 'none' are terminal on purpose — a missing cwebp must not make every boot
-- re-download every original.

-- +goose Up

-- +goose StatementBegin
ALTER TABLE documents ADD COLUMN thumbnail_status TEXT NOT NULL DEFAULT 'none'
    CHECK (thumbnail_status IN ('pending', 'ready', 'failed', 'none'));
-- +goose StatementEnd

-- Rows written by 07001 predate the column: one that already has its thumbnail is
-- 'ready', and anything else is left at the 'none' default rather than 'pending' —
-- re-deriving thumbnails for the whole existing archive on the next boot would be a
-- worse first impression than a few type icons.
-- +goose StatementBegin
UPDATE documents SET thumbnail_status = 'ready'
 WHERE thumbnail_key IS NOT NULL AND thumbnail_key <> '';
-- +goose StatementEnd

-- The boot sweep reads both pending sets; give this one the same partial index
-- idx_documents_preview_pending gives the other.
-- +goose StatementBegin
CREATE INDEX idx_documents_thumbnail_pending ON documents (thumbnail_status) WHERE thumbnail_status = 'pending';
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP INDEX IF EXISTS idx_documents_thumbnail_pending;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE documents DROP COLUMN thumbnail_status;
-- +goose StatementEnd
