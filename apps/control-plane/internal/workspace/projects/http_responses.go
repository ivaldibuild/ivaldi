package projects

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type ProjectResponse struct {
	Body api.Response[workspace.ProjectSnapshot]
}

type ProjectListResponse struct {
	Body api.Response[pagination.Result[workspace.ProjectSnapshot]]
}

func projectResponse(p *workspace.Project) *ProjectResponse {
	return &ProjectResponse{Body: api.SuccessResponse(nil, p.Snapshot())}
}

func projectListResponse(page *pagination.Result[*workspace.Project]) *ProjectListResponse {
	items := make([]workspace.ProjectSnapshot, 0, len(page.Items))
	for _, p := range page.Items {
		items = append(items, p.Snapshot())
	}
	return &ProjectListResponse{Body: api.SuccessResponse(nil, pagination.Result[workspace.ProjectSnapshot]{
		Items: items, TotalCount: page.TotalCount, HasNext: page.HasNext, HasPrevious: page.HasPrevious,
		NextCursor: page.NextCursor, PreviousCursor: page.PreviousCursor,
	})}
}
