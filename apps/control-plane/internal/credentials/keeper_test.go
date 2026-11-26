package credentials

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/kms"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

type memoryKeys struct {
	sealed map[string]SealedCredential
	keys   map[string]DataKey
}

func (m *memoryKeys) PutSealedCredential(_ context.Context, c SealedCredential) error {
	m.sealed[c.StoreID] = c
	return nil
}
func (m *memoryKeys) GetSealedCredential(_ context.Context, _, storeID string) (SealedCredential, error) {
	c, ok := m.sealed[storeID]
	if !ok {
		return SealedCredential{}, ErrNotFound
	}
	return c, nil
}
func (m *memoryKeys) DeleteSealedCredential(_ context.Context, _, storeID string) error {
	delete(m.sealed, storeID)
	return nil
}
func (m *memoryKeys) GetDataKey(_ context.Context, org string) (DataKey, error) {
	k, ok := m.keys[org]
	if !ok {
		return DataKey{}, ErrNotFound
	}
	return k, nil
}
func (m *memoryKeys) CreateDataKeyIfAbsent(_ context.Context, k DataKey) error {
	if _, ok := m.keys[k.OrganizationID]; !ok {
		m.keys[k.OrganizationID] = k
	}
	return nil
}

// A credential is stored sealed with the Organization's data key, which is
// stored wrapped, and opens only for its own store.
func TestCredentialKeeper(t *testing.T) {
	ctx := context.Background()
	kek, err := kms.NewLocal(bytes.Repeat([]byte{1}, 32))
	require.NoError(t, err)
	repo := &memoryKeys{sealed: map[string]SealedCredential{}, keys: map[string]DataKey{}}
	k, err := NewCredentialKeeper(kek, repo, repo)
	require.NoError(t, err)
	newStore := func() *SecretStore {
		s, err := NewSecretStore(storeProps(secretstore.KindDoppler, Settings{Doppler: &DopplerSettings{Project: "p", Config: "c"}},
			Access{Mode: AccessBrokered}, testEnvA), nil)
		require.NoError(t, err)
		return s
	}
	a, b := newStore(), newStore()

	_, err = k.Get(ctx, a)
	assert.ErrorIs(t, err, ErrNoCredential)

	credential := MintingCredential{Token: "dp.sa.s3cret"}
	require.NoError(t, k.Put(ctx, a, credential))
	assert.NotContains(t, string(repo.sealed[a.ID()].Ciphertext), "s3cret")
	assert.NotContains(t, string(repo.keys["org_a"].Wrapped), "s3cret")
	got, err := k.Get(ctx, a)
	require.NoError(t, err)
	assert.Equal(t, credential, got)

	moved := repo.sealed[a.ID()]
	moved.StoreID = b.ID()
	repo.sealed[b.ID()] = moved
	_, err = k.Get(ctx, b)
	assert.ErrorIs(t, err, kms.ErrDecrypt, "a credential copied to another store does not open")

	require.NoError(t, k.Delete(ctx, "org_a", a.ID()))
	_, err = k.Get(ctx, a)
	assert.ErrorIs(t, err, ErrNoCredential)
}
