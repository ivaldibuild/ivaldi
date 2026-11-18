// Package routing turns domain events into notifications: it decides who is
// told about what, and records what they are told (docs/design/notifications.md).
package routing

import (
	"context"
	"fmt"

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
			SubjectID: opened.ID, EventID: e.ID, Title: notifications.Shorten(opened.Title, notifications.MaxTitleLength),
			Body:        notifications.Shorten("Waiting for your approval: "+opened.Reason, notifications.MaxBodyLength),
			Link:        approvalLink(opened.ID),
			OwnerTeamID: routedTo,
		}, nil)
		if err != nil {
			return err
		}
		ns = append(ns, n)
	}
	return h.repo.CreateNotifications(ctx, ns)
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
	return h.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.repo.ResolveForSubject(ctx, closed.OrganizationID, notifications.KindApprovalRequested,
			notifications.SubjectApprovalRequest, closed.ID, clock.Now()); err != nil {
			return err
		}
		if closed.State == policy.ApprovalCancelled {
			return nil
		}
		asker, err := h.asker(ctx, closed)
		if err != nil || asker == "" {
			return err
		}
		n, err := notifications.NewNotification(notifications.NewNotificationProps{
			OrganizationID: closed.OrganizationID, RecipientPrincipalID: asker,
			Kind: notifications.KindApprovalDecided, SubjectType: notifications.SubjectApprovalRequest,
			SubjectID: closed.ID, EventID: e.ID, Title: notifications.Shorten(closed.Title, notifications.MaxTitleLength),
			Body: notifications.Shorten(h.outcome(ctx, closed), notifications.MaxBodyLength),
			Link: approvalLink(closed.ID),
		}, nil)
		if err != nil {
			return err
		}
		return h.repo.CreateNotifications(ctx, []*notifications.Notification{n})
	})
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
