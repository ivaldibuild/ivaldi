package sources

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// Controller serves the Source operations. Every operation requires an
// access token; the caller's Organization comes from the token.
type Controller struct {
	service versioncontrol.SourceService
}

var _ api.Routes = (*Controller)(nil)

func NewController(service versioncontrol.SourceService) *Controller {
	return &Controller{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *Controller) RegisterRoutes(h huma.API) {
	register(h, "create-source", http.MethodPost, "/projects/{project_id}/sources", "Connect a repository as a Source under a Project", http.StatusCreated, c.CreateSource)
	register(h, "list-project-sources", http.MethodGet, "/projects/{project_id}/sources", "List a Project's Sources", 0, c.ListProjectSources)
	register(h, "list-workspace-sources", http.MethodGet, "/workspaces/{workspace_id}/sources", "List a Workspace's Sources", 0, c.ListWorkspaceSources)
	register(h, "get-source", http.MethodGet, "/sources/{source_id}", "Get a Source", 0, c.GetSource)
	register(h, "archive-source", http.MethodPost, "/sources/{source_id}/archive", "Disconnect a Source", 0, c.ArchiveSource)
	register(h, "restore-source", http.MethodPost, "/sources/{source_id}/restore", "Restore a disconnected Source", 0, c.RestoreSource)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"version-control"},
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

func (c *Controller) CreateSource(ctx context.Context, req *CreateSourceRequest) (*SourceResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	source, err := c.service.CreateSource(ctx, who.Actor, versioncontrol.CreateSourceInput{
		OrganizationID:       who.OrganizationID,
		ProjectID:            req.ProjectID,
		ConnectionID:         req.Body.ConnectionID,
		ExternalRepositoryID: req.Body.ExternalRepositoryID,
	})
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return sourceResponse(source), nil
}

func (c *Controller) ListProjectSources(ctx context.Context, req *ListProjectSourcesRequest) (*SourceListResponse, error) {
	return c.list(ctx, versioncontrol.ListSourcesQuery{
		ProjectID: req.ProjectID, IncludeArchived: req.IncludeArchived, Page: req.PageParams.query(),
	})
}

func (c *Controller) ListWorkspaceSources(ctx context.Context, req *ListWorkspaceSourcesRequest) (*SourceListResponse, error) {
	return c.list(ctx, versioncontrol.ListSourcesQuery{
		WorkspaceID: req.WorkspaceID, IncludeArchived: req.IncludeArchived, Page: req.PageParams.query(),
	})
}

func (c *Controller) list(ctx context.Context, query versioncontrol.ListSourcesQuery) (*SourceListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	query.OrganizationID = who.OrganizationID
	page, err := c.service.ListSources(ctx, who.Actor, query)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	out := &SourceListResponse{}
	out.Body = api.SuccessResponse(nil, snapshots(page))
	return out, nil
}

func (c *Controller) GetSource(ctx context.Context, req *SourceRequest) (*SourceResponse, error) {
	return c.source(ctx, req.SourceID, c.service.GetSource)
}

func (c *Controller) ArchiveSource(ctx context.Context, req *SourceRequest) (*SourceResponse, error) {
	return c.source(ctx, req.SourceID, c.service.ArchiveSource)
}

func (c *Controller) RestoreSource(ctx context.Context, req *SourceRequest) (*SourceResponse, error) {
	return c.source(ctx, req.SourceID, c.service.RestoreSource)
}

func (c *Controller) source(ctx context.Context, sourceID string, op func(context.Context, identity.Actor, string, string) (*versioncontrol.Source, error)) (*SourceResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	source, err := op(ctx, who.Actor, who.OrganizationID, sourceID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return sourceResponse(source), nil
}

func snapshots(page *pagination.Result[*versioncontrol.Source]) pagination.Result[versioncontrol.SourceSnapshot] {
	items := make([]versioncontrol.SourceSnapshot, 0, len(page.Items))
	for _, source := range page.Items {
		items = append(items, source.Snapshot())
	}
	return pagination.Result[versioncontrol.SourceSnapshot]{
		Items:          items,
		TotalCount:     page.TotalCount,
		HasNext:        page.HasNext,
		HasPrevious:    page.HasPrevious,
		NextCursor:     page.NextCursor,
		PreviousCursor: page.PreviousCursor,
	}
}
