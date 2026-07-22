package httpx

import (
	"net/http"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/reqctx"
)

// AuthConfig configures the authentication middleware. When BypassActor is
// non-nil the dev bypass is active: every request is authenticated as that
// actor and introspection is skipped (development only — refused in production
// by config validation).
type AuthConfig struct {
	Introspector auth.Introspector
	BypassActor  *reqctx.Actor
}

// Authenticate verifies the bearer token (via introspection) and stores the
// actor in the request context. It does not gate roles — reads are open to any
// authenticated user; RequireWrite/RequireAdmin gate mutations and logs.
func Authenticate(ac AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ac.BypassActor != nil {
				ctx := reqctx.WithActor(r.Context(), *ac.BypassActor)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			token := bearerToken(r)
			if token == "" {
				WriteError(w, ErrUnauthorized("missing bearer token"))
				return
			}
			claims, err := ac.Introspector.Introspect(r.Context(), token)
			if err != nil {
				WriteError(w, ErrUnauthorized("token verification failed"))
				return
			}
			if !claims.Active {
				WriteError(w, ErrUnauthorized("inactive or invalid token"))
				return
			}
			actor := reqctx.Actor{
				UserID: claims.UserID,
				Type:   "user",
				Label:  claims.Label,
				Roles:  claims.Roles,
			}
			next.ServeHTTP(w, r.WithContext(reqctx.WithActor(r.Context(), actor)))
		})
	}
}

// RequireWrite allows editor or admin (or the "*" superuser). Must run after
// Authenticate.
func RequireWrite(next http.Handler) http.Handler {
	return requireRole(next, "editor", "admin")
}

// RequireAdmin allows only admin (or the "*" superuser). Must run after
// Authenticate.
func RequireAdmin(next http.Handler) http.Handler {
	return requireRole(next, "admin")
}

func requireRole(next http.Handler, allowed ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := reqctx.ActorFrom(r.Context())
		if !ok {
			// Authenticate did not run / no actor — treat as unauthenticated.
			WriteError(w, ErrUnauthorized("authentication required"))
			return
		}
		if !reqctx.HasRole(actor.Roles, allowed...) {
			WriteError(w, ErrForbidden("insufficient role"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}
