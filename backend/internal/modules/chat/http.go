package chat

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
)

// Handler serves the `chat` tag of openapi.yaml 0.12.0.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers chat's routes on the authenticated /api router.
//
// ⚠ THERE IS NO RequireWrite ANYWHERE IN THIS FILE, AND THAT IS THE DECISION
// (D222). Chat is the first module in Home where a `reader` writes: they post,
// reply, edit and delete their own messages, create conversations, rename them, add
// and remove members, and move a room to the koš. The one thing a reader cannot do
// is clean up storage — `/chat/uklid`, PR 3 — which is the single recorded
// asymmetry in the module and the only place a role is consulted.
//
// The role gate every other module puts here is replaced by MEMBERSHIP, which is
// enforced in SQL rather than by middleware: see scope.go.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/chat", func(c chi.Router) {
		c.Use(h.autoJoin)

		c.Get("/conversations", h.listConversations)
		c.Post("/conversations", h.createConversation)
		c.Get("/conversations/{id}", h.getConversation)
		c.Patch("/conversations/{id}", h.renameConversation)
		c.Delete("/conversations/{id}", h.deleteConversation)
		c.Post("/conversations/{id}/restore", h.restoreConversation)

		c.Get("/conversations/{id}/members", h.listMembers)
		c.Post("/conversations/{id}/members", h.addMember)
		c.Delete("/conversations/{id}/members/{user_id}", h.removeMember)
		c.Patch("/conversations/{id}/members/me", h.updateSelf)

		c.Get("/conversations/{id}/messages", h.thread)
		c.Post("/conversations/{id}/messages", h.sendMessage)
		c.Post("/conversations/{id}/read", h.advanceRead)

		c.Patch("/messages/{id}", h.editMessage)
		c.Delete("/messages/{id}", h.deleteMessage)

		c.Get("/search", h.search)
		c.Get("/directory", h.directory)
	})
}

// autoJoin is "membership accrues at first sight" (FR-V10-2), in the one place
// every chat request passes through.
//
// ⚠ IT CANNOT HAPPEN AT BOOT. The directory is projected from `sessions` and a
// member who has never logged in does not exist yet — so the first request that
// resolves this caller to an actor is the first moment there is anybody to enrol.
//
// ⚠ AND IT IS SILENT ON FAILURE. A household room that could not be joined must not
// take the whole module down: the caller then sees the conversations they are
// already in, and the next request tries again. The alternative — 500 on every chat
// request because one INSERT lost a race — is worse than a delayed join.
//
// The store does a SELECT first and INSERTs only on a miss, so the steady-state cost
// is one indexed read per request rather than a write against a single-writer
// database.
func (h *Handler) autoJoin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if actor := actorID(r.Context()); actor != "" {
			if err := h.svc.store.EnsureDefaultMembership(r.Context(), actor); err != nil {
				h.svc.logger.Warn("chat: auto-join Všichni", "err", err, "user", actor)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---- conversations ----

func (h *Handler) listConversations(w http.ResponseWriter, r *http.Request) {
	page, err := h.svc.ListConversations(r.Context(), r.URL.Query().Get("state"))
	respond(w, http.StatusOK, page, err)
}

func (h *Handler) createConversation(w http.ResponseWriter, r *http.Request) {
	var in ConversationCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	c, err := h.svc.CreateConversation(r.Context(), in)
	respond(w, http.StatusCreated, c, err)
}

func (h *Handler) getConversation(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.GetConversation(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, c, err)
}

func (h *Handler) renameConversation(w http.ResponseWriter, r *http.Request) {
	var in ConversationUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	c, err := h.svc.RenameConversation(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, c, err)
}

// deleteConversation is the koš, or the purge.
//
// `?hard=true` exists so somebody deleting a heavy conversation TO FIX AN OVERRUN is
// never told to come back in seven days (D253).
func (h *Handler) deleteConversation(w http.ResponseWriter, r *http.Request) {
	hard := r.URL.Query().Get("hard") == "true"
	respondNoContent(w, h.svc.DeleteConversation(r.Context(), chi.URLParam(r, "id"), hard))
}

// restoreConversation brings a room back from the koš.
//
// ⚠ A nil conversation with no error means the caller restored a room they are not
// in — the D255 admin — so there is nothing they may be shown and the answer is 204.
// A member gets 200 and the room. The alternative, returning the conversation to
// whoever asked, would hand an admin in the response body exactly what the next GET
// correctly refuses them.
func (h *Handler) restoreConversation(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.RestoreConversation(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if c == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	httpx.JSON(w, http.StatusOK, c)
}

// ---- membership ----

func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.ListMembers(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, list, err)
}

func (h *Handler) addMember(w http.ResponseWriter, r *http.Request) {
	var in ConversationMemberAdd
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	list, err := h.svc.AddMember(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, list, err)
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	respondNoContent(w, h.svc.RemoveMember(r.Context(),
		chi.URLParam(r, "id"), chi.URLParam(r, "user_id")))
}

func (h *Handler) updateSelf(w http.ResponseWriter, r *http.Request) {
	var in ConversationMemberSelfUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	c, err := h.svc.UpdateSelf(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, c, err)
}

// ---- messages ----

func (h *Handler) thread(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := h.svc.Thread(r.Context(), chi.URLParam(r, "id"),
		q.Get("direction"), q.Get("cursor"), limit)
	respond(w, http.StatusOK, page, err)
}

// sendMessage takes JSON only in PR 2.
//
// ⚠ The multipart branch is PR 3's (D224: one request either way, never an
// upload-then-reference pair). A client that sends multipart here gets the ordinary
// decode refusal rather than a half-implemented upload.
func (h *Handler) sendMessage(w http.ResponseWriter, r *http.Request) {
	var in MessageCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	m, err := h.svc.SendMessage(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusCreated, m, err)
}

func (h *Handler) advanceRead(w http.ResponseWriter, r *http.Request) {
	var in ReadUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	state, err := h.svc.AdvanceRead(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, state, err)
}

func (h *Handler) editMessage(w http.ResponseWriter, r *http.Request) {
	var in MessageUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	m, err := h.svc.EditMessage(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, m, err)
}

func (h *Handler) deleteMessage(w http.ResponseWriter, r *http.Request) {
	respondNoContent(w, h.svc.DeleteMessage(r.Context(), chi.URLParam(r, "id")))
}

// ---- search and directory ----

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := h.svc.Search(r.Context(), q.Get("q"), q.Get("conversation_id"), q.Get("cursor"), limit)
	respond(w, http.StatusOK, page, err)
}

func (h *Handler) directory(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Directory(r.Context())
	respond(w, http.StatusOK, d, err)
}

// ---- rendering ----

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
