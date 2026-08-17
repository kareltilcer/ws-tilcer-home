package finance

import (
	"context"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
)

// ListMissingMonths names the months MetricMissingMonths counts — deliberately
// the SAME KEY, because it is the same selection (D90, D77's rule). The
// scheduler relies on that: a summary referencing both resolves the list once
// and derives the count as len(items).
//
// This is the point of the whole module's citizenship. Composed with v5's
// conditions it becomes the notification this household actually wants: a
// summary on day 1 of each month, conditioned on `finance.missing_months gt 0`,
// whose body lists exactly what is missing — and silent in every month where
// nothing is.
const ListMissingMonths = MetricMissingMonths

type listProvider struct {
	store *Store
	loc   *time.Location
}

// ListProvider exposes this module's lists to the platform registry.
func (m *Module) ListProvider() lists.Provider { return m.lists }

func (p *listProvider) Descriptors() []lists.Descriptor {
	return []lists.Descriptor{
		{
			Key:   ListMissingMonths,
			Label: "Nezadané měsíce",
			// What the notification says instead of an empty list. "nic nechybí"
			// reads as good news, which is what an empty gap set actually is.
			Empty: "nic nechybí",
			Scope: lists.ScopeHousehold,
		},
	}
}

// Items returns the missing months as Czech labels in ASCENDING order — oldest
// gap first, because that is the one most likely to be forgotten for good.
func (p *listProvider) Items(ctx context.Context, _ string, key string, asOf time.Time) ([]string, error) {
	if key != ListMissingMonths {
		return nil, fmt.Errorf("finance: unknown list %q", key)
	}
	// The same read and the same gap computation the metric uses, so the count
	// and this list are the same answer expressed two ways.
	mp := &metricProvider{store: p.store, loc: p.loc}
	missing, err := mp.missing(ctx, asOf)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(missing))
	for _, ym := range missing {
		out = append(out, monthLabel(ym))
	}
	return out, nil
}
