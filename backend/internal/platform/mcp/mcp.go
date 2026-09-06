// Package mcp is the FIFTH registered catalog (v11, PRD §V11-4 FR-M2, D276) and
// the second front door to all eleven modules.
//
// mcp lives in platform/ and is imported BY modules; it must never import a
// module. The host reaches feature data through Provider, never through a
// module's tables — the same rule registry.Catalog, metrics.Registry and
// lists.Registry all follow, and the reason `internal/arch` stays green with a
// sixth cross-module consumer in the tree.
//
// ⚠ WHAT IS DIFFERENT ABOUT THIS CATALOG, in one paragraph. The other four answer
// a question with a value: a widget returns a payload for one host, a metric an
// int, a list a bounded slice of Czech lines, a storage declaration a table name.
// This one answers with a VERB — and the caller is a program with nobody watching
// the screen. So the shape of the surface is decided by what is ABSENT: delete,
// publish, purge, upload, membership change, admin mutation, season close and
// dashboard layout are not gated, not confirmed, they simply do not exist here
// (D295). A gated destructive tool is one a model still proposes and a member
// still approves at 23:40.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
)

// Source is the optional interface a feature module implements to publish an MCP
// provider.
//
// ⚠ IT IS DELIBERATELY NOT PART OF registry.Module (D56, restated at every
// catalog since): adding a capability must not change the contract every module
// implements. Collect type-asserts for it, exactly as metrics.Source and
// lists.Source are asserted.
type Source interface{ MCPProvider() Provider }

// Provider is how one module publishes tools, search and resources.
//
// ⚠ A PROVIDER WITH NO TOOLS IS NOT AN EMPTY PROVIDER. `logging` implements this
// for Search alone — an empty Tools() and a real index over audit_events_fts. If
// an empty Tools() made something skip the provider entirely, search would lose a
// fifth of its corpus silently.
type Provider interface {
	// Module is the English code identifier, matching registry.Module.Name().
	//
	// ⚠ It is a METHOD rather than something derived from the tool-name prefix.
	// Deriving it would make `home_admin_status` look like it belongs to a module
	// called `admin_status`, and the completeness test needs the real answer to
	// check openapi's McpModule enum against the registry.
	Module() string

	// Tools this module publishes. MAY be empty — see logging.
	Tools() []Tool

	// Call executes one of them. name is the FULL tool name
	// ("home_todo_card_create"). ctx carries a resolved reqctx.Actor; a ctx
	// without one is an error, never an unscoped read (D302).
	Call(ctx context.Context, name string, args json.RawMessage) (Result, error)

	// Search contributes this module's rows to the one cross-module search.
	// Return (nil, nil) for a module with nothing to find.
	Search(ctx context.Context, q Query) ([]Hit, error)

	// Resources are the URI templates this module addresses. MAY be empty.
	Resources() []ResourceTemplate

	// ListResources enumerates the concrete resources the CALLER may read, at
	// most limit of them.
	//
	// ⚠ IT IS VIEWER-SCOPED, and that is leak row 11 (D308): a private root
	// appears only for its owner, a conversation only for its members. This is
	// the *existence* leak v9 spent a whole version closing, arriving in a new
	// surface — a listing is an answer even when every read is refused.
	ListResources(ctx context.Context, limit int) ([]Resource, error)

	// Read resolves one URI. ⚠ Access is re-checked here, FROM SCRATCH, on every
	// call: a URI is not a capability (D308). One a member obtained out of band
	// must return the same thing a URI that never existed returns.
	Read(ctx context.Context, uri string) (Content, error)
}

