package agentapi

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
)

type TargetsHTTPResponse struct {
	Body api.Response[TargetsResponse]
}

type AssignmentsHTTPResponse struct {
	Body api.Response[[]delivery.DeploymentAssignment]
}

type ObservationHTTPResponse struct {
	Body api.Response[delivery.DeploymentSnapshot]
}

type HeartbeatHTTPResponse struct{}
