package secretstores

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// SetSecretStoreCredential sets or replaces a brokered store's minting
// credential. The store records who set it and when; the keeper keeps the
// credential, sealed, in the same transaction.
func (s *service) SetSecretStoreCredential(ctx context.Context, actor identity.Actor, organizationID, storeID string, c credentials.MintingCredential) (*credentials.SecretStore, error) {
	if s.keeper == nil {
		return nil, credentials.NewErrInvalidInput("a store cannot be given a minting credential: " + credentials.ErrNoBrokering.Error())
	}
	return s.changeStore(ctx, actor, organizationID, storeID, credentials.EventSecretStoreCredentialSet,
		func(ctx context.Context, store *credentials.SecretStore) error {
			kept, err := store.SetCredential(actor.PrincipalID, c)
			if err != nil {
				return err
			}
			return s.keeper.Put(ctx, store, kept)
		})
}

// RemoveSecretStoreCredential removes a brokered store's minting credential.
// Removing it from a store that has none does nothing.
func (s *service) RemoveSecretStoreCredential(ctx context.Context, actor identity.Actor, organizationID, storeID string) (*credentials.SecretStore, error) {
	return s.changeStore(ctx, actor, organizationID, storeID, credentials.EventSecretStoreCredentialRemoved,
		func(_ context.Context, store *credentials.SecretStore) error {
			if !store.RemoveCredential() {
				return change.ErrUnchanged
			}
			// changeStore deletes what the keeper kept.
			return nil
		})
}
