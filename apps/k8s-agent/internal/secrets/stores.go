package secrets

import (
	"context"
	"fmt"
	"os"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ivaldibuild/ivaldi/pkg/secretstore"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
)

// newStore builds the store a Deployment's secret names. Stores are built per
// Deployment and never cached across them.
func (r *resolver) newStore(ctx context.Context, s apiclient.StoreSettings, open opening) (secretstore.Store, error) {
	switch s.Access.Mode {
	case apiclient.StoreAccessModeAgent:
		return r.agentStore(ctx, s, open)
	case apiclient.StoreAccessModeFederated:
		if open.jwt == "" {
			return nil, deploy.Failf("the control plane granted no access to secret store %s, which is in federated mode", s.Id)
		}
		return r.federatedStore(ctx, s, open)
	case apiclient.StoreAccessModeBrokered:
		if open.credential == nil {
			return nil, deploy.Failf("the control plane granted no access to secret store %s, which is in brokered mode", s.Id)
		}
		return r.brokeredStore(ctx, s, open)
	}
	return nil, deploy.Failf("secret store %s is opened in %q mode, which this agent cannot use", s.Id, s.Access.Mode)
}

// agentStore opens a store with the agent's own identity, or a credential
// its operator configured.
func (r *resolver) agentStore(ctx context.Context, s apiclient.StoreSettings, open opening) (secretstore.Store, error) {
	switch s.Kind {
	case apiclient.StoreSettingsKindKubernetes:
		return secretstore.NewKubernetes(r.kubernetesGetter(open.namespace)), nil
	case apiclient.StoreSettingsKindAwsSecretsManager:
		if s.Aws == nil {
			return nil, deploy.Failf("secret store %s has no AWS settings", s.Id)
		}
		api, err := secretstore.NewAWSClient(ctx, s.Aws.Region, deref(s.Access.RoleArn))
		if err != nil {
			return nil, &deploy.Failure{Reason: fmt.Sprintf("secret store %s", s.Id), Err: err}
		}
		return secretstore.NewAWS(api), nil
	case apiclient.StoreSettingsKindVault:
		return r.vault(s, "kubernetes", func() (string, error) {
			raw, err := os.ReadFile(r.cfg.VaultTokenFile)
			return strings.TrimSpace(string(raw)), err
		})
	case apiclient.StoreSettingsKindDoppler:
		if s.Doppler == nil {
			return nil, deploy.Failf("secret store %s has no Doppler settings", s.Id)
		}
		name := deref(s.Access.Credential)
		credential, ok := r.cfg.Credentials[name]
		if !ok {
			return nil, deploy.Failf("secret store %s needs the credential %q, which this agent's operator has not configured; credentials are mapped to Secrets in the chart's secrets.credentials",
				s.Id, name)
		}
		return secretstore.NewDoppler(secretstore.DopplerConfig{
			Project: deref(s.Doppler.Project), Config: deref(s.Doppler.Config),
			Token: func(ctx context.Context) (string, error) { return r.credential(ctx, name, credential) },
		}), nil
	case apiclient.StoreSettingsKindInfisical:
		return r.agentInfisical(s, open)
	}
	return nil, deploy.Failf("this agent cannot read %q secret stores", s.Kind)
}

// federatedStore opens a store by exchanging the JWT the control plane
// granted for the store's own credential. The store's configuration decides
// what that credential may read.
func (r *resolver) federatedStore(ctx context.Context, s apiclient.StoreSettings, open opening) (secretstore.Store, error) {
	switch s.Kind {
	case apiclient.StoreSettingsKindAwsSecretsManager:
		if s.Aws == nil {
			return nil, deploy.Failf("secret store %s has no AWS settings", s.Id)
		}
		// The session name is what CloudTrail records the reads under.
		api, err := secretstore.NewAWSWebIdentityClient(ctx, s.Aws.Region, deref(s.Access.RoleArn), "ivaldi-"+open.deploymentID, open.jwt)
		if err != nil {
			return nil, &deploy.Failure{Reason: fmt.Sprintf("secret store %s", s.Id), Err: err}
		}
		return secretstore.NewAWS(api), nil
	case apiclient.StoreSettingsKindVault:
		return r.vault(s, "jwt", func() (string, error) { return open.jwt, nil })
	case apiclient.StoreSettingsKindDoppler:
		if s.Doppler == nil {
			return nil, deploy.Failf("secret store %s has no Doppler settings", s.Id)
		}
		identity := deref(s.Access.IdentityId)
		// Exchanged once, on first use: a retry reads with the same token.
		var token string
		return secretstore.NewDoppler(secretstore.DopplerConfig{
			Project: deref(s.Doppler.Project), Config: deref(s.Doppler.Config),
			Token: func(ctx context.Context) (string, error) {
				if token != "" {
					return token, nil
				}
				t, err := secretstore.DopplerOIDC(ctx, nil, "", identity, open.jwt)
				if err == nil {
					open.mask(t)
					token = t
				}
				return t, err
			},
		}), nil
	case apiclient.StoreSettingsKindInfisical:
		return r.federatedInfisical(s, open)
	}
	return nil, deploy.Failf("this agent cannot open %q secret stores in federated mode", s.Kind)
}

// vault opens a vault store at an address the operator allowed, logging in
// with jwt at the store's auth mount, or defaultMount when it names none.
func (r *resolver) vault(s apiclient.StoreSettings, defaultMount string, jwt func() (string, error)) (secretstore.Store, error) {
	if s.Vault == nil {
		return nil, deploy.Failf("secret store %s has no Vault settings", s.Id)
	}
	if !r.cfg.allows(s.Vault.Address) {
		return nil, deploy.Failf("secret store %s is a Vault at %s, which this agent is not set up to log in to; its operator lists the addresses it may use in the chart's secrets.addresses",
			s.Id, s.Vault.Address)
	}
	mount := deref(s.Access.AuthMount)
	if mount == "" {
		mount = defaultMount
	}
	return secretstore.NewVault(secretstore.VaultConfig{
		Address: s.Vault.Address, Mount: s.Vault.Mount, Namespace: deref(s.Vault.Namespace),
		AuthRole: deref(s.Access.Role), AuthMount: mount, JWT: jwt,
	}), nil
}

// credential reads a named credential from its Secret in the agent's
// namespace, each time it is needed, so a rotated one is used at once.
func (r *resolver) credential(ctx context.Context, name string, c Credential) (string, error) {
	data, _, err := r.kubernetesGetter(r.cfg.AgentNamespace)(ctx, c.Secret)
	if err != nil {
		return "", fmt.Errorf("credential %s, in Secret %s: %w", name, c.Secret, err)
	}
	value := strings.TrimSpace(string(data[c.Key]))
	if value == "" {
		return "", fmt.Errorf("credential %s: Secret %s has no key %s", name, c.Secret, c.Key)
	}
	return value, nil
}

// kubernetesGetter reads Secrets in namespace with the agent's own RBAC.
func (r *resolver) kubernetesGetter(namespace string) secretstore.SecretGetter {
	return func(ctx context.Context, name string) (map[string][]byte, string, error) {
		secret, err := r.cfg.Kube.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			return nil, "", secretstore.ErrNotFound
		case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
			return nil, "", fmt.Errorf("%w: RBAC denies get on secrets in %s", secretstore.ErrDenied, namespace)
		case err != nil:
			return nil, "", fmt.Errorf("%w: %v", secretstore.ErrUnavailable, err)
		}
		return secret.Data, secret.ResourceVersion, nil
	}
}
