package environments

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// EnvironmentController serves the Environment and Target operations. Every
// operation requires an access token; the caller's Organization comes from
// the token, never from the request.
type EnvironmentController struct {
	service delivery.EnvironmentService
}

var _ api.Routes = (*EnvironmentController)(nil)

func NewEnvironmentController(service delivery.EnvironmentService) *EnvironmentController {
	return &EnvironmentController{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *EnvironmentController) RegisterRoutes(h huma.API) {
	register(h, "create-environment", http.MethodPost, "/workspaces/{workspace_id}/environments", "Create an Environment in a Workspace", http.StatusCreated, c.CreateEnvironment)
	register(h, "list-environments", http.MethodGet, "/workspaces/{workspace_id}/environments", "List a Workspace's Environments", 0, c.ListEnvironments)
	register(h, "get-environment", http.MethodGet, "/environments/{environment_id}", "Get an Environment", 0, c.GetEnvironment)
	register(h, "update-environment", http.MethodPatch, "/environments/{environment_id}", "Change an Environment", 0, c.UpdateEnvironment)
	register(h, "archive-environment", http.MethodPost, "/environments/{environment_id}/archive", "Archive an Environment that has no active Targets and is in no active Lifecycle", 0, c.ArchiveEnvironment)
	register(h, "restore-environment", http.MethodPost, "/environments/{environment_id}/restore", "Restore an archived Environment", 0, c.RestoreEnvironment)

	register(h, "create-target", http.MethodPost, "/environments/{environment_id}/targets", "Add a Target to an Environment", http.StatusCreated, c.CreateTarget)
	register(h, "list-targets", http.MethodGet, "/environments/{environment_id}/targets", "List an Environment's Targets", 0, c.ListTargets)
	register(h, "get-target", http.MethodGet, "/targets/{target_id}", "Get a Target", 0, c.GetTarget)
	register(h, "update-target", http.MethodPatch, "/targets/{target_id}", "Change a Target", 0, c.UpdateTarget)
	register(h, "archive-target", http.MethodPost, "/targets/{target_id}/archive", "Archive a Target", 0, c.ArchiveTarget)
	register(h, "restore-target", http.MethodPost, "/targets/{target_id}/restore", "Restore an archived Target", 0, c.RestoreTarget)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"environments"},
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

func (c *EnvironmentController) CreateEnvironment(ctx context.Context, req *CreateEnvironmentRequest) (*EnvironmentResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	e, err := c.service.CreateEnvironment(ctx, who.Actor, req.input(who.OrganizationID))
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return environmentResponse(e), nil
}

func (c *EnvironmentController) ListEnvironments(ctx context.Context, req *ListEnvironmentsRequest) (*EnvironmentListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	page, err := c.service.ListEnvironments(ctx, who.Actor, delivery.ListEnvironmentsQuery{
		OrganizationID:  who.OrganizationID,
		WorkspaceID:     req.WorkspaceID,
		IncludeArchived: req.IncludeArchived,
		Page:            req.query(),
	})
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	out := &EnvironmentListResponse{}
	out.Body = api.SuccessResponse(nil, snapshots(page, (*delivery.Environment).Snapshot))
	return out, nil
}

func (c *EnvironmentController) GetEnvironment(ctx context.Context, req *EnvironmentRequest) (*EnvironmentResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	e, err := c.service.GetEnvironment(ctx, who.Actor, who.OrganizationID, req.EnvironmentID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return environmentResponse(e), nil
}

func (c *EnvironmentController) UpdateEnvironment(ctx context.Context, req *UpdateEnvironmentRequest) (*EnvironmentResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	e, err := c.service.UpdateEnvironment(ctx, who.Actor, who.OrganizationID, req.EnvironmentID, req.props())
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return environmentResponse(e), nil
}

func (c *EnvironmentController) ArchiveEnvironment(ctx context.Context, req *EnvironmentRequest) (*EnvironmentResponse, error) {
	return c.batchEnvironment(ctx, req.EnvironmentID, c.service.ArchiveEnvironments)
}

func (c *EnvironmentController) RestoreEnvironment(ctx context.Context, req *EnvironmentRequest) (*EnvironmentResponse, error) {
	return c.batchEnvironment(ctx, req.EnvironmentID, c.service.RestoreEnvironments)
}

// batchEnvironment calls a batch operation for one Environment.
func (c *EnvironmentController) batchEnvironment(ctx context.Context, environmentID string, op func(context.Context, identity.Actor, string, []string) ([]*delivery.Environment, error)) (*EnvironmentResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	envs, err := op(ctx, who.Actor, who.OrganizationID, []string{environmentID})
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	if len(envs) != 1 {
		return nil, huma.Error500InternalServerError("the request failed")
	}
	return environmentResponse(envs[0]), nil
}

func (c *EnvironmentController) CreateTarget(ctx context.Context, req *CreateTargetRequest) (*TargetResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	t, err := c.service.CreateTarget(ctx, who.Actor, req.input(who.OrganizationID))
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return targetResponse(t), nil
}

func (c *EnvironmentController) ListTargets(ctx context.Context, req *ListTargetsRequest) (*TargetListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	page, err := c.service.ListTargets(ctx, who.Actor, delivery.ListTargetsQuery{
		OrganizationID:  who.OrganizationID,
		EnvironmentID:   req.EnvironmentID,
		IncludeArchived: req.IncludeArchived,
		Page:            req.query(),
	})
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	out := &TargetListResponse{}
	out.Body = api.SuccessResponse(nil, snapshots(page, (*delivery.Target).Snapshot))
	return out, nil
}

func (c *EnvironmentController) GetTarget(ctx context.Context, req *TargetRequest) (*TargetResponse, error) {
	return c.target(ctx, req.TargetID, c.service.GetTarget)
}

func (c *EnvironmentController) UpdateTarget(ctx context.Context, req *UpdateTargetRequest) (*TargetResponse, error) {
	return c.target(ctx, req.TargetID, func(ctx context.Context, actor identity.Actor, organizationID, targetID string) (*delivery.Target, error) {
		return c.service.UpdateTarget(ctx, actor, organizationID, targetID, req.props())
	})
}

func (c *EnvironmentController) ArchiveTarget(ctx context.Context, req *TargetRequest) (*TargetResponse, error) {
	return c.target(ctx, req.TargetID, c.service.ArchiveTarget)
}

func (c *EnvironmentController) RestoreTarget(ctx context.Context, req *TargetRequest) (*TargetResponse, error) {
	return c.target(ctx, req.TargetID, c.service.RestoreTarget)
}

// target calls an operation on one Target.
func (c *EnvironmentController) target(ctx context.Context, targetID string, op func(context.Context, identity.Actor, string, string) (*delivery.Target, error)) (*TargetResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	t, err := op(ctx, who.Actor, who.OrganizationID, targetID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return targetResponse(t), nil
}
