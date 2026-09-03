// Package statusreport posts crash and error events to the status.tilcer.cz
// ingest API (docs/integration.md in ws-tilcer-status).
//
// It is derived from that repository's copy-in Go helper and trimmed to what
// this service actually wires, because internal/arch's TestNoDeadCode fails the
// build on an unreachable function — a vendored convenience nothing calls is a
// vendored convenience nobody maintains.
//
// ⚠ EVERY PATH IN HERE FAILS SAFE, WHICH IS THE WHOLE CONTRACT. A monitoring
// client that can take down the app it monitors is worse than no monitoring at
// all, so: Capture never blocks the caller, no method ever panics or returns an
// error, a nil *Client is a working no-op, and an ingest failure — a 4xx, a 5xx,
// a DNS failure, a timeout — is dropped in silence. Nothing here logs, which is
// also what keeps the slog forwarder in loghandler.go from feeding itself.
//
// The one place that silence is paid for is diagnosis: a misconfigured key
// produces no signal anywhere. The boot line in cmd/home/main.go says whether
// reporting is on, and that line is the only confirmation there will be.
package statusreport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Level values accepted by the ingest API. A group tracks the highest level it
// has seen, so these are the sort order too.
const (
	// LevelFatal is reserved for a panic that ends the process. A panic this
	// service RECOVERS from — an HTTP handler's, a scheduler job's — is an
	// `error`: the request failed, the process did not.
	LevelFatal = "fatal"
	LevelError = "error"
)

// Payload caps. The server refuses a body over STATUS_MAX_INGEST_BYTES (64 KB by
// default) with a 413 — and because the client is deliberately silent, a report
// that trips that limit is a report nobody ever learns about. These keep an
// event comfortably inside it without any call site having to think about it.
const (
	maxMessageChars      = 2000
	maxStackBytes        = 8 << 10
	maxContextKeys       = 32
	maxContextValueChars = 512
)

// sendTimeout bounds one ingest round trip, and it is ONE number rather than two
// that have to agree: http.Client.Timeout and the request context both cover the
// whole exchange, so a pair of different values means the larger one can never
// fire and is read by the next person as a bound that does something.
const sendTimeout = 5 * time.Second

// maxDrainBytes is how much of an ingest response is read before the body is
// closed. A body left undrained cannot be reused, so every report after the first
// would pay a fresh TCP + TLS handshake; the documented 202 body is a two-field
// JSON object, and the cap is there so an unexpectedly chatty error page cannot
// turn a dropped failure into work.
const maxDrainBytes = 4 << 10

// The 429 back-off. status's rate limit is per SITE, and this process shares
// that site with every browser tab home has open — so it can be refused while
// still comfortably inside its own bucket, and the bucket alone cannot get it to
// stop. defaultMute is what integration.md's Retry-After carries in practice and
// what is used when the header is missing or unreadable; maxMute is there
// because a header this client cannot verify must not be able to silence the
// process for a day.
const (
	defaultMute = 60 * time.Second
	maxMute     = 5 * time.Minute
)

// Client posts events to one site's ingest endpoint.
type Client struct {
	url         string // full ingest URL, e.g. https://status.tilcer.cz/api/ingest/home
	key         string // per-site ingest key (X-Ingest-Key)
	environment string
	release     string
	http        *http.Client
	limiter     *bucket

	// mu guards mutedUntil, which send writes from its own goroutine and Capture
	// reads from the caller's.
	mu         sync.Mutex
	mutedUntil time.Time
}

// Option configures a Client.
type Option func(*Client)

// WithEnvironment sets the environment tag sent on every event ("prod"/"dev").
func WithEnvironment(env string) Option {
	return func(c *Client) { c.environment = strings.TrimSpace(env) }
}

// WithRelease sets the release tag sent on every event ("home@2026.36.1").
func WithRelease(rel string) Option { return func(c *Client) { c.release = strings.TrimSpace(rel) } }

