package agents

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/orchestration"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

const (
	org    = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	teamID = "tem_01ARZ3NDEKTSV4RRFFQ69G5FAV"
)

var (
	t0  = time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	ada = identity.Actor{PrincipalID: "prn_01ARZ3NDEKTSV4RRFFQ69G5FAD", Kind: identity.ActorUser}
	ctx = context.Background()
)

type store struct {
	agents      map[string]*iam.Agent
	configs     map[string]*orchestration.AgentConfig
	memberships map[string][]team.Membership
	audit       []audit.Event
	events      []events.Envelope
}

type agentsRepo struct{ *store }

func (r agentsRepo) Get(_ context.Context, organizationID, principalID string) (*iam.Agent, error) {
	a, ok := r.agents[principalID]
	if !ok || a.OrganizationID() != organizationID {
		return nil, iam.ErrNotFound
	}
	return a, nil
}

func (r agentsRepo) GetBySlug(_ context.Context, organizationID, slug string) (*iam.Agent, error) {
	for _, a := range r.agents {
		if a.OrganizationID() == organizationID && a.Slug() == slug {
			return a, nil
		}
	}
	return nil, iam.ErrNotFound
}

func (r agentsRepo) List(_ context.Context, query iam.ListAgentsQuery) (*pagination.Result[*iam.Agent], error) {
	var items []*iam.Agent
	for _, a := range r.agents {
		if a.OrganizationID() == query.OrganizationID {
			items = append(items, a)
		}
	}
	return pagination.NewResult(items, len(items), false, false, nil, nil), nil
}

func (r agentsRepo) Write(_ context.Context, agent *iam.Agent) error {
	r.agents[agent.ID()] = agent
	return nil
}

type configsRepo struct{ *store }

func (r configsRepo) Get(_ context.Context, organizationID, agentID string) (*orchestration.AgentConfig, error) {
	c, ok := r.configs[agentID]
	if !ok || c.OrganizationID() != organizationID {
		return nil, orchestration.ErrNotFound
	}
	return c, nil
}

func (r configsRepo) ListByAgentIDs(_ context.Context, organizationID string, agentIDs []string) (map[string]*orchestration.AgentConfig, error) {
	out := map[string]*orchestration.AgentConfig{}
	for _, id := range agentIDs {
		if c, ok := r.configs[id]; ok && c.OrganizationID() == organizationID {
			out[id] = c
		}
	}
	return out, nil
}

func (r configsRepo) Upsert(_ context.Context, config *orchestration.AgentConfig) error {
	r.configs[config.AgentID()] = config
	return nil
}

type directory struct{ *store }

func (d directory) IsActiveTeam(context.Context, string, string) (bool, error) { return true, nil }

func (d directory) ListTeamsForMembers(_ context.Context, _ string, principalIDs []string) (map[string][]team.Membership, error) {
	out := map[string][]team.Membership{}
	for _, id := range principalIDs {
		out[id] = d.memberships[id]
	}
	return out, nil
}

type teams struct{ *store }

func (t teams) CreateAgent(_ context.Context, _ identity.Actor, in team.CreateAgentInput) (*team.CreatedAgent, error) {
	slug := in.Slug
	if slug == "" {
		slug = "agent"
	}
	agent, err := iam.NewAgent(iam.NewAgentProps{
		PrincipalProps: iam.PrincipalProps{OrganizationID: in.OrganizationID, DisplayName: in.DisplayName},
		Slug:           slug,
	}, nil)
	if err != nil {
		return nil, err
	}
	role := in.Role
	if role == "" {
		role = team.RoleMember
	}
	tm, err := team.NewTeam(team.NewTeamProps{
		OrganizationID: in.OrganizationID,
		DisplayName:    "Platform",
		Members: []team.NewTeamMemberProps{{
			PrincipalID: agent.ID(), PrincipalKind: team.MemberAgent, Role: role,
		}},
	}, nil)
	if err != nil {
		return nil, err
	}
	t.agents[agent.ID()] = agent
	t.memberships[agent.ID()] = []team.Membership{{
		TeamID: tm.ID(), Slug: tm.Slug(), DisplayName: tm.DisplayName(), Role: role,
	}}
	return &team.CreatedAgent{Agent: agent, Team: tm}, nil
}

