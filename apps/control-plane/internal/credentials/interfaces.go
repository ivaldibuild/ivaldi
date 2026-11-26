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

// SecretStoreGrantRepository stores grants of access to Secret Stores.
type SecretStoreGrantRepository interface {
	CreateSecretStoreGrants(ctx context.Context, grants []*SecretStoreGrant) error
	// ListDeploymentGrants returns the grants made for a Deployment, oldest
	// first.
	ListDeploymentGrants(ctx context.Context, organizationID, deploymentID string) ([]*SecretStoreGrant, error)
	// UpdateSecretStoreGrant saves a grant's revocation.
	UpdateSecretStoreGrant(ctx context.Context, g *SecretStoreGrant) error
}

// SecretStoreCredentialRepository stores brokered stores' minting
// credentials, sealed; it never sees one in the clear.
type SecretStoreCredentialRepository interface {
	// PutSealedCredential saves a store's sealed credential, replacing any.
	PutSealedCredential(ctx context.Context, c SealedCredential) error
	// GetSealedCredential returns ErrNotFound when the store has none.
	GetSealedCredential(ctx context.Context, organizationID, storeID string) (SealedCredential, error)
	DeleteSealedCredential(ctx context.Context, organizationID, storeID string) error
}

// DataKeyRepository stores each Organization's data key, wrapped by the
// installation's key-encryption key.
type DataKeyRepository interface {
	// GetDataKey returns ErrNotFound when the Organization has none yet.
	GetDataKey(ctx context.Context, organizationID string) (DataKey, error)
	// CreateDataKeyIfAbsent saves a data key unless the Organization has
	// one, which wins.
	CreateDataKeyIfAbsent(ctx context.Context, k DataKey) error
}

// CredentialKeeper keeps brokered stores' minting credentials, encrypted
// with envelope encryption (ADR 0004). Only it and the brokers ever see one
// in the clear.
type CredentialKeeper interface {
	Put(ctx context.Context, store *SecretStore, c MintingCredential) error
	// Get returns ErrNoCredential when the store has none.
	Get(ctx context.Context, store *SecretStore) (MintingCredential, error)
	Delete(ctx context.Context, organizationID, storeID string) error
}

// Broker mints, and revokes, the credentials brokered stores are read with.
// Its errors match ErrStoreRefused when the store refused, and
// ErrStoreUnavailable when it could not be reached; neither contains a
// credential.
type Broker interface {
	// Mint mints a read-only, short-lived credential for one Deployment's
	// secrets in a store, scoped as narrowly as the store allows.
	Mint(ctx context.Context, r MintRequest) (Minted, error)
	// Revoke revokes what Mint minted, and reports whether the store could:
	// an AWS session or an Infisical access token cannot be revoked, and
	// expires instead.
	Revoke(ctx context.Context, r RevokeRequest) (bool, error)
}

// CreateSecretStoreInput describes a Secret Store to create.
type CreateSecretStoreInput struct {
	OrganizationID string
	WorkspaceID    string
	Slug           string
	DisplayName    string
	Kind           secretstore.Kind
	Settings       Settings
	Access         Access
	EnvironmentIDs []string
}

// SecretStoreService manages a Workspace's Secret Stores. Reading needs
// secret_store.read and changing needs secret_store.manage, on the
// Workspace. A store holds no credential, so reading one reveals where
// secrets are kept and how they are opened, not what they are.
type SecretStoreService interface {
	CreateSecretStore(ctx context.Context, actor identity.Actor, in CreateSecretStoreInput) (*SecretStore, error)
	GetSecretStore(ctx context.Context, actor identity.Actor, organizationID, storeID string) (*SecretStore, error)
	ListSecretStores(ctx context.Context, actor identity.Actor, organizationID, workspaceID string, includeArchived bool) ([]*SecretStore, error)
	UpdateSecretStore(ctx context.Context, actor identity.Actor, organizationID, storeID string, props UpdateSecretStoreProps) (*SecretStore, error)
	// ArchiveSecretStore archives a store no active variable's value refers to.
	ArchiveSecretStore(ctx context.Context, actor identity.Actor, organizationID, storeID string) (*SecretStore, error)
	// SetSecretStoreCredential sets or replaces a brokered store's minting
	// credential. It is never read back.
	SetSecretStoreCredential(ctx context.Context, actor identity.Actor, organizationID, storeID string, c MintingCredential) (*SecretStore, error)
	// RemoveSecretStoreCredential removes a brokered store's minting
	// credential.
	RemoveSecretStoreCredential(ctx context.Context, actor identity.Actor, organizationID, storeID string) (*SecretStore, error)
}
