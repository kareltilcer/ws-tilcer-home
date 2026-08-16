// Package push is the single shared Web Push channel (PRD §V5-1 D52, HANDOFF-7
// §2). It lives in platform/ beside audit and ws precisely because EVERY module
// may send through it: there is one service worker on one origin, therefore one
// subscription per device, therefore one channel.
//
// The split that matters: this package owns the channel (VAPID keys, the
// subscription store, mute preferences, the fan-out). The `admin` module owns
// only the CONFIGURATION on top of it — what gets sent, to whom, and when. An
// admin can never subscribe a member's device, and a member's mutes are always
// honoured here, at send time, whatever the sender asked for (D53/D53a).
package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Categories are the mute buckets a member toggles independently (D53a). Every
// envelope declares one; a send is dropped for a member who muted that bucket.
const (
	CategoryBroadcast = "broadcast"
	CategoryTriggers  = "triggers"
	CategorySummaries = "summaries"
)

// Kinds label a delivery attempt in the operational log (§9). They are coarser
// than categories: a test send of a summary is kind "test", category "summaries".
const (
	KindBroadcast = "broadcast"
	KindTrigger   = "trigger"
	KindSchedule  = "schedule"
	KindTest      = "test"
)

// Delivery statuses recorded per endpoint attempt.
const (
	StatusSent    = "sent"
	StatusFailed  = "failed"
	StatusExpired = "expired"
)

// Envelope is one notification. Its two halves are deliberately different in
// kind, and only the first ever reaches the browser:
//
//   - the WIRE payload (Module…Data) is encrypted and delivered; the service
//     worker shows Title/Body and routes a click by Module/Type to URL;
//   - the DELIVERY metadata (Category…BypassMutes) never leaves the server. It
//     decides who is eligible and how the attempt is logged.
type Envelope struct {
	Module string         `json:"module"` // "admin" | "todo" | … — routes the SW click
	Type   string         `json:"type"`   // "broadcast" | "trigger" | "summary"
	Title  string         `json:"title"`
	Body   string         `json:"body"`
	URL    string         `json:"url"`            // in-app path a click opens; defaults to "/"
	Tag    string         `json:"tag,omitempty"`  // collapse key: a newer push replaces an older one
	Data   map[string]any `json:"data,omitempty"` // extra routing hints for the SW

	Category    string `json:"-"` // mute bucket (required)
	Kind        string `json:"-"` // delivery-log label (defaults per category — see kindForCategory)
	RuleID      string `json:"-"` // originating rule/schedule, for the delivery log
	BypassMutes bool   `json:"-"` // a self-test reaches the caller's own devices regardless (FR-ADM6)
}

// DeliveryResult is one attempt against one endpoint.
type DeliveryResult struct {
	UserID         string
	SubscriptionID string
	Endpoint       string
	Status         string // StatusSent | StatusFailed | StatusExpired
	Error          string // empty unless Status != sent
	Kind           string
	Category       string
	RuleID         string
}

// Recorder persists delivery attempts. The `notification_deliveries` table is
// owned by the admin module's migrations (§1), so platform does not write it
// directly — the module hands its recorder in at composition. With no recorder
// the channel still sends; it just keeps no operational log.
type Recorder interface {
	RecordDeliveries(ctx context.Context, results []DeliveryResult) error
}

// Sender is what callers (broadcast, the trigger listener, the scheduler) use.
type Sender interface {
	// Send fans out to every recipient's subscriptions, honouring their mute
	// preferences, and reports one result per endpoint attempted.
	//
	// It never returns an error: a push that cannot be delivered is a recorded
	// per-endpoint failure, never a failed caller. Callers run it OFF the request
	// path — no mutation may be slowed or failed by a push service.
	Send(ctx context.Context, recipients []string, e Envelope) []DeliveryResult
	// VAPIDPublicKey is the browser's applicationServerKey (FR-P3). The private
	// half never leaves this process.
	VAPIDPublicKey() string
	// Enabled reports whether a VAPID keypair is configured at all.
	Enabled() bool
}

