package garden

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
)

// Handler serves the garden HTTP endpoints (openapi.yaml 0.9.0, tag `garden`).
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the garden routes on the (authenticated) /api router.
//
// READS are ungated beyond the session — every member sees Zahrada, `reader`
// included. WRITES take the ordinary RequireWrite gate, and TICKING A TASK OFF
// IS AN ORDINARY WRITE (D124): a reader exception was considered and declined,
// so a reader sees the widget and its controls disabled rather than hidden.
//
// EXACTLY ONE ADMIN-ONLY ROUTE exists in this module: re-opening a closed
// season, because that rewrites the rotation history C3 and C8 depend on.
// Everything else an admin can do here, an editor can do too.
func (h *Handler) Mount(r chi.Router) {
	// Knowledge base.
	r.Get("/garden/plants", h.listPlants)
	r.With(httpx.RequireWrite).Post("/garden/plants", h.createPlant)
	// The two literal paths are registered BEFORE /{id} — chi matches literals
	// first regardless, but keeping them together is how a reader sees that
	// "prompt-template" is not a crop id.
	r.Get("/garden/plants/prompt-template", h.promptTemplate)
	r.With(httpx.RequireWrite).Post("/garden/plants/import", h.importPlants)
	r.Get("/garden/export", h.export)
	r.Get("/garden/plants/{id}", h.getPlant)
	r.With(httpx.RequireWrite).Patch("/garden/plants/{id}", h.updatePlant)
	r.With(httpx.RequireWrite).Delete("/garden/plants/{id}", h.deletePlant)

	// Varieties.
	r.Get("/garden/plants/{id}/varieties", h.listVarieties)
	r.With(httpx.RequireWrite).Post("/garden/plants/{id}/varieties", h.createVariety)
	r.Get("/garden/varieties/{id}", h.getVariety)
	r.With(httpx.RequireWrite).Patch("/garden/varieties/{id}", h.updateVariety)
	r.With(httpx.RequireWrite).Delete("/garden/varieties/{id}", h.deleteVariety)

	// Beds. The move route is what changes adjacency (D117) — an ordinary write,
	// but the audit summary says what it did.
	r.Get("/garden/beds", h.listBeds)
	r.With(httpx.RequireWrite).Post("/garden/beds", h.createBed)
	r.Get("/garden/beds/{id}", h.getBed)
	r.With(httpx.RequireWrite).Patch("/garden/beds/{id}", h.updateBed)
	r.With(httpx.RequireWrite).Delete("/garden/beds/{id}", h.deleteBed)
	r.With(httpx.RequireWrite).Post("/garden/beds/{id}/move", h.moveBed)
	r.Get("/garden/beds/{id}/history", h.bedHistory)

	// Seasons, addressed by {year} (D128).
	r.Get("/garden/seasons", h.listSeasons)
	r.With(httpx.RequireWrite).Post("/garden/seasons", h.createSeason)
	r.Get("/garden/seasons/{year}", h.getSeason)
	r.With(httpx.RequireWrite).Patch("/garden/seasons/{year}", h.updateSeason)
	r.With(httpx.RequireWrite).Post("/garden/seasons/{year}/close", h.closeSeason)
	r.With(httpx.RequireAdmin).Post("/garden/seasons/{year}/reopen", h.reopenSeason)

	// Kontrola plánu.
	r.Get("/garden/seasons/{year}/check", h.checkSeason)
	r.With(httpx.RequireWrite).Post("/garden/seasons/{year}/check/dismissals", h.dismissWarning)
	r.With(httpx.RequireWrite).Delete("/garden/seasons/{year}/check/dismissals/{key}", h.restoreWarning)

	// Plantings.
	r.Get("/garden/plantings", h.listPlantings)
	r.With(httpx.RequireWrite).Post("/garden/plantings", h.createPlanting)
	r.Get("/garden/plantings/{id}", h.getPlanting)
	r.With(httpx.RequireWrite).Patch("/garden/plantings/{id}", h.updatePlanting)
	r.With(httpx.RequireWrite).Delete("/garden/plantings/{id}", h.deletePlanting)
	r.With(httpx.RequireWrite).Post("/garden/plantings/{id}/shift-tasks", h.shiftTasks)

	// Work calendar.
	r.Get("/garden/tasks", h.listTasks)
	r.With(httpx.RequireWrite).Post("/garden/tasks", h.createTask)
	r.Get("/garden/tasks/{id}", h.getTask)
	r.With(httpx.RequireWrite).Patch("/garden/tasks/{id}", h.updateTask)
	r.With(httpx.RequireWrite).Delete("/garden/tasks/{id}", h.deleteTask)
	r.With(httpx.RequireWrite).Post("/garden/tasks/{id}/complete", h.completeTask)
	r.With(httpx.RequireWrite).Post("/garden/tasks/{id}/reopen", h.reopenTask)

	// Harvest and storage.
	r.Get("/garden/harvests", h.listHarvests)
	r.With(httpx.RequireWrite).Post("/garden/harvests", h.createHarvest)
	r.Get("/garden/harvests/{id}", h.getHarvest)
	r.With(httpx.RequireWrite).Patch("/garden/harvests/{id}", h.updateHarvest)
	r.With(httpx.RequireWrite).Delete("/garden/harvests/{id}", h.deleteHarvest)
	r.Get("/garden/storage", h.listStorage)
	r.With(httpx.RequireWrite).Post("/garden/storage", h.createStorage)
	r.Get("/garden/storage/{id}", h.getStorage)
	r.With(httpx.RequireWrite).Patch("/garden/storage/{id}", h.updateStorage)
	r.With(httpx.RequireWrite).Delete("/garden/storage/{id}", h.deleteStorage)

	// Rules, settings, reference.
	r.Get("/garden/rules", h.listRules)
	r.With(httpx.RequireWrite).Post("/garden/rules", h.createRule)
	r.Get("/garden/rules/{id}", h.getRule)
	r.With(httpx.RequireWrite).Patch("/garden/rules/{id}", h.updateRule)
	r.With(httpx.RequireWrite).Delete("/garden/rules/{id}", h.deleteRule)
	r.Get("/garden/settings", h.getSettings)
	r.With(httpx.RequireWrite).Patch("/garden/settings", h.updateSettings)
	r.Get("/garden/weather", h.weather)
	r.Get("/garden/enums", h.enums)
}

