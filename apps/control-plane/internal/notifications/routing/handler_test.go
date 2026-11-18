package routing

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

const (
	org   = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	ada   = "prn_ada"   // the person the requesting Session acts for
	grace = "prn_grace" // in the owning Team, and could decide
	bob   = "prn_bob"   // in the owning Team, but could not decide
	zoe   = "prn_zoe"   // an Organization admin who could decide
	agent = "prn_agent"
	squad = "tem_squad"
)

// world is what IAM, team, and policy would say, and the notifications
// stored.
type world struct {
	request *policy.ApprovalRequest
	teams   map[string][]string
	could   []string
	stored  []*notifications.Notification
}

func (w *world) LookupApprovalRequest(context.Context, string, string) (*policy.ApprovalRequest, error) {
	return w.request, nil
}

func (w *world) Deciders(_ context.Context, r *policy.ApprovalRequest, users []string) ([]string, error) {
	var out []string
	for _, u := range users {
		if slices.Contains(w.could, u) && !r.RequestedBy(u) {
			out = append(out, u)
		}
	}
	return out, nil
}

func (w *world) ListTeamUsers(_ context.Context, _, teamID string) ([]string, error) {
	return w.teams[teamID], nil
}

func (w *world) ActiveUsers(_ context.Context, _ string, ids []string) ([]iam.Contact, error) {
	var out []iam.Contact
	for _, id := range ids {
		if strings.HasPrefix(id, "prn_") && id != agent {
			out = append(out, iam.Contact{PrincipalID: id, DisplayName: strings.TrimPrefix(id, "prn_")})
		}
	}
	return out, nil
}

func (w *world) Administrators(context.Context, string) ([]iam.Contact, error) {
	return []iam.Contact{{PrincipalID: zoe, DisplayName: "zoe"}}, nil
}

func (w *world) WithinTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

// The repository: a notification is unique per event and recipient.
func (w *world) CreateNotifications(_ context.Context, ns []*notifications.Notification) error {
	for _, n := range ns {
		s := n.Snapshot()
		if !slices.ContainsFunc(w.stored, func(o *notifications.Notification) bool {
			return o.Snapshot().EventID == s.EventID && o.RecipientPrincipalID() == s.RecipientPrincipalID
		}) {
			w.stored = append(w.stored, n)
		}
	}
	return nil
}

func (w *world) ResolveForSubject(_ context.Context, org string, kind notifications.Kind, subjectType, subjectID string, at time.Time) error {
	for i, n := range w.stored {
		s := n.Snapshot()
		if s.Kind == kind && s.SubjectID == subjectID && s.ResolvedAt == nil {
			s.ResolvedAt = &at
			w.stored[i] = must(notifications.NewNotification(notifications.NewNotificationProps{
				OrganizationID: s.OrganizationID, RecipientPrincipalID: s.RecipientPrincipalID, Kind: s.Kind,
				SubjectType: s.SubjectType, SubjectID: s.SubjectID, EventID: s.EventID, Title: s.Title, Body: s.Body,
				Link: s.Link, OwnerTeamID: s.OwnerTeamID, ResolvedAt: s.ResolvedAt, CreatedAt: &s.CreatedAt,
			}, &s.ID))
		}
	}
	return nil
}

