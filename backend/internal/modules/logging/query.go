package logging

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
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
	// Redacted is true when this event concerns a PRIVATE note or document and the
	// caller is not its owner (v9, D187). The row is still returned — the spine
	// records everything — but `summary` carries the fixed Czech phrase,
	// `entity_id` is dropped and `changes` comes back empty.
	//
	// It is REQUIRED on the wire so a client can tell a redacted row from a row
	// about something dull. Without it the browser renders the phrase as though it
	// were a summary somebody actually wrote.
	Redacted    bool `json:"redacted"`
	ChangeCount int  `json:"change_count"`

	// visibility/ownerID are read out of `meta` by the SQL rather than by parsing
	// the JSON per row, purely to decide redaction. Unexported: they are an
	// implementation detail of the read path and never part of the response.
	visibility string
	ownerID    string
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
//
// ⚠ A NEW FIELD MUST ANSWER THE BROWSING/MATCHING QUESTION (D188/D209) — in
// selectsOnContent, right below — before it joins the WHERE clause. A field that
// selects on what a private event CONTAINS (its text, its entity, its diffs) and
// silently takes the browsing rule leaks confirmation of private content: the
// hit itself is the answer, redacted or not.
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

// selectsOnContent reports whether this filter SELECTS on a private event's
// content — the MATCHING rule (D188/D209): such a query must EXCLUDE foreign
// private rows rather than redact them, because whether a row comes back at all
// is itself the answer. It lives beside the struct so a new field cannot be
// added without meeting the question; dimension filters (module, actor, action,
// entity_type, level, time) stay under the browsing rule, which the household
// is meant to have.
func (f Filter) selectsOnContent() bool {
	return f.Q != "" || f.EntityID != ""
}

const (
	defaultLimit = 50
	maxLimit     = 200
)

const eventCols = `e.id, e.ts, e.actor_user_id, e.actor_type, e.actor_label, e.module, e.action,
	e.entity_type, e.entity_id, e.summary, e.level, e.request_id, e.ip, e.user_agent, e.site, e.meta,
	(SELECT COUNT(*) FROM audit_changes c WHERE c.event_id = e.id) AS change_count,
	json_extract(e.meta, '$.visibility'), json_extract(e.meta, '$.owner_id')`

// Redaction of private-item events (v9, PRD D187/D188/D209).
//
// v9 gives notes and documents a second, PRIVATE root per member. Those mutations
// are audited in FULL and hidden on the way out, by exactly one function —
// audit.Redact — which every path below routes through. Two implementations of a
// privacy rule is one implementation and one bug.
//
// ⚠ THERE ARE TWO RULES, NOT ONE (D188), and they apply to FOUR doors, not two
// (D209):
//
//	BROWSING  (GET /api/logs, GET /api/logs/{id}, the entity timeline) REDACTS:
//	          the row comes back with the fixed phrase, no entity_id, no changes.
//	          The household may learn that something private happened; that is not
//	          the secret.
//
//	MATCHING  (?q=, ?entity_id=, and /stats) EXCLUDES. Redacting a hit is not
//	          enough — the hit ITSELF tells the searcher that their term occurs in
//	          a private title, which is precisely the protected thing. The
//	          entity_id filter is the stronger case: an exact match confirms an id
//	          exists even when every returned row is redacted, and the purge screen
//	          hands admins ids by design (D198). /stats would otherwise count
//	          private events into admin-visible dimension and bucket totals.

// visibleEventsCond excludes private events belonging to someone else. It is the
// MATCHING rule; the browsing rule is applied in Go by redactEvent.
//
// The SQL itself comes from the platform (audit.VisibleEventsSQL), NOT a local
// spelling: the platform owns the meta keys and the 'private' value, and a
// second copy of the predicate here is exactly the drift D187 guards against —
// the Go rule would evolve and this one would keep matching the old shape.
func visibleEventsCond(viewerID string) (string, []any) {
	return audit.VisibleEventsSQL(viewerID)
}

