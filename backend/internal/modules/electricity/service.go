package electricity

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Notifier publishes a websocket change after commit.
type Notifier func(ctx context.Context, typ string, payload any)

// Service orchestrates electricity mutations: validate → WithTx (row change AND
// audit event in ONE transaction) → notify after commit. The same shape every
// other module uses.
//
// NOTHING HERE IS EVER LOCKED (PRD D139). There is no closed-period guard, no
// 409 on editing a period that already carries a vyúčtování, and no admin tier
// on delete. Do not add one: the audit spine is the compensating control, and
// every one of the fifteen actions carries a field-level diff, so "who changed
// the VT price and to what" is answerable a year later — which is the question
// this module's Log entries exist to answer.
type Service struct {
	db     *sql.DB
	store  *Store
	sink   audit.ModuleSink
	notify Notifier
	loc    *time.Location
}

// NewService builds the service. loc is HOME_TIMEZONE and is the ONLY place a
// clock enters this module: `today` is resolved here, once per request, and
// travels into compute.go on the Snapshot.
func NewService(db *sql.DB, sink audit.Sink, notify Notifier, loc *time.Location) *Service {
	if notify == nil {
		notify = func(context.Context, string, any) {}
	}
	if loc == nil {
		loc = time.UTC
	}
	return &Service{db: db, store: NewStore(db), sink: audit.For(sink, audit.ModuleElectricity), notify: notify, loc: loc}
}

func (s *Service) Store() *Store { return s.store }

// Today is the current date in HOME_TIMEZONE. Every read path resolves it here
// so all three computed endpoints agree about what day it is.
func (s *Service) Today() dates.Date { return dates.Today(s.loc) }

// record writes one audit event through the spine, INSIDE the caller's tx. The
// error is returned unchanged so the transaction rolls back: an action that
// succeeds unlogged is the bug the spine exists to prevent.
//
// ⚠ The type is audit.Event, NOT the live-sync notifier's Entry. Two different
// structs on two different paths — audit goes in the transaction, the notifier
// fires after commit — and mixing them produces a compiler error unhelpful
// enough that the tempting "fix" is to reach for the wrong one.
func (s *Service) record(ctx context.Context, tx *sql.Tx, action, entityType, entityID, summary string, changes []audit.Change) error {
	return s.sink.Record(ctx, tx, audit.Event{
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		Summary:    summary,
		Changes:    changes,
	})
}

// ---------------------------------------------------------------------------
// Small helpers for diffs and formatting
// ---------------------------------------------------------------------------

// nbsp is written as an ESCAPE deliberately: a literal U+00A0 sitting in the
// source is indistinguishable from a plain space to the next person reading it.
const nbsp = "\u00a0"

// Money in a log summary is written by the SERVER — the one place that happens.
// Everywhere else the API hands over haléře and the screen formats them
// (types.go), but a summary is prose frozen at write time, so it has to carry
// the same two precisions the screens use, or the log disagrees with the page it
// describes. Same rule, same reason as frontend/src/modules/electricity/format.ts:
//
//	kc  — TOTALS (zálohy, platby), whole koruny:  150000 → "1 500 Kč"
//	kc2 — UNIT PRICES and FEES, two decimals:     485865 → "4 858,65 Kč"
//
// A unit price's haléře ARE the number, typed off the supplier's contract; a
// total's last haléř is the residue of a dozen roundings and means nothing.
func kc(haler int64) string {
	k := haler / 100
	switch r := haler % 100; {
	case r >= 50:
		k++
	case r <= -50:
		k--
	}
	return group(k) + nbsp + "Kč"
}

func kc2(haler int64) string {
	sign := ""
	if haler < 0 {
		sign, haler = "−", -haler
	}
	return sign + group(haler/100) + "," + fmt.Sprintf("%02d", haler%100) + nbsp + "Kč"
}

// group writes an integer with Czech NON-BREAKING thousands separators, so
// "1 500 Kč" never wraps into "1" / "500 Kč" halfway across a log line.
func group(n int64) string {
	sign := ""
	if n < 0 {
		sign, n = "−", -n
	}
	d := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i := 0; i < len(d); i++ {
		if i > 0 && (len(d)-i)%3 == 0 {
			b.WriteString(nbsp)
		}
		b.WriteByte(d[i])
	}
	return sign + b.String()
}

// kwh groups and non-breaks exactly like the money helpers above, for the same
// reason: a register reads five digits, and "12 345,6 kWh" on the odečty screen
// must be the same string the log froze. Keep in step with kwh() in
// frontend/src/modules/electricity/format.ts — the tenth is kept, never rounded,
// because a METER READING must never be rounded at all.
func kwh(dkwh int64) string {
	sign := ""
	if dkwh < 0 {
		sign, dkwh = "−", -dkwh
	}
	body := group(dkwh / 10)
	if tenth := dkwh % 10; tenth != 0 {
		body += "," + strconv.FormatInt(tenth, 10)
	}
	return sign + body + nbsp + "kWh"
}

func diffStr(field, old, new string, out *[]audit.Change) {
	if old != new {
		*out = append(*out, audit.Change{Field: field, Old: audit.Ptr(old), New: audit.Ptr(new)})
	}
}

func diffI64(field string, old, new int64, out *[]audit.Change) {
	diffStr(field, strconv.FormatInt(old, 10), strconv.FormatInt(new, 10), out)
}

func diffPtrStr(field string, old, new *string, out *[]audit.Change) {
	o, n := "", ""
	if old != nil {
		o = *old
	}
	if new != nil {
		n = *new
	}
	diffStr(field, o, n, out)
}

