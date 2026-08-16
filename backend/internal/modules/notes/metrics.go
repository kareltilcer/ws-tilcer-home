package notes

import (
	"context"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
)

// MetricPinnedCount is the number of notes pinned VISIBLE TO ONE MEMBER:
// household pins ∪ that member's personal pins, de-duplicated — the same set the
// notes.pripnute widget shows them (D35/D69).
//
// It is PERSONAL-scoped, and it is one of only two such metrics at launch. That
// matters: it is what keeps summaries resolving once per recipient rather than
// once per send, so the mechanism stays exercised rather than theoretical (D60).
const MetricPinnedCount = "notes.pinned_count"

type metricProvider struct{ store *Store }

// MetricProvider exposes this module's metrics to the platform registry.
func (m *Module) MetricProvider() metrics.Provider { return m.metrics }

func (p *metricProvider) Descriptors() []metrics.Descriptor {
	return []metrics.Descriptor{
		{Key: MetricPinnedCount, Label: "Připnuté poznámky", Unit: "poznámek", Scope: metrics.ScopePersonal},
	}
}

func (p *metricProvider) Value(ctx context.Context, userID, key string, _ time.Time) (int, error) {
	if key != MetricPinnedCount {
		return 0, fmt.Errorf("notes: unknown metric %q", key)
	}
	return p.store.CountPinnedFor(ctx, userID)
}
