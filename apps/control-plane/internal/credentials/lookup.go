package credentials

import (
	"context"
	"errors"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

// NewStoreLookup returns delivery's view of Secret Stores: what it needs to
// check a secret variable's value may use a store, and to tell an agent how
// to read it. It does not authorize; delivery's own services do. Without
// WithFederation, a federated store cannot be read, and without
// WithBrokering, a brokered one cannot.
func NewStoreLookup(stores SecretStoreRepository, opts ...LookupOption) (delivery.SecretStores, error) {
	if stores == nil {
		return nil, errors.New("secret store lookup: a SecretStoreRepository is required")
	}
	l := storeLookup{stores: stores}
	for _, opt := range opts {
		opt(&l)
	}
	return l, nil
}

// LookupOption says what this installation can grant.
type LookupOption func(*storeLookup)

// WithFederation says this installation can sign federated JWTs.
func WithFederation(enabled bool) LookupOption {
	return func(l *storeLookup) { l.federation = enabled }
}

// WithBrokering says this installation can keep minting credentials.
func WithBrokering(enabled bool) LookupOption {
	return func(l *storeLookup) { l.brokering = enabled }
}

type storeLookup struct {
	stores     SecretStoreRepository
	federation bool
	brokering  bool
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
		switch {
		case s.Access().Mode == AccessFederated && !l.federation:
			ref.Unusable = ErrNoFederation.Error()
		case s.Access().Mode == AccessBrokered && !l.brokering:
			ref.Unusable = ErrNoBrokering.Error()
		case s.NeedsCredential() && !s.CredentialSet():
			ref.Unusable = ErrNoCredential.Error()
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
			Mode: delivery.StoreAccessMode(access.Mode), Method: delivery.StoreAuthMethod(access.Method),
			RoleARN: access.RoleARN, Role: access.Role,
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
	case secretstore.KindInfisical:
		i := settings.Infisical
		out.Infisical = &delivery.InfisicalStore{
			SiteURL: i.SiteURL, ProjectID: i.ProjectID, Environment: i.Environment, SecretPath: i.SecretPath,
		}
	default:
		out.Kubernetes = &delivery.KubernetesStore{}
	}
	return out
}
