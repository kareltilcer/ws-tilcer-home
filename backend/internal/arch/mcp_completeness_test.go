package arch

// The SEVENTH host map (v11, PRD §V11-6, D319).
//
// ⚠ THE PRD'S OWN ARITHMETIC COUNTS SIX (D213) and the MCP tool catalog makes
// seven — a place where a fact about a module has to be repeated somewhere else
// or the feature quietly degrades. Six of the seven are hand-maintained. This one
// is not: `backend/mcp-manifest.json` is GENERATED from the live registry by
// `platform/mcp`'s own test, and this file is the guard that the generated
// artifact still agrees with everything else that names a module.
//
// ⚠ IT READS THE COMMITTED FILE AND NEVER BUILDS THE APP, which is why it can
// live in `internal/arch` at all: the generator needs a database and every real
// service, and this needs neither. The split is deliberate —
//   - platform/mcp's TestManifestMatchesTheCommittedFile proves the file
//     describes the LIVE surface;
//   - this file proves the file agrees with `openapi.yaml`, with the module
//     directories, and with the decisions v11 wrote down.
//
// Either half alone would be satisfiable by a lie.
//
// ⚠ AND `platform/storage`'s TestRealModulesImplementTheStorageCatalog IS WHY
// THIS EXISTS AT ALL. That test was written because a real bug shipped green —
// `StorageBlobs`/`PrivateItems` implemented on the *Service* rather than the
// *Module*, everything compiling, every test passing, and the Úložiště page
// reporting 0 B. *"It was found by opening the page."* There is no page to open
// here.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// manifestTool mirrors mcp.ManifestTool, declared again rather than imported.
//
// ⚠ THAT DUPLICATION IS THE POINT. Importing the producer's own struct would make
// this test agree with the producer by construction — including about a field the
// producer stopped writing. Reading the committed JSON on its own terms is what
// lets it notice.
type manifestTool struct {
	Name        string         `json:"name"`
	Module      string         `json:"module"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	ReadOnly    bool           `json:"read_only"`
	AdminOnly   bool           `json:"admin_only"`
	InputSchema map[string]any `json:"input_schema"`
}

type manifestResource struct {
	Module      string `json:"module"`
	URITemplate string `json:"uri_template"`
	Name        string `json:"name"`
}

type manifestPrompt struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

type manifestFile struct {
	ManifestVersion int                `json:"manifest_version"`
	ProtocolVersion string             `json:"protocol_version"`
	Tools           []manifestTool     `json:"tools"`
	Resources       []manifestResource `json:"resource_templates"`
	Prompts         []manifestPrompt   `json:"prompts"`
}

func repoFile(t *testing.T, rel ...string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the arch test file")
	}
	// internal/arch → backend
	parts := append([]string{filepath.Dir(thisFile), "..", ".."}, rel...)
	return filepath.Join(parts...)
}

func loadManifest(t *testing.T) manifestFile {
	t.Helper()
	raw, err := os.ReadFile(repoFile(t, "mcp-manifest.json"))
	if err != nil {
		t.Fatalf("read mcp-manifest.json: %v\n\nGenerate it with:\n"+
			"  go test ./internal/platform/mcp/ -run TestManifestMatchesTheCommittedFile -update", err)
	}
	var m manifestFile
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse mcp-manifest.json: %v", err)
	}
	if len(m.Tools) == 0 {
		t.Fatal("the manifest publishes no tools — every assertion below would be vacuous")
	}
	return m
}

// Tool names are unique across the registry, and shaped `home_<module>_<verb>`.
func TestManifestToolNamesAreUniqueAndWellFormed(t *testing.T) {
	m := loadManifest(t)
	seen := map[string]string{}
	for _, tool := range m.Tools {
		if prev, dup := seen[tool.Name]; dup {
			t.Errorf("duplicate tool name %q (modules %q and %q)", tool.Name, prev, tool.Module)
		}
		seen[tool.Name] = tool.Module
		if !strings.HasPrefix(tool.Name, "home_") {
			t.Errorf("tool %q does not start with home_", tool.Name)
		}
		if tool.Module != "" && !strings.HasPrefix(tool.Name, "home_"+tool.Module+"_") {
			t.Errorf("tool %q is attributed to module %q, which its name does not carry",
				tool.Name, tool.Module)
		}
		if strings.TrimSpace(tool.Description) == "" {
			t.Errorf("tool %q has no description — it is the whole of a model's"+
				" tool-selection evidence", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %q has no input schema", tool.Name)
		}
	}
}

// Every module a tool claims is a real feature module.
//
// ⚠ THE MODULE IS A FIELD RATHER THAN SOMETHING DERIVED FROM THE NAME, and this
// is the assertion that needs the real answer: deriving it would make
// `home_admin_status` look like it belongs to a module called `admin_status`.
func TestManifestModulesAreRegisteredModules(t *testing.T) {
	m := loadManifest(t)
	entries, err := os.ReadDir(modulesDir(t))
	if err != nil {
		t.Fatalf("read modules dir: %v", err)
	}
	known := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			known[e.Name()] = true
		}
	}
	for _, tool := range m.Tools {
		// The empty module is the seven cross-cutting tools, which belong to the
		// host and to no feature.
		if tool.Module == "" || known[tool.Module] {
			continue
		}
		t.Errorf("tool %q claims module %q, which is not a directory under internal/modules",
			tool.Name, tool.Module)
	}
	for _, r := range m.Resources {
		if !known[r.Module] {
			t.Errorf("resource template %q claims module %q, which is not a real module",
				r.URITemplate, r.Module)
		}
	}
}

// Every member of openapi's `McpModule` enum publishes at least one tool.
//
// ⚠ THE ENUM IS WHAT A MEMBER MAY SCOPE A TOKEN TO, so a name in the list with
// nothing behind it is a token that silently reaches nothing — the failure the
// `modules` allowlist's own 422 exists to prevent, arriving one layer up.
func TestMcpModuleEnumMatchesTheManifest(t *testing.T) {
	m := loadManifest(t)
	enum := readMcpModuleEnum(t)
	if len(enum) == 0 {
		t.Fatal("no McpModule enum found in openapi.yaml — the assertion below would be vacuous")
	}

	published := map[string]int{}
	for _, tool := range m.Tools {
		if tool.Module != "" {
			published[tool.Module]++
		}
	}
	for _, module := range enum {
		if published[module] == 0 {
			t.Errorf("openapi's McpModule enum names %q, which publishes no tool", module)
		}
	}
	for module := range published {
		if !contains(enum, module) {
			t.Errorf("module %q publishes tools but is not in openapi's McpModule enum,"+
				" so no member can scope a token to it", module)
		}
	}
	// ⚠ `dashboard` and `logging` are absent from the enum ON PURPOSE (FR-M2):
	// the dashboard is the member's screen rather than their data, and logging
	// publishes only a Search. Asserted so that adding either reads as the
	// decision it would be.
	for _, absent := range []string{"dashboard", "logging"} {
		if contains(enum, absent) {
			t.Errorf("McpModule names %q, which FR-M2 says publishes no tool", absent)
		}
	}
}

// ⚠ NOTHING CARRIES destructiveHint, AND THE ABSENCE IS THE MECHANISM (D294). A
// gated destructive tool is one a model still proposes and a member still
// approves at 23:40; the guarantee is that there is nothing to propose.
func TestNoDestructiveTools(t *testing.T) {
	raw, err := os.ReadFile(repoFile(t, "mcp-manifest.json"))
	if err != nil {
		t.Fatalf("read mcp-manifest.json: %v", err)
	}
	if strings.Contains(string(raw), "destructiveHint") || strings.Contains(string(raw), "destructive_hint") {
		t.Error("the manifest mentions a destructive hint. v11 publishes nothing" +
			" destructive AT ALL — the verb is meant to be absent rather than annotated (D294).")
	}

	m := loadManifest(t)
	// ⚠ THE ABSENT VERBS ARE CHECKED BY NAME (D295/FR-M5), because "we did not add
	// one" is not a property a type system carries. Each is absent for its own
	// reason; `season_close` is the one that reads as harmless and destroys work.
	for _, verb := range []string{
		"delete", "publish", "purge", "upload", "season_close", "season_reopen",
		"layout", "membership", "remove", "broadcast", "revoke",
	} {
		for _, tool := range m.Tools {
			if strings.Contains(tool.Name, verb) {
				t.Errorf("tool %q contains the verb %q, which v11 publishes nothing for", tool.Name, verb)
			}
		}
	}
}

// The ceiling (D293). 144 REST paths cannot become 144 tools: a model's selection
// accuracy collapses well before that, and the list alone would spend the context
// budget before the first question.
func TestToolCountUnderTheCeiling(t *testing.T) {
	m := loadManifest(t)
	if len(m.Tools) > 45 {
		t.Errorf("%d tools, ceiling is 45 — every tool past it must retire one", len(m.Tools))
	}
	if len(m.Tools) != 39 {
		t.Errorf("%d tools; §V11-4 FR-M4 specifies 39. If the surface really did"+
			" change, this number and the PRD move together.", len(m.Tools))
	}
}

// Every declared resource template is owned by a module that could serve it, and
// the three FR-M7 fixes are the three that exist.
func TestResourceTemplates(t *testing.T) {
	m := loadManifest(t)
	want := map[string]string{
		"home://notes/{path}":                         "notes",
		"home://documents/{path}":                     "documents",
		"home://chat/{conversation}/attachments/{id}": "chat",
	}
	if len(m.Resources) != len(want) {
		t.Errorf("%d resource templates, want %d", len(m.Resources), len(want))
	}
	for _, r := range m.Resources {
		module, ok := want[r.URITemplate]
		if !ok {
			t.Errorf("unexpected resource template %q", r.URITemplate)
			continue
		}
		if r.Module != module {
			t.Errorf("template %q is owned by %q, want %q", r.URITemplate, r.Module, module)
		}
	}
	// ⚠ THERE IS NO `sdilene` SEGMENT ANYWHERE, and inventing one would be the only
	// place in the application where the shared root is NAMED. A private item is
	// prefixed `soukrome`; a shared one carries no prefix at all, exactly as
	// lib/scope.ts parses a URL.
	for _, r := range m.Resources {
		if strings.Contains(r.URITemplate, "sdilene") {
			t.Errorf("template %q names the shared root", r.URITemplate)
		}
	}
}

// The manifest is a contract, so the version it speaks is part of it.
func TestManifestDeclaresItsVersions(t *testing.T) {
	m := loadManifest(t)
	if m.ManifestVersion != 1 {
		t.Errorf("manifest_version is %d, want 1", m.ManifestVersion)
	}
	// ⚠ PROTOCOL-VERSION DRIFT IS OURS TO TRACK (D278) — home hand-rolls JSON-RPC
	// rather than taking a pre-1.0 SDK, and this is the second of the two places
	// the supported revision is written down. The first is the constant in
	// `platform/mcp/jsonrpc.go`, which carries the checklist for changing it.
	if m.ProtocolVersion != "2025-06-18" {
		t.Errorf("protocol_version is %q. If home really did move revisions, check"+
			" jsonrpc.go's note first: whether batching is still removed, whether"+
			" MCP-Protocol-Version is still the negotiation header, and whether the"+
			" tool/resource/prompt result shapes are unchanged.", m.ProtocolVersion)
	}
}

// readMcpModuleEnum pulls the `McpModule` enum out of the served contract.
//
// ⚠ IT PARSES openapi.yaml AS TEXT rather than with a YAML library, and that is
// this package's habit rather than a shortcut: `internal/arch` already reads Go
// source with go/parser and nothing else, and adding a YAML dependency to assert
// one line would be the largest thing in the file.
func readMcpModuleEnum(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(repoFile(t, "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "McpModule:" {
			continue
		}
		// The enum is a flow sequence a few lines below the key.
		for j := i; j < i+20 && j < len(lines); j++ {
			trimmed := strings.TrimSpace(lines[j])
			rest, ok := strings.CutPrefix(trimmed, "enum: [")
			if !ok {
				continue
			}
			rest = strings.TrimSuffix(rest, "]")
			var out []string
			for _, part := range strings.Split(rest, ",") {
				if v := strings.TrimSpace(part); v != "" {
					out = append(out, v)
				}
			}
			return out
		}
	}
	return nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
