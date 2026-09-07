package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
)

// The MCP personal access token store (v11, PRD §V11-4 FR-M3, D283–D289).
//
// ⚠ IT LIVES IN platform/auth AND NOT IN platform/mcp, deliberately: it is a
// CREDENTIAL store and it belongs beside the other one. `auth` writes it, the MCP
// host reads it, `admin` lists it, and none of the three may import a feature
// module. The HTTP handlers for /api/mcp/tokens live in platform/mcp — those are
// the MCP feature's own surface — so platform/mcp imports platform/auth and
// nothing imports back.
//
// ⚠ THE RESEMBLANCE TO SessionStore IS A TRAP IN EXACTLY ONE PLACE: a session
// SLIDES on use and a token's expiry does not (D285). Everything else here is
// deliberately the same — hash-only storage, a lazy last-used stamp bounded by
// touchGranularity, revoke-by-stamping — because the two are the same kind of
// object and a second set of habits is how one of them drifts.

// TokenPrefix is the literal every MCP secret starts with.
//
// ⚠ IT IS NOT DECORATION. It makes a leaked token greppable in a paste, a config
// file and a log, which is the only cheap mitigation that exists for a credential
// that lives on somebody's laptop (D284). `prefix` below is the first 8
// characters of the WHOLE string — this literal plus three — so a member can match
// a row in Nastavení to a line in their client's config by eye.
const TokenPrefix = "hmcp_"

// prefixLen is how much of the secret is stored in the clear.
const prefixLen = 8

// ErrTokenNotFound is returned when no live token matches. ⚠ Callers must map it
// to the SAME answer an unknown id gets — never "revoked", never "expired". Which
// of the three it was is exactly the thing an attacker holding a guessed token
// would like to learn.
var ErrTokenNotFound = errors.New("auth: mcp token not found")

// MCPToken is one credential row.
type MCPToken struct {
	ID     string
	UserID string
	Name   string
	Prefix string
	// Modules is the owner's convenience narrowing: which modules this token is
	// offered. ⚠ Empty means "every module the owner can see", and it NARROWS AND
	// NEVER WIDENS (D288). It is not an access control and the PRD says so out
	// loud — the three access axes are enforced underneath it exactly as they are
	// for a browser session.
	Modules    []string
	CreatedAt  time.Time
	ExpiresAt  time.Time // zero ⇒ never
	LastUsedAt time.Time // zero ⇒ never used
	LastUsedIP string
	RevokedAt  time.Time // non-zero ⇒ revoked
}

// Live reports whether this token would authenticate a call at `now`.
func (t MCPToken) Live(now time.Time) bool {
	if !t.RevokedAt.IsZero() {
		return false
	}
	return t.ExpiresAt.IsZero() || now.UTC().Before(t.ExpiresAt)
}

// MCPTokenStore persists MCP tokens in the platform `mcp_tokens` table.
type MCPTokenStore struct{ db *sql.DB }

// NewMCPTokenStore returns a token store over db.
func NewMCPTokenStore(db *sql.DB) *MCPTokenStore { return &MCPTokenStore{db: db} }

// NewMCPSecret returns a fresh secret, its SHA-256 hex, and its stored prefix.
//
// ⚠ There is no timing side channel to protect against here and no
// subtle.ConstantTimeCompare anywhere in this file, because lookup is BY HASH —
// one seek on a unique index, never a scan-and-compare. If somebody later adds a
// prefix-then-compare path, that stops being true.
func NewMCPSecret() (secret, hash, prefix string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("auth: generate mcp token: %w", err)
	}
	secret = TokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(secret))
	return secret, hex.EncodeToString(sum[:]), secret[:prefixLen], nil
}

// HashMCPSecret derives the `mcp_tokens.token_hash` column. The raw secret never
// touches the database, so every lookup goes through here.
func HashMCPSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

const mcpTokenCols = `id, user_id, name, prefix, modules, created_at, expires_at,
	last_used_at, last_used_ip, revoked_at`

// Create inserts a token within tx, so it commits atomically with the caller's
// `mcp.token.create` audit event. hash and prefix come from NewMCPSecret; the
// secret itself is never passed in and never stored.
//
// expiresAt is computed by the caller at mint and WRITTEN ONCE. Nothing in this
// file ever updates it (D285).
func (s *MCPTokenStore) Create(ctx context.Context, tx *sql.Tx, userID, name, hash, prefix string, modules []string, expiresAt time.Time, now time.Time) (MCPToken, error) {
	if modules == nil {
		modules = []string{}
	}
	mods, err := json.Marshal(modules)
	if err != nil {
		return MCPToken{}, fmt.Errorf("auth: marshal mcp token modules: %w", err)
	}
	t := MCPToken{
		ID:        idgen.New(),
		UserID:    userID,
		Name:      name,
		Prefix:    prefix,
		Modules:   modules,
		CreatedAt: now.UTC(),
		ExpiresAt: expiresAt,
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO mcp_tokens (id, user_id, name, token_hash, prefix, modules, created_at, expires_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		t.ID, userID, name, hash, prefix, string(mods), t.CreatedAt.Format(tsLayout), nullTime(expiresAt),
	); err != nil {
		return MCPToken{}, fmt.Errorf("auth: insert mcp token: %w", err)
	}
	return t, nil
}

// LookupBySecret resolves a raw bearer to its row — including a revoked or
// expired one, because the CALLER decides what those mean and this is the one
// place both answers are available.
//
// ⚠ One indexed seek, every call, with NO POSITIVE CACHE anywhere (D287). That is
// what makes a revoke take effect on the very next call rather than on the next
// restart, and it is the whole reason the hash column is UNIQUE.
func (s *MCPTokenStore) LookupBySecret(ctx context.Context, secret string) (MCPToken, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+mcpTokenCols+` FROM mcp_tokens WHERE token_hash = ?`, HashMCPSecret(secret))
	t, err := scanMCPToken(row)
	if err == sql.ErrNoRows {
		return MCPToken{}, ErrTokenNotFound
	}
	return t, err
}

