-- dashboard module — per-user widget layout (PRD §5, HANDOFF-4, D24). Version
-- block 05000. This is the WHOLE module's schema: the host owns no feature data,
-- only each user's arrangement (which widgets are visible, their order, size).

-- +goose Up

-- +goose StatementBegin
CREATE TABLE user_dashboard_layout (
    user_id    TEXT NOT NULL,
    widget_key TEXT NOT NULL,
    visible    INTEGER NOT NULL DEFAULT 1,
    position   TEXT NOT NULL,                                     -- lexorank order key
    size       TEXT NOT NULL DEFAULT 'narrow' CHECK (size IN ('narrow', 'wide')),
    PRIMARY KEY (user_id, widget_key)
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_layout_user ON user_dashboard_layout (user_id);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS user_dashboard_layout;
-- +goose StatementEnd
