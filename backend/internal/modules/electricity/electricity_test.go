package electricity_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/electricity"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

type h struct {
	t   *testing.T
	svc *electricity.Service
	db  *sql.DB
}

func newH(t *testing.T) *h {
	t.Helper()
	db := testsupport.NewDB(t)
	prague, err := time.LoadLocation("Europe/Prague")
	if err != nil {
		prague = time.UTC
	}
	return &h{t: t, svc: electricity.NewService(db, audit.NewSink(), nil, prague), db: db}
}

func editorCtx() context.Context { return testsupport.CtxUser("u-editor", "editor") }

func d(s string) dates.Date {
	v, err := dates.Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}

func dp(s string) *dates.Date { v := d(s); return &v }
func i64(v int64) *int64      { return &v }
func ip(v int) *int           { return &v }

func status(t *testing.T, err error) int {
	t.Helper()
	var ae *httpx.APIError
	if errors.As(err, &ae) {
		return ae.Status
	}
	t.Fatalf("expected *httpx.APIError, got %v", err)
	return 0
}

// addReading enters a reading in WHOLE kWh, the way the form does (D148): the
// wire carries tenths, so the helper multiplies, exactly as the UI will.
func addReading(t *testing.T, x *h, on string, vt, nt int64) electricity.Reading {
	t.Helper()
	r, err := x.svc.CreateReading(editorCtx(), electricity.ReadingInput{
		ReadOn: dp(on), VTDkwh: i64(vt * 10), NTDkwh: i64(nt * 10),
	})
	if err != nil {
		t.Fatalf("reading %s failed: %v", on, err)
	}
	return r
}

// ---------------------------------------------------------------------------
// The module ships NO seed (D152) — the counterpart of finance's and garden's
// seed-exclusion tests, asserting the opposite thing.
// ---------------------------------------------------------------------------

func TestModuleHasNoSeedSource(t *testing.T) {
	db := testsupport.NewDB(t)
	for _, table := range []string{
		"electricity_readings", "electricity_tariffs", "electricity_advances",
		"electricity_payments", "electricity_periods",
	} {
		if n := testsupport.CountRows(t, db, table); n != 0 {
			t.Errorf("%s holds %d rows on a fresh database, want 0 — this module has no seed "+
				"source and must not grow one (D152)", table, n)
		}
	}
}

// ---------------------------------------------------------------------------
// Monotonicity (FR-E1) — BOTH neighbours, BOTH directions
// ---------------------------------------------------------------------------

func TestReadingMonotonicityChecksBothNeighbours(t *testing.T) {
	x := newH(t)
	addReading(t, x, "2026-04-01", 12000, 30000)
	addReading(t, x, "2026-08-01", 12640, 31480)

	// Too low: caught against the EARLIER neighbour.
	_, err := x.svc.CreateReading(editorCtx(), electricity.ReadingInput{
		ReadOn: dp("2026-06-01"), VTDkwh: i64(11900 * 10), NTDkwh: i64(31000 * 10),
	})
	if got := status(t, err); got != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", got)
	}
	if !strings.Contains(err.Error(), "1. 4. 2026") {
		t.Errorf("message must name the offending neighbour, got %q", err.Error())
	}

	// Too high: caught against the LATER neighbour. This is the case a
	// prev-only check would miss — the back-filled row breaks the chain in
	// front of it and the meter appears to run backwards over the summer.
	_, err = x.svc.CreateReading(editorCtx(), electricity.ReadingInput{
		ReadOn: dp("2026-06-01"), VTDkwh: i64(12800 * 10), NTDkwh: i64(31000 * 10),
	})
	if got := status(t, err); got != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", got)
	}
	if !strings.Contains(err.Error(), "1. 8. 2026") {
		t.Errorf("message must name the LATER neighbour, got %q", err.Error())
	}

	// A value between both neighbours is fine.
	addReading(t, x, "2026-06-01", 12300, 31000)
}

func TestReadingDuplicateDateIs409(t *testing.T) {
	x := newH(t)
	addReading(t, x, "2026-04-01", 12000, 30000)
	_, err := x.svc.CreateReading(editorCtx(), electricity.ReadingInput{
		ReadOn: dp("2026-04-01"), VTDkwh: i64(120000), NTDkwh: i64(300000),
	})
	if got := status(t, err); got != http.StatusConflict {
		t.Errorf("status = %d, want 409", got)
	}
}

