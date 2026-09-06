package garden

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
)

// LIST KEYS (PRD §V7-4a).
//
// The four countable keys MIRROR their metrics exactly — same key, same
// selection, count = len(items) — and they do so BY CONSTRUCTION: both go through
// the same selection function below, so a count and its list cannot part ways
// (D77). That rule is the reason the shared functions live here rather than in
// each provider.
//
// Two keys are LIST-ONLY, on the D100 precedent: garden.harvest_ready and
// garden.frost_sensitive_now. "How many things are ripe" is a number nobody
// asked for; WHICH things are ripe is the whole message.
const (
	ListTasksDue7d          = MetricTasksDue7d
	ListTasksOverdue        = MetricTasksOverdue
	ListPlanWarnings        = MetricPlanWarnings
	ListBedsUnplanned       = MetricBedsUnplanned
	ListHarvestReady        = "garden.harvest_ready"
	ListFrostSensitiveNow   = "garden.frost_sensitive_now"
)

type listProvider struct{ svc *Service }

// ListProvider exposes this module's lists to the platform registry.
func (m *Module) ListProvider() lists.Provider { return m.lists }

func (p *listProvider) Descriptors() []lists.Descriptor {
	return []lists.Descriptor{
		{Key: ListTasksDue7d, Label: "Práce na zahradě (7 dní)", Empty: "tento týden na zahradě nic nečeká", Scope: lists.ScopeHousehold},
		{Key: ListTasksOverdue, Label: "Zmeškané práce", Empty: "nic zmeškaného", Scope: lists.ScopeHousehold},
		{Key: ListPlanWarnings, Label: "Varování v plánu", Empty: "plán je bez varování", Scope: lists.ScopeHousehold},
		{Key: ListBedsUnplanned, Label: "Nezaplánované záhony", Empty: "všechny záhony jsou zaplánované", Scope: lists.ScopeHousehold},
		{Key: ListHarvestReady, Label: "Ke sklizni", Empty: "nic není ke sklizni", Scope: lists.ScopeHousehold},
		{Key: ListFrostSensitiveNow, Label: "Citlivé plodiny venku", Empty: "nic citlivého venku", Scope: lists.ScopeHousehold},
	}
}

func (p *listProvider) Items(ctx context.Context, _ string, key string, asOf time.Time) ([]string, error) {
	s := p.svc
	local := asOf.In(s.opts.Location)

	switch key {
	case ListTasksDue7d:
		return s.tasksDueWithin(ctx, local, 7)
	case ListTasksOverdue:
		return s.tasksOverdue(ctx, local)
	case ListPlanWarnings:
		return s.planWarnings(ctx, local)
	case ListBedsUnplanned:
		return s.bedsUnplanned(ctx, local)
	case ListHarvestReady:
		return s.harvestReady(ctx, local)
	case ListFrostSensitiveNow:
		// THE NIGHT, not the day. asOf still decides the moment — the same rule
		// every other key here follows — but the moment this key is about is the
		// coming night, and a night spans midnight: nightRow picks the daily row
		// whose minimum still lies ahead, exactly as garden.frost_risk_tonight
		// and the frost_warning event do.
		//
		// Using asOf's own date instead would make a summary conditioned on
		// garden.frost_risk_tonight list what is outside TODAY while the metric
		// gating it read TOMORROW's low — so a crop going in the ground tomorrow
		// morning would be missed and one pulled tomorrow would be named.
		return s.frostSensitiveNow(ctx, nightRow(local))
	default:
		return nil, fmt.Errorf("garden: unknown list %q", key)
	}
}

// ================= the shared selections (metric == list) =================

// tasksDueWithin returns the open work whose window OVERLAPS the next n days —
// overlaps, not starts inside, or every job already under way disappears from
// the count on the day it becomes urgent.
func (s *Service) tasksDueWithin(ctx context.Context, local time.Time, days int) ([]string, error) {
	today := dates.New(local.Year(), local.Month(), local.Day())
	// Exhaustive, like every selection here: a page boundary would cap the
	// metric at 200 and silently drop the rest of the list.
	tasks, err := s.store.allTasks(ctx, nil, TaskFilter{
		From: today.String(), To: today.AddDays(days).String(), Status: TaskOpen,
	})
	if err != nil {
		return nil, err
	}
	return taskLines(tasks), nil
}

// tasksOverdue is the one that will nag in August. ToEnd, not To: overdue means
// the WHOLE window has passed, and the overlap bound would count every job
// still mid-window as missed.
func (s *Service) tasksOverdue(ctx context.Context, local time.Time) ([]string, error) {
	today := dates.New(local.Year(), local.Month(), local.Day())
	// Exhaustive: a neglected season is exactly when this passes 200, and a
	// capped count is a condition ("tasks_overdue gte …") that can never fire.
	tasks, err := s.store.allTasks(ctx, nil, TaskFilter{
		ToEnd: today.AddDays(-1).String(), Status: TaskOpen,
	})
	if err != nil {
		return nil, err
	}
	return taskLines(tasks), nil
}

