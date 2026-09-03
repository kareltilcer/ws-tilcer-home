package statusreport

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// received is one captured ingest request.
type received struct {
	body   wireEvent
	key    string
	ctype  string
	method string
}

// ingest starts a stand-in for status.tilcer.cz that decodes each POST onto a
// channel. The channel is buffered so a test that only asserts the first event
// does not deadlock the ones behind it.
func ingest(t *testing.T, status int) (url string, got chan received) {
	t.Helper()
	got = make(chan received, 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var e wireEvent
		_ = json.Unmarshal(raw, &e)
		select {
		case got <- received{body: e, key: r.Header.Get("X-Ingest-Key"), ctype: r.Header.Get("Content-Type"), method: r.Method}:
		default:
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, got
}

func waitFor(t *testing.T, got chan received) received {
	t.Helper()
	select {
	case r := <-got:
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("no event reached the ingest endpoint")
		return received{}
	}
}

func TestCapturePostsTheDocumentedEnvelope(t *testing.T) {
	url, got := ingest(t, http.StatusAccepted)
	c := New(url, "ik_test", WithEnvironment("prod"), WithRelease("home@1.2.3"))

	c.Capture(Report{
		Message: "documents: reading the object failed",
		Stack:   "goroutine 1 [running]:\nmain.main()",
		Context: map[string]any{"document_id": "doc_1", "attempts": 2},
	})

	r := waitFor(t, got)
	if r.method != http.MethodPost {
		t.Errorf("method = %q, want POST", r.method)
	}
	if r.key != "ik_test" {
		t.Errorf("X-Ingest-Key = %q, want ik_test", r.key)
	}
	if r.ctype != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", r.ctype)
	}
	if r.body.Message != "documents: reading the object failed" {
		t.Errorf("message = %q", r.body.Message)
	}
	// Absent Level would mean "error" server-side anyway, but sending it keeps
	// the wire payload readable in a proxy log.
	if r.body.Level != LevelError {
		t.Errorf("level = %q, want %q", r.body.Level, LevelError)
	}
	if r.body.Environment != "prod" || r.body.Release != "home@1.2.3" {
		t.Errorf("tags = %q/%q, want prod/home@1.2.3", r.body.Environment, r.body.Release)
	}
	if !strings.HasPrefix(r.body.Stack, "goroutine 1") {
		t.Errorf("stack = %q", r.body.Stack)
	}
	// Context values are rendered to strings so no caller's type can make the
	// whole event unserialisable.
	if r.body.Context["document_id"] != "doc_1" || r.body.Context["attempts"] != "2" {
		t.Errorf("context = %#v", r.body.Context)
	}
	if _, err := time.Parse(time.RFC3339, r.body.OccurredAt); err != nil {
		t.Errorf("occurred_at %q is not RFC3339: %v", r.body.OccurredAt, err)
	}
}

func TestCaptureSyncSendsBeforeItReturns(t *testing.T) {
	url, got := ingest(t, http.StatusAccepted)
	New(url, "ik_test").CaptureSync(Report{Message: "home failed to start: boom", Level: LevelFatal})

	select {
	case r := <-got:
		if r.body.Level != LevelFatal {
			t.Errorf("level = %q, want %q", r.body.Level, LevelFatal)
		}
	default:
		t.Fatal("CaptureSync returned before the event was sent — the os.Exit after it would have lost the report")
	}
}

// A disabled deployment must be a working no-op rather than a nil dereference:
// New returns nil for either half missing, and the composition root wires the
// same calls regardless.
func TestDisabledClientIsASafeNoOp(t *testing.T) {
	for _, tc := range []struct{ name, url, key string }{
		{"neither", "", ""},
		{"no key", "https://status.tilcer.cz/api/ingest/home", ""},
		{"no url", "", "ik_test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New(tc.url, tc.key)
			if c != nil {
				t.Fatalf("New(%q, %q) = %v, want nil", tc.url, tc.key, c)
			}
			c.Capture(Report{Message: "boom"})
			c.CaptureSync(Report{Message: "boom"})
			c.Recover() // no panic in flight: returns without re-panicking
		})
	}
}