// TestDeletedReadingReleasesItsDate is why the unique index is PARTIAL: delete a
// mistyped odečet and you must be able to enter the right one for the same day.
func TestDeletedReadingReleasesItsDate(t *testing.T) {
	x := newH(t)
	r := addReading(t, x, "2026-04-01", 12000, 30000)
	if err := x.svc.DeleteReading(editorCtx(), r.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	addReading(t, x, "2026-04-01", 12500, 30500)
}

// TestDeleteNeedsNoMonotonicityCheck — if a <= b <= c then a <= c, so removing a
// middle reading merges two valid intervals into one.
func TestDeleteNeedsNoMonotonicityCheck(t *testing.T) {
	x := newH(t)
	addReading(t, x, "2026-04-01", 12000, 30000)
	mid := addReading(t, x, "2026-06-01", 12300, 31000)
	addReading(t, x, "2026-08-01", 12640, 31480)
	if err := x.svc.DeleteReading(editorCtx(), mid.ID); err != nil {
		t.Fatalf("deleting a middle reading must always be allowed: %v", err)
	}
}

func TestUpdateReadingRevalidatesAgainstNewNeighbours(t *testing.T) {
	x := newH(t)
	addReading(t, x, "2026-04-01", 12000, 30000)
	mid := addReading(t, x, "2026-06-01", 12300, 31000)
	addReading(t, x, "2026-08-01", 12640, 31480)

	// Moving the value below the earlier neighbour is refused.
	_, err := x.svc.UpdateReading(editorCtx(), mid.ID, electricity.ReadingInput{VTDkwh: i64(110000)})
	if got := status(t, err); got != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", got)
	}
	// So is moving the DATE somewhere the value no longer fits.
	_, err = x.svc.UpdateReading(editorCtx(), mid.ID, electricity.ReadingInput{ReadOn: dp("2026-03-01")})
	if got := status(t, err); got != http.StatusUnprocessableEntity {
		t.Errorf("moving the date must re-validate against the new neighbours, status = %d", got)
	}
}

// ---------------------------------------------------------------------------
// Periods
// ---------------------------------------------------------------------------

