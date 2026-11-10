// Package config is the gateway's configuration. It mirrors the parts of the
// shared configuration file the gateway needs, because a component must not
// import the control plane's config package.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// Config is the gateway's configuration. It reads the same file as the
// control plane: logging, redis, events.stream, and
// version_control.github.web_url are shared, and gateway.* carries the
// gateway's own settings.
type Config struct {
	Logging        Logging        `yaml:"logging"`
	Redis          Redis          `yaml:"redis"`
	Events         Events         `yaml:"events"`
	VersionControl VersionControl `yaml:"version_control"`
	Gateway        Gateway        `yaml:"gateway"`
}

// Logging is the gateway's logging configuration, with the same keys as the
// control plane's.
type Logging struct {
	Level         string `yaml:"level" env:"IVALDI_LOG_LEVEL" envDefault:"info"`
	Output        string `yaml:"output" env:"IVALDI_LOG_OUTPUT" envDefault:"console"`
	Format        string `yaml:"format" env:"IVALDI_LOG_FORMAT" envDefault:"json"`
	Directory     string `yaml:"directory" env:"IVALDI_LOG_DIRECTORY" envDefault:"logs"`
	FilePrefix    string `yaml:"file_prefix" env:"IVALDI_LOG_FILE_PREFIX" envDefault:"ivaldi-gateway"`
	MaxFileSizeMB int    `yaml:"max_file_size_mb" env:"IVALDI_LOG_MAX_FILE_SIZE_MB" envDefault:"50"`
	MaxAgeDays    int    `yaml:"max_age_days" env:"IVALDI_LOG_MAX_AGE_DAYS" envDefault:"7"`
}

// Redis is the gateway's Redis connection. It mirrors the control plane's
// settings but may use its own ACL user and password file.
type Redis struct {
	Address      string        `yaml:"address" env:"IVALDI_REDIS_ADDRESS" envDefault:"localhost:6379"`
	Username     string        `yaml:"username" env:"IVALDI_REDIS_USERNAME"`
	PasswordFile string        `yaml:"password_file" env:"IVALDI_REDIS_PASSWORD_FILE"`
	Database     int           `yaml:"database" env:"IVALDI_REDIS_DATABASE" envDefault:"0"`
	TLS          bool          `yaml:"tls" env:"IVALDI_REDIS_TLS" envDefault:"false"`
	CAFile       string        `yaml:"ca_file" env:"IVALDI_REDIS_CA_FILE"`
	DialTimeout  time.Duration `yaml:"dial_timeout" env:"IVALDI_REDIS_DIAL_TIMEOUT" envDefault:"5s"`
}

