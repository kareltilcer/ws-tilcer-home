package dashboard_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/dashboard"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/auth"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// fakeProvider is a stand-in widget so the host is tested in isolation — proof
// that the host needs nothing but the WidgetProvider contract (D28).
type fakeProvider struct {
	key, size string
	admin     bool
	calls     *int
}

func (f fakeProvider) Key() string         { return f.key }
func (f fakeProvider) Title() string       { return f.key }
func (f fakeProvider) Module() string      { return "test" }
func (f fakeProvider) Description() string { return "" }
func (f fakeProvider) DefaultSize() string { return f.size }
func (f fakeProvider) AdminOnly() bool     { return f.admin }
func (f fakeProvider) Data(context.Context, registry.User) (any, error) {
	if f.calls != nil {
		*f.calls++
	}
	return map[string]string{"widget": f.key}, nil
}

func newService(t *testing.T, providers ...registry.WidgetProvider) *dashboard.Service {
	t.Helper()
	db := testsupport.NewDB(t)
	cat, err := registry.NewCatalog(providers)
	if err != nil {
		t.Fatal(err)
	}
	return dashboard.NewService(cat, dashboard.NewLayoutStore(db))
}

var (
	editor = registry.User{ID: "u1", Roles: []string{"editor"}}
	admin  = registry.User{ID: "a1", Roles: []string{"admin"}}
	reader = registry.User{ID: "r1", Roles: []string{"reader"}}
)

func TestCatalog_FiltersAdminOnly(t *testing.T) {
	svc := newService(t,
		fakeProvider{key: "a.one", size: "wide"},
		fakeProvider{key: "a.two", size: "narrow"},
		fakeProvider{key: "a.admin", size: "narrow", admin: true},
	)
	if got := len(svc.Catalog(editor)); got != 2 {
		t.Errorf("editor catalog = %d, want 2 (admin-only hidden)", got)
	}
	if got := len(svc.Catalog(admin)); got != 3 {
		t.Errorf("admin catalog = %d, want 3", got)
	}
}

func TestDashboard_DefaultLayoutFanOut(t *testing.T) {
	var calls int
	svc := newService(t,
		fakeProvider{key: "a.one", size: "wide", calls: &calls},
		fakeProvider{key: "a.two", size: "narrow", calls: &calls},
	)
	d, err := svc.Dashboard(context.Background(), editor)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Layout) != 2 || len(d.Widgets) != 2 {
		t.Fatalf("default layout=%d widgets=%d, want 2/2", len(d.Layout), len(d.Widgets))
	}
	// Order follows the catalog; sizes are the defaults.
	if d.Widgets[0].Key != "a.one" || d.Widgets[0].Size != "wide" {
		t.Errorf("first widget = %+v, want a.one/wide", d.Widgets[0])
	}
	if calls != 2 {
		t.Errorf("provider Data called %d times, want 2", calls)
	}
}

func TestDashboard_SaveLayoutRoundTripAndHiddenExcluded(t *testing.T) {
	svc := newService(t,
		fakeProvider{key: "a.one", size: "wide"},
		fakeProvider{key: "a.two", size: "narrow"},
	)
	ctx := context.Background()
	// Reorder (two before one), resize one→narrow, hide one, plus an unknown key.
	saved, err := svc.SaveLayout(ctx, editor, []dashboard.LayoutItemInput{
		{WidgetKey: "a.two", Visible: true, Size: "wide"},
		{WidgetKey: "a.one", Visible: false, Size: "narrow"},
		{WidgetKey: "nope", Visible: true, Size: "narrow"}, // ignored (unknown)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 {
		t.Fatalf("saved %d, want 2 (unknown dropped)", len(saved))
	}
	if saved[0].WidgetKey != "a.two" || saved[0].Size != "wide" {
		t.Errorf("saved[0] = %+v, want a.two/wide", saved[0])
	}

	d, err := svc.Dashboard(ctx, editor)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Layout) != 2 {
		t.Fatalf("layout after save = %d, want 2", len(d.Layout))
	}
	if d.Layout[0].WidgetKey != "a.two" {
		t.Errorf("order not persisted: %+v", d.Layout)
	}
	// a.one is hidden → not rendered.
	if len(d.Widgets) != 1 || d.Widgets[0].Key != "a.two" {
		t.Errorf("widgets = %+v, want only a.two visible", d.Widgets)
	}
}

func TestSaveLayout_BadSizeIsUnprocessable(t *testing.T) {
	svc := newService(t, fakeProvider{key: "a.one", size: "wide"})
	_, err := svc.SaveLayout(context.Background(), editor, []dashboard.LayoutItemInput{
		{WidgetKey: "a.one", Visible: true, Size: "huge"},
	})
	if err == nil {
		t.Fatal("expected 422 for a bad size")
	}
}

func TestSaveLayout_IgnoresAdminOnlyForNonAdmin(t *testing.T) {
	svc := newService(t,
		fakeProvider{key: "a.one", size: "wide"},
		fakeProvider{key: "a.admin", size: "narrow", admin: true},
	)
	saved, err := svc.SaveLayout(context.Background(), editor, []dashboard.LayoutItemInput{
		{WidgetKey: "a.one", Visible: true, Size: "wide"},
		{WidgetKey: "a.admin", Visible: true, Size: "narrow"}, // not available to editor
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].WidgetKey != "a.one" {
		t.Errorf("saved = %+v, want only a.one (admin-only dropped)", saved)
	}
}

func TestWidget_RefreshAndErrors(t *testing.T) {
	svc := newService(t,
		fakeProvider{key: "a.one", size: "wide"},
		fakeProvider{key: "a.admin", size: "narrow", admin: true},
	)
	ctx := context.Background()
	inst, err := svc.Widget(ctx, editor, "a.one")
	if err != nil || inst.Key != "a.one" || inst.Size != "wide" {
		t.Errorf("widget refresh = %+v, %v; want a.one/wide", inst, err)
	}
	if _, err := svc.Widget(ctx, editor, "nope"); err == nil {
		t.Error("unknown widget should 404")
	}
	if _, err := svc.Widget(ctx, editor, "a.admin"); err == nil {
		t.Error("admin-only widget should 403 for a non-admin")
	}
}

// A reader may arrange their own dashboard — the single write a reader is allowed
// (D24). The PUT route carries no RequireWrite gate.
func TestReaderMayArrangeLayout_HTTP(t *testing.T) {
	db := testsupport.NewDB(t)
	cat, _ := registry.NewCatalog([]registry.WidgetProvider{fakeProvider{key: "a.one", size: "wide"}})
	mod := dashboard.NewModule(cat, db)
	router := httpx.NewRouter(httpx.Deps{
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		DB:        db,
		Site:      "home",
		SessionMW: auth.NewSessionAuth(auth.Config{BypassActor: &reqctx.Actor{UserID: reader.ID, Type: "user", Roles: reader.Roles}}),
		CSRFMW:    auth.NewCSRF(nil, true), // bypass CSRF in the test
		MountAPI:  func(api chi.Router) { mod.RegisterRoutes(api) },
	})
	req := httptest.NewRequest(http.MethodPut, "/api/dashboard/layout",
		strings.NewReader(`[{"widget_key":"a.one","visible":true,"size":"narrow"}]`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("reader PUT layout = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}
