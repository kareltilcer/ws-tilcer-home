// Package config loads and validates the service configuration from the
// environment (PRD §9). It fails fast and loudly: a missing required variable
// or an invalid value aborts startup with a message listing every problem —
// a silently-defaulted secret is worse than a crash.
package config

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is the fully-validated runtime configuration. All fields are safe to
// read concurrently after Load returns.
type Config struct {
	// Env is the deployment environment: "development" (default) or "production".
	// It gates the dev auth bypass — the bypass is refused outright in production.
	Env string

	// Addr is the TCP listen address for the HTTP server, e.g. ":8080".
	Addr string

	// DBPath is the SQLite database file path (persisted volume in production).
	DBPath string

	// StaticDir is the directory of the built SPA the server serves on non-API
	// routes (with an index.html fallback for client-side routes). Empty in
	// development, where Vite serves the SPA and proxies /api + /ws to Go.
	StaticDir string

	// SiteKey is the auth site key this service authenticates against (default "home").
	SiteKey string

	// AuthBaseURL is the shared auth service base URL (login redirect,
	// /introspect, /token/refresh), e.g. "https://auth.tilcer.cz".
	AuthBaseURL string

	// AuthServiceSecret is the auth service-client secret bound to the site,
	// used to authenticate /introspect calls. Never logged.
	AuthServiceSecret string

	// AuthJWTSecret is the shared HS256 secret home uses to VERIFY the site-scoped
	// access tokens returned by auth's /internal/login and /internal/token/mint
	// (their identity + roles live in the JWT claims, not the JSON envelope). It
	// must equal the auth service's JWT signing secret. Never logged.
	AuthJWTSecret string

	// AuthJWTIssuer, when set, is the exact `iss` claim home requires on those
	// access tokens (auth stamps its OWN base URL, e.g. https://auth.tilcer.cz —
	// which is NOT the same as AuthBaseURL, the URL home CALLS auth at, e.g.
	// https://auth.tilcer.cz/api). Left empty (the default) the issuer is not
	// checked; the token signature + audience already bind it to auth and this
	// site. Set it only as extra hardening when you know auth's exact issuer.
	AuthJWTIssuer string

	// AllowedOrigins is the CSRF Origin allowlist for cookie-authenticated
	// mutations (Mode B, FR-A5). Entries may be exact origins or wildcards
	// ("https://*.tilcer.cz"). Home's own calls are same-origin (no CORS).
	AllowedOrigins []string

	// SessionTTLDays is home's own session sliding window (Mode B, D29; default 90).
	SessionTTLDays int

	// RoleRefreshMinutes is how often home re-mints to refresh a session's cached
	// roles against auth (Mode B, FR-A2; default 15).
	RoleRefreshMinutes int

	// Warnings are the configuration problems that did NOT stop the boot — today,
	// a window clamped to its ceiling. The composition root logs them at Warn
	// right after Load. Empty on a clean configuration.
	Warnings []string

	// WSRevalidateMinutes is how often an already-open websocket re-takes its
	// session decision (v10; default 5). It is the upper bound on how long a
	// socket keeps receiving member-restricted payloads after a revocation that
	// auth could not announce — an expiring TTL, a row changed out of band — so it
	// is tunable without a rebuild, like the two windows above.
	WSRevalidateMinutes int

	// Timezone is the IANA location used for "today", month boundaries, and
	// recurrence expansion (never UTC). TimezoneName is its original string.
	Timezone     *time.Location
	TimezoneName string

	// DashboardLookbackDays bounds how long an uncompleted reminder stays on
	// Nástěnka after its date (default 30).
	DashboardLookbackDays int

	// RRuleMaxOccurrences caps occurrence expansion per event per request (default 500).
	RRuleMaxOccurrences int

	// RRuleMaxWindowMonths caps the requested occurrence window span (default 24).
	RRuleMaxWindowMonths int

	// LogRetentionDays is the audit prune threshold; 0 = keep forever (default 0).
	LogRetentionDays int

	// NotesImageMaxUploadMB is the hard per-image cap for pasted/dropped note images;
	// over it the upload is rejected 413 (default 10). The image BYTES reuse the
	// documents R2 bucket (a distinct note-images/ prefix) and its backup mirror.
	NotesImageMaxUploadMB int

	// Storage is the v9 Úložiště page's configuration: two plain vars, neither a
	// secret, and NOTHING for privacy itself. A privacy feature with a kill switch
	// is a privacy feature whose guarantee depends on an environment variable
	// nobody re-reads (§V9-9).
	Storage StorageConfig

	// ChatTrashDays is how long a deleted conversation sits in the koš before the
	// drain destroys its bytes (default 7). ⚠ It is chat's ONLY environment
	// variable: the module's two storage thresholds are DATABASE ROWS, not vars,
	// because `admin` edits them at runtime and a limit somebody has to redeploy to
	// change is a limit nobody changes (D236).
	ChatTrashDays int

	// DevAuthBypass, when true (and only outside production), skips real JWT
	// introspection and injects a fake actor so the app runs offline. It is a
	// development-only convenience and a security hole if ever enabled in prod.
	DevAuthBypass bool

	// DevActorID and DevActorRoles configure the fake actor used under the bypass.
	DevActorID    string
	DevActorRoles []string

	// Docs is the documents module's configuration (v4): object storage for the
	// file bytes, the preview pipeline, and the blob backup mirror.
	Docs DocsConfig

	// Notif is the v5 notification configuration: the shared Web Push channel
	// (VAPID) and the wall-clock scheduler that fires summary notifications.
	Notif NotifConfig

	// Garden is the v7 garden module's configuration: the outbound forecast, and
	// nothing else. The module runs with none of these set.
	Garden GardenConfig

	// Status is the outbound crash-reporting configuration (status.tilcer.cz).
	// It is the second external dependency this service has, and — like the
	// garden's forecast — it is soft: with none of it set the app runs exactly as
	// before and reports nothing.
	Status StatusConfig
}

// StatusConfig points the crash reporter at one site's ingest endpoint on
// status.tilcer.cz. The variable NAMES are not HOME_-prefixed on purpose: they
// are the fleet-wide convention every ws-tilcer-* service reads (the status
// integration guide's wiring checklist), so a second service is configured from
// the same muscle memory.
type StatusConfig struct {
	// IngestURL is the full per-site endpoint, e.g.
	// https://status.tilcer.cz/api/ingest/home.
	IngestURL string

	// IngestKey is that site's ingest key (ik_…), sent as X-Ingest-Key. It is a
	// secret on the SERVER side — the browser has its own, deliberately public
	// one baked into the SPA — so it is never logged.
	IngestKey string

	// Environment tags every event ("prod" / "dev"). Defaults to HOME_ENV.
	Environment string

	// Release tags every event, e.g. "home@2026.36.1". Free-form; empty is fine
	// and simply leaves the field off.
	Release string
}

