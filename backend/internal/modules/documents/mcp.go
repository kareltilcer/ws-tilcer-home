package documents

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	"encoding/json"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The documents module's MCP provider (v11, PRD §V11-4 FR-M4, FR-M7).
//
// ⚠ IT IS notes' TWIN AND NOT ITS SHARED IMPLEMENTATION, which is v4's D40
// precedent and `notes/mirror.go`'s stated rule: Dokumenty MIRRORS Poznámky's
// folder model — one behaviour, two implementations. The private-root scoping,
// the 404-never-403 refusal and the `soukrome` URI segment are therefore written
// out here rather than imported, and the two files are meant to read the same.
//
// ⚠ WHAT IS ABSENT, BY NAME: every delete, every folder mutation, `publish`, and
// — the one worth naming — **upload**. A document's bytes reach Home through a
// multipart request against a 50 MB cap; posting them as base64 through JSON-RPC
// for a use case nobody asked for is what D295 refuses.
//
// ⚠ AND NOTHING EXTRACTS TEXT FROM A .docx. `home-gotenberg` converts Office
// files to PDF, never to text (D227 stands), so an Office document reaches the
// model as a rendered PDF or not at all. There is no new conversion path here.

type mcpProvider struct{ svc *Service }

// MCPProvider implements mcp.Source.
func (m *Module) MCPProvider() mcp.Provider { return &mcpProvider{svc: m.svc} }

func (p *mcpProvider) Module() string { return "documents" }

const (
	toolDocsTree   = "home_documents_tree"
	toolDocsUpdate = "home_documents_update"
	toolDocsPin    = "home_documents_pin"
)

// uriPrefix is the fixed head of this module's resource URIs — the SPA's own
// splat, with `soukrome` for the private root and no segment at all for the
// shared one. See notes/mcp.go for why there is no `sdilene`.
const uriPrefix = "home://documents/"

const privateSegment = "soukrome"

func (p *mcpProvider) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        toolDocsTree,
			Title:       "Documents tree",
			Description: "Returns the folder tree of the household's shared documents, or with scope \"private\" the caller's own; pass document for one document's metadata, and read its contents with the home://documents/ resource.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "scope": {"type": "string", "enum": ["shared", "private"], "description": "Which root to read. \"private\" is the CALLER's own; there is no way to name another member's."},
    "document": {"type": "string", "description": "Document id. Returns that document's metadata and its path."},
    "include_archived": {"type": "boolean"}
  },
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
		{
			Name:        toolDocsUpdate,
			Title:       "Rename a document",
			Description: "Changes a document's title, description or archived flag; the FILE itself is never replaced, because there is no upload tool.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string"},
    "title": {"type": "string", "minLength": 1},
    "description": {"type": "string"},
    "archived": {"type": "boolean"}
  },
  "required": ["id"],
  "additionalProperties": false
}`),
		},
		{
			Name:        toolDocsPin,
			Title:       "Pin a document",
			Description: "Pins or unpins a document on the dashboard, either for the whole household or just for the caller; a private document can only ever be pinned personally.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string"},
    "scope": {"type": "string", "enum": ["household", "personal"]},
    "pinned": {"type": "boolean"}
  },
  "required": ["id", "scope", "pinned"],
  "additionalProperties": false
}`),
		},
	}
}

func (p *mcpProvider) Call(ctx context.Context, name string, args json.RawMessage) (mcp.Result, error) {
	switch name {
	case toolDocsTree:
		return p.tree(ctx, args)
	case toolDocsUpdate:
		return p.update(ctx, args)
	case toolDocsPin:
		return p.pin(ctx, args)
	default:
		return mcp.Result{}, mcp.UnknownToolError(name)
	}
}

type treeArgs struct {
	Scope           string `json:"scope"`
	Document        string `json:"document"`
	IncludeArchived bool   `json:"include_archived"`
}

