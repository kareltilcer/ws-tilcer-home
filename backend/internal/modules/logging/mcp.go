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