// Enabled reports whether crash reporting is configured. Both halves are needed:
// the endpoint says which site, the key authenticates it.
func (s StatusConfig) Enabled() bool { return s.IngestURL != "" && s.IngestKey != "" }

// The crash reporter's environment variables, named here rather than written
// inline like every other key in this file — because these four are the only
// ones read TWICE. cmd/home builds the reporter from them before Load runs, so
// that a configuration error or a failed migration is itself reportable, and two
// spellings of one variable would be a feature that is simply off with nothing
// anywhere saying so.
//
// EnvHomeEnv and DefaultEnv are exported for the same reason and no other: the
// pre-config reporter has to reproduce the environment tag this file would have
// given it, and a second literal spelling of either is how one deployment ends
// up on the board under two names.
const (
	EnvStatusIngestURL   = "STATUS_INGEST_URL"
	EnvStatusIngestKey   = "STATUS_INGEST_KEY"
	EnvStatusEnvironment = "STATUS_ENVIRONMENT"
	EnvStatusRelease     = "STATUS_RELEASE"

	EnvHomeEnv = "HOME_ENV"
	DefaultEnv = "development"
)

// GardenConfig configures the ONE external dependency v7 adds (PRD §V7-9).
//
// Every field has a working default, and the module degrades to manual frost
// dates with no user-visible error when the fetch is off or fails — a forecast
// that did not load is not something anyone can act on (D112).
//
// NOTE WHAT IS NOT HERE: latitude, longitude and altitude. They live in
// garden_settings next to the frost dates they serve (D112) because they are
// user data rather than secrets, and a typo should not need a redeploy. Nor is
// there any notification setting — audience, timing and channels are all
// Administrace's (D113).
type GardenConfig struct {
	// WeatherEnabled is the master switch. false ⇒ manual frost dates only; the
	// metric resolves to "no data" and any condition gating on it stays silent.
	WeatherEnabled bool
	// WeatherURL is the forecast endpoint. Overridable for testing or if the
	// provider changes.
	WeatherURL string
	// WeatherPollHours is the poll interval, 1–24.
	WeatherPollHours int
	// ImportMaxBytes caps the pasted LLM JSON. It is untrusted input from a
	// language model, so the size limit is the first of four defences (the others
	// are schema validation, explicit enum mapping, and applying only after a
	// preview).
	ImportMaxBytes int64
}

// NotifConfig configures the v5 push channel, the trigger notifier, and the
// summary scheduler (PRD §V5-9, HANDOFF-7 §14).
//
// The VAPID keypair is a SECRET and identifies this server to every browser push
// service. It is generated ONCE (cmd/vapidgen) and never rotated casually:
// rotating it invalidates every existing subscription, silently — every device
// would have to re-subscribe. Only the PUBLIC half is ever served (FR-P3).
type NotifConfig struct {
	// VAPIDPublicKey / VAPIDPrivateKey are base64url (raw, unpadded): an
	// uncompressed P-256 point (65 bytes) and its scalar (32 bytes). Both empty
	// disables push entirely; exactly one set is a misconfiguration.
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	// VAPIDSubject identifies the sender to the push service — a "mailto:" or
	// "https://" URL. Required once the keypair is set.
	VAPIDSubject string

	// CoalesceDefault is the per-rule default window for collapsing a burst of one
	// trigger rule's matches into a single push (D57). 0 = fire every event.
	CoalesceDefault time.Duration
	// DeliveryRetentionDays prunes notification_deliveries beyond this age;
	// 0 = keep forever (D64). Deliveries are operational, not audit.
	DeliveryRetentionDays int
	// MaxFailDays prunes a subscription that has been failing continuously for
	// this long (a device that was wiped without unsubscribing).
	MaxFailDays int

	// PushEndpointHosts are EXTRA push-service hosts a subscription endpoint may
	// name, on top of the built-in vendor list (push.DefaultPushServiceHosts).
	// The endpoint decides where this server sends, so it is allowlisted rather
	// than taken on trust; this is the escape hatch for a browser the built-in
	// list has not caught up with. Matched exactly or as a subdomain.
	PushEndpointHosts []string

	// SchedTick is the scheduler's granularity; slots are minute-resolution, so
	// anything above a minute would start missing them.
	SchedTick time.Duration
	// CatchupGrace fires a slot missed while the process was down, if the backend
	// is back within this window; older misses are skipped rather than backfilled
	// as a storm (D58a).
	CatchupGrace time.Duration
}

// PushEnabled reports whether a usable VAPID keypair is configured. When false
// the push channel is inert: subscriptions are refused and nothing is sent.
func (n NotifConfig) PushEnabled() bool {
	return n.VAPIDPublicKey != "" && n.VAPIDPrivateKey != ""
}

// DocsConfig configures the documents module (PRD §9, HANDOFF-6 §16). The file
// BYTES live in object storage (SQLite keeps only metadata), so this is the one
// module with storage configuration of its own.
//
// Two deliberate deviations from HANDOFF-6 §16, decided with Karel:
//   - Office→PDF conversion runs in a **Gotenberg sidecar** (GotenbergURL)
//     instead of a headless LibreOffice binary inside this image, so the runtime
//     image stays small. HOME_DOCS_SOFFICE_PATH therefore does not exist.
//   - The blob mirror runs **daily**, not hourly, and HOME_DOCS_MIRROR_CRON is a
//     Go duration (e.g. "24h", "0" disables) rather than a cron expression — no
//     cron parser, and the value reads the way the job behaves.
type DocsConfig struct {
	// Primary bucket for document bytes. Empty selects the local filesystem store
	// (LocalDir) — development and tests only; refused in production.
	R2Bucket          string
	R2Endpoint        string
	R2AccessKeyID     string
	R2SecretAccessKey string

	// Backup bucket for the mirror (D45). Empty disables mirroring. The endpoint
	// and keys default to the primary's when the backup lives in the same account.
	BackupBucket          string
	BackupEndpoint        string
	BackupAccessKeyID     string
	BackupSecretAccessKey string

	// MirrorInterval is how often the mirror + reconciliation pass runs; 0 disables it.
	MirrorInterval time.Duration
	// OrphanGraceHours is how old an object with no live row must be before the
	// reconciliation pass deletes it (an in-flight upload writes the object before
	// the row, so young orphans are normal).
	OrphanGraceHours int
	// OrphanMaxPercent bounds how much of the bucket ONE pass may delete before it
	// refuses and logs instead. The sweep infers "orphan" from a missing row, so an
	// empty or half-restored database would otherwise read as "every object is
	// orphaned" — this is the guard against that. 100 disables it.
	OrphanMaxPercent int

	// MaxUploadMB is the hard per-file cap; over it the upload is rejected 413.
	MaxUploadMB int
	// AllowedMIME, when non-empty, is the allowlist checked against the
	// SERVER-SNIFFED type (never the client's claim, D48). Empty = allow all.
	AllowedMIME []string

	// PreviewEnabled gates the whole preview/thumbnail worker.
	PreviewEnabled bool
	// PreviewTimeout bounds one conversion/thumbnail job.
	PreviewTimeout time.Duration
	// PreviewWorkers is the size of the in-process worker pool.
	PreviewWorkers int
	// GotenbergURL is the Office→PDF converter's base URL (e.g.
	// http://gotenberg:3000). Empty leaves Office files download-only.
	GotenbergURL string
	// PdftoppmPath and CwebpPath are the thumbnail helpers (poppler-utils,
	// libwebp-tools). A missing binary degrades to "no thumbnail", never an error.
	PdftoppmPath string
	CwebpPath    string
	// ThumbMaxPx is the longest edge of a generated thumbnail.
	ThumbMaxPx int
	// ImageMaxMegapixels caps the DECODED size of an image the worker will
	// thumbnail. MaxUploadMB bounds the file, not the pixels — compression ratio is
	// unlimited — and the worker decodes in the app process, so an unbounded decode
	// is an OOM of the whole backend rather than a failed thumbnail.
	ImageMaxMegapixels int

	// LocalDir backs the filesystem BlobStore when R2Bucket is empty.
	LocalDir string
	// PublicBaseURL prefixes the permanent /d/{id} links; empty keeps them
	// relative to the app origin (correct for home's same-origin deploy).
	PublicBaseURL string
}

