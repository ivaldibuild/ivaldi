package inbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// oneNotification is a repository holding one notification, for ada.
type oneNotification struct {
	n       *notifications.Notification
	updated int
}

func (r *oneNotification) GetNotification(context.Context, string, string) (*notifications.Notification, error) {
	return r.n, nil
}

func (r *oneNotification) UpdateNotification(context.Context, *notifications.Notification) error {
	r.updated++
	return nil
}

func (r *oneNotification) CreateNotifications(context.Context, []*notifications.Notification) error {
	panic("unused")
}
func (r *oneNotification) ListNotifications(context.Context, notifications.ListNotificationsQuery) (*pagination.Result[*notifications.Notification], error) {
	panic("unused")
}
func (r *oneNotification) ListPendingEmails(context.Context, string, string) ([]*notifications.Notification, error) {
	panic("unused")
}
func (r *oneNotification) CountUnread(context.Context, string, string) (int, error) { panic("unused") }
func (r *oneNotification) MarkAllRead(context.Context, string, string, time.Time) error {
	panic("unused")
}
func (r *oneNotification) ResolveForSubject(context.Context, string, notifications.Kind, string, string, time.Time) error {
	panic("unused")
}

func TestOnlyTheRecipientMarksANotificationRead(t *testing.T) {
	n, err := notifications.NewNotification(notifications.NewNotificationProps{
		OrganizationID: "org_a", RecipientPrincipalID: "prn_ada", Kind: notifications.KindApprovalRequested,
		SubjectType: notifications.SubjectApprovalRequest, SubjectID: "apr_1", EventID: "evt_1",
		Title: "Deploy api 1.4.0 to Production", Link: "/approvals/apr_1",
	}, nil)
	require.NoError(t, err)
	repo := &oneNotification{n: n}
	svc := &service{repo: repo}

	_, err = svc.MarkRead(context.Background(), identity.Actor{PrincipalID: "prn_grace", Kind: identity.ActorUser}, "org_a", n.ID())
	assert.ErrorIs(t, err, notifications.ErrNotFound, "another person's notification reads as not found")
	assert.False(t, n.Read())

	ada := identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser}
	_, err = svc.MarkRead(context.Background(), ada, "org_a", n.ID())
	require.NoError(t, err)
	_, err = svc.MarkRead(context.Background(), ada, "org_a", n.ID())
	require.NoError(t, err)
	assert.Equal(t, 1, repo.updated, "marking it read again changes nothing")
}
