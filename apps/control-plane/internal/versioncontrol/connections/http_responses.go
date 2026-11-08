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

// RepositorySnapshot is a repository a Connection's account lets Ivaldi
// read, as the provider reports it. ExternalID is what connecting it as a
// Source takes.
type RepositorySnapshot struct {
	ExternalID    string `json:"external_id" doc:"The provider's stable repository ID"`
	FullName      string `json:"full_name" doc:"The repository's name, such as acme/checkout"`
	DefaultBranch string `json:"default_branch"`
	Visibility    string `json:"visibility" doc:"public, private, or internal"`
	WebURL        string `json:"web_url"`
	Connected     bool   `json:"connected" doc:"Whether an active Source in the Organization already connects it"`
}

type RepositoryListResponse struct {
	Body api.Response[[]RepositorySnapshot]
}

func repositorySnapshots(repositories []versioncontrol.AvailableRepository) []RepositorySnapshot {
	out := make([]RepositorySnapshot, 0, len(repositories))
	for _, a := range repositories {
		r := a.Repository
		out = append(out, RepositorySnapshot{
			ExternalID: r.ExternalID, FullName: r.FullName, DefaultBranch: r.DefaultBranch,
			Visibility: r.Visibility, WebURL: r.WebURL, Connected: a.Connected,
		})
	}
	return out
}