// UsesObjectStorage reports whether documents run against a real S3/R2 bucket
// (as opposed to the local development filesystem store).
func (d DocsConfig) UsesObjectStorage() bool { return d.R2Bucket != "" }

// MirrorEnabled reports whether the daily blob mirror should run.
func (d DocsConfig) MirrorEnabled() bool {
	return d.BackupBucket != "" && d.MirrorInterval > 0
}

// IsProduction reports whether the service is running in production.
func (c *Config) IsProduction() bool { return c.Env == "production" }

// Redacted returns a log-safe one-line summary of the configuration with the
// service secret masked.
func (c *Config) Redacted() string {
	secret := "unset"
	if c.AuthServiceSecret != "" {
		secret = "set(***)"
	}
	jwtSecret := "unset"
	if c.AuthJWTSecret != "" {
		jwtSecret = "set(***)"
	}
	static := c.StaticDir
	if static == "" {
		static = "none"
	}
	jwtIssuer := c.AuthJWTIssuer
	if jwtIssuer == "" {
		jwtIssuer = "any"
	}
	return fmt.Sprintf(
		"env=%s addr=%s db=%s static=%s site=%s auth_base=%s auth_secret=%s jwt_secret=%s jwt_issuer=%s tz=%s "+
			"lookback=%d rrule_max=%d rrule_window_months=%d log_retention=%d "+
			"session_ttl_days=%d role_refresh_min=%d ws_revalidate_min=%d origins=%v dev_auth_bypass=%t %s %s %s",
		c.Env, c.Addr, c.DBPath, static, c.SiteKey, c.AuthBaseURL, secret, jwtSecret, jwtIssuer, c.TimezoneName,
		c.DashboardLookbackDays, c.RRuleMaxOccurrences, c.RRuleMaxWindowMonths,
		c.LogRetentionDays, c.SessionTTLDays, c.RoleRefreshMinutes, c.WSRevalidateMinutes, c.AllowedOrigins, c.DevAuthBypass,
		c.Docs.redacted(), c.Notif.redacted(), c.Status.redacted(),
	)
}

// redacted summarises the crash reporter. The URL is not a secret — it names the
// site and is the first thing to check when nothing arrives on the board — but
// the ingest key is, so only its prefix is printed, which is enough to tell a
// paste error from a rotation without putting the key in Coolify's log.
func (s StatusConfig) redacted() string {
	if !s.Enabled() {
		return "status_report=off"
	}
	release := s.Release
	if release == "" {
		release = "unset"
	}
	return fmt.Sprintf("status_report=on(%s env=%s release=%s key=%s)",
		s.IngestURL, s.Environment, release, redactKey(s.IngestKey))
}

// redacted summarises the notification configuration. The VAPID keys are secrets;
// only whether they are present is ever logged.
func (n NotifConfig) redacted() string {
	push := "off(no VAPID keypair)"
	if n.PushEnabled() {
		push = fmt.Sprintf("on(subject=%s)", n.VAPIDSubject)
	}
	retention := "forever"
	if n.DeliveryRetentionDays > 0 {
		retention = fmt.Sprintf("%dd", n.DeliveryRetentionDays)
	}
	hosts := "builtin"
	if len(n.PushEndpointHosts) > 0 {
		// Listed in full: an endpoint refused as an "unknown push service" is
		// diagnosed straight from the boot line.
		hosts = fmt.Sprintf("builtin+%v", n.PushEndpointHosts)
	}
	return fmt.Sprintf("push=%s notif_coalesce=%s notif_retention=%s notif_max_faildays=%d push_hosts=%s sched_tick=%s sched_catchup=%s",
		push, n.CoalesceDefault, retention, n.MaxFailDays, hosts, n.SchedTick, n.CatchupGrace)
}

// redacted summarises the documents configuration with the storage secrets masked.
func (d DocsConfig) redacted() string {
	store := "fs(" + d.LocalDir + ")"
	if d.UsesObjectStorage() {
		store = fmt.Sprintf("r2(%s@%s keys=%s)", d.R2Bucket, d.R2Endpoint, maskPair(d.R2AccessKeyID, d.R2SecretAccessKey))
	}
	// The job does two jobs: mirroring to the backup bucket AND the reconciliation
	// pass, which DELETES orphaned objects from the primary. The pass runs on the
	// interval whether or not a backup bucket exists, so "off" here would be a lie
	// about a destructive daily job — the interval alone decides that it runs.
	mirror := "off"
	switch {
	case d.MirrorInterval <= 0:
	case d.MirrorEnabled():
		mirror = fmt.Sprintf("%s→%s", d.MirrorInterval, d.BackupBucket)
	default:
		mirror = fmt.Sprintf("%s(reconcile-only, no backup bucket)", d.MirrorInterval)
	}
	preview := "off"
	if d.PreviewEnabled {
		gotenberg := d.GotenbergURL
		if gotenberg == "" {
			gotenberg = "none(office=download-only)"
		}
		preview = fmt.Sprintf("on(workers=%d timeout=%s gotenberg=%s)", d.PreviewWorkers, d.PreviewTimeout, gotenberg)
	}
	allowed := "all"
	if len(d.AllowedMIME) > 0 {
		allowed = strings.Join(d.AllowedMIME, "|")
	}
	return fmt.Sprintf("docs_store=%s docs_mirror=%s docs_preview=%s docs_max_upload_mb=%d docs_allowed_mime=%s",
		store, mirror, preview, d.MaxUploadMB, allowed)
}

