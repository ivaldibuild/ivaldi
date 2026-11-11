package server

import (
	"fmt"
	"os"
	"strings"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/bootstrap"
)

// First-party OAuth clients. Their client IDs are fixed, so the web app and
// ivaldictl can name themselves without configuration.
const (
	webClientID = "ivaldi-web"
	cliClientID = "ivaldictl"
)

// cliRedirectURIs are ivaldictl's loopback redirects. It listens on a port
// it picks when it signs in, and loopback redirects match any port (RFC 8252).
var cliRedirectURIs = []string{"http://127.0.0.1/callback", "http://[::1]/callback"}

// githubDotCom is GitHub's own address; only GitHub Enterprise Server needs
// one stored.
const githubDotCom = "https://github.com"

// bootstrapSettings translates the configuration into what bootstrap applies,
// so bootstrap does not depend on the configuration's shape.
func bootstrapSettings(cfg *config.Config) (bootstrap.Settings, error) {
	s := bootstrap.Settings{
		Mode:    bootstrap.Mode(cfg.Mode()),
		Clients: []bootstrap.ClientSettings{{ClientID: cliClientID, Name: "Ivaldi CLI", RedirectURIs: cliRedirectURIs}},
	}
	if uris := cfg.Auth.WebClient.RedirectURIs; len(uris) > 0 {
		s.Clients = append(s.Clients, bootstrap.ClientSettings{ClientID: webClientID, Name: "Ivaldi", RedirectURIs: uris})
	}
	for _, w := range cfg.Platform.Workloads {
		keys, err := w.PublicKeys()
		if err != nil {
			return bootstrap.Settings{}, err
		}
		s.PlatformWorkloads = append(s.PlatformWorkloads, bootstrap.PlatformWorkloadSettings{
			Name: w.Name, Component: iam.WorkloadComponent(w.Component), PublicKeys: keys,
		})
	}
	if cfg.Mode() != config.DeploymentModeSelfHosted {
		return s, nil
	}

	b := cfg.Bootstrap
	s.Organization = bootstrap.OrganizationSettings{Slug: b.OrganizationSlug, Name: b.OrganizationName}
	s.AdminEmail = b.AdminEmail
	if b.AdminPasswordFile != "" {
		pw, err := os.ReadFile(b.AdminPasswordFile)
		if err != nil {
			return bootstrap.Settings{}, fmt.Errorf("bootstrap.admin_password_file: %w", err)
		}
		s.AdminPassword = strings.TrimRight(string(pw), "\r\n")
	}

	p := cfg.Auth.Providers
	if p.Google.Enabled() {
		s.Providers = append(s.Providers, bootstrap.ProviderSettings{
			Protocol: iam.ProtocolGoogle,
			Name:     "Google",
			Config: iam.IdentityProviderConfig{
				ClientID:       p.Google.ClientID,
				AllowedDomains: lower(p.Google.AllowedDomains),
				AllowedEmails:  p.Google.AllowedEmails,
			},
			JITProvisioning: p.Google.JIT(),
		})
	}
	if p.GitHub.Enabled() {
		c := iam.IdentityProviderConfig{
			ClientID:             p.GitHub.ClientID,
			AllowedOrganizations: p.GitHub.AllowedOrganizations,
			AllowedEmails:        p.GitHub.AllowedEmails,
		}
		if base := strings.TrimRight(p.GitHub.URL, "/"); base != githubDotCom {
			c.BaseURL = base
		}
		s.Providers = append(s.Providers, bootstrap.ProviderSettings{Protocol: iam.ProtocolGitHub, Name: "GitHub", Config: c, JITProvisioning: p.GitHub.JIT()})
	}
	if p.OIDC.Enabled() {
		s.Providers = append(s.Providers, bootstrap.ProviderSettings{
			Protocol: iam.ProtocolOIDC,
			Name:     p.OIDC.DisplayName,
			Config: iam.IdentityProviderConfig{
				Issuer:         p.OIDC.IssuerURL,
				ClientID:       p.OIDC.ClientID,
				AllowedDomains: lower(p.OIDC.AllowedDomains),
				AllowedEmails:  p.OIDC.AllowedEmails,
			},
			JITProvisioning: p.OIDC.JIT(),
		})
	}
	if cfg.Auth.BuiltinAccounts {
		s.Providers = append(s.Providers, bootstrap.ProviderSettings{Protocol: iam.ProtocolBuiltin, Name: "Email and password"})
	}
	return s, nil
}

func lower(list []string) []string {
	if len(list) == 0 {
		return nil
	}
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = strings.ToLower(v)
	}
	return out
}
