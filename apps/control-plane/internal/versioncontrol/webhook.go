package versioncontrol

import (
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// WebhookDelivery is a provider webhook delivery the control plane has
// handled, recorded once its route key has resolved to an Organization. Its
// primary key (provider, route key, delivery ID) is platform-wide, so a
// redelivery is recognized whichever Organization it resolves to.
type WebhookDelivery struct {
	Provider       vcs.ProviderKind
	RouteKey       string
	DeliveryID     string
	OrganizationID string
	// ReceivedAt defaults to the current time when zero.
	ReceivedAt time.Time
}
