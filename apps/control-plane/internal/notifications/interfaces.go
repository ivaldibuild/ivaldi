package notifications

// The interfaces this capability defines for itself: its repository, what
// it reads from other capabilities, and its inbox service.

import (
	"context"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// NotificationRepository persists and retrieves notifications. Every
// method is scoped to an Organization.
type NotificationRepository interface {
	// CreateNotifications inserts the notifications, skipping any whose
	// event and recipient already have one, so handling an event again
	// adds nothing.
	CreateNotifications(ctx context.Context, ns []*Notification) error
	// GetNotification returns a notification, or ErrNotFound.
	GetNotification(ctx context.Context, organizationID, notificationID string) (*Notification, error)
	// SaveRead writes when a notification was read, unless it already was.
	SaveRead(ctx context.Context, n *Notification) error
	// SaveEmail writes whether a notification's email was sent, unless it
	// no longer owes one. Each save writes only its own fields, so the
	// recipient reading a notification and its email being sent never undo
	// each other.
	SaveEmail(ctx context.Context, n *Notification) error
	// ListPendingEmails returns the event's notifications whose email is
	// still owed.
	ListPendingEmails(ctx context.Context, organizationID, eventID string) ([]*Notification, error)
	ListNotifications(ctx context.Context, q ListNotificationsQuery) (*pagination.Result[*Notification], error)
	// CountUnread counts a recipient's notifications that are neither read
	// nor resolved.
	CountUnread(ctx context.Context, organizationID, recipientPrincipalID string) (int, error)
	// MarkAllRead marks every unread notification of the recipient read at at.
	MarkAllRead(ctx context.Context, organizationID, recipientPrincipalID string, at time.Time) error
	// ResolveForSubject marks resolved, at at, the unresolved notifications
	// of the kind about the subject.
	ResolveForSubject(ctx context.Context, organizationID string, kind Kind, subjectType, subjectID string, at time.Time) error
}

// ListNotificationsQuery lists one recipient's notifications, newest first
// unless the page says otherwise.
type ListNotificationsQuery struct {
	OrganizationID       string
	RecipientPrincipalID string
	// Unread keeps only notifications not yet read.
	Unread bool
	Page   pagination.Query
}

// InboxService lets people read their own notifications. The inbox
// sub-package implements it and serves it over HTTP.
type InboxService interface {
	ListNotifications(ctx context.Context, actor identity.Actor, organizationID string, unread bool, page pagination.Query) (*pagination.Result[NotificationSnapshot], error)
	// CountUnread counts the actor's notifications that are neither read
	// nor resolved, for the web app's bell.
	CountUnread(ctx context.Context, actor identity.Actor, organizationID string) (int, error)
	// MarkRead marks one of the actor's notifications read. Another
	// person's notification is ErrNotFound.
	MarkRead(ctx context.Context, actor identity.Actor, organizationID, notificationID string) (*Notification, error)
	MarkAllRead(ctx context.Context, actor identity.Actor, organizationID string) error
}

// Message is one plain-text email to one person.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Mailer sends email. The smtpmail sub-package implements it over SMTP.
type Mailer interface {
	// Send returns once the mail server has accepted the message.
	Send(ctx context.Context, m Message) error
}
