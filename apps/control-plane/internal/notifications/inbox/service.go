// Package inbox implements notifications.InboxService: people reading their
// own notifications, and its HTTP API.
//
// NewService returns the service wrapped in an authorizing layer
// (authorization.go). Reading and marking notifications read is contact
// tracking, not a change anyone authorizes on someone else's behalf, so it
// records no event and no audit record.
package inbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type Option func(*options)

type options struct {
	repo  notifications.NotificationRepository
	authz authorization.Authorizer
}

func WithNotificationRepo(r notifications.NotificationRepository) Option {
	return func(o *options) { o.repo = r }
}

func WithAuthorizer(a authorization.Authorizer) Option { return func(o *options) { o.authz = a } }

// NewService returns the InboxService, authorizing every operation, or an
// error naming a missing dependency.
func NewService(opts ...Option) (notifications.InboxService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	switch {
	case o.repo == nil:
		return nil, errors.New("inbox service: NotificationRepo is required")
	case o.authz == nil:
		return nil, errors.New("inbox service: Authorizer is required")
	}
	return &authorized{next: &service{repo: o.repo}, authz: o.authz}, nil
}

// service serves the actor's own notifications once the call is
// authorized. Every method reads and writes only the actor's.
type service struct {
	repo notifications.NotificationRepository
}

var _ notifications.InboxService = (*service)(nil)

func (s *service) ListNotifications(ctx context.Context, actor identity.Actor, organizationID string, unread bool, page pagination.Query) (*pagination.Result[notifications.NotificationSnapshot], error) {
	if page.SortOrder == "" {
		page.SortOrder = pagination.SortDescending
	}
	found, err := s.repo.ListNotifications(ctx, notifications.ListNotificationsQuery{
		OrganizationID: organizationID, RecipientPrincipalID: actor.PrincipalID, Unread: unread, Page: page,
	})
	if err != nil {
		return nil, err
	}
	items := make([]notifications.NotificationSnapshot, len(found.Items))
	for i, n := range found.Items {
		items[i] = n.Snapshot()
	}
	return pagination.NewResult(items, found.TotalCount, found.HasNext, found.HasPrevious, found.NextCursor, found.PreviousCursor), nil
}

func (s *service) CountUnread(ctx context.Context, actor identity.Actor, organizationID string) (int, error) {
	return s.repo.CountUnread(ctx, organizationID, actor.PrincipalID)
}

func (s *service) MarkRead(ctx context.Context, actor identity.Actor, organizationID, notificationID string) (*notifications.Notification, error) {
	n, err := s.repo.GetNotification(ctx, organizationID, notificationID)
	if err != nil {
		return nil, err
	}
	if n.RecipientPrincipalID() != actor.PrincipalID {
		return nil, notifications.ErrNotFound
	}
	if n.MarkRead() {
		if err := s.repo.UpdateNotification(ctx, n); err != nil {
			return nil, fmt.Errorf("mark notification %s read: %w", notificationID, err)
		}
	}
	return n, nil
}

func (s *service) MarkAllRead(ctx context.Context, actor identity.Actor, organizationID string) error {
	return s.repo.MarkAllRead(ctx, organizationID, actor.PrincipalID, clock.Now())
}
