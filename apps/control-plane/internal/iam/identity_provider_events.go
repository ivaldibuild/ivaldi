package iam

// Events an IdentityProvider records. Each carries the IdentityProvider as
// it is after the change; the audit record keeps the state before it. Its
// secrets are held elsewhere and appear only as references.

// IdentityProviderCreated is recorded when an IdentityProvider is created.
type IdentityProviderCreated struct {
	IdentityProvider IdentityProviderSnapshot `json:"identity_provider"`
}

// IdentityProviderUpdated is recorded when an IdentityProvider's name,
// config, secret reference, or JIT provisioning changes.
type IdentityProviderUpdated struct {
	IdentityProvider IdentityProviderSnapshot `json:"identity_provider"`
}

// IdentityProviderConfigured is recorded when the installation's
// configuration changes an IdentityProvider, making it active and managed by
// configuration.
type IdentityProviderConfigured struct {
	IdentityProvider IdentityProviderSnapshot `json:"identity_provider"`
}

// IdentityProviderStatusChanged is recorded when an IdentityProvider is
// enabled or disabled, including when it is removed from the installation's
// configuration.
type IdentityProviderStatusChanged struct {
	IdentityProvider IdentityProviderSnapshot `json:"identity_provider"`
	PreviousStatus   IdentityProviderStatus   `json:"previous_status"`
}

func (IdentityProviderCreated) EventName() string    { return "iam.v1.identity_provider.created" }
func (IdentityProviderUpdated) EventName() string    { return "iam.v1.identity_provider.updated" }
func (IdentityProviderConfigured) EventName() string { return "iam.v1.identity_provider.configured" }
func (IdentityProviderStatusChanged) EventName() string {
	return "iam.v1.identity_provider.status_changed"
}
