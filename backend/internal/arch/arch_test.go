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
