package brokers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

var now = time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)

func brokeredStore(t *testing.T, kind secretstore.Kind, settings credentials.Settings, access credentials.Access) *credentials.SecretStore {
	t.Helper()
	access.Mode = credentials.AccessBrokered
	s, err := credentials.NewSecretStore(credentials.NewSecretStoreProps{
		OrganizationID: "org_a", WorkspaceID: "wsp_a", DisplayName: "Store", Kind: kind, Settings: settings, Access: access,
		EnvironmentIDs: []string{"env_01BX5ZZKBKACTAV9WEVGEMMVRA"},
	}, nil)
	require.NoError(t, err)
	return s
}

// recorder is a store's API that records each request and answers from a
// table by method and path.
type recorder struct {
	t       *testing.T
	answers map[string]func(w http.ResponseWriter)
	got     []request
}

type request struct {
	Method, Path string
	Header       http.Header
	Body         map[string]any
}

func (rec *recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	rec.got = append(rec.got, request{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
	answer, ok := rec.answers[r.Method+" "+r.URL.Path]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	answer(w)
}

func serve(t *testing.T, answers map[string]func(w http.ResponseWriter)) (*recorder, string) {
	rec := &recorder{t: t, answers: answers}
	server := httptest.NewServer(rec)
	t.Cleanup(server.Close)
	return rec, server.URL
}

func json200(v any) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) { _ = json.NewEncoder(w).Encode(v) }
}

func answer(status int, v any) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
}

// Doppler mints a read-only token for the config, named after the
// Deployment and expiring with the grant, and deletes it by slug.
func TestDopplerBroker(t *testing.T) {
	clock.Fix(t, now)
	rec, api := serve(t, map[string]func(w http.ResponseWriter){
		"POST /v3/configs/config/tokens":         json200(map[string]any{"token": map[string]any{"key": "dp.st.prd.minted", "slug": "slug-1"}}),
		"DELETE /v3/configs/config/tokens/token": json200(map[string]any{"success": true}),
	})
	b := New(WithDopplerAPI(api))
	store := brokeredStore(t, secretstore.KindDoppler, credentials.Settings{Doppler: &credentials.DopplerSettings{Project: "payments", Config: "prd"}}, credentials.Access{})
	minting := credentials.MintingCredential{Token: "dp.sa.minting"}

	m, err := b.Mint(context.Background(), credentials.MintRequest{Store: store, Credential: minting, DeploymentID: "dpl_a"})
	require.NoError(t, err)
	assert.Equal(t, credentials.Minted{Credential: credentials.BrokeredCredential{Token: "dp.st.prd.minted"}, ExpiresAt: now.Add(15 * time.Minute), ProviderID: "slug-1"}, m)
	assert.Equal(t, "Bearer dp.sa.minting", rec.got[0].Header.Get("Authorization"))
	assert.Equal(t, map[string]any{"project": "payments", "config": "prd", "name": "ivaldi-dpl_a", "access": "read", "expire_at": float64(now.Add(15 * time.Minute).Unix())}, rec.got[0].Body)

	revoked, err := b.Revoke(context.Background(), credentials.RevokeRequest{Store: store, Credential: minting, ProviderID: "slug-1"})
	require.NoError(t, err)
	assert.True(t, revoked)
	assert.Equal(t, map[string]any{"project": "payments", "config": "prd", "slug": "slug-1"}, rec.got[1].Body)

	t.Run("a token already gone is revoked", func(t *testing.T) {
		_, api := serve(t, map[string]func(w http.ResponseWriter){
			"DELETE /v3/configs/config/tokens/token": answer(http.StatusBadRequest, map[string]any{"messages": []string{"Invalid service token."}, "success": false}),
		})
		revoked, err := New(WithDopplerAPI(api)).Revoke(context.Background(), credentials.RevokeRequest{Store: store, Credential: minting, ProviderID: "slug-1"})
		require.NoError(t, err)
		assert.True(t, revoked)
	})

	t.Run("a config doppler cannot find is an error", func(t *testing.T) {
		_, api := serve(t, map[string]func(w http.ResponseWriter){
			"DELETE /v3/configs/config/tokens/token": answer(http.StatusNotFound, map[string]any{"messages": []string{"Could not find requested config 'prd'"}, "success": false}),
		})
		_, err := New(WithDopplerAPI(api)).Revoke(context.Background(), credentials.RevokeRequest{Store: store, Credential: minting, ProviderID: "slug-1"})
		assert.ErrorIs(t, err, credentials.ErrStoreRefused)
	})
}

