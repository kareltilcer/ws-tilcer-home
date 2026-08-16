package push_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	. "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// ---- fakes ----

// fakeClient stands in for the browser push service. It records every POST and
// replies with whatever status the test assigned to that endpoint, so the
// endpoint-health policy (410 prunes, 5xx marks failing) is exercised without a
// network.
type fakeClient struct {
	mu       sync.Mutex
	status   map[string]int // endpoint -> status; missing = 201
	err      map[string]error
	requests []*http.Request
}

func newFakeClient() *fakeClient {
	return &fakeClient{status: map[string]int{}, err: map[string]error{}}
}

func (f *fakeClient) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	url := req.URL.String()
	if err, ok := f.err[url]; ok {
		return nil, err
	}
	status, ok := f.status[url]
	if !ok {
		status = http.StatusCreated
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}, nil
}

func (f *fakeClient) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// recorder captures delivery rows in memory, standing in for the admin module's
// table (which platform deliberately does not own).
type recorder struct {
	mu      sync.Mutex
	results []DeliveryResult
}

func (r *recorder) RecordDeliveries(_ context.Context, results []DeliveryResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results = append(r.results, results...)
	return nil
}

// ---- harness ----

// Real key material, generated once per test binary. The payload is genuinely
// encrypted (RFC 8291 ECDH against the client's public key, VAPID-signed), so
// made-up base64 of the right length is not enough — an invalid curve point
// fails inside the crypto and never reaches the transport.
var (
	testVAPIDPrivate, testVAPIDPublic = mustVAPIDKeys()
	testP256dh, testAuth              = mustClientKeys()
)

func mustVAPIDKeys() (priv, pub string) {
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		panic("generate VAPID keys: " + err.Error())
	}
	return priv, pub
}

