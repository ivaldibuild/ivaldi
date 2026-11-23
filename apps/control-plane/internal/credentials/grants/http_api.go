package grants

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Controller serves the grant operation a Cluster's agent calls.
type Controller struct {
	service Service
}

var _ api.Routes = (*Controller)(nil)

func NewController(service Service) *Controller { return &Controller{service: service} }

type GrantRequest struct {
	DeploymentID string `path:"deployment_id" doc:"The applying Deployment whose secrets the agent is about to read"`
}

// GrantView is access to one store. Its JWT is a credential: the agent
// masks it, uses it once, and never stores or reports it.
type GrantView struct {
	StoreID   string    `json:"store_id"`
	Mode      string    `json:"mode" enum:"federated"`
	ExpiresAt time.Time `json:"expires_at"`
	// JWT is set for a federated grant: the agent exchanges it at the store.
	JWT string `json:"jwt,omitempty"`
}

type GrantsView struct {
	Grants []GrantView `json:"grants"`
}

type GrantResponse struct {
	Body api.Response[GrantsView]
}

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
	out := GrantsView{Grants: make([]GrantView, len(grants))}
	for i, g := range grants {
		out.Grants[i] = GrantView{StoreID: g.StoreID, Mode: string(g.Mode), ExpiresAt: g.ExpiresAt, JWT: g.JWT}
	}
	return &GrantResponse{Body: api.SuccessResponse(nil, out)}, nil
}

// httpError turns a service error into the HTTP error the API returns.
// Anything unexpected is logged and becomes a 500 with no detail.
func httpError(ctx context.Context, err error) error {
	var deliveryInvalid *delivery.ErrInvalidInput
	var invalid *credentials.ErrInvalidInput
	switch {
	case errors.As(err, &deliveryInvalid):
		return huma.Error422UnprocessableEntity(deliveryInvalid.Message)
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, delivery.ErrNotClusterAgent), errors.Is(err, delivery.ErrNotDeploymentAgent):
		return huma.Error403Forbidden("the caller does not serve this deployment's cluster")
	case errors.Is(err, delivery.ErrNotFound), errors.Is(err, credentials.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, delivery.ErrDeploymentStatus):
		return huma.Error409Conflict(err.Error())
	}
	return api.InternalError(ctx, err)
}
