package logging

import (
	"context"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The logging module's MCP provider (v11, PRD §V11-4 FR-M2).
//
// ⚠ A PROVIDER WITH NO TOOLS IS NOT AN EMPTY PROVIDER, and this is the one the
// rule was written for. `logging` implements mcp.Source for `Search` ALONE — its
// tool list is empty because `home_activity` already answers every question the
// Log page answers, and a second spelling of it would be two redaction paths
// over the same table. What it contributes is a fifth of the household's search
// corpus: `audit_events_fts` is where "when did we last do X" actually lives.
//
// If an empty Tools() ever made something skip a provider entirely, that corpus
// would disappear silently — which is why FR-M2 says so out loud and why
// mcp.NoTools exists as a named thing to embed rather than three empty methods.

type mcpProvider struct {
	mcp.NoTools
	mcp.NoResources
	store *Store
}

// MCPProvider implements mcp.Source.
func (m *Module) MCPProvider() mcp.Provider { return &mcpProvider{store: m.store} }

func (p *mcpProvider) Module() string { return "logging" }

// Search reads the audit spine's own index.
//
// ⚠ IT IS THE *MATCHING* RULE, NOT THE BROWSING ONE (D188/D209). A free-text
// search over audit_events EXCLUDES another member's private-item events rather
// than redacting them — a redacted hit still tells the searcher that their term
// occurs in a private title, which is precisely the protected thing. That
// exclusion is `Store.Browse`'s, applied here by passing the term as `Q`: this
// provider adds no predicate of its own, because a second spelling of a privacy
// rule is one implementation and one bug.
//
// ⚠ D302: an actor-less ctx is an ERROR, never an empty slice. Here it is also
// the difference between "the household searched" and "nobody searched", which
// is what the exclusion is keyed on.
func (p *mcpProvider) Search(ctx context.Context, q mcp.Query) ([]mcp.Hit, error) {
	actor, ok := reqctx.ActorFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("logging: search without an actor")
	}
	// ⚠ ADMIN ONLY, BECAUSE `/api/logs/**` IS. The Log browser has sat behind
	// httpx.RequireAdmin since D5, so a member whose browser answers 403 must not
	// get the same summaries back from a search — and this is the SECOND time that
	// exact hole appeared in v11: home_activity had it too, found in review, for
	// the same reason. A read-only surface is not an ungated one.
	//
	// ⚠ IT RETURNS NOTHING RATHER THAN AN ERROR. A non-admin searching the
	// household is doing something perfectly legitimate; the Log is simply not part
	// of their corpus, and the host reports `logging: 0` beside the modules that did
	// answer. Erroring would turn one member's ordinary search into a failure.
	if !reqctx.IsAdmin(ctx) {
		return nil, nil
	}
	page, err := p.store.Browse(ctx, Filter{Q: q.Text, Limit: q.Limit}, actor.UserID)
	if err != nil {
		return nil, err
	}
	hits := make([]mcp.Hit, 0, len(page.Items))
	for _, e := range page.Items {
		// ⚠ A REDACTED ROW IS NEVER A HIT. The matching rule should already have
		// excluded it — this is the belt to that brace, and it costs one comparison
		// on a path where the alternative is publishing the fixed phrase as a search
		// result for somebody else's private note.
		if e.Redacted {
			continue
		}
		ts, _ := time.Parse(audit.TSLayout, e.TS)
		who := ""
		if e.ActorLabel != nil {
			who = *e.ActorLabel
		} else if e.ActorUserID != nil {
			who = *e.ActorUserID
		}
		hits = append(hits, mcp.Hit{
			Kind:      "logging.event",
			ID:        e.ID,
			Title:     e.Summary,
			Snippet:   e.Module + "." + e.Action + " — " + who,
			UpdatedAt: ts,
		})
	}
	return hits, nil
}

// Get answers home_get for one audit event.
//
// ⚠ A MODULE THAT PUBLISHES NO TOOL STILL OWES home_get AN ANSWER, and `logging`
// was the one provider whose search hits carried an id nothing could resolve: a
// model handed `logging.event` rows and told home_get turns a hit into an entity
// was refused for the only module it had just been reading. mcp.EntityGetter is
// not a tool — it adds nothing to tools/list and nothing to the count FR-M4 fixes
// — so answering here does not reopen FR-M2's decision.
//
// ⚠ ADMIN ONLY, FOR THE THIRD TIME IN THIS FILE'S HISTORY. `/api/logs/**` has sat
// behind httpx.RequireAdmin since D5, and home_get carries no per-kind role gate
// of its own — so an ungated event read here would be the audit spine reached by
// a member whose browser answers 403, which is leak row 6 wearing yet another hat.
// A non-admin gets the ordinary not-found refusal: they cannot obtain a
// logging.event id through this surface in the first place (Search returns nothing
// for them), so there is nothing for the refusal to confirm.
//
// ⚠ AND THE REDACTION IS THE STORE'S. `Store.Get` applies redactEvent and drops a
// redacted row's field diffs — the same rule §13.2 asks home_activity to honour —
// so another member's private item stays a fixed phrase with a blanked id even
// for an admin.
func (p *mcpProvider) Get(ctx context.Context, kind, id string) (mcp.Result, error) {
	if kind != "logging.event" {
		return mcp.NotFoundResult(), nil
	}
	actor, ok := reqctx.ActorFrom(ctx)
	if !ok {
		return mcp.Result{}, fmt.Errorf("logging: entity read without an actor")
	}
	if !reqctx.IsAdmin(ctx) {
		return mcp.NotFoundResult(), nil
	}
	ev, err := p.store.Get(ctx, id, actor.UserID)
	if err != nil {
		return mcp.Result{}, err
	}
	if ev == nil {
		return mcp.NotFoundResult(), nil
	}
	who := ""
	if ev.ActorLabel != nil {
		who = *ev.ActorLabel
	} else if ev.ActorUserID != nil {
		who = *ev.ActorUserID
	}
	text := fmt.Sprintf("%s — %s.%s, %s (id %s)", ev.Summary, ev.Module, ev.Action, ev.TS, ev.ID)
	if who != "" {
		text += "\nKdo: " + who
	}
	if ev.Via != nil {
		text += "\nPřes: " + *ev.Via
	}
	return mcp.TextResult(text, ev)
}
