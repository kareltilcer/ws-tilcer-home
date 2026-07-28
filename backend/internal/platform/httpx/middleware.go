package httpx

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// requestHeaderID is the inbound/outbound correlation header.
const requestHeaderID = "X-Request-Id"

// statusRecorder captures the response status for the access log. It forwards
// optional ResponseWriter capabilities via Unwrap and ReadFrom so wrapping the
// writer does not defeat websocket upgrades, streaming, or the sendfile fast
// path for static assets.
type statusRecorder struct {
	http.ResponseWriter
	status    int
	wrote     bool
	onUpgrade func() // fired once, from Hijack, after the 101 handshake is taken over
}

func (r *statusRecorder) WriteHeader(code int) {
	// A 1xx response (e.g. 101 Switching Protocols) is informational and does
	// not complete the request: when a websocket handshake is written but the
	// hijack then fails, net/http reports the real final status afterwards. Only
	// a final (>=200) status commits the recorded value, so that failure is
	// logged as its true 500 rather than a phantom 101.
	if !r.wrote && code >= 200 {
		r.status = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.status = http.StatusOK
		r.wrote = true
	}
	return r.ResponseWriter.Write(b)
}

// ReadFrom forwards to the underlying writer's io.ReaderFrom so http.FileServer
// and http.ServeContent keep the sendfile zero-copy fast path when serving
// static SPA assets; without it every static byte is copied through userspace.
// Falls back to io.Copy when the underlying writer has no ReaderFrom.
func (r *statusRecorder) ReadFrom(src io.Reader) (int64, error) {
	if !r.wrote {
		r.status = http.StatusOK
		r.wrote = true
	}
	if rf, ok := r.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}
	return io.Copy(r.ResponseWriter, src)
}

// Unwrap exposes the wrapped ResponseWriter so optional interfaces are found on
// the real writer rather than falsely advertised by this wrapper.
// coder/websocket's Accept and http.ResponseController walk the Unwrap chain to
// locate Hijacker (for the /ws upgrade) and Flusher (for streaming). Statically
// implementing Hijacker here would claim support even on transports that cannot
// hijack (HTTP/2, test recorders), turning a clean 501 into a late 500. Hijack
// is instead added conditionally by hijackRecorder, only over a transport that
// can actually hijack (see Logger).
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// hijackRecorder adds Hijack to a statusRecorder. Logger wraps the recorder in
// this type only when the underlying transport is itself hijackable, preserving
// the Unwrap-based discovery above: over a non-hijackable transport the bare
// statusRecorder is used, so coder/websocket still reports the upgrade as
// unsupported (a clean 501) instead of failing late with a 500. Interposing on
// Hijack lets the access log fire the instant the handshake is taken over — and
// only when it actually succeeds, so a hijack that fails after the 101 is not
// logged as a phantom success.
type hijackRecorder struct {
	*statusRecorder
}

func (r *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		// Unreachable: Logger only wraps a hijackable writer. Guard anyway so a
		// future miswiring degrades to a 500 rather than panicking mid-handshake.
		return nil, nil, http.ErrNotSupported
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, nil, err
	}
	if r.onUpgrade != nil {
		fn := r.onUpgrade
		r.onUpgrade = nil // log the upgrade at most once
		fn()
	}
	return conn, brw, nil
}

// RequestID mints (or accepts) a request id, stores request metadata in the
// context, and echoes the id on the response. The site defaults to the given
// value and is stamped onto every audit event.
func RequestID(site string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(requestHeaderID)
			if id == "" {
				id = idgen.New()
			}
			w.Header().Set(requestHeaderID, id)
			info := reqctx.RequestInfo{
				RequestID: id,
				IP:        clientIP(r),
				UserAgent: r.UserAgent(),
				Site:      site,
			}
			next.ServeHTTP(w, r.WithContext(reqctx.WithRequest(r.Context(), info)))
		})
	}
}

// Logger emits one structured access log line per request carrying the request
// id. For a websocket upgrade the line is written when the 101 handshake is
// hijacked, so a live connection is visible immediately and latency_ms reflects
// the handshake rather than the open-ended session that follows.
func Logger(l *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			logged := false
			emit := func(status int) {
				id := ""
				if info, ok := reqctx.RequestFrom(r.Context()); ok {
					id = info.RequestID
				}
				l.Info("request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", status,
					"latency_ms", time.Since(start).Milliseconds(),
					"request_id", id,
				)
			}
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			rec.onUpgrade = func() {
				logged = true
				emit(http.StatusSwitchingProtocols)
			}
			// Add Hijack only over a hijackable transport so non-hijackable ones
			// keep coder/websocket's clean 501 (see hijackRecorder). The upgrade
			// is then logged from Hijack — after the handshake is taken over, so a
			// hijack that fails post-101 records its real status, not a phantom 101.
			var rw http.ResponseWriter = rec
			if _, ok := w.(http.Hijacker); ok {
				rw = &hijackRecorder{rec}
			}
			next.ServeHTTP(rw, r)
			if !logged {
				emit(rec.status)
			}
		})
	}
}

// Recover converts a panic into a 500 and logs it with the request id, so one
// bad handler cannot take the process down.
func Recover(l *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					id := ""
					if info, ok := reqctx.RequestFrom(r.Context()); ok {
						id = info.RequestID
					}
					l.Error("panic recovered", "panic", p, "path", r.URL.Path, "request_id", id)
					WriteError(w, ErrInternal(""))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP extracts a best-effort client IP, honouring X-Forwarded-For (Coolify
// terminates TLS and proxies).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(first)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