func TestEmptyMessageIsDropped(t *testing.T) {
	url, got := ingest(t, http.StatusAccepted)
	c := New(url, "ik_test")
	c.CaptureSync(Report{Message: ""})
	select {
	case r := <-got:
		t.Fatalf("an empty message was sent: %#v", r.body)
	default:
	}
}

// Every ingest failure is swallowed. The statuses below are the documented ones:
// a bad key, an unknown site, an oversized body, an invalid payload, a rate
// limit. None of them may reach the caller in any form.
func TestIngestFailuresAreSwallowed(t *testing.T) {
	for _, status := range []int{
		http.StatusUnauthorized, http.StatusNotFound, http.StatusRequestEntityTooLarge,
		http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusInternalServerError,
	} {
		url, _ := ingest(t, status)
		New(url, "ik_test").CaptureSync(Report{Message: "boom"})
	}
	// An endpoint that is not listening at all — the network-failure case.
	New("http://127.0.0.1:1/api/ingest/home", "ik_test").CaptureSync(Report{Message: "boom"})
}

// …but a 429 is the one status worth reading, because it is the server talking
// about the events STILL TO COME. Its rate limit is per SITE and this process
// shares that site with every browser tab home has open, so it can be refused
// while comfortably inside its own bucket — and then the bucket alone would have
// it keep posting one report per second into the refusal for the whole storm.
// integration.md: honour Retry-After, back off, then drop.
func TestA429BacksOffTheAsyncPath(t *testing.T) {
	var hits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	count := func() int { mu.Lock(); defer mu.Unlock(); return hits }

	c := New(srv.URL, "ik_test")
	c.CaptureSync(Report{Message: "the storm starts"})
	if count() != 1 {
		t.Fatalf("the first event did not reach the endpoint: %d requests", count())
	}
	if !c.muted(time.Now()) {
		t.Fatal("a 429 did not silence the client — it would keep posting into the refusal")
	}
	if c.muted(time.Now().Add(121 * time.Second)) {
		t.Error("the back-off outlasted the Retry-After the server asked for")
	}

	// Dropped BEFORE the bucket: an event the server has already refused must not
	// spend the allowance the events after the back-off will need.
	before := c.limiter.tokens
	c.Capture(Report{Message: "and continues"})
	if c.limiter.tokens != before {
		t.Errorf("a muted event spent a token: %v → %v", before, c.limiter.tokens)
	}
	if count() != 1 {
		t.Errorf("a muted event was still posted: %d requests", count())
	}

	// CaptureSync ignores the back-off. Its only callers are a dying process's,
	// and a back-off that swallowed those would be the one drop that matters.
	c.CaptureSync(Report{Message: "home failed to start: boom"})
	if count() != 2 {
		t.Errorf("the back-off swallowed a dying process's report: %d requests", count())
	}
}

