package organizations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// OrganizationController serves the caller's Organization. A token is for
// one Organization, so the path names none: it comes from the token, never
// from the request.
type OrganizationController struct {
	service iam.OrganizationService
}

var _ api.Routes = (*OrganizationController)(nil)

func NewOrganizationController(service iam.OrganizationService) *OrganizationController {
	return &OrganizationController{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *OrganizationController) RegisterRoutes(h huma.API) {
	huma.Register(h, huma.Operation{
		OperationID: "get-organization",
		Method:      http.MethodGet,
		Path:        "/organization",
		Summary:     "Get the caller's Organization",
		Tags:        []string{"organization"},
		Security:    bearer,
	}, c.GetOrganization)
	huma.Register(h, huma.Operation{
		OperationID: "update-organization",
		Method:      http.MethodPatch,
		Path:        "/organization",
		Summary:     "Rename the caller's Organization, change its address, or change its logo",
		Description: "Changing the slug takes effect at once: sign-in hints and links that name the old slug stop matching, " +
			"while ones that name the Organization's ID keep working.",
		Tags:     []string{"organization"},
		Security: bearer,
	}, c.UpdateOrganization)
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

func (c *OrganizationController) GetOrganization(ctx context.Context, _ *struct{}) (*OrganizationResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	o, err := c.service.GetOrganization(ctx, who.Actor, who.OrganizationID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return organizationResponse(o), nil
}

func (c *OrganizationController) UpdateOrganization(ctx context.Context, req *UpdateOrganizationRequest) (*OrganizationResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	o, err := c.service.UpdateOrganization(ctx, who.Actor, who.OrganizationID, req.props())
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return organizationResponse(o), nil
}
