package finance

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
)

// Handler serves the finance HTTP endpoints (openapi.yaml 0.8.0, tag `finance`).
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers finance routes on the (authenticated) /api router.
//
// Reads are ungated beyond the session — every member sees Finance, `reader`
// included (D84), and that a reader sees both household incomes is an accepted
// consequence recorded in the PRD. Writes take the ordinary RequireWrite gate.
//
// DELETE carries the SAME gate as create and edit, not an admin one: with hard
// delete (D87) there is no soft/hard distinction left to tier, so there is no
// admin-only route anywhere in this module. Finance is not `admin`/`logging`.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/finance/months", h.list)
	r.With(httpx.RequireWrite).Post("/finance/months", h.create)
	r.Get("/finance/months/{id}", h.get)
	r.With(httpx.RequireWrite).Patch("/finance/months/{id}", h.update)
	r.With(httpx.RequireWrite).Delete("/finance/months/{id}", h.delete) // permanent
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := h.svc.List(r.Context(), limit, q.Get("cursor"))
	httpx.Respond(w, http.StatusOK, page, err)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in MonthCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	m, err := h.svc.Create(r.Context(), in)
	httpx.Respond(w, http.StatusCreated, m, err)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	m, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	httpx.Respond(w, http.StatusOK, m, err)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var in MonthUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	m, err := h.svc.Update(r.Context(), chi.URLParam(r, "id"), in)
	httpx.Respond(w, http.StatusOK, m, err)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	httpx.NoContent(w, h.svc.Delete(r.Context(), chi.URLParam(r, "id")))
}
