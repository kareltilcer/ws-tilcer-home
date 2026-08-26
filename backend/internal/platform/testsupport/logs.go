package testsupport

import (
	"bytes"
	"io"
	"log/slog"
	"sync"
)

// SyncBuffer is a concurrency-safe sink for a *slog.Logger under test.
//
// ⚠ The point of it is the mutex. The code being asserted on writes from a
// goroutine it owns — a websocket connection's handler, a preview worker — while
// the test reads, so a bare bytes.Buffer here is a data race the assertions
// would only sometimes lose, and only sometimes on CI.
//
// It lives here rather than in each package because it was already written twice
// verbatim, and the next package that wants to assert on a log line would have
// written a third.
type SyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *SyncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *SyncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// CaptureLogger returns a JSON logger and the buffer it writes to, for asserting
// on output that is the ONLY observable effect of a branch — a warning about a
// connection that is a bug state rather than a policy, say, which changes nothing
// else and can otherwise be deleted whole with the suite still green.
func CaptureLogger() (*slog.Logger, *SyncBuffer) {
	var b SyncBuffer
	return slog.New(slog.NewJSONHandler(&b, nil)), &b
}

// DiscardLogger keeps a component's own logging out of the test output.
func DiscardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
