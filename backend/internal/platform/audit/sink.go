package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Writer is the in-process writer to the home DB's audit tables.
type Writer struct {
	// nudge, when set, is called after an event is written so the outbox tailer
	// can wake early instead of waiting for its next poll (v5, HANDOFF-7 §4).
	nudge atomic.Pointer[func()]
}

// NewSink returns the in-process SQLite audit sink.
func NewSink() *Writer { return &Writer{} }

// SetNudge attaches the outbox tailer's wake signal. Called once at composition.
//
// The nudge is a LATENCY optimisation only, never a correctness path: Record
// runs inside the caller's write transaction, so the signal necessarily arrives
// a moment before the commit it refers to, and the tailer's poll is what
// guarantees the event is eventually seen. Whatever fn does, it must not block —
// it would be blocking someone's database transaction.
func (w *Writer) SetNudge(fn func()) { w.nudge.Store(&fn) }

// Record inserts one audit_events row (and one audit_changes row per change)
// using tx. Actor and request metadata are taken from ctx, never from e.
//
// Enum values (level, actor_type) are enforced by the DB CHECK constraints
// rather than duplicated here: a bad value fails the insert, which fails the
// whole transaction — exactly the atomicity guarantee we want, and a
// programming-error path that surfaces loudly instead of logging garbage.
func (w *Writer) Record(ctx context.Context, tx *sql.Tx, e Event) (string, error) {
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

	// The typed redaction marker becomes the meta keys every read path keys off
	// (redact.go). Stamped HERE, not in each module, so the platform owns the
	// spelling — see the Event field comment.
	//
	// ⚠ INTO A FRESH MAP, never into the caller's. Event is passed by value but its
	// Meta is a shared reference, so stamping in place writes the marker into a map
	// the caller still holds — the discipline Redact states one file away ("A fresh
	// map, not a delete on the old one: Entry is a value but its map is shared with
	// the caller's copy"). A caller that records two events from one base map (a
	// cascade loop hoisting `m := map[string]any{"via":"cascade"}`, or any helper
	// returning a reused map) would get the FIRST event's visibility and owner_id
	// stamped permanently into it, so the second — a shared item — is written with
	// the private marker and then redacted for the whole household. The copy costs
	// a handful of keys on a path that is about to marshal them to JSON anyway.
	if e.Visibility != "" {
		meta := make(map[string]any, len(e.Meta)+2)
		for k, v := range e.Meta {
			meta[k] = v
		}
		meta[MetaVisibility] = e.Visibility
		if e.OwnerID != "" {
			meta[MetaOwnerID] = e.OwnerID
		}
		e.Meta = meta
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

	if fn := w.nudge.Load(); fn != nil {
		(*fn)()
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
