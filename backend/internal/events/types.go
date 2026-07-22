// Package events implements Okno do budoucnosti: all-day future events with an
// optional RFC 5545 RRULE subset (expanded on read, never stored per occurrence)
// and one optional in-app reminder. The only per-occurrence row is a reminder
// completion. Every mutation writes an audit event in the same transaction.
package events

// ---- Wire types (match openapi.yaml schemas) ----

type Event struct {
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	Description     *string `json:"description"`
	StartsOn        string  `json:"starts_on"`
	RRule           *string `json:"rrule"`
	Timezone        string  `json:"timezone"`
	ReminderEnabled bool    `json:"reminder_enabled"`
	ReminderLead    *string `json:"reminder_lead"`
	Archived        bool    `json:"archived"`
	CreatedBy       *string `json:"created_by"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type EventLink struct {
	ID       string  `json:"id"`
	EventID  string  `json:"event_id"`
	URL      string  `json:"url"`
	Title    *string `json:"title"`
	Position string  `json:"position"`
}

type EventWithLinks struct {
	Event
	Links []EventLink `json:"links"`
}

type EventSeriesPage struct {
	Items      []Event `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// Occurrence is a computed occurrence — never persisted.
type Occurrence struct {
	EventID           string  `json:"event_id"`
	OccurrenceOn      string  `json:"occurrence_on"`
	Title             string  `json:"title"`
	Description       *string `json:"description"`
	Recurring         bool    `json:"recurring"`
	ReminderEnabled   bool    `json:"reminder_enabled"`
	ReminderLead      *string `json:"reminder_lead"`
	ReminderCompleted bool    `json:"reminder_completed"`
}

type OccurrenceMonth struct {
	Month       string       `json:"month"`
	Occurrences []Occurrence `json:"occurrences"`
}

type OccurrenceMonths struct {
	Months []OccurrenceMonth `json:"months"`
}

type ReminderCompletion struct {
	EventID      string  `json:"event_id"`
	OccurrenceOn string  `json:"occurrence_on"`
	CompletedBy  *string `json:"completed_by"`
	CompletedAt  string  `json:"completed_at"`
}

// ---- Request DTOs ----

type EventCreate struct {
	Title           string `json:"title"`
	Description     string `json:"description"`
	StartsOn        string `json:"starts_on"`
	RRule           string `json:"rrule"`
	ReminderEnabled bool   `json:"reminder_enabled"`
	ReminderLead    string `json:"reminder_lead"`
}

type EventUpdate struct {
	Title           *string `json:"title"`
	Description     *string `json:"description"`
	StartsOn        *string `json:"starts_on"`
	RRule           *string `json:"rrule"`
	ReminderEnabled *bool   `json:"reminder_enabled"`
	ReminderLead    *string `json:"reminder_lead"`
	Archived        *bool   `json:"archived"`
}

type EventLinkCreate struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// validLeads is the reminder lead-time whitelist (FR-E4).
var validLeads = map[string]bool{"1d": true, "2d": true, "1w": true, "2w": true, "1m": true}
