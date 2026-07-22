package todo

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/httpx"
)

// Handler serves the todo HTTP endpoints. Reads are open to any authenticated
// user; writes are gated with httpx.RequireWrite at mount time.
type Handler struct{ svc *Service }

// NewHandler returns a todo HTTP handler over svc.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers all todo routes on the (authenticated) /api router.
func (h *Handler) Mount(r chi.Router) {
	// Boards
	r.Get("/boards", h.listBoards)
	r.With(httpx.RequireWrite).Post("/boards", h.createBoard)
	r.Get("/boards/{id}", h.getBoard)
	r.With(httpx.RequireWrite).Patch("/boards/{id}", h.updateBoard)
	r.With(httpx.RequireWrite).Delete("/boards/{id}", h.deleteBoard)
	r.Get("/boards/{id}/tree", h.tree)

	// Columns
	r.Get("/boards/{id}/columns", h.listColumns)
	r.With(httpx.RequireWrite).Post("/boards/{id}/columns", h.createColumn)
	r.With(httpx.RequireWrite).Patch("/columns/{id}", h.updateColumn)
	r.With(httpx.RequireWrite).Delete("/columns/{id}", h.deleteColumn)
	r.With(httpx.RequireWrite).Post("/columns/{id}/move", h.moveColumn)

	// Cards
	r.With(httpx.RequireWrite).Post("/columns/{id}/cards", h.createCard)
	r.Get("/cards/{id}", h.getCard)
	r.With(httpx.RequireWrite).Patch("/cards/{id}", h.updateCard)
	r.With(httpx.RequireWrite).Delete("/cards/{id}", h.deleteCard)
	r.With(httpx.RequireWrite).Post("/cards/{id}/move", h.moveCard)

	// Card links
	r.Get("/cards/{id}/links", h.listLinks)
	r.With(httpx.RequireWrite).Post("/cards/{id}/links", h.createLink)
	r.With(httpx.RequireWrite).Delete("/links/{id}", h.deleteLink)

	// Checklist
	r.Get("/cards/{id}/checklist", h.listChecklist)
	r.With(httpx.RequireWrite).Post("/cards/{id}/checklist", h.createChecklist)
	r.With(httpx.RequireWrite).Patch("/checklist/{id}", h.updateChecklist)
	r.With(httpx.RequireWrite).Delete("/checklist/{id}", h.deleteChecklist)

	// Labels
	r.Get("/boards/{id}/labels", h.listLabels)
	r.With(httpx.RequireWrite).Post("/boards/{id}/labels", h.createLabel)
	r.With(httpx.RequireWrite).Patch("/labels/{id}", h.updateLabel)
	r.With(httpx.RequireWrite).Delete("/labels/{id}", h.deleteLabel)
	r.With(httpx.RequireWrite).Post("/cards/{id}/labels/{labelId}", h.attachLabel)
	r.With(httpx.RequireWrite).Delete("/cards/{id}/labels/{labelId}", h.detachLabel)
}

func boolParam(r *http.Request, name string) bool {
	v := r.URL.Query().Get(name)
	return v == "true" || v == "1"
}

// ---- Boards ----

func (h *Handler) listBoards(w http.ResponseWriter, r *http.Request) {
	boards, err := h.svc.ListBoards(r.Context())
	respond(w, http.StatusOK, boards, err)
}

func (h *Handler) createBoard(w http.ResponseWriter, r *http.Request) {
	var in BoardCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	b, err := h.svc.CreateBoard(r.Context(), in)
	respond(w, http.StatusCreated, b, err)
}

func (h *Handler) getBoard(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.GetBoard(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, b, err)
}

func (h *Handler) updateBoard(w http.ResponseWriter, r *http.Request) {
	var in BoardUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	b, err := h.svc.UpdateBoard(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, b, err)
}

func (h *Handler) deleteBoard(w http.ResponseWriter, r *http.Request) {
	err := h.svc.DeleteBoard(r.Context(), chi.URLParam(r, "id"), boolParam(r, "hard"))
	respondNoContent(w, err)
}

func (h *Handler) tree(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	t, err := h.svc.Tree(r.Context(), chi.URLParam(r, "id"), q["label"], q.Get("q"), boolParam(r, "include_archived"))
	respond(w, http.StatusOK, t, err)
}

// ---- Columns ----

