package events

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/httpx"
)

// Handler serves the events HTTP endpoints. Reads open to any authenticated
// user; writes gated with httpx.RequireWrite at mount time.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers events routes on the (authenticated) /api router. The static
// /events/occurrences route is registered before the parameterised /events/{id}
// so "occurrences" is never parsed as an event id (chi prioritises static
// segments; this makes it explicit and covered by a test).
func (h *Handler) Mount(r chi.Router) {
	r.Get("/events", h.list)
	r.With(httpx.RequireWrite).Post("/events", h.create)
	r.Get("/events/occurrences", h.occurrences)
	r.Get("/events/{id}", h.get)
	r.With(httpx.RequireWrite).Patch("/events/{id}", h.update)
	r.With(httpx.RequireWrite).Delete("/events/{id}", h.delete)

	r.Get("/events/{id}/links", h.listLinks)
	r.With(httpx.RequireWrite).Post("/events/{id}/links", h.createLink)
	r.With(httpx.RequireWrite).Delete("/event-links/{id}", h.deleteLink)

	r.With(httpx.RequireWrite).Post("/events/{id}/complete", h.complete)
	r.With(httpx.RequireWrite).Delete("/events/{id}/complete", h.uncomplete)
}

func boolParam(r *http.Request, name string) bool {
	v := r.URL.Query().Get(name)
	return v == "true" || v == "1"
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := h.svc.ListSeries(r.Context(), boolParam(r, "include_archived"), limit, q.Get("cursor"))
	respond(w, http.StatusOK, page, err)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in EventCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	e, err := h.svc.CreateEvent(r.Context(), in)
	respond(w, http.StatusCreated, e, err)
}

func (h *Handler) occurrences(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	months, err := h.svc.Occurrences(r.Context(), q.Get("from"), q.Get("to"), boolParam(r, "include_archived"))
	respond(w, http.StatusOK, months, err)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	e, err := h.svc.GetEvent(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, e, err)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var in EventUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	e, err := h.svc.UpdateEvent(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, e, err)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	respondNoContent(w, h.svc.DeleteEvent(r.Context(), chi.URLParam(r, "id"), boolParam(r, "hard")))
}

func (h *Handler) listLinks(w http.ResponseWriter, r *http.Request) {
	links, err := h.svc.ListLinks(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, links, err)
}

func (h *Handler) createLink(w http.ResponseWriter, r *http.Request) {
	var in EventLinkCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	l, err := h.svc.CreateLink(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusCreated, l, err)
}

func (h *Handler) deleteLink(w http.ResponseWriter, r *http.Request) {
	respondNoContent(w, h.svc.DeleteLink(r.Context(), chi.URLParam(r, "id")))
}

type completeReq struct {
	OccurrenceOn string `json:"occurrence_on"`
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	var in completeReq
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	c, err := h.svc.Complete(r.Context(), chi.URLParam(r, "id"), in.OccurrenceOn, r.URL.Query().Get("via"))
	respond(w, http.StatusOK, c, err)
}

func (h *Handler) uncomplete(w http.ResponseWriter, r *http.Request) {
	err := h.svc.Uncomplete(r.Context(), chi.URLParam(r, "id"), r.URL.Query().Get("occurrence_on"))
	respondNoContent(w, err)
}

func respond(w http.ResponseWriter, status int, v any, err error) {
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, status, v)
}

func respondNoContent(w http.ResponseWriter, err error) {
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
