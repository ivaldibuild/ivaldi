package inbox

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Controller serves the caller's own notifications.
type Controller struct {
	service notifications.InboxService
}

var _ api.Routes = (*Controller)(nil)

func NewController(service notifications.InboxService) *Controller {
	return &Controller{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *Controller) RegisterRoutes(h huma.API) {
	register(h, "list-notifications", http.MethodGet, "/notifications",
		"List your notifications", 0, c.ListNotifications)
	register(h, "count-unread-notifications", http.MethodGet, "/notifications/unread-count",
		"Count your notifications that are neither read nor resolved", 0, c.CountUnread)
	register(h, "mark-notification-read", http.MethodPost, "/notifications/{notification_id}/read",
		"Mark one of your notifications read", 0, c.MarkRead)
	register(h, "mark-notifications-read", http.MethodPost, "/notifications/read",
		"Mark all your notifications read", http.StatusNoContent, c.MarkAllRead)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID: id, Method: method, Path: path, Summary: summary,
		Tags: []string{"notifications"}, DefaultStatus: status, Security: bearer,
	}, handler)
}

// caller returns the authenticated caller. The API refuses a secured
// operation without a valid token, so a missing caller is a wiring bug.
func caller(ctx context.Context) (identity.Caller, error) {
	c, ok := identity.CallerFrom(ctx)
	if !ok {
		return identity.Caller{}, huma.Error401Unauthorized("an access token is required")
	}
	return c, nil
}

func (c *Controller) ListNotifications(ctx context.Context, req *ListNotificationsRequest) (*NotificationListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	page, err := c.service.ListNotifications(ctx, who.Actor, who.OrganizationID, req.Unread, req.page())
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return &NotificationListResponse{Body: api.SuccessResponse(nil, *page)}, nil
}

func (c *Controller) CountUnread(ctx context.Context, _ *struct{}) (*UnreadCountResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	n, err := c.service.CountUnread(ctx, who.Actor, who.OrganizationID)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return &UnreadCountResponse{Body: api.SuccessResponse(nil, UnreadCount{Count: n})}, nil
}

func (c *Controller) MarkRead(ctx context.Context, req *NotificationIDRequest) (*NotificationResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	n, err := c.service.MarkRead(ctx, who.Actor, who.OrganizationID, req.NotificationID)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return &NotificationResponse{Body: api.SuccessResponse(nil, n.Snapshot())}, nil
}

func (c *Controller) MarkAllRead(ctx context.Context, _ *struct{}) (*struct{}, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.service.MarkAllRead(ctx, who.Actor, who.OrganizationID); err != nil {
		return nil, httpError(ctx, err)
	}
	return nil, nil
}
