package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
)

// The activity digest (v11, PRD §V11-4 FR-M4 `home_activity`, D298).
//
// ⚠ IT LIVES HERE AND NOT IN THE MCP HOST, deliberately: this package owns what
// `visibility` means, what the redaction phrases are, and the SQL predicate that
// separates the browsing rule from the matching rule. A second reader of
// `audit_events` written outside it would be a second privacy implementation, and
// two implementations of a privacy rule is one implementation and one bug (see
// the package doc). `modules/logging` is the OTHER reader — it renders the Log
// page — and it goes through the same Redact* pair.
//
// ⚠ THIS IS THE v9 TRIGGER-LISTENER GAP ARRIVING IN A NEW CONSUMER. v9's finding
// was that the redaction code was correct and NOTHING TESTED IT on its second
// consumer; v11 adds a third and writes the test with it.

// ActivityFilter selects a window of the audit spine.
//
// ⚠ A NEW FIELD MUST ANSWER THE BROWSING/MATCHING QUESTION (D188/D209) before it
// joins the WHERE clause — see selectsOnContent below. A field that selects on
// what a private event CONTAINS (its text, its entity, its diffs) and silently
// takes the browsing rule leaks confirmation of private content: the hit itself
// is the answer, redacted or not.
type ActivityFilter struct {
	Since      time.Time
	Until      time.Time
	Module     string
	Action     string
	Actor      string // actor_user_id
	EntityType string
	EntityID   string
	Limit      int
}

// selectsOnContent reports whether this filter selects on a private event's
// CONTENT, in which case foreign private rows must be EXCLUDED rather than
// redacted — an exact id match confirms the id exists even when every returned
// row is redacted, and v9's purge screen hands admins those ids by design (D198).
//
// The dimension filters — module, action, actor, entity_type, time — stay under
// the browsing rule, which the household is meant to have.
func (f ActivityFilter) selectsOnContent() bool { return f.EntityID != "" }

// ⚠ THE DIGEST CARRIES NO FIELD DIFFS AND NO CHANGE COUNT, and Entry has nowhere
// to put either. The diffs are what redaction removes — template tokens select
// them by SHAPE rather than by field name (D207), so no enumeration of fields
// makes them safe — and a change count beside a redacted summary is both
// self-contradictory and a residual disclosure about how much of a private item
// an edit touched. The Log page reaches the same conclusion by hand in
// `logging/query.go`, where the wire type does have somewhere to put them.

// ActivityReader reads the audit spine for a digest consumer.
type ActivityReader struct{ db *sql.DB }

// NewActivityReader returns a reader over db.
func NewActivityReader(db *sql.DB) *ActivityReader { return &ActivityReader{db: db} }

const activityCols = `e.id, e.ts, e.actor_user_id, e.actor_type, e.actor_label, e.module, e.action,
	e.entity_type, e.entity_id, e.summary, e.level, e.request_id, e.meta, e.via, e.via_token_id`

// Activity returns the newest-first window matching f, AS viewerUserID IS
// ALLOWED TO SEE IT.
//
// ⚠ EVERY ROW GOES THROUGH RedactRendered, NEVER THROUGH Redact ALONE. Redact
// cannot reach the field diffs, so a consumer that called it and then returned
// changes would have redacted nothing that matters — this reader loads no diffs
// at all, and routing through the pair is what keeps that true if one is ever
// added. The acceptance criterion is asserted against a SECOND MEMBER's private
// item, not against a fixture the caller owns.
func (r *ActivityReader) Activity(ctx context.Context, f ActivityFilter, viewerUserID string) ([]Entry, error) {
	var conds []string
	var args []any
	add := func(col, val string) {
		if val != "" {
			conds = append(conds, "e."+col+" = ?")
			args = append(args, val)
		}
	}
	if !f.Since.IsZero() {
		conds = append(conds, "e.ts >= ?")
		args = append(args, f.Since.UTC().Format(TSLayout))
	}
	if !f.Until.IsZero() {
		conds = append(conds, "e.ts <= ?")
		args = append(args, f.Until.UTC().Format(TSLayout))
	}
	add("module", f.Module)
	add("action", f.Action)
	add("actor_user_id", f.Actor)
	add("entity_type", f.EntityType)
	add("entity_id", f.EntityID)
	if f.selectsOnContent() {
		visSQL, visArgs := VisibleEventsSQL(viewerUserID)
		conds = append(conds, visSQL)
		args = append(args, visArgs...)
	}

	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx,
		"SELECT "+activityCols+" FROM audit_events e"+where+" ORDER BY e.ts DESC, e.id DESC LIMIT ?", args...)
	if err != nil {
		return nil, fmt.Errorf("audit: activity: %w", err)
	}
	out, err := appdb.Collect(rows, scanActivity)
	if err != nil {
		return nil, fmt.Errorf("audit: activity: %w", err)
	}
	for i := range out {
		out[i], _ = RedactRendered(out[i], nil, viewerUserID)
	}
	return out, nil
}

func scanActivity(row appdb.Scanner) (Entry, error) {
	var (
		a                                           Entry
		ts                                          string
		actorUser, actorLabel, entityType, entityID sql.NullString
		level, requestID, meta, via, viaToken       sql.NullString
	)
	if err := row.Scan(&a.ID, &ts, &actorUser, &a.ActorType, &actorLabel, &a.Module, &a.Action,
		&entityType, &entityID, &a.Summary, &level, &requestID, &meta, &via, &viaToken); err != nil {
		return Entry{}, err
	}
	a.TS, _ = time.Parse(TSLayout, ts)
	a.ActorUser, a.ActorLabel = actorUser.String, actorLabel.String
	a.EntityType, a.EntityID = entityType.String, entityID.String
	a.Level, a.RequestID = level.String, requestID.String
	a.Via, a.ViaTokenID = via.String, viaToken.String
	if meta.Valid && meta.String != "" {
		_ = json.Unmarshal([]byte(meta.String), &a.Meta)
	}
	return a, nil
}
