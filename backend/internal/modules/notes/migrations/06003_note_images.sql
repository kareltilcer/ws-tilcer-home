-- notes module — inline image blobs (Poznámky image support). Version block 06003:
-- stays in the notes block of the one Goose sequence (…notes(06) → documents(07),
-- see bootstrap.MigrationSources) so it applies before the documents tables.
--
-- Pasted/dropped images no longer live inline in body_md as base64 data URIs (which
-- blew past the 1 MiB API body cap and froze the editor). The BYTES go to object
-- storage under the note-images/ prefix (blobstore, like documents) and body_md
-- holds only a small `![](/api/notes/images/{id})` reference. This table is the
-- id → object metadata map the content endpoint, the garbage collector, and the
-- backup mirror/reconciliation pass read.

-- +goose Up

-- One row per stored image. note_id is the OWNING note: the image is uploaded while
-- editing that note, and GC keys on it (an image no longer referenced by its owner's
-- body_md is deleted). ON DELETE CASCADE drops the rows when a note is hard-deleted;
-- the reconciliation pass then removes the now-unclaimed objects. The FK targets
-- notes(id) — the logical TEXT key, which is UNIQUE — not the seq rowid.
-- +goose StatementBegin
CREATE TABLE note_images (
    id           TEXT PRIMARY KEY,
    note_id      TEXT NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    content_type TEXT NOT NULL,                                        -- server-sniffed image/* (never the client's claim)
    byte_size    INTEGER NOT NULL,
    checksum     TEXT NOT NULL,                                        -- sha-256 hex; serves as the content ETag
    created_by   TEXT,
    created_at   TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_note_images_note ON note_images (note_id);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS note_images;
-- +goose StatementEnd
