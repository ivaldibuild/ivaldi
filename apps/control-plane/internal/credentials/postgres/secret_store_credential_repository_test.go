package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials/migrations"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

// A brokered store's sealed credential and its Organization's data key
// round-trip, and the store's row records who set the credential.
func TestSecretStoreCredentialRepository(t *testing.T) {
	db := databasetest.New(t, migrations.FS)
	clock.Fix(t, clock.Now())
	ctx := context.Background()
	const org = "org_a"

	stores := NewSecretStoreRepository(db)
	store, err := credentials.NewSecretStore(credentials.NewSecretStoreProps{
		OrganizationID: org, WorkspaceID: "wsp_a", DisplayName: "Doppler", Kind: secretstore.KindDoppler,
		Settings:       credentials.Settings{Doppler: &credentials.DopplerSettings{Project: "payments", Config: "prd"}},
		Access:         credentials.Access{Mode: credentials.AccessBrokered},
		EnvironmentIDs: []string{"env_01BX5ZZKBKACTAV9WEVGEMMVRA"},
	}, nil)
	require.NoError(t, err)
	require.NoError(t, stores.CreateSecretStore(ctx, store))
	_, err = store.SetCredential("prn_ada", credentials.MintingCredential{Token: "dp.sa.x"})
	require.NoError(t, err)
	require.NoError(t, stores.UpdateSecretStore(ctx, store))
	loaded, err := stores.GetSecretStore(ctx, org, store.ID())
	require.NoError(t, err)
	assert.Equal(t, store.Snapshot(), loaded.Snapshot(), "who set the credential, and when")

	repo := NewSecretStoreCredentialRepository(db)
	_, err = repo.GetSealedCredential(ctx, org, store.ID())
	assert.ErrorIs(t, err, credentials.ErrNotFound)
	require.NoError(t, repo.PutSealedCredential(ctx, credentials.SealedCredential{OrganizationID: org, StoreID: store.ID(), Ciphertext: []byte{1}}))
	require.NoError(t, repo.PutSealedCredential(ctx, credentials.SealedCredential{OrganizationID: org, StoreID: store.ID(), Ciphertext: []byte{2}}))
	sealed, err := repo.GetSealedCredential(ctx, org, store.ID())
	require.NoError(t, err)
	assert.Equal(t, []byte{2}, sealed.Ciphertext, "replaced")
	_, err = repo.GetSealedCredential(ctx, "org_b", store.ID())
	assert.ErrorIs(t, err, credentials.ErrNotFound, "another organization's")
	require.NoError(t, repo.DeleteSealedCredential(ctx, org, store.ID()))
	_, err = repo.GetSealedCredential(ctx, org, store.ID())
	assert.ErrorIs(t, err, credentials.ErrNotFound)

	_, err = repo.GetDataKey(ctx, org)
	assert.ErrorIs(t, err, credentials.ErrNotFound)
	require.NoError(t, repo.CreateDataKeyIfAbsent(ctx, credentials.DataKey{OrganizationID: org, KeyID: "local:a", Wrapped: []byte{1}}))
	require.NoError(t, repo.CreateDataKeyIfAbsent(ctx, credentials.DataKey{OrganizationID: org, KeyID: "local:b", Wrapped: []byte{2}}))
	key, err := repo.GetDataKey(ctx, org)
	require.NoError(t, err)
	assert.Equal(t, credentials.DataKey{OrganizationID: org, KeyID: "local:a", Wrapped: []byte{1}}, key, "the first one saved wins")
}
