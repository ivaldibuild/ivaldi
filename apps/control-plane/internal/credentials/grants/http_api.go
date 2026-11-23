package grants

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Controller serves the grant operation a Cluster's agent calls.
type Controller struct {
	service Service
}

var _ api.Routes = (*Controller)(nil)

func NewController(service Service) *Controller { return &Controller{service: service} }

func (c *Controller) RegisterRoutes(h huma.API) {
	huma.Register(h, huma.Operation{
		OperationID:   "agent-secret-grants",
		Method:        http.MethodPost,
		Path:          "/agent/deployments/{deployment_id}/secret-grants",
		Summary:       "Grant the caller's Cluster's agent access to the Secret Stores an applying Deployment reads",
		Description:   "The agent names only the Deployment. Access is granted to every store its secrets are in whose mode needs a grant, or to none. A Deployment whose stores need none gets no grant.",
		Tags:          []string{"agent"},
		DefaultStatus: http.StatusOK,
		Security:      []map[string][]string{{api.BearerAuth: {}}},
	}, c.Grant)
}

func (c *Controller) Grant(ctx context.Context, req *GrantRequest) (*GrantResponse, error) {
	who, ok := identity.CallerFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("an access token is required")
	}
	grants, err := c.service.Grant(ctx, who, req.DeploymentID)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return grantsResponse(grants), nil
}
