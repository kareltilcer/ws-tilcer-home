package mcp_test

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
)

// updateManifest rewrites the golden file instead of comparing against it.
//
//	go test ./internal/platform/mcp/ -run TestManifestMatchesTheCommittedFile -update
var updateManifest = flag.Bool("update", false, "rewrite backend/mcp-manifest.json from the live registry")

// manifestPath is `backend/mcp-manifest.json`, found from this file rather than
// from the working directory — `go test` runs in the package's directory and a
// relative path from the repo root would only work from one place.
func manifestPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the manifest test file")
	}
	// internal/platform/mcp → backend
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "mcp-manifest.json")
}

// ⚠ THE MANIFEST IS THE MCP SURFACE'S CONTRACT, and this is the test that keeps
// it honest (D320). `openapi.yaml` describes HTTP resources; MCP is JSON-RPC with
// a JSON Schema per tool, and forcing one into the other produces a document
// wrong in both directions — so the contract is a generated, committed file, and
// a schema cannot change without somebody seeing it in a diff.
//
// ⚠ IT IS GENERATED FROM THE LIVE REGISTRY, which is why it lives HERE rather
// than in `internal/arch`: this package's harness composes every real provider
// over a real database, and a manifest built from anything less would be a
// description of a fixture. `internal/arch/mcp_completeness_test.go` is the other
// half — it reads the committed file and checks the host-map alignment without
// needing a database at all.
func TestManifestMatchesTheCommittedFile(t *testing.T) {
	h := newHarness(t)
	got, err := mcp.MarshalManifest(mcp.BuildManifest(h.host))
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	path := manifestPath(t)
	if *updateManifest {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
		t.Logf("wrote %s (%d bytes)", path, len(got))
		return
	}

	wantRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v\n\nRegenerate it with:\n"+
			"  go test ./internal/platform/mcp/ -run TestManifestMatchesTheCommittedFile -update", path, err)
	}
	// ⚠ CR IS STRIPPED BEFORE COMPARING. This worktree is CRLF and the blobs are
	// LF, so a byte comparison against the checked-out file fails on every line for
	// a reason that has nothing to do with the surface.
	want := strings.ReplaceAll(string(wantRaw), "\r\n", "\n")
	if want == string(got) {
		return
	}
	t.Errorf("backend/mcp-manifest.json is stale — the published MCP surface has"+
		" changed and the committed contract has not.\n\nRegenerate it and READ THE"+
		" DIFF:\n  go test ./internal/platform/mcp/ -run"+
		" TestManifestMatchesTheCommittedFile -update\n\n%s", manifestDiff(want, string(got)))
}

// manifestDiff reports the first differing line with a little context, because a
// 40 kB "want/got" dump is a test failure nobody reads.
func manifestDiff(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := range max(len(wantLines), len(gotLines)) {
		w, g := lineAt(wantLines, i), lineAt(gotLines, i)
		if w == g {
			continue
		}
		var b strings.Builder
		for j := max(0, i-2); j < i; j++ {
			b.WriteString("   " + lineAt(wantLines, j) + "\n")
		}
		b.WriteString("  -" + w + "\n")
		b.WriteString("  +" + g + "\n")
		return "first difference at line " + itoa(i+1) + ":\n" + b.String()
	}
	return "the files differ in length only"
}

