// Package routing turns domain events into notifications: it decides who is
// told about what, and records what they are told (docs/design/notifications.md).
package routing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type Option func(*Handler)

func WithNotificationRepo(r notifications.NotificationRepository) Option {
	return func(h *Handler) { h.repo = r }
}

// WithApprovals reads approval requests and who could decide them
// (approvals.NewLookup).
func WithApprovals(a policy.ApprovalLookup) Option { return func(h *Handler) { h.approvals = a } }

// WithTeamMembers lists the users in a request's owning Team
// (team.NewMemberLookup).
func WithTeamMembers(m team.MemberLookup) Option { return func(h *Handler) { h.teams = m } }

// WithContacts finds active users and the Organization's owners and admins
// (iam's ContactReader).
func WithContacts(c iam.ContactReader) Option { return func(h *Handler) { h.contacts = c } }

func WithTransactor(tx transaction.Transactor) Option { return func(h *Handler) { h.tx = tx } }

func WithLogger(l logging.Logger) Option { return func(h *Handler) { h.logger = l } }

// WithEmail emails each notification through mailer too, with its link made
// a full address on webBase, the address people reach the web app at.
// Without it, notifications go to the inbox only. Sending one event's emails
// stops at budget, leaving the rest for the event's next delivery, so the
// handler finishes before the consumer would give the event to another
// handler while this one still sends.
func WithEmail(mailer notifications.Mailer, webBase string, budget time.Duration) Option {
	return func(h *Handler) {
		h.mailer, h.webBase, h.emailBudget = mailer, strings.TrimRight(webBase, "/"), budget
	}
}

// Handler records the notifications domain events call for. Each handler
// can run again for the same event: a notification is unique per event and
// recipient.
type Handler struct {
	repo      notifications.NotificationRepository
	approvals policy.ApprovalLookup
	teams     team.MemberLookup
	contacts  iam.ContactReader
	tx        transaction.Transactor
	logger    logging.Logger
	mailer    notifications.Mailer
	webBase   string
	// emailBudget bounds the time one event's emails may take.
	emailBudget time.Duration
}

// NewHandler returns a Handler, or an error naming a missing dependency.
func NewHandler(opts ...Option) (*Handler, error) {
	h := &Handler{}
	for _, opt := range opts {
		opt(h)
	}
	for _, d := range []struct {
		name string
		ok   bool
	}{
		{"NotificationRepo", h.repo != nil},
		{"Approvals", h.approvals != nil},
		{"TeamMembers", h.teams != nil},
		{"Contacts", h.contacts != nil},
		{"Transactor", h.tx != nil},
		{"Logger", h.logger != nil},
		{"the web app's address for email links", h.mailer == nil || h.webBase != ""},
		{"a time budget for email", h.mailer == nil || h.emailBudget > 0},
	} {
		if !d.ok {
			return nil, fmt.Errorf("notification routing: %s is required", d.name)
		}
	}
	return h, nil
}

// Register subscribes the handler to the events it notifies people about.
func (h *Handler) Register(r *events.Router) {
	events.On(r, h.approvalRequested)
	events.On(r, func(ctx context.Context, e events.Envelope, ev policy.ApprovalRequestApproved) error {
		return h.approvalClosed(ctx, e, ev.ApprovalRequest)
	})
	events.On(r, func(ctx context.Context, e events.Envelope, ev policy.ApprovalRequestRejected) error {
		return h.approvalClosed(ctx, e, ev.ApprovalRequest)
	})
	events.On(r, func(ctx context.Context, e events.Envelope, ev policy.ApprovalRequestCancelled) error {
		return h.approvalClosed(ctx, e, ev.ApprovalRequest)
	})
}