// Vault logs in with the AppRole, creates a non-renewable token from the
// token role, and revokes it by accessor; a refused login is the store's.
func TestVaultBroker(t *testing.T) {
	clock.Fix(t, now)
	rec, address := serve(t, map[string]func(w http.ResponseWriter){
		"POST /v1/auth/approle/login":         json200(map[string]any{"auth": map[string]any{"client_token": "hvs.approle"}}),
		"POST /v1/auth/token/create/payments": json200(map[string]any{"auth": map[string]any{"client_token": "hvs.minted", "accessor": "acc-1", "lease_duration": 900}}),
		"POST /v1/auth/token/revoke-accessor": func(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) },
		"POST /v1/auth/elsewhere/login":       func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadRequest) },
	})
	b := New()
	store := brokeredStore(t, secretstore.KindVault, credentials.Settings{Vault: &credentials.VaultSettings{Address: address, Mount: "kv", Namespace: "team"}},
		credentials.Access{TokenRole: "payments"})
	minting := credentials.MintingCredential{RoleID: "role", SecretID: "secret"}

	m, err := b.Mint(context.Background(), credentials.MintRequest{Store: store, Credential: minting, DeploymentID: "dpl_a"})
	require.NoError(t, err)
	assert.Equal(t, credentials.Minted{Credential: credentials.BrokeredCredential{Token: "hvs.minted"}, ExpiresAt: now.Add(15 * time.Minute), ProviderID: "acc-1"}, m)
	assert.Equal(t, map[string]any{"role_id": "role", "secret_id": "secret"}, rec.got[0].Body)
	assert.Equal(t, "team", rec.got[0].Header.Get("X-Vault-Namespace"))
	assert.Equal(t, "hvs.approle", rec.got[1].Header.Get("X-Vault-Token"))
	assert.Equal(t, false, rec.got[1].Body["renewable"])
	assert.Equal(t, "900s", rec.got[1].Body["ttl"])

	revoked, err := b.Revoke(context.Background(), credentials.RevokeRequest{Store: store, Credential: minting, ProviderID: "acc-1"})
	require.NoError(t, err)
	assert.True(t, revoked)
	assert.Equal(t, map[string]any{"accessor": "acc-1"}, rec.got[3].Body)

	refused := brokeredStore(t, secretstore.KindVault, credentials.Settings{Vault: &credentials.VaultSettings{Address: address, Mount: "kv"}},
		credentials.Access{TokenRole: "payments", AuthMount: "elsewhere"})
	_, err = b.Mint(context.Background(), credentials.MintRequest{Store: refused, Credential: minting, DeploymentID: "dpl_a"})
	require.ErrorIs(t, err, credentials.ErrStoreRefused)
	assert.NotContains(t, err.Error(), "secret\"")
}

// Infisical hands over the identity's access token, which cannot be revoked
// without the token, so it expires instead.
func TestInfisicalBroker(t *testing.T) {
	clock.Fix(t, now)
	rec, site := serve(t, map[string]func(w http.ResponseWriter){
		"POST /api/v1/auth/universal-auth/login": json200(map[string]any{"accessToken": "inf.minted", "expiresIn": 600, "tokenType": "Bearer"}),
	})
	b := New()
	store := brokeredStore(t, secretstore.KindInfisical, credentials.Settings{Infisical: &credentials.InfisicalSettings{SiteURL: site, ProjectID: "prj", Environment: "prod"}}, credentials.Access{})
	minting := credentials.MintingCredential{ClientID: "client", ClientSecret: "secret"}

	m, err := b.Mint(context.Background(), credentials.MintRequest{Store: store, Credential: minting, DeploymentID: "dpl_a"})
	require.NoError(t, err)
	assert.Equal(t, "inf.minted", m.Credential.Token)
	assert.Equal(t, now.Add(10*time.Minute), m.ExpiresAt)
	assert.Regexp(t, `^sha256:[0-9a-f]{16}$`, m.ProviderID)
	assert.Equal(t, map[string]any{"clientId": "client", "clientSecret": "secret"}, rec.got[0].Body)

	revoked, err := b.Revoke(context.Background(), credentials.RevokeRequest{Store: store, Credential: minting, ProviderID: m.ProviderID})
	require.NoError(t, err)
	assert.False(t, revoked)
}

type fakeRoles struct{ got assumeRole }

func (f *fakeRoles) AssumeRole(_ context.Context, in assumeRole) (credentials.BrokeredCredential, time.Time, error) {
	f.got = in
	return credentials.BrokeredCredential{AccessKeyID: "ASIA", SecretAccessKey: "secret", SessionToken: "session"}, now.Add(15 * time.Minute), nil
}

// AWS assumes the store's role, with the store as the external ID, under a
// session policy naming only the Deployment's secrets.
func TestAWSBroker(t *testing.T) {
	roles := &fakeRoles{}
	b := New(func(o *options) { o.roles = roles })
	store := brokeredStore(t, secretstore.KindAWSSecretsManager, credentials.Settings{AWS: &credentials.AWSSettings{Region: "eu-west-1"}},
		credentials.Access{RoleARN: "arn:aws:iam::123456789012:role/ivaldi-payments"})

	m, err := b.Mint(context.Background(), credentials.MintRequest{Store: store, DeploymentID: "dpl_a",
		Paths: []string{"payments/db", "arn:aws:secretsmanager:eu-west-1:123456789012:secret:api-AbCdEf", "payments/db"}})
	require.NoError(t, err)
	assert.Equal(t, credentials.BrokeredCredential{AccessKeyID: "ASIA", SecretAccessKey: "secret", SessionToken: "session"}, m.Credential)
	assert.Equal(t, "ivaldi-dpl_a", m.ProviderID)
	assert.Equal(t, store.ID(), roles.got.ExternalID)
	assert.Equal(t, 15*time.Minute, roles.got.Duration)

	var policy struct {
		Statement []struct {
			Action   string
			Resource any
		}
	}
	require.NoError(t, json.Unmarshal([]byte(roles.got.Policy), &policy))
	assert.Equal(t, "secretsmanager:GetSecretValue", policy.Statement[0].Action)
	assert.Equal(t, []any{
		"arn:aws:secretsmanager:eu-west-1:123456789012:secret:payments/db-??????",
		"arn:aws:secretsmanager:eu-west-1:123456789012:secret:api-AbCdEf",
	}, policy.Statement[0].Resource)

	revoked, err := b.Revoke(context.Background(), credentials.RevokeRequest{Store: store, ProviderID: m.ProviderID})
	require.NoError(t, err)
	assert.False(t, revoked, "an AWS session expires instead")
}
