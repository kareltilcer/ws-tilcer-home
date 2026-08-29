package chat

import (
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Handler serves the `chat` tag of openapi.yaml 0.13.0.
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
		// ⚠ THE BYTE ROUTES SIT IN THEIR OWN GROUP, OUTSIDE autoJoin, DELIBERATELY.
		// They are the only high-frequency routes in the module — one request per image
		// per thread render, twenty on a photo-heavy conversation — and the auto-join is
		// a write path that begins with an indexed read, against a pool capped at a
		// SINGLE connection (platform/db). Paying it per image serialises twenty extra
		// reads behind every other request in the application, to enrol somebody who by
		// definition reached the thread through a route that already enrolled them.
		//
		// ⚠ NOTHING ABOUT ACCESS CHANGES, and that is why this is safe. autoJoin only
		// ever ADDS a membership row for Všichni — it is not a check. The check is
		// AttachmentForViewer's join, which is the same predicate either way, and a
		// first-sight enrolment into the household room cannot make anybody a member of
		// the conversation an attachment belongs to.
		//
		// ⚠ A chi Group RATHER THAN A SECOND Route. Two Routes on overlapping prefixes
		// (`/chat` and `/chat/attachments`) do not compose: the first mounts a subtree
		// wildcard and swallows the second, which is exactly what happened — every
		// attachment path answered "no such endpoint" while the code read correctly.
		c.Group(func(raw chi.Router) {
			// ⚠ HEAD IS ROUTED EXPLICITLY BESIDE GET, and it is not decoration. chi does
			// not answer HEAD from a GET route, and the refusal has to be identical on
			// both: a HEAD-only oracle still answers "does this attachment exist" for a
			// conversation the caller may not open, which is the question D217 closes.
			raw.Get("/attachments/{id}/raw", h.attachmentRaw)
			raw.Head("/attachments/{id}/raw", h.attachmentRaw)
			raw.Get("/attachments/{id}/thumbnail", h.attachmentThumbnail)
			raw.Head("/attachments/{id}/thumbnail", h.attachmentThumbnail)
		})

		c.Group(func(g chi.Router) {
			g.Use(h.autoJoin)

			g.Get("/conversations", h.listConversations)
			g.Post("/conversations", h.createConversation)
			g.Get("/conversations/{id}", h.getConversation)
			g.Patch("/conversations/{id}", h.renameConversation)
			g.Delete("/conversations/{id}", h.deleteConversation)
			g.Post("/conversations/{id}/restore", h.restoreConversation)

			g.Get("/conversations/{id}/members", h.listMembers)
			g.Post("/conversations/{id}/members", h.addMember)
			g.Delete("/conversations/{id}/members/{user_id}", h.removeMember)
			g.Patch("/conversations/{id}/members/me", h.updateSelf)

			g.Get("/conversations/{id}/messages", h.thread)
			g.Post("/conversations/{id}/messages", h.sendMessage)
			g.Post("/conversations/{id}/read", h.advanceRead)

			g.Patch("/messages/{id}", h.editMessage)
			g.Delete("/messages/{id}", h.deleteMessage)
			// ⚠ PUT, NOT POST, AND NOT A TOGGLE ROUTE (v10.1, D265). The body names
			// the state the chip should be in when this returns, so the double-tap
			// gesture — which fires twice far more easily than a button does — is
			// idempotent rather than a coin flip. The emoji rides in the BODY rather
			// than in the path: ❤️ is two code points and a path segment would make
			// the route's identity depend on how a client percent-encoded U+FE0F.
			g.Put("/messages/{id}/reactions", h.setReaction)

			g.Delete("/attachments/{id}", h.removeAttachment)
			g.Post("/attachments/{id}/move", h.moveAttachment)

			g.Get("/search", h.search)
			g.Get("/storage", h.storage)
			g.Get("/cleanup", h.cleanup)
			g.Get("/directory", h.directory)
		})
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
		if actor := reqctx.ActorID(r.Context()); actor != "" {
			if err := h.svc.store.EnsureDefaultMembership(r.Context(), actor); err != nil {
				h.svc.logger.Warn("chat: auto-join Všichni", "err", err, "user", actor)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---- conversations ----

func (h *Handler) listConversations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := h.svc.ListConversations(r.Context(), q.Get("state"), q.Get("cursor"), limit)
	httpx.Respond(w, http.StatusOK, page, err)
}

func (h *Handler) createConversation(w http.ResponseWriter, r *http.Request) {
	var in ConversationCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	c, err := h.svc.CreateConversation(r.Context(), in)
	httpx.Respond(w, http.StatusCreated, c, err)
}

func (h *Handler) getConversation(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.GetConversation(r.Context(), chi.URLParam(r, "id"))
	httpx.Respond(w, http.StatusOK, c, err)
}

func (h *Handler) renameConversation(w http.ResponseWriter, r *http.Request) {
	var in ConversationUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	c, err := h.svc.RenameConversation(r.Context(), chi.URLParam(r, "id"), in)
	httpx.Respond(w, http.StatusOK, c, err)
}

// deleteConversation is the koš, or the purge.
//
// `?hard=true` exists so somebody deleting a heavy conversation TO FIX AN OVERRUN is
// never told to come back in seven days (D253).
//
// ⚠ THE FLAG IS PARSED, NOT STRING-COMPARED (v10 review). The spec types it as a
// boolean, so `?hard=1`, `?hard=True` and a bare `?hard` are all things a client
// legitimately sends — and `== "true"` answered every one of them with a SOFT
// delete plus a 204, telling somebody who deleted a room to free space to come back
// in seven days for bytes they believe are already gone. An unparseable value is
// the safe direction (false), which is what ParseBool's error branch leaves it as.
func (h *Handler) deleteConversation(w http.ResponseWriter, r *http.Request) {
	hard := queryBool(r, "hard")
	httpx.NoContent(w, h.svc.DeleteConversation(r.Context(), chi.URLParam(r, "id"), hard))
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
	httpx.Respond(w, http.StatusOK, list, err)
}

func (h *Handler) addMember(w http.ResponseWriter, r *http.Request) {
	var in ConversationMemberAdd
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	list, err := h.svc.AddMember(r.Context(), chi.URLParam(r, "id"), in)
	httpx.Respond(w, http.StatusOK, list, err)
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	httpx.NoContent(w, h.svc.RemoveMember(r.Context(),
		chi.URLParam(r, "id"), chi.URLParam(r, "user_id")))
}

func (h *Handler) updateSelf(w http.ResponseWriter, r *http.Request) {
	var in ConversationMemberSelfUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	c, err := h.svc.UpdateSelf(r.Context(), chi.URLParam(r, "id"), in)
	httpx.Respond(w, http.StatusOK, c, err)
}

// ---- messages ----

func (h *Handler) thread(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := h.svc.Thread(r.Context(), chi.URLParam(r, "id"),
		q.Get("direction"), q.Get("cursor"), limit)
	httpx.Respond(w, http.StatusOK, page, err)
}

// sendMessage takes JSON, or multipart/form-data when the message carries files.
//
// ⚠ ONE REQUEST EITHER WAY, NEVER AN UPLOAD-THEN-REFERENCE PAIR (D224). A two-step
// flow orphans an object every time the second step does not happen, and chat has
// no reconciliation pass to find one — `documents` has a mirror job that sweeps its
// prefix and chat deliberately has neither (D229).
//
// The branch is on the request's own Content-Type rather than on a query flag: it
// is the one signal that is already correct in every client, including curl.
func (h *Handler) sendMessage(w http.ResponseWriter, r *http.Request) {
	if isMultipart(r) {
		mr, err := r.MultipartReader()
		if err != nil {
			httpx.WriteError(w, httpx.ErrUnprocessable("Poškozený multipart požadavek."))
			return
		}
		m, err := h.svc.SendMessageMultipart(r.Context(), chi.URLParam(r, "id"), mr)
		httpx.Respond(w, http.StatusCreated, m, err)
		return
	}
	var in MessageCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	m, err := h.svc.SendMessage(r.Context(), chi.URLParam(r, "id"), in)
	httpx.Respond(w, http.StatusCreated, m, err)
}

func isMultipart(r *http.Request) bool {
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && ct == "multipart/form-data"
}

// ---- attachments ----

func (h *Handler) attachmentRaw(w http.ResponseWriter, r *http.Request) {
	h.serveAttachment(w, r, chi.URLParam(r, "id"), contentRaw)
}

func (h *Handler) attachmentThumbnail(w http.ResponseWriter, r *http.Request) {
	h.serveAttachment(w, r, chi.URLParam(r, "id"), contentThumbnail)
}

func (h *Handler) removeAttachment(w http.ResponseWriter, r *http.Request) {
	httpx.NoContent(w, h.svc.RemoveAttachment(r.Context(), chi.URLParam(r, "id")))
}

func (h *Handler) moveAttachment(w http.ResponseWriter, r *http.Request) {
	var in AttachmentMove
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	a, err := h.svc.MoveAttachment(r.Context(), chi.URLParam(r, "id"), in.FolderID)
	httpx.Respond(w, http.StatusOK, a, err)
}

// ---- storage and clean-up ----

func (h *Handler) storage(w http.ResponseWriter, r *http.Request) {
	s, err := h.svc.Storage(r.Context())
	httpx.Respond(w, http.StatusOK, s, err)
}

func (h *Handler) cleanup(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := h.svc.Cleanup(r.Context(), q.Get("conversation_id"), q.Get("sort"), q.Get("cursor"), limit)
	httpx.Respond(w, http.StatusOK, page, err)
}

func (h *Handler) advanceRead(w http.ResponseWriter, r *http.Request) {
	var in ReadUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	state, err := h.svc.AdvanceRead(r.Context(), chi.URLParam(r, "id"), in)
	httpx.Respond(w, http.StatusOK, state, err)
}

func (h *Handler) editMessage(w http.ResponseWriter, r *http.Request) {
	var in MessageUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	m, err := h.svc.EditMessage(r.Context(), chi.URLParam(r, "id"), in)
	httpx.Respond(w, http.StatusOK, m, err)
}

func (h *Handler) deleteMessage(w http.ResponseWriter, r *http.Request) {
	httpx.NoContent(w, h.svc.DeleteMessage(r.Context(), chi.URLParam(r, "id")))
}

// setReaction adds or removes the caller's reaction and answers with the whole
// re-rendered message — the shape editMessage uses, for the reason it uses it.
func (h *Handler) setReaction(w http.ResponseWriter, r *http.Request) {
	var in ReactionUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	m, err := h.svc.SetReaction(r.Context(), chi.URLParam(r, "id"), in)
	httpx.Respond(w, http.StatusOK, m, err)
}

// ---- search and directory ----

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := h.svc.Search(r.Context(), q.Get("q"), q.Get("conversation_id"), q.Get("cursor"), limit)
	httpx.Respond(w, http.StatusOK, page, err)
}

func (h *Handler) directory(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Directory(r.Context())
	httpx.Respond(w, http.StatusOK, d, err)
}

// queryBool reads a boolean query parameter the way the spec declares it: any
// spelling strconv.ParseBool accepts (`true`, `1`, `T`, `True`…), plus the bare
// `?flag` form, which ParseBool refuses because its value is the empty string.
// Anything else is false — for `hard` that is the reversible direction.
//
// ⚠ AN EMPTY VALUE IS NOT THE BARE FLAG (v10 review). url.Values cannot tell
// `?hard` from `?hard=`: both decode to [""], so reading the bare form as "the key
// is present with an empty value" made `?hard=` a PURGE — and `?hard=${flag}` is
// what any client emits when its variable is empty. That spelling destroyed a
// conversation and every message in it with no koš row and no seven-day window.
// The bare flag is therefore read from RawQuery, where the two forms differ.
func queryBool(r *http.Request, name string) bool {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return hasBareFlag(r.URL.RawQuery, name)
	}
	v, err := strconv.ParseBool(raw)
	return err == nil && v
}

// hasBareFlag reports whether the raw query carries `name` written with no `=` at
// all — the one spelling url.Values cannot distinguish from an empty value.
func hasBareFlag(rawQuery, name string) bool {
	for rawQuery != "" {
		var segment string
		segment, rawQuery, _ = strings.Cut(rawQuery, "&")
		if segment == name {
			return true
		}
	}
	return false
}
