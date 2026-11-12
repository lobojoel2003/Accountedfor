// Package config loads Accountedfor's settings from environment variables.
//
// Every setting has a sensible default so a deployment only needs
// DATABASE_URL and BASE_URL to start. See .env.example for documentation of
// each variable.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds every runtime setting.
type Config struct {
	DatabaseURL string
	ListenAddr  string
	BaseURL     *url.URL

	SiteName        string
	EventName       string
	OperatorName    string
	OperatorContact string
	Notice          string
	DefaultLanguage string
	Location        *time.Location

	PFIFDomain    string
	PublicFeed    bool
	RetentionDays int

	SMSNumber            string
	SMSDefaultVisibility string
	TwilioAuthToken      string
	SMSWebhookSecret     string

	TrustProxy         bool
	PostsPerHourPerIP  int
	SMSPerHourPerPhone int
	SessionTTL         time.Duration

	BootstrapAdminUser     string
	BootstrapAdminPassword string

	LogFormat string
	LogLevel  string
}

// Getenv is the lookup function used by Load. Tests replace it.
type Getenv func(string) string

// Load reads the configuration from the process environment.
func Load() (*Config, error) { return LoadFrom(os.Getenv) }

// LoadFrom reads the configuration using the provided lookup function.
func LoadFrom(getenv Getenv) (*Config, error) {
	str := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	var errs []error
	integer := func(key string, def int) int {
		v := strings.TrimSpace(getenv(key))
		if v == "" {
			return def
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			errs = append(errs, fmt.Errorf("%s must be a non-negative integer, got %q", key, v))
			return def
		}
		return n
	}
	boolean := func(key string, def bool) bool {
		v := strings.TrimSpace(getenv(key))
		if v == "" {
			return def
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s must be true or false, got %q", key, v))
			return def
		}
		return b
	}

	c := &Config{
		DatabaseURL:            str("DATABASE_URL", ""),
		ListenAddr:             str("LISTEN_ADDR", ""),
		SiteName:               str("SITE_NAME", "Accountedfor"),
		EventName:              str("EVENT_NAME", ""),
		OperatorName:           str("OPERATOR_NAME", ""),
		OperatorContact:        str("OPERATOR_CONTACT", ""),
		Notice:                 str("NOTICE", ""),
		DefaultLanguage:        str("DEFAULT_LANGUAGE", "en"),
		PFIFDomain:             str("PFIF_DOMAIN", ""),
		PublicFeed:             boolean("PUBLIC_FEED", true),
		RetentionDays:          integer("RETENTION_DAYS", 180),
		SMSNumber:              str("SMS_NUMBER", ""),
		SMSDefaultVisibility:   str("SMS_DEFAULT_VISIBILITY", "limited"),
		TwilioAuthToken:        str("TWILIO_AUTH_TOKEN", ""),
		SMSWebhookSecret:       str("SMS_WEBHOOK_SECRET", ""),
		TrustProxy:             boolean("TRUST_PROXY", false),
		PostsPerHourPerIP:      integer("RATE_LIMIT_POSTS_PER_HOUR", 120),
		SMSPerHourPerPhone:     integer("RATE_LIMIT_SMS_PER_HOUR", 30),
		SessionTTL:             time.Duration(integer("SESSION_HOURS", 12)) * time.Hour,
		BootstrapAdminUser:     str("BOOTSTRAP_ADMIN_USER", "admin"),
		BootstrapAdminPassword: str("BOOTSTRAP_ADMIN_PASSWORD", ""),
		LogFormat:              str("LOG_FORMAT", "text"),
		LogLevel:               str("LOG_LEVEL", "info"),
	}

	tz := str("TIMEZONE", "UTC")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		errs = append(errs, fmt.Errorf("TIMEZONE must be an IANA time zone such as America/Chicago, got %q", tz))
		loc = time.UTC
	}
	c.Location = loc

	if c.ListenAddr == "" {
		if port := strings.TrimSpace(getenv("PORT")); port != "" {
			c.ListenAddr = ":" + port
		} else {
			c.ListenAddr = ":8080"
		}
	}

	rawBase := strings.TrimRight(str("BASE_URL", "http://localhost:8080"), "/")
	base, err := url.Parse(rawBase)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		errs = append(errs, fmt.Errorf("BASE_URL must be an absolute http(s) URL, got %q", rawBase))
		base, _ = url.Parse("http://localhost:8080")
	}
	c.BaseURL = base

	if c.PFIFDomain == "" {
		c.PFIFDomain = base.Hostname()
	}
	if strings.ContainsAny(c.PFIFDomain, "/ ") {
		errs = append(errs, fmt.Errorf("PFIF_DOMAIN must be a bare domain name, got %q", c.PFIFDomain))
	}

	switch c.SMSDefaultVisibility {
	case "public", "limited", "private":
	default:
		errs = append(errs, fmt.Errorf("SMS_DEFAULT_VISIBILITY must be public, limited or private, got %q", c.SMSDefaultVisibility))
	}
	if c.SessionTTL <= 0 {
		c.SessionTTL = 12 * time.Hour
	}
	if c.BootstrapAdminPassword != "" && len(c.BootstrapAdminPassword) < 12 {
		errs = append(errs, errors.New("BOOTSTRAP_ADMIN_PASSWORD must be at least 12 characters"))
	}
	return c, errors.Join(errs...)
}

// RequireDatabase returns an error when DATABASE_URL is missing.
func (c *Config) RequireDatabase() error {
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is not set (example: postgres://accountedfor:secret@localhost:5432/accountedfor?sslmode=disable)")
	}
	return nil
}

// SecureCookies reports whether cookies should carry the Secure attribute.
func (c *Config) SecureCookies() bool { return c.BaseURL.Scheme == "https" }

// URL joins path onto the configured BASE_URL.
func (c *Config) URL(path string) string {
	return strings.TrimRight(c.BaseURL.String(), "/") + path
}

// Retention returns how long new records are kept, or 0 for forever.
func (c *Config) Retention() time.Duration {
	return time.Duration(c.RetentionDays) * 24 * time.Hour
}
