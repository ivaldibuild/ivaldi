package brokers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// dopplerBroker mints a read-only config service token named after the
// Deployment, which Doppler deletes when it expires, with a service account
// token that can manage the config's service tokens.
type dopplerBroker struct {
	client *http.Client
	api    string
}

func (d *dopplerBroker) header(c credentials.MintingCredential) http.Header {
	return http.Header{"Authorization": {"Bearer " + c.Token}}
}

func (d *dopplerBroker) Mint(ctx context.Context, r credentials.MintRequest) (credentials.Minted, error) {
	settings := r.Store.Settings().Doppler
	if settings == nil {
		return credentials.Minted{}, credentials.NewErrInvalidInput("a brokered doppler store names its project and config")
	}
	expires := clock.Now().Add(credentials.BrokeredLifetime)
	var out struct {
		Token struct {
			Key  string `json:"key"`
			Slug string `json:"slug"`
		} `json:"token"`
	}
	_, err := call(ctx, d.client, http.MethodPost, strings.TrimRight(d.api, "/")+"/v3/configs/config/tokens", d.header(r.Credential), map[string]string{
		"project": settings.Project, "config": settings.Config, "name": "ivaldi-" + r.DeploymentID,
		"access": "read", "expire_at": expires.Format("2006-01-02T15:04:05Z07:00"),
	}, &out)
	if err != nil {
		return credentials.Minted{}, fmt.Errorf("doppler service token: %w", err)
	}
	if out.Token.Key == "" || out.Token.Slug == "" {
		return credentials.Minted{}, fmt.Errorf("%w: doppler gave no service token", credentials.ErrStoreUnavailable)
	}
	return credentials.Minted{Credential: credentials.BrokeredCredential{Token: out.Token.Key}, ExpiresAt: expires, ProviderID: out.Token.Slug}, nil
}

func (d *dopplerBroker) Revoke(ctx context.Context, r credentials.RevokeRequest) (bool, error) {
	settings := r.Store.Settings().Doppler
	if settings == nil {
		return false, credentials.NewErrInvalidInput("a brokered doppler store names its project and config")
	}
	status, err := call(ctx, d.client, http.MethodDelete, strings.TrimRight(d.api, "/")+"/v3/configs/config/tokens/token", d.header(r.Credential),
		map[string]string{"project": settings.Project, "config": settings.Config, "slug": r.ProviderID}, nil)
	if status == http.StatusNotFound {
		// Expired, and deleted by Doppler.
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("delete doppler service token: %w", err)
	}
	return true, nil
}