func maskPair(id, secret string) string {
	if id == "" && secret == "" {
		return "unset"
	}
	if id == "" || secret == "" {
		return "partial(***)"
	}
	return "set(***)"
}

// Getenv is the environment lookup used by Load; it mirrors os.LookupEnv and is
// injected in tests.
type Getenv func(key string) (string, bool)

// Defaults for optional variables. HOME_ENV's default is DefaultEnv, up beside
// the status keys, because cmd/home needs it too.
const (
	defaultAddr                 = ":8080"
	defaultSiteKey              = "home"
	defaultTimezone             = "Europe/Prague"
	defaultDashboardLookback    = 30
	defaultRRuleMaxOccurrences  = 500
	defaultRRuleMaxWindowMonths = 24
	defaultLogRetentionDays     = 0
	defaultSessionTTLDays       = 90
	defaultRoleRefreshMinutes   = 15
	defaultWSRevalidateMinutes  = 5
	// ⚠ ALL THREE SESSION WINDOWS ARE BOUNDED AT BOTH ENDS, and the upper bound is
	// not tidiness. Each is multiplied into a time.Duration in the composition
	// root, and a large enough value OVERFLOWS int64 nanoseconds into a NEGATIVE
	// duration that every comparison then reads backwards: a negative role-refresh
	// threshold re-mints on every single request and every revalidation tick, and
	// a negative session TTL issues cookies with a negative MaxAge — the browser
	// deletes them on arrival — against rows that are already expired. Both load
	// silently and break login.
	//
	// The caps are ten years, a day and a day: past those the window has stopped
	// bounding the incident it exists for, long before arithmetic is the problem.
	//
	// ⚠ THE TWO PRE-EXISTING WINDOWS ARE CLAMPED TO THE CAP, NOT REFUSED, AND THAT
	// IS THE WHOLE DIFFERENCE BETWEEN A DEGRADED SETTING AND AN OUTAGE.
	// HOME_SESSION_TTL_DAYS and HOME_ROLE_REFRESH_MINUTES have shipped with a
	// FLOOR check only, and every secret and window on this service lives in
	// Coolify (see CLAUDE.md) — so nothing in this repo can say what they are set
	// to. Refusing an over-cap value aborts Load, which does not degrade a
	// feature: it crash-loops the container on the deploy that lands v10, with the
	// only signal a log line inside a restarting container. Clamping closes the
	// overflow hole just as completely (the arithmetic never sees the big value)
	// and the loud warning names the variable, so an operator finds it from the
	// logs of a service that is UP. HOME_WS_REVALIDATE_MINUTES is new in v10 and
	// has no deployed value to break, so it is range-checked outright.
	maxSessionTTLDays      = 3650
	maxRoleRefreshMinutes  = 1440
	maxWSRevalidateMinutes = 1440

	// documents (v4)
	defaultDocsMirrorInterval   = 24 * time.Hour
	defaultDocsOrphanGraceHours = 24
	defaultDocsOrphanMaxPercent = 25
	defaultDocsMaxUploadMB      = 50
	defaultDocsPreviewTimeout   = 60
	defaultDocsPreviewWorkers   = 2
	defaultDocsPdftoppm         = "pdftoppm"
	defaultDocsCwebp            = "cwebp"
	defaultDocsThumbMaxPx       = 480
	defaultDocsImageMaxMP       = 50

	// notes (v4.1) inline images
	defaultNotesImageMaxUploadMB = 10

	// chat (v10). Seven days is long enough that "I deleted the wrong room" is
	// recoverable over a weekend away, and short enough that the bytes of a
	// conversation somebody deleted TO FREE SPACE actually go. `?hard=true` is
	// there for when seven days is still too long.
	defaultChatTrashDays = 7

	// defaultStorageWarnTotalMB is a CHANGE detector, not a bill detector (D196).
	// R2's free allowance is 10 GB and household usage is expected to sit well
	// under a gigabyte, so a threshold parked at the billing cliff would stay
	// silent for years and teach nobody anything. At 1 GB the line fires when
	// something has CHANGED — a runaway preview job, an unusually large upload, a
	// private tree growing faster than anyone expected — with nine-tenths of the
	// allowance still in hand. A smoke alarm, not an invoice.
	defaultStorageWarnTotalMB = 1024
	// defaultStorageCacheSeconds keeps the snapshot cheap without making it state:
	// nothing survives a restart, and nothing can be stale for longer than a minute.
	defaultStorageCacheSeconds = 60

	// notifications + scheduler (v5)
	defaultNotifCoalesce         = 60 * time.Second
	defaultNotifRetentionDays    = 30
	defaultNotifMaxFailDays      = 14
	defaultSchedTick             = 60 * time.Second
	defaultSchedCatchupGraceMins = 120

	// Byte lengths of a decoded VAPID keypair: an uncompressed P-256 point
	// (0x04 ‖ X ‖ Y) and its 256-bit private scalar.
	vapidPublicKeyBytes  = 65
	vapidPrivateKeyBytes = 32
)

// defaultAllowedOrigins is the CSRF Origin allowlist when HOME_ALLOWED_ORIGINS is
// unset (PRD §9).
var defaultAllowedOrigins = []string{"https://*.tilcer.cz"}

// LoadFromEnv loads the configuration using os.LookupEnv.
func LoadFromEnv() (*Config, error) {
	return Load(osLookup)
}

