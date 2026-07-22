package audit

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Store is the read side of the audit spine (the log browser, FR-L3–L6). It is
// query-only; writes go through Sink.
type Store struct{ db *sql.DB }

// NewStore returns a read store over db.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// ---- Wire types (match openapi.yaml schemas) ----

type AuditEvent struct {
	ID          string          `json:"id"`
	TS          string          `json:"ts"`
	ActorUserID *string         `json:"actor_user_id"`
	ActorType   string          `json:"actor_type"`
	ActorLabel  *string         `json:"actor_label"`
	Module      string          `json:"module"`
	Action      string          `json:"action"`
	EntityType  *string         `json:"entity_type"`
	EntityID    *string         `json:"entity_id"`
	Summary     string          `json:"summary"`
	Level       string          `json:"level"`
	RequestID   *string         `json:"request_id"`
	IP          *string         `json:"ip"`
	UserAgent   *string         `json:"user_agent"`
	Site        string          `json:"site"`
	Meta        json.RawMessage `json:"meta"`
	ChangeCount int             `json:"change_count"`
}

type AuditChange struct {
	Field string  `json:"field"`
	Old   *string `json:"old_value"`
	New   *string `json:"new_value"`
}

type AuditEventDetail struct {
	AuditEvent
	Changes []AuditChange `json:"changes"`
}

type EventPage struct {
	Items      []AuditEvent `json:"items"`
	NextCursor *string      `json:"next_cursor"`
}

type DetailPage struct {
	Items      []AuditEventDetail `json:"items"`
	NextCursor *string            `json:"next_cursor"`
}

// Filter is the composed (AND) filter set for Browse (FR-L3).
type Filter struct {
	From, To   string // RFC3339 (any precision); normalised internally
	Module     string
	Actor      string
	Action     string
	EntityType string
	EntityID   string
	Level      string
	Q          string
	Limit      int
	Cursor     string
}

const (
	defaultLimit = 50
	maxLimit     = 200
)

const eventCols = `e.id, e.ts, e.actor_user_id, e.actor_type, e.actor_label, e.module, e.action,
	e.entity_type, e.entity_id, e.summary, e.level, e.request_id, e.ip, e.user_agent, e.site, e.meta,
	(SELECT COUNT(*) FROM audit_changes c WHERE c.event_id = e.id) AS change_count`

// Browse returns a newest-first page of events matching filter (FR-L3).
func (s *Store) Browse(ctx context.Context, f Filter) (EventPage, error) {
	limit := clampLimit(f.Limit)

	conds, args, err := commonConds(f)
	if err != nil {
		return EventPage{}, err
	}
	if f.Cursor != "" {
		ts, id, err := decodeCursor(f.Cursor)
		if err != nil {
			return EventPage{}, errInvalid("cursor", err)
		}
		conds = append(conds, "(e.ts < ? OR (e.ts = ? AND e.id < ?))")
		args = append(args, ts, ts, id)
	}

	query := "SELECT " + eventCols + " FROM audit_events e" + whereClause(conds) +
		" ORDER BY e.ts DESC, e.id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return EventPage{}, err
	}
	defer rows.Close()

	items, err := scanEvents(rows)
	if err != nil {
		return EventPage{}, err
	}

	page := EventPage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		page.Items = items[:limit]
		cur := encodeCursor(last.TS, last.ID)
		page.NextCursor = &cur
	}
	return page, nil
}

// Get returns one event with its full field changes (FR-L4). Returns
// (nil, nil) when the event does not exist.
func (s *Store) Get(ctx context.Context, id string) (*AuditEventDetail, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+eventCols+" FROM audit_events e WHERE e.id = ?", id)
	ev, err := scanEvent(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	changes, err := s.changesFor(ctx, []string{ev.ID})
	if err != nil {
		return nil, err
	}
	return &AuditEventDetail{AuditEvent: ev, Changes: changes[ev.ID]}, nil
}

