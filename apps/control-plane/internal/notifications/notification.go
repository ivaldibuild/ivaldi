package notifications

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

// Kind is what a notification tells its recipient.
type Kind string

const (
	// KindApprovalRequested asks someone who could decide an approval
	// request to decide it.
	KindApprovalRequested Kind = "approval_requested"
	// KindApprovalDecided tells the person who asked how it was decided.
	KindApprovalDecided Kind = "approval_decided"
)

func (k Kind) Valid() bool { return k == KindApprovalRequested || k == KindApprovalDecided }

// EmailStatus is where a notification's email stands.
type EmailStatus string

const (
	// EmailPending means an email is owed.
	EmailPending EmailStatus = "pending"
	// EmailSent means the SMTP server accepted it.
	EmailSent EmailStatus = "sent"
	// EmailNotSent means none was sent: email is off, or the recipient has
	// no address.
	EmailNotSent EmailStatus = "not_sent"
)

func (s EmailStatus) Valid() bool { return s == EmailPending || s == EmailSent || s == EmailNotSent }

// Notification is one message to one person about something, which stays
// the source of truth: the notification only points at it. It is unique per
// event and recipient. Its recipient reads it, and it is resolved when what
// it asked for no longer waits.
type Notification struct {
	aggregate.Root
	organizationID       string
	recipientPrincipalID string
	kind                 Kind
	subjectType          string
	subjectID            string
	eventID              string
	title                string
	body                 string
	link                 string
	ownerTeamID          *string
	readAt               *time.Time
	resolvedAt           *time.Time
	emailStatus          EmailStatus
	emailedAt            *time.Time
	createdAt            time.Time
}

