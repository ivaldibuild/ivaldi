package credentials

import "time"

// Limits on what one record may hold. Text lengths match the columns.
const (
	maxDisplayNameLength = 255
	maxSettingLength     = 512
	// maxCredentialNameLength is a DNS subdomain's, so a Kubernetes
	// Secret's name can serve as a credential's.
	maxCredentialNameLength = 253
	maxStoreEnvironments    = 50
	// maxMintingCredentialLength bounds each field of a minting credential.
	maxMintingCredentialLength = 4096
	// MaxSecretStoresPerWorkspace bounds a Workspace's active Secret Stores,
	// which are listed whole.
	MaxSecretStoresPerWorkspace = 100
)

// BrokeredLifetime is how long a brokered credential lasts where the store
// lets the control plane choose: long enough for an agent to read a
// Deployment's secrets, and no longer.
const BrokeredLifetime = 15 * time.Minute

// AggregateSecretStore names the domain object that raises events.
const AggregateSecretStore = "credentials.SecretStore"

// Permissions this capability asks the authorizer about, on the Workspace a
// Secret Store belongs to.
const (
	PermissionSecretStoreRead   = "secret_store.read"
	PermissionSecretStoreManage = "secret_store.manage"
)

// Actions name what an actor asks the authorizer to allow.
const (
	ActionSecretStoreArchive          = "secret_store.archive"
	ActionSecretStoreCreate           = "secret_store.create"
	ActionSecretStoreList             = "secret_store.list"
	ActionSecretStoreRead             = "secret_store.read"
	ActionSecretStoreUpdate           = "secret_store.update"
	ActionSecretStoreSetCredential    = "secret_store.set_credential"
	ActionSecretStoreRemoveCredential = "secret_store.remove_credential"
)

// Audit event types.
const (
	EventSecretStoreCreated  = "secret_store.created"
	EventSecretStoreUpdated  = "secret_store.updated"
	EventSecretStoreArchived = "secret_store.archived"
	// EventSecretStoreAccessGranted is recorded for each grant, by the
	// agent it was made to, on the Deployment it is for.
	EventSecretStoreAccessGranted = "secret_store.access_granted"
	// EventSecretStoreAccessRevoked is recorded, by the system, for each
	// brokered grant revoked when its Deployment leaves applying.
	EventSecretStoreAccessRevoked = "secret_store.access_revoked"
	// EventSecretStoreCredentialSet and EventSecretStoreCredentialRemoved
	// record a brokered store's minting credential being set or removed;
	// never the credential.
	EventSecretStoreCredentialSet     = "secret_store.credential_set"
	EventSecretStoreCredentialRemoved = "secret_store.credential_removed"
)
