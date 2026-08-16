package lists_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
)

// fake is a provider whose items are whatever the test dictates.
type fake struct {
	descriptors []lists.Descriptor
	items       map[string][]string
	perUser     map[string]map[string][]string
	err         error
}

func (f *fake) Descriptors() []lists.Descriptor { return f.descriptors }

func (f *fake) Items(_ context.Context, userID, key string, _ time.Time) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	if byUser, ok := f.perUser[userID]; ok {
		if v, ok := byUser[key]; ok {
			return v, nil
		}
	}
	return f.items[key], nil
}

// source wraps a provider as a module would.
type source struct{ p lists.Provider }

func (s source) ListProvider() lists.Provider { return s.p }

// notASource stands for a module that publishes no lists — the common case.
type notASource struct{}

func TestRegistryResolvesRegisteredKeys(t *testing.T) {
	r := lists.NewRegistry()
	if err := r.Register(&fake{
		descriptors: []lists.Descriptor{
			{Key: "events.today", Label: "Dnes", Empty: "žádné připomínky", Scope: lists.ScopeHousehold},
		},
		items: map[string][]string{"events.today": {"Vynést koš", "Zalít kytky"}},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := r.Resolve(context.Background(), "u1", "events.today", time.Now())
	if err != nil || strings.Join(got, "|") != "Vynést koš|Zalít kytky" {
		t.Errorf("Resolve = %v (%v), want the two items in order", got, err)
	}
	if !r.Has("events.today") {
		t.Error("Has = false for a registered key")
	}
	if d, ok := r.Descriptor("events.today"); !ok || d.Empty != "žádné připomínky" {
		t.Errorf("Descriptor = %+v (%v), want the empty text the module published", d, ok)
	}
}

// An unknown key must fail at the registry, which is what lets the composer
// reject a bad {{list.…}} token at SAVE time instead of at fire time.
func TestRegistryRejectsUnknownKey(t *testing.T) {
	r := lists.NewRegistry()
	if r.Has("nope.nope") {
		t.Error("Has = true for an unregistered key")
	}
	if _, err := r.Resolve(context.Background(), "u1", "nope.nope", time.Now()); err == nil {
		t.Error("expected an error resolving an unknown list")
	}
}

func TestRegistryRejectsDuplicateKeys(t *testing.T) {
	r := lists.NewRegistry()
	d := []lists.Descriptor{{Key: "dup.key", Label: "A", Scope: lists.ScopeHousehold}}
	if err := r.Register(&fake{descriptors: d}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := r.Register(&fake{descriptors: d})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("second register err = %v, want a duplicate-key complaint", err)
	}
}

func TestRegistryValidatesScope(t *testing.T) {
	r := lists.NewRegistry()
	err := r.Register(&fake{descriptors: []lists.Descriptor{{Key: "x.y", Label: "X", Scope: "global"}}})
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Errorf("err = %v, want an invalid-scope complaint", err)
	}
}

// Collect discovers providers by optional interface; a module without lists is
// skipped rather than treated as an error.
func TestCollectSkipsModulesWithoutLists(t *testing.T) {
	r, err := lists.Collect(
		notASource{},
		source{&fake{descriptors: []lists.Descriptor{{Key: "a.b", Label: "A", Scope: lists.ScopeHousehold}}}},
		notASource{},
	)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if cat := r.Catalog(); len(cat) != 1 || cat[0].Key != "a.b" {
		t.Errorf("catalog = %+v, want just a.b", cat)
	}
}

// The catalog drives a palette, so its order must not depend on map iteration.
func TestCatalogIsSortedByKey(t *testing.T) {
	r := lists.NewRegistry()
	_ = r.Register(&fake{descriptors: []lists.Descriptor{
		{Key: "z.last", Label: "Z", Scope: lists.ScopeHousehold},
		{Key: "a.first", Label: "A", Scope: lists.ScopeHousehold},
		{Key: "m.middle", Label: "M", Scope: lists.ScopePersonal},
	}})
	var keys []string
	for _, d := range r.Catalog() {
		keys = append(keys, d.Key)
	}
	if strings.Join(keys, ",") != "a.first,m.middle,z.last" {
		t.Errorf("catalog order = %v, want sorted by key", keys)
	}
}

func TestHasPersonal(t *testing.T) {
	r := lists.NewRegistry()
	_ = r.Register(&fake{descriptors: []lists.Descriptor{
		{Key: "events.today", Label: "T", Scope: lists.ScopeHousehold},
		{Key: "notes.pinned", Label: "N", Scope: lists.ScopePersonal},
	}})

	if r.HasPersonal([]string{"events.today"}) {
		t.Error("HasPersonal = true for a household-only template")
	}
	if !r.HasPersonal([]string{"events.today", "notes.pinned"}) {
		t.Error("HasPersonal = false although one list personalizes")
	}
	if r.HasPersonal([]string{"unknown.key"}) {
		t.Error("HasPersonal = true for an unknown key")
	}
}

// A personal list must actually see the recipient — this is the mechanism the
// per-recipient render rests on.
func TestResolvePassesTheRecipient(t *testing.T) {
	r := lists.NewRegistry()
	_ = r.Register(&fake{
		descriptors: []lists.Descriptor{{Key: "notes.pinned", Label: "N", Scope: lists.ScopePersonal}},
		perUser: map[string]map[string][]string{
			"karel": {"notes.pinned": {"Wi-Fi heslo"}},
			"eva":   {"notes.pinned": {"Nákup", "Recept na bábovku"}},
		},
	})

	karel, _ := r.Resolve(context.Background(), "karel", "notes.pinned", time.Now())
	eva, _ := r.Resolve(context.Background(), "eva", "notes.pinned", time.Now())
	if len(karel) != 1 || len(eva) != 2 {
		t.Errorf("per-recipient resolve = %v/%v, want 1 and 2 items", karel, eva)
	}
}

// A host that never wired a list catalog must degrade, not panic: the nil
// registry is what an admin's save is validated against in that case.
func TestNilRegistryIsSafeToRead(t *testing.T) {
	var r *lists.Registry
	if r.Has("anything") {
		t.Error("Has = true on a nil registry")
	}
	if r.HasPersonal([]string{"anything"}) {
		t.Error("HasPersonal = true on a nil registry")
	}
	if cat := r.Catalog(); len(cat) != 0 {
		t.Errorf("Catalog = %v on a nil registry", cat)
	}
	if _, err := r.Resolve(context.Background(), "u1", "anything", time.Now()); err == nil {
		t.Error("expected an error resolving against a nil registry")
	}
}