// mustClientKeys mimics what a browser's PushManager hands the SPA: an
// uncompressed P-256 public key and a 16-byte auth secret, both base64url.
func mustClientKeys() (p256dh, auth string) {
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		panic("generate client key: " + err.Error())
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		panic("generate auth secret: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		base64.RawURLEncoding.EncodeToString(secret)
}

func newService(t *testing.T, sqldb *sql.DB, client *fakeClient, now func() time.Time) (*Service, *recorder) {
	t.Helper()
	svc := NewService(NewStore(sqldb), Config{
		VAPIDPublicKey:  testVAPIDPublic,
		VAPIDPrivateKey: testVAPIDPrivate,
		VAPIDSubject:    "mailto:test@tilcer.cz",
		MaxFailDays:     14,
		HTTPClient:      client,
		Now:             now,
		// Replaces the vendor allowlist outright: these tests subscribe fake
		// endpoints, and pretending to be Google would hide what is being asserted.
		AllowedEndpointHosts: []string{"push.example"},
	})
	rec := &recorder{}
	svc.SetRecorder(rec)
	return svc, rec
}

// subscribe inserts a subscription directly, the way the HTTP handler would.
func subscribe(t *testing.T, sqldb *sql.DB, store *Store, userID, endpoint string) Subscription {
	t.Helper()
	var sub Subscription
	err := appdb.WithTx(context.Background(), sqldb, func(tx *sql.Tx) error {
		var err error
		sub, _, err = store.Upsert(context.Background(), tx, userID, endpoint, testP256dh, testAuth, "test-agent", time.Now())
		return err
	})
	if err != nil {
		t.Fatalf("subscribe %s: %v", endpoint, err)
	}
	return sub
}

func setPrefs(t *testing.T, sqldb *sql.DB, store *Store, userID string, patch PreferencesPatch) {
	t.Helper()
	err := appdb.WithTx(context.Background(), sqldb, func(tx *sql.Tx) error {
		_, err := store.UpdatePreferences(context.Background(), tx, userID, patch, time.Now())
		return err
	})
	if err != nil {
		t.Fatalf("set prefs: %v", err)
	}
}

func boolp(b bool) *bool { return &b }

// ---- subscriptions ----

// A browser re-issues the same endpoint on every page load; a second POST must
// refresh the row, not create a duplicate that doubles every notification.
func TestUpsertIsIdempotentPerEndpoint(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	store := NewStore(sqldb)

	first := subscribe(t, sqldb, store, "u1", "https://push.example/aaa")
	second := subscribe(t, sqldb, store, "u1", "https://push.example/aaa")

	if first.ID != second.ID {
		t.Errorf("re-subscribe created a new row: %s vs %s", first.ID, second.ID)
	}
	subs, err := store.ListForUser(context.Background(), "u1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("got %d subscriptions, want 1", len(subs))
	}
}

// A re-subscribe is how a device recovers, so it must clear the failure streak.
func TestUpsertClearsFailingSince(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	store := NewStore(sqldb)
	sub := subscribe(t, sqldb, store, "u1", "https://push.example/aaa")

	if err := store.MarkFailing(context.Background(), sub.ID, time.Now()); err != nil {
		t.Fatalf("mark failing: %v", err)
	}
	subscribe(t, sqldb, store, "u1", "https://push.example/aaa")

	subs, _ := store.ListForUser(context.Background(), "u1")
	if subs[0].FailingSince != nil {
		t.Errorf("failing_since survived a re-subscribe: %v", subs[0].FailingSince)
	}
}

// Deletion is scoped by session user: one member must not be able to unsubscribe
// another member's device by pasting its endpoint.
func TestDeleteIsScopedToTheOwner(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	store := NewStore(sqldb)
	subscribe(t, sqldb, store, "owner", "https://push.example/owned")

	var removed bool
	err := appdb.WithTx(context.Background(), sqldb, func(tx *sql.Tx) error {
		var err error
		removed, err = store.Delete(context.Background(), tx, "attacker", "https://push.example/owned")
		return err
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if removed {
		t.Error("another member's endpoint was deleted")
	}
	subs, _ := store.ListForUser(context.Background(), "owner")
	if len(subs) != 1 {
		t.Errorf("owner lost their subscription: %d rows", len(subs))
	}
}

// ---- preferences ----

func TestPreferencesDefaultToAllOn(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	store := NewStore(sqldb)

	prefs, err := store.Preferences(context.Background(), "never-opened-settings")
	if err != nil {
		t.Fatalf("prefs: %v", err)
	}
	if !prefs.Enabled || !prefs.Categories.Broadcast || !prefs.Categories.Triggers || !prefs.Categories.Summaries {
		t.Errorf("absent row should mean all-on, got %+v", prefs)
	}
}

func TestPreferencesPatchIsPartial(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	store := NewStore(sqldb)

	setPrefs(t, sqldb, store, "u1", PreferencesPatch{Summaries: boolp(false)})
	setPrefs(t, sqldb, store, "u1", PreferencesPatch{Broadcast: boolp(false)})

	prefs, _ := store.Preferences(context.Background(), "u1")
	if prefs.Categories.Summaries || prefs.Categories.Broadcast {
		t.Errorf("both mutes should be off: %+v", prefs.Categories)
	}
	if !prefs.Categories.Triggers || !prefs.Enabled {
		t.Errorf("untouched fields should stay on: %+v", prefs)
	}
}

// ---- the mute matrix, honoured at send time ----

func TestSendHonoursMutes(t *testing.T) {
	cases := []struct {
		name     string
		patch    PreferencesPatch
		envelope string
		wantSent bool
	}{
		{"all on", PreferencesPatch{}, CategoryBroadcast, true},
		{"master off blocks everything", PreferencesPatch{Enabled: boolp(false)}, CategoryBroadcast, false},
		{"category off blocks its own", PreferencesPatch{Broadcast: boolp(false)}, CategoryBroadcast, false},
		{"category off leaves others", PreferencesPatch{Broadcast: boolp(false)}, CategoryTriggers, true},
		{"summaries off", PreferencesPatch{Summaries: boolp(false)}, CategorySummaries, false},
		{"triggers off", PreferencesPatch{Triggers: boolp(false)}, CategoryTriggers, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sqldb := testsupport.NewDB(t)
			client := newFakeClient()
			svc, _ := newService(t, sqldb, client, time.Now)
			subscribe(t, sqldb, svc.Store(), "u1", "https://push.example/u1")
			if (tc.patch != PreferencesPatch{}) {
				setPrefs(t, sqldb, svc.Store(), "u1", tc.patch)
			}

			results := svc.Send(context.Background(), []string{"u1"}, Envelope{
				Title: "T", Body: "B", Category: tc.envelope,
			})

			if tc.wantSent && len(results) != 1 {
				t.Fatalf("expected one delivery, got %d", len(results))
			}
			if !tc.wantSent && len(results) != 0 {
				t.Fatalf("expected the mute to suppress the send, got %d deliveries", len(results))
			}
			if tc.wantSent && results[0].Status != StatusSent {
				t.Errorf("status = %s (%s), want sent", results[0].Status, results[0].Error)
			}
		})
	}
}

// A self-test must reach the caller's own devices even while everything is muted
// — otherwise "does this device work?" is unanswerable (FR-ADM6).
func TestSendBypassMutesForSelfTest(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	client := newFakeClient()
	svc, _ := newService(t, sqldb, client, time.Now)
	subscribe(t, sqldb, svc.Store(), "admin", "https://push.example/admin")
	setPrefs(t, sqldb, svc.Store(), "admin", PreferencesPatch{Enabled: boolp(false)})

	results := svc.Send(context.Background(), []string{"admin"}, Envelope{
		Title: "Test", Body: "B", Category: CategoryBroadcast, Kind: KindTest, BypassMutes: true,
	})
	if len(results) != 1 || results[0].Status != StatusSent {
		t.Fatalf("self-test should bypass mutes, got %+v", results)
	}
}

// ---- endpoint health ----

func TestSendPrunesGoneEndpoints(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			sqldb := testsupport.NewDB(t)
			client := newFakeClient()
			svc, rec := newService(t, sqldb, client, time.Now)
			subscribe(t, sqldb, svc.Store(), "u1", "https://push.example/dead")
			client.status["https://push.example/dead"] = status

			results := svc.Send(context.Background(), []string{"u1"}, Envelope{Title: "T", Body: "B", Category: CategoryBroadcast})

			if len(results) != 1 || results[0].Status != StatusExpired {
				t.Fatalf("want one expired result, got %+v", results)
			}
			subs, _ := svc.Store().ListForUser(context.Background(), "u1")
			if len(subs) != 0 {
				t.Errorf("dead endpoint was not pruned: %d rows remain", len(subs))
			}
			if len(rec.results) != 1 || rec.results[0].Status != StatusExpired {
				t.Errorf("the attempt should still be recorded: %+v", rec.results)
			}
		})
	}
}

