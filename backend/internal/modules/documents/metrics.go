package documents

import (
	"context"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
)

// MetricPinnedCount is the number of documents pinned visible to one member:
// household pins ∪ their personal pins, de-duplicated — the documents.pripnute
// widget's population (D47/D69). PERSONAL-scoped, like its notes twin.
const MetricPinnedCount = "documents.pinned_count"

type metricProvider struct{ store *Store }

// MetricProvider exposes this module's metrics to the platform registry.
func (m *Module) MetricProvider() metrics.Provider { return m.metrics }

func (p *metricProvider) Descriptors() []metrics.Descriptor {
	return []metrics.Descriptor{
		{Key: MetricPinnedCount, Label: "Připnuté dokumenty", Unit: "dokumentů", Scope: metrics.ScopePersonal},
	}
}

func (p *metricProvider) Value(ctx context.Context, userID, key string, _ time.Time) (int, error) {
	if key != MetricPinnedCount {
		return 0, fmt.Errorf("documents: unknown metric %q", key)
	}
	return p.store.CountPinnedFor(ctx, userID)
}
