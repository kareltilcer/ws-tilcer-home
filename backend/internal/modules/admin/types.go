package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/scheduler"
)

// Condition is one "count compared to a number" clause. Key names a METRIC or
// LIST from the catalogs — a list's value is its length (the platform/lists
// contract makes a shared key count the same selection). Op is one of
// conditionOps below.
type Condition struct {
	Key   string `json:"key"`
	Op    string `json:"op"`
	Value int    `json:"value"`
}

// Conditions is a saved condition block: every item ("all") or at least one
// ("any") must hold for the notification to be sent. Nil means "always send".
type Conditions struct {
	Mode  string      `json:"mode"` // "all" | "any"
	Items []Condition `json:"items"`
}

// normalized collapses "no items" to nil, so "no conditions" has exactly one
// representation in the database and in responses, and defaults an empty mode
// to "all" so a saved block always evaluates the same way it validates.
func (c *Conditions) normalized() *Conditions {
	if c == nil || len(c.Items) == 0 {
		return nil
	}
	out := *c
	if out.Mode == "" {
		out.Mode = ModeAll
	}
	return &out
}

// Keys returns the metric/list keys a condition block references.
func (c *Conditions) Keys() []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, len(c.Items))
	for _, it := range c.Items {
		out = append(out, it.Key)
	}
	return out
}

// Condition combinators and comparison operators.
const (
	ModeAll = "all"
	ModeAny = "any"
)

var conditionOps = map[string]func(v, want int) bool{
	"gt":  func(v, want int) bool { return v > want },
	"gte": func(v, want int) bool { return v >= want },
	"lt":  func(v, want int) bool { return v < want },
	"lte": func(v, want int) bool { return v <= want },
	"eq":  func(v, want int) bool { return v == want },
	"neq": func(v, want int) bool { return v != want },
}

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
	// Conditions gate the SEND, not the match: they are evaluated when the
	// notification is about to go out (after the coalescing window), so "jen
	// když zbývá něco nedodělaného" is judged against the current counts.
	Conditions *Conditions `json:"conditions"`
	// ActiveFromLocal/ActiveToLocal bound the rule to a wall-clock window
	// ("HH:MM" in HOME_TIMEZONE, may wrap midnight). Both nil ⇒ always.
	ActiveFromLocal *string   `json:"active_from_local"`
	ActiveToLocal   *string   `json:"active_to_local"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
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
	Conditions            *Conditions   `json:"conditions"`
	ActiveFromLocal       *string       `json:"active_from_local"`
	ActiveToLocal         *string       `json:"active_to_local"`
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
	// Conditions follows the same three-state scheme as the nullable strings:
	// absent ⇒ keep, null (or a block with no items) ⇒ clear, a block ⇒ set.
	Conditions      **Conditions `json:"conditions"`
	ActiveFromLocal **string     `json:"active_from_local"`
	ActiveToLocal   **string     `json:"active_to_local"`
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
	"conditions": true, "active_from_local": true, "active_to_local": true,
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
		{"active_from_local", &u.ActiveFromLocal},
		{"active_to_local", &u.ActiveToLocal},
	} {
		v, err := patchField[string](raw, f.key)
		if err != nil {
			return err
		}
		*f.dst = v
	}

	conds, err := patchField[Conditions](raw, "conditions")
	if err != nil {
		return err
	}
	// A block with no items normalizes to the cleared state, so the composer
	// can always send the full object.
	if conds != nil && *conds != nil {
		*conds = (*conds).normalized()
	}
	u.Conditions = conds
	return nil
}

// patchField resolves one nullable field of a patch:
//
//	absent  → nil        (leave the stored value alone)
//	null    → &(*T)(nil) (clear the stored value)
//	a value → &&value    (set it)
func patchField[T any](raw map[string]json.RawMessage, key string) (**T, error) {
	msg, present := raw[key]
	if !present {
		return nil, nil
	}
	if string(bytes.TrimSpace(msg)) == "null" {
		var cleared *T
		return &cleared, nil
	}
	var v T
	if err := strictUnmarshal(msg, &v); err != nil {
		return nil, err
	}
	p := &v
	return &p, nil
}

// strictUnmarshal decodes the way httpx.DecodeJSON does: an unknown field is an
// error. Every hand-rolled decode in this file needs it, because a type with a
// custom UnmarshalJSON takes its whole payload OFF the request decoder's
// DisallowUnknownFields path. Without it a mistyped key inside a patch is
// silently DROPPED rather than refused — a clause written as {"valu": 3} would
// store value 0, turning "víc než 3" into "víc než 0" — while the very same body
// is a 422 on the create endpoint, which has no custom unmarshaller.
func strictUnmarshal(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// Schedule is a summary schedule (FR-ADM3).
type Schedule struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Enabled       bool          `json:"enabled"`
	Schedule      ScheduleSpec  `json:"schedule"`
	Audience      push.Audience `json:"audience"`
	TitleTemplate string        `json:"title_template"`
	BodyTemplate  string        `json:"body_template"`
	// Conditions gate the fire: a summary whose conditions do not hold at its
	// slot is skipped for that day (a personal-scope condition skips just the
	// recipients it fails for). Nil ⇒ always send.
	Conditions         *Conditions `json:"conditions"`
	LastFiredAt        *time.Time  `json:"last_fired_at"`
	LastFiredLocalDate string      `json:"-"`
	Description        string      `json:"description"` // "Každý den v 8:00"
	CreatedBy          string      `json:"created_by"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
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
	Conditions    *Conditions   `json:"conditions"`
}