func TestPeriodOverlapIs422ButAdjacencyIsFine(t *testing.T) {
	x := newH(t)
	_, err := x.svc.CreatePeriod(editorCtx(), electricity.PeriodInput{
		StartsOn: dp("2026-06-24"), EndsOn: dp("2027-06-23"),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = x.svc.CreatePeriod(editorCtx(), electricity.PeriodInput{
		StartsOn: dp("2027-01-01"), EndsOn: dp("2027-12-31"),
	})
	if got := status(t, err); got != http.StatusUnprocessableEntity {
		t.Errorf("overlap status = %d, want 422", got)
	}

	// Adjacency is the NORMAL case: one meter reading closes one period and
	// opens the next (D134), so 23. 6. followed by 24. 6. must be allowed.
	if _, err := x.svc.CreatePeriod(editorCtx(), electricity.PeriodInput{
		StartsOn: dp("2027-06-24"), EndsOn: dp("2028-06-23"),
	}); err != nil {
		t.Errorf("adjacent periods must be allowed: %v", err)
	}
}

// TestPeriodEndDefaultsToAYearMinusADay pins D153.
func TestPeriodEndDefaultsToAYearMinusADay(t *testing.T) {
	x := newH(t)
	p, err := x.svc.CreatePeriod(editorCtx(), electricity.PeriodInput{StartsOn: dp("2026-06-24")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := p.EndsOn.String(); got != "2027-06-23" {
		t.Errorf("default ends_on = %s, want 2027-06-23", got)
	}
	if p.EndsOnConfirmed {
		t.Error("a defaulted end must start UNCONFIRMED — the supplier has not stated it (D153)")
	}
}

// TestPeriodNeverLocks pins D139. A period carrying a recorded vyúčtování stays
// fully editable; there is no close ritual and no admin tier.
func TestPeriodNeverLocks(t *testing.T) {
	x := newH(t)
	p, err := x.svc.CreatePeriod(editorCtx(), electricity.PeriodInput{
		StartsOn: dp("2026-06-24"), EndsOn: dp("2027-06-23"),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := x.svc.UpdatePeriod(editorCtx(), p.ID, electricity.PeriodInput{
		InvoicedTotalHaler: i64(2_189_000), InvoicedTotalSet: true,
	}); err != nil {
		t.Fatalf("record invoice: %v", err)
	}
	// Still editable afterwards — every field, including the dates.
	if _, err := x.svc.UpdatePeriod(editorCtx(), p.ID, electricity.PeriodInput{
		EndsOn: dp("2027-07-31"), EndsOnConfirmed: &[]bool{true}[0],
	}); err != nil {
		t.Errorf("a period with an invoice must stay editable (D139): %v", err)
	}
	if err := x.svc.DeletePeriod(editorCtx(), p.ID); err != nil {
		t.Errorf("delete must not be gated either: %v", err)
	}
}

// ---------------------------------------------------------------------------
// D160 — the narrow tariff-delete 409
// ---------------------------------------------------------------------------

func TestTariffDeleteIsRefusedOnlyWhenItWouldStrandAPeriod(t *testing.T) {
	x := newH(t)
	if _, err := x.svc.CreatePeriod(editorCtx(), electricity.PeriodInput{
		StartsOn: dp("2026-06-24"), EndsOn: dp("2027-06-23"),
	}); err != nil {
		t.Fatalf("period: %v", err)
	}
	opening, err := x.svc.CreateTariff(editorCtx(), electricity.TariffInput{
		EffectiveFrom: dp("2026-06-24"), PriceVTHaler: i64(485865),
		PriceNTHaler: i64(402669), MonthlyFeeHaler: i64(64235),
	})
	if err != nil {
		t.Fatalf("tariff: %v", err)
	}
	middle, err := x.svc.CreateTariff(editorCtx(), electricity.TariffInput{
		EffectiveFrom: dp("2027-01-01"), PriceVTHaler: i64(500000),
		PriceNTHaler: i64(410000), MonthlyFeeHaler: i64(65000),
	})
	if err != nil {
		t.Fatalf("tariff: %v", err)
	}
	future, err := x.svc.CreateTariff(editorCtx(), electricity.TariffInput{
		EffectiveFrom: dp("2030-01-01"), PriceVTHaler: i64(600000),
		PriceNTHaler: i64(500000), MonthlyFeeHaler: i64(70000),
	})
	if err != nil {
		t.Fatalf("tariff: %v", err)
	}

	// A MIDDLE version deletes cleanly — it legitimately reprices its days.
	if err := x.svc.DeleteTariff(editorCtx(), middle.ID); err != nil {
		t.Errorf("deleting a middle version must be allowed (D160): %v", err)
	}
	// So does a purely future one.
	if err := x.svc.DeleteTariff(editorCtx(), future.ID); err != nil {
		t.Errorf("deleting a future version must be allowed: %v", err)
	}
	// The one covering the period's start is the only load-bearing version.
	err = x.svc.DeleteTariff(editorCtx(), opening.ID)
	if got := status(t, err); got != http.StatusConflict {
		t.Errorf("status = %d, want 409", got)
	}
	if !strings.Contains(err.Error(), "24. 6. 2026") {
		t.Errorf("the refusal must name the stranded period, got %q", err.Error())
	}
}

// A period that ALREADY has no version covering its first day must not make the
// guard refuse everything. The NOT EXISTS half of the predicate is satisfied by
// such a period whatever is being deleted, so on its own it fires for a version
// dated AFTER the period start — and then says that version "is the only one
// effective at the start", which is false and traps a mistyped effective_from
// with no way to remove it.
func TestTariffDeleteIsAllowedWhenItNeverCoveredAPeriodStart(t *testing.T) {
	x := newH(t)
	if _, err := x.svc.CreatePeriod(editorCtx(), electricity.PeriodInput{
		StartsOn: dp("2026-06-24"), EndsOn: dp("2027-06-23"),
	}); err != nil {
		t.Fatalf("period: %v", err)
	}
	// The only ceník in the database starts a week AFTER the period does.
	late, err := x.svc.CreateTariff(editorCtx(), electricity.TariffInput{
		EffectiveFrom: dp("2026-07-01"), PriceVTHaler: i64(485865),
		PriceNTHaler: i64(402669), MonthlyFeeHaler: i64(64235),
	})
	if err != nil {
		t.Fatalf("tariff: %v", err)
	}
	if err := x.svc.DeleteTariff(editorCtx(), late.ID); err != nil {
		t.Errorf("deleting a version that never governed the period's first day must be "+
			"allowed — it cannot strand a day it never covered: %v", err)
	}
}

// ---------------------------------------------------------------------------
// due_day validation — refused at the edges, never CLAMPED on write (D155)
// ---------------------------------------------------------------------------

func TestDueDayIsValidatedNotClamped(t *testing.T) {
	x := newH(t)
	for _, bad := range []int{0, 32, -1} {
		_, err := x.svc.CreateAdvance(editorCtx(), electricity.AdvanceInput{
			EffectiveFrom: dp("2026-06-24"), AmountHaler: i64(150000), DueDay: ip(bad),
		})
		if got := status(t, err); got != http.StatusUnprocessableEntity {
			t.Errorf("due_day %d: status = %d, want 422", bad, got)
		}
	}
	a, err := x.svc.CreateAdvance(editorCtx(), electricity.AdvanceInput{
		EffectiveFrom: dp("2026-06-24"), AmountHaler: i64(150000), DueDay: ip(31),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// STORED RAW. A stored clamp would be right for one February and wrong for
	// every March after it.
	var stored int
	if err := x.db.QueryRow(`SELECT due_day FROM electricity_advances WHERE id = ?`, a.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 31 {
		t.Errorf("stored due_day = %d, want the raw 31 (D155)", stored)
	}
}

// ---------------------------------------------------------------------------
// The audit spine — in the SAME transaction
// ---------------------------------------------------------------------------

func TestEveryMutationWritesExactlyOneAuditEvent(t *testing.T) {
	x := newH(t)
	r := addReading(t, x, "2026-06-24", 32, 70)
	if n := auditCount(t, x.db, "reading.create"); n != 1 {
		t.Errorf("reading.create events = %d, want 1", n)
	}
	if _, err := x.svc.UpdateReading(editorCtx(), r.ID, electricity.ReadingInput{VTDkwh: i64(330)}); err != nil {
		t.Fatal(err)
	}
	if n := auditCount(t, x.db, "reading.update"); n != 1 {
		t.Errorf("reading.update events = %d, want 1", n)
	}
	if err := x.svc.DeleteReading(editorCtx(), r.ID); err != nil {
		t.Fatal(err)
	}
	if n := auditCount(t, x.db, "reading.delete"); n != 1 {
		t.Errorf("reading.delete events = %d, want 1", n)
	}
	// Every event belongs to this module, and none has a system actor: there is
	// no background job here, so nothing can be written by anything but a person.
	var wrongModule int
	if err := x.db.QueryRow(
		`SELECT COUNT(*) FROM audit_events WHERE action LIKE 'reading.%' AND module <> 'electricity'`,
	).Scan(&wrongModule); err != nil {
		t.Fatal(err)
	}
	if wrongModule != 0 {
		t.Errorf("%d events carry the wrong module", wrongModule)
	}
}

// TestRolledBackWriteLeavesNoAuditEvent is the spine's real claim: the row
// change and its event commit together or not at all.
func TestRolledBackWriteLeavesNoAuditEvent(t *testing.T) {
	x := newH(t)
	addReading(t, x, "2026-04-01", 12000, 30000)
	before := testsupport.CountRows(t, x.db, "audit_events")

	// Refused by the monotonicity guard INSIDE the transaction.
	if _, err := x.svc.CreateReading(editorCtx(), electricity.ReadingInput{
		ReadOn: dp("2026-05-01"), VTDkwh: i64(1000), NTDkwh: i64(1000),
	}); err == nil {
		t.Fatal("expected the write to be refused")
	}
	if got := testsupport.CountRows(t, x.db, "audit_events"); got != before {
		t.Errorf("audit_events = %d, want %d — a rolled-back write must leave no event", got, before)
	}
	if got := testsupport.CountRows(t, x.db, "electricity_readings"); got != 1 {
		t.Errorf("electricity_readings = %d, want 1", got)
	}
}

func TestTariffUpdateCarriesFieldLevelDiffs(t *testing.T) {
	x := newH(t)
	tr, err := x.svc.CreateTariff(editorCtx(), electricity.TariffInput{
		EffectiveFrom: dp("2026-06-24"), PriceVTHaler: i64(485865),
		PriceNTHaler: i64(402669), MonthlyFeeHaler: i64(64235),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x.svc.UpdateTariff(editorCtx(), tr.ID, electricity.TariffInput{
		PriceVTHaler: i64(500000),
	}); err != nil {
		t.Fatal(err)
	}
	// "Who changed the VT price and to what" is the question these entries exist
	// to answer a year later, when the vyúčtování disagrees. The diffs live in
	// audit_changes, one row per changed field.
	rows, err := x.db.Query(
		`SELECT c.field, c.old_value, c.new_value FROM audit_changes c
		 JOIN audit_events e ON e.id = c.event_id
		 WHERE e.action = 'tariff.update' AND e.module = 'electricity'`)
	if err != nil {
		t.Fatalf("query diffs: %v", err)
	}
	defer rows.Close()

	got := map[string][2]string{}
	for rows.Next() {
		var field string
		var oldV, newV sql.NullString
		if err := rows.Scan(&field, &oldV, &newV); err != nil {
			t.Fatal(err)
		}
		got[field] = [2]string{oldV.String, newV.String}
	}
	if v, ok := got["price_vt_haler"]; !ok || v[0] != "485865" || v[1] != "500000" {
		t.Errorf("price_vt_haler diff = %v, want 485865 → 500000", v)
	}
	// An UNCHANGED field must not appear: a diff that lists every column tells
	// you nothing about what the person actually did.
	if _, ok := got["price_nt_haler"]; ok {
		t.Errorf("an unchanged field must not appear in the diff, got %v", got)
	}
	if _, ok := got["monthly_fee_haler"]; ok {
		t.Errorf("an unchanged field must not appear in the diff, got %v", got)
	}
}

// auditCount narrows to THIS module as well as the action, which is what the copy
// it replaced did. The narrowing is belt-and-braces rather than load-bearing:
// testsupport.NewDB migrates an empty temp file per test and seeds nothing, so no
// other module writes into this database today.
func auditCount(t *testing.T, db *sql.DB, action string) int {
	return testsupport.CountAudit(t, db, "electricity", action)
}

// ---------------------------------------------------------------------------
// Module registration — what it publishes, and what it must not
// ---------------------------------------------------------------------------

// TestModulePublishesNothing is D147 as a test rather than a comment, so a
// future refactor cannot quietly add a widget, a metric or a list.
func TestModulePublishesNothing(t *testing.T) {
	m := electricity.NewModule(nil)

	if w := m.Widgets(); w != nil {
		t.Errorf("Widgets() = %v, want nil — Elektřina puts NOTHING on Nástěnka (D147). "+
			"Note: nil, not an empty non-nil slice, which would put an empty section "+
			"in the admin composer", w)
	}
	// metrics.Collect and lists.Collect are duck-typed on their Source
	// interfaces, so NOT implementing them is the mechanism that keeps this
	// module out of both catalogs. Assert the absence directly.
	if _, ok := any(m).(interface{ MetricProvider() any }); ok {
		t.Error("the module implements MetricProvider — it must publish no metric (D147)")
	}
	if _, ok := any(m).(interface{ ListProvider() any }); ok {
		t.Error("the module implements ListProvider — it must publish no list (D147)")
	}
	if got := m.Name(); got != "electricity" {
		t.Errorf("Name() = %q", got)
	}
	if got := len(m.AuditActions()); got != 15 {
		t.Errorf("AuditActions() = %d verbs, want 15", got)
	}
	// A module with no widget contributes nothing to the catalog.
	if got := registry.CollectWidgets([]registry.Module{m}); len(got) != 0 {
		t.Errorf("CollectWidgets = %d providers, want 0", len(got))
	}
}

// ---------------------------------------------------------------------------
// HTTP: role gating, cursors, and the D161 nulls on the wire
// ---------------------------------------------------------------------------

// router hands back the Service alongside the handler: several tests here seed
// through the service and then assert over HTTP, and the two must share one
// database.
func router(t *testing.T, roles ...string) (http.Handler, *electricity.Service) {
	t.Helper()
	db := testsupport.NewDB(t)
	svc := electricity.NewService(db, audit.NewSink(), nil, time.UTC)
	hd := electricity.NewHandler(svc)
	return testsupport.Router(t, db, hd.Mount, roles...), svc
}

func send(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return testsupport.Send(t, h, method, path, body)
}

// TestReaderReadsEverythingAndWritesNothing — an ordinary all-roles module with
// NO admin-only route (D151), delete included.
func TestReaderReadsEverythingAndWritesNothing(t *testing.T) {
	reader, _ := router(t, "reader")

	for _, path := range []string{
		"/api/electricity/readings", "/api/electricity/tariffs",
		"/api/electricity/advances", "/api/electricity/payments",
		"/api/electricity/periods",
	} {
		if rr := send(t, reader, http.MethodGet, path, ""); rr.Code != http.StatusOK {
			t.Errorf("GET %s as reader = %d, want 200", path, rr.Code)
		}
	}
	// The computed reads 404 with no period, which is the honest first-run
	// answer ("create a period"), not a permission problem.
	if rr := send(t, reader, http.MethodGet, "/api/electricity/summary", ""); rr.Code != http.StatusNotFound {
		t.Errorf("GET summary with no period = %d, want 404", rr.Code)
	}

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/electricity/readings", `{"read_on":"2026-06-24","vt_dkwh":320,"nt_dkwh":700}`},
		{http.MethodPost, "/api/electricity/tariffs", `{"effective_from":"2026-06-24","price_vt_haler":1,"price_nt_haler":1,"monthly_fee_haler":1}`},
		{http.MethodPost, "/api/electricity/periods", `{"starts_on":"2026-06-24"}`},
		{http.MethodDelete, "/api/electricity/readings/x", ""},
	} {
		if rr := send(t, reader, tc.method, tc.path, tc.body); rr.Code != http.StatusForbidden {
			t.Errorf("%s %s as reader = %d, want 403", tc.method, tc.path, rr.Code)
		}
	}
}

// TestMalformedCursorsAre422 is the D149 guard. The failure it prevents is
// specific: a UUIDv7 compared LEXICALLY against a date sorts below every
// plausible date, so the query returns an empty page and the client concludes
// there is nothing more. Silently correct-looking, and wrong.
func TestMalformedCursorsAre422(t *testing.T) {
	editor, _ := router(t, "editor")
	uuidv7 := "01920000-0000-7000-8000-000000000000"

	for _, path := range []string{
		"/api/electricity/readings", "/api/electricity/tariffs",
		"/api/electricity/advances", "/api/electricity/periods",
	} {
		for _, cursor := range []string{"nonsense", "2026-13-01", uuidv7, "2026-06"} {
			rr := send(t, editor, http.MethodGet, path+"?cursor="+cursor, "")
			if rr.Code != http.StatusUnprocessableEntity {
				t.Errorf("GET %s?cursor=%s = %d, want 422", path, cursor, rr.Code)
			}
		}
	}
	// Payments keyset on a MONTH key, so a date is wrong there too.
	for _, cursor := range []string{"nonsense", "2026-13", uuidv7, "2026-06-24"} {
		rr := send(t, editor, http.MethodGet, "/api/electricity/payments?cursor="+cursor, "")
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("GET payments?cursor=%s = %d, want 422", cursor, rr.Code)
		}
	}
	// A well-formed cursor still works.
	if rr := send(t, editor, http.MethodGet, "/api/electricity/readings?cursor=2026-06-24", ""); rr.Code != http.StatusOK {
		t.Errorf("a valid date cursor = %d, want 200", rr.Code)
	}
}

// A Snapshot is bounded to ONE period, so a chart drawn from one snapshot can
// only ever carry that period's months. /history loads one per period and sums
// them; without that, every month of an earlier period comes back zero and a
// year of charted consumption disappears the day the next period opens.
func TestHistorySpansEveryPeriod(t *testing.T) {
	x := newH(t)
	if _, err := x.svc.CreateTariff(editorCtx(), electricity.TariffInput{
		EffectiveFrom: dp("2025-01-01"), PriceVTHaler: i64(320000),
		PriceNTHaler: i64(240000), MonthlyFeeHaler: i64(35000),
	}); err != nil {
		t.Fatalf("tariff: %v", err)
	}
	// Two ADJACENT periods: one reading closes the first and opens the second.
	for _, p := range [][2]string{{"2026-01-01", "2026-06-30"}, {"2026-07-01", "2026-12-31"}} {
		if _, err := x.svc.CreatePeriod(editorCtx(), electricity.PeriodInput{
			StartsOn: dp(p[0]), EndsOn: dp(p[1]),
		}); err != nil {
			t.Fatalf("period %s: %v", p[0], err)
		}
	}
	addReading(t, x, "2026-01-01", 12000, 30000)
	addReading(t, x, "2026-04-01", 12300, 30800)
	addReading(t, x, "2026-07-01", 12640, 31480)
	addReading(t, x, "2026-10-01", 12900, 32100)

	from, err := electricity.ParseMonth("2026-01")
	if err != nil {
		t.Fatalf("from: %v", err)
	}
	to, err := electricity.ParseMonth("2026-09")
	if err != nil {
		t.Fatalf("to: %v", err)
	}
	points, err := x.svc.History(context.Background(), &from, &to)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(points) != 9 {
		t.Fatalf("months = %d, want 9", len(points))
	}
	for _, p := range points {
		if p.EnergyHaler <= 0 || p.FeesHaler <= 0 {
			t.Errorf("%s: energy %d, fees %d — both periods must contribute, so no month "+
				"inside either of them may come back empty", p.Month, p.EnergyHaler, p.FeesHaler)
		}
	}
	// Adjacency must not double-count: the two periods meet at ends_on+1 ==
	// next.starts_on, the half-open bound one stops at and the other begins from.
	var fees int64
	for _, p := range points {
		fees += p.FeesHaler
	}
	if want := int64(9 * 35000); fees != want {
		t.Errorf("Σ fees = %d, want %d — nine whole months at 350 Kč, counted once each", fees, want)
	}
}

func TestHistoryRejectsMalformedMonths(t *testing.T) {
	editor, _ := router(t, "editor")
	for _, q := range []string{"?from=2026-1", "?from=2026-13", "?to=nonsense", "?from=2026-06-24"} {
		rr := send(t, editor, http.MethodGet, "/api/electricity/history"+q, "")
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("GET history%s = %d, want 422", q, rr.Code)
		}
	}
}

func TestNegativeAmountsAre422(t *testing.T) {
	editor, _ := router(t, "editor")
	for _, tc := range []struct{ path, body string }{
		{"/api/electricity/readings", `{"read_on":"2026-06-24","vt_dkwh":-1,"nt_dkwh":700}`},
		{"/api/electricity/tariffs", `{"effective_from":"2026-06-24","price_vt_haler":-1,"price_nt_haler":1,"monthly_fee_haler":1}`},
		{"/api/electricity/advances", `{"effective_from":"2026-06-24","amount_haler":-1,"due_day":15}`},
		{"/api/electricity/payments", `{"month":"2026-07","amount_haler":-1}`},
		{"/api/electricity/payments", `{"month":"2026-13","amount_haler":1}`},
	} {
		rr := send(t, editor, http.MethodPost, tc.path, tc.body)
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("POST %s %s = %d, want 422", tc.path, tc.body, rr.Code)
		}
	}
}

// TestSummarySerializesAbsentTotalsAsNull is D161 ON THE WIRE — not merely in
// Go. A pointer that is nil in Go and `0` in JSON is exactly the bug this shape
// exists to prevent, and only a test that reads the bytes can see the difference.
func TestSummarySerializesAbsentTotalsAsNull(t *testing.T) {
	editor, _ := router(t, "editor")

	// Karel's real day one: one period, one ceník, one záloha, one reading.
	mustOK := func(path, body string) {
		t.Helper()
		if rr := send(t, editor, http.MethodPost, path, body); rr.Code != http.StatusCreated {
			t.Fatalf("POST %s = %d: %s", path, rr.Code, rr.Body.String())
		}
	}
	mustOK("/api/electricity/periods", `{"starts_on":"2026-06-24","ends_on":"2027-06-23"}`)
	mustOK("/api/electricity/tariffs",
		`{"effective_from":"2026-06-24","price_vt_haler":485865,"price_nt_haler":402669,"monthly_fee_haler":64235}`)
	mustOK("/api/electricity/advances", `{"effective_from":"2026-06-24","amount_haler":150000,"due_day":15}`)
	mustOK("/api/electricity/readings", `{"read_on":"2026-06-24","vt_dkwh":320,"nt_dkwh":700}`)

	rr := send(t, editor, http.MethodGet, "/api/electricity/summary?period_id="+firstPeriodID(t, editor), "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET summary = %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	// The bytes themselves: null, never 0.
	for _, field := range []string{"cost_total_haler", "balance_haler", "energy_total_haler", "fee_total_haler"} {
		if !strings.Contains(body, `"`+field+`":null`) {
			t.Errorf("%s must serialize as null, not 0 — a 0 Kč nedoplatek is a lie that "+
				"looks like good news. Body: %s", field, body)
		}
	}

	var got struct {
		Status   string `json:"status"`
		Headroom *struct {
			EnergyBudgetHaler int64 `json:"energy_budget_haler"`
			KwhAllVTDkwh      int64 `json:"kwh_all_vt_dkwh"`
			KwhAllNTDkwh      int64 `json:"kwh_all_nt_dkwh"`
			KwhMixDkwh        int64 `json:"kwh_mix_dkwh"`
		} `json:"headroom"`
		Advances struct {
			MonthsCounted      int   `json:"months_counted"`
			ExpectedTotalHaler int64 `json:"expected_total_haler"`
		} `json:"advances"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "insufficient_data" {
		t.Errorf("status = %q, want insufficient_data", got.Status)
	}
	if got.Headroom == nil {
		t.Fatal("headroom must stand in for the totals — it is the whole point of the day-one screen")
	}
	// The screen Karel actually sees, end to end through the wire.
	if got.Headroom.EnergyBudgetHaler != 85_765 || got.Headroom.KwhAllVTDkwh != 1765 ||
		got.Headroom.KwhAllNTDkwh != 2130 || got.Headroom.KwhMixDkwh != 2006 {
		t.Errorf("headroom = %+v, want 85 765 h / 1765 / 2130 / 2006 dkWh", got.Headroom)
	}
	if got.Advances.MonthsCounted != 12 || got.Advances.ExpectedTotalHaler != 1_800_000 {
		t.Errorf("advances = %+v, want 12 months and 1 800 000 h", got.Advances)
	}
}

func firstPeriodID(t *testing.T, h http.Handler) string {
	t.Helper()
	rr := send(t, h, http.MethodGet, "/api/electricity/periods", "")
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil || len(page.Items) == 0 {
		t.Fatalf("no period: %v %s", err, rr.Body.String())
	}
	return page.Items[0].ID
}

// TestPatchCanClearARecordedInvoiceFigure — the reason the handler reads the
// body twice: a PATCH must distinguish "field omitted" from "field set to null",
// or a mistyped vyúčtování could never be cleared.
func TestPatchCanClearARecordedInvoiceFigure(t *testing.T) {
	editor, _ := router(t, "editor")
	if rr := send(t, editor, http.MethodPost, "/api/electricity/periods",
		`{"starts_on":"2026-06-24","ends_on":"2027-06-23","invoiced_total_haler":2189000}`); rr.Code != http.StatusCreated {
		t.Fatalf("create: %s", rr.Body.String())
	}
	id := firstPeriodID(t, editor)

	// Omitting the field leaves it alone.
	rr := send(t, editor, http.MethodPatch, "/api/electricity/periods/"+id, `{"note":"cokoliv"}`)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"invoiced_total_haler":2189000`) {
		t.Errorf("an omitted field must be left alone, got %s", rr.Body.String())
	}
	// Setting it to null clears it.
	rr = send(t, editor, http.MethodPatch, "/api/electricity/periods/"+id, `{"invoiced_total_haler":null}`)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"invoiced_total_haler":null`) {
		t.Errorf("an explicit null must clear the field, got %s", rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Log summaries — the one place the SERVER formats money
// ---------------------------------------------------------------------------

// TestLogSummariesSpeakKoruny pins the summary prose, because the summary is
// frozen at write time: a wrong unit is not a rendering bug that a later deploy
// repairs, it is a wrong sentence sitting in the log forever. The two precisions
// are the ones the screens use — whole koruny for a záloha that somebody pays,
// haléře for a unit price typed off the supplier's contract.
func TestLogSummariesSpeakKoruny(t *testing.T) {
	x := newH(t)
	if _, err := x.svc.CreateAdvance(editorCtx(), electricity.AdvanceInput{
		EffectiveFrom: dp("2026-06-24"), AmountHaler: i64(150_000), DueDay: ip(15),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := x.svc.CreateTariff(editorCtx(), electricity.TariffInput{
		EffectiveFrom: dp("2026-06-24"), PriceVTHaler: i64(485_865),
		PriceNTHaler: i64(402_669), MonthlyFeeHaler: i64(64_235),
	}); err != nil {
		t.Fatal(err)
	}
	m, err := electricity.ParseMonth("2026-07")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x.svc.CreatePayment(editorCtx(), electricity.PaymentInput{
		Month: &m, AmountHaler: i64(150_000),
	}); err != nil {
		t.Fatal(err)
	}

	// Written as an ESCAPE for the same reason service.go writes it that way: a
	// literal U+00A0 here is invisible in a diff, and one whitespace-normalising
	// pass would quietly turn this guard into an assertion about a plain space.
	const nbsp = "\u00a0"
	for _, tc := range []struct{ action, want string }{
		{"advance.create", "Předpis záloh od 24. 6. 2026 — 1" + nbsp + "500" + nbsp + "Kč/měs, splatnost 15."},
		{"tariff.create", "Ceník od 24. 6. 2026 — VT 4" + nbsp + "858,65" + nbsp + "Kč/MWh, NT 4" + nbsp +
			"026,69" + nbsp + "Kč/MWh, poplatky 642,35" + nbsp + "Kč/měs"},
		{"payment.create", "Zaplacená záloha za 2026-07 — 1" + nbsp + "500" + nbsp + "Kč"},
	} {
		var got string
		if err := x.db.QueryRow(
			`SELECT summary FROM audit_events WHERE action = ? AND module = 'electricity'`,
			tc.action).Scan(&got); err != nil {
			t.Fatalf("%s: %v", tc.action, err)
		}
		if got != tc.want {
			t.Errorf("%s summary =\n %q\nwant\n %q", tc.action, got, tc.want)
		}
	}
}
