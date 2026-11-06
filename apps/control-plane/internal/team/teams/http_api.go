package teams

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// TeamController serves the Team operations. Every operation requires an
// access token; the caller's Organization comes from the token, never from
// the request.
type TeamController struct {
	service team.TeamService
}

var _ api.Routes = (*TeamController)(nil)

func NewTeamController(service team.TeamService) *TeamController {
	return &TeamController{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *TeamController) RegisterRoutes(h huma.API) {
	register(h, "create-team", http.MethodPost, "/teams", "Create a Team", http.StatusCreated, c.CreateTeam)
	register(h, "list-teams", http.MethodGet, "/teams", "List the Organization's Teams", 0, c.ListTeams)
	register(h, "get-team", http.MethodGet, "/teams/{team_id}", "Get a Team with its members", 0, c.GetTeam)
	register(h, "update-team", http.MethodPatch, "/teams/{team_id}", "Change a Team", 0, c.UpdateTeam)
	register(h, "archive-team", http.MethodPost, "/teams/{team_id}/archive", "Archive a Team", 0, c.ArchiveTeam)
	register(h, "restore-team", http.MethodPost, "/teams/{team_id}/restore", "Restore an archived Team", 0, c.RestoreTeam)

	register(h, "list-team-members", http.MethodGet, "/teams/{team_id}/members", "List a Team's members", 0, c.ListTeamMembers)
	register(h, "set-team-member", http.MethodPut, "/teams/{team_id}/members/{principal_id}", "Add an active user to a Team, or change a member's role", 0, c.SetTeamMember)
	register(h, "remove-team-member", http.MethodDelete, "/teams/{team_id}/members/{principal_id}", "Remove a member from a Team", 0, c.RemoveTeamMember)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"teams"},
		DefaultStatus: status,
		Security:      bearer,
	}, handler)
}

// caller returns the authenticated caller. The API refuses a secured
// operation without a valid token, so a missing caller is a wiring bug.
func caller(ctx context.Context) (identity.Caller, error) {
	c, ok := identity.CallerFrom(ctx)
	if !ok {
		return identity.Caller{}, huma.Error401Unauthorized("an access token is required")
	}
	return c, nil
}

func (c *TeamController) CreateTeam(ctx context.Context, req *CreateTeamRequest) (*TeamResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	t, err := c.service.CreateTeam(ctx, who.Actor, req.input(who.OrganizationID))
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return teamResponse(t), nil
}

func (c *TeamController) ListTeams(ctx context.Context, req *ListTeamsRequest) (*TeamListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	page, err := c.service.ListTeams(ctx, who.Actor, team.ListTeamsQuery{
		OrganizationID:  who.OrganizationID,
		IncludeArchived: req.IncludeArchived,
		Page:            req.query(),
	})
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	out := &TeamListResponse{}
	out.Body = api.SuccessResponse(nil, snapshots(page, (*team.Team).Snapshot))
	return out, nil
}

func (c *TeamController) GetTeam(ctx context.Context, req *TeamRequest) (*TeamResponse, error) {
	return c.team(ctx, req.TeamID, c.service.GetTeam)
}

func (c *TeamController) UpdateTeam(ctx context.Context, req *UpdateTeamRequest) (*TeamResponse, error) {
	return c.team(ctx, req.TeamID, func(ctx context.Context, actor identity.Actor, organizationID, teamID string) (*team.Team, error) {
		return c.service.UpdateTeam(ctx, actor, organizationID, teamID, req.props())
	})
}

func (c *TeamController) ArchiveTeam(ctx context.Context, req *TeamRequest) (*TeamResponse, error) {
	return c.team(ctx, req.TeamID, c.service.ArchiveTeam)
}

func (c *TeamController) RestoreTeam(ctx context.Context, req *TeamRequest) (*TeamResponse, error) {
	return c.team(ctx, req.TeamID, c.service.RestoreTeam)
}

func (c *TeamController) ListTeamMembers(ctx context.Context, req *TeamRequest) (*TeamMemberListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	members, err := c.service.ListTeamMembers(ctx, who.Actor, who.OrganizationID, req.TeamID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &TeamMemberListResponse{Body: api.SuccessResponse(nil, members)}, nil
}

func (c *TeamController) SetTeamMember(ctx context.Context, req *SetTeamMemberRequest) (*TeamResponse, error) {
	return c.team(ctx, req.TeamID, func(ctx context.Context, actor identity.Actor, organizationID, teamID string) (*team.Team, error) {
		return c.service.SetTeamMember(ctx, actor, organizationID, teamID, req.PrincipalID, team.Role(req.Body.Role))
	})
}

func (c *TeamController) RemoveTeamMember(ctx context.Context, req *TeamMemberRequest) (*TeamResponse, error) {
	return c.team(ctx, req.TeamID, func(ctx context.Context, actor identity.Actor, organizationID, teamID string) (*team.Team, error) {
		return c.service.RemoveTeamMember(ctx, actor, organizationID, teamID, req.PrincipalID)
	})
}

// team calls an operation on one Team.
func (c *TeamController) team(ctx context.Context, teamID string, op func(context.Context, identity.Actor, string, string) (*team.Team, error)) (*TeamResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	t, err := op(ctx, who.Actor, who.OrganizationID, teamID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return teamResponse(t), nil
}
