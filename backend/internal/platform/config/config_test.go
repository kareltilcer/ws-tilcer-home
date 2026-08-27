package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// envMap builds a Getenv from a map for hermetic tests.
func envMap(m map[string]string) Getenv {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

// validBase is the minimal environment for a valid non-bypass load.
func validBase() map[string]string {
	return map[string]string{
		"HOME_DB_PATH":             "/data/home.db",
		"AUTH_BASE_URL":            "https://auth.tilcer.cz",
		"HOME_AUTH_SERVICE_SECRET": "s3cret",
		"HOME_AUTH_JWT_SECRET":     "jwt-s3cret",
	}
}

func TestLoad_Defaults(t *testing.T) {
	c, err := Load(envMap(validBase()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Env != "development" {
		t.Errorf("Env = %q, want development", c.Env)
	}
	if c.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", c.Addr)
	}
	if c.SiteKey != "home" {
		t.Errorf("SiteKey = %q, want home", c.SiteKey)
	}
	if c.TimezoneName != "Europe/Prague" || c.Timezone == nil {
		t.Errorf("Timezone = %q/%v, want Europe/Prague", c.TimezoneName, c.Timezone)
	}
	if c.DashboardLookbackDays != 30 {
		t.Errorf("DashboardLookbackDays = %d, want 30", c.DashboardLookbackDays)
	}
	if c.RRuleMaxOccurrences != 500 {
		t.Errorf("RRuleMaxOccurrences = %d, want 500", c.RRuleMaxOccurrences)
	}
	if c.RRuleMaxWindowMonths != 24 {
		t.Errorf("RRuleMaxWindowMonths = %d, want 24", c.RRuleMaxWindowMonths)
	}
	if c.LogRetentionDays != 0 {
		t.Errorf("LogRetentionDays = %d, want 0", c.LogRetentionDays)
	}
	if c.SessionTTLDays != 90 {
		t.Errorf("SessionTTLDays = %d, want 90", c.SessionTTLDays)
	}
	if c.RoleRefreshMinutes != 15 {
		t.Errorf("RoleRefreshMinutes = %d, want 15", c.RoleRefreshMinutes)
	}
	if c.WSRevalidateMinutes != 5 {
		t.Errorf("WSRevalidateMinutes = %d, want 5", c.WSRevalidateMinutes)
	}
	if c.DevAuthBypass {
		t.Error("DevAuthBypass = true, want false")
	}
	// The JWT issuer pin is optional and defaults to unset (not enforced).
	if c.AuthJWTIssuer != "" {
		t.Errorf("AuthJWTIssuer = %q, want empty (issuer not enforced by default)", c.AuthJWTIssuer)
	}
}

// TestLoad_SessionWindowRangeChecks. The three session windows are the numbers an
// operator reaches for from Coolify during an incident, so a value outside their
// range has to fail the boot rather than be silently replaced.
//
// ⚠ ALL THREE are bounded at BOTH ends, and the upper bound is the one that
// matters. Each is multiplied into a time.Duration in the composition root, and a
// large enough value overflows int64 nanoseconds into a NEGATIVE duration every
// comparison then reads backwards: a negative role-refresh threshold re-mints on
// every request and every revalidation tick, and a negative session TTL issues
// cookies with a negative MaxAge against rows that are already expired. On the
// revalidation window a 0 or a negative additionally reads as "turn the pump off"
// and would become the 5-minute default inside ws.Handler, with Redacted()
// printing a number the process is not using.
func TestLoad_SessionWindowRangeChecks(t *testing.T) {
	// A value BELOW the floor has always been refused, so nothing deployed can be
	// carrying one and refusing it costs nobody a boot.
	for _, tc := range []struct{ key, value string }{
		{"HOME_SESSION_TTL_DAYS", "0"},
		{"HOME_SESSION_TTL_DAYS", "-1"},
		{"HOME_ROLE_REFRESH_MINUTES", "0"},
		{"HOME_ROLE_REFRESH_MINUTES", "-1"},
		{"HOME_WS_REVALIDATE_MINUTES", "0"},
		{"HOME_WS_REVALIDATE_MINUTES", "-1"},
		// The revalidation window is NEW in v10, so no deployed value can be above
		// its ceiling either: it is refused at both ends.
		{"HOME_WS_REVALIDATE_MINUTES", "10000"},
	} {
		env := validBase()
		env[tc.key] = tc.value
		_, err := Load(envMap(env))
		if err == nil || !strings.Contains(err.Error(), tc.key) {
			t.Errorf("%s=%s was accepted, want a boot error naming it: %v", tc.key, tc.value, err)
		}
	}
	// And a legal value round-trips rather than falling back to the default.
	env := validBase()
	env["HOME_WS_REVALIDATE_MINUTES"] = "2"
	c, err := Load(envMap(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.WSRevalidateMinutes != 2 {
		t.Errorf("WSRevalidateMinutes = %d, want 2", c.WSRevalidateMinutes)
	}
}

// TestLoad_PreExistingWindowsAreClampedNotRefused. The two windows that shipped
// before v10 were FLOOR-checked only, so any value above the new ceiling is one
// that has been legal for their whole life — and every one of them lives in
// Coolify, where the repo cannot see it.
//
// ⚠ Refusing it aborts Load, and Load's error is not a degraded setting: it
// crash-loops the container on the deploy that lands v10, with the only signal a
// log line inside the restart loop. Clamping closes the overflow hole just as
// completely — the oversized value never reaches the multiplication — and leaves
// the operator a warning they can read from a service that is UP.
func TestLoad_PreExistingWindowsAreClampedNotRefused(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value string
		want  int
		get   func(*Config) int
	}{
		// Both of these overflow their time.Duration multiplication if they reach it.
		{"HOME_SESSION_TTL_DAYS", "200000", maxSessionTTLDays,
			func(c *Config) int { return c.SessionTTLDays }},
		{"HOME_ROLE_REFRESH_MINUTES", "200000000", maxRoleRefreshMinutes,
			func(c *Config) int { return c.RoleRefreshMinutes }},
		// And a merely-too-large one, which is the realistic Coolify value.
		{"HOME_ROLE_REFRESH_MINUTES", "10080", maxRoleRefreshMinutes,
			func(c *Config) int { return c.RoleRefreshMinutes }},
	} {
		env := validBase()
		env[tc.key] = tc.value
		c, err := Load(envMap(env))
		if err != nil {
			t.Fatalf("%s=%s refused the boot (%v) — a value that was legal before this "+
				"release must not crash-loop the container", tc.key, tc.value, err)
		}
		if got := tc.get(c); got != tc.want {
			t.Errorf("%s=%s loaded as %d, want it clamped to %d — the oversized value must "+
				"never reach the time.Duration multiplication", tc.key, tc.value, got, tc.want)
		}
		// ⚠ And it is LOUD. A silent clamp is a setting the operator believes is in
		// force and is not, which is the failure mode a bare cap avoids.
		var named bool
		for _, w := range c.Warnings {
			if strings.Contains(w, tc.key) {
				named = true
			}
		}
		if !named {
			t.Errorf("%s=%s was clamped with no warning naming it (warnings: %v) — a silent "+
				"clamp leaves the operator believing a setting is in force that is not",
				tc.key, tc.value, c.Warnings)
		}
	}

	// A configuration inside the ranges warns about nothing.
	c, err := Load(envMap(validBase()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Warnings) != 0 {
		t.Errorf("a valid configuration produced warnings %v, want none", c.Warnings)
	}
}

func TestLoad_JWTIssuerOptional(t *testing.T) {
	env := validBase()
	env["HOME_AUTH_JWT_ISSUER"] = "https://auth.tilcer.cz"
	c, err := Load(envMap(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.AuthJWTIssuer != "https://auth.tilcer.cz" {
		t.Errorf("AuthJWTIssuer = %q, want the configured issuer", c.AuthJWTIssuer)
	}
}

func TestLoad_MissingRequiredAreAggregated(t *testing.T) {
	_, err := Load(envMap(map[string]string{}))
	if err == nil {
		t.Fatal("expected error for empty environment")
	}
	for _, want := range []string{"HOME_DB_PATH", "AUTH_BASE_URL", "HOME_AUTH_SERVICE_SECRET", "HOME_AUTH_JWT_SECRET"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing mention of %s:\n%v", want, err)
		}
	}
}

func TestLoad_InvalidTimezone(t *testing.T) {
	env := validBase()
	env["HOME_TIMEZONE"] = "Mars/Olympus_Mons"
	_, err := Load(envMap(env))
	if err == nil || !strings.Contains(err.Error(), "HOME_TIMEZONE") {
		t.Fatalf("expected timezone error, got: %v", err)
	}
}

func TestLoad_InvalidInt(t *testing.T) {
	env := validBase()
	env["HOME_RRULE_MAX_OCCURRENCES"] = "lots"
	_, err := Load(envMap(env))
	if err == nil || !strings.Contains(err.Error(), "HOME_RRULE_MAX_OCCURRENCES") {
		t.Fatalf("expected int parse error, got: %v", err)
	}
}

func TestLoad_DevBypassRelaxesAuthRequirements(t *testing.T) {
	// With the bypass on, AUTH_BASE_URL and the service secret are not required.
	env := map[string]string{
		"HOME_DB_PATH":         "/data/home.db",
		"HOME_DEV_AUTH_BYPASS": "true",
	}
	c, err := Load(envMap(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.DevAuthBypass {
		t.Error("DevAuthBypass = false, want true")
	}
	if len(c.DevActorRoles) != 1 || c.DevActorRoles[0] != "admin" {
		t.Errorf("DevActorRoles = %v, want [admin]", c.DevActorRoles)
	}
}

func TestLoad_DevBypassRefusedInProduction(t *testing.T) {
	env := validBase()
	env["HOME_ENV"] = "production"
	env["HOME_DEV_AUTH_BYPASS"] = "true"
	_, err := Load(envMap(env))
	if err == nil || !strings.Contains(err.Error(), "HOME_DEV_AUTH_BYPASS") {
		t.Fatalf("expected production bypass refusal, got: %v", err)
	}
}

func TestLoad_AllowedOriginsCSV(t *testing.T) {
	env := validBase()
	env["HOME_ALLOWED_ORIGINS"] = "https://a.tilcer.cz, https://b.tilcer.cz ,"
	c, err := Load(envMap(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.AllowedOrigins) != 2 || c.AllowedOrigins[0] != "https://a.tilcer.cz" || c.AllowedOrigins[1] != "https://b.tilcer.cz" {
		t.Errorf("AllowedOrigins = %v, want two trimmed origins", c.AllowedOrigins)
	}
}

// ---- documents (v4) ----

func TestLoad_DocsDefaults(t *testing.T) {
	c, err := Load(envMap(validBase()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d := c.Docs
	if d.UsesObjectStorage() {
		t.Error("UsesObjectStorage = true with no bucket configured")
	}
	// Development falls back to a filesystem store beside the database file, so
	// the docker harness keeps blobs on the same persisted volume.
	if d.LocalDir == "" || !strings.Contains(strings.ReplaceAll(d.LocalDir, "\\", "/"), "/data/blobs") {
		t.Errorf("LocalDir = %q, want a blobs dir next to the DB", d.LocalDir)
	}
	if d.MaxUploadMB != 50 {
		t.Errorf("MaxUploadMB = %d, want 50", d.MaxUploadMB)
	}
	if !d.PreviewEnabled {
		t.Error("PreviewEnabled = false, want true by default")
	}
	if d.PreviewTimeout.Seconds() != 60 {
		t.Errorf("PreviewTimeout = %s, want 60s", d.PreviewTimeout)
	}
	if d.MirrorInterval.Hours() != 24 {
		t.Errorf("MirrorInterval = %s, want 24h (daily)", d.MirrorInterval)
	}
	// No backup bucket configured ⇒ the mirror stays off even with an interval.
	if d.MirrorEnabled() {
		t.Error("MirrorEnabled = true with no backup bucket")
	}
	if len(d.AllowedMIME) != 0 {
		t.Errorf("AllowedMIME = %v, want empty (allow all, still sniffed)", d.AllowedMIME)
	}
}

func TestLoad_DocsBucketRequiresEndpointAndKeys(t *testing.T) {
	env := validBase()
	env["HOME_DOCS_R2_BUCKET"] = "home-docs"
	_, err := Load(envMap(env))
	if err == nil {
		t.Fatal("expected an error for a half-configured bucket")
	}
	for _, want := range []string{"HOME_DOCS_R2_ENDPOINT", "HOME_DOCS_R2_ACCESS_KEY_ID"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing mention of %s:\n%v", want, err)
		}
	}
}

func TestLoad_DocsProductionRequiresObjectStorage(t *testing.T) {
	env := validBase()
	env["HOME_ENV"] = "production"
	_, err := Load(envMap(env))
	if err == nil || !strings.Contains(err.Error(), "HOME_DOCS_R2_BUCKET") {
		t.Fatalf("expected production to refuse the filesystem document store, got: %v", err)
	}
}

func TestLoad_DocsBackupInheritsPrimaryCredentials(t *testing.T) {
	env := validBase()
	env["HOME_DOCS_R2_BUCKET"] = "home-docs"
	env["HOME_DOCS_R2_ENDPOINT"] = "https://acct.r2.cloudflarestorage.com"
	env["HOME_DOCS_R2_ACCESS_KEY_ID"] = "ak"
	env["HOME_DOCS_R2_SECRET_ACCESS_KEY"] = "sk-r2-secret"
	env["HOME_DOCS_R2_BACKUP_BUCKET"] = "home-docs-backup"
	c, err := Load(envMap(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d := c.Docs
	if !d.UsesObjectStorage() || d.LocalDir != "" {
		t.Errorf("expected object storage with no local dir, got bucket=%q dir=%q", d.R2Bucket, d.LocalDir)
	}
	if d.BackupEndpoint != d.R2Endpoint || d.BackupAccessKeyID != "ak" || d.BackupSecretAccessKey != "sk-r2-secret" {
		t.Error("backup connection details should default to the primary's")
	}
	if !d.MirrorEnabled() {
		t.Error("MirrorEnabled = false with a backup bucket and a 24h interval")
	}
	if s := c.Redacted(); strings.Contains(s, "sk-r2-secret") {
		t.Errorf("Redacted leaked the R2 secret: %s", s)
	}
}

// A backup bucket with no primary has nothing to inherit its endpoint and keys
// from. Config must name the missing variables; without this check the store
// constructor aborts the boot with a bare "s3 needs an endpoint".
func TestLoad_DocsBackupWithoutAPrimaryNamesTheMissingVars(t *testing.T) {
	env := validBase()
	env["HOME_DOCS_R2_BACKUP_BUCKET"] = "home-docs-backup"
	_, err := Load(envMap(env))
	if err == nil {
		t.Fatal("expected a backup bucket with no connection details to be refused")
	}
	for _, want := range []string{"HOME_DOCS_R2_BACKUP_ENDPOINT", "HOME_DOCS_R2_BACKUP_ACCESS_KEY_ID"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing mention of %s:\n%v", want, err)
		}
	}
}

func TestLoad_DocsMirrorCronRejectsCronExpression(t *testing.T) {
	env := validBase()
	env["HOME_DOCS_MIRROR_CRON"] = "0 * * * *"
	_, err := Load(envMap(env))
	if err == nil || !strings.Contains(err.Error(), "HOME_DOCS_MIRROR_CRON") {
		t.Fatalf("expected a cron-expression rejection, got: %v", err)
	}
}

func TestLoad_DocsMirrorCronZeroDisables(t *testing.T) {
	env := validBase()
	env["HOME_DOCS_MIRROR_CRON"] = "0"
	env["HOME_DOCS_R2_BUCKET"] = "home-docs"
	env["HOME_DOCS_R2_ENDPOINT"] = "https://acct.r2.cloudflarestorage.com"
	env["HOME_DOCS_R2_ACCESS_KEY_ID"] = "ak"
	env["HOME_DOCS_R2_SECRET_ACCESS_KEY"] = "sk-r2-secret"
	env["HOME_DOCS_R2_BACKUP_BUCKET"] = "home-docs-backup"
	c, err := Load(envMap(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Docs.MirrorInterval != 0 || c.Docs.MirrorEnabled() {
		t.Errorf("MirrorInterval = %s / enabled = %t, want disabled", c.Docs.MirrorInterval, c.Docs.MirrorEnabled())
	}
}

func TestLoad_DocsAllowlistAndRangeChecks(t *testing.T) {
	env := validBase()
	env["HOME_DOCS_ALLOWED_MIME"] = "application/pdf, image/jpeg ,"
	env["HOME_DOCS_MAX_UPLOAD_MB"] = "0"
	env["HOME_DOCS_THUMB_MAX_PX"] = "8"
	_, err := Load(envMap(env))
	if err == nil {
		t.Fatal("expected range errors")
	}
	for _, want := range []string{"HOME_DOCS_MAX_UPLOAD_MB", "HOME_DOCS_THUMB_MAX_PX"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing mention of %s:\n%v", want, err)
		}
	}

	env["HOME_DOCS_MAX_UPLOAD_MB"] = "25"
	env["HOME_DOCS_THUMB_MAX_PX"] = "320"
	c, err := Load(envMap(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Docs.AllowedMIME) != 2 || c.Docs.AllowedMIME[0] != "application/pdf" {
		t.Errorf("AllowedMIME = %v, want two trimmed types", c.Docs.AllowedMIME)
	}
}

func TestRedacted_MasksSecret(t *testing.T) {
	c, err := Load(envMap(validBase()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := c.Redacted()
	if strings.Contains(s, "s3cret") {
		t.Errorf("Redacted leaked the secret: %s", s)
	}
	if !strings.Contains(s, "auth_secret=set") {
		t.Errorf("Redacted should mark the secret set: %s", s)
	}
}

// ---- v5: notifications, push (VAPID) and the scheduler ----

// vapidPair returns a syntactically valid keypair: the loader checks encoding and
// byte length, not that the point is on the curve (the push library does that).
func vapidPair() (pub, priv string) {
	return base64.RawURLEncoding.EncodeToString(make([]byte, vapidPublicKeyBytes)),
		base64.RawURLEncoding.EncodeToString(make([]byte, vapidPrivateKeyBytes))
}

func withVAPID(env map[string]string) map[string]string {
	pub, priv := vapidPair()
	env["HOME_VAPID_PUBLIC_KEY"] = pub
	env["HOME_VAPID_PRIVATE_KEY"] = priv
	env["HOME_VAPID_SUBJECT"] = "mailto:karel@tilcer.cz"
	return env
}

func TestLoad_NotifDefaults(t *testing.T) {
	c, err := Load(envMap(validBase()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	n := c.Notif
	if n.PushEnabled() {
		t.Error("PushEnabled = true with no VAPID keypair, want false (push is optional)")
	}
	if n.CoalesceDefault != 60*time.Second {
		t.Errorf("CoalesceDefault = %s, want 60s", n.CoalesceDefault)
	}
	if n.DeliveryRetentionDays != 30 {
		t.Errorf("DeliveryRetentionDays = %d, want 30", n.DeliveryRetentionDays)
	}
	if n.MaxFailDays != 14 {
		t.Errorf("MaxFailDays = %d, want 14", n.MaxFailDays)
	}
	if n.SchedTick != 60*time.Second {
		t.Errorf("SchedTick = %s, want 60s", n.SchedTick)
	}
	if n.CatchupGrace != 120*time.Minute {
		t.Errorf("CatchupGrace = %s, want 120m", n.CatchupGrace)
	}
}

func TestLoad_NotifVAPIDEnablesPush(t *testing.T) {
	c, err := Load(envMap(withVAPID(validBase())))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.Notif.PushEnabled() {
		t.Error("PushEnabled = false with a valid keypair, want true")
	}
}

// A half-configured keypair must fail at boot, not at the first send.
func TestLoad_NotifVAPIDHalfConfigured(t *testing.T) {
	env := validBase()
	pub, _ := vapidPair()
	env["HOME_VAPID_PUBLIC_KEY"] = pub
	_, err := Load(envMap(env))
	if err == nil || !strings.Contains(err.Error(), "HOME_VAPID_PRIVATE_KEY") {
		t.Fatalf("expected a half-configured keypair to be refused, got: %v", err)
	}
}

func TestLoad_NotifVAPIDMalformedKeys(t *testing.T) {
	t.Run("not base64url", func(t *testing.T) {
		env := withVAPID(validBase())
		env["HOME_VAPID_PRIVATE_KEY"] = "not valid base64!!"
		_, err := Load(envMap(env))
		if err == nil || !strings.Contains(err.Error(), "base64url") {
			t.Fatalf("expected a base64 complaint, got: %v", err)
		}
		if strings.Contains(err.Error(), "not valid base64!!") {
			t.Errorf("the error echoed the whole key back; it must be redacted:\n%v", err)
		}
	})
	t.Run("swapped pair", func(t *testing.T) {
		env := validBase()
		pub, priv := vapidPair()
		env["HOME_VAPID_PUBLIC_KEY"], env["HOME_VAPID_PRIVATE_KEY"] = priv, pub
		env["HOME_VAPID_SUBJECT"] = "mailto:karel@tilcer.cz"
		_, err := Load(envMap(env))
		if err == nil || !strings.Contains(err.Error(), "swapped") {
			t.Fatalf("expected the length check to catch a swapped pair, got: %v", err)
		}
	})
	t.Run("padded base64 is accepted", func(t *testing.T) {
		env := withVAPID(validBase())
		env["HOME_VAPID_PRIVATE_KEY"] = base64.StdEncoding.EncodeToString(make([]byte, vapidPrivateKeyBytes))
		if _, err := Load(envMap(env)); err != nil {
			t.Fatalf("padded base64 should be tolerated: %v", err)
		}
	})
}

func TestLoad_NotifVAPIDSubjectRequiredAndShaped(t *testing.T) {
	env := withVAPID(validBase())
	delete(env, "HOME_VAPID_SUBJECT")
	_, err := Load(envMap(env))
	if err == nil || !strings.Contains(err.Error(), "HOME_VAPID_SUBJECT") {
		t.Fatalf("expected a missing subject to be refused, got: %v", err)
	}

	env["HOME_VAPID_SUBJECT"] = "karel@tilcer.cz" // no scheme
	_, err = Load(envMap(env))
	if err == nil || !strings.Contains(err.Error(), "mailto:") {
		t.Fatalf("expected a schemeless subject to be refused, got: %v", err)
	}
}

// A tick longer than a minute would step over whole wall-clock slots.
func TestLoad_NotifSchedTickBounded(t *testing.T) {
	env := validBase()
	env["HOME_SCHED_TICK_SECONDS"] = "300"
	_, err := Load(envMap(env))
	if err == nil || !strings.Contains(err.Error(), "HOME_SCHED_TICK_SECONDS") {
		t.Fatalf("expected a >60s tick to be refused, got: %v", err)
	}
}

func TestLoad_NotifRangeChecks(t *testing.T) {
	env := validBase()
	env["HOME_NOTIF_COALESCE_DEFAULT"] = "-1"
	env["HOME_NOTIF_DELIVERY_RETENTION_DAYS"] = "-5"
	env["HOME_NOTIF_MAX_FAILDAYS"] = "0"
	env["HOME_SCHED_CATCHUP_GRACE"] = "-10"
	_, err := Load(envMap(env))
	if err == nil {
		t.Fatal("expected range errors")
	}
	for _, want := range []string{
		"HOME_NOTIF_COALESCE_DEFAULT", "HOME_NOTIF_DELIVERY_RETENTION_DAYS",
		"HOME_NOTIF_MAX_FAILDAYS", "HOME_SCHED_CATCHUP_GRACE",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing mention of %s:\n%v", want, err)
		}
	}
}

// Zero is the documented "off" value for both retention and coalescing.
func TestLoad_NotifZeroesAreLegal(t *testing.T) {
	env := validBase()
	env["HOME_NOTIF_COALESCE_DEFAULT"] = "0"
	env["HOME_NOTIF_DELIVERY_RETENTION_DAYS"] = "0"
	env["HOME_SCHED_CATCHUP_GRACE"] = "0"
	c, err := Load(envMap(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Notif.CoalesceDefault != 0 || c.Notif.DeliveryRetentionDays != 0 || c.Notif.CatchupGrace != 0 {
		t.Errorf("zeroes should round-trip: %+v", c.Notif)
	}
}

// The endpoint allowlist is what stops a subscription pointing the server at an
// arbitrary host, so the escape hatch has to take HOSTS — a pasted URL would be
// accepted here and then silently match nothing at subscribe time.
func TestLoad_PushEndpointHosts(t *testing.T) {
	env := validBase()
	env["HOME_PUSH_ENDPOINT_HOSTS"] = "push.newbrowser.example, wns2.notify.windows.com"
	c, err := Load(envMap(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"push.newbrowser.example", "wns2.notify.windows.com"}
	if len(c.Notif.PushEndpointHosts) != len(want) {
		t.Fatalf("PushEndpointHosts = %v, want %v", c.Notif.PushEndpointHosts, want)
	}
	for i, h := range want {
		if c.Notif.PushEndpointHosts[i] != h {
			t.Errorf("PushEndpointHosts[%d] = %q, want %q", i, c.Notif.PushEndpointHosts[i], h)
		}
	}

	env["HOME_PUSH_ENDPOINT_HOSTS"] = "https://push.example/"
	if _, err := Load(envMap(env)); err == nil || !strings.Contains(err.Error(), "HOME_PUSH_ENDPOINT_HOSTS") {
		t.Fatalf("expected a URL to be refused, got: %v", err)
	}
}

func TestRedacted_MasksVAPIDKeys(t *testing.T) {
	env := withVAPID(validBase())
	c, err := Load(envMap(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := c.Redacted()
	if strings.Contains(s, env["HOME_VAPID_PRIVATE_KEY"]) || strings.Contains(s, env["HOME_VAPID_PUBLIC_KEY"]) {
		t.Errorf("Redacted leaked a VAPID key: %s", s)
	}
	if !strings.Contains(s, "push=on") {
		t.Errorf("Redacted should report push on: %s", s)
	}
}