func lineAt(lines []string, i int) string {
	if i < 0 || i >= len(lines) {
		return ""
	}
	return lines[i]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// ⚠ A PROMPT IS THE ONE PLACE IN v11 WHERE A TOOL NAME IS WRITTEN AS PROSE. The
// bodies tell a model to call `home_garden_tasks` and friends in a Czech
// sentence, so a renamed tool leaves the prompt compiling, the manifest updating
// cleanly, and the assistant reporting a broken server to whoever tried it. This
// is the only thing that notices.
func TestPromptsOnlyNameToolsThatExist(t *testing.T) {
	h := newHarness(t)
	m := mcp.BuildManifest(h.host)

	known := map[string]bool{}
	for _, tool := range m.Tools {
		known[tool.Name] = true
	}
	if len(m.Prompts) != 5 {
		t.Fatalf("%d prompts, want the five §V11-4 FR-M8 specifies", len(m.Prompts))
	}

	named := 0
	for _, p := range m.Prompts {
		for _, ref := range backtickedNames(p.Text) {
			if !strings.HasPrefix(ref, "home_") {
				continue
			}
			named++
			if !known[ref] {
				t.Errorf("prompt %q tells the model to call %q, which no module publishes",
					p.Name, ref)
			}
		}
	}
	// ⚠ THE COUNT IS ASSERTED TOO. A prompt whose backticks stopped parsing — a
	// rewrite to plain quotes, say — would make every assertion above vacuous, and
	// the test would go on passing over prompts naming nothing at all.
	if named < 10 {
		t.Fatalf("only %d tool names were found across five prompts, which is too few"+
			" to be real — has the backtick convention changed?", named)
	}
}

// backtickedNames pulls `identifiers` out of a prompt body.
func backtickedNames(text string) []string {
	var out []string
	for rest := text; ; {
		open := strings.IndexByte(rest, '`')
		if open < 0 {
			return out
		}
		rest = rest[open+1:]
		closeAt := strings.IndexByte(rest, '`')
		if closeAt < 0 {
			return out
		}
		out = append(out, rest[:closeAt])
		rest = rest[closeAt+1:]
	}
}

// ⚠ AN ARGUMENT DECLARED AND NEVER SUBSTITUTED IS A VALUE SILENTLY DROPPED. It is
// the one failure on this surface that produces no error anywhere: the member
// types a month, `prompts/list` advertised the field so the client collected it,
// and the body the model reads never mentions it — so the answer is about a
// different month and looks entirely plausible. The placeholder is what makes the
// declaration mean something, and this asserts every declaration has one.
func TestEveryDeclaredPromptArgumentReachesItsBody(t *testing.T) {
	h := newHarness(t)
	declared := 0
	for _, p := range mcp.BuildManifest(h.host).Prompts {
		for _, arg := range p.Arguments {
			declared++
			if !strings.Contains(p.Text, "{{"+arg+"}}") {
				t.Errorf("prompt %q declares the argument %q and its body never uses"+
					" {{%s}} — a client would collect the value and the model would never"+
					" see it", p.Name, arg, arg)
			}
		}
	}
	if declared == 0 {
		t.Fatal("no prompt declares an argument, so this assertion is vacuous —" +
			" `měsíční-uzávěrka` takes `měsíc` (§V11-4 FR-M8)")
	}
}

// The arguments reach the body, and a name the prompt does not declare is
// refused rather than dropped.
func TestPromptArgumentsAreSubstituted(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	got := h.rpc(secret, "prompts/get", map[string]any{
		"name":      "měsíční-uzávěrka",
		"arguments": map[string]any{"měsíc": "2026-08"},
	})
	body := got.Body.String()
	if !strings.Contains(body, "2026-08") {
		t.Errorf("the supplied month never reached the prompt body:\n%s", body)
	}
	if strings.Contains(body, "{{") {
		t.Errorf("a placeholder survived into the served body:\n%s", body)
	}

	// ⚠ AND WITH NO ARGUMENT THE PLACEHOLDER IS EMPTIED, NEVER PRINTED. A body
	// carrying a literal {{měsíc}} is what a model reads as an instruction to
	// invent one.
	bare := h.rpc(secret, "prompts/get", map[string]any{"name": "měsíční-uzávěrka"})
	if strings.Contains(bare.Body.String(), "{{") {
		t.Errorf("prompts/get with no arguments served a raw placeholder:\n%s", bare.Body.String())
	}

	// An undeclared name is a protocol error: a value quietly dropped here is a
	// close-out of the wrong month with nothing on the wire saying so.
	bad := h.rpc(secret, "prompts/get", map[string]any{
		"name":      "měsíční-uzávěrka",
		"arguments": map[string]any{"mesic": "2026-08"},
	})
	if env := decodeEnvelope(t, bad); env.Error == nil {
		t.Errorf("an argument the prompt does not declare was accepted: %s", bad.Body.String())
	}
}

// The prompts are served, not merely declared.
func TestPromptsAreServed(t *testing.T) {
	h := newHarness(t)
	secret, _ := h.mintToken(memberA, "Claude", nil, time.Time{})

	list := h.rpc(secret, "prompts/list", nil)
	for _, name := range []string{"co-mě-čeká", "nákup", "večeře-z-toho-co-máme", "měsíční-uzávěrka", "zahrada-týden"} {
		if !strings.Contains(list.Body.String(), name) {
			t.Errorf("prompts/list does not offer %q:\n%s", name, list.Body.String())
		}
	}

	got := h.rpc(secret, "prompts/get", map[string]any{"name": "co-mě-čeká"})
	if !strings.Contains(got.Body.String(), "home_today") {
		t.Errorf("prompts/get returned no body:\n%s", got.Body.String())
	}
	// An unknown prompt is a PROTOCOL error: a model cannot fix it by trying
	// different arguments.
	unknown := h.rpc(secret, "prompts/get", map[string]any{"name": "neexistuje"})
	if env := decodeEnvelope(t, unknown); env.Error == nil {
		t.Errorf("an unknown prompt name was accepted: %s", unknown.Body.String())
	}
	// ⚠ AND prompts/list REQUIRES THE BEARER, like everything but initialize
	// (D312): the prompt bodies name this household's modules and habits.
	if rr := h.rpc("", "prompts/list", nil); rr.Code != 401 {
		t.Errorf("prompts/list without a bearer got %d, want 401", rr.Code)
	}
}
