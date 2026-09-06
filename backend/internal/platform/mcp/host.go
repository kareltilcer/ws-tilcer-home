package mcp

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lists"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Config is the MCP host's slice of the environment (PRD §V11-9). All six
// variables are defaulted and none is secret.
//
// ⚠ ALL SIX REFUSE AT BOTH ENDS RATHER THAN CLAMPING, and that follows v10's
// precedent rather than v9's for one reason: the three older session windows
// clamp with a loud CONFIGURATION CORRECTED warning because aborting Load would
// crash-loop the container on the deploy that lands them. Nothing is upgrading
// into these, so a nonsensical value can only be a fresh mistake, and refusing it
// is correct. The validation lives in platform/config.
type Config struct {
	Enabled          bool
	RatePerMin       int
	CallTimeout      time.Duration
	MaxResultBytes   int
	MaxTokensPerUser int
	SearchLimit      int
	// AllowedOrigins is HOME_ALLOWED_ORIGINS — the SAME list CSRF uses (D281).
	AllowedOrigins []string
	// ServerVersion is what `initialize` reports.
	//
	// ⚠ THERE IS NO BUILD-TIME VERSION IN THE GO BINARY. `VITE_APP_COMMIT` is a
	// Vite arg with no backend twin, and the only backend release identifier is
	// the free-form STATUS_RELEASE, which defaults to empty. The composition root
	// passes that when set and the string "dev" otherwise — do NOT invent a build
	// stamp for this one field.
	ServerVersion string
	// Timezone is HOME_TIMEZONE, which `home_whoami` reports and `home_today`
	// resolves "today" in. Never UTC.
	Timezone     *time.Location
	TimezoneName string
}

// DisplayNames resolves the household directory for the admin token listing.
//
// ⚠ IT IS A FUNCTION THE COMPOSITION ROOT WIRES, not a query this package
// writes. Home has no user table — the directory is PROJECTED from `sessions`,
// freshest row per member, and platform/push already owns that projection with a
// long comment about why it is freshest and not newest. A second projection here
// is how two screens come to disagree about a member's name.
type DisplayNames func(ctx context.Context) (map[string]string, error)

// Deps is everything the host needs, assembled once at composition.
type Deps struct {
	Registry *Registry
	Auth     auth.MCPAuth
	Tokens   *auth.MCPTokenStore
	DB       *sql.DB
	Sink     audit.Sink
	Activity *audit.ActivityReader
	Metrics  *metrics.Registry
	Lists    *lists.Registry
	Names    DisplayNames
	Config   Config
	Logger   *slog.Logger
	Now      func() time.Time
}

// Host is the MCP front door: one POST handler, a JSON-RPC dispatcher, and the
// four REST routes that manage the credential it authenticates with.
type Host struct {
	deps Deps
	sems *semaphores
	// calls limits authenticated traffic per token id; anon limits the methods
	// that need no bearer, per client IP; fails limits bearers that do not
	// RESOLVE, also per client IP — see serve for why the third is not the second.
	calls *rateLimiter
	anon  *rateLimiter
	fails *rateLimiter
}

// New builds the host.
func New(d Deps) *Host {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	window := time.Minute
	return &Host{
		deps:  d,
		sems:  newSemaphores(),
		calls: newRateLimiter(d.Config.RatePerMin, window, d.Now),
		anon:  newRateLimiter(d.Config.RatePerMin, window, d.Now),
		fails: newRateLimiter(d.Config.RatePerMin, window, d.Now),
	}
}

func (h *Host) now() time.Time { return h.deps.Now() }

// ---- Tool assembly ----

// visibleTools returns the tools this caller is offered, in a stable order: the
// seven cross-cutting ones first, then each module's in registration order.
//
// ⚠ THE `modules` ALLOWLIST NARROWS AND NEVER WIDENS (D288). It is a convenience
// — *this token is for the garden* — and NOT an access control: every tool it
// does offer still resolves the caller's roles and the three access axes
// underneath. The core seven are never narrowed BY THE ALLOWLIST, because a token
// that cannot say who it is is a token nobody can debug.
//
// ⚠ ROLES ARE A DIFFERENT QUESTION FROM THE ALLOWLIST AND ARE APPLIED HERE TOO.
// An admin-only tool is not offered to a member who is not an admin — the model
// should not propose a verb its caller will be refused, and the refusal in
// toolsCall is the second half rather than the only one.
func (h *Host) visibleTools(s *callSession) []Tool {
	admin := reqctx.HasRole(s.actor.Roles, "admin")
	var out []Tool
	for _, t := range h.coreTools() {
		if t.AdminOnly && !admin {
			continue
		}
		out = append(out, t)
	}
	allowed := moduleSet(s.principal.Token.Modules)
	for _, p := range h.deps.Registry.Providers() {
		if allowed != nil && !allowed[p.Module()] {
			continue
		}
		for _, t := range p.Tools() {
			if t.AdminOnly && !admin {
				continue
			}
			out = append(out, t)
		}
	}
	return out
}

