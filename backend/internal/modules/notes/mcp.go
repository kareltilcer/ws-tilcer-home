package notes

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcpctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The notes module's MCP provider (v11, PRD §V11-4 FR-M4, FR-M7, FR-M10).
//
// ⚠ THIS IS THE PROVIDER PR 1 EXISTS TO PROVE (D323). notes owns the private
// roots v9 built, so leak rows 7, 8, 10 and 11 are all testable here — in the
// FIRST pull request rather than the last, while the diff is still small enough
// to read.
//
// ⚠ THE ACCESS RULE IS v9's, UNCHANGED, AND THE TOKEN DOES NOT WIDEN IT. A member
// sees the shared tree plus their OWN private root, and nothing else — an admin
// included, 404 and never 403. What the token adds is that reading a private note
// is AUDITED (D297), because a surface deliberately opened onto private notes must
// be able to answer "what has the assistant seen?".
//
// ⚠ WHAT IS ABSENT, BY NAME: every delete, every folder mutation, `publish`, and
// every image verb. Publish is the sharpest of them — it is ONE-WAY (D182) and it
// moves an item OUT of a private root into the household's, which is exactly the
// kind of irreversible act nobody is watching a screen for.

type mcpProvider struct{ svc *Service }

// MCPProvider implements mcp.Source.
func (m *Module) MCPProvider() mcp.Provider { return &mcpProvider{svc: m.svc} }

func (p *mcpProvider) Module() string { return "notes" }

const (
	toolTree   = "home_notes_tree"
	toolCreate = "home_notes_create"
	toolUpdate = "home_notes_update"
	toolPin    = "home_notes_pin"
)

// uriPrefix is the fixed head of this module's resource URIs.
//
// ⚠ `{path}` IS THE SPA'S OWN SPLAT, VERBATIM, WITH NO SCOPE SEGMENT (FR-M7). A
// shared item has no prefix (home://notes/recepty/gulas) and a private one is
// prefixed `soukrome` (home://notes/soukrome/denik) — exactly as
// frontend/src/lib/scope.ts's parseScopedPath reads a URL. ⚠ THERE IS NO
// `sdilene` SEGMENT and inventing one would be the only place in the whole app
// where the shared root is named. The parse is unambiguous for the same reason
// the SPA's is: `soukrome` is a RESERVED slug at both shared roots (D185),
// backfilled by 06004, so a folder can never shadow the private tree.
//
// The payoff is that the tail of a URI is a route: paste `notes/soukrome/denik`
// after /poznamky/ and you are looking at the thing the model just read.
const uriPrefix = "home://notes/"

// privateSegment is the reserved first segment naming a member's own root.
const privateSegment = "soukrome"

func (p *mcpProvider) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        toolTree,
			Title:       "Notes tree",
			Description: "Returns the folder tree of the household's shared notes, or with scope \"private\" the caller's own private notes; pass note for one note's full Markdown body.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "scope": {"type": "string", "enum": ["shared", "private"], "description": "Which root to read. \"private\" is the CALLER's own; there is no way to name another member's."},
    "note": {"type": "string", "description": "Note id. Returns that note in full, including its Markdown body."},
    "include_archived": {"type": "boolean"}
  },
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
		{
			Name:        toolCreate,
			Title:       "Create a note",
			Description: "Writes a new Markdown note into the shared tree, or with scope \"private\" into the caller's own; a note created under a folder inherits that folder's scope, and disagreeing with it is refused.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "title": {"type": "string", "minLength": 1},
    "body_md": {"type": "string", "description": "Markdown body."},
    "folder_id": {"type": "string", "description": "Optional parent folder. Omit for the root of the chosen scope."},
    "scope": {"type": "string", "enum": ["shared", "private"]}
  },
  "required": ["title"],
  "additionalProperties": false
}`),
		},
		{
			Name:        toolUpdate,
			Title:       "Update a note",
			Description: "Replaces a note's title, Markdown body or archived flag; the body is replaced whole, so read it with home_notes_tree first if you mean to append rather than overwrite.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string"},
    "title": {"type": "string", "minLength": 1},
    "body_md": {"type": "string"},
    "archived": {"type": "boolean"}
  },
  "required": ["id"],
  "additionalProperties": false
}`),
		},
		{
			Name:        toolPin,
			Title:       "Pin a note",
			Description: "Pins or unpins a note on the dashboard, either for the whole household or just for the caller; a private note can only ever be pinned personally.",
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
	case toolTree:
		return p.tree(ctx, args)
	case toolCreate:
		return p.create(ctx, args)
	case toolUpdate:
		return p.update(ctx, args)
	case toolPin:
		return p.pin(ctx, args)
	default:
		return mcp.Result{}, mcp.UnknownToolError(name)
	}
}

