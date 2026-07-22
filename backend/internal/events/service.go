package events

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/dates"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/recur"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/reqctx"
)

// Notifier publishes a websocket change after commit.
type Notifier func(typ string, payload any)

// Service orchestrates events mutations (WithTx + audit-in-tx + notify) and the
// read-time occurrence expansion.
type Service struct {
	db              *sql.DB
	store           *Store
	sink            audit.Sink
	notify          Notifier
	maxOccurrences  int
	maxWindowMonths int
}

// NewService builds an events service. maxOccurrences caps per-event expansion;
// maxWindowMonths bounds the occurrences window span.
func NewService(db *sql.DB, sink audit.Sink, notify Notifier, maxOccurrences, maxWindowMonths int) *Service {
	if notify == nil {
		notify = func(string, any) {}
	}
	return &Service{db: db, store: NewStore(db), sink: sink, notify: notify, maxOccurrences: maxOccurrences, maxWindowMonths: maxWindowMonths}
}

// Store exposes the read store (used by the dashboard module).
func (s *Service) Store() *Store { return s.store }

func actorID(ctx context.Context) string {
	if a, ok := reqctx.ActorFrom(ctx); ok {
		return a.UserID
	}
	return ""
}

func ap(s string) *string { return &s }

func eqp(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func diff(changes *[]audit.Change, field string, old, newVal *string) {
	if !eqp(old, newVal) {
		*changes = append(*changes, audit.Change{Field: field, Old: old, New: newVal})
	}
}

func (s *Service) record(ctx context.Context, tx *sql.Tx, action, entityID, summary string, changes []audit.Change, meta map[string]any) error {
	_, err := s.sink.Record(ctx, tx, audit.Event{
		Module:     audit.ModuleEvents,
		Action:     action,
		EntityType: "event",
		EntityID:   entityID,
		Summary:    summary,
		Meta:       meta,
		Changes:    changes,
	})
	return err
}

// ---- Reads ----

func (s *Service) ListSeries(ctx context.Context, includeArchived bool, limit int, cursor string) (EventSeriesPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	items, next, err := s.store.ListSeries(ctx, includeArchived, limit, cursor)
	if err != nil {
		return EventSeriesPage{}, err
	}
	if items == nil {
		items = []Event{}
	}
	return EventSeriesPage{Items: items, NextCursor: next}, nil
}

func (s *Service) GetEvent(ctx context.Context, id string) (*EventWithLinks, error) {
	e, err := s.store.GetEventWithLinks(ctx, id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, httpx.ErrNotFound("event not found")
	}
	return e, nil
}

// ---- Series CRUD ----

func (s *Service) CreateEvent(ctx context.Context, in EventCreate) (*Event, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, httpx.ErrUnprocessable("title is required")
	}
	startsOn, err := dates.Parse(in.StartsOn)
	if err != nil {
		return nil, httpx.ErrUnprocessable("starts_on must be a YYYY-MM-DD date")
	}
	rule, err := recur.Parse(in.RRule)
	if err != nil {
		return nil, httpx.ErrUnprocessable("unsupported recurrence rule")
	}
	if err := validateReminder(in.ReminderEnabled, in.ReminderLead); err != nil {
		return nil, err
	}

	ev := Event{
		Title:           in.Title,
		Description:     nonEmpty(in.Description),
		StartsOn:        startsOn.String(),
		RRule:           nonEmpty(rule.String()),
		ReminderEnabled: in.ReminderEnabled,
		ReminderLead:    nonEmpty(in.ReminderLead),
	}
	var out *Event
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var err error
		out, err = s.store.InsertEvent(ctx, tx, ev, actorID(ctx))
		if err != nil {
			return err
		}
		changes := []audit.Change{{Field: "title", New: ap(out.Title)}, {Field: "starts_on", New: ap(out.StartsOn)}}
		if out.RRule != nil {
			changes = append(changes, audit.Change{Field: "rrule", New: out.RRule})
		}
		return s.record(ctx, tx, "event.create", out.ID,
			fmt.Sprintf("Vytvořena událost „%s“", out.Title), changes, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify("event.changed", out)
	return out, nil
}

func (s *Service) UpdateEvent(ctx context.Context, id string, in EventUpdate) (*Event, error) {
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		return nil, httpx.ErrUnprocessable("title cannot be empty")
	}
	if in.StartsOn != nil {
		if _, err := dates.Parse(*in.StartsOn); err != nil {
			return nil, httpx.ErrUnprocessable("starts_on must be a YYYY-MM-DD date")
		}
	}
	if in.RRule != nil {
		rule, err := recur.Parse(*in.RRule)
		if err != nil {
			return nil, httpx.ErrUnprocessable("unsupported recurrence rule")
		}
		canonical := rule.String()
		in.RRule = &canonical // normalise
	}

	var out *Event
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetEvent(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("event not found")
		}
		// Validate the resulting reminder state against the CHECK constraint.
		enabled := before.ReminderEnabled
		if in.ReminderEnabled != nil {
			enabled = *in.ReminderEnabled
		}
		lead := deref(before.ReminderLead)
		if in.ReminderLead != nil {
			lead = *in.ReminderLead
		}
		if err := validateReminder(enabled, lead); err != nil {
			return err
		}
		if err := s.store.UpdateEvent(ctx, tx, id, in); err != nil {
			return err
		}
		out, err = s.store.GetEvent(ctx, tx, id)
		if err != nil {
			return err
		}
		var changes []audit.Change
		diff(&changes, "title", ap(before.Title), ap(out.Title))
		diff(&changes, "description", before.Description, out.Description)
		diff(&changes, "starts_on", ap(before.StartsOn), ap(out.StartsOn))
		diff(&changes, "rrule", before.RRule, out.RRule)
		diff(&changes, "reminder_enabled", ap(fmt.Sprint(before.ReminderEnabled)), ap(fmt.Sprint(out.ReminderEnabled)))
		diff(&changes, "reminder_lead", before.ReminderLead, out.ReminderLead)
		diff(&changes, "archived", ap(fmt.Sprint(before.Archived)), ap(fmt.Sprint(out.Archived)))
		return s.record(ctx, tx, "event.update", id,
			fmt.Sprintf("Upravena událost „%s“ (celá série)", out.Title), changes, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify("event.changed", out)
	return out, nil
}

func (s *Service) DeleteEvent(ctx context.Context, id string, hard bool) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetEvent(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("event not found")
		}
		var changes []audit.Change
		if hard {
			if err := s.store.DeleteEvent(ctx, tx, id); err != nil {
				return err
			}
		} else {
			if err := s.store.UpdateEvent(ctx, tx, id, EventUpdate{Archived: boolPtr(true)}); err != nil {
				return err
			}
			changes = []audit.Change{{Field: "archived", Old: ap("false"), New: ap("true")}}
		}
		return s.record(ctx, tx, "event.delete", id,
			fmt.Sprintf("Smazána událost „%s“", before.Title), changes, metaHard(hard))
	})
	if err != nil {
		return err
	}
	s.notify("event.changed", map[string]string{"id": id})
	return nil
}

