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
		sess                                       Session
		displayName, rolesJSON                     string
		rolesRefreshed, lastSeen, expires, revoked sql.NullString
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
	sess.DisplayName = displayName
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

// RefreshRoles caches freshly-minted roles and stamps roles_refreshed_at.
func (s *SessionStore) RefreshRoles(ctx context.Context, id string, roles []string, at time.Time) error {
	b, _ := json.Marshal(roles)
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET roles = ?, roles_refreshed_at = ? WHERE id = ?`,
		string(b), at.UTC().Format(tsLayout), id)
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
