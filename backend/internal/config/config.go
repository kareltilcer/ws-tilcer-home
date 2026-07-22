// Package config loads and validates the service configuration from the
// environment (PRD §9). It fails fast and loudly: a missing required variable
// or an invalid value aborts startup with a message listing every problem —
// a silently-defaulted secret is worse than a crash.
package config

import (
	"fmt"
	"os"
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

	// AllowedOrigins is the CORS allowlist for the cross-subdomain auth refresh
	// flow (home's own calls are same-origin and need no CORS).
	AllowedOrigins []string

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

	// DevAuthBypass, when true (and only outside production), skips real JWT
	// introspection and injects a fake actor so the app runs offline. It is a
	// development-only convenience and a security hole if ever enabled in prod.
	DevAuthBypass bool

	// DevActorID and DevActorRoles configure the fake actor used under the bypass.
	DevActorID    string
	DevActorRoles []string
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
	static := c.StaticDir
	if static == "" {
		static = "none"
	}
	return fmt.Sprintf(
		"env=%s addr=%s db=%s static=%s site=%s auth_base=%s auth_secret=%s tz=%s "+
			"lookback=%d rrule_max=%d rrule_window_months=%d log_retention=%d dev_auth_bypass=%t",
		c.Env, c.Addr, c.DBPath, static, c.SiteKey, c.AuthBaseURL, secret, c.TimezoneName,
		c.DashboardLookbackDays, c.RRuleMaxOccurrences, c.RRuleMaxWindowMonths,
		c.LogRetentionDays, c.DevAuthBypass,
	)
}

// Getenv is the environment lookup used by Load; it mirrors os.LookupEnv and is
// injected in tests.
type Getenv func(key string) (string, bool)

// Defaults for optional variables.
const (
	defaultEnv                  = "development"
	defaultAddr                 = ":8080"
	defaultSiteKey              = "home"
	defaultTimezone             = "Europe/Prague"
	defaultDashboardLookback    = 30
	defaultRRuleMaxOccurrences  = 500
	defaultRRuleMaxWindowMonths = 24
	defaultLogRetentionDays     = 0
)

// LoadFromEnv loads the configuration using os.LookupEnv.
func LoadFromEnv() (*Config, error) {
	return Load(osLookup)
}

// Load reads, defaults, and validates configuration from getenv. On any problem
// it returns a single error enumerating every issue found (not just the first).
func Load(getenv Getenv) (*Config, error) {
	l := &loader{getenv: getenv}
	c := &Config{}

	c.Env = l.strDefault("HOME_ENV", defaultEnv)
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
	} else {
		c.AuthBaseURL = l.strRequired("AUTH_BASE_URL")
		c.AuthServiceSecret = l.strRequired("HOME_AUTH_SERVICE_SECRET")
	}

	c.AllowedOrigins = l.csvDefault("HOME_ALLOWED_ORIGINS", nil)

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

	// Security hard-stop: the dev bypass must never be active in production.
	if c.DevAuthBypass && c.IsProduction() {
		l.errf("HOME_DEV_AUTH_BYPASS must not be enabled when HOME_ENV=production " +
			"(fake authentication in production is a security hole)")
	}

	if len(l.errs) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(l.errs, "\n  - "))
	}
	return c, nil
}

// osLookup adapts os.LookupEnv to the Getenv signature.
func osLookup(key string) (string, bool) { return os.LookupEnv(key) }

// loader accumulates validation errors while reading typed values.
type loader struct {
	getenv Getenv
	errs   []string
}

func (l *loader) errf(format string, a ...any) { l.errs = append(l.errs, fmt.Sprintf(format, a...)) }

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
