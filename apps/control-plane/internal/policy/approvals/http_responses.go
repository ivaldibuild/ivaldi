package approvals

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type ApprovalResponse struct {
	Body api.Response[policy.ApprovalRequestView]
}

type ApprovalListResponse struct {
	Body api.Response[pagination.Result[policy.ApprovalRequestView]]
}