// Load reads, defaults, and validates configuration from getenv. On any problem
// it returns a single error enumerating every issue found (not just the first).
func Load(getenv Getenv) (*Config, error) {
	l := &loader{getenv: getenv}
	c := &Config{}

	c.Env = l.strDefault(EnvHomeEnv, DefaultEnv)
	if c.Env != "development" && c.Env != "production" {
		l.errf("HOME_ENV must be \"development\" or \"production\" (got %q)", c.Env)
	}
	c.Addr = l.strDefault("HOME_ADDR", defaultAddr)
	c.DBPath = l.strRequired("HOME_DB_PATH")
	c.StaticDir = l.strDefault("HOME_STATIC_DIR", "")
	c.SiteKey = l.strDefault("HOME_SITE_KEY", defaultSiteKey)

	c.DevAuthBypass = l.boolDefault("HOME_DEV_AUTH_BYPASS", false)
	c.DevActorID = l.strDefault("HOME_DEV_ACTOR_ID", "dev-user")
	c.DevActorRoles = l.csvDefault("HOME_DEV_ACTOR_ROLES", []string{"admin"})

	// The auth service is only strictly required when the bypass is off; offline
	// development with the bypass on does not need to reach auth.tilcer.cz.
	if c.DevAuthBypass {
		c.AuthBaseURL = l.strDefault("AUTH_BASE_URL", "")
		c.AuthServiceSecret = l.strDefault("HOME_AUTH_SERVICE_SECRET", "")
		c.AuthJWTSecret = l.strDefault("HOME_AUTH_JWT_SECRET", "")
	} else {
		c.AuthBaseURL = l.strRequired("AUTH_BASE_URL")
		c.AuthServiceSecret = l.strRequired("HOME_AUTH_SERVICE_SECRET")
		c.AuthJWTSecret = l.strRequired("HOME_AUTH_JWT_SECRET")
	}
	// Optional in both modes: the expected token issuer (auth's own base URL).
	// Empty = do not enforce (see AuthJWTIssuer).
	c.AuthJWTIssuer = l.strDefault("HOME_AUTH_JWT_ISSUER", "")

	c.AllowedOrigins = l.csvDefault("HOME_ALLOWED_ORIGINS", defaultAllowedOrigins)

	c.SessionTTLDays = l.intDefault("HOME_SESSION_TTL_DAYS", defaultSessionTTLDays)
	c.RoleRefreshMinutes = l.intDefault("HOME_ROLE_REFRESH_MINUTES", defaultRoleRefreshMinutes)
	c.WSRevalidateMinutes = l.intDefault("HOME_WS_REVALIDATE_MINUTES", defaultWSRevalidateMinutes)

	c.TimezoneName = l.strDefault("HOME_TIMEZONE", defaultTimezone)
	if loc, err := time.LoadLocation(c.TimezoneName); err != nil {
		l.errf("HOME_TIMEZONE %q is not a valid IANA location: %v", c.TimezoneName, err)
	} else {
		c.Timezone = loc
	}

	c.DashboardLookbackDays = l.intDefault("HOME_DASHBOARD_LOOKBACK_DAYS", defaultDashboardLookback)
	c.RRuleMaxOccurrences = l.intDefault("HOME_RRULE_MAX_OCCURRENCES", defaultRRuleMaxOccurrences)
	c.RRuleMaxWindowMonths = l.intDefault("HOME_RRULE_MAX_WINDOW_MONTHS", defaultRRuleMaxWindowMonths)
	c.LogRetentionDays = l.intDefault("HOME_LOG_RETENTION_DAYS", defaultLogRetentionDays)
	c.NotesImageMaxUploadMB = l.intDefault("HOME_NOTES_IMAGE_MAX_UPLOAD_MB", defaultNotesImageMaxUploadMB)
	c.Storage = StorageConfig{
		WarnTotalMB:  l.intDefault("HOME_STORAGE_WARN_TOTAL_MB", defaultStorageWarnTotalMB),
		CacheSeconds: l.intDefault("HOME_STORAGE_CACHE_SECONDS", defaultStorageCacheSeconds),
	}
	c.ChatTrashDays = l.intDefault("HOME_CHAT_TRASH_DAYS", defaultChatTrashDays)

	// Range sanity — these bound server work, so a nonsensical value is a bug.
	if c.DashboardLookbackDays < 0 {
		l.errf("HOME_DASHBOARD_LOOKBACK_DAYS must be >= 0 (got %d)", c.DashboardLookbackDays)
	}
	if c.RRuleMaxOccurrences < 1 {
		l.errf("HOME_RRULE_MAX_OCCURRENCES must be >= 1 (got %d)", c.RRuleMaxOccurrences)
	}
	if c.RRuleMaxWindowMonths < 1 {
		l.errf("HOME_RRULE_MAX_WINDOW_MONTHS must be >= 1 (got %d)", c.RRuleMaxWindowMonths)
	}
	if c.LogRetentionDays < 0 {
		l.errf("HOME_LOG_RETENTION_DAYS must be >= 0 (got %d)", c.LogRetentionDays)
	}
	if c.NotesImageMaxUploadMB < 1 {
		l.errf("HOME_NOTES_IMAGE_MAX_UPLOAD_MB must be >= 1 (got %d)", c.NotesImageMaxUploadMB)
	}
	// ⚠ Bounded at 1, not at 0. A zero-day koš is not "delete immediately" — it is
	// a koš whose Obnovit button races the drain, which is worse than either
	// behaviour on its own. Deleting a conversation's bytes now is `?hard=true`.
	if c.ChatTrashDays < 1 {
		l.errf("HOME_CHAT_TRASH_DAYS must be >= 1 (got %d)", c.ChatTrashDays)
	}
	// The three session windows, all bounded at BOTH ends for the same reason —
	// see maxSessionTTLDays. A 0 or a negative on the revalidation window reads as
	// "turn the pump off" and would additionally become the 5-minute default
	// inside ws.Handler, with Redacted() printing a value the process does not have.
	//
	// ⚠ The FLOOR is an error and the CEILING is a clamp, for the two long-lived
	// variables. A too-small value has always been refused, so nothing deployed
	// can be carrying one; a too-large one has always been ACCEPTED, so refusing
	// it now is a boot failure introduced by an upgrade. See maxSessionTTLDays.
	if c.SessionTTLDays < 1 {
		l.errf("HOME_SESSION_TTL_DAYS must be >= 1 (got %d)", c.SessionTTLDays)
	}
	c.SessionTTLDays = l.clampMax("HOME_SESSION_TTL_DAYS", c.SessionTTLDays, maxSessionTTLDays)
	if c.RoleRefreshMinutes < 1 {
		l.errf("HOME_ROLE_REFRESH_MINUTES must be >= 1 (got %d)", c.RoleRefreshMinutes)
	}
	c.RoleRefreshMinutes = l.clampMax("HOME_ROLE_REFRESH_MINUTES", c.RoleRefreshMinutes, maxRoleRefreshMinutes)
	// New in v10: no deployed value can be above the cap, so it is refused rather
	// than clamped.
	if c.WSRevalidateMinutes < 1 || c.WSRevalidateMinutes > maxWSRevalidateMinutes {
		l.errf("HOME_WS_REVALIDATE_MINUTES must be between 1 and %d (got %d)",
			maxWSRevalidateMinutes, c.WSRevalidateMinutes)
	}

	c.Docs = l.docs(c)
	c.Notif = l.notif()
	c.Garden = l.garden()
	c.Status = l.status(c)

	// Security hard-stop: the dev bypass must never be active in production.
	if c.DevAuthBypass && c.IsProduction() {
		l.errf("HOME_DEV_AUTH_BYPASS must not be enabled when HOME_ENV=production " +
			"(fake authentication in production is a security hole)")
	}

	if len(l.errs) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(l.errs, "\n  - "))
	}
	c.Warnings = l.warns
	return c, nil
}

// osLookup adapts os.LookupEnv to the Getenv signature.
func osLookup(key string) (string, bool) { return os.LookupEnv(key) }

