package credentials

import (
	"context"

	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

// SecretStoreRepository stores Secret Stores. Every read is limited to one
// Organization.
type SecretStoreRepository interface {
	// CreateSecretStore returns ErrSlugTaken when the Workspace has an
	// active store with the slug.
	CreateSecretStore(ctx context.Context, s *SecretStore) error
	GetSecretStore(ctx context.Context, organizationID, storeID string) (*SecretStore, error)
	// GetSecretStoreForUpdate is GetSecretStore that also locks the row
	// until the transaction ends. It must be called inside a transaction.
	GetSecretStoreForUpdate(ctx context.Context, organizationID, storeID string) (*SecretStore, error)
	// GetSecretStores returns the stores that exist among the IDs, archived
	// ones included.
	GetSecretStores(ctx context.Context, organizationID string, storeIDs []string) ([]*SecretStore, error)
	// GetSecretStoresForShare is GetSecretStores that also holds the rows
	// FOR SHARE, in ID order, until the transaction ends, so they cannot be
	// changed or archived meanwhile while other readers still can read them.
	// It must be called inside a transaction.
	GetSecretStoresForShare(ctx context.Context, organizationID string, storeIDs []string) ([]*SecretStore, error)
	// ListSecretStores returns a Workspace's stores by name.
	ListSecretStores(ctx context.Context, organizationID, workspaceID string, includeArchived bool) ([]*SecretStore, error)
	UpdateSecretStore(ctx context.Context, s *SecretStore) error
	CountActiveSecretStores(ctx context.Context, organizationID, workspaceID string) (int, error)
}

// CreateSecretStoreInput describes a Secret Store to create.
type CreateSecretStoreInput struct {
	OrganizationID string
	WorkspaceID    string
	Slug           string
	DisplayName    string
	Kind           secretstore.Kind
	Settings       Settings
	EnvironmentIDs []string
}

// SecretStoreService manages a Workspace's Secret Stores. Reading needs
// secret_store.read and changing needs secret_store.manage, on the
// Workspace. A store holds no credential, so reading one reveals where
// secrets are kept, not what they are.
type SecretStoreService interface {
	CreateSecretStore(ctx context.Context, actor identity.Actor, in CreateSecretStoreInput) (*SecretStore, error)
	GetSecretStore(ctx context.Context, actor identity.Actor, organizationID, storeID string) (*SecretStore, error)
	ListSecretStores(ctx context.Context, actor identity.Actor, organizationID, workspaceID string, includeArchived bool) ([]*SecretStore, error)
	UpdateSecretStore(ctx context.Context, actor identity.Actor, organizationID, storeID string, props UpdateSecretStoreProps) (*SecretStore, error)
	// ArchiveSecretStore archives a store no active variable's value refers to.
	ArchiveSecretStore(ctx context.Context, actor identity.Actor, organizationID, storeID string) (*SecretStore, error)
}
