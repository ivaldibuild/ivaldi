package credentials

import (
	"context"
	"errors"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

// NewStoreLookup returns delivery's view of Secret Stores: what it needs to
// check a secret variable's value may use a store, and to tell an agent how
// to read it. It does not authorize; delivery's own services do. federation
// says whether this installation can sign federated JWTs: without it, a
// federated store cannot be read.
func NewStoreLookup(stores SecretStoreRepository, federation bool) (delivery.SecretStores, error) {
	if stores == nil {
		return nil, errors.New("secret store lookup: a SecretStoreRepository is required")
	}
	return storeLookup{stores: stores, federation: federation}, nil
}

type storeLookup struct {
	stores     SecretStoreRepository
	federation bool
}

func (l storeLookup) LookupSecretStores(ctx context.Context, organizationID string, storeIDs []string) (map[string]delivery.SecretStoreRef, error) {
	return l.lookup(ctx, organizationID, storeIDs, l.stores.GetSecretStores)
}

// LookupSecretStoresForShare holds the stores against the archive and the
// update that take them FOR UPDATE, until the transaction ends.
func (l storeLookup) LookupSecretStoresForShare(ctx context.Context, organizationID string, storeIDs []string) (map[string]delivery.SecretStoreRef, error) {
	return l.lookup(ctx, organizationID, storeIDs, l.stores.GetSecretStoresForShare)
}

func (l storeLookup) lookup(ctx context.Context, organizationID string, storeIDs []string,
	get func(context.Context, string, []string) ([]*SecretStore, error)) (map[string]delivery.SecretStoreRef, error) {
	out := map[string]delivery.SecretStoreRef{}
	if len(storeIDs) == 0 {
		return out, nil
	}
	stores, err := get(ctx, organizationID, storeIDs)
	if err != nil {
		return nil, err
	}
	for _, s := range stores {
		ref := delivery.SecretStoreRef{
			ID: s.ID(), WorkspaceID: s.WorkspaceID(), DisplayName: s.DisplayName(),
			Settings: assignmentSettings(s), EnvironmentIDs: s.EnvironmentIDs(), Archived: s.Archived(),
		}
		if s.Access().Mode == AccessFederated && !l.federation {
			ref.Unusable = ErrNoFederation.Error()
		}
		out[s.ID()] = ref
	}
	return out, nil
}

// assignmentSettings returns a store's settings in the shape an agent is sent.
func assignmentSettings(s *SecretStore) delivery.StoreSettings {
	access := s.Access()
	out := delivery.StoreSettings{
		ID: s.ID(), Kind: delivery.StoreKind(s.Kind()),
		Access: delivery.StoreAccess{
			Mode: delivery.StoreAccessMode(access.Mode), RoleARN: access.RoleARN, Role: access.Role,
			AuthMount: access.AuthMount, Credential: access.Credential, IdentityID: access.IdentityID,
		},
	}
	settings := s.Settings()
	switch s.Kind() {
	case secretstore.KindAWSSecretsManager:
		out.AWS = &delivery.AWSStore{Region: settings.AWS.Region}
	case secretstore.KindVault:
		out.Vault = &delivery.VaultStore{Address: settings.Vault.Address, Mount: settings.Vault.Mount, Namespace: settings.Vault.Namespace}
	case secretstore.KindDoppler:
		out.Doppler = &delivery.DopplerStore{}
		if settings.Doppler != nil {
			out.Doppler.Project, out.Doppler.Config = settings.Doppler.Project, settings.Doppler.Config
		}
	default:
		out.Kubernetes = &delivery.KubernetesStore{}
	}
	return out
}
