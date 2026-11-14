package agentapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Controller serves the operations a Cluster's runtime agent calls.
type Controller struct {
	service Service
}

var _ api.Routes = (*Controller)(nil)

func NewController(service Service) *Controller { return &Controller{service: service} }

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *Controller) RegisterRoutes(h huma.API) {
	register(h, "agent-targets", http.MethodGet, "/agent/targets",
		"List what the caller's Cluster's agent watches", 0, c.Targets)
	register(h, "agent-assignments", http.MethodGet, "/agent/assignments",
		"List the in-flight Deployments on the caller's Cluster's Targets", 0, c.Assignments)
	register(h, "agent-heartbeat", http.MethodPost, "/agent/heartbeat",
		"Report that the agent is in contact and what it can do", http.StatusNoContent, c.Heartbeat)
	register(h, "agent-observation", http.MethodPost, "/agent/observations",
		"Report what the agent sees running on a Target", 0, c.Observe)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"agent"},
		DefaultStatus: status,
		Security:      bearer,
	}, handler)
}

func caller(ctx context.Context) (identity.Caller, error) {
	c, ok := identity.CallerFrom(ctx)
	if !ok {
		return identity.Caller{}, huma.Error401Unauthorized("an access token is required")
	}
	return c, nil
}

func (c *Controller) Targets(ctx context.Context, _ *struct{}) (*TargetsHTTPResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	out, err := c.service.Targets(ctx, who)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &TargetsHTTPResponse{Body: api.SuccessResponse(nil, *out)}, nil
}

func (c *Controller) Assignments(ctx context.Context, _ *struct{}) (*AssignmentsHTTPResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	items, err := c.service.Assignments(ctx, who)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &AssignmentsHTTPResponse{Body: api.SuccessResponse(nil, items)}, nil
}

func (c *Controller) Heartbeat(ctx context.Context, req *HeartbeatRequest) (*HeartbeatHTTPResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.service.Heartbeat(ctx, who, req.input()); err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &HeartbeatHTTPResponse{}, nil
}

func (c *Controller) Observe(ctx context.Context, req *ObservationRequest) (*ObservationHTTPResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	detail, err := c.service.Observe(ctx, who, req.input(who.OrganizationID))
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &ObservationHTTPResponse{Body: api.SuccessResponse(nil, detail.Deployment)}, nil
}
