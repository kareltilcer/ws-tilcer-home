package push_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	. "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// pushRouter mounts /api/push/** the way main.go does, with a fixed actor. The
// role is deliberately a parameter: every route here is open to EVERY member,
// `reader` included (D53), because a device is a personal preference.
func pushRouter(t *testing.T, sqldb *sql.DB, svc *Service, userID, role string) http.Handler {
	t.Helper()
	h := NewHandler(svc, sqldb, audit.NewSink())
	return testsupport.RouterAs(t, sqldb, reqctx.Actor{UserID: userID, Type: "user", Roles: []string{role}}, h.Mount)
}

// The self-test is the last step of the permission gauntlet, and it must make a
// REAL send to the caller's own endpoints: a button that only claims to have
// sent something turns "my device is broken" into "the app says it works".
func TestSelfTestSendsToTheCallersOwnDevices(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	client := newFakeClient()
	svc, rec := newService(t, sqldb, client, time.Now)
	store := svc.Store()

	subscribe(t, sqldb, store, "u1", "https://push.example/u1-phone")
	subscribe(t, sqldb, store, "u1", "https://push.example/u1-laptop")
	subscribe(t, sqldb, store, "u2", "https://push.example/u2")
	// Muted at the master switch: a self-test bypasses mutes on purpose, because
	// the question it answers is "does this device work", not "do I want this".
	setPrefs(t, sqldb, store, "u1", PreferencesPatch{Enabled: boolp(false)})

	rr := httptest.NewRecorder()
	pushRouter(t, sqldb, svc, "u1", "reader").
		ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/push/test", nil))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("POST /api/push/test = %d (%s), want 202", rr.Code, rr.Body.String())
	}

	var res TestResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if res.Subscriptions != 2 || res.Sent != 2 {
		t.Errorf("result = %+v, want both of u1's devices attempted and delivered", res)
	}

	// It reached only the caller's endpoints, never another member's.
	if got := client.count(); got != 2 {
		t.Errorf("push service saw %d requests, want 2 (u2 must not be touched)", got)
	}
	if got := len(rec.results); got != 2 {
		t.Errorf("recorded %d delivery rows, want 2", got)
	}

	// Consent-adjacent actions are audited, like subscribe and prefs.
	var module, action string
	if err := sqldb.QueryRow(
		`SELECT module, action FROM audit_events ORDER BY id DESC LIMIT 1`).Scan(&module, &action); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if module != audit.ModulePlatform || action != "push.test" {
		t.Errorf("audit row = %s/%s, want platform/push.test", module, action)
	}
}

// A member with no registered device asked a question the panel must answer
// honestly: nothing was sent, and reporting "odesláno" would be a lie.
func TestSelfTestWithNoDeviceIs422(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	svc, _ := newService(t, sqldb, newFakeClient(), time.Now)

	rr := httptest.NewRecorder()
	pushRouter(t, sqldb, svc, "u1", "reader").
		ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/push/test", nil))
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("POST /api/push/test with no device = %d, want 422", rr.Code)
	}
}

// ...but a FAILED LOOKUP is not "no device", and must never be reported as one.
//
// Send returns an empty result for both, so the handler establishes "has this
// account any endpoint?" separately. Collapsing the two told a member with a
// working subscription that no device was registered — which the settings copy
// invites them to fix by unsubscribing and re-running the one-shot permission
// flow, i.e. by breaking something that was never broken.
func TestSelfTestReportsALookupFailureAsAServerFault(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	svc, _ := newService(t, sqldb, newFakeClient(), time.Now)
	subscribe(t, sqldb, svc.Store(), "u1", "https://push.example/u1-phone")

	// The device is real and registered; the eligibility query is what breaks.
	if _, err := sqldb.Exec(`DROP TABLE notification_preferences`); err != nil {
		t.Fatalf("drop preferences: %v", err)
	}

	rr := httptest.NewRecorder()
	pushRouter(t, sqldb, svc, "u1", "reader").
		ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/push/test", nil))
	if rr.Code == http.StatusUnprocessableEntity {
		t.Fatalf("a failed lookup was reported as 422 “no device registered”: %s", rr.Body.String())
	}
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("POST /api/push/test with a broken lookup = %d, want 500", rr.Code)
	}
}

// ---- subscribe: what reaches the audit log ----

