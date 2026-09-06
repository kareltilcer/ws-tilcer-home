package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The admin module's MCP provider (v11, PRD §V11-4 FR-M4).
//
// ⚠ ONE TOOL, READ-ONLY, ADMIN-GATED. Every admin MUTATION is absent (D295):
// broadcasts, trigger rules, scheduled summaries, thresholds, the private-item
// purge. A broadcast is the sharpest of them — it reaches every member's lock
// screen and nothing unsends it — but the reasoning covers the quiet ones too: a
// rule an assistant created is a rule nobody remembers agreeing to.
//
// ⚠ AND `admin` STILL IMPORTS NO FEATURE MODULE. The status this reports is
// assembled from the storage catalog and the delivery log it already owns, the
// same way the Úložiště page assembles it — through registries, never through
// another module's tables (D191/D28).

type mcpProvider struct {
	mcp.NoResources
	svc *Service
}

// MCPProvider implements mcp.Source.
func (m *Module) MCPProvider() mcp.Provider { return &mcpProvider{svc: m.svc} }

func (p *mcpProvider) Module() string { return "admin" }

const toolStatus = "home_admin_status"

func (p *mcpProvider) Tools() []mcp.Tool {
	return []mcp.Tool{{
		Name:        toolStatus,
		Title:       "Household service status",
		Description: "Admin only. Returns how much space the household is using — database and object storage, broken down by module — and how notification delivery has been going lately.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "refresh": {"type": "boolean", "description": "Recompute rather than serving the cached snapshot. Costs a full storage scan."}
  },
  "additionalProperties": false
}`),
		ReadOnly: true,
		// ⚠ ADMIN-ONLY, AND THE HOST ENFORCES IT BEFORE DISPATCH. It is the same
		// gate `/api/admin/**` carries, expressed where the MCP host can read it —
		// a read-only tool is not an ungated one, which is exactly the mistake the
		// audit digest made before it was caught.
		AdminOnly: true,
	}}
}

func (p *mcpProvider) Call(ctx context.Context, name string, args json.RawMessage) (mcp.Result, error) {
	if name != toolStatus {
		return mcp.Result{}, mcp.UnknownToolError(name)
	}
	var in struct {
		Refresh bool `json:"refresh"`
	}
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	// ⚠ THE ROLE IS RE-CHECKED HERE, and that is the second of the two gates the
	// PRD asks for rather than a duplicate of the first: `reqctx.IsAdmin` is the
	// service-layer half, and it is what protects this tool if it is ever reached
	// from anywhere but the host's dispatcher.
	if !reqctx.IsAdmin(ctx) {
		return mcp.Result{}, httpx.ErrForbidden("admin only")
	}
	if p.svc.Storage() == nil {
		return mcp.Result{}, httpx.ErrNotImplemented("storage reporting is not configured")
	}
	snap, err := p.svc.Storage().Snapshot(ctx, in.Refresh)
	if err != nil {
		return mcp.Result{}, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Databáze: %s (WAL %s)\n", mb(&snap.Database.TotalBytes), mb(&snap.Database.WALBytes))
	// ⚠ A NIL `bytes` IS "NOT MEASURED", NOT ZERO, and the difference is the whole
	// reason the field is a pointer: without `dbstat` the per-table figures do not
	// exist, and printing 0 B for every module would read as an empty database.
	if !snap.Database.BytesAvailable {
		b.WriteString("  (per-table figures unavailable — this SQLite build has no dbstat)\n")
	}
	for _, m := range snap.Database.Modules {
		fmt.Fprintf(&b, "  • %s: %s\n", m.Module, mb(m.Bytes))
	}
	switch {
	case !snap.Blobs.Available:
		// A bucket outage must never blank the answer — the database figures above
		// are intact and are most of what an admin asked for.
		b.WriteString("Objekty (R2): nedostupné")
		if snap.Blobs.Error != nil {
			fmt.Fprintf(&b, " — %s", *snap.Blobs.Error)
		}
		b.WriteString("\n")
	default:
		fmt.Fprintf(&b, "Objekty (R2): %s\n", mb(snap.Blobs.TotalBytes))
		for _, m := range snap.Blobs.Modules {
			fmt.Fprintf(&b, "  • %s: %s\n", m.Module, mb(m.Bytes))
		}
	}
	if snap.Warning.Exceeded {
		fmt.Fprintf(&b, "⚠ Přes práh %d MB (naměřeno %s)\n", snap.Warning.ThresholdMB, mb(snap.Warning.MeasuredBytes))
	}
	if snap.Chat != nil {
		b.WriteString("Chat má vlastní prahy — viz Administrace → Úložiště.\n")
	}
	return mcp.TextResult(strings.TrimRight(b.String(), "\n"), snap)
}

// mb renders a byte count, and renders a MISSING one as "neměřeno".
//
// ⚠ THE POINTER IS THE POINT. Every per-module figure in the snapshot is a
// *int64 because "we could not measure this" and "this is empty" are different
// answers, and the Úložiště page draws them differently for the same reason.
func mb(bytes *int64) string {
	if bytes == nil {
		return "neměřeno"
	}
	return fmt.Sprintf("%.1f MB", float64(*bytes)/(1<<20))
}

// Search contributes nothing.
//
// ⚠ D302 STILL APPLIES TO A PROVIDER WITH NOTHING TO FIND. The actor check is not
// decoration here: it is the assertion that this provider was reached through a
// resolved token, and a provider that skipped it would be the one place a later
// refactor could route an actor-less call without anything going red.
func (p *mcpProvider) Search(ctx context.Context, _ mcp.Query) ([]mcp.Hit, error) {
	if _, ok := reqctx.ActorFrom(ctx); !ok {
		return nil, fmt.Errorf("admin: search without an actor")
	}
	// Administrace has nothing a household member searches FOR — its rules and
	// summaries are configuration, not content.
	return nil, nil
}