// New builds a client for a fully-qualified ingest URL and per-site key. An
// empty url or key returns nil, which every method below treats as "reporting is
// off" — so the composition root wires the same call whether or not the
// deployment configured status.
//
// ⚠ EVERY ARGUMENT IS TRIMMED HERE, and that is a correctness rule rather than a
// courtesy. A Coolify variable pasted with a trailing newline makes url.Parse
// refuse the request inside send, which — this client being silent by contract —
// drops every event forever with nothing anywhere saying so; a key with the same
// contamination is an invalid header value and fails the same way. config.status
// trims the same two variables before it validates them, so without this the two
// readers of one environment disagree and the boot line says "crash reporting
// ready" about a client that cannot send.
func New(url, key string, opts ...Option) *Client {
	url, key = strings.TrimSpace(url), strings.TrimSpace(key)
	if url == "" || key == "" {
		return nil
	}
	c := &Client{
		url:  url,
		key:  key,
		http: &http.Client{Timeout: sendTimeout},
		// The server's own per-site limit is 60/min with a burst of 120, and past
		// it events are refused with a Retry-After. Mirroring it HERE is what stops
		// an error storm — a mirror pass failing on every object, a listener
		// panicking on every event — from turning into a goroutine and a socket per
		// log line, spent to be told 429. The excess is dropped rather than queued,
		// which is the documented client behaviour.
		limiter: &bucket{tokens: 120, burst: 120, rate: 1, last: time.Now()},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Report is one event to send.
type Report struct {
	// Message is required and drives grouping; an empty one is dropped. The
	// server normalises it (numbers, hex, UUIDs, quoted strings and paths are
	// stripped) before hashing, so "document 41 not found" and "document 99 not
	// found" land in one group.
	Message string
	// Level defaults to LevelError when empty.
	Level string
	// Stack, when present, contributes its first frame to the default fingerprint.
	Stack string
	// Context is free-form metadata shown per event: ids, keys, counts, a route.
	// ⚠ NOT USER CONTENT. status is read by Karel's admin session, which is a
	// different lock from the one on a member's private note, and a value that
	// travels here has left home's privacy model behind (widget.md §5).
	//
	// Nothing here can enforce that, because almost every Report this service
	// sends is built by the slog forwarder out of a log line's attrs — so the rule
	// belongs at the `logger.Error` call, and loghandler.go's NewLogHandler doc is
	// where it is written for the person making one.
	Context map[string]any
}

// Capture sends one event and returns immediately. It is rate-limited: past the
// bucket the event is DROPPED, not queued.
func (c *Client) Capture(r Report) {
	if c == nil || r.Message == "" {
		return
	}
	// Checked BEFORE the bucket: an event the server has already told us it will
	// refuse must not also spend the allowance that the events after the back-off
	// will need.
	if c.muted(time.Now()) {
		return
	}
	if !c.limiter.allow(time.Now()) {
		return
	}
	e := c.event(r)
	go c.send(e)
}

// CaptureSync sends one event on the calling goroutine, for the moments where
// returning first means never sending at all — the process is about to exit.
// It is deliberately NOT rate-limited AND NOT MUTED: a bucket or a back-off that
// swallowed a process's dying report would be the one drop that matters.
func (c *Client) CaptureSync(r Report) {
	if c == nil || r.Message == "" {
		return
	}
	c.send(c.event(r))
}

// Recover is a deferred panic handler for a goroutine whose panic ends the
// process: `defer sr.Recover()`. It reports the panic as fatal SYNCHRONOUSLY —
// there is no later — and then re-panics, so the crash keeps its normal Go
// semantics and its normal stderr trace.
func (c *Client) Recover() {
	p := recover()
	if p == nil {
		return
	}
	c.CaptureSync(Report{
		Message: fmt.Sprintf("panic: %v", p),
		Level:   LevelFatal,
		Stack:   string(debug.Stack()),
	})
	panic(p)
}

// event builds the wire payload, applying the size caps.
func (c *Client) event(r Report) wireEvent {
	level := r.Level
	if level == "" {
		level = LevelError
	}
	return wireEvent{
		Message:     truncate(r.Message, maxMessageChars),
		Level:       level,
		Stack:       truncate(r.Stack, maxStackBytes),
		Environment: c.environment,
		Release:     c.release,
		Context:     boundContext(r.Context),
		// Stamped at capture time rather than left to the server's receipt time:
		// Capture hands the send to a goroutine, and an error storm can put real
		// distance between the two.
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// wireEvent is the ingest payload (the CrashReport schema).
type wireEvent struct {
	Message     string         `json:"message"`
	Level       string         `json:"level,omitempty"`
	Stack       string         `json:"stack,omitempty"`
	Environment string         `json:"environment,omitempty"`
	Release     string         `json:"release,omitempty"`
	Context     map[string]any `json:"context,omitempty"`
	OccurredAt  string         `json:"occurred_at,omitempty"`
}

// send posts one event and drops every failure, including its own.
func (c *Client) send(e wireEvent) {
	defer func() { _ = recover() }() // the reporter must never crash the app
	body, err := json.Marshal(e)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ingest-Key", c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}
	// Drained, then closed: net/http only returns a connection to the idle pool
	// once its body has been read to EOF, and an error storm is exactly when
	// paying a handshake per report is worst.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainBytes))
	_ = resp.Body.Close()
	// The one status worth reading. Every other failure is dropped and forgotten,
	// but a 429 is the server saying "not yet" about the events still to come, and
	// the documented client behaviour is to honour Retry-After and drop rather
	// than keep posting into a refusal (integration.md, "Responses & error
	// handling"). Server-side there is no CORS to hide the header behind, so
	// unlike the browser half this really does read what the server asked for.
	if resp.StatusCode == http.StatusTooManyRequests {
		c.mute(retryAfter(resp.Header.Get("Retry-After")))
	}
}

// muted reports whether a 429 has silenced the asynchronous path.
func (c *Client) muted(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return now.Before(c.mutedUntil)
}

// mute silences Capture for d. The longest standing deadline wins, so a short
// Retry-After arriving from a request that was already in flight cannot shorten
// a longer back-off the server has since asked for.
func (c *Client) mute(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if until := time.Now().Add(d); until.After(c.mutedUntil) {
		c.mutedUntil = until
	}
}

// retryAfter reads a 429's Retry-After (documented as whole seconds), falling
// back to defaultMute for anything missing or unreadable and clamping to
// maxMute.
func retryAfter(header string) time.Duration {
	secs, err := strconv.Atoi(strings.TrimSpace(header))
	if err != nil || secs <= 0 {
		return defaultMute
	}
	return min(time.Duration(secs)*time.Second, maxMute)
}

// boundContext caps the context block: at most maxContextKeys entries, each
// rendered as a string of at most maxContextValueChars. Rendering rather than
// passing values through also removes the one way a context value could fail the
// whole send — a type json.Marshal refuses, which would drop the event silently.
//
// The keys are SORTED before the cut, so which ones survive is a property of the
// event rather than of Go's randomised map order. A group collects many events of
// one error, and a context block whose membership changes per occurrence is the
// kind of difference an operator reads as meaningful when it is noise.
func boundContext(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	keys = keys[:min(len(keys), maxContextKeys)]
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		out[k] = truncate(fmt.Sprint(in[k]), maxContextValueChars)
	}
	return out
}

// truncate cuts s to at most n bytes, on a rune boundary, marking the cut. It
// counts BYTES because the caps exist to bound the request body; the rune
// boundary is so the JSON stays valid UTF-8 rather than ending in half a ř.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut] + "…[truncated]"
}

// utf8Start reports whether b starts a UTF-8 rune (i.e. is not a continuation
// byte 0b10xxxxxx).
func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// bucket is a token bucket: `rate` tokens per second up to `burst`.
type bucket struct {
	mu     sync.Mutex
	tokens float64
	burst  float64
	rate   float64
	last   time.Time
}

// allow takes one token, refilling for the time since the last call. A nil
// bucket allows everything, so a hand-built Client is never silently muted.
func (b *bucket) allow(now time.Time) bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if elapsed := now.Sub(b.last); elapsed > 0 {
		b.last = now
		b.tokens = min(b.burst, b.tokens+elapsed.Seconds()*b.rate)
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
