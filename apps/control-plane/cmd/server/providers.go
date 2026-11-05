package server

import (
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/authhttp"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/bootstrap"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/connector"
)

// installationProviders builds a connector for each sign-in provider a
// self-hosted installation configures, signing in to the identity provider
// bootstrap applied for it.
func installationProviders(cfg *config.Config, install *bootstrap.Installation) ([]authhttp.Provider, error) {
	if cfg.Mode() != config.DeploymentModeSelfHosted || install == nil {
		return nil, nil
	}
	p := cfg.Auth.Providers
	var out []authhttp.Provider
	add := func(protocol iam.IdentityProtocol, build func(secret string) (connector.Connector, error), resolve func() (config.Secret, error)) error {
		idpID, ok := install.Providers[protocol]
		if !ok {
			return fmt.Errorf("sign-in provider %s was not applied", protocol)
		}
		secret, err := resolve()
		if err != nil {
			return err
		}
		c, err := build(secret.Reveal())
		if err != nil {
			return err
		}
		out = append(out, authhttp.Provider{Kind: string(protocol), Connector: c, OrganizationID: install.OrganizationID, IdentityProviderID: idpID})
		return nil
	}

	if p.Google.Enabled() {
		err := add(iam.ProtocolGoogle, func(secret string) (connector.Connector, error) {
			return connector.NewGoogle(connector.GoogleConfig{ClientID: p.Google.ClientID, ClientSecret: secret})
		}, p.Google.Secret)
		if err != nil {
			return nil, err
		}
	}
	if p.GitHub.Enabled() {
		err := add(iam.ProtocolGitHub, func(secret string) (connector.Connector, error) {
			return connector.NewGitHub(connector.GitHubConfig{
				BaseURL: p.GitHub.URL, ClientID: p.GitHub.ClientID, ClientSecret: secret,
				ReadOrganizations: len(p.GitHub.AllowedOrganizations) > 0,
			})
		}, p.GitHub.Secret)
		if err != nil {
			return nil, err
		}
	}
	if p.OIDC.Enabled() {
		err := add(iam.ProtocolOIDC, func(secret string) (connector.Connector, error) {
			return connector.NewOIDC(connector.OIDCConfig{
				IssuerURL: p.OIDC.IssuerURL, ClientID: p.OIDC.ClientID, ClientSecret: secret,
				Name: p.OIDC.DisplayName, Scopes: p.OIDC.Scopes,
			})
		}, p.OIDC.Secret)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
