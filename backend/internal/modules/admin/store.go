package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/scheduler"
)

// tsFormat matches the platform convention: RFC 3339 with nanoseconds, so string
// ordering is chronological (which the delivery log's keyset paging relies on).
const tsFormat = time.RFC3339Nano

// Store is the admin module's data access. It owns the rule, schedule and
// delivery tables; it never reaches into another module's.
type Store struct{ db *sql.DB }

// NewStore returns the admin store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// dbtx is satisfied by *sql.DB and *sql.Tx. The pool holds a single connection,
// so a read issued on the DB while a transaction is open would deadlock against
// that transaction — anything callable from inside a tx takes the querier.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ---- rules ----

const ruleColumns = `id, name, enabled, action_key, action_prefix, filter_module, filter_entity_type,
	filter_level, audience, title_template, body_template, coalesce_window_seconds, exclude_actor,
	created_by, created_at, updated_at`

// ListRules returns rules newest-first, keyset-paged by UUIDv7 id.
func (s *Store) ListRules(ctx context.Context, enabledOnly *bool, limit int, cursor string) ([]Rule, *string, error) {
	q := `SELECT ` + ruleColumns + ` FROM notification_rules WHERE 1=1`
	var args []any
	if enabledOnly != nil {
		q += ` AND enabled = ?`
		args = append(args, b2i(*enabledOnly))
	}
	if cursor != "" {
		q += ` AND id < ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Rule
	for rows.Next() {
		r, err := scanRule(rows)
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
		last := out[limit-1].ID
		out = out[:limit]
		next = &last
	}
	return out, next, nil
}

// EnabledRules loads the rules the trigger listener matches against. The listener
// caches the result and reloads on CRUD, so this is not on the per-event path.
func (s *Store) EnabledRules(ctx context.Context) ([]Rule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+ruleColumns+` FROM notification_rules WHERE enabled = 1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRule returns one rule, or nil when it does not exist.
func (s *Store) GetRule(ctx context.Context, q dbtx, id string) (*Rule, error) {
	row := q.QueryRowContext(ctx, `SELECT `+ruleColumns+` FROM notification_rules WHERE id = ?`, id)
	r, err := scanRule(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// InsertRule writes a new rule inside the caller's transaction (so it commits
// with its audit event).
func (s *Store) InsertRule(ctx context.Context, tx *sql.Tx, r Rule) error {
	audienceJSON, err := json.Marshal(r.Audience)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO notification_rules (`+ruleColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Name, b2i(r.Enabled), r.ActionKey, r.ActionPrefix, r.FilterModule, r.FilterEntityType,
		r.FilterLevel, string(audienceJSON), r.TitleTemplate, r.BodyTemplate, r.CoalesceWindowSeconds,
		b2i(r.ExcludeActor), r.CreatedBy, r.CreatedAt.UTC().Format(tsFormat), r.UpdatedAt.UTC().Format(tsFormat))
	return err
}

// UpdateRule replaces a rule's mutable fields.
func (s *Store) UpdateRule(ctx context.Context, tx *sql.Tx, r Rule) error {
	audienceJSON, err := json.Marshal(r.Audience)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE notification_rules
		    SET name = ?, enabled = ?, action_key = ?, action_prefix = ?, filter_module = ?,
		        filter_entity_type = ?, filter_level = ?, audience = ?, title_template = ?,
		        body_template = ?, coalesce_window_seconds = ?, exclude_actor = ?, updated_at = ?
		  WHERE id = ?`,
		r.Name, b2i(r.Enabled), r.ActionKey, r.ActionPrefix, r.FilterModule, r.FilterEntityType,
		r.FilterLevel, string(audienceJSON), r.TitleTemplate, r.BodyTemplate, r.CoalesceWindowSeconds,
		b2i(r.ExcludeActor), r.UpdatedAt.UTC().Format(tsFormat), r.ID)
	return err
}

// DeleteRule removes a rule. Rules are configuration, so deletion is hard —
// there is no archived state to browse and the audit spine records the removal.
func (s *Store) DeleteRule(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM notification_rules WHERE id = ?`, id)
	return err
}

func scanRule(row scanner) (Rule, error) {
	var (
		r             Rule
		enabled, excl int
		audienceJSON  string
		created, upd  string
		createdBy     sql.NullString
	)
	if err := row.Scan(&r.ID, &r.Name, &enabled, &r.ActionKey, &r.ActionPrefix, &r.FilterModule,
		&r.FilterEntityType, &r.FilterLevel, &audienceJSON, &r.TitleTemplate, &r.BodyTemplate,
		&r.CoalesceWindowSeconds, &excl, &createdBy, &created, &upd); err != nil {
		return Rule{}, err
	}
	r.Enabled = enabled == 1
	r.ExcludeActor = excl == 1
	r.CreatedBy = createdBy.String
	_ = json.Unmarshal([]byte(audienceJSON), &r.Audience)
	r.CreatedAt = parseTS(created)
	r.UpdatedAt = parseTS(upd)
	return r, nil
}

// ---- schedules ----

const scheduleColumns = `id, name, enabled, time_local, days_spec, audience, title_template,
	body_template, last_fired_at, last_fired_local_date, created_by, created_at, updated_at`

// ListSchedules returns schedules newest-first, keyset-paged.
func (s *Store) ListSchedules(ctx context.Context, enabledOnly *bool, limit int, cursor string) ([]Schedule, *string, error) {
	q := `SELECT ` + scheduleColumns + ` FROM notification_schedules WHERE 1=1`
	var args []any
	if enabledOnly != nil {
		q += ` AND enabled = ?`
		args = append(args, b2i(*enabledOnly))
	}
	if cursor != "" {
		q += ` AND id < ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Schedule
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	var next *string
	if len(out) > limit {
		last := out[limit-1].ID
		out = out[:limit]
		next = &last
	}
	return out, next, nil
}

// GetSchedule returns one schedule, or nil.
func (s *Store) GetSchedule(ctx context.Context, q dbtx, id string) (*Schedule, error) {
	row := q.QueryRowContext(ctx, `SELECT `+scheduleColumns+` FROM notification_schedules WHERE id = ?`, id)
	sc, err := scanSchedule(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sc, nil
}

func (s *Store) InsertSchedule(ctx context.Context, tx *sql.Tx, sc Schedule) error {
	audienceJSON, err := json.Marshal(sc.Audience)
	if err != nil {
		return err
	}
	daysJSON, err := json.Marshal(sc.Schedule.Days)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO notification_schedules (`+scheduleColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sc.ID, sc.Name, b2i(sc.Enabled), sc.Schedule.TimeLocal, string(daysJSON), string(audienceJSON),
		sc.TitleTemplate, sc.BodyTemplate, nil, nil, sc.CreatedBy,
		sc.CreatedAt.UTC().Format(tsFormat), sc.UpdatedAt.UTC().Format(tsFormat))
	return err
}

func (s *Store) UpdateSchedule(ctx context.Context, tx *sql.Tx, sc Schedule) error {
	audienceJSON, err := json.Marshal(sc.Audience)
	if err != nil {
		return err
	}
	daysJSON, err := json.Marshal(sc.Schedule.Days)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE notification_schedules
		    SET name = ?, enabled = ?, time_local = ?, days_spec = ?, audience = ?,
		        title_template = ?, body_template = ?, updated_at = ?
		  WHERE id = ?`,
		sc.Name, b2i(sc.Enabled), sc.Schedule.TimeLocal, string(daysJSON), string(audienceJSON),
		sc.TitleTemplate, sc.BodyTemplate, sc.UpdatedAt.UTC().Format(tsFormat), sc.ID)
	return err
}

func (s *Store) DeleteSchedule(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM notification_schedules WHERE id = ?`, id)
	return err
}

// DueCandidates implements scheduler.Store: every enabled schedule. A household
// has a handful, so the calendar filtering happens in Go where the DST and
// short-month rules live rather than in SQL date functions.
func (s *Store) DueCandidates(ctx context.Context) ([]scheduler.Schedule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, enabled, time_local, days_spec, updated_at, COALESCE(last_fired_local_date, '')
		   FROM notification_schedules WHERE enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []scheduler.Schedule
	for rows.Next() {
		var (
			sc                scheduler.Schedule
			enabled           int
			daysJSON, updated string
		)
		if err := rows.Scan(&sc.ID, &sc.Name, &enabled, &sc.TimeLocal, &daysJSON, &updated,
			&sc.LastFiredLocalDate); err != nil {
			return nil, err
		}
		sc.Enabled = enabled == 1
		// updated_at is the schedule's "configured at": the ticker uses it to tell
		// a slot missed during downtime from one that had not been configured yet.
		sc.ConfiguredAt = parseTS(updated)
		_ = json.Unmarshal([]byte(daysJSON), &sc.Days)
		out = append(out, sc)
	}
	return out, rows.Err()
}

// MarkFired implements scheduler.Store. It is called BEFORE the send, so the
// slot is spent even if the process dies mid-fan-out.
func (s *Store) MarkFired(ctx context.Context, scheduleID, localDate string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notification_schedules SET last_fired_at = ?, last_fired_local_date = ? WHERE id = ?`,
		at.UTC().Format(tsFormat), localDate, scheduleID)
	return err
}

func scanSchedule(row scanner) (Schedule, error) {
	var (
		sc                     Schedule
		enabled                int
		daysJSON, audienceJSON string
		lastFiredAt, lastLocal sql.NullString
		createdBy              sql.NullString
		created, upd           string
	)
	if err := row.Scan(&sc.ID, &sc.Name, &enabled, &sc.Schedule.TimeLocal, &daysJSON, &audienceJSON,
		&sc.TitleTemplate, &sc.BodyTemplate, &lastFiredAt, &lastLocal, &createdBy, &created, &upd); err != nil {
		return Schedule{}, err
	}
	sc.Enabled = enabled == 1
	sc.CreatedBy = createdBy.String
	_ = json.Unmarshal([]byte(daysJSON), &sc.Schedule.Days)
	_ = json.Unmarshal([]byte(audienceJSON), &sc.Audience)
	if lastFiredAt.Valid && lastFiredAt.String != "" {
		t := parseTS(lastFiredAt.String)
		sc.LastFiredAt = &t
	}
	sc.LastFiredLocalDate = lastLocal.String
	sc.CreatedAt = parseTS(created)
	sc.UpdatedAt = parseTS(upd)
	sc.Description = scheduler.Describe(sc.Schedule.TimeLocal, sc.Schedule.Days)
	return sc, nil
}

// ---- deliveries (operational) ----

// RecordDeliveries implements push.Recorder: one row per endpoint attempt.
//
// The table is the admin module's, which is why platform/push does not write it
// directly — it calls this recorder instead, and a household running without the
// admin module would simply keep no delivery log.
func (s *Store) RecordDeliveries(ctx context.Context, results []push.DeliveryResult) error {
	if len(results) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	ts := time.Now().UTC().Format(tsFormat)
	for _, r := range results {
		var ruleID, subID, errMsg any
		if r.RuleID != "" {
			ruleID = r.RuleID
		}
		if r.SubscriptionID != "" {
			subID = r.SubscriptionID
		}
		if r.Error != "" {
			errMsg = truncate(r.Error, 500)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO notification_deliveries (id, ts, kind, category, rule_id, user_id, subscription_id, status, error)
			 VALUES (?,?,?,?,?,?,?,?,?)`,
			idgen.New(), ts, r.Kind, r.Category, ruleID, r.UserID, subID, r.Status, errMsg); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListDeliveries pages the delivery log newest-first by UUIDv7 keyset — the same
// pattern the log browser uses, so the two screens behave identically.
func (s *Store) ListDeliveries(ctx context.Context, f DeliveryFilter) ([]Delivery, *string, error) {
	q := `SELECT id, ts, kind, category, rule_id, user_id, subscription_id, status, error
	        FROM notification_deliveries WHERE 1=1`
	var args []any
	if f.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, f.Kind)
	}
	if f.Status != "" {
		q += ` AND status = ?`
		args = append(args, f.Status)
	}
	if f.RuleID != "" {
		q += ` AND rule_id = ?`
		args = append(args, f.RuleID)
	}
	if f.UserID != "" {
		q += ` AND user_id = ?`
		args = append(args, f.UserID)
	}
	if f.From != "" {
		q += ` AND ts >= ?`
		args = append(args, dayStart(f.From))
	}
	if f.To != "" {
		q += ` AND ts <= ?`
		args = append(args, dayEnd(f.To))
	}
	if f.Cursor != "" {
		q += ` AND id < ?`
		args = append(args, f.Cursor)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, f.Limit+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Delivery
	for rows.Next() {
		var (
			d           Delivery
			ts          string
			ruleID, sub sql.NullString
			errMsg      sql.NullString
		)
		if err := rows.Scan(&d.ID, &ts, &d.Kind, &d.Category, &ruleID, &d.UserID, &sub, &d.Status, &errMsg); err != nil {
			return nil, nil, err
		}
		d.TS = parseTS(ts)
		d.RuleID = nullable(ruleID)
		d.SubscriptionID = nullable(sub)
		d.Error = nullable(errMsg)
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	var next *string
	if len(out) > f.Limit {
		last := out[f.Limit-1].ID
		out = out[:f.Limit]
		next = &last
	}
	return out, next, nil
}

// PruneDeliveries drops rows older than retentionDays. 0 keeps them forever
// (D64). Deliveries are operational evidence, not the audit spine, so pruning
// them loses no accountability.
func (s *Store) PruneDeliveries(ctx context.Context, retentionDays int, now time.Time) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := now.UTC().AddDate(0, 0, -retentionDays).Format(tsFormat)
	res, err := s.db.ExecContext(ctx, `DELETE FROM notification_deliveries WHERE ts < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---- helpers ----

// dayStart / dayEnd widen a DATE-ONLY delivery-log bound to the UTC day it names.
//
// ts is stored as RFC 3339 with nanoseconds and the filters compare it as a
// STRING, so a bare "2026-08-16" sorts before every timestamp on that day. As a
// lower bound that happens to be right; as an upper bound it is the opposite of
// what was asked for — `to=2026-08-16` excluded the whole of the 16th, which is
// the one day the caller named. Both are normalised here so the two bounds mean
// the same thing, and a value that already carries a time passes through
// untouched (`to=2026-08-16T12:00:00Z` still means noon).
func dayStart(v string) string {
	if isDateOnly(v) {
		return v + "T00:00:00Z"
	}
	return v
}

func dayEnd(v string) string {
	if isDateOnly(v) {
		// The largest timestamp the stored format can produce on that day.
		return v + "T23:59:59.999999999Z"
	}
	return v
}

func isDateOnly(v string) bool {
	_, err := time.Parse("2006-01-02", v)
	return err == nil
}

type scanner interface{ Scan(dest ...any) error }

func parseTS(s string) time.Time {
	t, err := time.Parse(tsFormat, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullable(ns sql.NullString) *string {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	v := ns.String
	return &v
}

// truncate clips a delivery error to max RUNES, never bytes. Push services
// answer in their own wording, so slicing at a byte index can cut a multi-byte
// rune in half and leave invalid UTF-8 in the log — which the Doručení tab then
// renders as replacement characters, since encoding/json substitutes those for
// bytes it cannot decode. Same rule, same reason as push.truncateRunes.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}
