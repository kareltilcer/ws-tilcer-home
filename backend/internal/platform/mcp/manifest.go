package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// The golden manifest (v11, PRD §V11-6, D319/D320).
//
// ⚠ OPENAPI DESCRIBES HTTP RESOURCES AND MCP IS JSON-RPC WITH A JSON SCHEMA PER
// TOOL. Forcing one into the other produces a document that is wrong in both
// directions, so the MCP surface's contract is this file's output —
// `backend/mcp-manifest.json`, generated from the LIVE registry, committed, and
// diffed in review — with the prose in `HANDOFF-13-mcp.md`.
//
// ⚠ AND IT IS GENERATED, NEVER HAND-WRITTEN. The tool catalog is the SEVENTH host
// map on the PRD's own arithmetic (D213 counted six), and the six that came before
// it are hand-maintained; this one is not going to be. `platform/storage`'s table
// declarations got the same treatment because a real bug shipped green there —
// `StorageBlobs` implemented on the Service rather than the Module, everything
// compiling, every test passing, and the Úložiště page reporting 0 B. *"It was
// found by opening the page."* **There is no page to open here**, which is the
// whole argument.

// ManifestVersion is the manifest's own schema version, so a reader can tell a
// v11 manifest from whatever replaces it.
const ManifestVersion = 1

// Manifest is the serialised surface: every tool, every resource template, every
// prompt.
type Manifest struct {
	ManifestVersion int                `json:"manifest_version"`
	ProtocolVersion string             `json:"protocol_version"`
	Tools           []ManifestTool     `json:"tools"`
	Resources       []ManifestResource `json:"resource_templates"`
	Prompts         []ManifestPrompt   `json:"prompts"`
}

// ManifestTool is one tool as the wire publishes it.
//
// ⚠ THE SCHEMA IS EMBEDDED AS PARSED JSON, not as the raw string. A schema that
// changed only its whitespace would otherwise produce a diff nobody can read,
// and one that changed a field would produce a diff that looks like whitespace.
type ManifestTool struct {
	Name        string          `json:"name"`
	Module      string          `json:"module"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	ReadOnly    bool            `json:"read_only"`
	AdminOnly   bool            `json:"admin_only,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type ManifestResource struct {
	Module      string `json:"module"`
	URITemplate string `json:"uri_template"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	MIMEType    string `json:"mime_type,omitempty"`
}

type ManifestPrompt struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Arguments   []string `json:"arguments,omitempty"`
	// ⚠ THE BODY IS IN THE MANIFEST. A prompt is household knowledge written as
	// prose — what this family eats, what not to touch — and a change to it is
	// exactly the kind that should be read in a diff rather than discovered by
	// somebody wondering why the assistant started suggesting something new.
	Text string `json:"text"`
}

// BuildManifest serialises the whole published surface.
//
// ⚠ THE ORDER IS SORTED, NOT REGISTRATION ORDER, and that is deliberate even
// though registration order is meaningful elsewhere (it is the search merge's
// third tiebreak). A golden file exists to be diffed, and a diff that reshuffles
// when somebody reorders the composition root is a diff nobody reads.
func BuildManifest(h *Host) Manifest {
	m := Manifest{
		ManifestVersion: ManifestVersion,
		ProtocolVersion: protocolVersion,
		Tools:           []ManifestTool{},
		Resources:       []ManifestResource{},
		Prompts:         []ManifestPrompt{},
	}

	appendTool := func(module string, t Tool) {
		m.Tools = append(m.Tools, ManifestTool{
			Name:        t.Name,
			Module:      module,
			Title:       t.Title,
			Description: t.Description,
			ReadOnly:    t.ReadOnly,
			AdminOnly:   t.AdminOnly,
			InputSchema: compactSchema(t.InputSchema),
		})
	}
	// ⚠ The core tools are attributed to the empty module, which is what they
	// have: `home_whoami` belongs to no feature and deriving one from its name
	// would invent a module called "whoami".
	for _, t := range h.coreTools() {
		appendTool("", t)
	}
	for _, p := range h.deps.Registry.Providers() {
		for _, t := range p.Tools() {
			appendTool(p.Module(), t)
		}
		for _, r := range p.Resources() {
			m.Resources = append(m.Resources, ManifestResource{
				Module:      p.Module(),
				URITemplate: r.URITemplate,
				Name:        r.Name,
				Title:       r.Title,
				Description: r.Description,
				MIMEType:    r.MIMEType,
			})
		}
	}
	for _, p := range h.prompts() {
		args := make([]string, 0, len(p.Arguments))
		for _, a := range p.Arguments {
			args = append(args, a.Name)
		}
		m.Prompts = append(m.Prompts, ManifestPrompt{
			Name:        p.Name,
			Title:       p.Title,
			Description: p.Description,
			Arguments:   args,
			Text:        p.Text,
		})
	}

	sort.Slice(m.Tools, func(i, j int) bool { return m.Tools[i].Name < m.Tools[j].Name })
	sort.Slice(m.Resources, func(i, j int) bool { return m.Resources[i].URITemplate < m.Resources[j].URITemplate })
	sort.Slice(m.Prompts, func(i, j int) bool { return m.Prompts[i].Name < m.Prompts[j].Name })
	return m
}

// MarshalManifest renders the manifest as the committed file's bytes.
//
// ⚠ TWO-SPACE INDENT AND A TRAILING NEWLINE, so the file is a normal text file a
// diff can be read from — and so that regenerating it on another machine produces
// the same bytes.
func MarshalManifest(m Manifest) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// ⚠ HTML escaping OFF. A schema description containing `<` or `&` would
	// otherwise be written as <, which is valid JSON and unreadable prose.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, fmt.Errorf("mcp: marshal manifest: %w", err)
	}
	return buf.Bytes(), nil
}

// compactSchema re-encodes a tool's schema so the manifest carries structure
// rather than the source file's line breaks.
func compactSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		// A schema that does not parse is a programming error the registry already
		// refuses to register — but the manifest must not lose it silently, so it
		// travels verbatim and the diff shows something wrong.
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}
