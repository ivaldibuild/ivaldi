package inbox

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type NotificationResponse struct {
	Body api.Response[notifications.NotificationSnapshot]
}

type NotificationListResponse struct {
	Body api.Response[pagination.Result[notifications.NotificationSnapshot]]
}

// UnreadCount is how many of the caller's notifications are neither read
// nor resolved.
type UnreadCount struct {
	Count int `json:"count"`
}

type UnreadCountResponse struct {
	Body api.Response[UnreadCount]
}