// Timeline returns the chronological (oldest-first) history of one entity, each
// event with its changes (FR-L5). An unknown entity yields an empty page.
func (s *Store) Timeline(ctx context.Context, entityType, entityID, from, to string, limit int, cursor string) (DetailPage, error) {
	limit = clampLimit(limit)
	conds := []string{"e.entity_type = ?", "e.entity_id = ?"}
	args := []any{entityType, entityID}

	if from != "" {
		v, err := normaliseTS(from)
		if err != nil {
			return DetailPage{}, errInvalid("from", err)
		}
		conds = append(conds, "e.ts >= ?")
		args = append(args, v)
	}
	if to != "" {
		v, err := normaliseTS(to)
		if err != nil {
			return DetailPage{}, errInvalid("to", err)
		}
		conds = append(conds, "e.ts <= ?")
		args = append(args, v)
	}
	if cursor != "" {
		ts, id, err := decodeCursor(cursor)
		if err != nil {
			return DetailPage{}, errInvalid("cursor", err)
		}
		conds = append(conds, "(e.ts > ? OR (e.ts = ? AND e.id > ?))")
		args = append(args, ts, ts, id)
	}

	query := "SELECT " + eventCols + " FROM audit_events e" + whereClause(conds) +
		" ORDER BY e.ts ASC, e.id ASC LIMIT ?"
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return DetailPage{}, err
	}
	defer rows.Close()
	events, err := scanEvents(rows)
	if err != nil {
		return DetailPage{}, err
	}

	page := DetailPage{}
	var nextCursor *string
	if len(events) > limit {
		last := events[limit-1]
		events = events[:limit]
		cur := encodeCursor(last.TS, last.ID)
		nextCursor = &cur
	}

	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}
	changes, err := s.changesFor(ctx, ids)
	if err != nil {
		return DetailPage{}, err
	}
	page.Items = make([]AuditEventDetail, len(events))
	for i, e := range events {
		page.Items[i] = AuditEventDetail{AuditEvent: e, Changes: changes[e.ID]}
	}
	page.NextCursor = nextCursor
	return page, nil
}

// ---- Stats (FR-L6) ----

type StatBucket struct {
	TS     string         `json:"ts"`
	Counts map[string]int `json:"counts"`
}

type StatTotal struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type StatsResult struct {
	Dimension string       `json:"dimension"`
	Bucket    string       `json:"bucket"`
	Buckets   []StatBucket `json:"buckets"`
	Totals    []StatTotal  `json:"totals"`
}

const statsTopN = 10

// Stats returns grouped counts over time plus top-N totals (FR-L6). dimension is
// one of module|actor|action|level; bucket is day|week.
func (s *Store) Stats(ctx context.Context, dimension, bucket, from, to string) (StatsResult, error) {
	keyExpr, ok := dimensionExpr(dimension)
	if !ok {
		return StatsResult{}, errInvalid("dimension", fmt.Errorf("must be module|actor|action|level"))
	}
	if bucket != "day" && bucket != "week" {
		return StatsResult{}, errInvalid("bucket", fmt.Errorf("must be day|week"))
	}

	conds := []string{}
	args := []any{}
	if from != "" {
		v, err := normaliseTS(from)
		if err != nil {
			return StatsResult{}, errInvalid("from", err)
		}
		conds = append(conds, "ts >= ?")
		args = append(args, v)
	}
	if to != "" {
		v, err := normaliseTS(to)
		if err != nil {
			return StatsResult{}, errInvalid("to", err)
		}
		conds = append(conds, "ts <= ?")
		args = append(args, v)
	}

	query := "SELECT ts, " + keyExpr + " AS k FROM audit_events" + whereClause(conds)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return StatsResult{}, err
	}
	defer rows.Close()

	bucketCounts := map[string]map[string]int{}
	totals := map[string]int{}
	for rows.Next() {
		var ts, key string
		if err := rows.Scan(&ts, &key); err != nil {
			return StatsResult{}, err
		}
		bkey := bucketKey(ts, bucket)
		if bucketCounts[bkey] == nil {
			bucketCounts[bkey] = map[string]int{}
		}
		bucketCounts[bkey][key]++
		totals[key]++
	}
	if err := rows.Err(); err != nil {
		return StatsResult{}, err
	}

	res := StatsResult{Dimension: dimension, Bucket: bucket}
	for bkey, counts := range bucketCounts {
		res.Buckets = append(res.Buckets, StatBucket{TS: bkey, Counts: counts})
	}
	sort.Slice(res.Buckets, func(i, j int) bool { return res.Buckets[i].TS < res.Buckets[j].TS })

	for k, c := range totals {
		res.Totals = append(res.Totals, StatTotal{Key: k, Count: c})
	}
	sort.Slice(res.Totals, func(i, j int) bool {
		if res.Totals[i].Count != res.Totals[j].Count {
			return res.Totals[i].Count > res.Totals[j].Count
		}
		return res.Totals[i].Key < res.Totals[j].Key
	})
	if len(res.Totals) > statsTopN {
		res.Totals = res.Totals[:statsTopN]
	}
	return res, nil
}

// ---- helpers ----

// InvalidError signals a bad filter/parameter (maps to HTTP 422).
type InvalidError struct {
	Param string
	Err   error
}

func (e *InvalidError) Error() string { return fmt.Sprintf("invalid %s: %v", e.Param, e.Err) }

func errInvalid(param string, err error) error { return &InvalidError{Param: param, Err: err} }

func clampLimit(n int) int {
	if n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}

func dimensionExpr(dim string) (string, bool) {
	switch dim {
	case "module":
		return "module", true
	case "action":
		return "action", true
	case "level":
		return "level", true
	case "actor":
		return "COALESCE(actor_user_id, actor_label, actor_type)", true
	default:
		return "", false
	}
}