// Config configures the channel. An empty keypair disables sending entirely.
type Config struct {
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
	// MaxFailDays prunes a subscription failing continuously for this long.
	MaxFailDays int
	// AllowedEndpointHosts constrains which hosts a subscription endpoint may
	// point at. Empty means DefaultPushServiceHosts; a deployment extends it
	// through EndpointHosts, and tests replace it outright.
	AllowedEndpointHosts []string
	// Workers bounds the concurrent fan-out (default 8).
	Workers int
	// TTL is how long the push service may hold an undelivered message.
	TTL time.Duration
	// HTTPClient is injected by tests; nil uses a sane default client — one with
	// a TIMEOUT, which is the whole reason this is defaulted here rather than
	// left to webpush-go (see NewService).
	HTTPClient webpush.HTTPClient
	// HTTPTimeout bounds one delivery attempt (default 15s). It only applies to
	// the default client; an injected HTTPClient carries its own policy.
	HTTPTimeout time.Duration
	Logger      *slog.Logger
	// Now is injected by tests.
	Now func() time.Time
}

const (
	defaultWorkers = 8
	defaultTTL     = 24 * time.Hour
	// defaultHTTPTimeout bounds ONE delivery attempt. A push service that accepts
	// the connection and then never answers must not be able to hold a goroutine
	// (or the scheduler's ticker) forever — see NewService.
	defaultHTTPTimeout = 15 * time.Second
	// Web Push caps an encrypted record at 4 KB; keep bodies well inside it.
	maxBodyRunes  = 300
	maxTitleRunes = 80
)

// DefaultPushServiceHosts is the allowlist a subscription endpoint's host must
// match — exactly, or as a subdomain (Mozilla and WNS shard across per-region
// subdomains, so the suffix form is required, not a convenience).
//
// The endpoint is the ONE field of a subscription that decides where this server
// sends. Accepting any https URL made it an outbound-request generator that any
// authenticated member — `reader` included, since the push routes are open to
// every role (D53) — could aim at a host of their choosing, retried for every
// notification until MaxFailDays prunes it. A browser only ever produces one of
// the hosts below, so constraining it costs nothing real.
//
// A new browser vendor is a one-line change here, or HOME_PUSH_ENDPOINT_HOSTS
// for a deployment that cannot wait for one.
var DefaultPushServiceHosts = []string{
	"fcm.googleapis.com",        // Chrome, Edge, Opera, Brave, Vivaldi
	"android.googleapis.com",    // older Chrome on Android
	"push.services.mozilla.com", // Firefox (updates.push.services.mozilla.com)
	"web.push.apple.com",        // Safari, and iOS inside an installed PWA
	"notify.windows.com",        // legacy Edge / WNS
}

// EndpointHosts is the effective allowlist for a deployment: the built-in vendor
// hosts plus whatever HOME_PUSH_ENDPOINT_HOSTS added. Extra hosts EXTEND the
// list rather than replacing it, so a deployment can never accidentally lock out
// the browsers everybody actually uses.
func EndpointHosts(extra []string) []string {
	if len(extra) == 0 {
		return DefaultPushServiceHosts
	}
	return append(append([]string(nil), DefaultPushServiceHosts...), extra...)
}

// Service is the concrete Sender over the platform subscription store.
type Service struct {
	store    *Store
	cfg      Config
	recorder Recorder
	logger   *slog.Logger
	now      func() time.Time
}

// NewService builds the push channel over sqldb.
func NewService(store *Store, cfg Config) *Service {
	if cfg.Workers <= 0 {
		cfg.Workers = defaultWorkers
	}
	if cfg.TTL <= 0 {
		cfg.TTL = defaultTTL
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = defaultHTTPTimeout
	}
	if len(cfg.AllowedEndpointHosts) == 0 {
		cfg.AllowedEndpointHosts = DefaultPushServiceHosts
	}
	if cfg.HTTPClient == nil {
		// webpush-go's own fallback is a bare `&http.Client{}` — NO timeout. That
		// is unusable here: a delivery runs on the scheduler's ticker goroutine
		// (synchronously, once per due summary) and on a detached goroutine for
		// triggers, neither of which has a deadline of its own. One push endpoint
		// that accepts the connection and never replies would stop every future
		// summary until the process restarts. So the timeout lives on the client.
		cfg.HTTPClient = &http.Client{Timeout: cfg.HTTPTimeout}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, cfg: cfg, logger: cfg.Logger, now: now}
}

