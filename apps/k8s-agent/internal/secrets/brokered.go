package secrets

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi/pkg/secretstore"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
)

// brokeredStore opens a store with the credential the control plane minted
// for the Deployment, as it is: session credentials for AWS, a token for the
// others. A token goes only to an address the operator allowed, as in the
// other modes.
func (r *resolver) brokeredStore(ctx context.Context, s apiclient.StoreSettings, open opening) (secretstore.Store, error) {
	c := open.credential
	switch s.Kind {
	case apiclient.StoreSettingsKindAwsSecretsManager:
		if s.Aws == nil {
			return nil, deploy.Failf("secret store %s has no AWS settings", s.Id)
		}
		api, err := secretstore.NewAWSSessionClient(ctx, s.Aws.Region, deref(c.AccessKeyId), deref(c.SecretAccessKey), deref(c.SessionToken))
		if err != nil {
			return nil, &deploy.Failure{Reason: fmt.Sprintf("secret store %s", s.Id), Err: err}
		}
		return secretstore.NewAWS(api), nil
	case apiclient.StoreSettingsKindVault:
		if s.Vault == nil {
			return nil, deploy.Failf("secret store %s has no Vault settings", s.Id)
		}
		if !r.cfg.allows(s.Vault.Address) {
			return nil, deploy.Failf("secret store %s is a Vault at %s, which this agent is not set up to send a token to; its operator lists the addresses it may use in the chart's secrets.addresses",
				s.Id, s.Vault.Address)
		}
		return secretstore.NewVault(secretstore.VaultConfig{
			Address: s.Vault.Address, Mount: s.Vault.Mount, Namespace: deref(s.Vault.Namespace), Token: deref(c.Token),
		}), nil
	case apiclient.StoreSettingsKindDoppler:
		if s.Doppler == nil {
			return nil, deploy.Failf("secret store %s has no Doppler settings", s.Id)
		}
		token := deref(c.Token)
		return secretstore.NewDoppler(secretstore.DopplerConfig{
			Project: deref(s.Doppler.Project), Config: deref(s.Doppler.Config),
			Token: func(context.Context) (string, error) { return token, nil },
		}), nil
	case apiclient.StoreSettingsKindInfisical:
		token := deref(c.Token)
		return r.infisical(s, open, func(context.Context, string) (string, error) { return token, nil })
	}
	return nil, deploy.Failf("this agent cannot open %q secret stores in brokered mode", s.Kind)
}
