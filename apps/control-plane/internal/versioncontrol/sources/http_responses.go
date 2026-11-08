package sources

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type SourceResponse struct {
	Body api.Response[versioncontrol.SourceSnapshot]
}

type SourceListResponse struct {
	Body api.Response[pagination.Result[versioncontrol.SourceSnapshot]]
}

func sourceResponse(s *versioncontrol.Source) *SourceResponse {
	return &SourceResponse{Body: api.SuccessResponse(nil, s.Snapshot())}
}
