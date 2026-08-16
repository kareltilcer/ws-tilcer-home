package admin_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/admin"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// router mounts the admin module the way main.go does, with a fixed actor.
func (f *fixture) router(roles ...string) http.Handler {
	f.t.Helper()
	mod := admin.NewModule(f.svc)
	return httpx.NewRouter(httpx.Deps{
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		DB:        f.db,
		Site:      "home",
		SessionMW: auth.NewSessionAuth(auth.Config{BypassActor: &reqctx.Actor{UserID: "u-admin", Type: "user", Roles: roles}}),
		MountAPI:  func(api chi.Router) { mod.RegisterRoutes(api) },
	})
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

// Administrace is admin-only, exactly like the log browser (D62). There is no
// reader view — for a non-admin the module does not exist.
func TestAdminRoutesAreAdminOnly(t *testing.T) {
	routes := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/admin/notifications/rules", ""},
		{http.MethodPost, "/api/admin/notifications/rules", `{"name":"x","action_key":"card.move","audience":{"scope":"all"}}`},
		{http.MethodGet, "/api/admin/notifications/rules/some-id", ""},
		{http.MethodPatch, "/api/admin/notifications/rules/some-id", `{"enabled":false}`},
		{http.MethodDelete, "/api/admin/notifications/rules/some-id", ""},
		{http.MethodPost, "/api/admin/notifications/rules/some-id/test", ""},
		{http.MethodGet, "/api/admin/notifications/schedules", ""},
		{http.MethodPost, "/api/admin/notifications/schedules", `{"name":"x"}`},
		{http.MethodGet, "/api/admin/notifications/schedules/some-id", ""},
		{http.MethodPatch, "/api/admin/notifications/schedules/some-id", `{"enabled":false}`},
		{http.MethodDelete, "/api/admin/notifications/schedules/some-id", ""},
		{http.MethodPost, "/api/admin/notifications/schedules/some-id/test", ""},
		{http.MethodPost, "/api/admin/notifications/broadcast", `{"title":"t","body":"b","audience":{"scope":"all"}}`},
		{http.MethodGet, "/api/admin/notifications/catalog", ""},
		{http.MethodGet, "/api/admin/notifications/deliveries", ""},
	}

	for _, role := range []string{"reader", "editor"} {
		f := newFixture(t)
		h := f.router(role)
		for _, rt := range routes {
			if rr := do(t, h, rt.method, rt.path, rt.body); rr.Code != http.StatusForbidden {
				t.Errorf("%s %s %s = %d, want 403", role, rt.method, rt.path, rr.Code)
			}
		}
	}

	// The "*" superuser passes the same gate — there is no separate tier.
	for _, role := range []string{"admin", "*"} {
		f := newFixture(t)
		h := f.router(role)
		if rr := do(t, h, http.MethodGet, "/api/admin/notifications/rules", ""); rr.Code != http.StatusOK {
			t.Errorf("%s GET rules = %d, want 200", role, rr.Code)
		}
	}
}

func TestRuleCRUDOverHTTP(t *testing.T) {
	f := newFixture(t)
	f.member("u-admin", "Admin", []string{"admin"}, true)
	h := f.router("admin")

	rr := do(t, h, http.MethodPost, "/api/admin/notifications/rules",
		`{"name":"Hotové úkoly","action_key":"card.move","audience":{"scope":"all"}}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d (%s), want 201", rr.Code, rr.Body.String())
	}
	var created admin.Rule
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" || !created.Enabled {
		t.Fatalf("created = %+v, want an id and enabled by default", created)
	}

	if rr := do(t, h, http.MethodGet, "/api/admin/notifications/rules/"+created.ID, ""); rr.Code != http.StatusOK {
		t.Errorf("get = %d, want 200", rr.Code)
	}

	rr = do(t, h, http.MethodPatch, "/api/admin/notifications/rules/"+created.ID, `{"enabled":false}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("patch = %d (%s), want 200", rr.Code, rr.Body.String())
	}
	var patched admin.Rule
	_ = json.Unmarshal(rr.Body.Bytes(), &patched)
	if patched.Enabled {
		t.Error("patch did not disable the rule")
	}

	if rr := do(t, h, http.MethodDelete, "/api/admin/notifications/rules/"+created.ID, ""); rr.Code != http.StatusNoContent {
		t.Errorf("delete = %d, want 204", rr.Code)
	}
	if rr := do(t, h, http.MethodGet, "/api/admin/notifications/rules/"+created.ID, ""); rr.Code != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", rr.Code)
	}
}

