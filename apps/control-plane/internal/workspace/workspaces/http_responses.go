package workspaces

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
)

type WorkspaceResponse struct {
	Body api.Response[workspace.WorkspaceSnapshot]
}

type WorkspaceListResponse struct {
	Body api.Response[[]workspace.WorkspaceSnapshot]
}

func workspaceResponse(w *workspace.Workspace) *WorkspaceResponse {
	return &WorkspaceResponse{Body: api.SuccessResponse(nil, w.Snapshot())}
}

func workspaceListResponse(ws []*workspace.Workspace) *WorkspaceListResponse {
	items := make([]workspace.WorkspaceSnapshot, 0, len(ws))
	for _, w := range ws {
		items = append(items, w.Snapshot())
	}
	return &WorkspaceListResponse{Body: api.SuccessResponse(nil, items)}
}
