package audit_test

import (
	"reflect"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
)

// HardMeta and WithVia build the `meta` an event carries, and meta is rendered
// verbatim in the Log browser. Both answer "absent" with nil rather than with an
// empty or false-valued map, which is the half a reader actually sees.

func TestHardMeta(t *testing.T) {
	if got := audit.HardMeta(true); !reflect.DeepEqual(got, map[string]any{"hard": true}) {
		t.Errorf("HardMeta(true) = %#v", got)
	}
	// ⚠ Nil, NOT {"hard": false}. A false flag on every ordinary delete is a line of
	// noise on the majority of rows, marking the case that is not happening.
	if got := audit.HardMeta(false); got != nil {
		t.Errorf("HardMeta(false) = %#v, want nil", got)
	}
}

func TestWithVia(t *testing.T) {
	t.Run("stamps via onto an existing meta", func(t *testing.T) {
		got := audit.WithVia(map[string]any{"scope": "household"}, "widget")
		want := map[string]any{"scope": "household", "via": "widget"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %#v, want %#v", got, want)
		}
	})

	t.Run("builds a meta when there was none", func(t *testing.T) {
		got := audit.WithVia(nil, "pin")
		if !reflect.DeepEqual(got, map[string]any{"via": "pin"}) {
			t.Errorf("got %#v", got)
		}
	})

	// The ?via= parameter is absent far more often than it is present, so both
	// halves matter: no marker for an empty via, and no empty map left behind.
	t.Run("an absent via leaves the meta alone", func(t *testing.T) {
		base := map[string]any{"scope": "household"}
		got := audit.WithVia(base, "")
		if !reflect.DeepEqual(got, map[string]any{"scope": "household"}) {
			t.Errorf("got %#v", got)
		}
		if _, ok := got["via"]; ok {
			t.Error("an empty via was stamped anyway")
		}
	})

	t.Run("nothing at all collapses to nil", func(t *testing.T) {
		if got := audit.WithVia(nil, ""); got != nil {
			t.Errorf("got %#v, want nil", got)
		}
		if got := audit.WithVia(map[string]any{}, ""); got != nil {
			t.Errorf("empty map with no via = %#v, want nil", got)
		}
	})

	// ⚠ It mutates the map it is handed rather than copying, which is what both
	// call sites expect — they pass a literal built on the spot. Pinned so a future
	// caller sharing a map across two events knows what it is buying.
	t.Run("writes through to the caller's map", func(t *testing.T) {
		base := map[string]any{"scope": "household"}
		audit.WithVia(base, "widget")
		if base["via"] != "widget" {
			t.Errorf("caller's map not updated: %#v", base)
		}
	})
}
