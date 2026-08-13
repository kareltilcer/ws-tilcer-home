-- documents module — Dokumenty tables (PRD §5, HANDOFF-6 §1). Version block 07000.
--
-- ORDERING: goose applies the merged sequence by numeric filename prefix, so this
-- runs last: logging(01) → platform(02) → todo(03) → events(04) → dashboard(05) →
-- notes(06) → documents(07). HANDOFF-6 §1 asks for "after notes, before dashboard",
-- but dashboard already occupies 05 and owns a single layout table with no
-- dependency on documents — "after notes" is the only real constraint (plus the
-- 01xxx audit tables, which every module writes through). 07 satisfies it.
--
-- SQLite stores METADATA ONLY. The file bytes live in object storage under
-- id-based keys (documents/{id}/original, /preview.pdf, /thumb.webp), so a rename
-- or a move never touches storage (D42). Bytes are IMMUTABLE (D41): there are no
-- version columns and no replace path — a changed file is a new document row.
--
-- Its OWN folder tree, isolated from Poznámky (D40): these are `document_folders`,
-- not `folders`. Slugs are unique across the sibling folders AND documents under
-- one parent (D32); SQLite treats NULL as distinct, so the sibling indexes key on
-- COALESCE(parent,'') to dedupe root-level siblings — a plain UNIQUE(parent, slug)
-- would NOT. The cross-table half of that invariant (a folder and a document under
-- one parent may not share a slug) has no single-index form and is enforced in the
-- write transaction by the service.
--
-- Nothing is seeded. Must apply cleanly on an empty DB and after a Litestream
-- restore (metadata restores; the bytes are already in R2 and previews are NOT
-- re-derived, because preview_key/thumbnail_key still point at surviving objects).

-- +goose Up

