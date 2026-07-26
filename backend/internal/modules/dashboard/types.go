// Package dashboard is the Nástěnka widget host (PRD §4 FR-D1–D5, D24/D28). It
// owns exactly two things: each user's layout (user_dashboard_layout) and the
// fan-out that renders it. It reaches feature data ONLY through the
// registry.WidgetProvider contract — it never imports todo/events or queries
// their tables. If a SELECT ... FROM cards appears here, it's wrong.
package dashboard

// LayoutItem is one persisted widget placement (matches openapi LayoutItem).
type LayoutItem struct {
	WidgetKey string `json:"widget_key"`
	Visible   bool   `json:"visible"`
	Position  string `json:"position"` // lexorank order key
	Size      string `json:"size"`     // narrow | wide
}

// LayoutItemInput is a PUT /api/dashboard/layout row; order is taken from the
// array and position keys are derived server-side (openapi LayoutItemInput).
type LayoutItemInput struct {
	WidgetKey string `json:"widget_key"`
	Visible   bool   `json:"visible"`
	Size      string `json:"size"`
}

// WidgetInstance is one rendered widget: its key, size, and provider payload
// (openapi WidgetInstance). Data is whatever the owning module's provider
// returned — the host does not interpret it.
type WidgetInstance struct {
	Key  string `json:"key"`
	Size string `json:"size"`
	Data any    `json:"data"`
}

// Dashboard is the GET /api/dashboard response: the layout plus each visible
// widget's data, in layout order (openapi Dashboard).
type Dashboard struct {
	Layout  []LayoutItem     `json:"layout"`
	Widgets []WidgetInstance `json:"widgets"`
}
