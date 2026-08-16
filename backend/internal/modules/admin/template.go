package admin

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
)

// The template engine (PRD §V5-10 D61, HANDOFF-7 §8).
//
// This is NOT a template language. It is a whitelisted token substitutor: an
// admin writes free Czech and inserts tokens picked from a catalog, and the
// engine replaces exactly those tokens with values. There are no expressions, no
// conditionals, no loops, and no way to reach anything the palette does not name
// — which is what makes "give the admin real composition freedom" safe.
//
// Validation happens at SAVE time, not at fire time: a rule with an unknown
// token is refused with a 422 on the field, so a notification never fails at
// 08:00 because of a typo made three weeks earlier.

// tokenPattern matches {{ token.path }} with optional inner whitespace.
var tokenPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.]+)\s*\}\}`)

// bracePattern matches ANY {{…}} run, including ones tokenPattern rejects. It
// exists so validation can refuse what rendering would otherwise leave standing:
// Render only substitutes what tokenPattern matches, so a near-miss like the
// composer's `{{change.<pole>.old}}` placeholder would survive verbatim onto a
// household member's lock screen.
var bracePattern = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// Contexts a template can be written in. Each has its own palette.
const (
	ContextBroadcast = "broadcast"
	ContextTrigger   = "trigger"
	ContextSummary   = "summary"
)

// Time tokens are available in every context.
var timeTokens = []string{"now", "date"}

// Event tokens are available to trigger rules only.
var eventTokens = []string{
	"event.summary",
	"event.action",
	"event.module",
	"event.entity_type",
	"event.entity_id",
	"event.actor_label",
}

// Placeholder is what an unresolvable token renders as. A notification must
// never show a raw {{…}} to a household member, and must never fail to send
// because one number could not be computed.
const Placeholder = "—"

// TokenPalette is what the composer offers per context (FR-ADM4).
type TokenPalette struct {
	Time   []string `json:"time"`
	Event  []string `json:"event,omitempty"`
	Change []string `json:"change,omitempty"`
	Metric []string `json:"metric,omitempty"`
}

// MetricKeyLister is the part of the metrics registry the engine needs. Keeping
// it an interface means the template code depends on the catalog's shape, not on
// the registry's implementation.
type MetricKeyLister interface {
	Has(key string) bool
}

// ValidateTemplate checks every token in a template against its context's
// palette, returning a Czech reason suitable for a 422 on the field.
//
// `{{change.<field>.old|new}}` is validated by SHAPE rather than by name: the
// field set differs per entity type and per event, so demanding a known field
// here would refuse legitimate rules. An unknown field simply renders as the
// placeholder at fire time.
func ValidateTemplate(tmpl, context string, metrics MetricKeyLister) error {
	// Refuse anything brace-shaped that rendering would NOT substitute, before
	// checking the tokens themselves. The composer's change chips are inserted as
	// `{{change.<pole>.old}}` for the admin to name the field in; left as-is they
	// resolve to nothing and ship the raw braces to a phone.
	for _, brace := range bracePattern.FindAllString(tmpl, -1) {
		if tokenPattern.MatchString(brace) {
			continue
		}
		if strings.ContainsAny(brace, "<>") {
			return fmt.Errorf("v údaji %s nahraďte <pole> názvem pole (například change.title.old)", brace)
		}
		return fmt.Errorf("neznámý údaj: %s", brace)
	}
	for _, token := range ExtractTokens(tmpl) {
		if err := validateToken(token, context, metrics); err != nil {
			return err
		}
	}
	return nil
}

func validateToken(token, context string, metrics MetricKeyLister) error {
	for _, t := range timeTokens {
		if token == t {
			return nil
		}
	}

	switch {
	case strings.HasPrefix(token, "event."):
		if context != ContextTrigger {
			return fmt.Errorf("údaj {{%s}} lze použít jen v pravidle reagujícím na akci", token)
		}
		for _, t := range eventTokens {
			if token == t {
				return nil
			}
		}
		return fmt.Errorf("neznámý údaj o události: {{%s}}", token)

	case strings.HasPrefix(token, "change."):
		if context != ContextTrigger {
			return fmt.Errorf("údaj {{%s}} lze použít jen v pravidle reagujícím na akci", token)
		}
		// change.<field>.old | change.<field>.new
		parts := strings.Split(token, ".")
		if len(parts) < 3 || (parts[len(parts)-1] != "old" && parts[len(parts)-1] != "new") {
			return fmt.Errorf("údaj o změně musí končit .old nebo .new: {{%s}}", token)
		}
		return nil

	case strings.HasPrefix(token, "metric."):
		if context != ContextSummary {
			return fmt.Errorf("číslo {{%s}} lze použít jen v souhrnu", token)
		}
		key := strings.TrimPrefix(token, "metric.")
		if metrics == nil || !metrics.Has(key) {
			return fmt.Errorf("neznámé číslo: {{%s}}", token)
		}
		return nil

	default:
		return fmt.Errorf("neznámý údaj: {{%s}}", token)
	}
}

// ExtractTokens returns every token name used in a template, in order.
func ExtractTokens(tmpl string) []string {
	matches := tokenPattern.FindAllStringSubmatch(tmpl, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// MetricKeysIn returns the metric keys a template references — the scheduler uses
// this to decide whether a summary personalizes per recipient.
func MetricKeysIn(tmpl string) []string {
	var out []string
	for _, token := range ExtractTokens(tmpl) {
		if strings.HasPrefix(token, "metric.") {
			out = append(out, strings.TrimPrefix(token, "metric."))
		}
	}
	return out
}

// UsesChangeTokens reports whether a template needs the audit field diffs. The
// outbox tailer asks this before paying for a second query per event.
func UsesChangeTokens(tmpl string) bool {
	for _, token := range ExtractTokens(tmpl) {
		if strings.HasPrefix(token, "change.") {
			return true
		}
	}
	return false
}

// RenderContext carries the values a render may substitute. Anything absent
// renders as the placeholder.
type RenderContext struct {
	Now     time.Time
	Event   *audit.Entry
	Changes []audit.Change
	// Metrics are pre-resolved by key (without the "metric." prefix) so rendering
	// stays synchronous and cannot fail.
	Metrics map[string]int
}

// Render substitutes every token. It CANNOT fail: an unknown or unresolvable
// token becomes the placeholder, because a notification with one em dash in it
// is infinitely better than a notification that was never sent.
func Render(tmpl string, rc RenderContext) string {
	return tokenPattern.ReplaceAllStringFunc(tmpl, func(match string) string {
		sub := tokenPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return Placeholder
		}
		return resolveToken(sub[1], rc)
	})
}

func resolveToken(token string, rc RenderContext) string {
	switch token {
	case "now":
		return rc.Now.Format("15:04")
	case "date":
		// d. M. yyyy — the app-wide Czech date format.
		return fmt.Sprintf("%d. %d. %d", rc.Now.Day(), int(rc.Now.Month()), rc.Now.Year())
	}

	switch {
	case strings.HasPrefix(token, "event."):
		if rc.Event == nil {
			return Placeholder
		}
		switch token {
		case "event.summary":
			return orPlaceholder(rc.Event.Summary)
		case "event.action":
			return orPlaceholder(rc.Event.Action)
		case "event.module":
			return orPlaceholder(rc.Event.Module)
		case "event.entity_type":
			return orPlaceholder(rc.Event.EntityType)
		case "event.entity_id":
			return orPlaceholder(rc.Event.EntityID)
		case "event.actor_label":
			return orPlaceholder(rc.Event.ActorLabel)
		}
		return Placeholder

	case strings.HasPrefix(token, "change."):
		parts := strings.Split(token, ".")
		if len(parts) < 3 {
			return Placeholder
		}
		which := parts[len(parts)-1]
		field := strings.Join(parts[1:len(parts)-1], ".")
		for _, c := range rc.Changes {
			if c.Field != field {
				continue
			}
			v := c.New
			if which == "old" {
				v = c.Old
			}
			if v == nil || *v == "" {
				return Placeholder
			}
			return *v
		}
		return Placeholder

	case strings.HasPrefix(token, "metric."):
		key := strings.TrimPrefix(token, "metric.")
		if rc.Metrics == nil {
			return Placeholder
		}
		v, ok := rc.Metrics[key]
		if !ok {
			return Placeholder
		}
		return strconv.Itoa(v)
	}

	return Placeholder
}

func orPlaceholder(s string) string {
	if strings.TrimSpace(s) == "" {
		return Placeholder
	}
	return s
}

// PaletteFor returns the insertable tokens for a context. metricKeys come from
// the live registry so the composer can only ever offer what a module publishes.
func PaletteFor(context string, metricKeys []string) TokenPalette {
	p := TokenPalette{Time: append([]string(nil), timeTokens...)}
	switch context {
	case ContextTrigger:
		p.Event = append([]string(nil), eventTokens...)
		// The change palette is shape-based; the composer offers the two suffixes
		// and the admin names the field.
		p.Change = []string{"change.<pole>.old", "change.<pole>.new"}
	case ContextSummary:
		p.Metric = make([]string, 0, len(metricKeys))
		for _, k := range metricKeys {
			p.Metric = append(p.Metric, "metric."+k)
		}
	}
	return p
}
