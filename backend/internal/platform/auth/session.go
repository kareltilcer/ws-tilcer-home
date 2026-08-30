package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
)

// tsLayout is the timestamp format for the sessions table (RFC3339 UTC). Session
// timestamps are compared as parsed time.Time, not as strings, so any fixed
// layout is fine.
const tsLayout = time.RFC3339Nano

// Session is a live home session (Mode B). Only the SHA-256 hash of the cookie
// token is ever stored (D29); the raw token exists only in the browser cookie.
type Session struct {
	ID               string
	UserID           string
	Email            string
	DisplayName      string
	Roles            []string
	RolesRefreshedAt time.Time
	LastSeenAt       time.Time
	ExpiresAt        time.Time
}

// identity is what the session currently says its owner is — the identity auth
// handed over at login, or the one the last successful re-mint replaced it with.
// Every request authorised from this row acts under it.
func (s Session) identity() Identity {
	return Identity{UserID: s.UserID, Email: s.Email, DisplayName: s.DisplayName, Roles: s.Roles}
}

// SessionStore persists home sessions in the platform `sessions` table.
type SessionStore struct{ db *sql.DB }

// NewSessionStore returns a session store over db.
func NewSessionStore(db *sql.DB) *SessionStore { return &SessionStore{db: db} }

// newToken returns a 256-bit URL-safe random token and its SHA-256 hash. Only the
// hash is stored; the raw token goes to the cookie and is never persisted.
func newToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("session: generate token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw), nil
}

// hashToken derives the `sessions.token_hash` column: the raw cookie token never
// touches the database, so every lookup and revoke goes through here.
//
// ⚠ platform/ws has a byte-identical hashPumpToken, and it is NOT this function
// under another name. That one is only a map-key for "did two sockets hand over
// the same string?" and is never compared against a value auth produced, so this
// one is free to grow a salt or a KDF without going to look for it. See
// ws.hashPumpToken, which says the same from its side.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Create inserts a session for id within tx (so it commits atomically with the
// caller's platform.login audit event) and returns the raw cookie token and the
// new session id.
func (s *SessionStore) Create(ctx context.Context, tx *sql.Tx, id Identity, userAgent, ip string, ttl time.Duration, now time.Time) (rawToken, sessionID string, err error) {
	raw, hash, err := newToken()
	if err != nil {
		return "", "", err
	}
	roles, _ := json.Marshal(id.Roles)
	sessionID = idgen.New()
	now = now.UTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sessions
		   (id, user_id, token_hash, email, display_name, roles, roles_refreshed_at,
		    user_agent, ip, created_at, last_seen_at, expires_at, revoked_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,NULL)`,
		sessionID, id.UserID, hash, id.Email, nullStr(id.DisplayName), string(roles), now.Format(tsLayout),
		nullStr(userAgent), nullStr(ip), now.Format(tsLayout), now.Format(tsLayout),
		now.Add(ttl).Format(tsLayout),
	); err != nil {
		return "", "", fmt.Errorf("session: insert: %w", err)
	}
	return raw, sessionID, nil
}

// Lookup resolves a raw cookie token to a live session, rejecting missing,
// revoked, and expired rows. The second return is false when no live session
// matches.
func (s *SessionStore) Lookup(ctx context.Context, rawToken string, now time.Time) (Session, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, email, display_name, roles, roles_refreshed_at, last_seen_at, expires_at, revoked_at
		   FROM sessions WHERE token_hash = ?`, hashToken(rawToken))
	var (
		sess                                                    Session
		rolesJSON                                               string
		displayName, rolesRefreshed, lastSeen, expires, revoked sql.NullString
	)
	if err := row.Scan(&sess.ID, &sess.UserID, &sess.Email, &displayName, &rolesJSON,
		&rolesRefreshed, &lastSeen, &expires, &revoked); err != nil {
		if err == sql.ErrNoRows {
			return Session{}, false, nil
		}
		return Session{}, false, err
	}
	if revoked.Valid && revoked.String != "" {
		return Session{}, false, nil
	}
	sess.DisplayName = displayName.String
	_ = json.Unmarshal([]byte(rolesJSON), &sess.Roles)
	sess.RolesRefreshedAt = parseTS(rolesRefreshed.String)
	sess.LastSeenAt = parseTS(lastSeen.String)
	sess.ExpiresAt = parseTS(expires.String)
	if !sess.ExpiresAt.IsZero() && !now.UTC().Before(sess.ExpiresAt) {
		return Session{}, false, nil // expired
	}
	return sess, true, nil
}

// Touch slides last_seen_at and expires_at forward (FR-A2). Called lazily by the
// middleware (only when last_seen is stale) so the landing route doesn't write on
// every request.
func (s *SessionStore) Touch(ctx context.Context, id string, lastSeen, expires time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?`,
		lastSeen.UTC().Format(tsLayout), expires.UTC().Format(tsLayout), id)
	return err
}

// RefreshIdentity caches a freshly-minted identity — roles, and the email and
// display name that ride in the same token — and stamps roles_refreshed_at.
//
// ⚠ IT WRITES THE WHOLE IDENTITY BECAUSE THIS ROW IS THE ONLY RECORD HOME HAS OF
// WHO A MEMBER IS. Home has no user table (auth owns identity), so `sessions` is
// what push.Store.Members projects the household directory from — which is the
// author label on every chat message, every members-panel row, the add-member
// picker, the "Vybraným lidem" audience and the delivery log. Caching only the
// roles left all of that frozen at login: a member who renamed themselves in auth
// went on being shown under their old name to everybody until they next logged in,
// which behind a 90-day sliding session is effectively never. The mint already
// returns the whole identity — this used to fetch it every fifteen minutes and
// throw everything but the roles away.
//
// ⚠ THE COLUMN IS STILL CALLED roles_refreshed_at, and it still means what it
// says: it is the re-mint threshold's stamp, and the re-mint is what brings the
// name along. Renaming it would be a migration over the one table every request
// reads, for a word.
//
// ⚠ CLEARING IS THE CALLER'S DECISION AND IT IS NOT TAKEN HERE — see
// mergeIdentity. What arrives is what should be stored.
func (s *SessionStore) RefreshIdentity(ctx context.Context, sessionID string, id Identity, at time.Time) error {
	b, _ := json.Marshal(id.Roles)
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET roles = ?, email = ?, display_name = ?, roles_refreshed_at = ? WHERE id = ?`,
		string(b), id.Email, nullStr(id.DisplayName), at.UTC().Format(tsLayout), sessionID)
	return err
}

// RevokeByID marks a session revoked (used by the middleware when a mint
// fails closed). Idempotent.
func (s *SessionStore) RevokeByID(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(tsLayout), id)
	return err
}

// RevokeByToken marks the session for rawToken revoked within tx (so logout is
// atomic with its platform.logout audit event) and returns the session's user id
// and id. ok is false when no live session matched.
func (s *SessionStore) RevokeByToken(ctx context.Context, tx *sql.Tx, rawToken string, now time.Time) (userID, sessionID string, ok bool, err error) {
	row := tx.QueryRowContext(ctx,
		`SELECT id, user_id FROM sessions WHERE token_hash = ? AND revoked_at IS NULL`, hashToken(rawToken))
	if err := row.Scan(&sessionID, &userID); err != nil {
		if err == sql.ErrNoRows {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ? WHERE id = ?`, now.UTC().Format(tsLayout), sessionID); err != nil {
		return "", "", false, err
	}
	return userID, sessionID, true, nil
}

func parseTS(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(tsLayout, s)
	if err != nil {
		// Tolerate a plain RFC3339 value written by an older row.
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t.UTC()
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