// docs loads and validates the documents module's configuration. Storage is the
// one part with a hard production rule: real deployments MUST use object storage,
// because the local filesystem store lives on a container's disk and is not
// covered by any backup (D45).
func (l *loader) docs(c *Config) DocsConfig {
	d := DocsConfig{}

	d.R2Bucket = l.strDefault("HOME_DOCS_R2_BUCKET", "")
	d.R2Endpoint = l.strDefault("HOME_DOCS_R2_ENDPOINT", "")
	d.R2AccessKeyID = l.strDefault("HOME_DOCS_R2_ACCESS_KEY_ID", "")
	d.R2SecretAccessKey = l.strDefault("HOME_DOCS_R2_SECRET_ACCESS_KEY", "")

	if d.R2Bucket != "" {
		// A half-configured bucket would fail on the first upload, in production,
		// after the user picked a file — so fail at boot instead.
		if d.R2Endpoint == "" {
			l.errf("HOME_DOCS_R2_ENDPOINT is required when HOME_DOCS_R2_BUCKET is set")
		}
		if d.R2AccessKeyID == "" || d.R2SecretAccessKey == "" {
			l.errf("HOME_DOCS_R2_ACCESS_KEY_ID and HOME_DOCS_R2_SECRET_ACCESS_KEY are required when HOME_DOCS_R2_BUCKET is set")
		}
	} else {
		if c.IsProduction() {
			l.errf("HOME_DOCS_R2_BUCKET is required when HOME_ENV=production " +
				"(the local filesystem document store has no backup — PRD D45)")
		}
		// Default the dev store next to the database so the docker harness keeps
		// document bytes on the same persisted volume as the DB.
		d.LocalDir = l.strDefault("HOME_DOCS_LOCAL_DIR", filepath.Join(filepath.Dir(c.DBPath), "blobs"))
	}

	d.BackupBucket = l.strDefault("HOME_DOCS_R2_BACKUP_BUCKET", "")
	// The backup bucket usually lives in the same account, so its connection
	// details default to the primary's; override only for a distinct account.
	d.BackupEndpoint = l.strDefault("HOME_DOCS_R2_BACKUP_ENDPOINT", d.R2Endpoint)
	d.BackupAccessKeyID = l.strDefault("HOME_DOCS_R2_BACKUP_ACCESS_KEY_ID", d.R2AccessKeyID)
	d.BackupSecretAccessKey = l.strDefault("HOME_DOCS_R2_BACKUP_SECRET_ACCESS_KEY", d.R2SecretAccessKey)
	if d.BackupBucket != "" {
		// Those defaults are empty when there is no primary bucket to inherit from (the
		// local dev store). Say that here rather than letting the store constructor
		// abort the boot with "s3 needs an endpoint", which names neither variable.
		if d.BackupEndpoint == "" {
			l.errf("HOME_DOCS_R2_BACKUP_ENDPOINT is required when HOME_DOCS_R2_BACKUP_BUCKET is set " +
				"(it defaults to HOME_DOCS_R2_ENDPOINT, which is unset)")
		}
		if d.BackupAccessKeyID == "" || d.BackupSecretAccessKey == "" {
			l.errf("HOME_DOCS_R2_BACKUP_ACCESS_KEY_ID and HOME_DOCS_R2_BACKUP_SECRET_ACCESS_KEY are required " +
				"when HOME_DOCS_R2_BACKUP_BUCKET is set (they default to the primary's, which are unset)")
		}
	}

	d.MirrorInterval = l.durationDefault("HOME_DOCS_MIRROR_CRON", defaultDocsMirrorInterval)
	if d.MirrorInterval < 0 {
		l.errf("HOME_DOCS_MIRROR_CRON must not be negative (got %s)", d.MirrorInterval)
	}
	if d.MirrorInterval > 0 && d.MirrorInterval < time.Minute {
		l.errf("HOME_DOCS_MIRROR_CRON must be at least 1m (or 0 to disable); got %s", d.MirrorInterval)
	}
	d.OrphanGraceHours = l.intDefault("HOME_DOCS_ORPHAN_GRACE_HOURS", defaultDocsOrphanGraceHours)
	if d.OrphanGraceHours < 1 {
		l.errf("HOME_DOCS_ORPHAN_GRACE_HOURS must be >= 1 (got %d) — a shorter window can delete an in-flight upload", d.OrphanGraceHours)
	}
	d.OrphanMaxPercent = l.intDefault("HOME_DOCS_ORPHAN_MAX_PERCENT", defaultDocsOrphanMaxPercent)
	if d.OrphanMaxPercent < 1 || d.OrphanMaxPercent > 100 {
		l.errf("HOME_DOCS_ORPHAN_MAX_PERCENT must be between 1 and 100 (got %d) — it is the reconciliation "+
			"pass's blast-radius guard; 100 disables it", d.OrphanMaxPercent)
	}

	d.MaxUploadMB = l.intDefault("HOME_DOCS_MAX_UPLOAD_MB", defaultDocsMaxUploadMB)
	if d.MaxUploadMB < 1 {
		l.errf("HOME_DOCS_MAX_UPLOAD_MB must be >= 1 (got %d)", d.MaxUploadMB)
	}
	d.AllowedMIME = l.csvDefault("HOME_DOCS_ALLOWED_MIME", nil)

	d.PreviewEnabled = l.boolDefault("HOME_DOCS_PREVIEW_ENABLED", true)
	d.PreviewTimeout = time.Duration(l.intDefault("HOME_DOCS_PREVIEW_TIMEOUT_SEC", defaultDocsPreviewTimeout)) * time.Second
	if d.PreviewTimeout < time.Second {
		l.errf("HOME_DOCS_PREVIEW_TIMEOUT_SEC must be >= 1 (got %s)", d.PreviewTimeout)
	}
	d.PreviewWorkers = l.intDefault("HOME_DOCS_PREVIEW_WORKERS", defaultDocsPreviewWorkers)
	if d.PreviewWorkers < 1 {
		l.errf("HOME_DOCS_PREVIEW_WORKERS must be >= 1 (got %d)", d.PreviewWorkers)
	}
	d.GotenbergURL = strings.TrimRight(l.strDefault("HOME_DOCS_GOTENBERG_URL", ""), "/")
	d.PdftoppmPath = l.strDefault("HOME_DOCS_PDFTOPPM_PATH", defaultDocsPdftoppm)
	d.CwebpPath = l.strDefault("HOME_DOCS_CWEBP_PATH", defaultDocsCwebp)
	d.ThumbMaxPx = l.intDefault("HOME_DOCS_THUMB_MAX_PX", defaultDocsThumbMaxPx)
	if d.ThumbMaxPx < 32 {
		l.errf("HOME_DOCS_THUMB_MAX_PX must be >= 32 (got %d)", d.ThumbMaxPx)
	}
	d.ImageMaxMegapixels = l.intDefault("HOME_DOCS_IMAGE_MAX_MEGAPIXELS", defaultDocsImageMaxMP)
	if d.ImageMaxMegapixels < 1 {
		l.errf("HOME_DOCS_IMAGE_MAX_MEGAPIXELS must be >= 1 (got %d) — it bounds an in-process decode, "+
			"so an unbounded value is an OOM of the whole backend", d.ImageMaxMegapixels)
	}

	d.PublicBaseURL = strings.TrimRight(l.strDefault("HOME_DOCS_PUBLIC_BASE_URL", ""), "/")
	return d
}

