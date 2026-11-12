package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) Getenv { return func(k string) string { return m[k] } }

func TestDefaults(t *testing.T) {
	c, err := LoadFrom(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr != ":8080" || c.SiteName != "Accountedfor" || c.RetentionDays != 180 {
		t.Errorf("unexpected defaults: %+v", c)
	}
	if c.PFIFDomain != "localhost" || c.SecureCookies() {
		t.Errorf("domain %q secure %v", c.PFIFDomain, c.SecureCookies())
	}
	if err := c.RequireDatabase(); err == nil {
		t.Error("RequireDatabase should fail without DATABASE_URL")
	}
}

func TestValues(t *testing.T) {
	c, err := LoadFrom(env(map[string]string{
		"BASE_URL":       "https://safe.example.org/",
		"PORT":           "9000",
		"RETENTION_DAYS": "30",
		"PUBLIC_FEED":    "false",
		"TIMEZONE":       "America/Chicago",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr != ":9000" || c.RetentionDays != 30 || c.PublicFeed {
		t.Errorf("values not applied: %+v", c)
	}
	if c.PFIFDomain != "safe.example.org" || !c.SecureCookies() {
		t.Errorf("domain %q", c.PFIFDomain)
	}
	if got := c.URL("/p/x"); got != "https://safe.example.org/p/x" {
		t.Errorf("URL = %q", got)
	}
	if c.Location.String() != "America/Chicago" {
		t.Errorf("location %v", c.Location)
	}
}

func TestErrors(t *testing.T) {
	_, err := LoadFrom(env(map[string]string{
		"BASE_URL":                 "not a url",
		"RETENTION_DAYS":           "-1",
		"PUBLIC_FEED":              "maybe",
		"SMS_DEFAULT_VISIBILITY":   "everyone",
		"TIMEZONE":                 "Mars/Olympus",
		"BOOTSTRAP_ADMIN_PASSWORD": "short",
	}))
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"BASE_URL", "RETENTION_DAYS", "PUBLIC_FEED", "SMS_DEFAULT_VISIBILITY", "TIMEZONE", "BOOTSTRAP_ADMIN_PASSWORD"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}
