package versioncontrol

import (
	"context"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// WebhookDelivery is a provider webhook delivery the control plane has
// handled. Its primary key is platform-wide, so a redelivery is recognized
// even before the route key is resolved to an Organization.
type WebhookDelivery struct {
	Provider       vcs.ProviderKind
	RouteKey       string
	DeliveryID     string
	OrganizationID string
	ReceivedAt     time.Time
}

// WebhookDeliveryRepository records handled webhook deliveries for
// deduplication.
type WebhookDeliveryRepository interface {
	// RecordWebhookDelivery inserts the delivery and reports whether it
	// was new. A duplicate is not an error.
	RecordWebhookDelivery(ctx context.Context, d WebhookDelivery) (bool, error)
}
