// Package mcpctx carries the id of the MCP token a request arrived on (v11,
// PRD §V11-4 FR-M9, D290).
//
// ⚠ IT IS ITS OWN PACKAGE FOR ONE REASON: `platform/audit` reads it and
// `platform/mcp` writes it, and `platform/mcp` already imports `platform/audit`
// to record the token lifecycle events. Putting the accessor in either of those
// two would be an import cycle. It is a leaf with no dependency but `context`,
// the same shape `platform/reqctx` has and for the same reason.
//
// ⚠ AND IT IS SEPARATE FROM reqctx.Actor ON PURPOSE. The actor on the MCP path is
// the MEMBER — real user id, live roles, Type "user" (D291) — so that every
// ownership check, membership check and `created_by` stamp in eleven modules keeps
// working with no change at all. The token is not a second identity; it is a
// second credential for the same one. What rides here is only the ANSWER TO "HOW
// DID THIS ARRIVE", which is a property of the request and not of the person.
package mcpctx

import "context"

type ctxKey int

const tokenKey ctxKey = iota

// WithToken returns a context recording that this request was authenticated by
// MCP token tokenID. Called once, by the MCP bearer middleware, after the token
// has resolved.
func WithToken(ctx context.Context, tokenID string) context.Context {
	return context.WithValue(ctx, tokenKey, tokenID)
}

// TokenFrom returns the MCP token id this request arrived on, and whether there
// was one. A browser request has none, which is the whole distinction the Log's
// `via` filter reads.
//
// ⚠ The audit sink reads this FROM THE CONTEXT rather than taking it as a
// parameter, exactly as it reads the actor — so a handler cannot forge how a
// change was made, and so that the 28 non-test Record call sites (21 across ten
// feature modules, 7 in platform itself) need no edit at all.
func TokenFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(tokenKey).(string)
	return id, ok && id != ""
}