type treeArgs struct {
	Scope           string `json:"scope"`
	Note            string `json:"note"`
	IncludeArchived bool   `json:"include_archived"`
}

func (p *mcpProvider) tree(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in treeArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if in.Note != "" {
		return p.readNote(ctx, in.Note)
	}
	// ParseScope resolves "private" against the CALLER's identity and there is no
	// value that names another member's root — the same discipline v5's audience
	// resolution follows for roles.
	sc, err := ParseScope(ctx, in.Scope)
	if err != nil {
		return mcp.Result{}, err
	}
	t, err := p.svc.Tree(ctx, in.IncludeArchived, sc)
	if err != nil {
		return mcp.Result{}, err
	}
	var b strings.Builder
	root := "sdílené poznámky"
	if sc.Private {
		root = "soukromé poznámky"
	}
	fmt.Fprintf(&b, "Strom — %s:\n", root)
	for _, n := range t.Roots {
		renderFolder(&b, n, 0)
	}
	for _, n := range t.RootNotes {
		fmt.Fprintf(&b, "\n• %s (id %s)", n.Title, n.ID)
	}
	return mcp.TextResult(b.String(), t)
}

func renderFolder(b *strings.Builder, node FolderNode, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(b, "\n%s📁 %s (id %s)", indent, node.Folder.Name, node.Folder.ID)
	for _, n := range node.Notes {
		fmt.Fprintf(b, "\n%s  • %s (id %s)", indent, n.Title, n.ID)
	}
	for _, sub := range node.Subfolders {
		renderFolder(b, sub, depth+1)
	}
}

// readNote returns one note in full and audits the read when it is private and
// the reader is a token.
func (p *mcpProvider) readNote(ctx context.Context, id string) (mcp.Result, error) {
	note, err := p.svc.GetNoteDetail(ctx, id)
	if err != nil {
		// ⚠ A FOREIGN PRIVATE NOTE ARRIVES HERE AS httpx.ErrNotFound — NOT as nil
		// and not as a 403. The store enforces the audience in SQL (viewerCond), so
		// the row never reaches Go and the service cannot tell it from an id that
		// was never issued. The error is RETURNED rather than translated here: the
		// host's one mapper collapses 403 and 404 onto the same refusal, which is
		// what makes the two answers byte-identical (leak row 9).
		return mcp.Result{}, err
	}
	if note == nil {
		return mcp.NotFoundResult(), nil
	}
	if err := p.svc.RecordTokenNoteRead(ctx, *note); err != nil {
		return mcp.Result{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (id %s)", note.Title, note.ID)
	if note.Visibility == visibilityPrivate {
		b.WriteString(" [soukromá]")
	}
	fmt.Fprintf(&b, "\n%s%s\n", uriPrefix, p.uriPath(note.Visibility, note.SlugPath))
	if note.BodyMD != nil && *note.BodyMD != "" {
		fmt.Fprintf(&b, "\n%s", *note.BodyMD)
	}
	return mcp.TextResult(b.String(), note)
}

// uriPath renders the SPA splat for one item: the slug path, prefixed `soukrome`
// when private and unprefixed when shared.
func (p *mcpProvider) uriPath(visibility, slugPath string) string {
	if visibility == visibilityPrivate {
		return privateSegment + "/" + slugPath
	}
	return slugPath
}

type createArgs struct {
	Title    string  `json:"title"`
	BodyMD   string  `json:"body_md"`
	FolderID *string `json:"folder_id"`
	Scope    string  `json:"scope"`
}

func (p *mcpProvider) create(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in createArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.Title) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Název poznámky nesmí být prázdný.")
	}
	if in.Scope != "" && in.Scope != scopeParamShared && in.Scope != scopeParamPrivate {
		return mcp.Result{}, httpx.ErrUnprocessable("scope musí být shared nebo private.")
	}
	note, err := p.svc.CreateNote(ctx, NoteCreate{
		Title:    in.Title,
		BodyMD:   in.BodyMD,
		FolderID: in.FolderID,
		Scope:    in.Scope,
	})
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.TextResult(fmt.Sprintf("Poznámka „%s“ vytvořena (id %s).", note.Title, note.ID), note)
}

