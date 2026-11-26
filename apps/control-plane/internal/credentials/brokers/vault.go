package brokers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// defaultAppRoleMount is where Vault's AppRole auth is mounted by default.
const defaultAppRoleMount = "approle"

// vaultBroker logs in with an AppRole, whose token may create tokens from
// the store's token role, and creates a non-renewable token from that role
// for each Deployment. The role decides what the token may read, and should
// be orphan, so the token outlives the AppRole login's.
type vaultBroker struct {
	client *http.Client
}

func (v *vaultBroker) login(ctx context.Context, store *credentials.SecretStore, c credentials.MintingCredential) (string, http.Header, error) {
	settings := store.Settings().Vault
	if settings == nil {
		return "", nil, credentials.NewErrInvalidInput("a vault store names its address")
	}
	header := http.Header{}
	if settings.Namespace != "" {
		header.Set("X-Vault-Namespace", settings.Namespace)
	}
	mount := store.Access().AuthMount
	if mount == "" {
		mount = defaultAppRoleMount
	}
	var out struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	base := strings.TrimRight(settings.Address, "/")
	_, err := call(ctx, v.client, http.MethodPost, base+"/v1/auth/"+url.PathEscape(mount)+"/login", header,
		map[string]string{"role_id": c.RoleID, "secret_id": c.SecretID}, &out)
	if err != nil {
		return "", nil, fmt.Errorf("vault approle login at %s: %w", mount, err)
	}
	if out.Auth.ClientToken == "" {
		return "", nil, fmt.Errorf("%w: vault approle login gave no token", credentials.ErrStoreRefused)
	}
	header.Set("X-Vault-Token", out.Auth.ClientToken)
	return base, header, nil
}

func (v *vaultBroker) Mint(ctx context.Context, r credentials.MintRequest) (credentials.Minted, error) {
	base, header, err := v.login(ctx, r.Store, r.Credential)
	if err != nil {
		return credentials.Minted{}, err
	}
	role := r.Store.Access().TokenRole
	var out struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			Accessor      string `json:"accessor"`
			LeaseDuration int    `json:"lease_duration"`
		} `json:"auth"`
	}
	_, err = call(ctx, v.client, http.MethodPost, base+"/v1/auth/token/create/"+url.PathEscape(role), header, map[string]any{
		"ttl": fmt.Sprintf("%ds", int(credentials.BrokeredLifetime.Seconds())), "renewable": false,
		"display_name": "ivaldi-" + r.DeploymentID, "meta": map[string]string{"deployment": r.DeploymentID},
	}, &out)
	if err != nil {
		return credentials.Minted{}, fmt.Errorf("vault token from role %s: %w", role, err)
	}
	if out.Auth.ClientToken == "" || out.Auth.Accessor == "" {
		return credentials.Minted{}, fmt.Errorf("%w: vault gave no token", credentials.ErrStoreUnavailable)
	}
	lifetime := time.Duration(out.Auth.LeaseDuration) * time.Second
	if lifetime <= 0 || lifetime > credentials.BrokeredLifetime {
		lifetime = credentials.BrokeredLifetime
	}
	return credentials.Minted{
		Credential: credentials.BrokeredCredential{Token: out.Auth.ClientToken},
		ExpiresAt:  clock.Now().Add(lifetime), ProviderID: out.Auth.Accessor,
	}, nil
}

func (v *vaultBroker) Revoke(ctx context.Context, r credentials.RevokeRequest) (bool, error) {
	base, header, err := v.login(ctx, r.Store, r.Credential)
	if err != nil {
		return false, err
	}
	status, err := call(ctx, v.client, http.MethodPost, base+"/v1/auth/token/revoke-accessor", header,
		map[string]string{"accessor": r.ProviderID}, nil)
	if status == http.StatusBadRequest {
		// Vault answers an accessor it no longer knows, such as an expired
		// token's, with 400.
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("revoke vault token: %w", err)
	}
	return true, nil
}
