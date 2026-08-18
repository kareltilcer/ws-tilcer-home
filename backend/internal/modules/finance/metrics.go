package finance

import (
	"context"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
)

// Metric keys published by this module (PRD §V6-4a, D89).
//
// All four are HOUSEHOLD-scoped: a household budget has no per-recipient value,
// so none of them takes the userID. That is the whole justification — if the
// numbers ever became per-person, the scope would have to change with them,
// because the scheduler renders a household summary once for everyone.
const (
	MetricTotalIncomeCurrent = "finance.total_income_current"
	MetricSavingsCurrent     = "finance.savings_current"
	MetricMissingMonths      = "finance.missing_months"
	MetricMonthsRecorded     = "finance.months_recorded"
)

type metricProvider struct {
	store *Store
	loc   *time.Location
}

// MetricProvider exposes this module's metrics to the platform registry.
func (m *Module) MetricProvider() metrics.Provider { return m.metrics }

func (p *metricProvider) Descriptors() []metrics.Descriptor {
	return []metrics.Descriptor{
		{Key: MetricTotalIncomeCurrent, Label: "Celkový příjem tento měsíc", Unit: "Kč", Scope: metrics.ScopeHousehold},
		{Key: MetricSavingsCurrent, Label: "Spoření tento měsíc", Unit: "Kč", Scope: metrics.ScopeHousehold},
		{Key: MetricMissingMonths, Label: "Nezadané měsíce", Unit: "měsíců", Scope: metrics.ScopeHousehold},
		{Key: MetricMonthsRecorded, Label: "Zadaných měsíců", Unit: "měsíců", Scope: metrics.ScopeHousehold},
	}
}

// Value resolves one metric at one moment. asOf is already in HOME_TIMEZONE and
// is used rather than time.Now(): a summary firing at 00:01 must agree with the
// widget about which month "this month" is.
func (p *metricProvider) Value(ctx context.Context, _ string, key string, asOf time.Time) (int, error) {
	switch key {
	case MetricTotalIncomeCurrent:
		m, ok, err := p.store.ForMonth(ctx, currentMonth(asOf, p.loc))
		if err != nil || !ok {
			return 0, err // an unrecorded month is 0, not an error
		}
		return m.Split.TotalIncome, nil

	case MetricSavingsCurrent:
		m, ok, err := p.store.ForMonth(ctx, currentMonth(asOf, p.loc))
		if err != nil || !ok {
			return 0, err
		}
		return m.Split.Savings(), nil

	case MetricMissingMonths:
		// Counted as len() of exactly the selection the list of the same key
		// returns — same store read, same gap computation — so the number and
		// the items can never disagree (D77's rule).
		missing, err := p.missing(ctx, asOf)
		if err != nil {
			return 0, err
		}
		return len(missing), nil

	case MetricMonthsRecorded:
		return p.store.Count(ctx)

	default:
		return 0, fmt.Errorf("finance: unknown metric %q", key)
	}
}

// missing is the shared selection behind the finance.missing_months metric and
// the list of the same key. Both go through here; neither reimplements it.
func (p *metricProvider) missing(ctx context.Context, asOf time.Time) ([]string, error) {
	recorded, err := p.store.RecordedMonths(ctx)
	if err != nil {
		return nil, err
	}
	return missingMonths(recorded, currentMonth(asOf, p.loc)), nil
}