type updateArgs struct {
	ID       string  `json:"id"`
	Title    *string `json:"title"`
	BodyMD   *string `json:"body_md"`
	Archived *bool   `json:"archived"`
}

func (p *mcpProvider) update(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in updateArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.ID) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Chybí id poznámky.")
	}
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Název poznámky nesmí být prázdný.")
	}
	if in.Title == nil && in.BodyMD == nil && in.Archived == nil {
		return mcp.Result{}, httpx.ErrUnprocessable("Zadejte alespoň jednu změnu.")
	}
	note, err := p.svc.UpdateNote(ctx, in.ID, NoteUpdate{Title: in.Title, BodyMD: in.BodyMD, Archived: in.Archived}, "")
	if err != nil {
		return mcp.Result{}, err
	}
	if note == nil {
		return mcp.NotFoundResult(), nil
	}
	return mcp.TextResult(fmt.Sprintf("Poznámka „%s“ upravena.", note.Title), note)
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
		return mcp.Result{}, httpx.ErrUnprocessable("Chybí id poznámky.")
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
	verb := "odepnuta"
	if *in.Pinned {
		verb = "připnuta"
	}
	return mcp.TextResult("Poznámka "+verb+".", state)
}

// Search reads BOTH roots the caller may see: the household's shared tree and
// their OWN private one.
//
// ⚠ LEAK ROW 8, AND IT IS TWO FAILURES IN ONE PLACE.
//
// The first is scope: `Service.List` takes a Scope and queries exactly that root,
// so member B searching for a term that occurs only in member A's private note
// gets nothing — B's private root is a different root, and there is no value that
// names A's. Merging the two roots into one unscoped query is precisely what D177
// rejected.
//
// The second is the ACTOR, and it is the one v9's build actually got bitten by:
// its preview worker and image GC had no actor, a viewer-scoped read returned
// nothing, and the next person reached for the unscoped load. An MCP tool call has
// an actor only AFTER the token resolves, so an ERROR here — never an empty slice,
// never AnyScope — is what makes that impossible to reintroduce silently. An empty
// slice is what the bug looks like on the day somebody "fixes" the nil.
func (p *mcpProvider) Search(ctx context.Context, q mcp.Query) ([]mcp.Hit, error) {
	actor, ok := reqctx.ActorFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("notes: search without an actor")
	}
	scopes := []Scope{{}}
	if actor.UserID != "" {
		scopes = append(scopes, Scope{Private: true, OwnerID: actor.UserID})
	}
	var hits []mcp.Hit
	for _, sc := range scopes {
		page, err := p.svc.List(ctx, q.Text, nil, false, sc)
		if err != nil {
			return nil, err
		}
		for i, n := range page.Items {
			if i >= q.Limit {
				break
			}
			updated, _ := time.Parse(tsFormat, n.UpdatedAt)
			hits = append(hits, mcp.Hit{
				Kind:      "notes.note",
				ID:        n.ID,
				Title:     n.Title,
				Snippet:   scopeLabel(n.Visibility),
				UpdatedAt: updated,
				ExactHit:  strings.EqualFold(strings.TrimSpace(n.Title), strings.TrimSpace(q.Text)),
			})
		}
	}
	// ⚠ THE BUDGET IS PER MODULE, NOT PER ROOT (D301). notes is the one provider
	// that reads TWO roots — the household's shared tree and the caller's own
	// private one — so taking q.Limit from each would spend double what the host
	// handed out and report a count the host's own budget contradicts. The trim
	// uses the SAME order the host merges by (exact title match, then recency), so
	// what survives here is what would have survived there: a private note is not
	// dropped for being private, only for being older.
	if q.Limit > 0 && len(hits) > q.Limit {
		sort.SliceStable(hits, func(i, j int) bool {
			if hits[i].ExactHit != hits[j].ExactHit {
				return hits[i].ExactHit
			}
			return hits[i].UpdatedAt.After(hits[j].UpdatedAt)
		})
		hits = hits[:q.Limit]
	}
	return hits, nil
}