// redactEvent applies the BROWSING rule to one row, through audit.Redact.
//
// It round-trips the wire type through audit.Entry deliberately rather than
// re-implementing the decision here: the platform owns what "private" means and
// what the phrase is, and a second copy of that logic in the log module is exactly
// the drift D187 guards against.
func redactEvent(e AuditEvent, viewerID string) AuditEvent {
	if e.visibility != audit.VisibilityPrivate {
		return e
	}
	entry := audit.Entry{
		EntityType: derefStr(e.EntityType),
		EntityID:   derefStr(e.EntityID),
		Summary:    e.Summary,
		Meta: map[string]any{
			audit.MetaVisibility: e.visibility,
			audit.MetaOwnerID:    e.ownerID,
		},
	}
	red := audit.Redact(entry, viewerID)
	if !red.Redacted {
		return e
	}
	e.Summary = red.Summary
	e.EntityID = nil
	e.Redacted = true
	// ⚠ The COUNT goes with the changes. A redacted row's `changes` comes back
	// empty, so a change_count of 2 beside an empty list is both self-contradictory
	// and a residual disclosure: how many fields a private item's edit touched is
	// metadata about that item, which is exactly what redaction removes.
	e.ChangeCount = 0
	// The raw meta carried owner_id, which names the member the item belongs to.
	// That is not itself the secret, but it is not the log browser's business
	// either, and `meta` is rendered verbatim by the SPA's detail view.
	e.Meta = nil
	return e
}

func redactEvents(items []AuditEvent, viewerID string) []AuditEvent {
	for i := range items {
		items[i] = redactEvent(items[i], viewerID)
	}
	return items
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Browse returns a newest-first page of events matching filter (FR-L3).
//
// v9: private events belonging to another member come back REDACTED (leak table
// row 12) — unless the filter is one of the MATCHING kinds, in which case
// commonConds has already excluded them (rows 13–14).
func (s *Store) Browse(ctx context.Context, f Filter, viewerID string) (EventPage, error) {
	limit := clampLimit(f.Limit)

	conds, args, err := commonConds(f, viewerID)
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
	page.Items = redactEvents(page.Items, viewerID)
	return page, nil
}

// Get returns one event with its full field changes (FR-L4). Returns
// (nil, nil) when the event does not exist.
//
// v9: a redacted row loses its changes as well as its summary. Redact cannot do
// that itself — the diffs are loaded separately — so it is the caller's job, here
// and in Timeline.
func (s *Store) Get(ctx context.Context, id, viewerID string) (*AuditEventDetail, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+eventCols+" FROM audit_events e WHERE e.id = ?", id)
	ev, err := scanEvent(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ev = redactEvent(ev, viewerID); ev.Redacted {
		// `[]`, never null — D174 still holds, and a client that indexes into
		// `changes` must not have to special-case a redacted row.
		return &AuditEventDetail{AuditEvent: ev, Changes: []AuditChange{}}, nil
	}
	changes, err := s.changesFor(ctx, []string{ev.ID})
	if err != nil {
		return nil, err
	}
	return &AuditEventDetail{AuditEvent: ev, Changes: orEmptyChanges(changes[ev.ID])}, nil
}

// Timeline returns the chronological (oldest-first) history of one entity, each
// event with its changes (FR-L5). An unknown entity yields an empty page.
//
// ⚠ This is the door the first draft of the leak table missed (D209), and it is
// the worst of the four, because it is addressed BY ID and returns FULL field
// diffs — while the purge screen hands admins the ids of foreign private items by
// design (D198). Left alone, an admin could read every private title and every
// before/after value by pasting an id from one Administrace screen into another.
//
// It takes the MATCHING rule, not the browsing one: an entity_id is an exact
// match, so returning N redacted rows would still confirm the id exists. A
// non-owner asking about a private id gets an EMPTY PAGE — the same page an id
// that was never issued produces.
func (s *Store) Timeline(ctx context.Context, entityType, entityID, from, to string, limit int, cursor, viewerID string) (DetailPage, error) {
	limit = clampLimit(limit)
	visSQL, visArgs := visibleEventsCond(viewerID)
	conds := []string{"e.entity_type = ?", "e.entity_id = ?", visSQL}
	args := []any{entityType, entityID}
	args = append(args, visArgs...)

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
		page.Items[i] = AuditEventDetail{AuditEvent: e, Changes: orEmptyChanges(changes[e.ID])}
	}
	page.NextCursor = nextCursor
	return page, nil
}

