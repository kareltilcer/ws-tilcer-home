-- chat module — reactions (v10.1, PRD D265). One row per (message, member, emoji).
--
-- ⚠ A NEW TABLE, NEVER A COLUMN ON chat_messages. That table carries an
-- external-content FTS5 index keyed on its rowid and must never be rebuilt
-- (12001's own note, the v9 D179 precedent) — and a reaction is not a property of
-- a message anyway: it is a fact about a MEMBER and a message, which is a row.
--
-- ⚠ THE THIRD FILE IN BLOCK 12 RATHER THAN AN EDIT TO EITHER OF THE FIRST TWO.
-- `12001` and `12002` are applied on the live database, so they are history: goose
-- will not re-run them and editing one would make the schema in this repo disagree
-- with the schema in production, silently, for everyone who builds fresh. Same
-- reasoning `12002` records for itself.
--
-- ⚠ THE ALLOW-LIST IS NOT A CHECK CONSTRAINT. Seven emoji are the palette (D265),
-- and the service refuses everything else — but a CHECK here would make widening
-- the palette a table rebuild, and this table's whole reason to exist is that
-- chat_messages may not be rebuilt. Putting the same trap one table over would be
-- the joke told twice. `reactions.go` holds the list, and one test asserts the
-- service refuses anything absent from it.

-- +goose Up

-- +goose StatementBegin
CREATE TABLE chat_reactions (
    message_id TEXT NOT NULL REFERENCES chat_messages (id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL,
    emoji      TEXT NOT NULL,
    created_at TEXT NOT NULL,

    -- ⚠ THE PRIMARY KEY IS THE IDEMPOTENCY, AND IT IS ALSO THE READ INDEX. One
    -- member reacts with one emoji at most once, so `INSERT OR IGNORE` is the whole
    -- of "add" and a `DELETE` is the whole of "remove" — neither needs to read
    -- first, which matters against a pool capped at one connection.
    --
    -- SQLite materialises this as an ordinary index whose leading column is
    -- `message_id`, so the batched page load (`WHERE message_id IN (…)`) is served
    -- by it. A separate index on `message_id` would be that same prefix a second
    -- time, paid for on every write.
    PRIMARY KEY (message_id, user_id, emoji)
);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE chat_reactions;
-- +goose StatementEnd