// moduleSet turns a token's allowlist into a lookup, or nil for "everything the
// owner can see" — the empty-array meaning, written once so no caller has to
// remember that `[]` is not "nothing".
func moduleSet(mods []string) map[string]bool {
	if len(mods) == 0 {
		return nil
	}
	set := make(map[string]bool, len(mods))
	for _, m := range mods {
		set[m] = true
	}
	return set
}

// searchModules returns the providers a search may fan out to, honouring the
// allowlist.
func (h *Host) searchModules(tok auth.MCPToken) []Provider {
	allowed := moduleSet(tok.Modules)
	var out []Provider
	for _, p := range h.deps.Registry.Providers() {
		if allowed != nil && !allowed[p.Module()] {
			continue
		}
		out = append(out, p)
	}
	return out
}

// ---- Dispatch ----

// callSession is one authenticated request's state, threaded through the
// dispatcher so no handler has to re-derive it.
type callSession struct {
	principal auth.MCPPrincipal
	actor     reqctx.Actor
}

// needsBearer reports whether a method requires a resolved token.
//
// ⚠ EVERYTHING BUT `initialize` DOES (D312). An unauthenticated `initialize`
// returns server capabilities and NOTHING ELSE — no tool names — so `tools/list`
// cannot be used to learn the household's shape. That is leak row 13, and it is
// why the tool list is assembled after the token is known rather than held as a
// field.
//
// ⚠ `notifications/initialized` IS THE ONE EXEMPTION D312 DOES NOT NAME, and it
// is not an answer: it is the fire-and-forget notification every client sends
// immediately after `initialize`, it returns no body, and it reads nothing. A
// client that probed with an unauthenticated `initialize` would otherwise be 401'd
// on the very next frame of its own handshake. `ping` is deliberately NOT on this
// list — it is a real method with a real response, and D312 says so.
func needsBearer(method string) bool {
	switch method {
	case "initialize", methodInitialized:
		return false
	default:
		return true
	}
}

// methodInitialized is the client's post-handshake notification.
const methodInitialized = "notifications/initialized"

// dispatch routes one call. It returns the JSON-RPC result value, or an error —
// a *ProtocolError for anything the model must not retry blindly.
func (h *Host) dispatch(ctx context.Context, s *callSession, method string, params json.RawMessage) (any, error) {
	switch method {
	case "initialize":
		return h.initialize(params)
	case "ping":
		return map[string]any{}, nil
	case methodInitialized:
		// ⚠ ACCEPTED AND IGNORED, RATHER THAN FALLING THROUGH TO method-not-found.
		// It is a notification, so nothing is written back either way — but serve
		// logs a failed notification at Warn, and without this case every healthy
		// client would leave an error-shaped line in the log on every session, which
		// is how a log stops being read.
		return nil, nil
	case "tools/list":
		return map[string]any{"tools": toolsWire(h.visibleTools(s))}, nil
	case "tools/call":
		return h.toolsCall(ctx, s, params)
	case "resources/list":
		return h.resourcesList(ctx, s, params)
	case "resources/templates/list":
		return h.resourceTemplates(s), nil
	case "resources/read":
		return h.resourcesRead(ctx, s, params)
	case "prompts/list":
		return map[string]any{"prompts": promptsWire(h.prompts())}, nil
	case "prompts/get":
		return h.promptsGet(params)
	default:
		return nil, &ProtocolError{Code: CodeMethodNotFound, Message: "unknown method " + method}
	}
}

// initializeParams is the handshake's only interesting field.
type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