// Tool is one published verb.
type Tool struct {
	Name string // "home_todo_card_create", unique across the registry
	// Title is a short English label.
	Title string
	// Description is ONE English sentence saying what the tool returns and what
	// it costs. ⚠ It is the whole of a model's tool-selection evidence: a model
	// choosing the wrong tool is a documentation defect with no failing test.
	Description string
	// InputSchema is JSON Schema, draft 2020-12.
	InputSchema json.RawMessage
	// ReadOnly feeds annotations.readOnlyHint, truthfully.
	//
	// ⚠ There is no DestructiveHint field, and its absence is the mechanism
	// (D294). Nothing destructive is published at all, so there is nothing to
	// annotate — and TestNoDestructiveTools asserts the annotation never appears.
	ReadOnly bool
	// AdminOnly marks a tool whose HTTP twin sits behind httpx.RequireAdmin.
	//
	// ⚠ READ-ONLY IS NOT THE SAME QUESTION AS UNGATED, and conflating them is how
	// a token comes to out-rank its owner (leak row 6). `home_activity` reads the
	// audit spine, whose routes have been admin-only since D5: a `reader` is
	// refused it in the browser with a 403, so a reader's TOKEN must be refused it
	// too — "roles gate exactly as they do over HTTP" (PRD §V11-3). The host takes
	// this decision in one place, beside the write gate, rather than leaving each
	// tool to remember.
	//
	// ⚠ An admin-only tool is HIDDEN from a non-admin's tools/list and REFUSED if
	// called anyway — never mapped onto the not-found answer. Leak row 9's
	// 403-reads-as-404 rule is about OWNERSHIP and MEMBERSHIP surfaces, where the
	// existence of the row is the secret; a role refusal hides nothing, and the
	// HTTP twin says 403 out loud.
	AdminOnly bool
}

// Result is what a tool call returns.
type Result struct {
	// Text is the human-readable answer, with Czech data verbatim.
	Text string
	// JSON becomes structuredContent; nil when there is nothing structured.
	JSON json.RawMessage
	// IsError marks a tool that RAN AND REFUSED — never a protocol failure.
	//
	// ⚠ The two are not interchangeable and getting them backwards is not
	// cosmetic: a model RETRIES a protocol error and READS a tool error.
	IsError bool
}

// Query is one cross-module search.
type Query struct {
	Text string // the caller's terms, unparsed
	// Limit is PER MODULE (D301), already clamped by the host. A search that
	// returns 40 chat messages and no notes because chat is chattier is a worse
	// answer than 8 of each.
	Limit int
}

// Hit is one search row.
//
// ⚠ IT CARRIES NO SCORE, AND THE TYPE IS WHERE THAT DECISION IS ENFORCED (D300).
// bm25 values from five separate external-content FTS5 indexes are not
// comparable, and a global "relevance" assembled from them is confident nonsense.
// A provider that wanted to rank globally has nowhere to put the number.
//
// ⚠ THE JSON TAGS ARE NOT DECORATION. This is the ONE type in the catalog that
// reaches a caller by being marshalled directly rather than through a module's
// wire type, and without them home_search's structuredContent was the only
// payload in the whole surface spelled in Go field names — "Title" beside the
// "title" every other tool answers with. A model that has to know which tool
// capitalises its keys is being asked to remember an accident.
type Hit struct {
	Kind      string    `json:"kind"`       // "notes.note", "todo.card", "garden.plant" …
	ID        string    `json:"id"`         //
	Title     string    `json:"title"`      // Czech, verbatim
	Snippet   string    `json:"snippet"`    // Czech, verbatim; may be ""
	UpdatedAt time.Time `json:"updated_at"` // for the merge order — NOT a score
	URI       string    `json:"uri"`        // resource URI when the hit is addressable; "" otherwise
	ExactHit  bool      `json:"exact_hit"`  // the module's own judgement that the title matched exactly
}

// ResourceTemplate is one URI shape a module addresses.
type ResourceTemplate struct {
	URITemplate string // "home://notes/{path}"
	Name        string // stable identifier, e.g. "note"
	Title       string // Czech label
	Description string // one sentence
	MIMEType    string // when every instance shares one; "" otherwise
}

// Resource is one concrete addressable thing, as `resources/list` renders it.
type Resource struct {
	URI      string
	Name     string // Czech title
	MIMEType string
	Size     int64 // bytes, 0 when unknown
}

// Content is what a resource read returns. Exactly one of Text and Blob is set.
//
// ⚠ THERE IS NO Truncated FIELD, and its absence is deliberate rather than an
// omission. Truncation is the HOST's (D307) and it happens on the way to the
// wire, in contentWire, which builds the outbound map from this value and says so
// there — a field on the provider's return type would be a place for a provider
// to answer a question that is not theirs, and nothing would ever read it.
type Content struct {
	URI      string
	MIMEType string
	Text     string
	Blob     []byte
}

// ---- Embeddable defaults ----

// NoResources is embedded by a provider that addresses nothing by URI. It exists
// so that "this module has no resources" is one line rather than three empty
// methods per provider — and so that adding a method to Provider later fails the
// build in one place instead of nine.
type NoResources struct{}

func (NoResources) Resources() []ResourceTemplate { return nil }

func (NoResources) ListResources(context.Context, int) ([]Resource, error) { return nil, nil }