// NotificationSnapshot is a Notification's state as plain values, for the
// repository and the inbox API.
type NotificationSnapshot struct {
	ID                   string `json:"id"`
	OrganizationID       string `json:"organization_id"`
	RecipientPrincipalID string `json:"recipient_principal_id"`
	Kind                 Kind   `json:"kind" enum:"approval_requested,approval_decided"`
	SubjectType          string `json:"subject_type"`
	SubjectID            string `json:"subject_id"`
	// EventID is the event that caused it, which only deduplicates.
	EventID string `json:"-"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	// Link is the path in the web app it opens.
	Link        string     `json:"link"`
	OwnerTeamID *string    `json:"owner_team_id"`
	ReadAt      *time.Time `json:"read_at"`
	ResolvedAt  *time.Time `json:"resolved_at"`
	// EmailStatus and EmailedAt say whether its email went out; they are
	// the sender's bookkeeping, not the recipient's.
	EmailStatus EmailStatus `json:"-"`
	EmailedAt   *time.Time  `json:"-"`
	CreatedAt   time.Time   `json:"created_at"`
}

// NewNotificationProps describes a Notification. A new one leaves ReadAt,
// ResolvedAt, and CreatedAt nil; a repository loading a stored one sets them.
type NewNotificationProps struct {
	OrganizationID       string
	RecipientPrincipalID string
	Kind                 Kind
	SubjectType          string
	SubjectID            string
	EventID              string
	Title                string
	Body                 string
	Link                 string
	OwnerTeamID          *string
	ReadAt               *time.Time
	ResolvedAt           *time.Time
	// EmailStatus is EmailPending when an email is owed, and defaults to
	// EmailNotSent.
	EmailStatus EmailStatus
	EmailedAt   *time.Time
	CreatedAt   *time.Time
}

// NewNotification returns a Notification, with a new ID unless
// notificationID gives a stored one.
func NewNotification(p NewNotificationProps, notificationID *string) (*Notification, error) {
	root, err := newRoot(Notification{}.Prefix(), notificationID)
	if err != nil {
		return nil, err
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	created := clock.Now()
	if p.CreatedAt != nil {
		created = *p.CreatedAt
	}
	if p.EmailStatus == "" {
		p.EmailStatus = EmailNotSent
	}
	return &Notification{
		Root:                 root,
		organizationID:       p.OrganizationID,
		recipientPrincipalID: p.RecipientPrincipalID,
		kind:                 p.Kind,
		subjectType:          p.SubjectType,
		subjectID:            p.SubjectID,
		eventID:              p.EventID,
		title:                p.Title,
		body:                 p.Body,
		link:                 p.Link,
		ownerTeamID:          optional.OfPtr(p.OwnerTeamID),
		readAt:               clock.CopyTime(p.ReadAt),
		resolvedAt:           clock.CopyTime(p.ResolvedAt),
		emailStatus:          p.EmailStatus,
		emailedAt:            clock.CopyTime(p.EmailedAt),
		createdAt:            created,
	}, nil
}

func (p NewNotificationProps) validate() error {
	switch {
	case p.OrganizationID == "" || p.RecipientPrincipalID == "":
		return NewErrInvalidInput("a notification needs an organization and a recipient")
	case !p.Kind.Valid():
		return errorf("notification kind %q must be approval_requested or approval_decided", p.Kind)
	case p.SubjectType == "" || p.SubjectID == "" || p.EventID == "":
		return NewErrInvalidInput("a notification needs its subject and the event that caused it")
	case p.Title == "" || utf8.RuneCountInString(p.Title) > MaxTitleLength:
		return errorf("a notification's title must be 1-%d characters", MaxTitleLength)
	case utf8.RuneCountInString(p.Body) > MaxBodyLength:
		return errorf("a notification's body must be at most %d characters", MaxBodyLength)
	case p.EmailStatus != "" && !p.EmailStatus.Valid():
		return errorf("email status %q must be pending, sent, or not_sent", p.EmailStatus)
	case (p.EmailStatus == EmailSent) != (p.EmailedAt != nil):
		return NewErrInvalidInput("a notification's email has a sent time exactly when it was sent")
	case !strings.HasPrefix(p.Link, "/") || utf8.RuneCountInString(p.Link) > maxLinkLength:
		return errorf("a notification's link must be a path of at most %d characters", maxLinkLength)
	}
	return nil
}

// Prefix is the type prefix of Notification IDs.
func (Notification) Prefix() string { return "ntf" }

func (n *Notification) OrganizationID() string       { return n.organizationID }
func (n *Notification) RecipientPrincipalID() string { return n.recipientPrincipalID }
func (n *Notification) Read() bool                   { return n.readAt != nil }
func (n *Notification) Kind() Kind                   { return n.kind }
func (n *Notification) Title() string                { return n.title }
func (n *Notification) Body() string                 { return n.body }
func (n *Notification) Link() string                 { return n.link }
func (n *Notification) EmailStatus() EmailStatus     { return n.emailStatus }

// MarkRead records that the recipient read it, and reports whether that
// changed anything.
func (n *Notification) MarkRead() bool {
	if n.Read() {
		return false
	}
	now := clock.Now()
	n.readAt = &now
	return true
}

// EmailWasSent records that the SMTP server accepted its email.
func (n *Notification) EmailWasSent() error {
	if n.emailStatus != EmailPending {
		return errorf("notification %s owes no email: it is %s", n.ID(), n.emailStatus)
	}
	now := clock.Now()
	n.emailStatus, n.emailedAt = EmailSent, &now
	return nil
}

// EmailCannotBeSent records that its email will not be sent, because the
// recipient has no address or is no longer active.
func (n *Notification) EmailCannotBeSent() error {
	if n.emailStatus != EmailPending {
		return errorf("notification %s owes no email: it is %s", n.ID(), n.emailStatus)
	}
	n.emailStatus = EmailNotSent
	return nil
}

// Snapshot returns the notification's state.
func (n *Notification) Snapshot() NotificationSnapshot {
	return NotificationSnapshot{
		ID:                   n.ID(),
		OrganizationID:       n.organizationID,
		RecipientPrincipalID: n.recipientPrincipalID,
		Kind:                 n.kind,
		SubjectType:          n.subjectType,
		SubjectID:            n.subjectID,
		EventID:              n.eventID,
		Title:                n.title,
		Body:                 n.body,
		Link:                 n.link,
		OwnerTeamID:          optional.OfPtr(n.ownerTeamID),
		ReadAt:               clock.CopyTime(n.readAt),
		ResolvedAt:           clock.CopyTime(n.resolvedAt),
		EmailStatus:          n.emailStatus,
		EmailedAt:            clock.CopyTime(n.emailedAt),
		CreatedAt:            n.createdAt,
	}
}

// Shorten cuts s to at most max characters, ending it with an ellipsis when
// it is cut, so text copied from elsewhere always fits.
func Shorten(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