// notif loads and validates the v5 notification configuration.
//
// Push is OPTIONAL: with no VAPID keypair the channel is inert and the rest of
// the app runs untouched (development, and any deploy that does not want push).
// But a HALF-configured keypair, or a malformed one, is always an error — it
// would fail at the first send, in production, long after boot, with a message
// about elliptic curves rather than about an environment variable.
func (l *loader) notif() NotifConfig {
	n := NotifConfig{}

	n.VAPIDPublicKey = strings.TrimSpace(l.strDefault("HOME_VAPID_PUBLIC_KEY", ""))
	n.VAPIDPrivateKey = strings.TrimSpace(l.strDefault("HOME_VAPID_PRIVATE_KEY", ""))
	n.VAPIDSubject = strings.TrimSpace(l.strDefault("HOME_VAPID_SUBJECT", ""))

	switch {
	case n.VAPIDPublicKey == "" && n.VAPIDPrivateKey == "":
		// Push disabled. main.go logs this once, loudly, at boot.
	case n.VAPIDPublicKey == "" || n.VAPIDPrivateKey == "":
		l.errf("HOME_VAPID_PUBLIC_KEY and HOME_VAPID_PRIVATE_KEY must be set together " +
			"(set neither to disable push, both to enable it)")
	default:
		l.vapidKey("HOME_VAPID_PUBLIC_KEY", n.VAPIDPublicKey, vapidPublicKeyBytes)
		l.vapidKey("HOME_VAPID_PRIVATE_KEY", n.VAPIDPrivateKey, vapidPrivateKeyBytes)
		if n.VAPIDSubject == "" {
			l.errf("HOME_VAPID_SUBJECT is required when a VAPID keypair is set " +
				`(a "mailto:you@example.com" or "https://home.tilcer.cz" contact for the push service)`)
		} else if !strings.HasPrefix(n.VAPIDSubject, "mailto:") && !strings.HasPrefix(n.VAPIDSubject, "https://") {
			l.errf("HOME_VAPID_SUBJECT must start with \"mailto:\" or \"https://\" (got %q)", n.VAPIDSubject)
		}
	}

	n.CoalesceDefault = time.Duration(l.intDefault("HOME_NOTIF_COALESCE_DEFAULT", int(defaultNotifCoalesce/time.Second))) * time.Second
	if n.CoalesceDefault < 0 {
		l.errf("HOME_NOTIF_COALESCE_DEFAULT must be >= 0 seconds (0 = send every event); got %s", n.CoalesceDefault)
	}
	n.DeliveryRetentionDays = l.intDefault("HOME_NOTIF_DELIVERY_RETENTION_DAYS", defaultNotifRetentionDays)
	if n.DeliveryRetentionDays < 0 {
		l.errf("HOME_NOTIF_DELIVERY_RETENTION_DAYS must be >= 0 (0 = keep forever); got %d", n.DeliveryRetentionDays)
	}
	n.MaxFailDays = l.intDefault("HOME_NOTIF_MAX_FAILDAYS", defaultNotifMaxFailDays)
	if n.MaxFailDays < 1 {
		l.errf("HOME_NOTIF_MAX_FAILDAYS must be >= 1 (got %d)", n.MaxFailDays)
	}
	// Hosts only, never URLs: the check runs against a parsed endpoint's hostname,
	// so a pasted "https://…/" would silently match nothing.
	n.PushEndpointHosts = l.csvDefault("HOME_PUSH_ENDPOINT_HOSTS", nil)
	for _, h := range n.PushEndpointHosts {
		if strings.ContainsAny(h, "/:") {
			l.errf("HOME_PUSH_ENDPOINT_HOSTS takes bare hostnames, not URLs (got %q)", h)
		}
	}

	n.SchedTick = time.Duration(l.intDefault("HOME_SCHED_TICK_SECONDS", int(defaultSchedTick/time.Second))) * time.Second
	if n.SchedTick < time.Second || n.SchedTick > time.Minute {
		// Slots are wall-clock minutes: a tick longer than a minute steps over them.
		l.errf("HOME_SCHED_TICK_SECONDS must be between 1 and 60 (got %s) — a longer tick skips whole minute slots", n.SchedTick)
	}
	n.CatchupGrace = time.Duration(l.intDefault("HOME_SCHED_CATCHUP_GRACE", defaultSchedCatchupGraceMins)) * time.Minute
	if n.CatchupGrace < 0 {
		l.errf("HOME_SCHED_CATCHUP_GRACE must be >= 0 minutes (0 = never catch up); got %s", n.CatchupGrace)
	}

	return n
}

// vapidKey checks that a configured VAPID key is raw base64url of the expected
// byte length. base64.RawURLEncoding is strict about padding, so a key pasted
// with "=" padding (some generators emit it) is trimmed rather than rejected.
func (l *loader) vapidKey(key, value string, wantBytes int) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(value, "="))
	if err != nil {
		l.errf("%s must be base64url-encoded (got %q): %v", key, redactKey(value), err)
		return
	}
	if len(raw) != wantBytes {
		l.errf("%s must decode to %d bytes, got %d — is the public/private pair swapped?", key, wantBytes, len(raw))
	}
}

// redactKey shows just enough of a key to identify a paste error without putting
// the secret in a log line.
func redactKey(v string) string {
	if len(v) <= 6 {
		return "***"
	}
	return v[:6] + "…"
}

// loader accumulates validation errors while reading typed values.
type loader struct {
	getenv Getenv
	errs   []string
	warns  []string
}

func (l *loader) errf(format string, a ...any) { l.errs = append(l.errs, fmt.Sprintf(format, a...)) }

// warnf records a problem the process can BOOT THROUGH — a value that had to be
// corrected rather than one that makes the configuration unusable. Load surfaces
// them on Config.Warnings and the composition root logs them at Warn.
//
// ⚠ It exists so that "this setting is wrong" and "this service cannot start"
// stop being the same outcome. errf aborts Load, which crash-loops the container
// and puts the only explanation inside the restart loop; a warning reaches the
// operator from a service that is up.
func (l *loader) warnf(format string, a ...any) { l.warns = append(l.warns, fmt.Sprintf(format, a...)) }

