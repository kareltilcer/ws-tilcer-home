package finance

import (
	"context"
	"database/sql"
	"errors"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
)

// DBTX is appdb.DBTX under this module's own name — an ALIAS, not a copy, so
// the two are one Go type. The WithTx read rule that governs it, and the note
// on why it lives in platform/db, are on that declaration.
type DBTX = appdb.DBTX

// Store is the finance module's SQL layer. It reads and writes the INPUTS only —
// the split is composed in Go by withSplit after scanning, and is never part of
// a query (D82).
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// tsFormat is RFC 3339 with fixed-width milliseconds, matching what the rest of
// home writes. Rows seeded from `fin` keep fin's nanosecond precision instead —
// provenance beats cosmetic consistency, and this module orders by `month`,
// never by a timestamp.
const tsFormat = "2006-01-02T15:04:05.000Z07:00"

func nowUTC() string { return appdb.NowUTC(tsFormat) }

const monthCols = `id, month, income_kaja, income_andy,
	rate_personal, rate_operational, rate_fun, rate_nofun,
	created_by, created_at, updated_at`

// scanMonth reads one row's inputs and composes the derived split onto it.
func scanMonth(s appdb.Scanner) (Month, error) {
	var m Month
	var createdBy sql.NullString
	err := s.Scan(&m.ID, &m.Month, &m.IncomeKaja, &m.IncomeAndy,
		&m.Rates.Personal, &m.Rates.Operational, &m.Rates.Fun, &m.Rates.NoFun,
		&createdBy, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return Month{}, err
	}
	if createdBy.Valid {
		v := createdBy.String
		m.CreatedBy = &v
	}
	return withSplit(m), nil
}

// List returns months newest first, keyset-paginated on `month` rather than on
// `id`: the user-visible order is by month, and month is unique, so it is the
// only cursor that agrees with what is on screen. (Ordering by id would put a
// back-filled month in the wrong place.)
func (s *Store) List(ctx context.Context, limit int, cursor string) ([]Month, *string, error) {
	q := `SELECT ` + monthCols + ` FROM finance_months`
	var args []any
	if cursor != "" {
		q += ` WHERE month < ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY month DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	out, err := appdb.Collect(rows, scanMonth)
	if err != nil {
		return nil, nil, err
	}

	var next *string
	if len(out) > limit {
		out = out[:limit]
		c := out[len(out)-1].Month
		next = &c
	}
	return out, next, nil
}

// Get returns one month by id, or ok=false when there is no such row.
func (s *Store) Get(ctx context.Context, q DBTX, id string) (Month, bool, error) {
	if q == nil {
		q = s.db
	}
	m, err := scanMonth(q.QueryRowContext(ctx, `SELECT `+monthCols+` FROM finance_months WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Month{}, false, nil
	}
	if err != nil {
		return Month{}, false, err
	}
	return m, true, nil
}

// ForMonth returns the row for a YYYY-MM key. It backs the widget's two states
// and the *_current metrics.
func (s *Store) ForMonth(ctx context.Context, ym string) (Month, bool, error) {
	m, err := scanMonth(s.db.QueryRowContext(ctx, `SELECT `+monthCols+` FROM finance_months WHERE month = ?`, ym))
	if errors.Is(err, sql.ErrNoRows) {
		return Month{}, false, nil
	}
	if err != nil {
		return Month{}, false, err
	}
	return m, true, nil
}

// Latest returns the most recent recorded month, whatever it is.
func (s *Store) Latest(ctx context.Context) (Month, bool, error) {
	m, err := scanMonth(s.db.QueryRowContext(ctx,
		`SELECT `+monthCols+` FROM finance_months ORDER BY month DESC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return Month{}, false, nil
	}
	if err != nil {
		return Month{}, false, err
	}
	return m, true, nil
}

// RecordedMonths returns every recorded YYYY-MM key in ascending order. The
// finance.missing_months METRIC and the LIST of the same key both derive from
// this one function, which is what makes the count and the items agree by
// construction rather than by review (D77's rule).
func (s *Store) RecordedMonths(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT month FROM finance_months ORDER BY month ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MonthTaken reports whether a month key is already recorded by a DIFFERENT row.
// exceptID is the row being edited (empty on create). Used to turn a duplicate
// into a 409 with a useful message rather than a raw constraint violation.
func (s *Store) MonthTaken(ctx context.Context, q DBTX, ym, exceptID string) (bool, error) {
	if q == nil {
		q = s.db
	}
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM finance_months WHERE month = ? AND id <> ?`, ym, exceptID).Scan(&n)
	return n > 0, err
}

// Insert writes a new month's inputs. Called inside the caller's transaction so
// the audit event commits with it.
func (s *Store) Insert(ctx context.Context, tx DBTX, m Month, actorID string) (Month, error) {
	m.CreatedAt = nowUTC()
	m.UpdatedAt = m.CreatedAt
	if actorID != "" {
		m.CreatedBy = &actorID
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO finance_months (`+monthCols+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.Month, m.IncomeKaja, m.IncomeAndy,
		m.Rates.Personal, m.Rates.Operational, m.Rates.Fun, m.Rates.NoFun,
		m.CreatedBy, m.CreatedAt, m.UpdatedAt)
	if err != nil {
		return Month{}, err
	}
	return withSplit(m), nil
}

// Update rewrites a month's inputs. created_by and created_at are never touched:
// a correction does not reassign authorship of the original entry.
func (s *Store) Update(ctx context.Context, tx DBTX, m Month) (Month, error) {
	m.UpdatedAt = nowUTC()
	_, err := tx.ExecContext(ctx, `UPDATE finance_months SET
		month = ?, income_kaja = ?, income_andy = ?,
		rate_personal = ?, rate_operational = ?, rate_fun = ?, rate_nofun = ?,
		updated_at = ?
		WHERE id = ?`,
		m.Month, m.IncomeKaja, m.IncomeAndy,
		m.Rates.Personal, m.Rates.Operational, m.Rates.Fun, m.Rates.NoFun,
		m.UpdatedAt, m.ID)
	if err != nil {
		return Month{}, err
	}
	return withSplit(m), nil
}

// Delete removes the row outright — a real DELETE, carried over from `fin`
// (D87). There is no deleted_at and no restore path; the month.delete audit
// event's full-row diff is the compensating control.
func (s *Store) Delete(ctx context.Context, tx DBTX, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM finance_months WHERE id = ?`, id)
	return err
}

// Count returns the number of recorded months (the finance.months_recorded
// metric).
func (s *Store) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM finance_months`).Scan(&n)
	return n, err
}