// uniqueViolation maps a UNIQUE index failure onto the same 409 the in-tx guard
// produces. The guards catch the ordinary duplicate and give the better message;
// this catches the one two concurrent writers can still slip past — both read
// "free", the first commits, the second's write hits the partial index. Returned
// raw, that error has no *httpx.APIError to match and would surface as a 500,
// which is neither what openapi.yaml documents nor what the frontend can turn
// into Czech.
func uniqueViolation(err error, msg string) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return httpx.ErrConflict(msg)
	}
	return err
}

// ---------------------------------------------------------------------------
// Readings
// ---------------------------------------------------------------------------

// ReadingInput is a create or a patch. Nil fields on a patch are left alone.
type ReadingInput struct {
	ReadOn *dates.Date
	VTDkwh *int64
	NTDkwh *int64
	Note   *string
	// NoteSet distinguishes "note omitted" from "note cleared to null".
	NoteSet bool
}

// checkMonotonic enforces PRD FR-E1 inside the transaction: both registers must
// be non-decreasing in date order, checked against the neighbours on BOTH SIDES.
//
// Checking only the previous reading lets a back-filled row break the chain in
// FRONT of it — enter 1. 6. = 12 800 between a 1. 4. of 12 000 and a 1. 8. of
// 12 640 and the meter appears to run backwards over the summer. With výměna
// elektroměru out of scope (D150), a falling counter is ALWAYS a typo, so
// refusing it is correct rather than restrictive.
//
// A trigger could enforce this, but could not name the neighbour — and that
// message is the point. The user is standing at the meter cupboard with the
// display in front of them; "je nižší než odečet z 1. 8. 2026 (12 640 kWh)" is
// what lets them see which digit they fumbled.
//
// DELETE needs no such check: if a ≤ b ≤ c then a ≤ c, so removing a middle
// reading merges two valid intervals into one. It CAN create a hard block, which
// is discovered on read rather than prevented on write.
func (s *Service) checkMonotonic(ctx context.Context, tx *sql.Tx, on dates.Date, vt, nt int64, excludeID string) error {
	prev, next, err := s.store.NeighbourReadings(ctx, tx, on, excludeID)
	if err != nil {
		return err
	}
	if prev != nil {
		if vt < prev.VTDkwh {
			return httpx.ErrUnprocessable(fmt.Sprintf(
				"Odečet %s ve VT je nižší než odečet z %s (%s).",
				kwh(vt), czDate(prev.ReadOn), kwh(prev.VTDkwh)))
		}
		if nt < prev.NTDkwh {
			return httpx.ErrUnprocessable(fmt.Sprintf(
				"Odečet %s v NT je nižší než odečet z %s (%s).",
				kwh(nt), czDate(prev.ReadOn), kwh(prev.NTDkwh)))
		}
	}
	if next != nil {
		if next.VTDkwh < vt {
			return httpx.ErrUnprocessable(fmt.Sprintf(
				"Odečet %s ve VT je vyšší než pozdější odečet z %s (%s).",
				kwh(vt), czDate(next.ReadOn), kwh(next.VTDkwh)))
		}
		if next.NTDkwh < nt {
			return httpx.ErrUnprocessable(fmt.Sprintf(
				"Odečet %s v NT je vyšší než pozdější odečet z %s (%s).",
				kwh(nt), czDate(next.ReadOn), kwh(next.NTDkwh)))
		}
	}
	return nil
}

func (s *Service) CreateReading(ctx context.Context, in ReadingInput) (Reading, error) {
	if in.ReadOn == nil || in.VTDkwh == nil || in.NTDkwh == nil {
		return Reading{}, httpx.ErrUnprocessable("Datum odečtu a oba registry jsou povinné.")
	}
	if *in.VTDkwh < 0 || *in.NTDkwh < 0 {
		return Reading{}, httpx.ErrUnprocessable("Stav elektroměru nemůže být záporný.")
	}
	now := nowUTC()
	r := Reading{
		ID: idgen.New(), ReadOn: *in.ReadOn, VTDkwh: *in.VTDkwh, NTDkwh: *in.NTDkwh,
		Note: in.Note, CreatedAt: now, UpdatedAt: now,
	}
	if a := reqctx.ActorID(ctx); a != "" {
		r.CreatedBy = &a
	}

	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, taken, err := s.store.ReadingOn(ctx, tx, r.ReadOn, ""); err != nil {
			return err
		} else if taken {
			return httpx.ErrConflict(fmt.Sprintf("Odečet k %s už existuje.", czDate(r.ReadOn)))
		}
		if err := s.checkMonotonic(ctx, tx, r.ReadOn, r.VTDkwh, r.NTDkwh, ""); err != nil {
			return err
		}
		if err := s.store.InsertReading(ctx, tx, r); err != nil {
			return uniqueViolation(err, fmt.Sprintf("Odečet k %s už existuje.", czDate(r.ReadOn)))
		}
		return s.record(ctx, tx, "reading.create", "electricity_reading", r.ID,
			fmt.Sprintf("Odečet k %s — VT %s, NT %s", czDate(r.ReadOn), kwh(r.VTDkwh), kwh(r.NTDkwh)), nil)
	})
	if err != nil {
		return Reading{}, err
	}
	s.notify(ctx, "electricity_reading.changed", map[string]any{"id": r.ID})
	return r, nil
}

