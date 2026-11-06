package people

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

// PeopleController serves GET /people for the caller's Organization, which
// comes from the token, never from the request.
type PeopleController struct {
	service iam.PeopleService
}

var _ api.Routes = (*PeopleController)(nil)

func NewPeopleController(service iam.PeopleService) *PeopleController {
	return &PeopleController{service: service}
}

func (c *PeopleController) RegisterRoutes(h huma.API) {
	huma.Register(h, huma.Operation{
		OperationID: "list-people",
		Method:      http.MethodGet,
		Path:        "/people",
		Summary:     "List the Organization's people",
		Description: "Lists the Organization's users with their emails and the roles they hold on the Organization. " +
			"Agents are listed through their Teams; service accounts and workloads are not listed.",
		Tags:     []string{"people"},
		Security: []map[string][]string{{api.BearerAuth: {}}},
	}, c.ListPeople)
}

type ListPeopleRequest struct {
	Search          string `query:"q" maxLength:"320" doc:"Keep the people whose name or email contains this, ignoring case"`
	IncludeInactive bool   `query:"include_inactive" doc:"Also list suspended and deactivated people"`
	Limit           int    `query:"limit" minimum:"1" maximum:"100" doc:"Most items to return"`
	SortBy          string `query:"sort_by" enum:"display_name,created_at" doc:"Field to sort by: display_name (the default) or created_at"`
	SortOrder       string `query:"sort_order" enum:"asc,desc" doc:"Sort direction"`
	NextCursor      string `query:"next_cursor" doc:"Cursor from a previous page's next_cursor"`
	PreviousCursor  string `query:"previous_cursor" doc:"Cursor from a previous page's previous_cursor"`
}

func (r *ListPeopleRequest) query(organizationID string) iam.ListPeopleQuery {
	page := pagination.Query{Limit: r.Limit, SortBy: r.SortBy, SortOrder: r.SortOrder}
	if r.NextCursor != "" {
		page.NextCursor = &r.NextCursor
	}
	if r.PreviousCursor != "" {
		page.PreviousCursor = &r.PreviousCursor
	}
	return iam.ListPeopleQuery{OrganizationID: organizationID, Search: r.Search, IncludeInactive: r.IncludeInactive, Page: page}
}

type PeopleResponse struct {
	Body api.Response[pagination.Result[Person]]
}

// Person is one of the Organization's users.
type Person struct {
	ID             string              `json:"id"`
	DisplayName    string              `json:"display_name"`
	Email          string              `json:"email"`
	Status         iam.PrincipalStatus `json:"status"`
	CreatedAt      time.Time           `json:"created_at"`
	JoinedAt       time.Time           `json:"joined_at"`
	LastSignedInAt *time.Time          `json:"last_signed_in_at"`
	// Roles are the roles held on the Organization itself, directly or
	// through a group.
	Roles []Role `json:"roles"`
}

type Role struct {
	// Role is the built-in role's key, such as "owner", or empty for a
	// custom role.
	Role   string `json:"role"`
	RoleID string `json:"role_id"`
}

func (c *PeopleController) ListPeople(ctx context.Context, req *ListPeopleRequest) (*PeopleResponse, error) {
	who, ok := identity.CallerFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("an access token is required")
	}
	page, err := c.service.ListPeople(ctx, who.Actor, req.query(who.OrganizationID))
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	items := make([]Person, len(page.Items))
	for i, p := range page.Items {
		roles := make([]Role, len(p.Roles))
		for j, r := range p.Roles {
			roles[j] = Role{Role: r.Key, RoleID: r.RoleID}
		}
		items[i] = Person{
			ID: p.User.ID, DisplayName: p.User.DisplayName, Email: p.Email, Status: p.User.Status,
			CreatedAt: p.User.CreatedAt, JoinedAt: p.User.JoinedAt, LastSignedInAt: p.User.LastSignedInAt, Roles: roles,
		}
	}
	return &PeopleResponse{Body: api.SuccessResponse(nil, pagination.Result[Person]{
		Items: items, TotalCount: page.TotalCount, HasNext: page.HasNext, HasPrevious: page.HasPrevious,
		NextCursor: page.NextCursor, PreviousCursor: page.PreviousCursor,
	})}, nil
}

// translateToHttpError turns a service error into the HTTP error the API
// returns. Anything unexpected is logged and becomes a 500 with no detail.
func translateToHttpError(ctx context.Context, err error) error {
	var invalid *iam.ErrInvalidInput
	switch {
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, iam.ErrInvalid):
		return huma.Error422UnprocessableEntity("the request is invalid")
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
