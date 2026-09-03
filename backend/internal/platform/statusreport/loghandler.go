package statusreport

import (
	"context"
	"log/slog"
	"slices"
	"strings"
)

// NewLogHandler wraps next so that every record at slog.LevelError or above is
// also reported to status, then handed on unchanged. A nil client returns next
// itself, so a deployment with no ingest configuration pays nothing.
//
// ⚠ THE SEAM IS THE LOGGER BECAUSE THE CURATION ALREADY EXISTS. This service
// reserves Error for a fault that should not happen — a mirror pass that cannot
// list its bucket, a recovered panic, a session revoke that failed — and logs
// every soft or expected failure at Warn (`scheduler: job failed`, `audit
// notifier: listener failed`, a forecast that did not load). So "what belongs on
// the crash board" is a question the code has been answering at 54 call sites
// for ten versions, and one wrapper inherits all of them. The alternative — a
// sr.Capture beside each — is 54 places to forget on the 55th.
//
// What it deliberately does NOT do is invent a level. Everything forwarded here
// is `error`, including a recovered panic: the request died, the process did
// not. The only `fatal` this service sends is written by hand in main, for the
// two crashes that really do end it.
//
// ⚠ AND IT IS WHERE Report.Context's "NOT USER CONTENT" RULE IS ACTUALLY DECIDED.
// Every attr of every Error record is forwarded verbatim, so the rule cannot be
// applied here — the only place it can be applied is the `logger.Error` call, and
// the person writing one is not reading this file. The rule, spelled out where it
// bites: an Error line's attrs travel to a board read by Karel's ADMIN session,
// which is a different lock from the one on a member's private note (widget.md
// §5). Log the id, the key, the count, the route — never the title, the body, the
// filename or the message. Audited at the time of writing: all 54 sites carry ids
// and opaque object keys, and home has no API path with a user-authored segment,
// so `path` on a recovered panic is safe too.
func NewLogHandler(next slog.Handler, c *Client) slog.Handler {
	if c == nil {
		return next
	}
	return &logHandler{next: next, client: c}
}

// logHandler forwards error records to status and delegates everything else.
//
// It keeps its OWN copy of the attrs added through WithAttrs/WithGroup, because
// a slog.Handler is given no way to read them back out of the handler it wraps —
// so a `logger.With("module", "chat")` would otherwise reach the local log line
// and be missing from the report of the same event.
type logHandler struct {
	next   slog.Handler
	client *Client
	// pre is the WithAttrs history, already flattened and group-prefixed at the
	// moment each attr was added.
	pre []kv
	// groups is the currently open WithGroup stack, applied to record attrs.
	groups []string
}

// kv is one flattened attribute: a dotted key, a rendered value, and whether it
// sat at the record's top level — which is the only place err/panic/stack are
// read as the special keys they are, so a "stack" inside a group stays context.
type kv struct {
	key   string
	value string
	top   bool
}

func (h *logHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *logHandler) Handle(ctx context.Context, r slog.Record) error {
	// The local log line goes first and unconditionally: it is the authoritative
	// record, and Coolify captures it whether or not status is reachable.
	err := h.next.Handle(ctx, r)
	if r.Level >= slog.LevelError {
		h.forward(r)
	}
	return err
}

func (h *logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return &logHandler{
		next:   h.next.WithAttrs(attrs),
		client: h.client,
		pre:    flattenAll(slices.Clone(h.pre), h.groups, attrs),
		groups: h.groups,
	}
}

func (h *logHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h // slog contract: an empty group name is a no-op
	}
	return &logHandler{
		next:   h.next.WithGroup(name),
		client: h.client,
		pre:    h.pre,
		groups: append(slices.Clone(h.groups), name),
	}
}

// forward turns one error record into one crash report.
//
// It runs on the goroutine that logged, so it does the cheap half only: Capture
// hands the request itself to a goroutine. The recover is the belt to that
// brace — a slog.Handler that panics takes down its caller, which here would be
// any of 54 error paths, and a monitoring seam must not be able to convert a
// logged error into a crash.
func (h *logHandler) forward(r slog.Record) {
	defer func() { _ = recover() }()

	flat := slices.Clone(h.pre)
	r.Attrs(func(a slog.Attr) bool {
		flat = flattenAttr(flat, h.groups, a)
		return true
	})

	var errText, panicText, stack string
	reportCtx := make(map[string]any, len(flat))
	for _, e := range flat {
		if e.top {
			switch e.key {
			case "err":
				errText = e.value
				continue
			case "panic":
				panicText = e.value
				continue
			case "stack":
				stack = e.value
				continue
			}
		}
		reportCtx[e.key] = e.value
	}

	// The cause joins the MESSAGE rather than the context, because the message is
	// what the server fingerprints: "admin: load trigger rules" alone would
	// collapse every distinct failure of that call into one group, while its
	// normalisation still folds the variants of one cause back together.
	message := r.Message
	if cause := firstNonEmpty(errText, panicText); cause != "" {
		message += ": " + cause
	}

	h.client.Capture(Report{
		Message: message,
		Level:   LevelError,
		Stack:   stack,
		Context: reportCtx,
	})
}

// flattenAll appends every attr in attrs to dst under the open groups.
func flattenAll(dst []kv, groups []string, attrs []slog.Attr) []kv {
	for _, a := range attrs {
		dst = flattenAttr(dst, groups, a)
	}
	return dst
}

// flattenAttr appends one attr — recursing into slog.Group values so a nested
// group becomes dotted keys, which is the shape the report's context block has.
func flattenAttr(dst []kv, groups []string, a slog.Attr) []kv {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return dst // slog contract: an empty attr is ignored
	}
	if a.Value.Kind() == slog.KindGroup {
		inner := a.Value.Group()
		if len(inner) == 0 {
			return dst // slog contract: an empty group is elided, key and all
		}
		// A group with an empty key is inlined rather than nested.
		if a.Key == "" {
			return flattenAll(dst, groups, inner)
		}
		return flattenAll(dst, append(slices.Clone(groups), a.Key), inner)
	}
	if a.Key == "" {
		return dst
	}
	key := a.Key
	if len(groups) > 0 {
		key = strings.Join(groups, ".") + "." + key
	}
	return append(dst, kv{key: key, value: a.Value.String(), top: len(groups) == 0})
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
