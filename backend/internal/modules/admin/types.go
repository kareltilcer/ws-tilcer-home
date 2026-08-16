package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/scheduler"
)

// Rule is a trigger rule: an audited action bound to a push (FR-ADM2).
type Rule struct {
	ID                    string        `json:"id"`
	Name                  string        `json:"name"`
	Enabled               bool          `json:"enabled"`
	ActionKey             *string       `json:"action_key"`
	ActionPrefix          *string       `json:"action_prefix"`
	FilterModule          *string       `json:"filter_module"`
	FilterEntityType      *string       `json:"filter_entity_type"`
	FilterLevel           *string       `json:"filter_level"`
	Audience              push.Audience `json:"audience"`
	TitleTemplate         *string       `json:"title_template"`
	BodyTemplate          *string       `json:"body_template"`
	CoalesceWindowSeconds int           `json:"coalesce_window_seconds"`
	ExcludeActor          bool          `json:"exclude_actor"`
	CreatedBy             string        `json:"created_by"`
	CreatedAt             time.Time     `json:"created_at"`
	UpdatedAt             time.Time     `json:"updated_at"`
}

// RuleCreate is the create payload (openapi NotificationRuleCreate).
type RuleCreate struct {
	Name                  string        `json:"name"`
	Enabled               *bool         `json:"enabled"`
	ActionKey             *string       `json:"action_key"`
	ActionPrefix          *string       `json:"action_prefix"`
	FilterModule          *string       `json:"filter_module"`
	FilterEntityType      *string       `json:"filter_entity_type"`
	FilterLevel           *string       `json:"filter_level"`
	Audience              push.Audience `json:"audience"`
	TitleTemplate         *string       `json:"title_template"`
	BodyTemplate          *string       `json:"body_template"`
	CoalesceWindowSeconds *int          `json:"coalesce_window_seconds"`
	ExcludeActor          *bool         `json:"exclude_actor"`
}

// RuleUpdate is a partial update (openapi NotificationRuleUpdate). A nil field
// is left alone; the two-level pointers distinguish "not sent" from "set to null"
// — but ONLY because UnmarshalJSON below establishes that distinction by hand.
// encoding/json cannot: decoding `null` into a **string sets the OUTER pointer
// to nil, making an explicit null indistinguishable from an omitted key, which
// would make "clear this template" a silent no-op.
type RuleUpdate struct {
	Name                  *string        `json:"name"`
	Enabled               *bool          `json:"enabled"`
	ActionKey             **string       `json:"action_key"`
	ActionPrefix          **string       `json:"action_prefix"`
	FilterModule          **string       `json:"filter_module"`
	FilterEntityType      **string       `json:"filter_entity_type"`
	FilterLevel           **string       `json:"filter_level"`
	Audience              *push.Audience `json:"audience"`
	TitleTemplate         **string       `json:"title_template"`
	BodyTemplate          **string       `json:"body_template"`
	CoalesceWindowSeconds *int           `json:"coalesce_window_seconds"`
	ExcludeActor          *bool          `json:"exclude_actor"`
}

// ruleUpdateFields is every key a rule patch may carry. A custom UnmarshalJSON
// BYPASSES the decoder's DisallowUnknownFields, so the check has to be made here
// instead — otherwise a client-side typo ("body_temlate") becomes a 200 that
// changes nothing, which is the worst possible answer to a save.
var ruleUpdateFields = map[string]bool{
	"name": true, "enabled": true, "action_key": true, "action_prefix": true,
	"filter_module": true, "filter_entity_type": true, "filter_level": true,
	"audience": true, "title_template": true, "body_template": true,
	"coalesce_window_seconds": true, "exclude_actor": true,
}

// UnmarshalJSON decodes a rule patch, keeping "absent" and "null" apart for
// every nullable field. The plain fields go through the normal decoder; the
// nullable ones are resolved against the raw key set, which is the only place
// the difference actually survives.
func (u *RuleUpdate) UnmarshalJSON(data []byte) error {
	// A distinct type so this method is not called recursively.
	type plain struct {
		Name                  *string        `json:"name"`
		Enabled               *bool          `json:"enabled"`
		Audience              *push.Audience `json:"audience"`
		CoalesceWindowSeconds *int           `json:"coalesce_window_seconds"`
		ExcludeActor          *bool          `json:"exclude_actor"`
	}
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key := range raw {
		if !ruleUpdateFields[key] {
			return fmt.Errorf("json: unknown field %q", key)
		}
	}

	*u = RuleUpdate{
		Name:                  p.Name,
		Enabled:               p.Enabled,
		Audience:              p.Audience,
		CoalesceWindowSeconds: p.CoalesceWindowSeconds,
		ExcludeActor:          p.ExcludeActor,
	}

	for _, f := range []struct {
		key string
		dst **(*string)
	}{
		{"action_key", &u.ActionKey},
		{"action_prefix", &u.ActionPrefix},
		{"filter_module", &u.FilterModule},
		{"filter_entity_type", &u.FilterEntityType},
		{"filter_level", &u.FilterLevel},
		{"title_template", &u.TitleTemplate},
		{"body_template", &u.BodyTemplate},
	} {
		v, err := patchString(raw, f.key)
		if err != nil {
			return err
		}
		*f.dst = v
	}
	return nil
}

