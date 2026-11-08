package connections

import (
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
)

type ConnectionResponse struct {
	Body api.Response[versioncontrol.ConnectionSnapshot]
}

type ConnectionListResponse struct {
	Body api.Response[[]versioncontrol.ConnectionSnapshot]
}

func connectionResponse(c *versioncontrol.Connection) *ConnectionResponse {
	return &ConnectionResponse{Body: api.SuccessResponse(nil, c.Snapshot())}
}

func snapshots(connections []*versioncontrol.Connection) []versioncontrol.ConnectionSnapshot {
	out := make([]versioncontrol.ConnectionSnapshot, 0, len(connections))
	for _, c := range connections {
		out = append(out, c.Snapshot())
	}
	return out
}

// InstallationStartSnapshot tells the frontend where to send the person to
// install the provider's app.
type InstallationStartSnapshot struct {
	URL       string    `json:"url" doc:"Where to send the person to install the app"`
	ExpiresAt time.Time `json:"expires_at" doc:"When the install must be completed by"`
}

type InstallationStartResponse struct {
	Body api.Response[InstallationStartSnapshot]
}
