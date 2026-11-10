package vcs

import "time"

// WebhookAction is a normalized action a provider reported through a webhook.
type WebhookAction string

const (
	WebhookRepositoryChanged       WebhookAction = "repository_changed"
	WebhookRepositoryArchived      WebhookAction = "repository_archived"
	WebhookRepositoryUnarchived    WebhookAction = "repository_unarchived"
	WebhookInstallationSuspended   WebhookAction = "installation_suspended"
	WebhookInstallationUnsuspended WebhookAction = "installation_unsuspended"
	WebhookInstallationDeleted     WebhookAction = "installation_deleted"
)

// Event names the gateway publishes a normalized webhook under.
const (
	EventProviderRepositoryChanged   = "versioncontrol.v1.provider.repository_changed"
	EventProviderInstallationChanged = "versioncontrol.v1.provider.installation_changed"
)

// WebhookEvent is the normalized payload the gateway publishes and the
// control plane consumes. Its JSON form is the contract in
// schemas/version-control-webhook.v1.json.
type WebhookEvent struct {
	DeliveryID     string             `json:"delivery_id"`
	RouteKey       string             `json:"route_key"`
	Provider       ProviderKind       `json:"provider"`
	HostURL        string             `json:"host_url"`
	Action         WebhookAction      `json:"action"`
	InstallationID string             `json:"installation_id,omitempty"`
	Repository     *WebhookRepository `json:"repository,omitempty"`
	// OccurredAt is the provider's timestamp for the change, which orders a
	// repository's or installation's events. It is zero when the provider
	// gave none; such an event applies without being ordered.
	OccurredAt time.Time `json:"occurred_at"`
}

// WebhookRepository is the repository a webhook event reports.
type WebhookRepository struct {
	ExternalID    string `json:"external_id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Visibility    string `json:"visibility"`
	CloneURL      string `json:"clone_url"`
	WebURL        string `json:"web_url"`
	Archived      bool   `json:"archived"`
}

// ProviderRepositoryChanged is the payload of a normalized repository event.
// Its JSON shape is WebhookEvent's.
type ProviderRepositoryChanged struct {
	WebhookEvent
}

func (ProviderRepositoryChanged) EventName() string {
	return EventProviderRepositoryChanged
}

// ProviderInstallationChanged is the payload of a normalized installation
// event. Its JSON shape is WebhookEvent's.
type ProviderInstallationChanged struct {
	WebhookEvent
}

func (ProviderInstallationChanged) EventName() string {
	return EventProviderInstallationChanged
}

// WebhookRoute is where a platform-wide webhook route key resolves.
type WebhookRoute struct {
	OrganizationID string       `json:"organization_id"`
	ConnectionID   string       `json:"connection_id"`
	Provider       ProviderKind `json:"provider"`
	HostURL        string       `json:"host_url"`
}