// Retry-After is documented as whole seconds. Anything else falls back, and a
// header this client cannot verify may not silence the process for a day.
func TestRetryAfterIsHonouredAndClamped(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{
		{"120", 120 * time.Second},
		{" 30 ", 30 * time.Second},
		{"", defaultMute},
		{"Wed, 21 Oct 2026 07:28:00 GMT", defaultMute}, // the HTTP-date form, unused by status
		{"0", defaultMute},
		{"-5", defaultMute},
		{"86400", maxMute},
	} {
		if got := retryAfter(tc.header); got != tc.want {
			t.Errorf("retryAfter(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}

func TestRecoverReportsFatalThenRepanics(t *testing.T) {
	url, got := ingest(t, http.StatusAccepted)
	c := New(url, "ik_test")

	func() {
		defer func() {
			if p := recover(); p == nil {
				t.Error("Recover swallowed the panic — the process would have carried on after a fatal crash")
			} else if p != "boom" {
				t.Errorf("re-panicked with %v, want the original value", p)
			}
		}()
		defer c.Recover()
		panic("boom")
	}()

	r := waitFor(t, got)
	if r.body.Level != LevelFatal {
		t.Errorf("level = %q, want %q", r.body.Level, LevelFatal)
	}
	if r.body.Message != "panic: boom" {
		t.Errorf("message = %q, want %q", r.body.Message, "panic: boom")
	}
	if !strings.Contains(r.body.Stack, "TestRecoverReportsFatalThenRepanics") {
		t.Errorf("stack does not name the panicking frame: %q", r.body.Stack)
	}
}

// Recover on a goroutine that is NOT panicking must return, not re-panic with
// nil — otherwise every clean shutdown would end in a crash.
func TestRecoverIsANoOpWithoutAPanic(t *testing.T) {
	New("http://127.0.0.1:1/api/ingest/home", "ik_test").Recover()
}

// …and a REAL panic through a DISABLED client must still re-panic. This is the
// only path `defer reporter.Recover()` in main takes until the status site
// exists, and TestDisabledClientIsASafeNoOp only exercises the no-panic half of
// it: a Recover that touched c before CaptureSync's nil check would replace a
// crash's real stderr trace with a nil dereference raised from inside the panic
// handler, on every deployment that has not configured reporting yet.
func TestRecoverRepanicsWithReportingOff(t *testing.T) {
	var c *Client // New("", "") — reporting disabled
	func() {
		defer func() {
			if p := recover(); p != "boom" {
				t.Errorf("re-panicked with %v, want the original value", p)
			}
		}()
		defer c.Recover()
		panic("boom")
	}()
}

func TestPayloadIsCapped(t *testing.T) {
	url, got := ingest(t, http.StatusAccepted)
	c := New(url, "ik_test")

	ctx := map[string]any{}
	for i := range maxContextKeys * 2 {
		ctx[string(rune('a'+i%26))+strings.Repeat("x", i)] = strings.Repeat("v", maxContextValueBytes*2)
	}
	c.CaptureSync(Report{
		Message: strings.Repeat("m", maxMessageBytes*2),
		Stack:   strings.Repeat("s", maxStackBytes*2),
		Context: ctx,
	})

	r := waitFor(t, got)
	if len(r.body.Message) > maxMessageBytes+len("…[truncated]") {
		t.Errorf("message not capped: %d bytes", len(r.body.Message))
	}
	if len(r.body.Stack) > maxStackBytes+len("…[truncated]") {
		t.Errorf("stack not capped: %d bytes", len(r.body.Stack))
	}
	if len(r.body.Context) > maxContextKeys {
		t.Errorf("context has %d keys, want at most %d", len(r.body.Context), maxContextKeys)
	}
	for k, v := range r.body.Context {
		if len(v.(string)) > maxContextValueBytes+len("…[truncated]") {
			t.Errorf("context[%q] not capped: %d bytes", k, len(v.(string)))
		}
	}
}

// The cut must land on a rune boundary: half a multi-byte character is invalid
// UTF-8, and encoding/json replaces it — so a Czech message would arrive on the
// board ending in a replacement character with nothing to say why.
func TestTruncateCutsOnARuneBoundary(t *testing.T) {
	// "ěěěě" is four two-byte runes; cutting at 5 bytes must fall back to 4.
	got := truncate("ěěěě", 5)
	if got != "ěě"+"…[truncated]" {
		t.Errorf("truncate = %q, want %q", got, "ěě…[truncated]")
	}
	if s := truncate("short", 99); s != "short" {
		t.Errorf("an under-cap string was altered: %q", s)
	}
}

func TestBucketDropsPastTheBurst(t *testing.T) {
	b := &bucket{tokens: 2, burst: 2, rate: 1, last: time.Now()}
	now := b.last
	if !b.allow(now) || !b.allow(now) {
		t.Fatal("the first two events inside the burst were dropped")
	}
	if b.allow(now) {
		t.Error("the third event was allowed — the bucket does not bound anything")
	}
	// One token per second, and the refill never exceeds the burst.
	if !b.allow(now.Add(time.Second)) {
		t.Error("a token did not refill after a second")
	}
	if b.tokens > b.burst {
		t.Errorf("tokens = %v, above the burst of %v", b.tokens, b.burst)
	}
	// A nil bucket allows everything, so a hand-built Client is never muted.
	var nilBucket *bucket
	if !nilBucket.allow(now) {
		t.Error("a nil bucket refused an event")
	}
}

func TestCaptureIsRateLimited(t *testing.T) {
	c := New("http://127.0.0.1:1/api/ingest/home", "ik_test")
	c.limiter = &bucket{tokens: 1, burst: 1, rate: 0, last: time.Now()}
	c.Capture(Report{Message: "first"})
	c.Capture(Report{Message: "second"})
	if c.limiter.tokens != 0 {
		t.Errorf("tokens = %v, want 0 — the second event should have been dropped, not queued", c.limiter.tokens)
	}
}

// A Coolify variable pasted with a trailing newline is the most ordinary way this
// client is misconfigured, and it is the one misconfiguration nothing would ever
// report: url.Parse refuses the request inside send, which drops in silence, while
// config — which trims — has already logged "crash reporting ready". Trimming in
// New is what keeps the two readers of one environment agreeing.
func TestNewTrimsItsArguments(t *testing.T) {
	url, got := ingest(t, http.StatusAccepted)
	c := New(" "+url+"\n", "\tik_test\n", WithEnvironment(" prod\n"), WithRelease("\thome@1.2.3 "))
	if c == nil {
		t.Fatal("New returned nil for a value that only needed trimming")
	}
	c.CaptureSync(Report{Message: "boom"})

	r := waitFor(t, got)
	if r.key != "ik_test" {
		t.Errorf("X-Ingest-Key = %q, want the trimmed key", r.key)
	}
	if r.body.Environment != "prod" || r.body.Release != "home@1.2.3" {
		t.Errorf("tags = %q/%q, want prod/home@1.2.3", r.body.Environment, r.body.Release)
	}
}

// …and a variable that is nothing BUT whitespace is unset, exactly as config's
// strDefault reads it — not a client pointed at an unusable endpoint.
func TestNewTreatsWhitespaceOnlyAsUnset(t *testing.T) {
	if c := New("  \n", "ik_test"); c != nil {
		t.Errorf("New with a whitespace-only url = %v, want nil", c)
	}
	if c := New("https://status.tilcer.cz/api/ingest/home", " \t "); c != nil {
		t.Errorf("New with a whitespace-only key = %v, want nil", c)
	}
}

// Which keys survive the cap is a property of the EVENT, not of Go's randomised
// map order: a group collects many events of one error, and a context block whose
// membership changes per occurrence reads as a difference that means something.
func TestBoundContextKeepsTheSameKeysEveryTime(t *testing.T) {
	in := map[string]any{}
	for i := range maxContextKeys * 3 {
		in[fmt.Sprintf("k%03d", i)] = i
	}
	first := boundContext(in)
	if len(first) != maxContextKeys {
		t.Fatalf("kept %d keys, want %d", len(first), maxContextKeys)
	}
	for range 20 {
		again := boundContext(in)
		if !maps.Equal(toStrings(first), toStrings(again)) {
			t.Fatalf("the kept keys changed between calls:\n%v\n%v", first, again)
		}
	}
	// Sorted, so the surviving set is the one a reader can predict.
	if _, ok := first["k000"]; !ok {
		t.Errorf("the lowest key was dropped: %v", first)
	}
	if _, ok := first[fmt.Sprintf("k%03d", maxContextKeys)]; ok {
		t.Errorf("a key past the cap survived: %v", first)
	}
}

func toStrings(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = fmt.Sprint(v)
	}
	return out
}
