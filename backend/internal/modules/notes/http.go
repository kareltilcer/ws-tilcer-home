package notes

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Handler serves the notes HTTP endpoints. Reads are open to any authenticated
// member; writes are gated with httpx.RequireWrite at mount time — EXCEPT the
// pin routes, which are ungated because a personal pin is allowed for any member
// (incl. reader); the household-pin write gate is enforced in the service (D35).
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers notes routes on the authenticated /api router. Static routes
// (tree, resolve, folders*) are registered before the parameterised /notes/{id}
// so they are never parsed as a note id.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/notes", h.list) // ?q= search, ?folder_id=, ?include_archived=
	r.With(httpx.RequireWrite).Post("/notes", h.createNote)

	r.Get("/notes/tree", h.tree)
	r.Get("/notes/resolve", h.resolve)

	// Inline-image content: a static /notes/images/{imageId} GET (registered before
	// /notes/{id} so it is never parsed as a note id). Session-gated read, open to any
	// member; the upload is note-scoped and write-gated below.
	r.Get("/notes/images/{imageId}", h.getImage)

	// Folders (static /notes/folders prefix, before /notes/{id}).
	r.With(httpx.RequireWrite).Post("/notes/folders", h.createFolder)
	r.Get("/notes/folders/{id}", h.getFolder)
	r.With(httpx.RequireWrite).Patch("/notes/folders/{id}", h.updateFolder)
	r.With(httpx.RequireWrite).Delete("/notes/folders/{id}", h.deleteFolder) // hard=true additionally requires admin (in handler)
	r.With(httpx.RequireWrite).Post("/notes/folders/{id}/move", h.moveFolder)

	// Publish: private → shared, one-way (v9, D182). Registered on the static
	// /notes/folders prefix above and on /notes/{id} below; there is deliberately
	// NO unpublish route, and its absence is a decision rather than a gap.
	r.With(httpx.RequireWrite).Post("/notes/folders/{id}/publish", h.publishFolder)

	// Notes by id.
	r.Get("/notes/{id}", h.getNote)
	r.With(httpx.RequireWrite).Patch("/notes/{id}", h.updateNote)
	r.With(httpx.RequireWrite).Delete("/notes/{id}", h.deleteNote) // hard=true additionally requires admin (in handler)
	r.With(httpx.RequireWrite).Post("/notes/{id}/move", h.moveNote)
	r.With(httpx.RequireWrite).Post("/notes/{id}/publish", h.publishNote)
	r.With(httpx.RequireWrite).Post("/notes/{id}/images", h.uploadImage) // paste/drop image upload

	// Pin/unpin: ungated at the router — the service allows a personal pin for any
	// member and requires editor/admin only for a household pin.
	r.Post("/notes/{id}/pin", h.pin)
	r.Delete("/notes/{id}/pin", h.unpin)
}

func boolParam(r *http.Request, name string) bool {
	v := r.URL.Query().Get(name)
	return v == "true" || v == "1"
}

// folderParam reads ?folder_id=. The literal "root" selects the notes directly at
// the requested scope's root; an empty value means "no filter" (every note in the
// scope).
//
// ⚠ `notes` had no such sentinel before v9, and its absence was a live bug (D203):
// the handler passed the raw parameter through and the store's predicate
// dereferenced a nil pointer to the empty string, so OMITTING folder_id already meant "root
// notes only" rather than the "all notes" the 0.10.1 contract advertised. It is
// modelled on the one `documents` has always had, so the two modules finally agree.
func folderParam(r *http.Request) *string {
	v := r.URL.Query().Get("folder_id")
	if v == "" {
		return nil
	}
	if v == "root" {
		empty := ""
		return &empty
	}
	return &v
}

// scopeParam resolves ?scope= against the CALLER's identity. Default `shared`,
// which is why every pre-v9 client keeps working untouched; `private` always means
// the caller's own root, and there is no value that names anybody else's.
func scopeParam(r *http.Request) (Scope, error) {
	return ParseScope(r.Context(), r.URL.Query().Get("scope"))
}

func isAdmin(r *http.Request) bool {
	if a, ok := reqctx.ActorFrom(r.Context()); ok {
		return reqctx.HasRole(a.Roles, "admin")
	}
	return false
}

// ---- Notes ----

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	sc, err := scopeParam(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	page, err := h.svc.List(r.Context(), r.URL.Query().Get("q"), folderParam(r), boolParam(r, "include_archived"), sc)
	respond(w, http.StatusOK, page, err)
}

func (h *Handler) createNote(w http.ResponseWriter, r *http.Request) {
	var in NoteCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	n, err := h.svc.CreateNote(r.Context(), in)
	respond(w, http.StatusCreated, n, err)
}

