package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ivaldibuild/ivaldi/pkg/secretstore"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
)

// universalAuth is an Infisical Universal Auth credential, as its named
// credential holds it.
type universalAuth struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// agentInfisical opens an infisical store in agent mode: with the agent's
// service-account token at the identity's Kubernetes auth, or with the
// Universal Auth credential its operator configured.
func (r *resolver) agentInfisical(s apiclient.StoreSettings, open opening) (secretstore.Store, error) {
	identity := deref(s.Access.IdentityId)
	switch method(s.Access) {
	case apiclient.StoreAccessMethodKubernetes:
		return r.infisical(s, open, func(ctx context.Context, site string) (string, error) {
			raw, err := os.ReadFile(r.cfg.InfisicalTokenFile)
			if err != nil {
				return "", fmt.Errorf("%w: this agent has no service-account token for Infisical; its operator mounts one with the chart's secrets.infisical.kubernetesAuth: %v",
					secretstore.ErrDenied, err)
			}
			return secretstore.InfisicalLogin(ctx, nil, site, secretstore.InfisicalKubernetes, identity, strings.TrimSpace(string(raw)))
		})
	case apiclient.StoreAccessMethodUniversalAuth:
		name := deref(s.Access.Credential)
		credential, ok := r.cfg.Credentials[name]
		if !ok {
			return nil, deploy.Failf("secret store %s needs the credential %q, which this agent's operator has not configured; credentials are mapped to Secrets in the chart's secrets.credentials",
				s.Id, name)
		}
		return r.infisical(s, open, func(ctx context.Context, site string) (string, error) {
			raw, err := r.credential(ctx, name, credential)
			if err != nil {
				return "", err
			}
			// The parse error is left out: it can quote the credential.
			var c universalAuth
			if json.Unmarshal([]byte(raw), &c) != nil || c.ClientID == "" || c.ClientSecret == "" {
				return "", fmt.Errorf("%w: credential %s is not a JSON object with client_id and client_secret", secretstore.ErrDenied, name)
			}
			open.mask(c.ClientSecret)
			return secretstore.InfisicalUniversalAuthLogin(ctx, nil, site, c.ClientID, c.ClientSecret)
		})
	}
	return nil, deploy.Failf("secret store %s logs in to Infisical with method %q in agent mode, which this agent cannot use", s.Id, method(s.Access))
}

// federatedInfisical opens an infisical store by logging in with the
// granted JWT at the identity's OIDC or JWT auth.
func (r *resolver) federatedInfisical(s apiclient.StoreSettings, open opening) (secretstore.Store, error) {
	logins := map[apiclient.StoreAccessMethod]secretstore.InfisicalMethod{
		apiclient.StoreAccessMethodOidc: secretstore.InfisicalOIDC,
		apiclient.StoreAccessMethodJwt:  secretstore.InfisicalJWT,
	}
	login, ok := logins[method(s.Access)]
	if !ok {
		return nil, deploy.Failf("secret store %s logs in to Infisical with method %q in federated mode, which this agent cannot use", s.Id, method(s.Access))
	}
	identity := deref(s.Access.IdentityId)
	return r.infisical(s, open, func(ctx context.Context, site string) (string, error) {
		return secretstore.InfisicalLogin(ctx, nil, site, login, identity, open.jwt)
	})
}

// infisical opens an infisical store at a site the operator allowed. It
// logs in once, on first use, so a retry reads with the same access token,
// which is masked until the values are read.
func (r *resolver) infisical(s apiclient.StoreSettings, open opening, login func(ctx context.Context, site string) (string, error)) (secretstore.Store, error) {
	if s.Infisical == nil {
		return nil, deploy.Failf("secret store %s has no Infisical settings", s.Id)
	}
	site := s.Infisical.SiteUrl
	if !r.cfg.allowsInfisical(site) {
		return nil, deploy.Failf("secret store %s is an Infisical at %s, which this agent is not set up to log in to; its operator lists self-hosted Infisical addresses in the chart's secrets.addresses",
			s.Id, site)
	}
	var token string
	return secretstore.NewInfisical(secretstore.InfisicalConfig{
		SiteURL: site, ProjectID: s.Infisical.ProjectId, Environment: s.Infisical.Environment, SecretPath: s.Infisical.SecretPath,
		Token: func(ctx context.Context) (string, error) {
			if token != "" {
				return token, nil
			}
			t, err := login(ctx, site)
			if err == nil {
				open.mask(t)
				token = t
			}
			return t, err
		},
	}), nil
}

// method is the store's login method, empty when it names none.
func method(a apiclient.StoreAccess) apiclient.StoreAccessMethod {
	if a.Method == nil {
		return ""
	}
	return *a.Method
}