// approvalRequested tells the people who could decide a new request that it
// waits for them.
func (h *Handler) approvalRequested(ctx context.Context, e events.Envelope, ev policy.ApprovalRequestCreated) error {
	opened := ev.ApprovalRequest
	r, err := h.approvals.LookupApprovalRequest(ctx, opened.OrganizationID, opened.ID)
	if err != nil {
		return fmt.Errorf("read approval request %s: %w", opened.ID, err)
	}
	if !r.Pending() {
		// It was decided or cancelled before this event was handled.
		return nil
	}
	recipients, routedTo, err := h.deciders(ctx, r)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		h.logger.Warn("nobody could decide the approval request, so nobody was told",
			logging.F("organization_id", opened.OrganizationID), logging.F("approval_request_id", opened.ID))
		return nil
	}
	ns := make([]*notifications.Notification, 0, len(recipients))
	for _, recipient := range recipients {
		n, err := notifications.NewNotification(notifications.NewNotificationProps{
			OrganizationID: opened.OrganizationID, RecipientPrincipalID: recipient,
			Kind: notifications.KindApprovalRequested, SubjectType: notifications.SubjectApprovalRequest,
			// The stored request's title: an event from before requests had
			// titles carries none.
			SubjectID: opened.ID, EventID: e.ID, Title: notifications.Shorten(r.Title(), notifications.MaxTitleLength),
			Body:        notifications.Shorten("Waiting for your approval: "+opened.Reason, notifications.MaxBodyLength),
			Link:        approvalLink(opened.ID),
			OwnerTeamID: routedTo,
			EmailStatus: h.emailStatus(),
		}, nil)
		if err != nil {
			return err
		}
		ns = append(ns, n)
	}
	if err := h.repo.CreateNotifications(ctx, ns); err != nil {
		return err
	}
	// The request may have closed while these were made, and its closed
	// event, handled concurrently, may have resolved before they existed.
	// Reading it again settles that: either this read sees it closed, or it
	// closed after the insert, and that event's handler resolves them.
	r, err = h.approvals.LookupApprovalRequest(ctx, opened.OrganizationID, opened.ID)
	if err != nil {
		return fmt.Errorf("read approval request %s: %w", opened.ID, err)
	}
	if !r.Pending() {
		return h.repo.ResolveForSubject(ctx, opened.OrganizationID, notifications.KindApprovalRequested,
			notifications.SubjectApprovalRequest, opened.ID, clock.Now())
	}
	return h.sendEmails(ctx, opened.OrganizationID, e.ID)
}

// deciders returns who to tell about a request, and the Team they were found
// through: the owning Team's users who could decide it, or else the
// Organization's owners and admins who could.
func (h *Handler) deciders(ctx context.Context, r *policy.ApprovalRequest) ([]string, *string, error) {
	org := r.OrganizationID()
	if teamID := r.Snapshot().OwnerTeamID; teamID != nil {
		members, err := h.teams.ListTeamUsers(ctx, org, *teamID)
		if err != nil {
			return nil, nil, fmt.Errorf("list team %s: %w", *teamID, err)
		}
		active, err := h.contacts.ActiveUsers(ctx, org, members)
		if err != nil {
			return nil, nil, fmt.Errorf("read team %s's users: %w", *teamID, err)
		}
		deciders, err := h.approvals.Deciders(ctx, r, principalIDs(active))
		if err != nil || len(deciders) > 0 {
			return deciders, teamID, err
		}
	}
	admins, err := h.contacts.Administrators(ctx, org)
	if err != nil {
		return nil, nil, fmt.Errorf("read the organization's administrators: %w", err)
	}
	deciders, err := h.approvals.Deciders(ctx, r, principalIDs(admins))
	return deciders, nil, err
}

// approvalClosed resolves the notifications that asked people to decide a
// request, and tells the person who asked how it was decided. A
// cancellation tells no one: whoever caused it already knows.
func (h *Handler) approvalClosed(ctx context.Context, e events.Envelope, closed policy.ApprovalRequestSnapshot) error {
	// The outcome is worked out before the transaction, so a failed read,
	// such as naming the decider, cannot abort the writes.
	var decided *notifications.Notification
	if closed.State != policy.ApprovalCancelled {
		var err error
		if decided, err = h.decided(ctx, e, closed); err != nil {
			return err
		}
	}
	err := h.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.repo.ResolveForSubject(ctx, closed.OrganizationID, notifications.KindApprovalRequested,
			notifications.SubjectApprovalRequest, closed.ID, clock.Now()); err != nil {
			return err
		}
		if decided == nil {
			return nil
		}
		return h.repo.CreateNotifications(ctx, []*notifications.Notification{decided})
	})
	if err != nil {
		return err
	}
	return h.sendEmails(ctx, closed.OrganizationID, e.ID)
}

// decided is the notification telling the person who asked how their request
// was decided, or nil when they can no longer be told.
func (h *Handler) decided(ctx context.Context, e events.Envelope, closed policy.ApprovalRequestSnapshot) (*notifications.Notification, error) {
	asker, err := h.asker(ctx, closed)
	if err != nil || asker == "" {
		return nil, err
	}
	title := closed.Title
	if title == "" {
		// An event from before requests had titles carries none; the stored
		// request has one.
		r, err := h.approvals.LookupApprovalRequest(ctx, closed.OrganizationID, closed.ID)
		if err != nil {
			return nil, fmt.Errorf("read approval request %s: %w", closed.ID, err)
		}
		title = r.Title()
	}
	return notifications.NewNotification(notifications.NewNotificationProps{
		OrganizationID: closed.OrganizationID, RecipientPrincipalID: asker,
		Kind: notifications.KindApprovalDecided, SubjectType: notifications.SubjectApprovalRequest,
		SubjectID: closed.ID, EventID: e.ID, Title: notifications.Shorten(title, notifications.MaxTitleLength),
		Body:        notifications.Shorten(h.outcome(ctx, closed), notifications.MaxBodyLength),
		Link:        approvalLink(closed.ID),
		EmailStatus: h.emailStatus(),
	}, nil)
}

