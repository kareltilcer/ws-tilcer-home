-- notes module — the private root (v9, PRD §V9-5, D177/D178/D179/D200).
-- Version block 06004, applied after 06003. NO NEW MIGRATION BLOCK: v9 adds no
-- module, it changes three that exist.
--
-- Privacy here is a SECOND ROOT, not a per-item checkbox (D177). A tree is
-- addressed by its root scope — the pair (visibility, owner_id) — of which there
-- are 1 + N per module for N members. The per-item-flag alternative was specified
-- and rejected: it puts folders of mixed visibility into a tree everyone shares,
-- and every member then sees folders whose contents differ from what the folder
-- says it holds.
--
-- The invariant that ties the two columns is: shared ⇒ owner_id IS NULL,
-- private ⇒ owner_id IS NOT NULL. It is NOT a table CHECK, and that is deliberate
-- (D179): SQLite cannot ALTER TABLE … ADD CONSTRAINT, and rebuilding these tables
-- is not a routine migration — `notes` carries an explicit `seq INTEGER PRIMARY
-- KEY` precisely BECAUSE notes_fts is external-content and rowid-keyed (06001 says
-- so). A rebuild renumbers rowids and desynchronises the search index, and the
-- failure mode is that search silently returns the wrong rows. So the pairing is a
-- SERVICE-level invariant, exactly as v8's meter monotonicity is (D148).
--
-- DEFAULT 'shared' IS the migration of existing data (D200). There is no backfill
-- and no seed: every row that exists on deploy day stays exactly as visible as it
-- was, and there is no unpublish route to move it the other way (D182).
--
-- Must apply cleanly on an empty DB and after a Litestream restore — and, unlike
-- every migration before it, must be verified on a RESTORED PRODUCTION COPY with a
-- full-text search asserted to return the same rows it returned before.

-- +goose Up

-- +goose StatementBegin
ALTER TABLE folders ADD COLUMN visibility TEXT NOT NULL DEFAULT 'shared';
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE folders ADD COLUMN owner_id TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE notes ADD COLUMN visibility TEXT NOT NULL DEFAULT 'shared';
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE notes ADD COLUMN owner_id TEXT;
-- +goose StatementEnd

-- The sibling-slug indexes — the most important lines in v9 (D178).
--
-- 06001 keys them on COALESCE(parent, '') because SQLite treats NULLs as distinct,
-- so a plain UNIQUE(parent_id, slug) would not constrain the root at all. THAT
-- SENTINEL NOW COLLIDES WITH ITSELF: two members who each keep a private note
-- called "Recepty" at their own root both key on ('', 'recepty').
--
-- ⚠ And the symptom of leaving it is NOT the 409 you would expect (D210). The
-- service's freeSlug loops on Store.SiblingSlugTaken appending -2, -3…, so the
-- second member silently receives `recepty-2` — a slug that discloses a sibling
-- they cannot see — and both requests succeed. The index below is therefore only
-- HALF the fix; the store's collision query carries the same scope terms, and the
-- two move in one commit.
--
-- The 'root:' literal cannot collide with a real UUIDv7 parent id, which never
-- begins with those characters.
-- +goose StatementBegin
DROP INDEX ux_folders_sibling_slug;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_folders_sibling_slug ON folders (
    COALESCE(parent_id, 'root:' || visibility || ':' || COALESCE(owner_id, '')), slug
) WHERE archived = 0;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX ux_notes_sibling_slug;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_notes_sibling_slug ON notes (
    COALESCE(folder_id, 'root:' || visibility || ':' || COALESCE(owner_id, '')), slug
) WHERE archived = 0;
-- +goose StatementEnd

-- Partial lookup index for the private rows: the storage snapshot and the purge
-- listing both select exactly this subset, and it is a small fraction of the table.
-- +goose StatementBegin
CREATE INDEX idx_notes_owner_scope ON notes (owner_id, visibility) WHERE visibility = 'private';
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_folders_owner_scope ON folders (owner_id, visibility) WHERE visibility = 'private';
-- +goose StatementEnd

