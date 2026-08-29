package electricity

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
)

// Handler serves the electricity endpoints (openapi.yaml 0.10.0, tag
// `electricity`). Thirteen paths, and deliberately no more: there is no
// /recompute (nothing is stored, so nothing can be stale), no /close (periods
// never lock, D139) and no /import (there is no history to back-fill).
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the routes on the authenticated /api router.
//
// Reads are ungated beyond the session: every member sees Elektřina, `reader`
// included. Writes take the ordinary RequireWrite gate. There is NO admin-only
// route anywhere in this module (D151), delete included — nothing here is
// irreversible enough to warrant one, since delete is soft and every mutation is
// audited with a field-level diff.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/electricity/readings", h.listReadings)
	r.With(httpx.RequireWrite).Post("/electricity/readings", h.createReading)
	r.Get("/electricity/readings/{id}", h.getReading)
	r.With(httpx.RequireWrite).Patch("/electricity/readings/{id}", h.updateReading)
	r.With(httpx.RequireWrite).Delete("/electricity/readings/{id}", h.deleteReading)

	r.Get("/electricity/tariffs", h.listTariffs)
	r.With(httpx.RequireWrite).Post("/electricity/tariffs", h.createTariff)
	r.Get("/electricity/tariffs/{id}", h.getTariff)
	r.With(httpx.RequireWrite).Patch("/electricity/tariffs/{id}", h.updateTariff)
	r.With(httpx.RequireWrite).Delete("/electricity/tariffs/{id}", h.deleteTariff)

	r.Get("/electricity/advances", h.listAdvances)
	r.With(httpx.RequireWrite).Post("/electricity/advances", h.createAdvance)
	r.Get("/electricity/advances/{id}", h.getAdvance)
	r.With(httpx.RequireWrite).Patch("/electricity/advances/{id}", h.updateAdvance)
	r.With(httpx.RequireWrite).Delete("/electricity/advances/{id}", h.deleteAdvance)

	r.Get("/electricity/payments", h.listPayments)
	r.With(httpx.RequireWrite).Post("/electricity/payments", h.createPayment)
	r.Get("/electricity/payments/{id}", h.getPayment)
	r.With(httpx.RequireWrite).Patch("/electricity/payments/{id}", h.updatePayment)
	r.With(httpx.RequireWrite).Delete("/electricity/payments/{id}", h.deletePayment)

	r.Get("/electricity/periods", h.listPeriods)
	r.With(httpx.RequireWrite).Post("/electricity/periods", h.createPeriod)
	r.Get("/electricity/periods/{id}", h.getPeriod)
	r.With(httpx.RequireWrite).Patch("/electricity/periods/{id}", h.updatePeriod)
	r.With(httpx.RequireWrite).Delete("/electricity/periods/{id}", h.deletePeriod)

	r.Get("/electricity/summary", h.summary)
	r.Get("/electricity/intervals", h.intervals)
	r.Get("/electricity/history", h.history)
}

// ---------------------------------------------------------------------------
// Paging: the cursors are DATES, not UUIDv7 (PRD D149)
// ---------------------------------------------------------------------------

const defaultLimit = 100

func limitOf(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if n <= 0 || n > 500 {
		return defaultLimit
	}
	return n
}

// dateCursor validates a 'YYYY-MM-DD' keyset cursor.
//
// ⚠ A malformed cursor is a 422, NEVER a silent fall-back to page one. The
// failure this prevents is specific and nasty: pass a UUIDv7 where a `read_on`
// is expected and SQLite compares it LEXICALLY against a date string — "0192..."
// sorts below every plausible date, so the query returns an empty page and the
// client concludes there are no more readings. Silently correct-looking, and
// wrong. Validating here turns that into an error the caller can see.
func dateCursor(r *http.Request) (string, error) {
	c := r.URL.Query().Get("cursor")
	if c == "" {
		return "", nil
	}
	if _, err := dates.Parse(c); err != nil {
		return "", httpx.ErrUnprocessable("Neplatný cursor: čekám datum ve tvaru RRRR-MM-DD.")
	}
	return c, nil
}

