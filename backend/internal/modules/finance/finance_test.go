package finance_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/bootstrap"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/finance"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// ---- harness ----

type h struct {
	t   *testing.T
	svc *finance.Service
	db  *sql.DB
}

func newH(t *testing.T) *h {
	t.Helper()
	db := testsupport.NewDB(t)
	return &h{t: t, svc: finance.NewService(db, audit.NewSink(), nil), db: db}
}

func editorCtx() context.Context { return testsupport.CtxUser("u-editor", "editor") }

func (x *h) month(m *finance.Month, err error) *finance.Month {
	x.t.Helper()
	if err != nil {
		x.t.Fatalf("month op failed: %v", err)
	}
	return m
}

func ip(v int) *int { return &v }

func rates(p, o, f, n int) *finance.RatesInput {
	return &finance.RatesInput{Personal: ip(p), Operational: ip(o), Fun: ip(f), NoFun: ip(n)}
}

func create(month string, kaja, andy int, r *finance.RatesInput) finance.MonthCreate {
	return finance.MonthCreate{Month: month, IncomeKaja: ip(kaja), IncomeAndy: ip(andy), Rates: r}
}

func status(t *testing.T, err error) int {
	t.Helper()
	var ae *httpx.APIError
	if errors.As(err, &ae) {
		return ae.Status
	}
	t.Fatalf("expected *httpx.APIError, got %v", err)
	return 0
}

// ---- the seed split (D91) — the test that keeps the two migration sources honest ----

// TestSeedIsExcludedFromTestDatabases is the guard on the whole arrangement: a
// module test's database must carry a SCHEMA, not fifteen months of a real
// household's finances. If someone folds the seed into bootstrap.MigrationSources
// this fails, and it is the only thing that would notice.
func TestSeedIsExcludedFromTestDatabases(t *testing.T) {
	db := testsupport.NewDB(t)
	if n := testsupport.CountRows(t, db, "finance_months"); n != 0 {
		t.Fatalf("fresh test database holds %d months, want 0 — the production-only seed "+
			"has leaked into bootstrap.MigrationSources (D91)", n)
	}
}