// SetRecorder installs the delivery-log writer. Called once at composition, from
// the admin module (which owns the table).
func (s *Service) SetRecorder(r Recorder) { s.recorder = r }

// Store exposes the subscription store to the HTTP handler and the audience
// resolver.
func (s *Service) Store() *Store { return s.store }

func (s *Service) VAPIDPublicKey() string { return s.cfg.VAPIDPublicKey }

func (s *Service) Enabled() bool {
	return s.cfg.VAPIDPublicKey != "" && s.cfg.VAPIDPrivateKey != ""
}

// Send resolves recipients to eligible endpoints and fans out concurrently.
func (s *Service) Send(ctx context.Context, recipients []string, e Envelope) []DeliveryResult {
	if !s.Enabled() || len(recipients) == 0 {
		return nil
	}
	e = e.normalized()

	targets, err := s.store.EligibleSubscriptions(ctx, recipients, e.Category, e.BypassMutes)
	if err != nil {
		s.logger.Error("push: resolve subscriptions", "err", err, "category", e.Category)
		return nil
	}
	if len(targets) == 0 {
		return nil
	}

	payload, err := json.Marshal(e)
	if err != nil {
		s.logger.Error("push: marshal envelope", "err", err)
		return nil
	}

	results := make([]DeliveryResult, len(targets))
	var wg sync.WaitGroup
	sem := make(chan struct{}, s.cfg.Workers)
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t Subscription) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = s.deliver(ctx, t, payload, e)
		}(i, t)
	}
	wg.Wait()

	if s.recorder != nil {
		if err := s.recorder.RecordDeliveries(ctx, results); err != nil {
			// The delivery log is operational, not audit: losing a row must never
			// turn a delivered notification into an error.
			s.logger.Warn("push: record deliveries", "err", err, "count", len(results))
		}
	}
	sent, failed, expired := tally(results)
	s.logger.Info("push: fan-out complete",
		"kind", e.Kind, "category", e.Category, "rule_id", e.RuleID,
		"recipients", len(recipients), "subscriptions", len(targets),
		"sent", sent, "failed", failed, "expired", expired)
	return results
}

// deliver posts one encrypted message and applies the endpoint-health policy.
func (s *Service) deliver(ctx context.Context, sub Subscription, payload []byte, e Envelope) DeliveryResult {
	res := DeliveryResult{
		UserID: sub.UserID, SubscriptionID: sub.ID, Endpoint: sub.Endpoint,
		Kind: e.Kind, Category: e.Category, RuleID: e.RuleID,
	}

	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, &webpush.Options{
		HTTPClient:      s.cfg.HTTPClient,
		Subscriber:      s.cfg.VAPIDSubject,
		VAPIDPublicKey:  s.cfg.VAPIDPublicKey,
		VAPIDPrivateKey: s.cfg.VAPIDPrivateKey,
		TTL:             int(s.cfg.TTL.Seconds()),
		Urgency:         webpush.UrgencyNormal,
		Topic:           webpushTopic(e.Tag),
	})
	if err != nil {
		res.Status, res.Error = StatusFailed, err.Error()
		s.markFailing(ctx, sub)
		return res
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		// The endpoint is permanently dead — the browser was reset, the app was
		// uninstalled, or the subscription was revoked. Delete it: keeping it would
		// mean failing forever against an endpoint that can never come back.
		res.Status = StatusExpired
		res.Error = fmt.Sprintf("endpoint gone (%d)", resp.StatusCode)
		if err := s.store.DeleteSubscriptionByID(ctx, sub.ID); err != nil {
			s.logger.Warn("push: prune expired subscription", "err", err, "id", sub.ID)
		}
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		res.Status = StatusSent
		if err := s.store.ClearFailing(ctx, sub.ID, s.now().UTC()); err != nil {
			s.logger.Warn("push: clear failing_since", "err", err, "id", sub.ID)
		}
	default:
		res.Status = StatusFailed
		res.Error = fmt.Sprintf("push service returned %d", resp.StatusCode)
		s.markFailing(ctx, sub)
	}
	return res
}

