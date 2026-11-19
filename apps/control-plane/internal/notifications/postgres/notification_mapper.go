package postgres

import (
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications"
)

type notificationMapper struct{}

func (notificationMapper) ToPersistence(n *notifications.Notification) *notificationEntity {
	s := n.Snapshot()
	return &notificationEntity{
		ID: s.ID, OrganizationID: s.OrganizationID, RecipientPrincipalID: s.RecipientPrincipalID,
		Kind: string(s.Kind), SubjectType: s.SubjectType, SubjectID: s.SubjectID, EventID: s.EventID,
		Title: s.Title, Body: s.Body, Link: s.Link, OwnerTeamID: s.OwnerTeamID,
		ReadAt: s.ReadAt, ResolvedAt: s.ResolvedAt, EmailStatus: string(s.EmailStatus), EmailedAt: s.EmailedAt,
		CreatedAt: s.CreatedAt,
	}
}

func (notificationMapper) ToDomain(e *notificationEntity) (*notifications.Notification, error) {
	created := e.CreatedAt.UTC()
	return notifications.NewNotification(notifications.NewNotificationProps{
		OrganizationID: e.OrganizationID, RecipientPrincipalID: e.RecipientPrincipalID,
		Kind: notifications.Kind(e.Kind), SubjectType: e.SubjectType, SubjectID: e.SubjectID, EventID: e.EventID,
		Title: e.Title, Body: e.Body, Link: e.Link, OwnerTeamID: e.OwnerTeamID,
		ReadAt: utc(e.ReadAt), ResolvedAt: utc(e.ResolvedAt),
		EmailStatus: notifications.EmailStatus(e.EmailStatus), EmailedAt: utc(e.EmailedAt), CreatedAt: &created,
	}, &e.ID)
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	return new(t.UTC())
}
