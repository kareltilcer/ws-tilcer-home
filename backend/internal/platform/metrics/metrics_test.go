package metrics_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
)

// fake is a provider whose values are whatever the test dictates.
type fake struct {
	descriptors []metrics.Descriptor
	values      map[string]int
	perUser     map[string]map[string]int
	err         error
}

func (f *fake) Descriptors() []metrics.Descriptor { return f.descriptors }

func (f *fake) Value(_ context.Context, userID, key string, _ time.Time) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	if byUser, ok := f.perUser[userID]; ok {
		if v, ok := byUser[key]; ok {
			return v, nil
		}
	}
	return f.values[key], nil
}

// source wraps a provider as a module would.
type source struct{ p metrics.Provider }

func (s source) MetricProvider() metrics.Provider { return s.p }

// notASource stands for a module that publishes no metrics — the common case.
type notASource struct{}

func TestRegistryResolvesRegisteredKeys(t *testing.T) {
	r := metrics.NewRegistry()
	if err := r.Register(&fake{
		descriptors: []metrics.Descriptor{{Key: "todo.open", Label: "Otevřené", Unit: "úkolů", Scope: metrics.ScopeHousehold}},
		values:      map[string]int{"todo.open": 7},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := r.Resolve(context.Background(), "u1", "todo.open", time.Now())
	if err != nil || got != 7 {
		t.Errorf("Resolve = %d (%v), want 7", got, err)
	}
	if !r.Has("todo.open") {
		t.Error("Has = false for a registered key")
	}
}

// An unknown key must fail at the registry, which is what lets the composer
// reject a bad {{metric.…}} token at SAVE time instead of at fire time.
func TestRegistryRejectsUnknownKey(t *testing.T) {
	r := metrics.NewRegistry()
	if r.Has("nope.nope") {
		t.Error("Has = true for an unregistered key")
	}
	if _, err := r.Resolve(context.Background(), "u1", "nope.nope", time.Now()); err == nil {
		t.Error("expected an error resolving an unknown metric")
	}
}

// Two modules claiming one token is a programming error, not a silent shadow.
func TestRegistryRejectsDuplicateKeys(t *testing.T) {
	r := metrics.NewRegistry()
	d := []metrics.Descriptor{{Key: "dup.key", Label: "A", Scope: metrics.ScopeHousehold}}
	if err := r.Register(&fake{descriptors: d}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := r.Register(&fake{descriptors: d})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("second register err = %v, want a duplicate-key complaint", err)
	}
}

func TestRegistryValidatesScope(t *testing.T) {
	r := metrics.NewRegistry()
	err := r.Register(&fake{descriptors: []metrics.Descriptor{{Key: "x.y", Label: "X", Scope: "global"}}})
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Errorf("err = %v, want an invalid-scope complaint", err)
	}
}

// Collect discovers providers by optional interface; a module without metrics is
// skipped rather than treated as an error.
func TestCollectSkipsModulesWithoutMetrics(t *testing.T) {
	r, err := metrics.Collect(
		notASource{},
		source{&fake{descriptors: []metrics.Descriptor{{Key: "a.b", Label: "A", Scope: metrics.ScopeHousehold}}}},
		notASource{},
	)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if cat := r.Catalog(); len(cat) != 1 || cat[0].Key != "a.b" {
		t.Errorf("catalog = %+v, want just a.b", cat)
	}
}

// The catalog drives a dropdown, so its order must not depend on map iteration.
func TestCatalogIsSortedByKey(t *testing.T) {
	r := metrics.NewRegistry()
	_ = r.Register(&fake{descriptors: []metrics.Descriptor{
		{Key: "z.last", Label: "Z", Scope: metrics.ScopeHousehold},
		{Key: "a.first", Label: "A", Scope: metrics.ScopeHousehold},
		{Key: "m.middle", Label: "M", Scope: metrics.ScopePersonal},
	}})
	var keys []string
	for _, d := range r.Catalog() {
		keys = append(keys, d.Key)
	}
	if strings.Join(keys, ",") != "a.first,m.middle,z.last" {
		t.Errorf("catalog order = %v, want sorted by key", keys)
	}
}

// HasPersonal is what lets the scheduler render once for the whole audience when
// nothing in the template personalizes.
func TestHasPersonal(t *testing.T) {
	r := metrics.NewRegistry()
	_ = r.Register(&fake{descriptors: []metrics.Descriptor{
		{Key: "todo.count", Label: "T", Scope: metrics.ScopeHousehold},
		{Key: "notes.pinned", Label: "N", Scope: metrics.ScopePersonal},
	}})

	if r.HasPersonal([]string{"todo.count"}) {
		t.Error("HasPersonal = true for a household-only template")
	}
	if !r.HasPersonal([]string{"todo.count", "notes.pinned"}) {
		t.Error("HasPersonal = false although one token personalizes")
	}
	if r.HasPersonal([]string{"unknown.key"}) {
		t.Error("HasPersonal = true for an unknown key")
	}
}

// A personal metric must actually see the recipient — this is the mechanism the
// whole per-recipient render rests on.
func TestResolvePassesTheRecipient(t *testing.T) {
	r := metrics.NewRegistry()
	_ = r.Register(&fake{
		descriptors: []metrics.Descriptor{{Key: "notes.pinned", Label: "N", Scope: metrics.ScopePersonal}},
		perUser: map[string]map[string]int{
			"karel": {"notes.pinned": 3},
			"eva":   {"notes.pinned": 8},
		},
	})

	karel, _ := r.Resolve(context.Background(), "karel", "notes.pinned", time.Now())
	eva, _ := r.Resolve(context.Background(), "eva", "notes.pinned", time.Now())
	if karel != 3 || eva != 8 {
		t.Errorf("per-recipient resolve = %d/%d, want 3/8", karel, eva)
	}
}
