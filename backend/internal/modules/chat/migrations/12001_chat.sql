-- chat module — Chat tables (PRD §V10-5, HANDOFF-12 §2). Version block 12000:
-- applied last in the one Goose sequence (… garden(10) → electricity(11) →
-- chat(12), see bootstrap.MigrationSources). A NEW BLOCK, because there is a new
-- module — the schema-level statement of D216.
--
-- ⚠ THIS IS THE FIRST MODULE IN HOME WHOSE ROWS THE HOUSEHOLD DOES NOT READ.
-- v1–v8 published data every member could see; v9 added a second axis, OWNERSHIP,
-- and made a private item invisible to everyone but its owner. v10 adds a third,
-- MEMBERSHIP: a conversation is readable by the people in it, from their
-- `effective_from` onward. Every read path in the module joins `chat_members` and
-- bounds on the floor IN SQL — see chat/scope.go, which is the only place that
-- rule is spelled.
--
-- Two migrations land OUTSIDE this block and neither belongs here: `02004` adds
-- `cat_chat` to `notification_preferences` and creates the platform-owned
-- `storage_thresholds` (any module may send, and `admin` writes thresholds that
-- `chat` reads, so neither table can belong to a feature); `08003` rebuilds
-- `notification_deliveries` to widen two CHECKs for 'chat'.
--
-- House conventions: UUIDv7 `id`, TEXT timestamps in RFC 3339 with milliseconds,
-- `created_by` / `created_at` / `updated_at`. No lexorank `position` anywhere: a
-- thread is chronological, and a draggable order over messages would be a second,
-- contradictory truth about what was said first.

-- +goose Up

-- ============================== konverzace ==============================

