package brokers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
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
	_, err := call(ctx, d.client, http.MethodPost, strings.TrimRight(d.api, "/")+"/v3/configs/config/tokens", d.header(r.Credential), map[string]any{
		"project": settings.Project, "config": settings.Config, "name": "ivaldi-" + r.DeploymentID,
		// Unix seconds, as Doppler's CLI sends it: the API reference calls
		// it a Unix timestamp but types it as a date-time string.
		"access": "read", "expire_at": expires.Unix(),
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
	_, err := call(ctx, d.client, http.MethodDelete, strings.TrimRight(d.api, "/")+"/v3/configs/config/tokens/token", d.header(r.Credential),
		map[string]string{"project": settings.Project, "config": settings.Config, "slug": r.ProviderID}, nil)
	if dopplerTokenGone(err) {
		// Expired and deleted by Doppler, or revoked before.
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("delete doppler service token: %w", err)
	}
	return true, nil
}

// dopplerTokenGone reports whether Doppler refused a deletion because the
// service token no longer exists: it answers 400 "Invalid service token.",
// and 404 only for a project or config it cannot find.
func dopplerTokenGone(err error) bool {
	var answer *answerError
	if !errors.As(err, &answer) || answer.status != http.StatusBadRequest {
		return false
	}
	var body struct {
		Messages []string `json:"messages"`
	}
	return json.Unmarshal(answer.body, &body) == nil && slices.Contains(body.Messages, "Invalid service token.")
}