// initialize answers the handshake.
//
// ⚠ AN UNSUPPORTED VERSION IS REFUSED, NAMING THE ONE HOME SPEAKS. The MCP
// specification suggests a server may instead answer with its own version and let
// the client decide, but a client that cannot tell whether it is too old or too
// new retries forever — so home says which, once, in the error. This is the one
// place protocol drift surfaces (D278); the constant is in jsonrpc.go, alone,
// with the checklist for changing it.
func (h *Host) initialize(params json.RawMessage) (any, error) {
	var in initializeParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, &ProtocolError{Code: CodeInvalidParams, Message: "malformed initialize params"}
		}
	}
	if in.ProtocolVersion != "" && in.ProtocolVersion != protocolVersion {
		return nil, &ProtocolError{
			Code:    CodeInvalidParams,
			Message: "unsupported protocol version " + in.ProtocolVersion + "; home speaks " + protocolVersion,
		}
	}
	return map[string]any{
		"protocolVersion": protocolVersion,
		// ⚠ CAPABILITIES AND NOTHING ELSE. No tool names, no module names, no
		// counts — an unauthenticated caller learns that this is an MCP server and
		// not what the household keeps in it.
		"capabilities": map[string]any{
			"tools":     map[string]any{},
			"resources": map[string]any{},
			"prompts":   map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "home",
			"version": h.deps.Config.ServerVersion,
		},
	}, nil
}

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// toolsCall runs one tool.
func (h *Host) toolsCall(ctx context.Context, s *callSession, params json.RawMessage) (any, error) {
	var in toolsCallParams
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, &ProtocolError{Code: CodeInvalidParams, Message: "malformed tools/call params"}
	}
	if in.Name == "" {
		return nil, &ProtocolError{Code: CodeInvalidParams, Message: "tools/call requires a name"}
	}

	tool, ok := h.lookupTool(s.principal.Token, in.Name)
	if !ok {
		// ⚠ AN ABSENT VERB IS AN UNKNOWN TOOL, NOT A POLITE REFUSAL (D295). This is
		// also the answer for a tool narrowed away by the token's module allowlist:
		// a token "for the garden" should not be told which other rooms exist.
		return nil, UnknownToolError(in.Name)
	}
	if !tool.ReadOnly && !reqctx.CanWrite(ctx) {
		// ⚠ TWO GATES, AS OVER HTTP. There is no httpx.RequireWrite on this path, so
		// the host takes the HTTP-layer half here and the module's service takes the
		// service-layer half again underneath — which is precisely the case
		// reqctx.CanWrite's doc comment was written for.
		return resultWire(Result{Text: "Nemáte oprávnění k zápisu.", IsError: true}, 0), nil
	}
	if tool.AdminOnly && !reqctx.IsAdmin(ctx) {
		// ⚠ THE ADMIN GATE IS SEPARATE FROM THE WRITE GATE BECAUSE READ-ONLY IS NOT
		// THE SAME QUESTION AS UNGATED. home_activity is ReadOnly and its HTTP twin
		// is behind httpx.RequireAdmin (D5), so a reader's token must be refused it
		// exactly as the reader's browser is — otherwise the token out-ranks its
		// owner, which is leak row 6 wearing a fourth hat.
		//
		// ⚠ REFUSED IN WORDS, NOT COLLAPSED ONTO NotFoundResult. Leak row 9's
		// 403-reads-as-404 rule protects OWNERSHIP and MEMBERSHIP surfaces, where
		// whether the thing exists is the secret. That a household has a Log is not
		// a secret, and the HTTP twin answers 403 out loud.
		return resultWire(Result{Text: "Tento nástroj je jen pro správce.", IsError: true}, 0), nil
	}

	args := in.Arguments
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}

	res, err := h.callTool(ctx, s, in.Name, args)
	if err != nil {
		mapped, perr := toResult(err)
		if perr != nil {
			return nil, perr
		}
		// ⚠ AN INTERNAL FAILURE IS LOGGED WITH ITS REQUEST ID AND RETURNED WITHOUT
		// DETAIL. The detail belongs in the Log, where a person can read it; on the
		// wire it would be a description of the database's shape handed to whoever
		// holds the token.
		if mapped.Text == internalText {
			h.logTool(ctx, "mcp tool failed", in.Name, err)
		}
		res = mapped
	}
	return resultWire(res, h.deps.Config.MaxResultBytes), nil
}