func (s *Service) UpdateReading(ctx context.Context, id string, in ReadingInput) (Reading, error) {
	var out Reading
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		cur, ok, err := s.store.GetReading(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.ErrNotFound("Odečet nenalezen.")
		}
		next := cur
		if in.ReadOn != nil {
			next.ReadOn = *in.ReadOn
		}
		if in.VTDkwh != nil {
			next.VTDkwh = *in.VTDkwh
		}
		if in.NTDkwh != nil {
			next.NTDkwh = *in.NTDkwh
		}
		if in.NoteSet {
			next.Note = in.Note
		}
		if next.VTDkwh < 0 || next.NTDkwh < 0 {
			return httpx.ErrUnprocessable("Stav elektroměru nemůže být záporný.")
		}
		// Re-validate whenever the date OR either register moved: any of the
		// three can break the chain, and a change of date moves the row between
		// a different pair of neighbours entirely.
		if !next.ReadOn.Equal(cur.ReadOn) || next.VTDkwh != cur.VTDkwh || next.NTDkwh != cur.NTDkwh {
			if !next.ReadOn.Equal(cur.ReadOn) {
				if _, taken, err := s.store.ReadingOn(ctx, tx, next.ReadOn, id); err != nil {
					return err
				} else if taken {
					return httpx.ErrConflict(fmt.Sprintf("Odečet k %s už existuje.", czDate(next.ReadOn)))
				}
			}
			if err := s.checkMonotonic(ctx, tx, next.ReadOn, next.VTDkwh, next.NTDkwh, id); err != nil {
				return err
			}
		}
		next.UpdatedAt = nowUTC()

		var changes []audit.Change
		diffStr("read_on", cur.ReadOn.String(), next.ReadOn.String(), &changes)
		diffI64("vt_dkwh", cur.VTDkwh, next.VTDkwh, &changes)
		diffI64("nt_dkwh", cur.NTDkwh, next.NTDkwh, &changes)
		diffPtrStr("note", cur.Note, next.Note, &changes)

		if err := s.store.UpdateReading(ctx, tx, next); err != nil {
			return uniqueViolation(err, fmt.Sprintf("Odečet k %s už existuje.", czDate(next.ReadOn)))
		}
		out = next
		return s.record(ctx, tx, "reading.update", "electricity_reading", id,
			fmt.Sprintf("Upraven odečet k %s", czDate(next.ReadOn)), changes)
	})
	if err != nil {
		return Reading{}, err
	}
	s.notify(ctx, "electricity_reading.changed", map[string]any{"id": id})
	return out, nil
}

func (s *Service) DeleteReading(ctx context.Context, id string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		cur, ok, err := s.store.GetReading(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.ErrNotFound("Odečet nenalezen.")
		}
		if err := s.store.SoftDelete(ctx, tx, "electricity_readings", id, nowUTC()); err != nil {
			return err
		}
		return s.record(ctx, tx, "reading.delete", "electricity_reading", id,
			fmt.Sprintf("Smazán odečet k %s — VT %s, NT %s",
				czDate(cur.ReadOn), kwh(cur.VTDkwh), kwh(cur.NTDkwh)), nil)
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "electricity_reading.changed", map[string]any{"id": id})
	return nil
}

// ---------------------------------------------------------------------------
// Tariffs
// ---------------------------------------------------------------------------

type TariffInput struct {
	EffectiveFrom   *dates.Date
	PriceVTHaler    *int64
	PriceNTHaler    *int64
	MonthlyFeeHaler *int64
	Note            *string
	NoteSet         bool
}

func (s *Service) CreateTariff(ctx context.Context, in TariffInput) (Tariff, error) {
	if in.EffectiveFrom == nil || in.PriceVTHaler == nil || in.PriceNTHaler == nil || in.MonthlyFeeHaler == nil {
		return Tariff{}, httpx.ErrUnprocessable("Datum platnosti a všechny tři částky jsou povinné.")
	}
	if *in.PriceVTHaler < 0 || *in.PriceNTHaler < 0 || *in.MonthlyFeeHaler < 0 {
		return Tariff{}, httpx.ErrUnprocessable("Ceny ani poplatky nemohou být záporné.")
	}
	now := nowUTC()
	t := Tariff{
		ID: idgen.New(), EffectiveFrom: *in.EffectiveFrom,
		PriceVTHaler: *in.PriceVTHaler, PriceNTHaler: *in.PriceNTHaler,
		MonthlyFeeHaler: *in.MonthlyFeeHaler, Note: in.Note, CreatedAt: now, UpdatedAt: now,
	}
	if a := reqctx.ActorID(ctx); a != "" {
		t.CreatedBy = &a
	}
	msg := fmt.Sprintf("Ceník od %s už existuje.", czDate(t.EffectiveFrom))

	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, taken, err := s.store.TariffFrom(ctx, tx, t.EffectiveFrom, ""); err != nil {
			return err
		} else if taken {
			return httpx.ErrConflict(msg)
		}
		if err := s.store.InsertTariff(ctx, tx, t); err != nil {
			return uniqueViolation(err, msg)
		}
		return s.record(ctx, tx, "tariff.create", "electricity_tariff", t.ID,
			fmt.Sprintf("Ceník od %s — VT %s/MWh, NT %s/MWh, poplatky %s/měs",
				czDate(t.EffectiveFrom), kc2(t.PriceVTHaler), kc2(t.PriceNTHaler), kc2(t.MonthlyFeeHaler)), nil)
	})
	if err != nil {
		return Tariff{}, err
	}
	s.notify(ctx, "electricity_tariff.changed", map[string]any{"id": t.ID})
	return t, nil
}