// TestSeedAppliesUnderMigrationFSWithSeed is the other half: the server's opt-in
// assembly DOES carry `fin`'s history, all fifteen months of it.
func TestSeedAppliesUnderMigrationFSWithSeed(t *testing.T) {
	db, err := appdb.Open(filepath.Join(t.TempDir(), "seeded.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	migFS, err := bootstrap.MigrationFSWithSeed()
	if err != nil {
		t.Fatalf("assemble migrations: %v", err)
	}
	if err := appdb.Migrate(db, migFS); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if n := testsupport.CountRows(t, db, "finance_months"); n != 15 {
		t.Fatalf("seeded database holds %d months, want the 15 exported from fin", n)
	}
	var first, last string
	if err := db.QueryRow(`SELECT MIN(month), MAX(month) FROM finance_months`).Scan(&first, &last); err != nil {
		t.Fatalf("span: %v", err)
	}
	if first != "2025-06" || last != "2026-08" {
		t.Errorf("seeded span = %s…%s, want 2025-06…2026-08", first, last)
	}
	// Seeded rows record no actor: `fin` never had one.
	var withActor int
	if err := db.QueryRow(`SELECT COUNT(*) FROM finance_months WHERE created_by IS NOT NULL`).Scan(&withActor); err != nil {
		t.Fatalf("created_by: %v", err)
	}
	if withActor != 0 {
		t.Errorf("%d seeded rows carry a created_by, want 0", withActor)
	}
}

// ---- CRUD + validation ----

func TestCreateStoresInputsAndReturnsTheSplit(t *testing.T) {
	x := newH(t)
	m := x.month(x.svc.Create(editorCtx(), create("2026-08", 60000, 40000, rates(20, 60, 10, 10))))

	if m.Split.TotalIncome != 100000 || m.Split.PersonalKaja != 12000 || m.Split.Needs != 60000 {
		t.Fatalf("split = %+v, want the worked example", m.Split)
	}
	if m.CreatedBy == nil || *m.CreatedBy != "u-editor" {
		t.Errorf("created_by = %v, want the acting editor", m.CreatedBy)
	}
	// The split is DERIVED, never stored (D82): the table has no column for it.
	var cols int
	if err := x.db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('finance_months') WHERE name LIKE '%needs%' OR name LIKE '%savings%'`,
	).Scan(&cols); err != nil {
		t.Fatal(err)
	}
	if cols != 0 {
		t.Errorf("finance_months has %d split-ish columns; the split must not be stored", cols)
	}
}

func TestDuplicateMonthIsConflict(t *testing.T) {
	x := newH(t)
	x.month(x.svc.Create(editorCtx(), create("2026-08", 1000, 1000, rates(20, 60, 10, 10))))

	_, err := x.svc.Create(editorCtx(), create("2026-08", 2000, 2000, rates(20, 60, 10, 10)))
	if got := status(t, err); got != http.StatusConflict {
		t.Fatalf("duplicate month = %d, want 409", got)
	}
}

func TestValidationRejections(t *testing.T) {
	x := newH(t)
	cases := []struct {
		name string
		in   finance.MonthCreate
	}{
		{"bad month format", create("2026-8", 1000, 1000, rates(20, 60, 10, 10))},
		{"month 13", create("2026-13", 1000, 1000, rates(20, 60, 10, 10))},
		{"negative income", create("2026-08", -1, 1000, rates(20, 60, 10, 10))},
		{"rates below 100", create("2026-08", 1000, 1000, rates(20, 60, 10, 5))},
		{"rates above 100", create("2026-08", 1000, 1000, rates(30, 60, 10, 10))},
		{"negative rate", create("2026-08", 1000, 1000, rates(-10, 70, 20, 20))},
		{"missing rates", finance.MonthCreate{Month: "2026-08", IncomeKaja: ip(1), IncomeAndy: ip(1)}},
		{"partial rates", finance.MonthCreate{Month: "2026-08", IncomeKaja: ip(1), IncomeAndy: ip(1),
			Rates: &finance.RatesInput{Personal: ip(100)}}},
		{"missing income", finance.MonthCreate{Month: "2026-08", IncomeKaja: ip(1), Rates: rates(20, 60, 10, 10)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := x.svc.Create(editorCtx(), c.in)
			if got := status(t, err); got != http.StatusUnprocessableEntity {
				t.Fatalf("%s = %d, want 422", c.name, got)
			}
		})
	}
}

// A partial rates block on UPDATE is 422 too — a single rate is meaningless on
// its own, and silently keeping the other three would be a different month's
// budget (FR-F4).
func TestUpdatePartialRatesIsRejected(t *testing.T) {
	x := newH(t)
	m := x.month(x.svc.Create(editorCtx(), create("2026-08", 60000, 40000, rates(20, 60, 10, 10))))

	_, err := x.svc.Update(editorCtx(), m.ID, finance.MonthUpdate{
		Rates: &finance.RatesInput{Personal: ip(30)},
	})
	if got := status(t, err); got != http.StatusUnprocessableEntity {
		t.Fatalf("partial rates on update = %d, want 422", got)
	}
	// And nothing changed.
	after := x.month(x.svc.Get(editorCtx(), m.ID))
	if after.Rates.Personal != 20 {
		t.Errorf("rates.personal = %d after a rejected update, want the original 20", after.Rates.Personal)
	}
}

func TestUpdateRecomputesAndKeepsUntouchedFields(t *testing.T) {
	x := newH(t)
	m := x.month(x.svc.Create(editorCtx(), create("2026-08", 60000, 40000, rates(20, 60, 10, 10))))

	got := x.month(x.svc.Update(editorCtx(), m.ID, finance.MonthUpdate{IncomeKaja: ip(80000)}))
	if got.IncomeAndy != 40000 || got.Rates.Personal != 20 {
		t.Errorf("omitted fields changed: %+v", got)
	}
	if got.Split.TotalIncome != 120000 || got.Split.PersonalKaja != 16000 {
		t.Errorf("split not recomputed: %+v", got.Split)
	}
	if got.CreatedAt != m.CreatedAt {
		t.Errorf("created_at changed on update: %q → %q", m.CreatedAt, got.CreatedAt)
	}
}

func TestUpdateOntoAnotherMonthIsConflict(t *testing.T) {
	x := newH(t)
	x.month(x.svc.Create(editorCtx(), create("2026-07", 1000, 1000, rates(20, 60, 10, 10))))
	aug := x.month(x.svc.Create(editorCtx(), create("2026-08", 1000, 1000, rates(20, 60, 10, 10))))

	july := "2026-07"
	_, err := x.svc.Update(editorCtx(), aug.ID, finance.MonthUpdate{Month: &july})
	if got := status(t, err); got != http.StatusConflict {
		t.Fatalf("month collision on update = %d, want 409", got)
	}
}

// Delete is PERMANENT (D87): the row is gone and the month is immediately
// re-enterable. There is no soft-deleted row lingering behind the unique index.
func TestDeleteIsPermanentAndTheMonthIsReEnterable(t *testing.T) {
	x := newH(t)
	m := x.month(x.svc.Create(editorCtx(), create("2026-08", 60000, 40000, rates(20, 60, 10, 10))))

	if err := x.svc.Delete(editorCtx(), m.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n := testsupport.CountRows(t, x.db, "finance_months"); n != 0 {
		t.Fatalf("%d rows survive a permanent delete, want 0", n)
	}
	if _, err := x.svc.Get(editorCtx(), m.ID); status(t, err) != http.StatusNotFound {
		t.Error("deleted month is still readable")
	}
	again := x.month(x.svc.Create(editorCtx(), create("2026-08", 1, 2, rates(25, 25, 25, 25))))
	if again.Month != "2026-08" {
		t.Errorf("re-created month = %q", again.Month)
	}
}

func TestDeleteMissingIsNotFound(t *testing.T) {
	x := newH(t)
	if got := status(t, x.svc.Delete(editorCtx(), "no-such-id")); got != http.StatusNotFound {
		t.Fatalf("delete unknown id = %d, want 404", got)
	}
}

func TestListIsNewestFirstAndPages(t *testing.T) {
	x := newH(t)
	for _, ym := range []string{"2026-06", "2026-07", "2026-08"} {
		x.month(x.svc.Create(editorCtx(), create(ym, 1000, 1000, rates(20, 60, 10, 10))))
	}
	page, err := x.svc.List(editorCtx(), 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Month != "2026-08" || page.Items[1].Month != "2026-07" {
		t.Fatalf("first page = %+v, want 2026-08 then 2026-07", page.Items)
	}
	if page.NextCursor == nil || *page.NextCursor != "2026-07" {
		t.Fatalf("next_cursor = %v, want the last month on the page", page.NextCursor)
	}
	rest, err := x.svc.List(editorCtx(), 2, *page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.Items) != 1 || rest.Items[0].Month != "2026-06" || rest.NextCursor != nil {
		t.Fatalf("second page = %+v (cursor %v), want just 2026-06", rest.Items, rest.NextCursor)
	}
}

// ---- audit (D86) ----

func auditChanges(t *testing.T, db *sql.DB, action string) map[string][2]*string {
	t.Helper()
	rows, err := db.Query(`
		SELECT ac.field, ac.old_value, ac.new_value
		FROM audit_changes ac JOIN audit_events e ON e.id = ac.event_id
		WHERE e.module = 'finance' AND e.action = ?`, action)
	if err != nil {
		t.Fatalf("audit changes: %v", err)
	}
	defer rows.Close()
	out := map[string][2]*string{}
	for rows.Next() {
		var field string
		var oldV, newV sql.NullString
		if err := rows.Scan(&field, &oldV, &newV); err != nil {
			t.Fatal(err)
		}
		var o, n *string
		if oldV.Valid {
			v := oldV.String
			o = &v
		}
		if newV.Valid {
			v := newV.String
			n = &v
		}
		out[field] = [2]*string{o, n}
	}
	return out
}

func TestAuditSummariesAreCzechSentences(t *testing.T) {
	x := newH(t)
	m := x.month(x.svc.Create(editorCtx(), create("2026-08", 60000, 40000, rates(20, 60, 10, 10))))
	x.month(x.svc.Update(editorCtx(), m.ID, finance.MonthUpdate{IncomeKaja: ip(61000)}))
	if err := x.svc.Delete(editorCtx(), m.ID); err != nil {
		t.Fatal(err)
	}

	// A v5 trigger rule's default body IS the summary (D55), so these have to read
	// as the notification you would want to receive.
	want := map[string]string{
		"month.create": "Přidán měsíc srpen 2026",
		"month.update": "Upraven měsíc srpen 2026",
		"month.delete": "Smazán měsíc srpen 2026",
	}
	for action, wantSummary := range want {
		var got string
		if err := x.db.QueryRow(
			`SELECT summary FROM audit_events WHERE module='finance' AND action=?`, action).Scan(&got); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if got != wantSummary {
			t.Errorf("%s summary = %q, want %q", action, got, wantSummary)
		}
	}
}

func TestAuditUpdateCarriesOnlyTheChangedFields(t *testing.T) {
	x := newH(t)
	m := x.month(x.svc.Create(editorCtx(), create("2026-08", 60000, 40000, rates(20, 60, 10, 10))))
	x.month(x.svc.Update(editorCtx(), m.ID, finance.MonthUpdate{Rates: rates(30, 50, 10, 10)}))

	ch := auditChanges(t, x.db, "month.update")
	personal, ok := ch["rate_personal"]
	if !ok {
		t.Fatalf("month.update has no rate_personal diff; fields = %v", keys(ch))
	}
	if personal[0] == nil || *personal[0] != "20" || personal[1] == nil || *personal[1] != "30" {
		t.Errorf("rate_personal diff = %v → %v, want 20 → 30", deref(personal[0]), deref(personal[1]))
	}
	if _, unchanged := ch["income_kaja"]; unchanged {
		t.Error("month.update recorded income_kaja, which did not change")
	}
}

// The month.delete full-row diff is the compensating control that makes a hard
// delete acceptable (D87): the row is gone, so this event is the ONLY surviving
// record of what the month held. A delete event carrying just the id would have
// thrown the data away.
func TestAuditDeleteCarriesTheWholeRow(t *testing.T) {
	x := newH(t)
	m := x.month(x.svc.Create(editorCtx(), create("2026-08", 67418, 52299, rates(20, 60, 10, 10))))
	if err := x.svc.Delete(editorCtx(), m.ID); err != nil {
		t.Fatal(err)
	}

	ch := auditChanges(t, x.db, "month.delete")
	want := map[string]string{
		"month": "2026-08", "income_kaja": "67418", "income_andy": "52299",
		"rate_personal": "20", "rate_operational": "60", "rate_fun": "10", "rate_nofun": "10",
	}
	if len(ch) != len(want) {
		t.Fatalf("month.delete carries %d fields (%v), want all %d", len(ch), keys(ch), len(want))
	}
	for field, oldValue := range want {
		got, ok := ch[field]
		if !ok {
			t.Errorf("month.delete is missing %s — the deleted value is unrecoverable", field)
			continue
		}
		if got[0] == nil || *got[0] != oldValue {
			t.Errorf("%s old value = %v, want %q", field, deref(got[0]), oldValue)
		}
		if got[1] != nil {
			t.Errorf("%s new value = %v, want NULL (the row is gone)", field, deref(got[1]))
		}
	}
}

// A failed mutation must leave neither a row nor an audit event: the change and
// its log commit or roll back together.
func TestFailedMutationWritesNoAuditEvent(t *testing.T) {
	x := newH(t)
	x.month(x.svc.Create(editorCtx(), create("2026-08", 1000, 1000, rates(20, 60, 10, 10))))
	before := testsupport.CountRows(t, x.db, "audit_events")

	if _, err := x.svc.Create(editorCtx(), create("2026-08", 5, 5, rates(20, 60, 10, 10))); err == nil {
		t.Fatal("expected the duplicate month to fail")
	}
	if got := testsupport.CountRows(t, x.db, "audit_events"); got != before {
		t.Errorf("audit events = %d after a failed create, want %d", got, before)
	}
	if got := testsupport.CountRows(t, x.db, "finance_months"); got != 1 {
		t.Errorf("finance_months = %d, want 1", got)
	}
}

// ---- HTTP + role gating (D84) ----

func router(t *testing.T, roles ...string) http.Handler {
	t.Helper()
	db := testsupport.NewDB(t)
	h := finance.NewHandler(finance.NewService(db, audit.NewSink(), nil))
	return testsupport.Router(t, db, h.Mount, roles...)
}

func send(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return testsupport.Send(t, h, method, path, body)
}

const validBody = `{"month":"2026-08","income_kaja":60000,"income_andy":40000,` +
	`"rates":{"personal":20,"operational":60,"fun":10,"no_fun":10}}`

// A reader sees the whole module and can change none of it — including delete,
// which carries the ORDINARY write gate, not an admin one (D87 resolved
// OQ-V6-1: with a hard delete there is no soft/hard distinction left to tier).
func TestHTTPRoleGating(t *testing.T) {
	reader := router(t, "reader")
	if rr := send(t, reader, http.MethodGet, "/api/finance/months", ""); rr.Code != http.StatusOK {
		t.Errorf("reader GET = %d, want 200", rr.Code)
	}
	if rr := send(t, reader, http.MethodPost, "/api/finance/months", validBody); rr.Code != http.StatusForbidden {
		t.Errorf("reader POST = %d, want 403", rr.Code)
	}
	if rr := send(t, reader, http.MethodDelete, "/api/finance/months/x", ""); rr.Code != http.StatusForbidden {
		t.Errorf("reader DELETE = %d, want 403", rr.Code)
	}

	editor := router(t, "editor")
	rr := send(t, editor, http.MethodPost, "/api/finance/months", validBody)
	if rr.Code != http.StatusCreated {
		t.Fatalf("editor POST = %d, want 201: %s", rr.Code, rr.Body.String())
	}
	// An EDITOR may delete: there is no admin-only route anywhere in this module.
	var created struct {
		ID string `json:"id"`
	}
	decode(t, rr, &created)
	if rr := send(t, editor, http.MethodDelete, "/api/finance/months/"+created.ID, ""); rr.Code != http.StatusNoContent {
		t.Errorf("editor DELETE = %d, want 204", rr.Code)
	}
}

func TestHTTPWireFormatIsSnakeCase(t *testing.T) {
	editor := router(t, "editor")
	rr := send(t, editor, http.MethodPost, "/api/finance/months", validBody)
	if rr.Code != http.StatusCreated {
		t.Fatalf("POST = %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, field := range []string{`"no_fun"`, `"total_income"`, `"personal_kaja"`,
		`"to_operational_kaja"`, `"operational_received"`, `"fun_savings"`, `"no_fun_savings"`, `"needs"`} {
		if !strings.Contains(body, field) {
			t.Errorf("response is missing %s (D92: the wire format is home's, not fin's): %s", field, body)
		}
	}
	// fin's camelCase must not survive anywhere.
	for _, stale := range []string{"noFun", "totalIncome", "personalKaja", "funSavings"} {
		if strings.Contains(body, stale) {
			t.Errorf("response still carries fin's %q", stale)
		}
	}
}

func TestHTTPPatchNotPut(t *testing.T) {
	editor := router(t, "editor")
	rr := send(t, editor, http.MethodPost, "/api/finance/months", validBody)
	var created struct {
		ID string `json:"id"`
	}
	decode(t, rr, &created)

	if rr := send(t, editor, http.MethodPatch, "/api/finance/months/"+created.ID,
		`{"income_kaja":70000}`); rr.Code != http.StatusOK {
		t.Errorf("PATCH = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if rr := send(t, editor, http.MethodPut, "/api/finance/months/"+created.ID,
		validBody); rr.Code == http.StatusOK {
		t.Error("PUT is accepted; v6 dropped it in favour of PATCH (D92)")
	}
}

// ---- widget, metrics, list ----

func module(x *h) *finance.Module { return finance.NewModule(x.svc, time.UTC) }

func TestWidgetRendersBothStates(t *testing.T) {
	x := newH(t)
	m := module(x)
	provider := m.Widgets()[0]

	if provider.Key() != "finance.rozpocet" {
		t.Fatalf("widget key = %q", provider.Key())
	}
	if provider.AdminOnly() {
		t.Error("the widget is admin-only; Finance is an all-roles module (D84)")
	}

	// Missing: the state that matters — nobody has entered the current month.
	got, err := provider.Data(context.Background(), registry.User{ID: "u", Roles: []string{"reader"}})
	if err != nil {
		t.Fatal(err)
	}
	w := got.(finance.RozpocetWidget)
	nowMonth := time.Now().UTC().Format("2006-01")
	if w.State != "missing" || w.Month != nowMonth {
		t.Fatalf("widget = %+v, want the missing state for %s", w, nowMonth)
	}
	if w.TotalIncome != nil {
		t.Error("missing state carries numbers")
	}

	// Recorded.
	x.month(x.svc.Create(editorCtx(), create(nowMonth, 60000, 40000, rates(20, 60, 10, 10))))
	got, err = provider.Data(context.Background(), registry.User{ID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	w = got.(finance.RozpocetWidget)
	if w.State != "recorded" {
		t.Fatalf("widget = %+v, want the recorded state", w)
	}
	if w.TotalIncome == nil || *w.TotalIncome != 100000 {
		t.Errorf("total_income = %v, want 100000", w.TotalIncome)
	}
	if w.Savings == nil || *w.Savings != 20000 {
		t.Errorf("savings = %v, want fun + no-fun = 20000", w.Savings)
	}
}

func TestMetricDescriptorsAreAllHousehold(t *testing.T) {
	x := newH(t)
	got := module(x).MetricProvider().Descriptors()
	if len(got) != 4 {
		t.Fatalf("descriptors = %d, want 4", len(got))
	}
	for _, d := range got {
		// A household budget has no per-recipient value (D89).
		if d.Scope != "household" {
			t.Errorf("%s scope = %q, want household", d.Key, d.Scope)
		}
		if d.Label == "" || d.Unit == "" {
			t.Errorf("%s is missing a label or unit: %+v", d.Key, d)
		}
	}
}

func TestCurrentMonthMetricsUseAsOfNotWallClock(t *testing.T) {
	x := newH(t)
	p := module(x).MetricProvider()
	x.month(x.svc.Create(editorCtx(), create("2026-08", 60000, 40000, rates(20, 60, 10, 10))))

	asOf := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	income, err := p.Value(context.Background(), "", finance.MetricTotalIncomeCurrent, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if income != 100000 {
		t.Errorf("total_income_current = %d, want 100000", income)
	}
	savings, err := p.Value(context.Background(), "", finance.MetricSavingsCurrent, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if savings != 20000 {
		t.Errorf("savings_current = %d, want 20000", savings)
	}

	// A month with no row reports 0, not an error: an unrecorded month is a fact
	// about the household, not a failure.
	other := time.Date(2026, 9, 1, 0, 1, 0, 0, time.UTC)
	if v, err := p.Value(context.Background(), "", finance.MetricTotalIncomeCurrent, other); err != nil || v != 0 {
		t.Errorf("unrecorded month = %d (err %v), want 0", v, err)
	}
}

// The metric and the list of the SAME KEY must be the same answer expressed two
// ways — same store read, same gap computation (D90, D77's rule). This test is
// what makes that structural rather than aspirational.
func TestMissingMonthsMetricAndListAgree(t *testing.T) {
	x := newH(t)
	m := module(x)
	// A gapped history: May and July are recorded, June is not, and August (the
	// asOf month) is not either.
	for _, ym := range []string{"2026-05", "2026-07"} {
		x.month(x.svc.Create(editorCtx(), create(ym, 1000, 1000, rates(20, 60, 10, 10))))
	}
	asOf := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
	ctx := context.Background()

	count, err := m.MetricProvider().Value(ctx, "", finance.MetricMissingMonths, asOf)
	if err != nil {
		t.Fatal(err)
	}
	items, err := m.ListProvider().Items(ctx, "", finance.ListMissingMonths, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if count != len(items) {
		t.Fatalf("metric = %d but the list has %d items — the two have drifted apart", count, len(items))
	}
	want := []string{"červen 2026", "srpen 2026"}
	if len(items) != len(want) {
		t.Fatalf("items = %v, want %v", items, want)
	}
	for i := range want {
		if items[i] != want[i] {
			t.Errorf("items[%d] = %q, want %q (ascending, oldest gap first)", i, items[i], want[i])
		}
	}

	recorded, err := m.MetricProvider().Value(ctx, "", finance.MetricMonthsRecorded, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if recorded != 2 {
		t.Errorf("months_recorded = %d, want 2", recorded)
	}
}

// With nothing recorded there is no earliest month to count from — a brand-new
// household is not told it is four hundred months behind.
func TestMissingMonthsIsEmptyWithNoHistory(t *testing.T) {
	x := newH(t)
	m := module(x)
	asOf := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)

	count, err := m.MetricProvider().Value(context.Background(), "", finance.MetricMissingMonths, asOf)
	if err != nil {
		t.Fatal(err)
	}
	items, err := m.ListProvider().Items(context.Background(), "", finance.ListMissingMonths, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 || len(items) != 0 {
		t.Errorf("empty history reports %d missing months (%v), want none", count, items)
	}
}

func TestUnknownCatalogKeysError(t *testing.T) {
	x := newH(t)
	m := module(x)
	if _, err := m.MetricProvider().Value(context.Background(), "", "finance.nonsense", time.Now()); err == nil {
		t.Error("expected an error for an unknown metric key")
	}
	if _, err := m.ListProvider().Items(context.Background(), "", "finance.nonsense", time.Now()); err == nil {
		t.Error("expected an error for an unknown list key")
	}
}

func TestModuleDeclaresItsAuditActions(t *testing.T) {
	x := newH(t)
	got := module(x).AuditActions()
	want := map[string]bool{"month.create": true, "month.update": true, "month.delete": true}
	if len(got) != len(want) {
		t.Fatalf("audit actions = %v, want %v", got, keys(want))
	}
	for _, a := range got {
		if !want[a] {
			t.Errorf("unexpected audit action %q", a)
		}
	}
}

// ---- small helpers ----

func decode(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %s: %v", rr.Body.String(), err)
	}
}

func deref(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
