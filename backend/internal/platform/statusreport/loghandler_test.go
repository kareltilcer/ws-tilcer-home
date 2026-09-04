package statusreport

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// forwarding builds a logger whose records go to a discard JSON handler and, at
// Error and above, to a stand-in ingest endpoint. It returns both so a test can
// assert the local line was still written.
func forwarding(t *testing.T) (*slog.Logger, *bytes.Buffer, chan received) {
	t.Helper()
	url, got := ingest(t, http.StatusAccepted)
	var local bytes.Buffer
	next := slog.NewJSONHandler(&local, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(NewLogHandler(next, New(url, "ik_test"))), &local, got
}

func TestOnlyErrorRecordsAreForwarded(t *testing.T) {
	logger, local, got := forwarding(t)

	logger.Info("request", "status", 200)
	logger.Warn("scheduler: job failed", "job", "chat.drain")
	logger.Error("admin: load trigger rules", "err", "database is locked")

	r := waitFor(t, got)
	if !strings.HasPrefix(r.body.Message, "admin: load trigger rules") {
		t.Fatalf("the wrong record was forwarded: %q", r.body.Message)
	}
	select {
	case extra := <-got:
		t.Errorf("a record below Error was forwarded: %q", extra.body.Message)
	default:
	}
	// The local log is authoritative and must carry all three regardless.
	for _, want := range []string{"request", "scheduler: job failed", "admin: load trigger rules"} {
		if !strings.Contains(local.String(), want) {
			t.Errorf("the local log lost %q:\n%s", want, local.String())
		}
	}
}

// The err text joins the message because the message is what the server
// fingerprints: without it every failure of one call site collapses into a
// single group whose title names the call and not the fault.
func TestErrAndPanicJoinTheMessage(t *testing.T) {
	for _, tc := range []struct {
		name, key, want string
	}{
		{"err", "err", "admin: load trigger rules: database is locked"},
		{"panic", "panic", "admin: load trigger rules: database is locked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger, _, got := forwarding(t)
			logger.Error("admin: load trigger rules", tc.key, "database is locked")
			r := waitFor(t, got)
			if r.body.Message != tc.want {
				t.Errorf("message = %q, want %q", r.body.Message, tc.want)
			}
			if _, dup := r.body.Context[tc.key]; dup {
				t.Errorf("%q is in the context as well as the message: %#v", tc.key, r.body.Context)
			}
		})
	}
}

// `err` wins over `panic` when a line carries both, so the cause named in the
// group title is the specific one.
func TestErrWinsOverPanic(t *testing.T) {
	logger, _, got := forwarding(t)
	logger.Error("worker died", "panic", "runtime error", "err", "context canceled")
	if r := waitFor(t, got); r.body.Message != "worker died: context canceled" {
		t.Errorf("message = %q", r.body.Message)
	}
}

// A `stack` attr becomes the report's stack FIELD, where the server reads its
// first frame for the default fingerprint — not another context string.
func TestStackAttrBecomesTheStackField(t *testing.T) {
	logger, _, got := forwarding(t)
	logger.Error("panic recovered", "panic", "nil map", "path", "/api/todo",
		"request_id", "req_7", "stack", "goroutine 42 [running]:\nhome/todo.Add()")

	r := waitFor(t, got)
	if !strings.HasPrefix(r.body.Stack, "goroutine 42") {
		t.Errorf("stack = %q", r.body.Stack)
	}
	if _, dup := r.body.Context["stack"]; dup {
		t.Errorf("the stack is duplicated in the context: %#v", r.body.Context)
	}
	if r.body.Context["path"] != "/api/todo" || r.body.Context["request_id"] != "req_7" {
		t.Errorf("context = %#v, want the path and the request id", r.body.Context)
	}
}

// Everything else becomes context, with values rendered the way slog renders
// them so a number stays readable.
func TestOtherAttrsBecomeContext(t *testing.T) {
	logger, _, got := forwarding(t)
	logger.Error("documents: mirror pass cannot list the primary bucket", "objects", 412, "dry_run", true)

	r := waitFor(t, got)
	if r.body.Context["objects"] != "412" || r.body.Context["dry_run"] != "true" {
		t.Errorf("context = %#v", r.body.Context)
	}
}

// The three special keys are special only at the TOP level. A `stack` inside a
// group is somebody's data, not the record's stack trace.
func TestGroupedAttrsAreNotSpecialAndKeepTheirPath(t *testing.T) {
	logger, _, got := forwarding(t)
	logger.With("module", "chat").WithGroup("upload").
		Error("chat: storing an attachment failed", "stack", "not-a-trace", "err", "no space")

	r := waitFor(t, got)
	if r.body.Message != "chat: storing an attachment failed" {
		t.Errorf("a grouped err was lifted into the message: %q", r.body.Message)
	}
	if r.body.Stack != "" {
		t.Errorf("a grouped stack was lifted into the stack field: %q", r.body.Stack)
	}
	if r.body.Context["upload.stack"] != "not-a-trace" || r.body.Context["upload.err"] != "no space" {
		t.Errorf("context = %#v, want dotted upload.* keys", r.body.Context)
	}
	// WithAttrs history survives: a handler cannot read attrs back out of the one
	// it wraps, so the forwarder keeps its own copy.
	if r.body.Context["module"] != "chat" {
		t.Errorf("the With() attr is missing from the report: %#v", r.body.Context)
	}
}

// slog.Group values are flattened to dotted keys rather than dropped.
func TestInlineGroupValuesAreFlattened(t *testing.T) {
	logger, _, got := forwarding(t)
	logger.Error("boom", slog.Group("doc", "id", "d1", "bytes", 9))

	r := waitFor(t, got)
	if r.body.Context["doc.id"] != "d1" || r.body.Context["doc.bytes"] != "9" {
		t.Errorf("context = %#v", r.body.Context)
	}
}

// A nil client means the wrapper is skipped entirely: the caller gets the
// handler it passed in, so a deployment with no status configuration pays
// nothing per log line.
func TestNilClientReturnsTheHandlerUnchanged(t *testing.T) {
	next := slog.NewJSONHandler(&bytes.Buffer{}, nil)
	if got := NewLogHandler(next, nil); got != slog.Handler(next) {
		t.Errorf("NewLogHandler(next, nil) = %T, want the handler itself", got)
	}
}

// The forwarder must not change what Coolify sees. This pins the JSON line for
// an error record end to end, because the local log is the authoritative record
// and status is the copy.
func TestTheLocalLineIsUnchanged(t *testing.T) {
	logger, local, got := forwarding(t)
	logger.With("module", "notes").Error("notes: image sweep cannot count notes", "err", "disk full")
	waitFor(t, got)

	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(local.Bytes()), &line); err != nil {
		t.Fatalf("the local line is not JSON: %v\n%s", err, local.String())
	}
	if line["msg"] != "notes: image sweep cannot count notes" || line["err"] != "disk full" ||
		line["module"] != "notes" || line["level"] != "ERROR" {
		t.Errorf("the local line changed shape: %#v", line)
	}
}

// Enabled delegates, so a handler configured above Error never reaches the
// forwarder — the report follows what is actually logged rather than a second,
// silently diverging threshold.
func TestEnabledDelegates(t *testing.T) {
	next := slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelError})
	h := NewLogHandler(next, New("http://127.0.0.1:1/api/ingest/home", "ik_test"))
	if h.Enabled(t.Context(), slog.LevelWarn) {
		t.Error("Enabled(Warn) = true under an Error-level handler")
	}
	if !h.Enabled(t.Context(), slog.LevelError) {
		t.Error("Enabled(Error) = false under an Error-level handler")
	}
}
