-- chat module — where a moved attachment now lives (PR 3, PRD D246).
--
-- ⚠ A SECOND FILE IN BLOCK 12 RATHER THAN AN EDIT TO 12001. `12001` shipped with
-- PR 2 and is applied on the live database, so it is history: goose will not re-run
-- it and editing it would make the schema in this repo disagree with the schema in
-- production, silently, for everyone who builds fresh. One additive ALTER is the
-- cheaper honest answer.
--
-- ⚠ AND IT IS AN ALTER ON chat_attachments, NEVER ON chat_messages. That table
-- carries an external-content FTS5 index keyed on its rowid and must never be
-- rebuilt (12001's own note, the v9 D179 precedent). `chat_attachments` carries no
-- index of that kind, and ADD COLUMN is not a rebuild in any case.
--
-- WHY A COLUMN AND NOT A DERIVED STRING. After a move the bubble renders the file
-- from Dokumenty, so it needs that module's content URL. Building it in Go —
-- "/api/documents/" + document_id + "/raw" — would put another module's URL layout
-- inside `chat`, which is exactly the cross-module knowledge internal/arch forbids
-- and the storage catalog exists to broker. `storage.BlobSink` HANDS THE PATH BACK
-- as part of accepting custody (AcceptResult.Path); this column is where the answer
-- to "where did it go" is kept, beside the `document_id` it belongs to.

-- +goose Up

-- +goose StatementBegin
ALTER TABLE chat_attachments ADD COLUMN document_path TEXT;
-- +goose StatementEnd

-- +goose Down

-- SQLite has supported DROP COLUMN since 3.35 and the driver in go.mod is well
-- past it. The column is derived from document_id — dropping it loses a rendering
-- hint, not a fact — so the down is a plain drop rather than a rebuild.
-- +goose StatementBegin
ALTER TABLE chat_attachments DROP COLUMN document_path;
-- +goose StatementEnd