func (s *Service) UpdateTariff(ctx context.Context, id string, in TariffInput) (Tariff, error) {
	var out Tariff
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		cur, ok, err := s.store.GetTariff(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.ErrNotFound("Ceník nenalezen.")
		}
		next := cur
		if in.EffectiveFrom != nil {
			next.EffectiveFrom = *in.EffectiveFrom
		}
		if in.PriceVTHaler != nil {
			next.PriceVTHaler = *in.PriceVTHaler
		}
		if in.PriceNTHaler != nil {
			next.PriceNTHaler = *in.PriceNTHaler
		}
		if in.MonthlyFeeHaler != nil {
			next.MonthlyFeeHaler = *in.MonthlyFeeHaler
		}
		if in.NoteSet {
			next.Note = in.Note
		}
		if next.PriceVTHaler < 0 || next.PriceNTHaler < 0 || next.MonthlyFeeHaler < 0 {
			return httpx.ErrUnprocessable("Ceny ani poplatky nemohou být záporné.")
		}
		msg := fmt.Sprintf("Ceník od %s už existuje.", czDate(next.EffectiveFrom))
		if !next.EffectiveFrom.Equal(cur.EffectiveFrom) {
			if _, taken, err := s.store.TariffFrom(ctx, tx, next.EffectiveFrom, id); err != nil {
				return err
			} else if taken {
				return httpx.ErrConflict(msg)
			}
			// Moving the version is the same D160 strand as deleting it when the
			// new date no longer reaches a period's first day.
			stranded, err := s.store.PeriodsNeedingTariff(ctx, tx, id)
			if err != nil {
				return err
			}
			for _, p := range stranded {
				if next.EffectiveFrom.After(p.StartsOn) {
					return httpx.ErrConflict(fmt.Sprintf(
						"Tento ceník je jediný, který platí k začátku zúčtovacího období %s. Bez něj by období nešlo spočítat.",
						czDate(p.StartsOn)))
				}
			}
		}
		next.UpdatedAt = nowUTC()

		// "Who changed the VT price and to what" is the question this module's
		// Log entries exist to answer, so every field is diffed.
		var changes []audit.Change
		diffStr("effective_from", cur.EffectiveFrom.String(), next.EffectiveFrom.String(), &changes)
		diffI64("price_vt_haler", cur.PriceVTHaler, next.PriceVTHaler, &changes)
		diffI64("price_nt_haler", cur.PriceNTHaler, next.PriceNTHaler, &changes)
		diffI64("monthly_fee_haler", cur.MonthlyFeeHaler, next.MonthlyFeeHaler, &changes)
		diffPtrStr("note", cur.Note, next.Note, &changes)

		if err := s.store.UpdateTariff(ctx, tx, next); err != nil {
			return uniqueViolation(err, msg)
		}
		out = next
		return s.record(ctx, tx, "tariff.update", "electricity_tariff", id,
			fmt.Sprintf("Upraven ceník od %s", czDate(next.EffectiveFrom)), changes)
	})
	if err != nil {
		return Tariff{}, err
	}
	s.notify(ctx, "electricity_tariff.changed", map[string]any{"id": id})
	return out, nil
}

// DeleteTariff refuses with 409 ONLY when the deletion would leave a day inside
// a settlement period with no effective ceník (PRD D160).
//
// This is much narrower than it sounds, and deliberately so. Deleting a MIDDLE
// version legitimately reprices its days — that is a real thing a household
// does when a version was entered by mistake — and the audit event records it.
// The earlier "covering existing days" wording would have frozen every version
// that had ever been used, which is not a safety property, just a nuisance.
func (s *Service) DeleteTariff(ctx context.Context, id string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		cur, ok, err := s.store.GetTariff(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.ErrNotFound("Ceník nenalezen.")
		}
		stranded, err := s.store.PeriodsNeedingTariff(ctx, tx, id)
		if err != nil {
			return err
		}
		if len(stranded) > 0 {
			return httpx.ErrConflict(fmt.Sprintf(
				"Tento ceník je jediný, který platí k začátku zúčtovacího období %s. Bez něj by období nešlo spočítat.",
				czDate(stranded[0].StartsOn)))
		}
		if err := s.store.SoftDelete(ctx, tx, "electricity_tariffs", id, nowUTC()); err != nil {
			return err
		}
		return s.record(ctx, tx, "tariff.delete", "electricity_tariff", id,
			fmt.Sprintf("Smazán ceník od %s — VT %s/MWh, NT %s/MWh, poplatky %s/měs",
				czDate(cur.EffectiveFrom), kc2(cur.PriceVTHaler), kc2(cur.PriceNTHaler),
				kc2(cur.MonthlyFeeHaler)), nil)
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "electricity_tariff.changed", map[string]any{"id": id})
	return nil
}

// ---------------------------------------------------------------------------
// Advances
// ---------------------------------------------------------------------------

type AdvanceInput struct {
	EffectiveFrom *dates.Date
	AmountHaler   *int64
	DueDay        *int
	Note          *string
	NoteSet       bool
}

// validDueDay refuses 0 and 32 at write time. Note what it does NOT do: clamp.
// The day is stored RAW and clamped at READ time (D155) — storing a clamped 28
// for a due day of 31 would be right for one February and wrong for every March
// after it.
func validDueDay(d int) error {
	if d < 1 || d > 31 {
		return httpx.ErrUnprocessable("Den splatnosti musí být mezi 1 a 31.")
	}
	return nil
}

