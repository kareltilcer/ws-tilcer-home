package electricity

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
)

// DBTX is appdb.DBTX under this module's own name — an ALIAS, not a copy, so
// the two are one Go type. The WithTx read rule that governs it, and the note
// on why it lives in platform/db, are on that declaration.
type DBTX = appdb.DBTX

// Store is the electricity module's SQL layer. It stores INPUTS ONLY — every
// figure the module displays is computed on read by compute.go (PRD D152). There
// is no derived column, no cache table and nothing here that a background job
// could fall behind on.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const tsFormat = "2006-01-02T15:04:05.000Z07:00"

func nowUTC() string { return time.Now().UTC().Format(tsFormat) }

type scanner interface{ Scan(dest ...any) error }

func nullStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func strPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}

func i64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func nullI64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func datePtr(n sql.NullString) *dates.Date {
	if !n.Valid {
		return nil
	}
	d, err := dates.Parse(n.String)
	if err != nil {
		return nil
	}
	return &d
}

func nullDate(p *dates.Date) any {
	if p == nil {
		return nil
	}
	return p.String()
}

// ---------------------------------------------------------------------------
// Readings
// ---------------------------------------------------------------------------

const readingCols = `id, read_on, vt_dkwh, nt_dkwh, note, created_by, created_at, updated_at`

func scanReading(s scanner) (Reading, error) {
	var r Reading
	var readOn string
	var note, createdBy sql.NullString
	if err := s.Scan(&r.ID, &readOn, &r.VTDkwh, &r.NTDkwh, &note, &createdBy,
		&r.CreatedAt, &r.UpdatedAt); err != nil {
		return Reading{}, err
	}
	d, err := dates.Parse(readOn)
	if err != nil {
		return Reading{}, err
	}
	r.ReadOn, r.Note, r.CreatedBy = d, strPtr(note), strPtr(createdBy)
	return r, nil
}

// ListReadings returns readings NEWEST FIRST, keyset-paginated on `read_on`
// rather than on `id` (PRD D149).
//
// The cursor is a DATE because the user-visible order is chronological: a
// UUIDv7 cursor orders by INSERTION, so a reading back-filled today would page
// as if it belonged at the end of the list rather than at its own date. That is
// the finance month-key precedent. A malformed cursor is rejected by the handler
// with 422 rather than silently re-serving page one.
func (s *Store) ListReadings(ctx context.Context, limit int, cursor string) ([]Reading, *string, error) {
	q := `SELECT ` + readingCols + ` FROM electricity_readings WHERE deleted_at IS NULL`
	var args []any
	if cursor != "" {
		q += ` AND read_on < ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY read_on DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var out []Reading
	for rows.Next() {
		r, err := scanReading(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *string
	if len(out) > limit {
		out = out[:limit]
		c := out[len(out)-1].ReadOn.String()
		next = &c
	}
	return out, next, nil
}

func (s *Store) GetReading(ctx context.Context, q DBTX, id string) (Reading, bool, error) {
	if q == nil {
		q = s.db
	}
	r, err := scanReading(q.QueryRowContext(ctx,
		`SELECT `+readingCols+` FROM electricity_readings WHERE id = ? AND deleted_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Reading{}, false, nil
	}
	return r, err == nil, err
}

// NeighbourReadings returns the live readings immediately before and after `on`,
// excluding `excludeID`. This backs the monotonicity guard, which checks BOTH
// SIDES so a back-filled reading cannot break the chain in front of it (PRD
// FR-E1). Returning the neighbour rather than a boolean is deliberate: the error
// message has to name it and its value, and that message is the point.
func (s *Store) NeighbourReadings(ctx context.Context, q DBTX, on dates.Date, excludeID string) (prev, next *Reading, err error) {
	if q == nil {
		q = s.db
	}
	load := func(cmp, order string) (*Reading, error) {
		r, err := scanReading(q.QueryRowContext(ctx,
			`SELECT `+readingCols+` FROM electricity_readings
			 WHERE deleted_at IS NULL AND read_on `+cmp+` ? AND id <> ?
			 ORDER BY read_on `+order+` LIMIT 1`, on.String(), excludeID))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &r, nil
	}
	if prev, err = load("<", "DESC"); err != nil {
		return nil, nil, err
	}
	next, err = load(">", "ASC")
	return prev, next, err
}