func (teams) CreateTeam(context.Context, identity.Actor, team.CreateTeamInput) (*team.Team, error) {
	panic("not used")
}
func (teams) GetTeam(context.Context, identity.Actor, string, string) (*team.Team, error) {
	panic("not used")
}
func (teams) GetTeamBySlug(context.Context, identity.Actor, string, string) (*team.Team, error) {
	panic("not used")
}
func (teams) ListTeams(context.Context, identity.Actor, team.ListTeamsQuery) (*pagination.Result[*team.Team], error) {
	panic("not used")
}
func (teams) UpdateTeam(context.Context, identity.Actor, string, string, team.UpdateTeamProps) (*team.Team, error) {
	panic("not used")
}
func (teams) ArchiveTeam(context.Context, identity.Actor, string, string) (*team.Team, error) {
	panic("not used")
}
func (teams) RestoreTeam(context.Context, identity.Actor, string, string) (*team.Team, error) {
	panic("not used")
}
func (teams) ListTeamMembers(context.Context, identity.Actor, string, string) ([]team.TeamMemberSnapshot, error) {
	panic("not used")
}
func (teams) SetTeamMember(context.Context, identity.Actor, string, string, string, team.Role) (*team.Team, error) {
	panic("not used")
}
func (teams) RemoveTeamMember(context.Context, identity.Actor, string, string, string) (*team.Team, error) {
	panic("not used")
}

func (s *store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	agents, configs, memberships := maps.Clone(s.agents), maps.Clone(s.configs), maps.Clone(s.memberships)
	a, e := len(s.audit), len(s.events)
	if err := fn(ctx); err != nil {
		s.agents, s.configs, s.memberships = agents, configs, memberships
		s.audit, s.events = s.audit[:a], s.events[:e]
		return err
	}
	return nil
}

type auditLog struct{ *store }

func (a auditLog) Record(_ context.Context, e audit.Event) error {
	a.audit = append(a.audit, e)
	return nil
}

type outboxLog struct{ *store }

func (o outboxLog) Record(_ context.Context, envs ...events.Envelope) error {
	o.events = append(o.events, envs...)
	return nil
}

type authorizer struct {
	asked []authorization.Request
	deny  func(authorization.Request) bool
}

func (a *authorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.asked = append(a.asked, req)
	d := authorization.Decision{ID: "dec-" + req.Action, Outcome: authorization.OutcomeAllow}
	if a.deny != nil && a.deny(req) {
		d.Outcome, d.Reason = authorization.OutcomeDeny, "denied by the test"
	}
	return d, nil
}

type world struct {
	*store
	svc   orchestration.AgentService
	authz *authorizer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	clock.Fix(t, t0)
	s := &store{
		agents: map[string]*iam.Agent{}, configs: map[string]*orchestration.AgentConfig{},
		memberships: map[string][]team.Membership{},
	}
	authz := &authorizer{}
	svc, err := NewService(
		WithAgentRepo(agentsRepo{s}), WithConfigRepo(configsRepo{s}),
		WithTeams(teams{s}), WithTeamDirectory(directory{s}), WithAuthorizer(authz),
		WithTransactor(s), WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
	)
	require.NoError(t, err)
	return &world{store: s, svc: svc, authz: authz}
}

func (s *store) auditTypes() []string {
	var out []string
	for _, e := range s.audit {
		out = append(out, e.Type)
	}
	return out
}

func TestCreateAgentWithSystemPrompt(t *testing.T) {
	w := newWorld(t)
	created, err := w.svc.CreateAgent(ctx, ada, orchestration.CreateAgentInput{
		OrganizationID: org, TeamID: teamID, DisplayName: "Release helper",
		SystemPrompt: "Ship carefully and cite evidence.", Role: team.RoleLead,
	})
	require.NoError(t, err)
	assert.Equal(t, "Release helper", created.DisplayName)
	assert.Equal(t, "Ship carefully and cite evidence.", created.SystemPrompt)
	require.Len(t, created.Teams, 1)
	assert.Equal(t, team.RoleLead, created.Teams[0].Role)
	assert.Contains(t, w.configs, created.ID)
	assert.Equal(t, []string{orchestration.EventAgentConfigCreated}, w.auditTypes())
	assert.Equal(t, iam.PermissionAgentManage, w.authz.asked[0].Permission)
}

func TestCreateAgentRollsBackWhenThePromptIsInvalid(t *testing.T) {
	w := newWorld(t)
	_, err := w.svc.CreateAgent(ctx, ada, orchestration.CreateAgentInput{
		OrganizationID: org, TeamID: teamID, DisplayName: "Release helper",
		SystemPrompt: string(make([]byte, 64*1024+1)),
	})
	assert.ErrorIs(t, err, orchestration.ErrInvalid)
	assert.Empty(t, w.agents, "the Agent and its Team membership roll back")
	assert.Empty(t, w.configs)
}

