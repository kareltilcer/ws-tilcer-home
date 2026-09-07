package logging_test

import (
	"errors"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/logging"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcpctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// The Log's `via` filter (v11, D292) — and the value it must refuse.
//
// ⚠ IT IS THE ONE FILTER ON THIS ROUTE THAT IS A SWITCH RATHER THAN AN EQUALITY
// BIND, and that is why it needs its own test. Every other dimension filter
// narrows to nothing on a value nobody stored — honest, if unhelpful. This one
// used to fall through both cases and add NO condition, handing the caller the
// whole audit spine as though they had not filtered, with nothing on the wire
// saying so. `openapi.yaml` declares `enum: [mcp, ui]`; a value outside it is a
// 422.
func TestViaFilter(t *testing.T) {
	db := testsupport.NewDB(t)
	store := logging.NewStore(db)
	ctx := testsupport.CtxUser("karel", "admin")
	viaToken := mcpctx.WithToken(ctx, "tok-1")

	record(t, db, ctx, audit.Event{Module: "todo", Action: "card.create", Summary: "z prohlížeče"})
	record(t, db, viaToken, audit.Event{Module: "todo", Action: "card.create", Summary: "přes asistenta"})

	t.Run("mcp returns only the token's changes", func(t *testing.T) {
		page, err := store.Browse(ctx, logging.Filter{Via: logging.ViaMCP}, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("via=mcp returned %d items, want 1", len(page.Items))
		}
		if page.Items[0].Via == nil || *page.Items[0].Via != audit.ViaMCP {
			t.Fatalf("via=mcp returned a row whose via is %v", page.Items[0].Via)
		}
	})

	t.Run("ui returns only the browser's", func(t *testing.T) {
		page, err := store.Browse(ctx, logging.Filter{Via: logging.ViaUI}, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("via=ui returned %d items, want 1", len(page.Items))
		}
		if page.Items[0].Via != nil {
			t.Fatalf("via=ui returned a row whose via is %q", *page.Items[0].Via)
		}
	})

	t.Run("empty returns both", func(t *testing.T) {
		page, err := store.Browse(ctx, logging.Filter{}, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("no via filter returned %d items, want 2", len(page.Items))
		}
	})

	// ⚠ AND ANYTHING ELSE IS REFUSED RATHER THAN IGNORED. The failure this guards
	// against is not an error — it is a SUCCESS: every row returned, the filter
	// silently not applied, and a caller reading rows they explicitly excluded.
	for _, bad := range []string{"MCP", "browser", "null", "mcp "} {
		t.Run("refuses "+bad, func(t *testing.T) {
			page, err := store.Browse(ctx, logging.Filter{Via: bad}, "")
			if err == nil {
				t.Fatalf("via=%q was accepted and returned %d items — an unknown value must"+
					" not read as 'no filter'", bad, len(page.Items))
			}
			var ie *logging.InvalidError
			if !errors.As(err, &ie) {
				t.Fatalf("via=%q returned %T, want *logging.InvalidError (which maps to 422)", bad, err)
			}
			if ie.Param != "via" {
				t.Fatalf("the refusal names %q, want \"via\"", ie.Param)
			}
		})
	}
}
