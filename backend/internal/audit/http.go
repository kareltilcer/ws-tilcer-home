package audit

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/httpx"
)

// HTTPHandler serves the admin log-browser endpoints (FR-L3–L6). All routes
// require the admin role, enforced by the caller mounting them behind
// httpx.RequireAdmin.
type HTTPHandler struct{ store *Store }

// NewHTTPHandler returns the log-browser HTTP handler.
func NewHTTPHandler(store *Store) *HTTPHandler { return &HTTPHandler{store: store} }

// Mount registers the log routes on r (expected to be the /api/logs subrouter).
func (h *HTTPHandler) Mount(r chi.Router) {
	r.Get("/", h.list)
	r.Get("/stats", h.stats)
	r.Get("/entity/{type}/{entityId}", h.timeline)
	r.Get("/{id}", h.get)
}

func (h *HTTPHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := Filter{
		From:       q.Get("from"),
		To:         q.Get("to"),
		Module:     q.Get("module"),
		Actor:      q.Get("actor"),
		Action:     q.Get("action"),
		EntityType: q.Get("entity_type"),
		EntityID:   q.Get("entity_id"),
		Level:      q.Get("level"),
		Q:          q.Get("q"),
		Limit:      atoiOr(q.Get("limit"), 0),
		Cursor:     q.Get("cursor"),
	}
	page, err := h.store.Browse(r.Context(), f)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, page)
}

func (h *HTTPHandler) get(w http.ResponseWriter, r *http.Request) {
	detail, err := h.store.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if detail == nil {
		httpx.WriteError(w, httpx.ErrNotFound("event not found"))
		return
	}
	httpx.JSON(w, http.StatusOK, detail)
}

func (h *HTTPHandler) timeline(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, err := h.store.Timeline(r.Context(),
		chi.URLParam(r, "type"), chi.URLParam(r, "entityId"),
		q.Get("from"), q.Get("to"), atoiOr(q.Get("limit"), 0), q.Get("cursor"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, page)
}

func (h *HTTPHandler) stats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	bucket := q.Get("bucket")
	if bucket == "" {
		bucket = "day"
	}
	res, err := h.store.Stats(r.Context(), q.Get("dimension"), bucket, q.Get("from"), q.Get("to"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

func writeErr(w http.ResponseWriter, err error) {
	var ie *InvalidError
	if errors.As(err, &ie) {
		httpx.WriteError(w, httpx.ErrUnprocessable(ie.Error()))
		return
	}
	httpx.WriteError(w, httpx.ErrInternal(""))
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
