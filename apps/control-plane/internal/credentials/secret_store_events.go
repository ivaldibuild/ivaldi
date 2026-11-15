package credentials

// Events a Secret Store records. Each carries the store as it is after the
// change; the audit record keeps the state before it.

// SecretStoreCreated is recorded when a Secret Store is created.
type SecretStoreCreated struct {
	SecretStore SecretStoreSnapshot `json:"secret_store"`
}

// SecretStoreUpdated is recorded when a Secret Store's name, slug, settings,
// or Environments change.
type SecretStoreUpdated struct {
	SecretStore SecretStoreSnapshot `json:"secret_store"`
}

// SecretStoreArchived is recorded when a Secret Store is archived.
type SecretStoreArchived struct {
	SecretStore SecretStoreSnapshot `json:"secret_store"`
}

func (SecretStoreCreated) EventName() string  { return "credentials.v1.secret_store.created" }
func (SecretStoreUpdated) EventName() string  { return "credentials.v1.secret_store.updated" }
func (SecretStoreArchived) EventName() string { return "credentials.v1.secret_store.archived" }