// changesFor is skipped entirely for a redacted event, so a private item's
// before/after values never leave the database on a browse path. The one place
// they are loaded and then dropped is Get, where the row is fetched by id and the
// redaction decision is made after the scan.

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
//
// ⚠ v9: private events are EXCLUDED from a non-owner's totals (D209, leak table
// row 14). Otherwise a bucket count that ticks up while nothing visible changed
// says "somebody did something private just now", every day, on a chart. The
// counts are correspondingly not the same for every admin — which is the honest
// answer for a household where some activity is nobody else's business.
func (s *Store) Stats(ctx context.Context, dimension, bucket, from, to, viewerID string) (StatsResult, error) {
	keyExpr, ok := dimensionExpr(dimension)
	if !ok {
		return StatsResult{}, errInvalid("dimension", fmt.Errorf("must be module|actor|action|level"))
	}
	if bucket != "day" && bucket != "week" {
		return StatsResult{}, errInvalid("bucket", fmt.Errorf("must be day|week"))
	}

	visSQL, visArgs := visibleEventsCond(viewerID)
	conds := []string{visSQL}
	args := append([]any{}, visArgs...)
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

	// Aliased `e` because visibleEventsCond above is written against that alias —
	// the same fragment the browse and timeline queries use, so there is one
	// visibility predicate in this file rather than three that can drift.
	query := "SELECT e.ts, " + keyExpr + " AS k FROM audit_events e" + whereClause(conds)
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

	res := StatsResult{Dimension: dimension, Bucket: bucket, Buckets: []StatBucket{}, Totals: []StatTotal{}}
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

func commonConds(f Filter, viewerID string) ([]string, []any, error) {
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
	// The MATCHING rule (D188/D209). Unfiltered browsing REDACTS a private event —
	// a row appears, saying only that something private happened. But a filter that
	// SELECTS is different in kind: whether a row comes back at all is itself the
	// answer.
	//
	//	?q=      a redacted hit still tells the searcher their term occurs in a
	//	         private title, which is the thing being protected.
	//	entity_id an exact match confirms the id exists even if every row is
	//	         redacted — and it is stronger than the lexical case, because the
	//	         purge screen hands admins those ids on purpose (D198).
	//
	// Applied ONLY for content-selecting filters, deliberately. Adding it
	// unconditionally would make ordinary browsing hide private rows entirely, and
	// the household is meant to be able to see that something happened. WHICH
	// fields select on content is declared by Filter.selectsOnContent, beside the
	// struct — not by a field list here.
	if f.selectsOnContent() {
		visSQL, visArgs := visibleEventsCond(viewerID)
		conds = append(conds, visSQL)
		args = append(args, visArgs...)
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
		t, err = time.Parse(audit.TSLayout, s)
		if err != nil {
			return "", err
		}
	}
	return t.UTC().Format(audit.TSLayout), nil
}

func bucketKey(ts, bucket string) string {
	t, err := time.Parse(audit.TSLayout, ts)
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
		visibility, ownerID                           sql.NullString
	)
	if err := row.Scan(
		&e.ID, &e.TS, &actorUserID, &e.ActorType, &actorLabel, &e.Module, &e.Action,
		&entityType, &entityID, &e.Summary, &e.Level, &requestID, &ip, &userAgent, &e.Site, &meta,
		&e.ChangeCount, &visibility, &ownerID,
	); err != nil {
		return AuditEvent{}, err
	}
	e.visibility = visibility.String
	e.ownerID = ownerID.String
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
	out := []AuditEvent{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// orEmptyChanges keeps a nil slice out of the response: the API declares
// `changes` as an array, and an event with no field changes would otherwise
// serialise as null and break clients that index into it.
func orEmptyChanges(x []AuditChange) []AuditChange {
	if x == nil {
		return []AuditChange{}
	}
	return x
}

func nsToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}
