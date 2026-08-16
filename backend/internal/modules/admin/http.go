package admin

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

// Handler serves /api/admin/notifications/** (FR-ADM1–ADM6).
//
// Every route is ADMIN-ONLY, gated exactly like /api/logs/** (D62): the `*`
// superuser passes, there is no new tier, and there is no reader view — for a
// non-admin the module simply does not exist. Mutations additionally carry CSRF
// via the global middleware.
type Handler struct{ svc *Service }

// NewHandler builds the admin HTTP handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the admin routes behind the admin role gate.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/admin/notifications", func(ar chi.Router) {
		ar.Use(httpx.RequireAdmin)

		ar.Post("/broadcast", h.broadcast)
		ar.Get("/catalog", h.catalog)
		ar.Get("/deliveries", h.deliveries)

		ar.Get("/rules", h.listRules)
		ar.Post("/rules", h.createRule)
		ar.Get("/rules/{id}", h.getRule)
		ar.Patch("/rules/{id}", h.updateRule)
		ar.Delete("/rules/{id}", h.deleteRule)
		ar.Post("/rules/{id}/test", h.testRule)

		ar.Get("/schedules", h.listSchedules)
		ar.Post("/schedules", h.createSchedule)
		ar.Get("/schedules/{id}", h.getSchedule)
		ar.Patch("/schedules/{id}", h.updateSchedule)
		ar.Delete("/schedules/{id}", h.deleteSchedule)
		ar.Post("/schedules/{id}/test", h.testSchedule)
	})
}

// ---- rules ----

func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) {
	page, err := h.svc.ListRules(r.Context(), boolQuery(r, "enabled"), limitOf(r), r.URL.Query().Get("cursor"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, page)
}

func (h *Handler) getRule(w http.ResponseWriter, r *http.Request) {
	rule, err := h.svc.GetRule(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, rule)
}

func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) {
	var in RuleCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	rule, err := h.svc.CreateRule(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, rule)
}

func (h *Handler) updateRule(w http.ResponseWriter, r *http.Request) {
	var in RuleUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	rule, err := h.svc.UpdateRule(r.Context(), chi.URLParam(r, "id"), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, rule)
}

func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteRule(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) testRule(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.TestRule(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, res)
}

// ---- schedules ----

func (h *Handler) listSchedules(w http.ResponseWriter, r *http.Request) {
	page, err := h.svc.ListSchedules(r.Context(), boolQuery(r, "enabled"), limitOf(r), r.URL.Query().Get("cursor"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, page)
}

func (h *Handler) getSchedule(w http.ResponseWriter, r *http.Request) {
	sc, err := h.svc.GetSchedule(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, sc)
}

func (h *Handler) createSchedule(w http.ResponseWriter, r *http.Request) {
	var in ScheduleCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	sc, err := h.svc.CreateSchedule(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, sc)
}

func (h *Handler) updateSchedule(w http.ResponseWriter, r *http.Request) {
	var in ScheduleUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	sc, err := h.svc.UpdateSchedule(r.Context(), chi.URLParam(r, "id"), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, sc)
}

func (h *Handler) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteSchedule(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) testSchedule(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.TestSchedule(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, res)
}

// ---- broadcast, catalog, deliveries ----

func (h *Handler) broadcast(w http.ResponseWriter, r *http.Request) {
	var in BroadcastRequest
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	res, err := h.svc.Broadcast(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, res)
}

func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	cat, err := h.svc.BuildCatalog(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, cat)
}

func (h *Handler) deliveries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, err := h.svc.ListDeliveries(r.Context(), DeliveryFilter{
		Kind:   q.Get("kind"),
		Status: q.Get("status"),
		RuleID: q.Get("rule_id"),
		UserID: q.Get("user"),
		From:   q.Get("from"),
		To:     q.Get("to"),
		Limit:  limitOf(r),
		Cursor: q.Get("cursor"),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, page)
}

// ---- helpers ----

func limitOf(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}

// boolQuery returns nil when the parameter is absent, so "enabled=false" is
// distinguishable from "no filter".
func boolQuery(r *http.Request, key string) *bool {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return nil
	}
	return &v
}

// writeErr passes an APIError through unchanged and hides anything else behind a
// 500 — a SQL error must never reach a client verbatim.
func writeErr(w http.ResponseWriter, err error) {
	var apiErr *httpx.APIError
	if asAPIError(err, &apiErr) {
		httpx.WriteError(w, apiErr)
		return
	}
	httpx.WriteError(w, httpx.ErrInternal(""))
}

func asAPIError(err error, target **httpx.APIError) bool {
	if e, ok := err.(*httpx.APIError); ok {
		*target = e
		return true
	}
	return false
}
