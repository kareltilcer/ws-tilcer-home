package arch

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// allowedDead lists the unreachable functions this repository keeps ON PURPOSE.
// An entry is a decision, and the value is the reason — an allowlist with no
// reasons decays into a list of things nobody dared delete.
//
// The key is "<directory>.<Symbol>", which this test BUILDS; it is not what
// deadcode prints. deadcode names a package-level function bare (`Rebalance`)
// and a method by its receiver (`Store.SoftDeletePlant`), never with a package —
// so `Store.Foo` alone would exempt a dead `Store.Foo` in every module at once,
// and the directory is prepended unconditionally to stop that. A method's key
// therefore reads "garden.Store.Foo".
var allowedDead = map[string]string{
	"lexorank.Rebalance": "the sanctioned renormalisation escape hatch, named by the package doc. " +
		"Nothing calls it because keys lengthen instead of colliding, which is the design working; " +
		"a future first caller should find the correct implementation rather than an ad-hoc renumber",
}

// TestNoDeadCode runs golang.org/x/tools/cmd/deadcode over the whole module —
// tests included — and fails on any unreachable function that allowedDead does
// not name, and on any allowedDead entry that is no longer unreachable.
//
// ⚠ IT EXISTS BECAUSE THE SWEEP WAS OTHERWISE A ONE-OFF. The refactor pass that
// added this deleted eleven unreachable symbols, three of which carried doc
// comments claiming a caller they had lost — `documents.ParseTS` "used by the
// reconciliation pass", `db.Ping` "used by the readiness probe",
// `garden.coreFieldNames` "Exported for the resolve test" (it was neither
// exported nor referenced). A stale comment on dead code is worse than the dead
// code, because a reader believes it. Nothing stopped those from accumulating,
// and without a check nothing would stop the next eleven. The unused-entry half
// is the same rule pointed at this file: an allowedDead reason that has stopped
// being true is exactly the stale comment the test exists to catch.
//
// The tool is a pinned `tool` directive in go.mod. deadcode's findings are read
// from STDOUT ALONE: `go tool` writes "go: downloading …" to stderr the first
// time the tool is built on a machine, and folding that into the parsed stream
// failed the whole guard on any clone with a cold module cache. It costs about
// eight seconds; `-short` skips it.
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
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run deadcode: %v\n%s%s", err, stdout.String(), stderr.String())
	}

	matched := make(map[string]bool, len(allowedDead))
	for line := range strings.SplitSeq(strings.TrimSpace(stdout.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// "path/to/file.go:12:6: unreachable func: Func" — or "Type.Method" for
		// a method. Neither form carries a package, so one is prepended below.
		const marker = "unreachable func: "
		i := strings.Index(line, marker)
		if i < 0 {
			t.Errorf("deadcode said something this test does not understand: %q", line)
			continue
		}
		loc, symbol := strings.TrimRight(line[:i], ": "), line[i+len(marker):]
		key := filepath.Base(filepath.Dir(trimPosition(loc))) + "." + symbol
		if _, allowed := allowedDead[key]; allowed {
			matched[key] = true
			continue
		}
		t.Errorf("%s unreachable func %s — delete it, or add it to allowedDead with the reason it stays.\n"+
			"If its doc comment claims a caller, that comment is now false and is the more urgent half of the fix.", loc, key)
	}

	for key, reason := range allowedDead {
		if matched[key] {
			continue
		}
		t.Errorf("allowedDead names %s, but deadcode no longer reports it — it has a caller now, or it is gone.\n"+
			"Delete the entry: the reason it carries (%q) is no longer true.", key, reason)
	}
}

// trimPosition strips the ":line:col" suffix deadcode appends to a filename. It
// cuts from the RIGHT so a Windows drive letter ("C:\...") survives.
func trimPosition(loc string) string {
	for range 2 {
		if i := strings.LastIndex(loc, ":"); i >= 0 {
			loc = loc[:i]
		}
	}
	return loc
}
