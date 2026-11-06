package workspaces

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// WorkspaceController serves the Workspace operations. Every operation
// requires an access token; the caller's Organization comes from the token,
// never from the request.
type WorkspaceController struct {
	service workspace.WorkspaceService
}

var _ api.Routes = (*WorkspaceController)(nil)

func NewWorkspaceController(service workspace.WorkspaceService) *WorkspaceController {
	return &WorkspaceController{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *WorkspaceController) RegisterRoutes(h huma.API) {
	register(h, "create-workspace", http.MethodPost, "/workspaces", "Create a Workspace", http.StatusCreated, c.CreateWorkspace)
	register(h, "list-workspaces", http.MethodGet, "/workspaces", "List the Organization's Workspaces", 0, c.ListWorkspaces)
	register(h, "get-workspace", http.MethodGet, "/workspaces/{workspace_id}", "Get a Workspace", 0, c.GetWorkspace)
	register(h, "update-workspace", http.MethodPatch, "/workspaces/{workspace_id}", "Rename or redescribe a Workspace", 0, c.UpdateWorkspace)
	register(h, "set-default-workspace", http.MethodPost, "/workspaces/{workspace_id}/default", "Make a Workspace the Organization's default", 0, c.SetDefaultWorkspace)
	register(h, "archive-workspace", http.MethodPost, "/workspaces/{workspace_id}/archive", "Archive a Workspace that is not the default and has nothing active in it", 0, c.ArchiveWorkspace)
	register(h, "restore-workspace", http.MethodPost, "/workspaces/{workspace_id}/restore", "Restore an archived Workspace", 0, c.RestoreWorkspace)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"workspaces"},
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

// call runs an operation on one Workspace as the caller.
func call(ctx context.Context, op func(identity.Caller) (*workspace.Workspace, error)) (*WorkspaceResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	w, err := op(who)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return workspaceResponse(w), nil
}

func (c *WorkspaceController) CreateWorkspace(ctx context.Context, req *CreateWorkspaceRequest) (*WorkspaceResponse, error) {
	return call(ctx, func(who identity.Caller) (*workspace.Workspace, error) {
		return c.service.CreateWorkspace(ctx, who.Actor, req.input(who.OrganizationID))
	})
}

func (c *WorkspaceController) ListWorkspaces(ctx context.Context, req *ListWorkspacesRequest) (*WorkspaceListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	ws, err := c.service.ListWorkspaces(ctx, who.Actor, who.OrganizationID, req.IncludeArchived)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return workspaceListResponse(ws), nil
}

func (c *WorkspaceController) GetWorkspace(ctx context.Context, req *WorkspaceRequest) (*WorkspaceResponse, error) {
	return call(ctx, func(who identity.Caller) (*workspace.Workspace, error) {
		return c.service.GetWorkspace(ctx, who.Actor, who.OrganizationID, req.WorkspaceID)
	})
}

func (c *WorkspaceController) UpdateWorkspace(ctx context.Context, req *UpdateWorkspaceRequest) (*WorkspaceResponse, error) {
	return call(ctx, func(who identity.Caller) (*workspace.Workspace, error) {
		return c.service.UpdateWorkspace(ctx, who.Actor, who.OrganizationID, req.WorkspaceID, req.props())
	})
}

func (c *WorkspaceController) SetDefaultWorkspace(ctx context.Context, req *WorkspaceRequest) (*WorkspaceResponse, error) {
	return call(ctx, func(who identity.Caller) (*workspace.Workspace, error) {
		return c.service.SetDefaultWorkspace(ctx, who.Actor, who.OrganizationID, req.WorkspaceID)
	})
}

func (c *WorkspaceController) ArchiveWorkspace(ctx context.Context, req *WorkspaceRequest) (*WorkspaceResponse, error) {
	return call(ctx, func(who identity.Caller) (*workspace.Workspace, error) {
		return c.service.ArchiveWorkspace(ctx, who.Actor, who.OrganizationID, req.WorkspaceID)
	})
}

func (c *WorkspaceController) RestoreWorkspace(ctx context.Context, req *WorkspaceRequest) (*WorkspaceResponse, error) {
	return call(ctx, func(who identity.Caller) (*workspace.Workspace, error) {
		return c.service.RestoreWorkspace(ctx, who.Actor, who.OrganizationID, req.WorkspaceID)
	})
}
