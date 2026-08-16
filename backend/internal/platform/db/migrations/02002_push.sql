-- platform core — the shared Web Push channel (PRD §V5-5, HANDOFF-7 §1, D52/D53).
-- Version block 02000, alongside the session store: these tables are PLATFORM's,
-- not the admin module's, because ANY module may send through the channel and a
-- member's consent must outlive any one module.
--
-- One subscription row per browser endpoint (a member has as many rows as they
-- have devices). Consent and mutes are per-user; the admin module configures
-- WHAT is sent, never WHETHER a given device receives it (D53).

-- +goose Up

-- +goose StatementBegin
CREATE TABLE push_subscriptions (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL,
    endpoint      TEXT NOT NULL UNIQUE,                    -- the browser's push service URL
    p256dh        TEXT NOT NULL,                           -- base64url client public key
    auth          TEXT NOT NULL,                           -- base64url client auth secret
    user_agent    TEXT,
    created_at    TEXT NOT NULL,
    last_seen_at  TEXT NOT NULL,
    failing_since TEXT                                     -- non-NULL while sends keep failing
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_push_subs_user ON push_subscriptions (user_id);
-- +goose StatementEnd

-- Per-user mute preferences: a master switch plus one toggle per category
-- (D53a). An ABSENT row means "all on" — the row is created lazily on the first
-- PATCH, so a member who never opens the panel still receives everything.
-- +goose StatementBegin
CREATE TABLE notification_preferences (
    user_id        TEXT PRIMARY KEY,
    enabled        INTEGER NOT NULL DEFAULT 1,
    cat_broadcast  INTEGER NOT NULL DEFAULT 1,
    cat_triggers   INTEGER NOT NULL DEFAULT 1,
    cat_summaries  INTEGER NOT NULL DEFAULT 1,
    updated_at     TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS notification_preferences;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS push_subscriptions;
-- +goose StatementEnd
