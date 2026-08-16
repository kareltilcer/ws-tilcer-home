// Package lists is the fourth registered catalog, beside the widget catalog, the
// audit-action catalog and the metric catalog (PRD §V5-1 D59/D60, HANDOFF-7 §7).
//
// Metrics answer "how many". A summary that says "3 připomínky na dnešek" is
// usually one question short of useful — the household wants to know WHICH
// three. So a module may publish LIST descriptors beside its metric ones, in
// exactly the same shape: a descriptor plus a resolver, read only through this
// registry, so the admin module still imports no feature module (D28).
//
// The split from platform/metrics is deliberate rather than incidental: a metric
// resolves to an int and a list to a bounded slice of short Czech lines, and the
// scheduler treats them differently at render time. Merging them would mean one
// registry with two resolve methods and descriptors whose half the fields are
// meaningless — the same reason the widget catalog is not the metric catalog.
//
// A provider returns the ITEMS; it never formats them. Where the bullet goes,
// how many items fit, and what "…a další 4" reads like belongs to the sender
// (admin/template.go): a module knows what belongs on the list, only the sender
// knows what fits in a push.
package lists

import (
	"context"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/catalog"
)

// Scopes describe whether a list's contents personalize. Aliased from the
// shared catalog core — the SAME constants the metric catalog uses, not merely
// the same spelling, because the admin scheduler filters tokens from both
// catalogs through one scope predicate (D60).
const (
	// ScopeHousehold: the same items for everyone (today's shared reminders).
	ScopeHousehold = catalog.ScopeHousehold
	// ScopePersonal: computed per recipient (my pinned notes ≠ yours).
	ScopePersonal = catalog.ScopePersonal
)

// Descriptor is one publishable list, as the admin composer sees it.
//
// A list that shares its key with a METRIC names the same selection the metric
// counts — that is the point of sharing the key, and the scheduler relies on
// it: when a summary references both, it resolves the list once and derives
// the count as len(items) rather than paying for the same read twice. A module
// must not reuse a metric key for a list that selects differently.
type Descriptor struct {
	Key   string `json:"key"`   // "events.pripominky_today"
	Label string `json:"label"` // Czech label for the composer chip
	// Empty is what the notification says instead of an empty list — "žádné
	// připomínky". It lives on the descriptor because only the module knows the
	// right noun, and a summary that renders a bare placeholder for "nothing due
	// today" reads like a bug rather than like good news.
	Empty string `json:"empty,omitempty"`
	Scope string `json:"scope"` // ScopeHousehold | ScopePersonal
}

// Provider is what a module implements to publish lists.
type Provider interface {
	// Descriptors lists this module's lists.
	Descriptors() []Descriptor
	// Items resolves one of them for one recipient at one moment, in the order it
	// should be read. asOf is already in HOME_TIMEZONE, so "today" means the
	// recipient's local day.
	//
	// The result must be a BOUNDED set — the same bounded read a widget does. The
	// renderer shows the first few and counts the rest, so a provider that caps
	// its own query would make "…a další 4" a lie.
	Items(ctx context.Context, userID, key string, asOf time.Time) ([]string, error)
}

// Source is the optional interface a feature module implements to contribute its
// provider, discovered by type assertion exactly as metrics.Source is — adding
// lists changed no module contract.
type Source interface {
	ListProvider() Provider
}

// Registry is the assembled catalog. Built once at composition; read-only
// after. A thin typed facade over the shared catalog core (platform/catalog),
// which owns registration, lookup, and scope handling for every catalog.
//
// Every read is nil-receiver safe, so a host that wires no list catalog degrades
// to "no list is publishable" — an unknown-list 422 at save time — rather than
// panicking inside a notification send.
type Registry struct {
	core *catalog.Registry[Descriptor, Provider]
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{core: catalog.New[Descriptor, Provider]("lists", "list",
		func(d Descriptor) string { return d.Key },
		func(d Descriptor) string { return d.Scope },
	)}
}

// Register adds a provider's lists. A duplicate key is a programming error (two
// modules claiming one token) and fails the build of the registry rather than
// silently shadowing.
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
			continue // a module with no lists is normal, not an error
		}
		if err := r.Register(src.ListProvider()); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Catalog returns every published descriptor, ordered by key so the composer's
// palette is stable across restarts.
func (r *Registry) Catalog() []Descriptor {
	if r == nil {
		return nil
	}
	return r.core.Catalog()
}

// Descriptor looks up one list.
func (r *Registry) Descriptor(key string) (Descriptor, bool) {
	if r == nil {
		return Descriptor{}, false
	}
	return r.core.Descriptor(key)
}

// Has reports whether key is publishable — used to reject an unknown
// {{list.…}} token at SAVE time rather than at fire time.
func (r *Registry) Has(key string) bool {
	if r == nil {
		return false
	}
	return r.core.Has(key)
}

// HasPersonal reports whether any of the given keys personalizes. A summary
// whose lists are all household-scoped can be rendered ONCE for the whole
// audience instead of once per recipient.
func (r *Registry) HasPersonal(keys []string) bool {
	if r == nil {
		return false
	}
	return r.core.HasPersonal(keys)
}

// Resolve computes one list for one recipient.
func (r *Registry) Resolve(ctx context.Context, userID, key string, asOf time.Time) ([]string, error) {
	if r == nil {
		return nil, fmt.Errorf("lists: no list catalog is registered")
	}
	p, ok := r.core.Provider(key)
	if !ok {
		return nil, fmt.Errorf("lists: unknown list %q", key)
	}
	return p.Items(ctx, userID, key, asOf)
}
