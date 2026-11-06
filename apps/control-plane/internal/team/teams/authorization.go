package teams

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. It implements the whole interface, so a
// new operation does not compile until it says how it is authorized.
//
// Teams are not IAM resources, so every operation is checked on the
// Organization: team.read to read, team.manage to change, and agent.manage
// to create an Agent.
type authorized struct {
	next  team.TeamService
	authz authorization.Authorizer
}

var _ team.TeamService = (*authorized)(nil)

// memberProposal is what a SetTeamMember asks the authorizer to allow.
type memberProposal struct {
	TeamID      string    `json:"team_id"`
	PrincipalID string    `json:"principal_id"`
	Role        team.Role `json:"role"`
}

func (a *authorized) read(ctx context.Context, actor identity.Actor, organizationID, action string) error {
	_, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: team.PermissionTeamRead, OrganizationID: organizationID,
		ResourceID: organizationID, Action: action,
	})
	return err
}

// manage asks for a decision to change Teams and returns ctx carrying it
// for the call.
func (a *authorized) manage(ctx context.Context, actor identity.Actor, organizationID, action string, proposal any) (context.Context, error) {
	return a.decide(ctx, actor, team.PermissionTeamManage, organizationID, action, proposal)
}

func (a *authorized) decide(ctx context.Context, actor identity.Actor, permission, organizationID, action string, proposal any) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: permission, OrganizationID: organizationID,
		ResourceID: organizationID, Action: action, Proposal: proposal,
	})
	if err != nil {
		return nil, err
	}
	return authorization.WithDecision(ctx, "", d), nil
}

func (a *authorized) CreateTeam(ctx context.Context, actor identity.Actor, in team.CreateTeamInput) (*team.Team, error) {
	ctx, err := a.manage(ctx, actor, in.OrganizationID, team.ActionTeamCreate, in)
	if err != nil {
		return nil, err
	}
	return a.next.CreateTeam(ctx, actor, in)
}

func (a *authorized) GetTeam(ctx context.Context, actor identity.Actor, organizationID, teamID string) (*team.Team, error) {
	if err := a.read(ctx, actor, organizationID, team.ActionTeamRead); err != nil {
		return nil, err
	}
	return a.next.GetTeam(ctx, actor, organizationID, teamID)
}

func (a *authorized) GetTeamBySlug(ctx context.Context, actor identity.Actor, organizationID, slug string) (*team.Team, error) {
	if err := a.read(ctx, actor, organizationID, team.ActionTeamRead); err != nil {
		return nil, err
	}
	return a.next.GetTeamBySlug(ctx, actor, organizationID, slug)
}

func (a *authorized) ListTeams(ctx context.Context, actor identity.Actor, query team.ListTeamsQuery) (*pagination.Result[*team.Team], error) {
	if err := a.read(ctx, actor, query.OrganizationID, team.ActionTeamList); err != nil {
		return nil, err
	}
	return a.next.ListTeams(ctx, actor, query)
}

func (a *authorized) UpdateTeam(ctx context.Context, actor identity.Actor, organizationID, teamID string, props team.UpdateTeamProps) (*team.Team, error) {
	ctx, err := a.manage(ctx, actor, organizationID, team.ActionTeamUpdate, props)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateTeam(ctx, actor, organizationID, teamID, props)
}

func (a *authorized) ArchiveTeam(ctx context.Context, actor identity.Actor, organizationID, teamID string) (*team.Team, error) {
	ctx, err := a.manage(ctx, actor, organizationID, team.ActionTeamArchive, teamID)
	if err != nil {
		return nil, err
	}
	return a.next.ArchiveTeam(ctx, actor, organizationID, teamID)
}

func (a *authorized) RestoreTeam(ctx context.Context, actor identity.Actor, organizationID, teamID string) (*team.Team, error) {
	ctx, err := a.manage(ctx, actor, organizationID, team.ActionTeamRestore, teamID)
	if err != nil {
		return nil, err
	}
	return a.next.RestoreTeam(ctx, actor, organizationID, teamID)
}

func (a *authorized) ListTeamMembers(ctx context.Context, actor identity.Actor, organizationID, teamID string) ([]team.TeamMemberSnapshot, error) {
	if err := a.read(ctx, actor, organizationID, team.ActionTeamListMembers); err != nil {
		return nil, err
	}
	return a.next.ListTeamMembers(ctx, actor, organizationID, teamID)
}

func (a *authorized) SetTeamMember(ctx context.Context, actor identity.Actor, organizationID, teamID, principalID string, role team.Role) (*team.Team, error) {
	ctx, err := a.manage(ctx, actor, organizationID, team.ActionTeamSetMember,
		memberProposal{TeamID: teamID, PrincipalID: principalID, Role: role})
	if err != nil {
		return nil, err
	}
	return a.next.SetTeamMember(ctx, actor, organizationID, teamID, principalID, role)
}

func (a *authorized) RemoveTeamMember(ctx context.Context, actor identity.Actor, organizationID, teamID, principalID string) (*team.Team, error) {
	ctx, err := a.manage(ctx, actor, organizationID, team.ActionTeamRemoveMember,
		memberProposal{TeamID: teamID, PrincipalID: principalID})
	if err != nil {
		return nil, err
	}
	return a.next.RemoveTeamMember(ctx, actor, organizationID, teamID, principalID)
}

// CreateAgent needs agent.manage rather than team.manage: an Agent is an
// IAM principal that can hold grants.
func (a *authorized) CreateAgent(ctx context.Context, actor identity.Actor, in team.CreateAgentInput) (*team.CreatedAgent, error) {
	ctx, err := a.decide(ctx, actor, team.PermissionAgentManage, in.OrganizationID, team.ActionTeamCreateAgent, in)
	if err != nil {
		return nil, err
	}
	return a.next.CreateAgent(ctx, actor, in)
}
