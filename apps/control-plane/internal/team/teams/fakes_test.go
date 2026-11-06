package teams

import (
	"context"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

var org = id.New(iam.Organization{}.Prefix())

// Users in the test Organization.
var (
	alice     = id.New("prn") // active
	bob       = id.New("prn") // active
	suspended = id.New("prn")
	stranger  = id.New("prn") // not a user in the Organization
)

// state is everything the fakes store. A transaction restores it when its
// function fails, as a rolled-back database transaction would.
type state struct {
	teams  map[string]team.TeamSnapshot
	audit  []audit.Event
	events []events.Envelope
}

func (s state) clone() state {
	return state{
		teams:  maps.Clone(s.teams),
		audit:  append([]audit.Event(nil), s.audit...),
		events: append([]events.Envelope(nil), s.events...),
	}
}

// store fakes the repository, the transactor, and the IAM user lookup. Its
// auditLog and outboxLog fake the recorders.
type store struct {
	state
	users map[string]*iam.User
}

func newStore() *store {
	return &store{state: state{teams: map[string]team.TeamSnapshot{}}, users: map[string]*iam.User{}}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func (s *store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	saved := s.state.clone()
	if err := fn(ctx); err != nil {
		s.state = saved
		return err
	}
	return nil
}

// Teams.

func rebuildTeam(s team.TeamSnapshot) *team.Team {
	members := make([]team.NewTeamMemberProps, len(s.Members))
	for i, m := range s.Members {
		members[i] = team.NewTeamMemberProps{PrincipalID: m.PrincipalID, Role: m.Role, CreatedAt: &m.CreatedAt, UpdatedAt: &m.UpdatedAt}
	}
	return must(team.NewTeam(team.NewTeamProps{
		OrganizationID: s.OrganizationID, Slug: s.Slug, DisplayName: s.DisplayName, Description: s.Description,
		Members: members, CreatedAt: &s.CreatedAt, UpdatedAt: &s.UpdatedAt, ArchivedAt: s.ArchivedAt,
	}, &s.ID))
}

func (s *store) slugTaken(snap team.TeamSnapshot) bool {
	for _, o := range s.teams {
		if o.ID != snap.ID && o.OrganizationID == snap.OrganizationID && o.Slug == snap.Slug && o.ArchivedAt == nil && snap.ArchivedAt == nil {
			return true
		}
	}
	return false
}

func (s *store) CreateTeam(_ context.Context, t *team.Team) error {
	snap := t.Snapshot()
	if s.slugTaken(snap) {
		return team.ErrSlugTaken
	}
	s.teams[snap.ID] = snap
	return nil
}

func (s *store) GetTeam(_ context.Context, organizationID, teamID string) (*team.Team, error) {
	snap, ok := s.teams[teamID]
	if !ok || snap.OrganizationID != organizationID {
		return nil, team.ErrNotFound
	}
	return rebuildTeam(snap), nil
}

func (s *store) GetTeamForUpdate(ctx context.Context, organizationID, teamID string) (*team.Team, error) {
	return s.GetTeam(ctx, organizationID, teamID)
}

func (s *store) GetTeamBySlug(_ context.Context, organizationID, slug string) (*team.Team, error) {
	for _, snap := range s.teams {
		if snap.OrganizationID == organizationID && snap.Slug == slug && snap.ArchivedAt == nil {
			return rebuildTeam(snap), nil
		}
	}
	return nil, team.ErrNotFound
}

func (s *store) ListTeams(_ context.Context, q team.ListTeamsQuery) (*pagination.Result[*team.Team], error) {
	out := &pagination.Result[*team.Team]{}
	for _, snap := range s.teams {
		if snap.OrganizationID == q.OrganizationID && (q.IncludeArchived || snap.ArchivedAt == nil) {
			out.Items = append(out.Items, rebuildTeam(snap))
		}
	}
	out.TotalCount = len(out.Items)
	return out, nil
}

func (s *store) UpdateTeam(_ context.Context, t *team.Team) error {
	if _, ok := s.teams[t.ID()]; !ok {
		return team.ErrNotFound
	}
	snap := t.Snapshot()
	if s.slugTaken(snap) {
		return team.ErrSlugTaken
	}
	s.teams[t.ID()] = snap
	return nil
}

// Users.

func (s *store) Get(_ context.Context, organizationID, principalID string) (*iam.User, error) {
	u, ok := s.users[principalID]
	if !ok || u.OrganizationID() != organizationID {
		return nil, iam.ErrNotFound
	}
	return u, nil
}

func (s *store) addUser(principalID string, status iam.PrincipalStatus) {
	s.users[principalID] = must(iam.NewUser(iam.NewUserProps{
		PrincipalProps: iam.PrincipalProps{OrganizationID: org, DisplayName: "User", Status: status},
		AccountID:      id.New(iam.Account{}.Prefix()),
	}, &principalID))
}

type auditLog struct{ s *store }

func (a auditLog) Record(_ context.Context, e audit.Event) error {
	a.s.audit = append(a.s.audit, e)
	return nil
}

type outboxLog struct{ s *store }

func (o outboxLog) Record(_ context.Context, envs ...events.Envelope) error {
	o.s.events = append(o.s.events, envs...)
	return nil
}

func (s *store) eventTypes() []string {
	var out []string
	for _, e := range s.events {
		out = append(out, e.Type)
	}
	return out
}

func (s *store) auditTypes() []string {
	var out []string
	for _, e := range s.audit {
		out = append(out, e.Type)
	}
	return out
}

// authorizer allows everything except what deny refuses, and remembers
// what it was asked. Each decision's ID is "dec-" and the request's action.
type authorizer struct {
	asked []authorization.Request
	deny  func(authorization.Request) bool
}

func (a *authorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.asked = append(a.asked, req)
	d := authorization.Decision{ID: "dec-" + req.Action, Outcome: authorization.OutcomeAllow}
	if a.deny != nil && a.deny(req) {
		d.Outcome = authorization.OutcomeDeny
	}
	return d, nil
}

// world is a store with alice and bob active, a suspended user, and the
// TeamService over it.
type world struct {
	*store
	authz *authorizer
	svc   team.TeamService
}

func newWorld(t *testing.T) *world {
	t.Helper()
	s := newStore()
	s.addUser(alice, iam.PrincipalActive)
	s.addUser(bob, iam.PrincipalActive)
	s.addUser(suspended, iam.PrincipalSuspended)
	authz := &authorizer{}
	svc, err := NewTeamService(s.options(authz)...)
	require.NoError(t, err)
	return &world{store: s, authz: authz, svc: svc}
}

func (s *store) options(authz authorization.Authorizer) []TeamServiceOption {
	return []TeamServiceOption{
		WithTeamRepo(s), WithUsers(s), WithAuthorizer(authz), WithTransactor(s),
		WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
	}
}

// team stores a Team directly, bypassing the service, with members given
// as principal ID and role pairs.
func (w *world) team(t *testing.T, slug string, archived bool, members ...team.NewTeamMemberProps) *team.Team {
	t.Helper()
	tm := must(team.NewTeam(team.NewTeamProps{OrganizationID: org, Slug: slug, DisplayName: slug, Members: members}, nil))
	if archived {
		tm.Archive()
	}
	tm.PullEvents()
	require.NoError(t, w.CreateTeam(context.Background(), tm))
	return tm
}

func lead(principalID string) team.NewTeamMemberProps {
	return team.NewTeamMemberProps{PrincipalID: principalID, Role: team.RoleLead}
}

// reset forgets what was recorded so far.
func (w *world) reset() {
	w.audit, w.events, w.authz.asked = nil, nil, nil
}