// Password reads the Redis password from its file, or returns "" when none is
// configured.
func (r Redis) Password() (string, error) {
	if r.PasswordFile == "" {
		return "", nil
	}
	b, err := os.ReadFile(r.PasswordFile)
	if err != nil {
		return "", fmt.Errorf("redis.password_file: %w", err)
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

// Events is the stream the gateway publishes to.
type Events struct {
	Stream string `yaml:"stream" env:"IVALDI_EVENTS_STREAM" envDefault:"ivaldi:gateway:events"`
}

// VersionControl is the small part of the control plane's settings the
// gateway needs: the GitHub host its route keys are built from.
type VersionControl struct {
	GitHub GitHub `yaml:"github"`
}

// GitHub is the configured GitHub host.
type GitHub struct {
	WebURL string `yaml:"web_url" env:"IVALDI_VERSION_CONTROL_GITHUB_WEB_URL"`
}

// Host returns the GitHub host the gateway uses, defaulting to github.com.
func (g GitHub) Host() string {
	if strings.TrimSpace(g.WebURL) == "" {
		return "https://github.com"
	}
	return strings.TrimSuffix(g.WebURL, "/")
}

// Gateway is the gateway's own configuration.
type Gateway struct {
	Host                        string        `yaml:"host" env:"IVALDI_GATEWAY_HOST"`
	Port                        int           `yaml:"port" env:"IVALDI_GATEWAY_PORT" envDefault:"8663"`
	ShutdownTimeout             time.Duration `yaml:"shutdown_timeout" env:"IVALDI_GATEWAY_SHUTDOWN_TIMEOUT" envDefault:"30s"`
	ControlPlaneURL             string        `yaml:"control_plane_url" env:"IVALDI_GATEWAY_CONTROL_PLANE_URL" envDefault:"http://localhost:8660"`
	WebhookRouteLookupTokenFile string        `yaml:"webhook_route_lookup_token_file" env:"IVALDI_GATEWAY_WEBHOOK_ROUTE_LOOKUP_TOKEN_FILE"`
	RouteCacheTTL               time.Duration `yaml:"route_cache_ttl" env:"IVALDI_GATEWAY_ROUTE_CACHE_TTL" envDefault:"5m"`
	MaxBodyBytes                int64         `yaml:"max_body_bytes" env:"IVALDI_GATEWAY_MAX_BODY_BYTES" envDefault:"26214400"`
	GitHub                      GatewayGitHub `yaml:"github"`
}

// GatewayGitHub is the gateway's GitHub webhook configuration.
type GatewayGitHub struct {
	WebhookSecretFile string `yaml:"webhook_secret_file" env:"IVALDI_GATEWAY_GITHUB_WEBHOOK_SECRET_FILE"`
	Path              string `yaml:"path" env:"IVALDI_GATEWAY_GITHUB_PATH" envDefault:"/webhooks/github"`
}

// ControlPlaneBaseURL returns the control plane's API base, without a
// trailing slash.
func (g Gateway) ControlPlaneBaseURL() (string, error) {
	raw := strings.TrimSuffix(g.ControlPlaneURL, "/")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("gateway.control_plane_url %q must be an absolute http or https URL", g.ControlPlaneURL)
	}
	return raw, nil
}

// LookupToken reads the deployment token the gateway presents to the control
// plane.
func (c *Config) LookupToken() (string, error) {
	return readSecret("gateway.webhook_route_lookup_token_file", c.Gateway.WebhookRouteLookupTokenFile)
}

// WebhookSecret reads the GitHub App's webhook signing secret.
func (c *Config) WebhookSecret() (string, error) {
	return readSecret("gateway.github.webhook_secret_file", c.Gateway.GitHub.WebhookSecretFile)
}

// ValidationError lists every problem with a configuration.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "invalid gateway configuration:\n  - " + strings.Join(e.Problems, "\n  - ")
}

// Validate checks what the gateway needs before it starts.
func (c *Config) Validate() error {
	v := &validator{}
	g := c.Gateway

	if g.Port <= 0 || g.Port > 65535 {
		v.addf("gateway.port %d must be between 1 and 65535", g.Port)
	}
	if g.ShutdownTimeout <= 0 {
		v.addf("gateway.shutdown_timeout must be positive")
	}
	if g.RouteCacheTTL <= 0 {
		v.addf("gateway.route_cache_ttl must be positive")
	}
	if g.MaxBodyBytes <= 0 {
		v.addf("gateway.max_body_bytes must be positive")
	}
	if g.GitHub.Path == "" || !strings.HasPrefix(g.GitHub.Path, "/") {
		v.addf("gateway.github.path %q must start with /", g.GitHub.Path)
	}
	v.absoluteURL("gateway.control_plane_url", g.ControlPlaneURL)
	v.httpsURL("version_control.github.web_url", c.VersionControl.GitHub.Host())
	v.secretFile("gateway.webhook_route_lookup_token_file", g.WebhookRouteLookupTokenFile)
	v.secretFile("gateway.github.webhook_secret_file", g.GitHub.WebhookSecretFile)
	if c.Events.Stream == "" {
		v.addf("events.stream is required")
	}
	if c.Redis.Address == "" {
		v.addf("redis.address is required")
	}
	if c.Redis.DialTimeout <= 0 {
		v.addf("redis.dial_timeout must be positive")
	}
	if c.Redis.CAFile != "" && !c.Redis.TLS {
		v.addf("redis.ca_file needs redis.tls")
	}

	if len(v.problems) > 0 {
		return &ValidationError{Problems: v.problems}
	}
	return nil
}

type validator struct {
	problems []string
}

func (v *validator) addf(format string, args ...any) {
	v.problems = append(v.problems, fmt.Sprintf(format, args...))
}

func (v *validator) absoluteURL(name, raw string) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		v.addf("%s %q must be an absolute http or https URL", name, raw)
	}
}

func (v *validator) httpsURL(name, raw string) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Scheme != "https" {
		v.addf("%s %q must be an absolute https URL", name, raw)
	}
}

func (v *validator) secretFile(name, path string) {
	if path == "" {
		v.addf("%s is required", name)
		return
	}
	if _, err := readSecret(name, path); err != nil {
		v.addf("%v", err)
	}
}

func readSecret(name, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	secret := strings.TrimRight(string(b), "\r\n")
	if secret == "" {
		return "", fmt.Errorf("%s is empty", name)
	}
	return secret, nil
}
