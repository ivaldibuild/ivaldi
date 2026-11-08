package profile

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// ProfileController serves GET /me. It needs an access token and nothing
// more: it describes the caller from the token's principal and
// Organization, never from the request.
type ProfileController struct {
	service iam.ProfileService
}

var _ api.Routes = (*ProfileController)(nil)

func NewProfileController(service iam.ProfileService) *ProfileController {
	return &ProfileController{service: service}
}

func (c *ProfileController) RegisterRoutes(h huma.API) {
	huma.Register(h, huma.Operation{
		OperationID: "get-me",
		Method:      http.MethodGet,
		Path:        "/me",
		Summary:     "Get the signed-in caller, their Organization, and their roles in it",
		Tags:        []string{"me"},
		Security:    []map[string][]string{{api.BearerAuth: {}}},
	}, c.GetMe)
	huma.Register(h, huma.Operation{
		OperationID: "get-my-permissions",
		Method:      http.MethodGet,
		Path:        "/me/permissions",
		Summary:     "Check which of the given permissions the caller holds on a resource",
		Description: "Lets a frontend offer only what the caller may do. Every operation still decides for itself.",
		Tags:        []string{"me"},
		Security:    []map[string][]string{{api.BearerAuth: {}}},
	}, c.GetMyPermissions)
}

type MyPermissionsRequest struct {
	ResourceID  string   `query:"resource_id" doc:"The resource to check, such as a Project; the Organization when omitted"`
	Permissions []string `query:"permission,explode" required:"true" minItems:"1" maxItems:"20" doc:"The permissions to check, such as source.manage"`
}

func (c *ProfileController) GetMyPermissions(ctx context.Context, req *MyPermissionsRequest) (*MyPermissionsResponse, error) {
	who, ok := identity.CallerFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("an access token is required")
	}
	granted, err := c.service.Permissions(ctx, who.Actor, who.OrganizationID, req.ResourceID, req.Permissions)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	resource := req.ResourceID
	if resource == "" {
		resource = who.OrganizationID
	}
	out := &MyPermissionsResponse{}
	out.Body = api.SuccessResponse(nil, MyPermissions{ResourceID: resource, Granted: granted})
	return out, nil
}

func (c *ProfileController) GetMe(ctx context.Context, _ *struct{}) (*MeResponse, error) {
	who, ok := identity.CallerFrom(ctx)
	if !ok {
		// The API refuses a secured operation without a valid token, so this
		// is a wiring bug.
		return nil, huma.Error401Unauthorized("an access token is required")
	}
	p, err := c.service.Profile(ctx, who.Actor, who.OrganizationID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return meResponse(p), nil
}
