package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/env"
)

func writeFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func noEnv(string) (string, bool) { return "", false }

func TestConfigReadsTheSharedFile(t *testing.T) {
	privateKey := privateKeyFile(t)
	secret := writeFile(t, "webhook-secret\n")
	path := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(path, []byte(`
logging:
  level: debug
  format: text
redis:
  address: redis.internal:6380
events:
  stream: ivaldi:gateway:events
version_control:
  github:
    web_url: https://ghe.example.com
gateway:
  host: 127.0.0.1
  port: 8663
  control_plane_url: https://ivaldi.example.com
  identity:
    private_key_file: `+privateKey+`
  webhooks:
    route_cache_ttl: 2m
    max_body_bytes: 1024
    request_timeout: 15s
    github:
      secret_file: `+secret+`
      path: /hooks/github
`), 0o600))

	var cfg Config
	require.NoError(t, env.Load(&cfg, env.Options{YAMLPath: path, Source: noEnv}))
	require.NoError(t, cfg.Validate())

	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "redis.internal:6380", cfg.Redis.Address)
	assert.Equal(t, "ivaldi:gateway:events", cfg.Events.Stream)
	assert.Equal(t, "https://ghe.example.com", cfg.VersionControl.GitHub.Host())
	assert.Equal(t, 8663, cfg.Gateway.Port)
	assert.Equal(t, "/hooks/github", cfg.Gateway.Webhooks.GitHub.Path)
	assert.Equal(t, 2*time.Minute, cfg.Gateway.Webhooks.RouteCacheTTL)
	assert.Equal(t, int64(1024), cfg.Gateway.Webhooks.MaxBodyBytes)
	assert.Equal(t, 15*time.Second, cfg.Gateway.Webhooks.RequestTimeout)
	signer, err := cfg.Signer()
	require.NoError(t, err)
	assert.NotEmpty(t, signer.KeyID())
	webhookSecret, err := cfg.WebhookSecret()
	require.NoError(t, err)
	assert.Equal(t, "webhook-secret", webhookSecret)
}

func TestValidateReportsMissingFiles(t *testing.T) {
	cfg := Config{
		Events: Events{Stream: "events"},
		Redis:  Redis{Address: "redis:6379"},
		Gateway: Gateway{
			Port:            8663,
			ShutdownTimeout: 1,
			ControlPlaneURL: "http://localhost:8660",
			Webhooks: Webhooks{
				Enabled:        true,
				RouteCacheTTL:  1,
				MaxBodyBytes:   1,
				RequestTimeout: 1,
				GitHub:         WebhooksGitHub{Path: "/webhooks/github"},
			},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gateway.identity.private_key_file is required")
	assert.Contains(t, err.Error(), "gateway.webhooks.github.secret_file is required")
}

func TestControlPlaneBaseURLMustBeAbsolute(t *testing.T) {
	_, err := Gateway{ControlPlaneURL: "not a URL"}.ControlPlaneBaseURL()
	assert.Error(t, err)

	got, err := Gateway{ControlPlaneURL: "https://ivaldi.example.com/"}.ControlPlaneBaseURL()
	require.NoError(t, err)
	assert.Equal(t, "https://ivaldi.example.com", got)
}

func TestGitHubHostDefaultsToGitHubDotCom(t *testing.T) {
	assert.Equal(t, "https://github.com", GitHub{}.Host())
	assert.Equal(t, "https://ghe.example.com", GitHub{WebURL: "https://ghe.example.com/"}.Host())
}

func TestADisabledModuleNeedsNoSettings(t *testing.T) {
	cfg := Config{Gateway: Gateway{Port: 8663, ShutdownTimeout: 1}}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no gateway module is enabled", "a gateway serves at least one module")
	assert.NotContains(t, err.Error(), "secret_file", "a disabled module's settings are not checked")
	assert.NotContains(t, err.Error(), "redis", "nor Redis, which only webhooks use")
}

func TestWebhooksAreEnabledByDefault(t *testing.T) {
	var cfg Config
	require.NoError(t, env.Load(&cfg, env.Options{Source: noEnv}))
	assert.True(t, cfg.Gateway.Webhooks.Enabled)
}

// privateKeyFile writes a PEM Ed25519 private key and returns its path.
func privateKeyFile(t *testing.T) string {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)
	return writeFile(t, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})))
}