// Get returns one token by id, whatever its state.
func (s *MCPTokenStore) Get(ctx context.Context, id string) (MCPToken, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+mcpTokenCols+` FROM mcp_tokens WHERE id = ?`, id)
	t, err := scanMCPToken(row)
	if err == sql.ErrNoRows {
		return MCPToken{}, ErrTokenNotFound
	}
	return t, err
}

// ListByUser returns one member's tokens, newest first.
//
// ⚠ Revoked and expired rows are INCLUDED. A token named in a Log row must stay
// identifiable years later, and a list that hid dead rows would leave a member
// unable to explain a chip.
func (s *MCPTokenStore) ListByUser(ctx context.Context, userID string) ([]MCPToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+mcpTokenCols+` FROM mcp_tokens WHERE user_id = ? ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	return appdb.Collect(rows, scanMCPToken)
}

// ListAll returns every member's tokens, newest first — the admin listing.
func (s *MCPTokenStore) ListAll(ctx context.Context) ([]MCPToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+mcpTokenCols+` FROM mcp_tokens ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	return appdb.Collect(rows, scanMCPToken)
}

// CountLive returns how many of a member's tokens would authenticate right now —
// the figure HOME_MCP_MAX_TOKENS_PER_USER bounds (D289).
//
// ⚠ Not a security boundary, a hygiene one: a list nobody can read is a list
// nobody revokes from.
func (s *MCPTokenStore) CountLive(ctx context.Context, userID string, now time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mcp_tokens
		  WHERE user_id = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`,
		userID, now.UTC().Format(tsLayout)).Scan(&n)
	return n, err
}

// Update changes a token's name and module scope within tx, so it commits with
// the caller's `mcp.token.update` audit event.
//
// ⚠ THERE IS NO EXPIRY PARAMETER AND THERE MUST NOT BE ONE (D285). Extending a
// token's life is minting a new one, which is a decision a member should have to
// take deliberately.
func (s *MCPTokenStore) Update(ctx context.Context, tx *sql.Tx, id, name string, modules []string) error {
	if modules == nil {
		modules = []string{}
	}
	mods, err := json.Marshal(modules)
	if err != nil {
		return fmt.Errorf("auth: marshal mcp token modules: %w", err)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE mcp_tokens SET name = ?, modules = ? WHERE id = ?`, name, string(mods), id)
	if err != nil {
		return fmt.Errorf("auth: update mcp token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTokenNotFound
	}
	return nil
}

// Revoke stamps revoked_at within tx. Idempotent: re-revoking a revoked token
// changes nothing and is not an error, because the member's answer to "is this
// key dead" is the same either way.
//
// ⚠ A REVOKE, NOT A DELETE. The row stays so an audit event's `via_token_id`
// still resolves to a name years later, which is the whole reason the Log's chip
// can be trusted.
func (s *MCPTokenStore) Revoke(ctx context.Context, tx *sql.Tx, id string, now time.Time) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE mcp_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		now.UTC().Format(tsLayout), id)
	return err
}

// RevokeByID stamps revoked_at outside a transaction. Used by the MCP bearer
// path when a re-mint says the owner's account is closed (D287) — there is no
// caller transaction there, and the decision has already been taken.
func (s *MCPTokenStore) RevokeByID(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE mcp_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		now.UTC().Format(tsLayout), id)
	return err
}

// Touch records that a token was used, from ip, at now.
//
// ⚠ THE CALLER GATES THIS ON touchGranularity AND THAT IS NOT AN OPTIMISATION
// (D286). The pool is capped at ONE connection: without the gate a read-only
// conversation of forty tool calls does forty writes to this row, every read tool
// becomes a writer, and the household's browser queues behind them. `last_used_at`
// is therefore accurate to within an hour, which is what the column is for — a
// member deciding in eighteen months whether they still need a key.
func (s *MCPTokenStore) Touch(ctx context.Context, id string, at time.Time, ip string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE mcp_tokens SET last_used_at = ?, last_used_ip = ? WHERE id = ?`,
		at.UTC().Format(tsLayout), nullStr(ip), id)
	return err
}

func scanMCPToken(row appdb.Scanner) (MCPToken, error) {
	var (
		t                                  MCPToken
		modsJSON                           string
		created                            string
		expires, lastUsed, lastIP, revoked sql.NullString
	)
	if err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &modsJSON, &created,
		&expires, &lastUsed, &lastIP, &revoked); err != nil {
		return MCPToken{}, err
	}
	// A malformed modules column degrades to "every module the owner can see",
	// which is the value the column defaults to and the only safe reading: the
	// alternative is a token that silently reaches nothing and a member with no
	// way to tell that from a broken server.
	_ = json.Unmarshal([]byte(modsJSON), &t.Modules)
	if t.Modules == nil {
		t.Modules = []string{}
	}
	t.CreatedAt = parseTS(created)
	t.ExpiresAt = parseTS(expires.String)
	t.LastUsedAt = parseTS(lastUsed.String)
	t.LastUsedIP = lastIP.String
	t.RevokedAt = parseTS(revoked.String)
	return t, nil
}

// nullTime maps the zero time to SQL NULL, otherwise the formatted stamp.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(tsLayout)
}