func (s *Service) CreateAdvance(ctx context.Context, in AdvanceInput) (Advance, error) {
	if in.EffectiveFrom == nil || in.AmountHaler == nil || in.DueDay == nil {
		return Advance{}, httpx.ErrUnprocessable("Datum platnosti, částka i den splatnosti jsou povinné.")
	}
	if *in.AmountHaler < 0 {
		return Advance{}, httpx.ErrUnprocessable("Záloha nemůže být záporná.")
	}
	if err := validDueDay(*in.DueDay); err != nil {
		return Advance{}, err
	}
	now := nowUTC()
	a := Advance{
		ID: idgen.New(), EffectiveFrom: *in.EffectiveFrom, AmountHaler: *in.AmountHaler,
		DueDay: *in.DueDay, Note: in.Note, CreatedAt: now, UpdatedAt: now,
	}
	if id := reqctx.ActorID(ctx); id != "" {
		a.CreatedBy = &id
	}
	msg := fmt.Sprintf("Předpis záloh od %s už existuje.", czDate(a.EffectiveFrom))

	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, taken, err := s.store.AdvanceFrom(ctx, tx, a.EffectiveFrom, ""); err != nil {
			return err
		} else if taken {
			return httpx.ErrConflict(msg)
		}
		if err := s.store.InsertAdvance(ctx, tx, a); err != nil {
			return uniqueViolation(err, msg)
		}
		return s.record(ctx, tx, "advance.create", "electricity_advance", a.ID,
			fmt.Sprintf("Předpis záloh od %s — %s/měs, splatnost %d.",
				czDate(a.EffectiveFrom), kc(a.AmountHaler), a.DueDay), nil)
	})
	if err != nil {
		return Advance{}, err
	}
	s.notify(ctx, "electricity_advance.changed", map[string]any{"id": a.ID})
	return a, nil
}

func (s *Service) UpdateAdvance(ctx context.Context, id string, in AdvanceInput) (Advance, error) {
	var out Advance
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		cur, ok, err := s.store.GetAdvance(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.ErrNotFound("Předpis záloh nenalezen.")
		}
		next := cur
		if in.EffectiveFrom != nil {
			next.EffectiveFrom = *in.EffectiveFrom
		}
		if in.AmountHaler != nil {
			next.AmountHaler = *in.AmountHaler
		}
		if in.DueDay != nil {
			next.DueDay = *in.DueDay
		}
		if in.NoteSet {
			next.Note = in.Note
		}
		if next.AmountHaler < 0 {
			return httpx.ErrUnprocessable("Záloha nemůže být záporná.")
		}
		if err := validDueDay(next.DueDay); err != nil {
			return err
		}
		msg := fmt.Sprintf("Předpis záloh od %s už existuje.", czDate(next.EffectiveFrom))
		if !next.EffectiveFrom.Equal(cur.EffectiveFrom) {
			if _, taken, err := s.store.AdvanceFrom(ctx, tx, next.EffectiveFrom, id); err != nil {
				return err
			} else if taken {
				return httpx.ErrConflict(msg)
			}
		}
		next.UpdatedAt = nowUTC()

		var changes []audit.Change
		diffStr("effective_from", cur.EffectiveFrom.String(), next.EffectiveFrom.String(), &changes)
		diffI64("amount_haler", cur.AmountHaler, next.AmountHaler, &changes)
		diffI64("due_day", int64(cur.DueDay), int64(next.DueDay), &changes)
		diffPtrStr("note", cur.Note, next.Note, &changes)

		if err := s.store.UpdateAdvance(ctx, tx, next); err != nil {
			return uniqueViolation(err, msg)
		}
		out = next
		return s.record(ctx, tx, "advance.update", "electricity_advance", id,
			fmt.Sprintf("Upraven předpis záloh od %s", czDate(next.EffectiveFrom)), changes)
	})
	if err != nil {
		return Advance{}, err
	}
	s.notify(ctx, "electricity_advance.changed", map[string]any{"id": id})
	return out, nil
}

// DeleteAdvance has NO guard, unlike DeleteTariff. A counted month with no
// schedule and no payment contributes 0 Kč and renders "bez předpisu" — an
// honest, visible state rather than an error, so there is nothing to protect
// against here.
func (s *Service) DeleteAdvance(ctx context.Context, id string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		cur, ok, err := s.store.GetAdvance(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.ErrNotFound("Předpis záloh nenalezen.")
		}
		if err := s.store.SoftDelete(ctx, tx, "electricity_advances", id, nowUTC()); err != nil {
			return err
		}
		return s.record(ctx, tx, "advance.delete", "electricity_advance", id,
			fmt.Sprintf("Smazán předpis záloh od %s — %s/měs, splatnost %d.",
				czDate(cur.EffectiveFrom), kc(cur.AmountHaler), cur.DueDay), nil)
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "electricity_advance.changed", map[string]any{"id": id})
	return nil
}

// ---------------------------------------------------------------------------
// Payments
// ---------------------------------------------------------------------------

type PaymentInput struct {
	Month       *Month
	AmountHaler *int64
	PaidOn      *dates.Date
	PaidOnSet   bool
	Note        *string
	NoteSet     bool
}

func (s *Service) CreatePayment(ctx context.Context, in PaymentInput) (Payment, error) {
	if in.Month == nil || in.AmountHaler == nil {
		return Payment{}, httpx.ErrUnprocessable("Měsíc a částka jsou povinné.")
	}
	if *in.AmountHaler < 0 {
		return Payment{}, httpx.ErrUnprocessable("Zaplacená částka nemůže být záporná.")
	}
	now := nowUTC()
	p := Payment{
		ID: idgen.New(), Month: *in.Month, AmountHaler: *in.AmountHaler,
		PaidOn: in.PaidOn, Note: in.Note, CreatedAt: now, UpdatedAt: now,
	}
	if id := reqctx.ActorID(ctx); id != "" {
		p.CreatedBy = &id
	}
	msg := fmt.Sprintf("Platba za %s už je zapsaná.", p.Month)

	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, taken, err := s.store.PaymentForMonth(ctx, tx, p.Month, ""); err != nil {
			return err
		} else if taken {
			return httpx.ErrConflict(msg)
		}
		if err := s.store.InsertPayment(ctx, tx, p); err != nil {
			return uniqueViolation(err, msg)
		}
		return s.record(ctx, tx, "payment.create", "electricity_payment", p.ID,
			fmt.Sprintf("Zaplacená záloha za %s — %s", p.Month, kc(p.AmountHaler)), nil)
	})
	if err != nil {
		return Payment{}, err
	}
	s.notify(ctx, "electricity_payment.changed", map[string]any{"id": p.ID})
	return p, nil
}

