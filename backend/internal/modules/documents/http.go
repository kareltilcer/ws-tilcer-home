package documents

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Handler serves the documents HTTP endpoints. Reads (list, tree, resolve, and the
// four content streams) are open to any authenticated member; writes are gated with
// httpx.RequireWrite at mount time — EXCEPT the pin routes, which are ungated
// because a PERSONAL pin is allowed for any member including a reader. The
// household-pin write gate lives in the service (D47).
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers documents routes on the authenticated /api router. Static routes
// (tree, resolve, folders*) are registered before the parameterised /documents/{id}
// so they are never parsed as a document id.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/documents", h.list) // ?q= search, ?folder_id=, ?include_archived=, ?limit=, ?cursor=
	r.With(httpx.RequireWrite).Post("/documents", h.upload)

	r.Get("/documents/tree", h.tree)
	r.Get("/documents/resolve", h.resolve)

	// Folders (static /documents/folders prefix, before /documents/{id}).
	r.With(httpx.RequireWrite).Post("/documents/folders", h.createFolder)
	r.Get("/documents/folders/{id}", h.getFolder)
	r.With(httpx.RequireWrite).Patch("/documents/folders/{id}", h.updateFolder)
	r.With(httpx.RequireWrite).Delete("/documents/folders/{id}", h.deleteFolder) // hard=true additionally requires admin (in handler)
	r.With(httpx.RequireWrite).Post("/documents/folders/{id}/move", h.moveFolder)

	// Documents by id. PATCH is metadata only — there is no route that replaces the
	// bytes, by design (D41).
	r.Get("/documents/{id}", h.getDocument)
	r.With(httpx.RequireWrite).Patch("/documents/{id}", h.updateDocument)
	r.With(httpx.RequireWrite).Delete("/documents/{id}", h.deleteDocument) // hard=true additionally requires admin (in handler)
	r.With(httpx.RequireWrite).Post("/documents/{id}/move", h.moveDocument)

	// Content: the permanent, household-only streams (D42/D33). Reads, so no CSRF —
	// they are used as <img>/<iframe>/anchor targets and authorise from the session
	// cookie (same-origin, so the cookie rides along).
	//
	// Each is registered for GET *and* HEAD: serveContent writes the full header set
	// and then skips the body on HEAD, so a client can check size/type/validator
	// without pulling 50 MB. chi has no HEAD→GET fallback — without the explicit pair
	// a HEAD gets 405 and that branch is dead code.
	content := func(pattern string, fn http.HandlerFunc) {
		r.Get(pattern, fn)
		r.Head(pattern, fn)
	}
	content("/documents/{id}/raw", h.raw)
	content("/documents/{id}/download", h.download)
	content("/documents/{id}/preview", h.preview)
	content("/documents/{id}/thumbnail", h.thumbnail)

	// Pin/unpin: ungated at the router — the service allows a personal pin for any
	// member and requires editor/admin only for a household pin.
	r.Post("/documents/{id}/pin", h.pin)
	r.Delete("/documents/{id}/pin", h.unpin)
}

func boolParam(r *http.Request, name string) bool {
	v := r.URL.Query().Get(name)
	return v == "true" || v == "1"
}

// folderParam reads ?folder_id=. The literal "root" selects unfiled documents
// (folder_id IS NULL) — an empty value means "no filter", so the sentinel is the
// only way to ask for the root scope explicitly (openapi documents this).
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

func isAdmin(r *http.Request) bool {
	if a, ok := reqctx.ActorFrom(r.Context()); ok {
		return reqctx.HasRole(a.Roles, "admin")
	}
	return false
}

// ---- Documents ----

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := h.svc.List(r.Context(), q.Get("q"), folderParam(r), boolParam(r, "include_archived"), limit, q.Get("cursor"))
	respond(w, http.StatusOK, page, err)
}

// upload streams a multipart/form-data upload (FR-DOC1).
//
// The parts are read IN ORDER through a MultipartReader rather than buffered by
// ParseMultipartForm, so a 50 MB file never lands in memory (or in Go's own temp
// spill). That makes part order significant: the text fields must precede the file
// part, which is what the frontend sends. A field arriving after the file is
// ignored rather than silently applied late — noted in the response contract.
func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable("expected a multipart/form-data upload"))
		return
	}

	var meta UploadMeta
	for parts := 0; ; parts++ {
		// Each field is capped at metaFieldLimit, but without a count the loop would read
		// parts until EOF — an authenticated client could hold the request open forever
		// streaming tiny fields that never become the file part.
		if parts >= maxUploadParts {
			httpx.WriteError(w, httpx.ErrUnprocessable("too many multipart fields before the \"file\" part"))
			return
		}
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			// Ran out of parts without ever seeing the file.
			httpx.WriteError(w, httpx.ErrUnprocessable("the multipart body has no \"file\" part"))
			return
		}
		if err != nil {
			httpx.WriteError(w, httpx.ErrUnprocessable("malformed multipart body: "+err.Error()))
			return
		}

		if part.FormName() != "file" {
			if err := readMetaField(part, &meta); err != nil {
				_ = part.Close()
				httpx.WriteError(w, err)
				return
			}
			_ = part.Close()
			continue
		}

		// The file part: hand the stream straight to the service, which enforces the
		// size cap, checksums, sniffs the type, and writes the object before the row.
		filename := part.FileName()
		if filename == "" {
			filename = "soubor"
		}
		doc, err := h.svc.Upload(r.Context(), UploadInput{
			Filename:    filename,
			File:        part,
			FolderID:    meta.FolderID,
			Title:       meta.Title,
			Description: meta.Description,
		})
		_ = part.Close()
		respond(w, http.StatusCreated, doc, err)
		return
	}
}

