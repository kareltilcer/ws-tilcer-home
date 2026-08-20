// Package arch holds the architecture tests that enforce the modular-monolith
// boundaries at CI time (PRD §8, §10 D25/D28). A boundary violation fails the
// build — this is an acceptance criterion, not a nicety.
package arch

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const modulePrefix = "github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/"

func modulesDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the arch test file")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "modules")
}

// TestModulesDoNotImportEachOther asserts that no feature module imports another
// module's package (D28). In particular the dashboard host imports neither todo
// nor events — it reaches feature data only through the widget-provider contract.
// Test files are excluded: cross-module wiring is legitimate in the composition
// root and in black-box tests.
func TestModulesDoNotImportEachOther(t *testing.T) {
	root := modulesDir(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read modules dir: %v", err)
	}
	known := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			known[e.Name()] = true
		}
	}

	fset := token.NewFileSet()
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		self := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]

		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(p, modulePrefix) {
				continue
			}
			other := strings.SplitN(strings.TrimPrefix(p, modulePrefix), "/", 2)[0]
			if known[other] && other != self {
				t.Errorf("%s (module %q) imports module %q — cross-module imports are forbidden (D28); "+
					"cross-module data must flow through the widget-provider contract or the audit sink", rel, self, other)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk modules: %v", walkErr)
	}
}

// forbiddenImports names platform packages a specific module must never reach
// for, beyond the blanket cross-module ban above.
//
// These are not style preferences — each one is a DESIGN DECISION that a later
// "small improvement" would quietly undo, and a comment saying so is not
// enforcement. Adding an entry here is how a decision like "this module sends
// nothing" survives the person who did not read the PRD.
var forbiddenImports = map[string][]struct{ pkg, why string }{
	"garden": {
		{"platform/push", "the garden module sends NOTHING (PRD D113). It publishes the metric " +
			"garden.frost_risk_tonight, the list garden.frost_sensitive_now, and one garden.frost_warning " +
			"audit event whose Czech summary already reads as a finished notification. Administrace then " +
			"chooses at runtime between a scheduled summary and a trigger rule — both work on day one. " +
			"A third, hard-coded path inside the module would be the one nobody can configure or silence"},
		{"platform/blobstore", "the garden module holds NO BYTES (PRD D122). Photos of crops, varieties " +
			"and beds are explicitly out of scope, so there is no bucket, no mirror job and no " +
			"reconciliation to get wrong"},
	},
}

// TestForbiddenPlatformImports enforces the per-module platform bans above.
func TestForbiddenPlatformImports(t *testing.T) {
	root := modulesDir(t)
	fset := token.NewFileSet()

	for module, bans := range forbiddenImports {
		moduleRoot := filepath.Join(root, module)
		if _, err := os.Stat(moduleRoot); err != nil {
			t.Errorf("module %q not found — was it renamed? The bans in forbiddenImports would then "+
				"be silently unenforced, which is worse than a failing test", module)
			continue
		}
		walkErr := filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if perr != nil {
				return perr
			}
			rel, _ := filepath.Rel(root, path)
			for _, imp := range f.Imports {
				p := strings.Trim(imp.Path.Value, `"`)
				for _, ban := range bans {
					// The exact package AND anything nested under it: a future
					// subpackage (platform/push/webpush) is the same capability
					// through a longer path, and a suffix match alone would let it
					// slip past the ban silently.
					if strings.HasSuffix(p, "/internal/"+ban.pkg) || strings.Contains(p, "/internal/"+ban.pkg+"/") {
						t.Errorf("%s imports %s — %s", filepath.ToSlash(rel), ban.pkg, ban.why)
					}
				}
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", module, walkErr)
		}
	}
}
