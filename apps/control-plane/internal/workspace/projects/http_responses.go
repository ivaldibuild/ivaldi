package projects

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
)

type ProjectResponse struct {
	Body api.Response[workspace.ProjectSnapshot]
}

type ProjectListResponse struct {
	Body api.Response[[]workspace.ProjectSnapshot]
}

func projectResponse(p *workspace.Project) *ProjectResponse {
	return &ProjectResponse{Body: api.SuccessResponse(nil, p.Snapshot())}
}

func projectListResponse(ps []*workspace.Project) *ProjectListResponse {
	items := make([]workspace.ProjectSnapshot, 0, len(ps))
	for _, p := range ps {
		items = append(items, p.Snapshot())
	}
	return &ProjectListResponse{Body: api.SuccessResponse(nil, items)}
}
