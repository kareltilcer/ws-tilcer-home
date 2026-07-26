package dashboard

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Handler serves the widget-host endpoints. All are authorized from the session
// (the gated /api group); PUT /dashboard/layout is deliberately NOT behind
// RequireWrite so a reader may arrange their own view (D24).
type Handler struct{ svc *Service }

// NewHandler returns the dashboard HTTP handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the dashboard routes. Static sub-paths (catalog, layout,
// widgets/{key}) are distinct from the bare /dashboard route, so chi resolves
// them unambiguously (PRD §6 routing note).
func (h *Handler) Mount(r chi.Router) {
	r.Get("/dashboard", h.get)
	r.Get("/dashboard/catalog", h.catalog)
	r.Put("/dashboard/layout", h.putLayout)
	r.Get("/dashboard/widgets/{key}", h.widget)
}

// user builds the widget-provider view of the session actor.
func user(r *http.Request) registry.User {
	a, _ := reqctx.ActorFrom(r.Context())
	return registry.User{ID: a.UserID, Email: a.Label, Roles: a.Roles}
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Dashboard(r.Context(), user(r))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.svc.Catalog(user(r)))
}

func (h *Handler) putLayout(w http.ResponseWriter, r *http.Request) {
	var inputs []LayoutItemInput
	if err := httpx.DecodeJSON(r, &inputs); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	saved, err := h.svc.SaveLayout(r.Context(), user(r), inputs)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, saved)
}

func (h *Handler) widget(w http.ResponseWriter, r *http.Request) {
	inst, err := h.svc.Widget(r.Context(), user(r), chi.URLParam(r, "key"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, inst)
}
