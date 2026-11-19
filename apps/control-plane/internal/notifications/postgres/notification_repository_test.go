package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications/migrations"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
)

const (
	org   = "org_a"
	ada   = "prn_ada"
	grace = "prn_grace"
)

// t0 has microseconds, which PostgreSQL keeps, so round trips check precision.
var t0 = time.Date(2026, 11, 18, 9, 30, 15, 123456000, time.UTC)

func newNotification(t *testing.T, recipient, eventID, requestID string) *notifications.Notification {
	t.Helper()
	n, err := notifications.NewNotification(notifications.NewNotificationProps{
		OrganizationID: org, RecipientPrincipalID: recipient, Kind: notifications.KindApprovalRequested,
		SubjectType: notifications.SubjectApprovalRequest, SubjectID: requestID, EventID: eventID,
		Title: "Deploy api 1.4.0 to Production", Body: "Waiting for your approval", Link: "/approvals/" + requestID,
		EmailStatus: notifications.EmailPending,
	}, nil)
	require.NoError(t, err)
	return n
}

func TestNotificationRepository(t *testing.T) {
	db := databasetest.New(t, migrations.FS)
	clock.Fix(t, t0)
	ctx := context.Background()
	repo := NewNotificationRepository(db)

	first := newNotification(t, ada, "evt_1", "apr_1")
	require.NoError(t, repo.CreateNotifications(ctx, []*notifications.Notification{first, newNotification(t, grace, "evt_1", "apr_1")}))
	require.NoError(t, repo.CreateNotifications(ctx, []*notifications.Notification{newNotification(t, ada, "evt_1", "apr_1")}),
		"a redelivered event adds nothing")
	clock.Fix(t, t0.Add(time.Minute))
	require.NoError(t, repo.CreateNotifications(ctx, []*notifications.Notification{newNotification(t, ada, "evt_2", "apr_2")}))

	got, err := repo.GetNotification(ctx, org, first.ID())
	require.NoError(t, err)
	assert.Equal(t, first.Snapshot(), got.Snapshot(), "a notification survives a round trip")

	list := func(unread bool) int {
		t.Helper()
		page, err := repo.ListNotifications(ctx, notifications.ListNotificationsQuery{
			OrganizationID: org, RecipientPrincipalID: ada, Unread: unread,
		})
		require.NoError(t, err)
		return len(page.Items)
	}
	assert.Equal(t, 2, list(false), "ada's own notifications, one per event")
	unread := func() int {
		t.Helper()
		n, err := repo.CountUnread(ctx, org, ada)
		require.NoError(t, err)
		return n
	}
	assert.Equal(t, 2, unread())

	require.NoError(t, repo.ResolveForSubject(ctx, org, notifications.KindApprovalRequested, notifications.SubjectApprovalRequest, "apr_1", t0))
	assert.Equal(t, 1, unread(), "a resolved notification no longer counts")

	// Loaded before they are read, as the email sender would have them.
	owed, err := repo.ListPendingEmails(ctx, org, "evt_1")
	require.NoError(t, err)
	require.Len(t, owed, 2, "both of evt_1's notifications still owe an email")

	got.MarkRead()
	require.NoError(t, repo.SaveRead(ctx, got))
	assert.Equal(t, 1, list(true))
	require.NoError(t, repo.MarkAllRead(ctx, org, ada, t0))
	assert.Equal(t, 0, list(true))
	assert.Equal(t, 0, unread())

	require.NoError(t, owed[0].EmailWasSent())
	require.NoError(t, repo.SaveEmail(ctx, owed[0]))
	assert.Equal(t, 0, list(true), "recording the email does not undo the read")
	owed, err = repo.ListPendingEmails(ctx, org, "evt_1")
	require.NoError(t, err)
	assert.Len(t, owed, 1, "a sent email is no longer owed")
}
