// Package auth implements Mode B (PRD §3, D23): home hosts its own login and owns
// its own SESSION. The browser carries NO bearer token — every request is
// authorized from the session cookie, and roles are refreshed periodically by
// re-minting against the shared auth service BE→BE. This package owns the auth
// service clients (/internal/login, /internal/token/mint), the session store, the
// session + CSRF middleware, and the /api/auth endpoints.
//
// Everything the network touches is behind the Authenticator interface, so the
// exact wire contract (see mode_b.go) is the only thing that changes once the
// auth service's Mode B docs are confirmed, and tests run against a fake with no
// network.
package auth

import (
	"context"
	"errors"
)

// Identity is a user's site-scoped assertion as returned by the auth service.
type Identity struct {
	UserID      string   // auth subject id (sub)
	Email       string   //
	DisplayName string   // may be empty
	Roles       []string // e.g. ["editor"], ["admin"], or ["*"] (superuser)
}

// Authenticator verifies credentials and refreshes roles against the shared auth
// service, BE→BE, as the `home` service client (Mode B). It replaces v1's
// per-request token introspection (D2 adjusted).
type Authenticator interface {
	// Login verifies email+password. Returns the site-scoped Identity, or one of
	// the typed errors below.
	Login(ctx context.Context, email, password string) (Identity, error)
	// Mint refreshes a user's identity/roles for the home site. Returns
	// ErrUserClosed when the user is disabled/deleted/unverified in auth (the
	// fail-closed case that drops the home session).
	Mint(ctx context.Context, userID string) (Identity, error)
}

// Typed login/mint outcomes. The HTTP handler maps each to a status (FR-A1); the
// session middleware treats ErrUserClosed as fail-closed (FR-A2).
var (
	// ErrBadCredentials — wrong email/password. Mapped to a generic 401 (never
	// reveals which field).
	ErrBadCredentials = errors.New("auth: bad credentials")
	// ErrDisabled — the account is disabled/unverified or has no access to site
	// home. Mapped to 403.
	ErrDisabled = errors.New("auth: account disabled or no access to site")
	// ErrMFARequired — auth demands MFA, which home does not handle (D23). Mapped
	// to 409; the UI points the user at auth-hosted login.
	ErrMFARequired = errors.New("auth: mfa required")
	// ErrUserClosed — a mint says the user is disabled/deleted/unverified. The
	// session middleware revokes the session and returns 401 (FR-A2).
	ErrUserClosed = errors.New("auth: user closed")
	// ErrUnreachable — the auth service is unreachable or returned 5xx. Login maps
	// it to 502; a mint treats it as transient (keep cached roles).
	ErrUnreachable = errors.New("auth: service unreachable")
)
