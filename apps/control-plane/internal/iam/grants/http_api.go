package grants

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// GrantController serves the grant operations. The caller's Organization
// comes from the token, never from the request.
type GrantController struct {
	service iam.GrantService
}

var _ api.Routes = (*GrantController)(nil)

func NewGrantController(service iam.GrantService) *GrantController {
	return &GrantController{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *GrantController) RegisterRoutes(h huma.API) {
	huma.Register(h, huma.Operation{
		OperationID: "list-grants",
		Method:      http.MethodGet,
		Path:        "/grants",
		Summary:     "List the roles granted on a resource",
		Description: "Lists the grants on the resource, and with include_ancestors those on the resources above it, which apply to it too.",
		Tags:        []string{"grants"},
		Security:    bearer,
	}, c.ListGrants)
	huma.Register(h, huma.Operation{
		OperationID: "create-grant",
		Method:      http.MethodPost,
		Path:        "/grants",
		Summary:     "Grant a role on a resource to a principal, a group, or a Team",
		Description: "owner may be granted only on the Organization, and admin on the Organization or a Workspace. " +
			"Granting needs grant.manage on the resource; granting owner needs owner.manage, which only owners hold.",
		Tags:          []string{"grants"},
		DefaultStatus: http.StatusCreated,
		Security:      bearer,
	}, c.CreateGrant)
	huma.Register(h, huma.Operation{
		OperationID:   "remove-grant",
		Method:        http.MethodDelete,
		Path:          "/grants/{grant_id}",
		Summary:       "Remove a grant",
		Description:   "Removing the owner grant of the Organization's last active owner is refused.",
		Tags:          []string{"grants"},
		DefaultStatus: http.StatusNoContent,
		Security:      bearer,
	}, c.RemoveGrant)
}

type ListGrantsRequest struct {
	ResourceID       string `query:"resource_id" required:"true" doc:"The resource whose grants are listed, such as the Organization's ID or a Workspace's"`
	IncludeAncestors bool   `query:"include_ancestors" doc:"Also list the grants on the resources above it"`
	IncludeExpired   bool   `query:"include_expired" doc:"Also list grants that have expired"`
	Limit            int    `query:"limit" minimum:"1" maximum:"100" doc:"Most items to return"`
	SortOrder        string `query:"sort_order" enum:"asc,desc" doc:"Sort direction; grants sort by when they were made"`
	NextCursor       string `query:"next_cursor" doc:"Cursor from a previous page's next_cursor"`
	PreviousCursor   string `query:"previous_cursor" doc:"Cursor from a previous page's previous_cursor"`
}

type CreateGrantRequest struct {
	Body struct {
		PrincipalID string     `json:"principal_id,omitempty" required:"false" doc:"The user, Agent, or service account to grant the role to"`
		GroupID     string     `json:"group_id,omitempty" required:"false" doc:"The group to grant the role to"`
		TeamID      string     `json:"team_id,omitempty" required:"false" doc:"The Team to grant the role to; its members hold it while they are members"`
		Role        string     `json:"role" required:"true" enum:"owner,admin,member,viewer" doc:"The role to grant"`
		ResourceID  string     `json:"resource_id" required:"true" doc:"The resource the role is granted on, and everything beneath it"`
		ExpiresAt   *time.Time `json:"expires_at,omitempty" required:"false" doc:"When the grant stops applying; it never does when omitted"`
	}
}

type GrantRequest struct {
	GrantID string `path:"grant_id" doc:"The grant's ID"`
}

type GrantResponse struct {
	Body api.Response[Grant]
}

type GrantListResponse struct {
	Body api.Response[pagination.Result[Grant]]
}

type RemoveGrantResponse struct{}

// Grant is a role granted on a resource to a principal or a group.
type Grant struct {
	ID string `json:"id"`
	// Role is the built-in role's key, or empty for a custom role.
	Role       string `json:"role"`
	RoleID     string `json:"role_id"`
	ResourceID string `json:"resource_id"`
	// Principal is the grantee when the grant is to a principal, else null.
	Principal *Principal `json:"principal"`
	// Group is the grantee when the grant is to a group, else null.
	Group                *Group     `json:"group"`
	GrantedByPrincipalID string     `json:"granted_by_principal_id"`
	ExpiresAt            *time.Time `json:"expires_at"`
	CreatedAt            time.Time  `json:"created_at"`
}

type Principal struct {
	ID          string              `json:"id"`
	Kind        iam.PrincipalKind   `json:"kind"`
	DisplayName string              `json:"display_name"`
	Status      iam.PrincipalStatus `json:"status"`
}

type Group struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Source iam.GroupSource `json:"source"`
	// TeamID is the Team the group mirrors, for a Team's group.
	TeamID *string `json:"team_id"`
}

