package electricity

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

const computeFile = "compute.go"

// allowedComputeImports is a WHITELIST, deliberately, rather than a blacklist of
// database/sql.
//
// A blacklist only catches what its author thought of, and what actually creeps
// into a file like this is not database/sql — it is net/http (to read a query
// param "just here"), or a store type (to look up one more row), or context
// (which is the first symptom of both). A whitelist fails on all of them and on
// the ones nobody has imagined yet, and the failure message says what to do
// instead: load it in store.Snapshot and pass it in.
//
// platform/dates is on the list because Date, AddDays, DaysUntil and
// DaysInMonth live there. Re-implementing calendar arithmetic locally to keep
// this list shorter would create exactly the second source of truth this module
// exists to avoid — and the no-time.Now assertion below already keeps the one
// clock-reading function in that package (dates.Today) out of here.
var allowedComputeImports = map[string]bool{
	"fmt":     true,
	"sort":    true,
	"time":    true,
	"errors":  true,
	"strings": true,
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates": true,
}

// TestComputeIsPure is the guard on the module's one locked file (PRD §V8-8,
// brief §9/#12). compute.go takes a loaded snapshot in and returns the summary
// out; the moment it can reach a database, an HTTP request or a clock, it stops
// being a function two people can reproduce by hand — which is the whole claim
// this module makes about its own arithmetic.
func TestComputeIsPure(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, computeFile, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", computeFile, err)
	}
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if !allowedComputeImports[path] {
			t.Errorf("%s imports %q, which is not on the whitelist.\n"+
				"compute.go is PURE: it takes a loaded Snapshot and returns a Summary. "+
				"If you need more data, load it in store.Snapshot and put it on the Snapshot — "+
				"do not reach for it from here.", computeFile, path)
		}
	}
}

// TestComputeReadsNoClock. A summary that consulted the clock would change while
// it was being read: Přehled, Odečty and Historie all go through this file, and
// across a midnight the three would quietly disagree about what "today" was.
// `Today` is resolved once by the caller, from HOME_TIMEZONE, and travels in on
// the Snapshot.
func TestComputeReadsNoClock(t *testing.T) {
	src, err := os.ReadFile(computeFile)
	if err != nil {
		t.Fatalf("read %s: %v", computeFile, err)
	}
	for _, banned := range []string{"time.Now", "dates.Today"} {
		if strings.Contains(stripComments(string(src)), banned) {
			t.Errorf("%s calls %s. Resolve the date once in the caller and pass it in "+
				"as Snapshot.Today, or the three views that read through this file "+
				"will disagree across a midnight.", computeFile, banned)
		}
	}
}

// TestComputeHasNoFloats encodes D148 structurally. Money is integer haléře and
// energy is integer tenths of kWh; a float anywhere in this file — even
// transiently, even "just for the average" — loses the determinism the module's
// entire value rests on, which is that two people can reproduce the bill and get
// the same answer to the haléř.
//
// divRound is the only rounding routine in the file; this test is what stops a
// second one arriving as math.Round or an idiomatic-looking `+ 0.5`.
func TestComputeHasNoFloats(t *testing.T) {
	src, err := os.ReadFile(computeFile)
	if err != nil {
		t.Fatalf("read %s: %v", computeFile, err)
	}
	code := stripComments(string(src))
	for _, banned := range []string{"float64", "float32", "math.Round", "math.Floor", "math.Ceil"} {
		if strings.Contains(code, banned) {
			t.Errorf("%s contains %q — no float and no second rounding routine may enter "+
				"the money path (D148). Everything goes through divRound.", computeFile, banned)
		}
	}
}

// stripComments removes // and /* */ comments so a doc comment that MENTIONS a
// banned symbol (this file's own comments do) cannot fail the test that bans it.
func stripComments(src string) string {
	var b strings.Builder
	for i := 0; i < len(src); i++ {
		switch {
		case strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			i += end + 3
		default:
			b.WriteByte(src[i])
		}
	}
	return b.String()
}