// clampMax holds an already-deployed window to its ceiling and says so loudly,
// rather than refusing to start over a value that was legal until this release.
// See maxSessionTTLDays.
func (l *loader) clampMax(key string, got, max int) int {
	if got <= max {
		return got
	}
	l.warnf("%s is %d, above the maximum of %d — CLAMPED to %d for this process. "+
		"Fix the value in Coolify: past the cap the window has stopped bounding the "+
		"incident it exists for, and a large enough one overflows the time.Duration "+
		"it is multiplied into", key, got, max, max)
	return max
}

func (l *loader) strRequired(key string) string {
	v, ok := l.getenv(key)
	if !ok || strings.TrimSpace(v) == "" {
		l.errf("%s is required", key)
		return ""
	}
	return v
}

func (l *loader) strDefault(key, def string) string {
	v, ok := l.getenv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func (l *loader) intDefault(key string, def int) int {
	v, ok := l.getenv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		l.errf("%s must be an integer (got %q)", key, v)
		return def
	}
	return n
}

func (l *loader) boolDefault(key string, def bool) bool {
	v, ok := l.getenv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		l.errf("%s must be a boolean (got %q)", key, v)
		return def
	}
	return b
}

// durationDefault parses a Go duration ("24h", "30m", "0"). A cron-looking value
// is rejected with an explicit message: HOME_DOCS_MIRROR_CRON keeps its PRD name
// but takes a duration, so a copy-pasted "0 * * * *" must fail loudly rather than
// silently fall back to the default schedule.
func (l *loader) durationDefault(key string, def time.Duration) time.Duration {
	v, ok := l.getenv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	raw := strings.TrimSpace(v)
	if strings.ContainsAny(raw, " *?/,") {
		l.errf("%s must be a Go duration such as \"24h\" or \"0\" to disable, not a cron expression (got %q)", key, v)
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		l.errf("%s must be a Go duration such as \"24h\" (got %q)", key, v)
		return def
	}
	return d
}

// csvDefault splits a comma-separated value, trimming whitespace and dropping
// empty entries. Returns def when the variable is unset or empty.
func (l *loader) csvDefault(key string, def []string) []string {
	v, ok := l.getenv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

// defaultGardenWeatherURL is Open-Meteo's forecast endpoint: free, keyless and
// with no account, which is why it is the default rather than a required var.
const (
	defaultGardenWeatherURL     = "https://api.open-meteo.com/v1/forecast"
	defaultGardenWeatherPoll    = 12
	defaultGardenImportMaxBytes = 1 << 20 // 1 MiB of pasted JSON is ~200 crops
)

// garden loads the v7 garden configuration (PRD §V7-9). Every value has a
// working default, so the module runs with none of these set — the forecast is
// simply the one part of it that can be turned off.
func (l *loader) garden() GardenConfig {
	g := GardenConfig{
		WeatherEnabled:   l.boolDefault("HOME_GARDEN_WEATHER_ENABLED", true),
		WeatherURL:       l.strDefault("HOME_GARDEN_WEATHER_URL", defaultGardenWeatherURL),
		WeatherPollHours: l.intDefault("HOME_GARDEN_WEATHER_POLL_HOURS", defaultGardenWeatherPoll),
		ImportMaxBytes:   defaultGardenImportMaxBytes,
	}
	// Clamped rather than rejected: a bad poll interval should not stop the
	// service booting over a feature that is allowed to be absent entirely.
	if g.WeatherPollHours < 1 || g.WeatherPollHours > 24 {
		g.WeatherPollHours = defaultGardenWeatherPoll
	}
	return g
}

// status loads the crash reporter's configuration, on the VAPID pattern above:
// neither variable set means the feature is OFF and the boot line says so, and
// exactly one set is a REFUSAL rather than a default.
//
// ⚠ Half-configured is refused because the alternative is invisible. Reporting
// fails safe by design — a rejected event is dropped in silence, with no log
// line anywhere — so a key set against an unset URL would look exactly like a
// working install right up until the first crash nobody hears about. The other
// direction of the same rule is why a malformed URL is refused rather than
// tried: net/http would fail every send, silently, forever.
func (l *loader) status(c *Config) StatusConfig {
	s := StatusConfig{
		IngestURL: strings.TrimSpace(l.strDefault(EnvStatusIngestURL, "")),
		IngestKey: strings.TrimSpace(l.strDefault(EnvStatusIngestKey, "")),
	}
	switch {
	case s.IngestURL == "" && s.IngestKey == "":
		// Crash reporting disabled. main.go logs this once, loudly, at boot.
	case s.IngestURL == "" || s.IngestKey == "":
		l.errf("%s and %s must be set together "+
			"(set neither to disable crash reporting, both to enable it)",
			EnvStatusIngestURL, EnvStatusIngestKey)
	default:
		if u, err := url.Parse(s.IngestURL); err != nil || !u.IsAbs() || u.Host == "" ||
			(u.Scheme != "http" && u.Scheme != "https") {
			l.errf("%s must be an absolute http(s) URL such as "+
				"https://status.tilcer.cz/api/ingest/home (got %q)", EnvStatusIngestURL, s.IngestURL)
		}
	}
	// The environment tag defaults to the one this service already knows about
	// rather than to a literal: two names for one deployment is how a board ends
	// up with the same crash filed under "prod" and "production".
	//
	// Trimmed like its three neighbours, and for the same reason one step further
	// on: statusreport.WithEnvironment trims what it is given, so an untrimmed
	// value here would make the boot line and Redacted() print an environment tag
	// no event actually carries — the one place this configuration is ever read
	// back disagreeing with the wire.
	s.Environment = strings.TrimSpace(l.strDefault(EnvStatusEnvironment, c.Env))
	s.Release = strings.TrimSpace(l.strDefault(EnvStatusRelease, ""))
	return s
}

// StorageConfig is the Úložiště page's whole configuration (v9, §V9-9).
//
// Two plain integers, both defaulted, neither a secret — no new bucket credential,
// no feature flag, and deliberately NO CONFIGURATION FOR PRIVACY AT ALL.
type StorageConfig struct {
	// WarnTotalMB is the warning threshold on the MODULES' primary-bucket total.
	// 0 disables the warning.
	//
	// ⚠ Nothing is ever blocked by it (D196): no upload fails, there is no per-user
	// quota and no new 413. It exists so an R2 bill is a decision rather than a
	// surprise.
	//
	// ⚠ And it is compared against the per-module R2 total ONLY — not the whole
	// bucket. The Litestream replica and the backup mirror are derived copies and
	// sit outside the breakdown (D214/D205); folding them in would make the
	// default fire permanently and the register would stop meaning anything.
	WarnTotalMB int
	// CacheSeconds is the snapshot's in-process TTL; 0 disables caching and
	// ?refresh=true bypasses it either way.
	//
	// A one-minute TTL is not state: nothing survives a restart, nothing needs a
	// migration, and nothing can be wrong for longer than a minute — which is what
	// lets the whole page be computed on read with no table and no job (D195).
	CacheSeconds int
}