// emailStatus is what a new notification owes: an email when email is on.
func (h *Handler) emailStatus() notifications.EmailStatus {
	if h.mailer == nil {
		return notifications.EmailNotSent
	}
	return notifications.EmailPending
}

// sendEmails sends the emails the event's notifications still owe, after
// they are recorded, and records each one sent. If one fails, it stops and
// returns an error, so the consumer delivers the event again and only the
// unsent are retried. A recipient with no address, or no longer active, gets
// none.
func (h *Handler) sendEmails(ctx context.Context, organizationID, eventID string) error {
	if h.mailer == nil {
		return nil
	}
	owed, err := h.repo.ListPendingEmails(ctx, organizationID, eventID)
	if err != nil || len(owed) == 0 {
		return err
	}
	recipients := make([]string, len(owed))
	for i, n := range owed {
		recipients[i] = n.RecipientPrincipalID()
	}
	contacts, err := h.contacts.ActiveUsers(ctx, organizationID, recipients)
	if err != nil {
		return fmt.Errorf("read the recipients' addresses: %w", err)
	}
	addresses := make(map[string]string, len(contacts))
	for _, c := range contacts {
		addresses[c.PrincipalID] = c.Email
	}
	// Those with no address are settled first, so a failing server leaves
	// only real emails owed.
	var sendable []*notifications.Notification
	for _, n := range owed {
		if addresses[n.RecipientPrincipalID()] != "" {
			sendable = append(sendable, n)
			continue
		}
		if err := n.EmailCannotBeSent(); err != nil {
			return err
		}
		if err := h.repo.SaveEmail(ctx, n); err != nil {
			return err
		}
	}
	// The first failure stops the rest: the server is most likely down, and
	// waiting out a timeout per recipient could keep the event past the
	// consumer's claim timeout, when it would be handled twice at once.
	deadline := time.Now().Add(h.emailBudget)
	for i, n := range sendable {
		if !time.Now().Before(deadline) {
			return fmt.Errorf("%d emails are left for the event's next delivery: sending ran out of time", len(sendable)-i)
		}
		sendCtx, cancel := context.WithDeadline(ctx, deadline)
		err := h.mailer.Send(sendCtx, h.message(n, addresses[n.RecipientPrincipalID()]))
		cancel()
		if err != nil {
			h.logger.Warn("could not email a notification", logging.F("notification_id", n.ID()), logging.F("error", err.Error()))
			return fmt.Errorf("email notification %s: %w", n.ID(), err)
		}
		h.logger.Info("emailed a notification", logging.F("notification_id", n.ID()))
		if err := n.EmailWasSent(); err != nil {
			return err
		}
		if err := h.repo.SaveEmail(ctx, n); err != nil {
			return err
		}
	}
	return nil
}

// message is a notification as an email: its title, with what it asks for,
// as the subject, and its body with the full address of its link.
func (h *Handler) message(n *notifications.Notification, address string) notifications.Message {
	subject := "Approval needed: " + n.Title()
	if n.Kind() == notifications.KindApprovalDecided {
		subject = "Approval decided: " + n.Title()
	}
	return notifications.Message{
		To:      address,
		Subject: subject,
		Body:    n.Body() + "\n\n" + h.webBase + n.Link(),
	}
}

// asker is the person who asked for a request: the one its Session acts
// for, or the user who asked directly. A service account is no one to tell,
// so it returns "".
func (h *Handler) asker(ctx context.Context, closed policy.ApprovalRequestSnapshot) (string, error) {
	person := closed.RequestedByPrincipalID
	if closed.OnBehalfOfPrincipalID != nil {
		person = *closed.OnBehalfOfPrincipalID
	}
	users, err := h.contacts.ActiveUsers(ctx, closed.OrganizationID, []string{person})
	if err != nil || len(users) == 0 {
		return "", err
	}
	return users[0].PrincipalID, nil
}

// outcome says how a request was decided, by whom, and why, if they said.
func (h *Handler) outcome(ctx context.Context, closed policy.ApprovalRequestSnapshot) string {
	verb := "Approved"
	if closed.State == policy.ApprovalRejected {
		verb = "Rejected"
	}
	who := "someone"
	if closed.ClosedByPrincipalID != nil {
		// A name is a nicety: without it, the outcome still says what
		// happened.
		users, err := h.contacts.ActiveUsers(ctx, closed.OrganizationID, []string{*closed.ClosedByPrincipalID})
		if err != nil {
			h.logger.Warn("could not name who decided an approval request", logging.F("error", err.Error()))
		}
		if len(users) == 1 {
			who = users[0].DisplayName
		}
	}
	text := verb + " by " + who
	if closed.Note != nil && *closed.Note != "" {
		text += ": " + *closed.Note
	}
	return text
}

func approvalLink(requestID string) string { return "/approvals/" + requestID }

func principalIDs(contacts []iam.Contact) []string {
	ids := make([]string, len(contacts))
	for i, c := range contacts {
		ids[i] = c.PrincipalID
	}
	return ids
}
