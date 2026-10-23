package delivery

// Events a Registry records. Each carries the Registry as it is after the
// change; the audit record keeps the state before it. A Registry holds only
// a reference to its credential, never the credential, so its snapshot is
// safe to publish.

// RegistryCreated is recorded when a Registry is created.
type RegistryCreated struct {
	Registry RegistrySnapshot `json:"registry"`
}

// RegistryUpdated is recorded when a Registry's slug, display name, or URL changes.
type RegistryUpdated struct {
	Registry RegistrySnapshot `json:"registry"`
}

// RegistryCredentialSet is recorded when a Registry is pointed at a
// credential, or at a different one.
type RegistryCredentialSet struct {
	Registry RegistrySnapshot `json:"registry"`
}

// RegistryCredentialCleared is recorded when a Registry becomes anonymous.
type RegistryCredentialCleared struct {
	Registry RegistrySnapshot `json:"registry"`
}

// RegistryLookupChanged is recorded when where a Registry's lookups run changes.
type RegistryLookupChanged struct {
	Registry RegistrySnapshot `json:"registry"`
}

// RegistryArchived is recorded when a Registry is archived.
type RegistryArchived struct {
	Registry RegistrySnapshot `json:"registry"`
}

// RegistryRestored is recorded when an archived Registry is restored.
type RegistryRestored struct {
	Registry RegistrySnapshot `json:"registry"`
}

func (RegistryCreated) EventName() string           { return "delivery.v1.registry.created" }
func (RegistryUpdated) EventName() string           { return "delivery.v1.registry.updated" }
func (RegistryCredentialSet) EventName() string     { return "delivery.v1.registry.credential_set" }
func (RegistryCredentialCleared) EventName() string { return "delivery.v1.registry.credential_cleared" }
func (RegistryLookupChanged) EventName() string     { return "delivery.v1.registry.lookup_changed" }
func (RegistryArchived) EventName() string          { return "delivery.v1.registry.archived" }
func (RegistryRestored) EventName() string          { return "delivery.v1.registry.restored" }