func grantView(e iam.GrantEntry) Grant {
	g := Grant{
		ID: e.Grant.ID, Role: e.RoleKey, RoleID: e.Grant.RoleID, ResourceID: e.Grant.ResourceID,
		GrantedByPrincipalID: e.Grant.GrantedByPrincipalID, ExpiresAt: e.Grant.ExpiresAt, CreatedAt: e.Grant.CreatedAt,
	}
	if p := e.Principal; p != nil {
		g.Principal = &Principal{ID: p.ID, Kind: p.Kind, DisplayName: p.DisplayName, Status: p.Status}
	}
	if gr := e.Group; gr != nil {
		g.Group = &Group{ID: gr.ID, Name: gr.Name, Source: gr.Source}
		if gr.Source == iam.GroupTeam {
			g.Group.TeamID = gr.ExternalID
		}
	}
	return g
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

func (c *GrantController) ListGrants(ctx context.Context, req *ListGrantsRequest) (*GrantListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	page := pagination.Query{Limit: req.Limit, SortOrder: req.SortOrder}
	if req.NextCursor != "" {
		page.NextCursor = &req.NextCursor
	}
	if req.PreviousCursor != "" {
		page.PreviousCursor = &req.PreviousCursor
	}
	result, err := c.service.ListGrants(ctx, who.Actor, iam.ListResourceGrantsQuery{
		OrganizationID: who.OrganizationID, ResourceID: req.ResourceID,
		IncludeAncestors: req.IncludeAncestors, IncludeExpired: req.IncludeExpired, Page: page,
	})
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	items := make([]Grant, len(result.Items))
	for i, e := range result.Items {
		items[i] = grantView(e)
	}
	return &GrantListResponse{Body: api.SuccessResponse(nil, pagination.Result[Grant]{
		Items: items, TotalCount: result.TotalCount, HasNext: result.HasNext, HasPrevious: result.HasPrevious,
		NextCursor: result.NextCursor, PreviousCursor: result.PreviousCursor,
	})}, nil
}

func (c *GrantController) CreateGrant(ctx context.Context, req *CreateGrantRequest) (*GrantResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	e, err := c.service.CreateGrant(ctx, who.Actor, iam.CreateGrantInput{
		OrganizationID: who.OrganizationID, PrincipalID: b.PrincipalID, GroupID: b.GroupID, TeamID: b.TeamID,
		Role: b.Role, ResourceID: b.ResourceID, ExpiresAt: b.ExpiresAt,
	})
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &GrantResponse{Body: api.SuccessResponse(nil, grantView(*e))}, nil
}

func (c *GrantController) RemoveGrant(ctx context.Context, req *GrantRequest) (*RemoveGrantResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.service.RemoveGrant(ctx, who.Actor, who.OrganizationID, req.GrantID); err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &RemoveGrantResponse{}, nil
}

// translateToHttpError turns a service error into the HTTP error the API
// returns. Anything unexpected is logged and becomes a 500 with no detail,
// since its message may describe internals such as SQL.
func translateToHttpError(ctx context.Context, err error) error {
	var invalid *iam.ErrInvalidInput
	var approval *authorization.ApprovalRequiredError

	switch {
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, iam.ErrInvalid):
		return huma.Error422UnprocessableEntity("the request is invalid")
	case errors.Is(err, iam.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, iam.ErrAlreadyExists):
		return huma.Error409Conflict("the grantee already holds the role on the resource")
	case errors.Is(err, iam.ErrLastOwner):
		return huma.Error409Conflict("this is the Organization's last owner; grant owner to someone else first")
	case errors.Is(err, iam.ErrStatus):
		return huma.Error409Conflict(err.Error())
	case errors.As(err, &approval):
		return huma.Error403Forbidden("the change needs approval; decision " + approval.DecisionID)
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
