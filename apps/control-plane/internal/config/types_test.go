package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/env"
)

func lookup(vars map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := vars[k]
		return v, ok
	}
}

func TestDefaults(t *testing.T) {
	var c Config
	require.NoError(t, env.Parse(&c, env.Options{Source: lookup(nil)}))

	assert.Equal(t, "http://localhost:8660", c.ExternalURL)
	assert.Equal(t, 30*time.Second, c.ShutdownTimeout)
	assert.Equal(t, 8661, c.Runners.Port)
	assert.Equal(t, time.Hour, c.Runners.EnrollmentTokenTTL)
	assert.Equal(t, 24*time.Hour, c.Runners.WorkloadCredentialTTL)
	assert.Equal(t, 15*time.Minute, c.Auth.AccessTokenTTL)
	assert.Equal(t, 15*time.Minute, c.Auth.SessionCredentialTTL)
	assert.False(t, c.Auth.BuiltinAccounts, "built-in accounts are opt-in")
	assert.False(t, c.Auth.EphemeralSigningKey)
	assert.Equal(t, 10*time.Minute, c.Auth.SignInSessionTTL)
	assert.Equal(t, EncryptionLocal, c.Encryption.Provider)
	assert.Equal(t, "localhost:6379", c.Redis.Address)
	assert.Equal(t, 5*time.Second, c.Redis.DialTimeout)
	assert.Equal(t, "ivaldi:gateway:events", c.Events.Stream)
	assert.Equal(t, "control-plane", c.Events.ConsumerGroup)
	assert.Equal(t, 5*time.Minute, c.Events.ClaimIdleTimeout)
	assert.Equal(t, 5, c.Events.MaxDeliveries)
	assert.Equal(t, "ivaldi:gateway:events:dead", c.Events.DeadLetterStream)
	assert.Equal(t, "ivaldi:control-plane:events", c.Outbox.Stream)
	assert.Equal(t, 8662, c.Telemetry.Port)
	assert.InDelta(t, 0.1, c.Telemetry.TraceSampleRatio, 1e-9)
	assert.False(t, c.Persistence.MigrateOnStart)
	assert.Equal(t, "logs", c.Logging.Directory)
}

func TestProviders(t *testing.T) {
	secretFile := filepath.Join(t.TempDir(), "github-secret")
	require.NoError(t, os.WriteFile(secretFile, []byte("gh-secret\n"), 0o600))
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
auth:
  providers:
    google:
      client_id: 123.apps.googleusercontent.com
      client_secret: ignored-in-yaml
      allowed_domains: [acme.com]
    github:
      client_id: Iv1.abc
      client_secret_file: `+secretFile+`
      allowed_organizations: [acme]
`), 0o600))

	var c Config
	require.NoError(t, env.Load(&c, env.Options{YAMLPath: path, Source: lookup(map[string]string{
		"IVALDI_AUTH_GOOGLE_CLIENT_SECRET": "g-secret",
	})}))
	p := c.Auth.Providers

	assert.True(t, p.Google.Enabled())
	assert.True(t, p.GitHub.Enabled())
	assert.False(t, p.OIDC.Enabled(), "a provider without a client ID is off")

	google, err := p.Google.Secret()
	require.NoError(t, err)
	assert.Equal(t, "g-secret", google.Reveal(), "the secret comes from the environment; YAML cannot set it")
	assert.Equal(t, []string{"acme.com"}, p.Google.AllowedDomains)

	github, err := p.GitHub.Secret()
	require.NoError(t, err)
	assert.Equal(t, "gh-secret", github.Reveal(), "read from the file, without its trailing newline")
	assert.Equal(t, "https://github.com", p.GitHub.URL)
	assert.Equal(t, []string{"acme"}, p.GitHub.AllowedOrganizations)

	assert.Equal(t, []string{"email", "profile"}, p.OIDC.Scopes)
	assert.Equal(t, "Single sign-on", p.OIDC.DisplayName)
}

func TestProviderSecretNeedsOneSource(t *testing.T) {
	both := GitHubProvider{ClientSecret: "x", ClientSecretFile: "/run/secret"}
	_, err := both.Secret()
	assert.ErrorContains(t, err, "not both")

	missing := GitHubProvider{ClientSecretFile: filepath.Join(t.TempDir(), "absent")}
	_, err = missing.Secret()
	assert.Error(t, err)

	none, err := GitHubProvider{}.Secret()
	require.NoError(t, err)
	assert.Empty(t, none.Reveal())
}

func TestSecretIsRedacted(t *testing.T) {
	c := Config{Auth: Auth{Providers: Providers{GitHub: GitHubProvider{ClientID: "Iv1.abc", ClientSecret: "hunter2"}}}}

	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		assert.NotContains(t, fmt.Sprintf(format, c), "hunter2", format)
	}
	b, err := json.Marshal(c)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "hunter2")
	assert.Contains(t, string(b), redacted)
	assert.Equal(t, "hunter2", c.Auth.Providers.GitHub.ClientSecret.Reveal())
	assert.Empty(t, Secret("").String(), "an unset secret prints as empty, so it is clear it is unset")
}

func TestEnvironmentOverridesYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
external_url: https://ivaldi.example.com
auth:
  signing_key_files: [/keys/current.pem, /keys/previous.pem]
  access_token_ttl: 5m
encryption:
  provider: aws-kms
  aws_kms_key_id: alias/ivaldi
redis:
  address: redis.internal:6380
  tls: true
events:
  consumer_name: cp-0
`), 0o600))

	var c Config
	require.NoError(t, env.Load(&c, env.Options{YAMLPath: path, Source: lookup(map[string]string{
		"IVALDI_AUTH_ACCESS_TOKEN_TTL": "10m",
		"IVALDI_EVENTS_CONCURRENCY":    "32",
	})}))

	assert.Equal(t, "https://ivaldi.example.com", c.ExternalURL)
	assert.Equal(t, []string{"/keys/current.pem", "/keys/previous.pem"}, c.Auth.SigningKeyFiles)
	assert.Equal(t, 10*time.Minute, c.Auth.AccessTokenTTL)
	assert.Equal(t, 720*time.Hour, c.Auth.RefreshTokenTTL, "unset keys keep their defaults")
	assert.Equal(t, EncryptionAWSKMS, c.Encryption.Provider)
	assert.Equal(t, "alias/ivaldi", c.Encryption.AWSKMSKeyID)
	assert.Equal(t, "redis.internal:6380", c.Redis.Address)
	assert.True(t, c.Redis.TLS)
	assert.Equal(t, "cp-0", c.Events.ConsumerName)
	assert.Equal(t, 32, c.Events.Concurrency)
	assert.Equal(t, "control-plane", c.Events.ConsumerGroup, "unset keys keep their defaults")
}
