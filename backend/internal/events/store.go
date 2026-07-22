package events

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/lexorank"
)

// DBTX is satisfied by *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Store holds the DB handle for reads; mutations take an explicit tx.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const defaultTimezone = "Europe/Prague"

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }

func ptr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ---- Events ----

const eventCols = `id, title, description, starts_on, rrule, timezone, reminder_enabled,
	reminder_lead, archived, created_by, created_at, updated_at`

func scanEvent(r interface{ Scan(...any) error }) (Event, error) {
	var e Event
	var desc, rrule, lead, createdBy sql.NullString
	var reminder, archived int
	if err := r.Scan(&e.ID, &e.Title, &desc, &e.StartsOn, &rrule, &e.Timezone,
		&reminder, &lead, &archived, &createdBy, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return Event{}, err
	}
	e.Description = ptr(desc)
	e.RRule = ptr(rrule)
	e.ReminderLead = ptr(lead)
	e.CreatedBy = ptr(createdBy)
	e.ReminderEnabled = reminder != 0
	e.Archived = archived != 0
	return e, nil
}

func (s *Store) GetEvent(ctx context.Context, q DBTX, id string) (*Event, error) {
	row := q.QueryRowContext(ctx, `SELECT `+eventCols+` FROM events WHERE id = ?`, id)
	e, err := scanEvent(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) GetEventWithLinks(ctx context.Context, id string) (*EventWithLinks, error) {
	e, err := s.GetEvent(ctx, s.db, id)
	if err != nil || e == nil {
		return nil, err
	}
	links, err := s.ListEventLinks(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	return &EventWithLinks{Event: *e, Links: links}, nil
}

// ListSeries returns stored series records, newest-first, keyset-paginated by id.
func (s *Store) ListSeries(ctx context.Context, includeArchived bool, limit int, cursor string) ([]Event, *string, error) {
	var conds []string
	var args []any
	if !includeArchived {
		conds = append(conds, "archived = 0")
	}
	if cursor != "" {
		conds = append(conds, "id < ?")
		args = append(args, cursor)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `SELECT `+eventCols+` FROM events`+where+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, e)
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

// ListForWindow returns candidate events for occurrence expansion (all
// non-archived, or all when includeArchived). One query, expand in memory.
func (s *Store) ListForWindow(ctx context.Context, includeArchived bool) ([]Event, error) {
	where := ""
	if !includeArchived {
		where = " WHERE archived = 0"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+eventCols+` FROM events`+where+` ORDER BY starts_on`, )
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) InsertEvent(ctx context.Context, tx DBTX, e Event, createdBy string) (*Event, error) {
	id := idgen.New()
	now := nowUTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events (id, title, description, starts_on, rrule, timezone, reminder_enabled,
			reminder_lead, created_by, created_at, updated_at, archived)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,0)`,
		id, e.Title, nullable(deref(e.Description)), e.StartsOn, nullable(deref(e.RRule)), defaultTimezone,
		boolInt(e.ReminderEnabled), nullable(deref(e.ReminderLead)), nullable(createdBy), now, now,
	); err != nil {
		return nil, err
	}
	return s.GetEvent(ctx, tx, id)
}

func (s *Store) UpdateEvent(ctx context.Context, tx DBTX, id string, u EventUpdate) error {
	sets := []string{"updated_at = ?"}
	args := []any{nowUTC()}
	if u.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *u.Title)
	}
	if u.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, nullable(*u.Description))
	}
	if u.StartsOn != nil {
		sets = append(sets, "starts_on = ?")
		args = append(args, *u.StartsOn)
	}
	if u.RRule != nil {
		sets = append(sets, "rrule = ?")
		args = append(args, nullable(*u.RRule))
	}
	if u.ReminderEnabled != nil {
		sets = append(sets, "reminder_enabled = ?")
		args = append(args, boolInt(*u.ReminderEnabled))
	}
	if u.ReminderLead != nil {
		sets = append(sets, "reminder_lead = ?")
		args = append(args, nullable(*u.ReminderLead))
	}
	if u.Archived != nil {
		sets = append(sets, "archived = ?")
		args = append(args, boolInt(*u.Archived))
	}
	args = append(args, id)
	_, err := tx.ExecContext(ctx, `UPDATE events SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

func (s *Store) DeleteEvent(ctx context.Context, tx DBTX, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM events WHERE id = ?`, id)
	return err
}

// ---- Event links ----

func (s *Store) ListEventLinks(ctx context.Context, q DBTX, eventID string) ([]EventLink, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, event_id, url, title, position FROM event_links WHERE event_id = ? ORDER BY position, id`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EventLink{}
	for rows.Next() {
		var l EventLink
		var title sql.NullString
		if err := rows.Scan(&l.ID, &l.EventID, &l.URL, &title, &l.Position); err != nil {
			return nil, err
		}
		l.Title = ptr(title)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) lastEventLinkPosition(ctx context.Context, tx DBTX, eventID string) (string, error) {
	var pos sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT MAX(position) FROM event_links WHERE event_id = ?`, eventID).Scan(&pos); err != nil {
		return "", err
	}
	if !pos.Valid {
		return lexorank.First(), nil
	}
	return lexorank.Tail(pos.String), nil
}

func (s *Store) InsertEventLink(ctx context.Context, tx DBTX, eventID, url, title, position string) (*EventLink, error) {
	id := idgen.New()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO event_links (id, event_id, url, title, position) VALUES (?,?,?,?,?)`,
		id, eventID, url, nullable(title), position); err != nil {
		return nil, err
	}
	row := tx.QueryRowContext(ctx, `SELECT id, event_id, url, title, position FROM event_links WHERE id = ?`, id)
	var l EventLink
	var t sql.NullString
	if err := row.Scan(&l.ID, &l.EventID, &l.URL, &t, &l.Position); err != nil {
		return nil, err
	}
	l.Title = ptr(t)
	return &l, nil
}

func (s *Store) EventLinkEventID(ctx context.Context, q DBTX, linkID string) (string, error) {
	var eventID string
	err := q.QueryRowContext(ctx, `SELECT event_id FROM event_links WHERE id = ?`, linkID).Scan(&eventID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return eventID, err
}

func (s *Store) DeleteEventLink(ctx context.Context, tx DBTX, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM event_links WHERE id = ?`, id)
	return err
}

// ---- Reminder completions (the only per-occurrence rows) ----

// InsertCompletion records a completion idempotently (unique (event_id, occurrence_on)).
// Reports whether a new row was inserted.
func (s *Store) InsertCompletion(ctx context.Context, tx DBTX, eventID, occurrenceOn, completedBy string) (bool, error) {
	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO event_reminder_completions (id, event_id, occurrence_on, completed_by, completed_at)
		 VALUES (?,?,?,?,?)`,
		idgen.New(), eventID, occurrenceOn, nullable(completedBy), nowUTC())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) GetCompletion(ctx context.Context, q DBTX, eventID, occurrenceOn string) (*ReminderCompletion, error) {
	var c ReminderCompletion
	var by sql.NullString
	err := q.QueryRowContext(ctx,
		`SELECT event_id, occurrence_on, completed_by, completed_at FROM event_reminder_completions
		 WHERE event_id = ? AND occurrence_on = ?`, eventID, occurrenceOn).
		Scan(&c.EventID, &c.OccurrenceOn, &by, &c.CompletedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.CompletedBy = ptr(by)
	return &c, nil
}

func (s *Store) DeleteCompletion(ctx context.Context, tx DBTX, eventID, occurrenceOn string) error {
	_, err := tx.ExecContext(ctx,
		`DELETE FROM event_reminder_completions WHERE event_id = ? AND occurrence_on = ?`, eventID, occurrenceOn)
	return err
}

// CompletionsFor loads all completions for the given events, as
// event_id -> set of occurrence_on.
func (s *Store) CompletionsFor(ctx context.Context, eventIDs []string) (map[string]map[string]bool, error) {
	out := map[string]map[string]bool{}
	if len(eventIDs) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(eventIDs)), ",")
	args := make([]any, len(eventIDs))
	for i, id := range eventIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT event_id, occurrence_on FROM event_reminder_completions WHERE event_id IN (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var eventID, occ string
		if err := rows.Scan(&eventID, &occ); err != nil {
			return nil, err
		}
		if out[eventID] == nil {
			out[eventID] = map[string]bool{}
		}
		out[eventID][occ] = true
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
