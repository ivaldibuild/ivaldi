package credentials

import (
	"context"
	"errors"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
)

// NewStoreLookup returns delivery's view of Secret Stores: what it needs to
// check a secret variable's value may use a store, and to tell an agent how
// to read it. It does not authorize; delivery's own services do.
func NewStoreLookup(stores SecretStoreRepository) (delivery.SecretStores, error) {
	if stores == nil {
		return nil, errors.New("secret store lookup: a SecretStoreRepository is required")
	}
	return storeLookup{stores: stores}, nil
}

type storeLookup struct {
	stores SecretStoreRepository
}

func (l storeLookup) LookupSecretStores(ctx context.Context, organizationID string, storeIDs []string) (map[string]delivery.SecretStoreRef, error) {
	out := map[string]delivery.SecretStoreRef{}
	if len(storeIDs) == 0 {
		return out, nil
	}
	stores, err := l.stores.GetSecretStores(ctx, organizationID, storeIDs)
	if err != nil {
		return nil, err
	}
	for _, s := range stores {
		out[s.ID()] = delivery.SecretStoreRef{
			ID: s.ID(), WorkspaceID: s.WorkspaceID(), DisplayName: s.DisplayName(),
			Settings: assignmentSettings(s), EnvironmentIDs: s.EnvironmentIDs(), Archived: s.Archived(),
		}
	}
	return out, nil
}

// assignmentSettings returns a store's settings in the shape an agent is sent.
func assignmentSettings(s *SecretStore) delivery.StoreSettings {
	out := delivery.StoreSettings{ID: s.ID(), Kind: delivery.StoreKind(s.Kind())}
	settings := s.Settings()
	switch {
	case settings.AWS != nil:
		out.AWS = &delivery.AWSStore{Region: settings.AWS.Region, RoleARN: settings.AWS.RoleARN}
	case settings.Vault != nil:
		out.Vault = &delivery.VaultStore{
			Address: settings.Vault.Address, Mount: settings.Vault.Mount, AuthRole: settings.Vault.AuthRole,
			AuthMount: settings.Vault.AuthMount, Namespace: settings.Vault.Namespace,
		}
	case settings.Doppler != nil:
		out.Doppler = &delivery.DopplerStore{TokenSecretName: settings.Doppler.TokenSecretName, TokenSecretKey: settings.Doppler.TokenSecretKey}
	default:
		out.Kubernetes = &delivery.KubernetesStore{}
	}
	return out
}