func (h *Handler) listColumns(w http.ResponseWriter, r *http.Request) {
	cols, err := h.svc.ListColumns(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, cols, err)
}

func (h *Handler) createColumn(w http.ResponseWriter, r *http.Request) {
	var in ColumnCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	c, err := h.svc.CreateColumn(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusCreated, c, err)
}

func (h *Handler) updateColumn(w http.ResponseWriter, r *http.Request) {
	var in ColumnUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	c, err := h.svc.UpdateColumn(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, c, err)
}

func (h *Handler) deleteColumn(w http.ResponseWriter, r *http.Request) {
	err := h.svc.DeleteColumn(r.Context(), chi.URLParam(r, "id"), boolParam(r, "cascade"))
	respondNoContent(w, err)
}

func (h *Handler) moveColumn(w http.ResponseWriter, r *http.Request) {
	var in MoveRequest
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	c, err := h.svc.MoveColumn(r.Context(), chi.URLParam(r, "id"), in.Position)
	respond(w, http.StatusOK, c, err)
}

// ---- Cards ----

func (h *Handler) createCard(w http.ResponseWriter, r *http.Request) {
	var in CardCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	c, err := h.svc.CreateCard(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusCreated, c, err)
}

func (h *Handler) getCard(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.GetCardDetail(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, c, err)
}

func (h *Handler) updateCard(w http.ResponseWriter, r *http.Request) {
	var in CardUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	c, err := h.svc.UpdateCard(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, c, err)
}

func (h *Handler) deleteCard(w http.ResponseWriter, r *http.Request) {
	err := h.svc.DeleteCard(r.Context(), chi.URLParam(r, "id"), boolParam(r, "hard"))
	respondNoContent(w, err)
}

func (h *Handler) moveCard(w http.ResponseWriter, r *http.Request) {
	var in CardMoveRequest
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	c, err := h.svc.MoveCard(r.Context(), chi.URLParam(r, "id"), in, r.URL.Query().Get("via"))
	respond(w, http.StatusOK, c, err)
}

// ---- Card links ----

func (h *Handler) listLinks(w http.ResponseWriter, r *http.Request) {
	links, err := h.svc.ListCardLinks(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, links, err)
}

func (h *Handler) createLink(w http.ResponseWriter, r *http.Request) {
	var in CardLinkCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	l, err := h.svc.CreateCardLink(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusCreated, l, err)
}

func (h *Handler) deleteLink(w http.ResponseWriter, r *http.Request) {
	respondNoContent(w, h.svc.DeleteCardLink(r.Context(), chi.URLParam(r, "id")))
}

// ---- Checklist ----

func (h *Handler) listChecklist(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListChecklist(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, items, err)
}

func (h *Handler) createChecklist(w http.ResponseWriter, r *http.Request) {
	var in ChecklistItemCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	it, err := h.svc.CreateChecklistItem(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusCreated, it, err)
}

func (h *Handler) updateChecklist(w http.ResponseWriter, r *http.Request) {
	var in ChecklistItemUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	it, err := h.svc.UpdateChecklistItem(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, it, err)
}

func (h *Handler) deleteChecklist(w http.ResponseWriter, r *http.Request) {
	respondNoContent(w, h.svc.DeleteChecklistItem(r.Context(), chi.URLParam(r, "id")))
}

// ---- Labels ----

func (h *Handler) listLabels(w http.ResponseWriter, r *http.Request) {
	labels, err := h.svc.ListLabels(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, labels, err)
}

func (h *Handler) createLabel(w http.ResponseWriter, r *http.Request) {
	var in LabelCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	l, err := h.svc.CreateLabel(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusCreated, l, err)
}

func (h *Handler) updateLabel(w http.ResponseWriter, r *http.Request) {
	var in LabelCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	l, err := h.svc.UpdateLabel(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, l, err)
}

func (h *Handler) deleteLabel(w http.ResponseWriter, r *http.Request) {
	respondNoContent(w, h.svc.DeleteLabel(r.Context(), chi.URLParam(r, "id")))
}

func (h *Handler) attachLabel(w http.ResponseWriter, r *http.Request) {
	respondNoContent(w, h.svc.AttachLabel(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "labelId")))
}

func (h *Handler) detachLabel(w http.ResponseWriter, r *http.Request) {
	respondNoContent(w, h.svc.DetachLabel(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "labelId")))
}

// ---- response helpers ----

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