func (h *Handler) getNote(w http.ResponseWriter, r *http.Request) {
	n, err := h.svc.GetNoteDetail(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, n, err)
}

func (h *Handler) updateNote(w http.ResponseWriter, r *http.Request) {
	var in NoteUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	n, err := h.svc.UpdateNote(r.Context(), chi.URLParam(r, "id"), in, r.URL.Query().Get("via"))
	respond(w, http.StatusOK, n, err)
}

func (h *Handler) moveNote(w http.ResponseWriter, r *http.Request) {
	var in NoteMoveRequest
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	n, err := h.svc.MoveNote(r.Context(), chi.URLParam(r, "id"), in, r.URL.Query().Get("via"))
	respond(w, http.StatusOK, n, err)
}

func (h *Handler) deleteNote(w http.ResponseWriter, r *http.Request) {
	hard := boolParam(r, "hard")
	if hard && !isAdmin(r) { // destructive-op convention: hard purge is admin-only (PRD §3)
		httpx.WriteError(w, httpx.ErrForbidden("hard delete requires admin"))
		return
	}
	respondNoContent(w, h.svc.DeleteNote(r.Context(), chi.URLParam(r, "id"), hard))
}

func (h *Handler) pin(w http.ResponseWriter, r *http.Request) {
	var in PinRequest
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	st, err := h.svc.Pin(r.Context(), chi.URLParam(r, "id"), in.Scope, r.URL.Query().Get("via"))
	respond(w, http.StatusOK, st, err)
}

func (h *Handler) unpin(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.Unpin(r.Context(), chi.URLParam(r, "id"), r.URL.Query().Get("scope"), r.URL.Query().Get("via"))
	respond(w, http.StatusOK, st, err)
}

// ---- Folders ----

func (h *Handler) createFolder(w http.ResponseWriter, r *http.Request) {
	var in FolderCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	f, err := h.svc.CreateFolder(r.Context(), in)
	respond(w, http.StatusCreated, f, err)
}

func (h *Handler) getFolder(w http.ResponseWriter, r *http.Request) {
	f, err := h.svc.GetFolderDetail(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, f, err)
}

func (h *Handler) updateFolder(w http.ResponseWriter, r *http.Request) {
	var in FolderUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	f, err := h.svc.UpdateFolder(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, f, err)
}

func (h *Handler) deleteFolder(w http.ResponseWriter, r *http.Request) {
	hard := boolParam(r, "hard")
	if hard && !isAdmin(r) { // destructive-op convention: hard purge is admin-only (PRD §3)
		httpx.WriteError(w, httpx.ErrForbidden("hard delete requires admin"))
		return
	}
	respondNoContent(w, h.svc.DeleteFolder(r.Context(), chi.URLParam(r, "id"), boolParam(r, "cascade"), hard))
}

func (h *Handler) moveFolder(w http.ResponseWriter, r *http.Request) {
	var in FolderMoveRequest
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	f, err := h.svc.MoveFolder(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, f, err)
}

// ---- Tree / resolve ----

func (h *Handler) tree(w http.ResponseWriter, r *http.Request) {
	sc, err := scopeParam(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	t, err := h.svc.Tree(r.Context(), boolParam(r, "include_archived"), sc)
	respond(w, http.StatusOK, t, err)
}

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	sc, err := scopeParam(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	res, err := h.svc.Resolve(r.Context(), r.URL.Query().Get("path"), sc)
	respond(w, http.StatusOK, res, err)
}

// ---- Publish (v9) ----

func (h *Handler) publishNote(w http.ResponseWriter, r *http.Request) {
	in, err := decodePublish(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	n, err := h.svc.PublishNote(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, n, err)
}

func (h *Handler) publishFolder(w http.ResponseWriter, r *http.Request) {
	in, err := decodePublish(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	f, err := h.svc.PublishFolder(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, f, err)
}

// decodePublish accepts an absent or empty body: publishing to the shared ROOT is
// the common case, so `POST …/publish` with no body has to work.
//
// ⚠ AN EMPTY BODY IS NOT THE SAME TEST AS `ContentLength == 0`. A chunked request
// — curl -T, an HTTP/2 proxy, any client that streams without measuring first —
// carries ContentLength -1 whether or not it has content, so the length check
// alone sent an empty chunked publish into DecodeJSON, which returned io.EOF and
// turned the documented "no body publishes to the shared root" into a 422 saying
// "EOF". io.EOF from the decoder IS the empty body, so it is treated as one.
func decodePublish(r *http.Request) (PublishRequest, error) {
	var in PublishRequest
	if r.ContentLength == 0 {
		return in, nil
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		if errors.Is(err, io.EOF) {
			return PublishRequest{}, nil
		}
		return in, httpx.ErrUnprocessable(err.Error())
	}
	return in, nil
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