// lookupTool finds a tool the given token is offered.
func (h *Host) lookupTool(tok auth.MCPToken, name string) (Tool, bool) {
	for _, t := range h.coreTools() {
		if t.Name == name {
			return t, true
		}
	}
	owner, ok := h.deps.Registry.ToolOwner(name)
	if !ok {
		return Tool{}, false
	}
	if allowed := moduleSet(tok.Modules); allowed != nil && !allowed[owner] {
		return Tool{}, false
	}
	p, _ := h.deps.Registry.Provider(owner)
	for _, t := range p.Tools() {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// callTool dispatches to the core handlers or to the owning module's provider.
func (h *Host) callTool(ctx context.Context, s *callSession, name string, args json.RawMessage) (Result, error) {
	if fn, ok := h.coreHandler(name); ok {
		return fn(ctx, s, args)
	}
	p, ok := h.deps.Registry.ProviderForTool(name)
	if !ok {
		return Result{}, UnknownToolError(name)
	}
	return p.Call(ctx, name, args)
}

func (h *Host) logTool(ctx context.Context, msg, tool string, err error) {
	id := ""
	if info, ok := reqctx.RequestFrom(ctx); ok {
		id = info.RequestID
	}
	// ⚠ THE TOOL NAME AND THE REQUEST ID, NEVER THE ARGUMENTS. Every attr of an
	// Error record is forwarded verbatim to the crash board (platform/statusreport),
	// which is read by an admin session — a different lock from the one on a
	// member's private note. Arguments are user content.
	h.deps.Logger.Error(msg, "tool", tool, "request_id", id, "err", err)
}

// ---- Resources ----

func (h *Host) resourceTemplates(s *callSession) any {
	allowed := moduleSet(s.principal.Token.Modules)
	out := []map[string]any{}
	for _, p := range h.deps.Registry.Providers() {
		if allowed != nil && !allowed[p.Module()] {
			continue
		}
		for _, t := range p.Resources() {
			out = append(out, map[string]any{
				"uriTemplate": t.URITemplate,
				"name":        t.Name,
				"title":       t.Title,
				"description": t.Description,
				"mimeType":    t.MIMEType,
			})
		}
	}
	return map[string]any{"resourceTemplates": out}
}

// resourcesList enumerates what the CALLER may read.
//
// ⚠ LEAK ROW 11 (D308). A listing is an answer even when every read is refused:
// a private root that appears here for anyone but its owner, or a conversation
// for anyone who is not in it, discloses exactly the existence v9 and v10 each
// spent a version hiding. The scoping is the provider's, because only the module
// knows its own access axis — the host's job is to fan out and to cap.
func (h *Host) resourcesList(ctx context.Context, s *callSession, _ json.RawMessage) (any, error) {
	allowed := moduleSet(s.principal.Token.Modules)
	out := []map[string]any{}
	for _, p := range h.deps.Registry.Providers() {
		if allowed != nil && !allowed[p.Module()] {
			continue
		}
		items, err := p.ListResources(ctx, h.deps.Config.SearchLimit)
		if err != nil {
			h.logTool(ctx, "mcp resources/list failed", p.Module(), err)
			continue // one module's failure must not blank the whole listing
		}
		for _, it := range items {
			row := map[string]any{"uri": it.URI, "name": it.Name}
			if it.MIMEType != "" {
				row["mimeType"] = it.MIMEType
			}
			if it.Size > 0 {
				row["size"] = it.Size
			}
			out = append(out, row)
		}
	}
	return map[string]any{"resources": out}, nil
}

type resourcesReadParams struct {
	URI string `json:"uri"`
}

// resourcesRead resolves one URI.
//
// ⚠ EVERY READ RE-CHECKS ACCESS FROM SCRATCH, BECAUSE A URI IS NOT A CAPABILITY
// (D308). One a member obtained out of band — pasted from another member's
// context, guessed, recovered from a log — returns the same thing a URI that
// never existed returns, through the same NotFoundResult.
func (h *Host) resourcesRead(ctx context.Context, s *callSession, params json.RawMessage) (any, error) {
	var in resourcesReadParams
	if err := json.Unmarshal(params, &in); err != nil || in.URI == "" {
		return nil, &ProtocolError{Code: CodeInvalidParams, Message: "resources/read requires a uri"}
	}
	p, ok := h.providerForURI(s.principal.Token, in.URI)
	if !ok {
		// ⚠ Not "no such scheme" and not "that module is not in this token's scope":
		// one answer, the same one an unreadable URI gets.
		return nil, &ProtocolError{Code: CodeInvalidParams, Message: notFoundText}
	}
	content, err := p.Read(ctx, in.URI)
	if err != nil {
		if _, perr := toResult(err); perr != nil {
			return nil, perr
		}
		// ⚠ THE ANSWER IS THE SAME FOR EVERY FAILURE AND THE LOG LINE IS NOT.
		// Collapsing a database outage onto "Nenalezeno." is required — leak row 10
		// needs one answer, byte for byte — but collapsing it onto SILENCE is not,
		// and it is how an incident becomes invisible: the caller is told the note
		// does not exist while the store is what is broken. The module goes in the
		// line, never the URI: a URI carries a note's slug, which is user content.
		if !errors.Is(err, ErrResourceNotFound) {
			h.logTool(ctx, "mcp resources/read failed", p.Module(), err)
		}
		return nil, &ProtocolError{Code: CodeInvalidParams, Message: notFoundText}
	}
	return map[string]any{"contents": []any{contentWire(content, h.deps.Config.MaxResultBytes)}}, nil
}

// providerForURI finds the module that addresses uri, honouring the allowlist.
func (h *Host) providerForURI(tok auth.MCPToken, uri string) (Provider, bool) {
	allowed := moduleSet(tok.Modules)
	for _, p := range h.deps.Registry.Providers() {
		if allowed != nil && !allowed[p.Module()] {
			continue
		}
		for _, t := range p.Resources() {
			if uriMatchesTemplate(uri, t.URITemplate) {
				return p, true
			}
		}
	}
	return nil, false
}

// uriMatchesTemplate reports whether uri could be an instance of template. It
// compares only the fixed prefix before the first `{`, which is enough to route:
// the provider re-parses and re-checks the whole URI itself, and a router that
// validated the shape here would be a second parser of the same string.
func uriMatchesTemplate(uri, template string) bool {
	i := strings.Index(template, "{")
	if i < 0 {
		return uri == template
	}
	return strings.HasPrefix(uri, template[:i])
}

// ---- Wire shapes ----

func toolsWire(tools []Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		out = append(out, map[string]any{
			"name":        t.Name,
			"title":       t.Title,
			"description": t.Description,
			"inputSchema": schema,
			// ⚠ readOnlyHint is set truthfully and destructiveHint NEVER APPEARS —
			// not as false, not at all. Nothing destructive is published (D294), so
			// there is nothing to annotate, and TestNoDestructiveTools asserts the key
			// is absent from the serialised manifest.
			"annotations": map[string]any{"readOnlyHint": t.ReadOnly},
		})
	}
	return out
}

// resultWire renders a tool result, capping it at maxBytes.
//
// ⚠ THE CAP IS THE HOST'S, NOT EACH PROVIDER'S (D307): a cap eleven providers
// must remember is a cap that is wrong in at least one of them. ⚠ And truncation
// is STATED IN THE RESULT, never silent — a model handed half a list with no note
// will reason about it as though it were the whole list.
func resultWire(r Result, maxBytes int) map[string]any {
	text, truncated := capText(r.Text, maxBytes)
	if truncated {
		text += "\n\n[Zkráceno — výsledek byl příliš dlouhý.]"
	}
	out := map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": r.IsError,
	}
	// Structured content is dropped rather than cut when it would not fit: half a
	// JSON document is not a smaller JSON document, and a client that parses it
	// would fail on the truncation instead of reading the text beside it.
	if len(r.JSON) > 0 && (maxBytes <= 0 || len(r.JSON) <= maxBytes) {
		out["structuredContent"] = r.JSON
	}
	return out
}