// ReadingOn returns the live reading for a date, if any. Backs the 409 on a
// duplicate day.
func (s *Store) ReadingOn(ctx context.Context, q DBTX, on dates.Date, excludeID string) (Reading, bool, error) {
	if q == nil {
		q = s.db
	}
	r, err := scanReading(q.QueryRowContext(ctx,
		`SELECT `+readingCols+` FROM electricity_readings
		 WHERE deleted_at IS NULL AND read_on = ? AND id <> ?`, on.String(), excludeID))
	if errors.Is(err, sql.ErrNoRows) {
		return Reading{}, false, nil
	}
	return r, err == nil, err
}

func (s *Store) InsertReading(ctx context.Context, tx *sql.Tx, r Reading) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO electricity_readings (`+readingCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.ReadOn.String(), r.VTDkwh, r.NTDkwh, nullStr(r.Note), nullStr(r.CreatedBy),
		r.CreatedAt, r.UpdatedAt)
	return err
}

func (s *Store) UpdateReading(ctx context.Context, tx *sql.Tx, r Reading) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE electricity_readings SET read_on = ?, vt_dkwh = ?, nt_dkwh = ?, note = ?, updated_at = ?
		 WHERE id = ? AND deleted_at IS NULL`,
		r.ReadOn.String(), r.VTDkwh, r.NTDkwh, nullStr(r.Note), r.UpdatedAt, r.ID)
	return err
}

// ---------------------------------------------------------------------------
// Tariffs
// ---------------------------------------------------------------------------

const tariffCols = `id, effective_from, price_vt_haler, price_nt_haler, monthly_fee_haler,
	note, created_by, created_at, updated_at`

func scanTariff(s scanner) (Tariff, error) {
	var t Tariff
	var from string
	var note, createdBy sql.NullString
	if err := s.Scan(&t.ID, &from, &t.PriceVTHaler, &t.PriceNTHaler, &t.MonthlyFeeHaler,
		&note, &createdBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return Tariff{}, err
	}
	d, err := dates.Parse(from)
	if err != nil {
		return Tariff{}, err
	}
	t.EffectiveFrom, t.Note, t.CreatedBy = d, strPtr(note), strPtr(createdBy)
	return t, nil
}