// taskLines formats work for a notification: the job, and the bed it is in.
func taskLines(tasks []Task) []string {
	out := make([]string, 0, len(tasks))
	for _, t := range tasks {
		line := t.TitleCS
		if t.BedCode != nil && *t.BedCode != "" && !containsSubstring(line, *t.BedCode) {
			line += " (" + *t.BedCode + ")"
		}
		out = append(out, line)
	}
	return out
}

// planWarnings counts and names the non-dismissed error+warn findings in the
// CURRENT season. This is the key that gates the February planning nudge: once
// the plan is clean it resolves to zero and the summary goes quiet by itself.
func (s *Service) planWarnings(ctx context.Context, local time.Time) ([]string, error) {
	res, err := s.CheckSeason(ctx, local.Year())
	if err != nil {
		// No season for this year yet is not an error — there is simply nothing
		// to warn about, and a summary must not fail because February has not
		// happened. ONLY that case: a real read error must propagate, or the
		// metric reports a confident 0 and the nudge goes silent on a failure.
		var apiErr *httpx.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, w := range res.Warnings {
		if w.Dismissed {
			continue
		}
		if w.Severity != SeverityError && w.Severity != SeverityWarn {
			continue
		}
		out = append(out, w.TitleCS)
	}
	return out, nil
}

// bedsUnplanned names the active beds with nothing in them this season.
func (s *Service) bedsUnplanned(ctx context.Context, local time.Time) ([]string, error) {
	year := local.Year()
	beds, err := s.store.ListBeds(ctx, nil, false)
	if err != nil {
		return nil, err
	}
	// Exhaustive: a page boundary here would report a planted bed as unplanned
	// simply because its planting sorted past the 200th id.
	plantings, err := s.store.allPlantings(ctx, nil, PlantingFilter{Year: &year})
	if err != nil {
		return nil, err
	}
	// PERMANENTS COUNT AS PLANNED. A permanent has no season (D106), so the year
	// filter above cannot see it — and a bed holding only a fruit tree would then
	// be named "nezaplánováno" in every season for the rest of its life, while
	// check C7 (which reads the snapshot's seasonal+permanent set) correctly says
	// the bed is occupied. The two must agree, and this is the key that gates the
	// March nudge: it has to be able to reach zero.
	permanent, err := s.store.allPlantings(ctx, nil, PlantingFilter{Permanent: bp(true)})
	if err != nil {
		return nil, err
	}
	plantings = append(plantings, permanent...)
	used := map[string]bool{}
	for _, p := range plantings {
		if p.BedID != nil {
			used[*p.BedID] = true
		}
	}
	var out []string
	for _, b := range beds {
		if !used[b.ID] {
			out = append(out, b.Code+" — "+b.Name)
		}
	}
	return out, nil
}

// harvestReady is LIST-ONLY (D100 precedent): the plantings currently inside
// their harvest window, formatted with the bed. "How many things are ripe" is a
// number nobody asked for.
func (s *Service) harvestReady(ctx context.Context, local time.Time) ([]string, error) {
	today := dates.New(local.Year(), local.Month(), local.Day())
	year := local.Year()
	// Exhaustive, like the frost list: a truncated "ke sklizni" is a crop nobody
	// is told to pick, and nothing on the page says the list was cut short.
	plantings, err := s.store.allPlantings(ctx, nil, PlantingFilter{Year: &year})
	if err != nil {
		return nil, err
	}
	permanent, err := s.store.allPlantings(ctx, nil, PlantingFilter{Permanent: bp(true)})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range append(plantings, permanent...) {
		from, to := parseDatePtr(p.HarvestFrom), parseDatePtr(p.HarvestTo)
		if from == nil || to == nil {
			continue
		}
		if today.Before(*from) || today.After(*to) {
			continue
		}
		if p.Status == PlantingDone || p.Status == PlantingFailed {
			continue
		}
		// A crop already pulled from its bed is not "ke sklizni", however much of
		// its window remains — statuses only change at season close, so the actual
		// clearing/removal date is the only signal, read with the same field-aware
		// end OccupiesOn uses: the day the crop left no longer counts.
		if actual := firstDate(p.ClearedOn, p.RemovedOn); actual != nil && !today.Before(*actual) {
			continue
		}
		line := p.PlantName
		if p.BedCode != nil && *p.BedCode != "" {
			line += " (" + *p.BedCode + ")"
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return dedupe(out), nil
}

func containsSubstring(haystack, needle string) bool {
	return needle != "" && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