// ============================== plants ==============================

func (h *Handler) listPlants(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := h.svc.ListPlants(r.Context(), PlantFilter{
		Query:      q.Get("q"),
		Family:     q.Get("family"),
		PlantType:  q.Get("plant_type"),
		Unverified: q.Get("unverified") == "true",
	}, limit, q.Get("cursor"))
	respond(w, http.StatusOK, page, err)
}

func (h *Handler) createPlant(w http.ResponseWriter, r *http.Request) {
	var in PlantInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	p, err := h.svc.CreatePlant(r.Context(), in)
	respond(w, http.StatusCreated, p, err)
}

func (h *Handler) getPlant(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.GetPlant(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, p, err)
}

func (h *Handler) updatePlant(w http.ResponseWriter, r *http.Request) {
	var in PlantInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	p, err := h.svc.UpdatePlant(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, p, err)
}

func (h *Handler) deletePlant(w http.ResponseWriter, r *http.Request) {
	noContent(w, h.svc.DeletePlant(r.Context(), chi.URLParam(r, "id")))
}

// ============================= varieties =============================

func (h *Handler) listVarieties(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := h.svc.ListVarieties(r.Context(), chi.URLParam(r, "id"), limit, r.URL.Query().Get("cursor"))
	respond(w, http.StatusOK, page, err)
}

func (h *Handler) createVariety(w http.ResponseWriter, r *http.Request) {
	var in VarietyInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	v, err := h.svc.CreateVariety(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusCreated, v, err)
}

func (h *Handler) getVariety(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.GetVariety(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, v, err)
}

func (h *Handler) updateVariety(w http.ResponseWriter, r *http.Request) {
	var in VarietyInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	v, err := h.svc.UpdateVariety(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, v, err)
}

func (h *Handler) deleteVariety(w http.ResponseWriter, r *http.Request) {
	noContent(w, h.svc.DeleteVariety(r.Context(), chi.URLParam(r, "id")))
}

// ================================ beds ================================

func (h *Handler) listBeds(w http.ResponseWriter, r *http.Request) {
	beds, err := h.svc.ListBeds(r.Context(), r.URL.Query().Get("include_inactive") == "true")
	respond(w, http.StatusOK, map[string]any{"items": beds}, err)
}

func (h *Handler) createBed(w http.ResponseWriter, r *http.Request) {
	var in BedInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	b, err := h.svc.CreateBed(r.Context(), in)
	respond(w, http.StatusCreated, b, err)
}

func (h *Handler) getBed(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.GetBed(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, b, err)
}

func (h *Handler) updateBed(w http.ResponseWriter, r *http.Request) {
	var in BedInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	b, err := h.svc.UpdateBed(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, b, err)
}