// patchString resolves one nullable string field of a patch:
//
//	absent      → nil          (leave the stored value alone)
//	null        → &(*string)(nil) (clear the stored value)
//	"something" → &&"something" (set it)
func patchString(raw map[string]json.RawMessage, key string) (**string, error) {
	msg, present := raw[key]
	if !present {
		return nil, nil
	}
	if string(bytes.TrimSpace(msg)) == "null" {
		var cleared *string
		return &cleared, nil
	}
	var s string
	if err := json.Unmarshal(msg, &s); err != nil {
		return nil, err
	}
	p := &s
	return &p, nil
}

// Schedule is a summary schedule (FR-ADM3).
type Schedule struct {
	ID                 string        `json:"id"`
	Name               string        `json:"name"`
	Enabled            bool          `json:"enabled"`
	Schedule           ScheduleSpec  `json:"schedule"`
	Audience           push.Audience `json:"audience"`
	TitleTemplate      string        `json:"title_template"`
	BodyTemplate       string        `json:"body_template"`
	LastFiredAt        *time.Time    `json:"last_fired_at"`
	LastFiredLocalDate string        `json:"-"`
	Description        string        `json:"description"` // "Každý den v 8:00"
	CreatedBy          string        `json:"created_by"`
	CreatedAt          time.Time     `json:"created_at"`
	UpdatedAt          time.Time     `json:"updated_at"`
}

// ScheduleSpec is the wall-clock recurrence (openapi ScheduleSpec).
type ScheduleSpec struct {
	TimeLocal string             `json:"time_local"`
	Days      scheduler.DaysSpec `json:"days"`
}

// ScheduleCreate is the create payload.
type ScheduleCreate struct {
	Name          string        `json:"name"`
	Enabled       *bool         `json:"enabled"`
	Schedule      ScheduleSpec  `json:"schedule"`
	Audience      push.Audience `json:"audience"`
	TitleTemplate string        `json:"title_template"`
	BodyTemplate  string        `json:"body_template"`
}

// ScheduleUpdate is a partial update.
type ScheduleUpdate struct {
	Name          *string        `json:"name"`
	Enabled       *bool          `json:"enabled"`
	Schedule      *ScheduleSpec  `json:"schedule"`
	Audience      *push.Audience `json:"audience"`
	TitleTemplate *string        `json:"title_template"`
	BodyTemplate  *string        `json:"body_template"`
}

// BroadcastRequest is an ad-hoc send (FR-ADM1).
type BroadcastRequest struct {
	Title    string        `json:"title"`
	Body     string        `json:"body"`
	URL      *string       `json:"url"`
	Audience push.Audience `json:"audience"`
}

// SendResult is what a broadcast/test returns (openapi SendResult).
type SendResult struct {
	Recipients    int `json:"recipients"`
	Subscriptions int `json:"subscriptions"`
}

// Delivery is one recorded attempt (FR-ADM5). Operational, not audit.
type Delivery struct {
	ID             string    `json:"id"`
	TS             time.Time `json:"ts"`
	Kind           string    `json:"kind"`
	Category       string    `json:"category"`
	RuleID         *string   `json:"rule_id"`
	UserID         string    `json:"user_id"`
	UserLabel      string    `json:"user_label"` // resolved from the member directory
	SubscriptionID *string   `json:"subscription_id"`
	Status         string    `json:"status"`
	Error          *string   `json:"error"`
}

// DeliveryFilter is the delivery log's query (keyset-paged, newest first).
type DeliveryFilter struct {
	Kind   string
	Status string
	RuleID string
	UserID string
	From   string
	To     string
	Limit  int
	Cursor string
}

// Page wrappers match the openapi *Page schemas.
type RulePage struct {
	Items      []Rule  `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

type SchedulePage struct {
	Items      []Schedule `json:"items"`
	NextCursor *string    `json:"next_cursor"`
}

type DeliveryPage struct {
	Items      []Delivery `json:"items"`
	NextCursor *string    `json:"next_cursor"`
}

// Catalog is what the composer needs to offer picks instead of free-typed keys
// (FR-ADM4).
type Catalog struct {
	Actions []ActionDescriptor      `json:"actions"`
	Metrics []MetricDescriptor      `json:"metrics"`
	Lists   []ListDescriptor        `json:"lists"`
	Tokens  map[string]TokenPalette `json:"tokens"`
	// Members is an addition beyond openapi 0.6.0's NotificationCatalog: the
	// audience picker's "Vybraným lidem" needs names, and home has no user
	// directory endpoint. Serving it here keeps the composer to one round trip.
	Members []push.Member `json:"members"`
}

// ActionDescriptor is one audit action key a rule can bind to.
type ActionDescriptor struct {
	Key    string  `json:"key"`    // bare verb, matched against audit_events.action
	Module string  `json:"module"` // owning module
	Label  *string `json:"label"`  // human Czech phrase ("Když někdo dokončí připomínku")
}

// MetricDescriptor is one metric a summary can reference.
type MetricDescriptor struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Unit  *string `json:"unit"`
	Scope string  `json:"scope"`
}

// ListDescriptor is one module list a summary can name — the "which ones?" to a
// metric's "how many?". Empty is what the notification says when the list turns
// out to be empty, so the composer's preview can show that case honestly.
type ListDescriptor struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Empty string `json:"empty"`
	Scope string `json:"scope"`
}