func TestListAndGetAgents(t *testing.T) {
	w := newWorld(t)
	first, err := w.svc.CreateAgent(ctx, ada, orchestration.CreateAgentInput{
		OrganizationID: org, TeamID: teamID, DisplayName: "Release helper", SystemPrompt: "Ship.",
	})
	require.NoError(t, err)
	_, err = w.svc.CreateAgent(ctx, ada, orchestration.CreateAgentInput{
		OrganizationID: org, TeamID: teamID, DisplayName: "Triage helper",
	})
	require.NoError(t, err)

	page, err := w.svc.ListAgents(ctx, ada, orchestration.ListAgentsQuery{OrganizationID: org})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	var found orchestration.AgentSummary
	for _, item := range page.Items {
		if item.ID == first.ID {
			found = item
		}
	}
	assert.True(t, found.HasSystemPrompt)
	require.Len(t, found.Teams, 1)

	got, err := w.svc.GetAgent(ctx, ada, org, first.ID)
	require.NoError(t, err)
	assert.Equal(t, "Ship.", got.SystemPrompt)
	assert.Equal(t, iam.PermissionAgentRead, w.authz.asked[len(w.authz.asked)-1].Permission)
}

func TestUpdateAgentAndStatus(t *testing.T) {
	w := newWorld(t)
	created, err := w.svc.CreateAgent(ctx, ada, orchestration.CreateAgentInput{
		OrganizationID: org, TeamID: teamID, DisplayName: "Release helper", SystemPrompt: "Ship.",
	})
	require.NoError(t, err)

	name, prompt := "Release bot", "Prefer the graph."
	updated, err := w.svc.UpdateAgent(ctx, ada, org, created.ID, orchestration.UpdateAgentProps{
		DisplayName: &name, SystemPrompt: &prompt,
	})
	require.NoError(t, err)
	assert.Equal(t, name, updated.DisplayName)
	assert.Equal(t, prompt, updated.SystemPrompt)
	assert.Contains(t, w.auditTypes(), iam.EventAgentUpdated)
	assert.Contains(t, w.auditTypes(), orchestration.EventAgentConfigUpdated)

	suspended, err := w.svc.SuspendAgent(ctx, ada, org, created.ID)
	require.NoError(t, err)
	assert.Equal(t, iam.PrincipalSuspended, suspended.Status)
	reactivated, err := w.svc.ReactivateAgent(ctx, ada, org, created.ID)
	require.NoError(t, err)
	assert.Equal(t, iam.PrincipalActive, reactivated.Status)
	assert.Contains(t, w.auditTypes(), iam.EventAgentSuspended)
	assert.Contains(t, w.auditTypes(), iam.EventAgentReactivated)
}

func TestAgentAuthorization(t *testing.T) {
	w := newWorld(t)
	w.authz.deny = func(r authorization.Request) bool {
		return r.Permission == iam.PermissionAgentManage || r.Permission == iam.PermissionAgentRead
	}
	_, err := w.svc.CreateAgent(ctx, ada, orchestration.CreateAgentInput{
		OrganizationID: org, TeamID: teamID, DisplayName: "Release helper",
	})
	assert.ErrorIs(t, err, authorization.ErrDenied)
	_, err = w.svc.ListAgents(ctx, ada, orchestration.ListAgentsQuery{OrganizationID: org})
	assert.ErrorIs(t, err, authorization.ErrDenied)
}

func TestNewServiceRequiresItsDependencies(t *testing.T) {
	w := newWorld(t)
	all := []ServiceOption{
		WithAgentRepo(agentsRepo{w.store}), WithConfigRepo(configsRepo{w.store}),
		WithTeams(teams{w.store}), WithTeamDirectory(directory{w.store}),
		WithAuthorizer(w.authz), WithTransactor(w.store),
		WithOutboxRecorder(outboxLog{w.store}), WithAuditRecorder(auditLog{w.store}),
	}
	for i := range all {
		without := append(append([]ServiceOption{}, all[:i]...), all[i+1:]...)
		_, err := NewService(without...)
		assert.Error(t, err, "without option %d", i)
	}
}

var _ change.Aggregate = (*orchestration.AgentConfig)(nil)