// Read on a provider with no resources refuses exactly the way an unknown URI
// does — never with a distinguishable "this module has no resources" (D303).
func (NoResources) Read(context.Context, string) (Content, error) {
	return Content{}, ErrResourceNotFound
}

// ---- Provider helpers ----
//
// ⚠ THEY LIVE HERE RATHER THAN ONCE PER PROVIDER, and the reason is arithmetic:
// v11 ships three providers in PR 1 and six more in PR 2. Both of these were
// byte-identical copies in each of the three, and DecodeArgs in particular
// carries a RULE — D311's "a malformed body is a 422, not a 500", because an
// agent retries a 500 and gives up on a 422 — which a tenth copy written from
// memory is exactly how a module comes to lose.
//
// ⚠ AND THE HOST'S OWN SEVEN GO THROUGH THEM TOO. They were the exception for one
// round — three hand-written `json.Unmarshal`s with their own Czech refusal and
// three more guarded by `len(args) > 0` — which is three spellings of one rule
// inside one file, and three precedents for the eighth core tool to copy from.

// DecodeArgs decodes one tool call's arguments, mapping a malformed body onto a
// 422-shaped refusal rather than an internal error (D311). Absent arguments
// leave dst untouched, which is what a tool with only optional fields wants.
//
// ⚠ IT IS STRICT, AND IT IS STRICT BECAUSE THE OTHER FRONT DOOR IS. Every tool's
// InputSchema declares "additionalProperties": false, and httpx.DecodeJSON has
// refused an unknown field on all 178 HTTP handler call sites since v1 — so a
// lenient decode here made the SECOND door to the same eleven modules accept a
// class of mistake the FIRST one refuses. It is not theoretical and it is not
// loud: home_notes_create with "body" instead of "body_md" created a note with an
// EMPTY body and answered "Poznámka vytvořena", and home_notes_tree with "noteId"
// instead of "note" answered with the whole shared tree instead of one note. Both
// are the silently-ignored value this version refuses everywhere else — an
// unknown module is a 422, an unknown 'via' is a 422, an expiry on PATCH is a 422
// rather than a no-op — applied at last to the arguments themselves.
//
// ⚠ The unknown field's NAME goes back verbatim, in the English encoding/json
// spells it, exactly as the HTTP twin's 422 detail does: the model cannot fix a
// field it is not told about, and a translated message would not name the key.
func DecodeArgs(args json.RawMessage, dst any) error {
	if len(args) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return httpx.ErrUnprocessable("Neplatné parametry: " + err.Error())
	}
	// Trailing content after the first value is the same class of malformed body,
	// and httpx.DecodeJSON refuses it for the same reason.
	if dec.More() {
		return httpx.ErrUnprocessable("Neplatné parametry: nadbytečný obsah za argumenty.")
	}
	return nil
}

// TextResult builds a Result carrying the Czech answer and payload as
// structuredContent beside it.
//
// ⚠ A PAYLOAD THAT WILL NOT MARSHAL DEGRADES TO THE TEXT HALF RATHER THAN
// FAILING THE CALL — the text is a complete answer on its own, and refusing a
// read the caller could have had is the worse outcome. It is still a programming
// error, so it does not pass unnoticed: the text carries the note, which is the
// only channel a provider has (it holds no logger, by design — a provider is
// reached from the host, and the host is what owns the request id).
func TextResult(text string, payload any) (Result, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return Result{Text: text + "\n\n[Strukturovaná část odpovědi se nepodařilo sestavit.]"}, nil
	}
	return Result{Text: text, JSON: b}, nil
}

// KnownModules is the vocabulary a token's `modules` allowlist may name — the
// `McpModule` enum in openapi.yaml, in the same order.
//
// ⚠ IT IS A STATIC LIST AND NOT THE LIVE REGISTRY, and the difference matters
// exactly once: v11 ships as three pull requests, so between PR 1 and PR 2 six
// of these publish no tools yet. Validating against the registry would refuse a
// token scoped to `garden` on a server that is going to have garden tools next
// week — a contract the served openapi.yaml says otherwise about. Naming a
// module with nothing in it is harmless: the allowlist NARROWS and is a
// convenience rather than an access control (D288), so the worst case is a
// token that is offered fewer tools than its owner expected.
//
// ⚠ `dashboard` and `logging` are absent on purpose: the dashboard is the
// member's screen rather than their data, and logging publishes only Search,
// which `home_search` reaches without an allowlist entry. internal/arch's
// completeness test pins this list to the enum and to the registry (D319).
var KnownModules = []string{
	"todo", "events", "notes", "documents", "finance",
	"garden", "electricity", "chat", "admin",
}

