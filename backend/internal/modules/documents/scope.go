package documents

import (
	"context"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The root scope (v9, PRD D177/D184).
//
// Until v9 every row in `documents` was visible to every member, and roughly forty
// call sites across the two tree modules were written against that. Privacy here
// is a SECOND ROOT rather than a per-item checkbox: a tree is addressed by the
// pair (visibility, owner_id), of which there are 1 + N for N members, and every
// read and write names one.
//
// ⚠ Two things about this type are load-bearing:
//
//  1. The ZERO VALUE IS THE SHARED ROOT. That is what lets a pre-v9 call site keep
//     working unchanged, and it is why `scope` defaults to `shared` on the wire.
//  2. The store NEVER defaults it. A store method that guesses a scope when a
//     handler forgets to set one returns another member's rows — it fails OPEN,
//     which is the one failure mode this whole version exists to prevent. So every
//     store method that touches a note or a folder takes a Scope explicitly, even
//     where the caller obviously means `shared`.
//
// This is deliberately duplicated from the `notes` module rather than shared with
// it: the v4 D40 precedent is that Dokumenty MIRRORS Poznámky's folder model —
// one behaviour, two implementations — and `notes/mirror.go` already records why.
// The two copies must be kept in step by hand, and the tests on both sides are
// what catch it when they are not.
type Scope struct {
	// Private selects a member's own private root; false is the household tree.
	Private bool
	// OwnerID is the auth user id, set if and only if Private.
	OwnerID string
}

// Visibility is the `visibility` column value this scope selects.
func (s Scope) Visibility() string {
	if s.Private {
		return visibilityPrivate
	}
	return visibilityShared
}

// ownerArg is the `owner_id` column value: NULL for the shared root, the member's
// id for a private one. Returned as `any` so it can be passed straight as a query
// argument, where nil binds SQL NULL.
func (s Scope) ownerArg() any {
	if s.Private {
		return s.OwnerID
	}
	return nil
}

// ownerColumn is what an INSERT writes into owner_id — the same value as ownerArg,
// named separately because reading and writing it are different acts and a future
// change to one should not silently change the other.
func (s Scope) ownerColumn() any { return s.ownerArg() }

// rootSentinel is the value the sibling-slug indexes key a root-level row on. It
// MUST match the expression in 06004 exactly:
//
//	COALESCE(folder_id, 'root:' || visibility || ':' || COALESCE(owner_id, ''))
//
// Two members each keeping a private note called "Recepty" at their own root both
// used to key on the same bare empty-string sentinel; this is what separates them
// (D178).
func (s Scope) rootSentinel() string {
	if s.Private {
		return "root:" + visibilityPrivate + ":" + s.OwnerID
	}
	return "root:" + visibilityShared + ":"
}

// parentKey maps a parent id to the value the slug index and the sibling queries
// key on: the parent's id when there is one, this scope's root sentinel otherwise.
func (s Scope) parentKey(parentID *string) string {
	if parentID != nil && *parentID != "" {
		return *parentID
	}
	return s.rootSentinel()
}

const (
	visibilityShared  = "shared"
	visibilityPrivate = "private"
)

// There are TWO predicates in v9, not one, and using the wrong one is the shape
// most of this version's bugs would take. They answer different questions:
//
//	scopeCond  — "which ROOT am I reading?"  Tree, list, search, resolve, and every
//	             sibling/root-relative query. The caller names one root via ?scope=,
//	             so the predicate pins visibility AND owner exactly.
//	viewerCond — "may I see this ROW?"       Reads by id, which carry no ?scope=
//	             because an id is global. Selects the whole shared tree plus the
//	             caller's own private one.
//
// A by-id route must NOT take a scope parameter: it would let a caller probe for
// an id in a scope and read the answer off the difference between two responses.
// A tree route must NOT use viewerCond: it would merge both roots into one
// response, which is precisely what D177 rejected.
//
// Mutations by id take NEITHER. The service loads the row first (it needs it for
// the audit diff), so ownership is settled before the write — and it has to be
// that way, because an `admin` hard-deleting a foreign private item is doing a
// write to a row they may not read (D181). A viewer predicate on the write path
// would make that legitimate case impossible.

// scopeCond returns the WHERE terms and arguments selecting one root scope. prefix
// is the table alias including its dot ("n." / "d." / "" for an unaliased query).
//
// `owner_id IS ?` rather than `= ?`: SQLite's IS is the NULL-safe comparison, so
// one form covers both the shared root (NULL) and a private one, and there is no
// branch here for a future edit to get wrong on one side only.
func scopeCond(prefix string, sc Scope) (string, []any) {
	return prefix + "visibility = ? AND " + prefix + "owner_id IS ?",
		[]any{sc.Visibility(), sc.ownerArg()}
}

// viewerCond selects the rows viewerID may see: the whole shared tree, plus that
// member's own private tree. An empty viewerID (a system-initiated call with no
// actor) sees the shared tree only — it fails CLOSED, which is the direction every
// ambiguity in this file resolves.
func viewerCond(prefix, viewerID string) (string, []any) {
	if viewerID == "" {
		return prefix + "visibility = '" + visibilityShared + "'", nil
	}
	return "(" + prefix + "visibility = '" + visibilityShared + "' OR " + prefix + "owner_id = ?)",
		[]any{viewerID}
}

// siblingKeyExpr is the sibling-slug index expression from 06004/07004, as SQL.
//
// ⚠ It MUST stay character-identical in meaning to the index, for two reasons: it
// is what makes root-level siblings compare per root scope rather than collapsing
// into one bucket, and it is what lets SQLite answer these queries FROM the index
// instead of scanning. parentCol is "folder_id" or "parent_id".
func siblingKeyExpr(prefix, parentCol string) string {
	return "COALESCE(" + prefix + parentCol + ", 'root:' || " + prefix + "visibility || ':' || COALESCE(" + prefix + "owner_id, ''))"
}

// ---- request plumbing ----

// scopeParam is the `?scope=` query value.
const (
	scopeParamShared  = "shared"
	scopeParamPrivate = "private"
)

// ParseScope turns the wire value into a Scope, resolving `private` against the
// CALLER's identity.
//
// ⚠ There is no value that names another member's private root, and there is no
// parameter that would express one: a member's private tree is reachable only from
// their own session. `owner_id` is never read from a request body or query — the
// same discipline v5's audience resolution follows for roles.
func ParseScope(ctx context.Context, raw string) (Scope, error) {
	switch raw {
	case "", scopeParamShared:
		return Scope{}, nil
	case scopeParamPrivate:
		uid := reqctx.ActorID(ctx)
		if uid == "" {
			// No authenticated actor: there is no "my private root" to resolve.
			// 401 territory, but the session middleware has already run, so this is
			// only reachable for a system-initiated call, which has no private tree.
			return Scope{}, httpx.ErrUnprocessable("scope=private requires a signed-in member")
		}
		return Scope{Private: true, OwnerID: uid}, nil
	default:
		return Scope{}, httpx.ErrUnprocessable("scope must be shared or private")
	}
}

// ParseCreateScope parses the `scope` field of a CREATE body (or the upload's
// `scope` part), and — unlike ParseScope — it distinguishes ABSENT from an
// explicit "shared".
//
// ⚠ That distinction is the whole point. At the root the two mean the same thing,
// but UNDER A PARENT FOLDER they do not: an absent scope defers to the parent,
// while an explicit one that disagrees with it is a 422 (§V9-3). ParseScope maps
// both "" and "shared" onto the zero Scope, so a caller that used it here could
// not tell the two apart — and `scope:"shared"` aimed at a private folder was
// silently corrected to private instead of refused.
//
// nil means "the caller did not say"; a non-nil pointer is a scope they named.
func ParseCreateScope(ctx context.Context, raw string) (*Scope, error) {
	if raw == "" {
		return nil, nil
	}
	sc, err := ParseScope(ctx, raw)
	if err != nil {
		return nil, err
	}
	return &sc, nil
}

// callerScopeFor returns the scope an item with these stored columns lives in,
// which is how a read-by-id decides whether the caller may see it at all.
func callerScopeFor(visibility string, ownerID *string) Scope {
	if visibility == visibilityPrivate {
		return Scope{Private: true, OwnerID: deref(ownerID)}
	}
	return Scope{}
}

// ⚠ THERE IS NO `visibleTo(ctx, visibility, ownerID)` HELPER, and its absence is
// deliberate — see the notes twin. Every by-id read enforces the audience in SQL
// through viewerCond (GetDocument, GetFolder, GetStoredDocument), so a foreign
// private row never reaches Go. A Go-side predicate beside them would be a SECOND
// way to spell the same rule, and the failure mode is a new route that loads
// unscoped, forgets to call it, and fails OPEN.
//
// The refusal is still ALWAYS 404 and never 403 (D180): it falls out of the store
// returning (nil, nil), byte-identical to an id that was never issued.

// isAdminCtx reports whether the actor carries the admin role. Used ONLY for the
// hard-delete asymmetry (D181) — never to widen a read.
func isAdminCtx(ctx context.Context) bool {
	a, ok := reqctx.ActorFrom(ctx)
	if !ok {
		return false
	}
	return reqctx.HasRole(a.Roles, "admin")
}

// errCrossScopeMove is the D186 refusal, and it names the remedy that ACTUALLY
// EXISTS for the direction attempted.
//
// ⚠ "Publish it instead" is only true one way round. Publishing runs private →
// shared and there is deliberately no unpublish (D182), so a caller dragging a
// SHARED document into their own private folder was being sent after a route that
// was never built. An error that prescribes an impossible remedy is worse than one
// that only refuses: it costs the reader the time it takes to discover the advice
// was wrong. Mirrors the notes twin.
func errCrossScopeMove(from Scope) error {
	if from.Private {
		return httpx.ErrUnprocessable(
			"a move cannot cross between the shared and private trees — publish it instead")
	}
	return httpx.ErrUnprocessable(
		"a move cannot cross between the shared and private trees — a shared item cannot be made private")
}

// assertPairing enforces the visibility/owner_id invariant in the SERVICE, because
// SQLite cannot add a table CHECK without rebuilding the table — and `documents`
// carries an explicit `seq INTEGER PRIMARY KEY` precisely because documents_fts is
// external-content and rowid-keyed, so a rebuild desynchronises search silently
// (D179). The v8 meter-monotonicity precedent (D148) exactly.
func assertPairing(sc Scope) error {
	if sc.Private && sc.OwnerID == "" {
		return httpx.ErrUnprocessable("a private item must have an owner")
	}
	if !sc.Private && sc.OwnerID != "" {
		return httpx.ErrUnprocessable("a shared item must not have an owner")
	}
	return nil
}
