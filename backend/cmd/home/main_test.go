package main

import (
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/ws"
)

// TestWSRevalidation pins the boundary between auth's session verdict and the
// hub's, which nothing else in the suite crosses: the ws revalidation tests stub
// ws.Config.Revalidate outright, and the auth tests stop at SessionVerdict.
//
// ⚠ Every arm here is a household-wide outage if inverted, and a silent one.
// SessionLive mapped to RevalidationGone closes every socket on the first healthy
// tick; the default arm mapped to RevalidationGone does it on the first contended
// query — which is precisely what auth.SessionUnknown exists to prevent.
func TestWSRevalidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		verdict auth.SessionVerdict
		want    ws.Revalidation
		why     string
	}{
		{"live", auth.SessionLive, ws.RevalidationValid,
			"a healthy session must keep its socket"},
		{"gone", auth.SessionGone, ws.RevalidationGone,
			"a revoked, expired or closed session must lose its socket, or it keeps " +
				"receiving member-restricted payloads while 401 on every request"},
		{"unknown", auth.SessionUnknown, ws.RevalidationUnknown,
			"\"could not tell\" must KEEP the socket: a single-connection pool makes a " +
				"queued lookup fail under one long write, and closing on that would tear " +
				"down every socket in the household at once"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := wsRevalidation(tc.verdict); got != tc.want {
				t.Errorf("wsRevalidation(%v) = %v, want %v — %s", tc.verdict, got, tc.want, tc.why)
			}
		})
	}

	// An unrecognised verdict — a future arm added to auth and not to this switch —
	// must land on Unknown, not on Gone.
	if got := wsRevalidation(auth.SessionVerdict(99)); got != ws.RevalidationUnknown {
		t.Errorf("an unrecognised verdict mapped to %v, want RevalidationUnknown — a verdict "+
			"this switch does not know is a decision it could not take", got)
	}
}
