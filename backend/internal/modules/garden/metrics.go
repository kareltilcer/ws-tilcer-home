package garden

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
)

// Metric keys published by this module (PRD §V7-4a).
//
// All six are HOUSEHOLD-scoped: a garden has one plan and one forecast, so none
// of them takes the userID.
//
// THE METRICS EXIST TO BE CONDITIONS, NOT DECORATION. garden.plan_warnings gates
// a February planning nudge that goes quiet once the plan is clean;
// garden.frost_risk_tonight gates the frost alert entirely (D113); and
// garden.beds_unplanned is the March version of finance.missing_months.
const (
	MetricTasksDue7d      = "garden.tasks_due_7d"
	MetricTasksOverdue    = "garden.tasks_overdue"
	MetricPlanWarnings    = "garden.plan_warnings"
	MetricHarvestSeason   = "garden.harvest_season"
	MetricBedsUnplanned   = "garden.beds_unplanned"
	MetricFrostRiskTonight = "garden.frost_risk_tonight"
)

type metricProvider struct{ svc *Service }

// MetricProvider exposes this module's metrics to the platform registry.
func (m *Module) MetricProvider() metrics.Provider { return m.metrics }

func (p *metricProvider) Descriptors() []metrics.Descriptor {
	return []metrics.Descriptor{
		{Key: MetricTasksDue7d, Label: "Práce na zahradě (7 dní)", Unit: "úkolů", Scope: metrics.ScopeHousehold},
		{Key: MetricTasksOverdue, Label: "Zmeškané práce", Unit: "úkolů", Scope: metrics.ScopeHousehold},
		{Key: MetricPlanWarnings, Label: "Varování v plánu", Unit: "varování", Scope: metrics.ScopeHousehold},
		{Key: MetricHarvestSeason, Label: "Letošní sklizeň", Unit: "kg", Scope: metrics.ScopeHousehold},
		{Key: MetricBedsUnplanned, Label: "Nezaplánované záhony", Unit: "záhonů", Scope: metrics.ScopeHousehold},
		{Key: MetricFrostRiskTonight, Label: "Noční minimum", Unit: "°C", Scope: metrics.ScopeHousehold},
	}
}

// Value resolves one metric at one moment.
//
// asOf is USED rather than time.Now(): a summary firing at 00:01 must agree with
// the widget about which day "today" is, and about which season "letos" means.
func (p *metricProvider) Value(ctx context.Context, _ string, key string, asOf time.Time) (int, error) {
	s := p.svc
	local := asOf.In(s.opts.Location)

	switch key {
	case MetricTasksDue7d:
		// Counted as len() of exactly the selection the list of the same key
		// returns, so the number and the items can never disagree (D77).
		items, err := s.tasksDueWithin(ctx, local, 7)
		return len(items), err

	case MetricTasksOverdue:
		items, err := s.tasksOverdue(ctx, local)
		return len(items), err

	case MetricPlanWarnings:
		items, err := s.planWarnings(ctx, local)
		return len(items), err

	case MetricHarvestSeason:
		// kg ONLY — rows in ks/l/svazek are excluded rather than coerced, because
		// adding apples to bunches gives a number worse than no number. Rounded to
		// whole kilograms, which is the resolution a summary can use.
		total, err := s.store.SeasonHarvestKg(ctx, local.Year())
		return int(math.Round(total)), err

	case MetricBedsUnplanned:
		items, err := s.bedsUnplanned(ctx, local)
		return len(items), err

	case MetricFrostRiskTonight:
		// A metric resolves to an int, so the forecast is rounded to whole
		// degrees. That is enough for the one question it answers, and it keeps
		// the v5 condition `lte 2` meaning what a person would expect.
		//
		// NO FORECAST ⇒ metrics.ErrNoData, not a number. Zero degrees is a real
		// temperature and would make every frost condition fire on missing data
		// (D112) — and any sentinel value leaks into rendered summaries and
		// gte-shaped conditions as if it were a reading.
		temp, ok, err := s.FrostRiskTonight(ctx, local)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, metrics.ErrNoData
		}
		return int(math.Round(temp)), nil

	default:
		return 0, fmt.Errorf("garden: unknown metric %q", key)
	}
}