// metaFieldLimit bounds a single text field. Titles and descriptions are short;
// this stops a malicious client from streaming gigabytes into a "title".
const metaFieldLimit = 8 << 10

// maxUploadParts bounds how many parts the reader will consume while looking for the
// file. The frontend sends at most four (folder_id, title, description, file); the
// ceiling is generous enough that no honest client meets it.
const maxUploadParts = 16

func readMetaField(part *multipart.Part, meta *UploadMeta) error {
	b, err := io.ReadAll(io.LimitReader(part, metaFieldLimit+1))
	if err != nil {
		return httpx.ErrUnprocessable("malformed multipart field: " + err.Error())
	}
	if len(b) > metaFieldLimit {
		return httpx.ErrUnprocessable("multipart field " + part.FormName() + " is too long")
	}
	v := strings.TrimSpace(string(b))
	switch part.FormName() {
	case "folder_id":
		if v != "" {
			meta.FolderID = &v
		}
	case "title":
		meta.Title = v
	case "description":
		meta.Description = v
	}
	return nil
}

func (h *Handler) getDocument(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.GetDocumentDetail(r.Context(), chi.URLParam(r, "id"))
	respond(w, http.StatusOK, d, err)
}

func (h *Handler) updateDocument(w http.ResponseWriter, r *http.Request) {
	var in DocumentUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	d, err := h.svc.UpdateDocument(r.Context(), chi.URLParam(r, "id"), in, r.URL.Query().Get("via"))
	respond(w, http.StatusOK, d, err)
}

func (h *Handler) moveDocument(w http.ResponseWriter, r *http.Request) {
	var in DocumentMoveRequest
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	d, err := h.svc.MoveDocument(r.Context(), chi.URLParam(r, "id"), in, r.URL.Query().Get("via"))
	respond(w, http.StatusOK, d, err)
}

func (h *Handler) deleteDocument(w http.ResponseWriter, r *http.Request) {
	hard := boolParam(r, "hard")
	// Destructive-op convention: a hard purge is admin-only — and here it also
	// destroys the file bytes, which no soft delete does.
	if hard && !isAdmin(r) {
		httpx.WriteError(w, httpx.ErrForbidden("hard delete requires admin"))
		return
	}
	respondNoContent(w, h.svc.DeleteDocument(r.Context(), chi.URLParam(r, "id"), hard))
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
	var in DocFolderCreate
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
	var in DocFolderUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	f, err := h.svc.UpdateFolder(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, f, err)
}

func (h *Handler) deleteFolder(w http.ResponseWriter, r *http.Request) {
	hard := boolParam(r, "hard")
	// A hard folder delete purges every descendant document's bytes too (D50).
	if hard && !isAdmin(r) {
		httpx.WriteError(w, httpx.ErrForbidden("hard delete requires admin"))
		return
	}
	respondNoContent(w, h.svc.DeleteFolder(r.Context(), chi.URLParam(r, "id"), boolParam(r, "cascade"), hard))
}

func (h *Handler) moveFolder(w http.ResponseWriter, r *http.Request) {
	var in DocFolderMoveRequest
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	f, err := h.svc.MoveFolder(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, http.StatusOK, f, err)
}

// ---- Tree / resolve ----

func (h *Handler) tree(w http.ResponseWriter, r *http.Request) {
	t, err := h.svc.Tree(r.Context(), boolParam(r, "include_archived"))
	respond(w, http.StatusOK, t, err)
}

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Resolve(r.Context(), r.URL.Query().Get("path"))
	respond(w, http.StatusOK, res, err)
}

// ---- Content endpoints (FR-DOC8) ----

func (h *Handler) raw(w http.ResponseWriter, r *http.Request) {
	h.serveContent(w, r, contentRaw)
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	h.serveContent(w, r, contentDownload)
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	h.serveContent(w, r, contentPreview)
}

func (h *Handler) thumbnail(w http.ResponseWriter, r *http.Request) {
	h.serveContent(w, r, contentThumbnail)
}

// rfc5987 encodes a filename for Content-Disposition. Czech filenames are not
// Latin-1, so the header carries both a sanitised ASCII fallback and the UTF-8
// filename* form that modern browsers prefer.
func rfc5987(filename string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '\\' || r > 126 {
			return '_'
		}
		return r
	}, filename)
	return `filename="` + ascii + `"; filename*=UTF-8''` + urlEscapeFilename(filename)
}

// urlEscapeFilename percent-encodes a filename for the filename* parameter.
func urlEscapeFilename(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		// attr-char per RFC 5987: alphanumerics and a small punctuation set.
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0x0f])
	}
	return b.String()
}

// mimeTypeHeader formats a content type for a response header, keeping any charset.
func mimeTypeHeader(ct string) string {
	if ct == "" {
		return "application/octet-stream"
	}
	if _, _, err := mime.ParseMediaType(ct); err != nil {
		return "application/octet-stream"
	}
	return ct
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
