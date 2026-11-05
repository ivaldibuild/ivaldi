package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/bootstrap"
)

func TestBootstrapSettingsSelfHosted(t *testing.T) {
	pwFile := filepath.Join(t.TempDir(), "admin-password")
	require.NoError(t, os.WriteFile(pwFile, []byte("correct-horse\n"), 0o600))
	jitOff := false
	cfg := &config.Config{
		DeploymentMode: string(config.DeploymentModeSelfHosted),
		Auth: config.Auth{
			BuiltinAccounts: true,
			WebClient:       config.WebClient{RedirectURIs: []string{"https://app.acme.com/cb"}},
			Providers: config.Providers{
				Google: config.GoogleProvider{ClientID: "g", AllowedDomains: []string{"ACME.com"}},
				GitHub: config.GitHubProvider{ClientID: "gh", URL: "https://github.acme.com/", AllowedOrganizations: []string{"acme"}, JITProvisioning: &jitOff},
				OIDC:   config.OIDCProvider{ClientID: "o", IssuerURL: "https://sso.acme.com", DisplayName: "Okta"},
			},
		},
		Bootstrap: config.Bootstrap{OrganizationSlug: "acme", OrganizationName: "Acme", AdminEmail: "ada@acme.com", AdminPasswordFile: pwFile},
	}

	s, err := bootstrapSettings(cfg)

	require.NoError(t, err)
	assert.Equal(t, bootstrap.ModeSelfHosted, s.Mode)
	assert.Equal(t, []bootstrap.ClientSettings{
		{ClientID: "ivaldictl", Name: "Ivaldi CLI", RedirectURIs: cliRedirectURIs},
		{ClientID: "ivaldi-web", Name: "Ivaldi", RedirectURIs: []string{"https://app.acme.com/cb"}},
	}, s.Clients)
	assert.Equal(t, bootstrap.OrganizationSettings{Slug: "acme", Name: "Acme"}, s.Organization)
	assert.Equal(t, "correct-horse", s.AdminPassword, "read from the file without its newline")
	assert.Equal(t, []bootstrap.ProviderSettings{
		{Protocol: iam.ProtocolGoogle, Name: "Google", Config: iam.IdentityProviderConfig{ClientID: "g", AllowedDomains: []string{"acme.com"}}, JITProvisioning: true},
		{Protocol: iam.ProtocolGitHub, Name: "GitHub", Config: iam.IdentityProviderConfig{ClientID: "gh", BaseURL: "https://github.acme.com", AllowedOrganizations: []string{"acme"}}, JITProvisioning: false},
		{Protocol: iam.ProtocolOIDC, Name: "Okta", Config: iam.IdentityProviderConfig{Issuer: "https://sso.acme.com", ClientID: "o"}, JITProvisioning: true},
		{Protocol: iam.ProtocolBuiltin, Name: "Email and password"},
	}, s.Providers)
}

func TestBootstrapSettingsGitHubDotComStoresNoBaseURL(t *testing.T) {
	cfg := &config.Config{
		DeploymentMode: string(config.DeploymentModeSelfHosted),
		Auth:           config.Auth{Providers: config.Providers{GitHub: config.GitHubProvider{ClientID: "gh", URL: "https://github.com", AllowedEmails: []string{"a@b.c"}}}},
	}

	s, err := bootstrapSettings(cfg)

	require.NoError(t, err)
	require.Len(t, s.Providers, 1)
	assert.Empty(t, s.Providers[0].Config.BaseURL)
}

func TestBootstrapSettingsCloud(t *testing.T) {
	s, err := bootstrapSettings(&config.Config{DeploymentMode: string(config.DeploymentModeCloud)})

	require.NoError(t, err)
	assert.Equal(t, bootstrap.ModeCloud, s.Mode)
	assert.Len(t, s.Clients, 1, "the web app is registered only when its redirect URIs are set")
	assert.Empty(t, s.Providers)
	assert.Empty(t, s.Organization)
}