func (w *world) GetNotification(context.Context, string, string) (*notifications.Notification, error) {
	panic("unused")
}
func (w *world) UpdateNotification(context.Context, *notifications.Notification) error {
	panic("unused")
}
func (w *world) ListNotifications(context.Context, notifications.ListNotificationsQuery) (*pagination.Result[*notifications.Notification], error) {
	panic("unused")
}
func (w *world) CountUnread(context.Context, string, string) (int, error) { panic("unused") }
func (w *world) MarkAllRead(context.Context, string, string, time.Time) error {
	panic("unused")
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// newWorld has an agent Session, acting for ada, ask for a Deployment owned
// by squad.
func newWorld(t *testing.T) (*world, *events.Router) {
	t.Helper()
	request, err := policy.NewApprovalRequest(policy.NewApprovalRequestProps{
		OrganizationID: org, Subject: policy.Subject{Type: "delivery.deployment", ID: "dep_1"},
		Title:    "Deploy api 1.4.0 to Production",
		Decision: authorization.Decision{ID: authorization.NewDecisionID(), Rule: "builtin.agent_session_deployment", Reason: "an agent asked"},
		Action:   "deployment.create", Permission: "deployment.create", ResourceID: "env_prod",
		ProposalDigest: strings.Repeat("0", 64), Summary: json.RawMessage(`{}`), ReadResourceID: "prj_web",
		OwnerTeamID: new(squad), RequestedByPrincipalID: agent, SessionID: new("ses_1"), OnBehalfOfPrincipalID: new(ada),
		Requirements: []policy.Requirement{{Permission: "deployment.create", ResourceID: "env_prod", ResourcePath: []string{"env_prod", org}}},
	}, nil)
	require.NoError(t, err)
	w := &world{request: request, teams: map[string][]string{squad: {ada, grace, bob}}, could: []string{ada, grace, zoe}}
	logger := logging.NewMockLogger()
	logger.On("Warn", mock.Anything, mock.Anything, mock.Anything).Maybe()
	h, err := NewHandler(WithNotificationRepo(w), WithApprovals(w), WithTeamMembers(w), WithContacts(w),
		WithTransactor(w), WithLogger(logger))
	require.NoError(t, err)
	var router events.Router
	h.Register(&router)
	return w, &router
}

func dispatch(t *testing.T, router *events.Router, ev aggregate.Event) events.Envelope {
	t.Helper()
	envs, err := events.Wrap(context.Background(), events.Source{OrganizationID: org, AggregateType: policy.AggregateApprovalRequest, AggregateID: "apr_1"}, ev)
	require.NoError(t, err)
	require.NoError(t, router.Dispatch(context.Background(), envs[0]))
	return envs[0]
}

func recipients(ns []*notifications.Notification, kind notifications.Kind) []string {
	var out []string
	for _, n := range ns {
		if n.Snapshot().Kind == kind {
			out = append(out, n.RecipientPrincipalID())
		}
	}
	return out
}

func TestTheOwningTeamsDecidersAreTold(t *testing.T) {
	w, router := newWorld(t)
	env := dispatch(t, router, policy.ApprovalRequestCreated{ApprovalRequest: w.request.Snapshot()})
	assert.Equal(t, []string{grace}, recipients(w.stored, notifications.KindApprovalRequested),
		"bob could not decide, and ada's Session asked")
	s := w.stored[0].Snapshot()
	assert.Equal(t, "Deploy api 1.4.0 to Production", s.Title)
	assert.Equal(t, "/approvals/"+w.request.ID(), s.Link)
	assert.Equal(t, new(squad), s.OwnerTeamID)

	require.NoError(t, router.Dispatch(context.Background(), env))
	assert.Len(t, w.stored, 1, "a redelivered event adds nothing")
}

func TestTheOrganizationsAdministratorsAreToldWhenTheTeamCannotDecide(t *testing.T) {
	w, router := newWorld(t)
	w.teams[squad] = []string{ada, bob}
	dispatch(t, router, policy.ApprovalRequestCreated{ApprovalRequest: w.request.Snapshot()})
	assert.Equal(t, []string{zoe}, recipients(w.stored, notifications.KindApprovalRequested))
	assert.Nil(t, w.stored[0].Snapshot().OwnerTeamID, "it was not routed through the Team")
}

func TestADecisionResolvesTheAskAndTellsTheAsker(t *testing.T) {
	w, router := newWorld(t)
	dispatch(t, router, policy.ApprovalRequestCreated{ApprovalRequest: w.request.Snapshot()})
	require.NoError(t, w.request.Approve(zoe, authorization.NewDecisionID(), "ship it"))
	dispatch(t, router, policy.ApprovalRequestApproved{ApprovalRequest: w.request.Snapshot()})

	assert.NotNil(t, w.stored[0].Snapshot().ResolvedAt, "grace is no longer asked")
	assert.Equal(t, []string{ada}, recipients(w.stored, notifications.KindApprovalDecided), "the person the Session acts for")
	assert.Equal(t, "Approved by zoe: ship it", w.stored[1].Snapshot().Body)
}

func TestAWaitingRequestThatClosedIsNotAnnounced(t *testing.T) {
	w, router := newWorld(t)
	require.NoError(t, w.request.Cancel(ada, authorization.NewDecisionID(), "withdrawn"))
	dispatch(t, router, policy.ApprovalRequestCreated{ApprovalRequest: w.request.Snapshot()})
	assert.Empty(t, w.stored)
}