func contentWire(c Content, maxBytes int) map[string]any {
	out := map[string]any{"uri": c.URI}
	if c.MIMEType != "" {
		out["mimeType"] = c.MIMEType
	}
	if len(c.Blob) > 0 {
		blob := c.Blob
		if maxBytes > 0 && len(blob) > maxBytes {
			// ⚠ Bytes are truncated at the SOURCE, not after base64, so the client
			// receives a short-but-well-formed payload rather than a broken encoding.
			blob = blob[:maxBytes]
			out["_truncated"] = true
		}
		out["blob"] = base64.StdEncoding.EncodeToString(blob)
		return out
	}
	text, truncated := capText(c.Text, maxBytes)
	if truncated {
		text += "\n\n[Zkráceno — obsah byl příliš dlouhý.]"
	}
	out["text"] = text
	return out
}

// capText cuts s to at most maxBytes, on a UTF-8 boundary.
func capText(s string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s, false
	}
	cut := maxBytes
	// Back off to the start of the rune, so the result is still valid UTF-8 — a
	// broken final rune is a parse error at the client rather than a short answer.
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut], true
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// sortHits applies the documented merge order (D300).
//
// ⚠ IT IS A MERGE, NOT A RANKING, AND THE TOOL DESCRIPTION SAYS SO. bm25 values
// from five separate external-content FTS5 indexes are not comparable, and a
// global "relevance" assembled from them is confident nonsense — so the order is
// (exact title match, then recency, then module registration order) and a model
// reading it knows not to treat position as authority.
func sortHits(hits []Hit, moduleOrder map[string]int) {
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.ExactHit != b.ExactHit {
			return a.ExactHit
		}
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			return a.UpdatedAt.After(b.UpdatedAt)
		}
		return moduleOrder[moduleOf(a.Kind)] < moduleOrder[moduleOf(b.Kind)]
	})
}

// moduleOf reads the module out of a hit kind ("notes.note" → "notes").
func moduleOf(kind string) string {
	if i := strings.Index(kind, "."); i >= 0 {
		return kind[:i]
	}
	return kind
}
