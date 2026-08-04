-- notes module — Poznámky tables (PRD §5, HANDOFF-5 §1). Version block 06000:
-- applied last in the one Goose sequence (…events(04) → dashboard(05) → notes(06),
-- see bootstrap.MigrationSources). Nothing is seeded; must apply cleanly on an
-- empty DB and after a Litestream restore.
--
-- Markdown is the single canonical body (D30). A folder/note is addressable by a
-- human-readable slug path (D32); the slug is unique across the sibling folders
-- AND notes under one parent. SQLite treats NULL as distinct, so the sibling
-- indexes key on COALESCE(parent,'') to dedupe root-level siblings — a plain
-- UNIQUE(parent_id, slug) would NOT. The cross-table part of the invariant (a
-- folder and a note under one parent may not share a slug) has no single-index
-- form and is enforced in the write transaction by the service.

-- +goose Up

-- +goose StatementBegin
CREATE TABLE folders (
    id         TEXT PRIMARY KEY,
    parent_id  TEXT REFERENCES folders (id) ON DELETE CASCADE,      -- NULL = root
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL,
    position   TEXT NOT NULL,                                       -- lexorank
    archived   INTEGER NOT NULL DEFAULT 0,
    created_by TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_folders_parent_position ON folders (parent_id, position);
-- +goose StatementEnd
-- Sibling slug uniqueness (per table), root handled via COALESCE, live rows only.
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_folders_sibling_slug ON folders (COALESCE(parent_id, ''), slug) WHERE archived = 0;
-- +goose StatementEnd

-- `seq` is an explicit INTEGER PRIMARY KEY: a rowid alias whose values SQLite
-- preserves across VACUUM. A TEXT-PK table has only an implicit rowid, which
-- VACUUM renumbers — desyncing the external-content notes_fts index below. `id`
-- stays the logical TEXT key: UNIQUE (a valid FK target for note_pins.note_id).
-- +goose StatementBegin
CREATE TABLE notes (
    seq        INTEGER PRIMARY KEY,
    id         TEXT NOT NULL UNIQUE,
    folder_id  TEXT REFERENCES folders (id) ON DELETE CASCADE,      -- NULL = root/unfiled
    title      TEXT NOT NULL,
    slug       TEXT NOT NULL,
    body_md    TEXT,                                                -- the one canonical Markdown body (D30)
    position   TEXT NOT NULL,                                       -- lexorank
    archived   INTEGER NOT NULL DEFAULT 0,
    created_by TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_notes_folder_position ON notes (folder_id, position);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_notes_updated_at ON notes (updated_at DESC);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_notes_sibling_slug ON notes (COALESCE(folder_id, ''), slug) WHERE archived = 0;
-- +goose StatementEnd

-- Pin scopes (FR-P7, D35): one household pin per note; one personal pin per note
-- per user; the two are independent (a note can be both).
-- +goose StatementBegin
CREATE TABLE note_pins (
    id         TEXT PRIMARY KEY,
    note_id    TEXT NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    scope      TEXT NOT NULL CHECK (scope IN ('household', 'personal')),
    user_id    TEXT,                                                -- NULL for household; auth user id for personal
    pinned_by  TEXT,                                                -- actor, for audit
    position   TEXT NOT NULL,                                       -- lexorank, per scope/user
    created_at TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_note_pins_household ON note_pins (note_id) WHERE scope = 'household';
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_note_pins_personal ON note_pins (note_id, user_id) WHERE scope = 'personal';
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_note_pins_note ON note_pins (note_id);
-- +goose StatementEnd

-- Free-text search over note title + body (FR-P6). External-content FTS5 backed
-- by notes' rowid — the explicit INTEGER PRIMARY KEY `seq`, so the index survives
-- VACUUM; kept in sync by the triggers below. remove_diacritics
-- 2 folds Czech diacritics symmetrically so "gulas" finds "Guláš". Unlike the
-- immutable audit rows, notes are edited, so an AFTER UPDATE trigger is required.
-- notes_ad also fires for rows removed by folders' ON DELETE CASCADE (a hard folder
-- delete is an ordinary child-table delete), so those entries never orphan.
-- +goose StatementBegin
CREATE VIRTUAL TABLE notes_fts USING fts5 (
    title,
    body_md,
    content='notes',
    content_rowid='rowid',
    tokenize='unicode61 remove_diacritics 2'
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER notes_ai AFTER INSERT ON notes BEGIN
    INSERT INTO notes_fts (rowid, title, body_md)
    VALUES (new.rowid, new.title, new.body_md);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER notes_ad AFTER DELETE ON notes BEGIN
    INSERT INTO notes_fts (notes_fts, rowid, title, body_md)
    VALUES ('delete', old.rowid, old.title, old.body_md);
END;
-- +goose StatementEnd
-- Re-index only when a searchable column actually changes. A note row is UPDATEd by
-- far more than edits — reorder, reparent, re-slug, archive/restore all bump
-- updated_at without touching title/body — and re-tokenizing the full body_md on
-- every such move is wasted work (a burst of drag-reorders on large notes would
-- re-index each one). `IS NOT` is SQLite's NULL-safe distinct operator (body_md is
-- nullable, so `<>` would miss NULL↔value transitions).
-- +goose StatementBegin
CREATE TRIGGER notes_au AFTER UPDATE ON notes
WHEN old.title IS NOT new.title OR old.body_md IS NOT new.body_md
BEGIN
    INSERT INTO notes_fts (notes_fts, rowid, title, body_md)
    VALUES ('delete', old.rowid, old.title, old.body_md);
    INSERT INTO notes_fts (rowid, title, body_md)
    VALUES (new.rowid, new.title, new.body_md);
END;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TRIGGER IF EXISTS notes_au;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TRIGGER IF EXISTS notes_ad;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TRIGGER IF EXISTS notes_ai;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS notes_fts;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS note_pins;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS notes;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS folders;
-- +goose StatementEnd
