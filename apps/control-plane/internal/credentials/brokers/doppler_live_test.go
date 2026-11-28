package brokers

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDopplerBrokerLive mints, uses, and revokes a service token in a real
// Doppler config. It skips unless IVALDI_TEST_DOPPLER_TOKEN (a token that can
// manage the config's service tokens), IVALDI_TEST_DOPPLER_PROJECT,
// IVALDI_TEST_DOPPLER_CONFIG, and IVALDI_TEST_DOPPLER_SECRET (the name of a
// secret in that config) are set. It never prints a token or a value.
func TestDopplerBrokerLive(t *testing.T) {
	token, project, config, secret := os.Getenv("IVALDI_TEST_DOPPLER_TOKEN"), os.Getenv("IVALDI_TEST_DOPPLER_PROJECT"),
		os.Getenv("IVALDI_TEST_DOPPLER_CONFIG"), os.Getenv("IVALDI_TEST_DOPPLER_SECRET")
	if token == "" || project == "" || config == "" || secret == "" {
		t.Skip("IVALDI_TEST_DOPPLER_TOKEN, _PROJECT, _CONFIG, and _SECRET are not all set")
	}
	ctx := context.Background()
	b := New()
	store := brokeredStore(t, secretstore.KindDoppler, credentials.Settings{Doppler: &credentials.DopplerSettings{Project: project, Config: config}}, credentials.Access{})
	minting := credentials.MintingCredential{Token: token}
	deployment := "dpl_live" + strconv.FormatInt(time.Now().Unix(), 10)

	m, err := b.Mint(ctx, credentials.MintRequest{Store: store, Credential: minting, DeploymentID: deployment})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = b.Revoke(ctx, credentials.RevokeRequest{Store: store, Credential: minting, ProviderID: m.ProviderID})
	})

	t.Run("doppler keeps the expiry the broker asked for", func(t *testing.T) {
		var out struct {
			Tokens []struct {
				Name      string  `json:"name"`
				Slug      string  `json:"slug"`
				ExpiresAt *string `json:"expires_at"`
			} `json:"tokens"`
		}
		q := url.Values{"project": {project}, "config": {config}}
		_, err := call(ctx, http.DefaultClient, http.MethodGet, secretstore.DefaultDopplerAPI+"/v3/configs/config/tokens?"+q.Encode(),
			http.Header{"Authorization": {"Bearer " + token}}, nil, &out)
		require.NoError(t, err)
		for _, tok := range out.Tokens {
			if tok.Slug != m.ProviderID {
				continue
			}
			assert.Equal(t, "ivaldi-"+deployment, tok.Name)
			require.NotNil(t, tok.ExpiresAt, "doppler ignored expire_at: the token never expires")
			expires, err := time.Parse(time.RFC3339, *tok.ExpiresAt)
			require.NoError(t, err)
			assert.WithinDuration(t, m.ExpiresAt, expires, time.Minute)
			t.Logf("doppler expires the token at %s", expires.UTC())
			return
		}
		t.Fatalf("doppler lists no token with the minted slug")
	})

	read := func() error {
		_, err := secretstore.NewDoppler(secretstore.DopplerConfig{
			Token: func(context.Context) (string, error) { return m.Credential.Token, nil },
		}).Get(ctx, secretstore.Ref{Path: secret})
		return err
	}
	require.NoError(t, read(), "the minted token reads the config")

	revoked, err := b.Revoke(ctx, credentials.RevokeRequest{Store: store, Credential: minting, ProviderID: m.ProviderID})
	require.NoError(t, err)
	assert.True(t, revoked)
	assert.Error(t, read(), "a revoked token reads nothing")

	revoked, err = b.Revoke(ctx, credentials.RevokeRequest{Store: store, Credential: minting, ProviderID: m.ProviderID})
	require.NoError(t, err, "revoking twice is not an error")
	assert.True(t, revoked)
}