// A transient 5xx starts a failure streak but keeps the subscription.
func TestSendMarksFailingWithoutPruning(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	client := newFakeClient()
	svc, _ := newService(t, sqldb, client, time.Now)
	subscribe(t, sqldb, svc.Store(), "u1", "https://push.example/flaky")
	client.status["https://push.example/flaky"] = http.StatusServiceUnavailable

	results := svc.Send(context.Background(), []string{"u1"}, Envelope{Title: "T", Body: "B", Category: CategoryBroadcast})
	if len(results) != 1 || results[0].Status != StatusFailed {
		t.Fatalf("want one failed result, got %+v", results)
	}
	subs, _ := svc.Store().ListForUser(context.Background(), "u1")
	if len(subs) != 1 {
		t.Fatalf("a transient failure must not prune: %d rows", len(subs))
	}
	if subs[0].FailingSince == nil {
		t.Error("failing_since was not stamped")
	}
}

// Past MaxFailDays the device is assumed gone for good.
func TestSendPrunesLongFailingSubscription(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	client := newFakeClient()
	now := time.Now()
	clock := func() time.Time { return now }
	svc, _ := newService(t, sqldb, client, clock)
	sub := subscribe(t, sqldb, svc.Store(), "u1", "https://push.example/gone")
	client.status["https://push.example/gone"] = http.StatusInternalServerError

	// Streak started 20 days ago; MaxFailDays is 14.
	if err := svc.Store().MarkFailing(context.Background(), sub.ID, now.Add(-20*24*time.Hour)); err != nil {
		t.Fatalf("mark failing: %v", err)
	}

	svc.Send(context.Background(), []string{"u1"}, Envelope{Title: "T", Body: "B", Category: CategoryBroadcast})

	subs, _ := svc.Store().ListForUser(context.Background(), "u1")
	if len(subs) != 0 {
		t.Errorf("a subscription failing for 20 days should be pruned, %d remain", len(subs))
	}
}

// ---- fan-out ----

func TestSendFansOutToEveryDeviceOfEveryRecipient(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	client := newFakeClient()
	svc, rec := newService(t, sqldb, client, time.Now)
	subscribe(t, sqldb, svc.Store(), "u1", "https://push.example/u1-phone")
	subscribe(t, sqldb, svc.Store(), "u1", "https://push.example/u1-laptop")
	subscribe(t, sqldb, svc.Store(), "u2", "https://push.example/u2-phone")
	subscribe(t, sqldb, svc.Store(), "u3", "https://push.example/u3-phone") // not a recipient

	results := svc.Send(context.Background(), []string{"u1", "u2"}, Envelope{
		Title: "T", Body: "B", Category: CategoryBroadcast, Kind: KindBroadcast,
	})

	if len(results) != 3 {
		t.Fatalf("want 3 endpoint attempts, got %d", len(results))
	}
	if got := DistinctUsers(results); got != 2 {
		t.Errorf("DistinctUsers = %d, want 2", got)
	}
	if client.count() != 3 {
		t.Errorf("push service saw %d requests, want 3", client.count())
	}
	if len(rec.results) != 3 {
		t.Errorf("recorded %d deliveries, want 3", len(rec.results))
	}
}

