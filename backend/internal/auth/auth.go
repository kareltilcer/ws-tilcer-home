// Package auth verifies site-scoped JWTs against the shared auth service via
// token introspection (PRD D2, Mode A). The backend never holds the JWT signing
// secret; auth's /introspect is authoritative. Results are cached for the
// token's remaining TTL so the dashboard (the landing route) does not put an
// auth round-trip on every page load.
//
// Everything is behind the Introspector interface, so the exact wire contract
// (see introspect.go) is the ONLY thing that changes once the auth service's
// docs are confirmed, and tests run against a fake with no network.
package auth

import (
	"context"
	"time"
)

// Claims is the result of introspecting a token.
type Claims struct {
	Active    bool      // false ⇒ token unknown/expired/revoked ⇒ treat as unauthenticated
	UserID    string    // auth subject id
	Roles     []string  // e.g. ["editor"], ["admin"], or ["*"] (superuser)
	Label     string    // human-readable actor label for the log browser
	ExpiresAt time.Time // token expiry; bounds how long the result may be cached
}

// Introspector verifies a bearer token and returns its claims.
type Introspector interface {
	Introspect(ctx context.Context, token string) (Claims, error)
}
