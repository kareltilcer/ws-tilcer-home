// Package metrics is the third registered catalog, beside the widget catalog and
// the audit-action catalog (PRD §V5-1 D59/D60, HANDOFF-7 §7).
//
// Scheduled summaries need cross-module counts — "cards in Právě dělám",
// "reminders due today" — but the modular monolith forbids the admin module from
// importing todo or events (D28). So modules publish METRIC DESCRIPTORS and a
// resolver, exactly as they publish widget providers, and the scheduler reads
// values only through this registry. The admin composer offers the published
// keys as tokens; a new metric is a module change, never an admin one.
package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/catalog"
)

// Scopes describe whether a metric's value personalizes. Aliased from the
// shared catalog core so the metric and list catalogs cannot drift apart — the
// admin scheduler filters both through one scope predicate.
const (
	// ScopeHousehold: the same number for everyone (a shared board count).
	ScopeHousehold = catalog.ScopeHousehold
	// ScopePersonal: computed per recipient (my pinned notes ≠ yours). This is
	// what makes summaries resolve once PER RECIPIENT rather than once per send.
	ScopePersonal = catalog.ScopePersonal
)

// Descriptor is one publishable metric, as the admin composer sees it.
type Descriptor struct {
	Key   string `json:"key"`            // "todo.pravedelam_count"
	Label string `json:"label"`          // Czech label for the composer
	Unit  string `json:"unit,omitempty"` // "úkolů", "připomínek" — for the token chip
	Scope string `json:"scope"`          // ScopeHousehold | ScopePersonal
}

// Provider is what a module implements to publish metrics.
type Provider interface {
	// Descriptors lists this module's metrics.
	Descriptors() []Descriptor
	// Value resolves one of them for one recipient at one moment. asOf is
	// already in HOME_TIMEZONE, so "today" means the recipient's local day.
	Value(ctx context.Context, userID, key string, asOf time.Time) (int, error)
}

// Source is the optional interface a feature module implements to contribute its
// provider. It is deliberately NOT part of registry.Module: adding metrics must
// not change the contract every module implements (D56's "no interface change"
// applied to the metric catalog too). Collect type-asserts for it, the same way
// the host discovers anything else optional.
type Source interface {
	MetricProvider() Provider
}

// Registry is the assembled catalog. Built once at composition; read-only
// after. A thin typed facade over the shared catalog core (platform/catalog),
// which owns registration, lookup, and scope handling for every catalog.
type Registry struct {
	core *catalog.Registry[Descriptor, Provider]
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{core: catalog.New[Descriptor, Provider]("metrics", "metric",
		func(d Descriptor) string { return d.Key },
		func(d Descriptor) string { return d.Scope },
	)}
}

// Register adds a provider's metrics. A duplicate key is a programming error
// (two modules claiming one token) and fails the build of the registry rather
// than silently shadowing.
func (r *Registry) Register(p Provider) error {
	if p == nil {
		return nil
	}
	return r.core.Register(p, p.Descriptors())
}

// Collect builds a registry from anything that implements Source — in practice
// the module set, passed straight from the composition root.
func Collect(modules ...any) (*Registry, error) {
	r := NewRegistry()
	for _, m := range modules {
		src, ok := m.(Source)
		if !ok {
			continue // a module with no metrics is normal, not an error
		}
		if err := r.Register(src.MetricProvider()); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Catalog returns every published descriptor, ordered by key so the composer's
// dropdown is stable across restarts.
func (r *Registry) Catalog() []Descriptor {
	if r == nil {
		return nil
	}
	return r.core.Catalog()
}

// Descriptor looks up one metric.
func (r *Registry) Descriptor(key string) (Descriptor, bool) {
	if r == nil {
		return Descriptor{}, false
	}
	return r.core.Descriptor(key)
}

// Has reports whether key is publishable — used to reject an unknown
// {{metric.…}} token at SAVE time rather than at fire time (§8).
func (r *Registry) Has(key string) bool {
	if r == nil {
		return false
	}
	return r.core.Has(key)
}

// HasPersonal reports whether any of the given keys personalizes. A summary
// whose tokens are all household-scoped can be rendered ONCE for the whole
// audience instead of once per recipient.
func (r *Registry) HasPersonal(keys []string) bool {
	if r == nil {
		return false
	}
	return r.core.HasPersonal(keys)
}

// Resolve computes one metric for one recipient.
func (r *Registry) Resolve(ctx context.Context, userID, key string, asOf time.Time) (int, error) {
	if r == nil {
		return 0, fmt.Errorf("metrics: no metric catalog is registered")
	}
	p, ok := r.core.Provider(key)
	if !ok {
		return 0, fmt.Errorf("metrics: unknown metric %q", key)
	}
	return p.Value(ctx, userID, key, asOf)
}