func (h *Handler) deleteBed(w http.ResponseWriter, r *http.Request) {
	noContent(w, h.svc.DeleteBed(r.Context(), chi.URLParam(r, "id")))
}

func (h *Handler) moveBed(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Zone     *string `json:"zone"`
		AfterID  string  `json:"after_id"`
		BeforeID string  `json:"before_id"`
	}
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	b, err := h.svc.MoveBed(r.Context(), chi.URLParam(r, "id"), in.Zone, in.AfterID, in.BeforeID)
	respond(w, http.StatusOK, b, err)
}

func (h *Handler) bedHistory(w http.ResponseWriter, r *http.Request) {
	hist, err := h.svc.BedHistory(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, hist, err)
}

// =============================== seasons ===============================

func (h *Handler) listSeasons(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListSeasons(r.Context())
	respond(w, http.StatusOK, map[string]any{"items": items}, err)
}

// createSeason: dry_run DEFAULTS FALSE here (D129), unlike the import. A season
// creation should not require a second call in the common case; an import should
// never apply by accident. Both defaults are deliberate and they differ on
// purpose.
func (h *Handler) createSeason(w http.ResponseWriter, r *http.Request) {
	var in SeasonCreateInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	dryRun := r.URL.Query().Get("dry_run") == "true"
	season, preview, err := h.svc.CreateSeason(r.Context(), in, dryRun)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if preview != nil {
		httpx.JSON(w, http.StatusOK, preview) // 200, not 201: nothing was created
		return
	}
	httpx.JSON(w, http.StatusCreated, season)
}

