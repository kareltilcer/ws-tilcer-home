package admin

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
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
	List   []string `json:"list,omitempty"`
}

// KeyLister is the part of a catalog registry the engine needs: "may a template
// name this key?". Keeping it an interface means the template code depends on
// the catalogs' shape, not on either registry's implementation.
type KeyLister interface {
	Has(key string) bool
}

// ValidateTemplate checks every token in a template against its context's
// palette, returning a Czech reason suitable for a 422 on the field.
//
// `{{change.<field>.old|new}}` is validated by SHAPE rather than by name: the
// field set differs per entity type and per event, so demanding a known field
// here would refuse legitimate rules. An unknown field simply renders as the
// placeholder at fire time.
func ValidateTemplate(tmpl, context string, metrics, lists KeyLister) error {
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
		if err := validateToken(token, context, metrics, lists); err != nil {
			return err
		}
	}
	return nil
}

func validateToken(token, context string, metrics, lists KeyLister) error {
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
		// Numbers belong to summaries AND trigger rules (a rule may say "zbývá
		// {{metric.…}} úkolů"); only a broadcast has no fire moment to count at.
		if context == ContextBroadcast {
			return fmt.Errorf("číslo {{%s}} lze použít jen v pravidle nebo v souhrnu", token)
		}
		key := strings.TrimPrefix(token, "metric.")
		if metrics == nil || !metrics.Has(key) {
			return fmt.Errorf("neznámé číslo: {{%s}}", token)
		}
		return nil

	case strings.HasPrefix(token, "list."):
		if context == ContextBroadcast {
			return fmt.Errorf("seznam {{%s}} lze použít jen v pravidle nebo v souhrnu", token)
		}
		key := strings.TrimPrefix(token, "list.")
		if lists == nil || !lists.Has(key) {
			return fmt.Errorf("neznámý seznam: {{%s}}", token)
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

// ListKeysIn returns the list keys a template references — the scheduler uses
// this to resolve exactly the lists a summary names, and to decide whether it
// personalizes.
func ListKeysIn(tmpl string) []string {
	var out []string
	for _, token := range ExtractTokens(tmpl) {
		if strings.HasPrefix(token, "list.") {
			out = append(out, strings.TrimPrefix(token, "list."))
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
	// Lists are pre-resolved the same way, keyed without the "list." prefix.
	Lists map[string]ListValue
}

// clone copies the maps a later resolve pass writes into. A personalized send
// seeds every recipient from ONE shared household context, so the copy is what
// keeps Karel's pinned notes out of Eva's summary.
func (rc RenderContext) clone() RenderContext {
	out := rc
	out.Metrics = make(map[string]int, len(rc.Metrics))
	for k, v := range rc.Metrics {
		out.Metrics[k] = v
	}
	out.Lists = make(map[string]ListValue, len(rc.Lists))
	for k, v := range rc.Lists {
		out.Lists[k] = v
	}
	return out
}

// ListValue is one resolved list, as the renderer receives it: the module's
// items plus the module's word for "there are none". Formatting is done here
// rather than by the provider — see the package comment on platform/lists.
type ListValue struct {
	Items []string
	Empty string
}

// How a list is written into a notification body.
//
// One item per line with a bullet, because these tokens are read on a lock
// screen: "• Vynést koš⏎• Zalít kytky" is scannable where a comma-joined run of
// five titles is a wall. The caps exist because the whole payload is clamped to
// push.MaxBodyRunes before it is encrypted — an unbounded list would silently
// eat the sentence the admin wrote around it.
const (
	// maxListLines is how many items are named before the tail counts the rest.
	maxListLines = 5
	// maxListItemRunes clips one over-long title; whole notes can be pinned and
	// their titles are not written with a lock screen in mind.
	maxListItemRunes = 48
	listBullet       = "• "
	// minListLineRunes is the shortest a line may be clipped to. Below it a
	// bullet and an ellipsis is most of what survives, and a line the sender
	// cuts reads better than "• Vy…".
	minListLineRunes = 12
	// maxListsRunes is what ALL the lists in one template may occupy TOGETHER.
	//
	// Per-list caps alone do not keep the promise above: five 48-rune items plus
	// the tail is already ~266 of the sender's 300, so a second list token — the
	// composer offers every published list side by side — pushes the body past
	// the clamp. And that clamp cuts from the END, so what it takes is precisely
	// the "…a další N" tail, the one line that admits something was hidden.
	// Budgeting the lists against one shared allowance instead leaves ~90 runes
	// for the admin's own sentences whatever they insert, and makes a summary
	// drop a named item rather than its own honesty. Derived from the sender's
	// own clamp so tuning one cannot silently break the other.
	maxListsRunes = push.MaxBodyRunes - 90
)

// formatList renders a resolved list into notification text, within budget runes
// — this token's share of maxListsRunes.
func formatList(v ListValue, budget int) string {
	if len(v.Items) == 0 {
		// A module that published no empty text gets the placeholder — never an
		// empty string, which would leave "Dnes tě čeká:" hanging.
		if strings.TrimSpace(v.Empty) == "" {
			return Placeholder
		}
		return v.Empty
	}

	lines := make([]string, 0, len(v.Items))
	for _, item := range v.Items {
		// A title may carry newlines; left alone they would forge extra bullets.
		lines = append(lines, listBullet+truncate(strings.Join(strings.Fields(item), " "), maxListItemRunes))
	}

	// A tail of exactly one would read "…a další 1", which is bad Czech and
	// hides less than it costs — one line more is both shorter and better.
	shown, rest := lines, 0
	drop := func(n int) { shown, rest = lines[:len(lines)-n], n }
	if len(lines) > maxListLines+1 {
		drop(len(lines) - maxListLines)
	}
	// Then give up whole lines until the block fits its budget, so the tail is
	// counted here rather than amputated by the sender's clamp.
	for len(shown) > 1 && listRunes(shown, rest) > budget {
		drop(rest + 1)
	}
	if rest == 1 {
		if len(shown) > 1 {
			drop(2)
		} else {
			// Budgeting has already taken the block down to one line, so there is no
			// further line to give up. Name the second item rather than ship the tail
			// of one; the clip below is what pays for it.
			drop(0)
		}
	}

	// Dropping lines cannot always win: a list with items shows at least one, and
	// the rule above can force a second. n lists each keeping a full-width line
	// over budget is exactly how the shared allowance got overrun — and the
	// sender's clamp cuts from the END, taking the tail rather than the item. So
	// the last resort is to clip the lines still standing, here, where the tail
	// is safe. When even a legible clip cannot fit, give up another line and try
	// again rather than ship an over-budget block: the block MUST fit, because
	// past the budget the sender's clamp takes over and amputates the tails —
	// down to a lone clipped line (and, at the very smallest budgets, the tail
	// of one the line cap otherwise avoids: bad Czech beats losing the tail).
	for listRunes(shown, rest) > budget {
		room := budget - (len(shown) - 1) // the newlines between the lines
		if rest > 0 {
			room -= 1 + utf8.RuneCountInString(tailLine(rest))
		}
		if per := room / len(shown); per > minListLineRunes || len(shown) == 1 {
			clipped := make([]string, len(shown))
			for i, l := range shown {
				clipped[i] = truncate(l, max(per-1, 1)) // −1 for the ellipsis truncate adds back
			}
			shown = clipped
			break
		}
		drop(rest + 1)
	}

	out := strings.Join(shown, "\n")
	if rest > 0 {
		out += "\n" + tailLine(rest)
	}
	return out
}

// tailLine is the "…a další N" line — the one that admits something was hidden,
// and so the one the budget above exists to keep.
func tailLine(rest int) string {
	return fmt.Sprintf("%s…a další %d", listBullet, rest)
}

// listRunes is what a block of already-formatted lines costs once its separators
// and its "…a další N" tail are counted — what the sender's clamp will measure.
func listRunes(shown []string, rest int) int {
	n := len(shown) - 1 // the newlines between the lines
	for _, l := range shown {
		n += utf8.RuneCountInString(l)
	}
	if rest > 0 {
		n += 1 + utf8.RuneCountInString(tailLine(rest))
	}
	return n
}

// Render substitutes every token. It CANNOT fail: an unknown or unresolvable
// token becomes the placeholder, because a notification with one em dash in it
// is infinitely better than a notification that was never sent.
func Render(tmpl string, rc RenderContext) string {
	// Every list in this template shares one allowance, so inserting a second
	// one shortens both rather than overrunning the sender (see maxListsRunes).
	// Only lists that will actually render items claim a SHARE: an unresolved
	// token renders as the one-rune placeholder and an empty list as its short
	// empty text, and neither should cost a healthy list half its space. But
	// neither is free either, so what they will cost comes OFF the allowance
	// first — seven quiet lists beside one full one is exactly how the sender's
	// end-clamp got to take the "…a další N" tail this budget exists to keep.
	budget := maxListsRunes
	n := 0
	for _, key := range ListKeysIn(tmpl) {
		v := rc.Lists[key] // absent ⇒ the zero value, which formats as the placeholder
		if len(v.Items) > 0 {
			n++ // per occurrence: the same token twice renders — and costs — twice
			continue
		}
		budget -= utf8.RuneCountInString(formatList(v, 0))
	}
	if n > 1 {
		budget /= n
	}
	return tokenPattern.ReplaceAllStringFunc(tmpl, func(match string) string {
		sub := tokenPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return Placeholder
		}
		return resolveToken(sub[1], rc, budget)
	})
}

func resolveToken(token string, rc RenderContext, listBudget int) string {
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

	case strings.HasPrefix(token, "list."):
		key := strings.TrimPrefix(token, "list.")
		if rc.Lists == nil {
			return Placeholder
		}
		v, ok := rc.Lists[key]
		if !ok {
			// Unresolved (the module's read failed) is NOT the same as empty: the
			// placeholder says "we could not tell you", the module's empty text says
			// "there is nothing", and a summary must not confuse the two.
			return Placeholder
		}
		return formatList(v, listBudget)
	}

	return Placeholder
}

func orPlaceholder(s string) string {
	if strings.TrimSpace(s) == "" {
		return Placeholder
	}
	return s
}

// PaletteFor returns the insertable tokens for a context. metricKeys and
// listKeys come from the live registries so the composer can only ever offer
// what a module publishes.
func PaletteFor(context string, metricKeys, listKeys []string) TokenPalette {
	p := TokenPalette{Time: append([]string(nil), timeTokens...)}
	switch context {
	case ContextTrigger:
		p.Event = append([]string(nil), eventTokens...)
		// The change palette is shape-based; the composer offers the two suffixes
		// and the admin names the field.
		p.Change = []string{"change.<pole>.old", "change.<pole>.new"}
		// Trigger rules may also print counts and lists — the caller passes the
		// HOUSEHOLD-scoped keys only, because a trigger renders once for its
		// whole audience and a personal value has no recipient to belong to.
		fallthrough
	case ContextSummary:
		p.Metric = make([]string, 0, len(metricKeys))
		for _, k := range metricKeys {
			p.Metric = append(p.Metric, "metric."+k)
		}
		p.List = make([]string, 0, len(listKeys))
		for _, k := range listKeys {
			p.List = append(p.List, "list."+k)
		}
	}
	return p
}
