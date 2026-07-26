package config

import (
	"strings"
	"testing"
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
	if c.DevAuthBypass {
		t.Error("DevAuthBypass = true, want false")
	}
}

func TestLoad_MissingRequiredAreAggregated(t *testing.T) {
	_, err := Load(envMap(map[string]string{}))
	if err == nil {
		t.Fatal("expected error for empty environment")
	}
	for _, want := range []string{"HOME_DB_PATH", "AUTH_BASE_URL", "HOME_AUTH_SERVICE_SECRET"} {
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