// ---- Links ----

func (s *Service) ListLinks(ctx context.Context, eventID string) ([]EventLink, error) {
	e, err := s.store.GetEvent(ctx, s.db, eventID)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, httpx.ErrNotFound("event not found")
	}
	return s.store.ListEventLinks(ctx, s.db, eventID)
}

func (s *Service) CreateLink(ctx context.Context, eventID string, in EventLinkCreate) (*EventLink, error) {
	if err := validateURL(in.URL); err != nil {
		return nil, err
	}
	var out *EventLink
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		e, err := s.store.GetEvent(ctx, tx, eventID)
		if err != nil {
			return err
		}
		if e == nil {
			return httpx.ErrNotFound("event not found")
		}
		pos, err := s.store.lastEventLinkPosition(ctx, tx, eventID)
		if err != nil {
			return err
		}
		out, err = s.store.InsertEventLink(ctx, tx, eventID, in.URL, in.Title, pos)
		if err != nil {
			return err
		}
		return s.record(ctx, tx, "event.link.add", eventID,
			fmt.Sprintf("Přidán odkaz k události: %s", in.URL), nil, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify("event.changed", map[string]string{"id": eventID})
	return out, nil
}

func (s *Service) DeleteLink(ctx context.Context, linkID string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		eventID, err := s.store.EventLinkEventID(ctx, tx, linkID)
		if err != nil {
			return err
		}
		if eventID == "" {
			return httpx.ErrNotFound("link not found")
		}
		if err := s.store.DeleteEventLink(ctx, tx, linkID); err != nil {
			return err
		}
		return s.record(ctx, tx, "event.link.remove", eventID, "Odebrán odkaz z události", nil, nil)
	})
	return err
}