func scopeLabel(visibility string) string {
	if visibility == visibilityPrivate {
		return "soukromá poznámka"
	}
	return "sdílená poznámka"
}

// Get answers home_get.
func (p *mcpProvider) Get(ctx context.Context, kind, id string) (mcp.Result, error) {
	if kind != "notes.note" {
		return mcp.NotFoundResult(), nil
	}
	return p.readNote(ctx, id)
}

// ---- Resources ----

func (p *mcpProvider) Resources() []mcp.ResourceTemplate {
	return []mcp.ResourceTemplate{{
		URITemplate: uriPrefix + "{path}",
		Name:        "note",
		Title:       "Poznámka",
		Description: "Jedna poznámka podle své cesty ve stromu; soukromé mají prefix soukrome/.",
		MIMEType:    "text/markdown",
	}}
}

// ListResources enumerates the notes the CALLER may read.
//
// ⚠ LEAK ROW 11 (D308). This is the *existence* leak v9 spent a whole version
// closing, arriving in a new surface: a listing is an answer even when every read
// is refused. Both roots the caller may see are enumerated and no other, and
// `soukrome` is the only thing distinguishing them in a URI.
//
// ⚠ THE BUDGET IS THE HOST'S AND IT IS PER MODULE, NOT PER ROOT — the same
// arithmetic Search gets right (D301/D308), and notes is the one provider that
// reads two roots. Taking `limit` from EACH returned up to twice what the host
// asked for: a listing 200 rows over a cap whose only job is to bound a query
// against the ONE connection, and one that made the host's "this page was cut"
// warning fire on a page nothing had cut.
//
// ⚠ AND THE CALLER'S OWN PRIVATE ROOT IS ENUMERATED FIRST, which is what the
// shared budget's ORDER decides. When a household outgrows the cap something has
// to fall off the end, and it must not be the half only its owner can see: a
// shared note dropped here is still reachable through home_search and
// home_notes_tree, and the host logs a Warn naming the module, while a private
// tree that vanished because the shared one got big would look to the model
// exactly like a member who keeps no private notes.
func (p *mcpProvider) ListResources(ctx context.Context, limit int) ([]mcp.Resource, error) {
	actor, ok := reqctx.ActorFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("notes: resource listing without an actor")
	}
	var scopes []Scope
	if actor.UserID != "" {
		scopes = append(scopes, Scope{Private: true, OwnerID: actor.UserID})
	}
	scopes = append(scopes, Scope{})
	var out []mcp.Resource
	for _, sc := range scopes {
		if len(out) >= limit {
			break
		}
		rows, err := p.svc.Store().SlugPathsForScope(ctx, sc, limit-len(out))
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, mcp.Resource{
				URI:      uriPrefix + p.uriPath(sc.Visibility(), r.SlugPath),
				Name:     r.Title,
				MIMEType: "text/markdown",
			})
		}
	}
	return out, nil
}