func (s *Service) UpdatePayment(ctx context.Context, id string, in PaymentInput) (Payment, error) {
	var out Payment
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		cur, ok, err := s.store.GetPayment(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.ErrNotFound("Platba nenalezena.")
		}
		next := cur
		if in.Month != nil {
			next.Month = *in.Month
		}
		if in.AmountHaler != nil {
			next.AmountHaler = *in.AmountHaler
		}
		if in.PaidOnSet {
			next.PaidOn = in.PaidOn
		}
		if in.NoteSet {
			next.Note = in.Note
		}
		if next.AmountHaler < 0 {
			return httpx.ErrUnprocessable("Zaplacená částka nemůže být záporná.")
		}
		msg := fmt.Sprintf("Platba za %s už je zapsaná.", next.Month)
		if next.Month != cur.Month {
			if _, taken, err := s.store.PaymentForMonth(ctx, tx, next.Month, id); err != nil {
				return err
			} else if taken {
				return httpx.ErrConflict(msg)
			}
		}
		next.UpdatedAt = nowUTC()

		var changes []audit.Change
		diffStr("month", cur.Month.String(), next.Month.String(), &changes)
		diffI64("amount_haler", cur.AmountHaler, next.AmountHaler, &changes)
		oldPaid, newPaid := "", ""
		if cur.PaidOn != nil {
			oldPaid = cur.PaidOn.String()
		}
		if next.PaidOn != nil {
			newPaid = next.PaidOn.String()
		}
		diffStr("paid_on", oldPaid, newPaid, &changes)
		diffPtrStr("note", cur.Note, next.Note, &changes)

		if err := s.store.UpdatePayment(ctx, tx, next); err != nil {
			return uniqueViolation(err, msg)
		}
		out = next
		return s.record(ctx, tx, "payment.update", "electricity_payment", id,
			fmt.Sprintf("Upravena platba za %s", next.Month), changes)
	})
	if err != nil {
		return Payment{}, err
	}
	s.notify(ctx, "electricity_payment.changed", map[string]any{"id": id})
	return out, nil
}

func (s *Service) DeletePayment(ctx context.Context, id string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		cur, ok, err := s.store.GetPayment(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.ErrNotFound("Platba nenalezena.")
		}
		if err := s.store.SoftDelete(ctx, tx, "electricity_payments", id, nowUTC()); err != nil {
			return err
		}
		return s.record(ctx, tx, "payment.delete", "electricity_payment", id,
			fmt.Sprintf("Smazána platba za %s — %s", cur.Month, kc(cur.AmountHaler)), nil)
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "electricity_payment.changed", map[string]any{"id": id})
	return nil
}

// ---------------------------------------------------------------------------
// Periods
// ---------------------------------------------------------------------------

type PeriodInput struct {
	StartsOn             *dates.Date
	EndsOn               *dates.Date
	EndsOnConfirmed      *bool
	InvoicedTotalHaler   *int64
	InvoicedBalanceHaler *int64
	InvoicedVTDkwh       *int64
	InvoicedNTDkwh       *int64
	InvoicedAt           *string
	Note                 *string
	NoteSet              bool
	// EndsOnSet with a nil EndsOn means "clear it": the update re-applies the
	// default end, the same way create does when the field is omitted.
	EndsOnSet bool
	// *Set flags let a PATCH clear a recorded invoice figure back to null, which
	// matters when a vyúčtování was typed in wrong.
	InvoicedTotalSet   bool
	InvoicedBalanceSet bool
	InvoicedVTSet      bool
	InvoicedNTSet      bool
	InvoicedAtSet      bool
}

// defaultEndsOn is starts_on + 1 year − 1 day (PRD D153). Suppliers frequently
// do not state an end date in advance and a prediction has to project somewhere,
// so the period ships with an EXPECTED end and ends_on_confirmed false. The UI
// badges it "předpokládaný konec"; correcting it later is one PATCH field, after
// which every forecast figure follows and no actual figure moves.
func defaultEndsOn(start dates.Date) dates.Date {
	return dates.New(start.Y+1, start.M, start.D).AddDays(-1)
}

func (s *Service) CreatePeriod(ctx context.Context, in PeriodInput) (Period, error) {
	if in.StartsOn == nil {
		return Period{}, httpx.ErrUnprocessable("Začátek zúčtovacího období je povinný.")
	}
	p := Period{ID: idgen.New(), StartsOn: *in.StartsOn}
	if in.EndsOn != nil {
		p.EndsOn = *in.EndsOn
	} else {
		p.EndsOn = defaultEndsOn(p.StartsOn)
	}
	if in.EndsOnConfirmed != nil {
		p.EndsOnConfirmed = *in.EndsOnConfirmed
	}
	if p.EndsOn.Before(p.StartsOn) {
		return Period{}, httpx.ErrUnprocessable("Konec období nemůže být dřív než jeho začátek.")
	}
	now := nowUTC()
	p.Note, p.CreatedAt, p.UpdatedAt = in.Note, now, now
	p.InvoicedTotalHaler, p.InvoicedBalanceHaler = in.InvoicedTotalHaler, in.InvoicedBalanceHaler
	p.InvoicedVTDkwh, p.InvoicedNTDkwh, p.InvoicedAt = in.InvoicedVTDkwh, in.InvoicedNTDkwh, in.InvoicedAt
	if id := reqctx.ActorID(ctx); id != "" {
		p.CreatedBy = &id
	}

	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.checkOverlap(ctx, tx, p.StartsOn, p.EndsOn, ""); err != nil {
			return err
		}
		if err := s.store.InsertPeriod(ctx, tx, p); err != nil {
			return err
		}
		return s.record(ctx, tx, "period.create", "electricity_period", p.ID,
			fmt.Sprintf("Zúčtovací období %s – %s", czDate(p.StartsOn), czDate(p.EndsOn)), nil)
	})
	if err != nil {
		return Period{}, err
	}
	s.notify(ctx, "electricity_period.changed", map[string]any{"id": p.ID})
	return p, nil
}