// subscribeReq posts one endpoint as userID and returns the response recorder.
func subscribeReq(t *testing.T, sqldb *sql.DB, svc *Service, userID, endpoint string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"endpoint":   endpoint,
		"keys":       map[string]string{"p256dh": testP256dh, "auth": testAuth},
		"user_agent": "Mozilla/5.0 (Linux; Android 14) Mobile",
	})
	if err != nil {
		t.Fatalf("marshal subscribe body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/push/subscriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	pushRouter(t, sqldb, svc, userID, "reader").ServeHTTP(rr, req)
	return rr
}

// The endpoint decides where this server POSTs for every notification, so it is
// allowlisted rather than trusted. Without this, any signed-in member — reader
// included, since the push routes are open to every role — could point the
// backend at a host of their choosing and have it deliver there until the
// failure pruner caught up.
func TestSubscribeRefusesAnUnknownPushService(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	svc, _ := newService(t, sqldb, newFakeClient(), time.Now)

	for _, endpoint := range []string{
		"https://attacker.example/collect", // an arbitrary host
		"http://push.example/plain",        // https only
		"https://notpush.example.evil/x",   // suffix-matching must not be substring-matching
		"://nonsense",                      // unparseable
	} {
		rr := subscribeReq(t, sqldb, svc, "u1", endpoint)
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("subscribe %q = %d, want 422", endpoint, rr.Code)
		}
	}

	var n int
	if err := sqldb.QueryRow(`SELECT COUNT(*) FROM push_subscriptions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("stored %d subscriptions, want none to have been created", n)
	}

	// A host ON the list still works, including as a subdomain — Mozilla and WNS
	// shard across per-region subdomains, so exact-match alone would lock them out.
	if rr := subscribeReq(t, sqldb, svc, "u1", "https://push.example/ok"); rr.Code != http.StatusCreated {
		t.Errorf("allowed host = %d (%s), want 201", rr.Code, rr.Body.String())
	}
	if rr := subscribeReq(t, sqldb, svc, "u1", "https://shard-3.push.example/ok"); rr.Code != http.StatusCreated {
		t.Errorf("allowed subdomain = %d (%s), want 201", rr.Code, rr.Body.String())
	}
}

// deviceLabel clips a long user-agent for the audit summary, and it must clip
// RUNES: slicing bytes can cut a multi-byte sequence in half, and the invalid
// UTF-8 that lands in audit_events comes back out of the Log browser as
// replacement characters (encoding/json substitutes them for bytes it cannot
// decode).
func TestSubscribeAuditSummaryStaysValidUTF8(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	svc, _ := newService(t, sqldb, newFakeClient(), time.Now)

	// No recognised platform token, so the label falls through to the length
	// clip — and long enough that byte 40 lands INSIDE a two-byte rune.
	ua := "Prohlížeč " + strings.Repeat("ěščřž", 12)
	body, err := json.Marshal(map[string]any{
		"endpoint":   "https://push.example/cz",
		"keys":       map[string]string{"p256dh": testP256dh, "auth": testAuth},
		"user_agent": ua,
	})
	if err != nil {
		t.Fatalf("marshal subscribe body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/push/subscriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	pushRouter(t, sqldb, svc, "u1", "reader").ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("subscribe = %d (%s), want 201", rr.Code, rr.Body.String())
	}

	var summary string
	if err := sqldb.QueryRow(
		`SELECT summary FROM audit_events WHERE action = 'push.subscribe'`).Scan(&summary); err != nil {
		t.Fatalf("read audit summary: %v", err)
	}
	if !utf8.ValidString(summary) {
		t.Errorf("audit summary is not valid UTF-8, a rune was cut in half: %q", summary)
	}
	if !strings.Contains(summary, "Prohlížeč") {
		t.Errorf("audit summary lost the device label entirely: %q", summary)
	}
}

// auditActions returns every push audit action written so far, oldest first.
func auditActions(t *testing.T, sqldb *sql.DB) []string {
	t.Helper()
	rows, err := sqldb.Query(`SELECT action FROM audit_events WHERE module = ? ORDER BY id`, audit.ModulePlatform)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		out = append(out, a)
	}
	return out
}

// A browser re-issues the SAME endpoint across page loads, and usePushKeepalive
// re-registers it on every one, so a refresh must stay out of the log or it
// would bury the decisions that matter.
//
// A change of OWNER is one of those decisions. On a shared household browser the
// endpoint outlives the session: when the next member signs in, the same
// endpoint moves to them and the previous member stops receiving on that device.
// Treating that as "just a refresh" left it invisible.
func TestSubscribeAuditsCreationAndHandover(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	svc, _ := newService(t, sqldb, newFakeClient(), time.Now)
	const endpoint = "https://push.example/shared-laptop"

	if rr := subscribeReq(t, sqldb, svc, "u1", endpoint); rr.Code != http.StatusCreated {
		t.Fatalf("first subscribe = %d, want 201", rr.Code)
	}
	// The same member, same device, next page load.
	if rr := subscribeReq(t, sqldb, svc, "u1", endpoint); rr.Code != http.StatusOK {
		t.Fatalf("refresh = %d, want 200", rr.Code)
	}
	if got := auditActions(t, sqldb); len(got) != 1 || got[0] != "push.subscribe" {
		t.Fatalf("after a refresh the log holds %v, want exactly one push.subscribe", got)
	}

	// Somebody else signs in on the same browser; the endpoint comes with it.
	if rr := subscribeReq(t, sqldb, svc, "u2", endpoint); rr.Code != http.StatusOK {
		t.Fatalf("handover = %d, want 200 (the row already existed)", rr.Code)
	}
	if got := auditActions(t, sqldb); len(got) != 2 {
		t.Fatalf("the log holds %v, want the handover recorded as a second event", got)
	}

	// The row moved, so the previous member no longer receives on this device.
	var owner string
	if err := sqldb.QueryRow(`SELECT user_id FROM push_subscriptions WHERE endpoint = ?`, endpoint).Scan(&owner); err != nil {
		t.Fatalf("read owner: %v", err)
	}
	if owner != "u2" {
		t.Errorf("endpoint owner = %q, want u2", owner)
	}

	// And the log says whose device it was, which is the question it exists for.
	var meta sql.NullString
	if err := sqldb.QueryRow(
		`SELECT meta FROM audit_events WHERE module = ? ORDER BY id DESC LIMIT 1`,
		audit.ModulePlatform).Scan(&meta); err != nil {
		t.Fatalf("read handover meta: %v", err)
	}
	if !meta.Valid || !strings.Contains(meta.String, "u1") {
		t.Errorf("handover meta = %q, want it to name the previous owner u1", meta.String)
	}
}