func commonConds(f Filter) ([]string, []any, error) {
	var conds []string
	var args []any
	add := func(col, val string) {
		if val != "" {
			conds = append(conds, "e."+col+" = ?")
			args = append(args, val)
		}
	}
	if f.From != "" {
		v, err := normaliseTS(f.From)
		if err != nil {
			return nil, nil, errInvalid("from", err)
		}
		conds = append(conds, "e.ts >= ?")
		args = append(args, v)
	}
	if f.To != "" {
		v, err := normaliseTS(f.To)
		if err != nil {
			return nil, nil, errInvalid("to", err)
		}
		conds = append(conds, "e.ts <= ?")
		args = append(args, v)
	}
	add("module", f.Module)
	add("actor_user_id", f.Actor)
	add("action", f.Action)
	add("entity_type", f.EntityType)
	add("entity_id", f.EntityID)
	add("level", f.Level)
	if f.Q != "" {
		conds = append(conds, "e.rowid IN (SELECT rowid FROM audit_events_fts WHERE audit_events_fts MATCH ?)")
		args = append(args, ftsQuery(f.Q))
	}
	return conds, args, nil
}

func whereClause(conds []string) string {
	if len(conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(conds, " AND ")
}

// ftsQuery turns free text into a safe FTS5 MATCH expression: each whitespace
// token becomes a quoted term (implicit AND), with embedded quotes doubled.
func ftsQuery(q string) string {
	var parts []string
	for _, tok := range strings.Fields(q) {
		parts = append(parts, `"`+strings.ReplaceAll(tok, `"`, `""`)+`"`)
	}
	return strings.Join(parts, " ")
}

// normaliseTS parses an RFC3339 timestamp of any precision and reformats it to
// the fixed-width layout so string comparisons against audit_events.ts are correct.
func normaliseTS(s string) (string, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// Also accept the fixed layout itself.
		t, err = time.Parse(TSLayout, s)
		if err != nil {
			return "", err
		}
	}
	return t.UTC().Format(TSLayout), nil
}

func bucketKey(ts, bucket string) string {
	t, err := time.Parse(TSLayout, ts)
	if err != nil {
		// Fall back to a date prefix if parsing fails.
		if len(ts) >= 10 {
			return ts[:10] + "T00:00:00Z"
		}
		return ts
	}
	t = t.UTC()
	if bucket == "week" {
		// Truncate to Monday 00:00:00 UTC.
		weekday := (int(t.Weekday()) + 6) % 7 // Monday=0
		t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -weekday)
	} else {
		t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	return t.Format("2006-01-02T15:04:05Z")
}

func encodeCursor(ts, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(ts + "\x00" + id))
}

func decodeCursor(cur string) (ts, id string, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(cur)
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(string(raw), "\x00", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("malformed cursor")
	}
	return parts[0], parts[1], nil
}

// changesFor loads all changes for the given event ids, grouped by event id.
func (s *Store) changesFor(ctx context.Context, ids []string) (map[string][]AuditChange, error) {
	out := map[string][]AuditChange{}
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT event_id, field, old_value, new_value FROM audit_changes WHERE event_id IN ("+placeholders+") ORDER BY rowid",
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var eventID, field string
		var oldV, newV sql.NullString
		if err := rows.Scan(&eventID, &field, &oldV, &newV); err != nil {
			return nil, err
		}
		out[eventID] = append(out[eventID], AuditChange{
			Field: field,
			Old:   nsToPtr(oldV),
			New:   nsToPtr(newV),
		})
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanEvent(row scannable) (AuditEvent, error) {
	var (
		e                                             AuditEvent
		actorUserID, actorLabel, entityType, entityID sql.NullString
		requestID, ip, userAgent, meta                sql.NullString
	)
	if err := row.Scan(
		&e.ID, &e.TS, &actorUserID, &e.ActorType, &actorLabel, &e.Module, &e.Action,
		&entityType, &entityID, &e.Summary, &e.Level, &requestID, &ip, &userAgent, &e.Site, &meta,
		&e.ChangeCount,
	); err != nil {
		return AuditEvent{}, err
	}
	e.ActorUserID = nsToPtr(actorUserID)
	e.ActorLabel = nsToPtr(actorLabel)
	e.EntityType = nsToPtr(entityType)
	e.EntityID = nsToPtr(entityID)
	e.RequestID = nsToPtr(requestID)
	e.IP = nsToPtr(ip)
	e.UserAgent = nsToPtr(userAgent)
	if meta.Valid && meta.String != "" {
		e.Meta = json.RawMessage(meta.String)
	}
	return e, nil
}

func scanEvents(rows *sql.Rows) ([]AuditEvent, error) {
	var out []AuditEvent
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func nsToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}
