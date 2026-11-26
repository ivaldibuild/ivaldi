package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi/pkg/kms"
)

// NewCredentialKeeper returns the CredentialKeeper. Each Organization's
// credentials are sealed with its data key, which kek wraps; a credential
// opens only for the store it was sealed for.
func NewCredentialKeeper(kek kms.KeyEncryptor, credentials SecretStoreCredentialRepository, keys DataKeyRepository) (CredentialKeeper, error) {
	if kek == nil || credentials == nil || keys == nil {
		return nil, errors.New("credential keeper: a key-encryption key and both repositories are required")
	}
	return &keeper{kek: kek, credentials: credentials, keys: keys}, nil
}

type keeper struct {
	kek         kms.KeyEncryptor
	credentials SecretStoreCredentialRepository
	keys        DataKeyRepository
}

// aad binds a sealed credential to its store, so one copied to another
// store's row does not open.
func aad(organizationID, storeID string) []byte {
	return []byte("credentials.secret_store_credentials:" + organizationID + ":" + storeID)
}

func (k *keeper) Put(ctx context.Context, store *SecretStore, c MintingCredential) error {
	dataKey, err := k.dataKey(ctx, store.OrganizationID(), true)
	if err != nil {
		return err
	}
	plaintext, err := json.Marshal(c)
	if err != nil {
		return err
	}
	sealed, err := kms.Seal(dataKey, plaintext, aad(store.OrganizationID(), store.ID()))
	if err != nil {
		return fmt.Errorf("seal the credential: %w", err)
	}
	return k.credentials.PutSealedCredential(ctx, SealedCredential{
		OrganizationID: store.OrganizationID(), StoreID: store.ID(), Ciphertext: sealed,
	})
}

func (k *keeper) Get(ctx context.Context, store *SecretStore) (MintingCredential, error) {
	sealed, err := k.credentials.GetSealedCredential(ctx, store.OrganizationID(), store.ID())
	if errors.Is(err, ErrNotFound) {
		return MintingCredential{}, fmt.Errorf("%w: secret store %s", ErrNoCredential, store.ID())
	}
	if err != nil {
		return MintingCredential{}, fmt.Errorf("get the credential: %w", err)
	}
	dataKey, err := k.dataKey(ctx, store.OrganizationID(), false)
	if err != nil {
		return MintingCredential{}, err
	}
	plaintext, err := kms.Open(dataKey, sealed.Ciphertext, aad(store.OrganizationID(), store.ID()))
	if err != nil {
		return MintingCredential{}, fmt.Errorf("open the credential of secret store %s: %w", store.ID(), err)
	}
	var c MintingCredential
	if err := json.Unmarshal(plaintext, &c); err != nil {
		return MintingCredential{}, fmt.Errorf("the credential of secret store %s is unreadable", store.ID())
	}
	return c, nil
}

func (k *keeper) Delete(ctx context.Context, organizationID, storeID string) error {
	return k.credentials.DeleteSealedCredential(ctx, organizationID, storeID)
}

// dataKey returns the Organization's data key, unwrapped, creating it when
// create is set and it has none. Two creating at once keep the first saved.
func (k *keeper) dataKey(ctx context.Context, organizationID string, create bool) ([]byte, error) {
	stored, err := k.keys.GetDataKey(ctx, organizationID)
	if errors.Is(err, ErrNotFound) && create {
		if err := k.createDataKey(ctx, organizationID); err != nil {
			return nil, err
		}
		stored, err = k.keys.GetDataKey(ctx, organizationID)
	}
	if err != nil {
		return nil, fmt.Errorf("get the data key: %w", err)
	}
	key, err := k.kek.Unwrap(ctx, kms.Wrapped{KeyID: stored.KeyID, Ciphertext: stored.Wrapped})
	if err != nil {
		return nil, fmt.Errorf("unwrap the data key: %w", err)
	}
	return key, nil
}

func (k *keeper) createDataKey(ctx context.Context, organizationID string) error {
	key, err := kms.NewDataKey()
	if err != nil {
		return err
	}
	wrapped, err := k.kek.Wrap(ctx, key)
	if err != nil {
		return fmt.Errorf("wrap the data key: %w", err)
	}
	if err := k.keys.CreateDataKeyIfAbsent(ctx, DataKey{OrganizationID: organizationID, KeyID: wrapped.KeyID, Wrapped: wrapped.Ciphertext}); err != nil {
		return fmt.Errorf("save the data key: %w", err)
	}
	return nil
}