// checkOverlap is a guard query inside the tx: SQLite has no exclusion
// constraints, so non-overlap cannot be a table constraint. Adjacency is legal
// and is the normal case — one meter reading closes one period and opens the
// next (D134).
func (s *Service) checkOverlap(ctx context.Context, tx *sql.Tx, startsOn, endsOn dates.Date, excludeID string) error {
	other, found, err := s.store.OverlappingPeriod(ctx, tx, startsOn, endsOn, excludeID)
	if err != nil {
		return err
	}
	if found {
		return httpx.ErrUnprocessable(fmt.Sprintf(
			"Zúčtovací období se překrývá s obdobím %s – %s.",
			czDate(other.StartsOn), czDate(other.EndsOn)))
	}
	return nil
}

func (s *Service) UpdatePeriod(ctx context.Context, id string, in PeriodInput) (Period, error) {
	var out Period
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		cur, ok, err := s.store.GetPeriod(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.ErrNotFound("Zúčtovací období nenalezeno.")
		}
		// NO LOCK CHECK HERE, and that is deliberate (D139). A period whose
		// vyúčtování has already been recorded stays fully editable.
		next := cur
		if in.StartsOn != nil {
			next.StartsOn = *in.StartsOn
		}
		if in.EndsOn != nil {
			next.EndsOn = *in.EndsOn
		} else if in.EndsOnSet {
			next.EndsOn = defaultEndsOn(next.StartsOn)
		}
		if in.EndsOnConfirmed != nil {
			next.EndsOnConfirmed = *in.EndsOnConfirmed
		}
		if in.InvoicedTotalSet {
			next.InvoicedTotalHaler = in.InvoicedTotalHaler
		}
		if in.InvoicedBalanceSet {
			next.InvoicedBalanceHaler = in.InvoicedBalanceHaler
		}
		if in.InvoicedVTSet {
			next.InvoicedVTDkwh = in.InvoicedVTDkwh
		}
		if in.InvoicedNTSet {
			next.InvoicedNTDkwh = in.InvoicedNTDkwh
		}
		if in.InvoicedAtSet {
			next.InvoicedAt = in.InvoicedAt
		}
		if in.NoteSet {
			next.Note = in.Note
		}
		if next.EndsOn.Before(next.StartsOn) {
			return httpx.ErrUnprocessable("Konec období nemůže být dřív než jeho začátek.")
		}
		if !next.StartsOn.Equal(cur.StartsOn) || !next.EndsOn.Equal(cur.EndsOn) {
			if err := s.checkOverlap(ctx, tx, next.StartsOn, next.EndsOn, id); err != nil {
				return err
			}
		}
		next.UpdatedAt = nowUTC()

		var changes []audit.Change
		diffStr("starts_on", cur.StartsOn.String(), next.StartsOn.String(), &changes)
		diffStr("ends_on", cur.EndsOn.String(), next.EndsOn.String(), &changes)
		diffStr("ends_on_confirmed", strconv.FormatBool(cur.EndsOnConfirmed),
			strconv.FormatBool(next.EndsOnConfirmed), &changes)
		diffOptI64("invoiced_total_haler", cur.InvoicedTotalHaler, next.InvoicedTotalHaler, &changes)
		diffOptI64("invoiced_balance_haler", cur.InvoicedBalanceHaler, next.InvoicedBalanceHaler, &changes)
		diffOptI64("invoiced_vt_dkwh", cur.InvoicedVTDkwh, next.InvoicedVTDkwh, &changes)
		diffOptI64("invoiced_nt_dkwh", cur.InvoicedNTDkwh, next.InvoicedNTDkwh, &changes)
		diffPtrStr("invoiced_at", cur.InvoicedAt, next.InvoicedAt, &changes)
		diffPtrStr("note", cur.Note, next.Note, &changes)

		if err := s.store.UpdatePeriod(ctx, tx, next); err != nil {
			return err
		}
		out = next
		return s.record(ctx, tx, "period.update", "electricity_period", id,
			fmt.Sprintf("Upraveno zúčtovací období %s – %s", czDate(next.StartsOn), czDate(next.EndsOn)), changes)
	})
	if err != nil {
		return Period{}, err
	}
	s.notify(ctx, "electricity_period.changed", map[string]any{"id": id})
	return out, nil
}

func diffOptI64(field string, old, new *int64, out *[]audit.Change) {
	o, n := "", ""
	if old != nil {
		o = strconv.FormatInt(*old, 10)
	}
	if new != nil {
		n = strconv.FormatInt(*new, 10)
	}
	diffStr(field, o, n, out)
}

func (s *Service) DeletePeriod(ctx context.Context, id string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		cur, ok, err := s.store.GetPeriod(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.ErrNotFound("Zúčtovací období nenalezeno.")
		}
		if err := s.store.SoftDelete(ctx, tx, "electricity_periods", id, nowUTC()); err != nil {
			return err
		}
		return s.record(ctx, tx, "period.delete", "electricity_period", id,
			fmt.Sprintf("Smazáno zúčtovací období %s – %s", czDate(cur.StartsOn), czDate(cur.EndsOn)), nil)
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "electricity_period.changed", map[string]any{"id": id})
	return nil
}

