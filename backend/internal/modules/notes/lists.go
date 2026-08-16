package notes

import (
	"context"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
)

// ListPinned names the notes MetricPinnedCount counts: household pins ∪ the
// recipient's personal pins, de-duplicated, household block first — the same set
// in the same order the notes.pripnute widget shows them (D35/D69).
//
// It is PERSONAL-scoped, which makes it one of the few tokens that forces a
// summary to render once per recipient (D60): Karel's morning list must not
// contain Eva's personal pins.
const ListPinned = MetricPinnedCount

type listProvider struct{ store *Store }

// ListProvider exposes this module's lists to the platform registry.
func (m *Module) ListProvider() lists.Provider { return m.lists }

func (p *listProvider) Descriptors() []lists.Descriptor {
	return []lists.Descriptor{
		{Key: ListPinned, Label: "Připnuté poznámky", Empty: "nic připnutého", Scope: lists.ScopePersonal},
	}
}

func (p *listProvider) Items(ctx context.Context, userID, key string, _ time.Time) ([]string, error) {
	if key != ListPinned {
		return nil, fmt.Errorf("notes: unknown list %q", key)
	}
	rows, err := p.store.PinnedRowsFor(ctx, userID)
	if err != nil {
		return nil, err
	}

	// A note pinned both ways appears twice in the rows and once on the list —
	// the same de-duplication CountPinnedFor does in SQL, so the count and the
	// list can never disagree about the same person's pins.
	seen := make(map[string]bool, len(rows))
	out := make([]string, 0, len(rows))
	for _, scope := range []string{scopeHousehold, scopePersonal} {
		for _, r := range rows {
			if r.Scope != scope || seen[r.NoteID] {
				continue
			}
			seen[r.NoteID] = true
			out = append(out, r.Title)
		}
	}
	return out, nil
}