-- +goose StatementBegin
CREATE TABLE document_folders (
    id         TEXT PRIMARY KEY,
    parent_id  TEXT REFERENCES document_folders (id) ON DELETE CASCADE,  -- NULL = root
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL,
    position   TEXT NOT NULL,                                            -- lexorank
    archived   INTEGER NOT NULL DEFAULT 0,
    created_by TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_document_folders_parent_position ON document_folders (parent_id, position);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_docfolders_sibling_slug ON document_folders (COALESCE(parent_id, ''), slug) WHERE archived = 0;
-- +goose StatementEnd

-- `seq` is an explicit INTEGER PRIMARY KEY: a rowid alias whose values SQLite
-- preserves across VACUUM. A TEXT-PK table has only an implicit rowid, which
-- VACUUM renumbers — desyncing the external-content documents_fts index below.
-- `id` stays the logical TEXT key: UNIQUE, so it is a valid FK target for
-- document_pins.document_id.
-- +goose StatementBegin
CREATE TABLE documents (
    seq               INTEGER PRIMARY KEY,
    id                TEXT NOT NULL UNIQUE,
    folder_id         TEXT REFERENCES document_folders (id) ON DELETE CASCADE, -- NULL = root/unfiled
    title             TEXT NOT NULL,                                     -- display title (editable)
    slug              TEXT NOT NULL,
    description       TEXT,                                              -- optional, indexed for search
    original_filename TEXT NOT NULL,
    content_type      TEXT NOT NULL,                                     -- SERVER-SNIFFED MIME, never the client's claim (D48)
    byte_size         INTEGER NOT NULL,
    checksum          TEXT NOT NULL,                                     -- SHA-256 hex; also the /raw ETag
    storage_key       TEXT NOT NULL,                                     -- documents/{id}/original (write-once, D41)
    preview_kind      TEXT NOT NULL CHECK (preview_kind IN ('native', 'pdf', 'none')),
    preview_status    TEXT NOT NULL DEFAULT 'pending'
                      CHECK (preview_status IN ('pending', 'ready', 'failed', 'none')),
    preview_key       TEXT,                                              -- documents/{id}/preview.pdf (derived)
    thumbnail_key     TEXT,                                              -- documents/{id}/thumb.webp (derived)
    position          TEXT NOT NULL,                                     -- lexorank
    archived          INTEGER NOT NULL DEFAULT 0,
    created_by        TEXT,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_documents_folder_position ON documents (folder_id, position);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_documents_updated_at ON documents (updated_at DESC);
-- +goose StatementEnd
-- Checksum index: finds duplicate uploads of identical bytes (the UI can point out
-- "this file is already here"); intentionally NOT unique — re-uploading the same
-- file legitimately creates a new, independently-addressable document (D41).
-- +goose StatementBegin
CREATE INDEX idx_documents_checksum ON documents (checksum);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_documents_sibling_slug ON documents (COALESCE(folder_id, ''), slug) WHERE archived = 0;
-- +goose StatementEnd
-- The preview worker re-enqueues everything still 'pending' on boot (crash safety),
-- so that lookup gets its own partial index rather than scanning the table.
-- +goose StatementBegin
CREATE INDEX idx_documents_preview_pending ON documents (preview_status) WHERE preview_status = 'pending';
-- +goose StatementEnd

-- Pin scopes (FR-DOC10, D47): one household pin per document; one personal pin per
-- document per user; the two are independent (a document can be both). Household
-- pins are audited shared state; personal pins are an unaudited per-user view
-- preference — the ONE mutation in this module that writes no audit event.
-- +goose StatementBegin
CREATE TABLE document_pins (
    id          TEXT PRIMARY KEY,
    document_id TEXT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    scope       TEXT NOT NULL CHECK (scope IN ('household', 'personal')),
    user_id     TEXT,                                                    -- NULL for household; auth user id for personal
    pinned_by   TEXT,                                                    -- actor, for audit
    position    TEXT NOT NULL,                                           -- lexorank, per scope/user
    created_at  TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_document_pins_household ON document_pins (document_id) WHERE scope = 'household';
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_document_pins_personal ON document_pins (document_id, user_id) WHERE scope = 'personal';
-- +goose StatementEnd
-- The widget reads "household pins ∪ this user's personal pins" in one query, so
-- the scope/user pair leads the index.
-- +goose StatementBegin
CREATE INDEX idx_document_pins_scope_user ON document_pins (scope, user_id, position);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_document_pins_document ON document_pins (document_id);
-- +goose StatementEnd

-- Free-text search over METADATA ONLY (FR-DOC7, D46): title + original filename +
-- description. File CONTENTS are deliberately not indexed — no extraction, no OCR
-- in v4. External-content FTS5 backed by documents' rowid (the explicit INTEGER
-- PRIMARY KEY `seq`, so the index survives VACUUM), kept in sync by the triggers
-- below. remove_diacritics 2 folds Czech diacritics symmetrically, so "smlouva"
-- finds "Smlouva" and "zaruka" finds "Záruka".
-- +goose StatementBegin
CREATE VIRTUAL TABLE documents_fts USING fts5 (
    title,
    original_filename,
    description,
    content='documents',
    content_rowid='rowid',
    tokenize='unicode61 remove_diacritics 2'
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER documents_ai AFTER INSERT ON documents BEGIN
    INSERT INTO documents_fts (rowid, title, original_filename, description)
    VALUES (new.rowid, new.title, new.original_filename, new.description);
END;
-- +goose StatementEnd
-- documents_ad also fires for rows removed by document_folders' ON DELETE CASCADE
-- (a hard folder delete is an ordinary child-table delete), so entries never orphan.
-- +goose StatementBegin
CREATE TRIGGER documents_ad AFTER DELETE ON documents BEGIN
    INSERT INTO documents_fts (documents_fts, rowid, title, original_filename, description)
    VALUES ('delete', old.rowid, old.title, old.original_filename, old.description);
END;
-- +goose StatementEnd
-- Re-index only when a searchable column actually changes. A document row is
-- UPDATEd by far more than an edit — reorder, reparent, re-slug, archive/restore,
-- and every preview_status transition write the row without touching the indexed
-- columns. `IS NOT` is SQLite's NULL-safe distinct operator (description is
-- nullable, so `<>` would miss NULL↔value transitions).
-- +goose StatementBegin
CREATE TRIGGER documents_au AFTER UPDATE ON documents
WHEN old.title IS NOT new.title
  OR old.original_filename IS NOT new.original_filename
  OR old.description IS NOT new.description
BEGIN
    INSERT INTO documents_fts (documents_fts, rowid, title, original_filename, description)
    VALUES ('delete', old.rowid, old.title, old.original_filename, old.description);
    INSERT INTO documents_fts (rowid, title, original_filename, description)
    VALUES (new.rowid, new.title, new.original_filename, new.description);
END;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TRIGGER IF EXISTS documents_au;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TRIGGER IF EXISTS documents_ad;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TRIGGER IF EXISTS documents_ai;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS documents_fts;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS document_pins;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS documents;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS document_folders;
-- +goose StatementEnd
