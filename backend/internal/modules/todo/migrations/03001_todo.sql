-- todo module — the Úkoly board tables (PRD §5, HANDOFF-2). Version block 03000.
--
-- Conventions: ids are UUIDv7 as TEXT; `position` columns are lexorank-style TEXT
-- keys; TIMESTAMP columns hold RFC3339 UTC text; booleans are INTEGER 0/1. The
-- default board is NOT seeded here — seeding is done in Go guarded by an
-- empty-boards check, so a Litestream-restored build does not double-seed.

-- +goose Up

-- +goose StatementBegin
CREATE TABLE boards (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT,
    position    TEXT NOT NULL,
    created_by  TEXT,
    created_at  TEXT NOT NULL,
    archived    INTEGER NOT NULL DEFAULT 0
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_boards_position ON boards (position);
-- +goose StatementEnd

-- columns.kind is a free-form, NON-UNIQUE hint (PRD D7): a board may have several
-- 'now' and several 'done' columns. The (kind) index serves the dashboard's
-- cross-board kind='now' widget query.
-- +goose StatementBegin
CREATE TABLE columns (
    id         TEXT PRIMARY KEY,
    board_id   TEXT NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    priority   INTEGER NOT NULL DEFAULT 0,
    position   TEXT NOT NULL,
    kind       TEXT NOT NULL DEFAULT 'normal' CHECK (kind IN ('normal', 'now', 'done')),
    created_at TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_columns_board_position ON columns (board_id, position);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_columns_board_priority ON columns (board_id, priority);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_columns_kind ON columns (kind);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE cards (
    id         TEXT PRIMARY KEY,
    column_id  TEXT NOT NULL REFERENCES columns (id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    notes      TEXT,                                              -- markdown
    position   TEXT NOT NULL,
    created_by TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    done_at    TEXT,                                              -- set on entering a done column
    archived   INTEGER NOT NULL DEFAULT 0
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_cards_column_position ON cards (column_id, position);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_cards_updated ON cards (updated_at);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE card_links (
    id       TEXT PRIMARY KEY,
    card_id  TEXT NOT NULL REFERENCES cards (id) ON DELETE CASCADE,
    url      TEXT NOT NULL,
    title    TEXT,
    position TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_card_links_card_position ON card_links (card_id, position);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE checklist_items (
    id       TEXT PRIMARY KEY,
    card_id  TEXT NOT NULL REFERENCES cards (id) ON DELETE CASCADE,
    text     TEXT NOT NULL,
    done     INTEGER NOT NULL DEFAULT 0,
    position TEXT NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_checklist_card_position ON checklist_items (card_id, position);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE labels (
    id         TEXT PRIMARY KEY,
    board_id   TEXT NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    color      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (board_id, name)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE card_labels (
    card_id  TEXT NOT NULL REFERENCES cards (id) ON DELETE CASCADE,
    label_id TEXT NOT NULL REFERENCES labels (id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, label_id)
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_card_labels_label ON card_labels (label_id);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS card_labels;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS labels;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS checklist_items;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS card_links;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS cards;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS columns;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS boards;
-- +goose StatementEnd
