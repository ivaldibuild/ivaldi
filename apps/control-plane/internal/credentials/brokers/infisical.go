package brokers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// infisicalBroker logs in as a machine identity with its Universal Auth
// credential and hands over the access token. Infisical scopes a token to
// the identity, so each Environment should have its own, and revokes a
// token only given the token itself, which the control plane never keeps:
// the token expires on the identity's TTL instead, which its owner keeps
// short.
type infisicalBroker struct {
	client *http.Client
}

func (i *infisicalBroker) Mint(ctx context.Context, r credentials.MintRequest) (credentials.Minted, error) {
	settings := r.Store.Settings().Infisical
	if settings == nil {
		return credentials.Minted{}, credentials.NewErrInvalidInput("an infisical store names its site")
	}
	var out struct {
		AccessToken string `json:"accessToken"`
		ExpiresIn   int    `json:"expiresIn"`
	}
	_, err := call(ctx, i.client, http.MethodPost, strings.TrimRight(settings.SiteURL, "/")+"/api/v1/auth/universal-auth/login", nil,
		map[string]string{"clientId": r.Credential.ClientID, "clientSecret": r.Credential.ClientSecret}, &out)
	if err != nil {
		return credentials.Minted{}, fmt.Errorf("infisical universal auth login: %w", err)
	}
	if out.AccessToken == "" {
		return credentials.Minted{}, fmt.Errorf("%w: infisical gave no access token", credentials.ErrStoreUnavailable)
	}
	// The login names no ID for the token, so the grant names it by a
	// digest, which tells tokens apart without revealing one.
	sum := sha256.Sum256([]byte(out.AccessToken))
	return credentials.Minted{
		Credential: credentials.BrokeredCredential{Token: out.AccessToken},
		ExpiresAt:  clock.Now().Add(time.Duration(out.ExpiresIn) * time.Second),
		ProviderID: "sha256:" + hex.EncodeToString(sum[:8]),
	}, nil
}

func (i *infisicalBroker) Revoke(context.Context, credentials.RevokeRequest) (bool, error) {
	return false, nil
}
