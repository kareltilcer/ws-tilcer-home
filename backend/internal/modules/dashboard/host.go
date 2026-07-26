package dashboard

import (
	"context"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lexorank"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

// Service is the widget host. It composes the widget catalog (assembled by the
// core from every module's providers) with each user's stored layout, and fans
// out to visible providers. It holds no feature data.
type Service struct {
	catalog *registry.Catalog
	layout  *LayoutStore
}

// NewService builds the host over the widget catalog and the layout store.
func NewService(catalog *registry.Catalog, layout *LayoutStore) *Service {
	return &Service{catalog: catalog, layout: layout}
}

// Catalog returns the widgets this user may add (FR-D1).
func (s *Service) Catalog(u registry.User) []registry.CatalogEntry {
	return s.catalog.Entries(u.IsAdmin())
}

// defaultLayout is a first-run user's layout: every widget available to them,
// visible, at its default size, in catalog order (FR-D2).
func (s *Service) defaultLayout(u registry.User) []LayoutItem {
	entries := s.catalog.Entries(u.IsAdmin())
	keys := lexorank.NKeys(len(entries))
	out := make([]LayoutItem, 0, len(entries))
	for i, e := range entries {
		out = append(out, LayoutItem{WidgetKey: e.Key, Visible: true, Position: keys[i], Size: e.DefaultSize})
	}
	return out
}

// effectiveLayout returns the user's stored layout (default if none), dropping
// entries whose widget is no longer available to them (module removed, or an
// admin-only widget for a non-admin) — FR-D2 "ignore unknown/unavailable keys".
func (s *Service) effectiveLayout(ctx context.Context, u registry.User) ([]LayoutItem, error) {
	items, err := s.layout.Get(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return s.defaultLayout(u), nil
	}
	filtered := make([]LayoutItem, 0, len(items))
	for _, li := range items {
		if s.catalog.Available(li.WidgetKey, u.IsAdmin()) {
			filtered = append(filtered, li)
		}
	}
	return filtered, nil
}

// Dashboard fans out to each visible widget's provider and returns the layout +
// data (FR-D3). Providers are each bounded; the fan-out is sequential because the
// embedded SQLite pool is single-connection — concurrency would only serialize.
func (s *Service) Dashboard(ctx context.Context, u registry.User) (Dashboard, error) {
	layout, err := s.effectiveLayout(ctx, u)
	if err != nil {
		return Dashboard{}, err
	}
	widgets := make([]WidgetInstance, 0, len(layout))
	for _, li := range layout {
		if !li.Visible {
			continue
		}
		p, ok := s.catalog.Provider(li.WidgetKey)
		if !ok || (p.AdminOnly() && !u.IsAdmin()) {
			continue
		}
		data, err := p.Data(ctx, u)
		if err != nil {
			return Dashboard{}, err
		}
		widgets = append(widgets, WidgetInstance{Key: li.WidgetKey, Size: li.Size, Data: data})
	}
	return Dashboard{Layout: layout, Widgets: widgets}, nil
}

// SaveLayout upserts the caller's layout from an ordered input list (FR-D2).
// Unknown/unavailable and duplicate keys are ignored; order becomes lexorank
// positions. Allowed for any authenticated user, including a reader (the single
// write a reader may perform).
func (s *Service) SaveLayout(ctx context.Context, u registry.User, inputs []LayoutItemInput) ([]LayoutItem, error) {
	seen := map[string]bool{}
	valid := make([]LayoutItemInput, 0, len(inputs))
	for _, in := range inputs {
		if in.Size != "narrow" && in.Size != "wide" {
			return nil, httpx.ErrUnprocessable("size must be narrow or wide")
		}
		if seen[in.WidgetKey] || !s.catalog.Available(in.WidgetKey, u.IsAdmin()) {
			continue
		}
		seen[in.WidgetKey] = true
		valid = append(valid, in)
	}
	items := make([]LayoutItem, 0, len(valid))
	if len(valid) > 0 {
		keys := lexorank.NKeys(len(valid))
		for i, in := range valid {
			items = append(items, LayoutItem{WidgetKey: in.WidgetKey, Visible: in.Visible, Position: keys[i], Size: in.Size})
		}
	}
	if err := s.layout.Save(ctx, u.ID, items); err != nil {
		return nil, err
	}
	return items, nil
}

// Widget refreshes a single widget's payload (FR-D3, used on websocket pushes).
// Unknown key → 404; an admin-only widget for a non-admin → 403.
func (s *Service) Widget(ctx context.Context, u registry.User, key string) (WidgetInstance, error) {
	p, ok := s.catalog.Provider(key)
	if !ok {
		return WidgetInstance{}, httpx.ErrNotFound("unknown widget")
	}
	if p.AdminOnly() && !u.IsAdmin() {
		return WidgetInstance{}, httpx.ErrForbidden("admin only")
	}
	size := p.DefaultSize()
	if items, err := s.layout.Get(ctx, u.ID); err == nil {
		for _, li := range items {
			if li.WidgetKey == key {
				size = li.Size
			}
		}
	}
	data, err := p.Data(ctx, u)
	if err != nil {
		return WidgetInstance{}, err
	}
	return WidgetInstance{Key: key, Size: size, Data: data}, nil
}