// Read resolves one URI, re-checking access FROM SCRATCH.
//
// ⚠ LEAK ROW 10 (D308): A URI IS NOT A CAPABILITY. One member B obtained out of
// band — pasted from A's context, guessed, recovered from a log — must return
// exactly what a URI that does not exist returns. That falls out of Resolve
// walking from the NAMED ROOT and never leaving it: `soukrome/...` resolves
// against the CALLER's own private root, so A's path either names a note B also
// has or names nothing at all.
func (p *mcpProvider) Read(ctx context.Context, uri string) (mcp.Content, error) {
	actor, ok := reqctx.ActorFrom(ctx)
	if !ok {
		return mcp.Content{}, fmt.Errorf("notes: resource read without an actor")
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
	if err != nil || res == nil || res.Type != "note" {
		// Every failure — a bad path, a folder, another member's private note —
		// collapses to the same answer.
		return mcp.Content{}, mcp.ErrResourceNotFound
	}
	note, err := p.svc.GetNoteDetail(ctx, res.ID)
	if err != nil {
		// The service answers a note that is gone with httpx.ErrNotFound, which the
		// host reads through mcp.IsNotFound — so a row deleted between Resolve and
		// here is the ordinary refusal and not a line on the crash board, while a
		// store failure still is.
		return mcp.Content{}, err
	}
	if note == nil {
		return mcp.Content{}, mcp.ErrResourceNotFound
	}
	if err := p.svc.RecordTokenNoteRead(ctx, *note); err != nil {
		return mcp.Content{}, err
	}
	body := ""
	if note.BodyMD != nil {
		body = *note.BodyMD
	}
	return mcp.Content{URI: uri, MIMEType: "text/markdown", Text: body}, nil
}

// ---- The audited read (D297) ----

// RecordTokenNoteRead writes `notes.private.read` when a TOKEN reads a private
// note.
//
// ⚠ IT FIRES ONLY WHEN THE READER IS A TOKEN. A browser read writes nothing, as
// it always has — the Log is not a record of everything anybody has ever looked
// at, and making it one would be a far larger change than v11.
//
// ⚠ IT RECORDS THE CONTAINER AND NEVER THE CONTENT: the note's id and the token's,
// and no body, no snippet, no title beyond what the summary already needs. Without
// it, "what has the assistant seen?" has no answer at all — unacceptable for a
// surface deliberately opened onto private notes — and with more than it, the Log
// becomes a second copy of the note, which is exactly what D231 exists to prevent
// for chat.
//
// ⚠ THE EVENT IS ITSELF SCOPED PRIVATE, so the household sees "Soukromá poznámka —
// podrobnosti skryty" and the owner sees which note. A read event that leaked the
// title would disclose more than the read it records.
func (s *Service) RecordTokenNoteRead(ctx context.Context, note NoteDetail) error {
	tokenID, viaToken := mcpctx.TokenFrom(ctx)
	if !viaToken || note.Visibility != visibilityPrivate {
		return nil
	}
	sc := Scope{Private: true, OwnerID: deref(note.OwnerID)}
	return appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		return s.record(ctx, tx, "private.read", "note", note.ID,
			"Asistent otevřel soukromou poznámku „"+note.Title+"“",
			nil, map[string]any{"token_id": tokenID}, sc)
	})
}

// ---- store support ----

// SlugPathRow is one addressable note, for the MCP resource listing.
type SlugPathRow struct {
	ID       string
	Title    string
	SlugPath string
}

// SlugPathsForScope lists the notes in one root with their full slug paths.
//
// ⚠ THE PATH IS BUILT IN SQL WITH A RECURSIVE WALK RATHER THAN N QUERIES. The
// pool is ONE connection, so a listing that resolved each note's breadcrumb
// separately would be one query per note with the household's browser queued
// behind all of them.
func (s *Store) SlugPathsForScope(ctx context.Context, sc Scope, limit int) ([]SlugPathRow, error) {
	if limit <= 0 {
		return nil, nil
	}
	cond, args := scopeCond("n.", sc)
	fcond, fargs := scopeCond("f.", sc)
	query := `
		WITH RECURSIVE crumb(id, path) AS (
			SELECT f.id, f.slug FROM folders f WHERE f.parent_id IS NULL AND ` + fcond + `
			UNION ALL
			SELECT f.id, crumb.path || '/' || f.slug
			  FROM folders f JOIN crumb ON f.parent_id = crumb.id
		)
		SELECT n.id, n.title,
		       CASE WHEN n.folder_id IS NULL THEN n.slug ELSE crumb.path || '/' || n.slug END
		  FROM notes n
		  LEFT JOIN crumb ON crumb.id = n.folder_id
		 WHERE n.archived = 0 AND ` + cond + `
		 ORDER BY n.updated_at DESC
		 LIMIT ?`
	all := append(append([]any{}, fargs...), args...)
	all = append(all, limit)
	rows, err := s.db.QueryContext(ctx, query, all...)
	if err != nil {
		return nil, err
	}
	return appdb.Collect(rows, func(row appdb.Scanner) (SlugPathRow, error) {
		var r SlugPathRow
		var path sql.NullString
		if err := row.Scan(&r.ID, &r.Title, &path); err != nil {
			return SlugPathRow{}, err
		}
		r.SlugPath = path.String
		return r, nil
	})
}

// ---- helpers ----
