-- logging module — how a change was made (v11, PRD §V11-5, D290/D292,
-- HANDOFF-13 §2.2). Version block 01003, applied after 01002.
--
-- Every audit event written on the MCP path carries `via='mcp'` and the id of the
-- token that made it, so the Log can answer "did I do this, or did the assistant?"
-- — a question that did not exist before v11 and cannot be answered retroactively.
--
-- ⚠⚠ THIS MIGRATION CONTAINS NO `CREATE TABLE`, NO `DROP TABLE` AND NO RENAME, AND
-- A TEST ASSERTS IT (TestAuditViaMigrationDoesNotRebuild). The reason is a hazard,
-- not a preference.
--
-- The obvious alternative — a fourth `actor_type` — is impossible cheaply:
--
--     actor_type TEXT NOT NULL CHECK (actor_type IN ('user', 'system', 'service'))
--
-- SQLite cannot ALTER a CHECK, so a fourth value means REBUILDING `audit_events`:
-- the table holding every mutation the household has ever made, and the parent of
-- `audit_changes`, which declares REFERENCES audit_events (id) ON DELETE CASCADE.
-- With foreign_keys=ON (platform/db/db.go) dropping the parent performs an
-- implicit DELETE of every row, FIRES THAT CASCADE, and destroys the household's
-- entire change history WHILE REPORTING SUCCESS. `04002` needed 229 lines and a
-- rename-never-drop dance to avoid exactly this while widening one enum on a far
-- smaller table.
--
-- ADD COLUMN has no rebuild, no cascade and no down-migration risk, and the
-- partial index costs nothing on the 99% of rows where `via IS NULL`. The fourth
-- actor_type is DECLINED, not deferred (D290, confirmed by Karel 2026-09-06):
-- anyone who later finds actor_type lacking an `agent` value has found a decision.
-- TestActorTypeStaysThree is what keeps it found rather than repaired.

-- +goose Up

-- +goose StatementBegin
ALTER TABLE audit_events ADD COLUMN via TEXT;          -- 'mcp' or NULL
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE audit_events ADD COLUMN via_token_id TEXT; -- mcp_tokens.id or NULL
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_events_via ON audit_events (via, ts DESC) WHERE via IS NOT NULL;
-- +goose StatementEnd

-- +goose Down

-- ⚠ THE COLUMNS ARE DELIBERATELY NOT DROPPED, and this is not an unfinished down
-- migration. `ALTER TABLE ... DROP COLUMN` on SQLite rewrites the table — the one
-- operation this file exists to avoid, with the same cascade behind it — and two
-- nullable columns nobody reads cost nothing. Only the index goes.
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_events_via;
-- +goose StatementEnd