// The wire payload must carry the routing tag the service worker needs, and must
// NOT carry the server-side delivery metadata.
func TestEnvelopePayloadShape(t *testing.T) {
	e := Envelope{
		Module: "todo", Type: "trigger", Title: "Nový úkol", Body: "Kdo?",
		URL: "/ukoly", Category: CategoryTriggers, Kind: KindTrigger, RuleID: "rule-1",
	}
	b, err := json.Marshal(NormalizeForTest(e))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	payload := string(b)
	for _, want := range []string{`"module":"todo"`, `"type":"trigger"`, `"url":"/ukoly"`, `"title":"Nový úkol"`} {
		if !strings.Contains(payload, want) {
			t.Errorf("payload missing %s: %s", want, payload)
		}
	}
	for _, unwanted := range []string{"rule-1", "triggers", "RuleID", "Category"} {
		if strings.Contains(payload, unwanted) {
			t.Errorf("payload leaked delivery metadata %q: %s", unwanted, payload)
		}
	}
}

// An over-long body must be clipped, not dropped: Web Push caps the encrypted
// record at ~4 KB and a rejected payload is a silent non-delivery.
func TestEnvelopeTruncatesLongBody(t *testing.T) {
	e := NormalizeForTest(Envelope{Title: "T", Body: strings.Repeat("á", 5000), Category: CategoryBroadcast})
	if runes := []rune(e.Body); len(runes) > MaxBodyRunesForTest {
		t.Errorf("body not truncated: %d runes", len(runes))
	}
	if !strings.HasSuffix(e.Body, "…") {
		t.Error("a truncated body should end in an ellipsis")
	}
}

func TestEnvelopeDefaults(t *testing.T) {
	e := NormalizeForTest(Envelope{Title: "T", Body: "B"})
	if e.URL != "/" {
		t.Errorf("URL = %q, want /", e.URL)
	}
	if e.Category != CategoryBroadcast {
		t.Errorf("Category = %q, want the broadcast default", e.Category)
	}
	if e.Kind != KindBroadcast {
		t.Errorf("Kind = %q, want the broadcast kind", e.Kind)
	}
}

// Categories and kinds are two different vocabularies, so the Kind default has
// to TRANSLATE rather than copy. Copying produced "triggers"/"summaries", which
// notification_deliveries.kind rejects — and because the recorder writes a whole
// fan-out in one transaction, one bad value silently discarded the delivery
// record of every notification that had just gone out.
func TestEnvelopeKindDefaultsToAValidDeliveryKind(t *testing.T) {
	valid := map[string]bool{KindBroadcast: true, KindTrigger: true, KindSchedule: true, KindTest: true}
	want := map[string]string{
		CategoryBroadcast: KindBroadcast,
		CategoryTriggers:  KindTrigger,
		CategorySummaries: KindSchedule,
	}
	for category, wantKind := range want {
		e := NormalizeForTest(Envelope{Title: "T", Category: category})
		if !valid[e.Kind] {
			t.Errorf("category %q defaulted to kind %q, which the delivery log's CHECK rejects", category, e.Kind)
		}
		if e.Kind != wantKind {
			t.Errorf("category %q defaulted to kind %q, want %q", category, e.Kind, wantKind)
		}
	}
}

// An explicit Kind always wins: a test send of a summary is kind "test",
// category "summaries".
func TestEnvelopeKeepsAnExplicitKind(t *testing.T) {
	e := NormalizeForTest(Envelope{Title: "T", Category: CategorySummaries, Kind: KindTest})
	if e.Kind != KindTest {
		t.Errorf("Kind = %q, want the explicit %q to survive normalization", e.Kind, KindTest)
	}
}