// ---------------------------------------------------------------------------
// Computed reads — all three go through compute.go over ONE loaded snapshot
// ---------------------------------------------------------------------------

// resolvePeriod finds the period a computed endpoint should answer for: the one
// asked for, else the one containing today. 404 when there is none — the
// first-run state is "create a period", not an empty dashboard.
func (s *Service) resolvePeriod(ctx context.Context, periodID string) (Period, error) {
	if periodID != "" {
		p, ok, err := s.store.GetPeriod(ctx, nil, periodID)
		if err != nil {
			return Period{}, err
		}
		if !ok {
			return Period{}, httpx.ErrNotFound("Zúčtovací období nenalezeno.")
		}
		return p, nil
	}
	p, ok, err := s.store.PeriodContaining(ctx, s.Today())
	if err != nil {
		return Period{}, err
	}
	if !ok {
		return Period{}, httpx.ErrNotFound("Zatím není založené žádné zúčtovací období.")
	}
	return p, nil
}

func (s *Service) snapshotFor(ctx context.Context, periodID string) (Snapshot, error) {
	p, err := s.resolvePeriod(ctx, periodID)
	if err != nil {
		return Snapshot{}, err
	}
	snap, ok, err := s.store.Snapshot(ctx, p.ID, s.Today())
	if err != nil {
		return Snapshot{}, err
	}
	if !ok {
		return Snapshot{}, httpx.ErrNotFound("Zúčtovací období nenalezeno.")
	}
	return snap, nil
}

func (s *Service) Summary(ctx context.Context, periodID string) (Summary, error) {
	snap, err := s.snapshotFor(ctx, periodID)
	if err != nil {
		return Summary{}, err
	}
	return Summarize(snap), nil
}

func (s *Service) Intervals(ctx context.Context, periodID string) ([]Interval, error) {
	snap, err := s.snapshotFor(ctx, periodID)
	if err != nil {
		return nil, err
	}
	ivs, _ := BuildIntervals(snap)
	return ivs, nil
}

// maxHistoryMonths caps an explicit ?from/?to range at a century — far past any
// household's meter and still small enough that the response cannot be used to
// stall the process. The defaults never approach it; only a hand-written URL can.
const maxHistoryMonths = 1200

// monthSpan counts the months in the inclusive range [f, t].
func monthSpan(f, t Month) int {
	return (t.Y-f.Y)*12 + int(t.M) - int(f.M) + 1
}

// History answers over EVERY period, not just the one containing today.
//
// A Snapshot is bounded to one period by construction — readings within
// [starts_on, ends_on+1], fee chunks over the same window — so BuildHistory over
// a single snapshot returns zeros for every month outside it. Charting from one
// snapshot while widening the range back to the earliest reading (below) would
// therefore promise months it could never fill, and a year of drawn consumption
// would vanish the day the next period opened. So one snapshot per period is
// loaded and the month points are SUMMED.
//
// Summing cannot double-count: periods are non-overlapping (checkOverlap) and
// adjacent ones meet exactly at ends_on+1 == next.starts_on, which is the
// half-open bound one period's fees and intervals stop at and the next begins
// from. The shared boundary reading belongs to both periods and to neither
// period's day count twice.
func (s *Service) History(ctx context.Context, from, to *Month) ([]MonthPoint, error) {
	periods, err := s.store.AllPeriods(ctx)
	if err != nil {
		return nil, err
	}
	if len(periods) == 0 {
		return nil, httpx.ErrNotFound("Zatím není založené žádné zúčtovací období.")
	}
	f := MonthOf(periods[0].StartsOn)
	if earliest, ok, err := s.store.EarliestReadingMonth(ctx); err != nil {
		return nil, err
	} else if ok && earliest.Before(f) {
		f = earliest
	}
	if from != nil {
		f = *from
	}
	t := MonthOf(s.Today())
	if last := MonthOf(periods[len(periods)-1].EndsOn); t.Before(last) {
		t = last
	}
	if to != nil {
		t = *to
	}
	if t.Before(f) {
		return nil, httpx.ErrUnprocessable("Konec rozsahu je dřív než jeho začátek.")
	}
	// The DEFAULTS above are bounded by the data, but `from` and `to` are not:
	// ParseMonth happily accepts 0001-01 and 9999-12, and the loops below then
	// materialise a month point per month HERE and again inside BuildHistory for
	// every period. Refused rather than clamped, for the same reason a malformed
	// cursor is a 422 — a chart quietly drawn over a different range than the one
	// asked for is worse than an error.
	if months := monthSpan(f, t); months > maxHistoryMonths {
		return nil, httpx.ErrUnprocessable(fmt.Sprintf(
			"Rozsah historie je příliš dlouhý: %d měsíců, nejvýš %d.", months, maxHistoryMonths))
	}

	out := []MonthPoint{}
	at := map[Month]int{}
	for m := f; !t.Before(m); m = m.Next() {
		at[m] = len(out)
		out = append(out, MonthPoint{Month: m})
	}
	for _, p := range periods {
		snap, ok, err := s.store.Snapshot(ctx, p.ID, s.Today())
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for _, pt := range BuildHistory(snap, f, t) {
			i, in := at[pt.Month]
			if !in {
				continue
			}
			out[i].VTDkwh += pt.VTDkwh
			out[i].NTDkwh += pt.NTDkwh
			out[i].EnergyHaler += pt.EnergyHaler
			out[i].FeesHaler += pt.FeesHaler
			out[i].TotalHaler += pt.TotalHaler
			out[i].IsApproximate = out[i].IsApproximate || pt.IsApproximate
		}
	}
	return out, nil
}