// KnownModule reports whether name is in KnownModules.
func KnownModule(name string) bool {
	for _, m := range KnownModules {
		if m == name {
			return true
		}
	}
	return false
}

// ---- Registry ----

// Registry is the assembled catalog. Built once at composition; read-only after.
//
// ⚠ IT DOES NOT REUSE platform/catalog, and that is a deviation worth stating.
// The metric and list catalogs share that generic core because their descriptors
// have a key AND A SCOPE — household or personal — and the admin scheduler
// filters both through one scope predicate. A tool has no scope: every result is
// computed for the caller, always, so registering one through catalog.Register
// would mean inventing a scope value to satisfy a validator that would then mean
// nothing. What IS shared is the property that mattered — a duplicate key fails
// the BUILD of the registry rather than silently shadowing.
type Registry struct {
	providers []Provider          // registration (module) order — the merge tiebreak
	byModule  map[string]Provider //
	byTool    map[string]Provider //
	toolOwner map[string]string   // tool name → module
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		byModule:  map[string]Provider{},
		byTool:    map[string]Provider{},
		toolOwner: map[string]string{},
	}
}

// Register adds one provider. A duplicate module or a duplicate tool name is a
// programming error and fails the build of the registry.
func (r *Registry) Register(p Provider) error {
	// ⚠ reflect, BECAUSE `p == nil` IS NOT THE QUESTION. A Source whose
	// MCPProvider() returns a nil *someProvider hands over an interface that is
	// NOT nil, so the plain check passes and the next line panics at composition
	// — a crash-loop with a stack where every other failure in this function
	// produces a sentence naming the module.
	if isNilProvider(p) {
		return nil
	}
	mod := p.Module()
	if mod == "" {
		return fmt.Errorf("mcp: a provider published no module name")
	}
	if _, dup := r.byModule[mod]; dup {
		return fmt.Errorf("mcp: duplicate provider for module %q", mod)
	}
	for _, t := range p.Tools() {
		if t.Name == "" {
			return fmt.Errorf("mcp: module %q published a tool with no name", mod)
		}
		if owner, dup := r.toolOwner[t.Name]; dup {
			return fmt.Errorf("mcp: duplicate tool name %q (modules %q and %q)", t.Name, owner, mod)
		}
		if len(t.InputSchema) == 0 {
			return fmt.Errorf("mcp: tool %q has no input schema", t.Name)
		}
		r.byTool[t.Name] = p
		r.toolOwner[t.Name] = mod
	}
	r.byModule[mod] = p
	r.providers = append(r.providers, p)
	return nil
}

// isNilProvider reports whether p is nil, INCLUDING a typed nil in a non-nil
// interface.
func isNilProvider(p Provider) bool {
	if p == nil {
		return true
	}
	switch v := reflect.ValueOf(p); v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func:
		return v.IsNil()
	default:
		return false
	}
}

// Collect builds a registry from anything implementing Source — in practice the
// module set, passed straight from the composition root, in module order.
//
// ⚠ THE ORDER IS LOAD-BEARING and it is the caller's: it is the third tiebreak of
// the search merge (D300), so a stable module order is what makes a search return
// the same page twice.
func Collect(modules ...any) (*Registry, error) {
	r := NewRegistry()
	for _, m := range modules {
		src, ok := m.(Source)
		if !ok {
			continue // a module with no MCP surface is normal, not an error
		}
		if err := r.Register(src.MCPProvider()); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Providers returns every registered provider in module order. Nil-receiver safe,
// so a host wired with no catalog degrades to "nothing is published" rather than
// panicking inside a tool call.
func (r *Registry) Providers() []Provider {
	if r == nil {
		return nil
	}
	return r.providers
}

// ToolOwner returns the module publishing name.
func (r *Registry) ToolOwner(name string) (string, bool) {
	if r == nil {
		return "", false
	}
	m, ok := r.toolOwner[name]
	return m, ok
}

// ProviderForTool returns the provider that publishes name.
func (r *Registry) ProviderForTool(name string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	p, ok := r.byTool[name]
	return p, ok
}

// Provider returns one module's provider.
func (r *Registry) Provider(module string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	p, ok := r.byModule[module]
	return p, ok
}
