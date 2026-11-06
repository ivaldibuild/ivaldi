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
