// Package reqctx carries request-scoped identity and metadata through the
// context. The audit spine reads the actor and request info from here — never
// from handler arguments — so a handler cannot forge who did what.
package reqctx

import "context"

// Actor is the authenticated principal behind a request.
type Actor struct {
	UserID string   // auth subject id ("" for system/service)
	Type   string   // "user" | "system" | "service"
	Label  string   // human-readable label for the log browser
	Roles  []string // e.g. ["editor"], ["admin"], or ["*"] for superuser
}

// RequestInfo is the operational metadata stamped onto audit events, tying the
// domain log plane to the stdout request log via RequestID.
type RequestInfo struct {
	RequestID string
	IP        string
	UserAgent string
	Site      string
	// ClientID is an opaque per-tab identifier the browser sends on mutating
	// requests (X-Client-Id). It is echoed back as the `origin` of the resulting
	// websocket push so a client can tell its OWN change (already applied
	// optimistically) apart from one made on another device or tab. Empty for
	// non-browser callers.
	ClientID string
}

type ctxKey int

const (
	actorKey ctxKey = iota
	requestKey
)

// WithActor returns a context carrying a.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey, a)
}

// ActorFrom returns the actor stored in ctx, if any.
func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey).(Actor)
	return a, ok
}

// WithRequest returns a context carrying r.
func WithRequest(ctx context.Context, r RequestInfo) context.Context {
	return context.WithValue(ctx, requestKey, r)
}

// RequestFrom returns the request info stored in ctx, if any.
func RequestFrom(ctx context.Context) (RequestInfo, bool) {
	r, ok := ctx.Value(requestKey).(RequestInfo)
	return r, ok
}

// ActorID returns the id of ctx's authenticated user, or "" when there is no
// actor — a system or service caller, or a code path that never went through the
// session middleware. Callers stamp it onto rows as `created_by` / `updated_by`,
// where "" is the honest answer rather than a failure.
//
// ⚠ IT LIVES HERE BECAUSE IT EXISTED NINE TIMES. `admin`, `chat`, `documents`,
// `electricity`, `events`, `finance`, `garden`, `notes` and `todo` each declared
// a byte-identical five-line wrapper over ActorFrom — the widest duplication in
// the backend, and a thin one over this package's own accessor.
func ActorID(ctx context.Context) string {
	if a, ok := ActorFrom(ctx); ok {
		return a.UserID
	}
	return ""
}

// CanWrite reports whether ctx's actor holds `editor` or `admin`. It is the
// SERVICE-layer half of the write gate; httpx.RequireWrite is the HTTP-layer
// half, and the two name the same two roles on purpose — a service reached
// through a route is gated twice, and a service reached from a job or another
// module is still gated once.
//
// No actor is not a writer: a system caller that must write does so on a path
// that does not ask.
func CanWrite(ctx context.Context) bool {
	if a, ok := ActorFrom(ctx); ok {
		return HasRole(a.Roles, "editor", "admin")
	}
	return false
}

// HasRole reports whether roles grants access to any of allowed. The superuser
// token "*" always grants access.
func HasRole(roles []string, allowed ...string) bool {
	for _, r := range roles {
		if r == "*" {
			return true
		}
		for _, a := range allowed {
			if r == a {
				return true
			}
		}
	}
	return false
}
