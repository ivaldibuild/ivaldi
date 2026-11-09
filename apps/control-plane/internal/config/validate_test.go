package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/env"
)

// validConfig is the defaults plus what Validate needs: a signing key.
func validConfig(t *testing.T) *Config {
	t.Helper()
	var c Config
	require.NoError(t, env.Parse(&c, env.Options{Source: lookup(nil)}))
	c.Auth.SigningKeyFiles = []string{"/keys/current.pem"}
	c.Auth.LoginURL = "http://localhost:3000/login"
	return &c
}

func writeFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func TestValidateAcceptsAValidConfig(t *testing.T) {
	t.Run("self-hosted with every provider and bootstrap", func(t *testing.T) {
		c := validConfig(t)
		c.ExternalURL = "https://ivaldi.example.com/"
		c.Auth.BuiltinAccounts = true
		c.Auth.WebClient.RedirectURIs = []string{"https://app.example.com/auth/callback", "http://localhost:3000/auth/callback"}
		c.Auth.Providers.Google = GoogleProvider{ClientID: "g", ClientSecret: "s", AllowedDomains: []string{"acme.com"}}
		c.Auth.Providers.GitHub = GitHubProvider{ClientID: "gh", ClientSecretFile: writeFile(t, "s\n"), URL: "https://github.com", AllowedOrganizations: []string{"acme"}}
		c.Auth.Providers.OIDC = OIDCProvider{ClientID: "o", ClientSecret: "s", IssuerURL: "https://sso.acme.com"}
		c.Bootstrap = Bootstrap{OrganizationName: "Acme", OrganizationSlug: "acme", AdminEmail: "ada@acme.com", AdminPasswordFile: writeFile(t, "pw")}

		assert.NoError(t, c.Validate())
	})
	t.Run("cloud with platform providers", func(t *testing.T) {
		c := validConfig(t)
		c.DeploymentMode = string(DeploymentModeCloud)
		c.ExternalURL = "https://cloud.ivaldi.dev"
		c.Auth.Platform.Google = PlatformGoogleProvider{ClientID: "g", ClientSecret: "s"}
		c.Auth.Platform.GitHub = PlatformGitHubProvider{ClientID: "gh", ClientSecret: "s"}

		assert.NoError(t, c.Validate())
	})
	t.Run("ephemeral signing key instead of key files", func(t *testing.T) {
		c := validConfig(t)
		c.Auth.SigningKeyFiles = nil
		c.Auth.EphemeralSigningKey = true

		assert.NoError(t, c.Validate())
	})
}