-- Exactly one row carries kind='default' — "Všichni", the household room every
-- member is auto-joined to at first sight (FR-V10-2). It is renameable and
-- neither deletable nor leaveable; both refusals are 422 in the service, because
-- SQLite cannot express "this row only" as a constraint.
-- +goose StatementBegin
CREATE TABLE chat_conversations (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL CHECK (kind IN ('default', 'group')),
    name       TEXT NOT NULL,
    created_by TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    -- The koš (D253). NULL ⇔ active; a trashed conversation is invisible to
    -- EVERY read, not merely absent from the list.
    deleted_at TEXT,
    deleted_by TEXT
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_chat_default ON chat_conversations (kind) WHERE kind = 'default';
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_chat_conv_active ON chat_conversations (deleted_at);
-- +goose StatementEnd

-- ============================== členství ==============================

-- ⚠ `effective_from` IS NOT "when they joined". It is the instant from which this
-- conversation is theirs to read (D218). A group add writes now(); auto-joining
-- Všichni writes the conversation's own created_at (D258), so a member the app
-- meets for the first time in 2028 reads the household room in full.
--
-- ⚠ THE EXEMPTION IS A VALUE, NOT A BRANCH. There is no CASE anywhere on `kind`
-- in a read path, and TestDefaultConversationHasNoHistoryBranch walks the module's
-- Go files to keep it that way. The moment history depends on a branch it becomes
-- an exception somebody gets wrong in the fourth query that needs it.
--
-- ⚠ `effective_from_id` IS THE NEWEST MESSAGE THIS MEMBER MAY NOT READ, and every
-- read path compares `id > effective_from_id` — never a timestamp. The floor has to
-- be expressible as an `id` bound (so the thread reads from idx_chat_messages_conv)
-- AND as a per-row join predicate (because a search result set spans conversations
-- whose floors all differ), and deriving one form from the other at query time
-- would be two spellings of one access rule.
--
-- ⚠ IT IS NOT effective_from CONVERTED INTO A UUIDv7. That was the first
-- implementation and it is wrong by up to a millisecond in the wrong direction: a
-- message minted in the SAME millisecond somebody was added has a larger random
-- suffix than the synthetic bound, so it sorts above it and the new member reads
-- it. Anchoring on a real id removes the clock, and the empty string — the value
-- written for Všichni — means "the conversation's beginning", since every UUID
-- sorts above it. See chat/floor.go.
--
-- Removing a member DELETES the row. Re-adding writes a new one with a new floor,
-- so a removed-and-re-added member has a permanent hole in the middle of a
-- conversation they otherwise read in full — a consequence of D218, surfaced in
-- the removal dialog because nothing afterwards would explain it. Their messages
-- stay: authorship does not depend on membership.
-- +goose StatementBegin
CREATE TABLE chat_members (
    conversation_id   TEXT NOT NULL REFERENCES chat_conversations (id) ON DELETE CASCADE,
    user_id           TEXT NOT NULL,
    effective_from    TEXT NOT NULL,
    effective_from_id TEXT NOT NULL,
    added_by          TEXT,
    muted             INTEGER NOT NULL DEFAULT 0,
    last_read_at      TEXT,
    PRIMARY KEY (conversation_id, user_id)
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_chat_members_user ON chat_members (user_id);
-- +goose StatementEnd

-- ============================== zprávy ==============================

-- `seq` is an explicit INTEGER PRIMARY KEY: a rowid alias whose values SQLite
-- preserves across VACUUM. A TEXT-PK table has only an implicit rowid, which
-- VACUUM renumbers — desyncing the external-content chat_messages_fts index below
-- so that search quietly returns the wrong rows. `notes` (06001) and `documents`
-- (07001) carry the same line for the same reason, and it is the single line in
-- this migration most likely to be "simplified".
--
-- ⚠ CONSEQUENCE, AND IT IS NOT OPTIONAL: THIS TABLE MUST NEVER BE REBUILT. So the
-- "body or attachment" invariant is a SERVICE-LEVEL check in the write
-- transaction, never a table CHECK — the v9 D179 precedent exactly (see
-- documents/scope.go assertPairing). Do not add one later with an ALTER TABLE:
-- there is no such statement in SQLite, and the rebuild that would implement it
-- breaks search.
--
-- `id` stays the logical TEXT key: UUIDv7, so it sorts chronologically and is
-- simultaneously the keyset cursor, the floor bound and a valid FK target for
-- reply_to_id.
-- +goose StatementBegin
CREATE TABLE chat_messages (
    seq             INTEGER PRIMARY KEY,
    id              TEXT NOT NULL UNIQUE,
    conversation_id TEXT NOT NULL REFERENCES chat_conversations (id) ON DELETE CASCADE,
    author_id       TEXT NOT NULL,
    body            TEXT NOT NULL,
    reply_to_id     TEXT REFERENCES chat_messages (id),
    created_at      TEXT NOT NULL,
    edited_at       TEXT,
    -- The tombstone (D223). A delete blanks `body` in the SAME statement: an
    -- external-content FTS5 index still returns a body left in the table, so
    -- `deleted_at IS NOT NULL` alone would hide the message from the thread and
    -- leave it findable by search.
    deleted_at      TEXT
);
-- +goose StatementEnd
-- This one index carries the thread, the floor and the cursor: the floor is a
-- range on `id`, because UUIDv7 sorts chronologically. ⚠ Do NOT add an index on
-- created_at — it would be a second ordering that disagrees with the cursor under
-- clock skew, on the one screen where two messages swapping places is visible.
-- +goose StatementBegin
CREATE INDEX idx_chat_messages_conv ON chat_messages (conversation_id, id);
-- +goose StatementEnd

-- ============================== přílohy ==============================

-- Written by PR 3; the table lands here because block 12 is the module's schema
-- and splitting it would leave a 12002 altering a table one release old for no
-- reason.
--
-- `conversation_id` IS DENORMALISED ON PURPOSE. The per-conversation byte sums and
-- the clean-up listing are the two hottest queries in the storage half, and both
-- would otherwise join through chat_messages for a column that never changes. It
-- is written once at upload and is never updated.
-- +goose StatementBegin
CREATE TABLE chat_attachments (
    id                TEXT PRIMARY KEY,
    message_id        TEXT NOT NULL REFERENCES chat_messages (id) ON DELETE CASCADE,
    conversation_id   TEXT NOT NULL,
    kind              TEXT NOT NULL CHECK (kind IN ('image', 'video', 'file')),
    original_filename TEXT NOT NULL,
    content_type      TEXT NOT NULL,
    byte_size         INTEGER NOT NULL,
    checksum          TEXT NOT NULL,
    storage_key       TEXT NOT NULL,
    thumbnail_key     TEXT,
    -- Intrinsic dimensions for images, so the thread reserves the space and does
    -- not reflow as they load (HANDOFF-design §v10). NULL for video and files.
    width             INTEGER,
    height            INTEGER,
    state             TEXT NOT NULL DEFAULT 'live' CHECK (state IN ('live', 'moved', 'removed')),
    document_id       TEXT,
    uploaded_by       TEXT NOT NULL,
    created_at        TEXT NOT NULL,
    cleaned_by        TEXT,
    cleaned_at        TEXT
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_chat_att_conv_state ON chat_attachments (conversation_id, state);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_chat_att_message ON chat_attachments (message_id);
-- +goose StatementEnd

-- ============================== fronta ke smazání ==============================

-- The drain's queue (D247), drained by PR 3's scheduler job. A message delete and
-- a clean-up delete queue with purge_after = now; a conversation delete queues
-- with deleted_at + HOME_CHAT_TRASH_DAYS, restore removes the rows, and
-- Smazat natrvalo rewrites them to now.
-- +goose StatementBegin
CREATE TABLE chat_deleted_keys (
    key         TEXT PRIMARY KEY,
    queued_at   TEXT NOT NULL,
    purge_after TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_chat_deleted_due ON chat_deleted_keys (purge_after);
-- +goose StatementEnd

-- ============================== fulltext ==============================

-- ⚠ THE FIFTH EXTERNAL-CONTENT FTS5 INDEX IN HOME, which takes the shadow count
-- from twenty to twenty-five. It is declared through storage.FTSShadows in
-- chat.Module.StorageTables(); §V9-12 records that garden_plants_fts went
-- uncounted for two versions, and arch/storage_completeness_test.go is what stops
-- it a third time.
--
-- Only `body` is indexed. A conversation NAME is not searchable content: names are
-- readable by members only, and putting them in the same index would mean one more
-- surface on which the membership join has to be remembered.
-- +goose StatementBegin
CREATE VIRTUAL TABLE chat_messages_fts USING fts5 (
    body,
    content='chat_messages',
    content_rowid='seq',
    tokenize='unicode61 remove_diacritics 2'
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER chat_messages_ai AFTER INSERT ON chat_messages BEGIN
    INSERT INTO chat_messages_fts (rowid, body) VALUES (new.seq, new.body);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER chat_messages_ad AFTER DELETE ON chat_messages BEGIN
    INSERT INTO chat_messages_fts (chat_messages_fts, rowid, body)
    VALUES ('delete', old.seq, old.body);
END;
-- +goose StatementEnd
-- ⚠ THIS TRIGGER MUST FIRE ON THE SOFT-DELETE PATH. A delete blanks `body` in the
-- same UPDATE that sets deleted_at (§6.3), so the guard below is what actually
-- removes the text from the index — without it, `deleted_at` hides the message
-- from the thread while search still returns its body inside a snippet. A message
-- row is also UPDATEd by an edit, which changes `body` and re-indexes correctly by
-- the same predicate. `IS NOT` is SQLite's NULL-safe distinct operator.
-- +goose StatementBegin
CREATE TRIGGER chat_messages_au AFTER UPDATE ON chat_messages
WHEN old.body IS NOT new.body
BEGIN
    INSERT INTO chat_messages_fts (chat_messages_fts, rowid, body)
    VALUES ('delete', old.seq, old.body);
    INSERT INTO chat_messages_fts (rowid, body) VALUES (new.seq, new.body);
END;
-- +goose StatementEnd

-- ============================== seed ==============================

-- Exactly one row: "Všichni". NO MEMBERSHIP IS SEEDED — the directory is projected
-- from `sessions` (push.Store.Members) and a member who has never logged in does
-- not exist yet, so membership accrues at first sight instead (FR-V10-2).
--
-- The id is a FIXED UUIDv7 rather than a generated one: a migration has no id
-- generator, and pinning it makes the household room the same row on every fresh
-- build and after every Litestream restore. Its `created_at` is what every
-- auto-join to this conversation writes as its floor (D258), which is why the two
-- literals below agree — `01a04084-3000` is 2026-08-27T00:00:00Z as a UUIDv7
-- timestamp prefix, so the id sorts where the row's own date says it should.
-- +goose StatementBegin
INSERT INTO chat_conversations (id, kind, name, created_by, created_at, updated_at)
VALUES ('01a04084-3000-7000-8000-000000000001', 'default', 'Všichni', NULL,
        '2026-08-27T00:00:00.000Z', '2026-08-27T00:00:00.000Z');
-- +goose StatementEnd

-- +goose Down

-- ⚠ ORDER MATTERS. A down that drops chat_messages before its triggers leaves
-- orphaned triggers referencing a missing table, and the next `up` fails.
-- +goose StatementBegin
DROP TRIGGER IF EXISTS chat_messages_au;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TRIGGER IF EXISTS chat_messages_ad;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TRIGGER IF EXISTS chat_messages_ai;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS chat_messages_fts;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS chat_deleted_keys;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS chat_attachments;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS chat_messages;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS chat_members;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS chat_conversations;
-- +goose StatementEnd
