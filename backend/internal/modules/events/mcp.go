package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The events module's MCP provider (v11, PRD §V11-4 FR-M4).
//
// ⚠ WHAT IS ABSENT, BY NAME: every delete, every link mutation, and `uncomplete`.
// The first two for the D295 reason; `uncomplete` because "I did not actually do
// that" is a correction a person makes about their own memory, and an assistant
// undoing somebody's completion is the one verb here with a blast radius beyond
// the caller.

// ⚠ mcp.NoResources IS EMBEDDED RATHER THAN RE-IMPLEMENTED — see the twin note
// in todo/mcp.go. An event is not addressable by URI.
type mcpProvider struct {
	mcp.NoResources
	svc      *Service
	location *time.Location
}

// MCPProvider implements mcp.Source.
func (m *Module) MCPProvider() mcp.Provider {
	return &mcpProvider{svc: m.svc, location: m.loc}
}

func (p *mcpProvider) Module() string { return "events" }

const (
	toolUpcoming = "home_events_upcoming"
	toolCreate   = "home_events_create"
	toolUpdate   = "home_events_update"
	toolComplete = "home_events_complete"
)

// leadValues is the reminder-lead vocabulary, in ascending duration, and it is
// the ONE place v11 spells it.
//
// ⚠ "0d" IS THE SAME-DAY LEAD AND IT IS NOT A TYPO FOR "no reminder" (#46,
// §V10-16): it is a lead of nothing, which puts the reminder on the event's own
// morning. `reminder_enabled: false` is how an event has no reminder.
//
// ⚠ IT IS ORDERED AND validLeads IS A MAP, which is why the list is written out
// rather than derived from it: a Go map has no order, so a derived enum would
// offer the model "0d, 1d, 1m, 1w, 2d, 2w" — alphabetical, which reads as a
// vocabulary with no shape at all. TestLeadVocabularyMatchesTheValidator is what
// holds the two to the same SET instead.
var leadValues = []string{"0d", "1d", "2d", "1w", "2w", "1m"}

// leadEnum is leadValues as a JSON array, spliced into BOTH input schemas below.
//
// ⚠ THE SCHEMAS ARE WHAT A MODEL READS, so a hand-copied enum beside a
// validator is a documentation defect with no failing test: the seventh lead
// would be accepted by the service and never offered to the caller. One value,
// two uses — and the refusal message below joins the same slice, so the three
// places a lead is named in this file are one place.
var leadEnum = func() string {
	b, err := json.Marshal(leadValues)
	if err != nil {
		panic("events: marshal lead vocabulary: " + err.Error())
	}
	return string(b)
}()

