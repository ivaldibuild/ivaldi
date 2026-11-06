package lifecycles

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// LifecycleController serves the Lifecycle operations. Every operation
// requires an access token; the caller's Organization comes from the token,
// never from the request.
type LifecycleController struct {
	service delivery.LifecycleService
}

var _ api.Routes = (*LifecycleController)(nil)

func NewLifecycleController(service delivery.LifecycleService) *LifecycleController {
	return &LifecycleController{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *LifecycleController) RegisterRoutes(h huma.API) {
	register(h, "create-lifecycle", http.MethodPost, "/workspaces/{workspace_id}/lifecycles", "Create a Lifecycle in a Workspace", http.StatusCreated, c.CreateLifecycle)
	register(h, "list-lifecycles", http.MethodGet, "/workspaces/{workspace_id}/lifecycles", "List a Workspace's Lifecycles", 0, c.ListLifecycles)
	register(h, "get-lifecycle", http.MethodGet, "/lifecycles/{lifecycle_id}", "Get a Lifecycle", 0, c.GetLifecycle)
	register(h, "update-lifecycle", http.MethodPatch, "/lifecycles/{lifecycle_id}", "Change a Lifecycle", 0, c.UpdateLifecycle)
	register(h, "archive-lifecycle", http.MethodPost, "/lifecycles/{lifecycle_id}/archive", "Archive a Lifecycle no active Channel follows", 0, c.ArchiveLifecycle)
	register(h, "restore-lifecycle", http.MethodPost, "/lifecycles/{lifecycle_id}/restore", "Restore an archived Lifecycle", 0, c.RestoreLifecycle)

	register(h, "add-lifecycle-phase", http.MethodPost, "/lifecycles/{lifecycle_id}/phases", "Append a phase to a Lifecycle", 0, c.AddLifecyclePhase)
	register(h, "update-lifecycle-phase", http.MethodPut, "/lifecycles/{lifecycle_id}/phases/{phase_id}", "Replace a Lifecycle phase", 0, c.UpdateLifecyclePhase)
	register(h, "move-lifecycle-phase", http.MethodPut, "/lifecycles/{lifecycle_id}/phases/{phase_id}/position", "Move a Lifecycle phase to another position", 0, c.MoveLifecyclePhase)
	register(h, "remove-lifecycle-phase", http.MethodDelete, "/lifecycles/{lifecycle_id}/phases/{phase_id}", "Remove a Lifecycle phase", 0, c.RemoveLifecyclePhase)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"lifecycles"},
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

func (c *LifecycleController) CreateLifecycle(ctx context.Context, req *CreateLifecycleRequest) (*LifecycleResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	l, err := c.service.CreateLifecycle(ctx, who.Actor, req.input(who.OrganizationID))
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return lifecycleResponse(l), nil
}

func (c *LifecycleController) ListLifecycles(ctx context.Context, req *ListLifecyclesRequest) (*LifecycleListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	page, err := c.service.ListLifecycles(ctx, who.Actor, delivery.ListLifecyclesQuery{
		OrganizationID:  who.OrganizationID,
		WorkspaceID:     req.WorkspaceID,
		IncludeArchived: req.IncludeArchived,
		Page:            req.query(),
	})
	if err != nil {
		return nil, httpError(ctx, err)
	}
	out := &LifecycleListResponse{}
	out.Body = api.SuccessResponse(nil, snapshots(page, (*delivery.Lifecycle).Snapshot))
	return out, nil
}

func (c *LifecycleController) GetLifecycle(ctx context.Context, req *LifecycleRequest) (*LifecycleResponse, error) {
	return c.lifecycle(ctx, req.LifecycleID, c.service.GetLifecycle)
}

func (c *LifecycleController) UpdateLifecycle(ctx context.Context, req *UpdateLifecycleRequest) (*LifecycleResponse, error) {
	return c.lifecycle(ctx, req.LifecycleID, func(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*delivery.Lifecycle, error) {
		return c.service.UpdateLifecycle(ctx, actor, organizationID, lifecycleID, req.props())
	})
}

func (c *LifecycleController) ArchiveLifecycle(ctx context.Context, req *LifecycleRequest) (*LifecycleResponse, error) {
	return c.lifecycle(ctx, req.LifecycleID, c.service.ArchiveLifecycle)
}

func (c *LifecycleController) RestoreLifecycle(ctx context.Context, req *LifecycleRequest) (*LifecycleResponse, error) {
	return c.lifecycle(ctx, req.LifecycleID, c.service.RestoreLifecycle)
}

func (c *LifecycleController) AddLifecyclePhase(ctx context.Context, req *AddLifecyclePhaseRequest) (*LifecycleResponse, error) {
	return c.lifecycle(ctx, req.LifecycleID, func(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*delivery.Lifecycle, error) {
		return c.service.AddLifecyclePhase(ctx, actor, organizationID, lifecycleID, req.Body.props())
	})
}

func (c *LifecycleController) UpdateLifecyclePhase(ctx context.Context, req *UpdateLifecyclePhaseRequest) (*LifecycleResponse, error) {
	return c.lifecycle(ctx, req.LifecycleID, func(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*delivery.Lifecycle, error) {
		return c.service.UpdateLifecyclePhase(ctx, actor, organizationID, lifecycleID, req.PhaseID, req.Body.props())
	})
}

func (c *LifecycleController) MoveLifecyclePhase(ctx context.Context, req *MoveLifecyclePhaseRequest) (*LifecycleResponse, error) {
	return c.lifecycle(ctx, req.LifecycleID, func(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*delivery.Lifecycle, error) {
		return c.service.MoveLifecyclePhase(ctx, actor, organizationID, lifecycleID, req.PhaseID, req.Body.Position)
	})
}

func (c *LifecycleController) RemoveLifecyclePhase(ctx context.Context, req *LifecyclePhaseRequest) (*LifecycleResponse, error) {
	return c.lifecycle(ctx, req.LifecycleID, func(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*delivery.Lifecycle, error) {
		return c.service.RemoveLifecyclePhase(ctx, actor, organizationID, lifecycleID, req.PhaseID)
	})
}

// lifecycle calls an operation on one Lifecycle.
func (c *LifecycleController) lifecycle(ctx context.Context, lifecycleID string, op func(context.Context, identity.Actor, string, string) (*delivery.Lifecycle, error)) (*LifecycleResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	l, err := op(ctx, who.Actor, who.OrganizationID, lifecycleID)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return lifecycleResponse(l), nil
}
