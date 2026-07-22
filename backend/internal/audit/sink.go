package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/reqctx"
)

// sqliteSink is the v1 in-process writer to the home DB's audit tables.
type sqliteSink struct{}

// NewSink returns the in-process SQLite audit sink.
func NewSink() Sink { return &sqliteSink{} }

// Record inserts one audit_events row (and one audit_changes row per change)
// using tx. Actor and request metadata are taken from ctx, never from e.
//
// Enum values (level, actor_type) are enforced by the DB CHECK constraints
// rather than duplicated here: a bad value fails the insert, which fails the
// whole transaction — exactly the atomicity guarantee we want, and a
// programming-error path that surfaces loudly instead of logging garbage.
func (s *sqliteSink) Record(ctx context.Context, tx *sql.Tx, e Event) (string, error) {
	if e.Module == "" || e.Action == "" || e.Summary == "" {
		return "", fmt.Errorf("audit: module, action and summary are required (got module=%q action=%q)", e.Module, e.Action)
	}

	actor, _ := reqctx.ActorFrom(ctx)
	req, _ := reqctx.RequestFrom(ctx)

	actorType := actor.Type
	if actorType == "" {
		actorType = "system" // no authenticated actor ⇒ a system-initiated action
	}
	level := e.Level
	if level == "" {
		level = LevelInfo
	}
	site := req.Site
	if site == "" {
		site = "home"
	}

	var metaJSON any
	if len(e.Meta) > 0 {
		b, err := json.Marshal(e.Meta)
		if err != nil {
			return "", fmt.Errorf("audit: marshal meta: %w", err)
		}
		metaJSON = string(b)
	}

	id := idgen.New()
	ts := time.Now().UTC().Format(TSLayout)

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_events
		   (id, ts, actor_user_id, actor_type, actor_label, module, action,
		    entity_type, entity_id, summary, level, request_id, ip, user_agent, site, meta)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, ts, ns(actor.UserID), actorType, ns(actor.Label), e.Module, e.Action,
		ns(e.EntityType), ns(e.EntityID), e.Summary, level,
		ns(req.RequestID), ns(req.IP), ns(req.UserAgent), site, metaJSON,
	); err != nil {
		return "", fmt.Errorf("audit: insert event: %w", err)
	}

	for _, c := range e.Changes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO audit_changes (id, event_id, field, old_value, new_value)
			 VALUES (?,?,?,?,?)`,
			idgen.New(), id, c.Field, c.Old, c.New,
		); err != nil {
			return "", fmt.Errorf("audit: insert change %q: %w", c.Field, err)
		}
	}

	return id, nil
}

// ns maps an empty string to a SQL NULL, otherwise the string itself.
func ns(s string) any {
	if s == "" {
		return nil
	}
	return s
}