// A push service that accepts the connection and then never answers must not be
// able to hold the caller forever.
//
// This is the DEFAULT client's contract, deliberately tested with no HTTPClient
// injected: webpush-go's own fallback is a bare &http.Client{} with no timeout,
// and a delivery runs on the scheduler's ticker goroutine (synchronously, once
// per due summary) and on a detached goroutine for triggers. Neither carries a
// deadline, so an unbounded client meant one silent endpoint could stop every
// future summary until the process was restarted.
func TestDeliveryTimesOutOnAnUnresponsivePushService(t *testing.T) {
	hang := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-hang }))
	// Release the blocked handler BEFORE Close, which waits for it.
	defer func() { close(hang); srv.Close() }()

	sqldb := testsupport.NewDB(t)
	priv, pub := testVAPIDPrivate, testVAPIDPublic
	svc := NewService(NewStore(sqldb), Config{
		VAPIDPublicKey: pub, VAPIDPrivateKey: priv, VAPIDSubject: "mailto:test@tilcer.cz",
		// No HTTPClient: this is exactly the production wiring.
		HTTPTimeout: 250 * time.Millisecond,
	})
	subscribe(t, sqldb, svc.Store(), "u1", srv.URL+"/hangs-forever")

	done := make(chan []DeliveryResult, 1)
	go func() {
		// An uncancellable context, like the scheduler's: the timeout must come
		// from the client, not from the caller remembering to set a deadline.
		done <- svc.Send(context.Background(), []string{"u1"}, Envelope{
			Title: "T", Body: "B", Category: CategoryBroadcast, Kind: KindSchedule,
		})
	}()

	select {
	case results := <-done:
		if len(results) != 1 {
			t.Fatalf("got %d results, want 1", len(results))
		}
		if results[0].Status != StatusFailed {
			t.Errorf("status = %q, want %q for a timed-out endpoint", results[0].Status, StatusFailed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Send never returned: the default push client has no timeout")
	}
}

// A server with no keypair must be inert rather than erroring on every send.
func TestDisabledChannelSendsNothing(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	svc := NewService(NewStore(sqldb), Config{})
	subscribe(t, sqldb, svc.Store(), "u1", "https://push.example/u1")

	if svc.Enabled() {
		t.Error("Enabled = true without a keypair")
	}
	if results := svc.Send(context.Background(), []string{"u1"}, Envelope{Title: "T", Category: CategoryBroadcast}); results != nil {
		t.Errorf("a disabled channel sent %d deliveries", len(results))
	}
}

// ---- audience resolution ----

// seedSession writes the session row the member directory is projected from.
func seedSession(t *testing.T, sqldb *sql.DB, userID, email, name string, roles []string, created time.Time) {
	t.Helper()
	rolesJSON, _ := json.Marshal(roles)
	_, err := sqldb.Exec(
		`INSERT INTO sessions (id, user_id, token_hash, email, display_name, roles, roles_refreshed_at,
		                       created_at, last_seen_at, expires_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"sess-"+userID+"-"+created.Format("150405.000000000"), userID, "hash-"+userID+created.Format("150405.000000000"),
		email, name, string(rolesJSON), created.Format(time.RFC3339Nano),
		created.Format(time.RFC3339Nano), created.Format(time.RFC3339Nano), created.Add(24*time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func TestMembersProjectsTheNewestSessionPerUser(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	store := NewStore(sqldb)
	base := time.Now().Add(-time.Hour)

	seedSession(t, sqldb, "u1", "karel@tilcer.cz", "Karel starý", []string{"editor"}, base)
	seedSession(t, sqldb, "u1", "karel@tilcer.cz", "Karel", []string{"admin"}, base.Add(30*time.Minute))
	seedSession(t, sqldb, "u2", "eva@tilcer.cz", "", []string{"reader"}, base)
	subscribe(t, sqldb, store, "u1", "https://push.example/u1")

	members, err := store.Members(context.Background())
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("got %d members, want 2 (one row per user): %+v", len(members), members)
	}
	byID := map[string]Member{}
	for _, m := range members {
		byID[m.UserID] = m
	}
	if got := byID["u1"]; got.DisplayName != "Karel" || len(got.Roles) != 1 || got.Roles[0] != "admin" {
		t.Errorf("u1 should carry the NEWEST session's identity and roles, got %+v", got)
	}
	if byID["u1"].Subscriptions != 1 {
		t.Errorf("u1 subscription count = %d, want 1", byID["u1"].Subscriptions)
	}
	// A member with no display name falls back to their email so the picker never
	// shows a blank row.
	if byID["u2"].DisplayName != "eva@tilcer.cz" {
		t.Errorf("u2 display name = %q, want the email fallback", byID["u2"].DisplayName)
	}
}

func TestResolveAudience(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	store := NewStore(sqldb)
	base := time.Now().Add(-time.Hour)
	seedSession(t, sqldb, "admin1", "a@x.cz", "Admin", []string{"admin"}, base)
	seedSession(t, sqldb, "editor1", "e@x.cz", "Editor", []string{"editor"}, base)
	seedSession(t, sqldb, "reader1", "r@x.cz", "Reader", []string{"reader"}, base)
	seedSession(t, sqldb, "super1", "s@x.cz", "Super", []string{"*"}, base)
	// Only three of the four have a device.
	subscribe(t, sqldb, store, "admin1", "https://push.example/a")
	subscribe(t, sqldb, store, "editor1", "https://push.example/e")
	subscribe(t, sqldb, store, "super1", "https://push.example/s")

	t.Run("all is everyone with a device", func(t *testing.T) {
		got, err := store.ResolveAudience(context.Background(), Audience{Scope: ScopeAll})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		want := []string{"admin1", "editor1", "super1"}
		if strings.Join(SortedUserIDs(got), ",") != strings.Join(want, ",") {
			t.Errorf("all = %v, want %v (reader1 has no subscription)", got, want)
		}
	})

	t.Run("empty scope defaults to all", func(t *testing.T) {
		got, err := store.ResolveAudience(context.Background(), Audience{})
		if err != nil || len(got) != 3 {
			t.Errorf("default scope should behave as all, got %v (%v)", got, err)
		}
	})

	t.Run("roles match the cached session roles", func(t *testing.T) {
		got, err := store.ResolveAudience(context.Background(), Audience{Scope: ScopeRoles, Roles: []string{"editor"}})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		// The "*" superuser matches every role.
		want := []string{"editor1", "super1"}
		if strings.Join(SortedUserIDs(got), ",") != strings.Join(want, ",") {
			t.Errorf("roles=[editor] = %v, want %v", SortedUserIDs(got), want)
		}
	})

	t.Run("users are filtered to known members", func(t *testing.T) {
		got, err := store.ResolveAudience(context.Background(), Audience{
			Scope: ScopeUsers, Users: []string{"reader1", "nobody-home-has-seen"},
		})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if len(got) != 1 || got[0] != "reader1" {
			t.Errorf("users = %v, want just reader1 (the unknown id is dropped)", got)
		}
	})
}

// An explicitly empty selection is the composer error a user can fix; "all" with
// nobody subscribed is a household state, not a validation failure.
func TestAudienceValid(t *testing.T) {
	cases := []struct {
		aud  Audience
		want bool
	}{
		{Audience{Scope: ScopeAll}, true},
		{Audience{}, true},
		{Audience{Scope: ScopeRoles, Roles: []string{"admin"}}, true},
		{Audience{Scope: ScopeRoles}, false},
		{Audience{Scope: ScopeUsers, Users: []string{"u1"}}, true},
		{Audience{Scope: ScopeUsers}, false},
		{Audience{Scope: "nonsense"}, false},
	}
	for _, tc := range cases {
		if got := tc.aud.Valid(); got != tc.want {
			t.Errorf("Audience%+v.Valid() = %t, want %t", tc.aud, got, tc.want)
		}
	}
}

// ---- audit ----

// Consent changes are user actions and must be visible in the log.
func TestSubscribeIsAudited(t *testing.T) {
	sqldb := testsupport.NewDB(t)
	store := NewStore(sqldb)
	sink := audit.NewSink()
	ctx := testsupport.CtxUser("u1", "reader")

	err := appdb.WithTx(ctx, sqldb, func(tx *sql.Tx) error {
		sub, res, err := store.Upsert(ctx, tx, "u1", "https://push.example/u1", testP256dh, testAuth, "Android", time.Now())
		if err != nil || !res.Created {
			return err
		}
		_, err = sink.Record(ctx, tx, audit.Event{
			Module: audit.ModulePlatform, Action: "push.subscribe",
			EntityType: "push_subscription", EntityID: sub.ID,
			Summary: "Zapnutá oznámení na zařízení (Android)",
		})
		return err
	})
	if err != nil {
		t.Fatalf("subscribe tx: %v", err)
	}

	var action, module string
	if err := sqldb.QueryRow(`SELECT module, action FROM audit_events ORDER BY id DESC LIMIT 1`).Scan(&module, &action); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if module != audit.ModulePlatform || action != "push.subscribe" {
		t.Errorf("audit row = %s/%s, want platform/push.subscribe", module, action)
	}
}
