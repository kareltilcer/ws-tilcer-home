package arch

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// allowedDead lists the unreachable functions this repository keeps ON PURPOSE,
// keyed by "package.Func" as deadcode reports it. An entry is a decision, and the
// value is the reason — an allowlist with no reasons decays into a list of things
// nobody dared delete.
var allowedDead = map[string]string{
	"lexorank.Rebalance": "the sanctioned renormalisation escape hatch, named by the package doc. " +
		"Nothing calls it because keys lengthen instead of colliding, which is the design working; " +
		"a future first caller should find the correct implementation rather than an ad-hoc renumber",
}

// TestNoDeadCode runs golang.org/x/tools/cmd/deadcode over the whole module —
// tests included — and fails on any unreachable function that allowedDead does
// not name.
//
// ⚠ IT EXISTS BECAUSE THE SWEEP WAS OTHERWISE A ONE-OFF. The refactor pass that
// added this deleted eleven unreachable symbols, three of which carried doc
// comments claiming a caller they had lost — `documents.ParseTS` "used by the
// reconciliation pass", `db.Ping` "used by the readiness probe",
// `garden.coreFieldNames` "Exported for the resolve test" (it was neither
// exported nor referenced). A stale comment on dead code is worse than the dead
// code, because a reader believes it. Nothing stopped those from accumulating,
// and without a check nothing would stop the next eleven.
//
// The tool is a pinned `tool` directive in go.mod, so this needs a populated
// module cache but no network. It costs about eight seconds; `-short` skips it.
func TestNoDeadCode(t *testing.T) {
	if testing.Short() {
		t.Skip("deadcode does whole-program analysis; skipped under -short")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the arch test file")
	}
	moduleRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	cmd := exec.Command("go", "tool", "deadcode", "-test", "./...")
	cmd.Dir = moduleRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run deadcode: %v\n%s", err, out)
	}

	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// "path/to/file.go:12:6: unreachable func: pkg.Func" — or bare "Func"
		// for a package-level one, which deadcode prints without a qualifier.
		const marker = "unreachable func: "
		i := strings.Index(line, marker)
		if i < 0 {
			t.Errorf("deadcode said something this test does not understand: %q", line)
			continue
		}
		loc, symbol := strings.TrimRight(line[:i], ": "), line[i+len(marker):]
		key := symbol
		if !strings.Contains(key, ".") {
			// Qualify with the package directory so the allowlist keys are stable.
			key = filepath.Base(filepath.Dir(strings.SplitN(loc, ":", 2)[0])) + "." + key
		}
		if _, allowed := allowedDead[key]; allowed {
			continue
		}
		t.Errorf("%s unreachable func %s — delete it, or add it to allowedDead with the reason it stays.\n"+
			"If its doc comment claims a caller, that comment is now false and is the more urgent half of the fix.", loc, key)
	}
}