func (s *Store) ListTariffs(ctx context.Context, limit int, cursor string) ([]Tariff, *string, error) {
	q := `SELECT ` + tariffCols + ` FROM electricity_tariffs WHERE deleted_at IS NULL`
	var args []any
	if cursor != "" {
		q += ` AND effective_from < ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY effective_from DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []Tariff
	for rows.Next() {
		t, err := scanTariff(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *string
	if len(out) > limit {
		out = out[:limit]
		c := out[len(out)-1].EffectiveFrom.String()
		next = &c
	}
	return out, next, nil
}

// AllTariffs returns every live version ASCENDING. compute.go needs all of them,
// not just those inside a period: the version governing a period's first day
// normally starts well before it.
func (s *Store) AllTariffs(ctx context.Context, q DBTX) ([]Tariff, error) {
	if q == nil {
		q = s.db
	}
	rows, err := q.QueryContext(ctx,
		`SELECT `+tariffCols+` FROM electricity_tariffs WHERE deleted_at IS NULL ORDER BY effective_from ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tariff
	for rows.Next() {
		t, err := scanTariff(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetTariff(ctx context.Context, q DBTX, id string) (Tariff, bool, error) {
	if q == nil {
		q = s.db
	}
	t, err := scanTariff(q.QueryRowContext(ctx,
		`SELECT `+tariffCols+` FROM electricity_tariffs WHERE id = ? AND deleted_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Tariff{}, false, nil
	}
	return t, err == nil, err
}

// NextTariffFrom returns the effective_from of the earliest live version STRICTLY
// after `from` — the thing that DERIVES this version's end (D136), since no end
// is ever stored.
//
// The list endpoint reads this off its own neighbours; a single-resource response
// has no neighbours to look at, so it asks here instead of reporting the version
// as open-ended.
func (s *Store) NextTariffFrom(ctx context.Context, q DBTX, from dates.Date) (dates.Date, bool, error) {
	if q == nil {
		q = s.db
	}
	var next string
	err := q.QueryRowContext(ctx,
		`SELECT effective_from FROM electricity_tariffs
		 WHERE deleted_at IS NULL AND effective_from > ?
		 ORDER BY effective_from ASC LIMIT 1`, from.String()).Scan(&next)
	if errors.Is(err, sql.ErrNoRows) {
		return dates.Date{}, false, nil
	}
	if err != nil {
		return dates.Date{}, false, err
	}
	d, err := dates.Parse(next)
	return d, err == nil, err
}

func (s *Store) TariffFrom(ctx context.Context, q DBTX, from dates.Date, excludeID string) (Tariff, bool, error) {
	if q == nil {
		q = s.db
	}
	t, err := scanTariff(q.QueryRowContext(ctx,
		`SELECT `+tariffCols+` FROM electricity_tariffs
		 WHERE deleted_at IS NULL AND effective_from = ? AND id <> ?`, from.String(), excludeID))
	if errors.Is(err, sql.ErrNoRows) {
		return Tariff{}, false, nil
	}
	return t, err == nil, err
}

func (s *Store) InsertTariff(ctx context.Context, tx *sql.Tx, t Tariff) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO electricity_tariffs (`+tariffCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.EffectiveFrom.String(), t.PriceVTHaler, t.PriceNTHaler, t.MonthlyFeeHaler,
		nullStr(t.Note), nullStr(t.CreatedBy), t.CreatedAt, t.UpdatedAt)
	return err
}

func (s *Store) UpdateTariff(ctx context.Context, tx *sql.Tx, t Tariff) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE electricity_tariffs SET effective_from = ?, price_vt_haler = ?, price_nt_haler = ?,
		 monthly_fee_haler = ?, note = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		t.EffectiveFrom.String(), t.PriceVTHaler, t.PriceNTHaler, t.MonthlyFeeHaler,
		nullStr(t.Note), t.UpdatedAt, t.ID)
	return err
}

// ---------------------------------------------------------------------------
// Advances (the schedule) and Payments (what was really paid)
// ---------------------------------------------------------------------------

const advanceCols = `id, effective_from, amount_haler, due_day, note, created_by, created_at, updated_at`

func scanAdvance(s scanner) (Advance, error) {
	var a Advance
	var from string
	var note, createdBy sql.NullString
	if err := s.Scan(&a.ID, &from, &a.AmountHaler, &a.DueDay, &note, &createdBy,
		&a.CreatedAt, &a.UpdatedAt); err != nil {
		return Advance{}, err
	}
	d, err := dates.Parse(from)
	if err != nil {
		return Advance{}, err
	}
	a.EffectiveFrom, a.Note, a.CreatedBy = d, strPtr(note), strPtr(createdBy)
	return a, nil
}

func (s *Store) ListAdvances(ctx context.Context, limit int, cursor string) ([]Advance, *string, error) {
	q := `SELECT ` + advanceCols + ` FROM electricity_advances WHERE deleted_at IS NULL`
	var args []any
	if cursor != "" {
		q += ` AND effective_from < ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY effective_from DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []Advance
	for rows.Next() {
		a, err := scanAdvance(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *string
	if len(out) > limit {
		out = out[:limit]
		c := out[len(out)-1].EffectiveFrom.String()
		next = &c
	}
	return out, next, nil
}

func (s *Store) AllAdvances(ctx context.Context, q DBTX) ([]Advance, error) {
	if q == nil {
		q = s.db
	}
	rows, err := q.QueryContext(ctx,
		`SELECT `+advanceCols+` FROM electricity_advances WHERE deleted_at IS NULL ORDER BY effective_from ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Advance
	for rows.Next() {
		a, err := scanAdvance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetAdvance(ctx context.Context, q DBTX, id string) (Advance, bool, error) {
	if q == nil {
		q = s.db
	}
	a, err := scanAdvance(q.QueryRowContext(ctx,
		`SELECT `+advanceCols+` FROM electricity_advances WHERE id = ? AND deleted_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Advance{}, false, nil
	}
	return a, err == nil, err
}

func (s *Store) AdvanceFrom(ctx context.Context, q DBTX, from dates.Date, excludeID string) (Advance, bool, error) {
	if q == nil {
		q = s.db
	}
	a, err := scanAdvance(q.QueryRowContext(ctx,
		`SELECT `+advanceCols+` FROM electricity_advances
		 WHERE deleted_at IS NULL AND effective_from = ? AND id <> ?`, from.String(), excludeID))
	if errors.Is(err, sql.ErrNoRows) {
		return Advance{}, false, nil
	}
	return a, err == nil, err
}

func (s *Store) InsertAdvance(ctx context.Context, tx *sql.Tx, a Advance) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO electricity_advances (`+advanceCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.EffectiveFrom.String(), a.AmountHaler, a.DueDay, nullStr(a.Note),
		nullStr(a.CreatedBy), a.CreatedAt, a.UpdatedAt)
	return err
}

func (s *Store) UpdateAdvance(ctx context.Context, tx *sql.Tx, a Advance) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE electricity_advances SET effective_from = ?, amount_haler = ?, due_day = ?, note = ?, updated_at = ?
		 WHERE id = ? AND deleted_at IS NULL`,
		a.EffectiveFrom.String(), a.AmountHaler, a.DueDay, nullStr(a.Note), a.UpdatedAt, a.ID)
	return err
}

const paymentCols = `id, month, amount_haler, paid_on, note, created_by, created_at, updated_at`

func scanPayment(s scanner) (Payment, error) {
	var p Payment
	var month string
	var paidOn, note, createdBy sql.NullString
	if err := s.Scan(&p.ID, &month, &p.AmountHaler, &paidOn, &note, &createdBy,
		&p.CreatedAt, &p.UpdatedAt); err != nil {
		return Payment{}, err
	}
	m, err := ParseMonth(month)
	if err != nil {
		return Payment{}, err
	}
	p.Month, p.PaidOn, p.Note, p.CreatedBy = m, datePtr(paidOn), strPtr(note), strPtr(createdBy)
	return p, nil
}

func (s *Store) ListPayments(ctx context.Context, limit int, cursor string) ([]Payment, *string, error) {
	q := `SELECT ` + paymentCols + ` FROM electricity_payments WHERE deleted_at IS NULL`
	var args []any
	if cursor != "" {
		q += ` AND month < ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY month DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []Payment
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *string
	if len(out) > limit {
		out = out[:limit]
		c := out[len(out)-1].Month.String()
		next = &c
	}
	return out, next, nil
}

func (s *Store) GetPayment(ctx context.Context, q DBTX, id string) (Payment, bool, error) {
	if q == nil {
		q = s.db
	}
	p, err := scanPayment(q.QueryRowContext(ctx,
		`SELECT `+paymentCols+` FROM electricity_payments WHERE id = ? AND deleted_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Payment{}, false, nil
	}
	return p, err == nil, err
}

func (s *Store) PaymentForMonth(ctx context.Context, q DBTX, m Month, excludeID string) (Payment, bool, error) {
	if q == nil {
		q = s.db
	}
	p, err := scanPayment(q.QueryRowContext(ctx,
		`SELECT `+paymentCols+` FROM electricity_payments
		 WHERE deleted_at IS NULL AND month = ? AND id <> ?`, m.String(), excludeID))
	if errors.Is(err, sql.ErrNoRows) {
		return Payment{}, false, nil
	}
	return p, err == nil, err
}

// PaymentsBetween returns the live payments whose month key falls in
// [from, to] inclusive. compute.go only ever needs the counted months.
func (s *Store) PaymentsBetween(ctx context.Context, q DBTX, from, to Month) ([]Payment, error) {
	if q == nil {
		q = s.db
	}
	rows, err := q.QueryContext(ctx,
		`SELECT `+paymentCols+` FROM electricity_payments
		 WHERE deleted_at IS NULL AND month >= ? AND month <= ? ORDER BY month ASC`,
		from.String(), to.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Payment
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) InsertPayment(ctx context.Context, tx *sql.Tx, p Payment) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO electricity_payments (`+paymentCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Month.String(), p.AmountHaler, nullDate(p.PaidOn), nullStr(p.Note),
		nullStr(p.CreatedBy), p.CreatedAt, p.UpdatedAt)
	return err
}

func (s *Store) UpdatePayment(ctx context.Context, tx *sql.Tx, p Payment) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE electricity_payments SET month = ?, amount_haler = ?, paid_on = ?, note = ?, updated_at = ?
		 WHERE id = ? AND deleted_at IS NULL`,
		p.Month.String(), p.AmountHaler, nullDate(p.PaidOn), nullStr(p.Note), p.UpdatedAt, p.ID)
	return err
}

// ---------------------------------------------------------------------------
// Periods
// ---------------------------------------------------------------------------

const periodCols = `id, starts_on, ends_on, ends_on_confirmed,
	invoiced_total_haler, invoiced_balance_haler, invoiced_vt_dkwh, invoiced_nt_dkwh,
	invoiced_at, note, created_by, created_at, updated_at`

func scanPeriod(s scanner) (Period, error) {
	var p Period
	var startsOn, endsOn string
	var confirmed int
	var total, balance, vt, nt sql.NullInt64
	var invoicedAt, note, createdBy sql.NullString
	if err := s.Scan(&p.ID, &startsOn, &endsOn, &confirmed, &total, &balance, &vt, &nt,
		&invoicedAt, &note, &createdBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return Period{}, err
	}
	sd, err := dates.Parse(startsOn)
	if err != nil {
		return Period{}, err
	}
	ed, err := dates.Parse(endsOn)
	if err != nil {
		return Period{}, err
	}
	p.StartsOn, p.EndsOn, p.EndsOnConfirmed = sd, ed, confirmed != 0
	p.InvoicedTotalHaler, p.InvoicedBalanceHaler = i64Ptr(total), i64Ptr(balance)
	p.InvoicedVTDkwh, p.InvoicedNTDkwh = i64Ptr(vt), i64Ptr(nt)
	p.InvoicedAt, p.Note, p.CreatedBy = strPtr(invoicedAt), strPtr(note), strPtr(createdBy)
	return p, nil
}

func (s *Store) ListPeriods(ctx context.Context, limit int, cursor string) ([]Period, *string, error) {
	q := `SELECT ` + periodCols + ` FROM electricity_periods WHERE deleted_at IS NULL`
	var args []any
	if cursor != "" {
		q += ` AND starts_on < ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY starts_on DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []Period
	for rows.Next() {
		p, err := scanPeriod(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *string
	if len(out) > limit {
		out = out[:limit]
		c := out[len(out)-1].StartsOn.String()
		next = &c
	}
	return out, next, nil
}

// AllPeriods returns every live period ASCENDING. /history needs all of them,
// not just the one containing today: a Snapshot is bounded to a single period,
// so a chart drawn from one snapshot can only ever show that period's months.
func (s *Store) AllPeriods(ctx context.Context) ([]Period, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+periodCols+` FROM electricity_periods
		 WHERE deleted_at IS NULL ORDER BY starts_on ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Period
	for rows.Next() {
		p, err := scanPeriod(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetPeriod(ctx context.Context, q DBTX, id string) (Period, bool, error) {
	if q == nil {
		q = s.db
	}
	p, err := scanPeriod(q.QueryRowContext(ctx,
		`SELECT `+periodCols+` FROM electricity_periods WHERE id = ? AND deleted_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Period{}, false, nil
	}
	return p, err == nil, err
}

// PeriodContaining returns the live period containing `on`. It is what /summary
// and /intervals default to, so opening Elektřina lands on the period you are
// actually in.
func (s *Store) PeriodContaining(ctx context.Context, on dates.Date) (Period, bool, error) {
	p, err := scanPeriod(s.db.QueryRowContext(ctx,
		`SELECT `+periodCols+` FROM electricity_periods
		 WHERE deleted_at IS NULL AND starts_on <= ? AND ends_on >= ?
		 ORDER BY starts_on DESC LIMIT 1`, on.String(), on.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return Period{}, false, nil
	}
	return p, err == nil, err
}

// OverlappingPeriod finds a live period that collides with [startsOn, endsOn],
// excluding excludeID. SQLite has no exclusion constraints, so non-overlap is a
// guard query inside the transaction rather than a table constraint.
//
// The predicate is the standard interval overlap — `a.start <= b.end AND a.end
// >= b.start` — which correctly ALLOWS adjacency: a period ending 23. 6. and the
// next starting 24. 6. do not collide, and they are the normal case, since one
// meter reading closes one period and opens the next.
func (s *Store) OverlappingPeriod(ctx context.Context, q DBTX, startsOn, endsOn dates.Date, excludeID string) (Period, bool, error) {
	if q == nil {
		q = s.db
	}
	p, err := scanPeriod(q.QueryRowContext(ctx,
		`SELECT `+periodCols+` FROM electricity_periods
		 WHERE deleted_at IS NULL AND id <> ? AND starts_on <= ? AND ends_on >= ?
		 ORDER BY starts_on ASC LIMIT 1`, excludeID, endsOn.String(), startsOn.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return Period{}, false, nil
	}
	return p, err == nil, err
}

// PeriodsNeedingTariff backs D160's narrow 409. It returns the live periods that
// would be left with NO effective ceník if `excludeTariffID` were deleted — i.e.
// those with no OTHER version effective on or before their first day.
//
// Note how narrow this is. Deleting a MIDDLE version legitimately reprices its
// days and is allowed; the audit event records it. Only the version that covers
// a period's start is load-bearing, because without it the period has no
// baseline price at all.
//
// ⚠ BOTH halves of the predicate are required, and the EXISTS is the one that is
// easy to leave out. A period that ALREADY has no version covering its first day
// satisfies the NOT EXISTS whatever is being deleted, so on its own the guard
// refuses every tariff in the database and says the deleted one "is the only one
// effective at the start of period X" — a claim that is false about a version
// dated after X, and one that traps a mistyped effective_from with no way out.
// The version must itself cover the start before its removal can strand anyone.
func (s *Store) PeriodsNeedingTariff(ctx context.Context, q DBTX, excludeTariffID string) ([]Period, error) {
	if q == nil {
		q = s.db
	}
	rows, err := q.QueryContext(ctx,
		`SELECT `+periodCols+` FROM electricity_periods p
		 WHERE p.deleted_at IS NULL AND EXISTS (
		     SELECT 1 FROM electricity_tariffs t
		     WHERE t.deleted_at IS NULL AND t.id = ? AND t.effective_from <= p.starts_on)
		 AND NOT EXISTS (
		     SELECT 1 FROM electricity_tariffs t
		     WHERE t.deleted_at IS NULL AND t.id <> ? AND t.effective_from <= p.starts_on)
		 ORDER BY p.starts_on ASC`, excludeTariffID, excludeTariffID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Period
	for rows.Next() {
		p, err := scanPeriod(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) InsertPeriod(ctx context.Context, tx *sql.Tx, p Period) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO electricity_periods (`+periodCols+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.StartsOn.String(), p.EndsOn.String(), boolInt(p.EndsOnConfirmed),
		nullI64(p.InvoicedTotalHaler), nullI64(p.InvoicedBalanceHaler),
		nullI64(p.InvoicedVTDkwh), nullI64(p.InvoicedNTDkwh),
		nullStr(p.InvoicedAt), nullStr(p.Note), nullStr(p.CreatedBy), p.CreatedAt, p.UpdatedAt)
	return err
}

func (s *Store) UpdatePeriod(ctx context.Context, tx *sql.Tx, p Period) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE electricity_periods SET starts_on = ?, ends_on = ?, ends_on_confirmed = ?,
		 invoiced_total_haler = ?, invoiced_balance_haler = ?, invoiced_vt_dkwh = ?,
		 invoiced_nt_dkwh = ?, invoiced_at = ?, note = ?, updated_at = ?
		 WHERE id = ? AND deleted_at IS NULL`,
		p.StartsOn.String(), p.EndsOn.String(), boolInt(p.EndsOnConfirmed),
		nullI64(p.InvoicedTotalHaler), nullI64(p.InvoicedBalanceHaler),
		nullI64(p.InvoicedVTDkwh), nullI64(p.InvoicedNTDkwh),
		nullStr(p.InvoicedAt), nullStr(p.Note), p.UpdatedAt, p.ID)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Soft delete — the house default (?hard=true is not offered here)
// ---------------------------------------------------------------------------

// SoftDelete marks one row deleted. The table is a parameter rather than part of
// five near-identical methods; it is never user-supplied.
func (s *Store) SoftDelete(ctx context.Context, tx *sql.Tx, table, id, at string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE `+table+` SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		at, at, id)
	return err
}

// ---------------------------------------------------------------------------
// THE read that carries weight
// ---------------------------------------------------------------------------

// Snapshot loads everything one period's computation needs, in FIVE QUERIES and
// with no N+1.
//
// Summarize, BuildIntervals and BuildHistory all consume this one struct — three
// endpoints, one load path, one truth. That is the whole reason this function
// exists rather than each endpoint loading what it happens to need: if /summary
// and /intervals loaded readings by different bounds they would eventually
// disagree about a number, and the disagreement would be invisible.
//
// Two loading rules are easy to get subtly wrong, and both are load-bearing:
//
//   - readings load THROUGH ends_on + 1, not ends_on. The closing reading is
//     dated the day AFTER the period (D134), and its presence is what flips the
//     period to `complete` (D157). Loading to ends_on would mean a finished
//     period went on forecasting an answer it already had.
//   - ALL tariffs and ALL advances load, not just those inside the period. The
//     version governing the first day normally starts well before it, and a
//     version dated in the future is what the forecast prices with (D142).
//
// `today` is resolved ONCE by the caller from HOME_TIMEZONE and travels in on
// the snapshot; compute.go never reads a clock.
func (s *Store) Snapshot(ctx context.Context, periodID string, today dates.Date) (Snapshot, bool, error) {
	period, ok, err := s.GetPeriod(ctx, nil, periodID)
	if err != nil || !ok {
		return Snapshot{}, ok, err
	}
	snap := Snapshot{Period: period, Today: today}

	endExcl := period.EndsOn.AddDays(1)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+readingCols+` FROM electricity_readings
		 WHERE deleted_at IS NULL AND read_on >= ? AND read_on <= ?
		 ORDER BY read_on ASC`, period.StartsOn.String(), endExcl.String())
	if err != nil {
		return Snapshot{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanReading(rows)
		if err != nil {
			return Snapshot{}, false, err
		}
		snap.Readings = append(snap.Readings, r)
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, false, err
	}

	if snap.Tariffs, err = s.AllTariffs(ctx, nil); err != nil {
		return Snapshot{}, false, err
	}
	if snap.Advances, err = s.AllAdvances(ctx, nil); err != nil {
		return Snapshot{}, false, err
	}
	if snap.Payments, err = s.PaymentsBetween(ctx, nil,
		MonthOf(period.StartsOn), MonthOf(period.EndsOn)); err != nil {
		return Snapshot{}, false, err
	}
	return snap, true, nil
}

// EarliestReadingMonth backs /history's default `from`.
func (s *Store) EarliestReadingMonth(ctx context.Context) (Month, bool, error) {
	var on string
	err := s.db.QueryRowContext(ctx,
		`SELECT read_on FROM electricity_readings WHERE deleted_at IS NULL ORDER BY read_on ASC LIMIT 1`).Scan(&on)
	if errors.Is(err, sql.ErrNoRows) {
		return Month{}, false, nil
	}
	if err != nil {
		return Month{}, false, err
	}
	d, err := dates.Parse(on)
	if err != nil {
		return Month{}, false, err
	}
	return MonthOf(d), true, nil
}