// markFailing stamps failing_since (first failure) and prunes a subscription
// that has been failing for longer than MaxFailDays — a device that went away
// without unsubscribing, which would otherwise be retried forever.
func (s *Service) markFailing(ctx context.Context, sub Subscription) {
	now := s.now().UTC()
	if sub.FailingSince != nil && s.cfg.MaxFailDays > 0 {
		if now.Sub(*sub.FailingSince) > time.Duration(s.cfg.MaxFailDays)*24*time.Hour {
			if err := s.store.DeleteSubscriptionByID(ctx, sub.ID); err != nil {
				s.logger.Warn("push: prune long-failing subscription", "err", err, "id", sub.ID)
				return
			}
			s.logger.Info("push: pruned a subscription failing since", "id", sub.ID, "since", *sub.FailingSince)
			return
		}
	}
	if err := s.store.MarkFailing(ctx, sub.ID, now); err != nil {
		s.logger.Warn("push: mark failing_since", "err", err, "id", sub.ID)
	}
}

// normalized fills defaults and clamps the payload. Web Push allows ~4 KB
// encrypted; an over-long body is truncated rather than dropped, because a
// clipped notification is far better than a silent non-delivery.
func (e Envelope) normalized() Envelope {
	if e.URL == "" {
		e.URL = "/"
	}
	if e.Category == "" {
		e.Category = CategoryBroadcast
	}
	if e.Kind == "" {
		e.Kind = kindForCategory(e.Category)
	}
	e.Title = truncateRunes(e.Title, maxTitleRunes)
	e.Body = truncateRunes(e.Body, maxBodyRunes)
	return e
}

// kindForCategory is the default delivery-log label for a category. The two
// vocabularies are deliberately DIFFERENT — categories are the member's mute
// buckets, kinds are how a send was triggered — so a caller that leaves Kind
// empty must be translated, never copied: the delivery log's CHECK constraint
// accepts only the four Kind* values, and a "triggers" written into it fails the
// whole batch insert, silently losing the record of a notification that DID go out.
func kindForCategory(category string) string {
	switch category {
	case CategoryTriggers:
		return KindTrigger
	case CategorySummaries:
		return KindSchedule
	default:
		return KindBroadcast
	}
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max-1])) + "…"
}

// webpushTopic maps a collapse tag onto the Topic header. The header is
// restricted to base64url characters, so anything else is dropped rather than
// risking a 400 from the push service.
func webpushTopic(tag string) string {
	if tag == "" || len(tag) > 32 {
		return ""
	}
	for _, r := range tag {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return ""
		}
	}
	return tag
}

func tally(results []DeliveryResult) (sent, failed, expired int) {
	for _, r := range results {
		switch r.Status {
		case StatusSent:
			sent++
		case StatusExpired:
			expired++
		default:
			failed++
		}
	}
	return
}

// Disabled is the inert channel used when no VAPID keypair is configured: the
// app runs, the settings panel explains itself, and nothing is ever sent.
type Disabled struct{}

func (Disabled) Send(context.Context, []string, Envelope) []DeliveryResult { return nil }
func (Disabled) VAPIDPublicKey() string                                    { return "" }
func (Disabled) Enabled() bool                                             { return false }

// ErrPushDisabled is returned by the HTTP layer when a device tries to subscribe
// while the server has no keypair.
var ErrPushDisabled = errors.New("push is not configured on this server")
