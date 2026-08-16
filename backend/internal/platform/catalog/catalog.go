// Package catalog is the shared plumbing under the metric and list catalogs
// (platform/metrics, platform/lists): one registry implementation, generic over
// the descriptor and provider types, so a fix to registration, lookup, or scope
// handling lands once instead of once per catalog. The catalogs stay separate
// packages on purpose — a metric resolves to an int and a list to a bounded
// slice of lines, and their descriptors differ — but the mechanics do not.
package catalog

import (
	"fmt"
	"sort"
)

// Scopes describe whether a catalog entry's value personalizes. ONE definition,
// aliased by both catalogs: the admin scheduler filters tokens from BOTH
// catalogs through one predicate, which is only sound while the scope
// vocabulary cannot diverge.
const (
	// ScopeHousehold: the same value for everyone (a shared board count).
	ScopeHousehold = "household"
	// ScopePersonal: computed per recipient (my pinned notes ≠ yours). This is
	// what makes summaries resolve once PER RECIPIENT rather than once per send.
	ScopePersonal = "personal"
)

// Registry is the assembled catalog core. Built once at composition; read-only
// after. D is the catalog's descriptor type, P its provider type; key and scope
// teach the core to read a D without the descriptor needing methods.
//
// Every read is nil-receiver safe, so a host that wires no catalog degrades to
// "nothing is publishable" rather than panicking inside a notification send.
type Registry[D any, P any] struct {
	name        string // "metrics" | "lists" — the error-message prefix
	noun        string // "metric" | "list" — what one entry is called
	key, scope  func(D) string
	descriptors []D
	byKey       map[string]P
	scopes      map[string]string
}

// New returns an empty core for a catalog whose errors speak as name/noun.
func New[D any, P any](name, noun string, key, scope func(D) string) *Registry[D, P] {
	return &Registry[D, P]{
		name: name, noun: noun, key: key, scope: scope,
		byKey: map[string]P{}, scopes: map[string]string{},
	}
}

// Register adds one provider's descriptors. A duplicate key is a programming
// error (two modules claiming one token) and fails the build of the registry
// rather than silently shadowing.
func (r *Registry[D, P]) Register(p P, descriptors []D) error {
	for _, d := range descriptors {
		k := r.key(d)
		if k == "" {
			return fmt.Errorf("%s: provider published a descriptor with no key", r.name)
		}
		if _, dup := r.byKey[k]; dup {
			return fmt.Errorf("%s: duplicate %s key %q", r.name, r.noun, k)
		}
		if s := r.scope(d); s != ScopeHousehold && s != ScopePersonal {
			return fmt.Errorf("%s: %s %q has invalid scope %q", r.name, r.noun, k, s)
		}
		r.byKey[k] = p
		r.scopes[k] = r.scope(d)
		r.descriptors = append(r.descriptors, d)
	}
	return nil
}

// Catalog returns every published descriptor, ordered by key so the composer's
// palette is stable across restarts.
func (r *Registry[D, P]) Catalog() []D {
	if r == nil {
		return nil
	}
	out := append([]D(nil), r.descriptors...)
	sort.Slice(out, func(i, j int) bool { return r.key(out[i]) < r.key(out[j]) })
	return out
}

// Descriptor looks up one entry.
func (r *Registry[D, P]) Descriptor(key string) (D, bool) {
	if r != nil {
		for _, d := range r.descriptors {
			if r.key(d) == key {
				return d, true
			}
		}
	}
	var zero D
	return zero, false
}

// Has reports whether key is publishable — what lets an unknown token be
// refused at SAVE time rather than at fire time.
func (r *Registry[D, P]) Has(key string) bool {
	if r == nil {
		return false
	}
	_, ok := r.byKey[key]
	return ok
}

// HasPersonal reports whether any of the given keys personalizes. A summary
// whose tokens are all household-scoped can be rendered ONCE for the whole
// audience instead of once per recipient.
func (r *Registry[D, P]) HasPersonal(keys []string) bool {
	if r == nil {
		return false
	}
	for _, k := range keys {
		if r.scopes[k] == ScopePersonal {
			return true
		}
	}
	return false
}

// Provider returns the provider registered for key, for the catalog's typed
// Resolve to call.
func (r *Registry[D, P]) Provider(key string) (P, bool) {
	if r == nil {
		var zero P
		return zero, false
	}
	p, ok := r.byKey[key]
	return p, ok
}