-- Free the RESERVED ROOT SLUG on rows that already exist (D185).
--
-- The SPA routes /poznamky/soukrome/… as a literal, so `soukrome` may not be a
-- live slug at the SHARED root. The service enforces that from now on —
-- freeSlug's isReservedRootSlug hands a new or renamed "Soukromé" the suffix
-- instead — but it only ever runs on a create, a rename or an unarchive. A folder
-- named "Soukromé" that has been sitting at the root since v3 was never offered to
-- it, and after this migration its URL is permanently swallowed by the private
-- tree: the folder and everything under it become unreachable by path, with no
-- error anywhere. Enforcement that starts today has to be paired with a backfill
-- for yesterday.
--
-- Only LIVE ROOT-LEVEL SHARED rows are touched, because that is exactly the set
-- the route can collide with: an archived row is out of the live sibling index and
-- gets re-slugged by freeSlug on the way back in.
--
-- ⚠ The CASE is not decoration — it is what keeps this migration from failing the
-- boot. `soukrome-2` may itself be taken, and a UNIQUE violation here does not
-- degrade, it crash-loops the container. The fallback suffix is the row's own id,
-- which is unique because it is the PRIMARY KEY. The notes statement runs after the
-- folders one and re-reads both tables, so it also steps around whatever the folder
-- just took.
--
-- ⚠ THE WHOLE id, NOT substr(id, 1, 8). The truncated form was used here first and
-- described as "unique by construction", which it is not: these are UUIDv7s, whose
-- leading hex characters are MILLISECOND TIMESTAMP BITS — the first eight are
-- shared by every id minted in the same ~65-second window. An ugly slug on a row
-- that was already having its URL rewritten is worth nothing next to a boot-time
-- UNIQUE violation. The same correction applies to the Down block below and to
-- documents' 07004.
-- +goose StatementBegin
UPDATE folders SET slug = CASE
    WHEN EXISTS (SELECT 1 FROM folders f2 WHERE f2.archived = 0 AND f2.parent_id IS NULL
                   AND f2.visibility = 'shared' AND f2.slug = 'soukrome-2')
      OR EXISTS (SELECT 1 FROM notes n2 WHERE n2.archived = 0 AND n2.folder_id IS NULL
                   AND n2.visibility = 'shared' AND n2.slug = 'soukrome-2')
    THEN 'soukrome-' || id
    ELSE 'soukrome-2' END
WHERE archived = 0 AND parent_id IS NULL AND visibility = 'shared' AND slug = 'soukrome';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE notes SET slug = CASE
    WHEN EXISTS (SELECT 1 FROM folders f2 WHERE f2.archived = 0 AND f2.parent_id IS NULL
                   AND f2.visibility = 'shared' AND f2.slug = 'soukrome-2')
      OR EXISTS (SELECT 1 FROM notes n2 WHERE n2.archived = 0 AND n2.folder_id IS NULL
                   AND n2.visibility = 'shared' AND n2.slug = 'soukrome-2')
    THEN 'soukrome-' || id
    ELSE 'soukrome-2' END
WHERE archived = 0 AND folder_id IS NULL AND visibility = 'shared' AND slug = 'soukrome';
-- +goose StatementEnd

-- +goose Down

-- ⚠ DROP THE INDEXES BEFORE THE COLUMNS (D200). SQLite refuses to drop a column an
-- index references, so the reverse order fails halfway and leaves the table wedged.
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_folders_owner_scope;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_notes_owner_scope;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS ux_notes_sibling_slug;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS ux_folders_sibling_slug;
-- +goose StatementEnd

-- Re-slug live rows that would collide under the restored UNSCOPED key. The v9
-- index legally admits a private root 'recepty' beside a shared one; recreating
-- the pre-v9 index over both hits a UNIQUE violation and aborts the rollback
-- after the columns are already gone. Private rows yield — shared slugs keep
-- their pre-v9 URLs — and the fallback suffix is the row's WHOLE id, unique because
-- it is the PRIMARY KEY (the same suffix the Up block uses for 'soukrome').
--
-- ⚠ Not substr(id, 1, 8), which is what this was and which is NOT unique: a
-- UUIDv7's leading hex digits are millisecond timestamp bits, shared by every id
-- minted in the same ~65-second window. Two members creating a private "Recepty"
-- in one onboarding session would collide on the very statement that exists to
-- prevent a collision — aborting the rollback with the columns already dropped.
-- Runs while `visibility` still exists, so it must precede the column drops.
-- +goose StatementBegin
UPDATE folders SET slug = slug || '-' || id
WHERE archived = 0 AND visibility = 'private'
  AND EXISTS (SELECT 1 FROM folders f2 WHERE f2.archived = 0 AND f2.id <> folders.id
                AND COALESCE(f2.parent_id, '') = COALESCE(folders.parent_id, '')
                AND f2.slug = folders.slug);
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE notes SET slug = slug || '-' || id
WHERE archived = 0 AND visibility = 'private'
  AND EXISTS (SELECT 1 FROM notes n2 WHERE n2.archived = 0 AND n2.id <> notes.id
                AND COALESCE(n2.folder_id, '') = COALESCE(notes.folder_id, '')
                AND n2.slug = notes.slug);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE notes DROP COLUMN owner_id;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE notes DROP COLUMN visibility;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE folders DROP COLUMN owner_id;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE folders DROP COLUMN visibility;
-- +goose StatementEnd
-- Restore the pre-v9 sentinel so a rolled-back schema still dedupes root siblings.
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_folders_sibling_slug ON folders (COALESCE(parent_id, ''), slug) WHERE archived = 0;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_notes_sibling_slug ON notes (COALESCE(folder_id, ''), slug) WHERE archived = 0;
-- +goose StatementEnd
