package secretstores

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

// fakeKeeper keeps credentials in the clear, by store.
type fakeKeeper map[string]credentials.MintingCredential

func (k fakeKeeper) Put(_ context.Context, s *credentials.SecretStore, c credentials.MintingCredential) error {
	k[s.ID()] = c
	return nil
}
func (k fakeKeeper) Get(_ context.Context, s *credentials.SecretStore) (credentials.MintingCredential, error) {
	c, ok := k[s.ID()]
	if !ok {
		return credentials.MintingCredential{}, credentials.ErrNoCredential
	}
	return c, nil
}
func (k fakeKeeper) Delete(_ context.Context, _, storeID string) error {
	delete(k, storeID)
	return nil
}

// A brokered store is allowed only where credentials can be kept. Its
// credential is kept by the keeper, recorded on the store by who set it,
// never audited, and deleted when the store moves.
func TestBrokeredStoreCredential(t *testing.T) {
	w := newWorld(t)
	doppler := credentials.CreateSecretStoreInput{
		OrganizationID: org, WorkspaceID: w.workspace.ID(), DisplayName: "Doppler", Kind: secretstore.KindDoppler,
		Settings:       credentials.Settings{Doppler: &credentials.DopplerSettings{Project: "payments", Config: "prd"}},
		Access:         credentials.Access{Mode: credentials.AccessBrokered},
		EnvironmentIDs: []string{w.here},
	}
	_, err := w.svc.CreateSecretStore(ctx, ada, doppler)
	require.ErrorIs(t, err, credentials.ErrInvalid)
	assert.Contains(t, err.Error(), "no key-encryption key")

	kept := fakeKeeper{}
	w.svc, err = NewSecretStoreService(
		WithSecretStoreRepo(w), WithWorkspaces(w), WithEnvironmentLookup(w), WithVariableUsage(w),
		WithAuthorizer(w), WithTransactor(w), WithOutboxRecorder(outboxLog{}), WithAuditRecorder(w), WithCredentialKeeper(kept),
	)
	require.NoError(t, err)
	store, err := w.svc.CreateSecretStore(ctx, ada, doppler)
	require.NoError(t, err)

	store, err = w.svc.SetSecretStoreCredential(ctx, ada, org, store.ID(), credentials.MintingCredential{Token: "dp.sa.s3cret"})
	require.NoError(t, err)
	assert.Equal(t, "prn_ada", *store.Snapshot().CredentialSetBy)
	assert.Equal(t, credentials.MintingCredential{Token: "dp.sa.s3cret"}, kept[store.ID()])
	last := w.asked[len(w.asked)-1]
	assert.Equal(t, credentials.ActionSecretStoreSetCredential, last.Action)
	assert.Nil(t, last.Proposal, "the authorizer is never shown the credential")
	recorded, err := json.Marshal(w.audit)
	require.NoError(t, err)
	assert.NotContains(t, string(recorded), "s3cret", "nor is the audit record")

	_, err = w.svc.UpdateSecretStore(ctx, ada, org, store.ID(), credentials.UpdateSecretStoreProps{
		Settings: &credentials.Settings{Doppler: &credentials.DopplerSettings{Project: "payments", Config: "stg"}},
	})
	require.NoError(t, err)
	assert.Empty(t, kept, "moving the store deletes its credential")
	assert.False(t, w.stores[store.ID()].CredentialSet())

	_, err = w.svc.SetSecretStoreCredential(ctx, ada, org, store.ID(), credentials.MintingCredential{Token: "dp.sa.other"})
	require.NoError(t, err)
	store, err = w.svc.RemoveSecretStoreCredential(ctx, ada, org, store.ID())
	require.NoError(t, err)
	assert.False(t, store.CredentialSet())
	assert.Empty(t, kept)
}
