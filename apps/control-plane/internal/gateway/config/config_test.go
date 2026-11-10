package config

import (
	"os"
	"path/filepath"
	"testing"

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
	token := writeFile(t, "lookup-token\n")
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
  webhook_route_lookup_token_file: `+token+`
  route_cache_ttl: 2m
  max_body_bytes: 1024
  github:
    webhook_secret_file: `+secret+`
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
	assert.Equal(t, "/hooks/github", cfg.Gateway.GitHub.Path)
	lookupToken, err := cfg.LookupToken()
	require.NoError(t, err)
	assert.Equal(t, "lookup-token", lookupToken)
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
			RouteCacheTTL:   1,
			MaxBodyBytes:    1,
			ControlPlaneURL: "http://localhost:8660",
			GitHub:          GatewayGitHub{Path: "/webhooks/github"},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gateway.webhook_route_lookup_token_file is required")
	assert.Contains(t, err.Error(), "gateway.github.webhook_secret_file is required")
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