// A validation failure must come back as a 422 with a Czech reason the composer
// can show on the field.
func TestInvalidRuleOverHTTPIs422(t *testing.T) {
	f := newFixture(t)
	h := f.router("admin")

	rr := do(t, h, http.MethodPost, "/api/admin/notifications/rules",
		`{"name":"X","action_key":"card.teleport","audience":{"scope":"all"}}`)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("= %d (%s), want 422", rr.Code, rr.Body.String())
	}
	var body struct{ Error, Detail string }
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if !strings.Contains(body.Detail, "neznámá akce") {
		t.Errorf("detail = %q, want a Czech reason naming the problem", body.Detail)
	}
}

func TestBroadcastOverHTTPReturns202(t *testing.T) {
	f := newFixture(t)
	f.member("u-admin", "Admin", []string{"admin"}, true)
	h := f.router("admin")

	rr := do(t, h, http.MethodPost, "/api/admin/notifications/broadcast",
		`{"title":"Vypnutá voda","body":"Dnes od 9:00","audience":{"scope":"all"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("= %d (%s), want 202", rr.Code, rr.Body.String())
	}
	var res admin.SendResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Recipients != 1 || res.Subscriptions != 1 {
		t.Errorf("result = %+v, want 1/1", res)
	}
}

func TestScheduleCreateOverHTTP(t *testing.T) {
	f := newFixture(t)
	h := f.router("admin")

	rr := do(t, h, http.MethodPost, "/api/admin/notifications/schedules", `{
		"name":"Ranní přehled",
		"schedule":{"time_local":"08:00","days":{"preset":"daily"}},
		"audience":{"scope":"all"},
		"title_template":"Dobré ráno",
		"body_template":"Právě dělám: {{metric.todo.pravedelam_count}} úkolů"
	}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("= %d (%s), want 201", rr.Code, rr.Body.String())
	}
	var sc admin.Schedule
	if err := json.Unmarshal(rr.Body.Bytes(), &sc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The Czech phrase the list shows comes from the scheduler, so the two can
	// never disagree about what a day pattern means.
	if sc.Description != "Každý den v 8:00" {
		t.Errorf("description = %q, want the human schedule phrase", sc.Description)
	}
}

func TestCatalogOverHTTP(t *testing.T) {
	f := newFixture(t)
	h := f.router("admin")

	rr := do(t, h, http.MethodGet, "/api/admin/notifications/catalog", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("= %d, want 200", rr.Code)
	}
	var cat admin.Catalog
	if err := json.Unmarshal(rr.Body.Bytes(), &cat); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(cat.Actions) == 0 || len(cat.Metrics) == 0 || len(cat.Tokens) != 3 {
		t.Errorf("catalog = %d actions / %d metrics / %d token contexts, want all three populated",
			len(cat.Actions), len(cat.Metrics), len(cat.Tokens))
	}
}

func TestDeliveriesOverHTTP(t *testing.T) {
	f := newFixture(t)
	f.member("u-admin", "Admin", []string{"admin"}, true)
	if err := f.svc.Store().RecordDeliveries(t.Context(), []push.DeliveryResult{{
		UserID: "u-admin", Status: push.StatusSent, Kind: push.KindBroadcast, Category: push.CategoryBroadcast,
	}}); err != nil {
		t.Fatalf("record: %v", err)
	}

	h := f.router("admin")
	rr := do(t, h, http.MethodGet, "/api/admin/notifications/deliveries?limit=10", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("= %d, want 200", rr.Code)
	}
	var page admin.DeliveryPage
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(page.Items))
	}
	if page.Items[0].TS.IsZero() || page.Items[0].TS.After(time.Now().Add(time.Minute)) {
		t.Errorf("ts = %v, want a sane timestamp", page.Items[0].TS)
	}
}

// DisallowUnknownFields is what turns a client-side typo into a loud refusal
// instead of a save that quietly does nothing. RuleUpdate has a custom
// UnmarshalJSON (it has to, to tell an explicit null from an omitted key), and a
// custom unmarshaler bypasses the decoder's own unknown-field check — so it has
// to make that check itself. A malformed body is a 422 here, the same as in
// every other module.
func TestRulePatchRejectsAnUnknownField(t *testing.T) {
	f := newFixture(t)
	f.member("u-admin", "Admin", []string{"admin"}, true)
	h := f.router("admin")

	rr := do(t, h, http.MethodPost, "/api/admin/notifications/rules",
		`{"name":"R","action_key":"card.move","audience":{"scope":"all"}}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d (%s), want 201", rr.Code, rr.Body.String())
	}
	var created admin.Rule
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	rr = do(t, h, http.MethodPatch, "/api/admin/notifications/rules/"+created.ID, `{"body_temlate":"typo"}`)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("patch with a typo’d field = %d (%s), want 422", rr.Code, rr.Body.String())
	}

	// The real field name still works, and still clears the value.
	if rr := do(t, h, http.MethodPatch, "/api/admin/notifications/rules/"+created.ID,
		`{"body_template":null}`); rr.Code != http.StatusOK {
		t.Errorf("patch with the real field = %d (%s), want 200", rr.Code, rr.Body.String())
	}
}