func (h *Handler) getSeason(w http.ResponseWriter, r *http.Request) {
	year, err := yearParam(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	se, err := h.svc.GetSeason(r.Context(), year)
	respond(w, http.StatusOK, se, err)
}

func (h *Handler) updateSeason(w http.ResponseWriter, r *http.Request) {
	year, err := yearParam(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var in SeasonUpdateInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	se, err := h.svc.UpdateSeason(r.Context(), year, in)
	respond(w, http.StatusOK, se, err)
}

func (h *Handler) closeSeason(w http.ResponseWriter, r *http.Request) {
	year, err := yearParam(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var in SeasonCloseInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	se, err := h.svc.CloseSeason(r.Context(), year, in)
	respond(w, http.StatusOK, se, err)
}

func (h *Handler) reopenSeason(w http.ResponseWriter, r *http.Request) {
	year, err := yearParam(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	se, err := h.svc.ReopenSeason(r.Context(), year)
	respond(w, http.StatusOK, se, err)
}

// ================================ check ================================

func (h *Handler) checkSeason(w http.ResponseWriter, r *http.Request) {
	year, err := yearParam(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	res, err := h.svc.CheckSeason(r.Context(), year)
	respond(w, http.StatusOK, res, err)
}

func (h *Handler) dismissWarning(w http.ResponseWriter, r *http.Request) {
	year, err := yearParam(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var in struct {
		Key  string  `json:"key"`
		Note *string `json:"note"`
	}
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := h.svc.DismissWarning(r.Context(), year, in.Key, in.Note); err != nil {
		httpx.WriteError(w, err)
		return
	}
	res, err := h.svc.CheckSeason(r.Context(), year)
	respond(w, http.StatusOK, res, err)
}

func (h *Handler) restoreWarning(w http.ResponseWriter, r *http.Request) {
	year, err := yearParam(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	noContent(w, h.svc.RestoreWarning(r.Context(), year, chi.URLParam(r, "key")))
}

// ============================== plantings ==============================

func (h *Handler) listPlantings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	f := PlantingFilter{BedID: q.Get("bed_id"), PlantID: q.Get("plant_id"), Status: q.Get("status")}
	if v := q.Get("year"); v != "" {
		year, err := strconv.Atoi(v)
		if err != nil {
			httpx.WriteError(w, httpx.ErrUnprocessable("Rok musí být číslo."))
			return
		}
		f.Year = &year
	}
	switch q.Get("permanent") {
	case "true":
		f.Permanent = bp(true)
	case "false":
		f.Permanent = bp(false)
	}
	page, err := h.svc.ListPlantings(r.Context(), f, limit, q.Get("cursor"))
	respond(w, http.StatusOK, page, err)
}

func (h *Handler) createPlanting(w http.ResponseWriter, r *http.Request) {
	var in PlantingInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	p, err := h.svc.CreatePlanting(r.Context(), in)
	respond(w, http.StatusCreated, p, err)
}

func (h *Handler) getPlanting(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.GetPlanting(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, p, err)
}

func (h *Handler) updatePlanting(w http.ResponseWriter, r *http.Request) {
	var in PlantingInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	p, err := h.svc.UpdatePlanting(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, p, err)
}

func (h *Handler) deletePlanting(w http.ResponseWriter, r *http.Request) {
	noContent(w, h.svc.DeletePlanting(r.Context(), chi.URLParam(r, "id")))
}

func (h *Handler) shiftTasks(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Days  int      `json:"days"`
		Kinds []string `json:"kinds"`
	}
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	tasks, err := h.svc.ShiftTasks(r.Context(), chi.URLParam(r, "id"), in.Days, in.Kinds)
	respond(w, http.StatusOK, map[string]any{"items": tasks}, err)
}

// ================================ tasks ================================

func (h *Handler) listTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	// to_end is the OVERDUE bound (window_to <= x), not the overlap bound: the
	// two are opposites, and without it a caller cannot ask for missed work at
	// all — every `from` filter excludes exactly the rows it would return.
	f := TaskFilter{
		From: q.Get("from"), To: q.Get("to"), ToEnd: q.Get("to_end"),
		Status: q.Get("status"), Kind: q.Get("kind"),
		BedID: q.Get("bed_id"), PlantingID: q.Get("planting_id"),
	}
	if v := q.Get("year"); v != "" {
		year, err := strconv.Atoi(v)
		if err != nil {
			httpx.WriteError(w, httpx.ErrUnprocessable("Rok musí být číslo."))
			return
		}
		f.Year = &year
	}
	page, err := h.svc.ListTasks(r.Context(), f, limit, q.Get("cursor"))
	respond(w, http.StatusOK, page, err)
}

func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) {
	var in TaskInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	t, err := h.svc.CreateTask(r.Context(), in)
	respond(w, http.StatusCreated, t, err)
}

func (h *Handler) getTask(w http.ResponseWriter, r *http.Request) {
	t, err := h.svc.GetTask(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, t, err)
}

func (h *Handler) updateTask(w http.ResponseWriter, r *http.Request) {
	var in TaskInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	t, err := h.svc.UpdateTask(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, t, err)
}

func (h *Handler) deleteTask(w http.ResponseWriter, r *http.Request) {
	noContent(w, h.svc.DeleteTask(r.Context(), chi.URLParam(r, "id")))
}

// completeTask is IDEMPOTENT (D131): a second call on an already-complete task
// is a 200 carrying the same task, not a 409. The dashboard's 2000 ms hold can
// fire twice on a bad connection, and the second send must not look like an
// error to somebody standing in a garden with one bar of signal.
func (h *Handler) completeTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Meta map[string]any `json:"meta"`
	}
	// An empty body is fine — the widget sends meta, a page may not.
	_ = decode(r, &in)
	t, err := h.svc.CompleteTask(r.Context(), chi.URLParam(r, "id"), in.Meta)
	respond(w, http.StatusOK, t, err)
}

func (h *Handler) reopenTask(w http.ResponseWriter, r *http.Request) {
	t, err := h.svc.ReopenTask(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, t, err)
}

// ============================ harvest, storage ============================

func (h *Handler) listHarvests(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	var year *int
	if v := q.Get("year"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			httpx.WriteError(w, httpx.ErrUnprocessable("Rok musí být číslo."))
			return
		}
		year = &n
	}
	page, err := h.svc.ListHarvests(r.Context(), q.Get("planting_id"), year, limit, q.Get("cursor"))
	respond(w, http.StatusOK, page, err)
}

func (h *Handler) createHarvest(w http.ResponseWriter, r *http.Request) {
	var in HarvestInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.CreateHarvest(r.Context(), in)
	respond(w, http.StatusCreated, out, err)
}

func (h *Handler) getHarvest(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Store().GetHarvest(r.Context(), nil, chi.URLParam(r, "id"))
	respond(w, http.StatusOK, out, mapNotFound(err))
}

func (h *Handler) updateHarvest(w http.ResponseWriter, r *http.Request) {
	var in HarvestInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.UpdateHarvest(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, out, err)
}

func (h *Handler) deleteHarvest(w http.ResponseWriter, r *http.Request) {
	noContent(w, h.svc.DeleteHarvest(r.Context(), chi.URLParam(r, "id")))
}

func (h *Handler) listStorage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := h.svc.ListStorage(r.Context(), q.Get("status"), limit, q.Get("cursor"))
	respond(w, http.StatusOK, page, err)
}

func (h *Handler) createStorage(w http.ResponseWriter, r *http.Request) {
	var in StorageInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.CreateStorage(r.Context(), in)
	respond(w, http.StatusCreated, out, err)
}

