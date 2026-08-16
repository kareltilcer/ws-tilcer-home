package push

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Handler serves /api/push/** — the per-user half of the channel (FR-P1–P3, P5).
//
// Every route here is open to EVERY authenticated member, `reader` included:
// subscribing a device and muting a category are personal preferences, not data
// mutations (D53, mirroring the reader-may-arrange-their-own-dashboard rule).
// Every route is scoped to the SESSION's user id — a member can neither see nor
// remove another member's device, whatever they put in the request body.
type Handler struct {
	svc  *Service
	db   *sql.DB
	sink audit.Sink
	now  func() time.Time
}

// NewHandler builds the push HTTP handler.
func NewHandler(svc *Service, db *sql.DB, sink audit.Sink) *Handler {
	now := svc.now
	if now == nil {
		now = time.Now
	}
	return &Handler{svc: svc, db: db, sink: sink, now: now}
}

// Mount registers the push routes on the authenticated /api router.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/push", func(pr chi.Router) {
		pr.Get("/vapid-key", h.vapidKey)
		pr.Post("/subscriptions", h.subscribe)
		pr.Delete("/subscriptions", h.unsubscribe)
		pr.Post("/test", h.test)
		pr.Get("/preferences", h.getPreferences)
		pr.Patch("/preferences", h.patchPreferences)
	})
}

type vapidKeyResponse struct {
	Key string `json:"key"`
}

// vapidKey serves the PUBLIC key the browser needs as applicationServerKey. The
// private half never leaves this process (§15).
func (h *Handler) vapidKey(w http.ResponseWriter, r *http.Request) {
	if !h.svc.Enabled() {
		// A 503 rather than an empty key: the settings panel can then say "push is
		// not configured on this server" instead of failing inside PushManager.
		httpx.WriteError(w, &httpx.APIError{
			Status: http.StatusServiceUnavailable,
			Code:   "push_disabled",
			Detail: "server has no VAPID keypair configured",
		})
		return
	}
	httpx.JSON(w, http.StatusOK, vapidKeyResponse{Key: h.svc.VAPIDPublicKey()})
}

type subscribeRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	UserAgent *string `json:"user_agent"`
}

// subscribe upserts this device's subscription (FR-P1).
func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) {
	actor, ok := reqctx.ActorFrom(r.Context())
	if !ok || actor.UserID == "" {
		httpx.WriteError(w, httpx.ErrUnauthorized(""))
		return
	}
	if !h.svc.Enabled() {
		httpx.WriteError(w, &httpx.APIError{
			Status: http.StatusServiceUnavailable,
			Code:   "push_disabled",
			Detail: "server has no VAPID keypair configured",
		})
		return
	}

	var req subscribeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	if err := validateSubscription(req, h.svc.cfg.AllowedEndpointHosts); err != nil {
		httpx.WriteError(w, err)
		return
	}

	ua := ""
	if req.UserAgent != nil {
		ua = *req.UserAgent
	}
	if ua == "" {
		// Fall back to the request's own UA so the settings panel can label the
		// device without the SPA having to send it.
		ua = r.UserAgent()
	}

	var (
		sub Subscription
		res UpsertResult
	)
	if err := appdb.WithTx(r.Context(), h.db, func(tx *sql.Tx) error {
		var err error
		sub, res, err = h.svc.store.Upsert(r.Context(), tx, actor.UserID, req.Endpoint, req.Keys.P256dh, req.Keys.Auth, ua, h.now())
		if err != nil {
			return err
		}
		switch {
		case res.Created:
			_, err = h.sink.Record(r.Context(), tx, audit.Event{
				Module: audit.ModulePlatform, Action: "push.subscribe",
				EntityType: "push_subscription", EntityID: sub.ID,
				Summary: fmt.Sprintf("Zapnutá oznámení na zařízení (%s)", deviceLabel(ua)),
			})
		case res.Transferred():
			// The endpoint already existed but under SOMEBODY ELSE. On a shared
			// household browser the endpoint outlives the session, so this is how a
			// device follows whoever is signed in — and the previous member stops
			// receiving. That is a consent decision on both sides, so it is audited
			// even though no row was created.
			_, err = h.sink.Record(r.Context(), tx, audit.Event{
				Module: audit.ModulePlatform, Action: "push.subscribe",
				EntityType: "push_subscription", EntityID: sub.ID,
				Summary: fmt.Sprintf("Oznámení na zařízení (%s) převzata od jiného člena", deviceLabel(ua)),
				Meta:    map[string]any{"previous_user_id": res.PreviousUserID},
			})
		default:
			// A refresh of the caller's OWN endpoint is not a new consent decision;
			// logging it on every page load would bury the real ones.
		}
		return err
	}); err != nil {
		httpx.WriteError(w, httpx.ErrInternal(""))
		return
	}

	status := http.StatusOK
	if res.Created {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, sub)
}

type unsubscribeRequest struct {
	Endpoint string `json:"endpoint"`
}

// unsubscribe removes this device (FR-P2). Idempotent: 204 even if it was
// already gone, because the service worker calls this on pushsubscriptionchange
// and must not have to care whether the row still exists.
func (h *Handler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	actor, ok := reqctx.ActorFrom(r.Context())
	if !ok || actor.UserID == "" {
		httpx.WriteError(w, httpx.ErrUnauthorized(""))
		return
	}
	var req unsubscribeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	if strings.TrimSpace(req.Endpoint) == "" {
		httpx.WriteError(w, httpx.ErrUnprocessable("endpoint is required"))
		return
	}

	if err := appdb.WithTx(r.Context(), h.db, func(tx *sql.Tx) error {
		removed, err := h.svc.store.Delete(r.Context(), tx, actor.UserID, req.Endpoint)
		if err != nil || !removed {
			return err
		}
		_, err = h.sink.Record(r.Context(), tx, audit.Event{
			Module: audit.ModulePlatform, Action: "push.unsubscribe",
			EntityType: "push_subscription",
			Summary:    "Vypnutá oznámení na zařízení",
		})
		return err
	}); err != nil {
		httpx.WriteError(w, httpx.ErrInternal(""))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// TestResult is what a self-test reports back: how many of the caller's own
// endpoints were attempted, and how many actually took the message.
type TestResult struct {
	Subscriptions int `json:"subscriptions"`
	Sent          int `json:"sent"`
}

// test sends a notification to the CALLER's own devices, bypassing their mutes
// (FR-P: the permission gauntlet ends in "did it actually arrive?"). It is open
// to every member, `reader` included, because it can only ever reach the
// caller's own endpoints — the recipient list is the session's user id, never
// anything from the request.
//
// It is the counterpart of the admin module's rule/summary test send: that one
// answers "does my text read right", this one answers "does this device work".
func (h *Handler) test(w http.ResponseWriter, r *http.Request) {
	actor, ok := reqctx.ActorFrom(r.Context())
	if !ok || actor.UserID == "" {
		httpx.WriteError(w, httpx.ErrUnauthorized(""))
		return
	}
	if !h.svc.Enabled() {
		httpx.WriteError(w, &httpx.APIError{
			Status: http.StatusServiceUnavailable,
			Code:   "push_disabled",
			Detail: "server has no VAPID keypair configured",
		})
		return
	}

	// Establish "has this account any endpoint at all?" BEFORE sending, and as its
	// own answerable question. Send reports an empty result for both "nothing to
	// send" and "the lookup failed", and the two must not collapse: telling a
	// member with a working subscription that no device is registered invites them
	// to unsubscribe and re-run the entire one-shot permission flow to fix a
	// problem they never had.
	subs, err := h.svc.store.ListForUser(r.Context(), actor.UserID)
	if err != nil {
		h.svc.logger.Error("push: list subscriptions for self-test", "err", err)
		httpx.WriteError(w, httpx.ErrInternal(""))
		return
	}
	if len(subs) == 0 {
		// No endpoint at all: the caller believes this device is subscribed and it
		// is not. Saying so is the whole point of the button.
		httpx.WriteError(w, httpx.ErrUnprocessable("na tomto účtu není registrované žádné zařízení"))
		return
	}

	results := h.svc.Send(r.Context(), []string{actor.UserID}, Envelope{
		Module: "platform", Type: "test",
		Title: "Home", Body: "Zkušební oznámení — na tomto zařízení to funguje.",
		URL: "/nastaveni", Category: CategoryBroadcast, Kind: KindTest,
		BypassMutes: true,
	})
	if len(results) == 0 {
		// The precheck just saw endpoints, so an empty fan-out means Send's own
		// resolve failed — a server fault, not a missing device.
		httpx.WriteError(w, httpx.ErrInternal(""))
		return
	}

	res := TestResult{Subscriptions: len(results)}
	for _, d := range results {
		if d.Status == StatusSent {
			res.Sent++
		}
	}

	if err := appdb.WithTx(r.Context(), h.db, func(tx *sql.Tx) error {
		_, err := h.sink.Record(r.Context(), tx, audit.Event{
			Module: audit.ModulePlatform, Action: "push.test",
			Summary: fmt.Sprintf("Zkušební oznámení na vlastní zařízení (%d z %d doručeno)", res.Sent, res.Subscriptions),
		})
		return err
	}); err != nil {
		// The push already went out; failing the request would only invite a
		// second one. The delivery log already holds the operational record.
		h.svc.logger.Error("push: audit self-test", "err", err)
	}

	httpx.JSON(w, http.StatusAccepted, res)
}

func (h *Handler) getPreferences(w http.ResponseWriter, r *http.Request) {
	actor, ok := reqctx.ActorFrom(r.Context())
	if !ok || actor.UserID == "" {
		httpx.WriteError(w, httpx.ErrUnauthorized(""))
		return
	}
	prefs, err := h.svc.store.Preferences(r.Context(), actor.UserID)
	if err != nil {
		httpx.WriteError(w, httpx.ErrInternal(""))
		return
	}
	httpx.JSON(w, http.StatusOK, prefs)
}

type preferencesPatchRequest struct {
	Enabled    *bool `json:"enabled"`
	Categories *struct {
		Broadcast *bool `json:"broadcast"`
		Triggers  *bool `json:"triggers"`
		Summaries *bool `json:"summaries"`
	} `json:"categories"`
}

// patchPreferences applies a partial mute update (FR-P5).
func (h *Handler) patchPreferences(w http.ResponseWriter, r *http.Request) {
	actor, ok := reqctx.ActorFrom(r.Context())
	if !ok || actor.UserID == "" {
		httpx.WriteError(w, httpx.ErrUnauthorized(""))
		return
	}
	var req preferencesPatchRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	if req.Enabled == nil && req.Categories == nil {
		httpx.WriteError(w, httpx.ErrUnprocessable("at least one preference must be set"))
		return
	}

	patch := PreferencesPatch{Enabled: req.Enabled}
	if req.Categories != nil {
		patch.Broadcast = req.Categories.Broadcast
		patch.Triggers = req.Categories.Triggers
		patch.Summaries = req.Categories.Summaries
	}

	var prefs Preferences
	if err := appdb.WithTx(r.Context(), h.db, func(tx *sql.Tx) error {
		var err error
		prefs, err = h.svc.store.UpdatePreferences(r.Context(), tx, actor.UserID, patch, h.now())
		if err != nil {
			return err
		}
		_, err = h.sink.Record(r.Context(), tx, audit.Event{
			Module: audit.ModulePlatform, Action: "push.prefs",
			Summary: fmt.Sprintf("Změna nastavení oznámení (%s)", prefsSummary(prefs)),
		})
		return err
	}); err != nil {
		httpx.WriteError(w, httpx.ErrInternal(""))
		return
	}
	httpx.JSON(w, http.StatusOK, prefs)
}

// validateSubscription rejects a malformed PushSubscription before it can reach
// the store. The keys are base64url from the browser's own crypto, so they are
// checked for shape only — the encryption layer is the real judge.
//
// The ENDPOINT is checked much harder than shape, because it is the field that
// decides where this server sends: it must parse as https and name one of the
// known push services (see DefaultPushServiceHosts).
func validateSubscription(req subscribeRequest, allowedHosts []string) error {
	if req.Endpoint == "" {
		return httpx.ErrUnprocessable("endpoint is required")
	}
	u, err := url.Parse(req.Endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return httpx.ErrUnprocessable("endpoint must be an https URL")
	}
	if !hostAllowed(u.Hostname(), allowedHosts) {
		// Named, not generic: this is the error a household hits when a browser
		// nobody anticipated shows up, and the fix is one config value away.
		return httpx.ErrUnprocessable(fmt.Sprintf(
			"neznámá push služba: %s (přidejte ji do HOME_PUSH_ENDPOINT_HOSTS)", u.Hostname()))
	}
	if req.Keys.P256dh == "" || req.Keys.Auth == "" {
		return httpx.ErrUnprocessable("keys.p256dh and keys.auth are required")
	}
	return nil
}

// hostAllowed matches a host against the allowlist, exactly or as a subdomain.
// The trailing-dot form of a hostname ("fcm.googleapis.com.") resolves to the
// same place, so it is normalised away rather than treated as a different host.
func hostAllowed(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, want := range allowed {
		want = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(want), "."))
		if want == "" {
			continue
		}
		if host == want || strings.HasSuffix(host, "."+want) {
			return true
		}
	}
	return false
}

// deviceLabel reduces a user-agent string to something a household member can
// recognise in the audit log. It is a label, not a fingerprint.
func deviceLabel(ua string) string {
	if ua == "" {
		return "neznámé zařízení"
	}
	switch {
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "Macintosh"):
		return "Mac"
	case strings.Contains(ua, "Windows"):
		return "Windows"
	case strings.Contains(ua, "Linux"):
		return "Linux"
	}
	// RUNES, not bytes: a byte slice can cut a multi-byte sequence in half, and
	// the result goes straight into an audit summary that encoding/json then
	// renders as replacement characters in the Log browser. Same rule, same
	// reason as truncateRunes and admin's delivery-error clip.
	return truncateRunes(ua, 40)
}

// prefsSummary renders the resulting mute state for the audit summary.
func prefsSummary(p Preferences) string {
	if !p.Enabled {
		return "vypnuto"
	}
	var on []string
	if p.Categories.Broadcast {
		on = append(on, "rozeslaná")
	}
	if p.Categories.Triggers {
		on = append(on, "upozornění")
	}
	if p.Categories.Summaries {
		on = append(on, "souhrny")
	}
	if len(on) == 0 {
		return "zapnuto, všechny kategorie ztlumené"
	}
	return "zapnuto: " + strings.Join(on, ", ")
}
