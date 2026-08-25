-- documents module — the private root (v9, PRD §V9-5, FR-V9-8, D177/D178/D179/D200).
-- Version block 07004, applied after 07003. NO NEW MIGRATION BLOCK.
--
-- Identical in every respect to notes' 06004, over document_folders/documents.
-- The v4 precedent (D40) that Dokumenty MIRRORS Poznámky's folder model rather
-- than sharing an abstraction with it holds here too: one behaviour, two
-- implementations, deliberately. See 06004 for the reasoning in full — this file
-- states only what differs.
--
-- What does NOT change here:
--   * `documents.checksum` and idx_documents_checksum (leak table row 22). The
--     index is dormant — nothing queries it today — but 07001's comment anticipates
--     a "this file is already here" UI, which the moment it ships is a CROSS-SCOPE
--     DUPLICATE ORACLE: it would answer "does this exact file exist in someone
--     else's private tree?". Named here so it is scoped when it is built, not after.
--   * The R2 keys. They are id-based and independent of folder, slug and scope
--     (D42), which is why a publish leaves /d/{id} untouched and why no object is
--     moved by anything in v9.
--   * note_images has no counterpart here and gains no column anywhere (D204).

-- +goose Up

-- +goose StatementBegin
ALTER TABLE document_folders ADD COLUMN visibility TEXT NOT NULL DEFAULT 'shared';
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE document_folders ADD COLUMN owner_id TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE documents ADD COLUMN visibility TEXT NOT NULL DEFAULT 'shared';
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE documents ADD COLUMN owner_id TEXT;
-- +goose StatementEnd

-- The root scope enters the sibling-slug indexes (D178). Two of the four; the
-- other two are in 06004. Scoping these four is NOT enough on its own — the
-- store's SiblingSlugTaken carries its own COALESCE(parent,'') predicate and
-- freeSlug suffixes rather than erroring, so both halves move together (D210).
-- +goose StatementBegin
DROP INDEX ux_docfolders_sibling_slug;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_docfolders_sibling_slug ON document_folders (
    COALESCE(parent_id, 'root:' || visibility || ':' || COALESCE(owner_id, '')), slug
) WHERE archived = 0;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX ux_documents_sibling_slug;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_documents_sibling_slug ON documents (
    COALESCE(folder_id, 'root:' || visibility || ':' || COALESCE(owner_id, '')), slug
) WHERE archived = 0;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_documents_owner_scope ON documents (owner_id, visibility) WHERE visibility = 'private';
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_docfolders_owner_scope ON document_folders (owner_id, visibility) WHERE visibility = 'private';
-- +goose StatementEnd

-- Free the RESERVED ROOT SLUG on rows that already exist (D185) — the Dokumenty
-- half of the backfill in 06004; see there for the full argument.
--
-- Short version: /dokumenty/soukrome/… is an SPA route literal, the service's
-- isReservedRootSlug only runs on a create, a rename or an unarchive, and a folder
-- named "Soukromé" filed at the root before v9 was never offered to it. Left
-- alone, this migration is what makes it unreachable.
--
-- ⚠ The CASE keeps a UNIQUE violation from crash-looping the boot when
-- `soukrome-2` is itself taken; the fallback suffix is the row's own id. The
-- documents statement runs second and re-reads both tables, so it steps around
-- whatever the folder statement took.
-- +goose StatementBegin
UPDATE document_folders SET slug = CASE
    WHEN EXISTS (SELECT 1 FROM document_folders f2 WHERE f2.archived = 0 AND f2.parent_id IS NULL
                   AND f2.visibility = 'shared' AND f2.slug = 'soukrome-2')
      OR EXISTS (SELECT 1 FROM documents d2 WHERE d2.archived = 0 AND d2.folder_id IS NULL
                   AND d2.visibility = 'shared' AND d2.slug = 'soukrome-2')
    THEN 'soukrome-' || id
    ELSE 'soukrome-2' END
WHERE archived = 0 AND parent_id IS NULL AND visibility = 'shared' AND slug = 'soukrome';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE documents SET slug = CASE
    WHEN EXISTS (SELECT 1 FROM document_folders f2 WHERE f2.archived = 0 AND f2.parent_id IS NULL
                   AND f2.visibility = 'shared' AND f2.slug = 'soukrome-2')
      OR EXISTS (SELECT 1 FROM documents d2 WHERE d2.archived = 0 AND d2.folder_id IS NULL
                   AND d2.visibility = 'shared' AND d2.slug = 'soukrome-2')
    THEN 'soukrome-' || id
    ELSE 'soukrome-2' END
WHERE archived = 0 AND folder_id IS NULL AND visibility = 'shared' AND slug = 'soukrome';
-- +goose StatementEnd

-- +goose Down

-- ⚠ Indexes before columns (D200).
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_docfolders_owner_scope;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_documents_owner_scope;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS ux_documents_sibling_slug;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS ux_docfolders_sibling_slug;
-- +goose StatementEnd

-- Re-slug live rows that would collide under the restored UNSCOPED key. The v9
-- index legally admits a private root 'smlouvy' beside a shared one; recreating
-- the pre-v9 index over both hits a UNIQUE violation and aborts the rollback
-- after the columns are already gone. Private rows yield — shared slugs keep
-- their pre-v9 URLs — and the fallback suffix is the row's WHOLE id, unique because
-- it is the PRIMARY KEY (the same suffix the Up block uses for 'soukrome').
--
-- ⚠ Not substr(id, 1, 8), which is what this was and which is NOT unique: a
-- UUIDv7's leading hex digits are millisecond timestamp bits, shared by every id
-- minted in the same ~65-second window. Two members creating a private "Smlouvy"
-- in one onboarding session would collide on the very statement that exists to
-- prevent a collision — aborting the rollback with the columns already dropped.
-- Runs while `visibility` still exists, so it must precede the column drops.
-- +goose StatementBegin
UPDATE document_folders SET slug = slug || '-' || id
WHERE archived = 0 AND visibility = 'private'
  AND EXISTS (SELECT 1 FROM document_folders f2 WHERE f2.archived = 0 AND f2.id <> document_folders.id
                AND COALESCE(f2.parent_id, '') = COALESCE(document_folders.parent_id, '')
                AND f2.slug = document_folders.slug);
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE documents SET slug = slug || '-' || id
WHERE archived = 0 AND visibility = 'private'
  AND EXISTS (SELECT 1 FROM documents d2 WHERE d2.archived = 0 AND d2.id <> documents.id
                AND COALESCE(d2.folder_id, '') = COALESCE(documents.folder_id, '')
                AND d2.slug = documents.slug);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE documents DROP COLUMN owner_id;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE documents DROP COLUMN visibility;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE document_folders DROP COLUMN owner_id;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE document_folders DROP COLUMN visibility;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_docfolders_sibling_slug ON document_folders (COALESCE(parent_id, ''), slug) WHERE archived = 0;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_documents_sibling_slug ON documents (COALESCE(folder_id, ''), slug) WHERE archived = 0;
-- +goose StatementEnd
