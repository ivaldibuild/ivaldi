package projects

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// ProjectController serves the Project operations. Every operation requires
// an access token; the caller's Organization comes from the token, never
// from the request.
type ProjectController struct {
	service workspace.ProjectService
}

var _ api.Routes = (*ProjectController)(nil)

func NewProjectController(service workspace.ProjectService) *ProjectController {
	return &ProjectController{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *ProjectController) RegisterRoutes(h huma.API) {
	register(h, "create-project", http.MethodPost, "/workspaces/{workspace_id}/projects", "Create a Project in a Workspace", http.StatusCreated, c.CreateProject)
	register(h, "list-projects", http.MethodGet, "/workspaces/{workspace_id}/projects", "List a Workspace's Projects", 0, c.ListProjects)
	register(h, "get-project", http.MethodGet, "/projects/{project_id}", "Get a Project", 0, c.GetProject)
	register(h, "update-project", http.MethodPatch, "/projects/{project_id}", "Rename or redescribe a Project", 0, c.UpdateProject)
	register(h, "transfer-project", http.MethodPost, "/projects/{project_id}/transfer", "Hand a Project to another Team", 0, c.TransferProject)
	register(h, "archive-project", http.MethodPost, "/projects/{project_id}/archive", "Archive a Project that has nothing active in it", 0, c.ArchiveProject)
	register(h, "restore-project", http.MethodPost, "/projects/{project_id}/restore", "Restore an archived Project", 0, c.RestoreProject)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"projects"},
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

// call runs an operation on one Project as the caller.
func call(ctx context.Context, op func(identity.Caller) (*workspace.Project, error)) (*ProjectResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	p, err := op(who)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return projectResponse(p), nil
}

func (c *ProjectController) CreateProject(ctx context.Context, req *CreateProjectRequest) (*ProjectResponse, error) {
	return call(ctx, func(who identity.Caller) (*workspace.Project, error) {
		return c.service.CreateProject(ctx, who.Actor, req.input(who.OrganizationID))
	})
}

func (c *ProjectController) ListProjects(ctx context.Context, req *ListProjectsRequest) (*ProjectListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	ps, err := c.service.ListProjects(ctx, who.Actor, who.OrganizationID, req.WorkspaceID, req.IncludeArchived)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return projectListResponse(ps), nil
}

func (c *ProjectController) GetProject(ctx context.Context, req *ProjectRequest) (*ProjectResponse, error) {
	return call(ctx, func(who identity.Caller) (*workspace.Project, error) {
		return c.service.GetProject(ctx, who.Actor, who.OrganizationID, req.ProjectID)
	})
}

func (c *ProjectController) UpdateProject(ctx context.Context, req *UpdateProjectRequest) (*ProjectResponse, error) {
	return call(ctx, func(who identity.Caller) (*workspace.Project, error) {
		return c.service.UpdateProject(ctx, who.Actor, who.OrganizationID, req.ProjectID, req.props())
	})
}

func (c *ProjectController) TransferProject(ctx context.Context, req *TransferProjectRequest) (*ProjectResponse, error) {
	return call(ctx, func(who identity.Caller) (*workspace.Project, error) {
		return c.service.TransferProject(ctx, who.Actor, who.OrganizationID, req.ProjectID, req.Body.OwnerTeamID)
	})
}

func (c *ProjectController) ArchiveProject(ctx context.Context, req *ProjectRequest) (*ProjectResponse, error) {
	return call(ctx, func(who identity.Caller) (*workspace.Project, error) {
		return c.service.ArchiveProject(ctx, who.Actor, who.OrganizationID, req.ProjectID)
	})
}

func (c *ProjectController) RestoreProject(ctx context.Context, req *ProjectRequest) (*ProjectResponse, error) {
	return call(ctx, func(who identity.Caller) (*workspace.Project, error) {
		return c.service.RestoreProject(ctx, who.Actor, who.OrganizationID, req.ProjectID)
	})
}
