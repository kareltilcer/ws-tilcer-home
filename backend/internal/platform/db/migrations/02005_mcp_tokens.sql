-- platform core — MCP personal access tokens (v11, PRD §V11-5, D283/D284/D285,
-- HANDOFF-13 §2.1). Version block 02005, beside 02001_sessions, because the table
-- belongs to no module: `auth` writes it, the MCP host reads it, `admin` lists it,
-- and none of the three may import another.
--
-- A token is a SECOND CREDENTIAL FOR THE SAME IDENTITY. It carries its owner's id
-- and, at call time, its owner's live roles; it is refused everything the member
-- would be refused. What makes it different is that nobody is watching a screen,
-- which is why nothing destructive is exposed to it at all.
--
-- Only the SHA-256 of the secret is stored — never the secret. A restored replica
-- therefore grants nothing, which is the property that makes "you cannot see it
-- again" worth the complaint.

-- +goose Up

-- +goose StatementBegin
CREATE TABLE mcp_tokens (
    id            TEXT PRIMARY KEY,               -- UUIDv7
    user_id       TEXT NOT NULL,                  -- auth user id (sub)
    name          TEXT NOT NULL,                  -- "Claude na notebooku"
    -- ⚠ UNIQUE, and that index IS the lookup path: resolution is one seek per
    -- call with no positive cache anywhere (D287), which is what makes a revoke
    -- take effect on the next call rather than on the next restart.
    token_hash    TEXT NOT NULL UNIQUE,           -- SHA-256 hex of the secret
    prefix        TEXT NOT NULL,                  -- first 8 chars of the secret, for the UI
    modules       TEXT NOT NULL DEFAULT '[]',     -- JSON array; [] ⇒ every module the owner can see
    created_at    TEXT NOT NULL,                  -- RFC3339 UTC
    -- ⚠ WRITTEN ONCE AT MINT AND NEVER TOUCHED AGAIN (D285). The resemblance to
    -- `sessions` is a trap: a session slides on use (bounded by touchGranularity),
    -- and a token that renewed itself on every call would be a token that never
    -- expires — which is the option that was not chosen. PATCH cannot change it;
    -- extending a token's life is minting a new one.
    expires_at    TEXT,                           -- NULL ⇒ never
    last_used_at  TEXT,                           -- written at most once per hour (D286)
    last_used_ip  TEXT,
    -- ⚠ A revoke STAMPS, it does not delete. An audit event's via_token_id must
    -- still resolve to a name years later, so the row outlives the credential.
    revoked_at    TEXT
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_mcp_tokens_user ON mcp_tokens (user_id);
-- +goose StatementEnd

-- ⚠ THERE IS NO `roles` COLUMN, AND THERE MUST NOT BE ONE. `sessions` caches roles
-- because a session is minted at login and lives for weeks; a token reads its
-- owner's roles live, re-minted on the same HOME_ROLE_REFRESH_MINUTES threshold.
-- A roles column here would be a second, staler answer to a question that already
-- has one, and it is exactly how a token comes to outlive a demotion.

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS mcp_tokens;
-- +goose StatementEnd