func (h *Handler) getStorage(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Store().GetStorage(r.Context(), nil, chi.URLParam(r, "id"))
	respond(w, http.StatusOK, out, mapNotFound(err))
}

func (h *Handler) updateStorage(w http.ResponseWriter, r *http.Request) {
	var in StorageInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.UpdateStorage(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, out, err)
}

func (h *Handler) deleteStorage(w http.ResponseWriter, r *http.Request) {
	noContent(w, h.svc.DeleteStorage(r.Context(), chi.URLParam(r, "id")))
}

// ========================= rules, settings, ref =========================

func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := h.svc.ListRules(r.Context(), q.Get("scope"), q.Get("include_disabled") != "false", limit, q.Get("cursor"))
	respond(w, http.StatusOK, page, err)
}

func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) {
	var in RuleInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.CreateRule(r.Context(), in)
	respond(w, http.StatusCreated, out, err)
}

func (h *Handler) getRule(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Store().GetRule(r.Context(), nil, chi.URLParam(r, "id"))
	respond(w, http.StatusOK, out, mapNotFound(err))
}

func (h *Handler) updateRule(w http.ResponseWriter, r *http.Request) {
	var in RuleInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.UpdateRule(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, out, err)
}

func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request) {
	noContent(w, h.svc.DeleteRule(r.Context(), chi.URLParam(r, "id")))
}

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.GetSettings(r.Context())
	respond(w, http.StatusOK, out, err)
}

func (h *Handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in SettingsInput
	if err := decode(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.UpdateSettings(r.Context(), in)
	respond(w, http.StatusOK, out, err)
}

func (h *Handler) weather(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := h.svc.Weather(r.Context(), q.Get("from"), q.Get("to"))
	respond(w, http.StatusOK, out, err)
}

// enums serves the SAME registry that generates the importer's validator and the
// LLM prompt schema, so the frontend, the model and the validator cannot
// disagree about what a legal value is.
func (h *Handler) enums(w http.ResponseWriter, _ *http.Request) {
	out := map[string][]EnumValue{}
	for _, def := range Enums() {
		out[def.Name] = def.Values
	}
	httpx.JSON(w, http.StatusOK, out)
}

// =========================== import and export ===========================

// importPlants reads the pasted JSON.
//
// dry_run DEFAULTS TRUE (D129): an import should never apply by accident, and
// the preview is what makes pasting machine output feel safe rather than
// reckless. Only an explicit `dry_run=false` writes anything.
func (h *Handler) importPlants(w http.ResponseWriter, r *http.Request) {
	dryRun := r.URL.Query().Get("dry_run") != "false"
	model := strings.TrimSpace(r.URL.Query().Get("model"))

	limit := h.svc.Options().ImportMaxBytes
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("Tělo požadavku se nepodařilo přečíst."))
		return
	}
	if int64(len(body)) > limit {
		httpx.WriteError(w, httpx.ErrTooLarge("Vložený JSON je příliš velký."))
		return
	}
	out, err := h.svc.ImportPlants(r.Context(), body, model, dryRun)
	respond(w, http.StatusOK, out, err)
}

// promptTemplate hands off the Czech prompt plus the schema generated from the
// importer's own validator. With ?plant_id= it embeds the crop's current values
// so the model fills gaps rather than overwriting what somebody already checked.
func (h *Handler) promptTemplate(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.PromptTemplate(r.Context(), r.URL.Query().Get("plant_id"))
	respond(w, http.StatusOK, out, err)
}

func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Export(r.Context())
	respond(w, http.StatusOK, out, err)
}

// ============================== plumbing ==============================

// yearParam parses the {year} path parameter (D128). A season's identity IS its
// year, so a non-numeric one is a 422 rather than a 404 — the caller sent
// something that could never name a season.
func yearParam(r *http.Request) (int, error) {
	raw := chi.URLParam(r, "year")
	year, err := strconv.Atoi(raw)
	if err != nil || year < 1900 || year > 2200 {
		return 0, httpx.ErrUnprocessable("Rok sezóny musí být číslo, například 2027.")
	}
	return year, nil
}

// decode is httpx.DecodeJSON (size cap, unknown fields rejected) with the
// module's Czech client message.
func decode(r *http.Request, v any) error {
	if err := httpx.DecodeJSON(r, v); err != nil {
		return httpx.ErrUnprocessable("Tělo požadavku není platný JSON nebo obsahuje neznámá pole.")
	}
	return nil
}

func respond(w http.ResponseWriter, status int, v any, err error) {
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, status, v)
}

func noContent(w http.ResponseWriter, err error) {
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
