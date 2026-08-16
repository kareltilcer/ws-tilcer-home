package todo

import (
	"context"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
)

// List keys published by this module — the named counterparts of the metrics
// with the same keys (a summary can say how many, or which ones, or both).
// HOUSEHOLD-scoped: a board is shared, so every recipient reads the same cards.
const (
	ListPravedelamCount = MetricPravedelamCount
	ListDoneToday       = MetricDoneToday
	ListOpenTotal       = MetricOpenTotal
)

// listProvider names the cards the metrics count, reading the same bounded
// shape the Právě dělám widget does.
type listProvider struct{ store *Store }

// ListProvider exposes this module's lists to the platform registry.
func (m *Module) ListProvider() lists.Provider { return m.lists }

func (p *listProvider) Descriptors() []lists.Descriptor {
	return []lists.Descriptor{
		{Key: ListPravedelamCount, Label: "Úkoly v Právě dělám", Empty: "nic rozdělaného", Scope: lists.ScopeHousehold},
		{Key: ListDoneToday, Label: "Hotovo dnes", Empty: "zatím nic", Scope: lists.ScopeHousehold},
		{Key: ListOpenTotal, Label: "Nedokončené úkoly celkem", Empty: "nic nezbývá", Scope: lists.ScopeHousehold},
	}
}

func (p *listProvider) Items(ctx context.Context, _ string, key string, asOf time.Time) ([]string, error) {
	switch key {
	case ListPravedelamCount:
		return p.store.TitlesInKind(ctx, []string{"now"})
	case ListOpenTotal:
		return p.store.TitlesInKind(ctx, []string{"normal", "now"})
	case ListDoneToday:
		start, end := localDayBounds(asOf)
		return p.store.TitlesDoneBetween(ctx, start, end)
	default:
		return nil, fmt.Errorf("todo: unknown list %q", key)
	}
}