func (p *mcpProvider) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        toolUpcoming,
			Title:       "Upcoming events",
			Description: "Returns the household's events occurring in a date window, recurrences expanded, each with whether its reminder has been ticked off; defaults to the next 30 days.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "from": {"type": "string", "description": "ISO date (YYYY-MM-DD). Defaults to today."},
    "to": {"type": "string", "description": "ISO date (YYYY-MM-DD). Defaults to 30 days after from."},
    "include_archived": {"type": "boolean"}
  },
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
		{
			Name:        toolCreate,
			Title:       "Create an event",
			Description: "Adds a household event on a date, optionally recurring by RRULE and optionally reminding — reminder_lead \"0d\" reminds on the day itself, not the day before.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "title": {"type": "string", "minLength": 1},
    "starts_on": {"type": "string", "description": "ISO date (YYYY-MM-DD)."},
    "description": {"type": "string"},
    "rrule": {"type": "string", "description": "Optional iCalendar RRULE, e.g. FREQ=YEARLY."},
    "reminder_enabled": {"type": "boolean"},
    "reminder_lead": {"type": "string", "enum": ` + leadEnum + `, "description": "\"0d\" is the day itself."}
  },
  "required": ["title", "starts_on"],
  "additionalProperties": false
}`),
		},
		{
			Name:        toolUpdate,
			Title:       "Update an event",
			Description: "Changes an event's title, date, description, recurrence, reminder or archived flag; archiving is how the household retires an event, and unlike a delete it is reversible.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string"},
    "title": {"type": "string", "minLength": 1},
    "starts_on": {"type": "string"},
    "description": {"type": "string"},
    "rrule": {"type": "string"},
    "reminder_enabled": {"type": "boolean"},
    "reminder_lead": {"type": "string", "enum": ` + leadEnum + `},
    "archived": {"type": "boolean"}
  },
  "required": ["id"],
  "additionalProperties": false
}`),
		},
		{
			Name:        toolComplete,
			Title:       "Tick off a reminder",
			Description: "Marks one occurrence's reminder as done, which is what removes it from the household's Připomínky; it is per occurrence, so ticking one date leaves the rest of a recurring event alone.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "event_id": {"type": "string"},
    "occurrence_on": {"type": "string", "description": "The occurrence's ISO date (YYYY-MM-DD), as home_events_upcoming reports it."}
  },
  "required": ["event_id", "occurrence_on"],
  "additionalProperties": false
}`),
		},
	}
}

func (p *mcpProvider) Call(ctx context.Context, name string, args json.RawMessage) (mcp.Result, error) {
	switch name {
	case toolUpcoming:
		return p.upcoming(ctx, args)
	case toolCreate:
		return p.create(ctx, args)
	case toolUpdate:
		return p.update(ctx, args)
	case toolComplete:
		return p.complete(ctx, args)
	default:
		return mcp.Result{}, mcp.UnknownToolError(name)
	}
}

type upcomingArgs struct {
	From            string `json:"from"`
	To              string `json:"to"`
	IncludeArchived bool   `json:"include_archived"`
}

func (p *mcpProvider) upcoming(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in upcomingArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	// ⚠ BOTH BOUNDS ARE VALIDATED, AND UNCONDITIONALLY. The check used to live
	// inside the branch that DEFAULTS `to`, so a malformed `from` sent together
	// with an explicit `to` skipped it entirely and reached the service — which is
	// the 500-instead-of-422 shape D311 exists to prevent, and an agent retries a
	// 500 where it gives up on a 422.
	from := in.From
	if from == "" {
		from = time.Now().In(p.location).Format("2006-01-02")
	}
	start, err := time.ParseInLocation("2006-01-02", from, p.location)
	if err != nil {
		return mcp.Result{}, httpx.ErrUnprocessable("from musí být ve tvaru RRRR-MM-DD.")
	}
	to := in.To
	if to == "" {
		to = start.AddDate(0, 0, 30).Format("2006-01-02")
	} else if err := validDate(to, "to"); err != nil {
		return mcp.Result{}, err
	}
	months, err := p.svc.Occurrences(ctx, from, to, in.IncludeArchived)
	if err != nil {
		return mcp.Result{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Události %s – %s:\n", from, to)
	total := 0
	for _, m := range months.Months {
		for _, o := range m.Occurrences {
			total++
			fmt.Fprintf(&b, "\n• %s — %s (id %s)", o.OccurrenceOn, o.Title, o.EventID)
			if o.ReminderEnabled {
				state := "připomínka"
				if o.ReminderCompleted {
					state = "připomínka hotová"
				}
				fmt.Fprintf(&b, " [%s]", state)
			}
		}
	}
	if total == 0 {
		b.WriteString("\n(žádné události)")
	}
	return mcp.TextResult(b.String(), months)
}

type createArgs struct {
	Title           string `json:"title"`
	StartsOn        string `json:"starts_on"`
	Description     string `json:"description"`
	RRule           string `json:"rrule"`
	ReminderEnabled bool   `json:"reminder_enabled"`
	ReminderLead    string `json:"reminder_lead"`
}

// create validates before the service (D311).
func (p *mcpProvider) create(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in createArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.Title) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Název události nesmí být prázdný.")
	}
	if err := validDate(in.StartsOn, "starts_on"); err != nil {
		return mcp.Result{}, err
	}
	// ⚠ BOTH HALVES OF validateReminder, REFUSED HERE. The second half — a lead
	// outside the vocabulary — was checked; the first — a reminder switched ON with
	// no lead at all — was not, so it reached the service and came back in the
	// service's English ("reminder_lead must be one of 0d,1d,…") on a tool surface
	// whose every other refusal is Czech. That is D311's rule: no write tool may
	// reach a service with an input it could have refused itself.
	//
	// ⚠ AND ONLY create CAN DECIDE IT. UpdateEvent validates the MERGED state —
	// an event that already carries a lead may legitimately have its reminder
	// switched on with no lead in the patch — so the tool would have to load the
	// row to ask the question, and update keeps the one check it can take alone.
	if in.ReminderEnabled && !validLeads[in.ReminderLead] {
		return mcp.Result{}, httpx.ErrUnprocessable(
			"Při zapnuté připomínce zadejte reminder_lead, jedno z: " +
				strings.Join(leadValues, ", ") + ".")
	}
	if in.ReminderLead != "" && !validLeads[in.ReminderLead] {
		return mcp.Result{}, httpx.ErrUnprocessable(
			"reminder_lead musí být jedno z: " + strings.Join(leadValues, ", ") + ".")
	}
	ev, err := p.svc.CreateEvent(ctx, EventCreate{
		Title:           in.Title,
		Description:     in.Description,
		StartsOn:        in.StartsOn,
		RRule:           in.RRule,
		ReminderEnabled: in.ReminderEnabled,
		ReminderLead:    in.ReminderLead,
	})
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.TextResult(fmt.Sprintf("Událost „%s“ vytvořena na %s (id %s).", ev.Title, ev.StartsOn, ev.ID), ev)
}

type updateArgs struct {
	ID              string  `json:"id"`
	Title           *string `json:"title"`
	StartsOn        *string `json:"starts_on"`
	Description     *string `json:"description"`
	RRule           *string `json:"rrule"`
	ReminderEnabled *bool   `json:"reminder_enabled"`
	ReminderLead    *string `json:"reminder_lead"`
	Archived        *bool   `json:"archived"`
}

func (p *mcpProvider) update(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in updateArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.ID) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Chybí id události.")
	}
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Název události nesmí být prázdný.")
	}
	if in.StartsOn != nil {
		if err := validDate(*in.StartsOn, "starts_on"); err != nil {
			return mcp.Result{}, err
		}
	}
	if in.ReminderLead != nil && *in.ReminderLead != "" && !validLeads[*in.ReminderLead] {
		return mcp.Result{}, httpx.ErrUnprocessable(
			"reminder_lead musí být jedno z: " + strings.Join(leadValues, ", ") + ".")
	}
	if in.Title == nil && in.StartsOn == nil && in.Description == nil && in.RRule == nil &&
		in.ReminderEnabled == nil && in.ReminderLead == nil && in.Archived == nil {
		return mcp.Result{}, httpx.ErrUnprocessable("Zadejte alespoň jednu změnu.")
	}
	ev, err := p.svc.UpdateEvent(ctx, in.ID, EventUpdate{
		Title:           in.Title,
		Description:     in.Description,
		StartsOn:        in.StartsOn,
		RRule:           in.RRule,
		ReminderEnabled: in.ReminderEnabled,
		ReminderLead:    in.ReminderLead,
		Archived:        in.Archived,
	})
	if err != nil {
		return mcp.Result{}, err
	}
	if ev == nil {
		return mcp.NotFoundResult(), nil
	}
	return mcp.TextResult(fmt.Sprintf("Událost „%s“ upravena.", ev.Title), ev)
}

type completeArgs struct {
	EventID      string `json:"event_id"`
	OccurrenceOn string `json:"occurrence_on"`
}

func (p *mcpProvider) complete(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in completeArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.EventID) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Chybí event_id.")
	}
	if err := validDate(in.OccurrenceOn, "occurrence_on"); err != nil {
		return mcp.Result{}, err
	}
	// The empty `via` selects the audit summary's wording, as it does for the
	// HTTP handler; v11's `via` column is read from the context by the sink.
	done, err := p.svc.Complete(ctx, in.EventID, in.OccurrenceOn, "")
	if err != nil {
		return mcp.Result{}, err
	}
	if done == nil {
		return mcp.NotFoundResult(), nil
	}
	return mcp.TextResult(fmt.Sprintf("Připomínka na %s odškrtnuta.", done.OccurrenceOn), done)
}

// Search scans event titles.
//
// ⚠ D302: an actor-less ctx is an ERROR, never an empty slice. See the twin note
// in todo/mcp.go — the rule is the same in every provider whether or not this
// one's data happens to be household-visible.
func (p *mcpProvider) Search(ctx context.Context, q mcp.Query) ([]mcp.Hit, error) {
	if _, ok := reqctx.ActorFrom(ctx); !ok {
		return nil, fmt.Errorf("events: search without an actor")
	}
	rows, err := p.svc.Store().SearchEvents(ctx, q.Text, q.Limit)
	if err != nil {
		return nil, err
	}
	hits := make([]mcp.Hit, 0, len(rows))
	for _, e := range rows {
		updated, _ := time.Parse(time.RFC3339, e.UpdatedAt)
		hits = append(hits, mcp.Hit{
			Kind:      "events.event",
			ID:        e.ID,
			Title:     e.Title,
			Snippet:   "od " + e.StartsOn,
			UpdatedAt: updated,
			ExactHit:  strings.EqualFold(strings.TrimSpace(e.Title), strings.TrimSpace(q.Text)),
		})
	}
	return hits, nil
}

// Get answers home_get.
func (p *mcpProvider) Get(ctx context.Context, kind, id string) (mcp.Result, error) {
	if kind != "events.event" {
		return mcp.NotFoundResult(), nil
	}
	ev, err := p.svc.GetEvent(ctx, id)
	if err != nil {
		return mcp.Result{}, err
	}
	if ev == nil {
		return mcp.NotFoundResult(), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s — od %s (id %s)", ev.Title, ev.StartsOn, ev.ID)
	if ev.RRule != nil && *ev.RRule != "" {
		fmt.Fprintf(&b, "\nOpakování: %s", *ev.RRule)
	}
	if ev.ReminderEnabled && ev.ReminderLead != nil {
		fmt.Fprintf(&b, "\nPřipomínka: %s", *ev.ReminderLead)
	}
	if ev.Description != nil && *ev.Description != "" {
		fmt.Fprintf(&b, "\n\n%s", *ev.Description)
	}
	return mcp.TextResult(b.String(), ev)
}

func validDate(s, field string) error {
	if strings.TrimSpace(s) == "" {
		return httpx.ErrUnprocessable("Chybí " + field + ".")
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return httpx.ErrUnprocessable(field + " musí být ve tvaru RRRR-MM-DD.")
	}
	return nil
}

// EventHit is one row of the cross-module search.
type EventHit struct {
	ID        string
	Title     string
	StartsOn  string
	UpdatedAt string
}

// SearchEvents scans event titles. A title scan rather than an FTS5 index, for
// the reason todo.Store.SearchCards states: `events` carries no external-content
// FTS table, and a household's event list is short.
func (s *Store) SearchEvents(ctx context.Context, q string, limit int) ([]EventHit, error) {
	q = strings.TrimSpace(q)
	if q == "" || limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, starts_on, updated_at
		  FROM events
		 WHERE archived = 0 AND title LIKE ? ESCAPE '\'
		 ORDER BY updated_at DESC
		 LIMIT ?`, appdb.LikeContains(q), limit)
	if err != nil {
		return nil, err
	}
	return appdb.Collect(rows, func(row appdb.Scanner) (EventHit, error) {
		var h EventHit
		err := row.Scan(&h.ID, &h.Title, &h.StartsOn, &h.UpdatedAt)
		return h, err
	})
}