func (p *mcpProvider) tree(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in treeArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if in.Document != "" {
		return p.document(ctx, in.Document)
	}
	sc, err := ParseScope(ctx, in.Scope)
	if err != nil {
		return mcp.Result{}, err
	}
	t, err := p.svc.Tree(ctx, in.IncludeArchived, sc)
	if err != nil {
		return mcp.Result{}, err
	}
	root := "sdílené dokumenty"
	if sc.Private {
		root = "soukromé dokumenty"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Strom — %s:\n", root)
	for _, n := range t.Roots {
		renderDocFolder(&b, n, 0)
	}
	for _, d := range t.RootDocuments {
		fmt.Fprintf(&b, "\n• %s (id %s)", d.Title, d.ID)
	}
	return mcp.TextResult(b.String(), t)
}

func renderDocFolder(b *strings.Builder, node DocFolderNode, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(b, "\n%s📁 %s (id %s)", indent, node.Folder.Name, node.Folder.ID)
	for _, d := range node.Documents {
		fmt.Fprintf(b, "\n%s  • %s (id %s)", indent, d.Title, d.ID)
	}
	for _, sub := range node.Subfolders {
		renderDocFolder(b, sub, depth+1)
	}
}

func (p *mcpProvider) document(ctx context.Context, id string) (mcp.Result, error) {
	d, err := p.svc.GetDocumentDetail(ctx, id)
	if err != nil {
		if mcp.IsNotFound(err) {
			return mcp.NotFoundResult(), nil
		}
		return mcp.Result{}, err
	}
	if d == nil {
		return mcp.NotFoundResult(), nil
	}
	return mcp.TextResult(renderDocument(*d, p.uriFor(*d)), d)
}

func renderDocument(d DocumentDetail, uri string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (id %s)", d.Title, d.ID)
	if d.Visibility == visibilityPrivate {
		b.WriteString(" [soukromý]")
	}
	fmt.Fprintf(&b, "\n%s\n%s · %d B", uri, d.ContentType, d.ByteSize)
	if d.Description != nil && *d.Description != "" {
		fmt.Fprintf(&b, "\n\n%s", *d.Description)
	}
	return b.String()
}

func (p *mcpProvider) uriFor(d DocumentDetail) string {
	if d.Visibility == visibilityPrivate {
		return uriPrefix + privateSegment + "/" + d.SlugPath
	}
	return uriPrefix + d.SlugPath
}

type updateArgs struct {
	ID          string  `json:"id"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Archived    *bool   `json:"archived"`
}

func (p *mcpProvider) update(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in updateArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.ID) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Chybí id dokumentu.")
	}
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Název dokumentu nesmí být prázdný.")
	}
	if in.Title == nil && in.Description == nil && in.Archived == nil {
		return mcp.Result{}, httpx.ErrUnprocessable("Zadejte alespoň jednu změnu.")
	}
	d, err := p.svc.UpdateDocument(ctx, in.ID, DocumentUpdate{
		Title: in.Title, Description: in.Description, Archived: in.Archived,
	}, "")
	if err != nil {
		return mcp.Result{}, err
	}
	if d == nil {
		return mcp.NotFoundResult(), nil
	}
	return mcp.TextResult(fmt.Sprintf("Dokument „%s“ upraven.", d.Title), d)
}

type pinArgs struct {
	ID     string `json:"id"`
	Scope  string `json:"scope"`
	Pinned *bool  `json:"pinned"`
}

func (p *mcpProvider) pin(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in pinArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.ID) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Chybí id dokumentu.")
	}
	if in.Scope != "household" && in.Scope != "personal" {
		return mcp.Result{}, httpx.ErrUnprocessable("scope musí být household nebo personal.")
	}
	if in.Pinned == nil {
		return mcp.Result{}, httpx.ErrUnprocessable("Chybí pinned.")
	}
	var (
		state *PinState
		err   error
	)
	if *in.Pinned {
		state, err = p.svc.Pin(ctx, in.ID, in.Scope, "")
	} else {
		state, err = p.svc.Unpin(ctx, in.ID, in.Scope, "")
	}
	if err != nil {
		return mcp.Result{}, err
	}
	if state == nil {
		return mcp.NotFoundResult(), nil
	}
	verb := "odepnut"
	if *in.Pinned {
		verb = "připnut"
	}
	return mcp.TextResult("Dokument "+verb+".", state)
}

// Search reads BOTH roots the caller may see — the household's shared tree and
// their OWN private one — over the module's FTS5 index.
//
// ⚠ ONE BUDGET ACROSS THE TWO ROOTS (HANDOFF-13 §9.3), not one per root: a
// per-root budget hands out double what the host allocated, and the host's own
// merge is what decides which of the two survives.
//
// ⚠ D302: an actor-less ctx is an ERROR, never an empty slice. See notes/mcp.go
// for the full note — v9's build found two surfaces nobody had listed because a
// background job has no actor and the next person reached for the unscoped load.
func (p *mcpProvider) Search(ctx context.Context, q mcp.Query) ([]mcp.Hit, error) {
	actor, ok := reqctx.ActorFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("documents: search without an actor")
	}
	var hits []mcp.Hit
	for _, sc := range p.scopesFor(actor.UserID) {
		// ⚠ THE LIMIT ARGUMENT IS INERT ON THE SEARCH BRANCH, and passing q.Limit
		// into it read as though the budget were enforced at the query. It is not:
		// `Service.List` uses the module's own `searchLimit` whenever there is a
		// text query and looks at `limit` only when listing. The budget is spent
		// below, once, across BOTH roots — which is why notes' twin passes no limit
		// at all (its List has no such parameter) and why this one passes nothing
		// that could be mistaken for one.
		page, err := p.svc.List(ctx, q.Text, nil, false, 0, "", sc)
		if err != nil {
			return nil, err
		}
		for _, d := range page.Items {
			updated, _ := time.Parse(tsFormat, d.UpdatedAt)
			hits = append(hits, mcp.Hit{
				Kind:      "documents.document",
				ID:        d.ID,
				Title:     d.Title,
				Snippet:   docScopeLabel(d.Visibility) + " · " + d.ContentType,
				UpdatedAt: updated,
				ExactHit:  strings.EqualFold(strings.TrimSpace(d.Title), strings.TrimSpace(q.Text)),
			})
		}
	}
	return mcp.TrimHits(hits, q.Limit), nil
}

// scopesFor is the pair of roots one member may read: the household's, and their
// own. ⚠ There is no third, and no value anywhere names another member's.
func (p *mcpProvider) scopesFor(userID string) []Scope {
	if userID == "" {
		return []Scope{{}}
	}
	return []Scope{{}, {Private: true, OwnerID: userID}}
}

func docScopeLabel(visibility string) string {
	if visibility == visibilityPrivate {
		return "soukromý dokument"
	}
	return "sdílený dokument"
}

// Get answers home_get.
func (p *mcpProvider) Get(ctx context.Context, kind, id string) (mcp.Result, error) {
	if kind != "documents.document" {
		return mcp.NotFoundResult(), nil
	}
	return p.document(ctx, id)
}

// ---- Resources ----

func (p *mcpProvider) Resources() []mcp.ResourceTemplate {
	return []mcp.ResourceTemplate{{
		URITemplate: uriPrefix + "{path}",
		Name:        "document",
		Title:       "Dokument",
		Description: "Jeden dokument podle své cesty ve stromu; soukromé mají prefix soukrome/.",
	}}
}

// ListResources enumerates the documents the CALLER may read — leak row 11.
func (p *mcpProvider) ListResources(ctx context.Context, limit int) ([]mcp.Resource, error) {
	actor, ok := reqctx.ActorFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("documents: resource listing without an actor")
	}
	var out []mcp.Resource
	// ⚠ THE PRIVATE ROOT IS ENUMERATED FIRST, and one budget covers both. A big
	// shared tree must not be able to push a member's own documents off their own
	// listing.
	for _, sc := range privateFirst(p.scopesFor(actor.UserID)) {
		if len(out) >= limit {
			break
		}
		rows, err := p.svc.Store().SlugPathsForScope(ctx, sc, limit-len(out))
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			uri := uriPrefix + r.SlugPath
			if sc.Private {
				uri = uriPrefix + privateSegment + "/" + r.SlugPath
			}
			out = append(out, mcp.Resource{
				URI: uri, Name: r.Title, MIMEType: r.ContentType, Size: r.ByteSize,
			})
		}
	}
	return out, nil
}

func privateFirst(scopes []Scope) []Scope {
	out := make([]Scope, 0, len(scopes))
	for _, sc := range scopes {
		if sc.Private {
			out = append(out, sc)
		}
	}
	for _, sc := range scopes {
		if !sc.Private {
			out = append(out, sc)
		}
	}
	return out
}

// Read resolves one URI and serves the file, re-checking access from scratch.
//
// ⚠ LEAK ROW 10: A URI IS NOT A CAPABILITY (D308). A path member B obtained out
// of band resolves against B's OWN roots — `soukrome/...` names B's private tree
// and nobody else's — so A's path either names a document B also has or names
// nothing.
//
// ⚠ WHAT IS SERVED IS THE ORIGINAL, NEVER A PREVIEW. Text-like types come back
// as text; everything else comes back as bytes the model can look at. Nothing
// converts: `home-gotenberg` produces a PDF and never text, and reaching for the
// preview object here would hand back a rendering of a file rather than the file
// (D227).
func (p *mcpProvider) Read(ctx context.Context, uri string) (mcp.Content, error) {
	actor, ok := reqctx.ActorFrom(ctx)
	if !ok {
		return mcp.Content{}, fmt.Errorf("documents: resource read without an actor")
	}
	path, found := strings.CutPrefix(uri, uriPrefix)
	if !found || path == "" {
		return mcp.Content{}, mcp.ErrResourceNotFound
	}
	sc := Scope{}
	if rest, isPrivate := strings.CutPrefix(path, privateSegment+"/"); isPrivate {
		if actor.UserID == "" {
			return mcp.Content{}, mcp.ErrResourceNotFound
		}
		sc = Scope{Private: true, OwnerID: actor.UserID}
		path = rest
	}
	res, err := p.svc.Resolve(ctx, path, sc)
	if err != nil || res == nil || res.Type != "document" {
		return mcp.Content{}, mcp.ErrResourceNotFound
	}
	return p.readByID(ctx, uri, res.ID, actor.UserID)
}

// readByID loads the object through the viewer-scoped store read and streams at
// most maxResourceBytes of it.
//
// ⚠ THE OBJECT IS FETCHED WITH A RANGE, not read whole and then truncated. The
// per-file cap is 50 MB; reading one into memory to hand back 256 kB would put
// the whole file in the process's heap for the length of one tool call, on a
// service whose crash board already records what memory pressure does here.
func (p *mcpProvider) readByID(ctx context.Context, uri, id, viewerID string) (mcp.Content, error) {
	if p.svc.blob == nil {
		return mcp.Content{}, httpx.ErrNotImplemented("document storage is not configured")
	}
	sd, err := p.svc.Store().GetStoredDocument(ctx, p.svc.db, id, viewerID)
	if err != nil {
		return mcp.Content{}, err
	}
	// An archived document is gone as far as every reader is concerned, and its
	// permanent URL goes with it — the same rule the HTTP content route applies.
	if sd == nil || sd.Archived {
		return mcp.Content{}, mcp.ErrResourceNotFound
	}

	rng := blobstore.ByteRange{Offset: 0, Length: maxResourceBytes}
	body, _, err := p.svc.blob.Get(ctx, sd.StorageKey, &rng)
	if err != nil {
		return mcp.Content{}, err
	}
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(body, maxResourceBytes))
	if err != nil {
		return mcp.Content{}, err
	}

	content := mcp.Content{URI: uri, MIMEType: sd.ContentType}
	if isTextual(sd.ContentType) {
		content.Text = string(raw)
		return content, nil
	}
	content.Blob = raw
	return content, nil
}

// maxResourceBytes bounds what one resource read pulls out of object storage.
//
// ⚠ IT IS NOT THE HOST'S CAP AND MUST NOT BE MISTAKEN FOR IT. The host caps what
// reaches the client (HOME_MCP_MAX_RESULT_KB, D307); this bounds what reaches
// this PROCESS, which is a different resource with a different failure. It is
// deliberately larger, so the host's cap is the one that decides the answer and
// this one only stops a 50 MB file from becoming 50 MB of heap.
const maxResourceBytes = 8 << 20

// ---- store support ----

// DocSlugPathRow is one addressable document, for the MCP resource listing.
type DocSlugPathRow struct {
	ID          string
	Title       string
	SlugPath    string
	ContentType string
	ByteSize    int64
}

// SlugPathsForScope lists the documents in one root with their full slug paths.
//
// ⚠ THE PATH IS BUILT IN SQL WITH A RECURSIVE WALK. The pool is ONE connection,
// so resolving each document's breadcrumb separately would be one query per row
// with the household's browser queued behind all of them. Mirrors notes'.
func (s *Store) SlugPathsForScope(ctx context.Context, sc Scope, limit int) ([]DocSlugPathRow, error) {
	if limit <= 0 {
		return nil, nil
	}
	cond, args := scopeCond("d.", sc)
	fcond, fargs := scopeCond("f.", sc)
	query := `
		WITH RECURSIVE crumb(id, path) AS (
			SELECT f.id, f.slug FROM document_folders f WHERE f.parent_id IS NULL AND ` + fcond + `
			UNION ALL
			SELECT f.id, crumb.path || '/' || f.slug
			  FROM document_folders f JOIN crumb ON f.parent_id = crumb.id
		)
		SELECT d.id, d.title,
		       CASE WHEN d.folder_id IS NULL THEN d.slug ELSE crumb.path || '/' || d.slug END,
		       d.content_type, d.byte_size
		  FROM documents d
		  LEFT JOIN crumb ON crumb.id = d.folder_id
		 WHERE d.archived = 0 AND ` + cond + `
		 ORDER BY d.updated_at DESC
		 LIMIT ?`
	all := append(append([]any{}, fargs...), args...)
	all = append(all, limit)
	rows, err := s.db.QueryContext(ctx, query, all...)
	if err != nil {
		return nil, err
	}
	return appdb.Collect(rows, func(row appdb.Scanner) (DocSlugPathRow, error) {
		var r DocSlugPathRow
		var path sql.NullString
		if err := row.Scan(&r.ID, &r.Title, &path, &r.ContentType, &r.ByteSize); err != nil {
			return DocSlugPathRow{}, err
		}
		r.SlugPath = path.String
		return r, nil
	})
}