func TestValidateRefuses(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, c *Config)
		want   string
	}{
		{"unknown deployment mode", func(_ *testing.T, c *Config) { c.DeploymentMode = "hybrid" }, `deployment_mode "hybrid"`},
		{"relative external URL", func(_ *testing.T, c *Config) { c.ExternalURL = "ivaldi.example.com" }, "absolute http or https"},
		{"external URL with a path", func(_ *testing.T, c *Config) { c.ExternalURL = "https://example.com/ivaldi" }, "no path"},
		{"plain http to a public host", func(_ *testing.T, c *Config) { c.ExternalURL = "http://ivaldi.example.com" }, "must use https"},
		{"no signing key", func(_ *testing.T, c *Config) { c.Auth.SigningKeyFiles = nil }, "signing_key_files"},
		{"no login page", func(_ *testing.T, c *Config) { c.Auth.LoginURL = "" }, "auth.login_url is required"},
		{"login page over plain http", func(_ *testing.T, c *Config) { c.Auth.LoginURL = "http://app.example.com/login" }, "auth.login_url"},
		{"zero token lifetime", func(_ *testing.T, c *Config) { c.Auth.AccessTokenTTL = 0 }, "auth.access_token_ttl must be positive"},
		{"web redirect over plain http", func(_ *testing.T, c *Config) {
			c.Auth.WebClient.RedirectURIs = []string{"http://app.example.com/cb"}
		}, "must use https"},
		{"web redirect with a fragment", func(_ *testing.T, c *Config) {
			c.Auth.WebClient.RedirectURIs = []string{"https://app.example.com/cb#x"}
		}, "without a fragment"},
		{"unrestricted Google", func(_ *testing.T, c *Config) {
			c.Auth.Providers.Google = GoogleProvider{ClientID: "g", ClientSecret: "s"}
		}, "google needs allowed_domains or allowed_emails"},
		{"unrestricted GitHub", func(_ *testing.T, c *Config) {
			c.Auth.Providers.GitHub = GitHubProvider{ClientID: "gh", ClientSecret: "s", URL: "https://github.com"}
		}, "github needs allowed_organizations or allowed_emails"},
		{"provider without a secret", func(_ *testing.T, c *Config) {
			c.Auth.Providers.OIDC = OIDCProvider{ClientID: "o", IssuerURL: "https://sso.acme.com"}
		}, "auth.providers.oidc client secret is required"},
		{"provider secret file missing", func(t *testing.T, c *Config) {
			c.Auth.Providers.OIDC = OIDCProvider{ClientID: "o", ClientSecretFile: filepath.Join(t.TempDir(), "absent"), IssuerURL: "https://sso.acme.com"}
		}, "oidc client secret"},
		{"OIDC without an issuer", func(_ *testing.T, c *Config) {
			c.Auth.Providers.OIDC = OIDCProvider{ClientID: "o", ClientSecret: "s"}
		}, "issuer_url"},
		{"OIDC issuer over plain http", func(_ *testing.T, c *Config) {
			c.Auth.Providers.OIDC = OIDCProvider{ClientID: "o", ClientSecret: "s", IssuerURL: "http://localhost:8080/realms/acme"}
		}, "must be an absolute https URL"},
		{"platform providers when self-hosted", func(_ *testing.T, c *Config) {
			c.Auth.Platform.GitHub = PlatformGitHubProvider{ClientID: "gh", ClientSecret: "s"}
		}, "auth.platform providers are only used in cloud mode"},
		{"installation providers in the cloud", func(_ *testing.T, c *Config) {
			c.DeploymentMode = string(DeploymentModeCloud)
			c.Auth.Providers.GitHub = GitHubProvider{ClientID: "gh", ClientSecret: "s", URL: "https://github.com", AllowedEmails: []string{"a@b.c"}}
		}, "auth.providers are only used in self-hosted mode"},
		{"bootstrap in the cloud", func(_ *testing.T, c *Config) {
			c.DeploymentMode = string(DeploymentModeCloud)
			c.Bootstrap = Bootstrap{OrganizationName: "Acme", AdminEmail: "ada@acme.com"}
		}, "bootstrap is only used in self-hosted mode"},
		{"bootstrap without an admin", func(_ *testing.T, c *Config) {
			c.Bootstrap = Bootstrap{OrganizationName: "Acme"}
		}, "bootstrap.admin_email is required"},
		{"bootstrap with a malformed admin email", func(_ *testing.T, c *Config) {
			c.Bootstrap = Bootstrap{OrganizationName: "Acme", AdminEmail: "Ada <ada@acme.com>"}
		}, "is not an email address"},
		{"admin password without built-in accounts", func(t *testing.T, c *Config) {
			c.Bootstrap = Bootstrap{OrganizationName: "Acme", AdminEmail: "ada@acme.com", AdminPasswordFile: writeFile(t, "pw")}
		}, "admin_password_file needs auth.builtin_accounts"},
		{"redis password file missing", func(t *testing.T, c *Config) {
			c.Redis.PasswordFile = filepath.Join(t.TempDir(), "absent")
		}, "redis.password_file"},
		{"redis CA file missing", func(t *testing.T, c *Config) {
			c.Redis.TLS = true
			c.Redis.CAFile = filepath.Join(t.TempDir(), "absent")
		}, "redis.ca_file: stat"},
		{"redis CA file without TLS", func(t *testing.T, c *Config) {
			c.Redis.CAFile = writeFile(t, "ca")
		}, "redis.ca_file needs redis.tls"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig(t)
			tt.change(t, c)

			err := c.Validate()

			var verr *ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	c := validConfig(t)
	c.DeploymentMode = "hybrid"
	c.Auth.SigningKeyFiles = nil
	c.Bootstrap.OrganizationName = "Acme"

	var verr *ValidationError
	require.ErrorAs(t, c.Validate(), &verr)
	assert.Len(t, verr.Problems, 3)
}

func TestJITProvisioning(t *testing.T) {
	path := writeFile(t, `
auth:
  providers:
    github:
      jit_provisioning: false
`)
	var c Config
	require.NoError(t, env.Load(&c, env.Options{YAMLPath: path, Source: lookup(nil)}))

	assert.False(t, c.Auth.Providers.GitHub.JIT(), "YAML can turn it off; the default does not override false")
	assert.True(t, c.Auth.Providers.Google.JIT(), "on by default")
	assert.True(t, OIDCProvider{}.JIT(), "on when unset")
}

func TestGitHubInstallCallbackURL(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg  Config
		want string
	}{
		"derived from the sign-in page's host": {
			cfg:  Config{Auth: Auth{LoginURL: "https://app.example.com/login?x=1"}},
			want: "https://app.example.com/settings/connections/github/callback",
		},
		"set explicitly": {
			cfg: Config{
				Auth:           Auth{LoginURL: "https://app.example.com/login"},
				VersionControl: VersionControl{GitHub: GitHubApp{InstallCallbackURL: "https://web.example.com/gh"}},
			},
			want: "https://web.example.com/gh",
		},
		"no sign-in page": {cfg: Config{}, want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.cfg.GitHubInstallCallbackURL())
		})
	}
}
