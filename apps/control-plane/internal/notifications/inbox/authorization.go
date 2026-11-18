package inbox

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// authorized asks the authorizer before each operation. Every operation
// needs notification.read on the caller's Organization, which every member
// holds; the service then touches only the caller's own notifications.
type authorized struct {
	next  notifications.InboxService
	authz authorization.Authorizer
}

var _ notifications.InboxService = (*authorized)(nil)

func (a *authorized) require(ctx context.Context, actor identity.Actor, organizationID, action string) error {
	_, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: notifications.PermissionNotificationRead, OrganizationID: organizationID,
		ResourceID: organizationID, Action: action,
	})
	return err
}

func (a *authorized) ListNotifications(ctx context.Context, actor identity.Actor, organizationID string, unread bool, page pagination.Query) (*pagination.Result[notifications.NotificationSnapshot], error) {
	if err := a.require(ctx, actor, organizationID, notifications.ActionNotificationList); err != nil {
		return nil, err
	}
	return a.next.ListNotifications(ctx, actor, organizationID, unread, page)
}

func (a *authorized) CountUnread(ctx context.Context, actor identity.Actor, organizationID string) (int, error) {
	if err := a.require(ctx, actor, organizationID, notifications.ActionNotificationList); err != nil {
		return 0, err
	}
	return a.next.CountUnread(ctx, actor, organizationID)
}

func (a *authorized) MarkRead(ctx context.Context, actor identity.Actor, organizationID, notificationID string) (*notifications.Notification, error) {
	if err := a.require(ctx, actor, organizationID, notifications.ActionNotificationRead); err != nil {
		return nil, err
	}
	return a.next.MarkRead(ctx, actor, organizationID, notificationID)
}

func (a *authorized) MarkAllRead(ctx context.Context, actor identity.Actor, organizationID string) error {
	if err := a.require(ctx, actor, organizationID, notifications.ActionNotificationRead); err != nil {
		return err
	}
	return a.next.MarkAllRead(ctx, actor, organizationID)
}
