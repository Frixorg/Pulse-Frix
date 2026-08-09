// Package config loads control-plane API configuration from the environment.
package config

import (
	"os"
	"strings"
	"time"
)

// Tenancy controls single- vs multi-tenant behaviour. Self-hosted deployments
// run single-tenant; Pulse Cloud runs multi-tenant. The same code runs in both.
type Tenancy string

const (
	SingleTenant Tenancy = "single"
	MultiTenant  Tenancy = "multi"
)

// Config is the API runtime configuration.
type Config struct {
	Addr          string
	Mode          string // "local" or "cloud"
	Tenancy       Tenancy
	DatabaseURL   string
	MetricsURL    string
	SessionSecret string
	JWTSigningKey string
	CORSOrigins   []string
	RateLimitRPS  float64
	Env           string // "development" | "production"

	// Feature flags — the safest value is the default.
	EnableConfigMutation bool
	EnableAutoTLS        bool
	EnableRemoteActions  bool
	EnableAutoUpdate     bool

	// Public base URL (used to build OIDC redirect URIs), e.g. https://pulse.frix.me.
	PublicURL string
	// OIDC providers configured from the environment (Google, Telegram, ...).
	OIDC []OIDCProvider
}

// OIDCProvider is one configured OpenID Connect identity provider.
type OIDCProvider struct {
	Name         string   // url slug, e.g. "google", "telegram"
	Display      string   // human label
	Issuer       string   // OIDC issuer (discovery = issuer + /.well-known/openid-configuration)
	ClientID     string
	ClientSecret string
	Scopes       []string
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func boolEnv(k string, def bool) bool {
	switch strings.ToLower(env(k, "")) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

// Load builds the configuration from the environment with safe defaults.
func Load() *Config {
	mode := env("PULSE_MODE", "local")
	tenancy := SingleTenant
	if mode == "cloud" {
		tenancy = MultiTenant
	}
	c := &Config{
		Addr:          env("PULSE_API_ADDR", ":8080"),
		Mode:          mode,
		Tenancy:       tenancy,
		DatabaseURL:   env("DATABASE_URL", ""),
		MetricsURL:    env("METRICS_URL", "http://pulse-prometheus:9090"),
		SessionSecret: env("PULSE_SESSION_SECRET", ""),
		JWTSigningKey: env("PULSE_JWT_SIGNING_KEY", ""),
		CORSOrigins:   splitCSV(env("PULSE_CORS_ORIGINS", "")),
		RateLimitRPS:  10,
		Env:           env("PULSE_ENV", "development"),

		EnableConfigMutation: boolEnv("ENABLE_CONFIG_MUTATION", false),
		EnableAutoTLS:        boolEnv("ENABLE_AUTO_TLS", false),
		EnableRemoteActions:  boolEnv("ENABLE_REMOTE_ACTIONS", false),
		EnableAutoUpdate:     boolEnv("ENABLE_AUTO_UPDATE", false),
	}

	// Public URL for OIDC redirects: explicit PULSE_PUBLIC_URL, else derived
	// from the dashboard domain.
	c.PublicURL = strings.TrimRight(env("PULSE_PUBLIC_URL", ""), "/")
	if c.PublicURL == "" {
		if d := env("DASHBOARD_DOMAIN", ""); d != "" {
			c.PublicURL = "https://" + d
		}
	}

	// Configure OIDC providers when their client credentials are present.
	if id := env("GOOGLE_CLIENT_ID", ""); id != "" {
		c.OIDC = append(c.OIDC, OIDCProvider{
			Name: "google", Display: "Google",
			Issuer:       "https://accounts.google.com",
			ClientID:     id,
			ClientSecret: env("GOOGLE_CLIENT_SECRET", ""),
			Scopes:       []string{"openid", "email", "profile"},
		})
	}
	if id := env("TELEGRAM_CLIENT_ID", ""); id != "" {
		c.OIDC = append(c.OIDC, OIDCProvider{
			Name: "telegram", Display: "Telegram",
			Issuer:       env("TELEGRAM_ISSUER", "https://oauth.telegram.org"),
			ClientID:     id,
			ClientSecret: env("TELEGRAM_CLIENT_SECRET", ""),
			Scopes:       splitCSV(env("TELEGRAM_SCOPES", "openid")),
		})
	}
	return c
}

// OIDCProviderByName returns a configured provider by its url slug.
func (c *Config) OIDCProviderByName(name string) (OIDCProvider, bool) {
	for _, p := range c.OIDC {
		if p.Name == name {
			return p, true
		}
	}
	return OIDCProvider{}, false
}

// SessionTTL is how long a web session lasts.
func (c *Config) SessionTTL() time.Duration { return 12 * time.Hour }

// EnrollmentTTL is how long an enrollment token is valid.
func (c *Config) EnrollmentTTL() time.Duration { return 15 * time.Minute }

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