// ---- Reminder completion ----

// Complete records the completion of one occurrence's reminder (idempotent). via
// records the trigger ("dashboard") for cross-module attribution.
func (s *Service) Complete(ctx context.Context, eventID, occurrenceOn, via string) (*ReminderCompletion, error) {
	occDate, err := dates.Parse(occurrenceOn)
	if err != nil {
		return nil, httpx.ErrUnprocessable("occurrence_on must be a YYYY-MM-DD date")
	}
	var out *ReminderCompletion
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		event, err := s.store.GetEvent(ctx, tx, eventID)
		if err != nil {
			return err
		}
		if event == nil {
			return httpx.ErrNotFound("event not found")
		}
		rule, anchor, err := parseSeries(event)
		if err != nil {
			return err
		}
		if !recur.IsOccurrence(anchor, rule, occDate) {
			return httpx.ErrUnprocessable("occurrence_on is not a real occurrence of this event")
		}
		inserted, err := s.store.InsertCompletion(ctx, tx, eventID, occDate.String(), actorID(ctx))
		if err != nil {
			return err
		}
		if inserted { // idempotent: only log the first completion
			meta := map[string]any{}
			if via != "" {
				meta["via"] = via
			}
			if len(meta) == 0 {
				meta = nil
			}
			if err := s.record(ctx, tx, "reminder.complete", eventID,
				fmt.Sprintf("Odškrtnuta připomínka „%s“ (%s)", event.Title, occDate.String()), nil, meta); err != nil {
				return err
			}
		}
		out, err = s.store.GetCompletion(ctx, tx, eventID, occDate.String())
		return err
	})
	if err != nil {
		return nil, err
	}
	s.notify("event.completed", map[string]string{"event_id": eventID, "occurrence_on": occDate.String()})
	return out, nil
}

// Uncomplete removes a completion (undo).
func (s *Service) Uncomplete(ctx context.Context, eventID, occurrenceOn string) error {
	occDate, err := dates.Parse(occurrenceOn)
	if err != nil {
		return httpx.ErrUnprocessable("occurrence_on must be a YYYY-MM-DD date")
	}
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		event, err := s.store.GetEvent(ctx, tx, eventID)
		if err != nil {
			return err
		}
		if event == nil {
			return httpx.ErrNotFound("event not found")
		}
		if err := s.store.DeleteCompletion(ctx, tx, eventID, occDate.String()); err != nil {
			return err
		}
		return s.record(ctx, tx, "reminder.uncomplete", eventID,
			fmt.Sprintf("Zrušeno odškrtnutí připomínky „%s“ (%s)", event.Title, occDate.String()), nil, nil)
	})
	if err != nil {
		return err
	}
	s.notify("event.completed", map[string]string{"event_id": eventID, "occurrence_on": occDate.String()})
	return nil
}

// ---- validation helpers ----

func validateReminder(enabled bool, lead string) error {
	if enabled {
		if !validLeads[lead] {
			return httpx.ErrUnprocessable("reminder_lead must be one of 1d,2d,1w,2w,1m when the reminder is enabled")
		}
	} else if lead != "" && !validLeads[lead] {
		return httpx.ErrUnprocessable("reminder_lead must be one of 1d,2d,1w,2w,1m")
	}
	return nil
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return httpx.ErrUnprocessable("url must be a valid http(s) URL")
	}
	return nil
}

// parseSeries parses an event's stored rrule and anchor date.
func parseSeries(e *Event) (*recur.Rule, dates.Date, error) {
	anchor, err := dates.Parse(e.StartsOn)
	if err != nil {
		return nil, dates.Date{}, err
	}
	rule, err := recur.Parse(deref(e.RRule))
	if err != nil {
		return nil, dates.Date{}, err
	}
	return rule, anchor, nil
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func boolPtr(b bool) *bool { return &b }

func metaHard(hard bool) map[string]any {
	if hard {
		return map[string]any{"hard": true}
	}
	return nil
}