// ScheduleUpdate is a partial update.
type ScheduleUpdate struct {
	Name          *string        `json:"name"`
	Enabled       *bool          `json:"enabled"`
	Schedule      *ScheduleSpec  `json:"schedule"`
	Audience      *push.Audience `json:"audience"`
	TitleTemplate *string        `json:"title_template"`
	BodyTemplate  *string        `json:"body_template"`
	// Conditions follows RuleUpdate's three-state scheme, and for the same
	// reason: with a plain *Conditions, JSON null is indistinguishable from an
	// omitted key, so the natural way to say "no conditions" — which is also
	// what a read-modify-write of the GET response sends — would silently mean
	// "keep". Absent ⇒ keep, null (or a block with no items) ⇒ clear.
	Conditions **Conditions `json:"conditions"`
}

// scheduleUpdateFields is every key a schedule patch may carry. Like
// ruleUpdateFields, this exists because the custom UnmarshalJSON below bypasses
// the decoder's DisallowUnknownFields.
var scheduleUpdateFields = map[string]bool{
	"name": true, "enabled": true, "schedule": true, "audience": true,
	"title_template": true, "body_template": true, "conditions": true,
}

// UnmarshalJSON decodes a schedule patch, keeping "absent" and "null" apart for
// the conditions block. The plain fields go through the normal decoder.
func (u *ScheduleUpdate) UnmarshalJSON(data []byte) error {
	// A distinct type so this method is not called recursively. Conditions is
	// carried as raw bytes and discarded here — patchField below is what decodes
	// it — but it has to be DECLARED, or the strict pass would refuse the key.
	type plain struct {
		Name          *string         `json:"name"`
		Enabled       *bool           `json:"enabled"`
		Schedule      *ScheduleSpec   `json:"schedule"`
		Audience      *push.Audience  `json:"audience"`
		TitleTemplate *string         `json:"title_template"`
		BodyTemplate  *string         `json:"body_template"`
		Conditions    json.RawMessage `json:"conditions"`
	}
	var p plain
	// Strict, so a typo NESTED in schedule or audience is refused here the way it
	// would be on the create endpoint — the outer key check below sees only the
	// top level.
	if err := strictUnmarshal(data, &p); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key := range raw {
		if !scheduleUpdateFields[key] {
			return fmt.Errorf("json: unknown field %q", key)
		}
	}

	*u = ScheduleUpdate{
		Name: p.Name, Enabled: p.Enabled, Schedule: p.Schedule,
		Audience: p.Audience, TitleTemplate: p.TitleTemplate, BodyTemplate: p.BodyTemplate,
	}

	conds, err := patchField[Conditions](raw, "conditions")
	if err != nil {
		return err
	}
	// A block with no items normalizes to the cleared state, so the composer
	// can always send the full object.
	if conds != nil && *conds != nil {
		*conds = (*conds).normalized()
	}
	u.Conditions = conds
	return nil
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