// monthCursor is the same guard for the payments collection, whose natural key
// is a 'YYYY-MM' month (the finance precedent).
func monthCursor(r *http.Request) (string, error) {
	c := r.URL.Query().Get("cursor")
	if c == "" {
		return "", nil
	}
	if _, err := ParseMonth(c); err != nil {
		return "", httpx.ErrUnprocessable("Neplatný cursor: čekám měsíc ve tvaru RRRR-MM.")
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// Request bodies
// ---------------------------------------------------------------------------

// parseDate turns a wire 'YYYY-MM-DD' into a dates.Date.
func parseDate(s string, field string) (dates.Date, error) {
	d, err := dates.Parse(s)
	if err != nil {
		return dates.Date{}, httpx.ErrUnprocessable(field + ": čekám datum ve tvaru RRRR-MM-DD.")
	}
	return d, nil
}

type readingBody struct {
	ReadOn *string `json:"read_on"`
	VTDkwh *int64  `json:"vt_dkwh"`
	NTDkwh *int64  `json:"nt_dkwh"`
	Note   *string `json:"note"`
}

func (b readingBody) toInput(raw map[string]json.RawMessage) (ReadingInput, error) {
	in := ReadingInput{VTDkwh: b.VTDkwh, NTDkwh: b.NTDkwh, Note: b.Note}
	_, in.NoteSet = raw["note"]
	if b.ReadOn != nil {
		d, err := parseDate(*b.ReadOn, "read_on")
		if err != nil {
			return in, err
		}
		in.ReadOn = &d
	}
	return in, nil
}

type tariffBody struct {
	EffectiveFrom   *string `json:"effective_from"`
	PriceVTHaler    *int64  `json:"price_vt_haler"`
	PriceNTHaler    *int64  `json:"price_nt_haler"`
	MonthlyFeeHaler *int64  `json:"monthly_fee_haler"`
	Note            *string `json:"note"`
}

func (b tariffBody) toInput(raw map[string]json.RawMessage) (TariffInput, error) {
	in := TariffInput{PriceVTHaler: b.PriceVTHaler, PriceNTHaler: b.PriceNTHaler,
		MonthlyFeeHaler: b.MonthlyFeeHaler, Note: b.Note}
	_, in.NoteSet = raw["note"]
	if b.EffectiveFrom != nil {
		d, err := parseDate(*b.EffectiveFrom, "effective_from")
		if err != nil {
			return in, err
		}
		in.EffectiveFrom = &d
	}
	return in, nil
}

type advanceBody struct {
	EffectiveFrom *string `json:"effective_from"`
	AmountHaler   *int64  `json:"amount_haler"`
	DueDay        *int    `json:"due_day"`
	Note          *string `json:"note"`
}

func (b advanceBody) toInput(raw map[string]json.RawMessage) (AdvanceInput, error) {
	in := AdvanceInput{AmountHaler: b.AmountHaler, DueDay: b.DueDay, Note: b.Note}
	_, in.NoteSet = raw["note"]
	if b.EffectiveFrom != nil {
		d, err := parseDate(*b.EffectiveFrom, "effective_from")
		if err != nil {
			return in, err
		}
		in.EffectiveFrom = &d
	}
	return in, nil
}

type paymentBody struct {
	Month       *string `json:"month"`
	AmountHaler *int64  `json:"amount_haler"`
	PaidOn      *string `json:"paid_on"`
	Note        *string `json:"note"`
}

func (b paymentBody) toInput(raw map[string]json.RawMessage) (PaymentInput, error) {
	in := PaymentInput{AmountHaler: b.AmountHaler, Note: b.Note}
	_, in.NoteSet = raw["note"]
	_, in.PaidOnSet = raw["paid_on"]
	if b.Month != nil {
		m, err := ParseMonth(*b.Month)
		if err != nil {
			return in, httpx.ErrUnprocessable("month: čekám měsíc ve tvaru RRRR-MM.")
		}
		in.Month = &m
	}
	if b.PaidOn != nil {
		d, err := parseDate(*b.PaidOn, "paid_on")
		if err != nil {
			return in, err
		}
		in.PaidOn = &d
	}
	return in, nil
}

type periodBody struct {
	StartsOn             *string `json:"starts_on"`
	EndsOn               *string `json:"ends_on"`
	EndsOnConfirmed      *bool   `json:"ends_on_confirmed"`
	InvoicedTotalHaler   *int64  `json:"invoiced_total_haler"`
	InvoicedBalanceHaler *int64  `json:"invoiced_balance_haler"`
	InvoicedVTDkwh       *int64  `json:"invoiced_vt_dkwh"`
	InvoicedNTDkwh       *int64  `json:"invoiced_nt_dkwh"`
	InvoicedAt           *string `json:"invoiced_at"`
	Note                 *string `json:"note"`
}

func (b periodBody) toInput(raw map[string]json.RawMessage) (PeriodInput, error) {
	in := PeriodInput{
		EndsOnConfirmed:      b.EndsOnConfirmed,
		InvoicedTotalHaler:   b.InvoicedTotalHaler,
		InvoicedBalanceHaler: b.InvoicedBalanceHaler,
		InvoicedVTDkwh:       b.InvoicedVTDkwh,
		InvoicedNTDkwh:       b.InvoicedNTDkwh,
		InvoicedAt:           b.InvoicedAt,
		Note:                 b.Note,
	}
	_, in.NoteSet = raw["note"]
	_, in.InvoicedTotalSet = raw["invoiced_total_haler"]
	_, in.InvoicedBalanceSet = raw["invoiced_balance_haler"]
	_, in.InvoicedVTSet = raw["invoiced_vt_dkwh"]
	_, in.InvoicedNTSet = raw["invoiced_nt_dkwh"]
	_, in.InvoicedAtSet = raw["invoiced_at"]
	_, in.EndsOnSet = raw["ends_on"]
	if b.InvoicedAt != nil {
		if _, err := parseDate(*b.InvoicedAt, "invoiced_at"); err != nil {
			return in, err
		}
	}
	if b.StartsOn != nil {
		d, err := parseDate(*b.StartsOn, "starts_on")
		if err != nil {
			return in, err
		}
		in.StartsOn = &d
	}
	if b.EndsOn != nil {
		d, err := parseDate(*b.EndsOn, "ends_on")
		if err != nil {
			return in, err
		}
		in.EndsOn = &d
	}
	return in, nil
}

// decode reads the body twice: once into the typed struct, once into a raw map
// so a PATCH can tell "field omitted" from "field set to null". Without that a
// mistyped vyúčtování could never be cleared back to null.
func decode[T any](r *http.Request, body *T) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := httpx.DecodeJSON(r, &raw); err != nil {
		return nil, httpx.ErrUnprocessable(err.Error())
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, httpx.ErrUnprocessable(err.Error())
	}
	// The map swallowed every key as "known"; re-apply the unknown-field rejection
	// on the typed pass so a typo'd field is a 422 like everywhere else.
	dec := json.NewDecoder(bytes.NewReader(buf))
	dec.DisallowUnknownFields()
	if err := dec.Decode(body); err != nil {
		return nil, httpx.ErrUnprocessable(err.Error())
	}
	return raw, nil
}

// ---------------------------------------------------------------------------
// Readings
// ---------------------------------------------------------------------------

func (h *Handler) listReadings(w http.ResponseWriter, r *http.Request) {
	cursor, err := dateCursor(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, next, err := h.svc.Store().ListReadings(r.Context(), limitOf(r), cursor)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	page := readingPage{Items: []readingDTO{}, NextCursor: next}
	for _, it := range items {
		page.Items = append(page.Items, toReadingDTO(it))
	}
	httpx.JSON(w, http.StatusOK, page)
}

func (h *Handler) createReading(w http.ResponseWriter, r *http.Request) {
	var b readingBody
	raw, err := decode(r, &b)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	in, err := b.toInput(raw)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.CreateReading(r.Context(), in)
	httpx.Respond(w, http.StatusCreated, toReadingDTO(out), err)
}

func (h *Handler) updateReading(w http.ResponseWriter, r *http.Request) {
	var b readingBody
	raw, err := decode(r, &b)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	in, err := b.toInput(raw)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.UpdateReading(r.Context(), chi.URLParam(r, "id"), in)
	httpx.Respond(w, http.StatusOK, toReadingDTO(out), err)
}

func (h *Handler) getReading(w http.ResponseWriter, r *http.Request) {
	out, ok, err := h.svc.Store().GetReading(r.Context(), nil, chi.URLParam(r, "id"))
	if err == nil && !ok {
		err = httpx.ErrNotFound("Odečet nenalezen.")
	}
	httpx.Respond(w, http.StatusOK, toReadingDTO(out), err)
}

func (h *Handler) deleteReading(w http.ResponseWriter, r *http.Request) {
	httpx.NoContent(w, h.svc.DeleteReading(r.Context(), chi.URLParam(r, "id")))
}

// ---------------------------------------------------------------------------
// Tariffs
// ---------------------------------------------------------------------------

func (h *Handler) listTariffs(w http.ResponseWriter, r *http.Request) {
	cursor, err := dateCursor(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, next, err := h.svc.Store().ListTariffs(r.Context(), limitOf(r), cursor)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	page := tariffPage{Items: []tariffDTO{}, NextCursor: next}
	for i, it := range items {
		dto := toTariffDTO(it)
		// effective_to is the day before the NEXT version. Newest-first, so that
		// is the previous item — or, on a later page, the cursor itself, which is
		// the last effective_from of the page before. Without that the oldest row
		// on every page would render as still current.
		var nextFrom *dates.Date
		if i > 0 {
			nextFrom = &items[i-1].EffectiveFrom
		} else if cursor != "" {
			d, _ := dates.Parse(cursor)
			nextFrom = &d
		}
		if nextFrom != nil {
			to := dstr(nextFrom.AddDays(-1))
			dto.EffectiveTo = &to
		}
		page.Items = append(page.Items, dto)
	}
	httpx.JSON(w, http.StatusOK, page)
}

// tariffDTOWithEnd is toTariffDTO plus the DERIVED effective_to (D136): the day
// before the NEXT version starts, or null when this really is the current one.
//
// listTariffs derives it from the neighbouring row it already has; a
// single-resource response has no neighbour, so it asks the store. Without this
// every GET/POST/PATCH answered `effective_to: null`, which the schema defines as
// "still in force" — so a superseded ceník read back by id looked current.
//
// A lookup failure degrades to the old null rather than turning a successful
// write into a 500: the field is a convenience, not the resource.
func (h *Handler) tariffDTOWithEnd(r *http.Request, t Tariff) tariffDTO {
	dto := toTariffDTO(t)
	next, ok, err := h.svc.Store().NextTariffFrom(r.Context(), nil, t.EffectiveFrom)
	if err != nil || !ok {
		return dto
	}
	to := dstr(next.AddDays(-1))
	dto.EffectiveTo = &to
	return dto
}

func (h *Handler) createTariff(w http.ResponseWriter, r *http.Request) {
	var b tariffBody
	raw, err := decode(r, &b)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	in, err := b.toInput(raw)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.CreateTariff(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, h.tariffDTOWithEnd(r, out))
}

func (h *Handler) updateTariff(w http.ResponseWriter, r *http.Request) {
	var b tariffBody
	raw, err := decode(r, &b)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	in, err := b.toInput(raw)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.UpdateTariff(r.Context(), chi.URLParam(r, "id"), in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, h.tariffDTOWithEnd(r, out))
}

func (h *Handler) getTariff(w http.ResponseWriter, r *http.Request) {
	out, ok, err := h.svc.Store().GetTariff(r.Context(), nil, chi.URLParam(r, "id"))
	if err == nil && !ok {
		err = httpx.ErrNotFound("Ceník nenalezen.")
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, h.tariffDTOWithEnd(r, out))
}

func (h *Handler) deleteTariff(w http.ResponseWriter, r *http.Request) {
	httpx.NoContent(w, h.svc.DeleteTariff(r.Context(), chi.URLParam(r, "id")))
}

// ---------------------------------------------------------------------------
// Advances and payments
// ---------------------------------------------------------------------------

func (h *Handler) listAdvances(w http.ResponseWriter, r *http.Request) {
	cursor, err := dateCursor(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, next, err := h.svc.Store().ListAdvances(r.Context(), limitOf(r), cursor)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	page := advancePage{Items: []advanceDTO{}, NextCursor: next}
	for _, it := range items {
		page.Items = append(page.Items, toAdvanceDTO(it))
	}
	httpx.JSON(w, http.StatusOK, page)
}

func (h *Handler) createAdvance(w http.ResponseWriter, r *http.Request) {
	var b advanceBody
	raw, err := decode(r, &b)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	in, err := b.toInput(raw)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.CreateAdvance(r.Context(), in)
	httpx.Respond(w, http.StatusCreated, toAdvanceDTO(out), err)
}

func (h *Handler) updateAdvance(w http.ResponseWriter, r *http.Request) {
	var b advanceBody
	raw, err := decode(r, &b)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	in, err := b.toInput(raw)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.UpdateAdvance(r.Context(), chi.URLParam(r, "id"), in)
	httpx.Respond(w, http.StatusOK, toAdvanceDTO(out), err)
}

func (h *Handler) getAdvance(w http.ResponseWriter, r *http.Request) {
	out, ok, err := h.svc.Store().GetAdvance(r.Context(), nil, chi.URLParam(r, "id"))
	if err == nil && !ok {
		err = httpx.ErrNotFound("Předpis záloh nenalezen.")
	}
	httpx.Respond(w, http.StatusOK, toAdvanceDTO(out), err)
}

func (h *Handler) deleteAdvance(w http.ResponseWriter, r *http.Request) {
	httpx.NoContent(w, h.svc.DeleteAdvance(r.Context(), chi.URLParam(r, "id")))
}

func (h *Handler) listPayments(w http.ResponseWriter, r *http.Request) {
	cursor, err := monthCursor(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, next, err := h.svc.Store().ListPayments(r.Context(), limitOf(r), cursor)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	page := paymentPage{Items: []paymentDTO{}, NextCursor: next}
	for _, it := range items {
		page.Items = append(page.Items, toPaymentDTO(it))
	}
	httpx.JSON(w, http.StatusOK, page)
}

func (h *Handler) createPayment(w http.ResponseWriter, r *http.Request) {
	var b paymentBody
	raw, err := decode(r, &b)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	in, err := b.toInput(raw)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.CreatePayment(r.Context(), in)
	httpx.Respond(w, http.StatusCreated, toPaymentDTO(out), err)
}

func (h *Handler) updatePayment(w http.ResponseWriter, r *http.Request) {
	var b paymentBody
	raw, err := decode(r, &b)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	in, err := b.toInput(raw)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.UpdatePayment(r.Context(), chi.URLParam(r, "id"), in)
	httpx.Respond(w, http.StatusOK, toPaymentDTO(out), err)
}

func (h *Handler) getPayment(w http.ResponseWriter, r *http.Request) {
	out, ok, err := h.svc.Store().GetPayment(r.Context(), nil, chi.URLParam(r, "id"))
	if err == nil && !ok {
		err = httpx.ErrNotFound("Platba nenalezena.")
	}
	httpx.Respond(w, http.StatusOK, toPaymentDTO(out), err)
}

func (h *Handler) deletePayment(w http.ResponseWriter, r *http.Request) {
	httpx.NoContent(w, h.svc.DeletePayment(r.Context(), chi.URLParam(r, "id")))
}

// ---------------------------------------------------------------------------
// Periods
// ---------------------------------------------------------------------------

func (h *Handler) listPeriods(w http.ResponseWriter, r *http.Request) {
	cursor, err := dateCursor(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, next, err := h.svc.Store().ListPeriods(r.Context(), limitOf(r), cursor)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	page := periodPage{Items: []periodDTO{}, NextCursor: next}
	for _, it := range items {
		page.Items = append(page.Items, toPeriodDTO(it))
	}
	httpx.JSON(w, http.StatusOK, page)
}

func (h *Handler) createPeriod(w http.ResponseWriter, r *http.Request) {
	var b periodBody
	raw, err := decode(r, &b)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	in, err := b.toInput(raw)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.CreatePeriod(r.Context(), in)
	httpx.Respond(w, http.StatusCreated, toPeriodDTO(out), err)
}

func (h *Handler) updatePeriod(w http.ResponseWriter, r *http.Request) {
	var b periodBody
	raw, err := decode(r, &b)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	in, err := b.toInput(raw)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, err := h.svc.UpdatePeriod(r.Context(), chi.URLParam(r, "id"), in)
	httpx.Respond(w, http.StatusOK, toPeriodDTO(out), err)
}

func (h *Handler) getPeriod(w http.ResponseWriter, r *http.Request) {
	out, ok, err := h.svc.Store().GetPeriod(r.Context(), nil, chi.URLParam(r, "id"))
	if err == nil && !ok {
		err = httpx.ErrNotFound("Zúčtovací období nenalezeno.")
	}
	httpx.Respond(w, http.StatusOK, toPeriodDTO(out), err)
}

func (h *Handler) deletePeriod(w http.ResponseWriter, r *http.Request) {
	httpx.NoContent(w, h.svc.DeletePeriod(r.Context(), chi.URLParam(r, "id")))
}

// ---------------------------------------------------------------------------
// Computed
// ---------------------------------------------------------------------------

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	sum, err := h.svc.Summary(r.Context(), r.URL.Query().Get("period_id"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toSummaryDTO(sum))
}

func (h *Handler) intervals(w http.ResponseWriter, r *http.Request) {
	ivs, err := h.svc.Intervals(r.Context(), r.URL.Query().Get("period_id"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toIntervalList(ivs))
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var from, to *Month
	// `from`/`to` are YYYY-MM. A malformed one is a 422 rather than a silent
	// default: a chart quietly drawn over the wrong range is worse than an error.
	if s := q.Get("from"); s != "" {
		m, err := ParseMonth(s)
		if err != nil {
			httpx.WriteError(w, httpx.ErrUnprocessable("from: čekám měsíc ve tvaru RRRR-MM."))
			return
		}
		from = &m
	}
	if s := q.Get("to"); s != "" {
		m, err := ParseMonth(s)
		if err != nil {
			httpx.WriteError(w, httpx.ErrUnprocessable("to: čekám měsíc ve tvaru RRRR-MM."))
			return
		}
		to = &m
	}
	points, err := h.svc.History(r.Context(), from, to)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toHistoryDTO(points))
}
